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
	g, err := BuildHeightfield(Heightfield{NX: 8, NY: 8, MinX: -.5, MaxX: .5, MinY: -.5, MaxY: .5, Sampler: func(x, y float32) float32 { return x * y }, Color: color.RGBA{90, 160, 220, 255}})
	check(t, err)
	box := Box{Min: nt3dmath.Vec3{X: -.3, Y: -.3, Z: .2}, Size: nt3dmath.Vec3{X: .2, Y: .2, Z: .2}, Color: color.RGBA{255, 0, 0, 255}}
	m := nt3dmath.Translation(nt3dmath.Vec3{Z: .1}).Mul(nt3dmath.RotationZ(.4)).Mul(nt3dmath.Scaling(nt3dmath.Vec3{X: 2, Y: 1, Z: .5}))
	// A node transform must draw as the same geometry with the transform baked
	// in: positions, lighting through transformed normals, and rotated boxes,
	// which cannot be baked as boxes and are compared with an equal cuboid.
	placed := g
	placed.Boxes = []Box{box}
	cuboid, err := BuildCuboid(Cuboid{Size: box.Size, Color: box.Color})
	check(t, err)
	cuboid, err = TransformGeometry(cuboid, nt3dmath.Translation(box.Min.Add(box.Size.Scale(.5))))
	check(t, err)
	baked, err := MergeGeometry(g, cuboid)
	check(t, err)
	baked, err = TransformGeometry(baked, m)
	check(t, err)
	render := func(geometry Geometry, transform nt3dmath.Mat4) *image.RGBA {
		s := NewScene()
		id, err := s.SetGeometry("mesh", geometry)
		check(t, err)
		check(t, s.SetTransform(id, transform))
		f, err := s.Frame(DefaultCamera(), 200, 140)
		check(t, err)
		return softwareRender(f).(*image.RGBA)
	}
	a, b := render(placed, m), render(baked, nt3dmath.Identity())
	differ, drawn := 0, 0
	for i := 0; i < len(a.Pix); i += 4 {
		if a.Pix[i] != b.Pix[i] || a.Pix[i+1] != b.Pix[i+1] || a.Pix[i+2] != b.Pix[i+2] {
			differ++
		}
		if a.Pix[i] > 100 && a.Pix[i+1] < 40 && a.Pix[i+2] < 40 {
			drawn++
		}
	}
	// Float rounding may move a few edge pixels, or a shade by one.
	if differ > len(a.Pix)/4/200 || drawn < 50 {
		t.Fatalf("%d pixels differ; %d box pixels", differ, drawn)
	}
	s := NewScene()
	_, err = s.SetGeometry("mesh", placed)
	check(t, err)
	f := frame(t, s)
	f.Overlays = []OverlayRect{{X: 2, Y: 3, Width: 5, Height: 6, Color: color.RGBA{1, 2, 3, 255}}}
	img, _, err := Snapshot(f, Software, nil)
	check(t, err)
	if img.At(3, 4) != (color.RGBA{1, 2, 3, 255}) {
		t.Fatal("overlay")
	}
}

// Geometry is bound per node, so only each geometry is limited, not the frame.
func TestFrameHoldsSeveralMaximalMeshes(t *testing.T) {
	big := Geometry{Vertices: []Vertex{{}}, Indices: make([]uint32, MaxMeshIndices)}
	check(t, big.Validate())
	s := NewScene()
	for _, name := range []string{"a", "b"} {
		_, err := s.SetGeometry(name, big)
		check(t, err)
	}
	f, err := s.Frame(DefaultCamera(), 64, 48)
	check(t, err)
	check(t, validateFrame(f))
	big.Indices = append(big.Indices, 0, 0, 0)
	if big.Validate() == nil {
		t.Fatal("geometry over the binding limit accepted")
	}
}
