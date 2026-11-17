// Copyright (c) 2026 Neomantra Corp.

//go:build webgpu

package nt3d

import (
	"fmt"
	"image/color"
	"testing"
)

func TestGPUIndirectCoverage(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	for _, tc := range indirectCases() {
		t.Run(tc.name, func(t *testing.T) {
			// A test-only material hook exercises the actual scene WGSL helpers.
			// The left half displays HDR radiance / 8; the right displays coverage.
			source := fmt.Sprintf(`
fn nt3dMaterial(in:NT3DMaterialInput)->vec4<f32> {
 let front=indirectCovered(vec3<f32>(%f,%f,%f),%f);
 let fallback=indirectCovered(vec3<f32>(%f,%f,%f),%f);
 let result=indirectOver(indirectOccluded(front,%f),fallback);
 if in.uv.x<0.5 {return vec4<f32>(result.radiance/8.0,1.0);}
 return vec4<f32>(vec3<f32>(result.coverage),1.0);
}`, tc.front[0], tc.front[1], tc.front[2], tc.coverage, tc.fallback[0], tc.fallback[1], tc.fallback[2], tc.fallbackCoverage, tc.visibility)
			shader, err := NewMaterialShader(source, func(in MaterialColorInput) [4]float32 {
				if in.UV.U < .5 {
					return [4]float32{tc.want.radiance[0] / 8, tc.want.radiance[1] / 8, tc.want.radiance[2] / 8, 1}
				}
				return [4]float32{tc.want.coverage, tc.want.coverage, tc.want.coverage, 1}
			})
			check(t, err)
			f := pbrQuadFrame(&Material{Unlit: true, Shader: shader}, color.RGBA{255, 255, 255, 255})
			gpu, err := r.Render(f)
			check(t, err)
			want := softwareRender(f)
			for _, x := range []int{20, 60} {
				a, b := rgbaAt(gpu, x, 40), rgbaAt(want, x, 40)
				for _, d := range []int{int(a.R) - int(b.R), int(a.G) - int(b.G), int(a.B) - int(b.B)} {
					if d < -1 || d > 1 {
						t.Fatalf("x=%d: GPU %v, expected %v", x, a, b)
					}
				}
			}
		})
	}
}
