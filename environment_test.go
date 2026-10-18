package nt3d

import (
	"bytes"
	"image"
	"image/color"
	"math"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"strings"
	"testing"
)

func testEnvironment(t *testing.T) *Environment {
	t.Helper()
	img := image.NewNRGBA(image.Rect(-4, 7, 60, 39))
	for y := 7; y < 39; y++ {
		for x := -4; x < 60; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8((x + 4) * 4), uint8((y - 7) * 8), 96, 255})
		}
	}
	e, err := NewEnvironment(img)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestEnvironmentSnapshotAndBounds(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	e, err := NewEnvironment(img)
	check(t, err)
	img.SetNRGBA(4, 2, color.NRGBA{})
	s := NewScene()
	before := s.Revision()
	bounds := s.Bounds()
	s.SetEnvironment(e)
	if s.Revision() == before || s.Bounds() != bounds {
		t.Fatal("environment revision/bounds")
	}
	f, err := s.Frame(DefaultCamera(), 32, 24)
	check(t, err)
	if len(f.Geometry) != 0 || s.Pick(DefaultCamera(), f, 16, 12, 1) != nil {
		t.Fatal("environment is pickable geometry")
	}
	s.SetEnvironment(nil)
	if f.Environment != e || f.skyPixel(16, 12) != (color.RGBA{255, 255, 255, 255}) {
		t.Fatal("snapshot lost ownership")
	}
	for _, linear := range []bool{false, true} {
		f.Linear = linear
		f.Lighting = Lighting{}
		rig := newRig(&f)
		for _, n := range []nt3dmath.Vec3{{X: 1}, {Y: -1}, {Z: 1}, {Z: -1}} {
			rgb := [3]float32{1, 1, 1}
			rig.light(&rgb, n, nt3dmath.Vec3{})
			for _, c := range rgb {
				if math.Abs(float64(c-1)) > .002 {
					t.Fatalf("constant irradiance: %v", rgb)
				}
			}
		}
	}
	f.EnvironmentBackgroundOnly = true
	rig := newRig(&f)
	rgb := [3]float32{1, 1, 1}
	rig.light(&rgb, nt3dmath.Vec3{Z: 1}, nt3dmath.Vec3{})
	if rgb != ([3]float32{}) {
		t.Fatal("background-only environment lit surfaces")
	}
}

func TestEnvironmentSkyOrientation(t *testing.T) {
	s := NewScene()
	s.SetEnvironment(testEnvironment(t))
	camera := DefaultCamera()
	f, err := s.Frame(camera, 64, 48)
	check(t, err)
	original := softwareRender(f).(*image.RGBA)
	camera.Target = nt3dmath.Vec3{X: 10, Y: 5, Z: -2}
	camera.Distance *= 2
	moved, err := s.Frame(camera, 64, 48)
	check(t, err)
	if !bytes.Equal(original.Pix, softwareRender(moved).(*image.RGBA).Pix) {
		t.Fatal("sky moved with translation/zoom")
	}
	camera.Beta += 90
	rotated, err := s.Frame(camera, 64, 48)
	check(t, err)
	if bytes.Equal(original.Pix, softwareRender(rotated).(*image.RGBA).Pix) {
		t.Fatal("sky did not rotate")
	}
	f.Supersample = 2
	_, depth := softwareRenderDepth(f)
	for _, d := range depth {
		if d != noDepth {
			t.Fatal("sky wrote depth")
		}
	}
	wire, err := RenderWireframe(f, 20, 10)
	check(t, err)
	if !strings.Contains(wire, "48;") {
		t.Fatal("wireframe missing sky backgrounds")
	}
}

func TestEnvironmentValidation(t *testing.T) {
	if _, err := NewEnvironment(nil); err == nil {
		t.Fatal("accepted nil image")
	}
	f := flatFrame(16, 16)
	f.Environment = &Environment{}
	if _, err := NewSoftwareRenderer().Render(f); err == nil {
		t.Fatal("accepted empty environment")
	}
	f.Environment = testEnvironment(t)
	for _, strength := range []float32{-1, float32(math.NaN()), float32(math.Inf(1))} {
		f.EnvironmentStrength = strength
		if _, err := NewSoftwareRenderer().Render(f); err == nil {
			t.Fatal("accepted invalid strength")
		}
	}
}

func TestEnvironmentDirectionalDiffuse(t *testing.T) {
	e := GradientEnvironment(color.RGBA{255, 32, 16, 255}, color.RGBA{128, 32, 128, 255}, color.RGBA{16, 32, 255, 255})
	f := flatFrame(8, 8)
	f.Lighting = Lighting{}
	f.Environment = e
	rig := newRig(&f)
	top, bottom := [3]float32{1, 1, 1}, [3]float32{1, 1, 1}
	rig.light(&top, nt3dmath.Vec3{Z: 1}, nt3dmath.Vec3{})
	rig.light(&bottom, nt3dmath.Vec3{Z: -1}, nt3dmath.Vec3{})
	if top[0] <= top[2] || bottom[2] <= bottom[0] {
		t.Fatalf("direction lost: up %v down %v", top, bottom)
	}
	f.EnvironmentStrength = .5
	rig = newRig(&f)
	half := [3]float32{1, 1, 1}
	rig.light(&half, nt3dmath.Vec3{Z: 1}, nt3dmath.Vec3{})
	for i := range half {
		if math.Abs(float64(half[i]-top[i]*.5)) > 1e-6 {
			t.Fatal("strength did not scale diffuse")
		}
	}
}
