// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image"
	"image/color"
	"math"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

func TestSceneSetLODSelectionAndDistanceCull(t *testing.T) {
	s := NewScene()
	nearMesh, err := BuildSphere(Sphere{Radius: 1, Segments: 32, Rings: 16, Color: color.RGBA{240, 40, 40, 255}})
	check(t, err)
	midMesh, err := BuildSphere(Sphere{Radius: 1, Segments: 12, Rings: 6, Color: color.RGBA{40, 220, 60, 255}})
	check(t, err)
	farMesh, err := BuildSphere(Sphere{Radius: 1, Segments: 6, Rings: 4, Color: color.RGBA{40, 80, 240, 255}})
	check(t, err)

	inkCrease := OutlineStyle{Width: 2, Crease: 35, Color: color.RGBA{10, 10, 10, 255}}
	inkHull := OutlineStyle{Width: 1, Color: color.RGBA{10, 10, 10, 255}}

	id, err := s.Add(Root, "chunk")
	check(t, err)
	levels := []LODLevel{
		{MaxDistance: 10, Geometry: nearMesh, Outline: inkCrease},
		{MaxDistance: 30, Geometry: midMesh, Outline: inkHull},
		{MaxDistance: 60, Geometry: farMesh},
	}
	check(t, s.SetLOD(id, levels))

	// Mutating caller slices after SetLOD must not affect the stored levels.
	origPos := nearMesh.Vertices[0].Position
	levels[0].MaxDistance = 1
	nearMesh.Vertices[0].Position = nt3dmath.Vec3{X: 999}

	camAt := func(x float32) Camera {
		c := DefaultCamera()
		c.Projection = Perspective
		return c.LookAt(nt3dmath.Vec3{X: x}, nt3dmath.Vec3{})
	}

	// Sphere has radius 1 (AABB [-1, 1]^3), so eye at X=8 is distance 7 from AABB (<= 10 -> tier 0).
	fNear, err := s.Frame(camAt(8), 80, 80)
	check(t, err)
	if len(fNear.Geometry[0].Indices) != len(nearMesh.Indices) || fNear.Geometry[0].Outline != inkCrease || !fNear.Instances[0].Visible {
		t.Fatalf("near frame got indices=%d outline=%+v visible=%v", len(fNear.Geometry[0].Indices), fNear.Geometry[0].Outline, fNear.Instances[0].Visible)
	}

	// Eye at X=20 is distance 19 from AABB (10 < 19 <= 30 -> tier 1).
	fMid, err := s.Frame(camAt(20), 80, 80)
	check(t, err)
	if len(fMid.Geometry[0].Indices) != len(midMesh.Indices) || fMid.Geometry[0].Outline != inkHull || !fMid.Instances[0].Visible {
		t.Fatalf("mid frame got indices=%d outline=%+v visible=%v", len(fMid.Geometry[0].Indices), fMid.Geometry[0].Outline, fMid.Instances[0].Visible)
	}

	// Eye at X=45 is distance 44 from AABB (30 < 44 <= 60 -> tier 2).
	fFar, err := s.Frame(camAt(45), 80, 80)
	check(t, err)
	if len(fFar.Geometry[0].Indices) != len(farMesh.Indices) || fFar.Geometry[0].Outline != (OutlineStyle{}) || !fFar.Instances[0].Visible {
		t.Fatalf("far frame got indices=%d outline=%+v visible=%v", len(fFar.Geometry[0].Indices), fFar.Geometry[0].Outline, fFar.Instances[0].Visible)
	}

	// Eye at X=70 is distance 69 (> 60 -> distance-culled).
	fCulled, err := s.Frame(camAt(70), 80, 80)
	check(t, err)
	if fCulled.Instances[0].Visible {
		t.Fatal("expected node beyond final finite MaxDistance to be hidden")
	}
	img := softwareRender(fCulled).(*image.RGBA)
	if drawn(img, fCulled.Background) != 0 {
		t.Fatal("distance-culled node drew pixels")
	}

	// Zero MaxDistance on the final tier makes it unbounded.
	nearMesh.Vertices[0].Position = origPos
	check(t, s.SetLOD(id, []LODLevel{
		{MaxDistance: 10, Geometry: nearMesh},
		{Geometry: farMesh},
	}))
	fVeryFar, err := s.Frame(camAt(500), 80, 80)
	check(t, err)
	if !fVeryFar.Instances[0].Visible || len(fVeryFar.Geometry[0].Indices) != len(farMesh.Indices) {
		t.Fatalf("unbounded final tier not selected at distance: visible=%v indices=%d", fVeryFar.Instances[0].Visible, len(fVeryFar.Geometry[0].Indices))
	}
}

func TestSceneSetLODCityTileAABBDistance(t *testing.T) {
	s := NewScene()
	// A 100x100 city block slab spanning [0..100, 0..100, 0..10], translated to (200, 300, 0).
	hiBlock := Geometry{Boxes: []Box{{Min: nt3dmath.Vec3{}, Size: nt3dmath.Vec3{X: 100, Y: 100, Z: 10}, Color: color.RGBA{200, 60, 60, 255}}}}
	loBlock := Geometry{Boxes: []Box{{Min: nt3dmath.Vec3{}, Size: nt3dmath.Vec3{X: 100, Y: 100, Z: 10}, Color: color.RGBA{60, 60, 200, 255}}}}

	id, err := s.Add(Root, "tile")
	check(t, err)
	check(t, s.SetTransform(id, nt3dmath.Translation(nt3dmath.Vec3{X: 200, Y: 300})))
	check(t, s.SetLOD(id, []LODLevel{
		{MaxDistance: 25, Geometry: hiBlock},
		{Geometry: loBlock},
	}))

	// Hovering 15 units above the center of the roof at (250, 350, 25):
	// Distance to world origin (200, 300, 0) is ~75, but distance to world AABB [200..300, 300..400, 0..10] is 15 <= 25.
	c := DefaultCamera()
	c.Projection = Perspective
	c = c.LookAt(nt3dmath.Vec3{X: 250, Y: 350, Z: 25}, nt3dmath.Vec3{X: 250, Y: 350, Z: 5})
	f, err := s.Frame(c, 64, 64)
	check(t, err)
	if got := f.Geometry[0].Boxes[0].Color; got != hiBlock.Boxes[0].Color {
		t.Fatalf("camera above tile roof got color %v, want high-detail %v", got, hiBlock.Boxes[0].Color)
	}
}

func TestSceneSetLODSharedPickAndMutators(t *testing.T) {
	s := NewScene()
	hiSphere, err := BuildSphere(Sphere{Radius: 1, Segments: 32, Rings: 16, Color: color.RGBA{220, 80, 80, 255}})
	check(t, err)
	loSphere, err := BuildSphere(Sphere{Radius: 1, Segments: 24, Rings: 12, Color: color.RGBA{80, 80, 220, 255}})
	check(t, err)

	texA, err := NewTexture(image.NewRGBA(image.Rect(0, 0, 2, 2)))
	check(t, err)
	texB, err := NewTexture(image.NewRGBA(image.Rect(0, 0, 2, 2)))
	check(t, err)
	nmapA, err := NewNormalMap(image.NewRGBA(image.Rect(0, 0, 2, 2)))
	check(t, err)
	nmapB, err := NewNormalMap(image.NewRGBA(image.Rect(0, 0, 2, 2)))
	check(t, err)

	hiSphere.Material = &Material{Texture: texA, NormalMap: nmapA, NormalStrength: 1}
	sgHi, err := NewSharedGeometry(hiSphere)
	check(t, err)
	sgLo, err := NewSharedGeometry(loSphere)
	check(t, err)

	outlineNear := OutlineStyle{Width: 2, Color: color.RGBA{20, 30, 40, 255}}
	id, err := s.Add(Root, "prop")
	check(t, err)
	check(t, s.SetLOD(id, []LODLevel{
		{MaxDistance: 10, Shared: sgHi, Outline: outlineNear},
		{MaxDistance: float32(math.Inf(1)), Shared: sgLo},
	}))
	if sgHi.geometry.Outline != (OutlineStyle{}) {
		t.Fatal("LODLevel.Outline mutated SharedGeometry")
	}

	// WarmPickIndices warms BVH slots across all LOD levels.
	<-s.WarmPickIndices()
	if sgHi.pick == nil || sgHi.pick.index.Load() == nil || sgLo.pick == nil || sgLo.pick.index.Load() == nil {
		t.Fatal("WarmPickIndices did not build BVHs for shared LOD levels")
	}

	for _, dist := range []float32{5, 25} {
		c := DefaultCamera()
		c.Projection = Perspective
		c = c.LookAt(nt3dmath.Vec3{X: dist}, nt3dmath.Vec3{})
		f, err := s.Frame(c, 100, 100)
		check(t, err)
		if f.Shared[0] == 0 {
			t.Fatalf("dist %g: expected nonzero Shared ID", dist)
		}
		hit := s.Pick(c, f, 50, 50, 2)
		if hit == nil || hit.Node != id {
			t.Fatalf("dist %g: pick missed LOD node: %+v", dist, hit)
		}
	}

	// SetTexture and SetNormalMap update textured/normal-mapped tiers even when coarser tiers are untextured.
	check(t, s.SetTexture("prop", texB))
	check(t, s.SetNormalMap("prop", nmapB))
	if s.nodes[id].lod[0].geometry.Material.Texture != texB || s.nodes[id].lod[0].geometry.Material.NormalMap != nmapB {
		t.Fatal("SetTexture/SetNormalMap did not update textured LOD tier")
	}

	// SetOutline and SetToon propagate to all LOD tiers in the subtree.
	toon := ToonStyle{Bands: 3}
	newOutline := OutlineStyle{Width: 3, Color: color.RGBA{1, 2, 3, 255}}
	check(t, s.SetToon(Root, toon))
	check(t, s.SetOutline(Root, newOutline))
	for i, l := range s.nodes[id].lod {
		if l.geometry.Outline != newOutline || l.geometry.Material == nil || l.geometry.Material.Toon != toon {
			t.Fatalf("tier %d did not receive SetOutline/SetToon: outline=%+v mat=%+v", i, l.geometry.Outline, l.geometry.Material)
		}
	}

	// SetDeformation and point updates reject LOD nodes cleanly.
	if err := s.SetDeformation(id, Deformation{}); err == nil {
		t.Fatal("expected SetDeformation on LOD node to fail")
	}
	if err := s.UpdateNodePoints(id, 0, nil, nt3dmath.AABB{}); err == nil {
		t.Fatal("expected UpdateNodePoints on LOD node to fail")
	}

	// SetNodeGeometry clears the LOD chain.
	check(t, s.SetNodeGeometry(id, loSphere))
	if len(s.nodes[id].lod) != 0 {
		t.Fatal("SetNodeGeometry did not clear LOD levels")
	}
}

func TestSceneSetLODValidationAtomicity(t *testing.T) {
	s := NewScene()
	valid := Geometry{Points: []Point{{Position: nt3dmath.Vec3{Z: 1}, Radius: 4, Color: color.RGBA{255, 0, 0, 255}}}}
	id, err := s.SetGeometry("marker", valid)
	check(t, err)
	mesh, err := BuildSphere(Sphere{Radius: 1, Segments: 8, Rings: 4})
	check(t, err)
	sg, err := NewSharedGeometry(mesh)
	check(t, err)

	rev := s.Revision()
	badCases := [][]LODLevel{
		nil,
		{{MaxDistance: -1, Geometry: valid}},
		{{MaxDistance: float32(math.NaN()), Geometry: valid}},
		{{MaxDistance: 0, Geometry: valid}, {MaxDistance: 10, Geometry: valid}},
		{{MaxDistance: float32(math.Inf(1)), Geometry: valid}, {Geometry: valid}},
		{{MaxDistance: 10, Geometry: valid}, {MaxDistance: 10, Geometry: valid}},
		{{MaxDistance: 20, Geometry: valid}, {MaxDistance: 10, Geometry: valid}},
		{{MaxDistance: 10, Geometry: valid, Shared: sg}},
		{{MaxDistance: 10, Geometry: valid, Outline: OutlineStyle{Width: -1}}},
		{{MaxDistance: 10, Geometry: Geometry{Points: []Point{{Radius: -5}}}}},
	}
	for i, bad := range badCases {
		if err := s.SetLOD(id, bad); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
	if err := s.SetLOD(Root, []LODLevel{{Geometry: valid}}); err == nil {
		t.Fatal("expected Root to be rejected")
	}
	if err := s.SetLOD(999999, []LODLevel{{Geometry: valid}}); err == nil {
		t.Fatal("expected unknown node to be rejected")
	}
	if s.Revision() != rev || len(s.nodes[id].Points()) == 0 {
		t.Fatal("failed SetLOD mutated scene state")
	}
}

func TestSceneSetLODBoundsAndBillboardImpostor(t *testing.T) {
	s := NewScene()
	mesh, err := BuildSphere(Sphere{Radius: 1, Segments: 12, Rings: 6, Color: color.RGBA{180, 90, 60, 255}})
	check(t, err)
	impostor, err := (Label{Text: "T", Height: 2}).Geometry()
	check(t, err)

	id, err := s.Add(Root, "tree")
	check(t, err)
	check(t, s.SetTransform(id, nt3dmath.Translation(nt3dmath.Vec3{X: 10, Y: -5, Z: 2})))
	check(t, s.SetLOD(id, []LODLevel{
		{MaxDistance: 15, Geometry: mesh},
		{Geometry: impostor},
	}))

	b := s.Bounds()
	if !b.IsValid() || b.Min.X > 9.01 || b.Max.X < 10.99 {
		t.Fatalf("unexpected LOD Scene.Bounds: %+v", b)
	}

	// Far camera selects the billboard impostor and resolves its camera-facing world transform.
	c := DefaultCamera()
	c.Projection = Perspective
	c = c.LookAt(nt3dmath.Vec3{X: 50, Y: -5, Z: 2}, nt3dmath.Vec3{X: 10, Y: -5, Z: 2})
	f, err := s.Frame(c, 80, 80)
	check(t, err)
	if f.Geometry[0].Billboard == nil || !f.Instances[0].Visible {
		t.Fatalf("expected visible billboard impostor at far distance, got billboard=%v visible=%v", f.Geometry[0].Billboard, f.Instances[0].Visible)
	}
}

func (n *sceneNode) Points() []Point {
	if n == nil || n.geometry == nil {
		return nil
	}
	return n.geometry.Points
}
