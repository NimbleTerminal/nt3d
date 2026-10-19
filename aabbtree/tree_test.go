// Copyright (c) 2026 Neomantra Corp.
package nt3daabbtree

import (
	"math"
	"math/rand/v2"
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// boxes returns n seeded boxes in [-50,50]³ with extents in [0.1, 4].
func boxes(n int, seed uint64) []nt3dmath.AABB {
	r := rand.New(rand.NewPCG(seed, 11))
	out := make([]nt3dmath.AABB, n)
	for i := range out {
		lo := nt3dmath.Vec3{X: float32(r.Float64()*100 - 50), Y: float32(r.Float64()*100 - 50), Z: float32(r.Float64()*100 - 50)}
		size := nt3dmath.Vec3{X: float32(.1 + r.Float64()*3.9), Y: float32(.1 + r.Float64()*3.9), Z: float32(.1 + r.Float64()*3.9)}
		out[i] = box(lo, lo.Add(size))
	}
	return out
}

func box(lo, hi nt3dmath.Vec3) nt3dmath.AABB {
	var b nt3dmath.AABB
	b.Include(lo)
	b.Include(hi)
	return b
}

func unit(x, y, z float32) nt3dmath.AABB {
	return box(nt3dmath.Vec3{X: x, Y: y, Z: z}, nt3dmath.Vec3{X: x + 1, Y: y + 1, Z: z + 1})
}

// check walks the tree and fails on a parent whose bounds do not contain a
// child's, a wrong parent link, a wrong height, an unbalanced interior node,
// or a leaf count different from Len.
func check[T any](t *testing.T, tr *Tree[T]) {
	t.Helper()
	if tr.root == null {
		if tr.Len() != 0 {
			t.Fatalf("empty tree with Len %d", tr.Len())
		}
		return
	}
	if tr.nodes[tr.root].parent != null {
		t.Fatal("root has a parent")
	}
	leaves := 0
	var walk func(i int32) int32
	walk = func(i int32) int32 {
		n := &tr.nodes[i]
		if n.child1 == null {
			leaves++
			if n.height != 0 {
				t.Fatalf("leaf %d has height %d", i, n.height)
			}
			return 0
		}
		for _, c := range []int32{n.child1, n.child2} {
			child := &tr.nodes[c]
			if child.parent != i {
				t.Fatalf("node %d's child %d has parent %d", i, c, child.parent)
			}
			if child.min.X < n.min.X || child.min.Y < n.min.Y || child.min.Z < n.min.Z || child.max.X > n.max.X || child.max.Y > n.max.Y || child.max.Z > n.max.Z {
				t.Fatalf("node %d does not contain child %d", i, c)
			}
		}
		h1, h2 := walk(n.child1), walk(n.child2)
		if h := 1 + max(h1, h2); n.height != h {
			t.Fatalf("node %d has height %d, want %d", i, n.height, h)
		}
		if d := h1 - h2; d > 1 || d < -1 {
			t.Fatalf("node %d is unbalanced: %d vs %d", i, h1, h2)
		}
		return n.height
	}
	walk(tr.root)
	if leaves != tr.Len() {
		t.Fatalf("%d leaves, Len %d", leaves, tr.Len())
	}
}

func mustPanic(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("no panic, want %q", want)
		}
		if s, ok := r.(string); !ok || !strings.Contains(s, want) {
			t.Fatalf("panic %v, want %q", r, want)
		}
	}()
	f()
}

func TestInsert(t *testing.T) {
	tr := New[string]()
	if tr.Len() != 0 || tr.Height() != 0 {
		t.Fatalf("new tree: Len %d Height %d", tr.Len(), tr.Height())
	}
	b := unit(1, 2, 3)
	id := tr.Insert(b, "one")
	if tr.Len() != 1 || tr.Height() != 1 {
		t.Fatalf("one leaf: Len %d Height %d", tr.Len(), tr.Height())
	}
	if got := tr.Bounds(id); got.Min != b.Min || got.Max != b.Max || !got.IsValid() {
		t.Fatalf("Bounds %v, want %v", got, b)
	}
	if tr.Value(id) != "one" {
		t.Fatalf("Value %q", tr.Value(id))
	}
	check(t, tr)

	tr = New[string]()
	bs := boxes(1000, 1)
	ids := make([]ID, len(bs))
	for i, b := range bs {
		ids[i] = tr.Insert(b, "")
	}
	if tr.Len() != 1000 {
		t.Fatalf("Len %d", tr.Len())
	}
	check(t, tr)
	for i, id := range ids {
		if got := tr.Bounds(id); got.Min != bs[i].Min || got.Max != bs[i].Max {
			t.Fatalf("leaf %d: Bounds %v, want %v", i, got, bs[i])
		}
	}
	if h := tr.Height(); h > 24 {
		t.Fatalf("height %d for 1000 leaves", h)
	}
}

func TestInsertPanics(t *testing.T) {
	tr := New[int]()
	mustPanic(t, "invalid bounds", func() {
		tr.Insert(nt3dmath.AABB{Min: nt3dmath.Vec3{X: 1}, Max: nt3dmath.Vec3{}}, 0)
	})
	nan := float32(math.NaN())
	mustPanic(t, "invalid bounds", func() {
		tr.Insert(nt3dmath.AABB{Min: nt3dmath.Vec3{X: nan}, Max: nt3dmath.Vec3{X: 1}}, 0)
	})
	mustPanic(t, "invalid bounds", func() {
		tr.Insert(nt3dmath.AABB{Max: nt3dmath.Vec3{X: float32(math.Inf(1))}}, 0)
	})
	if tr.Len() != 0 {
		t.Fatal("a rejected insert changed the tree")
	}
	mustPanic(t, "unknown id", func() { tr.Bounds(ID(99)) })
	id := tr.Insert(nt3dmath.AABB{}, 7)
	if got := tr.Bounds(id); got.Min != (nt3dmath.Vec3{}) || got.Max != (nt3dmath.Vec3{}) || !got.IsValid() {
		t.Fatalf("zero box: %v", got)
	}
	check(t, tr)
}

func TestRemove(t *testing.T) {
	tr := New[int]()
	bs := boxes(1000, 2)
	ids := make([]ID, len(bs))
	for i, b := range bs {
		ids[i] = tr.Insert(b, i)
	}
	removed := 0
	for i := 0; i < len(ids); i += 3 {
		tr.Remove(ids[i])
		removed++
		if removed%100 == 0 {
			check(t, tr)
		}
	}
	if tr.Len() != len(ids)-removed {
		t.Fatalf("Len %d, want %d", tr.Len(), len(ids)-removed)
	}
	check(t, tr)
	for i, id := range ids {
		if i%3 == 0 {
			mustPanic(t, "unknown id", func() { tr.Bounds(id) })
			continue
		}
		if got := tr.Bounds(id); got.Min != bs[i].Min || got.Max != bs[i].Max || tr.Value(id) != i {
			t.Fatalf("survivor %d changed: %v %d", i, got, tr.Value(id))
		}
	}
	for i, id := range ids {
		if i%3 != 0 {
			tr.Remove(id)
		}
	}
	if tr.Len() != 0 || tr.Height() != 0 {
		t.Fatalf("after removing all: Len %d Height %d", tr.Len(), tr.Height())
	}
	check(t, tr)
	for i, b := range boxes(10, 3) {
		tr.Insert(b, i)
	}
	if tr.Len() != 10 {
		t.Fatalf("Len %d after reinserting", tr.Len())
	}
	check(t, tr)
}

func TestUpdate(t *testing.T) {
	tr := New[int]()
	bs := boxes(500, 4)
	ids := make([]ID, len(bs))
	for i, b := range bs {
		ids[i] = tr.Insert(b, i)
	}
	moved := boxes(500, 5)
	for i, id := range ids {
		tr.Update(id, moved[i])
	}
	if tr.Len() != 500 {
		t.Fatalf("Len %d", tr.Len())
	}
	check(t, tr)
	for i, id := range ids {
		if got := tr.Bounds(id); got.Min != moved[i].Min || got.Max != moved[i].Max {
			t.Fatalf("leaf %d: Bounds %v, want %v", i, got, moved[i])
		}
		if tr.Value(id) != i {
			t.Fatalf("leaf %d: Value %d", i, tr.Value(id))
		}
	}
	mustPanic(t, "invalid bounds", func() {
		tr.Update(ids[0], nt3dmath.AABB{Min: nt3dmath.Vec3{X: float32(math.NaN())}})
	})
	if got := tr.Bounds(ids[0]); got.Min != moved[0].Min {
		t.Fatal("a rejected update changed the leaf")
	}
	check(t, tr)
}

func TestStaleID(t *testing.T) {
	tr := New[string]()
	a := tr.Insert(unit(0, 0, 0), "a")
	tr.Insert(unit(5, 0, 0), "b")
	tr.Remove(a)
	mustPanic(t, "unknown id", func() { tr.Remove(a) })
	mustPanic(t, "unknown id", func() { tr.Update(a, unit(1, 1, 1)) })
	mustPanic(t, "unknown id", func() { tr.Value(a) })
	tr.Insert(unit(10, 0, 0), "c")
	tr.Insert(unit(15, 0, 0), "d")
	interior := -1
	for i, n := range tr.nodes {
		if n.height > 0 {
			interior = i
			break
		}
	}
	if interior < 0 {
		t.Fatal("no interior node")
	}
	mustPanic(t, "unknown id", func() { tr.Remove(ID(interior)) })
	if tr.Len() != 3 {
		t.Fatalf("Len %d", tr.Len())
	}
	check(t, tr)
}

// exercise feeds a seeded mix of inserts, removes, and updates to tr and
// returns the live IDs.
func exercise(tr *Tree[int], seed uint64) []ID {
	r := rand.New(rand.NewPCG(seed, 3))
	var ids []ID
	for i, b := range boxes(2000, seed) {
		ids = append(ids, tr.Insert(b, i))
	}
	for range 500 {
		k := r.IntN(len(ids))
		tr.Remove(ids[k])
		ids[k] = ids[len(ids)-1]
		ids = ids[:len(ids)-1]
	}
	for _, b := range boxes(500, seed+1) {
		tr.Update(ids[r.IntN(len(ids))], b)
	}
	return ids
}

func TestDeterministic(t *testing.T) {
	a, b := New[int](), New[int]()
	exercise(a, 9)
	exercise(b, 9)
	if a.root != b.root || len(a.nodes) != len(b.nodes) {
		t.Fatalf("roots %d %d, sizes %d %d", a.root, b.root, len(a.nodes), len(b.nodes))
	}
	for i := range a.nodes {
		if a.nodes[i] != b.nodes[i] {
			t.Fatalf("node %d differs: %+v vs %+v", i, a.nodes[i], b.nodes[i])
		}
	}
	check(t, a)
}

func TestAncestors(t *testing.T) {
	tr := New[int]()
	bs := boxes(200, 12)
	ids := make([]ID, len(bs))
	for i, b := range bs {
		ids[i] = tr.Insert(b, i)
	}
	var path []Node
	tr.Ancestors(ids[0], func(n Node) bool { path = append(path, n); return true })
	if len(path) == 0 || len(path) > tr.Height() {
		t.Fatalf("%d ancestors for height %d", len(path), tr.Height())
	}
	for i, n := range path {
		if n.Depth != i {
			t.Fatalf("node %d has depth %d", i, n.Depth)
		}
		if i > 0 {
			outer, inner := path[i-1].Bounds, n.Bounds
			if !overlaps(outer.Min, outer.Max, inner.Min, inner.Max) || outer.Min.X > inner.Min.X || outer.Max.X < inner.Max.X || outer.Min.Y > inner.Min.Y || outer.Max.Y < inner.Max.Y || outer.Min.Z > inner.Min.Z || outer.Max.Z < inner.Max.Z {
				t.Fatalf("ancestor %d does not contain %d", i-1, i)
			}
		}
		if n.Leaf != (i == len(path)-1) {
			t.Fatalf("node %d: leaf %v", i, n.Leaf)
		}
	}
	last := path[len(path)-1]
	if last.ID != ids[0] || last.Bounds.Min != bs[0].Min || last.Bounds.Max != bs[0].Max {
		t.Fatalf("last ancestor %+v", last)
	}
	calls := 0
	tr.Ancestors(ids[0], func(Node) bool { calls++; return false })
	if calls != 1 {
		t.Fatalf("%d calls after returning false", calls)
	}
	tr.Remove(ids[1])
	mustPanic(t, "unknown id", func() { tr.Ancestors(ids[1], func(Node) bool { return true }) })
}

func TestWalk(t *testing.T) {
	New[int]().Walk(func(Node) bool { t.Fatal("empty tree visited"); return true })
	tr := New[int]()
	bs := boxes(200, 13)
	ids := make([]ID, len(bs))
	for i, b := range bs {
		ids[i] = tr.Insert(b, i)
	}
	seen := map[ID]bool{}
	visits, deepest := 0, 0
	var parents []nt3dmath.AABB // Bounds of the current path by depth.
	tr.Walk(func(n Node) bool {
		visits++
		deepest = max(deepest, n.Depth)
		parents = append(parents[:n.Depth], n.Bounds)
		for _, p := range parents[:n.Depth] {
			if p.Min.X > n.Bounds.Min.X || p.Max.X < n.Bounds.Max.X || p.Min.Y > n.Bounds.Min.Y || p.Max.Y < n.Bounds.Max.Y || p.Min.Z > n.Bounds.Min.Z || p.Max.Z < n.Bounds.Max.Z {
				t.Fatalf("an ancestor does not contain a node at depth %d", n.Depth)
			}
		}
		if n.Leaf {
			if seen[n.ID] {
				t.Fatalf("leaf %d twice", n.ID)
			}
			seen[n.ID] = true
			if tr.Value(n.ID) < 0 || tr.Value(n.ID) >= len(ids) || ids[tr.Value(n.ID)] != n.ID {
				t.Fatalf("leaf %d carries value %d", n.ID, tr.Value(n.ID))
			}
		}
		return true
	})
	if visits != 2*len(bs)-1 || len(seen) != len(bs) || deepest+1 != tr.Height() {
		t.Fatalf("%d visits, %d leaves, deepest %d, height %d", visits, len(seen), deepest, tr.Height())
	}
	calls := 0
	tr.Walk(func(Node) bool { calls++; return false })
	if calls != 1 {
		t.Fatalf("%d calls after returning false", calls)
	}
}
