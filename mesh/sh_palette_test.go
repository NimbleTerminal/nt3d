// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import (
	"cmp"
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"os"
	"slices"
	"testing"
	"time"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/sh"
)

// randomCloud makes a valid Gaussian geometry of n splats at the given SH
// degree. With k > 0 the bands above DC take k distinct random sets (splat i
// has set i%k); with k == 0 every value is random. DC is always random.
func randomCloud(n, degree int, k int) nt3d.Geometry {
	rng := rand.New(rand.NewPCG(1, 2))
	value := func() float32 { return rng.Float32() - .5 }
	count := nt3dsh.Count(degree)
	sets := make([][]nt3dsh.RGB, k)
	for s := range sets {
		sets[s] = make([]nt3dsh.RGB, count-1)
		for j := range sets[s] {
			sets[s][j] = nt3dsh.RGB{value(), value(), value()}
		}
	}
	g := nt3d.Geometry{PointStyle: nt3d.PointStyle{Shape: nt3d.GaussianPoint, Units: nt3d.WorldUnits, Blend: nt3d.AlphaBlend}}
	g.Points = make([]nt3d.Point, n)
	g.GaussianSH = make([]nt3dsh.RGB, 0, n*count)
	for i := range n {
		g.Points[i] = nt3d.Point{Position: nt3dmath.Vec3{X: value(), Y: value(), Z: value()}, Radius: 1, GaussianAxes: [3]nt3dmath.Vec3{{X: .01}, {Y: .01}, {Z: .01}}}
		g.GaussianSH = append(g.GaussianSH, nt3dsh.RGB{value(), value(), value()})
		if k > 0 {
			g.GaussianSH = append(g.GaussianSH, sets[i%k]...)
			continue
		}
		for range count - 1 {
			g.GaussianSH = append(g.GaussianSH, nt3dsh.RGB{value(), value(), value()})
		}
	}
	g.Bounds = nt3d.GeometryBounds(g)
	return g
}

func TestBuildSHPaletteExact(t *testing.T) {
	g := randomCloud(5000, 3, 300)
	in := slices.Clone(g.GaussianSH)
	rep, err := BuildSHPalette(context.Background(), &g, PaletteOptions{})
	check(t, err)
	if rep.Centroids != 300 || rep.RMSError != 0 || !rep.MetBudget || g.GaussianSHPalette == nil || g.GaussianSHPalette.Count() != 300 || len(g.GaussianSHLabels) != 5000 || len(g.GaussianSH) != 5000 {
		t.Fatalf("report %+v, count %d", rep, g.GaussianSHPalette.Count())
	}
	check(t, g.Validate())
	e := g
	nt3d.ExpandSHPalette(&e)
	if !slices.Equal(e.GaussianSH, in) {
		t.Fatal("exact round trip lost coefficients")
	}
	// Building again is a no-op with the same report.
	again, err := BuildSHPalette(context.Background(), &g, PaletteOptions{})
	check(t, err)
	if again != rep {
		t.Fatalf("second build reported %+v", again)
	}
}

func TestBuildSHPaletteTiny(t *testing.T) {
	for _, n := range []int{1, 2, 7} {
		g := randomCloud(n, 2, 1) // one repeated set
		rep, err := BuildSHPalette(context.Background(), &g, PaletteOptions{Centroids: 4})
		check(t, err)
		if rep.Centroids != 1 || rep.RMSError != 0 {
			t.Fatalf("n %d: %+v", n, rep)
		}
		check(t, g.Validate())
	}
}

func TestBuildSHPaletteApproximates(t *testing.T) {
	g := randomCloud(20000, 3, 0)
	one, ten := g, g
	r1, err := BuildSHPalette(context.Background(), &one, PaletteOptions{Centroids: 512, Iterations: 1})
	check(t, err)
	r10, err := BuildSHPalette(context.Background(), &ten, PaletteOptions{Centroids: 512, Iterations: 10})
	check(t, err)
	if r10.RMSError > r1.RMSError || r10.RMSError <= 0 || r10.Centroids > 512 {
		t.Fatalf("1 iteration %+v, 10 iterations %+v", r1, r10)
	}
	// Below the trivial bound: the RMS of the coefficients themselves.
	var sum float64
	for _, c := range g.GaussianSH[1:] { // crude: includes every 16th DC; fine as a bound
		for _, v := range c {
			sum += float64(v * v)
		}
	}
	if float64(r10.RMSError) >= math.Sqrt(sum/float64(3*len(g.GaussianSH))) {
		t.Fatal("no better than a single zero centroid")
	}
	// Budget reporting.
	rb, err := BuildSHPalette(context.Background(), &g, PaletteOptions{Centroids: 512, MaxRMSError: 1e-6})
	check(t, err)
	if rb.MetBudget {
		t.Fatal("met an impossible budget")
	}
}

func TestBuildSHPaletteDeterministicAndSafe(t *testing.T) {
	g := randomCloud(20000, 3, 0)
	in := slices.Clone(g.GaussianSH)
	a, b := g, g
	var fractions []float64
	ra, err := BuildSHPalette(context.Background(), &a, PaletteOptions{Centroids: 1024, Progress: func(f float64) { fractions = append(fractions, f) }})
	check(t, err)
	rb, err := BuildSHPalette(context.Background(), &b, PaletteOptions{Centroids: 1024})
	check(t, err)
	if ra != rb || !slices.Equal(a.GaussianSHLabels, b.GaussianSHLabels) || !slices.Equal(a.GaussianSHPalette.Coefficients, b.GaussianSHPalette.Coefficients) {
		t.Fatal("two builds differ")
	}
	if len(fractions) < 2 || fractions[len(fractions)-1] != 1 || !slices.IsSortedFunc(fractions, func(x, y float64) int { return cmp.Compare(x, y) }) {
		t.Fatalf("progress %v", fractions)
	}
	if !slices.Equal(g.GaussianSH, in) || len(g.GaussianSH) != 16*20000 || g.GaussianSHPalette != nil {
		t.Fatal("the input geometry's slices changed")
	}
	// Cancellation leaves the geometry untouched.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := g
	if _, err := BuildSHPalette(ctx, &c, PaletteOptions{}); !errors.Is(err, context.Canceled) || c.GaussianSHPalette != nil || len(c.GaussianSH) != 16*20000 {
		t.Fatalf("canceled build: %v", err)
	}
	// Degree 0 and non-Gaussian inputs error.
	d0 := randomCloud(10, 0, 1)
	if _, err := BuildSHPalette(context.Background(), &d0, PaletteOptions{}); err == nil {
		t.Fatal("accepted degree 0")
	}
	if _, err := BuildSHPalette(context.Background(), &nt3d.Geometry{}, PaletteOptions{}); err == nil {
		t.Fatal("accepted an empty geometry")
	}
	if _, err := BuildSHPalette(context.Background(), &g, PaletteOptions{Centroids: nt3d.MaxSHPaletteCentroids + 1}); err == nil {
		t.Fatal("accepted too many centroids")
	}
}

// TestBuildSHPaletteMillion times the default build on a million random
// degree-3 splats. It runs only when NT3D_BENCH=1.
func TestBuildSHPaletteMillion(t *testing.T) {
	if os.Getenv("NT3D_BENCH") != "1" {
		t.Skip("set NT3D_BENCH=1 to time the palette builder")
	}
	g := randomCloud(1_000_000, 3, 0)
	start := time.Now()
	rep, err := BuildSHPalette(context.Background(), &g, PaletteOptions{})
	check(t, err)
	t.Logf("1M degree-3 splats: %.2f s, %d centroids, RMS error %.4f", time.Since(start).Seconds(), rep.Centroids, rep.RMSError)
}

// The palette does not depend on the number of workers.
func TestBuildSHPaletteWorkers(t *testing.T) {
	g := randomCloud(20000, 2, 0)
	one, many := g, g
	r1, err := BuildSHPalette(context.Background(), &one, PaletteOptions{Centroids: 700, Workers: 1})
	check(t, err)
	r7, err := BuildSHPalette(context.Background(), &many, PaletteOptions{Centroids: 700, Workers: 7})
	check(t, err)
	if r1 != r7 || !slices.Equal(one.GaussianSHLabels, many.GaussianSHLabels) || !slices.Equal(one.GaussianSHPalette.Coefficients, many.GaussianSHPalette.Coefficients) {
		t.Fatalf("1 worker %+v, 7 workers %+v", r1, r7)
	}
	check(t, one.Validate())
}
