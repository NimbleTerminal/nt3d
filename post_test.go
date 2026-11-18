// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func rgbaAt(img image.Image, x, y int) color.RGBA {
	return color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
}

func rgbaBytes(img image.Image) []byte {
	b := img.Bounds()
	out := make([]byte, b.Dx()*b.Dy()*4)
	i := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := rgbaAt(img, x, y)
			out[i], out[i+1], out[i+2], out[i+3] = c.R, c.G, c.B, c.A
			i += 4
		}
	}
	return out
}

func TestPostProcessValidation(t *testing.T) {
	base := edgeScene(t, 1)
	if err := validateFrame(base); err != nil {
		t.Fatalf("baseline frame invalid: %v", err)
	}

	valid := []PostProcess{
		{},
		{Exposure: -2.5, ToneMap: ReinhardToneMap, Vignette: 0.4},
		{Exposure: 3, ToneMap: ACESToneMap, Vignette: 1},
		{Bloom: Bloom{Strength: 1.2, Threshold: 0.8, Radius: 0.7}},
		{SSAO: SSAO{Strength: 0.75, Radius: 0.5, Bias: 0.03}},
	}
	for i, pp := range valid {
		f := base
		f.PostProcess = pp
		if err := validateFrame(f); err != nil {
			t.Fatalf("valid[%d] rejected: %v", i, err)
		}
	}

	invalid := []PostProcess{
		{Exposure: float32(math.NaN())},
		{Exposure: MaxExposure + 1},
		{Exposure: -MaxExposure - 1},
		{ToneMap: ToneMap(99)},
		{ToneMap: ACESToneMap + 1},
		{Vignette: -0.1},
		{Vignette: 1.1},
		{Bloom: Bloom{Strength: -0.1}},
		{Bloom: Bloom{Strength: MaxBloomStrength + 0.1}},
		{Bloom: Bloom{Strength: 0.5, Threshold: -1}},
		{Bloom: Bloom{Strength: 0.5, Radius: -0.1}},
		{Bloom: Bloom{Strength: 0.5, Radius: 1.1}},
		{SSAO: SSAO{Strength: -0.1}},
		{SSAO: SSAO{Strength: 1.1}},
		{SSAO: SSAO{Strength: 0.5, Radius: -0.1}},
		{SSAO: SSAO{Strength: 0.5, Bias: -0.01}},
		{SSAO: SSAO{Strength: 0.5, Bias: 1.0}},
	}
	for i, pp := range invalid {
		f := base
		f.PostProcess = pp
		if err := validateFrame(f); err == nil {
			t.Fatalf("invalid[%d] (%+v) accepted", i, pp)
		}
	}
}

func TestSoftwarePostProcessOffAndMSAAFallback(t *testing.T) {
	f := edgeScene(t, 1)
	base := softwareRender(f)

	f.MSAA = true
	msaaImg := softwareRender(f)
	if !bytes.Equal(rgbaBytes(base), rgbaBytes(msaaImg)) {
		t.Fatal("software MSAA fallback changed pixels")
	}

	f.PostProcess = PostProcess{Exposure: 1, ToneMap: ACESToneMap, Vignette: 0.5, Bloom: Bloom{Strength: 0.5}}
	if _, err := RenderWireframe(f, 40, 20); err != nil {
		t.Fatalf("wireframe failed with MSAA/PostProcess: %v", err)
	}
}

func flatQuadFrame(t *testing.T, col color.RGBA, unlit bool, lightIntensity float32) Frame {
	t.Helper()
	n := nt3dmath.Vec3{Z: 1}
	shading := ShadePhong
	if unlit {
		shading = ShadeUnlit
	}
	g := Geometry{
		Vertices: []Vertex{
			{Position: nt3dmath.Vec3{X: -3, Y: -3, Z: 0}, Normal: n, Color: col},
			{Position: nt3dmath.Vec3{X: 3, Y: -3, Z: 0}, Normal: n, Color: col},
			{Position: nt3dmath.Vec3{X: 3, Y: 3, Z: 0}, Normal: n, Color: col},
			{Position: nt3dmath.Vec3{X: -3, Y: 3, Z: 0}, Normal: n, Color: col},
		},
		Indices:  []uint32{0, 1, 2, 0, 2, 3},
		Material: &Material{Shading: shading},
	}
	s := NewScene()
	_, err := s.SetGeometry("quad", g)
	check(t, err)
	cam := Camera{
		Distance:   3,
		Elevation:  89,
		Azimuth:    -90,
		Projection: Perspective,
	}
	f, err := s.Frame(cam, 64, 64)
	check(t, err)
	f.Linear = true
	f.Lighting = Lighting{
		Lights: []Light{{
			Kind:      DirectionalLight,
			Direction: nt3dmath.Vec3{Z: 1},
			Color:     color.RGBA{255, 255, 255, 255},
			Intensity: lightIntensity,
		}},
	}
	return f
}

func TestSoftwareExposureToneMapVignetteAndOverlay(t *testing.T) {
	base := flatQuadFrame(t, color.RGBA{128, 128, 128, 255}, true, 1)
	raw := rgbaAt(softwareRender(base), 32, 32)

	bright := base
	bright.PostProcess = PostProcess{Exposure: 1}
	brightPx := rgbaAt(softwareRender(bright), 32, 32)

	dark := base
	dark.PostProcess = PostProcess{Exposure: -1}
	darkPx := rgbaAt(softwareRender(dark), 32, 32)

	if !(darkPx.R < raw.R && raw.R < brightPx.R) {
		t.Fatalf("exposure ordering broken: dark=%d raw=%d bright=%d", darkPx.R, raw.R, brightPx.R)
	}

	// HDR highlight > 1.0: NoToneMap clips to 255, Reinhard/ACES compress smoothly.
	hdr := flatQuadFrame(t, color.RGBA{255, 200, 120, 255}, false, 6)
	clipped := rgbaAt(softwareRender(hdr), 32, 32)
	if clipped.R != 255 {
		t.Fatalf("expected HDR clip to 255 without tone map, got %v", clipped)
	}
	reinhard := hdr
	reinhard.PostProcess = PostProcess{ToneMap: ReinhardToneMap}
	reinPx := rgbaAt(softwareRender(reinhard), 32, 32)
	if reinPx.R >= 250 || reinPx.R <= reinPx.B {
		t.Fatalf("unexpected Reinhard HDR compression: %v", reinPx)
	}
	aces := hdr
	aces.PostProcess = PostProcess{ToneMap: ACESToneMap}
	acesPx := rgbaAt(softwareRender(aces), 32, 32)
	if acesPx.R == 0 || acesPx.B >= acesPx.R {
		t.Fatalf("unexpected ACES HDR compression: %v", acesPx)
	}

	// Vignette darkens corners relative to center, while Overlays stay un-graded.
	vig := base
	overlayCol := color.RGBA{12, 234, 56, 255}
	vig.Overlays = []OverlayRect{{X: 2, Y: 2, Width: 6, Height: 6, Color: overlayCol}}
	vig.PostProcess = PostProcess{Exposure: -1.5, Vignette: 0.9}
	vigImg := softwareRender(vig)
	center := rgbaAt(vigImg, 32, 32)
	corner := rgbaAt(vigImg, 61, 61)
	if corner.R >= center.R {
		t.Fatalf("vignette did not darken corner: center=%v corner=%v", center, corner)
	}
	if got := rgbaAt(vigImg, 4, 4); got != overlayCol {
		t.Fatalf("overlay altered by post-processing: got %v, want %v", got, overlayCol)
	}
}

func emitterFrame(t *testing.T, intensity float32) Frame {
	t.Helper()
	n := nt3dmath.Vec3{Z: 1}
	white := color.RGBA{255, 255, 255, 255}
	g := Geometry{
		Vertices: []Vertex{
			{Position: nt3dmath.Vec3{X: -0.22, Y: -0.22, Z: 0}, Normal: n, Color: white},
			{Position: nt3dmath.Vec3{X: 0.22, Y: -0.22, Z: 0}, Normal: n, Color: white},
			{Position: nt3dmath.Vec3{X: 0.22, Y: 0.22, Z: 0}, Normal: n, Color: white},
			{Position: nt3dmath.Vec3{X: -0.22, Y: 0.22, Z: 0}, Normal: n, Color: white},
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	}
	s := NewScene()
	_, err := s.SetGeometry("emitter", g)
	check(t, err)
	cam := Camera{Distance: 3, Elevation: 89, Azimuth: -90, Projection: Perspective}
	f, err := s.Frame(cam, 80, 80)
	check(t, err)
	f.Linear = true
	f.Background = color.RGBA{0, 0, 0, 255}
	f.Lighting = Lighting{
		Lights: []Light{{
			Kind:      DirectionalLight,
			Direction: nt3dmath.Vec3{Z: 1},
			Color:     white,
			Intensity: intensity,
		}},
	}
	return f
}

func haloSum(img image.Image) int {
	b := img.Bounds()
	cx, cy := b.Dx()/2, b.Dy()/2
	sum := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dx, dy := x-cx, y-cy
			r2 := dx*dx + dy*dy
			if r2 >= 14*14 && r2 <= 24*24 {
				c := rgbaAt(img, x, y)
				sum += int(c.R) + int(c.G) + int(c.B)
			}
		}
	}
	return sum
}

func TestSoftwareBloom(t *testing.T) {
	f := emitterFrame(t, 5)
	noBloom := haloSum(softwareRender(f))
	if noBloom != 0 {
		t.Fatalf("expected black background outside emitter without bloom, got %d", noBloom)
	}

	f.PostProcess = PostProcess{Bloom: Bloom{Strength: 1.0, Threshold: 0.8, Radius: 0.75}, ToneMap: ACESToneMap}
	withBloom := haloSum(softwareRender(f))
	if withBloom < 200 {
		t.Fatalf("expected visible bloom halo around bright emitter, got sum=%d", withBloom)
	}

	dim := emitterFrame(t, 0.25)
	dim.PostProcess = PostProcess{Bloom: Bloom{Strength: 1.0, Threshold: 0.9, Radius: 0.75}}
	if got := haloSum(softwareRender(dim)); got != 0 {
		t.Fatalf("dim emitter below threshold should not bloom, got sum=%d", got)
	}
}

// cornerSSAOFrame builds a white floor (z = 0) meeting a vertical back wall
// (y = 0) along a horizontal crease across the middle of the frame.
func cornerSSAOFrame(t *testing.T) Frame {
	t.Helper()
	white := color.RGBA{235, 235, 235, 255}
	floor := Geometry{
		Vertices: []Vertex{
			{Position: nt3dmath.Vec3{X: -3, Y: -3, Z: 0}, Normal: nt3dmath.Vec3{Z: 1}, Color: white},
			{Position: nt3dmath.Vec3{X: 3, Y: -3, Z: 0}, Normal: nt3dmath.Vec3{Z: 1}, Color: white},
			{Position: nt3dmath.Vec3{X: 3, Y: 0, Z: 0}, Normal: nt3dmath.Vec3{Z: 1}, Color: white},
			{Position: nt3dmath.Vec3{X: -3, Y: 0, Z: 0}, Normal: nt3dmath.Vec3{Z: 1}, Color: white},
		},
		Indices:  []uint32{0, 1, 2, 0, 2, 3},
		Material: &Material{},
	}
	wall := Geometry{
		Vertices: []Vertex{
			{Position: nt3dmath.Vec3{X: -3, Y: 0, Z: 0}, Normal: nt3dmath.Vec3{Y: -1}, Color: white},
			{Position: nt3dmath.Vec3{X: 3, Y: 0, Z: 0}, Normal: nt3dmath.Vec3{Y: -1}, Color: white},
			{Position: nt3dmath.Vec3{X: 3, Y: 0, Z: 3}, Normal: nt3dmath.Vec3{Y: -1}, Color: white},
			{Position: nt3dmath.Vec3{X: -3, Y: 0, Z: 3}, Normal: nt3dmath.Vec3{Y: -1}, Color: white},
		},
		Indices:  []uint32{0, 1, 2, 0, 2, 3},
		Material: &Material{},
	}
	s := NewScene()
	_, err := s.SetGeometry("floor", floor)
	check(t, err)
	_, err = s.SetGeometry("wall", wall)
	check(t, err)
	cam := Camera{
		Target:     nt3dmath.Vec3{X: 0, Y: 0, Z: 0},
		Distance:   3.2,
		Elevation:  35,
		Azimuth:    -90,
		Projection: Perspective,
	}
	f, err := s.Frame(cam, 96, 96)
	check(t, err)
	f.Linear = true
	f.Lighting = Lighting{Ambient: Ambient{Sky: color.RGBA{255, 255, 255, 255}, Ground: color.RGBA{255, 255, 255, 255}}}
	f.Background = color.RGBA{40, 80, 160, 255}
	return f
}

func TestSoftwareSSAOTieredFallback(t *testing.T) {
	f := cornerSSAOFrame(t)
	f.PostProcess = PostProcess{SSAO: SSAO{Strength: 0.9, Radius: 0.6}}

	// Interactive software frames (Complete == false) skip CPU SSAO.
	f.Complete = false
	fastImg := softwareRender(f)
	if rgbaAt(fastImg, 48, 48).R != rgbaAt(fastImg, 48, 22).R {
		t.Fatalf("expected interactive software frame to skip SSAO: crease=%v open=%v", rgbaAt(fastImg, 48, 48), rgbaAt(fastImg, 48, 22))
	}

	// Complete software frames (e.g. Snapshot) evaluate SSAO and darken the crease.
	aoImg, _, err := Snapshot(f, Software, nil)
	check(t, err)
	crease := rgbaAt(aoImg, 48, 48).R
	openWall := rgbaAt(aoImg, 48, 22).R
	openFloor := rgbaAt(aoImg, 48, 82).R
	if !(crease+18 < openWall && crease+18 < openFloor) {
		t.Fatalf("expected SSAO to darken corner crease: crease=%d wall=%d floor=%d", crease, openWall, openFloor)
	}

	// Supersampled complete software frame also evaluates SSAO cleanly.
	f.Complete = true
	f.Supersample = 2
	ssImg := softwareRender(f)
	if rgbaAt(ssImg, 48, 48).R+15 >= rgbaAt(ssImg, 48, 22).R {
		t.Fatalf("expected supersampled SSAO to darken crease: crease=%d wall=%d", rgbaAt(ssImg, 48, 48).R, rgbaAt(ssImg, 48, 22).R)
	}
}
