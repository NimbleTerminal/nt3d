// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image"
	"image/color"
	"testing"
)

// stripes is a texture red on its left half and blue on its right.
func stripes(t *testing.T) *Texture {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 2))
	for y := range 2 {
		for x := range 8 {
			c := color.NRGBA{230, 30, 30, 255}
			if x >= 4 {
				c = color.NRGBA{30, 30, 230, 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	tex, err := NewTexture(img)
	check(t, err)
	return tex
}

func TestSampleWrap(t *testing.T) {
	tex := stripes(t)
	// At u = 0 a repeating texture blends its last texel with its first, as
	// the next tile's start does: no seam. Clamped, it is the first.
	if c := tex.sampleWrap(UV{0, .5}, true); c.R != 130 || c.B != 130 {
		t.Fatalf("wrapped edge %v, want an even blend", c)
	}
	if c := tex.sampleWrap(UV{0, .5}, false); c != (color.NRGBA{230, 30, 30, 255}) {
		t.Fatalf("clamped edge %v", c)
	}
	// Whole tiles away, a repeating texture is the same.
	for _, u := range []float32{.3, 2.3, -1.7} {
		if tex.sampleWrap(UV{u, .5}, true) != tex.sampleWrap(UV{.3, .5}, true) {
			t.Fatalf("u=%v differs from u=0.3", u)
		}
	}
}

// repeatScene is a quad seen from above whose UVs run 0..3 across, so a
// repeating texture shows three tiles.
func repeatScene(t *testing.T, repeat bool) Frame {
	t.Helper()
	g, err := BuildHeightfield(Heightfield{NX: 2, NY: 2, MinX: -.8, MaxX: .8, MinY: -.8, MaxY: .8, Heights: make([]float32, 4),
		UV: []UV{{0, 1}, {3, 1}, {0, 0}, {3, 0}}, Material: &Material{Texture: stripes(t), Unlit: true, Repeat: repeat}})
	check(t, err)
	s := NewScene()
	_, err = s.SetGeometry("quad", g)
	check(t, err)
	camera := DefaultCamera()
	camera.Alpha, camera.Beta = 89, -90
	f, err := s.Frame(camera, 240, 160)
	check(t, err)
	return f
}

// bands counts runs of red and of blue along the middle row.
func bands(img image.Image) int {
	b := img.Bounds()
	y, n, last := (b.Min.Y+b.Max.Y)/2, 0, ' '
	for x := b.Min.X; x < b.Max.X; x++ {
		c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
		k := ' '
		switch {
		case c.R > 180 && c.B < 80:
			k = 'r'
		case c.B > 180 && c.R < 80:
			k = 'b'
		}
		if k != ' ' && k != last {
			n++
		}
		if k != ' ' {
			last = k
		}
	}
	return n
}

func TestSoftwareRepeat(t *testing.T) {
	if n := bands(softwareRender(repeatScene(t, true))); n != 6 {
		t.Fatalf("%d bands repeating, want 6", n)
	}
	// Clamped, the first tile is followed by its edge, stretched.
	if n := bands(softwareRender(repeatScene(t, false))); n != 2 {
		t.Fatalf("%d bands clamped, want 2", n)
	}
}
