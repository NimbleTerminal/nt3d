// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"math/rand/v2"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// linearNearest is the brute-force answer the index must match.
func linearNearest(g *Geometry, r nt3dmath.Ray) (int, float32, bool) {
	best, found := float32(0), -1
	for i := 0; i+2 < len(g.Indices); i += 3 {
		if t, ok := rayTriangle(r, g.Vertices[g.Indices[i]].Position, g.Vertices[g.Indices[i+1]].Position, g.Vertices[g.Indices[i+2]].Position); ok && (found < 0 || t < best) {
			best, found = t, i/3
		}
	}
	return found, best, found >= 0
}

func TestBVHMatchesLinear(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	torus, err := BuildTorus(Torus{MajorRadius: 1, MinorRadius: .3, Segments: 120, Sides: 40})
	check(t, err)
	// A soup of random triangles, with duplicates and a degenerate one.
	var soup Geometry
	for range 3000 {
		base := nt3dmath.Vec3{X: rng.Float32()*4 - 2, Y: rng.Float32()*4 - 2, Z: rng.Float32()*4 - 2}
		for range 3 {
			soup.Indices = append(soup.Indices, uint32(len(soup.Vertices)))
			soup.Vertices = append(soup.Vertices, Vertex{Position: base.Add(nt3dmath.Vec3{X: rng.Float32() * .4, Y: rng.Float32() * .4, Z: rng.Float32() * .4})})
		}
	}
	soup.Indices = append(soup.Indices, soup.Indices[:6]...)
	soup.Indices = append(soup.Indices, 0, 0, 0)
	for name, g := range map[string]Geometry{"torus": torus, "soup": soup} {
		index := buildBVH(&g)
		hits := 0
		for i := range 4000 {
			origin := nt3dmath.Vec3{X: rng.Float32()*8 - 4, Y: rng.Float32()*8 - 4, Z: rng.Float32()*8 - 4}
			target := nt3dmath.Vec3{X: rng.Float32()*2 - 1, Y: rng.Float32()*2 - 1, Z: rng.Float32()*2 - 1}
			r := nt3dmath.Ray{Origin: origin, Direction: target.Sub(origin)}
			if i%10 == 0 {
				r.Direction.Y = 0 // Parallel to a pair of slabs.
			}
			tri, d, ok := index.nearest(&g, r)
			want, wantD, wantOK := linearNearest(&g, r)
			if ok != wantOK || ok && d != wantD {
				t.Fatalf("%s ray %d: index %d at %v (%v), linear %d at %v (%v)", name, i, tri, d, ok, want, wantD, wantOK)
			}
			if ok {
				hits++
			}
		}
		if hits < 400 {
			t.Fatalf("%s: only %d hits; the test rays miss", name, hits)
		}
	}
}

func TestPickUsesAndRefreshesTheIndex(t *testing.T) {
	sphere, err := BuildSphere(Sphere{Radius: .8, Segments: 64, Rings: 32})
	check(t, err)
	s := NewScene()
	id, err := s.SetGeometry("sphere", sphere)
	check(t, err)
	camera := DefaultCamera()
	f, err := s.Frame(camera, 320, 240)
	check(t, err)
	// The first pick is answered directly and starts the index.
	linear := s.Pick(camera, f, 160, 120, 0)
	slot := s.nodes[id].pickIndex
	if linear == nil || linear.Primitive != TrianglePrimitive || slot == nil {
		t.Fatalf("hit %+v; index started: %v", linear, slot != nil)
	}
	<-slot.done
	if s.pickIndex(f, 0) == nil {
		t.Fatal("index not used once built")
	}
	// The indexed answer is the one testing every triangle gave.
	if hit := s.Pick(camera, f, 160, 120, 0); hit == nil || hit.Index != linear.Index || hit.Position != linear.Position {
		t.Fatalf("indexed %+v, linear %+v", hit, linear)
	}
	// New geometry drops the index; an old frame is not answered from the new one.
	moved, err := TransformGeometry(sphere, nt3dmath.Translation(nt3dmath.Vec3{X: 5}))
	check(t, err)
	check(t, s.SetNodeGeometry(id, moved))
	if s.nodes[id].pickIndex != nil {
		t.Fatal("stale index kept")
	}
	if old := s.Pick(camera, f, 160, 120, 0); old == nil || old.Index != linear.Index || s.nodes[id].pickIndex != nil {
		t.Fatal("old frame answered from new geometry")
	}
	f, err = s.Frame(camera, 320, 240)
	check(t, err)
	if s.Pick(camera, f, 160, 120, 0) != nil {
		t.Fatal("picked a sphere that moved away")
	}
	<-s.nodes[id].pickIndex.done
	if s.Pick(camera, f, 160, 120, 0) != nil {
		t.Fatal("index of the moved sphere picked it")
	}
}

func TestPickIndexingCanBeTurnedOff(t *testing.T) {
	sphere, err := BuildSphere(Sphere{Radius: .8, Segments: 64, Rings: 32})
	check(t, err)
	s := NewScene()
	id, err := s.SetGeometry("sphere", sphere)
	check(t, err)
	camera := DefaultCamera()
	f, err := s.Frame(camera, 320, 240)
	check(t, err)
	indexed := s.Pick(camera, f, 160, 120, 0)
	<-s.nodes[id].pickIndex.done
	s.SetPickIndexing(false)
	if s.nodes[id].pickIndex != nil {
		t.Fatal("index kept after turning indexing off")
	}
	hit := s.Pick(camera, f, 160, 120, 0)
	if hit == nil || hit.Index != indexed.Index || s.nodes[id].pickIndex != nil {
		t.Fatalf("pick %+v; index started: %v", hit, s.nodes[id].pickIndex != nil)
	}
	s.SetPickIndexing(true)
	s.Pick(camera, f, 160, 120, 0)
	if s.nodes[id].pickIndex == nil {
		t.Fatal("indexing not resumed")
	}
	<-s.nodes[id].pickIndex.done
}
