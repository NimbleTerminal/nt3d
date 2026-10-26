//go:build webgpu

package nt3d

import (
	"image/color"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	nt3dclod "nimbleterminal.dev/nt3d/clod"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// slabSplats is count synthetic splats in a 10 by 10 by 2 slab: uniform
// positions, small axes, about half opaque, varied colors.
func slabSplats(count int) Geometry {
	rng := rand.New(rand.NewPCG(7, 11))
	points := make([]Point, count)
	for i := range points {
		points[i] = Point{
			Position: nt3dmath.Vec3{X: rng.Float32()*10 - 5, Y: rng.Float32()*10 - 5, Z: rng.Float32()*2 - 1},
			Radius:   1,
			Color:    color.RGBA{uint8(rng.UintN(256)), uint8(rng.UintN(256)), uint8(rng.UintN(256)), 128},
			GaussianAxes: [3]nt3dmath.Vec3{
				{X: .02}, {Y: .02}, {Z: .02},
			},
		}
	}
	return Geometry{PointStyle: PointStyle{Shape: GaussianPoint, Units: WorldUnits, Blend: AlphaBlend}, Points: points}
}

// benchSplats renders count splats five times from inside the slab and
// returns the mean of the last four frames, in milliseconds.
func benchSplats(t *testing.T, count int) float64 {
	t.Helper()
	ms, sorted := benchGeometry(t, slabSplats(count))
	if sorted != count {
		t.Fatalf("%d points sorted, want %d", sorted, count)
	}
	return ms
}

// benchGeometry renders g five times from inside the slab under the default
// splat budget and returns the mean of the last four frames, in
// milliseconds, and how many points the sort keyed.
func benchGeometry(t *testing.T, g Geometry) (float64, int) {
	t.Helper()
	s := NewScene()
	_, err := s.SetGeometry("slab", g)
	check(t, err)
	camera := DefaultCamera()
	camera.Projection, camera.Target, camera.Distance, camera.Alpha, camera.Beta = Perspective, nt3dmath.Vec3{}, 3, 10, 30
	f, err := s.Frame(camera, 640, 360)
	check(t, err)
	r := &gpuRenderer{}
	defer r.Close()
	var total time.Duration
	for i := range 5 {
		start := time.Now()
		_, err := r.Render(f)
		elapsed := time.Since(start)
		check(t, err)
		if i > 0 {
			total += elapsed
		}
	}
	return float64(total.Microseconds()) / 4000, r.plan.points
}

// TestGPUSortMillion times a frame of a million blended splats from inside
// the slab: a whole Render, sort, draw, and readback to the CPU. It runs
// only when NT3D_GPU_BENCH=1.
func TestGPUSortMillion(t *testing.T) {
	if os.Getenv("NT3D_GPU_BENCH") != "1" {
		t.Skip("set NT3D_GPU_BENCH=1 to time the sort")
	}
	t.Logf("1M splats: %.1f ms per frame (a whole Render: sort, draw, and readback to the CPU)", benchSplats(t, 1_000_000))
	// MaxGaussians(0) is 2.8M for one geometry: 3M do not fit.
	t.Logf("2.5M splats (the most one geometry holds, rounded down; 3M do not fit): %.1f ms per frame (a whole Render: sort, draw, and readback to the CPU)", benchSplats(t, 2_500_000))
}

// TestGPULODBudgeted times the 2.5M slab clustered at load and drawn under
// the default splat budget, from the same camera as TestGPUSortMillion. It
// runs only when NT3D_GPU_BENCH=1.
func TestGPULODBudgeted(t *testing.T) {
	if os.Getenv("NT3D_GPU_BENCH") != "1" {
		t.Skip("set NT3D_GPU_BENCH=1 to time the budgeted sort")
	}
	g := slabSplats(2_500_000)
	positions := make([]nt3dmath.Vec3, len(g.Points))
	for i, p := range g.Points {
		positions[i] = p.Position
	}
	start := time.Now()
	set, err := nt3dclod.Build(positions, nt3dclod.Options{})
	check(t, err)
	t.Logf("2.5M splats clustered in %.0f ms (%d clusters)", float64(time.Since(start).Microseconds())/1000, len(set.Clusters))
	g.Clusters = set
	ms, sorted := benchGeometry(t, g)
	if sorted == 0 || sorted > DefaultSplatBudget+len(set.Clusters) {
		t.Fatalf("%d splats sorted under a budget of %d", sorted, DefaultSplatBudget)
	}
	t.Logf("2.5M splats at the default budget (%d drawn): %.1f ms per frame (a whole Render: sort, draw, and readback to the CPU)", sorted, ms)
}
