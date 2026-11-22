// Copyright (c) 2026 Neomantra Corp.

package nt3dcollidescene

import (
	"image/color"
	"math"
	"testing"

	"nimbleterminal.dev/nt3d"
	nt3dcollide "nimbleterminal.dev/nt3d/collide"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func cuboid(t *testing.T) nt3d.Geometry {
	t.Helper()
	box, err := nt3d.BuildCuboid(nt3d.Cuboid{Size: nt3dmath.Vec3{X: 4, Y: 4, Z: 2}, Color: color.RGBA{A: 255}})
	if err != nil {
		t.Fatal(err)
	}
	return box
}

// A geometry becomes a mesh of its triangles, placed by the world matrix: a
// ray from above meets the lifted box's top face.
func TestNewMeshPlacesGeometryInTheWorld(t *testing.T) {
	world := nt3dmath.TRSd(nt3dmath.Vec3d{Z: 1}, nt3dmath.QuatdIdentity(), nt3dmath.Vec3d{X: 1, Y: 1, Z: 1})
	m := NewMesh(cuboid(t), world)
	if m.TriangleCount() != 12 {
		t.Fatalf("triangles = %d, want 12", m.TriangleCount())
	}
	hit, ok := m.RayCast(nt3dmath.Vec3d{Z: 5}, nt3dmath.Vec3d{Z: -1}, 10)
	if !ok || math.Abs(hit.Position.Z-2.0) > 1e-5 || hit.Normal.Z < 0.99 {
		t.Fatalf("RayCast = (%+v, %v), want a hit at Z=2 facing +Z", hit, ok)
	}
}

// The zero matrix means identity, as it does for a missing entry of worlds.
func TestZeroWorldMatrixMeansIdentity(t *testing.T) {
	box := cuboid(t)
	for name, m := range map[string]*nt3dcollide.Mesh{
		"NewMesh":               NewMesh(box, nt3dmath.Mat4d{}),
		"NewMeshFromGeometries": NewMeshFromGeometries([]nt3d.Geometry{box}, nil),
	} {
		lo, hi, ok := m.Bounds()
		if !ok || lo != (nt3dmath.Vec3d{X: -2, Y: -2, Z: -1}) || hi != (nt3dmath.Vec3d{X: 2, Y: 2, Z: 1}) {
			t.Fatalf("%s bounds = %v..%v (%v), want the box as built", name, lo, hi, ok)
		}
	}
}

// Several geometries make one mesh, each placed by its own matrix.
func TestNewMeshFromGeometriesCombines(t *testing.T) {
	box := cuboid(t)
	east := nt3dmath.TRSd(nt3dmath.Vec3d{X: 10}, nt3dmath.QuatdIdentity(), nt3dmath.Vec3d{X: 1, Y: 1, Z: 1})
	m := NewMeshFromGeometries([]nt3d.Geometry{box, box}, []nt3dmath.Mat4d{{}, east})
	if m.TriangleCount() != 24 {
		t.Fatalf("triangles = %d, want 24", m.TriangleCount())
	}
	if _, hi, _ := m.Bounds(); hi.X != 12 {
		t.Fatalf("bounds reach X=%v, want 12", hi.X)
	}
}

// Indices that point past the vertices, and a trailing partial triangle, are
// left out without a panic.
func TestTrianglesSkipsBadIndices(t *testing.T) {
	box := cuboid(t)
	box.Indices = append(box.Indices[:6:6], 0, 1, uint32(len(box.Vertices)), 0, 1)
	if got := len(Triangles(nil, box, nt3dmath.Mat4d{})); got != 2 {
		t.Fatalf("triangles = %d, want 2", got)
	}
}
