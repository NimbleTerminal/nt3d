// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"testing"
)

func referenceOpaque8(v float32) uint8 {
	// Keep both original float32 rounding stages before the final byte.
	work := float32(encodeSRGB(float64(v))) * 255
	return uint8(max(0, min(255, work+.5)))
}

func TestOpaqueEncodingExact(t *testing.T) {
	check := func(v float32) {
		t.Helper()
		if got, want := encodeOpaque8(v), referenceOpaque8(v); got != want {
			t.Fatalf("input %.10g (%08x): %d != %d", v, math.Float32bits(v), got, want)
		}
	}
	// Every final byte transition and every coarse lookup bucket transition,
	// including the adjacent representable floats on both sides.
	for _, v := range []float32{-1, 0, 1, 2} {
		check(v)
	}
	for _, v := range opaqueEncodeBoundary {
		check(math.Nextafter32(v, 0))
		check(v)
		check(math.Nextafter32(v, 1))
	}
	for i := 0; i < encodeSteps; i++ {
		v := (float32(i) + .5) / encodeSteps
		check(math.Nextafter32(v, 0))
		check(v)
		check(math.Nextafter32(v, 1))
	}
	rng := rand.New(rand.NewPCG(11, 19))
	for i := 0; i < 1<<20; i++ {
		check(float32(i) / (1 << 20))
		check(math.Float32frombits(rng.Uint32N(math.Float32bits(1))))
	}
}

func TestDirectTextureOutputMatchesFloatPath(t *testing.T) {
	tex := samplingTexture(31, 17, false)
	for _, linear := range []bool{false, true} {
		for _, kind := range []string{"opaque", "cutout", "transparent", "fog", "lit", "normal", "outline"} {
			f := outlineFrame(t)
			f.Linear = linear
			f.Supersample = 2
			g := &f.Geometry[0]
			g.Outline.Width = 0
			g.Material = &Material{Texture: tex, Unlit: true, Repeat: true}
			for i := range g.Vertices {
				g.Vertices[i].Color = color.RGBA{255, 255, 255, 180}
			}
			switch kind {
			case "cutout":
				g.Material.Cutout = true
			case "transparent":
				g.Material.Transparent = true
			case "fog":
				f.Fog = Fog{Near: 1, Far: 5, Color: white}
			case "lit":
				g.Material.Unlit = false
				g.Material.Specular = .4
			case "normal":
				g.Material.NormalMap = testNormalMap(t, color.NRGBA{170, 120, 200, 255})
				g.Material.NormalStrength = 1
			case "outline":
				g.Outline = OutlineStyle{Width: 2, Color: outlineInk}
			}
			got := softwareRender(f).(*image.RGBA)
			// A white tint is mathematically identical but forces the full original
			// float conversion path, even on eligible opaque/unlit/encoded frames.
			g.Material.Tint = true
			want := softwareRender(f).(*image.RGBA)
			if !bytes.Equal(got.Pix, want.Pix) {
				t.Fatalf("linear=%v %s: direct and float paths differ", linear, kind)
			}
		}
	}
}

var encodeSink uint8

func BenchmarkOpaqueEncoding(b *testing.B) {
	for _, exact := range []bool{false, true} {
		name := "reference"
		if exact {
			name = "corrected lookup"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				v := float32(i%65536) / 65535
				if exact {
					encodeSink = encodeOpaque8(v)
				} else {
					encodeSink = referenceOpaque8(v)
				}
			}
		})
	}
}

func TestDirectTextureMovingCamera(t *testing.T) {
	tex := samplingTexture(512, 512, false)
	g := quad(1, 0, white, &Material{Texture: tex, Unlit: true}, false)
	// Texture dimensions, UVs, and camera match the map consumer. White
	// interpolation is not always exactly one, so this must retain its rounding.
	g.UV = []UV{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	s := NewScene()
	_, err := s.SetGeometry("map", g)
	check(t, err)
	camera := DefaultCamera()
	camera.Alpha = 55
	camera.Distance = 3.4
	camera.Projection = Perspective
	for i := 0; i < 144; i++ {
		camera.Beta = -90 + float64(i)*5
		f, err := s.Frame(camera, 320, 160)
		check(t, err)
		f.Linear = false
		f.Anisotropy = 4
		direct := softwareRender(f).(*image.RGBA)
		m := *f.Geometry[0].Material
		m.Tint = true
		f.Geometry[0].Material = &m
		reference := softwareRender(f).(*image.RGBA)
		if !bytes.Equal(direct.Pix, reference.Pix) {
			t.Fatalf("camera %v: interpolated white changed output", camera.Beta)
		}
	}
}
