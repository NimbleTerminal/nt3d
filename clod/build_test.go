// Copyright (c) 2026 Neomantra Corp.
package nt3dclod

import (
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// cube returns n seeded positions in [-5,5]³.
func cube(n int, seed uint64) []nt3dmath.Vec3 {
	r := rand.New(rand.NewPCG(seed, 7))
	out := make([]nt3dmath.Vec3, n)
	for i := range out {
		out[i] = nt3dmath.Vec3{X: float32(r.Float64()*10 - 5), Y: float32(r.Float64()*10 - 5), Z: float32(r.Float64()*10 - 5)}
	}
	return out
}

func contains(b nt3dmath.AABB, p nt3dmath.Vec3, pad float32) bool {
	return b.IsValid() && b.Min.X <= p.X-pad && b.Min.Y <= p.Y-pad && b.Min.Z <= p.Z-pad && b.Max.X >= p.X+pad && b.Max.Y >= p.Y+pad && b.Max.Z >= p.Z+pad
}

func TestBuildGroupsEveryItemOnce(t *testing.T) {
	positions := cube(10_000, 1)
	s, err := Build(positions, Options{ClusterSize: 64})
	if err != nil {
		t.Fatal(err)
	}
	seen := make([]int, len(positions))
	for _, i := range s.Order {
		seen[i]++
	}
	for i, n := range seen {
		if n != 1 {
			t.Fatalf("item %d appears %d times", i, n)
		}
	}
	leaves := make([]int, len(s.Clusters))
	for i, n := range s.Nodes {
		if n.Cluster >= 0 {
			leaves[n.Cluster]++
			if n.Right != -1 {
				t.Fatalf("leaf %d has a right child", i)
			}
		} else if n.Right <= int32(i) {
			t.Fatalf("interior node %d has right child %d", i, n.Right)
		}
	}
	for c, n := range leaves {
		if n != 1 {
			t.Fatalf("cluster %d referenced by %d leaves", c, n)
		}
	}
	for c, cl := range s.Clusters {
		if cl.Count < 1 || cl.Count > 64 {
			t.Fatalf("cluster %d holds %d items", c, cl.Count)
		}
		items := s.Items(c)
		if len(items) != int(cl.Count) || &items[0] != &s.Order[cl.First] {
			t.Fatalf("cluster %d: Items does not alias Order", c)
		}
	}
}

func TestBuildBoundsContainItemsPlusPad(t *testing.T) {
	positions := cube(3_000, 2)
	s, err := Build(positions, Options{ClusterSize: 32, Pad: .25})
	if err != nil {
		t.Fatal(err)
	}
	for c, cl := range s.Clusters {
		for _, i := range s.Items(c) {
			if !contains(cl.Bounds, positions[i], .25) {
				t.Fatalf("cluster %d does not contain item %d with pad", c, i)
			}
		}
		center := cl.Bounds.Min.Add(cl.Bounds.Max).Scale(.5)
		d := cl.Bounds.Max.Sub(cl.Bounds.Min)
		radius := float32(math.Sqrt(float64(d.Dot(d)))) / 2
		if center != cl.Center || math.Abs(float64(radius-cl.Radius)) > 1e-6 {
			t.Fatalf("cluster %d: center %v radius %v, want %v %v", c, cl.Center, cl.Radius, center, radius)
		}
	}
	for i, n := range s.Nodes {
		var children []nt3dmath.AABB
		if n.Cluster >= 0 {
			children = append(children, s.Clusters[n.Cluster].Bounds)
		} else {
			children = append(children, s.Nodes[i+1].Bounds, s.Nodes[n.Right].Bounds)
		}
		for _, b := range children {
			if !contains(n.Bounds, b.Min, 0) || !contains(n.Bounds, b.Max, 0) {
				t.Fatalf("node %d does not contain its children", i)
			}
		}
	}
}

func TestBuildProgressivePrefixIsSpread(t *testing.T) {
	var positions []nt3dmath.Vec3
	for y := range 16 {
		for x := range 16 {
			positions = append(positions, nt3dmath.Vec3{X: float32(x), Y: float32(y)})
		}
	}
	s, err := Build(positions, Options{ClusterSize: 256})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Clusters) != 1 {
		t.Fatalf("%d clusters, want 1", len(s.Clusters))
	}
	cells := map[[2]int]bool{}
	for _, i := range s.Order[:16] {
		p := positions[i]
		cells[[2]int{int(p.X) / 4, int(p.Y) / 4}] = true
	}
	if len(cells) < 12 {
		t.Fatalf("the first 16 items cover %d of 16 cells", len(cells))
	}
}

func TestBuildDegenerate(t *testing.T) {
	s, err := Build(nil, Options{})
	if err != nil || len(s.Clusters) != 0 || len(s.Nodes) != 0 || len(s.Order) != 0 {
		t.Fatalf("empty build: %v, %d clusters, %d nodes, %d items", err, len(s.Clusters), len(s.Nodes), len(s.Order))
	}
	same := make([]nt3dmath.Vec3, 100)
	for i := range same {
		same[i] = nt3dmath.Vec3{X: 1, Y: 2, Z: 3}
	}
	s, err = Build(same, Options{})
	if err != nil || len(s.Clusters) != 1 || s.Clusters[0].Radius != 0 || s.Clusters[0].Count != 100 {
		t.Fatalf("identical positions: %v, %+v", err, s.Clusters)
	}
	s, err = Build(same, Options{ClusterSize: 7})
	if err != nil || len(s.Order) != 100 {
		t.Fatalf("identical positions split: %v", err)
	}
}

func TestBuildRejects(t *testing.T) {
	for name, try := range map[string]func() error{
		"NaN":          func() error { _, err := Build([]nt3dmath.Vec3{{X: float32(math.NaN())}}, Options{}); return err },
		"cluster size": func() error { _, err := Build(cube(10, 3), Options{ClusterSize: 70_000}); return err },
		"negative pad": func() error { _, err := Build(cube(10, 3), Options{Pad: -1}); return err },
	} {
		if err := try(); err == nil || !strings.Contains(err.Error(), "nt3dclod:") {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestBuildDeterministic(t *testing.T) {
	positions := cube(5_000, 4)
	a, err := Build(positions, Options{ClusterSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Build(positions, Options{ClusterSize: 100})
	if len(a.Order) != len(b.Order) || len(a.Clusters) != len(b.Clusters) {
		t.Fatal("sizes differ")
	}
	for i := range a.Order {
		if a.Order[i] != b.Order[i] {
			t.Fatalf("order differs at %d", i)
		}
	}
	for i := range a.Clusters {
		if a.Clusters[i] != b.Clusters[i] {
			t.Fatalf("cluster %d differs", i)
		}
	}
}

func TestRebuildReusesAndResizes(t *testing.T) {
	s, err := Build(cube(5_000, 5), Options{ClusterSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	last := s.Revision()
	for _, n := range []int{8_000, 1_000} {
		positions := cube(n, uint64(n))
		if err := s.Rebuild(positions); err != nil {
			t.Fatal(err)
		}
		if s.Revision() <= last {
			t.Fatal("revision did not increase")
		}
		last = s.Revision()
		fresh, _ := Build(positions, Options{ClusterSize: 100})
		if !slices.Equal(s.Order, fresh.Order) || !slices.Equal(s.Clusters, fresh.Clusters) || !slices.Equal(s.Nodes, fresh.Nodes) {
			t.Fatalf("rebuild to %d differs from a fresh build", n)
		}
	}
}

func TestRefitKeepsOrderUpdatesBounds(t *testing.T) {
	positions := cube(4_000, 6)
	s, err := Build(positions, Options{ClusterSize: 50, Pad: .1})
	if err != nil {
		t.Fatal(err)
	}
	order, root := slices.Clone(s.Order), s.Nodes[0].Bounds
	shift := nt3dmath.Vec3{X: .1, Y: .2, Z: .3}
	moved := make([]nt3dmath.Vec3, len(positions))
	for i, p := range positions {
		moved[i] = p.Add(shift)
	}
	if err := s.Refit(moved); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(order, s.Order) {
		t.Fatal("refit changed the order")
	}
	for c, cl := range s.Clusters {
		for _, i := range s.Items(c) {
			if !contains(cl.Bounds, moved[i], .1) {
				t.Fatalf("cluster %d lost moved item %d", c, i)
			}
		}
	}
	if got := s.Nodes[0].Bounds; got.Min.Sub(root.Min.Add(shift)).Dot(nt3dmath.Vec3{X: 1, Y: 1, Z: 1}) > 1e-5 || got.Max.Sub(root.Max.Add(shift)).Dot(nt3dmath.Vec3{X: 1, Y: 1, Z: 1}) > 1e-5 {
		t.Fatalf("root bounds %v, want the old ones moved by %v", got, shift)
	}
	for i, n := range s.Nodes {
		if n.Cluster >= 0 {
			continue
		}
		for _, b := range []nt3dmath.AABB{s.Nodes[i+1].Bounds, s.Nodes[n.Right].Bounds} {
			if !contains(n.Bounds, b.Min, 0) || !contains(n.Bounds, b.Max, 0) {
				t.Fatalf("node %d does not contain its children after refit", i)
			}
		}
	}
	if err := s.Refit(moved[:10]); err == nil {
		t.Fatal("refit with a different count succeeded")
	}
}
