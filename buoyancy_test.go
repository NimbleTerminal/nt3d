package nt3d

import (
	"errors"
	"math"
	ntmath "nimbleterminal.dev/nt3d/math"
	"testing"
)

type testWater struct {
	velocity ntmath.Vec3
	fail     bool
}

func (s testWater) AtWorld(x, y, time float64) (WaveSample, error) {
	if s.fail {
		return WaveSample{}, errors.New("sample failed")
	}
	return WaveSample{Position: ntmath.Vec3{X: float32(x), Y: float32(y)}, Normal: ntmath.Vec3{Z: 1}, Velocity: s.velocity}, nil
}
func testHull(t *testing.T) *BuoyantHull {
	t.Helper()
	h, e := NewBuoyantHull(BuoyantHullConfig{Density: 1000, Gravity: 10, Points: []BuoyancyPoint{{Position: ntmath.Vec3{X: 1}, Volume: .2, Height: 1, PlaningArea: .5}}, DragArea: ntmath.Vec3{X: 1, Y: 1, Z: 1}, DragCoefficient: ntmath.Vec3{X: 1, Y: 1, Z: 1}, PlaningCoefficient: .2, PlaningSpeed: 2})
	if e != nil {
		t.Fatal(e)
	}
	return h
}
func waterIdentity() ntmath.Mat4 { return ntmath.Mat4{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1} }
func TestBuoyancyLoads(t *testing.T) {
	h := testHull(t)
	state := WaterBodyState{Transform: waterIdentity()}
	f, e := h.Forces(testWater{}, state, 0)
	if e != nil {
		t.Fatal(e)
	}
	if math.Abs(float64(f.Force.Z-1000)) > 1e-3 || f.Torque.Y != -f.Force.Z || f.WetFraction != .5 {
		t.Fatal(f)
	}
	state.LinearVelocity = ntmath.Vec3{X: 4, Y: 1, Z: -.5}
	f, e = h.Forces(testWater{}, state, 0)
	if e != nil {
		t.Fatal(e)
	}
	if f.Drag.Dot(state.LinearVelocity) >= 0 || f.Planing.Z <= 0 || math.Abs(float64(f.Planing.Dot(state.LinearVelocity))) > .001 {
		t.Fatal("invalid drag/lift work", f)
	}
	f, e = h.Forces(testWater{velocity: state.LinearVelocity}, state, 0)
	if e != nil {
		t.Fatal(e)
	}
	if f.Drag != (ntmath.Vec3{}) || f.Planing != (ntmath.Vec3{}) {
		t.Fatal("not relative to water", f)
	}
	state.Transform[14] = 2
	f, e = h.Forces(testWater{}, state, 0)
	if e != nil || f != (WaterForces{}) {
		t.Fatal("dry hull has loads", f, e)
	}
}
func TestBuoyancyAngularDrag(t *testing.T) {
	h := testHull(t)
	state := WaterBodyState{Transform: waterIdentity(), AngularVelocity: ntmath.Vec3{Z: 2}}
	f, e := h.Forces(testWater{}, state, 0)
	if e != nil {
		t.Fatal(e)
	}
	if f.Drag.Y >= 0 || f.Torque.Z >= 0 {
		t.Fatal("angular point velocity missing", f)
	}
	state.LinearVelocity = ntmath.Vec3{X: -6}
	f, e = h.Forces(testWater{}, state, 0)
	if e != nil || f.Planing != (ntmath.Vec3{}) {
		t.Fatal("reverse planing", f, e)
	}
}
func TestBuoyancyValidationAndOwnership(t *testing.T) {
	points := []BuoyancyPoint{{Volume: 1, Height: 1}}
	cfg := BuoyantHullConfig{Density: 1000, Gravity: 10, Points: points}
	h, e := NewBuoyantHull(cfg)
	if e != nil {
		t.Fatal(e)
	}
	points[0].Volume = 0
	state := WaterBodyState{Transform: waterIdentity()}
	f, e := h.Forces(testWater{}, state, 0)
	if e != nil || f.Force.Z != 5000 {
		t.Fatal("caller mutated hull", f, e)
	}
	for _, scale := range []float32{2, -1, 0} {
		state.Transform[0] = scale
		if _, e = h.Forces(testWater{}, state, 0); e == nil {
			t.Fatal("accepted nonrigid pose")
		}
	}
	state.Transform = waterIdentity()
	f, e = h.Forces(testWater{fail: true}, state, 0)
	if e == nil || f != (WaterForces{}) {
		t.Fatal(f, e)
	}
	if _, e = NewBuoyantHull(cfg); e == nil {
		t.Fatal("accepted zero volume")
	}
}
