// Copyright (c) 2026 Neomantra Corp.

//go:build webgpu

package nt3d

import (
	"image/color"
	"testing"
)

func TestGPUHoldoutAndPointToonMatchSoftware(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()

	f := holdoutSplatScene(t, 3, 0.08)
	gpu := toonGPU(t, r, f)
	sw := softwareRender(f)

	ink := color.RGBA{10, 12, 18, 255}
	if n := coloredPixels(gpu, ink); n < 40 {
		t.Fatalf("GPU holdout ink pixels=%d, want >= 40", n)
	}
	if mad := hookImgMeanAbsDiff(gpu, sw); mad > 5.0 {
		t.Fatalf("GPU vs software holdout+toon-splat MAD=%.3f (center GPU=%v SW=%v)", mad, at(gpu, 64, 64), at(sw, 64, 64))
	}
}
