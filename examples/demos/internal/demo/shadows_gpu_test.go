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
