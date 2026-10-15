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
			if float32(g.Material.Texture.pixels[i]) >= g.Material.Threshold*255 {
				kept++
			}
		}
		if g.Material.Texture != shells[1].Material.Texture {
			t.Fatal("layers do not share one texture")
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
			if float32(layer.pixels[i+3]) < shells[1].Material.Threshold*255 {
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

// An untextured mesh's fur repeats a small tile, however dense.
func TestFurTiles(t *testing.T) {
	sphere, err := BuildSphere(Sphere{Radius: 1, Segments: 32, Rings: 16})
	check(t, err)
	coat, err := NewFur(Fur{Length: .05, Shells: 4, Spacing: .005})
	check(t, err)
	shells, err := coat.Shells(sphere)
	check(t, err)
	if d := coat.density(sphere); d < 700 || d > 760 {
		t.Fatalf("density %d", d)
	}
	layer := shells[1]
	if !layer.Material.Repeat || layer.Material.Texture.width != furTile*furTexelsPerStrand {
		t.Fatalf("not a repeating %d-strand tile: %d wide", furTile, layer.Material.Texture.width)
	}
	// A unit sphere's v spans a meridian, π; u spans a parallel, whose
	// length, averaged by area, is π²/2. At 0.005 apart, with 64 strands a
	// tile: 15 tiles around and 10 from pole to pole.
	last := layer.UV[len(layer.UV)-1]
	if last.U < 14 || last.U > 16 || last.V < 9 || last.V > 11 {
		t.Fatalf("UVs reach %v", last)
	}
	if shells[0].UV[len(shells[0].UV)-1].U != 1 {
		t.Fatal("the skin's UVs changed")
	}
}

// Each UV axis tiles for its own stretch, so strands stay round on a torus
// whose u spans, averaged by area, 2π(R + r²/2R) and v 2πr: 2.55 times as much.
func TestFurTilesEachAxis(t *testing.T) {
	torus, err := BuildTorus(Torus{MajorRadius: .7, MinorRadius: .3, Segments: 96, Sides: 32})
	check(t, err)
	su, sv := stretch(torus)
	if r := su / sv; r < 2.45 || r > 2.65 {
		t.Fatalf("stretch %v by %v", su, sv)
	}
	coat, err := NewFur(Fur{Length: .05, Shells: 4, Spacing: .005})
	check(t, err)
	shells, err := coat.Shells(torus)
	check(t, err)
	var top UV
	for _, uv := range shells[1].UV {
		top.U, top.V = max(top.U, uv.U), max(top.V, uv.V)
	}
	if r := top.U / top.V; r < 2.2 || r > 2.9 {
		t.Fatalf("tiles %v across, %v around the tube", top.U, top.V)
	}
}

// A flat-shaded mesh without UVs, as an STL file is, grows a closed coat
// with strands the same size on every side.
func TestFurWithoutUVs(t *testing.T) {
	cube, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 1, Y: 1, Z: 1}})
	check(t, err)
	cube.UV = nil // Six flat faces, corners split, no UVs.
	coat, err := NewFur(Fur{Length: .1, Shells: 4, Spacing: .01})
	check(t, err)
	shells, err := coat.Shells(cube)
	check(t, err)
	layer := shells[1]
	// Normals averaged at the corners keep the shell closed: its corners
	// coincide as the cube's do, eight places in all.
	corners := map[nt3dmath.Vec3]bool{}
	for _, v := range layer.Vertices {
		corners[v.Position] = true
	}
	if len(corners) != 8 {
		t.Fatalf("the shell has %d corner positions; it cracked", len(corners))
	}
	// A unit face spans 1 / (64 × 0.01) = 1.5625 tiles on each axis, on every side.
	if !layer.Material.Repeat {
		t.Fatal("projected fur does not repeat")
	}
	for i := 0; i+2 < len(layer.Indices); i += 3 {
		lo, hi := UV{9, 9}, UV{-9, -9}
		for _, k := range layer.Indices[i : i+3] {
			uv := layer.UV[k]
			lo, hi = UV{min(lo.U, uv.U), min(lo.V, uv.V)}, UV{max(hi.U, uv.U), max(hi.V, uv.V)}
		}
		if du, dv := hi.U-lo.U, hi.V-lo.V; abs(du-1.5625) > 1e-4 || abs(dv-1.5625) > 1e-4 {
			t.Fatalf("triangle %d spans %v by %v tiles", i/3, du, dv)
		}
	}
}

// Fins stand on every edge once, both ways round, as long as the fur, and
// are drawn only edge-on.
func TestFurFins(t *testing.T) {
	cube, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 1, Y: 1, Z: 1}})
	check(t, err)
	coat, err := NewFur(Fur{Length: .2, Shells: 4, Spacing: .01, Root: color.RGBA{10, 20, 30, 255}, Tip: color.RGBA{200, 150, 100, 255}})
	check(t, err)
	fins, err := coat.Fins(cube)
	check(t, err)
	// A cube's 12 edges and 6 face diagonals: 18 strips of 4 triangles.
	if len(fins.Indices)/3 != 18*4 {
		t.Fatalf("%d fin triangles", len(fins.Indices)/3)
	}
	m := fins.Material
	if !m.Cutout || !m.EdgeOn || !m.Repeat || !m.Tint {
		t.Fatalf("material %+v", m)
	}
	// Tops stand the fur's length off the corner, along its averaged normal.
	for i := 0; i < len(fins.Vertices); i += 4 {
		root, top := fins.Vertices[i], fins.Vertices[i+3]
		if d := top.Position.Sub(root.Position); abs(d.Dot(d)-.04) > 1e-5 || d.Normalize().Dot(root.Normal) < .9999 {
			t.Fatalf("fin %d rises %v", i/4, d)
		}
		if root.Color != coat.fur.Root || top.Color != coat.fur.Tip {
			t.Fatal("fin colors")
		}
	}
	// Each strip faces both ways.
	if a, b := faceNormal(fins, 0), faceNormal(fins, 6); a.Dot(b) >= 0 {
		t.Fatal("strips face one way")
	}
}

// Shells share no slices with each other or the base: editing one returned
// shell leaves the rest as they were.
func TestFurShellsShareNothing(t *testing.T) {
	coat, err := NewFur(Fur{Length: .2, Shells: 4, Density: 16})
	check(t, err)
	sphere, err := BuildSphere(Sphere{Radius: 1, Segments: 16, Rings: 8})
	check(t, err)
	if len(sphere.UV) == 0 {
		t.Fatal("fixture has no UVs")
	}
	shells, err := coat.Shells(sphere)
	check(t, err)
	before := sphere.Indices[0]
	for k, g := range shells {
		g.Indices[0] = 999
		if len(g.UV) > 0 {
			g.UV[0] = UV{U: 9, V: 9}
		}
		if sphere.Indices[0] != before || sphere.UV[0] == (UV{U: 9, V: 9}) {
			t.Fatalf("editing shell %d changed the base", k)
		}
		for j, other := range shells[k+1:] {
			if other.Indices[0] == 999 || len(other.UV) > 0 && other.UV[0] == (UV{U: 9, V: 9}) {
				t.Fatalf("editing shell %d changed shell %d", k, k+1+j)
			}
		}
	}
}
