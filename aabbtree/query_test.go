// Copyright (c) 2026 Neomantra Corp.
package nt3daabbtree

import (
	"math"
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

// rayBox is the reference slab test: entry t clamped to zero, or false.
func rayBox(r nt3dmath.Ray, b nt3dmath.AABB) (float32, bool) {
	near, far := float32(0), float32(math.Inf(1))
	o := [3]float32{r.Origin.X, r.Origin.Y, r.Origin.Z}
	d := [3]float32{r.Direction.X, r.Direction.Y, r.Direction.Z}
	lo := [3]float32{b.Min.X, b.Min.Y, b.Min.Z}
	hi := [3]float32{b.Max.X, b.Max.Y, b.Max.Z}
	for k := range 3 {
		if d[k] == 0 {
			if o[k] < lo[k] || o[k] > hi[k] {
				return 0, false
			}
			continue
		}
		t0, t1 := (lo[k]-o[k])/d[k], (hi[k]-o[k])/d[k]
		if t0 > t1 {
			t0, t1 = t1, t0
		}
		near, far = max(near, t0), min(far, t1)
		if far < near {
			return 0, false
		}
	}
	return near, true
}

func TestRayCast(t *testing.T) {
	tr := New[int]()
	bs := boxes(2000, 8)
	ids := make([]ID, len(bs))
	for i, b := range bs {
		ids[i] = tr.Insert(b, i)
	}
	r := rand.New(rand.NewPCG(8, 1))
	rays := 0
	for range 100 {
		ray := nt3dmath.Ray{
			Origin:    nt3dmath.Vec3{X: float32(r.Float64()*100 - 50), Y: float32(r.Float64()*100 - 50), Z: float32(r.Float64()*100 - 50)},
			Direction: nt3dmath.Vec3{X: float32(r.Float64()*6 - 3), Y: float32(r.Float64()*6 - 3), Z: float32(r.Float64()*6 - 3)},
		}
		want := map[ID]float32{}
		for i, b := range bs {
			if d, ok := rayBox(ray, b); ok {
				want[ids[i]] = d
			}
		}
		got := map[ID]float32{}
		tr.RayCast(ray, func(id ID, v int, d float32) bool {
			if ids[v] != id {
				t.Fatalf("id %d carries value %d", id, v)
			}
			if _, dup := got[id]; dup {
				t.Fatalf("leaf %d reported twice", id)
			}
			got[id] = d
			return true
		})
		if len(got) != len(want) {
			t.Fatalf("ray %v: got %d leaves, want %d", ray, len(got), len(want))
		}
		for id, d := range want {
			if g, ok := got[id]; !ok || math.Abs(float64(g-d)) > 1e-5*math.Max(1, float64(d)) {
				t.Fatalf("leaf %d: t %g, want %g", id, g, d)
			}
		}
		rays += len(want)
	}
	if rays == 0 {
		t.Fatal("no ray hit anything; the test is vacuous")
	}

	// A box entirely behind the origin is not a hit.
	behind := New[int]()
	behind.Insert(unit(-5, 0, 0), 0)
	behind.RayCast(nt3dmath.Ray{Origin: nt3dmath.Vec3{Y: .5, Z: .5}, Direction: nt3dmath.Vec3{X: 1}}, func(ID, int, float32) bool {
		t.Fatal("hit behind the origin")
		return true
	})
}

func TestRayCastInsideAndParallel(t *testing.T) {
	tr := New[string]()
	tr.Insert(unit(0, 0, 0), "home")
	tr.Insert(unit(3, 0, 0), "east")
	tr.Insert(unit(3, 5, 0), "northeast")
	var hits []string
	collect := func(_ ID, v string, d float32) bool {
		hits = append(hits, v)
		if v == "home" && d != 0 {
			t.Fatalf("inside leaf reports t %g", d)
		}
		return true
	}
	origin := nt3dmath.Vec3{X: .5, Y: .5, Z: .5}
	tr.RayCast(nt3dmath.Ray{Origin: origin, Direction: nt3dmath.Vec3{X: 1}}, collect)
	slices.Sort(hits)
	if !slices.Equal(hits, []string{"east", "home"}) {
		t.Fatalf("+X ray hit %v", hits)
	}
	hits = nil
	tr.RayCast(nt3dmath.Ray{Origin: nt3dmath.Vec3{X: .5, Y: 5.5, Z: .5}, Direction: nt3dmath.Vec3{X: 1}}, collect)
	if !slices.Equal(hits, []string{"northeast"}) {
		t.Fatalf("ray outside the Y slab hit %v", hits)
	}
	hits = nil
	tr.RayCast(nt3dmath.Ray{Origin: origin}, collect)
	if !slices.Equal(hits, []string{"home"}) {
		t.Fatalf("zero direction hit %v", hits)
	}
}

func TestRayCastEarlyOut(t *testing.T) {
	tr := New[int]()
	for i := range 20 {
		tr.Insert(unit(float32(i)*2, 0, 0), i)
	}
	calls := 0
	tr.RayCast(nt3dmath.Ray{Origin: nt3dmath.Vec3{X: -5, Y: .5, Z: .5}, Direction: nt3dmath.Vec3{X: 1}}, func(ID, int, float32) bool { calls++; return false })
	if calls != 1 {
		t.Fatalf("%d callbacks after returning false", calls)
	}
}

func TestRayCastNearFirst(t *testing.T) {
	tr := New[int]()
	for i := range 10 {
		tr.Insert(unit(float32(i)*3, 0, 0), i)
	}
	first := -1
	tr.RayCast(nt3dmath.Ray{Origin: nt3dmath.Vec3{X: -5, Y: .5, Z: .5}, Direction: nt3dmath.Vec3{X: 1}}, func(_ ID, v int, d float32) bool {
		if first < 0 {
			first = v
			if d != 5 {
				t.Fatalf("first hit at t %g, want 5", d)
			}
		}
		return true
	})
	if first != 0 {
		t.Fatalf("first hit was box %d", first)
	}
}

func TestRayCastNonFinite(t *testing.T) {
	tr := New[int]()
	for i := range 10 {
		tr.Insert(unit(float32(i)*2, 0, 0), i)
	}
	nan, inf := float32(math.NaN()), float32(math.Inf(-1))
	for _, ray := range []nt3dmath.Ray{
		{Origin: nt3dmath.Vec3{X: nan}, Direction: nt3dmath.Vec3{X: 1}},
		{Origin: nt3dmath.Vec3{Y: .5, Z: .5}, Direction: nt3dmath.Vec3{X: nan}},
		{Origin: nt3dmath.Vec3{Y: 50}, Direction: nt3dmath.Vec3{X: 1, Y: nan}},
		{Origin: nt3dmath.Vec3{X: inf, Y: .5, Z: .5}, Direction: nt3dmath.Vec3{X: 1}},
	} {
		tr.RayCast(ray, func(_ ID, v int, d float32) bool {
			t.Fatalf("ray %v hit leaf %d at t %g", ray, v, d)
			return true
		})
	}
}
