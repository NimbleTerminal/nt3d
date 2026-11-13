// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// toonRig is gray ambient a under one white directional light toward dir.
func toonRig(a float32, dir nt3dmath.Vec3) lightRig {
	return lightRig{shadowLight: -1, sky: [3]float32{a, a, a}, ground: [3]float32{a, a, a},
		lights: []rigLight{{kind: DirectionalLight, v: dir, color: [3]float32{1, 1, 1}}}}
}

func tilted(degrees float64) nt3dmath.Vec3 {
	r := degrees * math.Pi / 180
	return nt3dmath.Vec3{X: float32(math.Sin(r)), Z: float32(math.Cos(r))}
}

func toonLit(r *lightRig, t ToonStyle, n, view nt3dmath.Vec3) [3]float32 {
	c := [3]float32{1, 1, 1}
	rim := r.toonLight(&c, t, n, nt3dmath.Vec3{}, n, view)
	for k := range c {
		c[k] += rim[k]
	}
	return c
}

func TestToonBand(t *testing.T) {
	for x, want := range map[float32]float32{0: 0, .3: 0, .7: 1, 1: 1} {
		if got := toonBand(x, 2, .01); got != want {
			t.Fatalf("band(%v)=%v want %v", x, got, want)
		}
	}
	for x, want := range map[float32]float32{.1: 0, .5: .5, .9: 1} {
		if got := toonBand(x, 3, .01); abs(got-want) > 1e-6 {
			t.Fatalf("band(%v)=%v want %v", x, got, want)
		}
	}
	if a, b := toonBand(.495, 2, .1), toonBand(.505, 2, .1); !(a > 0 && a < b && b < 1) {
		t.Fatalf("edge is not a blend: %v %v", a, b)
	}
}

func TestToonValidation(t *testing.T) {
	for _, bands := range []int{0, 2, 8} {
		if err := (ToonStyle{Bands: bands}).validate(); err != nil {
			t.Fatalf("bands %d: %v", bands, err)
		}
	}
	nan := float32(math.NaN())
	for _, bad := range []ToonStyle{{Bands: 1}, {Bands: 9}, {Bands: -1}, {Bands: 2, Softness: nan}, {Bands: 2, Softness: -.1}, {Bands: 2, Softness: 1.1}, {Bands: 2, Rim: nan}, {Bands: 2, Rim: -.1}, {Bands: 2, Rim: 1.1}} {
		if bad.validate() == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	g, err := BuildSphere(Sphere{Radius: 1, Segments: 8, Rings: 6, Color: white, Material: &Material{Toon: ToonStyle{Bands: 3}}})
	check(t, err)
	check(t, g.Validate())
	g.Material = &Material{Toon: ToonStyle{Bands: 1}}
	if g.Validate() == nil {
		t.Fatal("geometry accepted an invalid toon style")
	}
}

func TestToonLightBands(t *testing.T) {
	r := toonRig(.2, up)
	two := ToonStyle{Bands: 2}
	for degrees, want := range map[float64]float32{0: 1.2, 45: 1.2, 80: .2} {
		if got := toonLit(&r, two, tilted(degrees), up); !near3(got, [3]float32{want, want, want}, 1e-5) {
			t.Fatalf("%v degrees: %v want %v", degrees, got, want)
		}
	}
	// Distance fade is not banded: a point light with fade 1/(1+3) adds a quarter.
	p := lightRig{shadowLight: -1, points: true, lights: []rigLight{{kind: PointLight, v: nt3dmath.Vec3{Z: float32(math.Sqrt(3))}, color: [3]float32{1, 1, 1}}}}
	if got := toonLit(&p, two, up, up); !near3(got, [3]float32{.25, .25, .25}, 1e-5) {
		t.Fatalf("point light: %v want .25", got)
	}
}

func TestToonShadeAndNoLights(t *testing.T) {
	shade := ToonStyle{Bands: 2, Shade: color.RGBA{128, 128, 255, 255}}
	want := [3]float32{.4 * 128 / 255, .4 * 128 / 255, .4}
	none := lightRig{shadowLight: -1, sky: [3]float32{.4, .4, .4}, ground: [3]float32{.4, .4, .4}}
	if got := toonLit(&none, shade, up, up); !near3(got, want, 1e-5) {
		t.Fatalf("no lights: %v want %v", got, want)
	}
	behind := toonRig(.4, nt3dmath.Vec3{Z: -1})
	if got := toonLit(&behind, shade, up, up); !near3(got, want, 1e-5) {
		t.Fatalf("light behind: %v want %v", got, want)
	}
	if got := toonLit(&none, ToonStyle{Bands: 2}, up, up); !near3(got, [3]float32{.4, .4, .4}, 1e-5) {
		t.Fatalf("unset shade is not white: %v", got)
	}
}

func TestToonHighlightIsFlat(t *testing.T) {
	r := toonRig(0, up)
	m := &Material{Specular: 1, Shininess: 32, Toon: ToonStyle{Bands: 2}}
	if got := r.toonHighlight(m, up, nt3dmath.Vec3{}, up, up); got != [3]float32{1, 1, 1} {
		t.Fatalf("mirror highlight %v", got)
	}
	if got := r.toonHighlight(m, up, nt3dmath.Vec3{}, tilted(60), up); got != [3]float32{} {
		t.Fatalf("off-mirror highlight %v", got)
	}
	if soft := r.highlightNormal(m, up, nt3dmath.Vec3{}, tilted(10), up); !(soft[0] > 0 && soft[0] < 1) {
		t.Fatalf("smooth highlight %v", soft)
	}
}

func TestToonRimOnlyWhereLit(t *testing.T) {
	side := nt3dmath.Vec3{X: 1}
	rim := ToonStyle{Bands: 2, Rim: .5}
	lit := toonRig(.2, up)
	if got := toonLit(&lit, rim, up, side); !near3(got, [3]float32{1.7, 1.7, 1.7}, 1e-5) {
		t.Fatalf("lit rim %v want 1.7", got)
	}
	dark := toonRig(.2, nt3dmath.Vec3{Z: -1})
	if got := toonLit(&dark, rim, up, side); !near3(got, [3]float32{.2, .2, .2}, 1e-5) {
		t.Fatalf("unlit rim %v want .2", got)
	}
	if got := toonLit(&lit, rim, up, up); !near3(got, [3]float32{1.2, 1.2, 1.2}, 1e-5) {
		t.Fatalf("facing rim %v want 1.2", got)
	}
}

var toonSun = Lighting{Ambient: Ambient{Sky: white, Ground: white, Intensity: .25}, Lights: []Light{{Direction: nt3dmath.Vec3{X: 1, Y: -1, Z: 1}, Intensity: .75}}}
var toonSky = color.RGBA{40, 60, 80, 255}

// toonSphere is a white sphere under toonSun, in a size-pixel square frame.
func toonSphere(t *testing.T, m *Material, size int) Frame {
	t.Helper()
	g, err := BuildSphere(Sphere{Radius: .7, Segments: 64, Rings: 40, Color: white, Material: m})
	check(t, err)
	s := NewScene()
	_, err = s.SetGeometry("ball", g)
	check(t, err)
	f, err := s.Frame(DefaultCamera(), size, size)
	check(t, err)
	f.Lighting, f.Background = toonSun, toonSky
	return f
}

// flatTones are the colors, other than skip, of pixels whose neighbors
// within reach pixels all share their color.
func flatTones(img image.Image, reach int, skip color.RGBA) map[color.RGBA]int {
	tones := map[color.RGBA]int{}
	b := img.Bounds()
	for y := b.Min.Y + reach; y < b.Max.Y-reach; y++ {
	pixels:
		for x := b.Min.X + reach; x < b.Max.X-reach; x++ {
			c := at(img, x, y)
			if c == skip {
				continue
			}
			for dy := -reach; dy <= reach; dy++ {
				for dx := -reach; dx <= reach; dx++ {
					if at(img, x+dx, y+dy) != c {
						continue pixels
					}
				}
			}
			tones[c]++
		}
	}
	return tones
}

func TestToonSphereHasFlatTones(t *testing.T) {
	img := softwareRender(toonSphere(t, &Material{Toon: ToonStyle{Bands: 3}}, 200))
	if tones := flatTones(img, 3, toonSky); len(tones) != 3 {
		t.Fatalf("toon sphere has %d flat tones, want 3: %v", len(tones), tones)
	}
	plain := softwareRender(toonSphere(t, &Material{}, 200))
	if tones := flatTones(plain, 0, toonSky); len(tones) <= 20 {
		t.Fatalf("smooth sphere has only %d tones", len(tones))
	}
}

func TestToonZeroBandsIsUnchanged(t *testing.T) {
	plain := softwareRender(toonSphere(t, &Material{Specular: .4}, 96)).(*image.RGBA)
	off := softwareRender(toonSphere(t, &Material{Specular: .4, Toon: ToonStyle{Softness: .5, Rim: .5, Shade: color.RGBA{0, 0, 255, 255}}}, 96)).(*image.RGBA)
	if !bytes.Equal(plain.Pix, off.Pix) {
		t.Fatal("a toon style without bands changed the frame")
	}
}

func TestToonShadowIsTwoToned(t *testing.T) {
	f := blendScene(t, &shadowLit, quad(.4, .3, white, nil, false))
	f.Geometry[0].Material = &Material{Toon: ToonStyle{Bands: 2}}
	img := softwareRender(f)
	sx, sy := pixel(f, nt3dmath.Vec3{X: -1.3, Z: -1})
	lx, ly := pixel(f, nt3dmath.Vec3{X: 1.5, Z: -1})
	shadow, lit := at(img, sx, sy), at(img, lx, ly)
	// The light is 45 degrees off the floor's normal: past the half-way
	// step, so fully lit, where a smooth floor would be at .78.
	if lit != blue || int(shadow.B)+60 > int(lit.B) {
		t.Fatalf("shadow %v, lit %v", shadow, lit)
	}
	between := 0
	for x := sx; x <= lx; x++ {
		y := sy + (ly-sy)*(x-sx)/(lx-sx)
		if c := at(img, x, y); c != shadow && c != lit {
			between++
		}
	}
	if between > 2 {
		t.Fatalf("%d pixels between the shadow's tone and the lit tone", between)
	}
}

func TestToonComposes(t *testing.T) {
	two := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	two.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
	two.SetNRGBA(1, 0, color.NRGBA{0, 0, 255, 255})
	tex, err := NewTexture(two)
	check(t, err)
	painted := quad(.8, .3, white, &Material{Texture: tex, Toon: ToonStyle{Bands: 2}}, false)
	painted.UV = []UV{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	f := blendScene(t, &shadowLit, painted)
	img := softwareRender(f)
	lx, ly := pixel(f, nt3dmath.Vec3{X: -.7, Z: .3})
	rx, ry := pixel(f, nt3dmath.Vec3{X: .7, Z: .3})
	if l, r := at(img, lx, ly), at(img, rx, ry); l.R < 250 || l.B > 5 || r.B < 250 || r.R > 5 {
		t.Fatalf("textured toon quad: left %v, right %v", l, r)
	}

	// The back of a transparent sheet (opaque ones are culled), seen from
	// above, is one of the two tones.
	f = blendScene(t, &shadowLit, quad(.8, .3, white, &Material{Transparent: true, Toon: ToonStyle{Bands: 2}}, true))
	cx, cy := pixel(f, nt3dmath.Vec3{Z: .3})
	if c := at(softwareRender(f), cx, cy); c != white && c != (color.RGBA{137, 137, 137, 255}) {
		t.Fatalf("back face %v is neither tone", c)
	}

	unlit := softwareRender(toonSphere(t, &Material{Unlit: true}, 96)).(*image.RGBA)
	both := softwareRender(toonSphere(t, &Material{Unlit: true, Toon: ToonStyle{Bands: 2}}, 96)).(*image.RGBA)
	if !bytes.Equal(unlit.Pix, both.Pix) {
		t.Fatal("toon changed an unlit material")
	}
}

// toonBands maps each node in s's frame to its material's band count, or -1
// without a material.
func toonBands(t *testing.T, s *Scene) map[NodeID]int {
	t.Helper()
	f := frame(t, s)
	bands := map[NodeID]int{}
	for i, g := range f.Geometry {
		bands[f.Instances[i].Node] = -1
		if g.Material != nil {
			bands[f.Instances[i].Node] = g.Material.Toon.Bands
		}
	}
	return bands
}

func TestSetToonSubtree(t *testing.T) {
	s := NewScene()
	mesh := func(m *Material) Geometry { return quad(.5, 0, white, m, false) }
	a, err := s.Add(Root, "a")
	check(t, err)
	check(t, s.SetNodeGeometry(a, mesh(nil)))
	b, err := s.Add(a, "b")
	check(t, err)
	check(t, s.SetNodeGeometry(b, mesh(&Material{Specular: .5})))
	c, err := s.SetGeometry("c", pointGeometry())
	check(t, err)
	d, err := s.SetGeometry("d", mesh(nil))
	check(t, err)

	check(t, s.SetToon(a, ToonStyle{Bands: 3}))
	if got := toonBands(t, s); got[a] != 3 || got[b] != 3 || got[c] != -1 || got[d] > 0 {
		t.Fatalf("bands after SetToon: %v", got)
	}
	if m := s.nodes[b].geometry.Material; m.Specular != .5 {
		t.Fatalf("SetToon lost the material's other fields: %+v", m)
	}
	before := s.Revision()
	check(t, s.SetToon(a, ToonStyle{Bands: 3}))
	if s.Revision() != before {
		t.Fatal("identical toon style dirtied the scene")
	}
	check(t, s.SetToon(a, ToonStyle{}))
	if got := toonBands(t, s); got[a] != 0 || got[b] != 0 {
		t.Fatalf("bands after disabling: %v", got)
	}
	if s.SetToon(NodeID(999999), ToonStyle{Bands: 2}) == nil || s.SetToon(a, ToonStyle{Bands: 1}) == nil {
		t.Fatal("accepted a missing node or an invalid style")
	}
}

func TestSetToonDoesNotShareMaterials(t *testing.T) {
	s := NewScene()
	shared := &Material{Specular: .25}
	x, err := s.SetGeometry("x", quad(.5, 0, white, shared, false))
	check(t, err)
	y, err := s.SetGeometry("y", quad(.5, 1, white, shared, false))
	check(t, err)
	check(t, s.SetToon(x, ToonStyle{Bands: 2}))
	if got := toonBands(t, s); shared.Toon.Bands != 0 || got[x] != 2 || got[y] != 0 {
		t.Fatalf("caller's material %d, bands %v", shared.Toon.Bands, got)
	}
}

func TestSetToonPreservesBuffersAndPose(t *testing.T) {
	s, id, joint := animationSkinScene(t)
	old := frame(t, s)
	rev := s.nodes[id].geometryRevision
	style := ToonStyle{Bands: 2}
	check(t, s.SetToon(Root, style))
	if s.nodes[id].deformation == nil || s.nodes[id].geometryRevision != rev {
		t.Fatal("toon replaced mesh/deformation")
	}
	if m := old.Geometry[0].Material; m != nil && m.Toon.on() {
		t.Fatal("toon changed an old snapshot")
	}
	next := frame(t, s)
	if &next.Geometry[0].Indices[0] != &old.Geometry[0].Indices[0] {
		t.Fatal("toon copied the index buffer")
	}
	check(t, s.SetTransform(joint, nt3dmath.Translation(nt3dmath.Vec3{Z: .2})))
	if posed := frame(t, s); posed.Geometry[0].Material == nil || posed.Geometry[0].Material.Toon != style {
		t.Fatal("deformation lost toon")
	}
}
