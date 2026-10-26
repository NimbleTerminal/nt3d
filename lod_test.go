// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"math"
	"math/rand/v2"
	"reflect"
	"testing"

	"nimbleterminal.dev/nt3d/clod"
	"nimbleterminal.dev/nt3d/math"
)

// lodCloud is n Gaussian points scattered through a 2×2×2 cube about the
// origin, clustered by size.
func lodCloud(t *testing.T, n, size int) Geometry {
	t.Helper()
	rng := rand.New(rand.NewPCG(7, 11))
	g := gaussianGeometry()
	g.Points = nil
	positions := make([]nt3dmath.Vec3, n)
	for i := range positions {
		positions[i] = nt3dmath.Vec3{X: rng.Float32()*2 - 1, Y: rng.Float32()*2 - 1, Z: rng.Float32()*2 - 1}
		p := gaussianGeometry().Points[0]
		p.Position = positions[i]
		g.Points = append(g.Points, p)
	}
	set, err := nt3dclod.Build(positions, nt3dclod.Options{ClusterSize: size})
	check(t, err)
	g.Clusters = set
	g.Bounds = GeometryBounds(g)
	return g
}

// lodOrtho is a 400×400 orthographic view from +Z in which the cube fills the frame.
func lodOrtho(geometry ...Geometry) Frame {
	f := Frame{Width: 400, Height: 400, Matrix: nt3dmath.Identity(), Cull: true}
	f.Matrix[10], f.Matrix[14] = -.25, .5
	f.Geometry = geometry
	return f
}

// lodPerspective is a 400×400 perspective view of the cube from distance on +X.
func lodPerspective(distance float64, geometry ...Geometry) Frame {
	cam := Camera{Projection: Perspective, Beta: 0, Distance: distance, Near: .1, Far: 1000}
	f := Frame{Width: 400, Height: 400, Matrix: cam.Matrix(1), Cull: true, Perspective: true}
	f.Eye, _, _, f.Forward = cam.basis()
	f.Geometry = geometry
	return f
}

func plan(t *testing.T, f Frame) (blendPlan, lodPlan) {
	t.Helper()
	bp, err := f.planBlend(f.drawn(1))
	check(t, err)
	return bp, f.planLOD(&bp)
}

func TestLODSelection(t *testing.T) {
	cloud := lodCloud(t, 4096, 64)

	// An unlimited budget draws every point, with no ranges.
	f := lodOrtho(cloud)
	f.SplatBudget = -1
	_, lod := plan(t, f)
	if len(lod.slots) != 1 || lod.slots[0].ranges != nil || lod.slots[0].selected != 4096 || lod.drawn != 4096 || lod.resident != 4096 {
		t.Fatalf("unlimited: %+v", lod)
	}

	// The default budget at full size draws every visible cluster in full.
	f.SplatBudget = 0
	_, lod = plan(t, f)
	s := lod.slots[0]
	if lod.drawn != 4096 || s.selected != 4096 || len(s.ranges) != len(cloud.Clusters.Clusters) {
		t.Fatalf("near: drawn %d, selected %d, %d ranges", lod.drawn, s.selected, len(s.ranges))
	}
	for _, r := range s.ranges {
		if r.f8 != 255 || r.count != cloud.Clusters.Clusters[clusterAt(cloud, r.first)].Count {
			t.Fatalf("near range not full: %+v", r)
		}
	}

	// Far away, clusters thin: some of each is drawn, and f8 says how much.
	far := lodPerspective(100, cloud)
	_, lod = plan(t, far)
	s = lod.slots[0]
	if lod.drawn >= 4096/4 || len(s.ranges) == 0 {
		t.Fatalf("far: drawn %d, %d ranges", lod.drawn, len(s.ranges))
	}
	thinned := 0
	for _, r := range s.ranges {
		total := cloud.Clusters.Clusters[clusterAt(cloud, r.first)].Count
		want := uint8(min(255, max(1, math.Round(255*float64(r.count)/float64(total)))))
		if r.count < 1 || r.f8 != want {
			t.Fatalf("far range %+v of %d: want f8 %d", r, total, want)
		}
		if r.f8 < 255 {
			thinned++
		}
	}
	if thinned == 0 {
		t.Fatal("far: no cluster thinned")
	}

	// A camera turned away culls the whole cloud: no slot, nothing drawn.
	away := lodPerspective(100, cloud)
	away.Matrix = Camera{Projection: Perspective, Pose: nt3dmath.Translation(nt3dmath.Vec3{X: 100}), Near: .1, Far: 1000}.Matrix(1)
	_, lod = plan(t, away)
	if lod.drawn != 0 || len(lod.slots) != 0 {
		t.Fatalf("away: %+v", lod)
	}

	// With half the cube out of view, the clusters wholly outside are culled
	// and the rest draw in full.
	half := lodOrtho(cloud)
	half.Matrix[12] = 2.3 // Points reach 257 px (1.285 clip units) past their clusters.
	_, lod = plan(t, half)
	s = lod.slots[0]
	visible := 0
	for _, r := range s.ranges {
		visible += int(r.count)
	}
	if n := len(cloud.Clusters.Clusters); len(s.ranges) == 0 || len(s.ranges) >= n || lod.drawn != visible || lod.drawn >= 4096 || lod.resident != 4096 {
		t.Fatalf("half: %d of %d clusters, drawn %d, resident %d", len(s.ranges), n, lod.drawn, lod.resident)
	}

	// A cluster whose sphere holds the eye is drawn in full, however far its
	// neighbors thin. The eye sits at the first cluster's center.
	c0 := cloud.Clusters.Clusters[0]
	inside := lodPerspective(100, cloud)
	cam := Camera{Projection: Perspective, Pose: nt3dmath.Translation(c0.Center), Near: .001, Far: 1000}
	inside.Matrix = cam.Matrix(1)
	inside.Cull = false
	_, lod = plan(t, inside)
	found := false
	for _, r := range lod.slots[0].ranges {
		if r.first == c0.First {
			found = true
			if r.count != c0.Count || r.f8 != 255 {
				t.Fatalf("eye cluster not full: %+v of %d", r, c0.Count)
			}
		}
	}
	if !found {
		t.Fatal("eye cluster missing")
	}

	// A small budget shared by many clusters is soft by at most one splat each.
	many := lodCloud(t, 16000, 16)
	small := lodOrtho(many)
	small.SplatBudget = 100
	_, lod = plan(t, small)
	if n := len(many.Clusters.Clusters); n < 1000 {
		t.Fatalf("only %d clusters", n)
	} else if lod.drawn > 100+n || len(lod.slots[0].ranges) != n {
		t.Fatalf("budget 100: drawn %d of %d clusters, %d ranges", lod.drawn, n, len(lod.slots[0].ranges))
	}
	for _, r := range lod.slots[0].ranges {
		if r.count < 1 {
			t.Fatalf("empty range %+v", r)
		}
	}

	// Unclustered points come off the budget first; a clustered slot shares the rest.
	fixed := gaussianGeometry()
	fixed.Points = nil
	for range 900 {
		fixed.Points = append(fixed.Points, gaussianGeometry().Points[0])
	}
	fixed.Bounds = GeometryBounds(fixed)
	two := lodPerspective(100, fixed, cloud)
	two.SplatBudget = 1000
	bp, lod := plan(t, two)
	if len(bp.slots) != 2 || len(lod.slots) != 2 {
		t.Fatalf("slots %v", bp.slots)
	}
	fixedSlot, cloudSlot := lod.slots[0], lod.slots[1] // Slots are in blend order, farthest first.
	if bp.slots[0] != 0 {
		fixedSlot, cloudSlot = cloudSlot, fixedSlot
	}
	if fixedSlot.ranges != nil || fixedSlot.selected != 900 {
		t.Fatalf("unclustered slot: %d selected, %d ranges", fixedSlot.selected, len(fixedSlot.ranges))
	}
	_, wanted := plan(t, lodPerspective(100, cloud)) // What the cloud draws unshared.
	if cloudSlot.selected >= wanted.drawn {
		t.Fatalf("clustered slot drew %d, no fewer than the %d it draws alone", cloudSlot.selected, wanted.drawn)
	}
	if shared := cloudSlot.selected; shared > 100+len(cloud.Clusters.Clusters) || shared < 1 || lod.drawn != 900+shared || bp.points != lod.drawn {
		t.Fatalf("clustered slot got %d, drawn %d, plan points %d", shared, lod.drawn, bp.points)
	}
	if lod.resident != 900+4096 {
		t.Fatalf("resident %d", lod.resident)
	}
}

// clusterAt is the index of the cluster of g whose Order range starts at first.
func clusterAt(g Geometry, first uint32) int {
	for i, c := range g.Clusters.Clusters {
		if c.First == first {
			return i
		}
	}
	return -1
}

func TestLODDeterministic(t *testing.T) {
	cloud := lodCloud(t, 4096, 64)
	f := lodPerspective(100, cloud)
	f.SplatBudget = 500
	_, a := plan(t, f)
	_, b := plan(t, f)
	if !reflect.DeepEqual(a, b) || a.drawn == 0 {
		t.Fatalf("plans differ or are empty: %d, %d", a.drawn, b.drawn)
	}
	drawn, resident := f.SplatSelection()
	if drawn != a.drawn || resident != a.resident || resident != 4096 {
		t.Fatalf("selection %d of %d, plan %d of %d", drawn, resident, a.drawn, a.resident)
	}
	f.SplatBudget = -1
	if drawn, resident = f.SplatSelection(); drawn != resident || drawn != 4096 {
		t.Fatalf("unlimited selection %d of %d", drawn, resident)
	}
}

func TestLODPayload(t *testing.T) {
	index, scale := unpackLOD(packLOD(0xABCDEF, 37))
	if want := float32(1 / math.Sqrt(37.0/255)); index != 0xABCDEF || scale != want {
		t.Fatalf("got %#x, %v; want 0xABCDEF, %v", index, scale, want)
	}
	if _, scale = unpackLOD(packLOD(5, 0)); scale != float32(1/math.Sqrt(1.0/255)) {
		t.Fatalf("f8 0 gave scale %v", scale)
	}
	if index, scale = unpackLOD(packLOD(1<<24-1, 255)); index != 1<<24-1 || scale != 1 {
		t.Fatalf("got %#x, %v", index, scale)
	}
}

func TestSceneSplatBudget(t *testing.T) {
	s := NewScene()
	f, err := s.Frame(DefaultCamera(), 10, 10)
	check(t, err)
	if f.SplatBudget != 0 {
		t.Fatalf("default budget %d", f.SplatBudget)
	}
	before := s.Revision()
	s.SetSplatBudget(500)
	if s.Revision() == before {
		t.Fatal("budget change left the revision")
	}
	before = s.Revision()
	s.SetSplatBudget(500)
	if s.Revision() != before {
		t.Fatal("same budget bumped the revision")
	}
	if f, err = s.Frame(DefaultCamera(), 10, 10); err != nil || f.SplatBudget != 500 {
		t.Fatalf("frame budget %d, %v", f.SplatBudget, err)
	}
}
