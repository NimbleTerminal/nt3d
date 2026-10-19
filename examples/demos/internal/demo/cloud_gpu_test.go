//go:build webgpu

package demo

import (
	"nimbleterminal.dev/nt3d"
	"testing"
)

func TestGPUCloudRenders(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupCloud(); err != nil {
		t.Fatal(err)
	}
	r := nt3d.NewGPURenderer()
	defer r.Close()
	for _, distance := range []float64{4, 40} {
		d.cloud.view, d.cloud.have = cloudView(distance), true
		if _, err := d.stepCloud(); err != nil {
			t.Fatal(err)
		}
		f, err := d.scene.Frame(nt3d.Camera{Alpha: 30, Beta: 40, Distance: distance, Projection: nt3d.Perspective}, 128, 96)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Render(f); err != nil {
			t.Fatal(err)
		}
	}
}
