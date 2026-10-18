// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"math"
	"slices"
	"sort"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// Channel identifies a track's destination. UserChannel is delivered to Apply.
type Channel uint8

const (
	Translation Channel = iota
	Rotation
	Scale
	MorphWeights
	UserChannel
)

type Interpolation uint8

const (
	Linear Interpolation = iota
	Step
	CubicSpline
)

type LoopMode uint8

const (
	Once LoopMode = iota
	Loop
	PingPong
)

// Track holds tightly packed key values. Rotation uses XYZW quaternions.
// CubicSpline stores in-tangent, value, out-tangent for each key, in units/second.
// Width is 3 for translation/scale, 4 for rotation, and the weight/channel count otherwise.
type Track struct {
	Node          NodeID
	Channel       Channel
	Name          string
	Width         int
	Times         []float32
	Values        []float32
	Interpolation Interpolation
}

// AnimationClip is format-independent and may be reused by independent players.
type AnimationClip struct {
	Name   string
	Tracks []Track
}

func (c AnimationClip) Duration() float64 {
	var d float32
	for _, t := range c.Tracks {
		if len(t.Times) > 0 {
			d = max(d, t.Times[len(t.Times)-1])
		}
	}
	return float64(d)
}
func (c AnimationClip) Validate() error {
	for _, t := range c.Tracks {
		w := t.Width
		mult := 1
		if t.Interpolation == CubicSpline {
			mult = 3
		}
		if w <= 0 || len(t.Times) == 0 || t.Interpolation > CubicSpline || t.Channel > UserChannel || len(t.Values)/w/mult != len(t.Times) || len(t.Values) != len(t.Times)*w*mult {
			return fmt.Errorf("nt3d: invalid animation track")
		}
		if (t.Channel == Translation || t.Channel == Scale) && w != 3 || t.Channel == Rotation && w != 4 {
			return fmt.Errorf("nt3d: invalid track width")
		}
		for i, v := range t.Times {
			if !validFloat(v) || v < 0 || i > 0 && v <= t.Times[i-1] {
				return fmt.Errorf("nt3d: key times must be finite and increasing")
			}
		}
		for _, v := range t.Values {
			if !validFloat(v) {
				return fmt.Errorf("nt3d: non-finite animation value")
			}
		}
		if t.Channel == Rotation {
			for i := range t.Times {
				at := i * w * mult
				if mult == 3 {
					at += w
				}
				q := t.Values[at : at+4]
				if dot4(q, q) == 0 {
					return fmt.Errorf("nt3d: zero rotation quaternion")
				}
			}
		}
	}
	return nil
}
func dot4(a, b []float32) float32 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] + a[3]*b[3] }
func normalizeQ(q []float32) {
	l := float32(math.Sqrt(float64(dot4(q, q))))
	if l == 0 {
		q[0], q[1], q[2], q[3] = 0, 0, 0, 1
		return
	}
	for i := range q {
		q[i] /= l
	}
}
func mixQ(a, b []float32, t float32) []float32 {
	a = slices.Clone(a)
	b = slices.Clone(b)
	normalizeQ(a)
	normalizeQ(b)
	d := dot4(a, b)
	if d < 0 {
		for i := range b {
			b[i] = -b[i]
		}
		d = -d
	}
	x, y := 1-t, t
	if d < .9995 {
		theta := math.Acos(float64(d))
		den := math.Sin(theta)
		x = float32(math.Sin(float64(1-t)*theta) / den)
		y = float32(math.Sin(float64(t)*theta) / den)
	}
	for i := range a {
		a[i] = x*a[i] + y*b[i]
	}
	normalizeQ(a)
	return a
}
func (t Track) sample(at float64) []float32 {
	n, w := len(t.Times), t.Width
	stride := w
	offset := 0
	if t.Interpolation == CubicSpline {
		stride = 3 * w
		offset = w
	}
	value := func(i int) []float32 { return t.Values[i*stride+offset : i*stride+offset+w] }
	hi := sort.Search(n, func(i int) bool { return float64(t.Times[i]) > at })
	if hi == 0 {
		return slices.Clone(value(0))
	}
	if hi == n {
		return slices.Clone(value(n - 1))
	}
	lo := hi - 1
	a, b := value(lo), value(hi)
	dt := t.Times[hi] - t.Times[lo]
	u := float32((at - float64(t.Times[lo])) / float64(dt))
	out := slices.Clone(a)
	if t.Interpolation == Step {
		return out
	}
	if t.Channel == Rotation && t.Interpolation == Linear {
		return mixQ(a, b, u)
	}
	for k := range out {
		if t.Interpolation == CubicSpline {
			u2, u3 := u*u, u*u*u
			out[k] = (2*u3-3*u2+1)*a[k] + (u3-2*u2+u)*dt*t.Values[lo*stride+2*w+k] + (-2*u3+3*u2)*b[k] + (u3-u2)*dt*t.Values[hi*stride+k]
		} else {
			out[k] = a[k] + u*(b[k]-a[k])
		}
	}
	if t.Channel == Rotation {
		normalizeQ(out)
	}
	return out
}

// TRS composes translation, a normalized XYZW quaternion, and scale.
func TRS(t nt3dmath.Vec3, q [4]float32, s nt3dmath.Vec3) nt3dmath.Mat4 {
	normalizeQ(q[:])
	x, y, z, w := q[0], q[1], q[2], q[3]
	return nt3dmath.Mat4{(1 - 2*(y*y+z*z)) * s.X, 2 * (x*y + z*w) * s.X, 2 * (x*z - y*w) * s.X, 0, 2 * (x*y - z*w) * s.Y, (1 - 2*(x*x+z*z)) * s.Y, 2 * (y*z + x*w) * s.Y, 0, 2 * (x*z + y*w) * s.Z, 2 * (y*z - x*w) * s.Z, (1 - 2*(x*x+y*y)) * s.Z, 0, t.X, t.Y, t.Z, 1}
}

// Animation is a scene-bound player. Call Step on the scene's owning goroutine.
// Sample works while paused. Apply receives custom channels; it must not retain values.
type Animation struct {
	Scene        *Scene
	Clip         AnimationClip
	Time, Speed  float64
	Mode         LoopMode
	Paused       bool
	Apply        func(name string, values []float32)
	lastRevision uint64
	lastTime     float64
	lastMode     LoopMode
	sampled      bool
}

func NewAnimation(s *Scene, c AnimationClip) (*Animation, error) {
	if s == nil {
		return nil, fmt.Errorf("nt3d: nil animation scene")
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	a := &Animation{Scene: s, Clip: c, Speed: 1}
	a.Clip.Tracks = slices.Clone(c.Tracks)
	for i, t := range c.Tracks {
		a.Clip.Tracks[i].Times = slices.Clone(t.Times)
		a.Clip.Tracks[i].Values = slices.Clone(t.Values)
		if t.Channel != UserChannel {
			_, ok := s.Node(t.Node)
			if !ok {
				return nil, fmt.Errorf("nt3d: animation names missing node")
			}
		}
	}
	return a, nil
}
func (a *Animation) Step(seconds float64) error {
	if a.Paused {
		return nil
	}
	return a.Sample(a.Time + seconds*a.Speed)
}
func (a *Animation) Sample(seconds float64) error { return a.SampleBlend(seconds, 1) }

// SampleBlend blends a sample into the current pose. For layered blending,
// sample the base clip first each frame, then apply overlays with weights 0..1.
func (a *Animation) SampleBlend(seconds float64, weight float32) error {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || !validFloat(weight) || weight < 0 || weight > 1 {
		return fmt.Errorf("nt3d: invalid animation time or weight")
	}
	if weight == 1 && a.sampled && a.lastTime == seconds && a.lastMode == a.Mode && a.lastRevision == a.Scene.Revision() {
		a.Time = seconds
		return nil
	}
	at := seconds
	d := a.Clip.Duration()
	if d > 0 {
		switch a.Mode {
		case Loop:
			at = math.Mod(at, d)
			if at < 0 {
				at += d
			}
		case PingPong:
			at = math.Mod(at, 2*d)
			if at < 0 {
				at += 2 * d
			}
			if at > d {
				at = 2*d - at
			}
		default:
			at = max(0, min(d, at))
		}
	}
	// Preserve untracked matrix components, including static shear. Rotation
	// tracks require a TRS basis, as glTF does.
	matrices := map[NodeID]nt3dmath.Mat4{}
	scales := map[NodeID]nt3dmath.Vec3{}
	for _, t := range a.Clip.Tracks {
		v := t.sample(at)
		if t.Channel == UserChannel {
			if a.Apply != nil {
				a.Apply(t.Name, v)
			}
			continue
		}
		if t.Channel == MorphWeights {
			old, err := a.Scene.MorphWeights(t.Node)
			if err != nil {
				return err
			}
			if len(old) != len(v) {
				return fmt.Errorf("nt3d: morph track width mismatch")
			}
			for i := range v {
				v[i] = old[i] + weight*(v[i]-old[i])
			}
			if err = a.Scene.SetMorphWeights(t.Node, v); err != nil {
				return err
			}
			continue
		}
		m, ok := matrices[t.Node]
		if !ok {
			n, exists := a.Scene.nodes[t.Node]
			if !exists {
				return fmt.Errorf("nt3d: animated node removed")
			}
			m = n.Local
			sc := n.animationScale
			if n.animationMatrix != m {
				sc = nt3dmath.Vec3{}
				lens := []*float32{&sc.X, &sc.Y, &sc.Z}
				for k := range 3 {
					*lens[k] = float32(math.Sqrt(float64(m[k*4]*m[k*4] + m[k*4+1]*m[k*4+1] + m[k*4+2]*m[k*4+2])))
				}
			}
			scales[t.Node] = sc
		}
		sc := scales[t.Node]
		lens := []*float32{&sc.X, &sc.Y, &sc.Z}
		switch t.Channel {
		case Translation:
			for k := range 3 {
				m[12+k] += weight * (v[k] - m[12+k])
			}
		case Scale:
			for k := range 3 {
				l := *lens[k]
				if l == 0 {
					return fmt.Errorf("nt3d: singular scale")
				}
				next := l + weight*(v[k]-l)
				factor := next / l
				for j := range 3 {
					m[k*4+j] *= factor
				}
				*lens[k] = next
			}
		case Rotation:
			qv := slices.Clone(v)
			normalizeQ(qv)
			if weight != 1 {
				basis := m
				for k := range 3 {
					for j := range 3 {
						basis[k*4+j] /= *lens[k]
					}
				}
				q := matrixQ(basis)
				qv = mixQ(q[:], v, weight)
			}
			m = TRS(nt3dmath.Vec3{X: m[12], Y: m[13], Z: m[14]}, [4]float32(qv), sc)
		}
		scales[t.Node] = sc
		matrices[t.Node] = m
	}
	for id, m := range matrices {
		if a.Scene.nodes[id].Local != m {
			if err := a.Scene.SetTransform(id, m); err != nil {
				return err
			}
		}
		a.Scene.nodes[id].animationMatrix = m
		a.Scene.nodes[id].animationScale = scales[id]
	}
	a.Time = seconds
	if err := a.Scene.UpdateDeformations(); err != nil {
		return err
	}
	a.sampled = weight == 1
	a.lastTime = seconds
	a.lastMode = a.Mode
	a.lastRevision = a.Scene.Revision()
	return nil
}
func matrixQ(m nt3dmath.Mat4) [4]float32 {
	for k := range 3 {
		l := float32(math.Sqrt(float64(m[k*4]*m[k*4] + m[k*4+1]*m[k*4+1] + m[k*4+2]*m[k*4+2])))
		for j := range 3 {
			m[k*4+j] /= l
		}
	}
	var q [4]float32
	tr := m[0] + m[5] + m[10]
	if tr > 0 {
		s := float32(math.Sqrt(float64(tr+1))) * 2
		q = [4]float32{(m[6] - m[9]) / s, (m[8] - m[2]) / s, (m[1] - m[4]) / s, s / 4}
	} else {
		i := 0
		if m[5] > m[0] {
			i = 1
		}
		if m[10] > m[i*5] {
			i = 2
		}
		j, k := (i+1)%3, (i+2)%3
		s := float32(math.Sqrt(float64(1+m[i*5]-m[j*5]-m[k*5]))) * 2
		q[i] = s / 4
		q[j] = (m[j*4+i] + m[i*4+j]) / s
		q[k] = (m[k*4+i] + m[i*4+k]) / s
		q[3] = (m[j*4+k] - m[k*4+j]) / s
	}
	normalizeQ(q[:])
	return q
}
