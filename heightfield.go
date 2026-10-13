// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"image/color"
	"nimbleterminal.dev/nt3d/math"
	"slices"
)

// Heightfield describes a regular XY mesh. Heights are row-major, with Y
// increasing by row. Sampler, when present, replaces Heights. No data colors
// or range policy are applied. Zero Color defaults to opaque white.
type Heightfield struct {
	NX, NY                 int
	MinX, MaxX, MinY, MaxY float32
	Heights                []float32
	Sampler                func(x, y float32) float32
	UV                     []UV
	Material               *Material
	Color                  color.RGBA
}

func BuildHeightfield(d Heightfield) (Geometry, error) {
	if d.NX < 2 || d.NY < 2 || d.NX > 512 || d.NY > 512 {
		return Geometry{}, fmt.Errorf("nt3d: heightfield dimensions must be 2..512")
	}
	if !validFloat(d.MinX, d.MaxX, d.MinY, d.MaxY) || d.MinX >= d.MaxX || d.MinY >= d.MaxY {
		return Geometry{}, errBounds
	}
	n := d.NX * d.NY
	if d.Sampler == nil && len(d.Heights) != n {
		return Geometry{}, fmt.Errorf("nt3d: height count mismatch")
	}
	if len(d.UV) != 0 && len(d.UV) != n {
		return Geometry{}, fmt.Errorf("nt3d: UV count mismatch")
	}
	if d.Color == (color.RGBA{}) {
		d.Color = color.RGBA{255, 255, 255, 255}
	}
	g := Geometry{WireGrid: [2]int{d.NX, d.NY}, Vertices: make([]Vertex, n), UV: slices.Clone(d.UV), Material: cloneMaterial(d.Material)}
	if len(g.UV) == 0 {
		g.UV = make([]UV, n)
		for y := range d.NY {
			for x := range d.NX {
				g.UV[y*d.NX+x] = UV{float32(x) / float32(d.NX-1), 1 - float32(y)/float32(d.NY-1)}
			}
		}
	}
	for y := range d.NY {
		for x := range d.NX {
			i := y*d.NX + x
			px := float32(float64(d.MinX) + (float64(d.MaxX)-float64(d.MinX))*float64(x)/float64(d.NX-1))
			py := float32(float64(d.MinY) + (float64(d.MaxY)-float64(d.MinY))*float64(y)/float64(d.NY-1))
			var z float32
			if d.Sampler != nil {
				z = d.Sampler(px, py)
			} else {
				z = d.Heights[i]
			}
			if !validFloat(z) {
				return Geometry{}, fmt.Errorf("nt3d: non-finite height at %d", i)
			}
			g.Vertices[i] = Vertex{Position: nt3dmath.Vec3{X: px, Y: py, Z: z}, Color: d.Color}
		}
	}
	for y := 0; y < d.NY-1; y++ {
		for x := 0; x < d.NX-1; x++ {
			a := uint32(y*d.NX + x)
			b, c := a+1, a+uint32(d.NX)
			g.Indices = append(g.Indices, a, b, c, b, c+1, c)
		}
	}
	for i := 0; i < len(g.Indices); i += 3 {
		a, b, c := g.Indices[i], g.Indices[i+1], g.Indices[i+2]
		normal := g.Vertices[b].Position.Sub(g.Vertices[a].Position).Cross(g.Vertices[c].Position.Sub(g.Vertices[a].Position))
		for _, j := range []uint32{a, b, c} {
			g.Vertices[j].Normal = g.Vertices[j].Normal.Add(normal)
		}
	}
	for i := range g.Vertices {
		g.Vertices[i].Normal = g.Vertices[i].Normal.Normalize()
	}
	g.Bounds = GeometryBounds(g)
	return g, g.Validate()
}
