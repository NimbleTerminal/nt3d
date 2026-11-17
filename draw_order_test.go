package nt3d

import (
	"image/color"
	"slices"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// drawOrderScene makes a scene whose named nodes each show one point, in the
// order given, and returns the scene and their IDs by name.
func drawOrderScene(t *testing.T, names ...string) (*Scene, map[string]NodeID) {
	t.Helper()
	s, ids := NewScene(), map[string]NodeID{}
	for i, name := range names {
		id, err := s.SetGeometry(name, Geometry{Points: []Point{{Radius: float32(i + 1), Color: color.RGBA{255, 255, 255, 255}}}})
		check(t, err)
		ids[name] = id
	}
	return s, ids
}

// drawnNames is the names of the nodes a frame of s draws, in the order it draws them.
func drawnNames(t *testing.T, s *Scene) []string {
	t.Helper()
	f, err := s.Frame(DefaultCamera(), 32, 32)
	check(t, err)
	names := make([]string, len(f.Instances))
	for i, in := range f.Instances {
		names[i] = in.Name
	}
	return names
}

func wantDrawn(t *testing.T, s *Scene, want ...string) {
	t.Helper()
	if got := drawnNames(t, s); !slices.Equal(got, want) {
		t.Fatalf("frame draws %v, want %v", got, want)
	}
}

func TestDrawOrderDefaultsToCreationOrder(t *testing.T) {
	s, _ := drawOrderScene(t, "a", "b", "c")
	wantDrawn(t, s, "a", "b", "c")
}

func TestDrawOrderSortsByLayerThenKeyThenCreation(t *testing.T) {
	s, ids := drawOrderScene(t, "a", "b", "c", "d", "e")
	check(t, s.SetNodeDrawOrder(ids["a"], DrawOrder{Layer: 1}))
	check(t, s.SetNodeDrawOrder(ids["c"], DrawOrder{Key: -5}))
	check(t, s.SetNodeDrawOrder(ids["d"], DrawOrder{Layer: 1, Key: -1}))
	// Layer 0: c (key -5), then b and e (key 0, as created). Layer 1: d (key -1), a.
	wantDrawn(t, s, "c", "b", "e", "d", "a")

	if got, err := s.NodeDrawOrder(ids["d"]); err != nil || got != (DrawOrder{Layer: 1, Key: -1}) {
		t.Fatalf("NodeDrawOrder = %v, %v", got, err)
	}
	check(t, s.SetNodeDrawOrder(ids["a"], DrawOrder{}))
	check(t, s.SetNodeDrawOrder(ids["c"], DrawOrder{}))
	check(t, s.SetNodeDrawOrder(ids["d"], DrawOrder{}))
	wantDrawn(t, s, "a", "b", "c", "d", "e")
}

func TestDrawOrderIsInheritedUntilOverridden(t *testing.T) {
	s := NewScene()
	point := Geometry{Points: []Point{{Radius: 1, Color: color.RGBA{255, 255, 255, 255}}}}
	late, err := s.Add(Root, "late")
	check(t, err)
	add := func(parent NodeID, name string) NodeID {
		id, err := s.Add(parent, name)
		check(t, err)
		check(t, s.SetNodeGeometry(id, point))
		return id
	}
	add(late, "late/1")
	inner, err := s.Add(late, "late/inner")
	check(t, err)
	add(inner, "late/inner/2")
	first := add(late, "late/first")
	add(Root, "plain")

	check(t, s.SetNodeDrawOrder(late, DrawOrder{Layer: 3}))
	wantDrawn(t, s, "plain", "late/1", "late/inner/2", "late/first")

	// A node's own order takes the place of its ancestors'.
	check(t, s.SetNodeDrawOrder(first, DrawOrder{Layer: -1}))
	wantDrawn(t, s, "late/first", "plain", "late/1", "late/inner/2")
}

func TestDrawOrderFollowsTheTree(t *testing.T) {
	s, ids := drawOrderScene(t, "a", "b", "c")
	back, err := s.Add(Root, "back")
	check(t, err)
	check(t, s.SetNodeDrawOrder(back, DrawOrder{Layer: 1}))
	wantDrawn(t, s, "a", "b", "c")

	check(t, s.Reparent(ids["a"], back))
	wantDrawn(t, s, "b", "c", "a")

	_, err = s.SetGeometry("d", Geometry{Points: []Point{{Radius: 1}}})
	check(t, err)
	wantDrawn(t, s, "b", "c", "d", "a")

	check(t, s.Reparent(ids["a"], Root))
	wantDrawn(t, s, "a", "b", "c", "d")

	check(t, s.Reparent(ids["b"], back))
	check(t, s.Remove(back))
	wantDrawn(t, s, "a", "c", "d")
}

// A frame's slices stay entry for entry in step however its nodes are ordered.
func TestDrawOrderKeepsFrameSlicesInStep(t *testing.T) {
	s, ids := drawOrderScene(t, "a", "b", "c")
	box, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 1, Y: 1, Z: 1}})
	check(t, err)
	shared, err := NewSharedGeometry(box)
	check(t, err)
	check(t, s.SetNodeShared(ids["b"], shared))
	before, err := s.Frame(DefaultCamera(), 32, 32)
	check(t, err)

	check(t, s.SetNodeDrawOrder(ids["a"], DrawOrder{Layer: 1}))
	after, err := s.Frame(DefaultCamera(), 32, 32)
	check(t, err)
	if n := len(after.Instances); n != 3 || len(after.Geometry) != n || len(after.GeometryRevisions) != n || len(after.Shared) != n {
		t.Fatalf("frame slices have %d, %d, %d, %d entries, want 3 each", len(after.Instances), len(after.Geometry), len(after.GeometryRevisions), len(after.Shared))
	}
	for i, in := range after.Instances {
		j := slices.IndexFunc(before.Instances, func(b Instance) bool { return b.Node == in.Node })
		if j < 0 {
			t.Fatalf("%s is not in the frame made before", in.Name)
		}
		if after.GeometryRevisions[i] != before.GeometryRevisions[j] || after.Shared[i] != before.Shared[j] ||
			len(after.Geometry[i].Points) != len(before.Geometry[j].Points) || len(after.Geometry[i].Vertices) != len(before.Geometry[j].Vertices) {
			t.Fatalf("%s moved from %d to %d without its geometry, revision, and sharing", in.Name, j, i)
		}
	}
	if after.Instances[2].Name != "a" || after.Shared[0] == 0 {
		t.Fatalf("frame draws %v with sharing %v, want a last and b, the shared one, first", drawnNames(t, s), after.Shared)
	}
}

func TestDrawOrderChangesTheSceneRevision(t *testing.T) {
	s, ids := drawOrderScene(t, "a")
	rev := s.Revision()
	check(t, s.SetNodeDrawOrder(ids["a"], DrawOrder{}))
	if s.Revision() != rev {
		t.Fatal("setting the order a node already has changed the scene's revision")
	}
	check(t, s.SetNodeDrawOrder(ids["a"], DrawOrder{Key: 1}))
	if s.Revision() == rev {
		t.Fatal("a new draw order left the scene's revision unchanged")
	}
}

func TestDrawOrderRefusesUnknownNodes(t *testing.T) {
	s := NewScene()
	if err := s.SetNodeDrawOrder(NodeID(1<<40), DrawOrder{Layer: 1}); err == nil {
		t.Fatal("SetNodeDrawOrder accepted a node the scene does not have")
	}
	if _, err := s.NodeDrawOrder(NodeID(1 << 40)); err == nil {
		t.Fatal("NodeDrawOrder accepted a node the scene does not have")
	}
}

// Ordering costs a frame nothing it would notice: the sorted sequence is kept
// until the tree or an order changes.
func TestDrawOrderAddsNoPerFrameAllocations(t *testing.T) {
	s := frameScene(t, 1000, false)
	plain := testing.AllocsPerRun(10, func() { _, _ = s.Frame(DefaultCamera(), 64, 64) })
	inner, _ := s.Find("inner")
	check(t, s.SetNodeDrawOrder(inner, DrawOrder{Layer: 1}))
	node, _ := s.Find("node500")
	check(t, s.SetNodeDrawOrder(node, DrawOrder{Layer: 2}))
	_, _ = s.Frame(DefaultCamera(), 64, 64)
	ordered := testing.AllocsPerRun(10, func() { _, _ = s.Frame(DefaultCamera(), 64, 64) })
	if ordered > plain {
		t.Fatalf("a frame of an ordered scene allocates %v times, an unordered one %v", ordered, plain)
	}
}

// coincident is a scene of a red and then a blue ball in the same place, so
// that the one drawn later is the one seen, and its frame.
func coincident(t *testing.T, redLayer int) Frame {
	t.Helper()
	s := NewScene()
	var red NodeID
	for _, ball := range []struct {
		name  string
		color color.RGBA
	}{{"red", color.RGBA{255, 0, 0, 255}}, {"blue", color.RGBA{0, 0, 255, 255}}} {
		g, err := BuildSphere(Sphere{Radius: .7, Segments: 24, Rings: 12, Color: ball.color, Material: &Material{Unlit: true}})
		check(t, err)
		id, err := s.SetGeometry(ball.name, g)
		check(t, err)
		if ball.name == "red" {
			red = id
		}
	}
	check(t, s.SetNodeDrawOrder(red, DrawOrder{Layer: redLayer}))
	f, err := s.StillFrame(DefaultCamera(), 64, 64)
	check(t, err)
	return f
}

// Draw order is part of the picture: of two surfaces at one depth, the later shows.
func TestDrawOrderDecidesCoincidentSurfaces(t *testing.T) {
	if got := at(softwareRender(coincident(t, 0)), 32, 32); got != (color.RGBA{0, 0, 255, 255}) {
		t.Fatalf("with no order the center is %v, want the blue ball added last", got)
	}
	if got := at(softwareRender(coincident(t, 1)), 32, 32); got != (color.RGBA{255, 0, 0, 255}) {
		t.Fatalf("with red in a later layer the center is %v, want red", got)
	}
}
