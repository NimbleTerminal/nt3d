// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image/color"
	"math"

	"nimbleterminal.dev/nt3d/math"
)

type strokePoint struct{ x, y, z, w float32 }
type screenStroke struct {
	a, b           strokePoint
	t0, t1         float32 // Parameters on the original world segment after depth clipping.
	dx, dy, length float32
}

// Clip depth before the perspective divide. Lateral viewport clipping is left
// to rasterization so strokes just outside the viewport can still cover pixels.
// The GPU uses the same three planes and interpolation in clipStroke.
func projectStroke(f Frame, a, b nt3dmath.Vec3) (s screenStroke, ok bool) {
	ax, ay, az, aw := f.Matrix.Transform(a)
	bx, by, bz, bw := f.Matrix.Transform(b)
	ca, cb := [4]float32{ax, ay, az, aw}, [4]float32{bx, by, bz, bw}
	if !validFloat(ca[:]...) || !validFloat(cb[:]...) {
		return s, false
	}
	t0, t1 := float32(0), float32(1)
	for plane := range 3 {
		distance := func(v [4]float32) float32 {
			switch plane {
			case 0:
				return v[2]
			case 1:
				return v[3] - v[2]
			default:
				return v[3] - 1e-5
			}
		}
		da, db := distance(ca), distance(cb)
		if da < 0 && db < 0 {
			return s, false
		}
		if (da < 0) != (db < 0) {
			t := da / (da - db)
			var v [4]float32
			for i := range 4 {
				v[i] = ca[i]*(1-t) + cb[i]*t
			}
			param := t0*(1-t) + t1*t
			if da < 0 {
				ca = v
				t0 = param
			} else {
				cb = v
				t1 = param
			}
		}
	}
	project := func(v [4]float32) strokePoint {
		return strokePoint{(v[0]/v[3] + 1) * float32(f.Width) * .5, (1 - v[1]/v[3]) * float32(f.Height) * .5, v[2] / v[3], v[3]}
	}
	s = screenStroke{a: project(ca), b: project(cb), t0: t0, t1: t1}
	dx, dy := s.b.x-s.a.x, s.b.y-s.a.y
	s.length = float32(math.Hypot(float64(dx), float64(dy)))
	s.dx, s.dy = 1, 0
	if s.length > 1e-5 {
		s.dx, s.dy = dx/s.length, dy/s.length
	}
	return s, true
}
func (s screenStroke) head(size, width float32) (length, halfWidth float32) {
	if s.t1 < 1 || size == 0 || s.length < 1e-5 {
		return 0, 0
	}
	length = min(size, s.length*.45)
	return length, max(width*.75, length*.5)
}

// worldT converts projected length fraction into a perspective-correct position
// on the original segment, including the depth clipping interval.
func (s screenStroke) worldT(t float32) float32 {
	q := (t / s.b.w) / ((1-t)/s.a.w + t/s.b.w)
	return s.t0*(1-q) + s.t1*q
}

func rasterStroke(f Frame, a, b Vertex, width, headSize, bias float32, put func(int, int, float32, color.RGBA)) {
	s, ok := projectStroke(f, a.Position, b.Position)
	if !ok {
		return
	}
	width = lineWidth(width)
	hl, hw := s.head(headSize, width)
	pad := max(width*.5, hw)
	lowX := max(0, int(math.Floor(float64(min(s.a.x, s.b.x)-pad))))
	highX := min(f.Width-1, int(math.Ceil(float64(max(s.a.x, s.b.x)+pad))))
	lowY := max(0, int(math.Floor(float64(min(s.a.y, s.b.y)-pad))))
	highY := min(f.Height-1, int(math.Ceil(float64(max(s.a.y, s.b.y)+pad))))
	for y := lowY; y <= highY; y++ {
		for x := lowX; x <= highX; x++ {
			rx, ry := float32(x)+.5-s.a.x, float32(y)+.5-s.a.y
			along, side := rx*s.dx+ry*s.dy, abs(-rx*s.dy+ry*s.dx)
			t := float32(0)
			if s.length < 1e-5 {
				if abs(rx) > width*.5 || abs(ry) > width*.5 {
					continue
				}
			} else {
				if along < 0 || along > s.length {
					continue
				}
				limit := width * .5
				if hl > 0 && along > s.length-hl {
					limit = hw * (s.length - along) / hl
				}
				if side > limit {
					continue
				}
				t = along / s.length
			}
			c := lerpRGBA(a.Color, b.Color, float64(s.worldT(t)))
			put(x, y, s.a.z*(1-t)+s.b.z*t+bias, c)
		}
	}
}
