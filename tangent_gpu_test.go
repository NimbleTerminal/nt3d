//go:build webgpu

package nt3d

import "testing"

// Tangents a scene generates at upload shade alike on the GPU and in software.
func TestGPUGeneratedTangents(t *testing.T) {
	s := NewScene()
	_, err := s.SetGeometry("mesh", normalFrame(t).Geometry[0])
	check(t, err)
	f, err := s.Frame(DefaultCamera(), 80, 80)
	check(t, err)
	f.Lighting = normalFrame(t).Lighting
	if len(f.Geometry[0].Tangents) != 4 {
		t.Fatal("scene did not generate tangents")
	}
	r := &gpuRenderer{}
	defer r.Close()
	got, err := r.Render(f)
	check(t, err)
	want := softwareRender(f)
	for _, p := range [][2]int{{40, 40}, {38, 39}, {45, 42}} {
		if !closeRGB(at(got, p[0], p[1]), at(want, p[0], p[1]), 4) {
			t.Fatalf("at %v GPU %v software %v", p, at(got, p[0], p[1]), at(want, p[0], p[1]))
		}
	}
	if at(want, 40, 40).R < 180 {
		t.Fatalf("generated frame did not tilt toward the light: %v", at(want, 40, 40))
	}
}
