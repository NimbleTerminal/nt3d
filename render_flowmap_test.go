//go:build webgpu

package nt3d

import (
	"image"
	"image/color"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"testing"
)

func TestGPUFlowNormals(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			img.SetNRGBA(x, y, color.NRGBA{uint8(30 + x*13), uint8(30 + y*13), 220, 255})
		}
	}
	normal, err := NewNormalMap(img)
	check(t, err)
	flow, err := NewFlowMap(img)
	check(t, err)
	m := &Material{NormalMap: normal, NormalStrength: 1, NormalRepeat: true, FlowMap: flow, FlowStrength: .6}
	g := quad(1, .4, color.RGBA{210, 180, 120, 255}, m, false)
	g.UV = []UV{{0, 1}, {1, 1}, {1, 0}, {0, 0}}
	r := &gpuRenderer{}
	defer r.Close()
	for i, phase := range []float32{0, .2, .4999, .5001, .9999, 1.0001, 0} {
		m.FlowPhase = phase
		if i == 6 {
			m.FlowStrength = 0
		}
		f := flatFrame(80, 60, g)
		f.Revision = 1
		f.Lighting = oldLight(nt3dmath.Vec3{X: .5, Z: 1}, .2, true)
		gpu, err := r.Render(f)
		check(t, err)
		cpu := softwareRender(f)
		for _, p := range [][2]int{{20, 20}, {40, 30}, {55, 40}} {
			if a, b := at(gpu, p[0], p[1]), at(cpu, p[0], p[1]); !closeRGB(a, b, 4) {
				t.Fatal("flow GPU/software mismatch", phase, p, a, b)
			}
		}
	}
}
