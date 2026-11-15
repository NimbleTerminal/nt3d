//go:build webgpu

// Copyright (c) 2026 Neomantra Corp.

package demo

import (
	"testing"

	"nimbleterminal.dev/nt3d"
)

// The same decorated demo frames render on the GPU in every shadow mode.
func TestGPUShadowDemos(t *testing.T) {
	renderer := nt3d.NewGPURenderer()
	defer renderer.Close()
	for _, forest := range []bool{false, true} {
		d := &demo{scene: nt3d.NewScene()}
		var err error
		if forest {
			err = d.setupForest()
		} else {
			err = d.setupLights()
		}
		if err != nil {
			t.Fatal(err)
		}
		d.fog = false
		for range 3 {
			if err := d.cycleShadows(); err != nil {
				t.Fatal(err)
			}
			camera := nt3d.DefaultCamera()
			if forest {
				camera.Distance = 9
			}
			f, err := d.scene.Frame(camera, 64, 48)
			if err != nil {
				t.Fatal(err)
			}
			f.Lighting = nt3d.DefaultLighting()
			d.decorate(&f)
			if _, err := renderer.Render(f); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestGPULightsCoatSheenParity(t *testing.T) {
	gpu := nt3d.NewGPURenderer()
	defer gpu.Close()
	sw := nt3d.NewSoftwareRenderer()
	defer sw.Close()

	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupLights(); err != nil {
		t.Fatal(err)
	}
	// Step material to "coat+sheen" (index 2).
	for d.lights.material < len(lightMaterialNames) && lightMaterialNames[d.lights.material] != "coat+sheen" {
		if err := d.toggleLightShine(); err != nil {
			t.Fatal(err)
		}
	}
	if lightMaterialNames[d.lights.material] != "coat+sheen" {
		t.Fatal("missing coat+sheen material preset")
	}

	f, err := d.scene.Frame(nt3d.DefaultCamera(), 96, 64)
	if err != nil {
		t.Fatal(err)
	}
	f.Linear = true
	f.Complete = true
	d.decorateLights(&f)

	gpuImg, err := gpu.Render(f)
	if err != nil {
		t.Fatal(err)
	}
	swImg, err := sw.Render(f)
	if err != nil {
		t.Fatal(err)
	}
	if mad := demoMeanAbsDiff(gpuImg, swImg); mad > 4.0 {
		t.Fatalf("lights coat+sheen GPU vs Software mean abs diff %.2f > 4.0", mad)
	}
}
