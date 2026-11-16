// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"fmt"
	"image/color"
	"slices"
	"testing"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"

	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

func addLODGridMesh(doc *gltf.Document, name string, cells int, matIdx int, variantExt gltf.Extensions) int {
	var pos [][3]float32
	var norm [][3]float32
	var idx []uint16
	step := float32(2.0) / float32(cells)
	for r := 0; r < cells; r++ {
		for c := 0; c < cells; c++ {
			x0 := -1.0 + float32(c)*step
			y0 := -1.0 + float32(r)*step
			x1 := x0 + step
			y1 := y0 + step
			base := uint16(len(pos))
			pos = append(pos,
				[3]float32{x0, y0, 0},
				[3]float32{x1, y0, 0},
				[3]float32{x1, y1, 0},
				[3]float32{x0, y1, 0},
			)
			norm = append(norm,
				[3]float32{0, 0, 1},
				[3]float32{0, 0, 1},
				[3]float32{0, 0, 1},
				[3]float32{0, 0, 1},
			)
			idx = append(idx, base, base+1, base+2, base, base+2, base+3)
		}
	}
	pAcc := modeler.WritePosition(doc, pos)
	nAcc := modeler.WriteNormal(doc, norm)
	iAcc := modeler.WriteIndices(doc, idx)
	prim := &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION: pAcc,
			gltf.NORMAL:   nAcc,
		},
		Indices:    gltf.Index(iAcc),
		Mode:       gltf.PrimitiveTriangles,
		Extensions: variantExt,
	}
	if matIdx >= 0 {
		prim.Material = gltf.Index(matIdx)
	}
	meshIdx := len(doc.Meshes)
	doc.Meshes = append(doc.Meshes, &gltf.Mesh{
		Name:       name,
		Primitives: []*gltf.Primitive{prim},
	})
	return meshIdx
}

func TestMultiLODGLBCityPropFleetAndVariants(t *testing.T) {
	doc := &gltf.Document{
		Asset:          gltf.Asset{Version: "2.0"},
		ExtensionsUsed: []string{"KHR_materials_variants"},
		Extensions: gltf.Extensions{
			"KHR_materials_variants": rawJSON(t, map[string]any{
				"variants": []map[string]any{
					{"name": "MunicipalGreen"},
					{"name": "SafetyYellow"},
				},
			}),
		},
		Materials: []*gltf.Material{
			{
				Name: "DefaultGray",
				PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
					BaseColorFactor: &[4]float64{0.6, 0.62, 0.65, 1},
					MetallicFactor:  gltf.Float(0.2),
					RoughnessFactor: gltf.Float(0.6),
				},
			},
			{
				Name: "MunicipalGreen",
				PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
					BaseColorFactor: &[4]float64{0.1, 0.55, 0.22, 1},
					MetallicFactor:  gltf.Float(0.1),
					RoughnessFactor: gltf.Float(0.45),
				},
			},
			{
				Name: "SafetyYellow",
				PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
					BaseColorFactor: &[4]float64{0.95, 0.75, 0.05, 1},
					MetallicFactor:  gltf.Float(0.05),
					RoughnessFactor: gltf.Float(0.35),
				},
			},
		},
	}

	varMappings := gltf.Extensions{
		"KHR_materials_variants": rawJSON(t, map[string]any{
			"mappings": []map[string]any{
				{"material": 1, "variants": []int{0}},
				{"material": 2, "variants": []int{1}},
			},
		}),
	}

	// LOD0: 4x4 grid = 32 triangles
	// LOD1: 2x2 grid = 8 triangles
	// LOD2: 1x1 grid = 2 triangles
	m0 := addLODGridMesh(doc, "Streetlight_LOD0", 4, 0, varMappings)
	m1 := addLODGridMesh(doc, "Streetlight_LOD1", 2, 0, varMappings)
	m2 := addLODGridMesh(doc, "Streetlight_LOD2", 1, 0, varMappings)

	doc.Nodes = []*gltf.Node{
		{Name: "SM_Streetlight_LOD0", Mesh: gltf.Index(m0), Translation: [3]float64{0, 0, 0}},
		{Name: "SM_Streetlight_LOD1", Mesh: gltf.Index(m1), Translation: [3]float64{4, 0, 0}}, // showcase offset!
		{Name: "SM_Streetlight_LOD2", Mesh: gltf.Index(m2), Translation: [3]float64{8, 0, 0}}, // showcase offset!
	}
	doc.Scenes = []*gltf.Scene{{Nodes: []int{0, 1, 2}}}
	doc.Scene = gltf.Index(0)

	model, err := Decode(glb(t, doc), nil)
	check(t, err)

	// Compile reusable LOD SceneAsset (LOD0 <= 25m, LOD1 <= 75m, LOD2 <= 180m, culled > 180m).
	lodAsset, err := model.LODAsset(25, 75, 180)
	check(t, err)

	scene := nt3d.NewScene()
	check(t, scene.SetLODHysteresis(0.05))

	// Place 60 streetlights spaced 4m apart along +Y (world Y = 0, +4, ..., +236),
	// facing a camera at Y = -10 looking toward +Y (glTF +Z turns to -Y in Z-up, so front faces -Y!).
	const count = 60
	roots := make([]nt3d.NodeID, count)
	for i := 0; i < count; i++ {
		inst, err := lodAsset.Instantiate(scene, nt3d.Root, fmt.Sprintf("lamp-%02d", i))
		check(t, err)
		roots[i] = inst.Root
		check(t, scene.SetTransform(inst.Root, nt3dmath.Translation(nt3dmath.Vec3{Y: float32(i * 4)})))
		if i%2 == 1 {
			check(t, scene.SetMaterialVariant(inst.Root, "SafetyYellow"))
		} else {
			check(t, scene.SetMaterialVariant(inst.Root, "MunicipalGreen"))
		}
	}

	// Also verify one-liner Model.AddLODTo on a separate scene.
	singleScene := nt3d.NewScene()
	rootID, err := model.AddLODTo(singleScene, nt3d.Root, "hero-lamp", 25, 75)
	check(t, err)
	if !slices.Equal(singleScene.MaterialVariants(rootID), []string{"MunicipalGreen", "SafetyYellow"}) {
		t.Fatalf("unexpected variants from AddLODTo: %v", singleScene.MaterialVariants(rootID))
	}

	// Orbit camera with Target=(0,0,0), Beta=0, Alpha=0, Distance=10 -> Eye=(0, -10, 0) looking +Y!
	// Distances to prop planes at Y = 4*i:
	// - LOD0 (<= 25m): i = 0..3 (Y = 0..12 -> dist = 10..22) -> 4 lamps * 32 tris = 128 tris
	// - LOD1 (25..75m): i = 4..16 (Y = 16..64 -> dist = 26..74) -> 13 lamps * 8 tris = 104 tris
	// - LOD2 (75..180m): i = 17..42 (Y = 68..168 -> dist = 78..178) -> 26 lamps * 2 tris = 52 tris
	// - Culled (> 180m): i = 43..59 (Y = 172..236 -> dist = 182..246) -> 17 lamps hidden!
	cam := nt3d.Camera{
		Target:     nt3dmath.Vec3{},
		Distance:   10,
		Beta:       -90, // Eye=(0, -10, 0) looking down +Y avenue
		Near:       0.5,
		Far:        400,
		Projection: nt3d.Perspective,
	}
	frame, err := scene.Frame(cam, 240, 160)
	check(t, err)
	st := frame.Stats()
	if st.LODTierInstances[0] != 4 || st.LODTierInstances[1] != 13 || st.LODTierInstances[2] != 26 {
		t.Fatalf("unexpected LOD tier distribution: %v (drawn=%d)", st.LODTierInstances[:4], st.DrawnInstances)
	}
	wantTris := 4*32 + 13*8 + 26*2
	if st.Triangles != wantTris {
		t.Fatalf("got %d triangles across 60 streetlights, want %d (vs %d without LOD)", st.Triangles, wantTris, count*32)
	}

	// Every drawn instance must carry a non-zero SharedGeometry ID, with at most 3 distinct Shared IDs (one per tier!).
	sharedIDs := map[uint64]bool{}
	for i, inst := range frame.Instances {
		if !inst.Visible {
			continue
		}
		if frame.Shared[i] == 0 {
			t.Fatalf("instance %d (%s) lost SharedGeometry instancing", i, inst.Name)
		}
		sharedIDs[frame.Shared[i]] = true
	}
	if len(sharedIDs) != 3 {
		t.Fatalf("expected exactly 3 distinct SharedGeometry buffers across 43 visible streetlights, got %d", len(sharedIDs))
	}

	// Render in software and pick center pixel (hits lamp-00 in LOD0).
	sw := nt3d.NewSoftwareRenderer()
	img, err := sw.Render(frame)
	check(t, err)
	centerPx := color.RGBAModel.Convert(img.At(120, 80)).(color.RGBA)
	if centerPx.G <= centerPx.R {
		t.Fatalf("expected MunicipalGreen lamp-00 at center pixel, got %v", centerPx)
	}
	hit := scene.Pick(cam, frame, 120, 80, 4)
	if hit == nil {
		t.Fatal("expected center ray to pick lamp-00")
	}
	if _, ok := scene.Node(roots[0]); !ok {
		t.Fatal("expected roots[0] node in scene")
	}
	if hit.Position.Y < -0.1 || hit.Position.Y > 0.1 {
		t.Fatalf("expected pick hit on lamp-00 at Y≈0, got %+v", hit.Position)
	}
}

func TestCombineLODSeparateGLBAndOBJFiles(t *testing.T) {
	makeSingleTierGLB := func(cells int, variantName string, col [4]float64) *nt3dmesh.Model {
		doc := &gltf.Document{
			Asset:          gltf.Asset{Version: "2.0"},
			ExtensionsUsed: []string{"KHR_materials_variants"},
			Extensions: gltf.Extensions{
				"KHR_materials_variants": rawJSON(t, map[string]any{
					"variants": []map[string]any{{"name": variantName}},
				}),
			},
			Materials: []*gltf.Material{
				{
					Name: "Base",
					PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
						BaseColorFactor: &[4]float64{0.8, 0.8, 0.8, 1},
					},
				},
				{
					Name: variantName,
					PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
						BaseColorFactor: &col,
					},
				},
			},
		}
		ext := gltf.Extensions{
			"KHR_materials_variants": rawJSON(t, map[string]any{
				"mappings": []map[string]any{{"material": 1, "variants": []int{0}}},
			}),
		}
		mIdx := addLODGridMesh(doc, "Prop", cells, 0, ext)
		doc.Nodes = []*gltf.Node{{Name: "Prop", Mesh: gltf.Index(mIdx)}}
		doc.Scenes = []*gltf.Scene{{Nodes: []int{0}}}
		doc.Scene = gltf.Index(0)
		m, err := Decode(glb(t, doc), nil)
		check(t, err)
		return m
	}

	glbLOD0 := makeSingleTierGLB(3, "Taxi", [4]float64{0.95, 0.75, 0.05, 1}) // 18 tris
	glbLOD1 := makeSingleTierGLB(2, "Taxi", [4]float64{0.95, 0.75, 0.05, 1}) // 8 tris

	// Tier 2: plain OBJ single quad (2 tris) in Z-up coordinates!
	const objSrc = `o Prop_LOD2
v -1 0 -1
v  1 0 -1
v  1 0  1
v -1 0  1
f 1 2 3 4
`
	objLOD2, err := nt3dmesh.DecodeOBJ([]byte(objSrc), &nt3dmesh.Options{Color: color.RGBA{180, 180, 180, 255}})
	check(t, err)

	s := nt3d.NewScene()
	rootID, err := nt3dmesh.AddLODModels(s, nt3d.Root, "mixed-prop", []nt3dmesh.ModelLOD{
		{MaxDistance: 15, Model: glbLOD0},
		{MaxDistance: 45, Model: glbLOD1},
		{MaxDistance: 0, Model: objLOD2},
	})
	check(t, err)
	check(t, s.SetMaterialVariant(rootID, "Taxi"))

	camAt := func(dist float64) nt3d.Camera {
		return nt3d.Camera{
			Target:     nt3dmath.Vec3{},
			Distance:   dist,
			Beta:       -90,
			Near:       0.1,
			Far:        300,
			Projection: nt3d.Perspective,
		}
	}

	f0, err := s.Frame(camAt(10), 120, 90)
	check(t, err)
	if f0.Stats().Triangles != 18 {
		t.Fatalf("expected 18 tris from GLB LOD0, got %d", f0.Stats().Triangles)
	}

	f1, err := s.Frame(camAt(30), 120, 90)
	check(t, err)
	if f1.Stats().Triangles != 8 {
		t.Fatalf("expected 8 tris from GLB LOD1, got %d", f1.Stats().Triangles)
	}

	f2, err := s.Frame(camAt(80), 120, 90)
	check(t, err)
	if f2.Stats().Triangles != 2 || f2.Shared[0] == 0 {
		t.Fatalf("expected 2 shared tris from OBJ LOD2, got tris=%d shared=%d", f2.Stats().Triangles, f2.Shared[0])
	}
}
