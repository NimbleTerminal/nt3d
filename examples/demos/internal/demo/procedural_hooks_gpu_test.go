// Copyright (c) 2026 Neomantra Corp.

//go:build webgpu

package demo

import (
	"image"
	"math"
	"testing"

	"nimbleterminal.dev/nt3d"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

func demoMeanAbsDiff(a, b image.Image) float64 {
	r := a.Bounds()
	var sum float64
	var n int
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			ar, ag, ab, aa := a.At(x, y).RGBA()
			br, bg, bb, ba := b.At(x, y).RGBA()
			sum += math.Abs(float64(ar>>8)-float64(br>>8)) +
				math.Abs(float64(ag>>8)-float64(bg>>8)) +
				math.Abs(float64(ab>>8)-float64(bb>>8)) +
				math.Abs(float64(aa>>8)-float64(ba>>8))
			n += 4
		}
	}
	return sum / float64(max(1, n))
}

func TestGPUProceduralHooksDemoParity(t *testing.T) {
	runner, err := NewRunner("procedural", nil, nil, nt3dmesh.ZUp, false)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	gpu := nt3d.NewGPURenderer()
	defer gpu.Close()
	sw := nt3d.NewSoftwareRenderer()

	for _, dt := range []float64{0, 0.35} {
		if dt > 0 {
			runner.Step(dt)
		}
		f, err := runner.Frame(runner.Camera(), 160, 120)
		if err != nil {
			t.Fatal(err)
		}
		f.Linear = true
		f.Complete = true
		gpuImg, err := gpu.Render(f)
		if err != nil {
			t.Fatal(err)
		}
		swImg, err := sw.Render(f)
		if err != nil {
			t.Fatal(err)
		}
		if mad := demoMeanAbsDiff(gpuImg, swImg); mad > 3.5 {
			t.Fatalf("dt=%.2f: procedural hooks demo GPU vs Software mean abs diff %.2f > 3.5", dt, mad)
		}
	}
}
