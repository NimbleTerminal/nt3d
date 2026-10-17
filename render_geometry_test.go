// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"encoding/binary"
	"image/color"
	"math"
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

// Points have compact records: position, radius (negative in world units),
// and color as RGBA8, 20 bytes each.
func TestGeometryDataPointRecords(t *testing.T) {
	for _, units := range []PointUnits{PixelUnits, WorldUnits} {
		g := Geometry{PointStyle: PointStyle{Units: units}, Points: []Point{{Position: nt3dmath.Vec3{X: 1, Y: 2, Z: 3}, Radius: 4, Color: color.RGBA{10, 20, 30, 40}}, {Radius: 2}}}
		data := geometryData(g)[0]
		if len(data) != 2*pointRecord || pointRecord != 20 {
			t.Fatalf("%d bytes for two points", len(data))
		}
		word := func(i int) uint32 { return binary.LittleEndian.Uint32(data[4*i:]) }
		radius := float32(4)
		if units == WorldUnits {
			radius = -4
		}
		if math.Float32frombits(word(0)) != 1 || math.Float32frombits(word(2)) != 3 || math.Float32frombits(word(3)) != radius || word(4) != 10|20<<8|30<<16|40<<24 {
			t.Fatalf("units %d: record % x", units, data[:20])
		}
	}
}
