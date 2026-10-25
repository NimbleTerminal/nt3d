// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"math"
	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// The demo integrates water forces at 120 Hz; rendering only reads the pose.
type waterCraft struct {
	bound                                    float64
	shoreline                                bool
	x, y, z, heading, speed, vz, pitch, roll float64
	time                                     float64
	vx, vy, throttle                         float64
	power, rudder, trim                      float64
	preset                                   int
	telemetry                                waterTelemetry
	orientation                              [4]float64 // unit quaternion, wxyz
	angular                                  [3]float64 // local angular velocity
	noPlaning                                bool
	lift                                     float32
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
	return
}

func (c *waterCraft) advance(surface *nt3d.WaveSurface, until float64) error {
	return c.advanceEffects(surface, until, nil)
}

func (c *waterCraft) advanceEffects(surface *nt3d.WaveSurface, until float64, effects *waterEffects) error {
	const dt = 1.0 / 120
	query := surface
	if effects != nil {
		var err error
		query, err = effects.deformation.surface(surface)
		if err != nil {
			return err
		}
	}
	if !c.ready {
		if c.shoreline {
			waterBank(c)
		}
		h, p, r, err := craftSupport(query, c.x, c.y, c.heading, c.time)
		if err != nil {
			return err
		}
		c.z, c.pitch, c.roll, c.ready = h+waterRestHeight, p, r, true
		c.orientation = waterQuaternion(c.heading, -math.Atan(p), math.Atan(r))
		c.vx, c.vy = c.speed*math.Cos(c.heading), c.speed*math.Sin(c.heading)
		c.angular = [3]float64{}
	}
	for c.time+dt <= until+1e-9 {
		c.time += dt
		if effects != nil {
			var err error
			query, err = effects.deformation.surface(surface)
			if err != nil {
				return err
			}
		}
		impact, turn, err := c.integrateWater(query, dt)
		if err != nil {
			return err
		}

		if effects != nil {
			if err := effects.step(query, c, dt, impact, turn); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *waterCraft) pose() nt3dmath.Mat4 {
	q := c.orientation
	if q == [4]float64{} {
		q = waterQuaternion(c.heading, -math.Atan(c.pitch), math.Atan(c.roll))
	}
	w, x, y, z := q[0], q[1], q[2], q[3]
	return nt3dmath.Mat4{
		float32(1 - 2*(y*y+z*z)), float32(2 * (x*y + w*z)), float32(2 * (x*z - w*y)), 0,
		float32(2 * (x*y - w*z)), float32(1 - 2*(x*x+z*z)), float32(2 * (y*z + w*x)), 0,
		float32(2 * (x*z + w*y)), float32(2 * (y*z - w*x)), float32(1 - 2*(x*x+y*y)), 0,
		float32(c.x), float32(c.y), float32(c.z), 1}

}

func (d *demo) waterKey(key string) (bool, error) {
	w := d.water
	switch key {
	case ".":
		if !d.animate {
			d.animationTime += 1.0 / 120
			return true, d.stepWater()
		}
		return true, nil
	case "x":
		w.craft.telemetry.lastImpact, w.craft.telemetry.peakAcceleration = 0, 0
		w.craft.launches, w.craft.landings = 0, 0
		return true, nil
	case "b":
		w.debugMode = (w.debugMode + 1) % 3
		return true, d.drawWaterDebug()
	case "u":
		w.craft.preset = (w.craft.preset + 1) % len(waterHandlingPresets)
		return true, nil
	case "k":
		w.craft.turn = 0
		w.craft.autopilot = false
		return true, nil
	case "[":
		w.craft.trim = math.Max(-.15, w.craft.trim-.03)
		return true, nil
	case "]":
		w.craft.trim = math.Min(.15, w.craft.trim+.03)
		return true, nil
	case "z":
		w.effects.deformation.disabled = !w.effects.deformation.disabled
		return true, d.stepWater()
	case "p":
		w.craft.noPlaning = !w.craft.noPlaning
		return true, nil
	case "v":
		w.shoreMode = (w.shoreMode + 1) % 4
		return true, d.stepWater()
	case "m":
		w.large = !w.large
		if err := d.rebuildWaterTiles(); err != nil {
			return true, err
		}
		if d.view != nil {
			camera := d.view.Camera()
			camera.Distance = 11
			if w.large {
				camera.Distance = 30
			}
			d.view.SetCamera(camera)
		}
		return true, nil
	case "t":
		w.optics = !w.optics
		return true, d.stepWater()
	case "h":
		w.softSpray = !w.softSpray
		return true, d.updateWaterSprayStyle()
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
		w.craft.throttle = math.Min(1, w.craft.throttle+.1)
	case "s":
		w.craft.throttle = math.Max(0, w.craft.throttle-.1)
	case "j":
		w.craft.autopilot = false
		w.craft.turn = math.Min(1, w.craft.turn+.35)
	case "l":
		w.craft.autopilot = false
		w.craft.turn = math.Max(-1, w.craft.turn-.35)
	case " ", "space":
		w.craft.autopilot = !w.craft.autopilot
		w.craft.turn = 0
	default:
		return false, nil
	}
	return true, nil
}
