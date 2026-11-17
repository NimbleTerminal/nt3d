// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"image"
	"image/png"
	"nimbleterminal.dev/nt3d"
	"os"
	"testing"
)

func roomTestFrame(t *testing.T, d *demo, w, h int) nt3d.Frame {
	t.Helper()
	c := nt3d.DefaultCamera()
	roomCamera(&c)
	f, err := d.scene.Frame(c, w, h)
	if err != nil {
		t.Fatal(err)
	}
	f.Complete = true
	d.decorateRoom(&f)
	return f
}
func roomPixelDifference(a, b image.Image) int {
	count := 0
	for y := 0; y < a.Bounds().Dy(); y++ {
		for x := 0; x < a.Bounds().Dx(); x++ {
			ar, ag, ab, _ := a.At(x, y).RGBA()
			br, bg, bb, _ := b.At(x, y).RGBA()
			if ar != br || ag != bg || ab != bb {
				count++
			}
		}
	}
	return count
}
func TestRoomComparisons(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupRoom(); err != nil {
		t.Fatal(err)
	}
	r := nt3d.NewSoftwareRenderer()
	defer r.Close()
	render := func() image.Image {
		img, err := r.Render(roomTestFrame(t, d, 240, 160))
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	before := render()
	for _, key := range []string{"p", "l"} {
		handled, err := d.roomKey(key)
		if !handled || err != nil {
			t.Fatalf("toggle %s: %v", key, err)
		}
		after := render()
		if n := roomPixelDifference(before, after); n < 100 {
			t.Fatalf("%s only changed %d pixels", key, n)
		}
		_, err = d.roomKey(key)
		if err != nil {
			t.Fatal(err)
		}
	}
	d.animationTime = 2
	if err := d.stepRoom(); err != nil {
		t.Fatal(err)
	}
	if roomPixelDifference(before, render()) < 100 {
		t.Fatal("sphere did not move")
	}
	if path := os.Getenv("NT3D_ROOM_PREVIEW"); path != "" {
		img, err := r.Render(roomTestFrame(t, d, 960, 640))
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err = png.Encode(file, img); err != nil {
			t.Fatal(err)
		}
	}
}
