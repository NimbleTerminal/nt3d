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
	// Spacing is about how far apart strands grow, in the mesh's units. It
	// sets how many strands cross the texture from how much surface the
	// mesh's UVs spread over it, up to 512. Leave it zero to set Density.
	Spacing float32
	// Density is strands across the texture, 4..512 (default 64), when
	// Spacing is zero. Strands follow the mesh's UVs, so they spread where its
	// UVs stretch.
	Density int
	// Root and Tip color strands at the skin and at their ends (defaults dark
	// and light brown). A textured mesh's fur takes the texture's color
	// instead, darker at the root. Inner layers are also darkened, as fur
	// shades itself.
	Root, Tip color.RGBA
	// Seed varies where strands grow and how long each is.
	Seed uint64
}

const (
	furTexelsPerStrand = 4
	furMaxTexture      = 2048
	furMaxDensity      = 512
)

// height is shell k's fraction of the fur's length. Layers sit at k/n, so the
// top one is just below the longest strands' tips, where it still catches
// them; a layer at the full length would only ever be empty.
func (f Fur) height(k int) float32 { return float32(k) / float32(f.Shells) }

// FurCoat grows a Fur on meshes. It makes each strand density's texture
// once, for every mesh that needs it.
type FurCoat struct {
	fur    Fur
	layers map[furLayers]*Texture
}

// furLayers names a strand texture: its strands, and the texture that colors
// them, if any.
type furLayers struct {
	density int
	source  *Texture
}

// NewFur checks a Fur and returns its coat. Strand textures are made by
// Shells, when a mesh shows what they need.
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
	case f.Spacing != 0 && !positive(f.Spacing):
		return nil, fmt.Errorf("nt3d: fur spacing must be positive")
	case f.Spacing == 0 && (f.Density < 4 || f.Density > furMaxDensity):
		return nil, fmt.Errorf("nt3d: fur density must be 4..%d", furMaxDensity)
	}
	return &FurCoat{fur: f, layers: map[furLayers]*Texture{}}, nil
}

// density is strands across the texture for a mesh: Density, or as many as
// put strands Spacing apart, given how much surface a unit of UV covers.
func (c *FurCoat) density(base Geometry) int {
	if c.fur.Spacing == 0 {
		return c.fur.Density
	}
	var surface, uv float64
	for i := 0; i+2 < len(base.Indices); i += 3 {
		a, b, d := base.Indices[i], base.Indices[i+1], base.Indices[i+2]
		n := base.Vertices[b].Position.Sub(base.Vertices[a].Position).Cross(base.Vertices[d].Position.Sub(base.Vertices[a].Position))
		surface += math.Sqrt(float64(n.Dot(n))) / 2
		p, q, r := base.UV[a], base.UV[b], base.UV[d]
		uv += math.Abs(float64((q.U-p.U)*(r.V-p.V)-(q.V-p.V)*(r.U-p.U))) / 2
	}
	if uv == 0 {
		return 4
	}
	return int(min(furMaxDensity, max(4, math.Round(math.Sqrt(surface/uv)/float64(c.fur.Spacing)))))
}

// strands makes, or returns, the texture all the layers share. Its alpha is
// the height, as a fraction of the fur's length, to which a strand reaches
// at each texel: a tapered strand is tallest at its center and short at its
// edge. A layer at height h is a cutout at threshold h, so it keeps just the
// strands' cross-section there, as a texture per layer would, at a fraction
// of the memory. A texel's color is that of its strand at its top, so short
// strand edges, which lower layers show most of, are darker.
func (c *FurCoat) strands(key furLayers) (*Texture, error) {
	if texture, ok := c.layers[key]; ok {
		return texture, nil
	}
	f, density := c.fur, key.density
	size := min(furMaxTexture, density*furTexelsPerStrand)
	cell := float64(size) / float64(density)
	// Each strand has a place in its cell, a length, a thickness, and a color.
	type strand struct {
		x, y, length, radius float64
		root, tip            [3]float64
	}
	rng := rand.New(rand.NewPCG(f.Seed, 0x9e3779b97f4a7c15))
	strands := make([]strand, density*density)
	channels := func(c color.RGBA) [3]float64 { return [3]float64{float64(c.R), float64(c.G), float64(c.B)} }
	for i := range strands {
		cx, cy := float64(i%density), float64(i/density)
		s := strand{
			x:      (cx + .25 + .5*rng.Float64()) * cell,
			y:      (cy + .25 + .5*rng.Float64()) * cell,
			length: .55 + .45*rng.Float64(),
			radius: (.36 + .14*rng.Float64()) * cell,
			root:   channels(f.Root),
			tip:    channels(f.Tip),
		}
		if key.source != nil {
			// The texture's color where the strand grows, darker at its root.
			t := key.source.sample(UV{U: float32(s.x / float64(size)), V: float32(s.y / float64(size))})
			s.tip = [3]float64{float64(t.R), float64(t.G), float64(t.B)}
			s.root = [3]float64{s.tip[0] * .55, s.tip[1] * .55, s.tip[2] * .55}
		}
		strands[i] = s
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			s := &strands[min(density-1, int(float64(y)/cell))*density+min(density-1, int(float64(x)/cell))]
			// A strand's radius shrinks to 40% at its tip, so a texel at
			// distance d stays inside it up to this height.
			d := math.Hypot(float64(x)+.5-s.x, float64(y)+.5-s.y)
			height := max(0, min(s.length, s.length*(1-d/s.radius)/.6))
			// Color turns toward the tip early, as most of a strand that
			// shows is its upper part; light reaches the outer fur. Texels
			// outside strands keep their strand's color too, so filtering at
			// strand edges does not darken them.
			along := height / s.length
			shade, tip := .7+.3*height, math.Sqrt(along)
			mix := func(c int) uint8 { return uint8((s.root[c] + (s.tip[c]-s.root[c])*tip) * shade) }
			img.SetNRGBA(x, y, color.NRGBA{mix(0), mix(1), mix(2), uint8(math.Round(height * 255))})
		}
	}
	texture, err := NewTexture(img)
	if err != nil {
		return nil, err
	}
	c.layers[key] = texture
	return texture, nil
}

// Shells returns the fur on a base mesh: first an opaque skin, then one
// cutout layer per shell, each pushed out along the base's normals. The base
// needs normals and UVs; points, lines, boxes, and arrows are not furred. A
// textured base colors its fur, and its skin wears the texture; otherwise the
// skin is the root color, darkened. Add each shell as a node placed as the
// base is; Sway then bends the fur by moving those nodes, without rebuilding
// them.
func (c *FurCoat) Shells(base Geometry) ([]Geometry, error) {
	if err := base.Validate(); err != nil {
		return nil, err
	}
	if len(base.Indices) == 0 || len(base.UV) != len(base.Vertices) {
		return nil, fmt.Errorf("nt3d: fur needs a mesh with UVs")
	}
	key := furLayers{density: c.density(base)}
	if textured(base) {
		key.source = base.Material.Texture
	}
	strands, err := c.strands(key)
	if err != nil {
		return nil, err
	}
	f := c.fur
	skin := Geometry{Vertices: slices.Clone(base.Vertices), Indices: base.Indices, UV: base.UV}
	if key.source != nil {
		skin.Material = &Material{Texture: key.source}
	} else {
		shade := func(v uint8) uint8 { return uint8(float64(v) * .6) }
		for i := range skin.Vertices {
			skin.Vertices[i].Color = color.RGBA{shade(f.Root.R), shade(f.Root.G), shade(f.Root.B), 255}
		}
	}
	skin.Bounds = GeometryBounds(skin)
	shells := []Geometry{skin}
	for k := 1; k < f.Shells; k++ {
		h := f.height(k)
		g := Geometry{Vertices: slices.Clone(base.Vertices), Indices: base.Indices, UV: base.UV, Material: &Material{Texture: strands, Cutout: true, Threshold: h}}
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
