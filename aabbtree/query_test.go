// Copyright (c) 2026 Neomantra Corp.
package nt3daabbtree

import (
	"math/rand/v2"
	"slices"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

func overlapBrute(bs []nt3dmath.AABB, ids []ID, q nt3dmath.AABB) []ID {
	var out []ID
	for i, b := range bs {
		if b.Min.X <= q.Max.X && b.Max.X >= q.Min.X && b.Min.Y <= q.Max.Y && b.Max.Y >= q.Min.Y && b.Min.Z <= q.Max.Z && b.Max.Z >= q.Min.Z {
			out = append(out, ids[i])
		}
	}
	slices.Sort(out)
	return out
}

func TestQueryOverlap(t *testing.T) {
	tr := New[int]()
	bs := boxes(2000, 6)
	ids := make([]ID, len(bs))
	for i, b := range bs {
		ids[i] = tr.Insert(b, i)
	}
	r := rand.New(rand.NewPCG(6, 1))
	for _, q := range boxes(100, 7) {
		q.Max = q.Max.Add(nt3dmath.Vec3{X: float32(r.Float64() * 10), Y: float32(r.Float64() * 10), Z: float32(r.Float64() * 10)})
		var got []ID
		tr.QueryOverlap(q, func(id ID, v int) bool {
			if ids[v] != id {
				t.Fatalf("id %d carries value %d", id, v)
			}
			got = append(got, id)
			return true
		})
		slices.Sort(got)
		for i := 1; i < len(got); i++ {
			if got[i] == got[i-1] {
				t.Fatalf("leaf %d reported twice", got[i])
			}
		}
		if want := overlapBrute(bs, ids, q); !slices.Equal(got, want) {
			t.Fatalf("query %v: got %d leaves, want %d", q, len(got), len(want))
		}
	}
	tr.QueryOverlap(unit(200, 200, 200), func(ID, int) bool { t.Fatal("hit far away"); return true })

	touch := New[string]()
	touch.Insert(unit(0, 0, 0), "a")
	hits := 0
	touch.QueryOverlap(unit(1, 0, 0), func(ID, string) bool { hits++; return true })
	if hits != 1 {
		t.Fatalf("touching face: %d hits", hits)
	}
	New[int]().QueryOverlap(unit(0, 0, 0), func(ID, int) bool { t.Fatal("empty tree hit"); return true })
}

func TestQueryOverlapEarlyOut(t *testing.T) {
	tr := New[int]()
	for i := range 20 {
		tr.Insert(unit(float32(i)*.5, 0, 0), i)
	}
	calls := 0
	tr.QueryOverlap(box(nt3dmath.Vec3{X: -1, Y: -1, Z: -1}, nt3dmath.Vec3{X: 20, Y: 2, Z: 2}), func(ID, int) bool { calls++; return false })
	if calls != 1 {
		t.Fatalf("%d callbacks after returning false", calls)
	}
}
