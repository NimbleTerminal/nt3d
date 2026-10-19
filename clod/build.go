// Copyright (c) 2026 Neomantra Corp.
package nt3dclod

import (
	"fmt"
	"math"
	"math/bits"
	"slices"

	"nimbleterminal.dev/nt3d/math"
)

const (
	// MaxItems is the most items a set holds; MaxClusters the most clusters.
	MaxItems    = 1 << 24
	MaxClusters = 1 << 22
	// DefaultClusterSize is the most items a cluster holds when Options
	// leaves ClusterSize zero.
	DefaultClusterSize = 256
	maxClusterSize     = 65536
)

// Options configures Build.
type Options struct {
	// ClusterSize is the most items a cluster holds, 1 to 65536; zero means
	// DefaultClusterSize. Clusters hold between half that and that many
	// items, except the whole set when it is smaller.
	ClusterSize int
	// Pad grows every cluster's bounds by this many world units on each
	// side: the largest radius an item draws past its position, so a
	// cluster is kept while any of its items could show.
	Pad float32
}

func (o Options) size() int {
	if o.ClusterSize == 0 {
		return DefaultClusterSize
	}
	return o.ClusterSize
}

// Cluster is a spatial group of items. Its items are Set.Order[First :
// First+Count], in progressive order: the first ceil(f*Count) of them are
// a spread subsample of fraction f.
type Cluster struct {
	Bounds nt3dmath.AABB // Of the items' positions, grown by Options.Pad.
	Center nt3dmath.Vec3 // The center of Bounds.
	Radius float32       // Half the diagonal of Bounds: a sphere around it.
	First  uint32
	Count  uint32
}

// Node is one node of the hierarchy, stored depth first: an interior node's
// left child follows it, and Right names the other; a leaf names its Cluster.
type Node struct {
	Bounds  nt3dmath.AABB
	Right   int32 // The right child, or -1 for a leaf.
	Cluster int32 // The cluster, or -1 for an interior node.
}

// Set is a clustered item set. Build makes one; Rebuild and Refit update it.
type Set struct {
	Clusters []Cluster
	Nodes    []Node
	// Order holds every item's index once, grouped by cluster, each
	// cluster's in progressive order.
	Order []uint32

	opts     Options
	revision uint64
	keys     []uint64 // Morton keys, reused by progressive ordering.
}

// Build clusters positions: items are grouped by recursive median splits
// on the longest axis until a group holds at most Options.ClusterSize.
// Positions must be finite and at most MaxItems. An empty set selects
// nothing.
func Build(positions []nt3dmath.Vec3, opts Options) (*Set, error) {
	if size := opts.size(); size < 1 || size > maxClusterSize {
		return nil, fmt.Errorf("nt3dclod: cluster size must be 1 to %d", maxClusterSize)
	}
	if !nt3dmath.IsFinite(float64(opts.Pad)) || opts.Pad < 0 {
		return nil, fmt.Errorf("nt3dclod: pad must be finite and not negative")
	}
	s := &Set{opts: opts}
	if err := s.Rebuild(positions); err != nil {
		return nil, err
	}
	return s, nil
}

// Revision changes whenever the set's contents change: GPU.Upload compares
// it to upload only new contents.
func (s *Set) Revision() uint64 { return s.revision }

// Items returns cluster c's item indices, in progressive order. The slice
// is the set's until its next Rebuild.
func (s *Set) Items(c int) []uint32 {
	cl := s.Clusters[c]
	return s.Order[cl.First : cl.First+cl.Count]
}

func validPositions(positions []nt3dmath.Vec3) error {
	if len(positions) > MaxItems {
		return fmt.Errorf("nt3dclod: %d items exceed %d", len(positions), MaxItems)
	}
	for i, p := range positions {
		if !nt3dmath.IsFinite(float64(p.X)) || !nt3dmath.IsFinite(float64(p.Y)) || !nt3dmath.IsFinite(float64(p.Z)) {
			return fmt.Errorf("nt3dclod: item %d has a non-finite position", i)
		}
	}
	return nil
}

// Rebuild regroups the set around new positions with the same Options,
// reusing its allocations. On an error, the set is unchanged.
func (s *Set) Rebuild(positions []nt3dmath.Vec3) error {
	if err := validPositions(positions); err != nil {
		return err
	}
	n := len(positions)
	order := slices.Grow(s.Order[:0], n)[:n]
	for i := range order {
		order[i] = uint32(i)
	}
	b := builder{positions: positions, size: s.opts.size(), pad: s.opts.Pad, order: order, clusters: s.Clusters[:0], nodes: s.Nodes[:0]}
	if n > 0 {
		b.split(0, n)
	}
	if len(b.clusters) > MaxClusters {
		return fmt.Errorf("nt3dclod: %d clusters exceed %d", len(b.clusters), MaxClusters)
	}
	s.Order, s.Clusters, s.Nodes = b.order, b.clusters, b.nodes
	for _, c := range s.Clusters {
		s.progressive(positions, s.Order[c.First:c.First+c.Count])
	}
	s.revision++
	return nil
}

// Refit keeps the set's groups and recomputes their bounds from moved
// positions, for items that moved a little; there must be as many as were
// built. Clusters that drifted far apart should be rebuilt instead.
func (s *Set) Refit(positions []nt3dmath.Vec3) error {
	if len(positions) != len(s.Order) {
		return fmt.Errorf("nt3dclod: refit needs %d positions, got %d", len(s.Order), len(positions))
	}
	if err := validPositions(positions); err != nil {
		return err
	}
	for c := range s.Clusters {
		var b nt3dmath.AABB
		for _, i := range s.Items(c) {
			b.Include(positions[i])
		}
		s.Clusters[c] = cluster(b, s.opts.Pad, s.Clusters[c].First, s.Clusters[c].Count)
	}
	// Children follow their parents, so walking backward sees them first.
	for i := len(s.Nodes) - 1; i >= 0; i-- {
		n := &s.Nodes[i]
		if n.Cluster >= 0 {
			n.Bounds = s.Clusters[n.Cluster].Bounds
			continue
		}
		b := s.Nodes[i+1].Bounds
		b.Include(s.Nodes[n.Right].Bounds.Min)
		b.Include(s.Nodes[n.Right].Bounds.Max)
		n.Bounds = b
	}
	s.revision++
	return nil
}

// cluster is the cluster record for items bounded by b, padded.
func cluster(b nt3dmath.AABB, pad float32, first, count uint32) Cluster {
	grow := nt3dmath.Vec3{X: pad, Y: pad, Z: pad}
	var padded nt3dmath.AABB
	padded.Include(b.Min.Sub(grow))
	padded.Include(b.Max.Add(grow))
	d := padded.Max.Sub(padded.Min)
	return Cluster{Bounds: padded, Center: padded.Min.Add(padded.Max).Scale(.5), Radius: float32(math.Sqrt(float64(d.Dot(d)))) / 2, First: first, Count: count}
}

type builder struct {
	positions []nt3dmath.Vec3
	size      int
	pad       float32
	order     []uint32
	clusters  []Cluster
	nodes     []Node
}

func components(v nt3dmath.Vec3) [3]float32 { return [3]float32{v.X, v.Y, v.Z} }

// split adds the node for order[lo:hi] and its descendants, returning its index.
func (b *builder) split(lo, hi int) int32 {
	var bounds nt3dmath.AABB
	for _, i := range b.order[lo:hi] {
		bounds.Include(b.positions[i])
	}
	index := int32(len(b.nodes))
	b.nodes = append(b.nodes, Node{Right: -1, Cluster: -1})
	extent := components(bounds.Max.Sub(bounds.Min))
	axis := 0
	for k := 1; k < 3; k++ {
		if extent[k] > extent[axis] {
			axis = k
		}
	}
	if hi-lo <= b.size || extent[axis] == 0 {
		c := cluster(bounds, b.pad, uint32(lo), uint32(hi-lo))
		b.nodes[index].Bounds, b.nodes[index].Cluster = c.Bounds, int32(len(b.clusters))
		b.clusters = append(b.clusters, c)
		return index
	}
	at := func(i uint32) float32 { return components(b.positions[i])[axis] }
	// Split at the middle of the extent; if every item falls on one side,
	// as rounding can arrange, split them in half instead.
	split := components(bounds.Min)[axis] + extent[axis]/2
	mid := lo
	for i := lo; i < hi; i++ {
		if at(b.order[i]) < split {
			b.order[i], b.order[mid] = b.order[mid], b.order[i]
			mid++
		}
	}
	if mid == lo || mid == hi {
		slices.SortFunc(b.order[lo:hi], func(p, q uint32) int {
			switch x, y := at(p), at(q); {
			case x < y:
				return -1
			case x > y:
				return 1
			}
			return int(p) - int(q)
		})
		mid = (lo + hi) / 2
	}
	b.split(lo, mid)
	right := b.split(mid, hi)
	b.nodes[index].Right = right
	b.nodes[index].Bounds = b.nodes[index+1].Bounds
	b.nodes[index].Bounds.Include(b.nodes[right].Bounds.Min)
	b.nodes[index].Bounds.Include(b.nodes[right].Bounds.Max)
	return index
}

// progressive reorders a cluster's items so that every prefix is spread
// over the cluster: items sorted along a Morton curve over the cluster's
// extents, then taken in bit-reversed order, which visits the curve at
// ever finer, evenly spaced steps. Ties and the order of items keep the
// result deterministic.
func (s *Set) progressive(positions []nt3dmath.Vec3, items []uint32) {
	n := len(items)
	if n < 2 {
		return
	}
	var b nt3dmath.AABB
	for _, i := range items {
		b.Include(positions[i])
	}
	lo, extent := components(b.Min), components(b.Max.Sub(b.Min))
	keys := slices.Grow(s.keys[:0], n)[:n]
	for k, i := range items {
		p := components(positions[i])
		var code uint64
		for axis := range 3 {
			var q uint64
			if extent[axis] > 0 {
				q = uint64(min(1023, max(0, (p[axis]-lo[axis])/extent[axis]*1024)))
			}
			code |= spread(q) << axis
		}
		keys[k] = code<<32 | uint64(i) // The item index breaks ties.
	}
	slices.Sort(keys)
	s.keys = keys
	width := bits.Len(uint(n - 1))
	k := 0
	for j := range 1 << width {
		r := int(bits.Reverse64(uint64(j)) >> (64 - width))
		if r < n {
			items[k] = uint32(keys[r])
			k++
		}
	}
}

// spread places the low ten bits of q at every third bit.
func spread(q uint64) uint64 {
	q &= 1023
	q = (q | q<<16) & 0x030000FF
	q = (q | q<<8) & 0x0300F00F
	q = (q | q<<4) & 0x030C30C3
	q = (q | q<<2) & 0x09249249
	return q
}
