// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"fmt"
	"image/color"

	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// WaterMain runs a shared wave surface and a force-driven craft.
func WaterMain() { mainDemo(false, sceneWater) }

type waterScene struct {
	tiles                []waterTile
	extent               float64
	large                bool
	shoreMode            int
	ripples, reflections bool
	optics, softSpray    bool
	surface              *nt3d.WaveSurface
	baseSurface          *nt3d.WaveSurface
	grid                 nt3d.Geometry
	mesh, float          nt3d.NodeID
	craft                waterCraft
	effects              waterEffects
	moored               bool
	debugMode            int
	debugNode            nt3d.NodeID
}

func (d *demo) setupWater() error {
	surface, err := nt3d.NewWaveSurface([]nt3d.GerstnerWave{
		{Direction: nt3dmath.Vec3{X: 1, Y: .3}, Wavelength: 4.5, Amplitude: .20},
		{Direction: nt3dmath.Vec3{X: -.3, Y: 1}, Wavelength: 2.7, Amplitude: .09, Phase: 1.1},
		{Direction: nt3dmath.Vec3{X: .7, Y: -1}, Wavelength: 1.4, Amplitude: .035, Phase: 2.4},
	}, 9.81)
	if err != nil {
		return err
	}
	g := nt3d.Geometry{Material: &nt3d.Material{Specular: .8, Shininess: 64}}
	w := &waterScene{surface: surface, baseSurface: surface, grid: g}
	w.craft = waterCraft{x: 0, y: -2.7, throttle: .65, autopilot: true}
	w.ripples, w.reflections = true, true
	w.optics, w.softSpray = true, true
	w.grid.Material.Reflectivity = .02
	w.grid.Material.NormalStrength = 1
	w.grid.Material.NormalRepeat = true
	d.water = w
	hull, err := nt3d.BuildCuboid(nt3d.Cuboid{Size: nt3dmath.Vec3{X: 1.5, Y: .7, Z: .23}, Color: color.RGBA{242, 148, 48, 255}, Material: &nt3d.Material{Specular: .25, Shininess: 24}})
	if err != nil {
		return err
	}
	w.float, err = d.scene.SetGeometry("float", hull)
	if err != nil {
		return err
	}
	if err = d.scene.Reparent(w.float, d.group); err != nil {
		return err
	}
	// An asymmetric deck and bow marker make pitch, roll, and heading readable.
	deck := nt3d.Geometry{Boxes: []nt3d.Box{
		{Min: nt3dmath.Vec3{X: -.55, Y: -.23, Z: .115}, Size: nt3dmath.Vec3{X: .85, Y: .46, Z: .12}, Color: color.RGBA{244, 235, 208, 255}},
		{Min: nt3dmath.Vec3{X: .45, Y: -.07, Z: .115}, Size: nt3dmath.Vec3{X: .13, Y: .14, Z: .3}, Color: color.RGBA{210, 65, 45, 255}},
	}}
	id, err := d.scene.SetGeometry("float-deck", deck)
	if err != nil {
		return err
	}
	if err = d.scene.Reparent(id, w.float); err != nil {
		return err
	}
	if err = d.setupWaterEffects(); err != nil {
		return err
	}
	w.debugNode, err = d.scene.SetGeometry("water-debug", nt3d.Geometry{})
	if err != nil {
		return err
	}
	if err = d.scene.Reparent(w.debugNode, d.group); err != nil {
		return err
	}
	d.scene.SetEnvironment(waterEnvironment())
	return d.rebuildWaterTiles()
}

func (d *demo) stepWater() error {
	w := d.water
	normal, err := waterRipples(d.animationTime)
	if err != nil {
		return err
	}
	w.grid.Material.Transmission, w.grid.Material.Refraction = 0, 0
	w.grid.Material.Absorption = nt3dmath.Vec3{}
	if w.optics {
		w.grid.Material.Transmission = .94
		w.grid.Material.Refraction = .018
		w.grid.Material.Absorption = vec(.85, .28, .12)
	}
	w.grid.Material.NormalMap = normal
	w.grid.Material.NormalStrength = 0
	if w.ripples {
		w.grid.Material.NormalStrength = 1
	}
	w.grid.Material.Reflectivity = 0
	if w.reflections {
		w.grid.Material.Reflectivity = .02
	}

	if !w.moored {
		if err := w.craft.advanceEffects(w.baseSurface, d.animationTime, &w.effects); err != nil {
			return err
		}
	}
	w.surface, err = w.effects.deformation.surface(w.baseSurface)
	if err != nil {
		return err
	}
	if err := d.stepWaterTiles(w.grid.Material); err != nil {
		return err
	}
	if !w.moored {
		if err := d.scene.SetTransform(w.float, w.craft.pose()); err != nil {
			return err
		}
		if err := d.drawWaterEffects(); err != nil {
			return err
		}
		return d.drawWaterDebug()
	}
	pose, err := waterFloatPose(w.surface, d.animationTime)
	if err != nil {
		return err
	}
	return d.scene.SetTransform(w.float, pose)
}

// Fit a plane to four fixed world-XY probes beneath the float. This is a
// kinematic demonstration, not forces, inertia, or a boat physics model.
func waterFloatPose(surface *nt3d.WaveSurface, time float64) (nt3dmath.Mat4, error) {
	const hx, hy = .6, .25
	var z, gx, gy float64
	for _, p := range [][2]float64{{-hx, -hy}, {hx, -hy}, {-hx, hy}, {hx, hy}} {
		s, err := surface.AtWorld(p[0], p[1], time)
		if err != nil {
			return nt3dmath.Mat4{}, err
		}
		h := float64(s.Position.Z)
		z += h / 4
		gx += h * p[0] / (4 * hx * hx)
		gy += h * p[1] / (4 * hy * hy)
	}
	up := (nt3dmath.Vec3{X: float32(-gx), Y: float32(-gy), Z: 1}).Normalize()
	forward := (nt3dmath.Vec3{X: 1, Z: float32(gx)}).Normalize()
	side := up.Cross(forward).Normalize()
	return nt3dmath.Mat4{
		forward.X, forward.Y, forward.Z, 0,
		side.X, side.Y, side.Z, 0,
		up.X, up.Y, up.Z, 0,
		0, 0, float32(z) + .065, 1,
	}, nil
}

func (d *demo) waterView(content string) string {
	state := "running"
	if !d.animate {
		state = "paused"
	}
	flight := "buoyant"
	if d.water.craft.lift > 80 && !d.water.craft.noPlaning {
		flight = "planing"
	}
	if d.water.craft.airborne {
		flight = "airborne"
	}
	pilot := "manual"
	if d.water.craft.autopilot {
		pilot = "autopilot"
	}
	wake := "wake on"
	if d.water.effects.deformation.disabled {
		wake = "wake off"
	}
	if d.water.debugMode != 0 {
		return "water · b debug · u preset · [/] trim · k center · . step · a pause · w/s throttle · j/l steer · space auto · q quit\n" + content + "\n" + d.waterDebugText()
	}
	return "water · b debug · u preset · w/s throttle · j/l steer · space auto · a pause · q quit\n" + content +
		fmt.Sprintf("\n%s · %s · %s · %s · %s · %.0f%% · %.1f m/s · %s · %s", d.rendering(), state, pilot, d.water.craft.handling().name, flight, 100*d.water.craft.throttle, d.water.craft.speed, wake, d.status)
}
