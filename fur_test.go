// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
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
