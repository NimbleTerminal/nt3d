// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image"
	"math"
)

// FlowMap is an immutable field of normal-UV displacement vectors. Red and
// green encode signed U/V in [-1,1]; 127 or 128 is neutral, blue/alpha ignored.
// Materials sample it at mesh vertices using Geometry.UV, then interpolate
// vectors across each triangle. Mesh density must resolve changes in the field.
// The field changes shading only: it is not a fluid or gameplay current.
type FlowMap struct{ texture *Texture }

// NewFlowMap snapshots an RG image as a linear signed vector field.
func NewFlowMap(src image.Image) (*FlowMap, error) {
	t, err := NewNormalMap(src)
	if err != nil {
		return nil, err
	}
	return &FlowMap{texture: t}, nil
}

// Sample bilinearly samples the field. Repeat wraps both axes; otherwise the
// edges clamp. Invalid coordinates and the zero FlowMap return a zero vector.
func (f *FlowMap) Sample(uv UV, repeat bool) UV {
	if f == nil || f.texture == nil || !validFloat(uv.U, uv.V) {
		return UV{}
	}
	// Decode texels before interpolation so an authored neutral field stays still.
	t := f.texture
	u, v := float64(uv.U), float64(uv.V)
	if repeat {
		u -= math.Floor(u)
		v -= math.Floor(v)
	} else {
		u = math.Max(0, math.Min(1, u))
		v = math.Max(0, math.Min(1, v))
	}
	x, y := u*float64(t.width)-.5, v*float64(t.height)-.5
	ix, iy := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := float32(x-float64(ix)), float32(y-float64(iy))
	coord := func(a, n int) int {
		if repeat {
			return (a%n + n) % n
		}
		return min(n-1, max(0, a))
	}
	decode := func(c byte) float32 {
		if c == 127 || c == 128 {
			return 0
		}
		return float32(c)*2/255 - 1
	}
	var out UV
	for j := range 2 {
		for i := range 2 {
			wx, wy := 1-fx, 1-fy
			if i == 1 {
				wx = fx
			}
			if j == 1 {
				wy = fy
			}
			off := 4 * (coord(iy+j, t.height)*t.width + coord(ix+i, t.width))
			out.U += decode(t.pixels[off]) * wx * wy
			out.V += decode(t.pixels[off+1]) * wx * wy
		}
	}
	return out
}
func materialFlow(m *Material, uv UV) UV {
	if m == nil {
		return UV{}
	}
	return m.FlowMap.Sample(uv, m.FlowRepeat)
}
func flowPhases(phase float32) (a, b, weight float32) {
	a = phase - float32(math.Floor(float64(phase)))
	b = a + .5
	b -= float32(math.Floor(float64(b)))
	return a, b, abs(2*a - 1)
}
func flowNormalSample(m *Material, uv, dx, dy, flow UV, aniso int) [4]float32 {
	if m.FlowMap == nil || m.FlowStrength == 0 || flow == (UV{}) {
		return m.NormalMap.sampleFootprint(uv, dx, dy, m.NormalRepeat, aniso)
	}
	a, b, w := flowPhases(m.FlowPhase)
	sample := func(p float32) [4]float32 {
		shift := UV{flow.U * m.FlowStrength * p, flow.V * m.FlowStrength * p}
		move := func(v UV) UV { return UV{v.U - shift.U, v.V - shift.V} }
		return m.NormalMap.sampleFootprint(move(uv), move(dx), move(dy), m.NormalRepeat, aniso)
	}
	x, y := sample(a), sample(b)
	for i := range x {
		x[i] += (y[i] - x[i]) * w
	}
	return x
}
