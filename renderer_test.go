package nt3d

import (
	"errors"
	"github.com/charmbracelet/x/ansi"
	"image"
	"image/color"
	"nimbleterminal.dev/nt3d/math"
	"strings"
	"testing"
)

type failedRenderer struct{}

func (failedRenderer) Render(Frame) (image.Image, error) { return nil, errors.New("no GPU") }
func (failedRenderer) Close() error                      { return nil }
func TestSnapshotFallbackAndWireframe(t *testing.T) {
	s := NewScene()
	_, err := s.SetGeometry("point", pointGeometry())
	check(t, err)
	f := frame(t, s)
	img, mode, err := Snapshot(f, WebGPU, failedRenderer{})
	check(t, err)
	if mode != Software || img.Bounds() != image.Rect(0, 0, 200, 120) {
		t.Fatal("fallback")
	}
	if color.RGBAModel.Convert(img.At(100, 60)) == f.Background {
		t.Fatal("missing point")
	}
	f.CellText = []CellText{{X: 0, Y: 0, Text: "origin", Color: color.RGBA{255, 255, 255, 255}}}
	wire, err := RenderWireframe(f, 40, 15)
	check(t, err)
	if !strings.Contains(ansi.Strip(wire), "origin") {
		t.Fatal("wire labels")
	}
	if _, _, err = Snapshot(f, Wireframe, nil); err == nil {
		t.Fatal("wireframe image accepted")
	}
	f.Width = MaxFramePixels + 1
	if _, _, err = Snapshot(f, Software, nil); err == nil {
		t.Fatal("oversize accepted")
	}
}
func TestNormalsAndRotatedBoxes(t *testing.T) {
	s := NewScene()
	g, err := BuildHeightfield(Heightfield{NX: 2, NY: 2, MinX: -.5, MaxX: .5, MinY: -.5, MaxY: .5, Sampler: func(x, y float32) float32 { return x }})
	check(t, err)
	g.Boxes = []Box{{Min: nt3dmath.Vec3{X: -.3, Y: -.3, Z: .5}, Size: nt3dmath.Vec3{X: .2, Y: .2, Z: .2}, Color: color.RGBA{255, 0, 0, 255}}}
	id, err := s.SetGeometry("mesh", g)
	check(t, err)
	m := nt3dmath.RotationZ(.4).Mul(nt3dmath.Scaling(nt3dmath.Vec3{X: 2, Y: 1, Z: .5}))
	check(t, s.SetTransform(id, m))
	f := frame(t, s)
	gs := worldGeometry(f)
	if len(gs) != 2 || len(gs[1].Indices) != 36 {
		t.Fatal("box not triangulated")
	}
	n := gs[0].Vertices[0].Normal
	tangent := m.Direction(nt3dmath.Vec3{X: 1, Z: 1})
	if abs(n.Dot(tangent)) > 1e-5 {
		t.Fatal("normal transform", n)
	}
	f.Overlays = []OverlayRect{{X: 2, Y: 3, Width: 5, Height: 6, Color: color.RGBA{1, 2, 3, 255}}}
	img, _, err := Snapshot(f, Software, nil)
	check(t, err)
	if img.At(3, 4) != (color.RGBA{1, 2, 3, 255}) {
		t.Fatal("overlay")
	}
}
