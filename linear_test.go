// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image"
	"image/color"
	"math"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

func TestSRGBRoundTrip(t *testing.T) {
	for i := range 256 {
		if got := encode8(srgbDecode[i]); int(got) != i {
			t.Fatalf("sRGB %d decodes and encodes to %d", i, got)
		}
	}
}

// litPane is a gray pane facing the camera, lit from straight on or at an
// angle whose cosine is cos; with linear light or without.
func litPane(t *testing.T, cos float64, linear bool) color.RGBA {
	t.Helper()
	gray := color.RGBA{200, 200, 200, 255}
	l := oldLight(nt3dmath.Vec3{X: float32(math.Sqrt(1 - cos*cos)), Z: float32(cos)}, .3, linear)
	f := blendScene(t, &l, quad(1, .3, gray, nil, false))
	f.Linear = linear
	return at(softwareRender(f), 40, 40)
}

// Linear light adds as light does: a surface at 60° to the light gets half
// its light in linear terms, brighter on screen than half the encoded
// value; full light, and a surface turned away, look as they did.
func TestLinearLighting(t *testing.T) {
	for _, c := range []struct {
		name string
		cos  float64
	}{{"facing the light", 1}, {"turned away", 0}} {
		if l, e := litPane(t, c.cos, true), litPane(t, c.cos, false); !closeRGB(l, e, 2) {
			t.Errorf("%s: linear %v, encoded %v", c.name, l, e)
		}
	}
	ambient := decodeSRGB(.3)
	want := uint8(encodeSRGB(decodeSRGB(200./255)*(ambient+(1-ambient)*.5))*255 + .5)
	if got := litPane(t, .5, true); !closeRGB(got, color.RGBA{want, want, want, 255}, 1) {
		t.Fatalf("at 60°: %v, want %d", got, want)
	}
	if l, e := litPane(t, .5, true), litPane(t, .5, false); l.R <= e.R+10 {
		t.Fatalf("at 60°, linear %v is not brighter than encoded %v", l, e)
	}
}

// Supersampling averages edges in linear light: a black and white edge
// pixel half covered is encoded half light, not half the encoded value.
func TestShrinkLinear(t *testing.T) {
	big := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for i := range big.Pix {
		big.Pix[i] = 255
	}
	for _, i := range []int{0, 4} { // The top row black.
		big.Pix[i], big.Pix[i+1], big.Pix[i+2] = 0, 0, 0
	}
	if got, want := shrink(big, 2, true).Pix[0], encode8(.5); got != want {
		t.Fatalf("linear average %d, want %d", got, want)
	}
	if got := shrink(big, 2, false).Pix[0]; got != 128 {
		t.Fatalf("encoded average %d, want 128", got)
	}
}
