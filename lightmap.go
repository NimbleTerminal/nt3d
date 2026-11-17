// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"fmt"
	"math"
)

// LightMap is immutable baked diffuse irradiance, before albedo and division by
// pi. It replaces ambient/environment diffuse at every texel, including black.
// Coordinates use a top-left origin and clamp at edges. Charts need padding for
// filtering. This nondirectional bake does not respond to normal-map rotation.
type LightMap struct{ texture *Texture }

// NewLightMap copies row-major linear RGB irradiance (three floats per texel).
// Dimensions must be 1..8192; values must be finite and in 0..65504. Storage and
// mip filtering use half precision on both renderers. Values above one are HDR.
func NewLightMap(width, height int, irradiance []float32) (*LightMap, error) {
	if width < 1 || height < 1 || width > 8192 || height > 8192 || len(irradiance) != width*height*3 {
		return nil, fmt.Errorf("nt3d: invalid lightmap dimensions or RGB length")
	}
	base := hdrImage{width: width, height: height, pix: make([]float32, len(irradiance))}
	for i, v := range irradiance {
		if !validFloat(v) || v < 0 || v > 65504 {
			return nil, fmt.Errorf("nt3d: lightmap irradiance must be finite and in 0..65504")
		}
		base.pix[i] = float16value(float16bits(v))
	}
	return &LightMap{texture: &Texture{data: true, width: width, height: height, hdr: buildHDRMipChain(base)}}, nil
}

// NewHDRLightMap reads a Radiance RGBE image whose pixels contain linear
// irradiance. The image is a UV atlas, not an equirectangular environment.
func NewHDRLightMap(data []byte) (*LightMap, error) {
	base, err := decodeRadianceHDR(data)
	if err != nil {
		return nil, err
	}
	return NewLightMap(base.width, base.height, base.pix)
}

func (m *LightMap) valid() bool { return m != nil && m.texture != nil && len(m.texture.hdr) > 0 }

func validateLightMap(g Geometry) error {
	if len(g.LightMapUV) != 0 && len(g.LightMapUV) != len(g.Vertices) {
		return fmt.Errorf("nt3d: lightmap UV length mismatch")
	}

	if g.Material != nil && g.Material.LightMap != nil && (!g.Material.LightMap.valid() || len(g.LightMapUV) != len(g.Vertices)) {
		return fmt.Errorf("nt3d: lightmap requires NewLightMap or NewHDRLightMap and independent vertex UVs")
	}
	return nil
}

func sampleLightMapLevels(levels []hdrImage, uv UV, lod float32) [4]float32 {
	lod = max(0, min(lod, float32(len(levels)-1)))
	sample := func(level hdrImage) [4]float32 {
		hu, hv := .5/float32(level.width), .5/float32(level.height)
		return sampleHDRImg(level, max(hu, min(1-hu, uv.U)), max(hv, min(1-hv, uv.V)))
	}
	i := int(lod)
	c := sample(levels[i])
	if f := lod - float32(i); f > 0 && i+1 < len(levels) {
		d := sample(levels[i+1])
		for k := range c {
			c[k] += (d[k] - c[k]) * f
		}
	}
	return c
}

func lightMapDiffuse(sample [4]float32, linear bool) indirectSample {
	var c [3]float32
	for k := range c {
		c[k] = sample[k] / math.Pi
		if !linear {
			c[k] = float32(encodeSRGB(float64(c[k])))
		}
	}
	return coveredIndirect(c, 1)
}

var emptyLightMap = &Texture{data: true, width: 1, height: 1, pixels: []byte{0, 0, 0, 0}}

func lightMapTexture(m *Material) *Texture {
	if m != nil && m.LightMap != nil {
		return m.LightMap.texture
	}
	return emptyLightMap
}
