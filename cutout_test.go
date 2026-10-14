// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image"
	"image/color"
	"testing"
)

// cutoutScene is a quad textured with an alpha checkerboard, held over a
// solid blue one, seen from above.
func cutoutScene(t *testing.T, cutout bool) (Frame, color.RGBA, color.RGBA) {
	t.Helper()
	front, back := color.RGBA{230, 40, 30, 255}, color.RGBA{30, 60, 220, 255}
	checker := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			c := color.NRGBA{front.R, front.G, front.B, 255}
			if (x+y)%2 == 1 {
				c.A = 0
			}
			checker.SetNRGBA(x, y, c)
		}
	}
	texture, err := NewTexture(checker)
	check(t, err)
	// The back quad is a little smaller, so the front one covers it from a
	// camera tilted just off vertical.
	quad := func(z, half float32, m *Material, c color.RGBA) Geometry {
		g, err := BuildHeightfield(Heightfield{NX: 2, NY: 2, MinX: -half, MaxX: half, MinY: -half, MaxY: half, Heights: []float32{z, z, z, z}, Material: m, Color: c})
		check(t, err)
		return g
	}
	s := NewScene()
	_, err = s.SetGeometry("back", quad(0, .75, &Material{Unlit: true}, back))
	check(t, err)
	_, err = s.SetGeometry("front", quad(.4, .8, &Material{Texture: texture, Unlit: true, Cutout: cutout}, color.RGBA{}))
	check(t, err)
	camera := DefaultCamera()
	camera.Alpha = 89
	f, err := s.Frame(camera, 160, 160)
	check(t, err)
	return f, front, back
}

func count(img image.Image, near color.RGBA) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			d := func(p, q uint8) int { return max(int(p)-int(q), int(q)-int(p)) }
			if d(c.R, near.R)+d(c.G, near.G)+d(c.B, near.B) < 60 {
				n++
			}
		}
	}
	return n
}

func TestSoftwareCutout(t *testing.T) {
	f, front, back := cutoutScene(t, true)
	img := softwareRender(f)
	shown, through := count(img, front), count(img, back)
	// Half the checkerboard is cut away, showing the quad behind.
	if shown < 2000 || through < 2000 || shown > 2*through || through > 2*shown {
		t.Fatalf("front %d, back %d pixels", shown, through)
	}
	// Without Cutout, alpha is ignored and the front quad hides the back.
	f, _, _ = cutoutScene(t, false)
	if n := count(softwareRender(f), back); n != 0 {
		t.Fatalf("%d back pixels through an opaque material", n)
	}
}

// rampScene is a quad whose alpha rises left to right, cut at threshold.
func rampScene(t *testing.T, threshold float32) (Frame, color.RGBA) {
	t.Helper()
	front := color.RGBA{230, 40, 30, 255}
	ramp := image.NewNRGBA(image.Rect(0, 0, 64, 4))
	for y := range 4 {
		for x := range 64 {
			ramp.SetNRGBA(x, y, color.NRGBA{front.R, front.G, front.B, uint8(x * 255 / 63)})
		}
	}
	texture, err := NewTexture(ramp)
	check(t, err)
	g, err := BuildHeightfield(Heightfield{NX: 2, NY: 2, MinX: -.8, MaxX: .8, MinY: -.8, MaxY: .8, Heights: make([]float32, 4), Material: &Material{Texture: texture, Unlit: true, Cutout: true, Threshold: threshold}})
	check(t, err)
	s := NewScene()
	_, err = s.SetGeometry("ramp", g)
	check(t, err)
	camera := DefaultCamera()
	camera.Alpha = 89
	f, err := s.Frame(camera, 160, 160)
	check(t, err)
	return f, front
}

func TestSoftwareCutoutThreshold(t *testing.T) {
	var shown [2]int
	for i, threshold := range []float32{.25, .75} {
		f, front := rampScene(t, threshold)
		shown[i] = count(softwareRender(f), front)
	}
	// Three times as much of the ramp clears a quarter as clears three quarters.
	if ratio := float64(shown[0]) / float64(shown[1]); ratio < 2.6 || ratio > 3.4 {
		t.Fatalf("kept %v pixels at thresholds 0.25 and 0.75", shown)
	}
	g, err := BuildHeightfield(Heightfield{NX: 2, NY: 2, MinX: 0, MaxX: 1, MinY: 0, MaxY: 1, Heights: make([]float32, 4), Material: &Material{Cutout: true, Threshold: 1.5}})
	if err == nil {
		t.Fatal("accepted a threshold over 1", g.Material)
	}
}
