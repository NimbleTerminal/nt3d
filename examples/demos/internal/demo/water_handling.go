// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"math"
	"nimbleterminal.dev/nt3d"
	ntmath "nimbleterminal.dev/nt3d/math"
)

// Demo handling presets share the visible hull and mass. These empirical
// coefficients are gameplay tuning, not a calibrated personal watercraft.
type waterHandling struct {
	name                                         string
	thrust, jetAngle, throttleRate, steeringRate float64
	jetSpeed                                     float64
	lateralDrag, lateralDamping, bankStrength    float64
}

var waterHandlingPresets = [...]waterHandling{
	{name: "touring", thrust: 260, jetAngle: .9, throttleRate: 1.5, steeringRate: 3, jetSpeed: 10, lateralDrag: .7, lateralDamping: 400, bankStrength: 300},
	{name: "sport", thrust: 340, jetAngle: 1, throttleRate: 2.5, steeringRate: 5, jetSpeed: 12, lateralDrag: .55, lateralDamping: 300, bankStrength: 500},
}
var waterProbePositions = [4]ntmath.Vec3{vec(-.5, -.24, 0), vec(-.5, .24, 0), vec(.5, -.24, 0), vec(.5, .24, 0)}

type waterProbeReading struct {
	center, surface ntmath.Vec3
	wet             float64
}
type waterTelemetry struct {
	water                                           nt3d.WaterForces
	origin, thrust, net, torque, velocity           ntmath.Vec3
	probes                                          [4]waterProbeReading
	steer, yawRate, slip, pitch, bank, acceleration float64
	lastImpact, peakAcceleration                    float64
}

func waterApproach(value, target, step float64) float64 {
	return value + math.Max(-step, math.Min(step, target-value))
}
func (c *waterCraft) handling() waterHandling { return waterHandlingPresets[c.preset] }
func (c *waterCraft) steering(omega ntmath.Vec3, pose ntmath.Mat4) float64 {
	if !c.autopilot {
		return c.turn
	}
	// Correct for sideslip as well as heading so the jet turns inward before
	// momentum carries the craft into the finite-patch bumpers.
	localX := float64(vec(c.vx, c.vy, 0).Dot(pose.Direction(vec(1, 0, 0))))
	localY := float64(vec(c.vx, c.vy, 0).Dot(pose.Direction(vec(0, 1, 0))))
	slip := math.Atan2(localY, math.Max(.5, localX))
	px, py := c.x+c.vx*.7, c.y+c.vy*.7
	desired := math.Atan2(py, px) + math.Pi/2 + math.Atan((math.Hypot(px, py)-2.5)*1.2) - slip
	headingError := math.Atan2(math.Sin(desired-c.heading), math.Cos(desired-c.heading))
	rate := c.speed/2.5 + 3*headingError
	return math.Max(-1, math.Min(1, .35*rate+1.5*(rate-float64(omega.Z))))
}

func (c *waterCraft) recordWater(surface *nt3d.WaveSurface, pose ntmath.Mat4, loads nt3d.WaterForces, thrust, force, torque, omega ntmath.Vec3, impact float64) error {
	previous := c.telemetry
	t := waterTelemetry{water: loads, origin: pose.Point(vec(0, 0, 0)), thrust: thrust, net: force, torque: torque, velocity: vec(c.vx, c.vy, c.vz), steer: c.rudder, yawRate: float64(omega.Z), lastImpact: previous.lastImpact, peakAcceleration: previous.peakAcceleration}
	t.pitch = math.Atan2(float64(pose[2]), math.Hypot(float64(pose[0]), float64(pose[1]))) * 180 / math.Pi
	t.bank = math.Atan2(float64(pose[6]), float64(pose[10])) * 180 / math.Pi
	t.slip = math.Atan2(float64(t.velocity.Dot(pose.Direction(vec(0, 1, 0)))), math.Max(.1, float64(t.velocity.Dot(pose.Direction(vec(1, 0, 0)))))) * 180 / math.Pi
	t.acceleration = math.Sqrt(float64(force.Dot(force))) / waterMass / 9.81
	t.peakAcceleration = math.Max(t.peakAcceleration, t.acceleration)
	if impact > 0 {
		t.lastImpact = impact
	}
	for i, p := range waterProbePositions {
		world := pose.Point(p)
		sample, err := surface.AtWorld(float64(world.X), float64(world.Y), c.time)
		if err != nil {
			return err
		}
		t.probes[i] = waterProbeReading{world, sample.Position, math.Max(0, math.Min(1, .5+float64(sample.Position.Z-world.Z)/.23))}
	}
	c.telemetry = t
	return nil
}
