// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"math"
	"nimbleterminal.dev/nt3d"
)

// A small demo integrator, not an engine API. Dimensions match the stylized
// 1.5 × .7 × .23 m hull; 80 kg includes its effective demo payload.
const waterMass = 80.0
const waterRestHeight = .23 * (.5 - waterMass/(1000*1.5*.7*.23))

var waterHulls = func() [len(waterHandlingPresets)][2]*nt3d.BuoyantHull {
	var hulls [len(waterHandlingPresets)][2]*nt3d.BuoyantHull
	for preset, handling := range waterHandlingPresets {
		for i := range hulls[preset] {
			cfg := nt3d.BuoyantHullConfig{Density: 1000, Gravity: 9.81,
				DragArea: vec(.1, handling.lateralDrag, 1.05), DragCoefficient: vec(.55, 1.2, 1.2), LinearDrag: vec(20, handling.lateralDamping, 3500),
				PlaningCoefficient: float32(i) * .35, PlaningSpeed: 2}
			for _, p := range waterProbePositions {
				cfg.Points = append(cfg.Points, nt3d.BuoyancyPoint{Position: p, Volume: 1.5 * .7 * .23 / 4, Height: .23, PlaningArea: .2})
			}
			var err error
			hulls[preset][i], err = nt3d.NewBuoyantHull(cfg)
			if err != nil {
				panic(err)
			}
		}
	}
	return hulls
}()

func waterQuaternion(yaw, pitch, roll float64) [4]float64 {
	cy, sy, cp, sp, cr, sr := math.Cos(yaw/2), math.Sin(yaw/2), math.Cos(pitch/2), math.Sin(pitch/2), math.Cos(roll/2), math.Sin(roll/2)
	return [4]float64{cr*cp*cy + sr*sp*sy, sr*cp*cy - cr*sp*sy, cr*sp*cy + sr*cp*sy, cr*cp*sy - sr*sp*cy}
}

func (c *waterCraft) integrateWater(surface *nt3d.WaveSurface, dt float64) (impact, turn float64, err error) {
	pose := c.pose()
	omega := pose.Direction(vec(c.angular[0], c.angular[1], c.angular[2]))
	handling := c.handling()
	hull := waterHulls[c.preset][1]
	if c.noPlaning {
		hull = waterHulls[c.preset][0]
	}
	probes := probeSurface{surface: surface}
	loads, err := hull.Forces(&probes, nt3d.WaterBodyState{Transform: pose, LinearVelocity: vec(c.vx, c.vy, c.vz), AngularVelocity: omega}, c.time)
	if err != nil {
		return 0, 0, err
	}
	c.lift = loads.Planing.Z
	wasAirborne := c.airborne
	c.airborne = loads.WetPoints == 0
	if c.airborne && !wasAirborne {
		c.launches++
	}
	if !c.airborne && wasAirborne {
		c.landings++
		impact = float64(loads.MaxEntrySpeed)
	}
	targetPower := c.throttle
	if c.autopilot {
		targetPower = math.Min(c.throttle, math.Max(0, .25+.4*(c.throttle*4-c.speed)))
	}
	c.power = waterApproach(c.power, targetPower, handling.throttleRate*dt)
	c.rudder = waterApproach(c.rudder, c.steering(omega, pose), handling.steeringRate*dt)
	// A dry intake cannot propel the craft. Positive trim points the thrust
	// down at the stern, producing a bow-up torque.
	forward := float64(vec(c.vx, c.vy, c.vz).Dot(pose.Direction(vec(1, 0, 0))))
	// A finite jet loses thrust with advance speed, preventing unbounded
	// acceleration as planing reduces immersion and water drag.
	advance := math.Max(0, forward) / handling.jetSpeed
	jet := handling.thrust * c.power * math.Min(1, float64(loads.WetFraction)/.1) / (1 + advance*advance)
	thrust := pose.Direction(vec(jet*math.Cos(c.rudder*handling.jetAngle)*math.Cos(c.trim), -jet*math.Sin(c.rudder*handling.jetAngle)*math.Cos(c.trim), -jet*math.Sin(c.trim)))
	force := loads.Force.Add(thrust).Add(vec(0, 0, -waterMass*9.81))
	torque := loads.Torque.Add(pose.Direction(vec(-.7, 0, -.06)).Cross(thrust))
	// A simplified rider shifts weight into the turn. This explicit control
	// torque is wet/contact gated and damped, separate from passive water loads.
	targetBank := -.3 * c.rudder * math.Min(1, math.Max(0, forward)/4)
	bank := math.Atan2(float64(pose[6]), float64(pose[10]))
	rider := (handling.bankStrength*(targetBank-bank) - 20*c.angular[0]) * math.Min(1, float64(loads.WetFraction)/.15)
	torque = torque.Add(pose.Direction(vec(rider, 0, 0)))
	if err := c.recordWater(&probes, pose, loads, thrust, force, torque, omega, impact); err != nil {
		return 0, 0, err
	}

	inertia := [3]float64{waterMass * (.7*.7 + .23*.23) / 12, waterMass * (1.5*1.5 + .23*.23) / 12, waterMass * (1.5*1.5 + .7*.7) / 12}
	localTorque := [3]float64{
		float64(torque.Dot(pose.Direction(vec(1, 0, 0)))),
		float64(torque.Dot(pose.Direction(vec(0, 1, 0)))),
		float64(torque.Dot(pose.Direction(vec(0, 0, 1)))),
	}
	old := c.angular
	for i := range 3 {
		j, k := (i+1)%3, (i+2)%3
		c.angular[i] += dt * (localTorque[i] - (inertia[k]-inertia[j])*old[j]*old[k]) / inertia[i]
	}
	q := c.orientation
	a, b, d := c.angular[0], c.angular[1], c.angular[2]
	dq := [4]float64{-q[1]*a - q[2]*b - q[3]*d, q[0]*a + q[2]*d - q[3]*b, q[0]*b + q[3]*a - q[1]*d, q[0]*d + q[1]*b - q[2]*a}
	norm := 0.0
	for i := range q {
		q[i] += dt * .5 * dq[i]
		norm += q[i] * q[i]
	}
	for i := range q {
		q[i] /= math.Sqrt(norm)
	}
	c.orientation = q
	c.vx += float64(force.X) / waterMass * dt
	c.vy += float64(force.Y) / waterMass * dt
	c.vz += float64(force.Z) / waterMass * dt
	c.x += c.vx * dt
	c.y += c.vy * dt
	c.z += c.vz * dt
	// Finite-patch bumpers are demo collision constraints, not hydrodynamics.
	bound := c.bound
	if bound == 0 {
		bound = 4.1
	}
	if math.Abs(c.x) > bound {
		c.x = math.Copysign(bound, c.x)
		if c.vx*c.x > 0 {
			c.vx *= -.4
		}
	}
	if math.Abs(c.y) > bound {
		c.y = math.Copysign(bound, c.y)
		if c.vy*c.y > 0 {
			c.vy *= -.4
		}
	}
	if c.shoreline {
		waterBank(c)
	}
	c.speed = math.Hypot(c.vx, c.vy)
	pose = c.pose()
	c.heading = math.Atan2(float64(pose[1]), float64(pose[0]))
	c.pitch = float64(pose[2])
	c.roll = float64(pose[6])
	return impact, float64(omega.Z), nil
}
