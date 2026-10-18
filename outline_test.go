// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"bytes"
	"image"
	"image/color"
	"math"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"testing"
)

var outlineInk = color.RGBA{200, 30, 90, 255}

func outlineFrame(t *testing.T) Frame {
	t.Helper()
	g, err := BuildSphere(Sphere{Radius: .7, Segments: 40, Rings: 24, Color: white, Material: &Material{Unlit: true}})
	check(t, err)
	g.Outline = OutlineStyle{Width: 3, Color: outlineInk}
	s := NewScene()
	_, err = s.SetGeometry("ball", g)
	check(t, err)
	f, err := s.Frame(DefaultCamera(), 96, 96)
	check(t, err)
	f.Background = color.RGBA{40, 60, 80, 255}
	return f
}

func TestOutlinePresenceAndDepth(t *testing.T) {
	f := outlineFrame(t)
	outlined, depth := softwareRenderDepth(f)
	if n := coloredPixels(outlined, outlineInk); n < 150 {
		t.Fatalf("only %d outline pixels", n)
	}
	if at(outlined, 48, 48) != white {
		t.Fatalf("hull covered the surface: %v", at(outlined, 48, 48))
	}
	f.Geometry[0].Outline.Width = 0
	_, plainDepth := softwareRenderDepth(f)
	if coloredPixels(softwareRender(f), outlineInk) != 0 {
		t.Fatal("disabled outline left pixels")
	}
	for i, d := range plainDepth {
		if d != noDepth && depth[i] != d {
			t.Fatal("hull changed interior depth")
		}
	}
	// An opaque screen-aligned pane in front of the entire object hides both
	// its surface and its expanded contour, in either geometry order.
	f.Geometry[0].Outline.Width = 3
	pane := quad(.95, .001, color.RGBA{20, 180, 40, 255}, &Material{Unlit: true}, false)
	inv, ok := f.Matrix.InverseAffine()
	if !ok {
		t.Fatal("inverse")
	}
	for i := range pane.Vertices {
		pane.Vertices[i].Position = inv.Point(pane.Vertices[i].Position)
	}
	pane.Bounds = GeometryBounds(pane)
	for _, reverse := range []bool{false, true} {
		occluded := f
		occluded.Instances = nil
		occluded.Geometry = []Geometry{f.Geometry[0], pane}
		if reverse {
			occluded.Geometry[0], occluded.Geometry[1] = occluded.Geometry[1], occluded.Geometry[0]
		}
		if n := coloredPixels(softwareRender(occluded), outlineInk); n != 0 {
			t.Fatalf("%d outline pixels leaked over nearer pane", n)
		}
	}
}

func TestOutlineAlphaAndSupersampling(t *testing.T) {
	for _, ss := range []int{1, 2, 3} {
		f := outlineFrame(t)
		f.Supersample = ss
		img := softwareRender(f)
		if n := coloredPixels(img, outlineInk); n < 100 {
			t.Fatalf("supersample %d lost contour: %d", ss, n)
		}
		f.Geometry[0].Outline.Width = 0
		plain := softwareRender(f)
		// The silhouette grows about three output pixels, independent of SS.
		bounds := func(img image.Image) image.Rectangle {
			b := image.Rectangle{}
			for y := 0; y < 96; y++ {
				for x := 0; x < 96; x++ {
					if at(img, x, y) != f.Background {
						b = b.Union(image.Rect(x, y, x+1, y+1))
					}
				}
			}
			return b
		}
		a, b := bounds(img), bounds(plain)
		if d := a.Dx() - b.Dx(); d < 4 || d > 8 {
			t.Fatalf("supersample %d width delta %d", ss, d)
		}
	}
	for _, transparent := range []bool{false, true} {
		f := outlineFrame(t)
		g := &f.Geometry[0]
		g.Material = &Material{Unlit: true, Texture: testTexture(t, color.RGBA{}), Cutout: !transparent, Transparent: transparent}
		outlined := softwareRender(f).(*image.RGBA)
		g.Outline.Width = 0
		if !bytes.Equal(outlined.Pix, softwareRender(f).(*image.RGBA).Pix) {
			t.Fatalf("invisible alpha generated outline, transparent=%v", transparent)
		}
	}
}

func TestOutlineValidationAndCulling(t *testing.T) {
	for _, w := range []float32{-1, 33, float32(math.NaN()), float32(math.Inf(1))} {
		if validateGeometry(Geometry{Outline: OutlineStyle{Width: w}}) == nil {
			t.Fatalf("accepted %v", w)
		}
	}
	g := quad(.02, .5, white, nil, false)
	g.Outline.Width = 4
	f := flatFrame(100, 100, g)
	f.Cull = true
	f.Instances = []Instance{{Visible: true, World: nt3dmath.Translation(nt3dmath.Vec3{X: 1.05})}}
	if !f.drawn(1)[0] {
		t.Fatal("culled hull reaching into viewport")
	}
	f.Geometry[0].Outline.Width = 0
	if f.drawn(1)[0] {
		t.Fatal("test mesh should be outside without hull")
	}
}

func TestMergeOutlineStyle(t *testing.T) {
	g := outlineFrame(t).Geometry[0]
	merged, err := MergeGeometry(g, g)
	check(t, err)
	if merged.Outline != g.Outline {
		t.Fatal("merge lost outline")
	}
	other := g
	other.Outline.Width++
	if _, err := MergeGeometry(g, other); err == nil {
		t.Fatal("merged conflicting outline styles")
	}
}

func TestSetOutlinePreservesPoseAndSnapshots(t *testing.T) {
	s, id, joint := animationSkinScene(t)
	old := frame(t, s)
	rev := s.nodes[id].geometryRevision
	style := OutlineStyle{Width: 2, Color: outlineInk}
	check(t, s.SetOutline(Root, style))
	if s.nodes[id].deformation == nil || s.nodes[id].geometryRevision != rev {
		t.Fatal("outline replaced mesh/deformation")
	}
	if old.Geometry[0].Outline.Width != 0 {
		t.Fatal("outline changed old snapshot")
	}
	check(t, s.SetTransform(joint, nt3dmath.Translation(nt3dmath.Vec3{Z: .2})))
	next := frame(t, s)
	if next.Geometry[0].Outline != style {
		t.Fatal("deformation lost outline")
	}
	before := s.Revision()
	check(t, s.SetOutline(Root, style))
	if s.Revision() != before {
		t.Fatal("identical outline dirtied scene")
	}
	if s.SetOutline(NodeID(999999), style) == nil {
		t.Fatal("accepted missing node")
	}
	check(t, s.SetOutline(id, OutlineStyle{}))
	if frame(t, s).Geometry[0].Outline.Width != 0 {
		t.Fatal("disable ignored")
	}
}
