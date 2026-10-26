// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"math"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/sh"
	"slices"
	"testing"
)

// paletteGeometry is n splats of degree 3 whose bands are k distinct sets.
func paletteGeometry(t *testing.T, n, k int) Geometry {
	t.Helper()
	g := shGaussianGeometry(3)
	base := g.Points[0]
	g.Points = make([]Point, n)
	g.GaussianSH = make([]nt3dsh.RGB, 0, n)
	g.GaussianSHLabels = make([]uint16, n)
	p := &SHPalette{Degree: 3}
	for c := range k {
		for j := range 15 {
			p.Coefficients = append(p.Coefficients, nt3dsh.RGB{float32(c)*.01 + float32(j)*.001, float32(c) * .02, -float32(j) * .003})
		}
	}
	for i := range n {
		base.Position = nt3dmath.Vec3{X: float32(i%7) * .1, Y: float32(i%5) * .1, Z: float32(i%3) * .1}
		g.Points[i] = base
		g.GaussianSH = append(g.GaussianSH, nt3dsh.RGB{.2, .3, .4})
		g.GaussianSHLabels[i] = uint16(i % k)
	}
	g.GaussianSHPalette = p
	g.Bounds = GeometryBounds(g)
	check(t, g.Validate())
	return g
}

func TestValidateSHPalette(t *testing.T) {
	g := paletteGeometry(t, 50, 4)
	if g.GaussianSHPalette.Count() != 4 || g.GaussianSHPalette.Stride() != 15 || gaussianSHTotal(&g) != 16 || gaussianSHCount(&g) != 1 {
		t.Fatalf("count %d stride %d total %d per point %d", g.GaussianSHPalette.Count(), g.GaussianSHPalette.Stride(), gaussianSHTotal(&g), gaussianSHCount(&g))
	}
	for name, mutate := range map[string]func(*Geometry){
		"label past count":       func(g *Geometry) { g.GaussianSHLabels[3] = 4 },
		"labels without palette": func(g *Geometry) { g.GaussianSHPalette = nil },
		"palette without labels": func(g *Geometry) { g.GaussianSHLabels = nil },
		"short labels":           func(g *Geometry) { g.GaussianSHLabels = g.GaussianSHLabels[:49] },
		"degree 0": func(g *Geometry) {
			g.GaussianSHPalette = &SHPalette{Degree: 0, Coefficients: g.GaussianSHPalette.Coefficients}
		},
		"degree 4": func(g *Geometry) {
			g.GaussianSHPalette = &SHPalette{Degree: 4, Coefficients: g.GaussianSHPalette.Coefficients}
		},
		"off stride": func(g *Geometry) {
			g.GaussianSHPalette = &SHPalette{Degree: 3, Coefficients: g.GaussianSHPalette.Coefficients[:59]}
		},
		"empty palette":        func(g *Geometry) { g.GaussianSHPalette = &SHPalette{Degree: 3} },
		"SH not one per point": func(g *Geometry) { g.GaussianSH = append(g.GaussianSH, nt3dsh.RGB{}) },
		"non-finite centroid": func(g *Geometry) {
			p := *g.GaussianSHPalette
			p.Coefficients = slices.Clone(p.Coefficients)
			p.Coefficients[7][1] = float32(math.NaN())
			g.GaussianSHPalette = &p
		},
		"not Gaussian": func(g *Geometry) { g.PointStyle.Shape = HardPoint },
	} {
		h := g
		h.GaussianSHLabels = slices.Clone(g.GaussianSHLabels)
		mutate(&h)
		if err := h.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// Too many centroids.
	big := SHPalette{Degree: 1, Coefficients: make([]nt3dsh.RGB, 3*(MaxSHPaletteCentroids+1))}
	h := g
	h.GaussianSHPalette = &big
	if err := h.Validate(); err == nil {
		t.Error("accepted 65537 centroids")
	}
	// Expansion equals the per-splat form, coefficient for coefficient.
	e := g
	ExpandSHPalette(&e)
	if e.GaussianSHPalette != nil || e.GaussianSHLabels != nil || len(e.GaussianSH) != 16*50 || g.GaussianSHPalette == nil {
		t.Fatal("expand did not produce 16 coefficients a splat, or mutated its input")
	}
	for i := range 50 {
		want := g.GaussianSHPalette.Coefficients[int(g.GaussianSHLabels[i])*15 : int(g.GaussianSHLabels[i])*15+15]
		if e.GaussianSH[16*i] != g.GaussianSH[i] || !slices.Equal(e.GaussianSH[16*i+1:16*i+16], want) {
			t.Fatalf("splat %d expanded wrong", i)
		}
	}
	check(t, e.Validate())
	// The software color of the two forms agrees exactly.
	f := flatFrame(80, 80, g)
	f.Forward = nt3dmath.Vec3{X: 1}
	for i := range 50 {
		if a, b := f.gaussianColor(&g, i, nt3dmath.Identity(), nt3dmath.Identity()), f.gaussianColor(&e, i, nt3dmath.Identity(), nt3dmath.Identity()); a != b {
			t.Fatalf("splat %d: palette %v, expanded %v", i, a, b)
		}
	}
	g.PointStyle.SHDCOnly, e.PointStyle.SHDCOnly = true, true
	if a, b := f.gaussianColor(&g, 3, nt3dmath.Identity(), nt3dmath.Identity()), f.gaussianColor(&e, 3, nt3dmath.Identity(), nt3dmath.Identity()); a != b {
		t.Fatal("DC-only differs between forms")
	}
}

func TestSHPaletteOperations(t *testing.T) {
	g := paletteGeometry(t, 50, 4)
	// Clip keeps labels with their points and shares the palette.
	var box nt3dmath.AABB
	box.Include(nt3dmath.Vec3{X: -1, Y: -1, Z: -1})
	box.Include(nt3dmath.Vec3{X: .25, Y: 1, Z: 1})
	c, err := Clip(g, box)
	check(t, err)
	if len(c.Points) == 0 || len(c.Points) == 50 || len(c.GaussianSHLabels) != len(c.Points) || len(c.GaussianSH) != len(c.Points) || c.GaussianSHPalette != g.GaussianSHPalette {
		t.Fatalf("clip: %d points, %d labels", len(c.Points), len(c.GaussianSHLabels))
	}
	check(t, c.Validate())
	for i, p := range c.Points {
		j := slices.IndexFunc(g.Points, func(q Point) bool { return q.Position == p.Position })
		if c.GaussianSHLabels[i] != g.GaussianSHLabels[j] {
			t.Fatalf("clipped point %d lost its label", i)
		}
	}
	// Transform keeps both.
	tr, err := TransformGeometry(g, nt3dmath.Translation(nt3dmath.Vec3{X: 2}))
	check(t, err)
	if tr.GaussianSHPalette != g.GaussianSHPalette || !slices.Equal(tr.GaussianSHLabels, g.GaussianSHLabels) {
		t.Fatal("transform dropped the palette or labels")
	}
	// Merge: a shared palette is kept; a mixed merge expands.
	same, err := MergeGeometry(g, tr)
	check(t, err)
	if same.GaussianSHPalette != g.GaussianSHPalette || len(same.GaussianSHLabels) != 100 || len(same.GaussianSH) != 100 {
		t.Fatalf("shared merge: palette %v, %d labels, %d SH", same.GaussianSHPalette == g.GaussianSHPalette, len(same.GaussianSHLabels), len(same.GaussianSH))
	}
	plain := g
	ExpandSHPalette(&plain)
	mixed, err := MergeGeometry(g, plain)
	check(t, err)
	if mixed.GaussianSHPalette != nil || mixed.GaussianSHLabels != nil || len(mixed.GaussianSH) != 16*100 {
		t.Fatal("mixed merge did not expand")
	}
	other := paletteGeometry(t, 50, 4) // Equal bytes, another pointer: shared by value.
	byValue, err := MergeGeometry(g, other)
	check(t, err)
	if byValue.GaussianSHPalette == nil || len(byValue.GaussianSHLabels) != 100 {
		t.Fatal("byte-identical palettes were not shared")
	}
	other.GaussianSHPalette = &SHPalette{Degree: 3, Coefficients: slices.Clone(other.GaussianSHPalette.Coefficients)}
	other.GaussianSHPalette.Coefficients[0][0] += 1
	differ, err := MergeGeometry(g, other)
	check(t, err)
	if differ.GaussianSHPalette != nil || len(differ.GaussianSH) != 16*100 {
		t.Fatal("different palettes were not expanded")
	}
	// Clone shares the palette and copies labels.
	cl := cloneGeometry(g)
	if cl.GaussianSHPalette != g.GaussianSHPalette || &cl.GaussianSHLabels[0] == &g.GaussianSHLabels[0] || !slices.Equal(cl.GaussianSHLabels, g.GaussianSHLabels) {
		t.Fatal("clone")
	}
}

func TestSHPalettePointUpdates(t *testing.T) {
	g := paletteGeometry(t, 50, 4)
	s := NewScene()
	id, err := s.SetGeometry("cloud", g)
	check(t, err)
	// Same count: labels and palette stay.
	moved := slices.Clone(g.Points)
	moved[0].Position.X += .01
	check(t, s.SetNodePoints(id, moved, GeometryBounds(g)))
	f, err := s.Frame(DefaultCamera(), 40, 40)
	check(t, err)
	if h := f.Geometry[0]; h.GaussianSHPalette == nil || len(h.GaussianSHLabels) != 50 {
		t.Fatal("a same-count update dropped the palette")
	}
	check(t, validateFrameGeometry(f.Geometry[0]))
	// A partial range update keeps them too.
	check(t, s.UpdateNodePoints(id, 10, moved[10:20], GeometryBounds(g)))
	f, err = s.Frame(DefaultCamera(), 40, 40)
	check(t, err)
	if h := f.Geometry[0]; h.GaussianSHPalette == nil || len(h.GaussianSHLabels) != 50 || len(h.GaussianSH) != 50 {
		t.Fatal("a range update dropped the palette")
	}
	check(t, validateFrameGeometry(f.Geometry[0]))
	// A count change is refused, leaving the SH state as it was.
	if s.SetNodePoints(id, moved[:20], GeometryBounds(g)) == nil {
		t.Fatal("a count change was accepted")
	}
	f, err = s.Frame(DefaultCamera(), 40, 40)
	check(t, err)
	if h := f.Geometry[0]; h.GaussianSHPalette == nil || len(h.GaussianSHLabels) != 50 {
		t.Fatal("a refused update changed SH state")
	}
}

func TestSHPaletteFrameCertificate(t *testing.T) {
	g := paletteGeometry(t, 50, 4)
	// A manually built frame is checked in full.
	bad := g
	bad.GaussianSHLabels = slices.Clone(g.GaussianSHLabels)
	bad.GaussianSHLabels[9] = 4
	if validateFrameGeometry(bad) == nil {
		t.Fatal("an uncertified label past the palette was accepted")
	}
	// A stored geometry is certified only while its palette keeps its size.
	s := NewScene()
	id, err := s.SetGeometry("cloud", g)
	check(t, err)
	stored := *s.nodes[id].geometry
	check(t, validateFrameGeometry(stored))
	stored.GaussianSHPalette = &SHPalette{Degree: 3, Coefficients: g.GaussianSHPalette.Coefficients[:30]}
	if validateFrameGeometry(stored) == nil {
		t.Fatal("a shrunken palette was accepted for stored labels")
	}
	// Clipping everything away leaves a valid empty geometry.
	var none nt3dmath.AABB
	none.Include(nt3dmath.Vec3{X: 50, Y: 50, Z: 50})
	none.Include(nt3dmath.Vec3{X: 51, Y: 51, Z: 51})
	c, err := Clip(g, none)
	check(t, err)
	if len(c.Points) != 0 || c.GaussianSHPalette != nil || c.GaussianSHLabels != nil {
		t.Fatal("an emptied clip kept palette state")
	}
}
