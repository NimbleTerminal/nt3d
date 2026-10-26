//go:build webgpu

package nt3d

import "testing"

// TestGPUOverbrightSHMatchesSoftware draws overlapping splats whose SH
// evaluates well above 1 and compares the renderers. The GPU accumulates
// blended color with headroom and clamps once at the end, so the software
// renderer must not clamp the running sum at each splat.
func TestGPUOverbrightSHMatchesSoftware(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	for _, precise := range []bool{false, true} {
		for _, linear := range []bool{false, true} {
			g := paletteGeometrySpread(t, 200, 9, 32)
			g.PointStyle.Precise = precise
			f := lodGPUFrame(lodPerspective(3, g), 0)
			f.Perspective, f.Linear = true, linear
			gpu, err := r.Render(f)
			check(t, err)
			if off := lodOff(gpu, softwareRender(f)); off > 12 {
				t.Errorf("precise %v linear %v: %d pixels differ from software", precise, linear, off)
			}
		}
	}
}
