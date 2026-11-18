// Copyright (c) 2026 Neomantra Corp.

//go:build webgpu

package nt3d

import (
	"image"
	"image/color"
	"math"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func meanAbsDiff(a, b image.Image) float64 {
	bounds := a.Bounds()
	var sum float64
	n := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			ca := color.RGBAModel.Convert(a.At(x, y)).(color.RGBA)
			cb := color.RGBAModel.Convert(b.At(x, y)).(color.RGBA)
			sum += math.Abs(float64(ca.R)-float64(cb.R)) +
				math.Abs(float64(ca.G)-float64(cb.G)) +
				math.Abs(float64(ca.B)-float64(cb.B))
			n += 3
		}
	}
	return sum / float64(max(1, n))
}

func TestGPUPBRDirectAndMapsParity(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()

	baseTex := testTexture(t, color.RGBA{220, 170, 60, 255})
	normTex := testNormalMap(t, color.NRGBA{160, 128, 240, 255})
	ormTex := testORMMap(t, color.NRGBA{180, 90, 230, 255})
	emTex := testTexture(t, color.RGBA{40, 160, 255, 255})

	cases := []struct {
		name   string
		linear bool
		mat    *Material
		col    color.RGBA
	}{
		{"dielectric-smooth", true, &Material{Shading: ShadePBR, PBR: PBRStyle{Metallic: 0, Roughness: .18, IOR: 1.5}}, color.RGBA{210, 70, 50, 255}},
		{"dielectric-diamond", true, &Material{Shading: ShadePBR, PBR: PBRStyle{Metallic: 0, Roughness: .25, IOR: 2.4}}, color.RGBA{30, 30, 35, 255}},
		{"conductor-gold", true, &Material{Shading: ShadePBR, PBR: PBRStyle{Metallic: 1, Roughness: .3}}, color.RGBA{245, 190, 55, 255}},
		{"conductor-rough-srgb", false, &Material{Shading: ShadePBR, PBR: PBRStyle{Metallic: .9, Roughness: .75}}, color.RGBA{180, 190, 205, 255}},
		{"all-four-maps", true, &Material{
			Shading:  ShadePBR,
			PBR:      PBRStyle{Metallic: 1, Roughness: 1, OcclusionStrength: .8, ORMMap: ormTex},
			Texture:  baseTex,
			Tint:     true,
			Normal:   NormalStyle{Map: normTex, Strength: .7},
			Emission: EmissionStyle{Color: color.RGBA{255, 255, 255, 255}, Intensity: .35, Map: emTex},
		}, white},
		{"pbr-toon", true, &Material{
			Shading: ShadePBR,
			PBR:     PBRStyle{Metallic: .4, Roughness: .35},
			Toon:    ToonStyle{Bands: 3, Softness: .2, Rim: .25},
		}, color.RGBA{90, 175, 240, 255}},
		{"clearcoat-over-normalmap", true, &Material{
			Shading: ShadePBR,
			PBR:     PBRStyle{Metallic: .7, Roughness: .45, Clearcoat: ClearcoatStyle{Amount: 1, Roughness: .08}},
			Normal:  NormalStyle{Map: normTex, Strength: .8},
		}, color.RGBA{190, 30, 42, 255}},
		{"sheen-velvet", true, &Material{
			Shading: ShadePBR,
			PBR:     PBRStyle{Roughness: .84, Sheen: SheenStyle{Color: color.RGBA{245, 165, 255, 255}, Roughness: .35}},
		}, color.RGBA{56, 26, 78, 255}},
		{"pbr-toon-coat-sheen", true, &Material{
			Shading: ShadePBR,
			PBR:     PBRStyle{Roughness: .78, Clearcoat: ClearcoatStyle{Amount: .9, Roughness: .12}, Sheen: SheenStyle{Color: color.RGBA{165, 235, 255, 255}, Roughness: .42}},
			Toon:    ToonStyle{Bands: 3, Softness: .2, Rim: .2},
		}, color.RGBA{42, 88, 132, 255}},
		{"specular-zero-matte", true, &Material{
			Shading: ShadePBR,
			PBR:     PBRStyle{Roughness: .22, Specular: SpecularStyle{Off: true}},
		}, color.RGBA{195, 65, 55, 255}},
		{"specular-tinted-hdr", true, &Material{
			Shading: ShadePBR,
			PBR:     PBRStyle{Roughness: .18, Specular: SpecularStyle{Color: color.RGBA{255, 110, 45, 255}, Intensity: 2.5, Strength: 200. / 255}},
		}, color.RGBA{28, 36, 52, 255}},
		{"specular-black-grazing-f90", true, &Material{
			Shading: ShadePBR,
			PBR:     PBRStyle{Roughness: .14, Specular: SpecularStyle{Color: color.RGBA{0, 0, 0, 255}}},
		}, color.RGBA{24, 24, 30, 255}},
		{"specular-toon-tinted", true, &Material{
			Shading: ShadePBR,
			PBR:     PBRStyle{Roughness: .28, Specular: SpecularStyle{Color: color.RGBA{80, 220, 255, 255}, Intensity: 2}},
			Toon:    ToonStyle{Bands: 3, Softness: .2},
		}, color.RGBA{50, 35, 70, 255}},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewScene()
			ball, err := BuildSphere(Sphere{Radius: .85, Segments: 40, Rings: 24, Color: tc.col, Material: tc.mat})
			check(t, err)
			_, err = s.SetGeometry("ball", ball)
			check(t, err)

			cam := DefaultCamera()
			cam.Distance = 2.6
			f, err := s.Frame(cam, 96, 96)
			check(t, err)
			f.Revision = uint64(100 + i)
			f.Linear = tc.linear
			f.Background = color.RGBA{18, 20, 26, 255}
			f.Lighting = Lighting{
				Ambient: Ambient{Sky: color.RGBA{90, 105, 130, 255}, Ground: color.RGBA{45, 40, 35, 255}, Intensity: .5},
				Lights: []Light{
					{Kind: DirectionalLight, Direction: nt3dmath.Vec3{X: .6, Y: -.8, Z: .9}.Normalize(), Intensity: .9},
					{Kind: PointLight, Position: nt3dmath.Vec3{X: -1.6, Y: -1.4, Z: .8}, Color: color.RGBA{255, 210, 170, 255}, Intensity: 1.8, Range: 6},
				},
			}

			got, err := r.Render(f)
			check(t, err)
			want := softwareRender(f)

			for _, pt := range [][2]int{{48, 48}, {42, 42}, {54, 52}} {
				if !closeRGB(at(got, pt[0], pt[1]), at(want, pt[0], pt[1]), 6) {
					t.Fatalf("center sample %v mismatch: GPU=%v Software=%v", pt, at(got, pt[0], pt[1]), at(want, pt[0], pt[1]))
				}
			}
			if mad := meanAbsDiff(got, want); mad > 2.5 {
				t.Fatalf("mean abs diff %.2f exceeds tolerance", mad)
			}
		})
	}
}

func TestGPUPBREnvironmentIBLAndSessionSharing(t *testing.T) {
	_, _, dev := newTestHostDevice(t)
	sess, err := NewGPUSession(dev)
	check(t, err)
	defer sess.Close()

	v1, err := sess.NewView()
	check(t, err)
	defer v1.Close()
	v2, err := sess.NewView()
	check(t, err)
	defer v2.Close()

	env := testSharpEnvironment(t)
	s := NewScene()
	s.SetEnvironment(env)

	// Smooth vs rough chrome spheres placed side by side using SharedGeometry/SetGeometry.
	smooth, err := BuildSphere(Sphere{
		Radius:   .65,
		Segments: 36,
		Rings:    20,
		Color:    color.RGBA{245, 245, 250, 255},
		Material: &Material{Shading: ShadePBR, PBR: PBRStyle{Metallic: 1, Roughness: .06}},
	})
	check(t, err)
	rough, err := BuildSphere(Sphere{
		Radius:   .65,
		Segments: 36,
		Rings:    20,
		Color:    color.RGBA{245, 245, 250, 255},
		Material: &Material{Shading: ShadePBR, PBR: PBRStyle{Metallic: 1, Roughness: .85}},
	})
	check(t, err)

	nSmooth, err := s.SetGeometry("smooth", smooth)
	check(t, err)
	check(t, s.SetTransform(nSmooth, nt3dmath.Translation(nt3dmath.Vec3{X: -.8})))
	nRough, err := s.SetGeometry("rough", rough)
	check(t, err)
	check(t, s.SetTransform(nRough, nt3dmath.Translation(nt3dmath.Vec3{X: .8})))

	cam := DefaultCamera()
	cam.Elevation, cam.Azimuth, cam.Distance = 0, -90, 3.4
	f, err := s.Frame(cam, 128, 72)
	check(t, err)
	f.Lighting = Lighting{} // Pure environment IBL

	gpu1, err := v1.RenderToImage(f)
	check(t, err)
	gpu2, err := v2.RenderToImage(f)
	check(t, err)
	sw := softwareRender(f)

	if mad := meanAbsDiff(gpu1, gpu2); mad > 0.1 {
		t.Fatalf("shared GPU session views diverged: %.3f", mad)
	}
	if mad := meanAbsDiff(gpu1, sw); mad > 4.0 {
		t.Fatalf("environment IBL GPU vs Software mean abs diff %.2f too high", mad)
	}
}

func TestGPUProceduralORMAndHDREmissiveBloom(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()

	// Procedural ORM map with tiled UVs exercises GPU procDownShader mip chain generation.
	fbORM := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			fbORM.SetNRGBA(x, y, color.NRGBA{230, 64, 255, 255})
		}
	}
	procORM, err := NewShaderORMMap(
		"fn nt3dTexture(uv: vec2<f32>) -> vec4<f32> { return vec4<f32>(230.0/255.0, 64.0/255.0, 1.0, 1.0); }",
		fbORM,
	)
	check(t, err)

	fbEm := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			fbEm.SetNRGBA(x, y, color.NRGBA{255, 110, 25, 255})
		}
	}
	procEm, err := NewShaderTexture(
		"fn nt3dTexture(uv: vec2<f32>) -> vec4<f32> { return vec4<f32>(1.0, 110.0/255.0, 25.0/255.0, 1.0); }",
		fbEm,
	)
	check(t, err)

	g := quad(.3, .4, color.RGBA{220, 190, 120, 255}, &Material{
		Shading:  ShadePBR,
		PBR:      PBRStyle{Metallic: 1, Roughness: 1, ORMMap: procORM, ORMRepeat: true},
		Emission: EmissionStyle{Color: white, Intensity: 5.0, Map: procEm, Repeat: true},
	}, false)
	g.UV = []UV{{0, 0}, {8, 0}, {8, 8}, {0, 8}} // Force higher mip levels!

	f := flatFrame(96, 96, g)
	f.Forward = nt3dmath.Vec3{Z: -1}
	f.Background = color.RGBA{0, 0, 0, 255}
	f.Linear = true
	f.Lighting = Lighting{Lights: []Light{{Direction: nt3dmath.Vec3{X: .5, Z: 1}.Normalize(), Intensity: .5}}}
	f.PostProcess = PostProcess{
		Bloom:   Bloom{Strength: .75, Threshold: 1.0, Radius: 1},
		ToneMap: ACESToneMap,
	}

	got, err := r.Render(f)
	check(t, err)
	want := softwareRender(f)

	centerGPU, centerSW := at(got, 48, 48), at(want, 48, 48)
	haloGPU, haloSW := at(got, 48, 28), at(want, 48, 28)
	if centerGPU.R < 200 || haloGPU.R < 5 {
		t.Fatalf("expected GPU HDR emissive bloom: center=%v halo=%v", centerGPU, haloGPU)
	}
	if !closeRGB(centerGPU, centerSW, 10) || !closeRGB(haloGPU, haloSW, 15) {
		t.Fatalf("HDR bloom parity mismatch: center GPU=%v SW=%v, halo GPU=%v SW=%v", centerGPU, centerSW, haloGPU, haloSW)
	}

	// Verify PBR + Transmission refraction on GPU matches Software.
	back := quad(.85, .8, color.RGBA{220, 45, 45, 255}, &Material{Shading: ShadeUnlit}, false)
	glass := quad(.55, .2, white, &Material{Shading: ShadePBR, PBR: PBRStyle{Roughness: .1, IOR: 1.52}, Transmission: TransmissionStyle{Amount: .85, Refraction: .03}}, false)
	fTrans := flatFrame(80, 80, back, glass)
	fTrans.Forward = nt3dmath.Vec3{Z: -1}
	fTrans.Revision = 77
	fTrans.Linear = true
	fTrans.Lighting = Lighting{
		Ambient: Ambient{Sky: white, Ground: white, Intensity: .2},
		Lights:  []Light{{Direction: nt3dmath.Vec3{X: .4, Z: 1}.Normalize(), Intensity: .8}},
	}
	gotTrans, err := r.Render(fTrans)
	check(t, err)
	wantTrans := softwareRender(fTrans)
	if !closeRGB(at(gotTrans, 40, 40), at(wantTrans, 40, 40), 8) {
		t.Fatalf("PBR transmission parity: GPU=%v SW=%v", at(gotTrans, 40, 40), at(wantTrans, 40, 40))
	}
}

func TestGPUClearcoatIBLAndSetMaterialParity(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()

	env := testSharpEnvironment(t)
	normTex := testNormalMap(t, color.NRGBA{190, 110, 235, 255})
	s := NewScene()
	s.SetEnvironment(env)

	ball, err := BuildSphere(Sphere{
		Radius:   .8,
		Segments: 40,
		Rings:    24,
		Color:    color.RGBA{34, 48, 86, 255},
		Material: &Material{
			Shading: ShadePBR,
			PBR:     PBRStyle{Roughness: .75},
			Normal:  NormalStyle{Map: normTex, Strength: .75},
		},
	})
	check(t, err)
	ballID, err := s.SetGeometry("ball", ball)
	check(t, err)

	cam := DefaultCamera()
	cam.Elevation, cam.Azimuth, cam.Distance = 90, 0, 2.4
	f0, err := s.Frame(cam, 96, 96)
	check(t, err)
	f0.Linear = true
	f0.Lighting = Lighting{
		Lights: []Light{{Direction: nt3dmath.Vec3{X: .5, Y: .8, Z: .4}.Normalize(), Intensity: .8}},
	}
	_, err = r.Render(f0)
	check(t, err)
	uploadsBefore := r.uploads

	// Live ApplyMaterial adding Clearcoat + Sheen reuses GPU mesh buffers with zero re-uploads.
	check(t, s.ApplyMaterial(ballID, &Material{
		Shading: ShadePBR,
		PBR:     PBRStyle{Roughness: .75, Clearcoat: ClearcoatStyle{Amount: 1, Roughness: .06}, Sheen: SheenStyle{Color: color.RGBA{180, 210, 255, 255}, Roughness: .4}},
		Normal:  NormalStyle{Map: normTex, Strength: .75},
	}))
	f1, err := s.Frame(cam, 96, 96)
	check(t, err)
	f1.Linear = true
	f1.Lighting = f0.Lighting

	got, err := r.Render(f1)
	check(t, err)
	if r.uploads != uploadsBefore {
		t.Fatalf("ApplyMaterial clearcoat+sheen triggered %d GPU geometry uploads; want 0", r.uploads-uploadsBefore)
	}
	want := softwareRender(f1)
	if mad := meanAbsDiff(got, want); mad > 3.0 {
		t.Fatalf("clearcoat+sheen IBL GPU vs Software mean abs diff %.2f exceeds tolerance", mad)
	}
}

func TestGPUBaseColorAndMaterialVariantsParity(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()

	ball, err := BuildSphere(Sphere{
		Radius:   .55,
		Segments: 36,
		Rings:    20,
		Color:    white,
		Material: &Material{Shading: ShadePBR, PBR: PBRStyle{Roughness: .35}},
	})
	check(t, err)
	sg, err := NewSharedGeometry(ball)
	check(t, err)

	baseTex := testTexture(t, color.RGBA{225, 225, 225, 255})
	signMesh, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: .7, Y: .2, Z: .35}, Color: white, Material: &Material{Shading: ShadePBR, PBR: PBRStyle{Roughness: .4}, Texture: baseTex}})
	check(t, err)
	signSG, err := NewSharedGeometry(signMesh)
	check(t, err)

	asset := &SceneAsset{
		Variants: []string{"Taxi", "Police", "Glass"},
		Nodes: []AssetNode{
			{
				Name:   "Body",
				Parent: -1,
				Local:  nt3dmath.Identity(),
				Shared: sg,
				VariantMaterials: map[int]*Material{
					0: {Shading: ShadePBR, PBR: PBRStyle{Roughness: .22, Clearcoat: ClearcoatStyle{Amount: 1, Roughness: .08}}, BaseColor: color.RGBA{245, 190, 25, 255}},
					1: {Shading: ShadePBR, PBR: PBRStyle{Metallic: .85, Roughness: .28}, BaseColor: color.RGBA{45, 95, 220, 255}},
					2: {Shading: ShadePBR, PBR: PBRStyle{Roughness: .15}, Alpha: AlphaTransparent, BaseColor: color.RGBA{90, 220, 255, 140}},
				},
			},
			{
				Name:   "Sign",
				Parent: 0,
				Local:  nt3dmath.Translation(nt3dmath.Vec3{Z: .85}),
				Shared: signSG,
				VariantMaterials: map[int]*Material{
					0: {Shading: ShadePBR, PBR: PBRStyle{Roughness: .4}, Texture: baseTex, BaseColor: color.RGBA{255, 120, 30, 255}},
					1: {Shading: ShadePBR, PBR: PBRStyle{Roughness: .4}, Texture: baseTex, BaseColor: color.RGBA{60, 170, 255, 255}},
				},
			},
		},
	}

	s := NewScene()
	c0, err := asset.Instantiate(s, Root, "c0")
	check(t, err)
	c1, err := asset.Instantiate(s, Root, "c1")
	check(t, err)
	c2, err := asset.Instantiate(s, Root, "c2")
	check(t, err)
	check(t, s.SetTransform(c0.Root, nt3dmath.Translation(nt3dmath.Vec3{X: -1.35})))
	check(t, s.SetTransform(c2.Root, nt3dmath.Translation(nt3dmath.Vec3{X: 1.35})))

	cam := DefaultCamera()
	cam.Elevation, cam.Azimuth, cam.Distance = 90, 12, 4.4
	lighting := Lighting{
		Ambient: Ambient{Sky: color.RGBA{95, 110, 135, 255}, Ground: color.RGBA{45, 40, 35, 255}, Intensity: .55},
		Lights: []Light{
			{Kind: DirectionalLight, Direction: nt3dmath.Vec3{X: .5, Y: -.8, Z: .7}.Normalize(), Intensity: .95},
		},
	}

	f0, err := s.Frame(cam, 144, 96)
	check(t, err)
	f0.Linear = true
	f0.Background = color.RGBA{18, 22, 28, 255}
	f0.Lighting = lighting
	_, err = r.Render(f0)
	check(t, err)
	uploadsBefore := r.uploads

	// Switch c0 -> Taxi and c1 -> Police: zero GPU vertex buffer re-uploads,
	// and all 3 Body instances + all 3 Sign instances stay batched (2 instanced draws total)!
	check(t, s.SetMaterialVariant(c0.Root, "Taxi"))
	check(t, s.SetMaterialVariant(c1.Root, "Police"))
	f1, err := s.Frame(cam, 144, 96)
	check(t, err)
	f1.Linear = true
	f1.Background = f0.Background
	f1.Lighting = lighting

	got1, err := r.Render(f1)
	check(t, err)
	if r.uploads != uploadsBefore {
		t.Fatalf("SetMaterialVariant triggered %d GPU geometry uploads; want 0", r.uploads-uploadsBefore)
	}
	if len(r.draws) != 2 {
		t.Fatalf("expected 2 instanced GPU draw groups (3 bodies + 3 signs), got %d", len(r.draws))
	}
	want1 := softwareRender(f1)
	if mad := meanAbsDiff(got1, want1); mad > 2.5 {
		t.Fatalf("opaque variants GPU vs Software mean abs diff %.2f exceeds tolerance", mad)
	}

	// Switch c2 -> Glass (Transparent BaseColor alpha) and verify GPU vs Software parity.
	check(t, s.SetMaterialVariant(c2.Root, "Glass"))
	f2, err := s.Frame(cam, 144, 96)
	check(t, err)
	f2.Linear = true
	f2.Background = f0.Background
	f2.Lighting = lighting
	got2, err := r.Render(f2)
	check(t, err)
	want2 := softwareRender(f2)
	if mad := meanAbsDiff(got2, want2); mad > 2.5 {
		t.Fatalf("transparent variant GPU vs Software mean abs diff %.2f exceeds tolerance", mad)
	}
}
