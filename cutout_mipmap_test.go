// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"testing"
)

// strandImage is a fur-like texture: one strand per cell×cell cell, grown
// somewhere in its middle half with a radius within a quarter of radius,
// whose alpha is the height it reaches at each texel, 1 at its center
// falling to 0 at its edge, as FurCoat.strands makes them.
func strandImage(size, cell int, radius float64) *image.NRGBA {
	rng := rand.New(rand.NewPCG(1, 2))
	cells := size / cell
	type strand struct{ x, y, r float64 }
	strands := make([]strand, cells*cells)
	for i := range strands {
		strands[i] = strand{(float64(i%cells) + .25 + .5*rng.Float64()) * float64(cell), (float64(i/cells) + .25 + .5*rng.Float64()) * float64(cell), radius * (.75 + .5*rng.Float64())}
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			s := strands[min(cells-1, y/cell)*cells+min(cells-1, x/cell)]
			d := math.Hypot(float64(x)+.5-s.x, float64(y)+.5-s.y)
			img.SetNRGBA(x, y, color.NRGBA{200, 160, 90, uint8(math.Round(255 * max(0, 1-d/s.r)))})
		}
	}
	return img
}

// coverage is the fraction of an image that a renderer draws at threshold:
// of samples filtered bilinearly at sixteen phases over every texel, as a
// renderer samples a level at any magnification, the share with alpha at
// or above threshold.
func coverage(img *image.NRGBA, threshold uint8) float64 {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	alpha := func(x, y int) float64 {
		return float64(img.Pix[max(0, min(h-1, y))*img.Stride+max(0, min(w-1, x))*4+3])
	}
	n, above := 0, 0
	for y := range h {
		for x := range w {
			for _, fy := range []float64{0, .25, .5, .75} {
				for _, fx := range []float64{0, .25, .5, .75} {
					top := alpha(x, y)*(1-fx) + alpha(x+1, y)*fx
					bottom := alpha(x, y+1)*(1-fx) + alpha(x+1, y+1)*fx
					n++
					if math.Round(top*(1-fy)+bottom*fy) >= float64(threshold) {
						above++
					}
				}
			}
		}
	}
	return float64(above) / float64(n)
}

// Each cutout level keeps level 0's coverage at every threshold, within a
// few percent from one half up, and never thinner below it (a coarse
// level's edge ramps cover a little more of the low range), where the box
// average alone cuts the strands away.
func TestCutoutLevelsPreserveCoverage(t *testing.T) {
	tex, err := NewTexture(strandImage(128, 8, 4))
	check(t, err)
	plain, cut := tex.levels(), tex.cutoutLevels()
	if &plain[0].Pix[0] != &cut[0].Pix[0] {
		t.Fatal("level 0 is not shared")
	}
	for _, threshold := range []uint8{64, 128, 192} {
		want := coverage(plain[0], threshold)
		over := .04
		if threshold < 128 {
			over = .1
		}
		for l := 1; l <= 3; l++ { // 64², 32², 16² texels.
			if got := coverage(cut[l], threshold); got < want-.04 || got > want+over {
				t.Errorf("threshold %d level %d: coverage %.3f, level 0 %.3f", threshold, l, got, want)
			}
			for i := 0; i < len(cut[l].Pix); i += 4 {
				if cut[l].Pix[i] != plain[l].Pix[i] || cut[l].Pix[i+1] != plain[l].Pix[i+1] || cut[l].Pix[i+2] != plain[l].Pix[i+2] {
					t.Fatalf("level %d: color changed at texel %d", l, i/4)
				}
			}
		}
	}
	// The ordinary chain averages the strands under one half by level 3.
	if got := coverage(plain[3], 128); got > .05 {
		t.Fatalf("plain level 3 coverage %.3f: the box filter should have cut the strands", got)
	}
}

// Binary leaves (alpha 0 or 255) keep their share too, down to the level
// whose texels are as wide as the leaves.
func TestCutoutLevelsBinary(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			a := uint8(0)
			if (x/4+y/4)%3 == 0 { // One cell in three, 4 texels square.
				a = 255
			}
			img.SetNRGBA(x, y, color.NRGBA{40, 120, 40, a})
		}
	}
	tex, err := NewTexture(img)
	check(t, err)
	want := coverage(tex.levels()[0], 128)
	for l := 1; l <= 2; l++ { // 32² and 16² texels: leaves 2 and 1 texels wide.
		if got := coverage(tex.cutoutLevels()[l], 128); math.Abs(got-want) > .05 {
			t.Errorf("level %d: coverage %.3f, level 0 %.3f", l, got, want)
		}
	}
}

// An opaque texture reuses the ordinary chain; an all-transparent one stays
// transparent; a level whose texels are all alike shows when level 0 covers
// more than half and cuts when it covers less.
func TestCutoutLevelsEdges(t *testing.T) {
	opaque := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for i := range opaque.Pix {
		opaque.Pix[i] = 255
	}
	tex, err := NewTexture(opaque)
	check(t, err)
	if &tex.cutoutLevels()[1].Pix[0] != &tex.levels()[1].Pix[0] {
		t.Fatal("an opaque texture should share the ordinary chain")
	}
	tex, err = NewTexture(image.NewNRGBA(image.Rect(0, 0, 8, 8)))
	check(t, err)
	for l, level := range tex.cutoutLevels() {
		if coverage(level, 1) != 0 {
			t.Fatalf("level %d of a transparent texture shows", l)
		}
	}
	for opaqueTexels, shown := range map[int]bool{1: false, 3: true} {
		img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		for i := 0; i < opaqueTexels; i++ {
			img.Pix[i*4+3] = 255
		}
		tex, err = NewTexture(img)
		check(t, err)
		if got := tex.cutoutLevels()[1].Pix[3]; (got >= 128) != shown {
			t.Errorf("%d of 4 texels opaque: 1×1 level alpha %d", opaqueTexels, got)
		}
	}
}

// strandFloor is a strand texture tiled over a floor seen from above at a
// tilt, cut at one half, red on a black background, from a camera turned
// beta degrees. Rows between farTop and farBottom of the height see the
// floor far enough that a pixel covers a few texels.
func strandFloor(t *testing.T, m Material, beta float64) Frame {
	t.Helper()
	tex, err := NewTexture(strandImage(128, 8, 4))
	check(t, err)
	m.Texture, m.Cutout, m.Unlit, m.Repeat = tex, true, true, true
	floor, err := BuildHeightfield(Heightfield{NX: 2, NY: 2, MinX: -20, MaxX: 20, MinY: -20, MaxY: 20, Heights: make([]float32, 4), Material: &m, Color: color.RGBA{230, 40, 30, 255},
		UV: []UV{{0, 0}, {50, 0}, {0, 50}, {50, 50}}})
	check(t, err)
	s := NewScene()
	_, err = s.SetGeometry("floor", floor)
	check(t, err)
	camera := DefaultCamera()
	camera.Projection, camera.Alpha, camera.Beta, camera.Distance = Perspective, 35, beta, 2.5
	f, err := s.Frame(camera, 160, 120)
	check(t, err)
	f.Background = color.RGBA{0, 0, 0, 255}
	return f
}

const farTop, farBottom = .2, .4

// drawnShare is the fraction of pixels that are not black in the rows from top
// to bottom of the image's height.
func drawnShare(img image.Image, top, bottom float64) float64 {
	b := img.Bounds()
	n, lit := 0, 0
	for y := int(top * float64(b.Dy())); y < int(bottom*float64(b.Dy())); y++ {
		for x := range b.Dx() {
			n++
			if c := at(img, x, y); c.R > 20 || c.G > 20 || c.B > 20 {
				lit++
			}
		}
	}
	return float64(lit) / float64(n)
}

// changed counts the pixels in the rows from top to bottom that differ
// between two images.
func changed(a, b image.Image, top, bottom float64) int {
	r := a.Bounds()
	n := 0
	for y := int(top * float64(r.Dy())); y < int(bottom*float64(r.Dy())); y++ {
		for x := range r.Dx() {
			if at(a, x, y) != at(b, x, y) {
				n++
			}
		}
	}
	return n
}

// Far off, a mipmapped cutout keeps about the density it has near by, and
// stops flickering: a small camera move changes few of its far pixels,
// where full size changes most of them.
func TestSoftwareCutoutMipsKeepDensity(t *testing.T) {
	img := softwareRender(strandFloor(t, Material{}, 40))
	near, far := drawnShare(img, .85, 1), drawnShare(img, farTop, farBottom)
	if near < .1 || far < .5*near || far > 1.5*near {
		t.Fatalf("drawn: near %.2f, far %.2f", near, far)
	}
	flips := changed(img, softwareRender(strandFloor(t, Material{}, 40.05)), farTop, farBottom)
	full := softwareRender(strandFloor(t, Material{FullSize: true}, 40))
	if fullFlips := changed(full, softwareRender(strandFloor(t, Material{FullSize: true}, 40.05)), farTop, farBottom); flips > fullFlips/2 {
		t.Fatalf("far pixels changed by a small move: mipmapped %d, full size %d", flips, fullFlips)
	}
}

// A shader cutout samples full size on both renderers; FullSize is level 0.
func TestCutoutShaderTextureIsFullSize(t *testing.T) {
	f := strandFloor(t, Material{FullSize: true}, 40)
	shader, err := NewShaderTexture("fn nt3dTexture(uv: vec2<f32>) -> vec4<f32> { return vec4<f32>(1.0); }", strandImage(128, 8, 4))
	check(t, err)
	g := strandFloor(t, Material{}, 40)
	g.Geometry[0].Material.Texture = shader
	if !bytes.Equal(softwareRender(f).(*image.RGBA).Pix, softwareRender(g).(*image.RGBA).Pix) {
		t.Fatal("a shader cutout differs from the same picture sampled full size")
	}
}
