// Copyright (c) 2026 Neomantra Corp.

package demo

import (
	"image"

	"nimbleterminal.dev/nt3d"
)

// effect is a post-processing preset, using either NT3D's built-in GPU/tiered
// PostProcess pipeline or a custom CPU pixel callback.
type effect struct {
	name  string
	post  nt3d.PostProcess
	apply func(*image.RGBA)
}

var effects = []effect{
	{name: "none"},
	{name: "studio", post: nt3d.PostProcess{
		SSAO:     nt3d.SSAO{Strength: .65, Radius: .35},
		Bloom:    nt3d.Bloom{Strength: .35, Threshold: .85, Radius: 1},
		ToneMap:  nt3d.ACESToneMap,
		Vignette: .35,
	}},
	{name: "bloom", post: nt3d.PostProcess{
		Bloom:   nt3d.Bloom{Strength: .6, Threshold: .72, Radius: 1},
		ToneMap: nt3d.ACESToneMap,
	}},
	{name: "ssao", post: nt3d.PostProcess{
		SSAO: nt3d.SSAO{Strength: .8, Radius: .4},
	}},
	{name: "vignette", post: nt3d.PostProcess{
		Vignette: .55,
	}},
	{name: "soft focus", apply: softFocus},
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
