// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"testing"
)

type indirectCase struct {
	name                                   string
	front, fallback                        [3]float32
	coverage, fallbackCoverage, visibility float32
	want                                   indirectSample
}

func indirectCases() []indirectCase {
	return []indirectCase{
		{"miss", [3]float32{8, 4, 2}, [3]float32{2, 3, 4}, 0, 1, 1, indirectSample{[3]float32{2, 3, 4}, 1}},
		{"black hit", [3]float32{}, [3]float32{2, 3, 4}, 1, 1, 1, indirectSample{[3]float32{}, 1}},
		{"HDR hit", [3]float32{8, 4, 2}, [3]float32{2, 3, 4}, 1, 1, 1, indirectSample{[3]float32{8, 4, 2}, 1}},
		{"partial hit", [3]float32{8, 4, 2}, [3]float32{2, 3, 4}, .25, 1, 1, indirectSample{[3]float32{3.5, 3.25, 3.5}, 1}},
		{"partial fallback", [3]float32{8, 4, 2}, [3]float32{2, 4, 8}, .5, .5, 1, indirectSample{[3]float32{4.5, 3, 3}, .75}},
		{"occluded hit blocks fallback", [3]float32{8, 4, 2}, [3]float32{2, 3, 4}, 1, 1, 0, indirectSample{[3]float32{}, 1}},
		{"occluded partial retains coverage", [3]float32{8, 4, 2}, [3]float32{2, 4, 8}, .5, 1, .25, indirectSample{[3]float32{2, 2.5, 4.25}, 1}},
	}
}

func TestIndirectCoverage(t *testing.T) {
	for _, tc := range indirectCases() {
		t.Run(tc.name, func(t *testing.T) {
			got := coveredIndirect(tc.front, tc.coverage).occluded(tc.visibility).over(coveredIndirect(tc.fallback, tc.fallbackCoverage))
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestIndirectCascade(t *testing.T) {
	// SSR replaces half the answer, a local probe covers half the remainder,
	// and the environment supplies the final quarter. AO cannot reopen a probe.
	screen := coveredIndirect([3]float32{4, 0, 0}, .5)
	probe := coveredIndirect([3]float32{0, 4, 0}, .5).occluded(.5)
	sky := coveredIndirect([3]float32{0, 0, 4}, 1)
	want := indirectSample{[3]float32{2, .5, 1}, 1}
	if got := screen.over(probe.over(sky)); got != want {
		t.Fatalf("cascade: %+v", got)
	}
	if got := screen.over(probe).over(sky); got != want {
		t.Fatalf("grouped cascade: %+v", got)
	}
}

func TestIndirectFallbacks(t *testing.T) {
	r := lightRig{sky: [3]float32{2, 4, 8}, ground: [3]float32{0, 0, 0}}
	up := nt3dmath.Vec3{Z: 1}
	if got := r.indirectDiffuse(up, .5); got != (indirectSample{[3]float32{1, 2, 4}, 1}) {
		t.Fatalf("diffuse: %+v", got)
	}
	if got := r.indirectSpecular(&Frame{}, up, 0, false, 1); got != (indirectSample{[3]float32{2, 4, 8}, 1}) {
		t.Fatalf("smooth: %+v", got)
	}
	if got := r.indirectSpecular(&Frame{}, up, 1, false, 1); got != (indirectSample{[3]float32{1, 2, 4}, 1}) {
		t.Fatalf("rough: %+v", got)
	}
	if got := r.indirectDiffuse(up, 0); got.coverage != 1 || got.radiance != ([3]float32{}) {
		t.Fatalf("dark fallback must remain complete: %+v", got)
	}
	// Background-only environments are excluded unless the material opts in.
	f := Frame{Environment: testSharpEnvironment(t), EnvironmentBackgroundOnly: true, Linear: true}
	zero := lightRig{}
	if got := zero.indirectSpecular(&f, nt3dmath.Vec3{X: 1}, 0, false, 1); got.radiance != ([3]float32{}) {
		t.Fatal("background-only environment lit a material")
	}
	if got := zero.indirectSpecular(&f, nt3dmath.Vec3{X: 1}, 0, true, 1); got.radiance[0] <= 0 || got.coverage != 1 {
		t.Fatal("explicit environment reflection lost")
	}
}
