// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"errors"
	"fmt"
	"nimbleterminal.dev/nt3d/math"
	"slices"
	"sync/atomic"
)

// NodeID is stable for a node's lifetime. IDs are never reused. Root is zero.
type NodeID uint64

const Root NodeID = 0

var nextNode atomic.Uint64
var nextRevision atomic.Uint64
var errBounds = errors.New("nt3d: invalid bounds")

// Node is a detached description of a node, without mutable geometry storage.
type Node struct {
	ID, Parent   NodeID
	Name         string
	Local, World nt3dmath.Mat4
	Visible      bool // Local visibility; parents can still hide this node.
	Children     []NodeID
}
type sceneNode struct {
	Node
	geometry         *Geometry
	shared           *SharedGeometry // When geometry is shared's.
	geometryRevision uint64
	pickIndex        *bvhSlot // Built for picking on demand, for this geometry.
	noPickIndex      bool     // SetNodePickIndexing turned indexing off here.
}

// Scene owns a tree of groups and geometry. Use NewScene; do not copy it.
// Mutations and Frame must run on one goroutine. Produced frames can be rendered
// asynchronously while the scene is edited, provided readers do not mutate them.
type Scene struct {
	nodes                                    map[NodeID]*sceneNode
	names                                    map[string]NodeID
	order                                    []NodeID
	revision, textureRevision, stateRevision uint64
	noPickIndex                              bool
}

func NewScene() *Scene {
	s := &Scene{nodes: make(map[NodeID]*sceneNode), names: make(map[string]NodeID)}
	s.nodes[Root] = &sceneNode{Node: Node{ID: Root, Local: nt3dmath.Identity(), World: nt3dmath.Identity(), Visible: true}}
	s.changed(true, false)
	return s
}
func (s *Scene) changed(mesh, texture bool) {
	s.stateRevision = nextRevision.Add(1)
	if mesh {
		s.revision = s.stateRevision
	}
	if texture {
		s.textureRevision = s.stateRevision
	}
}

// Revision changes for every scene mutation, including transforms and visibility.
func (s *Scene) Revision() uint64                { return s.stateRevision }
func (s *Scene) Find(name string) (NodeID, bool) { id, ok := s.names[name]; return id, ok }
func (s *Scene) Node(id NodeID) (Node, bool) {
	n, ok := s.nodes[id]
	if !ok {
		return Node{}, false
	}
	out := n.Node
	out.World = s.world(id)
	out.Children = slices.Clone(n.Children)
	return out, true
}
func (s *Scene) world(id NodeID) nt3dmath.Mat4 {
	chain := []NodeID{id}
	for id != Root {
		id = s.nodes[id].Parent
		chain = append(chain, id)
	}
	m := nt3dmath.Identity()
	for i := len(chain) - 1; i >= 0; i-- {
		m = m.Mul(s.nodes[chain[i]].Local)
	}
	return m
}

// Add creates a visible group with an identity transform. Names are unique and nonempty.
func (s *Scene) Add(parent NodeID, name string) (NodeID, error) {
	p, ok := s.nodes[parent]
	if !ok {
		return 0, fmt.Errorf("nt3d: unknown parent %d", parent)
	}
	if name == "" {
		return 0, errors.New("nt3d: empty node name")
	}
	if _, ok := s.names[name]; ok {
		return 0, fmt.Errorf("nt3d: duplicate name %q", name)
	}
	id := NodeID(nextNode.Add(1))
	s.nodes[id] = &sceneNode{Node: Node{ID: id, Parent: parent, Name: name, Visible: true, Local: nt3dmath.Identity()}}
	p.Children = append(p.Children, id)
	s.names[name] = id
	s.order = append(s.order, id)
	s.changed(false, false)
	return id, nil
}

// Reparent preserves the local transform. Reparenting Root or creating a cycle fails atomically.
func (s *Scene) Reparent(id, parent NodeID) error {
	n, ok := s.nodes[id]
	if !ok || id == Root {
		return fmt.Errorf("nt3d: invalid node %d", id)
	}
	p, ok := s.nodes[parent]
	if !ok {
		return fmt.Errorf("nt3d: unknown parent %d", parent)
	}
	for at := parent; ; at = s.nodes[at].Parent {
		if at == id {
			return errors.New("nt3d: scene cycle")
		}
		if at == Root {
			break
		}
	}
	if n.Parent == parent {
		return nil
	}
	oldParent := n.Parent
	n.Parent = parent
	for node := range s.nodes {
		w := s.world(node)
		if _, ok := w.InverseAffine(); !ok || w.Determinant3() <= 0 {
			n.Parent = oldParent
			return errors.New("nt3d: reparent would produce invalid world transform")
		}
	}
	n.Parent = oldParent
	old := s.nodes[n.Parent]
	old.Children = slices.DeleteFunc(old.Children, func(v NodeID) bool { return v == id })
	p.Children = append(p.Children, id)
	n.Parent = parent
	s.changed(false, false)
	return nil
}

// SetTransform accepts finite, invertible affine transforms with positive
// determinant. Reflections and singular scales are deliberately unsupported.
func (s *Scene) SetTransform(id NodeID, m nt3dmath.Mat4) error {
	n, ok := s.nodes[id]
	if !ok {
		return fmt.Errorf("nt3d: unknown node %d", id)
	}
	if _, ok := m.InverseAffine(); !ok || m.Determinant3() <= 0 {
		return errors.New("nt3d: transform must be invertible, affine, and orientation-preserving")
	}
	old := n.Local
	n.Local = m
	// Validate affected world transforms, including extreme nested scales.
	for node := range s.nodes {
		w := s.world(node)
		if _, ok := w.InverseAffine(); !ok || w.Determinant3() <= 0 {
			n.Local = old
			return errors.New("nt3d: world transform overflow or singularity")
		}
	}
	s.changed(false, false)
	return nil
}
func (s *Scene) SetVisible(id NodeID, visible bool) error {
	n, ok := s.nodes[id]
	if !ok {
		return fmt.Errorf("nt3d: unknown node %d", id)
	}
	if n.Visible != visible {
		n.Visible = visible
		s.changed(false, false)
	}
	return nil
}

// SetGeometry replaces a named node's geometry, or creates a node under Root.
// All slices and the material are copied. Texture pixels are already immutable.
func (s *Scene) SetGeometry(name string, g Geometry) (NodeID, error) {
	if err := g.Validate(); err != nil {
		return 0, err
	}
	id, ok := s.Find(name)
	if !ok {
		var err error
		id, err = s.Add(Root, name)
		if err != nil {
			return 0, err
		}
	}
	return id, s.SetNodeGeometry(id, g)
}
func (s *Scene) SetNodeGeometry(id NodeID, g Geometry) error {
	n, ok := s.nodes[id]
	if !ok || id == Root {
		return fmt.Errorf("nt3d: invalid geometry node %d", id)
	}
	if err := g.Validate(); err != nil {
		return err
	}
	g = cloneGeometry(g)
	g.Bounds = GeometryBounds(g)
	n.geometry, n.shared, n.pickIndex = &g, nil, nil
	s.changed(true, true)
	n.geometryRevision = s.revision
	return nil
}

// SharedGeometry is a triangle mesh that many nodes show: a forest of one
// tree, a field of markers. It is checked and copied once and held once;
// the GPU uploads it once and draws all its nodes in view at once (one
// instanced draw, or one per texture where SetTexture gave nodes their
// own), and picking indexes it once. It cannot change: to show something
// else, make another and set it. Points, boxes, lines, and arrows are not
// shared; they belong to each node's own geometry.
type SharedGeometry struct {
	geometry Geometry
	id       uint64
	pick     *bvhSlot // Built for picking on demand.
}

// NewSharedGeometry checks and copies a triangle mesh for nodes to share.
func NewSharedGeometry(g Geometry) (*SharedGeometry, error) {
	if err := g.Validate(); err != nil {
		return nil, err
	}
	if len(g.Points)+len(g.Boxes)+len(g.Lines)+len(g.Arrows) != 0 {
		return nil, errors.New("nt3d: shared geometry holds triangles only")
	}
	g = cloneGeometry(g)
	g.Bounds = GeometryBounds(g)
	return &SharedGeometry{geometry: g, id: nextRevision.Add(1)}, nil
}

// Bounds is the geometry's local bounds.
func (sg *SharedGeometry) Bounds() nt3dmath.AABB { return sg.geometry.Bounds }

// SetShared makes a named node show shared geometry, creating the node
// under Root if there is none, as SetGeometry does.
func (s *Scene) SetShared(name string, sg *SharedGeometry) (NodeID, error) {
	if sg == nil {
		return 0, errors.New("nt3d: nil shared geometry")
	}
	id, ok := s.Find(name)
	if !ok {
		var err error
		if id, err = s.Add(Root, name); err != nil {
			return 0, err
		}
	}
	return id, s.SetNodeShared(id, sg)
}

// SetNodeShared makes a node show shared geometry, in place of its own.
func (s *Scene) SetNodeShared(id NodeID, sg *SharedGeometry) error {
	n, ok := s.nodes[id]
	if !ok || id == Root {
		return fmt.Errorf("nt3d: invalid geometry node %d", id)
	}
	if sg == nil {
		return errors.New("nt3d: nil shared geometry")
	}
	n.geometry, n.shared, n.pickIndex = &sg.geometry, sg, nil
	s.changed(true, true)
	n.geometryRevision = sg.id // The same for every node that shows it.
	return nil
}

// SetTexture replaces only an existing texture, preserving mesh buffers.
func (s *Scene) SetTexture(name string, t *Texture) error {
	id, ok := s.Find(name)
	if !ok {
		return fmt.Errorf("nt3d: unknown node %q", name)
	}
	n := s.nodes[id]
	if !t.valid() || n.geometry == nil || !textured(*n.geometry) {
		return errors.New("nt3d: texture replacement requires a valid texture and textured geometry")
	}
	if n.geometry.Material.Texture == t {
		return nil
	}
	g := *n.geometry
	g.Material = cloneMaterial(g.Material)
	g.Material.Texture = t
	n.geometry = &g
	s.changed(false, true)
	return nil
}

// Remove deletes a subtree. Root cannot be removed.
func (s *Scene) Remove(id NodeID) error {
	n, ok := s.nodes[id]
	if !ok || id == Root {
		return fmt.Errorf("nt3d: invalid node %d", id)
	}
	parent := s.nodes[n.Parent]
	parent.Children = slices.DeleteFunc(parent.Children, func(v NodeID) bool { return v == id })
	queue := []NodeID{id}
	for len(queue) > 0 {
		at := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		node := s.nodes[at]
		queue = append(queue, node.Children...)
		delete(s.names, node.Name)
		delete(s.nodes, at)
	}
	s.order = slices.DeleteFunc(s.order, func(v NodeID) bool { return s.nodes[v] == nil })
	s.changed(true, true)
	return nil
}
func (s *Scene) Clear() {
	for _, id := range slices.Clone(s.nodes[Root].Children) {
		_ = s.Remove(id)
	}
}

// Bounds returns visible world-space bounds, without chart range policy.
func (s *Scene) Bounds() (b nt3dmath.AABB) {
	for _, id := range s.order {
		n := s.nodes[id]
		if n.geometry == nil || !s.visible(id) {
			continue
		}
		box := n.geometry.Bounds.Transformed(s.world(id))
		if box.IsValid() {
			b.Include(box.Min)
			b.Include(box.Max)
		}
	}
	return
}
func (s *Scene) visible(id NodeID) bool {
	for {
		n := s.nodes[id]
		if !n.Visible {
			return false
		}
		if id == Root {
			return true
		}
		id = n.Parent
	}
}
