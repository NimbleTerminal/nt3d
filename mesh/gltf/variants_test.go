// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"image/color"
	"slices"
	"strings"
	"testing"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"

	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func TestKHRMaterialsVariantsVehicleFleet(t *testing.T) {
	doc := &gltf.Document{
		Asset: gltf.Asset{Version: "2.0"},
		ExtensionsUsed: []string{
			"KHR_materials_variants",
			"KHR_materials_clearcoat",
			"KHR_materials_emissive_strength",
			"EXT_texture_webp",
		},
		ExtensionsRequired: []string{"KHR_materials_variants"},
		Extensions: gltf.Extensions{
			"KHR_materials_variants": rawJSON(t, map[string]any{
				"variants": []map[string]any{
					{"name": "Taxi"},
					{"name": "Police"},
					{"name": "Red"},
					{"name": "Weathered"},
				},
			}),
		},
	}

	policeImg := writeWebPImage(t, doc, "police-livery", color.NRGBA{30, 80, 235, 255})
	weatheredImg := writeWebPImage(t, doc, "weathered-albedo", color.NRGBA{165, 95, 55, 255})
	normImg := writeWebPImage(t, doc, "weathered-normal", color.NRGBA{128, 128, 255, 255})
	ormImg := writeWebPImage(t, doc, "weathered-orm", color.NRGBA{200, 210, 40, 255})

	webpTex := func(src int) *gltf.Texture {
		return &gltf.Texture{
			Extensions: gltf.Extensions{"EXT_texture_webp": rawJSON(t, map[string]any{"source": src})},
		}
	}
	doc.Textures = []*gltf.Texture{
		webpTex(policeImg),
		webpTex(weatheredImg),
		webpTex(normImg),
		webpTex(ormImg),
	}

	// Materials:
	// 0: Default white body paint (untextured)
	// 1: Taxi yellow clearcoat (untextured)
	// 2: Police blue livery texture
	// 3: Red metallic paint (untextured)
	// 4: Weathered rust albedo + ORM + normal map (over untextured default!)
	// 5: Black matte tire rubber (unmapped primitive)
	// 6: Hidden accessory (MASK alpha 0 < cutoff, untextured)
	// 7: Police active blue emissive lightbar
	doc.Materials = []*gltf.Material{
		{
			Name: "DefaultWhite",
			PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
				BaseColorFactor: &[4]float64{1, 1, 1, 1},
				MetallicFactor:  gltf.Float(0),
				RoughnessFactor: gltf.Float(0.25),
			},
		},
		{
			Name: "TaxiYellow",
			PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
				BaseColorFactor: &[4]float64{0.92, 0.68, 0.02, 1},
				MetallicFactor:  gltf.Float(0),
				RoughnessFactor: gltf.Float(0.2),
			},
			Extensions: gltf.Extensions{
				"KHR_materials_clearcoat": rawJSON(t, map[string]any{
					"clearcoatFactor":          1.0,
					"clearcoatRoughnessFactor": 0.05,
				}),
			},
		},
		{
			Name: "PoliceLivery",
			PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
				BaseColorTexture: &gltf.TextureInfo{Index: 0},
				MetallicFactor:   gltf.Float(0.1),
				RoughnessFactor:  gltf.Float(0.3),
			},
		},
		{
			Name: "RedMetallic",
			PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
				BaseColorFactor: &[4]float64{0.85, 0.03, 0.04, 1},
				MetallicFactor:  gltf.Float(0.8),
				RoughnessFactor: gltf.Float(0.22),
			},
		},
		{
			Name: "WeatheredRust",
			PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
				BaseColorTexture:         &gltf.TextureInfo{Index: 1},
				MetallicRoughnessTexture: &gltf.TextureInfo{Index: 3},
				MetallicFactor:           gltf.Float(1),
				RoughnessFactor:          gltf.Float(1),
			},
			NormalTexture:    &gltf.NormalTexture{Index: gltf.Index(2), Scale: gltf.Float(0.8)},
			OcclusionTexture: &gltf.OcclusionTexture{Index: gltf.Index(3), Strength: gltf.Float(0.9)},
		},
		{
			Name: "TireRubber",
			PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
				BaseColorFactor: &[4]float64{0.04, 0.04, 0.04, 1},
				MetallicFactor:  gltf.Float(0),
				RoughnessFactor: gltf.Float(0.92),
			},
		},
		{
			Name:        "HiddenLightbar",
			AlphaMode:   gltf.AlphaMask,
			AlphaCutoff: gltf.Float(0.5),
			PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
				BaseColorFactor: &[4]float64{1, 1, 1, 0},
			},
		},
		{
			Name: "PoliceLightbarOn",
			PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
				BaseColorFactor: &[4]float64{0.1, 0.4, 1, 1},
				MetallicFactor:  gltf.Float(0),
				RoughnessFactor: gltf.Float(0.15),
			},
			EmissiveFactor: [3]float64{0.2, 0.5, 1.0},
			Extensions: gltf.Extensions{
				"KHR_materials_emissive_strength": rawJSON(t, map[string]any{"emissiveStrength": 3.0}),
			},
		},
	}

	posAcc := modeler.WritePosition(doc, [][3]float32{
		{-0.5, 0, -0.5},
		{0.5, 0, -0.5},
		{0.5, 0, 0.5},
		{-0.5, 0, 0.5},
	})
	nrmAcc := modeler.WriteNormal(doc, [][3]float32{
		{0, 1, 0},
		{0, 1, 0},
		{0, 1, 0},
		{0, 1, 0},
	})
	uvAcc := modeler.WriteTextureCoord(doc, [][2]float32{
		{0, 0},
		{1, 0},
		{1, 1},
		{0, 1},
	})
	idxAcc := modeler.WriteIndices(doc, []uint16{0, 2, 1, 0, 3, 2})

	attrs := gltf.PrimitiveAttributes{
		gltf.POSITION:   posAcc,
		gltf.NORMAL:     nrmAcc,
		gltf.TEXCOORD_0: uvAcc,
	}

	doc.Meshes = []*gltf.Mesh{{
		Name: "Sedan",
		Primitives: []*gltf.Primitive{
			{
				Attributes: attrs,
				Indices:    gltf.Index(idxAcc),
				Material:   gltf.Index(0),
				Extensions: gltf.Extensions{
					"KHR_materials_variants": rawJSON(t, map[string]any{
						"mappings": []map[string]any{
							{"material": 1, "variants": []int{0}},
							{"material": 2, "variants": []int{1}},
							{"material": 3, "variants": []int{2}},
							{"material": 4, "variants": []int{3}},
						},
					}),
				},
			},
			{
				Attributes: attrs,
				Indices:    gltf.Index(idxAcc),
				Material:   gltf.Index(5), // Unmapped tires stay black rubber across all variants.
			},
			{
				Attributes: attrs,
				Indices:    gltf.Index(idxAcc),
				Material:   gltf.Index(6), // Hidden by default, visible only on Police (variant 1).
				Extensions: gltf.Extensions{
					"KHR_materials_variants": rawJSON(t, map[string]any{
						"mappings": []map[string]any{
							{"material": 7, "variants": []int{1}},
						},
					}),
				},
			},
		},
	}}
	doc.Nodes = []*gltf.Node{{Name: "Car", Mesh: gltf.Index(0)}}
	doc.Scenes = []*gltf.Scene{{Nodes: []int{0}}}
	doc.Scene = gltf.Index(0)

	m, err := Decode(glb(t, doc), nil)
	check(t, err)
	if len(m.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", m.Warnings)
	}
	wantVars := []string{"Taxi", "Police", "Red", "Weathered"}
	if !slices.Equal(m.Variants(), wantVars) {
		t.Fatalf("got Model.Variants()=%v, want %v", m.Variants(), wantVars)
	}

	// Instantiate 5 cars in one scene: default + each of the 4 colorways.
	s := nt3d.NewScene()
	cars := make([]*nt3d.AnimatedInstance, 5)
	for i := range cars {
		inst, err := m.Asset.Instantiate(s, nt3d.Root, "car-"+string(rune('0'+i)))
		check(t, err)
		cars[i] = inst
		check(t, s.SetTransform(inst.Root, nt3dmath.Translation(nt3dmath.Vec3{X: float32(i) * 1.5})))
	}
	check(t, s.SetMaterialVariant(cars[1].Root, "Taxi"))
	check(t, s.SetMaterialVariant(cars[2].Root, "Police"))
	check(t, s.SetMaterialVariant(cars[3].Root, "Red"))
	check(t, s.SetMaterialVariant(cars[4].Root, "Weathered"))

	cam := nt3d.DefaultCamera()
	cam.Distance = 10
	f, err := s.Frame(cam, 128, 64)
	check(t, err)

	// Verify untextured Body instances (default, Taxi, Red) share the exact same SharedGeometry ID,
	// Weathered has NormalMap + ORMMap + pre-populated UVs & Tangents, Tires stay untouched across all 5,
	// and Lightbar is opaque emissive ONLY on Police and invisible (Transparent alpha 0) on the rest.
	var bodySharedID uint64
	tireCount := 0
	for idx, inst := range f.Instances {
		mat := f.Geometry[idx].Material
		switch {
		case strings.HasPrefix(inst.Name, "car-0/") && strings.HasSuffix(inst.Name, ":Car"):
			// Each Car has 3 primitives in order: Body (node index 2), Tires (3), Lightbar (4).
		}
		switch inst.Name {
		case "car-0/2:Car": // Default white body
			if f.Shared[idx] == 0 || mat == nil || mat.BaseColor != (color.RGBA{}) {
				t.Fatalf("default body unexpected: shared=%d mat=%+v", f.Shared[idx], mat)
			}
			bodySharedID = f.Shared[idx]
			if len(f.Geometry[idx].UV) == 0 || len(f.Geometry[idx].Tangents) == 0 {
				t.Fatal("base SharedGeometry did not pre-populate UVs and Tangents for textured/normal-mapped variants")
			}
		case "car-1/2:Car": // Taxi body
			if f.Shared[idx] != bodySharedID || mat.Clearcoat != 1 || mat.BaseColor.R < 220 || mat.BaseColor.G < 180 || mat.BaseColor.B > 80 {
				t.Fatalf("Taxi body unexpected: shared=%d (want %d) mat=%+v", f.Shared[idx], bodySharedID, mat)
			}
		case "car-2/2:Car": // Police body (textured)
			if mat.Texture == nil || mat.Tint {
				t.Fatalf("Police body unexpected: mat=%+v", mat)
			}
		case "car-3/2:Car": // Red metallic body
			if f.Shared[idx] != bodySharedID || mat.Metallic != .8 || mat.BaseColor.R < 200 || mat.BaseColor.G > 80 {
				t.Fatalf("Red body unexpected: shared=%d (want %d) mat=%+v", f.Shared[idx], bodySharedID, mat)
			}
		case "car-4/2:Car": // Weathered body (Texture + NormalMap + ORMMap)
			if mat.Texture == nil || mat.NormalMap == nil || mat.ORMMap == nil || len(f.Geometry[idx].Tangents) == 0 {
				t.Fatalf("Weathered body missing maps or tangents: mat=%+v tangents=%d", mat, len(f.Geometry[idx].Tangents))
			}
		case "car-0/3:Car", "car-1/3:Car", "car-2/3:Car", "car-3/3:Car", "car-4/3:Car": // Unmapped Tires
			tireCount++
			if f.Shared[idx] == 0 || mat == nil || mat.Roughness != .92 {
				t.Fatalf("unmapped tire altered on %s: shared=%d mat=%+v", inst.Name, f.Shared[idx], mat)
			}
		case "car-2/4:Car": // Police active lightbar
			if mat.Transparent || mat.EmissiveIntensity != 3 {
				t.Fatalf("Police lightbar did not activate: %+v", mat)
			}
		case "car-1/4:Car", "car-3/4:Car": // Hidden lightbars on Taxi & Red
			if !mat.Transparent || mat.BaseColor.A != 0 {
				t.Fatalf("hidden lightbar became visible on %s: %+v", inst.Name, mat)
			}
		}
	}
	if tireCount != 5 {
		t.Fatalf("expected 5 tire instances, got %d", tireCount)
	}

	// Render a single car top-down through SoftwareRenderer across colorways and verify distinct colors.
	sr := nt3d.NewSoftwareRenderer()
	sSingle := nt3d.NewScene()
	solo, err := m.Asset.Instantiate(sSingle, nt3d.Root, "solo")
	check(t, err)
	// Hide Tires so we sample Body directly from above (+Z in nt3d = +Y in glTF).
	tiresNode, ok := sSingle.Find("solo/3:Car")
	if !ok {
		t.Fatal("missing tires node")
	}
	check(t, sSingle.SetVisible(tiresNode, false))

	topCam := nt3d.DefaultCamera()
	topCam.Alpha, topCam.Beta, topCam.Distance = 90, 89.9, 1.8
	sampleCenter := func(variant string) color.RGBA {
		check(t, sSingle.SetMaterialVariant(solo.Root, variant))
		fr, err := sSingle.Frame(topCam, 48, 48)
		check(t, err)
		img, err := sr.Render(fr)
		check(t, err)
		return color.RGBAModel.Convert(img.At(24, 24)).(color.RGBA)
	}

	cDefault := sampleCenter("")
	cTaxi := sampleCenter("Taxi")
	cPolice := sampleCenter("Police")
	cRed := sampleCenter("Red")
	cReset := sampleCenter("")

	if cTaxi.R <= cTaxi.B+80 || cTaxi.G <= cTaxi.B+60 {
		t.Fatalf("Taxi colorway did not render yellow: %v", cTaxi)
	}
	if cPolice.B <= cPolice.R+80 {
		t.Fatalf("Police colorway did not render blue: %v", cPolice)
	}
	if cRed.R <= cRed.G+60 || cRed.R <= cRed.B+60 {
		t.Fatalf("Red colorway did not render red: %v", cRed)
	}
	if cReset != cDefault {
		t.Fatalf("resetting variant to \"\" produced %v, want %v", cReset, cDefault)
	}
}

func TestKHRMaterialsVariantsMorphAndMirroredNodes(t *testing.T) {
	makeMaterialsAndExt := func(doc *gltf.Document) gltf.Extensions {
		doc.ExtensionsUsed = []string{"KHR_materials_variants"}
		doc.Extensions = gltf.Extensions{
			"KHR_materials_variants": rawJSON(t, map[string]any{
				"variants": []map[string]any{
					{"name": "Gold"},
					{"name": "Cobalt"},
				},
			}),
		}
		doc.Materials = []*gltf.Material{
			{
				Name: "Silver",
				PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
					BaseColorFactor: &[4]float64{0.9, 0.9, 0.92, 1},
					MetallicFactor:  gltf.Float(0.9),
					RoughnessFactor: gltf.Float(0.25),
				},
			},
			{
				Name: "Gold",
				PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
					BaseColorFactor: &[4]float64{1.0, 0.78, 0.18, 1},
					MetallicFactor:  gltf.Float(1.0),
					RoughnessFactor: gltf.Float(0.2),
				},
			},
			{
				Name: "Cobalt",
				PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
					BaseColorFactor: &[4]float64{0.08, 0.25, 0.9, 1},
					MetallicFactor:  gltf.Float(0.6),
					RoughnessFactor: gltf.Float(0.35),
				},
			},
		}
		return gltf.Extensions{
			"KHR_materials_variants": rawJSON(t, map[string]any{
				"mappings": []map[string]any{
					{"material": 1, "variants": []int{0}},
					{"material": 2, "variants": []int{1}},
				},
			}),
		}
	}

	// 1. Deformed morph-target mesh with KHR_materials_variants.
	morphDoc := &gltf.Document{Asset: gltf.Asset{Version: "2.0"}}
	morphExt := makeMaterialsAndExt(morphDoc)
	posAcc := modeler.WritePosition(morphDoc, [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}})
	deltaAcc := modeler.WritePosition(morphDoc, [][3]float32{{0, 0, 0.5}, {0, 0, 0.5}, {0, 0, 0.5}})
	idxAcc := modeler.WriteIndices(morphDoc, []uint16{0, 1, 2})
	morphDoc.Meshes = []*gltf.Mesh{{
		Name: "MorphedMesh",
		Primitives: []*gltf.Primitive{{
			Attributes: gltf.PrimitiveAttributes{gltf.POSITION: posAcc},
			Indices:    gltf.Index(idxAcc),
			Material:   gltf.Index(0),
			Targets:    []gltf.PrimitiveAttributes{{gltf.POSITION: deltaAcc}},
			Extensions: morphExt,
		}},
		Weights: []float64{1.0},
	}}
	morphDoc.Nodes = []*gltf.Node{{Name: "MorphNode", Mesh: gltf.Index(0)}}
	morphDoc.Scenes = []*gltf.Scene{{Nodes: []int{0}}}
	morphDoc.Scene = gltf.Index(0)

	mMorph, err := Decode(glb(t, morphDoc), nil)
	check(t, err)
	sMorph := nt3d.NewScene()
	rootMorph, err := mMorph.AddTo(sMorph, nt3d.Root, "morph-rig")
	check(t, err)
	check(t, sMorph.SetMaterialVariant(rootMorph, "Cobalt"))
	fMorph, err := sMorph.Frame(nt3d.DefaultCamera(), 64, 64)
	check(t, err)
	if len(fMorph.Geometry) != 1 || fMorph.Geometry[0].Material == nil || fMorph.Geometry[0].Material.BaseColor.B < 200 {
		t.Fatalf("morphed mesh did not receive Cobalt variant: %+v", fMorph.Geometry)
	}

	// 2. Static mirrored node (negative-determinant scale folded by staticReflections) with KHR_materials_variants.
	mirrorDoc := &gltf.Document{Asset: gltf.Asset{Version: "2.0"}}
	mirrorExt := makeMaterialsAndExt(mirrorDoc)
	mPos := modeler.WritePosition(mirrorDoc, [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}})
	mIdx := modeler.WriteIndices(mirrorDoc, []uint16{0, 1, 2})
	mirrorDoc.Meshes = []*gltf.Mesh{{
		Name: "StaticMirroredMesh",
		Primitives: []*gltf.Primitive{{
			Attributes: gltf.PrimitiveAttributes{gltf.POSITION: mPos},
			Indices:    gltf.Index(mIdx),
			Material:   gltf.Index(0),
			Extensions: mirrorExt,
		}},
	}}
	mirrorDoc.Nodes = []*gltf.Node{
		{Name: "LeftDoor", Mesh: gltf.Index(0)},
		{Name: "RightDoor", Mesh: gltf.Index(0), Scale: [3]float64{-1, 1, 1}},
	}
	mirrorDoc.Scenes = []*gltf.Scene{{Nodes: []int{0, 1}}}
	mirrorDoc.Scene = gltf.Index(0)

	mMirror, err := Decode(glb(t, mirrorDoc), nil)
	check(t, err)
	sMirror := nt3d.NewScene()
	rootMirror, err := mMirror.AddTo(sMirror, nt3d.Root, "car-doors")
	check(t, err)
	check(t, sMirror.SetMaterialVariant(rootMirror, "Gold"))
	fMirror, err := sMirror.Frame(nt3d.DefaultCamera(), 64, 64)
	check(t, err)
	if len(fMirror.Geometry) != 2 {
		t.Fatalf("expected 2 door instances, got %d", len(fMirror.Geometry))
	}
	for i, g := range fMirror.Geometry {
		if fMirror.Shared[i] == 0 || g.Material == nil || g.Material.BaseColor.R < 240 || g.Material.BaseColor.B > 140 {
			t.Fatalf("door %d (%s) did not keep SharedGeometry + Gold variant: shared=%d mat=%+v", i, fMirror.Instances[i].Name, fMirror.Shared[i], g.Material)
		}
	}
}

func TestKHRMaterialsVariantsValidation(t *testing.T) {
	baseValid := func() *gltf.Document {
		doc := gltfDoc(upTriangle)
		doc.ExtensionsUsed = []string{"KHR_materials_variants"}
		doc.Materials = []*gltf.Material{
			{Name: "M0"},
			{Name: "M1"},
		}
		doc.Extensions = gltf.Extensions{
			"KHR_materials_variants": rawJSON(t, map[string]any{
				"variants": []map[string]any{{"name": "V0"}, {"name": "V1"}},
			}),
		}
		doc.Meshes[0].Primitives[0].Material = gltf.Index(0)
		doc.Meshes[0].Primitives[0].Extensions = gltf.Extensions{
			"KHR_materials_variants": rawJSON(t, map[string]any{
				"mappings": []map[string]any{
					{"material": 1, "variants": []int{0}},
				},
			}),
		}
		return doc
	}

	badCases := []struct {
		name   string
		mutate func(doc *gltf.Document)
	}{
		{
			name: "empty-root-variants",
			mutate: func(doc *gltf.Document) {
				doc.Meshes[0].Primitives[0].Extensions = nil
				doc.Extensions["KHR_materials_variants"] = rawJSON(t, map[string]any{"variants": []any{}})
			},
		},
		{
			name: "empty-variant-name",
			mutate: func(doc *gltf.Document) {
				doc.Extensions["KHR_materials_variants"] = rawJSON(t, map[string]any{
					"variants": []map[string]any{{"name": ""}},
				})
			},
		},
		{
			name: "duplicate-variant-name",
			mutate: func(doc *gltf.Document) {
				doc.Extensions["KHR_materials_variants"] = rawJSON(t, map[string]any{
					"variants": []map[string]any{{"name": "Same"}, {"name": "Same"}},
				})
			},
		},
		{
			name: "primitive-without-root-extension",
			mutate: func(doc *gltf.Document) {
				doc.Extensions = nil
			},
		},
		{
			name: "empty-mappings",
			mutate: func(doc *gltf.Document) {
				doc.Meshes[0].Primitives[0].Extensions["KHR_materials_variants"] = rawJSON(t, map[string]any{
					"mappings": []any{},
				})
			},
		},
		{
			name: "missing-material-index",
			mutate: func(doc *gltf.Document) {
				doc.Meshes[0].Primitives[0].Extensions["KHR_materials_variants"] = rawJSON(t, map[string]any{
					"mappings": []map[string]any{{"variants": []int{0}}},
				})
			},
		},
		{
			name: "out-of-range-material-index",
			mutate: func(doc *gltf.Document) {
				doc.Meshes[0].Primitives[0].Extensions["KHR_materials_variants"] = rawJSON(t, map[string]any{
					"mappings": []map[string]any{{"material": 99, "variants": []int{0}}},
				})
			},
		},
		{
			name: "empty-mapping-variants",
			mutate: func(doc *gltf.Document) {
				doc.Meshes[0].Primitives[0].Extensions["KHR_materials_variants"] = rawJSON(t, map[string]any{
					"mappings": []map[string]any{{"material": 1, "variants": []int{}}},
				})
			},
		},
		{
			name: "out-of-range-variant-index",
			mutate: func(doc *gltf.Document) {
				doc.Meshes[0].Primitives[0].Extensions["KHR_materials_variants"] = rawJSON(t, map[string]any{
					"mappings": []map[string]any{{"material": 1, "variants": []int{2}}},
				})
			},
		},
		{
			name: "duplicate-variant-on-primitive",
			mutate: func(doc *gltf.Document) {
				doc.Meshes[0].Primitives[0].Extensions["KHR_materials_variants"] = rawJSON(t, map[string]any{
					"mappings": []map[string]any{
						{"material": 0, "variants": []int{0}},
						{"material": 1, "variants": []int{0}},
					},
				})
			},
		},
		{
			name: "invalid-variant-material-ior",
			mutate: func(doc *gltf.Document) {
				doc.ExtensionsUsed = append(doc.ExtensionsUsed, "KHR_materials_ior")
				doc.Materials[1].Extensions = gltf.Extensions{
					"KHR_materials_ior": rawJSON(t, map[string]any{"ior": -2.0}),
				}
			},
		},
	}

	for _, tc := range badCases {
		t.Run(tc.name, func(t *testing.T) {
			doc := baseValid()
			tc.mutate(doc)
			if _, err := Decode(glb(t, doc), nil); err == nil {
				t.Fatalf("expected Decode error for %s", tc.name)
			}
		})
	}

	// Points primitive with KHR_materials_variants warns and succeeds without attaching VariantMaterials.
	ptsDoc := baseValid()
	ptsDoc.Meshes[0].Primitives[0].Mode = gltf.PrimitivePoints
	ptsModel, err := Decode(glb(t, ptsDoc), nil)
	check(t, err)
	if len(ptsModel.Warnings) == 0 {
		t.Fatal("expected warning for KHR_materials_variants on points primitive")
	}
}
