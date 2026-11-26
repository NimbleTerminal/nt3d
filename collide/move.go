// Copyright (c) 2026 Neomantra Corp.

package nt3dcollide

import (
	"fmt"
	"math"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// MoveConfig tunes kinematic capsule movement against a Mesh. A zero field
// means its default; a negative or non-finite one is an error.
type MoveConfig struct {
	// MaxSlope is the steepest walkable slope from horizontal, in degrees, at
	// most 89. Zero means 50.
	MaxSlope float64
	// StepHeight is the tallest vertical ledge a grounded capsule steps onto,
	// in meters. Zero disables stepping.
	StepHeight float64
	// GroundSnap is how far downward a grounded capsule snaps to stay glued to
	// ramps and crests when not jumping, in meters. Zero disables snapping.
	GroundSnap float64
	// SkinWidth is the small separation kept from surfaces, in meters. Zero
	// means 0.005.
	SkinWidth float64
}

// Validate reports a configuration MoveCapsule would refuse.
func (c MoveConfig) Validate() error {
	for _, f := range []struct {
		name string
		v    float64
	}{
		{"MaxSlope", c.MaxSlope},
		{"StepHeight", c.StepHeight},
		{"GroundSnap", c.GroundSnap},
		{"SkinWidth", c.SkinWidth},
	} {
		if !nt3dmath.IsFinite(f.v) || f.v < 0 {
			return fmt.Errorf("nt3dcollide: MoveConfig.%s %v must not be negative", f.name, f.v)
		}
	}
	if c.MaxSlope > 89 {
		return fmt.Errorf("nt3dcollide: MoveConfig.MaxSlope %v must be at most 89", c.MaxSlope)
	}
	return nil
}

// resolved fills in the defaults of a valid configuration.
func (c MoveConfig) resolved() (minWalkZ, stepH, snap, skin float64) {
	slope := c.MaxSlope
	if slope == 0 {
		slope = 50
	}
	minWalkZ = math.Cos(slope * math.Pi / 180)
	skin = c.SkinWidth
	if skin == 0 {
		skin = 0.005
	}
	return minWalkZ, c.StepHeight, c.GroundSnap, skin
}

// maxSubsteps bounds the work of one MoveCapsule call. Each substep moves the
// capsule at most half its radius, which is what keeps it from passing
// through a surface, so one call can move it at most maxSubsteps/2 radii.
const maxSubsteps = 4096

// MoveResult reports the capsule's resolved pose, velocity, and contacts after
// MoveCapsule.
type MoveResult struct {
	Position     nt3dmath.Vec3d
	Velocity     nt3dmath.Vec3d
	Grounded     bool
	GroundNormal nt3dmath.Vec3d
	HitWall      bool
	WallNormal   nt3dmath.Vec3d
	ImpactSpeed  float64
}

// MoveCapsule advances a capsule with base at pos by vel*dt, resolving
// penetrations, sliding along walls and slopes, stepping up low curbs when
// grounded, and snapping onto walkable slopes. A nil or empty mesh is empty
// space: the capsule moves freely.
//
// It moves in substeps of at most half the capsule's radius, so the capsule
// does not pass through a surface however fast it goes.
//
// Wrong input is an error and moves nothing: the result then holds pos and
// vel as given. That covers a capsule or configuration that fails Validate, a
// position, velocity or dt that is not finite, a negative dt, and a move
// longer than 2048 radii in one call.
func MoveCapsule(m *Mesh, cap Capsule, pos, vel nt3dmath.Vec3d, dt float64, wasGrounded bool, cfg MoveConfig) (MoveResult, error) {
	res := MoveResult{
		Position:     pos,
		Velocity:     vel,
		GroundNormal: nt3dmath.Vec3d{Z: 1},
	}
	if err := cap.Validate(); err != nil {
		return res, err
	}
	if err := cfg.Validate(); err != nil {
		return res, err
	}
	if !pos.IsFinite() || !vel.IsFinite() {
		return res, fmt.Errorf("nt3dcollide: capsule position %v and velocity %v must be finite", pos, vel)
	}
	if !nt3dmath.IsFinite(dt) || dt < 0 {
		return res, fmt.Errorf("nt3dcollide: dt %v must be finite and not negative", dt)
	}
	if dt == 0 {
		return res, nil
	}
	totalDisp := vecLen(vecScale(vel, dt))
	maxStep := float64(cap.Radius * 0.5)
	if !(totalDisp <= float64(maxStep*maxSubsteps)) {
		return res, fmt.Errorf("nt3dcollide: a move of %.3g m in one call is more than %d radii; call with a smaller dt", totalDisp, maxSubsteps/2)
	}
	if m == nil || len(m.tris) == 0 {
		res.Position = pos.Add(vecScale(vel, dt))
		return res, nil
	}
	minWalkZ, stepH, snapDist, skin := cfg.resolved()

	initialVel := vel
	curPos := pos
	curVel := vel

	// Resolve any initial overlap first so a capsule spawned touching a surface
	// starts cleanly separated.
	curPos, _ = depenetrate(m, cap, curPos, skin, minWalkZ, &res, initialVel, false)

	steps := max(1, min(maxSubsteps, int(math.Ceil(totalDisp/maxStep))))
	subDt := dt / float64(steps)

	for range steps {
		if !(subDt > 1e-9) || vecLenSq(curVel) <= 1e-14 {
			continue
		}
		want := curPos.Add(vecScale(curVel, subDt))
		resolved, contacts := depenetrate(m, cap, want, skin, minWalkZ, &res, initialVel, true)
		if len(contacts) == 0 {
			curPos = resolved
			continue
		}

		// Try stepping over a low curb when grounded or moving mostly horizontally.
		if stepH > 0 && (wasGrounded || res.Grounded) && hasWallContact(contacts, minWalkZ) {
			if stepped, ok := tryStepUp(m, cap, curPos, vecScale(curVel, subDt), stepH, skin, minWalkZ); ok {
				horizGain := math.Hypot(stepped.X-curPos.X, stepped.Y-curPos.Y)
				slideGain := math.Hypot(resolved.X-curPos.X, resolved.Y-curPos.Y)
				if horizGain > slideGain+1e-3 {
					curPos = stepped
					res.Grounded = true
					if curVel.Z < 0 {
						curVel.Z = 0
					}
					continue
				}
			}
		}

		// Slide: take out of the velocity what points into each distinct
		// surface this substep touched.
		curPos = resolved
		var planes [4]nt3dmath.Vec3d
		nPlanes := 0
		for _, n := range contacts {
			if dot(curVel, n) >= 0 {
				continue
			}
			duplicate := false
			for k := range nPlanes {
				if dot(planes[k], n) > 0.995 {
					duplicate = true
					break
				}
			}
			if !duplicate && nPlanes < len(planes) {
				planes[nPlanes] = n
				nPlanes++
			}
		}
		curVel = clipVelocity(curVel, planes[:nPlanes])
	}

	// Ground probe and downward snap when grounded (or landing with vel.Z <= 0).
	probeDist := float64(skin * 4)
	if wasGrounded && initialVel.Z <= 0.05 && snapDist > probeDist {
		probeDist = snapDist
	}
	if initialVel.Z <= 0.05 {
		if snappedZ, gNorm, ok := probeGround(m, cap, curPos, probeDist, skin, minWalkZ); ok {
			curPos.Z = snappedZ
			res.Grounded = true
			res.GroundNormal = gNorm
			if into := dot(curVel, gNorm); into < 0 {
				curVel = curVel.Sub(vecScale(gNorm, into))
			}
		}
	}

	res.Position = curPos
	res.Velocity = curVel
	return res, nil
}

func hasWallContact(normals []nt3dmath.Vec3d, minWalkZ float64) bool {
	for _, n := range normals {
		if n.Z < minWalkZ && n.Z > -0.7 {
			return true
		}
	}
	return false
}

func clipVelocity(v nt3dmath.Vec3d, planes []nt3dmath.Vec3d) nt3dmath.Vec3d {
	switch len(planes) {
	case 0:
		return v
	case 1:
		d := dot(v, planes[0])
		if d < 0 {
			return v.Sub(vecScale(planes[0], d))
		}
		return v
	case 2:
		out := v
		for _, n := range planes {
			if d := dot(out, n); d < 0 {
				out = out.Sub(vecScale(n, d))
			}
		}
		if dot(out, planes[0]) < -1e-6 || dot(out, planes[1]) < -1e-6 {
			crease := cross(planes[0], planes[1])
			l := vecLen(crease)
			if !(l > 1e-6) {
				return nt3dmath.Vec3d{}
			}
			crease = vecScale(crease, 1/l)
			return vecScale(crease, dot(v, crease))
		}
		return out
	default:
		return nt3dmath.Vec3d{}
	}
}

func tryStepUp(m *Mesh, cap Capsule, start, delta nt3dmath.Vec3d, stepH, skin, minWalkZ float64) (nt3dmath.Vec3d, bool) {
	up := nt3dmath.Vec3d{X: start.X, Y: start.Y, Z: start.Z + stepH}
	if overlapsAny(m, cap, up) {
		return nt3dmath.Vec3d{}, false
	}
	across := nt3dmath.Vec3d{X: up.X + delta.X, Y: up.Y + delta.Y, Z: up.Z}
	var dummy MoveResult
	across, _ = depenetrate(m, cap, across, skin, minWalkZ, &dummy, nt3dmath.Vec3d{}, false)
	snappedZ, _, ok := probeGround(m, cap, across, stepH+skin*2, skin, minWalkZ)
	if !ok || snappedZ < start.Z-skin*2 || snappedZ > start.Z+stepH+skin*2 {
		return nt3dmath.Vec3d{}, false
	}
	landed := nt3dmath.Vec3d{X: across.X, Y: across.Y, Z: snappedZ}
	if overlapsAny(m, cap, landed) {
		return nt3dmath.Vec3d{}, false
	}
	return landed, true
}

func overlapsAny(m *Mesh, cap Capsule, pos nt3dmath.Vec3d) bool {
	lo, hi := cap.Bounds(pos, 0)
	segA, segB := cap.Segment(pos)
	hit := false
	m.queryBox(lo, hi, func(t Triangle) {
		if hit {
			return
		}
		sp, tp := closestSegmentTriangle(segA, segB, t)
		if vecLenSq(sp.Sub(tp)) < float64((cap.Radius-1e-4)*(cap.Radius-1e-4)) {
			hit = true
		}
	})
	return hit
}

func probeGround(m *Mesh, cap Capsule, pos nt3dmath.Vec3d, maxDrop, skin, minWalkZ float64) (snappedZ float64, normal nt3dmath.Vec3d, ok bool) {
	r := cap.Radius
	lo := nt3dmath.Vec3d{X: pos.X - r, Y: pos.Y - r, Z: pos.Z - maxDrop - skin*2}
	hi := nt3dmath.Vec3d{X: pos.X + r, Y: pos.Y + r, Z: pos.Z + r + skin*2}
	bestZ := math.Inf(-1)
	r2 := float64(r * r)
	probeCenter := nt3dmath.Vec3d{X: pos.X, Y: pos.Y, Z: pos.Z + r}

	m.queryBox(lo, hi, func(t Triangle) {
		if t.Normal.Z < minWalkZ {
			return
		}
		// Closest point on the walkable triangle to the bottom sphere's XY/3D center.
		cp := closestPointTriangle(probeCenter, t.A, t.B, t.C)
		dx, dy := pos.X-cp.X, pos.Y-cp.Y
		dxy2 := float64(dx*dx) + float64(dy*dy)
		if dxy2 >= float64((r-1e-5)*(r-1e-5)) {
			return
		}
		// Exact height of the triangle plane at (pos.X, pos.Y) when inside, or
		// hemisphere support over the closest edge/vertex cp.
		supportZ := cp.Z + math.Sqrt(math.Max(0, r2-dxy2)) - r + skin
		if supportZ <= pos.Z+skin*4 && supportZ >= pos.Z-maxDrop-skin*2 && supportZ > bestZ {
			bestZ = supportZ
			normal = t.Normal
			ok = true
		}
	})
	return bestZ, normal, ok
}

func depenetrate(m *Mesh, cap Capsule, pos nt3dmath.Vec3d, skin, minWalkZ float64, res *MoveResult, impactRef nt3dmath.Vec3d, record bool) (nt3dmath.Vec3d, []nt3dmath.Vec3d) {
	var contacts []nt3dmath.Vec3d
	targetR := cap.Radius + skin
	for range 4 {
		lo, hi := cap.Bounds(pos, skin+1e-3)
		var near []Triangle
		m.queryBox(lo, hi, func(t Triangle) { near = append(near, t) })
		moved := false
		for _, t := range near {
			segA, segB := cap.Segment(pos)
			sp, tp := closestSegmentTriangle(segA, segB, t)
			diff := sp.Sub(tp)
			d2 := vecLenSq(diff)
			if d2 >= float64(targetR*targetR) {
				continue
			}
			var n nt3dmath.Vec3d
			var pen float64
			if d2 > 1e-14 {
				d := math.Sqrt(d2)
				n = vecScale(diff, 1/d)
				pen = targetR - d
			} else {
				n = t.Normal
				if dot(segA.Sub(t.A), n) < 0 {
					n = vecScale(n, -1)
				}
				pen = targetR
			}
			pos = pos.Add(vecScale(n, pen))
			moved = true
			if record {
				contacts = append(contacts, n)
				into := -dot(impactRef, n)
				if into > res.ImpactSpeed {
					res.ImpactSpeed = into
				}
				if n.Z >= minWalkZ {
					// The most upright walkable contact is the ground.
					if !res.Grounded || n.Z > res.GroundNormal.Z {
						res.GroundNormal = n
					}
					res.Grounded = true
				} else if n.Z > -0.7 {
					res.HitWall = true
					res.WallNormal = n
				}
			}
		}
		if !moved {
			break
		}
	}
	return pos, contacts
}

// closestSegmentTriangle returns the closest pair of points (on segment [p, q]
// and on triangle t).
func closestSegmentTriangle(p, q nt3dmath.Vec3d, t Triangle) (segPt, triPt nt3dmath.Vec3d) {
	bestD2 := math.Inf(1)
	consider := func(sp, tp nt3dmath.Vec3d) {
		d2 := vecLenSq(sp.Sub(tp))
		if d2 < bestD2 {
			bestD2 = d2
			segPt, triPt = sp, tp
		}
	}

	// Endpoints against triangle.
	consider(p, closestPointTriangle(p, t.A, t.B, t.C))
	consider(q, closestPointTriangle(q, t.A, t.B, t.C))

	// Segment against each of the three triangle edges.
	edges := [3][2]nt3dmath.Vec3d{{t.A, t.B}, {t.B, t.C}, {t.C, t.A}}
	for _, e := range edges {
		sp, ep := closestSegmentSegment(p, q, e[0], e[1])
		consider(sp, ep)
	}

	// Segment crossing the triangle's plane in its interior.
	pq := q.Sub(p)
	denom := dot(pq, t.Normal)
	if math.Abs(denom) > 1e-12 {
		s := dot(t.A.Sub(p), t.Normal) / denom
		if s > 0 && s < 1 {
			sp := p.Add(vecScale(pq, s))
			consider(sp, closestPointTriangle(sp, t.A, t.B, t.C))
		}
	}
	return segPt, triPt
}

// closestPointTriangle implements Ericson's Voronoi-region closest point on
// triangle (a, b, c) to point p.
func closestPointTriangle(p, a, b, c nt3dmath.Vec3d) nt3dmath.Vec3d {
	ab := b.Sub(a)
	ac := c.Sub(a)
	ap := p.Sub(a)
	d1 := dot(ab, ap)
	d2 := dot(ac, ap)
	if d1 <= 0 && d2 <= 0 {
		return a
	}

	bp := p.Sub(b)
	d3 := dot(ab, bp)
	d4 := dot(ac, bp)
	if d3 >= 0 && d4 <= d3 {
		return b
	}

	vc := float64(d1*d4) - float64(d3*d2)
	if vc <= 0 && d1 >= 0 && d3 <= 0 {
		v := d1 / (d1 - d3)
		return a.Add(vecScale(ab, v))
	}

	cp := p.Sub(c)
	d5 := dot(ab, cp)
	d6 := dot(ac, cp)
	if d6 >= 0 && d5 <= d6 {
		return c
	}

	vb := float64(d5*d2) - float64(d1*d6)
	if vb <= 0 && d2 >= 0 && d6 <= 0 {
		w := d2 / (d2 - d6)
		return a.Add(vecScale(ac, w))
	}

	va := float64(d3*d6) - float64(d5*d4)
	if va <= 0 && (d4-d3) >= 0 && (d5-d6) >= 0 {
		w := (d4 - d3) / ((d4 - d3) + (d5 - d6))
		return b.Add(vecScale(c.Sub(b), w))
	}

	denom := 1 / (va + vb + vc)
	v := float64(vb * denom)
	w := float64(vc * denom)
	return a.Add(vecScale(ab, v).Add(vecScale(ac, w)))
}

func closestSegmentSegment(p1, q1, p2, q2 nt3dmath.Vec3d) (c1, c2 nt3dmath.Vec3d) {
	d1 := q1.Sub(p1)
	d2 := q2.Sub(p2)
	r := p1.Sub(p2)
	a := dot(d1, d1)
	e := dot(d2, d2)
	f := dot(d2, r)

	var s, t float64
	switch {
	case a <= eps && e <= eps:
		return p1, p2
	case a <= eps:
		t = clamp01(f / e)
	default:
		c := dot(d1, r)
		if e <= eps {
			s = clamp01(-c / a)
		} else {
			b := dot(d1, d2)
			denom := float64(a*e) - float64(b*b)
			if denom != 0 {
				s = clamp01((float64(b*f) - float64(c*e)) / denom)
			}
			t = (float64(b*s) + f) / e
			if t < 0 {
				t = 0
				s = clamp01(-c / a)
			} else if t > 1 {
				t = 1
				s = clamp01((b - c) / a)
			}
		}
	}
	return p1.Add(vecScale(d1, s)), p2.Add(vecScale(d2, t))
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
