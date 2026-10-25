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
	m := f.Geometry[0].Material
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
		if err := f.Geometry[0].Validate(); err == nil {
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
