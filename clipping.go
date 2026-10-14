// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image/color"
	"nimbleterminal.dev/nt3d/math"
)

func contains(b nt3dmath.AABB, p nt3dmath.Vec3) bool {
	return b.IsValid() && p.X >= b.Min.X && p.X <= b.Max.X && p.Y >= b.Min.Y && p.Y <= b.Max.Y && p.Z >= b.Min.Z && p.Z <= b.Max.Z
}
func clipBox(box Box, b nt3dmath.AABB) (Box, bool) {
	a, z, lo, hi := components(box.Min), components(box.Min.Add(box.Size)), components(b.Min), components(b.Max)
	for i := range 3 {
		a[i], z[i] = max(a[i], lo[i]), min(z[i], hi[i])
		if z[i] < a[i] {
			return Box{}, false
		}
	}
	box.Min, box.Size = vector(a), vector(z).Sub(vector(a))
	return box, true
}
func interpolateVertex(a, b Vertex, t float64) Vertex {
	mix := func(x, y float32) float32 { return float32(float64(x)*(1-t) + float64(y)*t) }
	vec := func(x, y nt3dmath.Vec3) nt3dmath.Vec3 {
		return nt3dmath.Vec3{X: mix(x.X, y.X), Y: mix(x.Y, y.Y), Z: mix(x.Z, y.Z)}
	}
	channel := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t + .5) }
	return Vertex{Position: vec(a.Position, b.Position), Normal: vec(a.Normal, b.Normal), Color: color.RGBA{R: channel(a.Color.R, b.Color.R), G: channel(a.Color.G, b.Color.G), B: channel(a.Color.B, b.Color.B), A: channel(a.Color.A, b.Color.A)}}
}
func clipLine(a, b Vertex, bounds nt3dmath.AABB) (Vertex, Vertex, bool) {
	lo, hi := components(bounds.Min), components(bounds.Max)
	for axis := range 3 {
		for side := range 2 {
			bound := lo[axis]
			if side == 1 {
				bound = hi[axis]
			}
			x, y := components(a.Position)[axis], components(b.Position)[axis]
			outside := func(v float32) bool {
				if side == 0 {
					return v < bound
				}
				return v > bound
			}
			oa, ob := outside(x), outside(y)
			if oa && ob {
				return Vertex{}, Vertex{}, false
			}
			if oa != ob {
				v := interpolateVertex(a, b, (float64(bound)-float64(x))/(float64(y)-float64(x)))
				p := components(v.Position)
				p[axis] = bound
				v.Position = vector(p)
				if oa {
					a = v
				} else {
					b = v
				}
			}
		}
	}
	return a, b, true
}

type meshVertex struct {
	Vertex
	UV
}

func clipTriangleUV(a, b, c meshVertex, bounds nt3dmath.AABB) []meshVertex {
	polygon := []meshVertex{a, b, c}
	lo, hi := components(bounds.Min), components(bounds.Max)
	for axis := range 3 {
		for side := range 2 {
			if len(polygon) == 0 {
				return nil
			}
			bound := lo[axis]
			if side == 1 {
				bound = hi[axis]
			}
			inside := func(v meshVertex) bool {
				x := components(v.Position)[axis]
				if side == 0 {
					return x >= bound
				}
				return x <= bound
			}
			out := make([]meshVertex, 0, len(polygon)+1)
			previous := polygon[len(polygon)-1]
			for _, current := range polygon {
				if inside(previous) != inside(current) {
					x, y := components(previous.Position)[axis], components(current.Position)[axis]
					t := (float64(bound) - float64(x)) / (float64(y) - float64(x))
					v := meshVertex{Vertex: interpolateVertex(previous.Vertex, current.Vertex, t), UV: UV{float32(float64(previous.U)*(1-t) + float64(current.U)*t), float32(float64(previous.V)*(1-t) + float64(current.V)*t)}}
					p := components(v.Position)
					p[axis] = bound
					v.Position = vector(p)
					out = append(out, v)
				}
				if inside(current) {
					out = append(out, current)
				}
				previous = current
			}
			polygon = out
		}
	}
	return polygon
}
func clipGeometry(g Geometry, b nt3dmath.AABB) Geometry {
	if g.Bounds.IsValid() && contains(b, g.Bounds.Min) && contains(b, g.Bounds.Max) {
		return g
	}
	out := g
	out.WireGrid = [2]int{}
	out.UV = nil
	out.Arrows = nil
	out.Points, out.Vertices, out.Indices, out.Boxes, out.Lines = nil, nil, nil, nil, nil
	for _, p := range g.Points {
		if contains(b, p.Position) {
			out.Points = append(out.Points, p)
		}
	}
	for i := 0; i+2 < len(g.Indices); i += 3 {
		at := func(index uint32) meshVertex {
			v := meshVertex{Vertex: g.Vertices[index]}
			if len(g.UV) > 0 {
				v.UV = g.UV[index]
			}
			return v
		}
		polygon := clipTriangleUV(at(g.Indices[i]), at(g.Indices[i+1]), at(g.Indices[i+2]), b)
		base := uint32(len(out.Vertices))
		for _, v := range polygon {
			out.Vertices = append(out.Vertices, v.Vertex)
			if len(g.UV) > 0 {
				out.UV = append(out.UV, v.UV)
			}
		}
		for j := 1; j+1 < len(polygon); j++ {
			out.Indices = append(out.Indices, base, base+uint32(j), base+uint32(j+1))
		}
	}
	for _, arrow := range g.Arrows {
		if a, ok := clipArrow(arrow, b); ok {
			out.Arrows = append(out.Arrows, a)
		}
	}
	for _, box := range g.Boxes {
		if clipped, ok := clipBox(box, b); ok {
			out.Boxes = append(out.Boxes, clipped)
		}
	}
	for i := 0; i+1 < len(g.Lines); i += 2 {
		if a, z, ok := clipLine(g.Lines[i], g.Lines[i+1], b); ok {
			out.Lines = append(out.Lines, a, z)
		}
	}
	return out
}

func clipArrow(a Arrow, b nt3dmath.AABB) (Arrow, bool) {
	start, end, ok := clipLine(Vertex{Position: a.Start}, Vertex{Position: a.End}, b)
	if !ok {
		return Arrow{}, false
	}
	if !contains(b, a.End) {
		a.HeadSize = 0
	} // A clipped boundary is not a vector tip.
	a.Start, a.End = start.Position, end.Position
	return a, true
}
