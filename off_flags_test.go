// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image"
	"image/color"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// offCase is three frames of one scene: with a thing on, with it turned Off,
// and without it at all.
type offCase struct {
	name             string
	on, off, without func(*Frame)
}

// offCases are the things a frame can have turned off: a light, the light
// that casts shadows, ambient light, and a reflection probe.
func offCases(t *testing.T) []offCase {
	key := Light{Kind: DirectionalLight, Direction: nt3dmath.Vec3{X: 1, Y: -1, Z: 2}.Normalize(), Intensity: .6}
	fill := Light{Kind: PointLight, Position: nt3dmath.Vec3{X: -2, Y: -2, Z: 1}, Color: color.RGBA{255, 120, 60, 255}, Intensity: 3}
	caster := Light{Kind: SpotLight, Position: nt3dmath.Vec3{Z: 4}, Direction: nt3dmath.Vec3{Z: -1}, Intensity: 6, Shadows: true}
	dim := Ambient{Sky: color.RGBA{20, 20, 20, 255}, Ground: color.RGBA{20, 20, 20, 255}, Intensity: 1}
	bright := Ambient{Sky: color.RGBA{200, 220, 255, 255}, Ground: color.RGBA{90, 80, 70, 255}, Intensity: 1}
	red, green := testProbe(t, color.NRGBA{255, 0, 0, 255}), testProbe(t, color.NRGBA{0, 255, 0, 255})
	lights := func(a Ambient, l ...Light) func(*Frame) {
		return func(f *Frame) { f.Lighting = Lighting{Ambient: a, Lights: l} }
	}
	probes := func(p ...ReflectionProbe) func(*Frame) {
		return func(f *Frame) { lights(dim, key)(f); f.ReflectionProbes = p }
	}
	off := func(l Light) Light { l.Off = true; return l }
	offProbe := func(p ReflectionProbe) ReflectionProbe { p.Off = true; return p }
	return []offCase{
		{"light", lights(dim, key, fill), lights(dim, key, off(fill)), lights(dim, key)},
		// An Off light is zero however bright it is said to be.
		{"light, whatever its intensity", lights(dim, key, fill), lights(dim, key, Light{Kind: PointLight, Position: fill.Position, Intensity: 1000, Off: true}), lights(dim, key)},
		// The light that casts shadows is the first with Shadows that is on.
		{"shadow caster", lights(dim, caster, key), lights(dim, off(caster), key), lights(dim, key)},
		{"ambient", lights(bright, key), lights(Ambient{Sky: bright.Sky, Ground: bright.Ground, Intensity: 1, Off: true}, key), lights(Ambient{Sky: color.RGBA{A: 255}, Ground: color.RGBA{A: 255}, Intensity: 1}, key)},
		{"probe", probes(red), probes(offProbe(red)), probes()},
		// An Off probe is passed over, so the next one that holds the surface lights it.
		{"probe ahead of another", probes(red, green), probes(offProbe(red), green), probes(green)},
	}
}

// offScene is a shiny ball over a floor, which shows lights, shadows, ambient
// light, and reflections.
func offScene(t *testing.T) Frame {
	t.Helper()
	s := NewScene()
	floor, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 3, Y: 3, Z: .1}, Color: color.RGBA{200, 200, 200, 255}})
	check(t, err)
	id, err := s.SetGeometry("floor", floor)
	check(t, err)
	check(t, s.SetTransform(id, nt3dmath.Translation(nt3dmath.Vec3{Z: -.8})))
	ball, err := BuildSphere(Sphere{Radius: .6, Segments: 24, Rings: 12, Color: color.RGBA{180, 180, 190, 255}, Material: &Material{Phong: PhongStyle{Reflectivity: .6}}})
	check(t, err)
	_, err = s.SetGeometry("ball", ball)
	check(t, err)
	f, err := s.StillFrame(DefaultCamera(), 64, 64)
	check(t, err)
	return f
}

// checkOffFlags holds, for a renderer, that a thing turned Off draws exactly
// the picture drawn without it, and that it made a difference while on.
func checkOffFlags(t *testing.T, render func(Frame) image.Image) {
	base := offScene(t)
	for _, c := range offCases(t) {
		t.Run(c.name, func(t *testing.T) {
			draw := func(set func(*Frame)) image.Image {
				f := base
				set(&f)
				return render(f)
			}
			on, off, without := draw(c.on), draw(c.off), draw(c.without)
			if sameImage(on, without) {
				t.Fatal("the scene does not show it: on and absent draw alike")
			}
			if !sameImage(off, without) {
				t.Fatal("turned off, it still changes the picture")
			}
		})
	}
}

// A light, ambient light, or reflection probe that is Off adds nothing: the
// frame is drawn as if it were not there. Off is how to ask for none, since a
// zero Intensity or Strength means the default.
func TestOffAddsNothingInSoftware(t *testing.T) {
	r := NewSoftwareRenderer()
	defer r.Close()
	checkOffFlags(t, func(f Frame) image.Image {
		img, err := r.Render(f)
		check(t, err)
		return img
	})
}

// A light that is Off keeps its place in the list, so turning one off and on
// does not disturb the others, and it still has to be a valid light.
func TestOffLightIsStillChecked(t *testing.T) {
	if !validLighting(Lighting{Lights: []Light{{Kind: PointLight, Off: true}}}) {
		t.Fatal("a point light that is off was refused")
	}
	if validLighting(Lighting{Lights: []Light{{Kind: DirectionalLight, Off: true}}}) {
		t.Fatal("a directional light with no direction was accepted because it is off")
	}
}

// Bloom.NoThreshold lets every pixel glow, which a zero Threshold cannot ask
// for because zero means the default.
func TestBloomNoThresholdLetsEveryPixelGlow(t *testing.T) {
	if got := (Bloom{Strength: 1}).threshold(); got != DefaultBloomThreshold {
		t.Fatalf("zero threshold is %v, want the default", got)
	}
	if got := (Bloom{Strength: 1, Threshold: 2, NoThreshold: true}).threshold(); got != 0 {
		t.Fatalf("NoThreshold left a threshold of %v", got)
	}
	glow := func(b Bloom) float32 {
		const w, h = 16, 16
		hdr := make([]float32, w*h*3)
		for i := range hdr {
			hdr[i] = .2 // Well under the default threshold.
		}
		softwareBloom(w, h, hdr, b)
		return hdr[(h/2*w+w/2)*3] - .2
	}
	if d := glow(Bloom{Strength: 1}); d > 1e-3 {
		t.Fatalf("a dim pixel glowed by %v under the default threshold", d)
	}
	if d := glow(Bloom{Strength: 1, NoThreshold: true}); !(d >= .05) { // Not NaN, either.
		t.Fatalf("a dim pixel glowed by only %v with no threshold", d)
	}
}

// A light too far from the camera's Origin to place in the frame's float32
// coordinates does not keep the frame from drawing. A directional light has
// no use for a position and keeps shining; a point or spot light that far
// lights nothing, and is off in the frame, keeping its place among the
// lights. Both a look's lights and light nodes are held to it.
func TestLightsFarFromOriginDoNotStopTheFrame(t *testing.T) {
	far := [3]float64{0, 1e39, 0}
	camera := DefaultCamera()
	camera.Origin = far
	ball, err := BuildSphere(Sphere{Radius: .5, Segments: 8, Rings: 4, Color: color.RGBA{A: 255}})
	check(t, err)
	sun := Light{Kind: DirectionalLight, Direction: nt3dmath.Vec3{Z: 1}}
	lamp := Light{Kind: PointLight, Position: nt3dmath.Vec3{X: 1}}
	for name, place := range map[string]func(*Scene){
		"look lights": func(s *Scene) {
			look := s.Look()
			look.Lighting.Lights = []Light{sun, lamp}
			s.SetLook(look)
		},
		"light nodes": func(s *Scene) {
			for i, l := range []Light{sun, lamp} {
				id, err := s.Add(Root, []string{"sun", "lamp"}[i])
				check(t, err)
				check(t, s.SetNodeLight(id, l))
			}
		},
	} {
		s := NewScene()
		id, err := s.SetGeometry("ball", ball)
		check(t, err)
		check(t, s.SetNodeOrigin(id, far))
		place(s)
		f, err := s.StillFrame(camera, 16, 16)
		check(t, err)
		if err := validateFrame(f); err != nil {
			t.Errorf("%s: a far light keeps the whole frame from drawing: %v", name, err)
			continue
		}
		lights := f.Lighting.Lights
		if len(lights) != 2 || lights[0].Off || !lights[1].Off {
			t.Errorf("%s: want the sun on and the lamp off, got %+v", name, lights)
		}
	}
}
