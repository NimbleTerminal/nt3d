package nt3d

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

type genericTextureImage struct{ image.Image }

func TestTextureTypedCopyMatchesGeneric(t *testing.T) {
	for _, opaque := range []bool{false, true} {
		for _, kind := range []string{"rgba", "nrgba"} {
			for _, cropped := range []bool{false, true} {
				rgba := image.NewRGBA(image.Rect(-5, 7, 19, 29))
				nrgba := image.NewNRGBA(rgba.Bounds())
				for y := 7; y < 29; y++ {
					for x := -5; x < 19; x++ {
						a := uint8((x + 5) * 11)
						if opaque {
							a = 255
						}
						c := color.NRGBA{R: uint8(x * 17), G: uint8(y * 31), B: uint8(x + y), A: a}
						rgba.Set(x, y, c)
						nrgba.SetNRGBA(x, y, c)
					}
				}
				var src image.Image = rgba
				if kind == "nrgba" {
					src = nrgba
				}
				if cropped {
					if kind == "nrgba" {
						src = nrgba.SubImage(image.Rect(-2, 9, 13, 21))
					} else {
						src = rgba.SubImage(image.Rect(-2, 9, 13, 21))
					}
				}
				got, err := NewTexture(src)
				if err != nil {
					t.Fatal(err)
				}
				want, err := NewTexture(genericTextureImage{src})
				if err != nil {
					t.Fatal(err)
				}
				if got.width != want.width || got.height != want.height || !bytes.Equal(got.pixels, want.pixels) {
					t.Fatalf("%s opaque=%v cropped=%v: conversion differs", kind, opaque, cropped)
				}
				saved := bytes.Clone(got.pixels)
				rgba.SetRGBA(src.Bounds().Min.X, src.Bounds().Min.Y, color.RGBA{})
				nrgba.SetNRGBA(src.Bounds().Min.X, src.Bounds().Min.Y, color.NRGBA{})
				if !bytes.Equal(got.pixels, saved) {
					t.Fatal("texture aliases source")
				}
			}
		}
	}
}

func TestTextureOpaqueRGBASubimage(t *testing.T) {
	parent := image.NewRGBA(image.Rect(-5, 7, 19, 29))
	bounds := image.Rect(-2, 9, 13, 21)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			parent.SetRGBA(x, y, color.RGBA{R: 128, G: 64, B: 32, A: 255})
		}
	}
	src := parent.SubImage(bounds).(*image.RGBA)
	if parent.Opaque() || !src.Opaque() {
		t.Fatal("expected opacity only within crop")
	}
	for _, translucent := range []bool{false, true} {
		if translucent {
			src.SetRGBA(bounds.Max.X-1, bounds.Max.Y-1, color.RGBA{R: 64, G: 32, B: 16, A: 128})
		}
		got, err := NewTexture(src)
		if err != nil {
			t.Fatal(err)
		}
		want, err := NewTexture(genericTextureImage{src})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.pixels, want.pixels) {
			t.Fatalf("cropped RGBA translucent=%v: conversion differs", translucent)
		}
	}
}

// Compare the typed paths with the equivalent image.Image conversion, including
// allocations. The wrapper hides the concrete type without changing pixels.
func BenchmarkNewTexture(b *testing.B) {
	rgba := image.NewRGBA(image.Rect(0, 0, 512, 512))
	nrgba := image.NewNRGBA(rgba.Bounds())
	for i := 0; i < len(rgba.Pix); i += 4 {
		rgba.Pix[i], rgba.Pix[i+1], rgba.Pix[i+2], rgba.Pix[i+3] = 128, 64, 32, 255
		nrgba.Pix[i], nrgba.Pix[i+1], nrgba.Pix[i+2], nrgba.Pix[i+3] = 128, 64, 32, 128
	}
	for _, tc := range []struct {
		name string
		img  image.Image
	}{
		{"RGBA", rgba}, {"RGBA/generic", genericTextureImage{rgba}},
		{"NRGBA", nrgba}, {"NRGBA/generic", genericTextureImage{nrgba}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(512 * 512 * 4)
			for b.Loop() {
				if _, err := NewTexture(tc.img); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
