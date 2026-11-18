package nt3d

import (
	"image"
	"image/color"
	"math/rand/v2"
	"testing"
)

// conversionSources are images of the kinds decoders hand back: JPEG and
// lossy WebP give YCbCr, WebP with alpha NYCbCrA, and the rest stand for
// anything else an image.Image can be.
func conversionSources() map[string]image.Image {
	rng := rand.New(rand.NewPCG(7, 11))
	const w, h = 16, 12
	fill := func(b []byte) {
		for i := range b {
			b[i] = byte(rng.IntN(256))
		}
	}
	ycbcr := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
	fill(ycbcr.Y)
	fill(ycbcr.Cb)
	fill(ycbcr.Cr)
	full := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio444)
	fill(full.Y)
	fill(full.Cb)
	fill(full.Cr)
	alpha := image.NewNYCbCrA(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
	fill(alpha.Y)
	fill(alpha.Cb)
	fill(alpha.Cr)
	fill(alpha.A)
	alpha.A[0], alpha.A[1] = 0, 255
	premultiplied := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(premultiplied.Pix); i += 4 {
		a := rng.IntN(256)
		premultiplied.Pix[i+3] = byte(a)
		for k := range 3 {
			premultiplied.Pix[i+k] = byte(rng.IntN(a + 1))
		}
	}
	gray := image.NewGray(image.Rect(0, 0, w, h))
	fill(gray.Pix)
	wide := image.NewNRGBA64(image.Rect(0, 0, w, h))
	fill(wide.Pix)
	straight := image.NewNRGBA(image.Rect(0, 0, w, h))
	fill(straight.Pix)
	return map[string]image.Image{
		"other, straight alpha": otherImage{straight},
		"YCbCr 4:2:0":           ycbcr,
		"YCbCr 4:4:4":           full,
		"YCbCr subimage":        ycbcr.SubImage(image.Rect(3, 2, 13, 9)),
		"NYCbCrA":               alpha,
		"NYCbCrA subimage":      alpha.SubImage(image.Rect(1, 1, 9, 7)),
		"premultiplied RGBA":    premultiplied,
		"Gray":                  gray,
		"NRGBA64 (any other)":   wide,
	}
}

// A texture holds straight-alpha pixels. Whatever kind of image it is made
// from, they are the pixels the image package's own NRGBA conversion gives.
func TestNewTextureConvertsEveryImageKindAlike(t *testing.T) {
	for name, src := range conversionSources() {
		tex, err := NewTexture(src)
		check(t, err)
		b := src.Bounds()
		for y := 0; y < b.Dy(); y++ {
			for x := 0; x < b.Dx(); x++ {
				want := color.NRGBAModel.Convert(src.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
				if got := tex.NRGBAAt(x, y); got != want {
					t.Fatalf("%s at %d,%d: texture holds %v, the image converts to %v", name, x, y, got, want)
				}
			}
		}
	}
}

// Photographs arrive as YCbCr, with or without alpha. Converting one must
// not allocate per pixel: a 2048-square texture would otherwise cost eight
// million allocations.
func TestNewTextureDoesNotAllocatePerPixel(t *testing.T) {
	sources := conversionSources()
	for _, name := range []string{"YCbCr 4:2:0", "NYCbCrA", "premultiplied RGBA", "Gray"} {
		src := sources[name]
		pixels := src.Bounds().Dx() * src.Bounds().Dy()
		allocs := testing.AllocsPerRun(20, func() { _, _ = NewTexture(src) })
		if allocs > 8 {
			t.Errorf("%s: %v allocations for %d pixels", name, allocs, pixels)
		}
	}
}

// otherImage hides an image's concrete type, as an image of a kind the
// converter has no accessor for.
type otherImage struct{ image.Image }
