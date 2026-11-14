// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image"
	"image/color"
	"math"
	"slices"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func meshTestBounds() (b nt3dmath.AABB) {
	b.Include(nt3dmath.Vec3{X: -3, Y: -3, Z: -3})
	b.Include(nt3dmath.Vec3{X: 3, Y: 3, Z: 3})
	return
}

func TestMeshUpdatesPreserveSnapshotsAndTopology(t *testing.T) {
	s := NewScene()
	nmap := testNormalMap(t, color.NRGBA{128, 128, 255, 255})
	base, err := BuildHeightfield(Heightfield{
		NX: 4, NY: 4,
		MinX: -1, MaxX: 1, MinY: -1, MaxY: 1,
		Sampler:  func(x, y float32) float32 { return 0 },
		Material: &Material{NormalMap: nmap, NormalStrength: 1},
	})
	check(t, err)
	id, err := s.SetGeometry("sheet", base)
	check(t, err)

	before := frame(t, s)
	if len(before.Geometry[0].Tangents) != len(before.Geometry[0].Vertices) {
		t.Fatal("expected generated tangents on upload")
	}

	sub := []Vertex{{
		Position: nt3dmath.Vec3{X: before.Geometry[0].Vertices[2].Position.X, Y: before.Geometry[0].Vertices[2].Position.Y, Z: .75},
		Normal:   nt3dmath.Vec3{X: -.3, Z: .95}.Normalize(),
		Color:    color.RGBA{40, 180, 220, 255},
	}}
	check(t, s.UpdateNodeVertices(id, 2, sub, meshTestBounds()))
	afterSub := frame(t, s)
	sub[0].Position.Z = 99
	if before.Geometry[0].Vertices[2].Position.Z != 0 || afterSub.Geometry[0].Vertices[2].Position.Z != .75 {
		t.Fatal("snapshot or caller slice ownership violated")
	}
	if before.GeometryRevisions[0] != afterSub.GeometryRevisions[0] {
		t.Fatal("fixed-topology update changed geometryRevision")
	}
	if &before.Geometry[0].Indices[0] != &afterSub.Geometry[0].Indices[0] || &before.Geometry[0].UV[0] != &afterSub.Geometry[0].UV[0] {
		t.Fatal("unchanged indices or UVs were cloned")
	}
	if afterSub.Geometry[0].Bounds != meshTestBounds() {
		t.Fatal("bounds were not taken from caller")
	}
	check(t, validateFrame(afterSub))

	// Explicit analytic tangents path.
	verts := slices.Clone(afterSub.Geometry[0].Vertices)
	tangents := make([]Tangent, len(verts))
	for i := range verts {
		verts[i].Position.Z = .25
		verts[i].Normal = nt3dmath.Vec3{Z: 1}
		tangents[i] = Tangent{Direction: nt3dmath.Vec3{X: 1}, W: -1}
	}
	check(t, s.SetNodeVerticesWithTangents(id, verts, tangents, meshTestBounds()))
	afterExplicit := frame(t, s)
	tangents[0].W = 1
	if afterExplicit.Geometry[0].Tangents[0].W != -1 {
		t.Fatal("caller tangent mutation leaked into snapshot")
	}
	if afterSub.Geometry[0].Vertices[2].Position.Z != .75 {
		t.Fatal("earlier snapshot mutated by SetNodeVerticesWithTangents")
	}
	check(t, validateFrame(afterExplicit))

	// Validation rejections leave stored state untouched.
	nan := float32(math.NaN())
	if s.UpdateNodeVertices(id, 0, []Vertex{{Position: nt3dmath.Vec3{X: nan}, Normal: nt3dmath.Vec3{Z: 1}}}, meshTestBounds()) == nil {
		t.Fatal("accepted NaN vertex")
	}
	if s.UpdateNodeVertices(id, 0, []Vertex{{Position: nt3dmath.Vec3{X: 10}, Normal: nt3dmath.Vec3{Z: 1}}}, meshTestBounds()) == nil {
		t.Fatal("accepted vertex outside supplied bounds")
	}
	if s.UpdateNodeVertices(id, len(verts), verts[:1], meshTestBounds()) == nil {
		t.Fatal("accepted out-of-range vertex offset")
	}
	if s.SetNodeVertices(id, verts[:2], meshTestBounds()) == nil {
		t.Fatal("accepted mismatched vertex count")
	}
	if s.SetNodeVerticesWithTangents(id, verts, nil, meshTestBounds()) == nil {
		t.Fatal("accepted dropped tangents on tangent-bearing mesh")
	}
	badTan := slices.Clone(afterExplicit.Geometry[0].Tangents)
	badTan[0].W = 0
	if s.SetNodeVerticesWithTangents(id, verts, badTan, meshTestBounds()) == nil {
		t.Fatal("accepted invalid tangent handedness")
	}
	if s.SetNodeVertices(id, verts, nt3dmath.AABB{}) == nil {
		t.Fatal("accepted invalid bounds")
	}
	if frame(t, s).Geometry[0].Vertices[0].Position.Z != .25 {
		t.Fatal("failed vertex update mutated scene state")
	}
}

func TestMeshCertificateValidatesTamperedFrameCopies(t *testing.T) {
	s := NewScene()
	nmap := testNormalMap(t, color.NRGBA{128, 128, 255, 255})
	g, err := BuildHeightfield(Heightfield{
		NX: 3, NY: 3,
		MinX: -1, MaxX: 1, MinY: -1, MaxY: 1,
		Sampler:  func(x, y float32) float32 { return 0 },
		Material: &Material{NormalMap: nmap, NormalStrength: 1},
	})
	check(t, err)
	_, err = s.SetGeometry("grid", g)
	check(t, err)
	f := frame(t, s)
	check(t, validateFrame(f))

	nan := float32(math.NaN())

	tamperVerts := f
	tamperVerts.Geometry = slices.Clone(f.Geometry)
	tamperVerts.Geometry[0].Vertices = slices.Clone(f.Geometry[0].Vertices)
	tamperVerts.Geometry[0].Vertices[0].Position.X = nan
	if validateFrame(tamperVerts) == nil {
		t.Fatal("tampered vertices passed validateFrame")
	}

	tamperIdx := f
	tamperIdx.Geometry = slices.Clone(f.Geometry)
	tamperIdx.Geometry[0].Indices = slices.Clone(f.Geometry[0].Indices)
	tamperIdx.Geometry[0].Indices[0] = 999
	if validateFrame(tamperIdx) == nil {
		t.Fatal("tampered indices passed validateFrame")
	}

	tamperUV := f
	tamperUV.Geometry = slices.Clone(f.Geometry)
	tamperUV.Geometry[0].UV = slices.Clone(f.Geometry[0].UV)
	tamperUV.Geometry[0].UV[0].U = nan
	if validateFrame(tamperUV) == nil {
		t.Fatal("tampered UVs passed validateFrame")
	}

	tamperTan := f
	tamperTan.Geometry = slices.Clone(f.Geometry)
	tamperTan.Geometry[0].Tangents = slices.Clone(f.Geometry[0].Tangents)
	tamperTan.Geometry[0].Tangents[0].W = 0
	if validateFrame(tamperTan) == nil {
		t.Fatal("tampered tangents passed validateFrame")
	}
}

func BenchmarkMeshUpdate64x64(b *testing.B) {
	img := testNormalImage(color.NRGBA{128, 128, 255, 255})
	nmap, err := NewNormalMap(img)
	if err != nil {
		b.Fatal(err)
	}
	mesh, err := BuildHeightfield(Heightfield{
		NX: 65, NY: 65,
		MinX: -5, MaxX: 5, MinY: -5, MaxY: 5,
		Sampler:  func(x, y float32) float32 { return 0 },
		Material: &Material{NormalMap: nmap, NormalStrength: 1},
	})
	if err != nil {
		b.Fatal(err)
	}
	mesh, err = GenerateTangents(mesh)
	if err != nil {
		b.Fatal(err)
	}
	bounds := meshTestBounds()
	bounds.Include(nt3dmath.Vec3{X: -6, Y: -6, Z: -2})
	bounds.Include(nt3dmath.Vec3{X: 6, Y: 6, Z: 2})

	b.Run("SetNodeGeometry", func(b *testing.B) {
		s := NewScene()
		id, err := s.SetGeometry("grid", mesh)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if err := s.SetNodeGeometry(id, mesh); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("SetNodeVerticesWithTangents", func(b *testing.B) {
		s := NewScene()
		id, err := s.SetGeometry("grid", mesh)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if err := s.SetNodeVerticesWithTangents(id, mesh.Vertices, mesh.Tangents, bounds); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func testNormalImage(c color.NRGBA) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}
