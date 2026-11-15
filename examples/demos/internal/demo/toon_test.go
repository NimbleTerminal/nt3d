// Copyright (c) 2026 Neomantra Corp.

package demo

import (
	"image/color"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/examples/demos/internal/demo/models"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
	nt3dwidget "nimbleterminal.dev/nt3d/widget"
)

func toonDemo(t *testing.T) *demo {
	t.Helper()
	old := SampleFiles
	SampleFiles = models.Toon
	t.Cleanup(func() { SampleFiles = old })
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupToon(); err != nil {
		t.Fatal(err)
	}
	camera := nt3d.DefaultCamera()
	toonCamera(&camera)
	d.view = nt3dwidget.New(d.scene, 80, 24, nt3dwidget.WithCamera(camera))
	t.Cleanup(func() { _ = d.view.Close() })
	return d
}

// under is the set of nodes in root's subtree.
func under(t *testing.T, s *nt3d.Scene, root nt3d.NodeID) map[nt3d.NodeID]bool {
	t.Helper()
	set := map[nt3d.NodeID]bool{}
	pending := []nt3d.NodeID{root}
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		set[id] = true
		n, ok := s.Node(id)
		if !ok {
			t.Fatalf("no node %d", id)
		}
		pending = append(pending, n.Children...)
	}
	return set
}

func TestToonDemo(t *testing.T) {
	d := toonDemo(t)
	if d.toon == nil || d.terrainScene() {
		t.Fatal("the toon scene still answers as terrain")
	}
	buildings, street, actors, props := under(t, d.scene, d.toon.buildings), under(t, d.scene, d.toon.street), under(t, d.scene, d.toon.actors), under(t, d.scene, d.toon.props)
	frame := func() nt3d.Frame {
		t.Helper()
		f, err := d.scene.Frame(d.view.Camera(), 160, 90)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	// look checks every mesh's band count, shadow softness, and the outline of the inked ones.
	look := func(bands int, width, shadowSoft float32) {
		t.Helper()
		f := frame()
		for i, g := range f.Geometry {
			if len(g.Indices) == 0 {
				continue
			}
			node := f.Instances[i].Node
			if g.Material == nil || g.Material.Toon.Bands != bands || g.Material.Toon.ShadowSoftness != shadowSoft {
				t.Fatalf("node %d: material %+v, want %d bands and shadow softness %v", node, g.Material, bands, shadowSoft)
			}
			want := nt3d.OutlineStyle{}
			if width > 0 && (buildings[node] || actors[node] || props[node]) {
				want = nt3d.OutlineStyle{Width: width, Color: toonInk, Crease: 30}
			}
			if g.Outline != want {
				t.Fatalf("node %d: outline %+v, want %+v", node, g.Outline, want)
			}
		}
	}

	f := frame()
	meshes, textured, transmissive, transparentProps := 0, 0, 0, 0
	colors := map[color.RGBA]bool{}
	low, high := float32(1e9), float32(-1e9)
	for i, g := range f.Geometry {
		node, world := f.Instances[i].Node, f.Instances[i].World
		if len(g.Indices) > 0 {
			meshes++
		}
		if len(g.Lines) > 0 {
			t.Fatalf("node %d still draws %d line vertices", node, len(g.Lines))
		}
		switch {
		case buildings[node]:
			for _, v := range g.Vertices {
				colors[v.Color] = true
			}
		case actors[node]:
			if g.Material != nil && g.Material.Texture != nil {
				textured++
			}
			for _, v := range g.Vertices {
				z := world.Point(v.Position).Z
				low, high = min(low, z), max(high, z)
			}
		case props[node]:
			if g.Material != nil && g.Material.Transmission > 0 {
				transmissive++
			}
			if g.Material != nil && g.Material.Transparent {
				transparentProps++
			}
		case street[node]:
			z := world.Point(g.Vertices[0].Position).Z
			if len(g.Indices) == 6 && (z < -.001 || z > .001) {
				t.Fatalf("the ground is at z %v", z)
			}
			if len(g.Indices) != 6 && len(g.Indices) != 1001*6 {
				t.Fatalf("a street mesh has %d indices", len(g.Indices))
			}
		}
	}
	if meshes < 149 || len(buildings) != 143 || len(colors) != 5 || textured < 1 || transmissive != 1 || transparentProps != 1 {
		t.Fatalf("%d meshes, %d building nodes, %d building colors, %d textured actors, %d transmissive, %d transparent props", meshes, len(buildings)-1, len(colors), textured, transmissive, transparentProps)
	}
	if high-low < 13.9 || high-low > 14.1 || low < .03 || low > .07 {
		t.Fatalf("actors stand from z %v to %v", low, high)
	}
	look(3, 2, toonShadowSoftness)

	press := func(r rune) {
		t.Helper()
		d.Update(tea.KeyPressMsg{Code: r})
		if d.view.Err() != nil {
			t.Fatal(d.view.Err())
		}
	}
	press('t')
	look(0, 2, 0)
	press('t')
	look(3, 2, toonShadowSoftness)
	for _, bands := range []int{4, 2, 3} {
		press('n')
		look(bands, 2, toonShadowSoftness)
	}
	press('b')
	look(3, 0, toonShadowSoftness)
	press('b')
	look(3, 2, toonShadowSoftness)
	press('s')
	look(3, 2, 0)
	if view := d.toonView("VIEW"); !strings.Contains(view, "crisp shadows") {
		t.Fatalf("view after shadow toggle: %q", view)
	}
	press('s')
	look(3, 2, toonShadowSoftness)
	press('a')
	d.Update(phaseMsg(time.Now().Add(200 * time.Millisecond)))
	look(3, 2, toonShadowSoftness)
	press('a')

	_, keys := d.sceneKeys()
	help := map[string]bool{}
	for _, k := range keys.scene {
		help[k.Help().Desc] = true
	}
	if !help["toon"] || !help["bands"] || !help["outlines"] || !help["shadows"] || !help["breeze"] {
		t.Fatalf("scene keys %v", help)
	}
	if view := d.toonView("VIEW"); !strings.Contains(view, "© OpenStreetMap contributors") || !strings.Contains(view, "3 bands") || !strings.Contains(view, "soft shadows") {
		t.Fatalf("view: %q", view)
	}

	f = frame()
	d.decorate(&f)
	if len(f.Lighting.Lights) != 1 || !f.Lighting.Lights[0].Shadows {
		t.Fatalf("lighting %+v", f.Lighting)
	}
	r := nt3d.NewSoftwareRenderer()
	defer r.Close()
	img, err := r.Render(f)
	if err != nil {
		t.Fatal(err)
	}
	drawn := 0
	for y := range 90 {
		for x := range 160 {
			if c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA); c != f.Background {
				drawn++
			}
		}
	}
	if drawn < 1000 {
		t.Fatalf("only %d pixels drawn", drawn)
	}
}

func TestToonDemoInGallery(t *testing.T) {
	names := Names()
	if len(names) != 19 || names[18] != "toon" {
		t.Fatalf("names %v", names)
	}
	r, err := NewRunner("toon", models.Toon, nil, nt3dmesh.ZUp, false)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.Frame(r.Camera(), 160, 90); err != nil {
		t.Fatal(err)
	}
	if header, status := r.HeaderAndStatus(); !strings.Contains(header+status, "OpenStreetMap") {
		t.Fatalf("header %q, status %q", header, status)
	}
}
