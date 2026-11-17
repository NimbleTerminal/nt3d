// Copyright (c) 2026 Neomantra Corp.

package demo

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image/color"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
	_ "nimbleterminal.dev/nt3d/mesh/gltf"
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

// The w key tells a cloud whose SH is already a palette from one with no
// bands to palette.
func TestPaletteKeyNothingToBuild(t *testing.T) {
	paletted := splatCloud(10)
	paletted.GaussianSH = paletted.GaussianSH[:0]
	for range paletted.Points {
		paletted.GaussianSH = append(paletted.GaussianSH, nt3dsh.RGB{})
	}
	paletted.GaussianSHPalette = &nt3d.SHPalette{Degree: 3, Coefficients: make([]nt3dsh.RGB, 15)}
	dc := splatCloud(10)
	dc.GaussianSH = dc.GaussianSH[:0]
	for range dc.Points {
		dc.GaussianSH = append(dc.GaussianSH, nt3dsh.RGB{})
	}
	for _, c := range []struct {
		name string
		g    nt3d.Geometry
		want string
	}{
		{"paletted", paletted, "already a palette"},
		{"dc only", dc, "no per-splat SH bands"},
	} {
		d := &demo{scene: nt3d.NewScene()}
		d.meshes = &meshScene{splats: true, hasSH: true, sources: []nt3d.Geometry{c.g}}
		d.Update(tea.KeyPressMsg{Code: 'w', Text: "w"})
		if !strings.Contains(d.status, c.want) || d.meshes.palette.building {
			t.Errorf("%s: status %q building %v, want %q", c.name, d.status, d.meshes.palette.building, c.want)
		}
	}
}

func TestMeshGlassOITCycle(t *testing.T) {
	d := &demo{scene: nt3d.NewScene(), meshes: &meshScene{}}
	group, err := d.scene.Add(nt3d.Root, "shapes")
	if err != nil {
		t.Fatal(err)
	}
	d.group = group
	if err := d.setupShapes(); err != nil {
		t.Fatal(err)
	}
	d.view = nt3dwidget.New(d.scene, 40, 16, nt3dwidget.WithRenderMode(nt3d.Software), nt3dwidget.WithFrameDecorator(d.decorate))
	defer d.view.Close()

	steps := []struct {
		wantLabel string
		wantOIT   bool
		wantGlass bool
	}{
		{"glass (OIT)", true, true},
		{"glass (sorted)", false, true},
		{"opaque", false, false},
	}
	for i, tc := range steps {
		if err := d.toggleGlass(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if view := d.meshesView(""); !strings.Contains(view, tc.wantLabel) {
			t.Fatalf("step %d: status missing %q: %s", i, tc.wantLabel, view)
		}
		f, err := d.scene.Frame(nt3d.DefaultCamera(), 48, 36)
		if err != nil {
			t.Fatal(err)
		}
		d.decorate(&f)
		if f.OIT != tc.wantOIT {
			t.Fatalf("step %d: f.OIT = %v, want %v", i, f.OIT, tc.wantOIT)
		}
		hasTrans := f.Draws[0].Geometry.Material != nil && f.Draws[0].Geometry.Material.Transparent
		if hasTrans != tc.wantGlass {
			t.Fatalf("step %d: Transparent = %v, want %v", i, hasTrans, tc.wantGlass)
		}
		if _, err := nt3d.NewSoftwareRenderer().Render(f); err != nil {
			t.Fatalf("step %d render: %v", i, err)
		}
	}
}

func TestIntersectingGlassShowcase(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	group, err := d.scene.Add(nt3d.Root, "shapes")
	if err != nil {
		t.Fatal(err)
	}
	d.group = group
	if err := d.setupMeshes(nil, nt3dmesh.ZUp); err != nil {
		t.Fatal(err)
	}
	d.view = nt3dwidget.New(d.scene, 48, 18, nt3dwidget.WithRenderMode(nt3d.Software), nt3dwidget.WithFrameDecorator(d.decorate))
	defer d.view.Close()

	// Press 'm' to switch from shape builders -> intersecting glass (opens directly in glass (OIT)).
	d.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if !d.meshes.intersecting || d.meshes.glass != 1 || d.meshes.title != "intersecting glass" {
		t.Fatalf("after m: intersecting=%v glass=%d title=%q", d.meshes.intersecting, d.meshes.glass, d.meshes.title)
	}
	if view := d.meshesView(""); !strings.Contains(view, "intersecting glass") || !strings.Contains(view, "m scene") || !strings.Contains(view, "glass (OIT)") {
		t.Fatalf("unexpected intersecting glass view: %s", view)
	}

	cam := nt3d.DefaultCamera()
	cam.Distance = 2.6
	fOIT, err := d.scene.Frame(cam, 96, 72)
	if err != nil {
		t.Fatal(err)
	}
	d.decorate(&fOIT)
	if !fOIT.OIT || len(fOIT.Draws) != 6 {
		t.Fatalf("expected OIT frame with 6 sculpture parts, got OIT=%v len=%d", fOIT.OIT, len(fOIT.Draws))
	}
	if fOIT.Draws[0].Geometry.Material != nil && fOIT.Draws[0].Geometry.Material.Transparent {
		t.Fatal("expected interior core to remain opaque")
	}
	for i := 1; i < len(fOIT.Draws); i++ {
		if fOIT.Draws[i].Geometry.Material == nil || !fOIT.Draws[i].Geometry.Material.Transparent {
			t.Fatalf("expected part %d to be transparent glass", i)
		}
	}
	sw := nt3d.NewSoftwareRenderer()
	imgOIT, err := sw.Render(fOIT)
	if err != nil {
		t.Fatalf("render intersecting glass (OIT): %v", err)
	}

	// Press 't' to switch to glass (sorted) and confirm OIT vs sorted produces distinct intersection compositing.
	d.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	fSorted, err := d.scene.Frame(cam, 96, 72)
	if err != nil {
		t.Fatal(err)
	}
	d.decorate(&fSorted)
	if fSorted.OIT {
		t.Fatal("expected fSorted.OIT == false in glass (sorted) mode")
	}
	imgSorted, err := sw.Render(fSorted)
	if err != nil {
		t.Fatalf("render intersecting glass (sorted): %v", err)
	}
	diff := 0
	for y := 0; y < 72; y++ {
		for x := 0; x < 96; x++ {
			if imgOIT.At(x, y) != imgSorted.At(x, y) {
				diff++
			}
		}
	}
	if diff < 50 {
		t.Fatalf("expected intersecting glass OIT vs sorted to differ visibly, got %d differing pixels", diff)
	}

	// Press 'm' again to return to classic shape builders.
	d.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if d.meshes.intersecting || d.meshes.title != "shape builders" || len(d.meshes.nodes) != 5 {
		t.Fatalf("expected return to 5 shape builders, got intersecting=%v title=%q len=%d", d.meshes.intersecting, d.meshes.title, len(d.meshes.nodes))
	}
}

func writeTestMultiLODVariantGLTF(t *testing.T) string {
	t.Helper()
	// 4 vertices forming a 2x2 quad in X/Z (becomes X/-Y facing camera in Z-up) + 2 indices sets:
	// - quad indices (6 uint16 = 2 tris) for LOD0
	// - single-tri indices (3 uint16 = 1 tri) for LOD1 & LOD2
	var buf []byte
	appendF32 := func(vals ...float32) {
		for _, v := range vals {
			var b [4]byte
			binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
			buf = append(buf, b[:]...)
		}
	}
	appendU16 := func(vals ...uint16) {
		for _, v := range vals {
			var b [2]byte
			binary.LittleEndian.PutUint16(b[:], v)
			buf = append(buf, b[:]...)
		}
	}
	// Positions (offset 0, 48 bytes): quad in X/Y with +Z normal (glTF +Z -> -Y in Z-up)
	appendF32(
		-1, -1, 0,
		1, -1, 0,
		1, 1, 0,
		-1, 1, 0,
	)
	// Normals (offset 48, 48 bytes)
	appendF32(
		0, 0, 1,
		0, 0, 1,
		0, 0, 1,
		0, 0, 1,
	)
	// Indices LOD0 (offset 96, 12 bytes): 2 tris
	appendU16(0, 1, 2, 0, 2, 3)
	// Indices LOD1/LOD2 (offset 108, 6 bytes + 2 pad): 1 tri
	appendU16(0, 1, 2, 0)

	uri := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(buf)
	gltfJSON := fmt.Sprintf(`{
  "asset": {"version": "2.0"},
  "extensionsUsed": ["KHR_materials_variants"],
  "extensions": {
    "KHR_materials_variants": {
      "variants": [{"name": "Taxi"}, {"name": "Police"}]
    }
  },
  "buffers": [{"uri": %q, "byteLength": %d}],
  "bufferViews": [
    {"buffer": 0, "byteOffset": 0, "byteLength": 48},
    {"buffer": 0, "byteOffset": 48, "byteLength": 48},
    {"buffer": 0, "byteOffset": 96, "byteLength": 12},
    {"buffer": 0, "byteOffset": 108, "byteLength": 6}
  ],
  "accessors": [
    {"bufferView": 0, "componentType": 5126, "count": 4, "type": "VEC3", "min": [-1, -1, 0], "max": [1, 1, 0]},
    {"bufferView": 1, "componentType": 5126, "count": 4, "type": "VEC3"},
    {"bufferView": 2, "componentType": 5123, "count": 6, "type": "SCALAR"},
    {"bufferView": 3, "componentType": 5123, "count": 3, "type": "SCALAR"}
  ],
  "materials": [
    {"name": "Default", "pbrMetallicRoughness": {"baseColorFactor": [0.8, 0.8, 0.8, 1]}},
    {"name": "Taxi", "pbrMetallicRoughness": {"baseColorFactor": [0.95, 0.75, 0.05, 1]}},
    {"name": "Police", "pbrMetallicRoughness": {"baseColorFactor": [0.1, 0.3, 0.95, 1]}}
  ],
  "meshes": [
    {"name": "Sign_LOD0", "primitives": [{"attributes": {"POSITION": 0, "NORMAL": 1}, "indices": 2, "material": 0, "extensions": {"KHR_materials_variants": {"mappings": [{"material": 1, "variants": [0]}, {"material": 2, "variants": [1]}]}}}]},
    {"name": "Sign_LOD1", "primitives": [{"attributes": {"POSITION": 0, "NORMAL": 1}, "indices": 3, "material": 0, "extensions": {"KHR_materials_variants": {"mappings": [{"material": 1, "variants": [0]}, {"material": 2, "variants": [1]}]}}}]},
    {"name": "Sign_LOD2", "primitives": [{"attributes": {"POSITION": 0, "NORMAL": 1}, "indices": 3, "material": 0, "extensions": {"KHR_materials_variants": {"mappings": [{"material": 1, "variants": [0]}, {"material": 2, "variants": [1]}]}}}]}
  ],
  "nodes": [
    {"name": "SM_Sign_LOD0", "mesh": 0, "translation": [0, 0, 0]},
    {"name": "SM_Sign_LOD1", "mesh": 1, "translation": [4, 0, 0]},
    {"name": "SM_Sign_LOD2", "mesh": 2, "translation": [8, 0, 0]}
  ],
  "scenes": [{"nodes": [0, 1, 2]}],
  "scene": 0
}`, uri, len(buf))

	p := filepath.Join(t.TempDir(), "sign_pack.gltf")
	if err := os.WriteFile(p, []byte(gltfJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMeshesGLTFVariantsAndMultiLOD(t *testing.T) {
	gltfPath := writeTestMultiLODVariantGLTF(t)

	d := &demo{scene: nt3d.NewScene()}
	group, err := d.scene.Add(nt3d.Root, "island")
	if err != nil {
		t.Fatal(err)
	}
	d.group = group
	if err := d.setupMeshes([]string{gltfPath}, nt3dmesh.ZUp); err != nil {
		t.Fatal(err)
	}
	d.view = nt3dwidget.New(d.scene, 60, 20, nt3dwidget.WithRenderMode(nt3d.Software), nt3dwidget.WithFrameDecorator(d.decorate))
	defer d.view.Close()

	if !d.meshes.hasLOD || !d.meshes.lodMode || d.meshes.maxLODTiers != 3 || d.meshes.triangles != 2 {
		t.Fatalf("expected auto LOD0 with 2 tris, got hasLOD=%v lodMode=%v tiers=%d tris=%d",
			d.meshes.hasLOD, d.meshes.lodMode, d.meshes.maxLODTiers, d.meshes.triangles)
	}

	cam := nt3d.Camera{Target: nt3dmath.Vec3{}, Distance: 3.0, Beta: -90, Near: 0.1, Far: 100, Projection: nt3d.Perspective}
	f0, err := d.scene.Frame(cam, 96, 72)
	if err != nil {
		t.Fatal(err)
	}
	d.decorate(&f0)
	view0 := d.meshesView("")
	if !strings.Contains(view0, "2 / 2 triangles") || !strings.Contains(view0, "tiers 1/0/0") || !strings.Contains(view0, "variant default (0/2)") {
		t.Fatalf("unexpected initial LOD0 view: %s", view0)
	}

	// Cycle colorway with 'v' -> Taxi (1/2) -> Police (2/2).
	d.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	if got := d.activeMeshVariantName(); got != "Taxi" {
		t.Fatalf("after v: active variant = %q, want Taxi", got)
	}
	fTaxi, err := d.scene.Frame(cam, 96, 72)
	if err != nil {
		t.Fatal(err)
	}
	sw := nt3d.NewSoftwareRenderer()
	imgTaxi, err := sw.Render(fTaxi)
	if err != nil {
		t.Fatal(err)
	}

	d.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	if got := d.activeMeshVariantName(); got != "Police" {
		t.Fatalf("after second v: active variant = %q, want Police", got)
	}
	fPolice, err := d.scene.Frame(cam, 96, 72)
	if err != nil {
		t.Fatal(err)
	}
	imgPolice, err := sw.Render(fPolice)
	if err != nil {
		t.Fatal(err)
	}
	pxTaxi := color.RGBAModel.Convert(imgTaxi.At(48, 36)).(color.RGBA)
	pxPolice := color.RGBAModel.Convert(imgPolice.At(48, 36)).(color.RGBA)
	if pxTaxi.R <= pxTaxi.B || pxPolice.B <= pxPolice.R {
		t.Fatalf("expected Taxi yellow vs Police blue at center pixel, got taxi=%v police=%v", pxTaxi, pxPolice)
	}

	// Zoom camera out to Distance = 6.5 (aabbDistance ~6.5 in (4.0, 7.4] -> LOD1: 1 tri, tiers 0/1/0!).
	camMid := cam
	camMid.Distance = 6.5
	f1, err := d.scene.Frame(camMid, 96, 72)
	if err != nil {
		t.Fatal(err)
	}
	d.decorate(&f1)
	if view1 := d.meshesView(""); !strings.Contains(view1, "1 / 2 triangles") || !strings.Contains(view1, "tiers 0/1/0") {
		t.Fatalf("unexpected LOD1 view at dist 6.5: %s", view1)
	}

	// Toggle 'l' to inspect raw uncollapsed hierarchy (2 + 1 + 1 = 4 triangles), preserving Police variant!
	d.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if d.meshes.lodMode || d.meshes.triangles != 4 {
		t.Fatalf("expected raw mode with 4 tris, got lodMode=%v tris=%d", d.meshes.lodMode, d.meshes.triangles)
	}
	if viewRaw := d.meshesView(""); !strings.Contains(viewRaw, "4 triangles · LOD raw") || !strings.Contains(viewRaw, "variant Police (2/2)") {
		t.Fatalf("unexpected raw LOD view: %s", viewRaw)
	}

	// Toggle 'b' outlines and 'l' back to LOD auto.
	d.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	d.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if !d.meshes.lodMode || !d.meshes.outlined || d.meshes.triangles != 2 {
		t.Fatalf("expected return to LOD auto with outlines, got lodMode=%v outlined=%v tris=%d",
			d.meshes.lodMode, d.meshes.outlined, d.meshes.triangles)
	}
}
