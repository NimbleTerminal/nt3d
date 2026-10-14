// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image"
	"image/color"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// quad is a square at height z, facing up (+Z) or, flipped, down.
func quad(s, z float32, c color.RGBA, m *Material, flipped bool) Geometry {
	n := nt3dmath.Vec3{Z: 1}
	indices := []uint32{0, 1, 2, 0, 2, 3}
	if flipped {
		n, indices = n.Scale(-1), []uint32{0, 2, 1, 0, 3, 2}
	}
	g := Geometry{Indices: indices, Material: m}
	for _, p := range [][2]float32{{-s, -s}, {s, -s}, {s, s}, {-s, s}} {
		g.Vertices = append(g.Vertices, Vertex{Position: nt3dmath.Vec3{X: p[0], Y: p[1], Z: z}, Normal: n, Color: c})
	}
	return g
}

// blendScene draws geometries over an opaque blue floor (at z = -1), seen from straight
// above, evenly lit unless light is given.
func blendScene(t *testing.T, light *Light, geometries ...Geometry) Frame {
	t.Helper()
	s := NewScene()
	_, err := s.SetGeometry("floor", quad(3, -1, blue, nil, false))
	check(t, err)
	for i, g := range geometries {
		_, err := s.SetGeometry(string(rune('a'+i)), g)
		check(t, err)
	}
	camera := DefaultCamera()
	camera.Projection, camera.Alpha = Perspective, 89
	f, err := s.Frame(camera, 80, 80)
	check(t, err)
	f.Light = Light{Direction: nt3dmath.Vec3{Z: 1}, Ambient: 1}
	if light != nil {
		f.Light = *light
	}
	return f
}

var (
	blue  = color.RGBA{0, 0, 200, 255}
	glass = &Material{Transparent: true}
)

// mixRGB blends top over under by a, in linear light, as scene frames do.
func mixRGB(top color.RGBA, a float32, under color.RGBA) color.RGBA {
	m := func(x, y uint8) uint8 { return encode8(srgbDecode[x]*a + srgbDecode[y]*(1-a)) }
	return color.RGBA{m(top.R, under.R), m(top.G, under.G), m(top.B, under.B), 255}
}

func closeRGB(a, b color.RGBA, tolerance int) bool {
	d := func(x, y uint8) bool { return int(x)-int(y) <= tolerance && int(y)-int(x) <= tolerance }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B)
}

func at(img image.Image, x, y int) color.RGBA {
	return color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
}

// A transparent pane shows the floor through it, by its alpha.
func TestSoftwareTransparent(t *testing.T) {
	red := color.RGBA{220, 0, 0, 128}
	f := blendScene(t, nil, quad(1, .3, red, glass, false))
	if got, want := at(softwareRender(f), 40, 40), mixRGB(red, 128./255, blue); !closeRGB(got, want, 2) {
		t.Fatalf("pane over floor: %v, want %v", got, want)
	}
	// The same pane, opaque, hides it.
	f = blendScene(t, nil, quad(1, .3, red, nil, false))
	if got := at(softwareRender(f), 40, 40); !closeRGB(got, color.RGBA{220, 0, 0, 255}, 1) {
		t.Fatalf("opaque pane: %v", got)
	}
}

// Panes blend farthest first, in whatever order the scene holds them.
func TestTransparentOrder(t *testing.T) {
	red, green := color.RGBA{220, 0, 0, 128}, color.RGBA{0, 220, 0, 128}
	a := float32(128. / 255)
	want := mixRGB(green, a, mixRGB(red, a, blue))
	for name, f := range map[string]Frame{
		"far first":  blendScene(t, nil, quad(1, .3, red, glass, false), quad(1, .6, green, glass, false)),
		"near first": blendScene(t, nil, quad(1, .6, green, glass, false), quad(1, .3, red, glass, false)),
	} {
		if got := at(softwareRender(f), 40, 40); !closeRGB(got, want, 3) {
			t.Fatalf("%s: %v, want %v", name, got, want)
		}
	}
}

// A transparent sphere shows its far side through its near one: two layers.
func TestTransparentSphere(t *testing.T) {
	white := color.RGBA{240, 240, 240, 128}
	ball, err := BuildSphere(Sphere{Radius: .6, Color: white, Material: glass})
	check(t, err)
	f := blendScene(t, nil, ball)
	a := float32(128. / 255)
	if got, want := at(softwareRender(f), 40, 40), mixRGB(white, a, mixRGB(white, a, blue)); !closeRGB(got, want, 3) {
		t.Fatalf("sphere center: %v, want two layers, %v", got, want)
	}
}

// Back faces are lit as seen from behind: a pane facing away from the
// camera, lit from the camera's side, is as bright as one facing it.
func TestTransparentBackFacesLit(t *testing.T) {
	light := &Light{Direction: nt3dmath.Vec3{Z: 1}, Ambient: .2}
	white := color.RGBA{240, 240, 240, 128}
	toward := at(softwareRender(blendScene(t, light, quad(1, .3, white, glass, false))), 40, 40)
	away := at(softwareRender(blendScene(t, light, quad(1, .3, white, glass, true))), 40, 40)
	if !closeRGB(toward, away, 2) {
		t.Fatalf("pane facing the camera %v, facing away %v", toward, away)
	}
}

// Transparent textures blend by the texel's alpha times the vertex's.
func TestTransparentTexture(t *testing.T) {
	half := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	half.SetNRGBA(0, 0, color.NRGBA{200, 200, 0, 128})
	tex, err := NewTexture(half)
	check(t, err)
	g := quad(1, .3, color.RGBA{255, 255, 255, 255}, &Material{Texture: tex, Transparent: true}, false)
	g.UV = make([]UV, 4)
	f := blendScene(t, nil, g)
	if got, want := at(softwareRender(f), 40, 40), mixRGB(color.RGBA{200, 200, 0, 255}, 128./255, blue); !closeRGB(got, want, 2) {
		t.Fatalf("textured pane: %v, want %v", got, want)
	}
}

func TestTransparentCutoutRejected(t *testing.T) {
	g := quad(1, 0, blue, &Material{Transparent: true, Cutout: true}, false)
	if NewScene().SetNodeGeometry(1, g) == nil {
		if _, err := NewScene().SetGeometry("x", g); err == nil {
			t.Fatal("a transparent cutout was accepted")
		}
	}
}

// Frames without Linear blend colors as encoded, as before linear light.
func TestTransparentEncoded(t *testing.T) {
	red := color.RGBA{220, 0, 0, 128}
	f := blendScene(t, nil, quad(1, .3, red, glass, false))
	f.Linear = false
	m := func(x, y uint8) uint8 { return uint8(float32(x)*128/255 + float32(y)*127/255 + .5) }
	if got, want := at(softwareRender(f), 40, 40), (color.RGBA{m(220, 0), 0, m(0, 200), 255}); !closeRGB(got, want, 1) {
		t.Fatalf("encoded blend: %v, want %v", got, want)
	}
}
