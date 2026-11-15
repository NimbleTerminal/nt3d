// Copyright (c) 2026 Neomantra Corp.

//go:build webgpu

package demo

import (
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/examples/demos/internal/demo/models"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

func TestGPUToonDemoParity(t *testing.T) {
	runner, err := NewRunner("toon", models.Toon, nil, nt3dmesh.ZUp, false)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	gpu := nt3d.NewGPURenderer()
	defer gpu.Close()
	sw := nt3d.NewSoftwareRenderer()
	defer sw.Close()

	checkParity := func(label string) {
		t.Helper()
		f, err := runner.Frame(runner.Camera(), 200, 120)
		if err != nil {
			t.Fatalf("%s frame: %v", label, err)
		}
		f.Linear = true
		f.Complete = true
		gpuImg, err := gpu.Render(f)
		if err != nil {
			t.Fatalf("%s GPU render: %v", label, err)
		}
		swImg, err := sw.Render(f)
		if err != nil {
			t.Fatalf("%s software render: %v", label, err)
		}
		if mad := demoMeanAbsDiff(gpuImg, swImg); mad > 5.0 {
			t.Fatalf("%s: toon demo GPU vs Software mean abs diff %.2f > 5.0", label, mad)
		}
	}

	checkParity("soft shadows + inked shelter")
	runner.SendKey("s")
	checkParity("crisp shadows")
	runner.SendKey("s")
	runner.SendKey("a")
	runner.Step(0.35)
	checkParity("soft shadows + swaying banner")
}
