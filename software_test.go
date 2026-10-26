// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

func depthAt(r SoftwareResult, x, y int) float32 { return r.Depth[y*r.Width+x] }

// The picture is the software renderer's, and depth is the depth it tests.
func TestRenderSoftware(t *testing.T) {
	red := color.RGBA{220, 0, 0, 255}
	f := blendScene(t, nil, quad(.5, .3, red, nil, false))
	for _, s := range []int{1, 2} {
		f.Supersample = s
		f.Overlays = []OverlayRect{{X: 0, Y: 0, Width: 6, Height: 6, Color: color.RGBA{0, 0, 0, 255}}}
		r, err := RenderSoftware(f)
		check(t, err)
		want, err := NewSoftwareRenderer().Render(f)
		check(t, err)
		if r.Width != f.Width || r.Height != f.Height || len(r.Depth) != f.Width*f.Height || !bytes.Equal(r.Image.Pix, want.(*image.RGBA).Pix) {
			t.Fatalf("supersample %d: result %d×%d with %d depths, or pictures differ", s, r.Width, r.Height, len(r.Depth))
		}
		// The pane is nearer than the floor, at the depth its point projects
		// to; overlays write no depth.
		_, _, z, w := f.Matrix.Transform(nt3dmath.Vec3{Z: .3})
		if d := depthAt(r, 40, 40); math.Abs(float64(d-z/w)) > 1e-3 {
			t.Fatalf("supersample %d: pane depth %v, want %v", s, d, z/w)
		}
		if floor := depthAt(r, 40, 5); floor <= depthAt(r, 40, 40) || math.IsInf(float64(floor), 0) {
			t.Fatalf("supersample %d: floor depth %v", s, floor)
		}
		if d := depthAt(r, 2, 2); math.IsInf(float64(d), 0) {
			t.Fatalf("supersample %d: the overlay hid the floor's depth", s)
		}
	}
	// Where nothing is drawn, depth is +Inf.
	bare := blendScene(t, nil)
	bare.Geometry, bare.Instances, bare.GeometryRevisions, bare.Shared = nil, nil, nil, nil
	r, err := RenderSoftware(bare)
	check(t, err)
	for _, d := range r.Depth {
		if !math.IsInf(float64(d), 1) {
			t.Fatalf("empty frame depth %v", d)
		}
	}
	if _, err := RenderSoftware(Frame{}); err == nil {
		t.Fatal("invalid frame rendered")
	}
}

// Transparent surfaces hide nothing, so they leave depth to what is behind.
func TestRenderSoftwareTransparentDepth(t *testing.T) {
	pane := blendScene(t, nil, quad(.5, .3, color.RGBA{220, 0, 0, 128}, glass, false))
	floor := blendScene(t, nil)
	a, err := RenderSoftware(pane)
	check(t, err)
	b, err := RenderSoftware(floor)
	check(t, err)
	if depthAt(a, 40, 40) != depthAt(b, 40, 40) {
		t.Fatalf("glass wrote depth: %v, floor %v", depthAt(a, 40, 40), depthAt(b, 40, 40))
	}
}

// Alpha Gaussians blend in one order across geometries, far to near, whatever
// the frame's order, and a transparent mesh between them takes its place.
func TestSoftwareGlobalBlendOrder(t *testing.T) {
	red, green, blue := color.RGBA{255, 0, 0, 128}, color.RGBA{0, 255, 0, 128}, color.RGBA{0, 0, 255, 128}
	// One splat, far wider than the frame, so its center pixel has all its alpha.
	splat := func(z float32, c color.RGBA) Geometry {
		return styled(PointStyle{Shape: GaussianPoint, Units: WorldUnits, Blend: AlphaBlend},
			Point{Position: nt3dmath.Vec3{Z: z}, Radius: 1, Color: c, GaussianAxes: [3]nt3dmath.Vec3{{X: 20}, {Y: 20}, {Z: .03}}})
	}
	// One geometry's points straddle another's: only a sort across geometries gets it right.
	straddle := splat(-1, red)
	straddle.Points = append(straddle.Points, splat(-3, blue).Points...)
	pane := quad(3, -2.5, color.RGBA{255, 255, 255, 128}, glass, false)
	render := func(geometry ...Geometry) color.RGBA {
		f := depthFrame(24, 24, geometry...)
		f.Background = color.RGBA{255, 255, 255, 255}
		return at(softwareRender(f), 12, 12)
	}
	over := func(layers ...color.RGBA) color.RGBA { // Farthest first.
		out := white
		for _, c := range layers {
			out = mixRGB(c, float32(c.A)/255, out)
		}
		return out
	}
	want := over(blue, green, red)
	for name, got := range map[string]color.RGBA{
		"nearest first":  render(splat(-1, red), splat(-2, green), splat(-3, blue)),
		"farthest first": render(splat(-3, blue), splat(-2, green), splat(-1, red)),
		"mixed":          render(splat(-2, green), splat(-3, blue), splat(-1, red)),
		"straddled":      render(straddle, splat(-2, green)),
		"straddled last": render(splat(-2, green), straddle),
	} {
		if !closeRGB(got, want, 4) {
			t.Errorf("%s: %v, want %v", name, got, want)
		}
	}
	// A pane between blue and green blends there, whichever geometry comes first.
	want = over(blue, color.RGBA{255, 255, 255, 128}, green, red)
	for name, got := range map[string]color.RGBA{
		"pane first": render(pane, splat(-1, red), splat(-2, green), splat(-3, blue)),
		"pane last":  render(splat(-1, red), splat(-2, green), splat(-3, blue), pane),
	} {
		if !closeRGB(got, want, 4) {
			t.Errorf("%s: %v, want %v", name, got, want)
		}
	}
}

// coveredPixels counts the pixels that differ from the background by more than a little.
func coveredPixels(img *image.RGBA, background color.RGBA) int {
	n := 0
	for i := 0; i < len(img.Pix); i += 4 {
		d := 0
		for k, b := range [3]uint8{background.R, background.G, background.B} {
			d = max(d, int(math.Abs(float64(img.Pix[i+k])-float64(b))))
		}
		if d > 8 {
			n++
		}
	}
	return n
}

// A far clustered cloud draws fewer, larger splats than the whole cloud, but
// covers about the same pixels.
func TestSoftwareLODThinsFarCloud(t *testing.T) {
	clustered := lodCloud(t, 4096, 64)
	plain := clustered
	plain.Clusters = nil
	bg := color.RGBA{10, 20, 30, 255}
	render := func(g Geometry) *image.RGBA {
		f := lodPerspective(100, g)
		f.Background, f.Complete = bg, true
		r, err := RenderSoftware(f)
		check(t, err)
		return r.Image
	}
	far := lodPerspective(100, clustered)
	if drawn, resident := far.SplatSelection(); drawn >= resident {
		t.Fatalf("far selection drew %d of %d splats; nothing thinned", drawn, resident)
	}
	thin, full := render(clustered), render(plain)
	if bytes.Equal(thin.Pix, full.Pix) {
		t.Fatal("the far clustered frame matches the unclustered one: nothing thinned")
	}
	a, b := coveredPixels(thin, bg), coveredPixels(full, bg)
	if b == 0 || math.Abs(float64(a-b)) > .15*float64(b) {
		t.Fatalf("covered pixels: %d clustered, %d unclustered", a, b)
	}
}

// An unlimited budget draws every splat as an unclustered cloud does.
func TestSoftwareUnlimitedBudgetMatchesUnclustered(t *testing.T) {
	clustered := lodCloud(t, 4096, 64)
	plain := clustered
	plain.Clusters = nil
	render := func(g Geometry) []byte {
		f := lodPerspective(100, g)
		f.Complete, f.SplatBudget = true, -1
		r, err := RenderSoftware(f)
		check(t, err)
		return r.Image.Pix
	}
	if !bytes.Equal(render(clustered), render(plain)) {
		t.Fatal("an unlimited budget changes the picture")
	}
}
