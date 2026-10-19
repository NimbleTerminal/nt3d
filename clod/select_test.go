// Copyright (c) 2026 Neomantra Corp.
package nt3dclod_test

import (
	"math"
	"slices"
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/clod"
	"nimbleterminal.dev/nt3d/math"
)

// camera looks from +X toward the origin: its right is +Y, its up +Z.
func camera(projection nt3d.Projection, distance float64) nt3d.Camera {
	return nt3d.Camera{Alpha: 0, Beta: 0, Distance: distance, Projection: projection, ResumeAfter: 1}
}

// view is c's view of a width×height frame, with margin pixels of slack.
func view(c nt3d.Camera, width, height int, margin float32) nt3dclod.View {
	return nt3dclod.View{Matrix: c.Matrix(float32(width) / float32(height)), Eye: c.Eye(), Perspective: c.Projection == nt3d.Perspective, Width: width, Height: height, Margin: margin}
}

var levels = []nt3dclod.Level{{Range: 4, Fraction: 1}, {Range: 8, Fraction: .5}, {Range: 16, Fraction: .25}, {Fraction: 0}}

// knot returns n positions within .001 of center.
func knot(center nt3dmath.Vec3, n int) []nt3dmath.Vec3 {
	out := make([]nt3dmath.Vec3, n)
	for i := range out {
		out[i] = center.Add(nt3dmath.Vec3{X: .001 * float32(i%3), Y: .001 * float32(i%2), Z: .0005 * float32(i)})
	}
	return out
}

func build(t *testing.T, positions []nt3dmath.Vec3, size int) *nt3dclod.Set {
	t.Helper()
	s, err := nt3dclod.Build(positions, nt3dclod.Options{ClusterSize: size})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func sel(t *testing.T, s *nt3dclod.Set, v nt3dclod.View, levels []nt3dclod.Level) nt3dclod.Selection {
	t.Helper()
	var out nt3dclod.Selection
	if err := s.Select(v, levels, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// clusterAt is the index of the cluster whose center is nearest p.
func clusterAt(s *nt3dclod.Set, p nt3dmath.Vec3) int {
	best, bestD := -1, float32(math.Inf(1))
	for c, cl := range s.Clusters {
		d := cl.Center.Sub(p)
		if d.Dot(d) < bestD {
			best, bestD = c, d.Dot(d)
		}
	}
	return best
}

func TestSelectCullsClustersOutsideTheView(t *testing.T) {
	s := build(t, slices.Concat(knot(nt3dmath.Vec3{}, 5), knot(nt3dmath.Vec3{Z: 100}, 5)), 8)
	out := sel(t, s, view(camera(nt3d.Perspective, 3), 100, 80, 0), levels)
	c := clusterAt(s, nt3dmath.Vec3{})
	if len(out.Picks) != 1 || out.Picks[0].Cluster != uint32(c) || out.Picks[0].Level != 0 {
		t.Fatalf("picks %+v, want only cluster %d at level 0", out.Picks, c)
	}
	if !slices.Equal(out.Items, s.Items(c)) {
		t.Fatalf("items %v, want %v", out.Items, s.Items(c))
	}
}

func TestSelectMarginKeepsEdgeClusters(t *testing.T) {
	// An orthographic view two units wide: NDC x is Y, so Y = 1.01 is just
	// past the right edge.
	s := build(t, knot(nt3dmath.Vec3{Y: 1.01}, 4), 8)
	v := view(camera(nt3d.Orthographic, 2), 200, 200, 0)
	if out := sel(t, s, v, levels); len(out.Picks) != 0 {
		t.Fatalf("no margin: %d picks, want 0", len(out.Picks))
	}
	v.Margin = 4 // 4 of 200 pixels: 0.04 in NDC.
	if out := sel(t, s, v, levels); len(out.Picks) != 1 {
		t.Fatalf("margin 4: %d picks, want 1", len(out.Picks))
	}
}

func TestSelectDistanceLevels(t *testing.T) {
	// The eye is at X = 20; knots along the axis at 1, 6, 12, and 40 units.
	xs := []float32{19, 14, 8, -20}
	var positions []nt3dmath.Vec3
	for _, x := range xs {
		positions = append(positions, knot(nt3dmath.Vec3{X: x}, 7)...)
	}
	s := build(t, positions, 8)
	out := sel(t, s, view(camera(nt3d.Perspective, 20), 100, 100, 0), levels)
	if len(out.Picks) != 4 {
		t.Fatalf("%d picks, want 4", len(out.Picks))
	}
	wantCount := []uint32{7, 4, 2, 0}
	for i, x := range xs {
		c := clusterAt(s, nt3dmath.Vec3{X: x})
		k := slices.IndexFunc(out.Picks, func(p nt3dclod.Pick) bool { return p.Cluster == uint32(c) })
		if k < 0 {
			t.Fatalf("cluster at %v not picked", x)
		}
		p := out.Picks[k]
		if p.Level != uint32(i) || p.Count != wantCount[i] {
			t.Fatalf("cluster at %v: level %d count %d, want %d %d", x, p.Level, p.Count, i, wantCount[i])
		}
		if !slices.Equal(out.Items[p.First:p.First+p.Count], s.Items(c)[:p.Count]) {
			t.Fatalf("cluster at %v: items are not the cluster's prefix", x)
		}
	}
	for i := 1; i < len(out.Picks); i++ {
		if out.Picks[i].Cluster <= out.Picks[i-1].Cluster || out.Picks[i].First != out.Picks[i-1].First+out.Picks[i-1].Count {
			t.Fatalf("picks are not in cluster order with cumulative offsets: %+v", out.Picks)
		}
	}
	if len(out.Items) != 13 {
		t.Fatalf("%d items, want 13", len(out.Items))
	}
}

func TestSelectOrthographicUsesViewSize(t *testing.T) {
	s := build(t, knot(nt3dmath.Vec3{}, 6), 8)
	for _, c := range []struct {
		distance float64
		level    uint32
	}{{6, 1}, {20, 3}, {3, 0}} {
		out := sel(t, s, view(camera(nt3d.Orthographic, c.distance), 100, 100, 0), levels)
		if len(out.Picks) != 1 || out.Picks[0].Level != c.level {
			t.Fatalf("distance %v: %+v, want level %d", c.distance, out.Picks, c.level)
		}
	}
}

func TestSelectEyeInsideCluster(t *testing.T) {
	var positions []nt3dmath.Vec3
	for i := range 8 {
		positions = append(positions, nt3dmath.Vec3{X: float32(i&1)*4 - 2, Y: float32(i>>1&1)*4 - 2, Z: float32(i>>2)*4 - 2})
	}
	s := build(t, positions, 8)
	out := sel(t, s, view(camera(nt3d.Perspective, .5), 100, 100, 0), levels)
	if len(out.Picks) != 1 || out.Picks[0].Level != 0 || out.Picks[0].Count != 8 {
		t.Fatalf("%+v, want level 0 with 8 items", out.Picks)
	}
}

func TestSelectTinyFractionListsOne(t *testing.T) {
	s := build(t, knot(nt3dmath.Vec3{}, 100), 100)
	out := sel(t, s, view(camera(nt3d.Perspective, 5), 100, 100, 0), []nt3dclod.Level{{Fraction: 1e-9}})
	if len(out.Picks) != 1 || out.Picks[0].Count != 1 || len(out.Items) != 1 || out.Items[0] != s.Items(0)[0] {
		t.Fatalf("%+v %v", out.Picks, out.Items)
	}
}

func TestSelectRejects(t *testing.T) {
	s := build(t, knot(nt3dmath.Vec3{}, 5), 8)
	good := view(camera(nt3d.Perspective, 5), 100, 100, 0)
	nan := good
	nan.Matrix[3] = float32(math.NaN())
	narrow := good
	narrow.Width = 0
	nine := make([]nt3dclod.Level, 9)
	for i := range nine {
		nine[i] = nt3dclod.Level{Range: float32(i + 1), Fraction: 1}
	}
	for name, c := range map[string]struct {
		v      nt3dclod.View
		levels []nt3dclod.Level
	}{
		"no levels":   {good, nil},
		"nine levels": {good, nine},
		"descending":  {good, []nt3dclod.Level{{Range: 8, Fraction: 1}, {Range: 4, Fraction: 1}, {Fraction: 0}}},
		"fraction":    {good, []nt3dclod.Level{{Fraction: 1.5}}},
		"width":       {narrow, levels},
		"matrix":      {nan, levels},
	} {
		out := nt3dclod.Selection{Picks: make([]nt3dclod.Pick, 3)}
		if err := s.Select(c.v, c.levels, &out); err == nil || len(out.Picks) != 3 {
			t.Fatalf("%s: err %v, picks %d", name, err, len(out.Picks))
		}
	}
}

func TestSelectReusesOutput(t *testing.T) {
	s := build(t, knot(nt3dmath.Vec3{}, 50), 100)
	v := view(camera(nt3d.Perspective, 5), 100, 100, 0)
	var out nt3dclod.Selection
	if err := s.Select(v, levels, &out); err != nil {
		t.Fatal(err)
	}
	picks, items := &out.Picks[0], &out.Items[0]
	if err := s.Select(v, levels, &out); err != nil {
		t.Fatal(err)
	}
	if &out.Picks[0] != picks || &out.Items[0] != items {
		t.Fatal("a second select reallocated")
	}
}

func TestGather(t *testing.T) {
	got := nt3dclod.Gather([]int{9, 9}[:0], []int{10, 20, 30}, []uint32{2, 0})
	if !slices.Equal(got, []int{30, 10}) {
		t.Fatal(got)
	}
}
