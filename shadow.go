// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"math"
	"nimbleterminal.dev/nt3d/math"
)

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

// shadowMap stores nearest caster depth, in row-major order with y down.
type shadowMap struct {
	shadowView
	depth []float32
}

// softwareShadowSize is the map's texels a side for a software frame.
func softwareShadowSize(f *Frame) int {
	if f.Complete {
		return shadowMapSize
	}
	return min(1024, 4*max(f.Width, f.Height))
}

// buildShadowMap rasterizes drawn opaque meshes and boxes from the light.
func buildShadowMap(f *Frame, drawn []bool, size int) *shadowMap {
	view, ok := fitShadow(f, drawn, size)
	if !ok {
		return nil
	}
	m := &shadowMap{shadowView: view, depth: make([]float32, size*size)}
	for i := range m.depth {
		m.depth[i] = 1
	}
	for gi, g := range f.Geometry {
		if !drawn[gi] {
			continue
		}
		world := instance(*f, gi).World
		tri := func(vertices []Vertex, indices [3]uint32, uv []UV, material *Material) {
			var x, y, z [3]float32
			for k, i := range indices {
				x[k], y[k], z[k] = m.mapCoords(world.Point(vertices[i].Position))
			}
			area := edge(x[0], y[0], x[1], y[1], x[2], y[2])
			if area >= -.000001 {
				return
			}
			owns := func(a, b int) bool { dx, dy := x[a]-x[b], y[a]-y[b]; return dy < 0 || dy == 0 && dx > 0 }
			own0, own1, own2 := owns(1, 2), owns(2, 0), owns(0, 1)
			loX, hiX := max(0, int(math.Floor(float64(min(x[0], x[1], x[2]))))), min(size-1, int(math.Ceil(float64(max(x[0], x[1], x[2])))))
			loY, hiY := max(0, int(math.Floor(float64(min(y[0], y[1], y[2]))))), min(size-1, int(math.Ceil(float64(max(y[0], y[1], y[2])))))
			for py := loY; py <= hiY; py++ {
				for px := loX; px <= hiX; px++ {
					cx, cy := float32(px)+.5, float32(py)+.5
					e0, e1, e2 := -edge(x[1], y[1], x[2], y[2], cx, cy), -edge(x[2], y[2], x[0], y[0], cx, cy), -edge(x[0], y[0], x[1], y[1], cx, cy)
					if e0 < 0 || e1 < 0 || e2 < 0 || e0 == 0 && !own0 || e1 == 0 && !own1 || e2 == 0 && !own2 {
						continue
					}
					a, b := -e0/area, -e1/area
					c := 1 - a - b
					d := a*z[0] + b*z[1] + c*z[2]
					if d < 0 || d > 1 {
						continue
					}
					if material != nil && material.Cutout && material.Texture != nil {
						u, v, w := uv[indices[0]], uv[indices[1]], uv[indices[2]]
						if material.Texture.sampleLinear(UV{a*u.U + b*v.U + c*w.U, a*u.V + b*v.V + c*w.V}, material.Repeat, 0)[3] < material.threshold() {
							continue
						}
					}
					i := py*size + px
					m.depth[i] = min(m.depth[i], d)
				}
			}
		}
		if !transparent(&g) {
			for i := 0; i+2 < len(g.Indices); i += 3 {
				tri(g.Vertices, [3]uint32{g.Indices[i], g.Indices[i+1], g.Indices[i+2]}, g.UV, g.Material)
			}
		}
		for _, box := range g.Boxes {
			v := boxVertices(box)
			for i := 0; i < len(v); i += 3 {
				tri(v, [3]uint32{uint32(i), uint32(i + 1), uint32(i + 2)}, nil, nil)
			}
		}
	}
	return m
}

// visibility applies normal offset, slope bias, and a clamped 3x3 PCF kernel.
func (m *shadowMap) visibility(p, n, l nt3dmath.Vec3) float32 {
	x, y, z := m.mapCoords(p.Add(n.Scale(1.5 * m.texel)))
	if x < 0 || y < 0 || x >= float32(m.size) || y >= float32(m.size) || z < 0 || z > 1 {
		return 1
	}
	cx, cy := int(math.Floor(float64(x))), int(math.Floor(float64(y)))
	bias := (1 + 2*(1-max(float32(0), n.Dot(l)))) / float32(m.size)
	var lit float32
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			sx, sy := max(0, min(m.size-1, cx+dx)), max(0, min(m.size-1, cy+dy))
			if m.depth[sy*m.size+sx] >= z-bias {
				lit++
			}
		}
	}
	return lit / 9
}

// dropDisc derives a geometry instance's drop shadow from its world bounds.
func dropDisc(g *Geometry, world nt3dmath.Mat4) (center nt3dmath.Vec3, radius, strength float32, ok bool) {
	if g.DropShadow.Strength <= 0 || !g.Bounds.IsValid() {
		return
	}
	b := g.Bounds.Transformed(world)
	center = nt3dmath.Vec3{X: (b.Min.X + b.Max.X) * .5, Y: (b.Min.Y + b.Max.Y) * .5, Z: b.Min.Z}
	radius = g.DropShadow.Radius
	if radius == 0 {
		radius = max(b.Max.X-b.Min.X, b.Max.Y-b.Min.Y) * .5
	}
	strength = g.DropShadow.Strength
	ok = radius > 0
	return
}
