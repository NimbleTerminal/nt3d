// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"slices"
)

// WaterSurface is the query interface used by water forces. WaveSurface,
// including WithWake snapshots, implements it. Velocity is fluid velocity in
// world space; an adapter may add a gameplay current before returning it.
type WaterSurface interface {
	AtWorld(x, y, time float64) (WaveSample, error)
}

// BuoyancyPoint approximates a hull cell centered at Position relative to the
// body's center of mass. Volume is its full displaced volume (m³); Height is
// its vertical immersion range (m), measured along world Z. PlaningArea is its
// effective lifting area (m²). These probe cells approximate hull geometry;
// they are not exact submerged-volume clipping, especially at large rotations.
type BuoyancyPoint struct {
	Position                    nt3dmath.Vec3
	Volume, Height, PlaningArea float32
}

// BuoyantHullConfig uses meters, kilograms, seconds, and Z up. DragArea and
// DragCoefficient are hull totals in local forward/left/up axes. LinearDrag
// is an optional total damping coefficient (N·s/m) in those axes. Drag totals
// are distributed among probes by volume and scaled by wet fraction.
type BuoyantHullConfig struct {
	Points                                []BuoyancyPoint
	Density, Gravity                      float32
	DragArea, DragCoefficient, LinearDrag nt3dmath.Vec3
	// PlaningCoefficient is an empirical lift coefficient, not a hull-pressure
	// solver. Lift fades in between PlaningSpeed and twice that speed, then
	// scales with forward water-relative speed squared and immersed area.
	PlaningCoefficient, PlaningSpeed float32
}

// WaterBodyState is a rigid pose about the center of mass, with world-space
// linear velocity (m/s) and angular velocity (rad/s). Transform must contain
// only a proper rotation and translation; scale belongs in the hull probes.
type WaterBodyState struct {
	Transform                       nt3dmath.Mat4
	LinearVelocity, AngularVelocity nt3dmath.Vec3
}

// WaterForces are world-space water loads, in newtons and newton-meters about
// the body's center of mass. They exclude gravity, thrust, collisions, and dt.
// Apply Force and Torque once per physics step; the engine owns integration.
type WaterForces struct {
	Force, Torque                               nt3dmath.Vec3
	Buoyancy, Drag, Planing                     nt3dmath.Vec3
	SubmergedVolume, WetFraction, MaxEntrySpeed float32
	WetPoints                                   int
}

// BuoyantHull is an immutable force provider, safe to evaluate concurrently
// when its WaterSurface is. It owns no rigid body, clock, or scene resources.
type BuoyantHull struct {
	config BuoyantHullConfig
	volume float32
}

func NewBuoyantHull(c BuoyantHullConfig) (*BuoyantHull, error) {
	if len(c.Points) == 0 || len(c.Points) > 64 || !validFloat(c.Density, c.Gravity, c.PlaningCoefficient, c.PlaningSpeed) || c.Density <= 0 || c.Gravity <= 0 || c.PlaningCoefficient < 0 || c.PlaningSpeed < 0 {
		return nil, fmt.Errorf("nt3d: invalid buoyant hull configuration")
	}
	for _, v := range []nt3dmath.Vec3{c.DragArea, c.DragCoefficient, c.LinearDrag} {
		if !validFloat(v.X, v.Y, v.Z) || v.X < 0 || v.Y < 0 || v.Z < 0 {
			return nil, fmt.Errorf("nt3d: water drag must be finite and nonnegative")
		}
	}
	total := float32(0)
	for _, p := range c.Points {
		if !validFloat(p.Position.X, p.Position.Y, p.Position.Z, p.Volume, p.Height, p.PlaningArea) || p.Volume <= 0 || p.Height <= 0 || p.PlaningArea < 0 {
			return nil, fmt.Errorf("nt3d: invalid buoyancy point")
		}
		total += p.Volume
	}
	if !validFloat(total) || total <= 0 {
		return nil, fmt.Errorf("nt3d: invalid hull volume")
	}
	c.Points = slices.Clone(c.Points)
	return &BuoyantHull{config: c, volume: total}, nil
}

func rigidWaterPose(m nt3dmath.Mat4) bool {
	if !validFloat(m[:]...) || m[3] != 0 || m[7] != 0 || m[11] != 0 || m[15] != 1 {
		return false
	}
	x, y, z := nt3dmath.Vec3{X: m[0], Y: m[1], Z: m[2]}, nt3dmath.Vec3{X: m[4], Y: m[5], Z: m[6]}, nt3dmath.Vec3{X: m[8], Y: m[9], Z: m[10]}
	return abs(x.Dot(x)-1) < 1e-4 && abs(y.Dot(y)-1) < 1e-4 && abs(z.Dot(z)-1) < 1e-4 && abs(x.Dot(y)) < 1e-4 && abs(y.Dot(z)) < 1e-4 && abs(z.Dot(x)) < 1e-4 && x.Cross(y).Dot(z) > .9999
}

// Forces evaluates Archimedes buoyancy, water-relative linear/quadratic drag,
// and optional empirical planing lift. Point velocity includes omega × lever.
// Lift is perpendicular to relative motion (no propulsive work), faces upward,
// and vanishes when dry, moving backward, or with an overturned hull. Errors
// return no partial loads. This function does not modify hull, body, or surface.
func (h *BuoyantHull) Forces(surface WaterSurface, body WaterBodyState, time float64) (WaterForces, error) {
	var out WaterForces
	if h == nil || surface == nil || !waveFinite(time) || !rigidWaterPose(body.Transform) || !validFloat(body.LinearVelocity.X, body.LinearVelocity.Y, body.LinearVelocity.Z, body.AngularVelocity.X, body.AngularVelocity.Y, body.AngularVelocity.Z) {
		return out, fmt.Errorf("nt3d: invalid water-force state")
	}
	c, m := h.config, body.Transform
	axes := [3]nt3dmath.Vec3{{X: m[0], Y: m[1], Z: m[2]}, {X: m[4], Y: m[5], Z: m[6]}, {X: m[8], Y: m[9], Z: m[10]}}
	area, coeff, linear := [3]float32{c.DragArea.X, c.DragArea.Y, c.DragArea.Z}, [3]float32{c.DragCoefficient.X, c.DragCoefficient.Y, c.DragCoefficient.Z}, [3]float32{c.LinearDrag.X, c.LinearDrag.Y, c.LinearDrag.Z}
	for _, p := range c.Points {
		lever := m.Direction(p.Position)
		world := m.Point(p.Position)
		sample, err := surface.AtWorld(float64(world.X), float64(world.Y), time)
		if err != nil {
			return WaterForces{}, err
		}
		if !validFloat(sample.Position.Z, sample.Velocity.X, sample.Velocity.Y, sample.Velocity.Z, sample.Normal.X, sample.Normal.Y, sample.Normal.Z) || sample.Normal.Z <= 0 {
			return WaterForces{}, fmt.Errorf("nt3d: invalid water-force surface sample")
		}
		wet := max(float32(0), min(float32(1), .5+(sample.Position.Z-world.Z)/p.Height))
		if wet == 0 {
			continue
		}
		normal := sample.Normal.Normalize()
		relative := body.LinearVelocity.Add(body.AngularVelocity.Cross(lever)).Sub(sample.Velocity)
		out.WetPoints++
		out.SubmergedVolume += p.Volume * wet
		out.MaxEntrySpeed = max(out.MaxEntrySpeed, -relative.Dot(normal))
		buoy := nt3dmath.Vec3{Z: c.Density * c.Gravity * p.Volume * wet}
		var drag, lift nt3dmath.Vec3
		weight := wet * p.Volume / h.volume
		for i, axis := range axes {
			v := relative.Dot(axis)
			magnitude := -weight * (linear[i]*v + .5*c.Density*coeff[i]*area[i]*abs(v)*v)
			drag = drag.Add(axis.Scale(magnitude))
		}
		forward := relative.Dot(axes[0])
		if c.PlaningCoefficient > 0 && p.PlaningArea > 0 && forward > c.PlaningSpeed && axes[2].Dot(normal) > .2 {
			factor := float32(1)
			if c.PlaningSpeed > 0 {
				u := min(float32(1), (forward-c.PlaningSpeed)/c.PlaningSpeed)
				factor = u * u * (3 - 2*u)
			}
			direction := relative.Normalize()
			liftDirection := axes[2].Sub(direction.Scale(axes[2].Dot(direction))).Normalize()
			if liftDirection.Z > 0 {
				magnitude := .5 * c.Density * c.PlaningCoefficient * p.PlaningArea * wet * forward * forward * factor
				lift = liftDirection.Scale(magnitude)
			}
		}
		force := buoy.Add(drag).Add(lift)
		out.Force = out.Force.Add(force)
		out.Torque = out.Torque.Add(lever.Cross(force))
		out.Buoyancy = out.Buoyancy.Add(buoy)
		out.Drag = out.Drag.Add(drag)
		out.Planing = out.Planing.Add(lift)
	}
	out.WetFraction = out.SubmergedVolume / h.volume
	for _, v := range []nt3dmath.Vec3{out.Force, out.Torque, out.Buoyancy, out.Drag, out.Planing} {
		if !validFloat(v.X, v.Y, v.Z) {
			return WaterForces{}, fmt.Errorf("nt3d: water-force overflow")
		}
	}
	if !validFloat(out.SubmergedVolume, out.WetFraction, out.MaxEntrySpeed) {
		return WaterForces{}, fmt.Errorf("nt3d: water-force overflow")
	}
	return out, nil
}
