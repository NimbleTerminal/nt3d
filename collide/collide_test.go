// Copyright (c) 2026 Neomantra Corp.

package nt3dcollide

import (
	"math"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// mustMove is MoveCapsule for input the test knows to be valid.
func mustMove(t *testing.T, m *Mesh, cap Capsule, pos, vel nt3dmath.Vec3d, dt float64, wasGrounded bool, cfg MoveConfig) MoveResult {
	t.Helper()
	res, err := MoveCapsule(m, cap, pos, vel, dt, wasGrounded, cfg)
	if err != nil {
		t.Fatalf("MoveCapsule: %v", err)
	}
	return res
}

func quadTris(x0, y0, x1, y1, z float64) []Triangle {
	a := nt3dmath.Vec3d{X: x0, Y: y0, Z: z}
	b := nt3dmath.Vec3d{X: x1, Y: y0, Z: z}
	c := nt3dmath.Vec3d{X: x1, Y: y1, Z: z}
	d := nt3dmath.Vec3d{X: x0, Y: y1, Z: z}
	return []Triangle{{A: a, B: b, C: c}, {A: a, B: c, C: d}}
}

func TestCapsuleLandsAndStaysAboveFloor(t *testing.T) {
	m := NewMeshFromTriangles(quadTris(-20, -20, 20, 20, 0))
	cap := Capsule{Radius: 0.35, Height: 1.8}
	cfg := MoveConfig{MaxSlopeDeg: 50, StepHeight: 0.35, GroundSnap: 0.15}

	pos := nt3dmath.Vec3d{Z: 3.0}
	vel := nt3dmath.Vec3d{}
	grounded := false
	const dt = 1.0 / 120.0
	for range 240 {
		vel.Z -= 9.81 * dt
		res := mustMove(t, m, cap, pos, vel, dt, grounded, cfg)
		pos, vel, grounded = res.Position, res.Velocity, res.Grounded
	}
	if !grounded {
		t.Fatalf("capsule did not land: pos=%v vel=%v", pos, vel)
	}
	if math.Abs(pos.Z) > 0.02 {
		t.Fatalf("landed at Z=%.4f, want ~0", pos.Z)
	}
}

func TestCapsuleSlidesAlongWallWithoutTunneling(t *testing.T) {
	tris := quadTris(-20, -20, 20, 20, 0)
	// Vertical wall at X = 2.0 running along Y in [-10, 10], Z in [0, 5].
	w0 := nt3dmath.Vec3d{X: 2, Y: -10, Z: 0}
	w1 := nt3dmath.Vec3d{X: 2, Y: 10, Z: 0}
	w2 := nt3dmath.Vec3d{X: 2, Y: 10, Z: 5}
	w3 := nt3dmath.Vec3d{X: 2, Y: -10, Z: 5}
	tris = append(tris, Triangle{A: w0, B: w1, C: w2}, Triangle{A: w0, B: w2, C: w3})
	m := NewMeshFromTriangles(tris)

	cap := Capsule{Radius: 0.4, Height: 1.8}
	cfg := MoveConfig{MaxSlopeDeg: 50, StepHeight: 0.35, GroundSnap: 0.15}

	// Charge diagonally into the wall at 20 m/s over a coarse 0.1 s step.
	res := mustMove(t, m, cap, nt3dmath.Vec3d{X: 0.5, Y: 0, Z: 0.005}, nt3dmath.Vec3d{X: 20, Y: 10, Z: 0}, 0.1, true, cfg)
	if !res.HitWall {
		t.Fatalf("expected HitWall, got %+v", res)
	}
	if res.Position.X > 2.0-cap.Radius+0.02 {
		t.Fatalf("capsule penetrated wall: X=%.4f (wall at 2.0, radius %.2f)", res.Position.X, cap.Radius)
	}
	if res.Position.Y < 0.5 {
		t.Fatalf("capsule failed to slide along Y: Y=%.4f", res.Position.Y)
	}
	if res.ImpactSpeed < 19.0 {
		t.Fatalf("ImpactSpeed=%.2f, want ~20", res.ImpactSpeed)
	}
}

func TestCapsuleStepsUpCurbAndSnapsDownSlope(t *testing.T) {
	// Ground at Z=0 for X < 1, curb riser at X=1 up to Z=0.25, then gentle
	// downhill ramp from (X=1, Z=0.25) to (X=6, Z=0).
	var tris []Triangle
	tris = append(tris, quadTris(-5, -5, 1, 5, 0)...)
	// Riser at X=1.
	r0 := nt3dmath.Vec3d{X: 1, Y: -5, Z: 0}
	r1 := nt3dmath.Vec3d{X: 1, Y: 5, Z: 0}
	r2 := nt3dmath.Vec3d{X: 1, Y: 5, Z: 0.25}
	r3 := nt3dmath.Vec3d{X: 1, Y: -5, Z: 0.25}
	tris = append(tris, Triangle{A: r0, B: r1, C: r2}, Triangle{A: r0, B: r2, C: r3})
	// Downhill ramp X in [1, 6].
	s0 := nt3dmath.Vec3d{X: 1, Y: -5, Z: 0.25}
	s1 := nt3dmath.Vec3d{X: 6, Y: -5, Z: 0}
	s2 := nt3dmath.Vec3d{X: 6, Y: 5, Z: 0}
	s3 := nt3dmath.Vec3d{X: 1, Y: 5, Z: 0.25}
	tris = append(tris, Triangle{A: s0, B: s1, C: s2}, Triangle{A: s0, B: s2, C: s3})
	m := NewMeshFromTriangles(tris)

	cap := Capsule{Radius: 0.35, Height: 1.8}
	cfg := MoveConfig{MaxSlopeDeg: 50, StepHeight: 0.35, GroundSnap: 0.2}
	pos := nt3dmath.Vec3d{X: 0, Y: 0, Z: 0.005}
	grounded := true
	const dt = 1.0 / 60.0
	for range 60 {
		res := mustMove(t, m, cap, pos, nt3dmath.Vec3d{X: 5, Y: 0, Z: 0}, dt, grounded, cfg)
		pos, grounded = res.Position, res.Grounded
		if !grounded {
			t.Fatalf("lost ground contact on curb/ramp at pos=%v", pos)
		}
	}
	if pos.X < 3.5 {
		t.Fatalf("blocked by 0.25 m curb: X=%.3f", pos.X)
	}
}

// A capsule running up a ramp reports the ramp's normal as its ground normal,
// although it is rising and the downward ground probe does not run.
func TestGroundNormalIsTheRampWhenRunningUpIt(t *testing.T) {
	// A ramp rising 1 in 2 along X, wide in Y.
	a := nt3dmath.Vec3d{X: -10, Y: -10, Z: -5}
	b := nt3dmath.Vec3d{X: 10, Y: -10, Z: 5}
	c := nt3dmath.Vec3d{X: 10, Y: 10, Z: 5}
	d := nt3dmath.Vec3d{X: -10, Y: 10, Z: -5}
	m := NewMeshFromTriangles([]Triangle{{A: a, B: b, C: c}, {A: a, B: c, C: d}})
	l := math.Sqrt(5)
	want := nt3dmath.Vec3d{X: -1 / l, Z: 2 / l}

	cap := Capsule{Radius: 0.35, Height: 1.8}
	// Running horizontally into the slope: the ramp turns that into a climb.
	res := mustMove(t, m, cap, nt3dmath.Vec3d{Z: 0.05}, nt3dmath.Vec3d{X: 4, Z: 0.5}, 1.0/60, true, MoveConfig{})
	if !res.Grounded {
		t.Fatalf("capsule on the ramp is not grounded: %+v", res)
	}
	if got := res.GroundNormal; math.Abs(got.X-want.X) > 1e-6 || math.Abs(got.Y) > 1e-6 || math.Abs(got.Z-want.Z) > 1e-6 {
		t.Fatalf("GroundNormal = %v, want the ramp's %v", got, want)
	}
}
