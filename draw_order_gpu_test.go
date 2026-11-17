//go:build webgpu

package nt3d

import (
	"image/color"
	"testing"
)

// The GPU agrees with software about which of two coincident surfaces shows.
func TestGPUDrawOrderDecidesCoincidentSurfaces(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	for _, c := range []struct {
		redLayer int
		want     color.RGBA
	}{{0, color.RGBA{0, 0, 255, 255}}, {1, color.RGBA{255, 0, 0, 255}}} {
		img, err := r.Render(coincident(t, c.redLayer))
		check(t, err)
		if got := at(img, 32, 32); got != c.want {
			t.Fatalf("with red in layer %d the center is %v, want %v", c.redLayer, got, c.want)
		}
	}
}
