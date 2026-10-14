// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image"
	"image/color"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

func TestFurShells(t *testing.T) {
	coat, err := NewFur(Fur{Length: .2, Shells: 8, Density: 16})
	check(t, err)
	sphere, err := BuildSphere(Sphere{Radius: 1, Segments: 16, Rings: 8})
	check(t, err)
	shells, err := coat.Shells(sphere)
	check(t, err)
	if len(shells) != 8 {
		t.Fatalf("%d shells", len(shells))
	}
	if shells[0].Material != nil {
		t.Fatal("the skin is not opaque")
	}
	// Strands thin and end with height: each layer keeps fewer texels.
	previous := 1 << 30
	for k, g := range shells[1:] {
		if g.Material == nil || !g.Material.Cutout || g.Material.Texture == nil {
			t.Fatalf("shell %d is not a cutout", k+1)
		}
		kept := 0
		for i := 3; i < len(g.Material.Texture.pixels); i += 4 {
			if g.Material.Texture.pixels[i] >= 128 {
				kept++
			}
		}
		if kept == 0 || kept > previous {
			t.Fatalf("shell %d keeps %d texels, below it %d", k+1, kept, previous)
		}
		previous = kept
		// Each layer stands off the surface by its share of the length.
		want := 1 + .2*float32(k+1)/8
		if r := g.Vertices[40].Position.Sub(nt3dmath.Vec3{}).Dot(g.Vertices[40].Normal); abs(r-want) > 1e-4 {
			t.Fatalf("shell %d at radius %v, want %v", k+1, r, want)
		}
	}
	// Sway moves tips most and roots not at all.
	sway := nt3dmath.Vec3{X: .3}
	if coat.Sway(0, sway) != nt3dmath.Identity() || coat.Sway(8, sway) != nt3dmath.Identity() {
		t.Fatal("sway of the skin, or of no shell")
	}
	if p := coat.Sway(4, sway).Point(nt3dmath.Vec3{}); abs(p.X-.3*.25) > 1e-6 {
		t.Fatal("sway grows with the square of height", p)
	}
	// Validation.
	for _, bad := range []Fur{{}, {Length: 1, Shells: 1}, {Length: 1, Density: 2}, {Length: 1, Shells: 65}} {
		if _, err := NewFur(bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	sphere.UV = nil
	if _, err = coat.Shells(sphere); err == nil {
		t.Fatal("furred a mesh without UVs")
	}
}

// Spacing sets strands from surface per unit of UV; a textured mesh colors
// its fur and wears its texture as skin; layers are made once per need.
func TestFurSpacingAndTexture(t *testing.T) {
	half := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			c := color.NRGBA{220, 30, 20, 255}
			if x >= 4 {
				c = color.NRGBA{20, 40, 230, 255}
			}
			half.SetNRGBA(x, y, c)
		}
	}
	texture, err := NewTexture(half)
	check(t, err)
	// A unit sphere: 4π of surface over one square of UV.
	sphere, err := BuildSphere(Sphere{Radius: 1, Segments: 32, Rings: 16, Material: &Material{Texture: texture}})
	check(t, err)
	coat, err := NewFur(Fur{Length: .1, Shells: 4, Spacing: .05})
	check(t, err)
	shells, err := coat.Shells(sphere)
	check(t, err)
	if d := coat.density(sphere); d < 66 || d > 76 { // The poles leave a little UV uncovered.
		t.Fatalf("density %d for 3.5 units of surface a side, 0.05 apart", d)
	}
	if shells[0].Material == nil || shells[0].Material.Texture != texture || shells[0].Material.Cutout {
		t.Fatal("the skin does not wear the texture")
	}
	// Strands on the left of the texture are red, on the right blue.
	layer := shells[1].Material.Texture
	var left, right [3]int
	for y := range layer.height {
		for x := range layer.width {
			i := (y*layer.width + x) * 4
			if layer.pixels[i+3] < 128 {
				continue
			}
			side := &left
			if x >= layer.width/2 {
				side = &right
			}
			side[0] += int(layer.pixels[i])
			side[2] += int(layer.pixels[i+2])
		}
	}
	if left[0] <= left[2] || right[2] <= right[0] {
		t.Fatalf("fur colors: left %v, right %v", left, right)
	}
	// A second mesh at the same density shares the layers.
	again, err := coat.Shells(sphere)
	check(t, err)
	if again[1].Material.Texture != layer || len(coat.layers) != 1 {
		t.Fatal("layers made again")
	}
	if _, err := NewFur(Fur{Length: .1, Spacing: -1}); err == nil {
		t.Fatal("accepted negative spacing")
	}
}
