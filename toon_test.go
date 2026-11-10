// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image/color"
	"math"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// toonRig is gray ambient a under one white directional light toward dir.
func toonRig(a float32, dir nt3dmath.Vec3) lightRig {
	return lightRig{shadowLight: -1, sky: [3]float32{a, a, a}, ground: [3]float32{a, a, a},
		lights: []rigLight{{kind: DirectionalLight, v: dir, color: [3]float32{1, 1, 1}}}}
}

func tilted(degrees float64) nt3dmath.Vec3 {
	r := degrees * math.Pi / 180
	return nt3dmath.Vec3{X: float32(math.Sin(r)), Z: float32(math.Cos(r))}
}

func toonLit(r *lightRig, t ToonStyle, n, view nt3dmath.Vec3) [3]float32 {
	c := [3]float32{1, 1, 1}
	r.toonLight(&c, t, n, nt3dmath.Vec3{}, n, view)
	return c
}

func TestToonBand(t *testing.T) {
	for x, want := range map[float32]float32{0: 0, .3: 0, .7: 1, 1: 1} {
		if got := toonBand(x, 2, .01); got != want {
			t.Fatalf("band(%v)=%v want %v", x, got, want)
		}
	}
	for x, want := range map[float32]float32{.1: 0, .5: .5, .9: 1} {
		if got := toonBand(x, 3, .01); abs(got-want) > 1e-6 {
			t.Fatalf("band(%v)=%v want %v", x, got, want)
		}
	}
	if a, b := toonBand(.495, 2, .1), toonBand(.505, 2, .1); !(a > 0 && a < b && b < 1) {
		t.Fatalf("edge is not a blend: %v %v", a, b)
	}
}

func TestToonValidation(t *testing.T) {
	for _, bands := range []int{0, 2, 8} {
		if err := (ToonStyle{Bands: bands}).validate(); err != nil {
			t.Fatalf("bands %d: %v", bands, err)
		}
	}
	nan := float32(math.NaN())
	for _, bad := range []ToonStyle{{Bands: 1}, {Bands: 9}, {Bands: -1}, {Bands: 2, Softness: nan}, {Bands: 2, Softness: -.1}, {Bands: 2, Softness: 1.1}, {Bands: 2, Rim: nan}, {Bands: 2, Rim: -.1}, {Bands: 2, Rim: 1.1}} {
		if bad.validate() == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	g, err := BuildSphere(Sphere{Radius: 1, Segments: 8, Rings: 6, Color: white, Material: &Material{Toon: ToonStyle{Bands: 3}}})
	check(t, err)
	check(t, g.Validate())
	g.Material = &Material{Toon: ToonStyle{Bands: 1}}
	if g.Validate() == nil {
		t.Fatal("geometry accepted an invalid toon style")
	}
}

func TestToonLightBands(t *testing.T) {
	r := toonRig(.2, up)
	two := ToonStyle{Bands: 2}
	for degrees, want := range map[float64]float32{0: 1.2, 45: 1.2, 80: .2} {
		if got := toonLit(&r, two, tilted(degrees), up); !near3(got, [3]float32{want, want, want}, 1e-5) {
			t.Fatalf("%v degrees: %v want %v", degrees, got, want)
		}
	}
	// Distance fade is not banded: a point light with fade 1/(1+3) adds a quarter.
	p := lightRig{shadowLight: -1, points: true, lights: []rigLight{{kind: PointLight, v: nt3dmath.Vec3{Z: float32(math.Sqrt(3))}, color: [3]float32{1, 1, 1}}}}
	if got := toonLit(&p, two, up, up); !near3(got, [3]float32{.25, .25, .25}, 1e-5) {
		t.Fatalf("point light: %v want .25", got)
	}
}

func TestToonShadeAndNoLights(t *testing.T) {
	shade := ToonStyle{Bands: 2, Shade: color.RGBA{128, 128, 255, 255}}
	want := [3]float32{.4 * 128 / 255, .4 * 128 / 255, .4}
	none := lightRig{shadowLight: -1, sky: [3]float32{.4, .4, .4}, ground: [3]float32{.4, .4, .4}}
	if got := toonLit(&none, shade, up, up); !near3(got, want, 1e-5) {
		t.Fatalf("no lights: %v want %v", got, want)
	}
	behind := toonRig(.4, nt3dmath.Vec3{Z: -1})
	if got := toonLit(&behind, shade, up, up); !near3(got, want, 1e-5) {
		t.Fatalf("light behind: %v want %v", got, want)
	}
	if got := toonLit(&none, ToonStyle{Bands: 2}, up, up); !near3(got, [3]float32{.4, .4, .4}, 1e-5) {
		t.Fatalf("unset shade is not white: %v", got)
	}
}

func TestToonHighlightIsFlat(t *testing.T) {
	r := toonRig(0, up)
	m := &Material{Specular: 1, Shininess: 32, Toon: ToonStyle{Bands: 2}}
	if got := r.toonHighlight(m, up, nt3dmath.Vec3{}, up, up); got != [3]float32{1, 1, 1} {
		t.Fatalf("mirror highlight %v", got)
	}
	if got := r.toonHighlight(m, up, nt3dmath.Vec3{}, tilted(60), up); got != [3]float32{} {
		t.Fatalf("off-mirror highlight %v", got)
	}
	if soft := r.highlightNormal(m, up, nt3dmath.Vec3{}, tilted(10), up); !(soft[0] > 0 && soft[0] < 1) {
		t.Fatalf("smooth highlight %v", soft)
	}
}

func TestToonRimOnlyWhereLit(t *testing.T) {
	side := nt3dmath.Vec3{X: 1}
	rim := ToonStyle{Bands: 2, Rim: .5}
	lit := toonRig(.2, up)
	if got := toonLit(&lit, rim, up, side); !near3(got, [3]float32{1.7, 1.7, 1.7}, 1e-5) {
		t.Fatalf("lit rim %v want 1.7", got)
	}
	dark := toonRig(.2, nt3dmath.Vec3{Z: -1})
	if got := toonLit(&dark, rim, up, side); !near3(got, [3]float32{.2, .2, .2}, 1e-5) {
		t.Fatalf("unlit rim %v want .2", got)
	}
	if got := toonLit(&lit, rim, up, up); !near3(got, [3]float32{1.2, 1.2, 1.2}, 1e-5) {
		t.Fatalf("facing rim %v want 1.2", got)
	}
}
