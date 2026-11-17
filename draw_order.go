package nt3d

import (
	"cmp"
	"fmt"
	"slices"
)

// DrawOrder places a node among the nodes a frame draws. Nodes are drawn in
// ascending Layer, then ascending Key, then the order they were added to
// the scene. Where two surfaces lie at the same depth the later one shows,
// and blended geometry (water, glass, Gaussian splats) must be drawn after
// what is seen through it, so the order is part of the picture.
//
// The zero DrawOrder is no order of the node's own: it draws with its
// parent's, and so on up to Root, whose zero order is Layer 0, Key 0. Setting
// a group's order therefore places everything under it, in the order it was
// added, apart from the nodes below that have an order of their own.
//
// Layer is for coarse passes, such as opaque scenery, then decals, then
// water. Key is for a fixed order within a layer, such as a tile's row and
// column: a scene that loads in the background adds its nodes in the order
// the loads finish, and Key makes two renders of the same view the same.
type DrawOrder struct {
	Layer int
	Key   int64
}

func (o DrawOrder) compare(p DrawOrder) int {
	return cmp.Or(cmp.Compare(o.Layer, p.Layer), cmp.Compare(o.Key, p.Key))
}

// SetNodeDrawOrder sets where id, and the nodes under it that have no order
// of their own, are drawn among the scene's nodes; see DrawOrder. The zero
// DrawOrder returns id to its parent's order. Geometry is not uploaded again.
func (s *Scene) SetNodeDrawOrder(id NodeID, order DrawOrder) error {
	n, ok := s.nodes[id]
	if !ok {
		return fmt.Errorf("nt3d: invalid node %d", id)
	}
	if n.drawOrder == order {
		return nil
	}
	switch zero := (DrawOrder{}); {
	case n.drawOrder == zero:
		s.drawOrdered++
	case order == zero:
		s.drawOrdered--
	}
	n.drawOrder = order
	s.drawSequence = nil
	s.changed(false, false)
	return nil
}

// NodeDrawOrder returns the order set on id itself by SetNodeDrawOrder: the
// zero DrawOrder when it draws with its parent's.
func (s *Scene) NodeDrawOrder(id NodeID) (DrawOrder, error) {
	n, ok := s.nodes[id]
	if !ok {
		return DrawOrder{}, fmt.Errorf("nt3d: invalid node %d", id)
	}
	return n.drawOrder, nil
}

// drawOrderOf is the order id draws with: its own, or its nearest
// ancestor's that has one.
func (s *Scene) drawOrderOf(id NodeID) DrawOrder {
	for {
		n := s.nodes[id]
		if n.drawOrder != (DrawOrder{}) || id == Root {
			return n.drawOrder
		}
		id = n.Parent
	}
}

// sequence is the scene's nodes in the order a frame draws them: the order
// they were added, unless some node has a DrawOrder. The sorted sequence is
// kept until a node is added, removed, moved, or given another order.
func (s *Scene) sequence() []NodeID {
	if s.drawOrdered == 0 {
		return s.order
	}
	if s.drawSequence != nil {
		return s.drawSequence
	}
	type placed struct {
		order DrawOrder
		id    NodeID
	}
	nodes := make([]placed, len(s.order))
	for i, id := range s.order {
		nodes[i] = placed{s.drawOrderOf(id), id}
	}
	slices.SortStableFunc(nodes, func(a, b placed) int { return a.order.compare(b.order) })
	s.drawSequence = make([]NodeID, len(nodes))
	for i, n := range nodes {
		s.drawSequence[i] = n.id
	}
	return s.drawSequence
}
