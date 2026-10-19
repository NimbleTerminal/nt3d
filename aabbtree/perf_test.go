// Copyright (c) 2026 Neomantra Corp.
package nt3daabbtree

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"nimbleterminal.dev/nt3d/math"
)

// perfScene is 20,000 boxes with 200 small queries and 200 rays.
func perfScene() (tr *Tree[int], bs []nt3dmath.AABB, ids []ID, queries []nt3dmath.AABB, rays []nt3dmath.Ray) {
	bs = boxes(20_000, 20)
	tr = New[int]()
	ids = make([]ID, len(bs))
	for i, b := range bs {
		ids[i] = tr.Insert(b, i)
	}
	queries = boxes(200, 21)
	r := rand.New(rand.NewPCG(22, 1))
	for range 200 {
		rays = append(rays, nt3dmath.Ray{
			Origin:    nt3dmath.Vec3{X: float32(r.Float64()*100 - 50), Y: float32(r.Float64()*100 - 50), Z: float32(r.Float64()*100 - 50)},
			Direction: nt3dmath.Vec3{X: float32(r.Float64()*2 - 1), Y: float32(r.Float64()*2 - 1), Z: float32(r.Float64()*2 - 1)},
		})
	}
	return
}

// fastest times f three times and returns the quickest, so a scheduler
// stall on a busy test runner does not count against either side.
func fastest(f func()) time.Duration {
	best := time.Duration(math.MaxInt64)
	for range 3 {
		start := time.Now()
		f()
		best = min(best, time.Since(start))
	}
	return best
}

func TestFasterThanLinear(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	tr, bs, _, queries, rays := perfScene()

	treeHits := 0
	treeOverlap := fastest(func() {
		treeHits = 0
		for _, q := range queries {
			tr.QueryOverlap(q, func(ID, int) bool { treeHits++; return true })
		}
	})
	linearHits := 0
	linearOverlap := fastest(func() {
		linearHits = 0
		for _, q := range queries {
			for _, b := range bs {
				if overlaps(b.Min, b.Max, q.Min, q.Max) {
					linearHits++
				}
			}
		}
	})
	if treeHits != linearHits {
		t.Fatalf("overlap: tree found %d, linear %d", treeHits, linearHits)
	}
	t.Logf("overlap: tree %v, linear %v (%.0fx), %d hits", treeOverlap, linearOverlap, float64(linearOverlap)/float64(treeOverlap), treeHits)
	if treeOverlap*2 > linearOverlap {
		t.Fatalf("overlap: tree %v is not clearly faster than linear %v", treeOverlap, linearOverlap)
	}

	treeRay := fastest(func() {
		treeHits = 0
		for _, r := range rays {
			tr.RayCast(r, func(ID, int, float32) bool { treeHits++; return true })
		}
	})
	linearRay := fastest(func() {
		linearHits = 0
		for _, r := range rays {
			for _, b := range bs {
				if _, ok := rayBox(r, b); ok {
					linearHits++
				}
			}
		}
	})
	if treeHits != linearHits {
		t.Fatalf("ray: tree found %d, linear %d", treeHits, linearHits)
	}
	t.Logf("ray: tree %v, linear %v (%.0fx), %d hits", treeRay, linearRay, float64(linearRay)/float64(treeRay), treeHits)
	if treeRay*2 > linearRay {
		t.Fatalf("ray: tree %v is not clearly faster than linear %v", treeRay, linearRay)
	}
}

func TestBalancedUnderSortedInserts(t *testing.T) {
	tr := New[int]()
	ids := make([]ID, 20_000)
	for i := range ids {
		ids[i] = tr.Insert(unit(float32(i), 0, 0), i)
	}
	if h := tr.Height(); h > 32 {
		t.Fatalf("height %d after sorted inserts", h)
	}
	for i := 0; i < len(ids); i += 2 {
		tr.Remove(ids[i])
	}
	if h := tr.Height(); h > 32 {
		t.Fatalf("height %d after removing half", h)
	}
	check(t, tr)
}

func BenchmarkInsert(b *testing.B) {
	bs := boxes(20_000, 20)
	tr := New[int]()
	for i := 0; b.Loop(); i++ {
		if tr.Len() == len(bs) {
			tr = New[int]()
		}
		tr.Insert(bs[i%len(bs)], i)
	}
}

func BenchmarkUpdate(b *testing.B) {
	tr, _, ids, _, _ := perfScene()
	moved := boxes(len(ids), 23)
	for i := 0; b.Loop(); i++ {
		k := i % len(ids)
		tr.Update(ids[k], moved[k])
	}
}

func BenchmarkQueryOverlap(b *testing.B) {
	tr, _, _, queries, _ := perfScene()
	for i := 0; b.Loop(); i++ {
		tr.QueryOverlap(queries[i%len(queries)], func(ID, int) bool { return true })
	}
}

func BenchmarkLinearOverlap(b *testing.B) {
	_, bs, _, queries, _ := perfScene()
	for i := 0; b.Loop(); i++ {
		q := queries[i%len(queries)]
		for _, box := range bs {
			overlaps(box.Min, box.Max, q.Min, q.Max)
		}
	}
}

func BenchmarkRayCast(b *testing.B) {
	tr, _, _, _, rays := perfScene()
	for i := 0; b.Loop(); i++ {
		tr.RayCast(rays[i%len(rays)], func(ID, int, float32) bool { return true })
	}
}

func BenchmarkLinearRayCast(b *testing.B) {
	_, bs, _, _, rays := perfScene()
	for i := 0; b.Loop(); i++ {
		r := rays[i%len(rays)]
		for _, box := range bs {
			rayBox(r, box)
		}
	}
}
