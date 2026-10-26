// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"fmt"
	"image/color"
	"math"
	"math/rand/v2"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
	nt3dsh "nimbleterminal.dev/nt3d/sh"
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
	for range 40 {
		press('y')
	}
	if got := budget(); got != 1<<30 {
		t.Fatalf("after many y, budget %d, want the 1<<30 cap", got)
	}
	for range 30 {
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

// splatCloud is a degree-3 splat cloud of n splats with a distinct color set
// each, small enough that a palette of n centroids is exact.
func splatCloud(n int) nt3d.Geometry {
	rng := rand.New(rand.NewPCG(5, 6))
	g := nt3d.Geometry{PointStyle: nt3d.PointStyle{Shape: nt3d.GaussianPoint, Units: nt3d.WorldUnits, Blend: nt3d.AlphaBlend}}
	for range n {
		g.Points = append(g.Points, nt3d.Point{Position: nt3dmath.Vec3{X: float32(rng.NormFloat64()), Y: float32(rng.NormFloat64()), Z: float32(rng.NormFloat64())}, Radius: .1, Color: color.RGBA{200, 200, 200, 255}, GaussianAxes: [3]nt3dmath.Vec3{{X: .05}, {Y: .05}, {Z: .05}}})
		for range 16 {
			g.GaussianSH = append(g.GaussianSH, nt3dsh.RGB{float32(rng.NormFloat64()) * .1, float32(rng.NormFloat64()) * .1, float32(rng.NormFloat64()) * .1})
		}
	}
	return g
}

// The palette status is the build's progress while it runs, then the
// centroids over all pieces and the compact SH bytes before and after:
// 3 a coefficient before, and after 3 for the DC, 2 for the label, and 45
// a centroid at degree 3.
func TestPaletteStatus(t *testing.T) {
	const n, centroids = 100000, 400
	piece := func() (nt3d.Geometry, nt3d.Geometry) {
		src := nt3d.Geometry{Points: make([]nt3d.Point, n), GaussianSH: make([]nt3dsh.RGB, 16*n)}
		built := nt3d.Geometry{Points: src.Points, GaussianSH: src.GaussianSH[:n],
			GaussianSHPalette: &nt3d.SHPalette{Degree: 3, Coefficients: make([]nt3dsh.RGB, 15*centroids)}}
		return src, built
	}
	srcA, builtA := piece()
	srcB, builtB := piece()
	d := &demo{meshes: &meshScene{splats: true, hasSH: true, sources: []nt3d.Geometry{srcA, {}, srcB}}}
	p := &d.meshes.palette
	if got := d.paletteStatus(); got != "" {
		t.Fatalf("idle status %q", got)
	}
	p.building = true
	p.fraction.Store(437)
	if got := d.paletteStatus(); got != "SH palette: building 43%" {
		t.Fatalf("building status %q", got)
	}
	p.building, p.active = false, true
	p.built = []nt3d.Geometry{builtA, {}, builtB}
	p.reports = []nt3dmesh.PaletteReport{{Centroids: centroids}, {Centroids: centroids}}
	before, after := p.shBytes(d.meshes.sources)
	if before != 2*48*n || after != 2*(5*n+45*centroids) {
		t.Fatalf("bytes %d -> %d, want %d -> %d", before, after, 2*48*n, 2*(5*n+45*centroids))
	}
	if got, want := d.paletteStatus(), "SH palette 800 centroids · SH 9.6 MB → 1.04 MB"; got != want {
		t.Fatalf("status %q, want %q", got, want)
	}
	for b, want := range map[int]string{48_000_000: "48 MB", 7_934_000: "7.93 MB", 24_000: "0.024 MB", 1_500_000_000: "1500 MB", 0: "0 MB"} {
		if got := megabytes(b); got != want {
			t.Fatalf("megabytes(%d) = %q, want %q", b, got, want)
		}
	}
	for n, want := range map[int]string{65536: "65,536", 999: "999", 1000: "1,000", 1234567: "1,234,567"} {
		if got := groupDigits(n); got != want {
			t.Fatalf("groupDigits(%d) = %q, want %q", n, got, want)
		}
	}
}

// The w key builds a palette in the background, swaps it in when done,
// swaps the originals back on a second press, and a cancel swaps nothing.
func TestPaletteKey(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	var sources []nt3d.Geometry
	d.meshes = &meshScene{splats: true, hasSH: true}
	for i, g := range []nt3d.Geometry{splatCloud(300), splatCloud(200)} {
		id, err := d.scene.SetGeometry(fmt.Sprintf("piece %d", i), g)
		if err != nil {
			t.Fatal(err)
		}
		d.meshes.nodes = append(d.meshes.nodes, id)
		sources = append(sources, g)
	}
	d.meshes.sources = sources
	d.view = nt3dwidget.New(d.scene, 80, 24, nt3dwidget.WithRenderMode(nt3d.Software), nt3dwidget.WithFrameDecorator(d.decorate))
	press := func() tea.Cmd {
		_, cmd := d.Update(tea.KeyPressMsg{Code: 'w', Text: "w"})
		return cmd
	}

	// A cancel: the build's context ends before it starts, and nothing swaps.
	cmd := press()
	if !d.meshes.palette.building {
		t.Fatal("w did not start a build")
	}
	d.meshes.palette.cancel()
	done := runPalette(cmd)
	if done == nil || done.err == nil {
		t.Fatalf("canceled build result %+v", done)
	}
	d.Update(*done)
	if d.meshes.palette.building || d.meshes.palette.active || !strings.Contains(d.status, "canceled") {
		t.Fatalf("after cancel: building %v active %v status %q", d.meshes.palette.building, d.meshes.palette.active, d.status)
	}

	// A build to the end.
	cmd = press()
	done = runPalette(cmd)
	if done == nil || done.err != nil || len(done.reports) != 2 {
		t.Fatalf("build result %+v", done)
	}
	d.Update(*done)
	p := &d.meshes.palette
	if !p.active || p.building || p.built[0].GaussianSHPalette == nil || p.built[1].GaussianSHPalette == nil {
		t.Fatalf("after build: active %v building %v", p.active, p.building)
	}
	if got, want := d.paletteStatus(), "SH palette 500 centroids · SH 0.024 MB → 0.025 MB"; got != want {
		t.Fatalf("status %q, want %q", got, want)
	}
	if sources[0].GaussianSHPalette != nil || len(sources[0].GaussianSH) != 16*300 {
		t.Fatal("the build wrote into the source")
	}
	if _, err := d.scene.Frame(nt3d.DefaultCamera(), 64, 48); err != nil {
		t.Fatal(err)
	}
	// Again: back to the originals.
	press()
	if p.active || p.built != nil {
		t.Fatal("second w did not swap back")
	}
	if got := d.paletteStatus(); got != "" {
		t.Fatalf("status after swap back %q", got)
	}
}

// runPalette runs a command, or those it batches, and returns the finished
// build's message if one comes back.
func runPalette(cmd tea.Cmd) *paletteDoneMsg {
	if cmd == nil {
		return nil
	}
	switch m := cmd().(type) {
	case paletteDoneMsg:
		return &m
	case tea.BatchMsg:
		for _, c := range m {
			if done := runPalette(c); done != nil {
				return done
			}
		}
	}
	return nil
}
