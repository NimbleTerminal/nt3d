// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"math"
	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// Horizontal handling is deliberately arcade-like. Vertical motion is ballistic
// whenever the hull outruns the wave beneath it; no jump impulse is injected.
type waterCraft struct {
	x, y, z, heading, speed, vz, pitch, roll float64
	time, support                            float64
	airborne, ready                          bool
	launches, landings                       int
	autopilot                                bool
	turn                                     float64
}

func craftSupport(s *nt3d.WaveSurface, x, y, heading, time float64) (height, pitch, roll float64, err error) {
	c, sn := math.Cos(heading), math.Sin(heading)
	for _, p := range [][2]float64{{-.6, -.25}, {.6, -.25}, {-.6, .25}, {.6, .25}} {
		q, e := s.AtWorld(x+c*p[0]-sn*p[1], y+sn*p[0]+c*p[1], time)
		if e != nil {
			err = e
			return
		}
		z := float64(q.Position.Z)
		height += z / 4
		pitch += z * p[0] / (4 * .6 * .6)
		roll += z * p[1] / (4 * .25 * .25)
	}
	height += .065
	return
}

func (c *waterCraft) advance(surface *nt3d.WaveSurface, until float64) error {
	return c.advanceEffects(surface, until, nil)
}

func (c *waterCraft) advanceEffects(surface *nt3d.WaveSurface, until float64, effects *waterEffects) error {
	const dt = 1.0 / 120
	if !c.ready {
		h, p, r, err := craftSupport(surface, c.x, c.y, c.heading, c.time)
		if err != nil {
			return err
		}
		c.z, c.support, c.pitch, c.roll, c.ready = h, h, p, r, true
	}
	for c.time+dt <= until+1e-9 {
		c.time += dt
		turn := c.turn
		if c.autopilot {
			turn = c.speed / 2.7
		}
		c.heading += turn * dt
		c.x += math.Cos(c.heading) * c.speed * dt
		c.y += math.Sin(c.heading) * c.speed * dt
		// Keep the prototype in its finite water patch. The boundary behaves as a
		// bumper; it changes heading, never teleports the craft across the surface.
		if math.Abs(c.x) > 4.1 {
			c.x = math.Copysign(4.1, c.x)
			c.heading = math.Pi - c.heading
		}
		if math.Abs(c.y) > 4.1 {
			c.y = math.Copysign(4.1, c.y)
			c.heading = -c.heading
		}
		h, p, r, err := craftSupport(surface, c.x, c.y, c.heading, c.time)
		if err != nil {
			return err
		}
		impact := 0.0
		next := c.z + c.vz*dt - .5*9.81*dt*dt
		if next > h+.001 {
			if !c.airborne {
				c.launches++
			}
			c.airborne = true
			c.z = next
			c.vz -= 9.81 * dt
			c.pitch += (-.12 - c.pitch) * (1 - math.Exp(-dt/.8))
		} else {
			if c.airborne {
				c.landings++
				impact = math.Max(0, (h-c.support)/dt-(c.vz-9.81*dt))
			}
			c.airborne = false
			c.z = h
			c.vz = (h - c.support) / dt
			blend := 1 - math.Exp(-dt/.08)
			c.pitch += (p - c.pitch) * blend
			c.roll += (r - c.roll) * blend
		}
		c.support = h
		if effects != nil {
			if err := effects.step(surface, c, dt, impact, turn); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *waterCraft) pose() nt3dmath.Mat4 {
	forward := (nt3dmath.Vec3{X: float32(math.Cos(c.heading)), Y: float32(math.Sin(c.heading)), Z: float32(c.pitch)}).Normalize()
	side := nt3dmath.Vec3{X: float32(-math.Sin(c.heading)), Y: float32(math.Cos(c.heading)), Z: float32(c.roll)}
	up := forward.Cross(side).Normalize()
	side = up.Cross(forward).Normalize()
	return nt3dmath.Mat4{forward.X, forward.Y, forward.Z, 0, side.X, side.Y, side.Z, 0, up.X, up.Y, up.Z, 0, float32(c.x), float32(c.y), float32(c.z), 1}
}

func (d *demo) waterKey(key string) (bool, error) {
	w := d.water
	switch key {
	case "f":
		w.effects.mode = (w.effects.mode + 1) % 4
		return true, d.drawWaterEffects()
	case "n":
		w.ripples = !w.ripples
		return true, d.stepWater()
	case "e":
		w.reflections = !w.reflections
		return true, d.stepWater()
	case "w":
		w.craft.speed = math.Min(7, w.craft.speed+.5)
	case "s":
		w.craft.speed = math.Max(0, w.craft.speed-.5)
	case "j":
		w.craft.autopilot = false
		w.craft.turn = math.Min(2, w.craft.turn+.35)
	case "l":
		w.craft.autopilot = false
		w.craft.turn = math.Max(-2, w.craft.turn-.35)
	case " ", "space":
		w.craft.autopilot = !w.craft.autopilot
		w.craft.turn = 0
	default:
		return false, nil
	}
	return true, nil
}
