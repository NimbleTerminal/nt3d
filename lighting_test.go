// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

var (
	white = color.RGBA{255, 255, 255, 255}
	up    = nt3dmath.Vec3{Z: 1}
)

// illuminate is the light reaching a surface: what rig.light multiplies
// white by.
func illuminate(r *lightRig, n, p nt3dmath.Vec3) [3]float32 {
	c := [3]float32{1, 1, 1}
	r.light(&c, n, p)
	return c
}

func near3(got [3]float32, want [3]float32, tolerance float32) bool {
	for i := range got {
		if math.Abs(float64(got[i]-want[i])) > float64(tolerance) || math.IsNaN(float64(got[i])) {
			return false
		}
	}
	return true
}

// The default rig lights a linear frame as the single light before it did:
// ambient a (.3 decoded) plus (1-a) times the cosine to the key light.
func TestDefaultLightingMatchesOldLight(t *testing.T) {
	f := Frame{Linear: true, Lighting: DefaultLighting()}
	r := newRig(&f)
	key := f.Lighting.Lights[0].Direction.Normalize()
	a := float32(decodeSRGB(.3))
	// Normals at cosines 1, .5, and 0 to the key light, in the plane of the
	// key light and a perpendicular to it.
	side := key.Cross(nt3dmath.Vec3{X: 1}).Normalize()
	for _, cos := range []float32{1, .5, 0} {
		n := key.Scale(cos).Add(side.Scale(float32(math.Sqrt(float64(1 - cos*cos))))).Normalize()
		want := a + (1-a)*cos
		if got := illuminate(&r, n, nt3dmath.Vec3{}); !near3(got, [3]float32{want, want, want}, .003) {
			t.Errorf("cos %.1f: %v, want %.4f", cos, got, want)
		}
	}
}

// Ambient blends from the ground's color, facing down, to the sky's.
func TestRigHemisphere(t *testing.T) {
	f := Frame{Lighting: Lighting{Ambient: Ambient{Sky: color.RGBA{255, 0, 0, 255}, Ground: color.RGBA{0, 0, 255, 255}}}}
	r := newRig(&f)
	for _, c := range []struct {
		n    nt3dmath.Vec3
		want [3]float32
	}{{up, [3]float32{1, 0, 0}}, {nt3dmath.Vec3{Z: -1}, [3]float32{0, 0, 1}}, {nt3dmath.Vec3{X: 1}, [3]float32{.5, 0, .5}}} {
		if got := illuminate(&r, c.n, nt3dmath.Vec3{}); !near3(got, c.want, .001) {
			t.Errorf("normal %v: %v, want %v", c.n, got, c.want)
		}
	}
}

// Lights add, each tinted by its color and scaled by its intensity.
func TestRigLightsAdd(t *testing.T) {
	f := Frame{Lighting: Lighting{Lights: []Light{
		{Direction: up, Intensity: .25},
		{Direction: up, Intensity: .5, Color: color.RGBA{255, 0, 0, 255}},
	}}}
	r := newRig(&f)
	if got := illuminate(&r, up, nt3dmath.Vec3{}); !near3(got, [3]float32{.75, .25, .25}, .001) {
		t.Fatalf("two lights overhead: %v", got)
	}
}

// Point lights fade with distance: smoothly to nothing at Range, or as
// 1/(1+d²) without one.
func TestRigPointFalloff(t *testing.T) {
	light := func(rng float32) lightRig {
		f := Frame{Lighting: Lighting{Lights: []Light{{Kind: PointLight, Position: nt3dmath.Vec3{Z: 1}, Range: rng}}}}
		return newRig(&f)
	}
	r := light(0)
	if got := illuminate(&r, up, nt3dmath.Vec3{}); !near3(got, [3]float32{.5, .5, .5}, .001) {
		t.Errorf("one unit below, no range: %v, want .5", got)
	}
	r = light(2)
	want := float32((1 - .25) * (1 - .25))
	if got := illuminate(&r, up, nt3dmath.Vec3{}); !near3(got, [3]float32{want, want, want}, .001) {
		t.Errorf("one unit below, range 2: %v, want %.4f", got, want)
	}
	if got := illuminate(&r, up, nt3dmath.Vec3{Z: -2}); !near3(got, [3]float32{}, 0) {
		t.Errorf("beyond range: %v", got)
	}
	// Facing away: nothing.
	if got := illuminate(&r, nt3dmath.Vec3{Z: -1}, nt3dmath.Vec3{}); !near3(got, [3]float32{}, 0) {
		t.Errorf("facing away: %v", got)
	}
}

// A point light on the surface itself has no direction: it adds nothing,
// and no NaN.
func TestRigPointLightAtSurface(t *testing.T) {
	f := Frame{Lighting: Lighting{Ambient: Ambient{Sky: white, Ground: white, Intensity: .1}, Lights: []Light{{Kind: PointLight}}}}
	r := newRig(&f)
	if got := illuminate(&r, up, nt3dmath.Vec3{}); !near3(got, [3]float32{.1, .1, .1}, .001) {
		t.Fatalf("light at the surface: %v", got)
	}
	m := &Material{Specular: 1}
	if got := r.highlight(m, up, nt3dmath.Vec3{}, up); !near3(got, [3]float32{}, 0) {
		t.Fatalf("highlight from a light at the surface: %v", got)
	}
}

// Highlights come from each light, tinted by it and faded as its diffuse
// light is; none from a light behind the surface.
func TestRigHighlight(t *testing.T) {
	m := &Material{Specular: .8, Shininess: 32}
	f := Frame{Lighting: Lighting{Lights: []Light{{Kind: PointLight, Position: nt3dmath.Vec3{Z: 1}, Color: color.RGBA{0, 255, 0, 255}}}}}
	r := newRig(&f)
	if got := r.highlight(m, up, nt3dmath.Vec3{}, up); !near3(got, [3]float32{0, .4, 0}, .001) {
		t.Errorf("green point light overhead: %v, want 0 .4 0", got)
	}
	f.Lighting.Lights[0].Position = nt3dmath.Vec3{Z: -1}
	r = newRig(&f)
	if got := r.highlight(m, up, nt3dmath.Vec3{}, up); !near3(got, [3]float32{}, 0) {
		t.Errorf("light behind: %v", got)
	}
}

// pointQuad is a white quad facing up at z = .3, seen from straight above,
// lit by one point light .3 above its middle, with ambient a.
func pointQuad(t *testing.T, rng, a float32) Frame {
	t.Helper()
	f := blendScene(t, nil, quad(1.2, .3, white, nil, false))
	f.Lighting = Lighting{Ambient: Ambient{Sky: white, Ground: white, Intensity: a}, Lights: []Light{{Kind: PointLight, Position: nt3dmath.Vec3{Z: .6}, Range: rng}}}
	if a == 0 {
		f.Lighting.Ambient = Ambient{}
	}
	return f
}

// Diffuse light is found per pixel: a point light just above a two-triangle
// quad is brightest under it, where no vertex is.
func TestPointLightHotspot(t *testing.T) {
	img := softwareRender(pointQuad(t, 0, 0))
	middle, corner := at(img, 40, 40), at(img, 40+18, 40+18)
	if int(middle.R) < int(corner.R)+100 {
		t.Fatalf("under the light %v, toward a corner %v", middle, corner)
	}
}

// Beyond its range a point light adds nothing: there is only ambient.
func TestPointLightRange(t *testing.T) {
	lit := pointQuad(t, .5, .2)
	dark := lit
	dark.Lighting.Lights = nil
	img, ambient := softwareRender(lit), softwareRender(dark)
	if got, want := at(img, 40+18, 40+18), at(ambient, 40+18, 40+18); !closeRGB(got, want, 1) {
		t.Errorf("beyond range %v, ambient alone %v", got, want)
	}
	if got, want := at(img, 40, 40), at(ambient, 40, 40); int(got.R) < int(want.R)+50 {
		t.Errorf("under the light %v, ambient alone %v", got, want)
	}
}

// A red light on a white surface shows red.
func TestLightColor(t *testing.T) {
	f := blendScene(t, nil, quad(1.2, .3, white, nil, false))
	f.Lighting = Lighting{Lights: []Light{{Direction: up, Color: color.RGBA{255, 0, 0, 255}}}}
	if got := at(softwareRender(f), 40, 40); !closeRGB(got, color.RGBA{255, 0, 0, 255}, 2) {
		t.Fatalf("white under red light: %v", got)
	}
}

// Without light, lit surfaces are black; unlit materials, points, and lines
// keep their colors.
func TestUnlitIgnoresLighting(t *testing.T) {
	red := color.RGBA{200, 0, 0, 255}
	g := Geometry{
		Points:    []Point{{Position: nt3dmath.Vec3{X: -.5, Z: .4}, Radius: 4, Color: red}},
		Lines:     []Vertex{{Position: nt3dmath.Vec3{X: .5, Y: -.5, Z: .4}, Color: red}, {Position: nt3dmath.Vec3{X: .5, Y: .5, Z: .4}, Color: red}},
		LineWidth: 3,
	}
	f := blendScene(t, nil, quad(.3, .3, white, &Material{Unlit: true}, false), quad(1.2, .2, white, nil, false), g)
	f.Lighting = Lighting{}
	img := softwareRender(f)
	if got := at(img, 40, 40); !closeRGB(got, white, 1) {
		t.Errorf("unlit quad: %v", got)
	}
	x, y, _, _ := f.Matrix.Project(nt3dmath.Vec3{X: .9, Y: .9, Z: .2}, f.Width, f.Height)
	if got := at(img, int(x), int(y)); !closeRGB(got, color.RGBA{0, 0, 0, 255}, 1) {
		t.Errorf("lit quad without light: %v", got)
	}
	for name, w := range map[string]nt3dmath.Vec3{"point": {X: -.5, Z: .4}, "line": {X: .5, Z: .4}} {
		x, y, _, _ := f.Matrix.Project(w, f.Width, f.Height)
		if got := at(img, int(x), int(y)); got.R < 150 {
			t.Errorf("%s darkened: %v", name, got)
		}
	}
}

func TestLightingValidation(t *testing.T) {
	for name, l := range map[string]Lighting{
		"nine lights":        {Lights: make([]Light, MaxLights+1)},
		"unknown kind":       {Lights: []Light{{Kind: 7}}},
		"NaN position":       {Lights: []Light{{Kind: PointLight, Position: nt3dmath.Vec3{X: float32(math.NaN())}}}},
		"negative range":     {Lights: []Light{{Kind: PointLight, Range: -1}}},
		"negative intensity": {Lights: []Light{{Direction: up, Intensity: -1}}},
		"infinite ambient":   {Ambient: Ambient{Intensity: float32(math.Inf(1))}},
		// A light written without its Kind is directional, and with no
		// Direction would light nothing: a point light missing its Kind.
		"no direction": {Lights: []Light{{Position: nt3dmath.Vec3{Z: 1}, Range: 2}}},
	} {
		f := blendScene(t, nil)
		f.Lighting = l
		if _, err := NewSoftwareRenderer().Render(f); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	f := blendScene(t, nil)
	f.Lighting.Lights = make([]Light, MaxLights)
	for i := range f.Lighting.Lights {
		f.Lighting.Lights[i].Direction = up
	}
	if _, err := NewSoftwareRenderer().Render(f); err != nil {
		t.Errorf("eight lights: %v", err)
	}
}

// A lit sphere filling much of a 640x480 frame, in software, as the widget
// draws when no GPU is available.
func BenchmarkSoftwareLitSphere(b *testing.B) {
	for name, l := range map[string]Lighting{
		"default": DefaultLighting(),
		"point":   {Ambient: Ambient{Sky: white, Ground: white, Intensity: .2}, Lights: []Light{{Kind: PointLight, Position: nt3dmath.Vec3{X: 1, Y: -1, Z: 1}, Range: 4}}},
	} {
		b.Run(name, func(b *testing.B) {
			s := NewScene()
			ball, err := BuildSphere(Sphere{Radius: .9, Color: gray})
			if err != nil {
				b.Fatal(err)
			}
			if _, err := s.SetGeometry("ball", ball); err != nil {
				b.Fatal(err)
			}
			f, err := s.Frame(DefaultCamera(), 640, 480)
			if err != nil {
				b.Fatal(err)
			}
			f.Lighting, f.Complete = l, true
			for b.Loop() {
				softwareRender(f)
			}
		})
	}
}

// The shader sizes its lights by MaxLights, given it by Go, so the two
// cannot disagree about the uniforms' layout.
func TestShaderMaxLights(t *testing.T) {
	src := sceneShaderSource()
	if want := fmt.Sprintf("const maxLights: u32 = %du;", MaxLights); !strings.HasPrefix(src, want) {
		t.Fatalf("shader does not begin with %q", want)
	}
	if !strings.Contains(src, "array<Light, maxLights>") {
		t.Fatal("shader's lights are not sized by maxLights")
	}
}
