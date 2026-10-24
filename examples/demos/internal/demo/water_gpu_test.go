//go:build webgpu

package demo

import (
	"nimbleterminal.dev/nt3d"
	"testing"
)

func TestGPUWaterMotionAndMaterials(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupWater(); err != nil {
		t.Fatal(err)
	}
	r := nt3d.NewGPURenderer()
	defer r.Close()
	camera := nt3d.DefaultCamera()
	camera.Distance = 11
	camera.Projection = nt3d.Perspective
	for _, at := range []float64{0, .5, 1, 2, 5} {
		d.animationTime = at
		if err := d.stepWater(); err != nil {
			t.Fatal(err)
		}
		f, err := d.scene.Frame(camera, 160, 100)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = r.Render(f); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{"n", "e", "n", "e", "t", "h", "t", "h", "f", "f", "f", "f", "v", "v", "v", "v", "m", "m"} {
		if _, err := d.waterKey(key); err != nil {
			t.Fatal(err)
		}
		f, err := d.scene.Frame(camera, 160, 100)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = r.Render(f); err != nil {
			t.Fatal(err)
		}
	}
}
