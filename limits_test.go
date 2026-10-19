// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"testing"

	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/sh"
)

// The point limits come from the 128 MiB storage binding WebGPU
// guarantees, over each per-point record.
func TestMaxPointsFromBinding(t *testing.T) {
	if MaxPoints != 6_710_886 || MaxGaussians(0) != 2_796_202 || MaxGaussians(16) != 699_050 || MaxGaussians(1) != 2_796_202 || MaxGaussians(5) != 0 {
		t.Fatalf("MaxPoints %d, MaxGaussians %d %d %d %d", MaxPoints, MaxGaussians(0), MaxGaussians(16), MaxGaussians(1), MaxGaussians(5))
	}
}

// Validate measures a geometry's buffers against the binding: a cloud
// whose axis records or SH would not fit is refused.
func TestValidateBuffers(t *testing.T) {
	defer func(b int) { bindingLimit = b }(bindingLimit)
	bindingLimit = 48 * 10 // Ten axis records.
	g := Geometry{PointStyle: PointStyle{Shape: GaussianPoint, Units: WorldUnits, Blend: AlphaBlend}}
	for range 10 {
		g.Points = append(g.Points, Point{Radius: 1, GaussianAxes: [3]nt3dmath.Vec3{{X: 1}, {Y: 1}, {Z: 1}}})
	}
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	g.Points = append(g.Points, g.Points[0])
	if err := g.Validate(); err == nil {
		t.Fatal("eleven axis records fit a ten-record binding")
	}
	// Two splats with 15 coefficients each: 48 + 360 bytes of SH.
	g.Points = g.Points[:2]
	g.GaussianSH = make([]nt3dsh.RGB, 32) // 16 per point, degree 3.
	bindingLimit = 48 + 12*32
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	bindingLimit--
	if err := g.Validate(); err == nil {
		t.Fatal("SH over the binding accepted")
	}
}
