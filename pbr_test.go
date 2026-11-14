// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image"
	"image/color"
	"math"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func testORMMap(t *testing.T, c color.NRGBA) *Texture {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			img.SetNRGBA(x, y, c)
		}
	}
	tex, err := NewORMMap(img)
	check(t, err)
	return tex
}

func testSharpEnvironment(t *testing.T) *Environment {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	for y := range 32 {
		for x := range 64 {
			c := color.NRGBA{12, 16, 24, 255}
			if x >= 30 && x <= 33 && y >= 14 && y <= 17 {
				c = color.NRGBA{255, 240, 200, 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	env, err := NewEnvironment(img)
	check(t, err)
	return env
}

func pbrQuadFrame(m *Material, col color.RGBA) Frame {
	g := quad(.9, .4, col, m, false)
	g.UV = []UV{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	f := flatFrame(80, 80, g)
	f.Forward = nt3dmath.Vec3{Z: -1}
	f.Linear = true
	return f
}

func TestPBRMetallicEnergyConservationAndTint(t *testing.T) {
	gold := color.RGBA{240, 180, 40, 255}

	// Off-axis grazing light where N·H is far from 1 on a low-roughness surface:
	// a dielectric keeps diffuse shading, whereas a pure conductor (Metallic=1)
	// suppresses diffuse (1 - metallic = 0).
	offLight := Lighting{
		Lights: []Light{{Direction: nt3dmath.Vec3{X: 1, Z: 1}.Normalize(), Intensity: 1}},
	}
	dielectric := pbrQuadFrame(&Material{PBR: true, Metallic: 0, Roughness: .15}, gold)
	dielectric.Lighting = offLight
	dPx := at(softwareRender(dielectric), 40, 40)

	metalOff := pbrQuadFrame(&Material{PBR: true, Metallic: 1, Roughness: .15}, gold)
	metalOff.Lighting = offLight
	mOffPx := at(softwareRender(metalOff), 40, 40)

	if dPx.R < 120 || mOffPx.R > 25 {
		t.Fatalf("expected conductor to suppress off-specular diffuse: dielectric=%v, metal=%v", dPx, mOffPx)
	}

	// Head-on white light (L == V == N == +Z): conductor F0 is tinted by gold (R > G >> B).
	headOn := Lighting{
		Lights: []Light{{Direction: nt3dmath.Vec3{Z: 1}, Intensity: .08}},
	}
	metalOn := pbrQuadFrame(&Material{PBR: true, Metallic: 1, Roughness: .35}, gold)
	metalOn.Lighting = headOn
	mOnPx := at(softwareRender(metalOn), 40, 40)
	if mOnPx.R <= mOnPx.G || int(mOnPx.G) <= int(mOnPx.B)*2 {
		t.Fatalf("expected conductor specular highlight to take gold F0 tint: %v", mOnPx)
	}
}

func TestPBRRoughnessAndIOR(t *testing.T) {
	headOn := Lighting{
		Lights: []Light{{Direction: nt3dmath.Vec3{Z: 1}, Intensity: .25}},
	}
	smooth := pbrQuadFrame(&Material{PBR: true, Metallic: 1, Roughness: .12}, white)
	smooth.Lighting = headOn
	rough := pbrQuadFrame(&Material{PBR: true, Metallic: 1, Roughness: .85}, white)
	rough.Lighting = headOn

	sPx := at(softwareRender(smooth), 40, 40)
	rPx := at(softwareRender(rough), 40, 40)
	if sPx.R <= rPx.R+80 {
		t.Fatalf("expected low-roughness GGX peak (%v) to exceed rough lobe (%v)", sPx, rPx)
	}

	// Pure black base color isolates dielectric Fresnel specular reflection at normal incidence.
	black := color.RGBA{0, 0, 0, 255}
	ior1 := pbrQuadFrame(&Material{PBR: true, Metallic: 0, Roughness: .2, IOR: 1.0}, black)
	ior1.Lighting = Lighting{Lights: []Light{{Direction: nt3dmath.Vec3{Z: 1}, Intensity: 1}}}
	iorDiamond := pbrQuadFrame(&Material{PBR: true, Metallic: 0, Roughness: .2, IOR: 2.42}, black)
	iorDiamond.Lighting = ior1.Lighting

	p1 := at(softwareRender(ior1), 40, 40)
	pD := at(softwareRender(iorDiamond), 40, 40)
	if p1.R != 0 || pD.R < 100 {
		t.Fatalf("expected IOR=1.0 zero reflection (%v) and IOR=2.42 strong reflection (%v)", p1, pD)
	}
}

func TestPBREnvironmentPrefilteredIBL(t *testing.T) {
	env := testSharpEnvironment(t)
	levels := env.specularLevels()
	if len(levels) < 4 || levels[0] != env.panorama {
		t.Fatalf("expected lazy GGX mip chain with level 0 == panorama, got %d levels", len(levels))
	}

	// Check the equirectangular direction pointing directly at the bright hotspot.
	// Roughness 0 samples level 0 sharply; roughness 0.9 blurs across surrounding dark texels.
	var hotDir nt3dmath.Vec3
	var best float32
	for _, d := range []nt3dmath.Vec3{{X: 1}, {X: -1}, {Y: 1}, {Y: -1}} {
		c := env.sampleSpecular(d, 0)
		if c[0] > best {
			best, hotDir = c[0], d
		}
	}
	sharp := env.sampleSpecular(hotDir, 0)
	blurry := env.sampleSpecular(hotDir, .9)
	if sharp[0] <= blurry[0]*2 {
		t.Fatalf("expected prefiltered IBL to blur hotspot at high roughness: sharp=%v, blurry=%v", sharp, blurry)
	}
}

func TestORMMapAndSceneSetters(t *testing.T) {
	// Occlusion (R=0) attenuates ambient light according to OcclusionStrength, leaving direct light intact.
	ormOcc := testORMMap(t, color.NRGBA{0, 128, 0, 255})
	ambientOnly := Lighting{Ambient: Ambient{Sky: white, Ground: white, Intensity: .8}}
	fullOcc := pbrQuadFrame(&Material{PBR: true, Roughness: 1, ORMMap: ormOcc, OcclusionStrength: 1}, white)
	fullOcc.Lighting = ambientOnly
	halfOcc := pbrQuadFrame(&Material{PBR: true, Roughness: 1, ORMMap: ormOcc, OcclusionStrength: .5}, white)
	halfOcc.Lighting = ambientOnly

	cFull := at(softwareRender(fullOcc), 40, 40)
	cHalf := at(softwareRender(halfOcc), 40, 40)
	if cFull.R > 2 || cHalf.R < 100 {
		t.Fatalf("expected ORM R=0 to occlude ambient proportionally: full=%v, half=%v", cFull, cHalf)
	}

	directOnly := Lighting{Lights: []Light{{Direction: nt3dmath.Vec3{Z: 1}, Intensity: .6}}}
	fullOcc.Lighting = directOnly
	if cDir := at(softwareRender(fullOcc), 40, 40); cDir.R < 150 {
		t.Fatalf("ambient occlusion should not darken direct light: %v", cDir)
	}

	// ORM B channel modulates Metallic, G channel modulates Roughness.
	ormMetalSmooth := testORMMap(t, color.NRGBA{255, 32, 255, 255})
	ormDielectricRough := testORMMap(t, color.NRGBA{255, 255, 0, 255})
	fMetal := pbrQuadFrame(&Material{PBR: true, Metallic: 1, Roughness: 1, ORMMap: ormMetalSmooth}, color.RGBA{240, 180, 40, 255})
	fDielectric := pbrQuadFrame(&Material{PBR: true, Metallic: 1, Roughness: 1, ORMMap: ormDielectricRough}, color.RGBA{240, 180, 40, 255})
	offLight := Lighting{Lights: []Light{{Direction: nt3dmath.Vec3{X: 1, Z: 1}.Normalize(), Intensity: 1}}}
	fMetal.Lighting = offLight
	fDielectric.Lighting = offLight
	if at(softwareRender(fMetal), 40, 40).R >= at(softwareRender(fDielectric), 40, 40).R {
		t.Fatal("ORM B/G channels did not modulate metallic/roughness")
	}

	// Scene.SetORMMap and Scene.SetEmissiveMap bump TextureRevision without bumping Revision.
	s := NewScene()
	em1 := testTexture(t, color.RGBA{200, 40, 10, 255})
	em2 := testTexture(t, color.RGBA{20, 180, 220, 255})
	g := fullOcc.Geometry[0]
	g.Material.Emissive = white
	g.Material.EmissiveMap = em1
	_, err := s.SetGeometry("mesh", g)
	check(t, err)
	before, err := s.Frame(DefaultCamera(), 64, 64)
	check(t, err)
	check(t, s.SetORMMap("mesh", ormMetalSmooth))
	mid, err := s.Frame(DefaultCamera(), 64, 64)
	check(t, err)
	if before.Revision != mid.Revision || before.TextureRevision == mid.TextureRevision || mid.Geometry[0].Material.ORMMap != ormMetalSmooth {
		t.Fatal("SetORMMap invalidated geometry revision or missed texture revision")
	}
	check(t, s.SetEmissiveMap("mesh", em2))
	after, err := s.Frame(DefaultCamera(), 64, 64)
	check(t, err)
	if mid.Revision != after.Revision || mid.TextureRevision == after.TextureRevision || after.Geometry[0].Material.EmissiveMap != em2 {
		t.Fatal("SetEmissiveMap invalidated geometry revision or missed texture revision")
	}

	// Procedural ORM constructor creates linear-data texture.
	procORM, err := NewShaderORMMap("fn nt3dTexture(uv: vec2<f32>) -> vec4<f32> { return vec4<f32>(1.0, 0.3, 1.0, 1.0); }", image.NewNRGBA(image.Rect(0, 0, 2, 2)))
	check(t, err)
	if !procORM.data {
		t.Fatal("NewShaderORMMap did not mark texture as linear data")
	}
}

func TestEmissiveHDRBloomAndCombinations(t *testing.T) {
	// Emissive surface in pitch black room glows and triggers HDR Bloom into neighboring background pixels.
	emMap := testTexture(t, color.RGBA{255, 120, 30, 255})
	g := quad(.25, .4, color.RGBA{0, 0, 0, 255}, &Material{
		PBR:               true,
		Roughness:         .5,
		Emissive:          color.RGBA{255, 255, 255, 255},
		EmissiveIntensity: 6,
		EmissiveMap:       emMap,
	}, false)
	g.UV = []UV{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	f := flatFrame(80, 80, g)
	f.Background = color.RGBA{0, 0, 0, 255}
	f.Linear = true
	f.Lighting = Lighting{}
	f.PostProcess = PostProcess{
		Bloom:   Bloom{Strength: .8, Threshold: 1.0, Radius: 1},
		ToneMap: ACESToneMap,
	}
	img := softwareRender(f)
	center := at(img, 40, 40)
	halo := at(img, 40, 22) // Outside the .25 quad, lit only by HDR bloom bleed.
	if center.R < 200 || halo.R < 5 {
		t.Fatalf("expected HDR emissive center (%v) to bleed bloom halo (%v)", center, halo)
	}

	// Unlit + Emissive adds glow without lighting.
	gUnlit := g
	mUnlit := *g.Material
	mUnlit.Unlit = true
	mUnlit.PBR = false
	gUnlit.Material = &mUnlit
	fUnlit := flatFrame(64, 64, gUnlit)
	fUnlit.Lighting = Lighting{}
	if c := at(softwareRender(fUnlit), 32, 32); c.R < 200 {
		t.Fatalf("expected unlit surface with Emissive to glow: %v", c)
	}

	// PBR + Toon + Rim and PBR + Transmission validate and render cleanly.
	sphereToon, err := BuildSphere(Sphere{
		Radius:   .6,
		Color:    color.RGBA{80, 160, 240, 255},
		Material: &Material{PBR: true, Metallic: .2, Roughness: .35, Toon: ToonStyle{Bands: 3, Rim: .3}},
	})
	check(t, err)
	check(t, sphereToon.Validate())
	_ = softwareRender(flatFrame(64, 64, sphereToon))

	back := quad(.8, .8, color.RGBA{220, 40, 40, 255}, &Material{Unlit: true}, false)
	glass := quad(.6, .2, white, &Material{PBR: true, Roughness: .1, IOR: 1.52, Transmission: .85, Refraction: .03}, false)
	fTrans := flatFrame(64, 64, back, glass)
	fTrans.Linear = true
	fTrans.Lighting = Lighting{
		Ambient: Ambient{Sky: white, Ground: white, Intensity: .2},
		Lights:  []Light{{Direction: nt3dmath.Vec3{Z: 1}, Intensity: .8}},
	}
	if c := at(softwareRender(fTrans), 32, 32); c.R < 140 {
		t.Fatalf("expected PBR transmissive glass to refract red backdrop: %v", c)
	}
}

func TestPBRValidation(t *testing.T) {
	base, err := BuildSphere(Sphere{Radius: .5})
	check(t, err)
	orm := testORMMap(t, color.NRGBA{255, 128, 0, 255})
	srgb := testTexture(t, white)

	badMaterials := []*Material{
		{PBR: true, Metallic: -0.1},
		{PBR: true, Metallic: 1.1},
		{PBR: true, Roughness: -0.01},
		{PBR: true, Roughness: 1.01},
		{PBR: true, IOR: 0.9},
		{PBR: true, IOR: 3.1},
		{PBR: true, IOR: float32(math.NaN())},
		{PBR: true, OcclusionStrength: -0.1},
		{PBR: true, OcclusionStrength: 1.1},
		{EmissiveIntensity: -0.5},
		{PBR: true, ORMMap: srgb},           // sRGB color texture rejected in ORM slot
		{Emissive: white, EmissiveMap: orm}, // Linear data texture rejected in EmissiveMap slot
	}
	for i, m := range badMaterials {
		g := base
		g.Material = m
		if g.Validate() == nil {
			t.Fatalf("case %d (%+v): expected Validate error", i, m)
		}
	}

	noUV := base
	noUV.UV = nil
	noUV.Material = &Material{PBR: true, ORMMap: orm}
	if noUV.Validate() == nil {
		t.Fatal("accepted ORMMap without UVs")
	}
	noUV.Material = &Material{Emissive: white, EmissiveMap: srgb}
	if noUV.Validate() == nil {
		t.Fatal("accepted EmissiveMap without UVs")
	}
}
