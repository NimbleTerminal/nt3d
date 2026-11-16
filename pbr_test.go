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
		{Clearcoat: -0.05},
		{Clearcoat: 1.05},
		{Clearcoat: float32(math.NaN())},
		{Clearcoat: 0.5, ClearcoatRoughness: -0.1},
		{Clearcoat: 0.5, ClearcoatRoughness: 1.1},
		{SheenColor: white, SheenRoughness: -0.01},
		{SheenColor: white, SheenRoughness: 1.01},
		{SheenColor: white, SheenRoughness: float32(math.Inf(1))},
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

	s := NewScene()
	ballID, err := s.SetGeometry("ball", base)
	check(t, err)
	for i, m := range badMaterials[:10] {
		if s.SetMaterial(ballID, m) == nil {
			t.Fatalf("SetMaterial case %d (%+v): expected validation error", i, m)
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

func TestPBRClearcoatGeometricNormalAndIBL(t *testing.T) {
	// Clearcoat > 0 activates PBR automatically even with PBR: false.
	if !pbrActive(&Material{Clearcoat: .8}) {
		t.Fatal("Clearcoat > 0 did not activate PBR")
	}

	// Tilted normal map deflects the base layer's shading normal away from head-on light,
	// whereas Clearcoat evaluates along the unperturbed geometric surface normal (+Z).
	tiltedNormal := testNormalMap(t, color.NRGBA{235, 128, 170, 255})
	headOn := Lighting{Lights: []Light{{Direction: nt3dmath.Vec3{Z: 1}, Intensity: 1.2}}}
	black := color.RGBA{0, 0, 0, 255}

	unclear := pbrQuadFrame(&Material{
		PBR:            true,
		Roughness:      .08,
		NormalMap:      tiltedNormal,
		NormalStrength: 1,
	}, black)
	unclear.Lighting = headOn

	coated := pbrQuadFrame(&Material{
		Roughness:          .85,
		Clearcoat:          1,
		ClearcoatRoughness: .08,
		NormalMap:          tiltedNormal,
		NormalStrength:     1,
	}, black)
	coated.Lighting = headOn

	pUncoated := at(softwareRender(unclear), 40, 40)
	pCoated := at(softwareRender(coated), 40, 40)
	if pUncoated.R > 30 || pCoated.R < 160 {
		t.Fatalf("expected clearcoat to keep crisp highlight on geometric normal over tilted normal map: uncoated=%v coated=%v", pUncoated, pCoated)
	}

	// Energy conservation: off-specular diffuse on a rough dielectric is attenuated by (1 - cc * Fc).
	offLight := Lighting{Lights: []Light{{Direction: nt3dmath.Vec3{X: .8, Z: .6}.Normalize(), Intensity: 1}}}
	matte := pbrQuadFrame(&Material{PBR: true, Roughness: .9}, white)
	matte.Lighting = offLight
	matteCoated := pbrQuadFrame(&Material{PBR: true, Roughness: .9, Clearcoat: 1, ClearcoatRoughness: .06}, white)
	matteCoated.Lighting = offLight
	if at(softwareRender(matteCoated), 40, 40).R >= at(softwareRender(matte), 40, 40).R {
		t.Fatal("expected clearcoat Fresnel transmission to attenuate underlying off-specular diffuse")
	}

	// Clearcoat environment IBL: smooth coat over rough dark base reflects bright environment hotspot.
	env := testSharpEnvironment(t)
	s := NewScene()
	s.SetEnvironment(env)
	dullBall, err := BuildSphere(Sphere{
		Radius:   .75,
		Segments: 40,
		Rings:    24,
		Color:    color.RGBA{18, 20, 24, 255},
		Material: &Material{PBR: true, Roughness: .95},
	})
	check(t, err)
	coatBall := dullBall
	coatBall.Material = &Material{PBR: true, Roughness: .95, Clearcoat: 1, ClearcoatRoughness: .05}
	ballID, err := s.SetGeometry("ball", dullBall)
	check(t, err)
	cam := DefaultCamera()
	cam.Alpha, cam.Beta, cam.Distance = 90, 0, 2.2
	fDull, err := s.Frame(cam, 80, 80)
	check(t, err)
	fDull.Lighting = Lighting{}

	revBefore := fDull.Revision
	check(t, s.SetMaterial(ballID, coatBall.Material))
	fCoat, err := s.Frame(cam, 80, 80)
	check(t, err)
	fCoat.Lighting = Lighting{}
	if fCoat.Revision != revBefore {
		t.Fatal("SetMaterial with Clearcoat unexpectedly bumped geometry Revision")
	}

	var maxDull, maxCoat uint8
	imgDull, imgCoat := softwareRender(fDull), softwareRender(fCoat)
	for y := 20; y < 60; y++ {
		for x := 20; x < 60; x++ {
			maxDull = max(maxDull, at(imgDull, x, y).R)
			maxCoat = max(maxCoat, at(imgCoat, x, y).R)
		}
	}
	if maxCoat <= maxDull+15 {
		t.Fatalf("expected clearcoat IBL reflection peak (%d) above rough base (%d)", maxCoat, maxDull)
	}
}

func TestPBRSheenAndToonCoatSheen(t *testing.T) {
	velvetTint := color.RGBA{245, 110, 210, 255}
	if !pbrActive(&Material{SheenColor: velvetTint}) {
		t.Fatal("nonzero SheenColor did not activate PBR")
	}

	// Grazing velvet sheen: dark rough sphere lit from front stays dark face-on
	// (N·H ≈ 1 -> Charlie sin²θ ≈ 0) and brightens with velvet tint toward silhouette.
	buildVelvet := func(sheen color.RGBA, toon ToonStyle) Frame {
		ball, err := BuildSphere(Sphere{
			Radius:   .85,
			Segments: 48,
			Rings:    32,
			Color:    color.RGBA{16, 16, 20, 255},
			Material: &Material{
				Roughness:      .88,
				SheenColor:     sheen,
				SheenRoughness: .35,
				Toon:           toon,
			},
		})
		check(t, err)
		s := NewScene()
		_, err = s.SetGeometry("ball", ball)
		check(t, err)
		cam := DefaultCamera()
		cam.Alpha, cam.Beta, cam.Distance = 0, 0, 2.4
		f, err := s.Frame(cam, 96, 96)
		check(t, err)
		f.Linear = true
		f.Background = color.RGBA{0, 0, 0, 255}
		f.Lighting = Lighting{
			Lights: []Light{{Direction: f.Eye.Normalize(), Intensity: 1.4}},
		}
		return f
	}

	imgNoSheen := softwareRender(buildVelvet(color.RGBA{}, ToonStyle{}))
	imgSheen := softwareRender(buildVelvet(velvetTint, ToonStyle{}))
	center := at(imgSheen, 48, 48)
	// Find brightest grazing pixel near the sphere's horizontal limb.
	var limbSheen, limbBase color.RGBA
	for x := 62; x <= 78; x++ {
		if c := at(imgSheen, x, 48); c.R > limbSheen.R {
			limbSheen = c
			limbBase = at(imgNoSheen, x, 48)
		}
	}
	if limbSheen.R <= center.R+40 || limbSheen.R <= limbBase.R+50 || limbSheen.R <= limbSheen.G {
		t.Fatalf("expected grazing magenta velvet sheen: center=%v limbSheen=%v limbBase=%v", center, limbSheen, limbBase)
	}

	// PBR + Toon: quantized sheen band near silhouette and quantized clearcoat lacquer highlight.
	imgToonBase := softwareRender(buildVelvet(color.RGBA{}, ToonStyle{Bands: 3}))
	imgToonSheen := softwareRender(buildVelvet(velvetTint, ToonStyle{Bands: 3}))
	var toonLimbBase, toonLimbSheen uint8
	for x := 62; x <= 78; x++ {
		toonLimbBase = max(toonLimbBase, at(imgToonBase, x, 48).R)
		toonLimbSheen = max(toonLimbSheen, at(imgToonSheen, x, 48).R)
	}
	if toonLimbSheen <= toonLimbBase+60 {
		t.Fatalf("expected PBR+Toon sheen band near silhouette: base=%d sheen=%d", toonLimbBase, toonLimbSheen)
	}

	// PBR + Toon Clearcoat: rough cel-shaded sphere gains crisp secondary lacquer spot at center.
	buildToonCoat := func(cc float32) color.RGBA {
		ball, err := BuildSphere(Sphere{
			Radius:   .85,
			Segments: 40,
			Rings:    24,
			Color:    color.RGBA{180, 40, 40, 255},
			Material: &Material{
				PBR:                true,
				Roughness:          .9,
				Clearcoat:          cc,
				ClearcoatRoughness: .1,
				Toon:               ToonStyle{Bands: 3},
			},
		})
		check(t, err)
		s := NewScene()
		_, err = s.SetGeometry("ball", ball)
		check(t, err)
		cam := DefaultCamera()
		cam.Alpha, cam.Beta, cam.Distance = 0, 0, 2.4
		f, err := s.Frame(cam, 80, 80)
		check(t, err)
		f.Linear = true
		f.Lighting = Lighting{Lights: []Light{{Direction: f.Eye.Normalize(), Intensity: 2.5}}}
		return at(softwareRender(f), 40, 40)
	}
	noCoatPx := buildToonCoat(0)
	coatPx := buildToonCoat(1)
	if coatPx.G <= noCoatPx.G+25 || coatPx.B <= noCoatPx.B+25 {
		t.Fatalf("expected PBR+Toon clearcoat white dielectric highlight over red base: uncoated=%v coated=%v", noCoatPx, coatPx)
	}
}

func TestPBRDielectricSpecularColorAndIntensity(t *testing.T) {
	if !pbrActive(&Material{SpecularColor: color.RGBA{255, 0, 0, 255}}) {
		t.Fatal("nonzero SpecularColor did not activate PBR")
	}
	if !pbrActive(&Material{SpecularIntensity: 2}) {
		t.Fatal("SpecularIntensity > 0 did not activate PBR")
	}
	if err := validateMaterial(&Material{SpecularIntensity: -.1}); err == nil {
		t.Fatal("accepted negative SpecularIntensity")
	}
	if err := validateMaterial(&Material{SpecularIntensity: float32(math.NaN())}); err == nil {
		t.Fatal("accepted NaN SpecularIntensity")
	}

	headOn := Lighting{Lights: []Light{{Direction: nt3dmath.Vec3{Z: 1}, Intensity: 1.5}}}
	black := color.RGBA{0, 0, 0, 255}

	// Default dielectric vs specularFactor = 0 (chalk/matte) vs tinted/HDR dielectric F0.
	fStd := pbrQuadFrame(&Material{PBR: true, Roughness: .1}, black)
	fStd.Lighting = headOn
	fZero := pbrQuadFrame(&Material{Roughness: .1, SpecularColor: color.RGBA{255, 255, 255, 0}}, black)
	fZero.Lighting = headOn
	fRed := pbrQuadFrame(&Material{Roughness: .1, SpecularColor: color.RGBA{255, 0, 0, 255}}, black)
	fRed.Lighting = headOn
	fHDR := pbrQuadFrame(&Material{Roughness: .1, SpecularIntensity: 4}, black)
	fHDR.Lighting = headOn

	pxStd := at(softwareRender(fStd), 40, 40)
	pxZero := at(softwareRender(fZero), 40, 40)
	pxRed := at(softwareRender(fRed), 40, 40)
	pxHDR := at(softwareRender(fHDR), 40, 40)

	if pxZero != black {
		t.Fatalf("specularFactor=0 dielectric on black base produced highlight %v", pxZero)
	}
	if pxStd.R < 80 {
		t.Fatalf("expected standard dielectric highlight, got %v", pxStd)
	}
	if pxRed.R < 80 || pxRed.G > 5 || pxRed.B > 5 {
		t.Fatalf("expected pure red dielectric F0 highlight, got %v", pxRed)
	}
	if pxHDR.R <= pxStd.R+40 {
		t.Fatalf("expected SpecularIntensity=4 (%v) brighter than default F0 (%v)", pxHDR, pxStd)
	}

	// specularColorFactor = [0, 0, 0] with specularFactor = 1 (color.RGBA{A: 255})
	// suppresses normal-incidence F0 while preserving grazing Fresnel F90 = 1,
	// whereas specularFactor = 0 (color.RGBA{R: 255, G: 255, B: 255, A: 0}) suppresses grazing too.
	buildGrazing := func(specCol color.RGBA) (center, limb color.RGBA) {
		ball, err := BuildSphere(Sphere{
			Radius:   .85,
			Segments: 48,
			Rings:    32,
			Color:    black,
			Material: &Material{
				Roughness:     .22,
				SpecularColor: specCol,
			},
		})
		check(t, err)
		s := NewScene()
		id, err := s.SetGeometry("ball", ball)
		check(t, err)
		cam := DefaultCamera()
		cam.Alpha, cam.Beta, cam.Distance = 0, 0, 2.4
		f0, err := s.Frame(cam, 96, 96)
		check(t, err)
		rev := f0.Revision
		// Verify SetMaterial changing SpecularColor preserves mesh Revision (instance uniform only).
		check(t, s.SetMaterial(id, &Material{Roughness: .22, SpecularColor: specCol}))
		f, err := s.Frame(cam, 96, 96)
		check(t, err)
		if f.Revision != rev {
			t.Fatal("SetMaterial changing SpecularColor unexpectedly bumped geometry Revision")
		}
		f.Linear = true
		f.Background = black
		eyeDir := f.Eye.Normalize()
		rimDir := eyeDir.Scale(-.75).Add(nt3dmath.Vec3{Y: .66}).Normalize()
		f.Lighting = Lighting{
			Lights: []Light{
				{Direction: eyeDir, Intensity: 1.5},
				{Direction: rimDir, Intensity: 3.0},
			},
		}
		img := softwareRender(f)
		center = at(img, 48, 48)
		for x := 62; x <= 80; x++ {
			if c := at(img, x, 48); c.R > limb.R {
				limb = c
			}
		}
		return center, limb
	}

	cZeroF0, lZeroF0 := buildGrazing(color.RGBA{A: 255})
	cZeroAll, lZeroAll := buildGrazing(color.RGBA{R: 255, G: 255, B: 255, A: 0})
	if cZeroF0 != black || cZeroAll != black || lZeroAll.R != 0 || lZeroF0.R < 40 {
		t.Fatalf("expected black SpecularColor with A=255 to keep grazing Fresnel over A=0: center=%v/%v limbF90=%v limbZero=%v", cZeroF0, cZeroAll, lZeroF0, lZeroAll)
	}
}
