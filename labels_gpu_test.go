//go:build webgpu

package nt3d

import (
	"image/color"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

func TestGPULabels(t *testing.T) {
	s := NewScene()
	g, err := (Label{Text: "Label", Pixels: true, Height: 30, Background: color.RGBA{255, 0, 0, 255}, Padding: 3}).Geometry()
	check(t, err)
	shared, err := NewSharedGeometry(g)
	check(t, err)
	for i := range 200 {
		id, err := s.SetShared(string(rune(1000+i)), shared)
		check(t, err)
		check(t, s.SetTransform(id, nt3dmath.Translation(nt3dmath.Vec3{Y: float32(i%20-10) * .2, Z: float32(i/20-5) * .2})))
	}
	cam := Camera{Distance: 4}
	f, err := s.Frame(cam, 320, 240)
	check(t, err)
	r := &gpuRenderer{}
	defer r.Close()
	img, err := r.Render(f)
	check(t, err)
	if coloredPixels(img, color.RGBA{255, 0, 0, 255}) < 100 {
		t.Fatal("GPU labels missing")
	}
	uploads, textures := r.uploads, r.textureUploads
	// One label texture plus the shared neutral normal-map binding.
	if uploads != 1 || textures != 2 {
		t.Fatalf("shared label uploaded %d meshes, %d textures", uploads, textures)
	}
	cam.Beta = 20
	f, err = s.Frame(cam, 320, 240)
	check(t, err)
	_, err = r.Render(f)
	check(t, err)
	if r.uploads != uploads || r.textureUploads != textures {
		t.Fatal("camera motion reuploaded labels")
	}
	// Fully covering opaque wall must hide the same labels on GPU and software.
	_, err = s.SetGeometry("wall", Geometry{Boxes: []Box{{Min: nt3dmath.Vec3{X: 2, Y: -4, Z: -4}, Size: nt3dmath.Vec3{X: .1, Y: 8, Z: 8}, Color: color.RGBA{0, 255, 0, 255}}}})
	check(t, err)
	f, err = s.Frame(cam, 320, 240)
	check(t, err)
	img, err = r.Render(f)
	check(t, err)
	if coloredPixels(img, color.RGBA{255, 0, 0, 255}) != 0 {
		t.Fatal("GPU labels visible through opaque wall")
	}
	soft, err := NewSoftwareRenderer().Render(f)
	check(t, err)
	if coloredPixels(soft, color.RGBA{255, 0, 0, 255}) != 0 {
		t.Fatal("software labels visible through opaque wall")
	}
}
