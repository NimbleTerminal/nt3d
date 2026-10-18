//go:build webgpu

package nt3d

import (
	"image/color"
	"testing"
)

func TestGPUEnvironment(t *testing.T) {
	s := NewScene()
	s.SetEnvironment(testEnvironment(t))
	camera := DefaultCamera()
	r := &gpuRenderer{}
	defer r.Close()
	for _, linear := range []bool{false, true} {
		for _, projection := range []Projection{Orthographic, Perspective} {
			camera.Projection = projection
			if projection == Perspective {
				camera.Alpha = 89
				camera.Beta = 180
			}
			f, err := s.Frame(camera, 80, 60)
			check(t, err)
			f.Linear = linear
			f.Supersample = 2
			got, err := r.Render(f)
			check(t, err)
			want := softwareRender(f)
			for y := 0; y < 60; y += 7 {
				for x := 0; x < 80; x += 7 {
					a := color.RGBAModel.Convert(got.At(x, y)).(color.RGBA)
					b := color.RGBAModel.Convert(want.At(x, y)).(color.RGBA)
					if !closeRGB(a, b, 3) {
						t.Fatalf("sky linear=%v projection=%v at %d,%d: GPU %v software %v", linear, projection, x, y, a, b)
					}
				}
			}
		}
	}
	f := flatFrame(64, 64)
	f.Environment = testEnvironment(t)
	f.Lighting = Lighting{}
	f.Geometry = []Geometry{quad(.5, .5, white, nil, false)}
	got, err := r.Render(f)
	check(t, err)
	want := softwareRender(f)
	if !closeRGB(at(got, 32, 32), at(want, 32, 32), 3) {
		t.Fatalf("IBL: GPU %v software %v", at(got, 32, 32), at(want, 32, 32))
	}
	// Reusing a sky leaves its GPU texture intact; transparent and unlit
	// surfaces must composite over it without acquiring diffuse illumination.
	cached := r.environmentTexture
	f.Revision++
	f.Geometry = []Geometry{quad(.5, .5, color.RGBA{255, 80, 40, 128}, &Material{Unlit: true, Transparent: true}, false)}
	got, err = r.Render(f)
	check(t, err)
	want = softwareRender(f)
	if !closeRGB(at(got, 32, 32), at(want, 32, 32), 3) {
		t.Fatalf("transparent surface: GPU %v software %v", at(got, 32, 32), at(want, 32, 32))
	}
	if cached != r.environmentTexture {
		t.Fatal("unchanged environment was uploaded again")
	}
	f.Environment = nil
	_, err = r.Render(f)
	check(t, err)
	if r.environmentTexture != nil {
		t.Fatal("environment texture retained")
	}
	f.Environment = testEnvironment(t)
	_, err = r.Render(f)
	check(t, err)
}
