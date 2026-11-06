// Copyright (c) 2026 Neomantra Corp.
package nt3daabbtree

import (
	"math"
	"slices"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// FuzzTreeOperations drives a tree through a byte-coded sequence of inserts,
// removes, updates, and queries, checking it against a flat list: every
// live box is found by overlap and ray queries, none is reported twice, and
// the tree's own structure stays consistent with its leaves.
func FuzzTreeOperations(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 0, 10, 20, 30, 1, 2, 3, 3, 0, 1, 5, 5, 5, 1, 1, 1})
	f.Add([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 2, 0})
	f.Add([]byte{0, 100, 100, 100, 1, 1, 1, 0, 200, 200, 200, 1, 1, 1, 2, 0, 50, 50, 50, 2, 2, 2, 3, 0, 0, 0, 1, 1, 1})
	f.Fuzz(func(t *testing.T, program []byte) {
		tr := New[int]()
		var live []nt3dmath.AABB
		var ids []ID
		var values []int
		next := 0
		box := func(p []byte) nt3dmath.AABB {
			var b nt3dmath.AABB
			lo := nt3dmath.Vec3{X: float32(int8(p[0])), Y: float32(int8(p[1])), Z: float32(int8(p[2]))}
			size := nt3dmath.Vec3{X: float32(p[3] & 15), Y: float32(p[4] & 15), Z: float32(p[5] & 15)}
			b.Include(lo)
			b.Include(lo.Add(size))
			return b
		}
		for len(program) >= 7 {
			op, arg := program[0]&3, program[0]>>2
			b := box(program[1:7])
			program = program[7:]
			switch op {
			case 0: // insert
				if len(live) >= 64 {
					continue
				}
				ids = append(ids, tr.Insert(b, next))
				values = append(values, next)
				live = append(live, b)
				next++
			case 1: // remove
				if len(live) == 0 {
					continue
				}
				i := int(arg) % len(live)
				tr.Remove(ids[i])
				live = slices.Delete(live, i, i+1)
				ids = slices.Delete(ids, i, i+1)
				values = slices.Delete(values, i, i+1)
			case 2: // update
				if len(live) == 0 {
					continue
				}
				i := int(arg) % len(live)
				tr.Update(ids[i], b)
				live[i] = b
			case 3: // ray cast from the box's corner along a direction from its size
				if len(live) == 0 {
					continue
				}
				dir := b.Max.Sub(b.Min)
				if dir == (nt3dmath.Vec3{}) {
					dir = nt3dmath.Vec3{X: 1}
				}
				ray := nt3dmath.Ray{Origin: b.Min.Sub(nt3dmath.Vec3{X: 40, Y: 40, Z: 40}), Direction: dir}
				// A hit that grazes a face is decided by rounding, so the
				// oracle demands only hits on boxes shrunk a little and
				// forbids only hits missing boxes grown a little.
				const eps = 1e-3
				must, may := map[ID]float32{}, map[ID]float32{}
				for i, lb := range live {
					if d, ok := rayBox(ray, grow(lb, eps)); ok {
						may[ids[i]] = d
					}
					if s, ok := shrink(lb, eps); ok {
						if d, ok := rayBox(ray, s); ok {
							must[ids[i]] = d
						}
					}
				}
				got := map[ID]float32{}
				tr.RayCast(ray, func(id ID, v int, d float32) bool {
					if _, dup := got[id]; dup {
						t.Fatalf("leaf %d reported twice", id)
					}
					if tr.Value(id) != v {
						t.Fatalf("leaf %d carries %d, callback said %d", id, tr.Value(id), v)
					}
					got[id] = d
					return true
				})
				for id, d := range got {
					if m, ok := may[id]; !ok {
						t.Fatalf("ray %v: hit leaf %d, which it misses", ray, id)
					} else if math.Abs(float64(d-m)) > 1e-2*math.Max(1, float64(m)) {
						t.Fatalf("leaf %d: t %g, want about %g", id, d, m)
					}
				}
				for id := range must {
					if _, ok := got[id]; !ok {
						t.Fatalf("ray %v: missed leaf %d %+v", ray, id, tr.Bounds(id))
					}
				}
			}
			if tr.Len() != len(live) {
				t.Fatalf("Len %d, %d live", tr.Len(), len(live))
			}
			for i, id := range ids {
				if tr.Bounds(id) != live[i] || tr.Value(id) != values[i] {
					t.Fatalf("leaf %d: %+v %d, want %+v %d", id, tr.Bounds(id), tr.Value(id), live[i], values[i])
				}
			}
			// Overlap query against the operation's box.
			want := overlapBrute(live, ids, b)
			var got []ID
			tr.QueryOverlap(b, func(id ID, v int) bool {
				got = append(got, id)
				return true
			})
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Fatalf("overlap %+v: got %v, want %v", b, got, want)
			}
			checkStructure(t, tr, len(live))
		}
	})
}

func grow(b nt3dmath.AABB, eps float32) nt3dmath.AABB {
	var out nt3dmath.AABB
	out.Include(b.Min.Sub(nt3dmath.Vec3{X: eps, Y: eps, Z: eps}))
	out.Include(b.Max.Add(nt3dmath.Vec3{X: eps, Y: eps, Z: eps}))
	return out
}

// shrink pulls the faces in; a box thinner than that on any axis has no
// interior to demand a hit on.
func shrink(b nt3dmath.AABB, eps float32) (nt3dmath.AABB, bool) {
	lo, hi := b.Min.Add(nt3dmath.Vec3{X: eps, Y: eps, Z: eps}), b.Max.Sub(nt3dmath.Vec3{X: eps, Y: eps, Z: eps})
	if lo.X > hi.X || lo.Y > hi.Y || lo.Z > hi.Z {
		return nt3dmath.AABB{}, false
	}
	var out nt3dmath.AABB
	out.Include(lo)
	out.Include(hi)
	return out, true
}

// checkStructure walks the tree: leaves are counted exactly, every node's
// bounds contain its descendants', and depth never exceeds what a balanced
// tree of this size allows with room for rotations.
func checkStructure(t *testing.T, tr *Tree[int], leaves int) {
	stack := []nt3dmath.AABB{}
	count, maxDepth := 0, 0
	tr.Walk(func(n Node) bool {
		stack = stack[:min(len(stack), n.Depth)]
		for _, parent := range stack {
			if n.Bounds.Min.X < parent.Min.X || n.Bounds.Min.Y < parent.Min.Y || n.Bounds.Min.Z < parent.Min.Z ||
				n.Bounds.Max.X > parent.Max.X || n.Bounds.Max.Y > parent.Max.Y || n.Bounds.Max.Z > parent.Max.Z {
				t.Fatalf("node %+v escapes ancestor %+v", n.Bounds, parent)
			}
		}
		stack = append(stack, n.Bounds)
		if n.Leaf {
			count++
		}
		maxDepth = max(maxDepth, n.Depth)
		return true
	})
	if count != leaves {
		t.Fatalf("walked %d leaves, %d live", count, leaves)
	}
	if leaves > 0 && maxDepth > 2*int(math.Log2(float64(leaves)))+4 {
		t.Fatalf("depth %d for %d leaves", maxDepth, leaves)
	}
	if h := tr.Height(); h != maxDepth+1 && !(leaves == 0 && h == 0) {
		t.Fatalf("Height %d, walked depth %d", h, maxDepth)
	}
}
