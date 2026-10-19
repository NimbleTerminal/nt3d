// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
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

// coverage is the fraction of an image's texels with alpha at or above
// threshold.
func coverage(img *image.NRGBA, threshold uint8) float64 {
	n, above := 0, 0
	for i := 3; i < len(img.Pix); i += 4 {
		n++
		if img.Pix[i] >= threshold {
			above++
		}
	}
	return float64(above) / float64(n)
}

// Each cutout level keeps level 0's share of texels above every threshold,
// where the box average alone cuts the strands away.
func TestCutoutLevelsPreserveCoverage(t *testing.T) {
	tex, err := NewTexture(strandImage(128, 8, 4))
	check(t, err)
	plain, cut := tex.levels(), tex.cutoutLevels()
	if &plain[0].Pix[0] != &cut[0].Pix[0] {
		t.Fatal("level 0 is not shared")
	}
	for _, threshold := range []uint8{64, 128, 192} {
		want := coverage(plain[0], threshold)
		for l := 1; l <= 3; l++ { // 64², 32², 16² texels.
			if got := coverage(cut[l], threshold); math.Abs(got-want) > .04 {
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

// Binary leaves (alpha 0 or 255) keep their share too.
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
	for l := 1; l <= 3; l++ {
		if got := coverage(tex.cutoutLevels()[l], 128); math.Abs(got-want) > .05 {
			t.Errorf("level %d: coverage %.3f, level 0 %.3f", l, got, want)
		}
	}
}

// An opaque texture reuses the ordinary chain; an all-transparent one stays
// transparent; a level whose texels are all alike shows when level 0 covers
// at least half.
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
	for opaqueTexels, want := range map[int]uint8{1: 0, 2: 255, 3: 255} {
		img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		for i := 0; i < opaqueTexels; i++ {
			img.Pix[i*4+3] = 255
		}
		tex, err = NewTexture(img)
		check(t, err)
		if got := tex.cutoutLevels()[1].Pix[3]; got != want {
			t.Errorf("%d of 4 texels opaque: 1×1 level alpha %d, want %d", opaqueTexels, got, want)
		}
	}
}
