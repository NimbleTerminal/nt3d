package nt3d

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func TestFlowMapSamplingAndSnapshot(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{255, 128, 0, 255})
	img.SetNRGBA(1, 0, color.NRGBA{0, 128, 0, 255})
	f, err := NewFlowMap(img)
	check(t, err)
	img.SetNRGBA(0, 0, color.NRGBA{})
	if got := f.Sample(UV{0, .5}, false); got != (UV{1, 0}) {
		t.Fatal("not a clamped immutable vector", got)
	}
	if got := f.Sample(UV{.5, .5}, false); math.Abs(float64(got.U)) > 1e-6 || got.V != 0 {
		t.Fatal("bilinear field", got)
	}
	for _, uv := range []UV{{.2, .7}, {-.2, 2}} {
		a, b := f.Sample(uv, true), f.Sample(UV{uv.U + 1, uv.V - 1}, true)
		if math.Abs(float64(a.U-b.U))+math.Abs(float64(a.V-b.V)) > 1e-6 {
			t.Fatal("flow seam", a, b)
		}
	}
	if got := f.Sample(UV{float32(math.NaN()), 0}, false); got != (UV{}) {
		t.Fatal("non-finite sample", got)
	}
}
func TestFlowPhaseContinuity(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := range 32 {
		for x := range 32 {
			img.SetNRGBA(x, y, color.NRGBA{uint8(128 + 70*math.Sin(float64(x)*math.Pi/16)), 128, 220, 255})
		}
	}
	normal, err := NewNormalMap(img)
	check(t, err)
	f, err := NewFlowMap(img)
	check(t, err)
	m := &Material{NormalMap: normal, NormalRepeat: true, FlowMap: f, FlowStrength: .4}
	sample := func(phase float32) [4]float32 {
		m.FlowPhase = phase
		return flowNormalSample(m, UV{.3, .4}, UV{.31, .4}, UV{.3, .41}, UV{.5, .2}, 1)
	}
	for _, phase := range []float32{0, .5, 1, -1, 100} {
		a, b := sample(phase-1e-5), sample(phase+1e-5)
		for i := range 3 {
			if math.Abs(float64(a[i]-b[i])) > 1e-4 {
				t.Fatal("phase reset popped", phase, a, b)
			}
		}
	}
	m.FlowStrength = 0
	a := sample(.2)
	b := normal.sampleFootprint(UV{.3, .4}, UV{.31, .4}, UV{.3, .41}, true, 1)
	if a != b {
		t.Fatal("disabled flow changed normals")
	}
}
