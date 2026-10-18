// Copyright (c) 2026 Neomantra Corp.

package nt3d

import "nimbleterminal.dev/nt3d/math"

const shadowMapSize = 2048

// shadowView is a shadow map's view of the world: matrix takes world points
// to x, y in -1..1 across the map and z in 0..1 through its depth; texel is
// a texel's width in world units; size is texels a side; light indexes
// Lighting.Lights.
type shadowView struct {
	matrix nt3dmath.Mat4
	texel  float32
	size   int
	light  int
}

// shadowCaster is the index of the first directional light with Shadows, or -1.
func shadowCaster(l Lighting) int {
	for i, light := range l.Lights {
		if light.Kind == DirectionalLight && light.Shadows {
			return i
		}
	}
	return -1
}

// fitShadow fits the map of size to the world bounds of drawn geometry with
// valid Bounds, seen along the light; ok is false when nothing has bounds or
// no light casts.
func fitShadow(f *Frame, drawn []bool, size int) (v shadowView, ok bool) {
	light := shadowCaster(f.Lighting)
	if light < 0 || size <= 0 {
		return v, false
	}
	forward := f.Lighting.Lights[light].Direction.Normalize().Scale(-1)
	up := nt3dmath.Vec3{Z: 1}
	if forward.Z > .99 || forward.Z < -.99 {
		up = nt3dmath.Vec3{Y: 1}
	}
	right := forward.Cross(up).Normalize()
	up = right.Cross(forward)
	var bounds nt3dmath.AABB
	for i, g := range f.Geometry {
		if !drawn[i] || !g.Bounds.IsValid() {
			continue
		}
		b := g.Bounds.Transformed(instance(*f, i).World)
		for _, x := range []float32{b.Min.X, b.Max.X} {
			for _, y := range []float32{b.Min.Y, b.Max.Y} {
				for _, z := range []float32{b.Min.Z, b.Max.Z} {
					p := nt3dmath.Vec3{X: x, Y: y, Z: z}
					bounds.Include(nt3dmath.Vec3{X: p.Dot(right), Y: p.Dot(up), Z: p.Dot(forward)})
				}
			}
		}
	}
	if !bounds.IsValid() {
		return v, false
	}
	lo, hi := bounds.Min, bounds.Max
	extent := max(hi.X-lo.X, hi.Y-lo.Y, float32(1e-3))
	w := extent + 2*extent/float32(size)
	x0, y0 := (lo.X+hi.X-w)*.5, (lo.Y+hi.Y-w)*.5
	depth := max(hi.Z-lo.Z, float32(1e-3))
	z0, d := lo.Z-depth*.01, depth*1.02
	v = shadowView{texel: w / float32(size), size: size, light: light}
	v.matrix = nt3dmath.Mat4{
		2 * right.X / w, 2 * up.X / w, forward.X / d, 0,
		2 * right.Y / w, 2 * up.Y / w, forward.Y / d, 0,
		2 * right.Z / w, 2 * up.Z / w, forward.Z / d, 0,
		-2*x0/w - 1, -2*y0/w - 1, -z0 / d, 1,
	}
	return v, true
}

// mapCoords takes a world point to map texel coordinates (x right, y down,
// 0..size) and normalized depth.
func (v *shadowView) mapCoords(p nt3dmath.Vec3) (x, y, z float32) {
	x, y, z, _ = v.matrix.Transform(p)
	return (x*.5 + .5) * float32(v.size), (.5 - y*.5) * float32(v.size), z
}
