// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image"
	"image/color"
)

// softwareTarget holds color and depth for one pass. acc is allocated when
// blending or HDR needs working-space colors with headroom above 255.
type softwareTarget struct {
	img                 *image.RGBA
	depth, acc          []float32
	width, height       int
	linear              bool
	oitMode             bool
	oitAccum, oitReveal []float32
}

func (dst *softwareTarget) toPixel(v float32) uint8 {
	if dst.linear {
		return encode8(v / 255)
	}
	return uint8(max(0, min(255, v+.5)))
}

func (dst *softwareTarget) fromPixel(p uint8) float32 {
	if dst.linear {
		return srgbDecode[p] * 255
	}
	return float32(p)
}

func (dst *softwareTarget) accumulate() {
	if dst.acc != nil {
		return
	}
	dst.acc = make([]float32, len(dst.depth)*3)
	for i := range dst.depth {
		off := i * 4
		dst.acc[i*3], dst.acc[i*3+1], dst.acc[i*3+2] = dst.fromPixel(dst.img.Pix[off]), dst.fromPixel(dst.img.Pix[off+1]), dst.fromPixel(dst.img.Pix[off+2])
	}
}

func (dst *softwareTarget) putDepth(x, y int, z float32) {
	if x < 0 || y < 0 || x >= dst.width || y >= dst.height || z < 0 || z > 1 {
		return
	}
	i := y*dst.width + x
	if z <= dst.depth[i]+.00001 {
		dst.depth[i] = z
	}
}

func (dst *softwareTarget) put(x, y int, z float32, c color.RGBA) {
	if x < 0 || y < 0 || x >= dst.width || y >= dst.height || z < 0 || z > 1 {
		return
	}
	i := y*dst.width + x
	if z <= dst.depth[i]+.00001 {
		dst.depth[i] = z
		off := i * 4
		dst.img.Pix[off], dst.img.Pix[off+1], dst.img.Pix[off+2] = c.R, c.G, c.B
		if dst.acc != nil {
			dst.acc[i*3], dst.acc[i*3+1], dst.acc[i*3+2] = dst.fromPixel(c.R), dst.fromPixel(c.G), dst.fromPixel(c.B)
		}
	}
}

func (dst *softwareTarget) putWork(x, y int, z float32, rgb [3]float32) {
	if x < 0 || y < 0 || x >= dst.width || y >= dst.height || z < 0 || z > 1 {
		return
	}
	i := y*dst.width + x
	if z <= dst.depth[i]+.00001 {
		if dst.acc == nil && (rgb[0] > 255 || rgb[1] > 255 || rgb[2] > 255) {
			dst.accumulate()
		}
		dst.depth[i] = z
		off := i * 4
		dst.img.Pix[off], dst.img.Pix[off+1], dst.img.Pix[off+2] = dst.toPixel(rgb[0]), dst.toPixel(rgb[1]), dst.toPixel(rgb[2])
		if dst.acc != nil {
			dst.acc[i*3], dst.acc[i*3+1], dst.acc[i*3+2] = max(0, rgb[0]), max(0, rgb[1]), max(0, rgb[2])
		}
	}
}

// blend tests opaque depth, then composites into the active transparency buffer.
func (dst *softwareTarget) blend(x, y int, z float32, rgb [3]float32, alpha float32) {
	if x < 0 || y < 0 || x >= dst.width || y >= dst.height || z < 0 || z > 1 {
		return
	}
	i := y*dst.width + x
	if z <= dst.depth[i]+.00001 {
		alpha = max(0, min(1, alpha))
		if dst.oitMode {
			aw := alpha * oitWeight(z, alpha)
			ao := i * 4
			dst.oitAccum[ao] += rgb[0] * aw
			dst.oitAccum[ao+1] += rgb[1] * aw
			dst.oitAccum[ao+2] += rgb[2] * aw
			dst.oitAccum[ao+3] += aw
			dst.oitReveal[i] *= 1 - alpha
			return
		}
		dst.accumulate()
		off, a := i*4, dst.acc[i*3:i*3+3]
		for k, v := range rgb {
			a[k] = v*alpha + a[k]*(1-alpha)
			dst.img.Pix[off+k] = dst.toPixel(a[k])
		}
	}
}

func (dst *softwareTarget) add(x, y int, z float32, rgb [3]float32, alpha float32) {
	if x < 0 || y < 0 || x >= dst.width || y >= dst.height || z < 0 || z > 1 {
		return
	}
	i := y*dst.width + x
	if z <= dst.depth[i]+.00001 {
		dst.accumulate()
		off, a := i*4, dst.acc[i*3:i*3+3]
		for k, v := range rgb {
			a[k] += v * alpha
			dst.img.Pix[off+k] = dst.toPixel(a[k])
		}
	}
}
