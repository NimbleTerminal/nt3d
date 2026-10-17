// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image"
	"image/color"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// materialScene is a quad facing a camera straight above it, textured gray,
// whose vertex normals and colors are given.
func materialScene(t *testing.T, m Material, normal nt3dmath.Vec3, c color.RGBA) Frame {
	t.Helper()
	gray := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for i := range gray.Pix {
		gray.Pix[i] = 128
	}
	for i := 3; i < len(gray.Pix); i += 4 {
		gray.Pix[i] = 255
	}
	tex, err := NewTexture(gray)
	check(t, err)
	m.Texture = tex
	g, err := BuildHeightfield(Heightfield{NX: 2, NY: 2, MinX: -.8, MaxX: .8, MinY: -.8, MaxY: .8, Heights: make([]float32, 4), Material: &m, Color: c})
	check(t, err)
	for i := range g.Vertices {
		g.Vertices[i].Normal = normal
	}
	s := NewScene()
	_, err = s.SetGeometry("quad", g)
	check(t, err)
	camera := DefaultCamera()
	camera.Projection, camera.Alpha = Perspective, 89
	f, err := s.Frame(camera, 120, 120)
	check(t, err)
	f.Lighting = even // Even light: color is the texture's.
	return f
}

func center(img image.Image) color.RGBA {
	return color.RGBAModel.Convert(img.At(60, 60)).(color.RGBA)
}

func drawn(img image.Image, background color.RGBA) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if color.RGBAModel.Convert(img.At(x, y)).(color.RGBA) != background {
				n++
			}
		}
	}
	return n
}

func TestSoftwareTint(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	if c := center(softwareRender(materialScene(t, Material{Tint: true}, nt3dmath.Vec3{Z: 1}, red))); c.R < 120 || c.R > 136 || c.G > 5 {
		t.Fatalf("tinted gray %v, want half red", c)
	}
	if c := center(softwareRender(materialScene(t, Material{}, nt3dmath.Vec3{Z: 1}, red))); c.R != c.G || c.R < 120 || c.R > 136 {
		t.Fatalf("untinted %v, want the texture's gray", c)
	}
}

func TestSoftwareEdgeOn(t *testing.T) {
	m := Material{Cutout: true, EdgeOn: true}
	white := color.RGBA{255, 255, 255, 255}
	// Normals toward the camera: face-on, cut away entirely.
	f := materialScene(t, m, nt3dmath.Vec3{Z: 1}, white)
	if n := drawn(softwareRender(f), f.Background); n != 0 {
		t.Fatalf("%d pixels face-on", n)
	}
	// Normals across the view: edge-on, shown.
	f = materialScene(t, m, nt3dmath.Vec3{X: 1}, white)
	if n := drawn(softwareRender(f), f.Background); n < 3000 {
		t.Fatalf("%d pixels edge-on", n)
	}
	// Wireframe and picking leave edge-on geometry out.
	s := NewScene()
	g := f.Geometry[0]
	_, err := s.SetGeometry("fin", g)
	check(t, err)
	camera := DefaultCamera()
	camera.Projection, camera.Alpha = Perspective, 89
	frame, err := s.Frame(camera, 120, 120)
	check(t, err)
	if s.Pick(camera, frame, 60, 60, 0) != nil {
		t.Fatal("picked edge-on geometry")
	}
	if d := newWireDepth(frame, 30, 15); d.faces[0] != 0 {
		t.Fatal("edge-on geometry in the wireframe depth buffer")
	}
}
