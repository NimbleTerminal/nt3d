// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"slices"

	"nimbleterminal.dev/nt3d/math"
)

// Fur describes shell fur: copies of a mesh pushed out along its normals,
// each cut to the cross-section of the strands at its height, so that the
// layers stack into strands. Zero values take the defaults noted.
type Fur struct {
	// Length is how far the tips stand off the surface, in the mesh's units.
	Length float32
	// Shells is the number of layers, 2..64 (default 16). More layers make
	// smoother strands, especially seen from the side, at a cost in triangles.
	Shells int
	// Density is strands across the texture, 4..512 (default 64). Strands
	// follow the mesh's UVs, so they spread where its UVs stretch.
	Density int
	// Root and Tip color strands at the skin and at their ends (defaults dark
	// and light brown). Inner layers are also darkened, as fur shades itself.
	Root, Tip color.RGBA
	// Seed varies where strands grow and how long each is.
	Seed uint64
}

// height is shell k's fraction of the fur's length. Layers sit at k/n, so the
// top one is just below the longest strands' tips, where it still catches
// them; a layer at the full length would only ever be empty.
func (f Fur) height(k int) float32 { return float32(k) / float32(f.Shells) }

const (
	furTexelsPerStrand = 4
	furMaxTexture      = 2048
)

// FurCoat holds the strand textures of a Fur, made once, from which the
// shells of any mesh are built.
type FurCoat struct {
	fur      Fur
	textures []*Texture // One per shell above the skin.
}

// NewFur makes the strand textures for a Fur.
func NewFur(f Fur) (*FurCoat, error) {
	if f.Shells == 0 {
		f.Shells = 16
	}
	if f.Density == 0 {
		f.Density = 64
	}
	if f.Root == (color.RGBA{}) {
		f.Root = color.RGBA{70, 44, 26, 255}
	}
	if f.Tip == (color.RGBA{}) {
		f.Tip = color.RGBA{196, 150, 98, 255}
	}
	switch {
	case !positive(f.Length):
		return nil, fmt.Errorf("nt3d: fur length must be positive")
	case f.Shells < 2 || f.Shells > 64:
		return nil, fmt.Errorf("nt3d: fur shells must be 2..64")
	case f.Density < 4 || f.Density > 512:
		return nil, fmt.Errorf("nt3d: fur density must be 4..512")
	}
	size := min(furMaxTexture, f.Density*furTexelsPerStrand)
	cell := float64(size) / float64(f.Density)
	// Each strand has a place in its cell, a length, and a thickness.
	type strand struct{ x, y, length, radius float64 }
	rng := rand.New(rand.NewPCG(f.Seed, 0x9e3779b97f4a7c15))
	strands := make([]strand, f.Density*f.Density)
	for i := range strands {
		cx, cy := float64(i%f.Density), float64(i/f.Density)
		strands[i] = strand{
			x:      (cx + .25 + .5*rng.Float64()) * cell,
			y:      (cy + .25 + .5*rng.Float64()) * cell,
			length: .55 + .45*rng.Float64(),
			radius: (.36 + .14*rng.Float64()) * cell,
		}
	}
	coat := &FurCoat{fur: f}
	for k := 1; k < f.Shells; k++ {
		h := float64(f.height(k))
		img := image.NewNRGBA(image.Rect(0, 0, size, size))
		for y := range size {
			for x := range size {
				s := strands[min(f.Density-1, int(float64(y)/cell))*f.Density+min(f.Density-1, int(float64(x)/cell))]
				if h > s.length {
					continue
				}
				// Strands taper to their tips, but keep enough body that the
				// outer layers, which are what shows, still cover the fur.
				along := h / s.length
				r := s.radius * (1 - .6*along)
				if math.Hypot(float64(x)+.5-s.x, float64(y)+.5-s.y) > r {
					continue
				}
				// Color turns toward the tip early, as most of a strand that
				// shows is its upper part; light reaches the outer fur.
				shade, tip := .7+.3*h, math.Sqrt(along)
				mix := func(a, b uint8) uint8 { return uint8((float64(a) + (float64(b)-float64(a))*tip) * shade) }
				img.SetNRGBA(x, y, color.NRGBA{mix(f.Root.R, f.Tip.R), mix(f.Root.G, f.Tip.G), mix(f.Root.B, f.Tip.B), 255})
			}
		}
		t, err := NewTexture(img)
		if err != nil {
			return nil, err
		}
		coat.textures = append(coat.textures, t)
	}
	return coat, nil
}

// Shells returns the fur on a base mesh: first an opaque skin in the root
// color, then one cutout layer per shell, each pushed out along the base's
// normals. The base needs normals and UVs; points, lines, boxes, and arrows
// are not furred. Add each shell as a node placed as the base is; Sway then
// bends the fur by moving those nodes, without rebuilding them.
func (c *FurCoat) Shells(base Geometry) ([]Geometry, error) {
	if err := base.Validate(); err != nil {
		return nil, err
	}
	if len(base.Indices) == 0 || len(base.UV) != len(base.Vertices) {
		return nil, fmt.Errorf("nt3d: fur needs a mesh with UVs")
	}
	f := c.fur
	skin := Geometry{Vertices: slices.Clone(base.Vertices), Indices: base.Indices, UV: base.UV}
	shade := func(v uint8) uint8 { return uint8(float64(v) * .6) }
	for i := range skin.Vertices {
		skin.Vertices[i].Color = color.RGBA{shade(f.Root.R), shade(f.Root.G), shade(f.Root.B), 255}
	}
	skin.Bounds = GeometryBounds(skin)
	shells := []Geometry{skin}
	for k, texture := range c.textures {
		h := f.height(k + 1)
		g := Geometry{Vertices: slices.Clone(base.Vertices), Indices: base.Indices, UV: base.UV, Material: &Material{Texture: texture, Cutout: true}}
		for i := range g.Vertices {
			v := &g.Vertices[i]
			v.Position = v.Position.Add(v.Normal.Normalize().Scale(f.Length * h))
		}
		g.Bounds = GeometryBounds(g)
		if err := g.Validate(); err != nil {
			return nil, err
		}
		shells = append(shells, g)
	}
	return shells, nil
}

// Sway returns the local transform that bends a shell's strands as gravity or
// wind would: the shell moves by sway times the square of its height, so
// roots stay put and tips move most. A uniform sway moves every point of a
// shell alike, so it is a translation of the shell's node, and animating it
// uploads no geometry. Compose it after the base's own placement.
func (c *FurCoat) Sway(shell int, sway nt3dmath.Vec3) nt3dmath.Mat4 {
	if shell <= 0 || shell >= c.fur.Shells || !finiteVec(sway) {
		return nt3dmath.Identity()
	}
	h := c.fur.height(shell)
	return nt3dmath.Translation(sway.Scale(h * h))
}
