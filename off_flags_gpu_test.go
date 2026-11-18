// Copyright (c) 2026 Neomantra Corp.

//go:build webgpu

package nt3d

import (
	"image"
	"testing"
)

// The GPU draws a light, ambient light, or reflection probe that is Off as it
// draws the frame without it, as software does.
func TestGPUOffAddsNothing(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	checkOffFlags(t, func(f Frame) image.Image {
		img, err := r.Render(f)
		check(t, err)
		return img
	})
}
