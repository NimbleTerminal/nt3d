// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
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
	defer func(b int) { bindingLimit = b }(bindingLimit)
	for _, precise := range []bool{true, false} {
		record := 24 // Compact axis records.
		if precise {
			record = 48
		}
		bindingLimit = record * 10 // Ten axis records.
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
	bindingLimit = 96
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	bindingLimit = 95
	if err := g.Validate(); err == nil {
		t.Fatal("compact SH over the binding accepted")
	}
	// Precise: 64 + 12 a coefficient.
	g.PointStyle.Precise = true
	bindingLimit = 64 + 12*32
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	bindingLimit--
	if err := g.Validate(); err == nil {
		t.Fatal("precise SH over the binding accepted")
	}

	// The same two splats with a palette of two degree-3 centroids and labels.
	g.PointStyle.Precise = false
	g.GaussianSH = make([]nt3dsh.RGB, 2)
	g.GaussianSHLabels = []uint16{0, 1}
	g.GaussianSHPalette = &SHPalette{Degree: 3, Coefficients: make([]nt3dsh.RGB, 30)}
	// Compact: the DCs (6 bytes, padded to 8), the labels (4) and 30 bands (90).
	bindingLimit = 8 + 4 + 90
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	bindingLimit--
	if err := g.Validate(); err == nil {
		t.Fatal("compact palette over the binding accepted")
	}
	// Precise: 64 + 12 a coefficient, DC and bands alike.
	g.PointStyle.Precise = true
	bindingLimit = 64 + 12*32
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	bindingLimit--
	if err := g.Validate(); err == nil {
		t.Fatal("precise palette over the binding accepted")
	}
}
