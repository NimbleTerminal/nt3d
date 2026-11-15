// Copyright (c) 2026 Neomantra Corp.

package nt3dcam

import (
	"math"
	"sort"
	"time"

	nt3dease "nimbleterminal.dev/nt3d/cam/ease"
	nt3dframes "nimbleterminal.dev/nt3d/cam/frames"
)

// Eval evaluates the camera path purely and deterministically at timestamp t
// using the default Resolver configured at NewPath (if any).
func (p *Path) Eval(t time.Duration) CameraState {
	return p.EvalOverrides(t, EvalOverrides{Resolver: p.resolver})
}

// EvalWith evaluates the camera path at timestamp t using r to resolve external
// target references ("node:...", "panel:...", "path:...").
func (p *Path) EvalWith(t time.Duration, r Resolver) CameraState {
	return p.EvalOverrides(t, EvalOverrides{Resolver: r})
}

// EvalOverrides evaluates the camera path at timestamp t with optional dynamic
// inlet overrides (such as live Nebula target or roll streams).
func (p *Path) EvalOverrides(t time.Duration, ov EvalOverrides) CameraState {
	if p == nil {
		return DefaultCameraState()
	}
	res := ov.Resolver
	if res == nil {
		res = p.resolver
	}

	tEff, loopReversed := p.wrapTime(t)
	s, segReverse := p.evalTiming(tEff)
	movingBackward := loopReversed != segReverse

	posCurve, tanCurve := p.lut.Eval(s)
	pos := fromCurveVec(posCurve)

	baseRot := p.evalLook(tEff, s, pos, fromCurveVec(tanCurve), movingBackward, res, ov.Target)

	rollRad := nt3dease.Sample(p.rollKeys, tEff, 0)
	if ov.RollRad != nil {
		rollRad = *ov.RollRad
	}
	rollRad += ov.RollAddRad
	if rollRad != 0 {
		// Local post-multiplication around camera-local forward (-Z):
		// baseRot ⊗ exp((rollRad/2) * (0, 0, -1)).
		half := 0.5 * rollRad
		localRoll := nt3dframes.Quat{Z: -math.Sin(half), W: math.Cos(half)}
		baseRot = baseRot.Mul(localRoll).Normalize()
	}

	return CameraState{
		Pos:       pos,
		Rot:       fromFrameQuat(baseRot),
		FovY:      nt3dease.Sample(p.fovKeys, tEff, DefaultFovY),
		FocusDist: nt3dease.Sample(p.focusKeys, tEff, 0),
		Aperture:  nt3dease.Sample(p.apertKeys, tEff, 0),
		Near:      p.cfg.Lens.Near,
		Far:       p.cfg.Lens.Far,
	}
}

func (p *Path) wrapTime(t time.Duration) (tEff time.Duration, reversed bool) {
	d := p.cfg.Duration
	if d <= 0 {
		return 0, false
	}
	switch p.cfg.Loop {
	case LoopRepeat:
		rem := t % d
		if rem < 0 {
			rem += d
		}
		return rem, false
	case LoopPingPong:
		period := 2 * d
		rem := t % period
		if rem < 0 {
			rem += period
		}
		if rem > d {
			return period - rem, true
		}
		return rem, false
	default:
		if t <= 0 {
			return 0, false
		}
		if t >= d {
			return d, false
		}
		return t, false
	}
}

func (p *Path) evalTiming(t time.Duration) (s float64, reverse bool) {
	n := len(p.timingSegs)
	if n == 0 {
		return p.timingInit, false
	}
	if t <= p.timingSegs[0].t0 {
		seg := p.timingSegs[0]
		return seg.s0, seg.s1 < seg.s0
	}
	last := p.timingSegs[n-1]
	if t >= last.t1 {
		return last.s1, last.s1 < last.s0
	}
	idx := sort.Search(n, func(i int) bool { return p.timingSegs[i].t1 > t })
	seg := p.timingSegs[idx]
	dt := float64(seg.t1 - seg.t0)
	if dt <= 0 {
		return seg.s1, seg.s1 < seg.s0
	}
	u := float64(t-seg.t0) / dt
	raw := seg.s0 + seg.ease.Eval(u)*(seg.s1-seg.s0)
	s = math.Max(0, math.Min(1, raw))
	derivSign := (seg.s1 - seg.s0) * seg.ease.Derivative(u)
	return s, derivSign < 0
}

func (p *Path) evalLook(t time.Duration, s float64, pos, tangent Vec3d, movingBackward bool, res Resolver, targetOverride *Vec3d) nt3dframes.Quat {
	n := len(p.lookSegs)
	if n == 0 {
		return p.transport.Eval(s, false)
	}
	// Find the highest segment index whose From <= t.
	active := sort.Search(n, func(i int) bool { return p.lookSegs[i].from > t }) - 1
	if active < 0 {
		active = 0
	}

	// Start from the earliest segment that still has an active blend window at t,
	// so overlapping blends remain pure and C0-continuous.
	start := active
	for start > 0 {
		seg := p.lookSegs[start]
		if seg.blend > 0 && t < seg.from+seg.blend {
			start--
		} else {
			break
		}
	}

	cur := p.evalLookSegment(p.lookSegs[start], t, s, pos, tangent, movingBackward, res, targetOverride)
	for i := start + 1; i <= active; i++ {
		seg := p.lookSegs[i]
		next := p.evalLookSegment(seg, t, s, pos, tangent, movingBackward, res, targetOverride)
		if seg.blend <= 0 || t >= seg.from+seg.blend {
			cur = next
			continue
		}
		u := float64(t-seg.from) / float64(seg.blend)
		w := seg.blendEase.Eval(u)
		cur = nt3dframes.Slerp(cur, next, w)
	}
	return cur
}

func (p *Path) evalLookSegment(seg compiledLookSeg, t time.Duration, s float64, pos, tangent Vec3d, movingBackward bool, res Resolver, targetOverride *Vec3d) nt3dframes.Quat {
	switch seg.mode {
	case LookTangent:
		return p.transport.Eval(s, seg.reverseWithMotion && movingBackward)
	case LookKeyframes:
		return seg.quatTrack.Eval(t)
	default: // LookTarget
		var target Vec3d
		if targetOverride != nil {
			target = *targetOverride
		} else if seg.targetRef != "" {
			if res != nil {
				if info, ok := res.ResolveTarget(seg.targetRef, t); ok && info.Pos.IsFinite() {
					target = info.Pos
				} else {
					target = seg.fallback
				}
			} else {
				target = seg.fallback
			}
		} else {
			target = seg.point
		}
		dir := target.Sub(pos)
		if dir.LenSq() < 1e-18 {
			if tangent.LenSq() > 1e-18 {
				dir = tangent
			} else {
				dir = Vec3d{Y: 1}
			}
		}
		return nt3dframes.LookRotation(dir.toCurve(), p.worldUp.toCurve())
	}
}
