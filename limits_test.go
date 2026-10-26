// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/sh"
)

// The point limits come from the 128 MiB storage binding WebGPU
// guarantees, over each per-point record. Compact splats (the default)
// hold 24-byte axes and three SH bytes a coefficient; Precise ones 48 and 12.
func TestMaxPointsFromBinding(t *testing.T) {
	if MaxPoints != 6_710_886 {
		t.Fatalf("MaxPoints %d", MaxPoints)
	}
	for _, c := range []struct{ coefficients, compact, precise int }{
		{0, 5_592_405, 2_796_202},
		{1, 5_592_405, 2_796_202},
		{4, 5_592_405, 2_796_201},
		{9, 4_971_026, 1_242_756},
		{16, 2_796_202, 699_050},
		{5, 0, 0},
	} {
		if g := MaxGaussians(c.coefficients); g != c.compact {
			t.Errorf("MaxGaussians(%d) = %d, want %d", c.coefficients, g, c.compact)
		}
		if g := MaxPreciseGaussians(c.coefficients); g != c.precise {
			t.Errorf("MaxPreciseGaussians(%d) = %d, want %d", c.coefficients, g, c.precise)
		}
	}
}

// Validate measures a geometry's buffers against the binding: a cloud
// whose axis records or SH would not fit is refused.
func TestValidateBuffers(t *testing.T) {
	defer setBindingLimit(DefaultBindingLimit)
	for _, precise := range []bool{true, false} {
		record := 24 // Compact axis records.
		if precise {
			record = 48
		}
		setBindingLimit(record * 10) // Ten axis records.
		g := Geometry{PointStyle: PointStyle{Shape: GaussianPoint, Units: WorldUnits, Blend: AlphaBlend, Precise: precise}}
		for range 10 {
			g.Points = append(g.Points, Point{Radius: 1, GaussianAxes: [3]nt3dmath.Vec3{{X: 1}, {Y: 1}, {Z: 1}}})
		}
		if err := g.Validate(); err != nil {
			t.Fatalf("precise %v: %v", precise, err)
		}
		g.Points = append(g.Points, g.Points[0])
		if err := g.Validate(); err == nil {
			t.Fatalf("precise %v: eleven axis records fit a ten-record binding", precise)
		}
	}

	// Two splats with 16 coefficients each, degree 3.
	g := Geometry{PointStyle: PointStyle{Shape: GaussianPoint, Units: WorldUnits, Blend: AlphaBlend}}
	for range 2 {
		g.Points = append(g.Points, Point{Radius: 1, GaussianAxes: [3]nt3dmath.Vec3{{X: 1}, {Y: 1}, {Z: 1}}})
	}
	g.GaussianSH = make([]nt3dsh.RGB, 32)
	// Compact: 3 bytes a coefficient, 96 bytes.
	setBindingLimit(96)
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	setBindingLimit(95)
	if err := g.Validate(); err == nil {
		t.Fatal("compact SH over the binding accepted")
	}
	// Precise: 64 + 12 a coefficient.
	g.PointStyle.Precise = true
	setBindingLimit(64 + 12*32)
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	setBindingLimit(BindingLimit() - 1)
	if err := g.Validate(); err == nil {
		t.Fatal("precise SH over the binding accepted")
	}

	// The same two splats with a palette of two degree-3 centroids and labels.
	g.PointStyle.Precise = false
	g.GaussianSH = make([]nt3dsh.RGB, 2)
	g.GaussianSHLabels = []uint16{0, 1}
	g.GaussianSHPalette = &SHPalette{Degree: 3, Coefficients: make([]nt3dsh.RGB, 30)}
	// Compact: the DCs (6 bytes, padded to 8), the labels (4) and 30 bands (90).
	setBindingLimit(8 + 4 + 90)
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	setBindingLimit(BindingLimit() - 1)
	if err := g.Validate(); err == nil {
		t.Fatal("compact palette over the binding accepted")
	}
	// Precise: 64 + 12 a coefficient, DC and bands alike.
	g.PointStyle.Precise = true
	setBindingLimit(64 + 12*32)
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	setBindingLimit(BindingLimit() - 1)
	if err := g.Validate(); err == nil {
		t.Fatal("precise palette over the binding accepted")
	}
}

func TestSetBindingLimitClamps(t *testing.T) {
	defer setBindingLimit(DefaultBindingLimit)
	if DefaultBindingLimit != 128<<20 || MaxBindingLimit != 1<<30 || MaxPointIndex != 1<<24 {
		t.Fatalf("constants %d %d %d", DefaultBindingLimit, MaxBindingLimit, MaxPointIndex)
	}
	for in, want := range map[int]int{0: DefaultBindingLimit, -1: DefaultBindingLimit, MaxBindingLimit + 1: MaxBindingLimit, 256 << 20: 256 << 20} {
		if got := SetBindingLimit(in); got != want || BindingLimit() != want {
			t.Errorf("SetBindingLimit(%d) = %d, BindingLimit() %d, want %d", in, got, BindingLimit(), want)
		}
	}
}

func TestPointLimitCeiling(t *testing.T) {
	defer setBindingLimit(DefaultBindingLimit)
	if PointLimit() != MaxPoints || MeshTriangleLimit() != MaxMeshTriangles || MeshIndexLimit() != MaxMeshIndices {
		t.Fatalf("default limits %d %d %d", PointLimit(), MeshTriangleLimit(), MeshIndexLimit())
	}
	SetBindingLimit(MaxBindingLimit)
	for name, got := range map[string][2]int{
		"PointLimit":              {PointLimit(), MaxPointIndex},
		"MaxGaussians(0)":         {MaxGaussians(0), MaxPointIndex},
		"MaxGaussians(16)":        {MaxGaussians(16), 16_777_216},
		"MaxPreciseGaussians(16)": {MaxPreciseGaussians(16), 5_592_405},
		"MeshTriangleLimit":       {MeshTriangleLimit(), 7_456_540},
	} {
		if got[0] != got[1] {
			t.Errorf("%s at the ceiling = %d, want %d", name, got[0], got[1])
		}
	}
}

// Validate's caps follow the limit: counts and bytes alike.
func TestValidateFollowsLimit(t *testing.T) {
	defer setBindingLimit(DefaultBindingLimit)
	points := make([]Point, 21)
	for i := range points {
		points[i] = Point{Radius: 1}
	}
	g := styled(PointStyle{Shape: HardPoint, Units: WorldUnits}, points...)
	setBindingLimit(400) // Twenty 20-byte records.
	if err := g.Validate(); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("21 points at 400 bytes: %v", err)
	}
	setBindingLimit(420)
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	m := Geometry{Vertices: []Vertex{{}, {Position: nt3dmath.Vec3{X: 1}}, {Position: nt3dmath.Vec3{Y: 1}}}, Indices: []uint32{0, 1, 2}}
	m.Bounds = GeometryBounds(m)
	setBindingLimit(143) // Two 48-byte records: no whole triangle.
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("a triangle at 143 bytes: %v", err)
	}
	setBindingLimit(144)
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
}

// Merging follows the limit: two meshes that fit one geometry at a raised
// limit merge, and the error at a low one names the limit in force.
func TestMergeFollowsLimit(t *testing.T) {
	defer setBindingLimit(DefaultBindingLimit)
	tri := Geometry{Vertices: []Vertex{{}, {Position: nt3dmath.Vec3{X: 1}}, {Position: nt3dmath.Vec3{Y: 1}}}, Indices: []uint32{0, 1, 2}}
	tri.Bounds = GeometryBounds(tri)
	setBindingLimit(144) // One triangle.
	if _, err := MergeGeometry(tri, tri); err == nil || !strings.Contains(err.Error(), "exceeds 1 triangles") {
		t.Fatalf("two triangles at a one-triangle limit: %v", err)
	}
	setBindingLimit(288)
	if _, err := MergeGeometry(tri, tri); err != nil {
		t.Fatal(err)
	}
}

// A scene generates tangents for meshes up to half the limit in force.
func TestGeneratedTangentsFollowLimit(t *testing.T) {
	defer setBindingLimit(DefaultBindingLimit)
	if maxGeneratedIndices() != MaxMeshIndices/2 {
		t.Fatalf("default %d", maxGeneratedIndices())
	}
	SetBindingLimit(MaxBindingLimit)
	if maxGeneratedIndices() != MeshIndexLimit()/2 || maxGeneratedIndices() != 3*7_456_540/2 {
		t.Fatalf("at the ceiling %d", maxGeneratedIndices())
	}
}

// A renderer without a device limit leaves the setting as it is.
func TestAdoptBindingLimitSoftware(t *testing.T) {
	defer setBindingLimit(DefaultBindingLimit)
	r := NewSoftwareRenderer()
	defer r.Close()
	if got, err := AdoptBindingLimit(r); err != nil || got != DefaultBindingLimit || BindingLimit() != DefaultBindingLimit {
		t.Fatalf("adopted %d, %v; limit %d", got, err, BindingLimit())
	}
	SetBindingLimit(256 << 20)
	if got, err := AdoptBindingLimit(r); err != nil || got != 256<<20 || BindingLimit() != 256<<20 {
		t.Fatalf("adopted %d, %v; limit %d", got, err, BindingLimit())
	}
}
