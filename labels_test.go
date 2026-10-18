package nt3d

import (
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"nimbleterminal.dev/nt3d/math"
)

func TestLabelProjectionAndSharing(t *testing.T) {
	for _, projection := range []Projection{Orthographic, Perspective} {
		s := NewScene()
		g, err := (Label{Text: "shared", Pixels: true, Height: 20}).Geometry()
		check(t, err)
		shared, err := NewSharedGeometry(g)
		check(t, err)
		a, err := s.SetShared("a", shared)
		check(t, err)
		b, err := s.SetShared("b", shared)
		check(t, err)
		parent, err := s.Add(Root, "parent")
		check(t, err)
		check(t, s.Reparent(a, parent))
		check(t, s.SetTransform(parent, nt3dmath.Translation(nt3dmath.Vec3{Z: .5})))
		check(t, s.SetTransform(b, nt3dmath.Translation(nt3dmath.Vec3{Z: -.5})))
		for _, distance := range []float64{3.4, 7} {
			cam := Camera{Alpha: 25, Beta: 40, Distance: distance, Projection: projection}
			f, err := s.Frame(cam, 320, 200)
			check(t, err)
			if f.Shared[0] != f.Shared[1] || f.GeometryRevisions[0] != f.GeometryRevisions[1] {
				t.Fatal("label instances lost sharing")
			}
			for i := range f.Geometry {
				m := f.Matrix.Mul(f.Instances[i].World)
				_, y0, _, _ := m.Project(g.Vertices[0].Position, 320, 200)
				_, y1, _, _ := m.Project(g.Vertices[3].Position, 320, 200)
				if math.Abs(float64(y0-y1-20)) > .001 {
					t.Fatalf("pixel height: %v", y0-y1)
				}
			}
			if f.Instances[0].World.Point(nt3dmath.Vec3{}).Z != .5 {
				t.Fatal("lost parent transform")
			}
		}
	}
}

func TestLabelDepthPickingAndWireframe(t *testing.T) {
	cam := Camera{Distance: 4} // Looks along -X; screen right is +Y, up is +Z.
	s := NewScene()
	label, err := s.SetLabel("label", Label{Text: "HELLO\nworld", Height: 1, Background: color.RGBA{255, 0, 0, 255}, Padding: 2})
	check(t, err)
	f, err := s.Frame(cam, 160, 120)
	check(t, err)
	img, err := NewSoftwareRenderer().Render(f)
	check(t, err)
	if color.RGBAModel.Convert(img.At(80, 60)) == f.Background {
		t.Fatal("missing software label")
	}
	if hit := s.Pick(cam, f, 80, 60, 0); hit == nil || hit.Node != label {
		t.Fatalf("label pick: %+v", hit)
	}
	wire, err := RenderWireframe(f, 80, 30)
	check(t, err)
	if !strings.Contains(ansi.Strip(wire), "HELLO") || !strings.Contains(ansi.Strip(wire), "world") {
		t.Fatal("missing multiline fallback")
	}
	// An opaque wall nearer the camera hides the label in both renderers and picking.
	wall, err := s.SetGeometry("wall", Geometry{Boxes: []Box{{Min: nt3dmath.Vec3{X: 1, Y: -2, Z: -2}, Size: nt3dmath.Vec3{X: .1, Y: 4, Z: 4}, Color: color.RGBA{0, 255, 0, 255}}}})
	check(t, err)
	f, err = s.Frame(cam, 160, 120)
	check(t, err)
	wire, err = RenderWireframe(f, 80, 30)
	check(t, err)
	if strings.Contains(ansi.Strip(wire), "HELLO") {
		t.Fatal("label visible through wall")
	}
	if hit := s.Pick(cam, f, 80, 60, 0); hit == nil || hit.Node != wall {
		t.Fatalf("wall pick: %+v", hit)
	}
	img, err = NewSoftwareRenderer().Render(f)
	check(t, err)
	c := color.RGBAModel.Convert(img.At(80, 60)).(color.RGBA)
	if c.R > c.G {
		t.Fatal("label rendered through wall")
	}
	// Move wall behind label: the depth-writing label hides it.
	check(t, s.SetTransform(wall, nt3dmath.Translation(nt3dmath.Vec3{X: -2})))
	f, err = s.Frame(cam, 160, 120)
	check(t, err)
	if hit := s.Pick(cam, f, 80, 60, 0); hit == nil || hit.Node != label {
		t.Fatal("foreground label did not occlude")
	}
}

func TestLabelCullingAndSnapshot(t *testing.T) {
	s := NewScene()
	cam := Camera{Distance: 4, Projection: Perspective}
	id, err := s.SetLabel("label", Label{Text: "OLD", Pixels: true})
	check(t, err)
	old, err := s.Frame(cam, 160, 120)
	check(t, err)
	_, err = s.SetLabel("label", Label{Text: "NEW", Pixels: true})
	check(t, err)
	if old.Geometry[0].Billboard.Text != "OLD" {
		t.Fatal("snapshot mutated")
	}
	for _, position := range []nt3dmath.Vec3{{X: 4}, {X: 5}, {X: -500}, {Y: 500}} {
		check(t, s.SetTransform(id, nt3dmath.Translation(position)))
		f, err := s.Frame(cam, 160, 120)
		check(t, err)
		if f.drawn(1)[0] {
			t.Fatalf("not culled: %v", position)
		}
		wire, err := RenderWireframe(f, 80, 30)
		check(t, err)
		if strings.Contains(ansi.Strip(wire), "NEW") {
			t.Fatal("clipped label in wireframe")
		}
		if s.Pick(cam, f, 80, 60, 0) != nil {
			t.Fatal("clipped label picked")
		}
	}
	// Fixed orientation retains its parent's complete matrix.
	_, err = s.SetLabel("label", Label{Text: "fixed", Height: 1, Fixed: true, NoPick: true})
	check(t, err)
	m := nt3dmath.Translation(nt3dmath.Vec3{Z: .25}).Mul(nt3dmath.RotationZ(.3))
	check(t, s.SetTransform(id, m))
	f, err := s.Frame(cam, 160, 120)
	check(t, err)
	if f.Instances[0].World != m {
		t.Fatal("fixed label changed orientation")
	}
	if s.Pick(cam, f, 80, 60, 0) != nil {
		t.Fatal("NoPick ignored")
	}
}

func TestLabelValidation(t *testing.T) {
	for _, l := range []Label{{Height: -1}, {Opacity: 2}, {Padding: -1}, {Border: 9000}, {Text: strings.Repeat("x", 9000)}} {
		if _, err := l.Geometry(); err == nil {
			t.Fatalf("accepted invalid label %+v", l)
		}
	}
}

func TestLabelBoundsAndCutout(t *testing.T) {
	s := NewScene()
	id, err := s.SetLabel("label", Label{Text: "Pixel label", Pixels: true, Height: 40})
	check(t, err)
	anchor := nt3dmath.Vec3{Z: 1}
	check(t, s.SetTransform(id, nt3dmath.Translation(anchor)))
	if b := s.Bounds(); b.Min != anchor || b.Max != anchor {
		t.Fatalf("pixel bounds treated as world size: %+v", b)
	}
	// An empty padded cutout must neither paint nor capture clicks.
	_, err = s.SetLabel("label", Label{Text: " ", Padding: 8, Height: 1})
	check(t, err)
	check(t, s.SetTransform(id, nt3dmath.Identity()))
	cam := Camera{Distance: 4}
	f, err := s.Frame(cam, 160, 120)
	check(t, err)
	if s.Pick(cam, f, 80, 60, 0) != nil {
		t.Fatal("picked a transparent gap")
	}
	img, err := NewSoftwareRenderer().Render(f)
	check(t, err)
	if color.RGBAModel.Convert(img.At(80, 60)) != f.Background {
		t.Fatal("cutout gap painted")
	}
	// Metadata is copied along with geometry, preserving immutable snapshots.
	g, err := (Label{Text: "original"}).Geometry()
	check(t, err)
	_, err = s.SetGeometry("label", g)
	check(t, err)
	g.Billboard.Text = "mutated"
	f, err = s.Frame(cam, 160, 120)
	check(t, err)
	if f.Geometry[0].Billboard.Text != "original" {
		t.Fatal("billboard metadata aliases caller")
	}
}
