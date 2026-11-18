// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image/color"
	"math"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"testing"
)

func TestEnvironmentReflectionFresnel(t *testing.T) {
	e := GradientEnvironment(color.RGBA{255, 255, 255, 255}, color.RGBA{255, 255, 255, 255}, color.RGBA{255, 255, 255, 255})
	for _, linear := range []bool{false, true} {
		var face, edge [3]float32
		reflectEnvironment(&face, e, .02, 1, nt3dmath.Vec3{Z: 1}, nt3dmath.Vec3{Z: 1}, linear)
		reflectEnvironment(&edge, e, .02, 1, nt3dmath.Vec3{Z: 1}, nt3dmath.Vec3{X: 1}, linear)
		if math.Abs(float64(face[0])-5.1) > .001 || math.Abs(float64(edge[0])-255) > .001 {
			t.Fatal(face, edge)
		}
		// A back-facing normal shades like its front: never a full mirror.
		var back [3]float32
		reflectEnvironment(&back, e, .02, 1, nt3dmath.Vec3{Z: -1}, nt3dmath.Vec3{Z: 1}, linear)
		if back != face {
			t.Fatal("back face mirrored the sky", back, face)
		}
	}
	f := normalFrame(t)
	f.Forward = nt3dmath.Vec3{Z: -1}
	f.Environment = e
	m := f.Draws[0].Geometry.Material
	m.NormalStrength = 0
	m.Reflectivity = 1
	f.Lighting = Lighting{}
	if c := at(softwareRender(f), 40, 40); c.R < 254 {
		t.Fatal("mirror did not reflect sky", c)
	}
	f.Environment = nil
	if c := at(softwareRender(f), 40, 40); c.R != 0 {
		t.Fatal("missing sky changed shading", c)
	}
	for _, v := range []float32{-1, 2, float32(math.NaN())} {
		m.Reflectivity = v
		if err := f.Draws[0].Geometry.Validate(); err == nil {
			t.Fatal("accepted reflection", v)
		}
	}
}

func TestShaderNormalMapData(t *testing.T) {
	fallback := testNormalMap(t, color.NRGBA{128, 128, 255, 255})
	tex, err := NewShaderNormalMap("fn nt3dTexture(uv:vec2<f32>)->vec4<f32>{return vec4<f32>(0.5,0.5,1.0,1.0);}", fallback)
	check(t, err)
	if !tex.data || tex.levels()[1].NRGBAAt(0, 0).R != 128 {
		t.Fatal("normal data underwent color conversion")
	}
	if _, err := NewShaderNormalMapWithParameters("", [4]float32{}, fallback); err == nil {
		t.Fatal("accepted empty shader")
	}
	if _, err := NewShaderNormalMapWithParameters("x", [4]float32{float32(math.NaN())}, fallback); err == nil {
		t.Fatal("accepted non-finite parameter")
	}
}

// A background-only environment is seen and nothing more: it neither lights
// surfaces nor is reflected by them. A surface that should reflect the sky all
// the same is given a reflection probe holding it, which is independent of
// the setting.
func TestBackgroundOnlyEnvironmentIsNotReflected(t *testing.T) {
	sky := GradientEnvironment(color.RGBA{240, 80, 40, 255}, color.RGBA{220, 120, 50, 255}, color.RGBA{40, 80, 220, 255})
	backdrop := func(m *Material) Frame {
		f := normalFrame(t)
		f.Forward = nt3dmath.Vec3{Z: -1}
		f.Environment = sky
		f.EnvironmentBackgroundOnly = true
		f.Lighting = Lighting{}
		f.Draws[0].Geometry.Material = m
		return f
	}
	center := func(f Frame) color.RGBA { return at(softwareRender(f), 40, 40) }

	// A matte surface receives no diffuse light from the sky.
	if c := center(backdrop(&Material{})); c.R != 0 || c.G != 0 || c.B != 0 {
		t.Fatalf("a matte surface is lit by a background-only sky: %v", c)
	}
	// A mirror, classic or PBR, does not reflect it.
	if c := center(backdrop(&Material{Reflectivity: 1})); c.R != 0 {
		t.Fatalf("a reflective surface reflects a background-only sky: %v", c)
	}
	pbrMirror := &Material{PBR: true, Metallic: 1, Roughness: .05}
	lit := backdrop(pbrMirror)
	lit.EnvironmentBackgroundOnly = false
	if only, full := center(backdrop(pbrMirror)).R, center(lit).R; int(only)+100 >= int(full) {
		t.Fatalf("a PBR mirror reflects a background-only sky: red %d, and %d with the environment lighting the frame", only, full)
	}

	// A probe holding the sky makes both reflect it, the frame still unlit by it.
	probes := []ReflectionProbe{{Environment: sky, Position: nt3dmath.Vec3{Z: 1}, Bounds: probeBox(10)}}
	water := backdrop(&Material{Reflectivity: 1})
	water.ReflectionProbes = probes
	if c := center(water); c.R < 200 {
		t.Fatalf("a probe of the sky is not reflected under a background-only environment: %v", c)
	}
	metal := backdrop(pbrMirror)
	metal.ReflectionProbes = probes
	if with, without := center(metal).R, center(backdrop(pbrMirror)).R; int(with) <= int(without)+100 {
		t.Fatalf("a PBR mirror does not reflect a probe of the sky: red %d, and %d without the probe", with, without)
	}
	matte := backdrop(&Material{})
	matte.ReflectionProbes = probes
	if c := center(matte); c.R != 0 || c.G != 0 || c.B != 0 {
		t.Fatalf("a probe lit a matte surface: %v", c)
	}
}

// probeBox is a cube of the given half size about the origin.
func probeBox(half float32) nt3dmath.AABB {
	var b nt3dmath.AABB
	b.Include(nt3dmath.Vec3{X: -half, Y: -half, Z: -half})
	b.Include(nt3dmath.Vec3{X: half, Y: half, Z: half})
	return b
}
