// Copyright (c) 2026 Neomantra Corp.
package terraindemo

import (
	"image"
	"math"
)

// effect is a post-process: it changes a rendered frame's pixels in place, on
// the render goroutine, before the frame is shown.
type effect struct {
	name  string
	apply func(*image.RGBA)
}

var effects = []effect{
	{"none", nil},
	{"soft focus", softFocus},
	{"vignette", vignette},
}

// softFocus blurs with two passes of a three-pixel box, about a Gaussian of
// one pixel, then mixes half the sharp image back, for a glow rather than a smear.
func softFocus(img *image.RGBA) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	sharp := append([]uint8(nil), img.Pix...)
	box := func(src, dst []uint8, dx, dy int) {
		for y := range h {
			for x := range w {
				i := y*img.Stride + x*4
				var sum [3]int
				for k := -1; k <= 1; k++ {
					sx, sy := min(w-1, max(0, x+k*dx)), min(h-1, max(0, y+k*dy))
					j := sy*img.Stride + sx*4
					sum[0], sum[1], sum[2] = sum[0]+int(src[j]), sum[1]+int(src[j+1]), sum[2]+int(src[j+2])
				}
				dst[i], dst[i+1], dst[i+2] = uint8(sum[0]/3), uint8(sum[1]/3), uint8(sum[2]/3)
			}
		}
	}
	tmp := make([]uint8, len(img.Pix))
	copy(tmp, img.Pix)
	for range 2 {
		box(img.Pix, tmp, 1, 0)
		box(tmp, img.Pix, 0, 1)
	}
	for i := 0; i+3 < len(img.Pix); i += 4 {
		for c := range 3 {
			img.Pix[i+c] = uint8((int(img.Pix[i+c]) + int(sharp[i+c])) / 2)
		}
	}
}

// vignette darkens toward the corners.
func vignette(img *image.RGBA) {
	w, h := float64(img.Rect.Dx()), float64(img.Rect.Dy())
	for y := range img.Rect.Dy() {
		for x := range img.Rect.Dx() {
			dx, dy := (float64(x)+.5)/w-.5, (float64(y)+.5)/h-.5
			k := 1 - 1.1*math.Min(.6, dx*dx+dy*dy)
			i := y*img.Stride + x*4
			for c := range 3 {
				img.Pix[i+c] = uint8(float64(img.Pix[i+c]) * k)
			}
		}
	}
}
