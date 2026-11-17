// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"fmt"
	"math"
)

// ToneMap selects how linear HDR scene radiance is compressed into [0, 1]
// display range before sRGB encoding.
type ToneMap uint8

const (
	// NoToneMap leaves linear radiance uncompressed (clamped to [0, 1] when
	// encoded to 8-bit targets, or preserved in RGBA16Float targets).
	NoToneMap ToneMap = iota
	// ReinhardToneMap softly compresses highlights with c / (1 + c).
	ReinhardToneMap
	// ACESToneMap applies the Narkowicz ACES filmic curve for crisp midtone
	// contrast and smooth highlight roll-off.
	ACESToneMap
)

// String returns "none", "Reinhard", or "ACES".
func (t ToneMap) String() string {
	switch t {
	case ReinhardToneMap:
		return "Reinhard"
	case ACESToneMap:
		return "ACES"
	default:
		return "none"
	}
}

const (
	// DefaultBloomThreshold is the luminance threshold Bloom uses when
	// Bloom.Threshold is zero.
	DefaultBloomThreshold = 0.8
	// DefaultBloomRadius is the pyramid upsample spread Bloom uses when
	// Bloom.Radius is zero.
	DefaultBloomRadius = 0.65
	// MaxBloomStrength is the largest Bloom.Strength allowed.
	MaxBloomStrength = 4
	// DefaultSSAOBias is the horizon cosine bias SSAO uses when SSAO.Bias is zero.
	DefaultSSAOBias = 0.04
	// MaxExposure is the magnitude bound on PostProcess.Exposure, in stops.
	MaxExposure = 10
)

// Bloom configures an HDR glow around bright pixels. Strength scales the glow
// added back to the scene, 0..MaxBloomStrength; zero disables bloom. Threshold
// is the linear brightness above which pixels contribute, with a smooth knee
// (>= 0; zero means DefaultBloomThreshold). Radius controls how broadly the
// glow spreads across mip levels, 0..1 (zero means DefaultBloomRadius).
type Bloom struct {
	Strength  float32
	Threshold float32
	Radius    float32
}

func (b Bloom) on() bool { return b.Strength > 0 }

func (b Bloom) threshold() float32 {
	if b.Threshold == 0 {
		return DefaultBloomThreshold
	}
	return b.Threshold
}

func (b Bloom) radius() float32 {
	if b.Radius == 0 {
		return DefaultBloomRadius
	}
	return b.Radius
}

// SSAO configures screen-space ambient occlusion estimated from the depth
// buffer, darkening indirect lighting in creases, corners, and contact areas.
// Direct lighting, emission, and unlit surfaces are unaffected. Material AO
// and SSAO combine by minimum visibility. An extra opaque pass supplies depth.
// Strength is the maximum occlusion darkening, 0..1; zero disables SSAO. Radius is the
// world-space sampling radius (>= 0; zero selects a view-proportional radius
// of about 6% of the viewport height at each surface's depth). Bias is the
// minimum horizon cosine step that suppresses self-occlusion on flat surfaces,
// 0..0.5 (zero means DefaultSSAOBias).
//
// Wireframe ignores SSAO. Software rendering evaluates SSAO only when
// Frame.Complete is true (such as Snapshot), skipping it during interactive
// pacing.
type SSAO struct {
	Strength float32
	Radius   float32
	Bias     float32
}

func (s SSAO) on() bool { return s.Strength > 0 }

func (s SSAO) bias() float32 {
	if s.Bias == 0 {
		return DefaultSSAOBias
	}
	return s.Bias
}

// PostProcess configures screen-space ambient occlusion within indirect
// lighting, followed by HDR bloom, exposure, tone mapping, and vignette in
// linear light before overlays and target encoding. The zero value disables
// all passes.
//
// Exposure is in photographic stops (-MaxExposure..MaxExposure), scaling
// linear scene radiance by 2^Exposure before tone mapping: zero leaves
// brightness unchanged, +1 doubles it, and -1 halves it. Vignette darkens the
// frame smoothly toward its corners, 0..1; zero disables it.
//
// Wireframe ignores PostProcess. Software rendering applies Exposure, ToneMap,
// Vignette, and a quarter-resolution Bloom on every frame, and SSAO on
// Complete frames.
type PostProcess struct {
	SSAO     SSAO
	Bloom    Bloom
	Exposure float32
	ToneMap  ToneMap
	Vignette float32
}

func (p PostProcess) on() bool {
	return p.SSAO.on() || p.Bloom.on() || p.Exposure != 0 || p.ToneMap != NoToneMap || p.Vignette > 0
}

func (p PostProcess) validate() error {
	if !validFloat(p.SSAO.Strength, p.SSAO.Radius, p.SSAO.Bias) || p.SSAO.Strength < 0 || p.SSAO.Strength > 1 || p.SSAO.Radius < 0 || p.SSAO.Bias < 0 || p.SSAO.Bias > 0.5 {
		return fmt.Errorf("nt3d: SSAO strength must be 0..1, radius >= 0, and bias 0..0.5")
	}
	if !validFloat(p.Bloom.Strength, p.Bloom.Threshold, p.Bloom.Radius) || p.Bloom.Strength < 0 || p.Bloom.Strength > MaxBloomStrength || p.Bloom.Threshold < 0 || p.Bloom.Radius < 0 || p.Bloom.Radius > 1 {
		return fmt.Errorf("nt3d: bloom strength must be 0..%d, threshold >= 0, and radius 0..1", MaxBloomStrength)
	}
	if !validFloat(p.Exposure) || p.Exposure < -MaxExposure || p.Exposure > MaxExposure {
		return fmt.Errorf("nt3d: exposure must be -%d..%d stops", MaxExposure, MaxExposure)
	}
	if p.ToneMap > ACESToneMap {
		return fmt.Errorf("nt3d: unknown tone map %d", p.ToneMap)
	}
	if !validFloat(p.Vignette) || p.Vignette < 0 || p.Vignette > 1 {
		return fmt.Errorf("nt3d: vignette must be 0..1")
	}
	return nil
}

// bloomMipGain normalizes the accumulated upsample pyramid energy across levels
// so Bloom.Strength produces consistent brightness across viewport sizes.
func bloomMipGain(levels int, radius float32) float32 {
	if levels <= 1 {
		return 1
	}
	sum, term := float32(1), float32(1)
	for i := 1; i < levels; i++ {
		term *= radius
		sum += term
	}
	return 1 / sum
}

// bloomSoftWeight returns the soft-knee threshold multiplier for a linear RGB
// color, matching post.wgsl.
func bloomSoftWeight(r, g, b, threshold float32) float32 {
	br := max(r, g, b)
	if br <= 1e-5 {
		return 0
	}
	knee := threshold*0.5 + 1e-4
	soft := min(2*knee, max(0, br-threshold+knee))
	contrib := max(br-threshold, (soft*soft)/(4*knee))
	return max(0, contrib/br)
}

func toneMapChannel(x float32, mode ToneMap) float32 {
	if x < 0 {
		x = 0
	}
	switch mode {
	case ReinhardToneMap:
		return x / (1 + x)
	case ACESToneMap:
		return min(1, max(0, (x*(2.51*x+0.03))/(x*(2.43*x+0.59)+0.14)))
	default:
		return x
	}
}

// vignetteFactor returns the smooth radial darkening factor at normalized
// centered coordinates dx, dy in [-0.5, 0.5], matching post.wgsl.
func vignetteFactor(dx, dy, strength float32) float32 {
	if strength <= 0 {
		return 1
	}
	r2 := dx*dx + dy*dy
	t := min(1, max(0, (r2-0.05)/(0.55-0.05)))
	smooth := t * t * (3 - 2*t)
	return max(0, 1-strength*smooth)
}

func exposureScale(stops float32) float32 {
	if stops == 0 {
		return 1
	}
	return float32(math.Exp2(float64(stops)))
}
