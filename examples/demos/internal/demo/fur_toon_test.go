// Copyright (c) 2026 Neomantra Corp.

package demo

import (
	"bytes"
	"image"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/examples/demos/internal/demo/models"
	nt3dwidget "nimbleterminal.dev/nt3d/widget"
)

func TestFurToonToggle(t *testing.T) {
	old := SampleFiles
	SampleFiles = models.Spot
	t.Cleanup(func() { SampleFiles = old })
	d := &demo{scene: nt3d.NewScene()}
	var err error
	if d.group, err = d.scene.Add(nt3d.Root, "island"); err != nil {
		t.Fatal(err)
	}
	if err = d.setupFur(); err != nil {
		t.Fatal(err)
	}
	d.view = nt3dwidget.New(d.scene, 80, 24)
	t.Cleanup(func() { _ = d.view.Close() })

	// look checks every mesh's bands, and that only the skin is inked, by a
	// hull as wide as the fur is long.
	look := func(bands int) {
		t.Helper()
		f, err := d.scene.Frame(d.view.Camera(), 80, 60)
		if err != nil {
			t.Fatal(err)
		}
		skin := d.fur.nodes[0]
		meshes := 0
		for i := range f.Draws {
			g := f.Draws[i].Geometry
			if len(g.Indices) == 0 {
				continue
			}
			meshes++
			got := 0
			if g.Material != nil {
				got = g.Material.Toon.Bands
			}
			want := nt3d.OutlineStyle{}
			if bands > 0 && f.Draws[i].Node == skin {
				want = nt3d.OutlineStyle{Width: furLengths[d.fur.length], Units: nt3d.WorldUnits, Color: toonInk}
			}
			if got != bands || g.Outline != want {
				t.Fatalf("%s: %d bands, outline %+v; want %d, %+v", f.Draws[i].Name, got, g.Outline, bands, want)
			}
		}
		if meshes < 3 {
			t.Fatalf("only %d fur meshes", meshes)
		}
	}
	press := func(r rune) {
		t.Helper()
		d.Update(tea.KeyPressMsg{Code: r})
		if d.view.Err() != nil {
			t.Fatal(d.view.Err())
		}
	}
	render := func() []uint8 {
		t.Helper()
		f, err := d.scene.Frame(d.view.Camera(), 120, 90)
		if err != nil {
			t.Fatal(err)
		}
		r := nt3d.NewSoftwareRenderer()
		defer r.Close()
		img, err := r.Render(f)
		if err != nil {
			t.Fatal(err)
		}
		return img.(*image.RGBA).Pix
	}
	look(0)
	smooth := render()
	press('t')
	look(3)
	if bytes.Equal(smooth, render()) {
		t.Fatal("the software renderer draws toon fur as smooth fur")
	}
	// Regrowing the coat (another length, shell count, or no fins) keeps the look.
	for _, key := range "lsf" {
		press(key)
		look(3)
	}
	if view := d.furView("VIEW"); !strings.Contains(view, "t toon") || !strings.Contains(view, "· toon ·") {
		t.Fatalf("view: %q", view)
	}
	press('t')
	look(0)
	_, keys := d.sceneKeys()
	found := false
	for _, k := range keys.scene {
		found = found || k.Help().Desc == "toon"
	}
	if !found {
		t.Fatal("no toon key among the fur scene's")
	}

	// Verify built-in PostProcess effects and AA cycling in fur.
	press('e') // "studio"
	if d.view.PostProcess() != effects[1].post || !strings.Contains(d.furView("VIEW"), "· studio ·") {
		t.Fatalf("fur studio effect not applied: %+v / %q", d.view.PostProcess(), d.furView("VIEW"))
	}
	press('x') // 1x -> 4x MSAA
	if !d.view.MSAA() || !strings.Contains(d.furView("VIEW"), "4× MSAA") {
		t.Fatalf("fur MSAA not toggled: msaa=%v view=%q", d.view.MSAA(), d.furView("VIEW"))
	}
}
