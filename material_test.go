// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image"
	"image/color"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func TestSetMaterialSubtreeSharedLODAndAtomicErrors(t *testing.T) {
	s := NewScene()
	group, err := s.Add(Root, "group")
	check(t, err)

	// Non-mesh point node in subtree is ignored by SetMaterial.
	ptsID, err := s.SetGeometry("pts", Geometry{Points: []Point{{Radius: 2, Color: white}}})
	check(t, err)
	check(t, s.Reparent(ptsID, group))

	// Shared mesh instances: one inside group, one outside group.
	cube, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 1, Y: 1, Z: 1}, Color: white})
	check(t, err)
	sg, err := NewSharedGeometry(cube)
	check(t, err)
	inID, err := s.SetShared("in-shared", sg)
	check(t, err)
	check(t, s.Reparent(inID, group))
	outID, err := s.SetShared("out-shared", sg)
	check(t, err)
	_ = outID

	// LOD mesh inside group.
	lodID, err := s.Add(group, "lod")
	check(t, err)
	check(t, s.SetLOD(lodID, []LODLevel{
		{MaxDistance: 5, Shared: sg},
		{MaxDistance: 0, Geometry: cube},
	}))

	before := frame(t, s)

	// Instance-uniform-only material update preserves SharedGeometry and revisions!
	pbrMat := &Material{PBR: true, Metallic: .8, Roughness: .25, Clearcoat: .5}
	check(t, s.SetMaterial(group, pbrMat))
	pbrMat.Metallic = .1 // Caller mutation must not leak.

	afterUniform := frame(t, s)
	for idx, inst := range afterUniform.Draws {
		switch inst.Name {
		case "pts":
			if afterUniform.Draws[idx].Geometry.Material != nil {
				t.Fatal("point node acquired material")
			}
		case "in-shared":
			m := afterUniform.Draws[idx].Geometry.Material
			if m == nil || m.Metallic != .8 || m.Clearcoat != .5 {
				t.Fatalf("in-shared material not updated: %+v", m)
			}
			if afterUniform.Draws[idx].Shared == 0 || afterUniform.Draws[idx].Revision != before.Draws[idx].Revision {
				t.Fatal("instance-only SetMaterial broke SharedGeometry")
			}
		case "out-shared":
			if afterUniform.Draws[idx].Geometry.Material != nil {
				t.Fatal("node outside subtree was modified")
			}
		case "lod":
			m := afterUniform.Draws[idx].Geometry.Material
			if m == nil || m.Metallic != .8 {
				t.Fatalf("LOD tier material not updated: %+v", m)
			}
		}
	}
	if before.Draws[1].Geometry.Material != nil {
		t.Fatal("previous Frame snapshot was mutated")
	}

	// Vertex-baked change (Transparent: true) detaches modified SharedGeometry
	// cleanly while leaving out-shared on sg!
	check(t, s.SetMaterial(group, &Material{Transparent: true, Specular: .4}))
	afterTrans := frame(t, s)
	for idx, inst := range afterTrans.Draws {
		switch inst.Name {
		case "in-shared":
			if !afterTrans.Draws[idx].Geometry.Material.Transparent || afterTrans.Draws[idx].Shared != 0 {
				t.Fatal("transparent transition did not detach shared geometry")
			}
		case "out-shared":
			if afterTrans.Draws[idx].Shared == 0 || afterTrans.Draws[idx].Geometry.Material != nil {
				t.Fatal("out-shared was disturbed")
			}
		}
	}

	// Atomic UV validation failure: BuildCuboid has UVs, so add an untextured
	// mesh without UVs deeper in the subtree and verify textured SetMaterial
	// fails without mutating any sibling.
	noUV := quad(.5, .5, white, nil, false)
	noUV.UV = nil
	noUVID, err := s.SetGeometry("no-uv", noUV)
	check(t, err)
	check(t, s.Reparent(noUVID, group))

	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
	tex, err := NewTexture(img)
	check(t, err)
	if err := s.SetMaterial(group, &Material{Texture: tex}); err == nil {
		t.Fatal("expected error applying textured material to UV-less mesh")
	}
	afterErr := frame(t, s)
	for idx, inst := range afterErr.Draws {
		if inst.Name == "in-shared" && !afterErr.Draws[idx].Geometry.Material.Transparent {
			t.Fatal("failed SetMaterial partially mutated sibling")
		}
	}

	// Clearing material with nil restores default shading across the subtree.
	check(t, s.SetMaterial(group, nil))
	afterNil := frame(t, s)
	for idx, inst := range afterNil.Draws {
		if inst.Name == "in-shared" && afterNil.Draws[idx].Geometry.Material != nil {
			t.Fatal("nil SetMaterial did not clear material")
		}
	}

	// Invalid material arguments fail even on empty subtrees.
	empty, err := s.Add(Root, "empty")
	check(t, err)
	if s.SetMaterial(empty, &Material{Metallic: 2}) == nil {
		t.Fatal("accepted out-of-range Metallic")
	}
	if s.SetMaterial(NodeID(99999), nil) == nil {
		t.Fatal("accepted unknown root node")
	}
}

func TestSetMaterialOnDeformedMesh(t *testing.T) {
	s := NewScene()
	g := quad(.6, .6, white, nil, true)
	g.UV = []UV{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	id, err := s.SetGeometry("cloth", g)
	check(t, err)
	morph := MorphTarget{Positions: make([]nt3dmath.Vec3, len(g.Vertices))}
	morph.Positions[0] = nt3dmath.Vec3{Z: .3}
	check(t, s.SetDeformation(id, Deformation{Morphs: []MorphTarget{morph}, Weights: []float32{1}}))

	nmap := testNormalMap(t, color.NRGBA{128, 128, 255, 255})
	check(t, s.SetMaterial(id, &Material{NormalMap: nmap, NormalStrength: 1, Specular: .5}))
	f := frame(t, s)
	check(t, validateFrame(f))
	if f.Draws[0].Geometry.Material == nil || f.Draws[0].Geometry.Material.NormalMap != nmap || len(f.Draws[0].Geometry.Tangents) != len(g.Vertices) {
		t.Fatal("deformed mesh did not receive normal map and generated tangents")
	}
}

func TestMaterialBaseColorSoftwareShading(t *testing.T) {
	cam := DefaultCamera()
	cam.Elevation, cam.Azimuth, cam.Distance = 90, 0, 2.2

	// 1. Untextured PBR surface with BaseColor matches vertex-baked color in both linear and sRGB frames.
	tint := color.RGBA{230, 175, 30, 255}
	for _, linear := range []bool{false, true} {
		sBaked := NewScene()
		bBaked, err := BuildSphere(Sphere{Radius: .8, Segments: 28, Rings: 18, Color: tint, Material: &Material{PBR: true, Roughness: .35}})
		check(t, err)
		_, err = sBaked.SetGeometry("ball", bBaked)
		check(t, err)
		fBaked, err := sBaked.Frame(cam, 64, 64)
		check(t, err)
		fBaked.Linear = linear

		sInst := NewScene()
		bInst, err := BuildSphere(Sphere{Radius: .8, Segments: 28, Rings: 18, Color: white, Material: &Material{PBR: true, Roughness: .35, BaseColor: tint}})
		check(t, err)
		_, err = sInst.SetGeometry("ball", bInst)
		check(t, err)
		fInst, err := sInst.Frame(cam, 64, 64)
		check(t, err)
		fInst.Linear = linear

		imgBaked := softwareRender(fBaked)
		imgInst := softwareRender(fInst)
		cBaked := at(imgBaked, 32, 32)
		cInst := at(imgInst, 32, 32)
		if !closeRGB(cBaked, cInst, 2) {
			t.Fatalf("linear=%v BaseColor %v != vertex color %v", linear, cInst, cBaked)
		}
	}

	// 2. Unlit textured mesh (which uses directTexture when BaseColor is zero) tints cleanly when BaseColor is set.
	tex := testTexture(t, color.RGBA{200, 200, 200, 255})
	sTex := NewScene()
	q := quad(1.2, 0, white, &Material{Unlit: true, Texture: tex, BaseColor: color.RGBA{255, 64, 32, 255}}, false)
	q.UV = []UV{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	_, err := sTex.SetGeometry("quad", q)
	check(t, err)
	topCam := DefaultCamera()
	topCam.Elevation, topCam.Azimuth, topCam.Distance = 90, 89.9, 2.0
	fTex, err := sTex.Frame(topCam, 48, 48)
	check(t, err)
	px := at(softwareRender(fTex), 24, 24)
	if px.R < 180 || px.G > 70 || px.B > 40 {
		t.Fatalf("expected red-orange tinted unlit texture, got %v", px)
	}

	// 3. Transparent BaseColor scales opacity, including zero-alpha invisible colorways.
	sTrans := NewScene()
	qt := quad(1.2, 0, white, &Material{Unlit: true, Transparent: true, BaseColor: color.RGBA{255, 255, 255, 0}}, false)
	qID, err := sTrans.SetGeometry("glass", qt)
	check(t, err)
	fZero, err := sTrans.Frame(topCam, 48, 48)
	check(t, err)
	fZero.Background = color.RGBA{20, 40, 80, 255}
	if got := at(softwareRender(fZero), 24, 24); !closeRGB(got, fZero.Background, 1) {
		t.Fatalf("zero-alpha BaseColor changed background: got %v, want %v", got, fZero.Background)
	}
	check(t, sTrans.SetMaterial(qID, &Material{Unlit: true, Transparent: true, BaseColor: color.RGBA{220, 40, 40, 128}}))
	fHalf, err := sTrans.Frame(topCam, 48, 48)
	check(t, err)
	fHalf.Background = color.RGBA{20, 40, 80, 255}
	gotHalf := at(softwareRender(fHalf), 24, 24)
	if gotHalf.R <= fHalf.Background.R+60 || gotHalf.B >= fHalf.Background.B {
		t.Fatalf("half-alpha BaseColor did not blend over background: got %v", gotHalf)
	}
}

func TestSceneAssetMaterialVariants(t *testing.T) {
	cube, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 1, Y: 1, Z: 1}, Color: white, Material: &Material{PBR: true, Roughness: .5}})
	check(t, err)
	bodySG, err := NewSharedGeometry(cube)
	check(t, err)
	trimSG, err := NewSharedGeometry(cube)
	check(t, err)

	taxiMat := &Material{PBR: true, Roughness: .2, Clearcoat: 1, BaseColor: color.RGBA{245, 195, 20, 255}}
	policeMat := &Material{PBR: true, Roughness: .3, BaseColor: color.RGBA{30, 60, 160, 255}}
	lightbarMat := &Material{PBR: true, Emissive: color.RGBA{40, 120, 255, 255}, EmissiveIntensity: 2}
	glassMat := &Material{PBR: true, Roughness: .1, Transparent: true, BaseColor: color.RGBA{120, 210, 255, 128}}

	asset := &SceneAsset{
		Variants: []string{"Taxi", "Police", "Glass"},
		Nodes: []AssetNode{
			{
				Name:   "Body",
				Parent: -1,
				Local:  nt3dmath.Identity(),
				Shared: bodySG,
				VariantMaterials: map[int]*Material{
					0: taxiMat,
					1: policeMat,
					2: glassMat,
				},
			},
			{
				Name:   "Lightbar",
				Parent: 0,
				Local:  nt3dmath.Translation(nt3dmath.Vec3{Z: .75}),
				Shared: trimSG,
				VariantMaterials: map[int]*Material{
					1: lightbarMat, // Only Police overrides Lightbar; Taxi & Glass revert to default.
				},
			},
		},
	}

	s := NewScene()
	carA, err := asset.Instantiate(s, Root, "carA")
	check(t, err)
	carB, err := asset.Instantiate(s, Root, "carB")
	check(t, err)
	carC, err := asset.Instantiate(s, Root, "carC")
	check(t, err)

	gotVars := s.MaterialVariants(carA.Root)
	if len(gotVars) != 3 || gotVars[0] != "Taxi" || gotVars[1] != "Police" || gotVars[2] != "Glass" {
		t.Fatalf("unexpected MaterialVariants: %v", gotVars)
	}
	gotVars[0] = "Mutated"
	if s.MaterialVariants(carA.Root)[0] != "Taxi" {
		t.Fatal("MaterialVariants was not cloned")
	}
	if s.MaterialVariant(carA.Root) != "" {
		t.Fatalf("expected default active variant \"\", got %q", s.MaterialVariant(carA.Root))
	}

	check(t, s.SetMaterialVariant(carA.Root, "Taxi"))
	check(t, s.SetMaterialVariant(carB.Root, "Police"))
	if s.MaterialVariant(carA.Root) != "Taxi" || s.MaterialVariant(carB.Root) != "Police" || s.MaterialVariant(carC.Root) != "" {
		t.Fatal("active variant mismatch across independent instances")
	}

	f := frame(t, s)
	var sharedBodyID uint64
	for idx, inst := range f.Draws {
		switch inst.Name {
		case "carA/0:Body":
			if f.Draws[idx].Geometry.Material.BaseColor != taxiMat.BaseColor || f.Draws[idx].Shared == 0 {
				t.Fatalf("carA body expected shared Taxi material, got mat=%+v shared=%d", f.Draws[idx].Geometry.Material, f.Draws[idx].Shared)
			}
			sharedBodyID = f.Draws[idx].Shared
		case "carB/0:Body":
			if f.Draws[idx].Geometry.Material.BaseColor != policeMat.BaseColor || f.Draws[idx].Shared != sharedBodyID {
				t.Fatalf("carB body did not share geometry with carA: shared=%d want=%d", f.Draws[idx].Shared, sharedBodyID)
			}
		case "carC/0:Body":
			if f.Draws[idx].Geometry.Material.BaseColor != (color.RGBA{}) || f.Draws[idx].Shared != sharedBodyID {
				t.Fatalf("carC body did not remain default shared: mat=%+v shared=%d", f.Draws[idx].Geometry.Material, f.Draws[idx].Shared)
			}
		case "carB/1:Lightbar":
			if f.Draws[idx].Geometry.Material.Emissive != lightbarMat.Emissive {
				t.Fatalf("carB lightbar did not activate on Police: %+v", f.Draws[idx].Geometry.Material)
			}
		}
	}

	// Switching carB from Police -> Taxi resets unmapped Lightbar back to default material!
	check(t, s.SetMaterialVariant(carB.Root, "Taxi"))
	f2 := frame(t, s)
	for idx, inst := range f2.Draws {
		if inst.Name == "carB/1:Lightbar" && f2.Draws[idx].Geometry.Material.Emissive != (color.RGBA{}) {
			t.Fatalf("unmapped Lightbar did not reset to default on Taxi: %+v", f2.Draws[idx].Geometry.Material)
		}
	}

	// Switching carA to Glass (Transparent) detaches carA's Body cleanly, then switching back to "" rejoins SharedGeometry!
	check(t, s.SetMaterialVariant(carA.Root, "Glass"))
	fGlass := frame(t, s)
	for idx, inst := range fGlass.Draws {
		if inst.Name == "carA/0:Body" && (!fGlass.Draws[idx].Geometry.Material.Transparent || fGlass.Draws[idx].Shared != 0) {
			t.Fatal("expected transparent variant to detach SharedGeometry")
		}
	}
	check(t, s.SetMaterialVariant(carA.Root, ""))
	fReset := frame(t, s)
	for idx, inst := range fReset.Draws {
		if inst.Name == "carA/0:Body" && (fReset.Draws[idx].Shared != sharedBodyID || fReset.Draws[idx].Geometry.Material.BaseColor != (color.RGBA{})) {
			t.Fatalf("reset to default did not reattach SharedGeometry: shared=%d want=%d", fReset.Draws[idx].Shared, sharedBodyID)
		}
	}

	// Unknown variant or unknown node fails atomically.
	check(t, s.SetMaterialVariant(carA.Root, "Taxi"))
	if s.SetMaterialVariant(carA.Root, "NoSuchVariant") == nil {
		t.Fatal("accepted unknown variant name")
	}
	if s.MaterialVariant(carA.Root) != "Taxi" {
		t.Fatal("failed SetMaterialVariant altered active variant")
	}
	if s.SetMaterialVariant(NodeID(999999), "Taxi") == nil {
		t.Fatal("accepted unknown root node")
	}

	// Remove cleans up variant state.
	check(t, s.Remove(carA.Root))
	if s.MaterialVariants(carA.Root) != nil || s.MaterialVariant(carA.Root) != "" {
		t.Fatal("Remove did not clean up variant state")
	}

	// Validation errors on malformed SceneAssets.
	if _, err := (&SceneAsset{Variants: []string{""}}).Instantiate(s, Root, "badEmpty"); err == nil {
		t.Fatal("accepted empty variant name")
	}
	if _, err := (&SceneAsset{Variants: []string{"A", "A"}}).Instantiate(s, Root, "badDup"); err == nil {
		t.Fatal("accepted duplicate variant name")
	}
	if _, err := (&SceneAsset{
		Variants: []string{"A"},
		Nodes:    []AssetNode{{Name: "n", Parent: -1, Local: nt3dmath.Identity(), Shared: bodySG, VariantMaterials: map[int]*Material{1: taxiMat}}},
	}).Instantiate(s, Root, "badIdx"); err == nil {
		t.Fatal("accepted out-of-range variant index")
	}
}

// A material's rules are written once: a geometry's Validate and the
// material's own check refuse the same faults with the same words.
func TestMaterialRulesAreSharedByGeometryAndMaterial(t *testing.T) {
	box, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 1, Y: 1, Z: 1}})
	check(t, err)
	for name, m := range map[string]Material{
		"metallic":      {Metallic: 2},
		"roughness":     {Roughness: -1},
		"ior":           {IOR: .5},
		"threshold":     {Threshold: 2},
		"shininess":     {Specular: .5, Shininess: 2000},
		"reflectivity":  {Reflectivity: 1.5},
		"transmission":  {Transmission: .5, Unlit: true},
		"cutout blend":  {Transparent: true, Cutout: true},
		"flow strength": {FlowStrength: -1},
		"emissive":      {EmissiveIntensity: -1},
		"occlusion":     {OcclusionStrength: 2},
		"clearcoat":     {Clearcoat: 2},
	} {
		want := validateMaterial(&m)
		if want == nil {
			t.Errorf("%s: the material's own check accepted it", name)
			continue
		}
		g := box
		g.Material = &m
		if got := g.Validate(); got == nil || got.Error() != want.Error() {
			t.Errorf("%s: geometry says %v, material says %v", name, got, want)
		}
	}
	// A map needs a UV for every vertex, which only the geometry can know.
	g := box
	g.UV = nil
	g.Material = &Material{Texture: testTexture(t, color.RGBA{255, 0, 0, 255})}
	if validateMaterial(g.Material) != nil || g.Validate() == nil {
		t.Error("a textured mesh without UVs should fail the geometry's check and pass the material's own")
	}
}
