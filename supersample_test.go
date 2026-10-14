// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image"
	"image/color"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// edgeScene is an unlit black triangle on white, with a red point of radius 4.
func edgeScene(t *testing.T, supersample int) Frame {
	t.Helper()
	black := color.RGBA{0, 0, 0, 255}
	g := Geometry{
		Vertices: []Vertex{{Position: nt3dmath.Vec3{X: -.9, Y: -.7}, Color: black}, {Position: nt3dmath.Vec3{X: .5, Y: -.7}, Color: black}, {Position: nt3dmath.Vec3{X: -.9, Y: .6}, Color: black}},
		Indices:  []uint32{0, 1, 2},
		Material: &Material{Unlit: true},
		Points:   []Point{{Position: nt3dmath.Vec3{X: .6, Y: .6}, Radius: 4, Color: color.RGBA{255, 0, 0, 255}}},
	}
	s := NewScene()
	_, err := s.SetGeometry("edge", g)
	check(t, err)
	camera := DefaultCamera()
	camera.Alpha, camera.Beta = 89, -90
	f, err := s.Frame(camera, 120, 120)
	check(t, err)
	f.Background = color.RGBA{255, 255, 255, 255}
	f.Supersample = supersample
	return f
}

// shades counts pixels between black and white on the triangle's edge, and
// the point's red pixels.
func shades(img image.Image) (between, red int) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			switch {
			case c.R > 200 && c.G < 60:
				red++
			case c.R == c.G && c.R > 20 && c.R < 235:
				between++
			}
		}
	}
	return between, red
}

func TestSoftwareSupersample(t *testing.T) {
	hardEdge, hardRed := shades(softwareRender(edgeScene(t, 1)))
	softEdge, softRed := shades(softwareRender(edgeScene(t, 3)))
	// Supersampling grays the edge; the point keeps its size.
	if hardEdge > 5 || softEdge < 60 {
		t.Fatalf("edge pixels between black and white: %d without, %d with", hardEdge, softEdge)
	}
	if hardRed < 20 || softRed < hardRed*2/3 || softRed > hardRed*3/2 {
		t.Fatalf("point pixels: %d without, %d with", hardRed, softRed)
	}
	if img := softwareRender(edgeScene(t, 3)); img.Bounds() != image.Rect(0, 0, 120, 120) {
		t.Fatal(img.Bounds())
	}
	if validateFrame(edgeScene(t, MaxSupersample+1)) == nil {
		t.Fatal("accepted too large a factor")
	}
	wide := edgeScene(t, 4)
	wide.Width = 3000
	if s := supersampleFactor(wide); s != 2 {
		t.Fatalf("factor %d for a frame 3000 wide", s)
	}
}
