// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"bytes"
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

func TestSceneLODBiasSelectionValidationAndO1(t *testing.T) {
	s := NewScene()
	if s.LODBias() != 1 || s.LODHysteresis() != 0 {
		t.Fatalf("defaults bias=%v hysteresis=%v, want 1, 0", s.LODBias(), s.LODHysteresis())
	}

	// Point at origin has zero-size AABB [0,0,0], so Eye at X=d has exact distance d.
	mkTier := func(c uint8) Geometry {
		return Geometry{Points: []Point{{Radius: 3, Color: color.RGBA{c, 40, 200, 255}}}}
	}
	t0, t1, t2 := mkTier(10), mkTier(20), mkTier(30)

	id, err := s.Add(Root, "probe")
	check(t, err)
	check(t, s.SetLOD(id, []LODLevel{
		{MaxDistance: 10, Geometry: t0},
		{MaxDistance: 20, Geometry: t1},
		{Geometry: t2},
	}))

	camAt := func(x float32) Camera {
		c := DefaultCamera()
		c.Projection = Perspective
		return c.LookAt(nt3dmath.Vec3{X: x}, nt3dmath.Vec3{})
	}
	tierAt := func(x float32) (int, bool) {
		f, err := s.Frame(camAt(x), 64, 64)
		check(t, err)
		f.Cull = false
		st := f.Stats()
		if !f.Instances[0].Visible {
			return -1, false
		}
		for i, n := range st.LODTierInstances {
			if n == 1 {
				return i, true
			}
		}
		t.Fatalf("expected 1 drawn LOD tier instance, got %+v", st.LODTierInstances)
		return -1, false
	}

	// Distance 15 against 10 / 20 / +Inf: bias 1 -> tier 1, bias 2 -> tier 0, bias 0.5 -> tier 2.
	if got, vis := tierAt(15); !vis || got != 1 {
		t.Fatalf("bias 1 at dist 15: got tier=%d vis=%v, want 1, true", got, vis)
	}
	check(t, s.SetLODBias(2))
	if s.LODBias() != 2 {
		t.Fatalf("LODBias() = %v, want 2", s.LODBias())
	}
	if got, vis := tierAt(15); !vis || got != 0 {
		t.Fatalf("bias 2 at dist 15: got tier=%d vis=%v, want 0, true", got, vis)
	}
	check(t, s.SetLODBias(0.5))
	if got, vis := tierAt(15); !vis || got != 2 {
		t.Fatalf("bias 0.5 at dist 15: got tier=%d vis=%v, want 2, true", got, vis)
	}

	// Finite hide-beyond cutoff scales with bias too.
	check(t, s.SetLOD(id, []LODLevel{
		{MaxDistance: 10, Geometry: t0},
		{MaxDistance: 20, Geometry: t1},
		{MaxDistance: 40, Geometry: t2},
	}))
	check(t, s.SetLODBias(1))
	if got, vis := tierAt(30); !vis || got != 2 {
		t.Fatalf("cutoff 40 bias 1 at dist 30: got tier=%d vis=%v, want 2, true", got, vis)
	}
	check(t, s.SetLODBias(0.5)) // effective cutoff 20 < 30 -> hidden
	if _, vis := tierAt(30); vis {
		t.Fatal("cutoff 40 bias 0.5 at dist 30: expected node hidden")
	}
	check(t, s.SetLODBias(2)) // effective tiers 20 / 40 / 80 -> tier 1 at dist 30
	if got, vis := tierAt(30); !vis || got != 1 {
		t.Fatalf("cutoff 40 bias 2 at dist 30: got tier=%d vis=%v, want 1, true", got, vis)
	}

	// Invalid biases return an error and leave bias and scene revision untouched.
	rev := s.Revision()
	for _, bad := range []float32{0, float32(math.Copysign(0, -1)), -1, float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))} {
		if err := s.SetLODBias(bad); err == nil {
			t.Fatalf("SetLODBias(%v) succeeded, want error", bad)
		}
	}
	if s.LODBias() != 2 || s.Revision() != rev {
		t.Fatalf("invalid SetLODBias mutated state: bias=%v rev=%d (want 2, %d)", s.LODBias(), s.Revision(), rev)
	}

	// O(1) bias update: level geometry pointers and revisions across many LOD nodes stay untouched.
	many := NewScene()
	sg0, err := NewSharedGeometry(mustSphere(t, 12, 6, color.RGBA{200, 60, 60, 255}))
	check(t, err)
	sg1, err := NewSharedGeometry(mustSphere(t, 6, 4, color.RGBA{60, 60, 200, 255}))
	check(t, err)
	const count = 1000
	nodes := make([]NodeID, count)
	origRevs := make([][2]uint64, count)
	for i := range count {
		nid, err := many.Add(Root, string(rune('a'+i%26))+itoaTest(i))
		check(t, err)
		if i%2 == 0 {
			check(t, many.SetLOD(nid, []LODLevel{{MaxDistance: 10, Shared: sg0}, {Shared: sg1}}))
		} else {
			check(t, many.SetLOD(nid, []LODLevel{{MaxDistance: 10, Geometry: t0}, {Geometry: t1}}))
		}
		nodes[i] = nid
		origRevs[i] = [2]uint64{many.nodes[nid].lod[0].geometryRevision, many.nodes[nid].lod[1].geometryRevision}
	}
	fBefore, err := many.Frame(camAt(15), 64, 64)
	check(t, err)
	check(t, many.SetLODBias(2))
	fAfter, err := many.Frame(camAt(15), 64, 64)
	check(t, err)
	if fAfter.Revision != fBefore.Revision || fAfter.TextureRevision != fBefore.TextureRevision {
		t.Fatalf("SetLODBias bumped mesh/texture revision: (%d,%d) -> (%d,%d)",
			fBefore.Revision, fBefore.TextureRevision, fAfter.Revision, fAfter.TextureRevision)
	}
	for i, nid := range nodes {
		got := [2]uint64{many.nodes[nid].lod[0].geometryRevision, many.nodes[nid].lod[1].geometryRevision}
		if got != origRevs[i] {
			t.Fatalf("node %d level revisions changed: %v -> %v", i, origRevs[i], got)
		}
	}
}

func TestSceneLODHysteresisAndResets(t *testing.T) {
	s := NewScene()
	t0 := Geometry{Points: []Point{{Radius: 3, Color: color.RGBA{220, 40, 40, 255}}}}
	t1 := Geometry{Points: []Point{{Radius: 3, Color: color.RGBA{40, 200, 60, 255}}}}

	// Validation of SetLODHysteresis.
	for _, bad := range []float32{-0.001, 0.5001, -1, 2, float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))} {
		if err := s.SetLODHysteresis(bad); err == nil {
			t.Fatalf("SetLODHysteresis(%v) succeeded, want error", bad)
		}
	}
	check(t, s.SetLODHysteresis(0.5))
	check(t, s.SetLODHysteresis(0))
	check(t, s.SetLODHysteresis(0.1))
	if s.LODHysteresis() != 0.1 {
		t.Fatalf("LODHysteresis() = %v, want 0.1", s.LODHysteresis())
	}

	group, err := s.Add(Root, "group")
	check(t, err)
	id, err := s.Add(group, "item")
	check(t, err)
	levels := []LODLevel{
		{MaxDistance: 20, Geometry: t0},
		{MaxDistance: 40, Geometry: t1},
	}
	check(t, s.SetLOD(id, levels))

	camAt := func(x float32) Camera {
		c := DefaultCamera()
		c.Projection = Perspective
		return c.LookAt(nt3dmath.Vec3{X: x}, nt3dmath.Vec3{})
	}
	step := func(x float32) (tier int, visible bool) {
		f, err := s.Frame(camAt(x), 64, 64)
		check(t, err)
		f.Cull = false
		if !f.Instances[0].Visible {
			return -1, false
		}
		st := f.Stats()
		if st.LODTierInstances[0] == 1 {
			return 0, true
		}
		return 1, true
	}

	// First frame selects without hysteresis: starting at 21 (> 20) picks tier 1 immediately.
	if tier, vis := step(21); !vis || tier != 1 {
		t.Fatalf("first frame at 21: got tier=%d vis=%v, want 1, true", tier, vis)
	}

	// Reset via SetLOD, then sweep outward from 19 across boundary 20 (band 0.1 -> upper 22, lower 18).
	check(t, s.SetLOD(id, levels))
	for _, d := range []float32{19, 20, 21, 21.9, 22} {
		if tier, vis := step(d); !vis || tier != 0 {
			t.Fatalf("outward sweep at %g: got tier=%d vis=%v, want 0, true", d, tier, vis)
		}
	}
	if tier, vis := step(22.1); !vis || tier != 1 {
		t.Fatalf("outward switch at 22.1: got tier=%d vis=%v, want 1, true", tier, vis)
	}

	// Sweep back inward: stays on tier 1 down to 18, switches to tier 0 at < 18.
	for _, d := range []float32{21, 20, 19, 18.1, 18} {
		if tier, vis := step(d); !vis || tier != 1 {
			t.Fatalf("inward sweep at %g: got tier=%d vis=%v, want 1, true", d, tier, vis)
		}
	}
	if tier, vis := step(17.9); !vis || tier != 0 {
		t.Fatalf("inward switch at 17.9: got tier=%d vis=%v, want 0, true", tier, vis)
	}

	// Final level's hide-beyond cutoff at 40 uses the same 0.1 band (hides > 44, re-shows < 36).
	if tier, vis := step(39); !vis || tier != 1 {
		t.Fatalf("at 39: got tier=%d vis=%v, want 1, true", tier, vis)
	}
	for _, d := range []float32{40.5, 43.9, 44} {
		if tier, vis := step(d); !vis || tier != 1 {
			t.Fatalf("cutoff outward at %g: got tier=%d vis=%v, want 1, true", d, tier, vis)
		}
	}
	if _, vis := step(44.1); vis {
		t.Fatal("cutoff outward at 44.1: expected hidden")
	}
	for _, d := range []float32{42, 39, 36.1, 36} {
		if _, vis := step(d); vis {
			t.Fatalf("cutoff inward at %g: expected still hidden", d)
		}
	}
	if tier, vis := step(35.9); !vis || tier != 1 {
		t.Fatalf("cutoff inward at 35.9: got tier=%d vis=%v, want 1, true", tier, vis)
	}
	// Multi-tier jump straight from hidden (> 44.1) back to near (< 18).
	if _, vis := step(50); vis {
		t.Fatal("at 50: expected hidden")
	}
	if tier, vis := step(10); !vis || tier != 0 {
		t.Fatalf("jump 50 -> 10: got tier=%d vis=%v, want 0, true", tier, vis)
	}

	// Reset rules while held inside the hysteresis deadband at 21 (tier 0 held above 20):
	if tier, vis := step(21); !vis || tier != 0 {
		t.Fatalf("held at 21: got tier=%d vis=%v, want 0, true", tier, vis)
	}
	// 1. SetLOD resets hysteresis -> 21 selects tier 1.
	check(t, s.SetLOD(id, levels))
	if tier, vis := step(21); !vis || tier != 1 {
		t.Fatalf("after SetLOD at 21: got tier=%d vis=%v, want 1, true", tier, vis)
	}

	// 2. Direct SetVisible false -> true (without intermediate Frame) resets hysteresis -> 19 held on tier 1 becomes tier 0.
	if tier, vis := step(19); !vis || tier != 1 {
		t.Fatalf("held on tier 1 at 19: got tier=%d vis=%v, want 1, true", tier, vis)
	}
	check(t, s.SetVisible(id, false))
	check(t, s.SetVisible(id, true))
	if tier, vis := step(19); !vis || tier != 0 {
		t.Fatalf("after direct SetVisible false->true at 19: got tier=%d vis=%v, want 0, true", tier, vis)
	}

	// 3. Ancestor SetVisible false -> true (with intermediate hidden Frame) resets hysteresis -> 21 held on tier 0 becomes tier 1.
	if tier, vis := step(21); !vis || tier != 0 {
		t.Fatalf("held on tier 0 at 21: got tier=%d vis=%v, want 0, true", tier, vis)
	}
	check(t, s.SetVisible(group, false))
	if _, vis := step(21); vis {
		t.Fatal("expected hidden while parent group is hidden")
	}
	check(t, s.SetVisible(group, true))
	if tier, vis := step(21); !vis || tier != 1 {
		t.Fatalf("after group SetVisible false->true at 21: got tier=%d vis=%v, want 1, true", tier, vis)
	}

	// 4. SetLODBias change resets hysteresis so budget step takes effect immediately.
	// Return to 17 (tier 0), move to 19.5 (tier 0). Now step bias to 0.9: effective boundary becomes 18,
	// and 18*(1+0.1) = 19.8 would have held tier 0 if hysteresis were not reset; reset selects tier 1!
	if tier, vis := step(17); !vis || tier != 0 {
		t.Fatalf("at 17: got tier=%d vis=%v, want 0, true", tier, vis)
	}
	if tier, vis := step(19.5); !vis || tier != 0 {
		t.Fatalf("at 19.5: got tier=%d vis=%v, want 0, true", tier, vis)
	}
	check(t, s.SetLODBias(0.9))
	if tier, vis := step(19.5); !vis || tier != 1 {
		t.Fatalf("after SetLODBias(0.9) at 19.5: got tier=%d vis=%v, want 1, true (reset on bias change)", tier, vis)
	}

	// Band 0 reproduces exact stateless switch points in both directions (including exact boundary == 20).
	check(t, s.SetLODBias(1))
	check(t, s.SetLODHysteresis(0))
	if tier, vis := step(20.1); !vis || tier != 1 {
		t.Fatalf("band 0 at 20.1: got tier=%d vis=%v, want 1, true", tier, vis)
	}
	if tier, vis := step(20); !vis || tier != 0 {
		t.Fatalf("band 0 returning to exact 20: got tier=%d vis=%v, want 0, true", tier, vis)
	}
}

func TestSceneLODDefaultBiasAndBandByteIdentical(t *testing.T) {
	build := func() (*Scene, [3]int) {
		s := NewScene()
		m0 := mustSphere(t, 20, 10, color.RGBA{220, 60, 50, 255})
		m1 := mustSphere(t, 12, 6, color.RGBA{50, 190, 80, 255})
		m2 := mustSphere(t, 6, 4, color.RGBA{60, 90, 220, 255})
		for i, x := range []float32{-3, 0, 3.5} {
			id, err := s.Add(Root, "n"+itoaTest(i))
			check(t, err)
			check(t, s.SetTransform(id, nt3dmath.Translation(nt3dmath.Vec3{X: x, Y: float32(i) - 1})))
			check(t, s.SetLOD(id, []LODLevel{
				{MaxDistance: 8, Geometry: m0},
				{MaxDistance: 16, Geometry: m1},
				{MaxDistance: 28, Geometry: m2},
			}))
		}
		return s, [3]int{len(m0.Indices), len(m1.Indices), len(m2.Indices)}
	}

	untouched, wantIdx := build()
	restored, _ := build()
	check(t, restored.SetLODBias(0.75))
	check(t, restored.SetLODHysteresis(0.15))
	// Run a frame under non-default settings, then restore defaults (1, 0).
	c0 := DefaultCamera()
	c0.Projection = Perspective
	c0 = c0.LookAt(nt3dmath.Vec3{Z: 12}, nt3dmath.Vec3{})
	_, err := restored.Frame(c0, 64, 48)
	check(t, err)
	check(t, restored.SetLODBias(1))
	check(t, restored.SetLODHysteresis(0))

	for _, dist := range []float32{5, 9, 12, 17, 25, 35, 17, 9, 5} {
		cam := DefaultCamera()
		cam.Projection = Perspective
		cam = cam.LookAt(nt3dmath.Vec3{X: 1, Y: -dist, Z: 3}, nt3dmath.Vec3{})

		fa, err := untouched.Frame(cam, 72, 54)
		check(t, err)
		fb, err := restored.Frame(cam, 72, 54)
		check(t, err)

		// Pin against pure AABB-distance <= MaxDistance reference selection.
		thresholds := [3]float32{8, 16, 28}
		for i, id := range untouched.order {
			n := untouched.nodes[id]
			d := aabbDistance(n.lodBounds.Transformed(fa.Instances[i].World), fa.Eye)
			wantTier := 3
			for k, th := range thresholds {
				if d <= th {
					wantTier = k
					break
				}
			}
			wantVis := wantTier < 3
			if fa.Instances[i].Visible != wantVis || fb.Instances[i].Visible != wantVis {
				t.Fatalf("dist %g node %d: visible=(%v,%v), want %v", dist, i, fa.Instances[i].Visible, fb.Instances[i].Visible, wantVis)
			}
			if wantVis && (len(fa.Geometry[i].Indices) != wantIdx[wantTier] || len(fb.Geometry[i].Indices) != wantIdx[wantTier]) {
				t.Fatalf("dist %g node %d: indices=(%d,%d), want %d", dist, i, len(fa.Geometry[i].Indices), len(fb.Geometry[i].Indices), wantIdx[wantTier])
			}
		}

		imgA := softwareRender(fa).(*image.RGBA)
		imgB := softwareRender(fb).(*image.RGBA)
		if !bytes.Equal(imgA.Pix, imgB.Pix) {
			t.Fatalf("dist %g: default bias=1 band=0 frames are not byte-identical", dist)
		}
	}
}

func BenchmarkSceneSetLODBias(b *testing.B) {
	s := NewScene()
	meshHi, err := BuildSphere(Sphere{Radius: .5, Segments: 10, Rings: 6})
	if err != nil {
		b.Fatal(err)
	}
	meshLo, err := BuildSphere(Sphere{Radius: .5, Segments: 5, Rings: 3})
	if err != nil {
		b.Fatal(err)
	}
	sgHi, err := NewSharedGeometry(meshHi)
	if err != nil {
		b.Fatal(err)
	}
	sgLo, err := NewSharedGeometry(meshLo)
	if err != nil {
		b.Fatal(err)
	}
	levels := []LODLevel{{MaxDistance: 10, Shared: sgHi}, {Shared: sgLo}}
	for i := range 4000 {
		id, err := s.Add(Root, "t"+itoaTest(i))
		if err != nil {
			b.Fatal(err)
		}
		if err := s.SetLOD(id, levels); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		bias := float32(0.9)
		if i&1 == 1 {
			bias = 1.1
		}
		if err := s.SetLODBias(bias); err != nil {
			b.Fatal(err)
		}
	}
}

func mustSphere(t *testing.T, segs, rings int, c color.RGBA) Geometry {
	t.Helper()
	g, err := BuildSphere(Sphere{Radius: 1, Segments: segs, Rings: rings, Color: c})
	check(t, err)
	return g
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func TestSceneSetLODReferenceBounds(t *testing.T) {
	var (
		tier0 = color.RGBA{220, 40, 40, 255}
		tier1 = color.RGBA{40, 200, 60, 255}
	)
	box := func(min, size nt3dmath.Vec3, c color.RGBA) Geometry {
		return Geometry{Boxes: []Box{{Min: min, Size: size, Color: c}}}
	}
	// Part a is a small box at the origin; part b is a slab 100 m east, so
	// their own bounds give very different camera distances.
	aMin, aSize := nt3dmath.Vec3{X: -1, Y: -1, Z: -1}, nt3dmath.Vec3{X: 2, Y: 2, Z: 2}
	bMin, bSize := nt3dmath.Vec3{X: 100, Y: -1, Z: -1}, nt3dmath.Vec3{X: 100, Y: 2, Z: 2}
	levels := func(min, size nt3dmath.Vec3) []LODLevel {
		return []LODLevel{
			{MaxDistance: 20, Geometry: box(min, size, tier0)},
			{MaxDistance: 40, Geometry: box(min, size, tier1)},
		}
	}

	s := NewScene()
	chunk, err := s.Add(Root, "chunk")
	check(t, err)
	a, err := s.Add(chunk, "a")
	check(t, err)
	b, err := s.Add(chunk, "b")
	check(t, err)
	plain, err := s.Add(chunk, "plain")
	check(t, err)
	check(t, s.SetNodeGeometry(plain, box(aMin, aSize, tier0)))
	check(t, s.SetLOD(a, levels(aMin, aSize)))
	check(t, s.SetLOD(b, levels(bMin, bSize)))

	camAt := func(x float32) Camera {
		c := DefaultCamera()
		c.Projection = Perspective
		return c.LookAt(nt3dmath.Vec3{X: x}, nt3dmath.Vec3{})
	}
	// state reports the tier color drawn for id, or the zero color when hidden.
	state := func(x float32, id NodeID) color.RGBA {
		t.Helper()
		f, err := s.Frame(camAt(x), 64, 64)
		check(t, err)
		for i, in := range f.Instances {
			if in.Node == id {
				if !in.Visible {
					return color.RGBA{}
				}
				return f.Geometry[i].Boxes[0].Color
			}
		}
		t.Fatalf("node %d missing from frame", id)
		return color.RGBA{}
	}

	// Own bounds: at x=-10 a is 9 m away (tier 0) but b is 109 m away (hidden).
	if ca, cb := state(-10, a), state(-10, b); ca != tier0 || cb != (color.RGBA{}) {
		t.Fatalf("own bounds at -10: a=%v b=%v, want tier0 and hidden", ca, cb)
	}

	// Errors: no LOD, root, unknown node, invalid bounds; each leaves the node unchanged.
	var ref nt3dmath.AABB
	ref.Include(nt3dmath.Vec3{X: -1, Y: -1, Z: -1})
	ref.Include(nt3dmath.Vec3{X: 1, Y: 1, Z: 1})
	// Min and Max are exported, so a valid box can still be made non-finite.
	badNaN, badInf := ref, ref
	badNaN.Min.X = float32(math.NaN())
	badInf.Max.X = float32(math.Inf(1))
	for name, err := range map[string]error{
		"no LOD":    s.SetLODReferenceBounds(plain, ref),
		"root":      s.SetLODReferenceBounds(Root, ref),
		"unknown":   s.SetLODReferenceBounds(NodeID(9999), ref),
		"empty box": s.SetLODReferenceBounds(a, nt3dmath.AABB{}),
		"NaN":       s.SetLODReferenceBounds(a, badNaN),
		"Inf":       s.SetLODReferenceBounds(a, badInf),
	} {
		if err == nil {
			t.Errorf("%s: SetLODReferenceBounds succeeded, want error", name)
		}
	}
	if cb := state(-10, b); cb != (color.RGBA{}) {
		t.Fatalf("failed calls changed b: %v", cb)
	}

	// Shared reference bounds: both parts now switch together across a sweep,
	// although b's own bounds are 100 m further east.
	rev := s.Revision()
	check(t, s.SetLODReferenceBounds(a, ref))
	check(t, s.SetLODReferenceBounds(b, ref))
	if s.Revision() == rev {
		t.Fatal("SetLODReferenceBounds did not advance Revision")
	}
	for _, c := range []struct {
		x    float32 // eye X; distance to ref is |x|-1
		want color.RGBA
	}{
		{-5, tier0}, {-15, tier0}, {-20, tier0}, {-22, tier1}, {-35, tier1}, {-41, tier1}, {-42, color.RGBA{}}, {-60, color.RGBA{}}, {-30, tier1}, {-10, tier0},
	} {
		if ca, cb := state(c.x, a), state(c.x, b); ca != c.want || cb != c.want {
			t.Fatalf("shared bounds at x=%g: a=%v b=%v, want both %v", c.x, ca, cb, c.want)
		}
	}

	// Bounds are measured through the node's world transform: moving the
	// chunk 100 m west brings the reference box to x=-100, so the eye at the
	// origin is 99 m from it.
	check(t, s.SetTransform(chunk, nt3dmath.Translation(nt3dmath.Vec3{X: -100})))
	if ca, cb := state(0, a), state(0, b); ca != (color.RGBA{}) || cb != (color.RGBA{}) {
		t.Fatalf("translated chunk: a=%v b=%v, want both hidden", ca, cb)
	}
	check(t, s.SetTransform(chunk, nt3dmath.Identity()))

	// SetLOD clears the reference bounds: b is back to its own distance.
	check(t, s.SetLOD(b, levels(bMin, bSize)))
	if cb := state(-10, b); cb != (color.RGBA{}) {
		t.Fatalf("after SetLOD b=%v, want own bounds (hidden)", cb)
	}
}

func TestSceneSetLODReferenceBoundsResetsHysteresis(t *testing.T) {
	s := NewScene()
	check(t, s.SetLODHysteresis(0.1))
	id, err := s.Add(Root, "item")
	check(t, err)
	tier0, tier1 := color.RGBA{220, 40, 40, 255}, color.RGBA{40, 200, 60, 255}
	g := func(c color.RGBA) Geometry {
		return Geometry{Boxes: []Box{{Min: nt3dmath.Vec3{X: -1, Y: -1, Z: -1}, Size: nt3dmath.Vec3{X: 2, Y: 2, Z: 2}, Color: c}}}
	}
	check(t, s.SetLOD(id, []LODLevel{{MaxDistance: 20, Geometry: g(tier0)}, {MaxDistance: 40, Geometry: g(tier1)}}))
	var ref nt3dmath.AABB
	ref.Include(nt3dmath.Vec3{X: -1, Y: -1, Z: -1})
	ref.Include(nt3dmath.Vec3{X: 1, Y: 1, Z: 1})
	check(t, s.SetLODReferenceBounds(id, ref))

	at := func(dist float32) color.RGBA {
		t.Helper()
		c := DefaultCamera()
		c.Projection = Perspective
		f, err := s.Frame(c.LookAt(nt3dmath.Vec3{X: -(dist + 1)}, nt3dmath.Vec3{}), 64, 64)
		check(t, err)
		return f.Geometry[0].Boxes[0].Color
	}
	// Start inside the near tier, then step into the deadband (20, 22]: the
	// hysteresis memory keeps tier 0.
	if got := at(19); got != tier0 {
		t.Fatalf("at 19: %v, want tier0", got)
	}
	if got := at(21); got != tier0 {
		t.Fatalf("at 21 in the deadband: %v, want tier0 held by hysteresis", got)
	}
	// Setting reference bounds (even unchanged) clears the memory, so the same
	// distance now selects its tier without hysteresis.
	check(t, s.SetLODReferenceBounds(id, ref))
	if got := at(21); got != tier1 {
		t.Fatalf("at 21 after reset: %v, want tier1", got)
	}
}
