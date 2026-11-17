package nt3d

import (
	"image"
	"reflect"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func sameImage(a, b image.Image) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			if at(a, x, y) != at(b, x, y) {
				return false
			}
		}
	}
	return true
}

// ballFrame is a scene's frame of one lit ball.
func ballFrame(t *testing.T) Frame {
	t.Helper()
	g, err := BuildSphere(Sphere{Radius: .7, Segments: 24, Rings: 12, Color: white})
	check(t, err)
	s := NewScene()
	_, err = s.SetGeometry("ball", g)
	check(t, err)
	f, err := s.StillFrame(DefaultCamera(), 64, 64)
	check(t, err)
	return f
}

// Geometry is plain data: every field is the application's to read and set.
// What a scene knows about a geometry travels beside it, in the Draw.
func TestGeometryHasNoHiddenFields(t *testing.T) {
	typ := reflect.TypeOf(Geometry{})
	for i := range typ.NumField() {
		if f := typ.Field(i); !f.IsExported() {
			t.Errorf("Geometry.%s is unexported", f.Name)
		}
	}
}

// A draw needs only its geometry: it is drawn where it lies, visible.
func TestDrawOfGeometryAloneIsVisibleAtIdentity(t *testing.T) {
	f := ballFrame(t)
	want := softwareRender(f)

	bare := f
	bare.Draws = []Draw{{Geometry: f.Draws[0].Geometry}}
	if !sameImage(softwareRender(bare), want) {
		t.Fatal("a draw of the geometry alone renders differently from the scene's draw of it")
	}
	explicit := f
	explicit.Draws = []Draw{{Geometry: f.Draws[0].Geometry, World: nt3dmath.Identity()}}
	if !sameImage(softwareRender(explicit), want) {
		t.Fatal("the zero World is not the identity")
	}
	if at(want, 32, 32) == f.Background {
		t.Fatal("the ball is not drawn at the center; the test compares backgrounds")
	}
}

func TestHiddenDrawIsNeitherDrawnNorPicked(t *testing.T) {
	g, err := BuildSphere(Sphere{Radius: .7, Segments: 24, Rings: 12, Color: white})
	check(t, err)
	s := NewScene()
	_, err = s.SetGeometry("ball", g)
	check(t, err)
	camera := DefaultCamera()
	f, err := s.Frame(camera, 64, 64)
	check(t, err)
	if s.Pick(camera, f, 32, 32, 0) == nil {
		t.Fatal("the ball is not picked at the center")
	}
	f.Draws[0].Hidden = true
	if got := at(softwareRender(f), 32, 32); got != f.Background {
		t.Fatalf("a hidden draw left %v at the center, want the background", got)
	}
	if hit := s.Pick(camera, f, 32, 32, 0); hit != nil {
		t.Fatalf("a hidden draw was picked: %+v", hit)
	}
	if st := f.Stats(); st.Instances != 1 || st.VisibleInstances != 0 {
		t.Fatalf("stats count %d draws, %d visible; want 1 and 0", st.Instances, st.VisibleInstances)
	}
}

// A hidden node keeps its draw, so one assignment shows it in a frame.
func TestSceneFrameKeepsHiddenNodesAsHiddenDraws(t *testing.T) {
	s, ids := drawOrderScene(t, "a", "b")
	check(t, s.SetVisible(ids["b"], false))
	f, err := s.Frame(DefaultCamera(), 32, 32)
	check(t, err)
	if len(f.Draws) != 2 || f.Draws[0].Hidden || !f.Draws[1].Hidden {
		t.Fatalf("frame has %d draws; want a shown and b hidden", len(f.Draws))
	}
}
