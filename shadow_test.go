// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"slices"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

func TestDropShadowValidation(t *testing.T) {
	g := quad(1, 0, white, nil, false)
	for _, c := range []struct {
		s, r float32
		ok   bool
	}{{0, 0, true}, {.5, 0, true}, {1, 2, true}, {1.5, 0, false}, {-.1, 0, false}, {.5, -1, false}, {float32(math.NaN()), 0, false}, {.5, float32(math.Inf(1)), false}} {
		g.DropShadow = DropShadow{c.s, c.r}
		if err := g.Validate(); (err == nil) != c.ok {
			t.Errorf("%+v: %v", c, err)
		}
	}
}

func TestDropShadowSurvivesTransformAndMerge(t *testing.T) {
	g := quad(1, 0, white, nil, false)
	g.DropShadow = DropShadow{Strength: .4, Radius: 2}
	moved, err := TransformGeometry(g, nt3dmath.Translation(nt3dmath.Vec3{X: 1}))
	check(t, err)
	later := quad(1, 2, white, nil, false)
	later.DropShadow = DropShadow{Strength: .8, Radius: 3}
	merged, err := MergeGeometry(quad(1, 1, white, nil, false), g, later)
	check(t, err)
	if moved.DropShadow != g.DropShadow || merged.DropShadow != g.DropShadow {
		t.Fatal("lost drop shadow or failed to retain first active shadow")
	}
}

func TestShadowCaster(t *testing.T) {
	down := Light{Direction: nt3dmath.Vec3{Z: 1}, Shadows: true}
	cases := []struct {
		l    Lighting
		want int
	}{
		{Lighting{}, -1},
		{Lighting{Lights: []Light{{Kind: PointLight, Position: nt3dmath.Vec3{Z: 1}, Shadows: true}}}, -1},
		{Lighting{Lights: []Light{{Direction: nt3dmath.Vec3{Z: 1}}, down}}, 1},
		{Lighting{Lights: []Light{down, down}}, 0},
	}
	for i, c := range cases {
		if got := shadowCaster(c.l); got != c.want {
			t.Errorf("%d: %d", i, got)
		}
	}
}
func TestFitShadowEnclosesBounds(t *testing.T) {
	f := blendScene(t, nil, quad(.5, .3, white, nil, false)) // floor ±3 at z -1, quad at z .3
	f.Lighting = Lighting{Lights: []Light{{Direction: nt3dmath.Vec3{X: 1, Z: 1}, Shadows: true}}}
	v, ok := fitShadow(&f, f.drawn(1), 2048)
	if !ok || v.light != 0 {
		t.Fatal(ok, v.light)
	}
	for _, p := range []nt3dmath.Vec3{{X: -3, Y: -3, Z: -1}, {X: 3, Y: 3, Z: -1}, {X: .5, Y: .5, Z: .3}} {
		x, y, z := v.mapCoords(p)
		if x < 0 || x > 2048 || y < 0 || y > 2048 || z < 0 || z > 1 {
			t.Errorf("%v -> %v %v %v", p, x, y, z)
		}
	}
	if v.texel <= 0 || v.texel > 6.2/2048*1.1 {
		t.Errorf("texel %v", v.texel)
	}
}

// A front face keeps its winding seen from the light: the floor's upward
// CCW quad, under a light from above, projects with the same screen-space
// sign as the camera gives it (negative area, y down), so CullModeBack and
// raster's test keep it.
func TestFitShadowKeepsWinding(t *testing.T) {
	f := blendScene(t, nil)
	for _, dir := range []nt3dmath.Vec3{{Z: 1}, {X: 1, Z: 1}, {X: -1, Y: 2, Z: .5}} {
		f.Lighting = Lighting{Lights: []Light{{Direction: dir, Shadows: true}}}
		v, ok := fitShadow(&f, f.drawn(1), 64)
		if !ok {
			t.Fatal(dir)
		}
		q := f.Geometry[0] // The floor: vertices 0, 1, 2 are its first CCW triangle.
		var x, y [3]float32
		for k := range 3 {
			x[k], y[k], _ = v.mapCoords(q.Vertices[q.Indices[k]].Position)
		}
		if area := (x[1]-x[0])*(y[2]-y[0]) - (y[1]-y[0])*(x[2]-x[0]); area >= 0 {
			t.Errorf("%v: area %v, should be negative like the camera's", dir, area)
		}
	}
}
func TestFitShadowStraightDownAndNoBounds(t *testing.T) {
	f := blendScene(t, nil)
	f.Lighting = Lighting{Lights: []Light{{Direction: nt3dmath.Vec3{Z: 1}, Shadows: true}}}
	if _, ok := fitShadow(&f, f.drawn(1), 64); !ok {
		t.Fatal("vertical light should fit")
	}
	f.Geometry[0].Bounds = nt3dmath.AABB{}
	if _, ok := fitShadow(&f, f.drawn(1), 64); ok {
		t.Fatal("no bounds, no fit")
	}
}

func TestFitShadowDrawnWorldBoundsAndPadding(t *testing.T) {
	g := quad(1, 0, white, nil, false)
	for _, vertex := range g.Vertices {
		g.Bounds.Include(vertex.Position)
	}
	lighting := Lighting{Lights: []Light{{Kind: PointLight, Shadows: true}, {Direction: nt3dmath.Vec3{Z: 1}, Shadows: true}}}
	f := Frame{Geometry: []Geometry{g, g}, Lighting: lighting, Instances: []Instance{
		{World: nt3dmath.Translation(nt3dmath.Vec3{X: 10, Z: 4}), Visible: true},
		{World: nt3dmath.Translation(nt3dmath.Vec3{X: 100}), Visible: true},
	}}
	v, ok := fitShadow(&f, []bool{true, false}, 100)
	if !ok || v.light != 1 || newRig(&f).shadowLight != 1 {
		t.Fatalf("fit %v, light %v, rig %v", ok, v.light, newRig(&f).shadowLight)
	}
	if math.Abs(float64(v.texel-.0204)) > 1e-6 {
		t.Fatalf("texel %v, want .0204", v.texel)
	}
	x, y, z := v.mapCoords(nt3dmath.Vec3{X: 10, Z: 4})
	if math.Abs(float64(x-50)) > .001 || math.Abs(float64(y-50)) > .001 || z < 0 || z > 1 {
		t.Fatalf("world center maps to %v %v %v", x, y, z)
	}
	if _, ok := fitShadow(&f, []bool{false, false}, 100); ok {
		t.Fatal("undrawn bounds must not fit")
	}
	f.Lighting = Lighting{}
	if _, ok := fitShadow(&f, []bool{true, true}, 100); ok || newRig(&f).shadowLight != -1 {
		t.Fatal("no casting light must disable shadow fit and rig")
	}
}

// shadowLit is a quarter ambient and a slanted casting light.
var shadowLit = Lighting{Ambient: Ambient{Sky: white, Ground: white, Intensity: .25}, Lights: []Light{{Direction: nt3dmath.Vec3{X: 1, Z: 1}, Intensity: .75, Shadows: true}}}

// pixel is where world point p lands in f.
func pixel(f Frame, p nt3dmath.Vec3) (int, int) {
	x, y, _, _ := f.Matrix.Project(p, f.Width, f.Height)
	return int(x), int(y)
}
func TestSoftwareShadowFalls(t *testing.T) {
	// A .4 quad at z .3 casts a shadow 1.3 units toward -X on the floor.
	f := blendScene(t, &shadowLit, quad(.4, .3, white, nil, false))
	img := softwareRender(f)
	sx, sy := pixel(f, nt3dmath.Vec3{X: -1.3, Z: -1})
	lx, ly := pixel(f, nt3dmath.Vec3{X: 1.5, Z: -1})
	if s, l := at(img, sx, sy), at(img, lx, ly); int(s.B)+60 > int(l.B) {
		t.Fatalf("shadow %v, lit %v", s, l)
	}
}
func TestSoftwareShadowNeedsNoOtherPosition(t *testing.T) {
	f := blendScene(t, &shadowLit, quad(.4, .3, white, nil, false))
	if f.Fog.on() || f.Geometry[1].Material != nil || newRig(&f).points {
		t.Fatal("scene requires only shadow positions")
	}
	img := softwareRender(f)
	sx, sy := pixel(f, nt3dmath.Vec3{X: -1.3, Z: -1})
	lx, ly := pixel(f, nt3dmath.Vec3{X: 1.5, Z: -1})
	if s, l := at(img, sx, sy), at(img, lx, ly); int(s.B)+60 > int(l.B) {
		t.Fatalf("shadow %v, lit %v", s, l)
	}
}

// shadowScenes are the shadow test scenes both renderers draw, each under
// shadowLit over the blue floor: a caster of each kind, a glass receiver,
// and no caster at all. The cutout's texture is 2x1: an opaque left texel,
// a transparent right one, on a quad whose UVs run 0..1 across X.
func shadowScenes(t *testing.T) map[string]Frame {
	glass := quad(.4, .3, color.RGBA{255, 255, 255, 128}, &Material{Transparent: true}, false)
	box := Geometry{Boxes: []Box{{Min: nt3dmath.Vec3{X: -.4, Y: -.4, Z: .3}, Size: nt3dmath.Vec3{X: .8, Y: .8, Z: .2}, Color: white}}}
	cut := quad(.4, .3, white, &Material{Texture: twoTexel(t), Cutout: true}, false)
	cut.UV = []UV{{0, 0}, {1, 0}, {1, 1}, {0, 1}} // UV per vertex: (0,0) (1,0) (1,1) (0,1)
	return map[string]Frame{
		"quad casts":          blendScene(t, &shadowLit, quad(.4, .3, white, nil, false)),
		"glass casts nothing": blendScene(t, &shadowLit, glass),
		"box casts":           blendScene(t, &shadowLit, box),
		"cutout casts":        blendScene(t, &shadowLit, cut),
		"onto glass":          glassScene(t),
		"no caster":           blendScene(t, &shadowLit),
		"glass with a box":    blendScene(t, &shadowLit, Geometry{Vertices: glass.Vertices, Indices: glass.Indices, Material: glass.Material, Boxes: box.Boxes}),
	}
}
func TestSoftwareShadowCasters(t *testing.T) {
	scenes := shadowScenes(t)
	floor := func(name string, x float32) color.RGBA {
		f := scenes[name]
		px, py := pixel(f, nt3dmath.Vec3{X: x, Z: -1})
		return at(softwareRender(f), px, py)
	}
	darker := func(a, b color.RGBA, by int) bool { return int(a.B)+by <= int(b.B) }
	if !darker(floor("box casts", -1.3), floor("box casts", 1.5), 60) {
		t.Error("box")
	}
	if darker(floor("glass casts nothing", -1.3), floor("glass casts nothing", 1.5), 10) {
		t.Error("glass cast")
	}
	if !darker(floor("cutout casts", -1.5), floor("cutout casts", 1.5), 60) || darker(floor("cutout casts", -1.1), floor("cutout casts", 1.5), 10) {
		t.Error("cutout")
	}
	if darker(floor("no caster", -1.3), floor("no caster", 1.5), 10) {
		t.Error("no caster")
	}
	if !darker(floor("glass with a box", -1.3), floor("glass with a box", 1.5), 60) {
		t.Error("a transparent geometry's box still casts")
	}
}

// A caster without bounds reaching outside the fitted depth range casts only
// within it, as the GPU clips: a tall unbounded wall behind the fitted box
// (farther from the light than everything bounded) darkens nothing.
func TestSoftwareShadowClipsDepth(t *testing.T) {
	f := blendScene(t, &shadowLit, quad(.4, .3, white, nil, false))
	wall := quad(3, -5, white, nil, false) // Below the floor, outside the fit, which the floor's bounds end at z -1.
	wall.Bounds = nt3dmath.AABB{}
	f.Geometry = append(f.Geometry, wall)
	f.Instances = append(f.Instances, Instance{World: nt3dmath.Identity(), Visible: true})
	f.GeometryRevisions, f.Shared = nil, nil
	m := buildShadowMap(&f, f.drawn(1), 256)
	for _, d := range m.depth {
		if d < 0 || d > 1 {
			t.Fatalf("depth %v outside 0..1", d)
		}
	}
}
func TestSoftwareShadowOnGlass(t *testing.T) {
	f := shadowScenes(t)["onto glass"]
	img := softwareRender(f)
	sx, sy := pixel(f, nt3dmath.Vec3{X: -.8, Z: -.5}) // In the pane's own shadow, over unshadowed unlit floor.
	lx, ly := pixel(f, nt3dmath.Vec3{X: 1.5, Z: -.5})
	if s, l := at(img, sx, sy), at(img, lx, ly); int(s.R)+20 > int(l.R) {
		t.Fatalf("shadow on glass %v, lit glass %v", s, l)
	}
}

// A shadow takes only the casting light's diffuse and specular; ambient and other lights stay.
func TestRigShadowScalesOneLight(t *testing.T) {
	f := Frame{Lighting: Lighting{Ambient: Ambient{Sky: white, Ground: white, Intensity: .2}, Lights: []Light{
		{Direction: nt3dmath.Vec3{Z: 1}, Intensity: .5, Shadows: true}, {Direction: nt3dmath.Vec3{X: 1}, Intensity: .3}}}}
	rig := newRig(&f)
	// A 64-texel map under the identity: the receiver at z .5 reads map
	// depth .5 (offset by 1.5 texels of .01 along n, still inside); every
	// stored depth is 0, nearer the light than .5 by far more than the bias
	// ((1/64)*(1+2*(1-.8)) = .022), so the light is blocked.
	dark := &shadowMap{shadowView: shadowView{matrix: nt3dmath.Identity(), texel: .01, size: 64, light: 0}, depth: make([]float32, 64*64)}
	n, p := nt3dmath.Vec3{X: .6, Z: .8}, nt3dmath.Vec3{Z: .5}
	shiny := &Material{Specular: .5, Shininess: 16}
	view := nt3dmath.Vec3{Z: 1}
	open, openHighlight := illuminate(&rig, n, p), rig.highlight(shiny, n, p, view)
	rig.shadow = dark
	shaded, shadedHighlight := illuminate(&rig, n, p), rig.highlight(shiny, n, p, view)
	if !near3(shaded, [3]float32{.2 + .3*.6, .2 + .3*.6, .2 + .3*.6}, .01) || near3(open, shaded, .01) {
		t.Fatalf("open %v, shaded %v", open, shaded)
	}
	if shadedHighlight[0] >= openHighlight[0] {
		t.Fatalf("highlight open %v, shaded %v", openHighlight, shadedHighlight)
	}
	// Only the casting light's highlight went: with that light alone the
	// shaded highlight is zero, and with the other alone it is unchanged.
	solo := f
	solo.Lighting.Lights = []Light{f.Lighting.Lights[1]}
	other := newRig(&solo)
	other.shadow = dark
	if h := other.highlight(shiny, n, p, view); !near3(h, [3]float32{shadedHighlight[0], shadedHighlight[1], shadedHighlight[2]}, .001) {
		t.Fatalf("other light's highlight %v, shaded %v", h, shadedHighlight)
	}
}

func twoTexel(t *testing.T) *Texture {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{255, 255, 255, 255})
	tex, err := NewTexture(img)
	check(t, err)
	return tex
}
func glassScene(t *testing.T) Frame {
	t.Helper()
	f := blendScene(t, &shadowLit, quad(.4, .3, white, nil, false), quad(3, -.5, color.RGBA{255, 255, 255, 128}, &Material{Transparent: true}, false))
	f.Geometry[0].Material = &Material{Unlit: true}
	return f
}

func TestSoftwareShadowSize(t *testing.T) {
	for _, c := range []struct {
		w, h     int
		complete bool
		want     int
	}{{80, 60, false, 320}, {400, 80, false, 1024}, {80, 60, true, 2048}} {
		if got := softwareShadowSize(&Frame{Width: c.w, Height: c.h, Complete: c.complete}); got != c.want {
			t.Errorf("%+v: %d", c, got)
		}
	}
}

func TestShadowVisibilityKernelAndBounds(t *testing.T) {
	m := &shadowMap{shadowView: shadowView{matrix: nt3dmath.Identity(), size: 8}, depth: make([]float32, 64)}
	n, l := nt3dmath.Vec3{Z: 1}, nt3dmath.Vec3{Z: 1}
	// Center maps to (4,4); exactly three of its nine neighbors are open.
	for x := 3; x <= 5; x++ {
		m.depth[3*8+x] = 1
	}
	if got := m.visibility(nt3dmath.Vec3{Z: .5}, n, l); abs(got-1.0/3) > .00001 {
		t.Fatalf("PCF %v", got)
	}
	for _, p := range []nt3dmath.Vec3{{X: 2, Z: .5}, {Y: 2, Z: .5}, {Z: -.1}, {Z: 1.1}} {
		if got := m.visibility(p, n, l); got != 1 {
			t.Errorf("outside %v: %v", p, got)
		}
	}
	// Clamped edge taps repeat the first column three times.
	for i := range m.depth {
		m.depth[i] = 0
	}
	for y := range 8 {
		m.depth[y*8] = 1
	}
	if got := m.visibility(nt3dmath.Vec3{X: -1, Z: .5}, n, l); abs(got-2.0/3) > .00001 {
		t.Fatalf("edge PCF %v", got)
	}
	// Equality at the normalized bias counts as lit.
	for i := range m.depth {
		m.depth[i] = .375
	}
	if got := m.visibility(nt3dmath.Vec3{Z: .5}, n, l); got != 1 {
		t.Fatalf("bias equality %v", got)
	}
}

func TestSoftwareDropShadow(t *testing.T) {
	// A .6 cube standing on the floor at x 1.5 (its bottom at z -1), seen from
	// above, with a disc of radius .6 reaching past its footprint: the floor
	// at y .4 (d = .667, alpha = .5*(1-.444)² = .15, about 30 levels of the floor blue (200))
	// is darker than open floor, and the cube's top is untouched. (The
	// automatic radius, .3, would lie wholly under the cube from above.)
	cube, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: .6, Y: .6, Z: .6}, Color: white})
	check(t, err)
	cube, err = TransformGeometry(cube, nt3dmath.Translation(nt3dmath.Vec3{X: 1.5, Z: -.7}))
	check(t, err)
	cube.DropShadow = DropShadow{Strength: .5, Radius: .6}
	f := blendScene(t, nil, cube) // Even light: the floor's blue is uniform.
	f.Linear = false              // Assert falloff directly in encoded blue levels.
	img := softwareRender(f)
	ex, ey := pixel(f, nt3dmath.Vec3{X: 1.5, Y: .4, Z: -1})
	ox, oy := pixel(f, nt3dmath.Vec3{X: 1.5, Y: -.8, Z: -1})
	tx, ty := pixel(f, nt3dmath.Vec3{X: 1.5, Z: -.4})
	if edge, open := at(img, ex, ey), at(img, ox, oy); int(edge.B)+20 > int(open.B) {
		t.Fatalf("disc edge %v, open floor %v", edge, open)
	}
	if top := at(img, tx, ty); top != white {
		t.Fatalf("top %v", top)
	}
	// Fogged wholly, the disc is the fog color.
	f.Fog = Fog{Near: 0, Far: 1}
	if c := at(softwareRender(f), ex, ey); !closeRGB(c, f.Background, 2) {
		t.Fatalf("fogged disc %v", c)
	}
}

// The automatic radius is the transformed bounds' larger horizontal half
// extent, the disc centered under them on their bottom plane.
func TestDropDisc(t *testing.T) {
	g := Geometry{Boxes: []Box{{Min: nt3dmath.Vec3{X: -1, Y: -.5, Z: 2}, Size: nt3dmath.Vec3{X: 2, Y: 1, Z: 3}}}}
	g.Bounds = GeometryBounds(g)
	g.DropShadow = DropShadow{Strength: .3}
	c, r, s, ok := dropDisc(&g, nt3dmath.Translation(nt3dmath.Vec3{X: 10}))
	if !ok || c != (nt3dmath.Vec3{X: 10, Y: 0, Z: 2}) || r != 1 || s != .3 {
		t.Fatalf("%v %v %v %v", c, r, s, ok)
	}
	g.DropShadow.Radius = 4
	if _, r, _, _ = dropDisc(&g, nt3dmath.Identity()); r != 4 {
		t.Fatal(r)
	}
}
func TestDropShadowNeedsBounds(t *testing.T) {
	g := quad(.4, .3, white, nil, false)
	f := blendScene(t, nil, g)
	plain := softwareRender(f)
	f.Geometry[1].DropShadow = DropShadow{Strength: 1}
	f.Geometry[1].Bounds = nt3dmath.AABB{}
	if !bytes.Equal(softwareRender(f).(*image.RGBA).Pix, plain.(*image.RGBA).Pix) {
		t.Fatal("a disc without bounds")
	}
}
func TestDropShadowCrossesNearPlane(t *testing.T) {
	// The camera lies on the floor looking across a big disc under it: the
	// quad crosses the near plane. It still draws (some floor pixels darken)
	// and nothing panics.
	g := quad(.1, 0, white, nil, false)
	g.DropShadow = DropShadow{Strength: 1, Radius: 4}
	s := NewScene()
	_, err := s.SetGeometry("floor", quad(6, -1, blue, nil, false))
	check(t, err)
	_, err = s.SetGeometry("g", g)
	check(t, err)
	camera := DefaultCamera()
	camera.Projection, camera.Alpha, camera.Beta, camera.Distance = Perspective, 1, 0, 2
	f, err := s.Frame(camera, 80, 80)
	check(t, err)
	f.Lighting = even
	plain := f
	plain.Geometry = slices.Clone(f.Geometry)
	plain.Geometry[1].DropShadow = DropShadow{}
	if bytes.Equal(softwareRender(f).(*image.RGBA).Pix, softwareRender(plain).(*image.RGBA).Pix) {
		t.Fatal("no disc drawn")
	}
}
