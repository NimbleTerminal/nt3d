// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"math/rand/v2"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// linearNearest is the brute-force answer the index must match.
func linearNearest(g *Geometry, r nt3dmath.Ray, skip func(int, float32, float32) bool) (int, float32, bool) {
	best, found := float32(0), -1
	for i := 0; i+2 < len(g.Indices); i += 3 {
		if t, u, w, ok := rayTriangle(r, g.Vertices[g.Indices[i]].Position, g.Vertices[g.Indices[i+1]].Position, g.Vertices[g.Indices[i+2]].Position); ok && (found < 0 || t < best) && (skip == nil || !skip(i/3, u, w)) {
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
			// Every other ray passes through odd triangles' outer halves,
			// as through a cutout's gaps.
			var skip func(int, float32, float32) bool
			if i%2 == 1 {
				skip = func(tri int, u, w float32) bool { return tri%2 == 1 && u+w > .5 }
			}
			tri, d, ok := index.nearest(&g, r, skip)
			want, wantD, wantOK := linearNearest(&g, r, skip)
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

// One node can opt out while others are indexed, and keeps its choice
// across new geometry.
func TestNodePickIndexing(t *testing.T) {
	sphere, err := BuildSphere(Sphere{Radius: .5, Segments: 64, Rings: 32})
	check(t, err)
	right, err := TransformGeometry(sphere, nt3dmath.Translation(nt3dmath.Vec3{X: .8}))
	check(t, err)
	left, err := TransformGeometry(sphere, nt3dmath.Translation(nt3dmath.Vec3{X: -.8}))
	check(t, err)
	s := NewScene()
	a, err := s.SetGeometry("indexed", right)
	check(t, err)
	b, err := s.SetGeometry("scanned", left)
	check(t, err)
	check(t, s.SetNodePickIndexing(b, false))
	camera := DefaultCamera()
	camera.Alpha, camera.Beta = 89, -90
	f, err := s.Frame(camera, 320, 240)
	check(t, err)
	// Pick each sphere at its projected center.
	at := func(x float32) (float32, float32) {
		px, py, _, _ := f.Matrix.Project(nt3dmath.Vec3{X: x}, 320, 240)
		return px, py
	}
	pick := func() {
		for _, x := range []float32{.8, -.8} {
			px, py := at(x)
			if hit := s.Pick(camera, f, px, py, 0); hit == nil {
				t.Fatalf("missed the sphere at x=%v", x)
			}
		}
	}
	pick()
	if s.nodes[a].pickIndex == nil || s.nodes[b].pickIndex != nil {
		t.Fatal("the opted-out node was indexed, or the other was not")
	}
	<-s.nodes[a].pickIndex.done
	// New geometry keeps the node's choice.
	check(t, s.SetNodeGeometry(b, left))
	f, err = s.Frame(camera, 320, 240)
	check(t, err)
	pick()
	if s.nodes[b].pickIndex != nil {
		t.Fatal("new geometry forgot the node's choice")
	}
	// Turning it back on indexes it; off again drops the index.
	check(t, s.SetNodePickIndexing(b, true))
	pick()
	if s.nodes[b].pickIndex == nil {
		t.Fatal("not indexed once allowed")
	}
	<-s.nodes[b].pickIndex.done
	check(t, s.SetNodePickIndexing(b, false))
	if s.nodes[b].pickIndex != nil {
		t.Fatal("index kept after opting out")
	}
	if s.SetNodePickIndexing(9999, false) == nil {
		t.Fatal("accepted an unknown node")
	}
}

func TestWarmPickIndices(t *testing.T) {
	sphere, err := BuildSphere(Sphere{Radius: .6, Segments: 64, Rings: 32})
	check(t, err)
	shared, err := NewSharedGeometry(sphere)
	check(t, err)

	s := NewScene()
	nodeID, err := s.SetGeometry("solo", sphere)
	check(t, err)
	_, err = s.SetShared("inst-1", shared)
	check(t, err)
	_, err = s.SetShared("inst-2", shared)
	check(t, err)

	// Before warming or picking, neither slot has started.
	if s.nodes[nodeID].pickIndex != nil || shared.pick != nil {
		t.Fatal("pick index started before WarmPickIndices or Pick")
	}

	<-s.WarmPickIndices()

	if s.nodes[nodeID].pickIndex == nil || s.nodes[nodeID].pickIndex.index.Load() == nil {
		t.Fatal("solo node BVH not warmed")
	}
	if shared.pick == nil || shared.pick.index.Load() == nil {
		t.Fatal("shared geometry BVH not warmed")
	}

	camera := DefaultCamera()
	f, err := s.Frame(camera, 320, 240)
	check(t, err)
	if s.hasUnindexedLargeMesh(f) {
		t.Fatal("expected all large meshes to be indexed after WarmPickIndices")
	}
	hit := s.Pick(camera, f, 160, 120, 0)
	if hit == nil || hit.Primitive != TrianglePrimitive {
		t.Fatalf("expected triangle hit after WarmPickIndices, got %+v", hit)
	}
}

func TestFrameUnproject(t *testing.T) {
	s := NewScene()
	for _, proj := range []Projection{Perspective, Orthographic} {
		cam := DefaultCamera()
		cam.Projection = proj
		cam.Alpha = 25
		cam.Beta = -40
		cam.Distance = 3.5
		f, err := s.Frame(cam, 640, 480)
		check(t, err)

		want := nt3dmath.Vec3{X: 0.35, Y: -0.25, Z: 0.45}
		px, py, pz, ok := f.Matrix.Project(want, f.Width, f.Height)
		if !ok {
			t.Fatalf("proj %v: Project failed for %v", proj, want)
		}
		got, ok := f.Unproject(px, py, pz)
		if !ok {
			t.Fatalf("proj %v: Unproject failed", proj)
		}
		diff := got.Sub(want)
		if diff.Dot(diff) > 1e-8 {
			t.Fatalf("proj %v: Unproject(%v, %v, %v) = %v, want %v", proj, px, py, pz, got, want)
		}
	}
}
