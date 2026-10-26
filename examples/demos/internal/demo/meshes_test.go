// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"math"
	"math/rand/v2"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
	"nimbleterminal.dev/nt3d/widget"
)

// A splat capture frames its subject, the dense middle of the cloud, not
// the shell of background splats around it; a mesh has no such framing.
func TestSplatFraming(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	style := nt3d.PointStyle{Shape: nt3d.GaussianPoint, Units: nt3d.WorldUnits, Blend: nt3d.AlphaBlend}
	var subject, shell nt3d.Geometry
	subject.PointStyle, shell.PointStyle = style, style
	for range 1000 {
		subject.Points = append(subject.Points, nt3d.Point{Position: nt3dmath.Vec3{X: 1 + .1*float32(rng.NormFloat64()), Y: 2 + .1*float32(rng.NormFloat64()), Z: 3 + .1*float32(rng.NormFloat64())}, Radius: 1})
	}
	for range 100 {
		shell.Points = append(shell.Points, nt3d.Point{Position: nt3dmath.Vec3{X: 50 * float32(rng.NormFloat64()), Y: 50 * float32(rng.NormFloat64()), Z: 50 * float32(rng.NormFloat64())}, Radius: 1})
	}
	m := &nt3dmesh.Model{Objects: []nt3dmesh.Object{{Geometry: subject}, {Geometry: shell}}}
	box, ok := splatFraming(m)
	if !ok {
		t.Fatal("a splat cloud has no framing")
	}
	center, size := box.Min.Add(box.Max).Scale(.5), box.Max.Sub(box.Min)
	if math.Abs(float64(center.X-1)) > .05 || math.Abs(float64(center.Y-2)) > .05 || math.Abs(float64(center.Z-3)) > .05 {
		t.Fatalf("center %v, want about (1, 2, 3)", center)
	}
	// Points scatter about 0.17 from the center on median; the box spans
	// about that in all.
	if size.X < .15 || size.X > .4 || math.Abs(float64(size.X-size.Y)) > 1e-5 || math.Abs(float64(size.Y-size.Z)) > 1e-5 {
		t.Fatalf("box size %v", size)
	}
	mesh := &nt3dmesh.Model{Objects: []nt3dmesh.Object{{Geometry: nt3d.Geometry{Vertices: make([]nt3d.Vertex, 3), Indices: []uint32{0, 1, 2}}}}}
	if _, ok := splatFraming(mesh); ok {
		t.Fatal("a mesh has a splat framing")
	}
}

// The splat budget keys halve, double, and reset the scene's budget, from
// the default, and never halve below the floor; elsewhere they do nothing.
func TestSplatBudgetKeys(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupGaussians(); err != nil {
		t.Fatal(err)
	}
	budget := func() int {
		t.Helper()
		f, err := d.scene.Frame(nt3d.DefaultCamera(), 64, 48)
		if err != nil {
			t.Fatal(err)
		}
		return f.SplatBudget
	}
	d.view = nt3dwidget.New(d.scene, 80, 24, nt3dwidget.WithRenderMode(nt3d.Software), nt3dwidget.WithFrameDecorator(d.decorate))
	press := func(r rune) { d.Update(tea.KeyPressMsg{Code: r, Text: string(r)}) }
	if got := budget(); got != 0 {
		t.Fatalf("new scene budget %d, want 0", got)
	}
	press('u')
	if got := budget(); got != nt3d.DefaultSplatBudget/2 {
		t.Fatalf("after u, budget %d, want %d", got, nt3d.DefaultSplatBudget/2)
	}
	press('y')
	if got := budget(); got != nt3d.DefaultSplatBudget {
		t.Fatalf("after y, budget %d, want %d", got, nt3d.DefaultSplatBudget)
	}
	press('y')
	if got := budget(); got != 2*nt3d.DefaultSplatBudget {
		t.Fatalf("after y y, budget %d, want %d", got, 2*nt3d.DefaultSplatBudget)
	}
	press('z')
	if got := budget(); got != 0 {
		t.Fatalf("after z, budget %d, want 0 (the default)", got)
	}
	for range 20 {
		press('u')
	}
	if got := budget(); got != 1000 {
		t.Fatalf("after many u, budget %d, want the 1000 floor", got)
	}
	f, err := d.scene.Frame(nt3d.DefaultCamera(), 64, 48)
	if err != nil {
		t.Fatal(err)
	}
	d.decorate(&f)
	if d.splats.resident != 576 || d.splats.drawn < 1 || d.splats.drawn > 576 {
		t.Fatalf("frame drew %d of %d, want a subset of 576", d.splats.drawn, d.splats.resident)
	}
	status := d.splatStatus()
	if !strings.Contains(status, "576 splats") || !strings.Contains(status, "budget 1k") {
		t.Fatalf("status %q", status)
	}

	// A scene without splats leaves the budget alone.
	other := &demo{scene: nt3d.NewScene(), meshes: &meshScene{}, view: d.view}
	other.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	if other.splats.budget != 0 {
		t.Fatalf("shapes scene budget %d, want 0", other.splats.budget)
	}
}

// The h hint and the SH mode show only for splats that have SH to toggle.
func TestSplatViewSHHint(t *testing.T) {
	for _, sh := range []bool{false, true} {
		d := &demo{scene: nt3d.NewScene(), meshes: &meshScene{splats: true, hasSH: sh}}
		d.view = nt3dwidget.New(d.scene, 20, 8, nt3dwidget.WithRenderMode(nt3d.Software))
		_, k := d.sceneKeys()
		hint := false
		for _, b := range k.scene {
			hint = hint || b.Help().Key == "h"
		}
		view := d.meshesView("")
		if hint != sh || strings.Contains(view, "h DC/full SH") != sh || strings.Contains(view, "full SH") != sh {
			t.Fatalf("hasSH %v: key hint %v, view %q", sh, hint, view)
		}
	}
}
