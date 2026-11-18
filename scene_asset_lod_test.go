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

func TestParseLODName(t *testing.T) {
	valid := []struct {
		name, parent string
		wantStem     string
		wantTier     int
	}{
		{"SM_Bench_01_LOD0", "", "SM_Bench_01", 0},
		{"SM_Bench_01_LOD12", "", "SM_Bench_01", 12},
		{"bench-lod1", "", "bench", 1},
		{"Bench.LOD2", "", "Bench", 2},
		{"Bench LOD3", "", "Bench", 3},
		{"Bench_LOD_0", "", "Bench", 0},
		{"Bench-lod-4", "", "Bench", 4},
		{"LOD0", "Streetlight", "Streetlight", 0},
		{"LOD_1", "Streetlight", "Streetlight", 1},
		{"lod2", "Y-up", "LOD", 2},
		{"LOD0", "", "LOD", 0},
	}
	for _, tc := range valid {
		stem, tier, ok := parseLODName(tc.name, tc.parent)
		if !ok || stem != tc.wantStem || tier != tc.wantTier {
			t.Fatalf("parseLODName(%q, %q) = (%q, %d, %v), want (%q, %d, true)",
				tc.name, tc.parent, stem, tier, ok, tc.wantStem, tc.wantTier)
		}
	}

	invalid := []string{
		"",
		"LOD",
		"Melod1",
		"Explod2",
		"Building_01",
		"_LOD0",
		"Bench_LOD32",
		"Bench_LOD100",
		"Bench_LOD_X",
	}
	for _, name := range invalid {
		if stem, tier, ok := parseLODName(name, "Parent"); ok {
			t.Fatalf("parseLODName(%q) unexpectedly matched (%q, %d)", name, stem, tier)
		}
	}
}

func makeLODTestMesh(t *testing.T, tris int, col color.RGBA) *SharedGeometry {
	t.Helper()
	var verts []Vertex
	var uvs []UV
	var idx []uint32
	for i := 0; i < tris; i++ {
		base := uint32(len(verts))
		z := float32(i) * 0.01
		verts = append(verts,
			Vertex{Position: nt3dmath.Vec3{X: -0.5, Y: -0.5, Z: z}, Normal: nt3dmath.Vec3{Z: 1}, Color: col},
			Vertex{Position: nt3dmath.Vec3{X: 0.5, Y: -0.5, Z: z}, Normal: nt3dmath.Vec3{Z: 1}, Color: col},
			Vertex{Position: nt3dmath.Vec3{X: 0, Y: 0.5, Z: z}, Normal: nt3dmath.Vec3{Z: 1}, Color: col},
		)
		uvs = append(uvs, UV{0, 0}, UV{1, 0}, UV{0.5, 1})
		idx = append(idx, base, base+1, base+2)
	}
	g := Geometry{
		Vertices: verts,
		UV:       uvs,
		Indices:  idx,
		Material: &Material{Roughness: 0.6, BaseColor: col},
	}
	sg, err := NewSharedGeometry(g)
	check(t, err)
	return sg
}

func poseCameraAt(eye nt3dmath.Vec3) Camera {
	return Camera{
		Pose:       nt3dmath.Translation(eye),
		FOV:        2.0 * 180 / math.Pi,
		Near:       0.1,
		Far:        500,
		Projection: Perspective,
	}
}

func TestSceneAssetWithLODSingleAndKitbash(t *testing.T) {
	lamp0 := makeLODTestMesh(t, 12, color.RGBA{240, 200, 80, 255})
	lamp1 := makeLODTestMesh(t, 4, color.RGBA{240, 200, 80, 255})
	lamp2 := makeLODTestMesh(t, 1, color.RGBA{240, 200, 80, 255})

	hydrant0 := makeLODTestMesh(t, 6, color.RGBA{210, 40, 40, 255})
	hydrant1 := makeLODTestMesh(t, 2, color.RGBA{210, 40, 40, 255})

	rotorMesh := makeLODTestMesh(t, 2, color.RGBA{180, 180, 190, 255})

	// Showcase-grid layout: Lamp_LOD0 at X=10, Lamp_LOD1 parked at X=25, Lamp_LOD2 parked at X=40,
	// plus a 2-tier Hydrant at Y=5 and an animated non-LOD Rotor sibling.
	kit := &SceneAsset{
		Nodes: []AssetNode{
			{Name: "Root", Parent: -1, Local: nt3dmath.Identity()},
			{Name: "SM_Lamp_LOD0", Parent: 0, Local: nt3dmath.Translation(nt3dmath.Vec3{X: 10})},
			{Name: "SM_Lamp_LOD0", Parent: 1, Local: nt3dmath.Identity(), Shared: lamp0},
			{Name: "SM_Lamp_LOD1", Parent: 0, Local: nt3dmath.Translation(nt3dmath.Vec3{X: 25})},
			{Name: "SM_Lamp_LOD1", Parent: 3, Local: nt3dmath.Identity(), Shared: lamp1},
			{Name: "SM_Lamp_LOD2", Parent: 0, Local: nt3dmath.Translation(nt3dmath.Vec3{X: 40})},
			{Name: "SM_Lamp_LOD2", Parent: 5, Local: nt3dmath.Identity(), Shared: lamp2},
			{Name: "Hydrant-lod0", Parent: 0, Local: nt3dmath.Translation(nt3dmath.Vec3{Y: 5}), Shared: hydrant0},
			{Name: "Hydrant-lod1", Parent: 0, Local: nt3dmath.Translation(nt3dmath.Vec3{Y: 5}), Shared: hydrant1},
			{Name: "Rotor", Parent: 0, Local: nt3dmath.Translation(nt3dmath.Vec3{X: -5}), Shared: rotorMesh},
		},
		Clips: []AnimationClip{
			{
				Name: "Spin",
				Tracks: []Track{
					{
						Node:          10, // 1-based index of "Rotor" (node 9)
						Channel:       Translation,
						Width:         3,
						Interpolation: Linear,
						Times:         []float32{0, 1},
						Values:        []float32{-5, 0, 0, -5, 0, 3},
					},
				},
			},
		},
	}

	// K-1 transition distances -> unbounded final tier.
	lodAsset, err := kit.WithLOD(20, 60)
	check(t, err)
	if len(lodAsset.Nodes) != 4 { // Root + SM_Lamp + Hydrant + Rotor
		t.Fatalf("got %d compiled nodes, want 4", len(lodAsset.Nodes))
	}
	// SharedGeometry pointers must be preserved without copying.
	if lodAsset.Nodes[1].LOD[0].Shared != lamp0 || lodAsset.Nodes[1].LOD[1].Shared != lamp1 || lodAsset.Nodes[1].LOD[2].Shared != lamp2 {
		t.Fatal("expected SharedGeometry pointers preserved on SM_Lamp LOD tiers")
	}

	s := NewScene()
	instA, err := lodAsset.Instantiate(s, Root, "blockA")
	check(t, err)
	instB, err := lodAsset.Instantiate(s, Root, "blockB")
	check(t, err)
	check(t, s.SetTransform(instB.Root, nt3dmath.Translation(nt3dmath.Vec3{Z: -100})))

	// Verify non-LOD sibling clip remapped cleanly after collapsing 6 LOD nodes into 2.
	clips := s.AnimationClips(instA.Root)
	if len(clips) != 1 || clips[0].Name != "Spin" {
		t.Fatalf("unexpected remapped clips: %+v", clips)
	}
	anim, err := NewAnimation(s, clips[0])
	check(t, err)
	check(t, anim.Sample(1))
	rotorNode, ok := s.Node(instA.Nodes[3])
	if !ok || math.Abs(float64(rotorNode.World[14]-3)) > 1e-4 {
		t.Fatalf("expected animated Rotor Z=3, got %v", rotorNode.World[14])
	}

	// Camera near blockA (Z=14, looking -Z with wide FOV) and far from blockB (Z=-100 -> dist ~114).
	cam := poseCameraAt(nt3dmath.Vec3{X: 2, Y: 2, Z: 14})
	f, err := s.Frame(cam, 320, 240)
	check(t, err)
	st := f.Stats()
	// blockA: Lamp (LOD0: 12 tris) + Hydrant (LOD0: 6 tris) + Rotor (2 tris) = 20 tris.
	// blockB (at Z=-100, dist ~110 > 60): Lamp (LOD2: 1 tri) + Hydrant (LOD1: 2 tris) + Rotor (2 tris) = 5 tris.
	if st.Triangles != 25 {
		t.Fatalf("got %d triangles, want 25 (stats=%+v)", st.Triangles, st)
	}
	if !slices.Equal(st.LODTierInstances[:3], []int{2, 1, 1}) {
		t.Fatalf("got LODTierInstances=%v, want [2 1 1 ...]", st.LODTierInstances[:4])
	}

	// Verify Lamp in blockB stayed at X=10 (showcase offset X=40 on SM_Lamp_LOD2 was NOT baked!).
	lampBNode, ok := s.Node(instB.Nodes[1])
	if !ok || math.Abs(float64(lampBNode.World[12]-10)) > 1e-4 {
		t.Fatalf("expected blockB Lamp X=10, got %v", lampBNode.World[12])
	}

	// Now test explicit K distances with finite hide-beyond cutoff (100m) + SetLODBias.
	culledAsset, err := kit.WithLOD(20, 60, 100)
	check(t, err)
	s2 := NewScene()
	_, err = culledAsset.Instantiate(s2, Root, "farBlock")
	check(t, err)
	farNode, _ := s2.Find("farBlock")
	check(t, s2.SetTransform(farNode, nt3dmath.Translation(nt3dmath.Vec3{Z: -120})))
	f2, err := s2.Frame(cam, 320, 240)
	check(t, err)
	// At dist ~130 > 100, both SM_Lamp and Hydrant are culled; only Rotor (2 tris) draws!
	if got := f2.Stats().Triangles; got != 2 {
		t.Fatalf("expected LOD props culled beyond 100m leaving 2 rotor tris, got %d", got)
	}
	// Increasing SetLODBias(2.0) extends cutoff to 200m -> Lamp (LOD2: 1) + Hydrant (LOD1: 2) + Rotor (2) = 5 tris!
	check(t, s2.SetLODBias(2.0))
	f3, err := s2.Frame(cam, 320, 240)
	check(t, err)
	if got := f3.Stats().Triangles; got != 5 {
		t.Fatalf("expected 5 tris after SetLODBias(2.0), got %d", got)
	}
}

func TestSceneAssetWithLODMultiPartSynchronizedBounds(t *testing.T) {
	body0 := makeLODTestMesh(t, 10, color.RGBA{200, 200, 220, 255})
	body1 := makeLODTestMesh(t, 4, color.RGBA{200, 200, 220, 255})
	wheel0 := makeLODTestMesh(t, 8, color.RGBA{40, 40, 40, 255})
	wheel1 := makeLODTestMesh(t, 3, color.RGBA{40, 40, 40, 255})
	glass0 := makeLODTestMesh(t, 2, color.RGBA{120, 180, 255, 255})
	proxy2 := makeLODTestMesh(t, 1, color.RGBA{150, 150, 160, 255})

	wheelLocal := nt3dmath.Translation(nt3dmath.Vec3{X: 4}).Mul(nt3dmath.RotationZ(0.6))
	proxyLocal := nt3dmath.Translation(nt3dmath.Vec3{Z: 0.5})

	// Car_LOD0: Body (origin), Wheel (at X=+4, rotated), Glass (origin, distinct material).
	// Car_LOD1: Body (origin), Wheel (at X=+4, rotated) -- Glass omitted!
	// Car_LOD2: ProxyCard (at Z=+0.5) -- gap in tiers 0 and 1!
	car := &SceneAsset{
		Nodes: []AssetNode{
			{Name: "Car_LOD0", Parent: -1, Local: nt3dmath.Identity()},
			{Name: "Body", Parent: 0, Local: nt3dmath.Identity(), Shared: body0},
			{Name: "Glass", Parent: 0, Local: nt3dmath.Identity(), Shared: glass0},
			{Name: "Wheel", Parent: 0, Local: wheelLocal, Shared: wheel0},

			{Name: "Car_LOD1", Parent: -1, Local: nt3dmath.Identity()},
			{Name: "Body", Parent: 4, Local: nt3dmath.Identity(), Shared: body1},
			{Name: "Wheel", Parent: 4, Local: wheelLocal, Shared: wheel1},

			{Name: "Car_LOD2", Parent: -1, Local: nt3dmath.Identity()},
			{Name: "ProxyCard", Parent: 7, Local: proxyLocal, Shared: proxy2},
		},
	}

	lodCar, err := car.WithLOD(10, 30)
	check(t, err)

	s := NewScene()
	inst, err := lodCar.Instantiate(s, Root, "car")
	check(t, err)

	// Place camera along +X at X = 13:
	// - Distance to Wheel's own AABB (around X=4, half-extent ~0.7) is ~8.3 (<= 10!).
	// - Distance to Body's own AABB (around X=0, half-extent 0.5) would be 12.5 (> 10!).
	// Because all slots share synchronized prop-level LODBounds (~[-0.5..4.7]), EVERY slot
	// sees dist ~8.3 <= 10 and stays together in LOD0 (10 + 8 + 2 = 20 tris, ProxyCard empty)!
	camAt := func(x float32) Camera {
		return Camera{
			Target:     nt3dmath.Vec3{},
			Distance:   float64(x),
			Azimuth:    0, // Eye along +X looking toward origin
			Near:       0.1,
			Far:        500,
			Projection: Perspective,
		}
	}

	f0, err := s.Frame(camAt(13), 320, 240)
	check(t, err)
	st0 := f0.Stats()
	if st0.Triangles != 20 {
		t.Fatalf("at X=13 expected synchronized LOD0 (20 tris), got %d", st0.Triangles)
	}
	// ProxyCard's empty Tier 0 Geometry{} must not inflate DrawnInstances or LODTierInstances!
	if st0.DrawnInstances != 3 || st0.LODTierInstances[0] != 3 {
		t.Fatalf("unexpected LOD0 instance stats: Drawn=%d Tiers=%v", st0.DrawnInstances, st0.LODTierInstances[:3])
	}

	// At X = 20 (prop distance ~15.3 in (10, 30]): synchronized switch to LOD1 (4 + 3 = 7 tris, Glass culled, ProxyCard empty).
	f1, err := s.Frame(camAt(20), 320, 240)
	check(t, err)
	st1 := f1.Stats()
	if st1.Triangles != 7 || st1.DrawnInstances != 2 || st1.LODTierInstances[1] != 2 {
		t.Fatalf("at X=20 expected synchronized LOD1 (7 tris, 2 drawn), got tris=%d drawn=%d tiers=%v",
			st1.Triangles, st1.DrawnInstances, st1.LODTierInstances[:3])
	}

	// At X = 45 (prop distance ~40.3 > 30): Body/Glass/Wheel all culled; ProxyCard activates in Tier 2 (1 tri)!
	f2, err := s.Frame(camAt(45), 320, 240)
	check(t, err)
	st2 := f2.Stats()
	if st2.Triangles != 1 || st2.DrawnInstances != 1 || st2.LODTierInstances[2] != 1 {
		t.Fatalf("at X=45 expected ProxyCard LOD2 (1 tri, 1 drawn), got tris=%d drawn=%d tiers=%v",
			st2.Triangles, st2.DrawnInstances, st2.LODTierInstances[:3])
	}

	// Verify ApplyDisplacement pads lodRefBounds across all parts synchronously:
	// At X = 16, unpadded distance is ~11.3 (> 10 -> LOD1 = 7 tris); with MaxDisplacement = 2.0,
	// padded prop max X becomes ~6.7 so distance is ~9.3 (<= 10 -> LOD0 = 20 tris)!
	disp, err := NewVertexDisplacement(
		"fn nt3dDisplace(in: NT3DVertexInput) -> NT3DVertexOutput { return NT3DVertexOutput(in.position, in.normal); }",
		2.0,
		func(in VertexDisplaceInput) VertexDisplaceOutput {
			return VertexDisplaceOutput{Position: in.Position, Normal: in.Normal}
		},
	)
	check(t, err)
	check(t, s.ApplyDisplacement(inst.Root, disp))
	fDisp, err := s.Frame(camAt(16), 320, 240)
	check(t, err)
	if fDisp.Stats().Triangles != 20 {
		t.Fatalf("expected displaced LOD0 (20 tris) at X=16, got %d", fDisp.Stats().Triangles)
	}
}

func TestSceneAssetLODMaterialVariantsAndCombine(t *testing.T) {
	sg0 := makeLODTestMesh(t, 6, color.RGBA{255, 255, 255, 255})
	sg1 := makeLODTestMesh(t, 2, color.RGBA{255, 255, 255, 255})

	normImg := image.NewRGBA(image.Rect(0, 0, 2, 2))
	copy(normImg.Pix, []byte{128, 128, 255, 255, 128, 128, 255, 255, 128, 128, 255, 255, 128, 128, 255, 255})
	normTex, err := NewNormalMap(normImg)
	check(t, err)

	taxi0 := &Material{Roughness: 0.2, BaseColor: color.RGBA{240, 190, 20, 255}}
	taxi1 := &Material{Roughness: 0.3, BaseColor: color.RGBA{235, 185, 25, 255}}
	weathered := &Material{Roughness: 0.9, BaseColor: color.RGBA{150, 90, 60, 255}, NormalMap: normTex}
	police1 := &Material{Roughness: 0.25, BaseColor: color.RGBA{30, 80, 230, 255}}

	tier0Asset := &SceneAsset{
		Variants: []string{"Taxi", "Weathered"},
		Nodes: []AssetNode{
			{Name: "Y-up", Parent: -1, Local: nt3dmath.RotationX(math.Pi / 2)},
			{
				Name:             "Sedan_LOD0",
				Parent:           0,
				Local:            nt3dmath.Identity(),
				Shared:           sg0,
				VariantMaterials: map[int]*Material{0: taxi0, 1: weathered},
			},
		},
	}
	// Tier 1 exported at 0.5 scale with overlapping + extra variant ("Police", "Taxi")!
	tier1Asset := &SceneAsset{
		Variants: []string{"Police", "Taxi"},
		Nodes: []AssetNode{
			{Name: "Y-up", Parent: -1, Local: nt3dmath.RotationX(math.Pi / 2)},
			{
				Name:             "Sedan_LOD1",
				Parent:           0,
				Local:            nt3dmath.Scaling(nt3dmath.Vec3{X: 0.5, Y: 0.5, Z: 0.5}),
				Shared:           sg1,
				VariantMaterials: map[int]*Material{0: police1, 1: taxi1},
			},
		},
	}

	combined, err := CombineAssetLOD([]AssetLOD{
		{MaxDistance: 25, Asset: tier0Asset},
		{MaxDistance: 0, Asset: tier1Asset},
	})
	check(t, err)
	if !slices.Equal(combined.Variants, []string{"Taxi", "Weathered", "Police"}) {
		t.Fatalf("got merged variants %v, want [Taxi Weathered Police]", combined.Variants)
	}
	// Tier 0 kept original SharedGeometry pointer; Tier 1 was scaled into a new SharedGeometry.
	lodNode := combined.Nodes[1]
	if lodNode.LOD[0].Shared != sg0 || lodNode.LOD[1].Shared == nil || lodNode.LOD[1].Shared == sg1 {
		t.Fatal("expected tier 0 zero-copy SharedGeometry and tier 1 scale-aligned SharedGeometry")
	}

	s := NewScene()
	inst, err := combined.Instantiate(s, Root, "sedan")
	check(t, err)

	camAt := func(z float32) Camera {
		return poseCameraAt(nt3dmath.Vec3{Z: z})
	}

	// Switch to "Weathered" (has NormalMap on untangent mesh -> detaches tier 0 from SharedGeometry to generate tangents).
	check(t, s.SetMaterialVariant(inst.Root, "Weathered"))
	fWeathered, err := s.Frame(camAt(5), 160, 120)
	check(t, err)
	if fWeathered.Draws[0].Shared != 0 || fWeathered.Draws[0].Geometry.Material.BaseColor != weathered.BaseColor {
		t.Fatal("expected Weathered variant to generate tangents and apply weathered BaseColor")
	}

	// Switch to "Taxi" -> vertex-compatible with sg0, so tier 0 MUST rejoin SharedGeometry!
	check(t, s.SetMaterialVariant(inst.Root, "Taxi"))
	fTaxiNear, err := s.Frame(camAt(5), 160, 120)
	check(t, err)
	if fTaxiNear.Draws[0].Shared == 0 || fTaxiNear.Draws[0].Geometry.Material.BaseColor != taxi0.BaseColor {
		t.Fatalf("expected tier 0 to rejoin SharedGeometry with taxi0 BaseColor, got shared=%d col=%v",
			fTaxiNear.Draws[0].Shared, fTaxiNear.Draws[0].Geometry.Material.BaseColor)
	}
	fTaxiFar, err := s.Frame(camAt(40), 160, 120)
	check(t, err)
	if fTaxiFar.Draws[0].Shared == 0 || fTaxiFar.Draws[0].Geometry.Material.BaseColor != taxi1.BaseColor {
		t.Fatalf("expected tier 1 to use remapped taxi1 BaseColor, got shared=%d col=%v",
			fTaxiFar.Draws[0].Shared, fTaxiFar.Draws[0].Geometry.Material.BaseColor)
	}

	// Switch to "Police" -> unmapped on tier 0 (reverts to default white), mapped on tier 1 (police1 blue)!
	check(t, s.SetMaterialVariant(inst.Root, "Police"))
	fPoliceNear, err := s.Frame(camAt(5), 160, 120)
	check(t, err)
	if fPoliceNear.Draws[0].Geometry.Material.BaseColor != (color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("expected unmapped tier 0 to revert to default white, got %v", fPoliceNear.Draws[0].Geometry.Material.BaseColor)
	}
	fPoliceFar, err := s.Frame(camAt(40), 160, 120)
	check(t, err)
	if fPoliceFar.Draws[0].Geometry.Material.BaseColor != police1.BaseColor {
		t.Fatalf("expected tier 1 Police BaseColor, got %v", fPoliceFar.Draws[0].Geometry.Material.BaseColor)
	}
}

func TestSceneAssetLODValidationErrors(t *testing.T) {
	sg := makeLODTestMesh(t, 2, color.RGBA{200, 200, 200, 255})

	var nilAsset *SceneAsset
	if _, err := nilAsset.WithLOD(20); err == nil {
		t.Fatal("expected error for nil SceneAsset")
	}

	validPair := &SceneAsset{
		Nodes: []AssetNode{
			{Name: "Prop_LOD0", Parent: -1, Local: nt3dmath.Identity(), Shared: sg},
			{Name: "Prop_LOD1", Parent: -1, Local: nt3dmath.Identity(), Shared: sg},
			{Name: "Prop_LOD2", Parent: -1, Local: nt3dmath.Identity(), Shared: sg},
		},
	}
	if _, err := validPair.WithLOD(); err == nil {
		t.Fatal("expected error for empty maxDistances")
	}
	if _, err := validPair.WithLOD(20); err == nil {
		t.Fatal("expected error for 1 distance on 3-tier asset")
	}
	if _, err := validPair.WithLOD(20, 0); err == nil {
		t.Fatal("expected error for zero non-final distance in K-1 form")
	}
	if _, err := validPair.WithLOD(30, 20); err == nil {
		t.Fatal("expected error for non-increasing distances")
	}

	noLOD := &SceneAsset{
		Nodes: []AssetNode{{Name: "Melody1", Parent: -1, Local: nt3dmath.Identity(), Shared: sg}},
	}
	if _, err := noLOD.WithLOD(20); err == nil {
		t.Fatal("expected error when no LOD groups match")
	}

	dupLOD := &SceneAsset{
		Nodes: []AssetNode{
			{Name: "Prop_LOD0", Parent: -1, Local: nt3dmath.Identity(), Shared: sg},
			{Name: "prop-lod0", Parent: -1, Local: nt3dmath.Identity(), Shared: sg},
		},
	}
	if _, err := dupLOD.WithLOD(20); err == nil {
		t.Fatal("expected error for duplicate LOD0 tier")
	}

	light := Light{Direction: nt3dmath.Vec3{Z: 1}, Color: color.RGBA{255, 255, 255, 255}}
	withLight := &SceneAsset{
		Nodes: []AssetNode{
			{Name: "Prop_LOD0", Parent: -1, Local: nt3dmath.Identity(), Shared: sg, Light: &light},
			{Name: "Prop_LOD1", Parent: -1, Local: nt3dmath.Identity(), Shared: sg},
		},
	}
	if _, err := withLight.WithLOD(20); err == nil {
		t.Fatal("expected error for light inside LOD tier")
	}

	if _, err := CombineAssetLOD(nil); err == nil {
		t.Fatal("expected error for empty CombineAssetLOD")
	}
	if _, err := CombineAssetLOD([]AssetLOD{{MaxDistance: 10, Asset: nil}}); err == nil {
		t.Fatal("expected error for nil asset in CombineAssetLOD")
	}

	// Verify SceneAsset.LODTiers, Bounds, and Triangles helpers.
	if got := nilAsset.LODTiers(); got != 0 {
		t.Fatalf("nilAsset.LODTiers() = %d, want 0", got)
	}
	if got := noLOD.LODTiers(); got != 0 {
		t.Fatalf("noLOD.LODTiers() = %d, want 0", got)
	}
	if got := withLight.LODTiers(); got != 0 {
		t.Fatalf("withLight.LODTiers() = %d, want 0 (light inside LOD tier is uncompilable)", got)
	}
	if got := validPair.LODTiers(); got != 3 {
		t.Fatalf("validPair.LODTiers() = %d, want 3", got)
	}
	if got := validPair.Triangles(); got != 6 {
		t.Fatalf("raw validPair.Triangles() = %d, want 6", got)
	}
	compiledPair, err := validPair.WithLOD(15, 45)
	check(t, err)
	if got := compiledPair.LODTiers(); got != 3 {
		t.Fatalf("compiledPair.LODTiers() = %d, want 3", got)
	}
	if got := compiledPair.Triangles(); got != 2 {
		t.Fatalf("compiledPair.Triangles() = %d, want 2 (finest tier)", got)
	}
	if b := compiledPair.Bounds(); !b.IsValid() || b.Min.X != -0.5 || b.Max.X != 0.5 {
		t.Fatalf("unexpected compiledPair.Bounds() = %+v", b)
	}

	// Regression (found by FuzzSceneAssetLOD):
	// 1. A lone *_LOD0 parent container wrapping a 3-tier child group must not mask the 3-tier group.
	nestedContainer := &SceneAsset{
		Nodes: []AssetNode{
			{Name: "Section_LOD0", Parent: -1, Local: nt3dmath.Identity()},
			{Name: "Lamp_LOD0", Parent: 0, Local: nt3dmath.Identity(), Shared: sg},
			{Name: "Lamp_LOD1", Parent: 0, Local: nt3dmath.Identity(), Shared: sg},
			{Name: "Lamp_LOD2", Parent: 0, Local: nt3dmath.Identity(), Shared: sg},
		},
	}
	if got := nestedContainer.LODTiers(); got != 3 {
		t.Fatalf("nestedContainer.LODTiers() = %d, want 3", got)
	}
	nestedCompiled, err := nestedContainer.WithLOD(15, 45)
	check(t, err)
	sNested := NewScene()
	_, err = nestedCompiled.Instantiate(sNested, Root, "nested")
	check(t, err)

	// 2. Singular non-LOD node transform must be rejected by both LODTiers and WithLOD.
	singularSibling := &SceneAsset{
		Nodes: []AssetNode{
			{Name: "BrokenZero", Parent: -1, Local: nt3dmath.Mat4{}},
			{Name: "Lamp_LOD0", Parent: -1, Local: nt3dmath.Identity(), Shared: sg},
			{Name: "Lamp_LOD1", Parent: -1, Local: nt3dmath.Identity(), Shared: sg},
		},
	}
	if got := singularSibling.LODTiers(); got != 0 {
		t.Fatalf("singularSibling.LODTiers() = %d, want 0", got)
	}
	if _, err := singularSibling.WithLOD(15); err == nil {
		t.Fatal("expected error for singular node transform in WithLOD")
	}
}
