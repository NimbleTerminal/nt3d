// Copyright (c) 2026 Neomantra Corp.

package nt3d

import "math"

type barycentric struct{ a, b, c float32 }

func (w barycentric) mix(a, b, c float32) float32 { return w.a*a + w.b*b + w.c*c }

func (w barycentric) perspective(aw, bw, cw float32) barycentric {
	a, b, c := w.a/aw, w.b/bw, w.c/cw
	sum := a + b + c
	return barycentric{a / sum, b / sum, c / sum}
}

func (w barycentric) uv(a, b, c UV) UV {
	// Keep the same product rounded on each axis. A different fused
	// multiply-add can shift a tiled texture across a texel boundary.
	return UV{
		w.a*a.U + float32(w.b*b.U) + w.c*c.U,
		w.a*a.V + float32(w.b*b.V) + w.c*c.V,
	}
}

// screenTriangle is a clipped triangle projected onto the target. Both
// mesh faces and drop-shadow discs use these bounds and edge ownership.
type screenTriangle struct {
	x0, y0, z0, x1, y1, z1, x2, y2, z2 float32
	area, sign                         float32
	own0, own1, own2                   bool
	loX, hiX, loY, hiY                 int
}

func projectTriangle(a, b, c *clipCorner, width, height int) screenTriangle {
	project := func(v *clipCorner) (float32, float32, float32) {
		return (v.x/v.w + 1) * float32(width) * .5, (1 - v.y/v.w) * float32(height) * .5, v.z / v.w
	}
	var s screenTriangle
	s.x0, s.y0, s.z0 = project(a)
	s.x1, s.y1, s.z1 = project(b)
	s.x2, s.y2, s.z2 = project(c)
	s.area = (s.x1-s.x0)*(s.y2-s.y0) - (s.y1-s.y0)*(s.x2-s.x0)
	s.sign = 1
	if s.area < 0 {
		s.sign = -1
	}
	// Orient edges clockwise in screen coordinates. Top edges run right,
	// and left edges run up. Only those edges own their pixel centers.
	owns := func(xa, ya, xb, yb float32) bool {
		dx, dy := (xb-xa)*s.sign, (yb-ya)*s.sign
		return dy < 0 || dy == 0 && dx > 0
	}
	s.own0, s.own1, s.own2 = owns(s.x1, s.y1, s.x2, s.y2), owns(s.x2, s.y2, s.x0, s.y0), owns(s.x0, s.y0, s.x1, s.y1)
	s.loX, s.hiX = max(0, int(math.Floor(float64(min(s.x0, s.x1, s.x2))))), min(width-1, int(math.Ceil(float64(max(s.x0, s.x1, s.x2)))))
	s.loY, s.hiY = max(0, int(math.Floor(float64(min(s.y0, s.y1, s.y2))))), min(height-1, int(math.Ceil(float64(max(s.y0, s.y1, s.y2)))))
	return s
}

func (s *screenTriangle) coverage(px, py float32) (barycentric, bool) {
	e0 := edge(s.x1, s.y1, s.x2, s.y2, px, py) * s.sign
	e1 := edge(s.x2, s.y2, s.x0, s.y0, px, py) * s.sign
	e2 := edge(s.x0, s.y0, s.x1, s.y1, px, py) * s.sign
	if e0 < 0 || e1 < 0 || e2 < 0 || e0 == 0 && !s.own0 || e1 == 0 && !s.own1 || e2 == 0 && !s.own2 {
		return barycentric{}, false
	}
	u, v := e0*s.sign/s.area, e1*s.sign/s.area
	return barycentric{u, v, 1 - u - v}, true
}

// Interpolate depth in screen space, with the same rounding for all passes.
func (s *screenTriangle) depth(w barycentric) float32 {
	return w.a*s.z0 + float32(w.b*s.z1) + w.c*s.z2
}

// edge is twice the signed area of the triangle a, b, p: zero on the line
// through a and b. Each product is rounded before subtracting, so the
// compiler cannot fuse them: the edge from b to a is then exactly the
// negative, and triangles sharing it agree which side a pixel is on.
func edge(xa, ya, xb, yb, px, py float32) float32 {
	return float32((xa-px)*(yb-py)) - float32((ya-py)*(xb-px))
}
