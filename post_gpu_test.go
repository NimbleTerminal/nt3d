// Copyright (c) 2026 Neomantra Corp.

//go:build webgpu

package nt3d

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/gogpu/gputypes"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func nrgbaBytes(img image.Image) []byte {
	b := img.Bounds()
	out := make([]byte, b.Dx()*b.Dy()*4)
	i := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			out[i], out[i+1], out[i+2], out[i+3] = c.R, c.G, c.B, c.A
			i += 4
		}
	}
	return out
}

func TestGPUMSAA(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()

	f1 := edgeScene(t, 1)
	img1, err := r.Render(f1)
	check(t, err)
	hardEdge, hardRed := shades(img1)

	f4 := f1
	f4.MSAA = true
	img4, err := r.Render(f4)
	check(t, err)
	msaaEdge, msaaRed := shades(img4)

	if hardEdge > 10 || msaaEdge < 40 {
		t.Fatalf("expected 4x MSAA to smooth diagonal edge: 1x=%d, 4x=%d", hardEdge, msaaEdge)
	}
	if hardRed < 20 || msaaRed < hardRed*2/3 || msaaRed > hardRed*3/2 {
		t.Fatalf("point coverage changed unexpectedly under MSAA: 1x=%d, 4x=%d", hardRed, msaaRed)
	}

	// Switch back to 1x to verify pipeline/target switching on the same renderer.
	img1Again, err := r.Render(f1)
	check(t, err)
	againEdge, _ := shades(img1Again)
	if againEdge > 10 {
		t.Fatalf("switching from 4x MSAA back to 1x kept smoothed edges: %d", againEdge)
	}
}

func TestGPUMSAAAllPipelinesAndSession(t *testing.T) {
	_, _, dev := newTestHostDevice(t)
	sess, err := NewGPUSession(dev)
	check(t, err)
	defer sess.Close()

	view, err := sess.NewView()
	check(t, err)
	defer view.Close()

	s := NewScene()
	white := color.RGBA{255, 255, 255, 255}
	checker := testTexture(t, color.RGBA{220, 90, 40, 255})

	// Opaque mesh + outline + crease + boxes + lines + arrows.
	rich := Geometry{
		Vertices: []Vertex{
			{Position: nt3dmath.Vec3{X: -0.7, Y: -0.6, Z: -0.2}, Normal: nt3dmath.Vec3{Z: 1}, Color: white},
			{Position: nt3dmath.Vec3{X: 0.7, Y: -0.6, Z: -0.2}, Normal: nt3dmath.Vec3{Z: 1}, Color: white},
			{Position: nt3dmath.Vec3{X: 0.0, Y: 0.7, Z: -0.2}, Normal: nt3dmath.Vec3{Z: 1}, Color: white},
			{Position: nt3dmath.Vec3{X: 0.7, Y: -0.6, Z: -0.2}, Normal: nt3dmath.Vec3{X: 1}, Color: white},
			{Position: nt3dmath.Vec3{X: 0.0, Y: 0.7, Z: -0.2}, Normal: nt3dmath.Vec3{X: 1}, Color: white},
			{Position: nt3dmath.Vec3{X: 0.3, Y: 0.0, Z: -0.8}, Normal: nt3dmath.Vec3{X: 1}, Color: white},
		},
		Indices:   []uint32{0, 1, 2, 3, 5, 4},
		Outline:   OutlineStyle{Width: 2, Color: color.RGBA{10, 10, 10, 255}, Crease: 30},
		Boxes:     []Box{{Min: nt3dmath.Vec3{X: -0.9, Y: 0.2, Z: -0.3}, Size: nt3dmath.Vec3{X: 0.25, Y: 0.25, Z: 0.25}, Color: color.RGBA{80, 180, 255, 255}}},
		Lines:     []Vertex{{Position: nt3dmath.Vec3{X: -0.8, Y: -0.8, Z: 0.1}, Color: white}, {Position: nt3dmath.Vec3{X: 0.8, Y: -0.8, Z: 0.1}, Color: white}},
		LineWidth: 2,
		Arrows:    []Arrow{{Start: nt3dmath.Vec3{X: -0.8, Y: 0.7, Z: 0.1}, End: nt3dmath.Vec3{X: 0.2, Y: 0.7, Z: 0.1}, Color: white, Width: 2, HeadSize: 6}},
	}
	_, err = s.SetGeometry("rich", rich)
	check(t, err)

	// Textured & cutout meshes.
	texMesh, err := BuildHeightfield(Heightfield{NX: 2, NY: 2, MinX: 0.2, MaxX: 0.8, MinY: 0.1, MaxY: 0.7, Heights: make([]float32, 4), Material: &Material{Texture: checker}})
	check(t, err)
	_, err = s.SetGeometry("tex", texMesh)
	check(t, err)

	cutMesh, err := BuildHeightfield(Heightfield{NX: 2, NY: 2, MinX: -0.8, MaxX: -0.3, MinY: -0.7, MaxY: -0.2, Heights: make([]float32, 4), Material: &Material{Texture: checker, Cutout: true}})
	check(t, err)
	_, err = s.SetGeometry("cut", cutMesh)
	check(t, err)

	// Transmissive glass (exercises sceneSnapshot + captureShaderMSAA + resumeScene).
	glass, err := BuildHeightfield(Heightfield{NX: 2, NY: 2, MinX: -0.3, MaxX: 0.3, MinY: -0.3, MaxY: 0.3, Heights: []float32{0.2, 0.2, 0.2, 0.2}, Material: &Material{Transmission: 0.75, Refraction: 0.04}})
	check(t, err)
	_, err = s.SetGeometry("glass", glass)
	check(t, err)

	// Transparent mesh + sorted alpha points + sprites.
	transMesh, err := BuildHeightfield(Heightfield{NX: 2, NY: 2, MinX: 0.1, MaxX: 0.5, MinY: -0.5, MaxY: -0.1, Heights: []float32{0.35, 0.35, 0.35, 0.35}, Material: &Material{Transparent: true}})
	check(t, err)
	for i := range transMesh.Vertices {
		transMesh.Vertices[i].Color = color.RGBA{120, 200, 255, 130}
	}
	_, err = s.SetGeometry("trans", transMesh)
	check(t, err)

	pts := Geometry{
		Points:     []Point{{Position: nt3dmath.Vec3{X: -0.2, Y: 0.3, Z: 0.4}, Radius: 5, Color: color.RGBA{255, 80, 120, 200}}},
		PointStyle: PointStyle{Blend: AlphaBlend, Shape: SoftPoint},
	}
	_, err = s.SetGeometry("pts", pts)
	check(t, err)

	spr := Geometry{
		Points:     []Point{{Position: nt3dmath.Vec3{X: 0.4, Y: -0.3, Z: 0.4}, Radius: 5, Color: white}},
		PointStyle: PointStyle{Blend: AlphaBlend, Texture: checker},
	}
	_, err = s.SetGeometry("spr", spr)
	check(t, err)

	pano := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for i := 0; i < len(pano.Pix); i += 4 {
		pano.Pix[i], pano.Pix[i+1], pano.Pix[i+2], pano.Pix[i+3] = 40, 70, 120, 255
	}
	env, err := NewEnvironment(pano)
	check(t, err)

	cam := Camera{Distance: 3.0, Alpha: 85, Beta: -90, Projection: Perspective}
	f, err := s.Frame(cam, 96, 96)
	check(t, err)
	f.Environment = env
	f.DynamicLines = rich.Lines
	f.Overlays = []OverlayRect{{X: 2, Y: 2, Width: 6, Height: 6, Color: color.RGBA{7, 8, 9, 255}}}

	// Render 1x then 4x MSAA on primary session, then 4x MSAA on shared view.
	_, err = sess.RenderToImage(f)
	check(t, err)

	f.MSAA = true
	img, err := sess.RenderToImage(f)
	check(t, err)
	if got := rgbaAt(img, 4, 4); got != (color.RGBA{7, 8, 9, 255}) {
		t.Fatalf("overlay mismatch under MSAA: got %v", got)
	}

	// ReadDepth on the 4x MSAA depth buffer hits the center geometry.
	world, depth, ok, err := sess.ReadDepth(f, 48, 48, 2)
	check(t, err)
	if !ok || depth <= 0 || depth >= 1 || math.IsNaN(float64(world.Z)) {
		t.Fatalf("expected valid MSAA ReadDepth hit at center: ok=%v depth=%v world=%v", ok, depth, world)
	}

	viewImg, err := view.RenderToImage(f)
	check(t, err)
	if !bytes.Equal(nrgbaBytes(img), nrgbaBytes(viewImg)) {
		t.Fatal("shared view MSAA output differs from primary session")
	}
}

func TestGPUPostProcess(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()

	// 1. Off is bit-identical to baseline.
	f := flatQuadFrame(t, color.RGBA{140, 110, 90, 255}, true, 1)
	baseImg, err := r.Render(f)
	check(t, err)

	// 2. Exposure + ACES/Reinhard + Vignette matches software closely.
	for _, tm := range []ToneMap{NoToneMap, ReinhardToneMap, ACESToneMap} {
		graded := f
		graded.PostProcess = PostProcess{Exposure: 0.6, ToneMap: tm, Vignette: 0.7}
		gpuImg, err := r.Render(graded)
		check(t, err)
		cpuImg := softwareRender(graded)
		for _, pt := range [][2]int{{32, 32}, {10, 10}, {60, 60}} {
			gc := rgbaAt(gpuImg, pt[0], pt[1])
			cc := rgbaAt(cpuImg, pt[0], pt[1])
			if absInt(int(gc.R)-int(cc.R)) > 3 || absInt(int(gc.G)-int(cc.G)) > 3 || absInt(int(gc.B)-int(cc.B)) > 3 {
				t.Fatalf("toneMap=%v at %v: gpu=%v cpu=%v", tm, pt, gc, cc)
			}
		}
	}

	// Confirm PostProcess{} after active post-processing restores exact baseline bytes.
	afterImg, err := r.Render(f)
	check(t, err)
	if !bytes.Equal(nrgbaBytes(baseImg), nrgbaBytes(afterImg)) {
		t.Fatal("rendering with PostProcess{} after post-processing did not match baseline")
	}

	// 3. GPU HDR Bloom glows around bright emitter and ignores dim emitter.
	emit := emitterFrame(t, 6)
	rawEmit, err := r.Render(emit)
	check(t, err)
	if got := haloSum(rawEmit); got != 0 {
		t.Fatalf("unexpected halo without bloom: %d", got)
	}
	emit.PostProcess = PostProcess{
		Bloom:   Bloom{Strength: 1.0, Threshold: 0.8, Radius: 0.75},
		ToneMap: ACESToneMap,
	}
	bloomEmit, err := r.Render(emit)
	check(t, err)
	if got := haloSum(bloomEmit); got < 400 {
		t.Fatalf("expected GPU bloom halo around bright emitter, got %d", got)
	}

	// 4. GPU SSAO darkens concave corner crease at 1x, 4x MSAA, and 2x Supersample,
	// without darkening sky background, and preserves Overlays un-graded.
	overlayCol := color.RGBA{12, 234, 56, 255}
	for _, cfg := range []struct {
		name string
		msaa bool
		ss   int
	}{
		{"1x", false, 1},
		{"4xMSAA", true, 1},
		{"2xSS", false, 2},
		{"4xMSAA+2xSS", true, 2},
	} {
		cf := cornerSSAOFrame(t)
		cf.MSAA = cfg.msaa
		cf.Supersample = cfg.ss
		cf.Overlays = []OverlayRect{{X: 2, Y: 2, Width: 6, Height: 6, Color: overlayCol}}

		unoccluded, err := r.Render(cf)
		check(t, err)
		skyBefore := rgbaAt(unoccluded, 48, 2)

		cf.PostProcess = PostProcess{SSAO: SSAO{Strength: 0.9, Radius: 0.6}}
		occluded, err := r.Render(cf)
		check(t, err)

		crease := rgbaAt(occluded, 48, 48).R
		openWall := rgbaAt(occluded, 48, 22).R
		openFloor := rgbaAt(occluded, 48, 82).R
		skyAfter := rgbaAt(occluded, 48, 2)
		ov := rgbaAt(occluded, 4, 4)

		if !(crease+18 < openWall && crease+18 < openFloor) {
			t.Fatalf("%s: expected SSAO to darken corner crease: crease=%d wall=%d floor=%d", cfg.name, crease, openWall, openFloor)
		}
		if skyAfter != skyBefore {
			t.Fatalf("%s: SSAO altered background sky: before=%v after=%v", cfg.name, skyBefore, skyAfter)
		}
		if ov != overlayCol {
			t.Fatalf("%s: overlay altered by post-processing: got %v, want %v", cfg.name, ov, overlayCol)
		}
	}
}

func TestGPUSessionPostProcessViewportAndLoad(t *testing.T) {
	_, _, dev := newTestHostDevice(t)
	sess, err := NewGPUSession(dev)
	check(t, err)
	defer sess.Close()

	tex, view := createHostTarget(t, dev, 128, 128, gputypes.TextureFormatRGBA8Unorm)

	// Fill full target with solid green using a base frame.
	bgScene := NewScene()
	cam := Camera{Distance: 3, Alpha: 89, Beta: -90, Projection: Perspective}
	bgFrame, err := bgScene.Frame(cam, 128, 128)
	check(t, err)
	bgFrame.Background = color.RGBA{20, 180, 40, 255}
	check(t, sess.Render(bgFrame, GPUTarget{
		Color:  view,
		Format: gputypes.TextureFormatRGBA8Unorm,
		Width:  128,
		Height: 128,
	}))

	// Render an MSAA + PostProcess + Overlay sub-viewport with transparent background and Load=true.
	sub := emitterFrame(t, 5)
	sub.Width, sub.Height = 64, 64
	sub.Background = color.RGBA{0, 0, 0, 0}
	sub.MSAA = true
	sub.Overlays = []OverlayRect{{X: 4, Y: 4, Width: 8, Height: 8, Color: color.RGBA{250, 10, 200, 255}}}
	sub.PostProcess = PostProcess{
		Bloom:   Bloom{Strength: 0.8, Threshold: 0.8},
		ToneMap: ACESToneMap,
	}
	check(t, sess.Render(sub, GPUTarget{
		Color:    view,
		Format:   gputypes.TextureFormatRGBA8Unorm,
		Width:    128,
		Height:   128,
		Viewport: image.Rect(32, 32, 96, 96),
		Load:     true,
	}))

	out := readHostTarget(t, dev, tex, 128, 128, gputypes.TextureFormatRGBA8Unorm, false)
	// Outside sub-viewport stays untouched green.
	if got := out.NRGBAAt(10, 10); got != (color.NRGBA{20, 180, 40, 255}) {
		t.Fatalf("outside viewport modified: %v", got)
	}
	// Transparent corner inside sub-viewport preserves underlying green via Load=true.
	if got := out.NRGBAAt(92, 92); got.G < 160 || got.R > 35 {
		t.Fatalf("transparent background inside Load=true sub-viewport did not preserve green: %v", got)
	}
	// Overlay inside sub-viewport is drawn crisp at (32+6, 32+6) = (38, 38).
	if got := out.NRGBAAt(38, 38); got != (color.NRGBA{250, 10, 200, 255}) {
		t.Fatalf("overlay in Load=true sub-viewport mismatch: %v", got)
	}
	// Emitter center at (64, 64) is bright white.
	if got := out.NRGBAAt(64, 64); got.R < 200 || got.G < 200 || got.B < 200 {
		t.Fatalf("emitter center missing in sub-viewport: %v", got)
	}
}

func TestGPUSSAOLighting(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	testSSAOLighting(t, r.Render)
}

func TestGPUSSAOResize(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	f := cornerSSAOFrame(t)
	f.PostProcess.SSAO = SSAO{Strength: .9, Radius: .6}
	for _, size := range []int{96, 80, 96} {
		f.Width, f.Height = size, size
		img, err := r.Render(f)
		check(t, err)
		if img.Bounds().Dx() != size {
			t.Fatal("incorrect resized AO frame")
		}
		// Compare to a fresh renderer, catching stale AO textures and bindings.
		fresh := &gpuRenderer{}
		want, err := fresh.Render(f)
		check(t, err)
		check(t, fresh.Close())
		if !bytes.Equal(nrgbaBytes(img), nrgbaBytes(want)) {
			t.Fatal("resized AO differs from fresh render")
		}
	}
}
