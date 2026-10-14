// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image/color"
	"math"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// signedVolume is positive when a closed mesh's faces wind outward.
func signedVolume(g Geometry) float64 {
	total := 0.0
	for i := 0; i < len(g.Indices); i += 3 {
		a, b, c := g.Vertices[g.Indices[i]].Position, g.Vertices[g.Indices[i+1]].Position, g.Vertices[g.Indices[i+2]].Position
		total += float64(a.Dot(b.Cross(c))) / 6
	}
	return total
}

// closed reports whether every edge, by position, joins exactly two faces
// traversing it in opposite directions.
func closed(t *testing.T, g Geometry) {
	t.Helper()
	type edge struct{ a, b nt3dmath.Vec3 }
	count := map[edge]int{}
	for i := 0; i < len(g.Indices); i += 3 {
		for k := range 3 {
			a, b := g.Vertices[g.Indices[i+k]].Position, g.Vertices[g.Indices[i+(k+1)%3]].Position
			count[edge{a, b}]++
		}
	}
	for e, n := range count {
		if n != 1 || count[edge{e.b, e.a}] != 1 {
			t.Fatalf("edge %v used %d times, reverse %d", e, n, count[edge{e.b, e.a}])
		}
	}
}

// normalsAgree checks that vertex normals point the way faces wind.
func normalsAgree(t *testing.T, g Geometry) {
	t.Helper()
	for i := 0; i < len(g.Indices); i += 3 {
		n := faceNormal(g, i)
		for k := range 3 {
			if v := g.Vertices[g.Indices[i+k]]; v.Normal.Dot(n) <= 0 {
				t.Fatalf("face %d corner %d normal %v opposes %v", i/3, k, v.Normal, n)
			}
		}
	}
}

func near(t *testing.T, name string, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance*math.Abs(want) {
		t.Fatalf("%s = %g, want %g ± %g%%", name, got, want, tolerance*100)
	}
}

func TestShapesAreClosedAndOutward(t *testing.T) {
	sphere, err := BuildSphere(Sphere{Radius: 2, Segments: 64, Rings: 32})
	check(t, err)
	cylinder, err := BuildCylinder(Cylinder{BottomRadius: 1, TopRadius: 1, Height: 3, Segments: 64})
	check(t, err)
	cone, err := BuildCylinder(Cylinder{BottomRadius: 1, Height: 3, Segments: 64})
	check(t, err)
	inverted, err := BuildCylinder(Cylinder{TopRadius: 1, Height: 3, Segments: 64})
	check(t, err)
	frustum, err := BuildCylinder(Cylinder{BottomRadius: 2, TopRadius: 1, Height: 1, Segments: 64})
	check(t, err)
	torus, err := BuildTorus(Torus{MajorRadius: 2, MinorRadius: .5, Segments: 96, Sides: 48})
	check(t, err)
	cuboid, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 1, Y: 2, Z: 3}})
	check(t, err)
	for _, c := range []struct {
		name   string
		g      Geometry
		volume float64
	}{
		{"sphere", sphere, 4 * math.Pi * 8 / 3},
		{"cylinder", cylinder, math.Pi * 3},
		{"cone", cone, math.Pi * 3 / 3},
		{"inverted cone", inverted, math.Pi * 3 / 3},
		{"frustum", frustum, math.Pi * (4 + 2 + 1) / 3},
		{"torus", torus, 2 * math.Pi * math.Pi * 2 * .25},
		{"cuboid", cuboid, 6},
	} {
		t.Run(c.name, func(t *testing.T) {
			closed(t, c.g)
			normalsAgree(t, c.g)
			near(t, "volume", signedVolume(c.g), c.volume, .01)
			if len(c.g.UV) != len(c.g.Vertices) || c.g.Vertices[0].Color != (color.RGBA{255, 255, 255, 255}) {
				t.Fatal("missing UVs or default color")
			}
		})
	}
	if b := sphere.Bounds; b.Min.Z != -2 || b.Max.Z != 2 {
		t.Fatal(b)
	}
}

func TestShapeTexturesAndLimits(t *testing.T) {
	texture := testTexture(t, color.RGBA{9, 9, 9, 255})
	s, err := BuildSphere(Sphere{Radius: 1, Material: &Material{Texture: texture}})
	check(t, err)
	if len(s.Indices)/3 != 32*16*2-2*32 {
		t.Fatal(len(s.Indices) / 3)
	}
	for _, bad := range []error{
		func() error { _, err := BuildSphere(Sphere{}); return err }(),
		func() error { _, err := BuildSphere(Sphere{Radius: 1, Segments: 2}); return err }(),
		func() error { _, err := BuildSphere(Sphere{Radius: float32(math.NaN())}); return err }(),
		func() error { _, err := BuildCylinder(Cylinder{Height: 1}); return err }(),
		func() error { _, err := BuildTorus(Torus{MajorRadius: 1, MinorRadius: 1}); return err }(),
		func() error { _, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 1, Y: 1}}); return err }(),
	} {
		if bad == nil {
			t.Fatal("invalid shape accepted")
		}
	}
}

func TestTransformGeometryMirrors(t *testing.T) {
	cone, err := BuildCylinder(Cylinder{BottomRadius: 1, Height: 2})
	check(t, err)
	mirrored, err := TransformGeometry(cone, nt3dmath.Scaling(nt3dmath.Vec3{X: -2, Y: 1, Z: 1}))
	check(t, err)
	closed(t, mirrored)
	normalsAgree(t, mirrored)
	near(t, "volume", signedVolume(mirrored), 2*signedVolume(cone), 1e-4)
	if mirrored.Bounds.Max.X != 2 || cone.Bounds.Max.X != 1 {
		t.Fatal("input changed or bounds stale")
	}
	boxes := Geometry{Boxes: []Box{{Min: nt3dmath.Vec3{X: 1}, Size: nt3dmath.Vec3{X: 1, Y: 1, Z: 1}}}}
	moved, err := TransformGeometry(boxes, nt3dmath.Scaling(nt3dmath.Vec3{X: -1, Y: 1, Z: 1}))
	check(t, err)
	if b := moved.Boxes[0]; b.Min.X != -2 || b.Size.X != 1 {
		t.Fatal(b)
	}
	if _, err = TransformGeometry(boxes, nt3dmath.RotationZ(.3)); err == nil {
		t.Fatal("rotated box accepted")
	}
	if _, err = TransformGeometry(cone, nt3dmath.Mat4{}); err == nil {
		t.Fatal("singular transform accepted")
	}
}

func TestMergeGeometry(t *testing.T) {
	a, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 1, Y: 1, Z: 1}})
	check(t, err)
	b, err := TransformGeometry(a, nt3dmath.Translation(nt3dmath.Vec3{X: 3}))
	check(t, err)
	b.Points = pointGeometry().Points
	merged, err := MergeGeometry(a, b)
	check(t, err)
	if len(merged.Indices) != 72 || merged.Indices[36] != 24 || len(merged.UV) != 48 || len(merged.Points) != 1 {
		t.Fatal(len(merged.Indices), merged.Indices[36], len(merged.UV))
	}
	near(t, "volume", signedVolume(merged), 2, 1e-6)
	if merged.Bounds.Max.X != 3.5 {
		t.Fatal(merged.Bounds)
	}
	texture := testTexture(t, color.RGBA{9, 9, 9, 255})
	b.Material = &Material{Texture: texture}
	if _, err = MergeGeometry(a, b); err == nil {
		t.Fatal("different materials merged")
	}
	a.UV = nil
	if merged, err = MergeGeometry(a, a); err != nil || merged.UV != nil {
		t.Fatal("partial UVs kept", err)
	}
}

func TestNormals(t *testing.T) {
	cuboid, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 1, Y: 1, Z: 1}})
	check(t, err)
	hard, err := SmoothNormals(cuboid, 30)
	check(t, err)
	if len(hard.Vertices) != 24 {
		t.Fatal("hard edges smoothed", len(hard.Vertices))
	}
	cuboid.UV = nil // Faces with UVs of their own cannot share corners.
	soft, err := SmoothNormals(cuboid, 180)
	check(t, err)
	if len(soft.Vertices) != 8 {
		t.Fatal(len(soft.Vertices))
	}
	for _, v := range soft.Vertices {
		// Each corner meets three faces with equal area.
		if d := v.Normal.Dot(v.Position.Normalize()); d < .9999 {
			t.Fatal(v)
		}
	}
	sphere, err := BuildSphere(Sphere{Radius: 1})
	check(t, err)
	flat, err := FlatNormals(sphere)
	check(t, err)
	closed(t, flat)
	for i := 0; i < len(flat.Indices); i += 3 {
		n := faceNormal(flat, i).Normalize()
		for k := range 3 {
			if flat.Vertices[flat.Indices[i+k]].Normal != n {
				t.Fatal("flat normal differs from its face")
			}
		}
	}
	// Smoothing a flat sphere restores normals close to the radial ones.
	smooth, err := SmoothNormals(flat, 60)
	check(t, err)
	for _, v := range smooth.Vertices {
		if v.Normal.Dot(v.Position.Normalize()) < .99 {
			t.Fatal(v)
		}
	}
	if len(smooth.UV) != len(smooth.Vertices) {
		t.Fatal("UVs lost")
	}
}
