// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/sh"
)

// A cloud over the capacity splits into slabs along its longest axis, SH
// kept with its points, each piece valid and sharing the object's fields.
func TestPointPieces(t *testing.T) {
	defer func(g func(int) int, p int) { maxGaussians, piecePoints = g, p }(maxGaussians, piecePoints)
	maxGaussians = func(int) int { return 4 }
	g := nt3d.Geometry{PointStyle: nt3d.PointStyle{Shape: nt3d.GaussianPoint, Units: nt3d.WorldUnits, Blend: nt3d.AlphaBlend}}
	for i := range 10 {
		x := float32((i * 7) % 10) // Shuffled along X, the longest axis.
		g.Points = append(g.Points, nt3d.Point{Position: nt3dmath.Vec3{X: x, Y: float32(i % 2)}, Radius: 1, Color: color.RGBA{R: uint8(x)}, GaussianAxes: [3]nt3dmath.Vec3{{X: 1}, {Y: 1}, {Z: 1}}})
		g.GaussianSH = append(g.GaussianSH, nt3dsh.RGB{x, 0, 0})
	}
	pieces := PointPieces(Object{Name: "cloud", Material: "m", Geometry: g})
	if len(pieces) != 3 || len(pieces[0].Geometry.Points) != 4 || len(pieces[2].Geometry.Points) != 2 {
		t.Fatalf("%d pieces", len(pieces))
	}
	last := float32(-1)
	for _, p := range pieces {
		if p.Name != "cloud" || p.Material != "m" || !p.Geometry.Bounds.IsValid() || p.Geometry.Validate() != nil {
			t.Fatal("piece lost its fields or validity")
		}
		for i, pt := range p.Geometry.Points {
			if pt.Position.X < last || p.Geometry.GaussianSH[i][0] != pt.Position.X || float32(pt.Color.R) != pt.Position.X {
				t.Fatalf("points out of order or SH/color detached at x=%v", pt.Position.X)
			}
			last = pt.Position.X
		}
	}
	piecePoints = 3
	plain := Object{Geometry: nt3d.Geometry{Points: g.Points}}
	if n := len(PointPieces(plain)); n != 4 {
		t.Fatalf("plain cloud: %d pieces", n)
	}
	small := Object{Geometry: nt3d.Geometry{Points: g.Points[:3]}}
	if pieces := PointPieces(small); len(pieces) != 1 || &pieces[0].Geometry.Points[0] != &small.Geometry.Points[0] {
		t.Fatalf("small cloud: %d pieces, or copied", len(pieces))
	}
}

// Gaussian PLY and point-cloud PLY over the capacity decode as pieces
// instead of failing.
func TestGaussianPLYSplits(t *testing.T) {
	defer func(g func(int) int, p int) { maxGaussians, piecePoints = g, p }(maxGaussians, piecePoints)
	maxGaussians = func(int) int { return 2 }
	var b strings.Builder
	b.WriteString("ply\nformat ascii 1.0\nelement vertex 5\n")
	for _, p := range gaussianProperties {
		fmt.Fprintf(&b, "property float %s\n", p)
	}
	b.WriteString("end_header\n")
	for i := range 5 {
		fmt.Fprintf(&b, "%d 0 0 0 0 0 0 0 0 0 1 0 0 0\n", i)
	}
	m, err := DecodePLY([]byte(b.String()), nil)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, o := range m.Objects {
		total += len(o.Geometry.Points)
	}
	if len(m.Objects) != 3 || total != 5 || m.Format != PLY {
		t.Fatalf("%d objects, %d points, format %q", len(m.Objects), total, m.Format)
	}
	piecePoints = 2
	cloud := "ply\nformat ascii 1.0\nelement vertex 5\nproperty float x\nproperty float y\nproperty float z\nend_header\n0 0 0\n1 0 0\n2 0 0\n3 0 0\n4 0 0\n"
	if m, err = DecodePLY([]byte(cloud), nil); err != nil || len(m.Objects) != 3 {
		t.Fatalf("point cloud: %v, %d objects", err, len(m.Objects))
	}
}
