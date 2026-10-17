// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image/color"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

var (
	overheadLight = oldLight(nt3dmath.Vec3{Z: 1}, .3, true)
	overhead      = &overheadLight
	gray          = color.RGBA{128, 128, 128, 255}
	shiny         = &Material{Specular: 1, Shininess: 32}
)

// A shiny sphere lit and seen from above is white where it mirrors the
// light; a dull one is not.
func TestSpecularSphere(t *testing.T) {
	for _, c := range []struct {
		name  string
		m     *Material
		white bool
	}{{"shiny", shiny, true}, {"dull", nil, false}, {"unlit", &Material{Specular: 1, Unlit: true}, false}} {
		ball, err := BuildSphere(Sphere{Radius: .6, Color: gray, Material: c.m})
		check(t, err)
		got := at(softwareRender(blendScene(t, overhead, ball)), 40, 40)
		if white := got.R >= 250 && got.G >= 250 && got.B >= 250; white != c.white {
			t.Errorf("%s sphere's top: %v", c.name, got)
		}
	}
}

// Highlights are found at each pixel: a quad of two triangles shines in its
// middle, under the camera, where no vertex is, and not toward its corners.
func TestSpecularPerPixel(t *testing.T) {
	f := blendScene(t, overhead, quad(1.2, .3, gray, &Material{Specular: 1, Shininess: 1024}, false))
	img := softwareRender(f)
	middle, corner := at(img, 40, 40), at(img, 40+18, 40+18)
	if middle.R < 250 || corner.R > 200 {
		t.Fatalf("middle %v, toward a corner %v", middle, corner)
	}
}

// Boxes take no material, so no highlight.
func TestSpecularSkipsBoxes(t *testing.T) {
	g := Geometry{Boxes: []Box{{Min: nt3dmath.Vec3{X: -.5, Y: -.5, Z: -.5}, Size: nt3dmath.Vec3{X: 1, Y: 1, Z: 1}, Color: gray}}, Material: shiny}
	if got := at(softwareRender(blendScene(t, overhead, g)), 40, 40); got.R > 200 {
		t.Fatalf("box top %v shines", got)
	}
}

// Fog fades with distance from the camera: nothing before Near, wholly at
// Far, linearly between, toward the background by default.
func TestFog(t *testing.T) {
	red := color.RGBA{220, 0, 0, 255}
	f := blendScene(t, nil, quad(.4, .3, red, nil, false))
	pane, floor := f.eyeDistance(nt3dmath.Vec3{Z: .3}), f.eyeDistance(nt3dmath.Vec3{Z: -1})
	f.Fog = Fog{Near: pane + .1, Far: floor + 1}
	img := softwareRender(f)
	if got := at(img, 40, 40); !closeRGB(got, red, 1) {
		t.Fatalf("pane nearer than Near: %v", got)
	}
	// The floor's middle, under the camera, without the pane.
	bare := blendScene(t, nil)
	bare.Fog = f.Fog
	k := f.Fog.amount(floor)
	if got, want := at(softwareRender(bare), 40, 40), mixRGB(f.Background, k, blue); !closeRGB(got, want, 2) {
		t.Fatalf("floor %.2f fogged: %v, want %v", k, got, want)
	}
	f.Fog = Fog{Near: 0, Far: 1, Color: color.RGBA{0, 200, 0, 255}}
	if got := at(softwareRender(f), 40, 40); !closeRGB(got, color.RGBA{0, 200, 0, 255}, 1) {
		t.Fatalf("beyond Far, in green fog: %v", got)
	}
}

// Points and lines fade too.
func TestFogMarks(t *testing.T) {
	g := Geometry{
		Points:    []Point{{Position: nt3dmath.Vec3{X: -.5, Z: .3}, Radius: 4, Color: color.RGBA{200, 0, 0, 255}}},
		Lines:     []Vertex{{Position: nt3dmath.Vec3{X: .2, Y: -.5, Z: .3}, Color: color.RGBA{200, 0, 0, 255}}, {Position: nt3dmath.Vec3{X: .2, Y: .5, Z: .3}, Color: color.RGBA{200, 0, 0, 255}}},
		LineWidth: 3,
	}
	f := blendScene(t, nil, g)
	clear := softwareRender(f)
	f.Fog = Fog{Near: 0, Far: .1, Color: color.RGBA{0, 0, 200, 255}}
	fogged := softwareRender(f)
	for name, w := range map[string]nt3dmath.Vec3{"point": {X: -.5, Z: .3}, "line": {X: .2, Z: .3}} {
		x, y, _, _ := f.Matrix.Project(w, f.Width, f.Height)
		if c := at(clear, int(x), int(y)); c.R < 150 {
			t.Fatalf("%s not drawn: %v", name, c)
		}
		if c := at(fogged, int(x), int(y)); c.R > 5 {
			t.Errorf("%s not fogged: %v", name, c)
		}
	}
}

func TestFogValidation(t *testing.T) {
	f := blendScene(t, nil)
	f.Fog.Far = float32(nt3dmath.Vec3{X: 1}.X) / 0
	if _, err := NewSoftwareRenderer().Render(f); err == nil {
		t.Fatal("infinite fog accepted")
	}
	if _, err := NewScene().SetGeometry("x", quad(1, 0, gray, &Material{Specular: 2}, false)); err == nil {
		t.Fatal("specular 2 accepted")
	}
}
