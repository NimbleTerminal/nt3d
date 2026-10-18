//go:build webgpu

package demo

import (
	"nimbleterminal.dev/nt3d"
	"testing"
)

func TestGPUParticleSprites(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupParticles(); err != nil {
		t.Fatal(err)
	}
	if err := d.toggleParticleSprites(); err != nil {
		t.Fatal(err)
	}
	r := nt3d.NewGPURenderer()
	defer r.Close()
	for _, at := range []float64{0, .3, 1} {
		d.particles.t = at
		if err := d.moveParticles(); err != nil {
			t.Fatal(err)
		}
		f, err := d.scene.Frame(nt3d.DefaultCamera(), 128, 96)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Render(f); err != nil {
			t.Fatal(err)
		}
	}
}
