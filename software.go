// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image"
	"math"
)

// SoftwareResult is a software rendering with its depth buffer.
type SoftwareResult struct {
	// Image is the picture, exactly as NewSoftwareRenderer().Render draws
	// the frame: Width × Height pixels.
	Image *image.RGBA
	// Depth holds each pixel's depth, row by row: Depth[y*Width+x], so
	// Width*Height values. It is the depth the renderer tests with,
	// WebGPU's normalized depth: 0 at the camera's near plane, 1 at its far
	// plane, growing with distance along the view (not linearly under
	// perspective: there it is affine in 1/distance). Pixels nothing covers
	// hold +Inf. Points, lines,
	// and arrows write depth as surfaces do; transparent surfaces and
	// overlays do not, as they hide nothing. A supersampled pixel's depth is
	// the nearest of its samples.
	Depth         []float32
	Width, Height int
}

// RenderSoftware renders a frame in software, as NewSoftwareRenderer does,
// returning the depth buffer with the picture: for outlines, fog, or
// compositing done after rendering.
func RenderSoftware(f Frame) (SoftwareResult, error) {
	if err := validateFrame(f); err != nil {
		return SoftwareResult{}, err
	}
	img, depth := softwareRenderDepth(f)
	for i, d := range depth {
		if d == noDepth {
			depth[i] = float32(math.Inf(1))
		}
	}
	return SoftwareResult{Image: img, Depth: depth, Width: f.Width, Height: f.Height}, nil
}
