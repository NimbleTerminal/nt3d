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
