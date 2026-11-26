// Copyright (c) 2026 Neomantra Corp.

package nt3dcollide

import (
	"math"
	"math/rand/v2"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// roomTris is a closed box room spanning lo..hi, two triangles a face.
func roomTris(lo, hi nt3dmath.Vec3d) []Triangle {
	v := func(x, y, z float64) nt3dmath.Vec3d { return nt3dmath.Vec3d{X: x, Y: y, Z: z} }
	quad := func(a, b, c, d nt3dmath.Vec3d) []Triangle {
		return []Triangle{{A: a, B: b, C: c}, {A: a, B: c, C: d}}
	}
	var out []Triangle
	out = append(out, quad(v(lo.X, lo.Y, lo.Z), v(hi.X, lo.Y, lo.Z), v(hi.X, hi.Y, lo.Z), v(lo.X, hi.Y, lo.Z))...) // floor
	out = append(out, quad(v(lo.X, lo.Y, hi.Z), v(hi.X, lo.Y, hi.Z), v(hi.X, hi.Y, hi.Z), v(lo.X, hi.Y, hi.Z))...) // ceiling
	out = append(out, quad(v(lo.X, lo.Y, lo.Z), v(lo.X, hi.Y, lo.Z), v(lo.X, hi.Y, hi.Z), v(lo.X, lo.Y, hi.Z))...) // west
	out = append(out, quad(v(hi.X, lo.Y, lo.Z), v(hi.X, hi.Y, lo.Z), v(hi.X, hi.Y, hi.Z), v(hi.X, lo.Y, hi.Z))...) // east
	out = append(out, quad(v(lo.X, lo.Y, lo.Z), v(hi.X, lo.Y, lo.Z), v(hi.X, lo.Y, hi.Z), v(lo.X, lo.Y, hi.Z))...) // south
	out = append(out, quad(v(lo.X, hi.Y, lo.Z), v(hi.X, hi.Y, lo.Z), v(hi.X, hi.Y, hi.Z), v(lo.X, hi.Y, hi.Z))...) // north
	return out
}

// Wrong input is an error, and the result then holds the pose as given: a
// caller that ignores the error does not find its capsule moved to the origin.
func TestMoveCapsuleRejectsWrongInput(t *testing.T) {
	m := NewMeshFromTriangles(quadTris(-5, -5, 5, 5, 0))
	good := Capsule{Radius: 0.4, Height: 1.8}
	pos := nt3dmath.Vec3d{X: 1, Y: 2, Z: 3}
	vel := nt3dmath.Vec3d{X: 1}
	nan, inf := math.NaN(), math.Inf(1)
	for name, tc := range map[string]struct {
		cap Capsule
		pos nt3dmath.Vec3d
		vel nt3dmath.Vec3d
		dt  float64
		cfg MoveConfig
	}{
		"zero radius":         {Capsule{Radius: 0, Height: 1.8}, pos, vel, 0.01, MoveConfig{}},
		"negative radius":     {Capsule{Radius: -1, Height: 1.8}, pos, vel, 0.01, MoveConfig{}},
		"NaN radius":          {Capsule{Radius: nan, Height: 1.8}, pos, vel, 0.01, MoveConfig{}},
		"height under 2r":     {Capsule{Radius: 0.4, Height: 0.5}, pos, vel, 0.01, MoveConfig{}},
		"infinite height":     {Capsule{Radius: 0.4, Height: inf}, pos, vel, 0.01, MoveConfig{}},
		"NaN position":        {good, nt3dmath.Vec3d{X: nan}, vel, 0.01, MoveConfig{}},
		"infinite position":   {good, nt3dmath.Vec3d{Z: inf}, vel, 0.01, MoveConfig{}},
		"NaN velocity":        {good, pos, nt3dmath.Vec3d{Y: nan}, 0.01, MoveConfig{}},
		"negative dt":         {good, pos, vel, -0.01, MoveConfig{}},
		"NaN dt":              {good, pos, vel, nan, MoveConfig{}},
		"infinite dt":         {good, pos, vel, inf, MoveConfig{}},
		"slope over 89":       {good, pos, vel, 0.01, MoveConfig{MaxSlope: 90}},
		"negative step":       {good, pos, vel, 0.01, MoveConfig{StepHeight: -0.1}},
		"NaN ground snap":     {good, pos, vel, 0.01, MoveConfig{GroundSnap: nan}},
		"negative skin":       {good, pos, vel, 0.01, MoveConfig{SkinWidth: -1}},
		"move past the limit": {good, pos, nt3dmath.Vec3d{X: 1e9}, 1, MoveConfig{}},
	} {
		res, err := MoveCapsule(m, tc.cap, tc.pos, tc.vel, tc.dt, false, tc.cfg)
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		same := func(a, b nt3dmath.Vec3d) bool {
			eq := func(x, y float64) bool { return x == y || (math.IsNaN(x) && math.IsNaN(y)) }
			return eq(a.X, b.X) && eq(a.Y, b.Y) && eq(a.Z, b.Z)
		}
		if !same(res.Position, tc.pos) || !same(res.Velocity, tc.vel) {
			t.Errorf("%s: result moved to %v with velocity %v, want the input back", name, res.Position, res.Velocity)
		}
	}
}

// A zero dt is no move and no error; a nil or empty mesh is empty space, where
// the capsule moves by vel*dt.
func TestMoveCapsuleZeroDtAndEmptySpace(t *testing.T) {
	cap := Capsule{Radius: 0.4, Height: 1.8}
	pos := nt3dmath.Vec3d{X: 1, Y: 2, Z: 3}
	vel := nt3dmath.Vec3d{X: 4, Y: -2, Z: 1}
	res, err := MoveCapsule(NewMeshFromTriangles(quadTris(-5, -5, 5, 5, 0)), cap, pos, vel, 0, false, MoveConfig{})
	if err != nil || res.Position != pos || res.Velocity != vel {
		t.Fatalf("dt 0 = %+v, %v; want the pose unchanged", res, err)
	}
	want := nt3dmath.Vec3d{X: 3, Y: 1, Z: 3.5}
	for name, m := range map[string]*Mesh{"nil": nil, "empty": NewMeshFromTriangles(nil)} {
		res, err := MoveCapsule(m, cap, pos, vel, 0.5, false, MoveConfig{})
		if err != nil || res.Position != want || res.Velocity != vel || res.Grounded || res.HitWall {
			t.Fatalf("%s mesh = %+v, %v; want a free move to %v", name, res, err, want)
		}
	}
}

// Validate accepts the zero configuration, whose fields mean their defaults.
func TestZeroMoveConfigIsValid(t *testing.T) {
	if err := (MoveConfig{}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Capsule{Radius: 0.4, Height: 0.8}).Validate(); err != nil {
		t.Fatalf("a sphere-shaped capsule (height = 2r): %v", err)
	}
}

// However fast it goes and however long the step, a capsule inside a closed
// room stays inside it: the move is cut into substeps too short to cross a
// wall.
func TestCapsuleNeverLeavesAClosedRoom(t *testing.T) {
	lo, hi := nt3dmath.Vec3d{X: -6, Y: -5, Z: 0}, nt3dmath.Vec3d{X: 6, Y: 5, Z: 4}
	m := NewMeshFromTriangles(roomTris(lo, hi))
	cap := Capsule{Radius: 0.38, Height: 1.75}
	cfg := MoveConfig{MaxSlope: 48, StepHeight: 0.32, GroundSnap: 0.22}
	rng := rand.New(rand.NewPCG(1, 2))
	pos := nt3dmath.Vec3d{Z: 1}
	grounded := false
	for i := range 4000 {
		speed := 300 * rng.Float64() * rng.Float64()
		az, el := 2*math.Pi*rng.Float64(), math.Pi*(rng.Float64()-0.5)
		vel := nt3dmath.Vec3d{
			X: speed * math.Cos(el) * math.Cos(az),
			Y: speed * math.Cos(el) * math.Sin(az),
			Z: speed * math.Sin(el),
		}
		dt := 0.25 * rng.Float64()
		res, err := MoveCapsule(m, cap, pos, vel, dt, grounded, cfg)
		if err != nil {
			t.Fatalf("move %d: %v", i, err)
		}
		p := res.Position
		const slack = 1e-6
		if p.X < lo.X+cap.Radius-slack || p.X > hi.X-cap.Radius+slack ||
			p.Y < lo.Y+cap.Radius-slack || p.Y > hi.Y-cap.Radius+slack ||
			p.Z < lo.Z-slack || p.Z > hi.Z-cap.Height+slack {
			t.Fatalf("move %d: from %v at %v m/s for %v s the capsule ended at %v, outside the room", i, pos, vel, dt, p)
		}
		pos, grounded = p, res.Grounded
	}
}

// A capsule at rest on a floor under gravity stays put: it neither sinks into
// the floor nor creeps sideways.
func TestRestingCapsuleDoesNotSinkOrDrift(t *testing.T) {
	m := NewMeshFromTriangles(quadTris(-20, -20, 20, 20, 0))
	cap := Capsule{Radius: 0.38, Height: 1.75}
	cfg := MoveConfig{MaxSlope: 48, StepHeight: 0.32, GroundSnap: 0.22}
	pos := nt3dmath.Vec3d{X: 1.25, Y: -3.5, Z: 0.5}
	var vel nt3dmath.Vec3d
	grounded := false
	const dt = 1.0 / 120
	for i := range 1200 {
		vel.Z -= 9.81 * dt
		res := mustMove(t, m, cap, pos, vel, dt, grounded, cfg)
		pos, vel, grounded = res.Position, res.Velocity, res.Grounded
		if pos.Z < -1e-9 {
			t.Fatalf("step %d: the capsule's base is at Z=%v, below the floor", i, pos.Z)
		}
	}
	if !grounded || pos.Z > 0.02 {
		t.Fatalf("after 10 s: grounded=%v at Z=%v, want resting on the floor", grounded, pos.Z)
	}
	if pos.X != 1.25 || pos.Y != -3.5 {
		t.Fatalf("after 10 s the capsule drifted to (%v, %v)", pos.X, pos.Y)
	}
}
