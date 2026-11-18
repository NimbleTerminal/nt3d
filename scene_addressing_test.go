// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image/color"
	"reflect"
	"strings"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// Nodes are addressed by NodeID. Only Find, which turns a name into an ID,
// and the three shortcuts that create a node when the name is new may take a
// name first; each shortcut returns the ID and has a SetNode form for a node
// the caller already holds. A new method that finds its node by name fails
// here.
func TestSceneAddressesNodesByID(t *testing.T) {
	byName := map[string]string{"Find": "", "SetGeometry": "SetNodeGeometry", "SetShared": "SetNodeShared", "SetLabel": "SetNodeLabel"}
	scene := reflect.TypeOf(&Scene{})
	str, id := reflect.TypeOf(""), reflect.TypeOf(NodeID(0))
	for i := range scene.NumMethod() {
		m := scene.Method(i)
		if m.Type.NumIn() < 2 || m.Type.In(1) != str { // In(0) is the receiver.
			continue
		}
		byID, allowed := byName[m.Name]
		if !allowed {
			t.Errorf("Scene.%s finds its node by name: take a NodeID, and let callers use Find", m.Name)
			continue
		}
		if m.Type.NumOut() == 0 || m.Type.Out(0) != id {
			t.Errorf("Scene.%s takes a name and so must return the NodeID it resolved", m.Name)
		}
		if byID == "" {
			continue
		}
		twin, ok := scene.MethodByName(byID)
		if !ok || twin.Type.NumIn() < 2 || twin.Type.In(1) != id {
			t.Errorf("Scene.%s needs a by-ID form, Scene.%s(id NodeID, …)", m.Name, byID)
		}
	}
	for name := range byName {
		if _, ok := scene.MethodByName(name); !ok {
			t.Errorf("Scene.%s is listed here but no longer exists", name)
		}
	}
}

// A method named Apply reaches a whole subtree, so it takes the subtree's
// root: a NodeID, first.
func TestSceneApplyMethodsTakeASubtreeRoot(t *testing.T) {
	scene := reflect.TypeOf(&Scene{})
	n := 0
	for i := range scene.NumMethod() {
		m := scene.Method(i)
		if !strings.HasPrefix(m.Name, "Apply") {
			continue
		}
		n++
		if m.Type.NumIn() < 2 || m.Type.In(1) != reflect.TypeOf(NodeID(0)) {
			t.Errorf("Scene.%s must take its subtree root, a NodeID, first", m.Name)
		}
	}
	if n == 0 {
		t.Fatal("no Apply methods found")
	}
}

// An Apply method changes what is under its root when it is called and
// remembers nothing: a mesh added afterwards keeps its own material.
func TestApplyMaterialDoesNotReachNodesAddedLater(t *testing.T) {
	s := NewScene()
	group, err := s.Add(Root, "group")
	check(t, err)
	mesh := func(name string) NodeID {
		g, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 1, Y: 1, Z: 1}, Color: color.RGBA{A: 255}})
		check(t, err)
		id, err := s.Add(group, name)
		check(t, err)
		check(t, s.SetNodeGeometry(id, g))
		return id
	}
	material := func(id NodeID) *Material {
		f, err := s.StillFrame(DefaultCamera(), 16, 16)
		check(t, err)
		for _, d := range f.Draws {
			if d.Node == id {
				return d.Geometry.Material
			}
		}
		t.Fatalf("node %d is not drawn", id)
		return nil
	}
	before := mesh("before")
	check(t, s.ApplyMaterial(group, &Material{Shading: ShadeUnlit}))
	after := mesh("after")
	if m := material(before); m == nil || !m.unlit() {
		t.Fatalf("the mesh under the root was not changed: %+v", m)
	}
	if m := material(after); m != nil && m.unlit() {
		t.Fatalf("a mesh added after the call was changed: %+v", m)
	}
}

// The per-node texture setters take a NodeID like every other per-node
// setter, and refuse one that names no node.
func TestNodeTextureSettersRejectUnknownNodes(t *testing.T) {
	s := NewScene()
	tex := testTexture(t, color.RGBA{R: 255, A: 255})
	const missing = NodeID(12345)
	for name, set := range map[string]func(NodeID, *Texture) error{
		"SetNodeTexture":     s.SetNodeTexture,
		"SetNodeNormalMap":   s.SetNodeNormalMap,
		"SetNodeORMMap":      s.SetNodeORMMap,
		"SetNodeEmissiveMap": s.SetNodeEmissiveMap,
	} {
		if err := set(missing, tex); err == nil {
			t.Errorf("%s accepted a node that does not exist", name)
		}
	}
}

// SetNodeLabel gives a label to a node the caller already holds, wherever it
// is in the tree, and keeps its place and transform.
func TestSetNodeLabelLabelsANodeByID(t *testing.T) {
	s := NewScene()
	group, err := s.Add(Root, "group")
	check(t, err)
	id, err := s.Add(group, "tag")
	check(t, err)
	move := nt3dmath.Translation(nt3dmath.Vec3{X: 2})
	check(t, s.SetTransform(id, move))
	check(t, s.SetNodeLabel(id, Label{Text: "north"}))
	n, _ := s.Node(id)
	if n.Parent != group || n.Local != move {
		t.Fatalf("the labeled node moved: parent %d, local %v", n.Parent, n.Local)
	}
	f, err := s.StillFrame(DefaultCamera(), 32, 32)
	check(t, err)
	if len(f.Draws) != 1 || f.Draws[0].Node != id || f.Draws[0].Geometry.Billboard == nil || f.Draws[0].Geometry.Billboard.Text != "north" {
		t.Fatalf("the node does not show the label: %+v", f.Draws)
	}
	if err := s.SetNodeLabel(NodeID(12345), Label{Text: "x"}); err == nil {
		t.Fatal("a node that does not exist was labeled")
	}
}
