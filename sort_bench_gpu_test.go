//go:build webgpu

package nt3d

import (
	"image/color"
	"math/rand/v2"
	"os"
	"testing"
	"time"

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
	s := NewScene()
	_, err := s.SetGeometry("slab", slabSplats(count))
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
	if r.plan.points != count {
		t.Fatalf("%d points sorted, want %d", r.plan.points, count)
	}
	return float64(total.Microseconds()) / 4000
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
