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

// pieces lowers the triangles an Object may hold.
func pieces(t *testing.T, n int) {
	t.Helper()
	real := pieceTriangles
	pieceTriangles = func() int { return n }
	t.Cleanup(func() { pieceTriangles = real })
}

// The piece caps read the binding limit when a cloud is split, not when the
// package loads.
func TestPieceCapsFollowLimit(t *testing.T) {
	defer nt3d.SetBindingLimit(nt3d.DefaultBindingLimit)
	if piecePoints() != nt3d.MaxPoints || pieceTriangles() != nt3d.MaxMeshTriangles || maxGaussians(16) != 2_796_202 {
		t.Fatalf("default caps %d %d %d", piecePoints(), pieceTriangles(), maxGaussians(16))
	}
	nt3d.SetBindingLimit(nt3d.MaxBindingLimit)
	if piecePoints() != nt3d.MaxPointIndex || pieceTriangles() != 7_456_540 || maxGaussians(16) != 16_777_216 {
		t.Fatalf("caps at the ceiling %d %d %d", piecePoints(), pieceTriangles(), maxGaussians(16))
	}
}

// sizes lists each object's triangles, checking it is valid geometry.
func sizes(t *testing.T, m *Model) (out []int) {
	t.Helper()
	for _, o := range m.Objects {
		check(t, o.Geometry.Validate())
		if len(o.Plain) != len(o.Geometry.Vertices) {
			t.Fatalf("%d plain marks for %d vertices", len(o.Plain), len(o.Geometry.Vertices))
		}
		out = append(out, len(o.Geometry.Indices)/3)
	}
	return out
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLargeObjectsAreSplit(t *testing.T) {
	pieces(t, 5)
	m, err := DecodeSTL(binarySTLData("", make([]uint16, 12)...), nil)
	check(t, err)
	if got := sizes(t, m); !equal(got, []int{5, 5, 2}) || m.Triangles() != 12 {
		t.Fatalf("STL pieces %v", got)
	}

	pieces(t, 3)
	files := cubeFiles(t)
	m, err = DecodeOBJ([]byte(cubeOBJ), &Options{Open: opener(files)})
	check(t, err)
	// Wood (4 triangles, shared vertices) and paint (8) split into pieces of
	// three, each with vertices of its own and its material.
	if got := sizes(t, m); !equal(got, []int{3, 1, 3, 3, 2, 0}) || m.Triangles() != 12 {
		t.Fatalf("OBJ pieces %v", got)
	}
	if m.Objects[1].Material != "wood" || m.Objects[1].Geometry.Material == nil || m.Objects[1].Name != "box" || len(m.Objects[1].Geometry.Vertices) != 3 {
		t.Fatalf("second wood piece %+v", m.Objects[1].Name)
	}

	m, err = Decode3MF(assembly3MF(t), nil)
	check(t, err)
	if got := sizes(t, m); !equal(got, []int{3, 1, 3, 1, 3, 1}) || m.Objects[3].Part != 1 || m.Objects[3].Name != "Lid" || len(m.Parts) != 3 {
		t.Fatalf("3MF pieces %v", got)
	}

	s := nt3d.NewScene()
	group, err := m.AddTo(s, nt3d.Root, "assembly")
	check(t, err)
	if node, _ := s.Node(group); len(node.Children) != 6 {
		t.Fatal(node.Children)
	}
}

func TestTriangleBudget(t *testing.T) {
	if (*Options)(nil).limit() != DefaultMaxTriangles || (&Options{MaxTriangles: 10_000_000}).limit() != 10_000_000 {
		t.Fatal("the budget is not the caller's to raise")
	}
}

// A cloud over the capacity splits into slabs along its longest axis, SH
// kept with its points, each piece valid and sharing the object's fields.
func TestPointPieces(t *testing.T) {
	defer func(g func(int) int, p func() int) { maxGaussians, piecePoints = g, p }(maxGaussians, piecePoints)
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
	piecePoints = func() int { return 3 }
	plain := Object{Geometry: nt3d.Geometry{Points: g.Points}}
	if n := len(PointPieces(plain)); n != 4 {
		t.Fatalf("plain cloud: %d pieces", n)
	}
	small := Object{Geometry: nt3d.Geometry{Points: g.Points[:3]}}
	if pieces := PointPieces(small); len(pieces) != 1 || &pieces[0].Geometry.Points[0] != &small.Geometry.Points[0] {
		t.Fatalf("small cloud: %d pieces, or copied", len(pieces))
	}
}

// A cloud splits at the compact limit by default and at the Precise limit
// with PointStyle.Precise.
func TestPointPiecesPrecise(t *testing.T) {
	defer func(c, p func(int) int) { maxGaussians, maxPreciseGaussians = c, p }(maxGaussians, maxPreciseGaussians)
	maxGaussians = func(int) int { return 4 }
	maxPreciseGaussians = func(int) int { return 2 }
	g := nt3d.Geometry{PointStyle: nt3d.PointStyle{Shape: nt3d.GaussianPoint, Units: nt3d.WorldUnits, Blend: nt3d.AlphaBlend}}
	for i := range 5 {
		g.Points = append(g.Points, nt3d.Point{Position: nt3dmath.Vec3{X: float32(i)}, Radius: 1, GaussianAxes: [3]nt3dmath.Vec3{{X: 1}, {Y: 1}, {Z: 1}}})
	}
	if n := len(PointPieces(Object{Geometry: g})); n != 2 {
		t.Fatalf("compact: %d pieces, want 2", n)
	}
	g.PointStyle.Precise = true
	if n := len(PointPieces(Object{Geometry: g})); n != 3 {
		t.Fatalf("precise: %d pieces, want 3", n)
	}
}

// Gaussian PLY and point-cloud PLY over the capacity decode as pieces
// instead of failing.
func TestGaussianPLYSplits(t *testing.T) {
	defer func(g func(int) int, p func() int) { maxGaussians, piecePoints = g, p }(maxGaussians, piecePoints)
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
	piecePoints = func() int { return 2 }
	cloud := "ply\nformat ascii 1.0\nelement vertex 5\nproperty float x\nproperty float y\nproperty float z\nend_header\n0 0 0\n1 0 0\n2 0 0\n3 0 0\n4 0 0\n"
	if m, err = DecodePLY([]byte(cloud), nil); err != nil || len(m.Objects) != 3 {
		t.Fatalf("point cloud: %v, %d objects", err, len(m.Objects))
	}
}
