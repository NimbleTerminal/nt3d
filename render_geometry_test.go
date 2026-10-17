// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image/color"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// Encoding geometry for the GPU allocates per kind, not per record, so
// points that change every frame stay cheap to send.
func TestGeometryDataAllocations(t *testing.T) {
	g := Geometry{Points: make([]Point, 1000), Lines: make([]Vertex, 1000), Boxes: make([]Box, 100)}
	for i := range g.Points {
		g.Points[i] = Point{Position: nt3dmath.Vec3{X: float32(i)}, Radius: 2, Color: color.RGBA{255, 0, 0, 255}}
	}
	if n := testing.AllocsPerRun(10, func() { geometryData(g) }); n > 8 {
		t.Fatalf("geometryData allocated %v times for 2,100 records", n)
	}
}

// BenchmarkGeometryData encodes 100,000 points, as a particle system
// changing every frame sends them.
func BenchmarkGeometryData(b *testing.B) {
	g := Geometry{Points: make([]Point, 100000)}
	for i := range g.Points {
		g.Points[i] = Point{Position: nt3dmath.Vec3{X: float32(i)}, Radius: 2, Color: color.RGBA{255, 160, 40, 255}}
	}
	b.ReportAllocs()
	for b.Loop() {
		geometryData(g)
	}
}
