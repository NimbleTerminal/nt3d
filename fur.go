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
	// sets how many strands cross the UV square from how much surface the
	// mesh's UVs spread over it. Leave it zero to set Density.
	Spacing float32
	// Density is strands across the UV square, 4..4096 (default 64), when
	// Spacing is zero. Strands follow the mesh's UVs, so they spread where its
	// UVs stretch. An untextured mesh's fur repeats a tile of 64 strands as
	// often as that needs, so density costs no memory; a textured mesh's fur,
	// colored strand by strand, takes one texture of up to 512 strands.
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
	furMaxTextured     = 512  // Strands across a textured mesh's one texture.
	furMaxDensity      = 4096 // Strands across the UV square, tiled.
	furTile            = 64   // Strands across a repeating tile.
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
	fins   *Texture
}

// furLayers names a strand texture: its strands, and the texture that colors
// them, if any, and whether that one repeats.
type furLayers struct {
	density int
	source  *Texture
	repeat  bool
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
	span := uvSpan(base)
	if span == 0 {
		return 4
	}
	return int(min(furMaxDensity, max(4, math.Round(span/float64(c.fur.Spacing)))))
}

// uvSpan is the side of the surface a mesh's UV square covers: the square
// root of its area over the area its UVs cover, or zero if they cover none.
func uvSpan(base Geometry) float64 {
	var surface, uv float64
	for i := 0; i+2 < len(base.Indices); i += 3 {
		a, b, d := base.Indices[i], base.Indices[i+1], base.Indices[i+2]
		n := base.Vertices[b].Position.Sub(base.Vertices[a].Position).Cross(base.Vertices[d].Position.Sub(base.Vertices[a].Position))
		surface += math.Sqrt(float64(n.Dot(n))) / 2
		p, q, r := base.UV[a], base.UV[b], base.UV[d]
		uv += math.Abs(float64((q.U-p.U)*(r.V-p.V)-(q.V-p.V)*(r.U-p.U))) / 2
	}
	if uv == 0 {
		return 0
	}
	return math.Sqrt(surface / uv)
}

// cracks reports whether vertices at one position have different normals,
// so that shells pushed along them would part there.
func cracks(g Geometry) bool {
	normals := make(map[nt3dmath.Vec3]nt3dmath.Vec3, len(g.Vertices))
	for _, v := range g.Vertices {
		n := v.Normal.Normalize()
		if seen, ok := normals[v.Position]; ok {
			if d := seen.Sub(n); d.Dot(d) > 1e-8 {
				return true
			}
			continue
		}
		normals[v.Position] = n
	}
	return false
}

// project gives a mesh without UVs texture coordinates for fur, in tiles of
// strands: each triangle is projected along the axis its face points most,
// in world units, so strands are the same size on every side. Corners are
// split per face, as faces on different axes map differently; positions and
// normals are kept, so the shells stay closed.
func (c *FurCoat) project(base Geometry) Geometry {
	spacing := float64(c.fur.Spacing)
	if spacing == 0 {
		b := GeometryBounds(base)
		size := b.Max.Sub(b.Min)
		spacing = float64(max(size.X, size.Y, size.Z)) / float64(c.fur.Density)
	}
	tile := float32(spacing * furTile)
	out := Geometry{Vertices: make([]Vertex, 0, len(base.Indices)), Indices: make([]uint32, 0, len(base.Indices)), UV: make([]UV, 0, len(base.Indices))}
	for i := 0; i+2 < len(base.Indices); i += 3 {
		n := faceNormal(base, i)
		ax, ay, az := abs(n.X), abs(n.Y), abs(n.Z)
		for _, k := range base.Indices[i : i+3] {
			v := base.Vertices[k]
			p := v.Position
			var uv UV
			switch {
			case ax >= ay && ax >= az:
				uv = UV{p.Y / tile, p.Z / tile}
			case ay >= az:
				uv = UV{p.X / tile, p.Z / tile}
			default:
				uv = UV{p.X / tile, p.Y / tile}
			}
			out.Indices = append(out.Indices, uint32(len(out.Vertices)))
			out.Vertices = append(out.Vertices, v)
			out.UV = append(out.UV, uv)
		}
	}
	out.Bounds = GeometryBounds(out)
	return out
}

// stretch is how much surface one unit of each UV axis spans, averaged over
// the mesh by area: from each triangle's derivatives of position along u and v.
func stretch(base Geometry) (su, sv float64) {
	var area float64
	for i := 0; i+2 < len(base.Indices); i += 3 {
		a, b, d := base.Indices[i], base.Indices[i+1], base.Indices[i+2]
		e1, e2 := base.Vertices[b].Position.Sub(base.Vertices[a].Position), base.Vertices[d].Position.Sub(base.Vertices[a].Position)
		u1, v1 := base.UV[b].U-base.UV[a].U, base.UV[b].V-base.UV[a].V
		u2, v2 := base.UV[d].U-base.UV[a].U, base.UV[d].V-base.UV[a].V
		det := u1*v2 - u2*v1
		if det == 0 {
			continue
		}
		dpdu, dpdv := e1.Scale(v2/det).Sub(e2.Scale(v1/det)), e2.Scale(u1/det).Sub(e1.Scale(u2/det))
		n := e1.Cross(e2)
		w := math.Sqrt(float64(n.Dot(n))) / 2
		su += w * math.Sqrt(float64(dpdu.Dot(dpdu)))
		sv += w * math.Sqrt(float64(dpdv.Dot(dpdv)))
		area += w
	}
	if area == 0 {
		return 1, 1
	}
	return su / area, sv / area
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
	strands, size, cell := c.furStrands(key)
	density := key.density
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			// A strand reaches past its cell into its neighbors', so a texel
			// shows the tallest of the strands of its cell and those around
			// it. The texture repeats, so neighbors wrap around its edges.
			px, py := float64(x)+.5, float64(y)+.5
			cx, cy := min(density-1, int(px/cell)), min(density-1, int(py/cell))
			s, height := &strands[cy*density+cx], 0.0
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nx, ny := cx+dx, cy+dy
					wx, wy := (nx+density)%density, (ny+density)%density
					n := &strands[wy*density+wx]
					// A strand's radius shrinks to 40% at its tip, so a
					// texel at distance d stays inside it up to this height.
					d := math.Hypot(px-n.x-float64(nx-wx)*cell, py-n.y-float64(ny-wy)*cell)
					if h := max(0, min(n.length, n.length*(1-d/n.radius)/.6)); h > height {
						s, height = n, h
					}
				}
			}
			// Color turns toward the tip early, as most of a strand that
			// shows is its upper part; light reaches the outer fur. Texels
			// outside strands keep their own cell's strand's color, so
			// filtering at strand edges does not darken them.
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

// furStrand is one strand of a strand texture: where it grows, in texels,
// its length as a fraction of the fur's, its radius at the root, in texels,
// and its colors at the root and tip.
type furStrand struct {
	x, y, length, radius float64
	root, tip            [3]float64
}

// furStrands places a strand texture's strands, one in each cell of a
// density × density grid on a size × size texture, row by row. Each grows
// somewhere in the middle half of its cell, and may reach past it.
func (c *FurCoat) furStrands(key furLayers) (strands []furStrand, size int, cell float64) {
	f, density := c.fur, key.density
	size = min(furMaxTexture, density*furTexelsPerStrand)
	cell = float64(size) / float64(density)
	rng := rand.New(rand.NewPCG(f.Seed, 0x9e3779b97f4a7c15))
	strands = make([]furStrand, density*density)
	channels := func(c color.RGBA) [3]float64 { return [3]float64{float64(c.R), float64(c.G), float64(c.B)} }
	for i := range strands {
		cx, cy := float64(i%density), float64(i/density)
		s := furStrand{
			x:      (cx + .25 + .5*rng.Float64()) * cell,
			y:      (cy + .25 + .5*rng.Float64()) * cell,
			length: .55 + .45*rng.Float64(),
			radius: (.36 + .14*rng.Float64()) * cell,
			root:   channels(f.Root),
			tip:    channels(f.Tip),
		}
		if key.source != nil {
			// The texture's color where the strand grows, darker at its root.
			t := key.source.sampleWrap(UV{U: float32(s.x / float64(size)), V: float32(s.y / float64(size))}, key.repeat)
			s.tip = [3]float64{float64(t.R), float64(t.G), float64(t.B)}
			s.root = [3]float64{s.tip[0] * .55, s.tip[1] * .55, s.tip[2] * .55}
		}
		strands[i] = s
	}
	return strands, size, cell
}

// Shells returns the fur on a base mesh: first an opaque skin, then one
// cutout layer per shell, each pushed out along the base's normals, averaged
// where faces meet. A mesh without UVs, as most STL, 3MF, and PLY files are,
// is given them by projecting each face along its main axis; points, lines,
// boxes, and arrows are not furred. A
// textured base colors its fur, and its skin wears the texture; otherwise the
// skin is the root color, darkened. Add each shell as a node placed as the
// base is; Sway then bends the fur by moving those nodes, without rebuilding
// them.
func (c *FurCoat) Shells(base Geometry) ([]Geometry, error) {
	if err := base.Validate(); err != nil {
		return nil, err
	}
	if len(base.Indices) == 0 {
		return nil, fmt.Errorf("nt3d: fur needs a mesh")
	}
	// Shells go out along the normals; where faces meet with different ones,
	// as at hard edges and on flat-shaded meshes like STL files, they would
	// part, so there the normals are averaged and the coat stays closed.
	if cracks(base) {
		smooth, err := SmoothNormals(base, 180)
		if err != nil {
			return nil, err
		}
		base = smooth
	}
	projected := len(base.UV) != len(base.Vertices)
	if projected {
		base = c.project(base)
	}
	// A textured mesh colors its strands from its texture, so they need one
	// texture of their own; others repeat a tile of strands, the shells'
	// UVs scaled so that it repeats as often as the density needs.
	// With Spacing, each axis tiles as often as its own stretch needs, so
	// strands stay round where UVs span more surface one way than the other.
	density := c.density(base)
	tilesU, tilesV := 1, 1
	key := furLayers{density: density}
	switch {
	case projected:
		key.density = furTile // Projected UVs count tiles already.
	case textured(base):
		key.source, key.repeat = base.Material.Texture, base.Material.Repeat
		key.density = min(density, furMaxTextured)
	case c.fur.Spacing != 0:
		su, sv := stretch(base)
		count := func(s float64) int {
			return int(max(1, math.Round(min(furMaxDensity, s/float64(c.fur.Spacing))/furTile)))
		}
		tilesU, tilesV = count(su), count(sv)
		key.density = min(density, furTile)
	case density > furTile:
		tilesU = int(math.Round(float64(density) / furTile))
		tilesV = tilesU
		key.density = furTile
	}
	tiled := projected || tilesU > 1 || tilesV > 1
	strands, err := c.strands(key)
	if err != nil {
		return nil, err
	}
	f := c.fur
	// Each shell has slices of its own: the caller may edit one.
	skin := Geometry{Vertices: slices.Clone(base.Vertices), Indices: slices.Clone(base.Indices), UV: slices.Clone(base.UV)}
	if key.source != nil {
		// The skin wears the base's material, as the coat it is, but opaque.
		skin.Material = cloneMaterial(base.Material)
		skin.Material.Transparent = false
	} else {
		shade := func(v uint8) uint8 { return uint8(float64(v) * .6) }
		for i := range skin.Vertices {
			skin.Vertices[i].Color = color.RGBA{shade(f.Root.R), shade(f.Root.G), shade(f.Root.B), 255}
		}
	}
	skin.Bounds = GeometryBounds(skin)
	shells := []Geometry{skin}
	uv := base.UV
	if tiled {
		uv = make([]UV, len(base.UV))
		for i, t := range base.UV {
			uv[i] = UV{U: t.U * float32(tilesU), V: t.V * float32(tilesV)}
		}
	}
	for k := 1; k < f.Shells; k++ {
		h := f.height(k)
		// Strands colored from a texture repeat, and are tinted by the
		// vertices' colors, as that texture is on the base.
		g := Geometry{Vertices: slices.Clone(base.Vertices), Indices: slices.Clone(base.Indices), UV: slices.Clone(uv), Material: &Material{Texture: strands, Cutout: true, Threshold: h, Repeat: tiled || key.repeat, Tint: key.source != nil && base.Material.Tint}}
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

// finRows is the height, in texels, of the fin texture's strands.
const finRows = 32

// finTexture is a side view of strands: gray, darker toward their roots,
// tinted by the fins' vertex colors. Rows run from tips (top) to roots.
func (c *FurCoat) finTexture() (*Texture, error) {
	if c.fins != nil {
		return c.fins, nil
	}
	strands, width, cell := c.finStrands()
	img := image.NewNRGBA(image.Rect(0, 0, width, finRows))
	for y := range finRows {
		h := 1 - (float64(y)+.5)/finRows // 0 at the root, 1 at the tip.
		for x := range width {
			// A strand reaches past its cell, so a texel is in the tallest
			// of its cell's strand and its neighbors', wrapping around the
			// texture's edges as it repeats.
			px, cx := float64(x)+.5, min(furTile-1, int((float64(x)+.5)/cell))
			along := -1.0
			for dx := -1; dx <= 1; dx++ {
				n := cx + dx
				s := strands[(n+furTile)%furTile]
				offset := float64(n-(n+furTile)%furTile) * cell
				if a := h / s.length; h <= s.length && math.Abs(px-s.x-offset) <= s.radius*(1-.6*a) {
					along = max(along, a)
				}
			}
			if along < 0 {
				continue
			}
			v := uint8(255 * (.55 + .45*math.Sqrt(along)) * (.7 + .3*h))
			img.SetNRGBA(x, y, color.NRGBA{v, v, v, 255})
		}
	}
	t, err := NewTexture(img)
	if err != nil {
		return nil, err
	}
	c.fins = t
	return t, nil
}

// finStrand is one strand of the fin texture: where it grows, in texels,
// its length as a fraction of the fur's, and its radius at the root.
type finStrand struct{ x, length, radius float64 }

// finStrands places the fin texture's strands, one in each of furTile cells
// across it, somewhere in the middle half of the cell.
func (c *FurCoat) finStrands() (strands []finStrand, width int, cell float64) {
	width, cell = furTile*furTexelsPerStrand, float64(furTexelsPerStrand)
	rng := rand.New(rand.NewPCG(c.fur.Seed^0x5f1e, 0x2545f4914f6cdd1d))
	strands = make([]finStrand, furTile)
	for i := range strands {
		strands[i] = finStrand{x: (float64(i) + .25 + .5*rng.Float64()) * cell, length: .55 + .45*rng.Float64(), radius: (.36 + .14*rng.Float64()) * cell}
	}
	return strands, width, cell
}

// spacing is about how far apart the shells' strands grow on a base, in the
// mesh's units, so that fins space theirs alike: Spacing, unless a texture
// caps how many strands it colors; for a mesh without UVs, as projected;
// otherwise the strands across the UV square, over the surface it spans.
func (c *FurCoat) spacing(base Geometry) float32 {
	if c.fur.Spacing != 0 && !textured(base) {
		return c.fur.Spacing
	}
	span := 0.0
	if len(base.UV) == len(base.Vertices) {
		span = uvSpan(base)
	}
	if span == 0 {
		if c.fur.Spacing != 0 {
			return c.fur.Spacing
		}
		b := GeometryBounds(base)
		size := b.Max.Sub(b.Min)
		return max(size.X, size.Y, size.Z) / float32(c.fur.Density)
	}
	strands := c.density(base)
	if textured(base) {
		strands = min(strands, furMaxTextured)
	}
	return float32(span / float64(strands))
}

// Fins returns the fur's fins on a base mesh: on every edge, a strip of
// strands standing out along the normals, as long as the fur. They are drawn
// only where the surface turns edge-on to the viewer (Material.EdgeOn), so
// they fill the silhouette, where the shells' layers would show as dashes,
// and vanish elsewhere. Strips face both ways. A textured base tints them
// from its texture, darker at the root; otherwise they shade from Root to
// Tip. Place the fins' node as the base is placed. Sway moves shells only;
// fins stand straight, which shows little, since they are seen only edge-on.
func (c *FurCoat) Fins(base Geometry) (Geometry, error) {
	if err := base.Validate(); err != nil {
		return Geometry{}, err
	}
	if len(base.Indices) == 0 {
		return Geometry{}, fmt.Errorf("nt3d: fur needs a mesh")
	}
	if cracks(base) {
		smooth, err := SmoothNormals(base, 180)
		if err != nil {
			return Geometry{}, err
		}
		base = smooth
	}
	texture, err := c.finTexture()
	if err != nil {
		return Geometry{}, err
	}
	f := c.fur
	tile := float64(c.spacing(base)) * furTile
	source := (*Texture)(nil)
	if textured(base) {
		source = base.Material.Texture
	}
	// Each edge once, by its corners' positions, whichever face names it.
	type edge struct{ a, b nt3dmath.Vec3 }
	seen := map[edge]bool{}
	fins := Geometry{Material: &Material{Texture: texture, Cutout: true, EdgeOn: true, Repeat: true, Tint: true}}
	for i := 0; i+2 < len(base.Indices); i += 3 {
		for k := range 3 {
			ia, ib := base.Indices[i+k], base.Indices[i+(k+1)%3]
			a, b := base.Vertices[ia], base.Vertices[ib]
			key := edge{a.Position, b.Position}
			if key.b.X < key.a.X || key.b.X == key.a.X && (key.b.Y < key.a.Y || key.b.Y == key.a.Y && key.b.Z < key.a.Z) {
				key.a, key.b = key.b, key.a
			}
			if seen[key] || key.a == key.b {
				continue
			}
			seen[key] = true
			// Root and tip colors at each end.
			colors := func(i uint32) (root, tip color.RGBA) {
				if source == nil {
					return f.Root, f.Tip
				}
				// As the base shows its texture: repeating, and tinted.
				t := source.sampleWrap(base.UV[i], base.Material.Repeat)
				tip = color.RGBA{t.R, t.G, t.B, 255}
				if base.Material.Tint {
					v := base.Vertices[i].Color
					tint := func(a, b uint8) uint8 { return uint8(uint16(a) * uint16(b) / 255) }
					tip = color.RGBA{tint(tip.R, v.R), tint(tip.G, v.G), tint(tip.B, v.B), 255}
				}
				return color.RGBA{uint8(float32(tip.R) * .55), uint8(float32(tip.G) * .55), uint8(float32(tip.B) * .55), 255}, tip
			}
			ra, ta := colors(ia)
			rb, tb := colors(ib)
			na, nb := a.Normal.Normalize(), b.Normal.Normalize()
			u := float32(float64(math.Sqrt(float64(b.Position.Sub(a.Position).Dot(b.Position.Sub(a.Position))))) / tile)
			first := uint32(len(fins.Vertices))
			fins.Vertices = append(fins.Vertices,
				Vertex{Position: a.Position, Normal: na, Color: ra},
				Vertex{Position: b.Position, Normal: nb, Color: rb},
				Vertex{Position: b.Position.Add(nb.Scale(f.Length)), Normal: nb, Color: tb},
				Vertex{Position: a.Position.Add(na.Scale(f.Length)), Normal: na, Color: ta})
			fins.UV = append(fins.UV, UV{0, 1}, UV{u, 1}, UV{u, 0}, UV{0, 0})
			// Both windings, as back faces are culled.
			fins.Indices = append(fins.Indices, first, first+1, first+2, first, first+2, first+3, first, first+2, first+1, first, first+3, first+2)
		}
	}
	fins.Bounds = GeometryBounds(fins)
	return fins, fins.Validate()
}
