// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"bytes"
	"image"
	"image/color"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"slices"
	"sync"
	"testing"
)

func TestSoftwareScratchEquivalenceAndOwnership(t *testing.T) {
	r := NewSoftwareRenderer().(*softwareRenderer)
	defer r.Close()
	// The allocating path is the pixel oracle. Exercise opaque, transparent,
	// textured and clipped geometry, lines, points, outlines, overlays and SSAA.
	frames := []Frame{blendScene(t, nil, quad(.5, .3, color.RGBA{220, 0, 0, 128}, glass, false)),
		checkerFloor(t, false), edgeScene(t, 3), repeatScene(t, true), outlineFrame(t)}
	clipped, _ := nearScene(t)
	frames = append(frames, clipped)
	var held []*image.RGBA
	var pixels [][]byte
	var depths []SoftwareResult
	var depthCopies [][]float32
	for _, f := range frames {
		for _, size := range []image.Point{{80, 40}, {40, 80}, {160, 90}, {32, 24}, {80, 40}} {
			f.Width, f.Height = size.X, size.Y
			f.DynamicLines = []Vertex{{Position: nt3dmath.Vec3{X: -1}, Color: white}, {Position: nt3dmath.Vec3{X: 1}, Color: white}}
			f.Overlays = []OverlayRect{{X: 1, Y: 2, Width: 6, Height: 4, Color: color.RGBA{10, 20, 30, 128}}}
			for _, ss := range []int{1, 2, 3} {
				f.Supersample = ss
				f.Linear = ss == 2
				want, err := RenderSoftware(f)
				check(t, err)
				got, err := r.Render(f)
				check(t, err)
				rgba := got.(*image.RGBA)
				if !bytes.Equal(want.Image.Pix, rgba.Pix) {
					t.Fatalf("pixels differ at %v, ss=%d", size, ss)
				}
				held = append(held, rgba)
				pixels = append(pixels, slices.Clone(rgba.Pix))
				depths = append(depths, want)
				depthCopies = append(depthCopies, slices.Clone(want.Depth))
			}
		}
	}
	check(t, r.Close())
	for i := range held {
		if !bytes.Equal(held[i].Pix, pixels[i]) || !bytes.Equal(depths[i].Image.Pix, pixels[i]) || !slices.Equal(depths[i].Depth, depthCopies[i]) {
			t.Fatalf("result %d overwritten", i)
		}
	}
}

func TestSoftwareScratchResetRetentionAndClose(t *testing.T) {
	r := NewSoftwareRenderer().(*softwareRenderer)
	f := blendScene(t, nil)
	f.Width, f.Height = 640, 320
	_, err := r.Render(f)
	check(t, err)
	t.Logf("retained depth after 640x320: %d bytes", cap(r.depth)*4)
	ptr := &r.depth[0]
	// Equal area with a new shape must reset every pixel, including hidden pixels.
	f.Width, f.Height = 320, 640
	f.Geometry, f.Instances, f.Shared, f.GeometryRevisions = nil, nil, nil, nil
	_, err = r.Render(f)
	check(t, err)
	if &r.depth[0] != ptr {
		t.Fatal("equal-area render reallocated scratch")
	}
	for _, d := range r.depth {
		if d != noDepth {
			t.Fatal("stale depth survived resize")
		}
	}
	// Invalid input does not resize/poison scratch or break the next valid render.
	if _, err = r.Render(Frame{}); err == nil {
		t.Fatal("invalid frame accepted")
	}
	if &r.depth[0] != ptr {
		t.Fatal("invalid frame changed scratch")
	}
	f.Width, f.Height = 2048, 1024
	_, err = r.Render(f)
	check(t, err)
	if &r.depth[0] != ptr || cap(r.depth) > maxSoftwareDepthPixels {
		t.Fatal("large snapshot retained scratch")
	}
	t.Logf("retained depth after 2048x1024: %d bytes", cap(r.depth)*4)
	f.Width, f.Height = 32, 32
	_, err = r.Render(f)
	check(t, err)
	if cap(r.depth) != 1024 {
		t.Fatal("scratch did not shrink", cap(r.depth))
	}
	f.Width, f.Height = 640, 320
	_, err = r.Render(f)
	check(t, err)
	if cap(r.depth) != 640*320 {
		t.Fatal("scratch did not grow exactly")
	}
	check(t, r.Close())
	check(t, r.Close())
	t.Logf("retained depth after Close: %d bytes", cap(r.depth)*4)
	if r.depth != nil {
		t.Fatal("Close retained scratch")
	}
	if _, err = r.Render(f); err == nil {
		t.Fatal("render after Close succeeded")
	}
}

func TestSoftwareConcurrentRenderClose(t *testing.T) {
	r := NewSoftwareRenderer()
	f := blendScene(t, nil)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 5 {
				img, err := r.Render(f)
				if err == nil && img == nil {
					t.Error("nil image")
				}
			}
		})
	}
	wg.Go(func() { check(t, r.Close()) })
	wg.Wait()
	check(t, r.Close())
	if _, err := r.Render(f); err == nil {
		t.Fatal("late render succeeded")
	}
}
