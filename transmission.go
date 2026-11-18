// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"math"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func opticalParams(m *Material, p PointStyle) (o [8]float32) {
	if m != nil {
		o[0], o[1], o[2], o[3], o[4] = m.Absorption.X, m.Absorption.Y, m.Absorption.Z, m.Transmission, m.Refraction
		o[6], o[7] = m.FlowPhase, m.FlowStrength
	}
	o[5] = p.SoftDistance
	return
}

// needsSceneSnapshot reports whether a drawn node samples the opaque scene:
// hidden and culled transmitters cost no capture passes.
func needsSceneSnapshot(f Frame, drawn []bool) bool {
	for i := range f.Draws {
		g := &f.Draws[i].Geometry
		if !drawn[i] {
			continue
		}
		if transmitting(g) || blendedPoints(g) && g.PointStyle.SoftDistance > 0 {
			return true
		}
	}
	return false
}

func sceneWorld(inv nt3dmath.Mat4, x, y, z float32, w, h int) nt3dmath.Vec3 {
	a, b, c, d := inv.Transform(nt3dmath.Vec3{X: 2*x/float32(w) - 1, Y: 1 - 2*y/float32(h), Z: z})
	if abs(d) < 1e-8 {
		return nt3dmath.Vec3{}
	}
	return nt3dmath.Vec3{X: a / d, Y: b / d, Z: c / d}
}
func sceneGap(inv nt3dmath.Mat4, x, y, z, back float32, w, h int) float32 {
	if back >= 1 {
		return 1000
	}
	if back <= z {
		return 0
	}
	a := sceneWorld(inv, x, y, z, w, h)
	b := sceneWorld(inv, x, y, back, w, h)
	v := b.Sub(a)
	return min(1000, float32(math.Sqrt(float64(v.Dot(v)))))
}

func transmitSoftware(rgb *[3]float32, m *Material, n, right, up nt3dmath.Vec3, x, y, z float32, w, h int, inv nt3dmath.Mat4, pixels []byte, depth []float32, decode func(uint8) float32) {
	thickness := sceneGap(inv, x, y, z, depth[int(y)*w+int(x)], w, h)
	strength := m.Refraction * float32(h) * min(1, thickness)
	sx, sy := x+n.Dot(right)*strength, y-n.Dot(up)*strength
	if sx < .5 || sy < .5 || sx > float32(w)-.5 || sy > float32(h)-.5 {
		sx, sy = x, y
	}
	ix, iy := int(math.Floor(float64(sx-.5))), int(math.Floor(float64(sy-.5)))
	blocked := false
	for j := range 2 {
		for i := range 2 {
			if depth[min(h-1, max(0, iy+j))*w+min(w-1, max(0, ix+i))] <= z {
				blocked = true
			}
		}
	}
	if blocked {
		sx, sy = x, y
	}
	ix, iy = int(math.Floor(float64(sx-.5))), int(math.Floor(float64(sy-.5)))
	fx, fy := sx-.5-float32(ix), sy-.5-float32(iy)
	var behind [3]float32
	for j := range 2 {
		for i := range 2 {
			weight := float32(1)
			if i == 0 {
				weight *= 1 - fx
			} else {
				weight *= fx
			}
			if j == 0 {
				weight *= 1 - fy
			} else {
				weight *= fy
			}
			off := 4 * (min(h-1, max(0, iy+j))*w + min(w-1, max(0, ix+i)))
			for k := range behind {
				behind[k] += decode(pixels[off+k]) * weight
			}
		}
	}
	thickness = sceneGap(inv, sx, sy, z, depth[int(sy)*w+int(sx)], w, h)
	absorption := [3]float32{m.Absorption.X, m.Absorption.Y, m.Absorption.Z}
	for k := range rgb {
		weight := m.Transmission * float32(math.Exp(-float64(absorption[k]*thickness)))
		rgb[k] += (behind[k] - rgb[k]) * weight
	}
}
