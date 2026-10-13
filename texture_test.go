// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image"
	"image/color"
	"testing"
)

func testTexture(t *testing.T, c color.RGBA) *Texture {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := range 2 {
		for x := range 2 {
			img.SetRGBA(x, y, c)
		}
	}
	tex, err := NewTexture(img)
	if err != nil {
		t.Fatal(err)
	}
	return tex
}
func TestTextureOwnershipAndValidation(t *testing.T) {
	src := image.NewNRGBA(image.Rect(5, -3, 7, -1))
	colors := []color.NRGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}, {255, 255, 255, 255}}
	for i, c := range colors {
		src.SetNRGBA(5+i%2, -3+i/2, c)
	}
	tex, err := NewTexture(src)
	if err != nil {
		t.Fatal(err)
	}
	src.SetNRGBA(5, -3, color.NRGBA{})
	if tex.Bounds() != image.Rect(0, 0, 2, 2) || tex.At(0, 0) != (color.RGBA{255, 0, 0, 255}) {
		t.Fatal("snapshot bounds or ownership")
	}
	for i, uv := range []UV{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		if got := tex.sample(uv); got != (color.RGBA(colors[i])) {
			t.Fatal("orientation", uv, got)
		}
	}
	if got := tex.sample(UV{.5, .5}); got != (color.RGBA{128, 128, 128, 255}) {
		t.Fatal("bilinear", got)
	}
	if tex.sample(UV{-2, 3}) != tex.sample(UV{0, 1}) {
		t.Fatal("clamp")
	}
	semi := image.NewNRGBA64(image.Rect(0, 0, 1, 1))
	semi.SetNRGBA64(0, 0, color.NRGBA64{A: 65534})
	var nilImage *image.RGBA
	for _, src := range []image.Image{nil, nilImage, semi, image.NewRGBA(image.Rect(0, 0, 0, 1)), image.NewRGBA(image.Rect(0, 0, 1, 1))} {
		if _, err := NewTexture(src); err == nil {
			t.Fatal("accepted invalid image")
		}
	}
}
