// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"testing"
	"time"

	"nimbleterminal.dev/nt3d/math"
)

// Transforms are checked through the subtree they move: a scale that
// overflows only in a grandchild's world transform is refused, by
// SetTransform and Reparent alike, and the scene is left as it was.
func TestSubtreeTransformChecks(t *testing.T) {
	s := NewScene()
	parent, err := s.Add(Root, "parent")
	check(t, err)
	child, err := s.Add(parent, "child")
	check(t, err)
	leaf, err := s.Add(child, "leaf")
	check(t, err)
	huge := nt3dmath.Scaling(nt3dmath.Vec3{X: 1e20, Y: 1e20, Z: 1e20}) // Two of them overflow float32.
	check(t, s.SetTransform(leaf, huge))
	if s.SetTransform(parent, huge) == nil {
		t.Fatal("a world transform overflowing below was accepted")
	}
	if n, _ := s.Node(parent); n.Local != nt3dmath.Identity() {
		t.Fatal("a refused transform was kept")
	}
	if s.SetTransform(parent, nt3dmath.Scaling(nt3dmath.Vec3{X: -1, Y: 1, Z: 1})) == nil {
		t.Fatal("a reflection was accepted")
	}
	other, err := s.Add(Root, "other")
	check(t, err)
	check(t, s.SetTransform(other, huge))
	if s.Reparent(child, other) == nil {
		t.Fatal("a reparent overflowing below was accepted")
	}
	if n, _ := s.Node(child); n.Parent != parent {
		t.Fatal("a refused reparent moved the node")
	}
	// Elsewhere in the scene, transforms are free to change.
	check(t, s.SetTransform(parent, nt3dmath.Translation(nt3dmath.Vec3{X: 1})))
}

// Root's transform is checked once, not multiplied by itself as its own
// parent: a scale that is fine on any other node is fine on Root.
func TestRootTransformCheckedOnce(t *testing.T) {
	for _, k := range []float32{1e20, 1e-30} {
		s := NewScene()
		check(t, s.SetTransform(Root, nt3dmath.Scaling(nt3dmath.Vec3{X: k, Y: k, Z: k})))
	}
}

// Moving every node of a large scene takes time in proportion to it, as
// animating thousands of instances needs.
func TestManyTransformsAreFast(t *testing.T) {
	s := NewScene()
	group, err := s.Add(Root, "group")
	check(t, err)
	var ids []NodeID
	for i := range 5000 {
		id, err := s.Add(group, fmt.Sprint("node ", i))
		check(t, err)
		ids = append(ids, id)
	}
	start := time.Now()
	for i, id := range ids {
		check(t, s.SetTransform(id, nt3dmath.Translation(nt3dmath.Vec3{X: float32(i)})))
	}
	// Checking the whole scene each time took over 3 seconds here.
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("5,000 transforms took %v", elapsed)
	}
}
