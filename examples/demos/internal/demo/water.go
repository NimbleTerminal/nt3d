// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"fmt"
	"image/color"

	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// WaterMain runs a shared wave surface and a float following four world samples.
func WaterMain() { mainDemo(false, sceneWater) }

type waterScene struct {
	ripples, reflections bool
	surface              *nt3d.WaveSurface
	grid                 nt3d.Geometry
	rest                 []nt3dmath.Vec3
	mesh, float          nt3d.NodeID
	craft                waterCraft
	effects              waterEffects
	moored               bool
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
	g, err := nt3d.BuildHeightfield(nt3d.Heightfield{NX: 81, NY: 81, MinX: -5, MaxX: 5, MinY: -5, MaxY: 5,
		Sampler: func(float32, float32) float32 { return 0 }, Color: color.RGBA{28, 127, 155, 255},
		Material: &nt3d.Material{Specular: .8, Shininess: 64}})
	if err != nil {
		return err
	}
	w := &waterScene{surface: surface, grid: g, rest: make([]nt3dmath.Vec3, len(g.Vertices))}
	for i, vertex := range g.Vertices {
		w.rest[i] = vertex.Position
	}
	w.craft = waterCraft{x: 0, y: -2.7, speed: 4.5, autopilot: true}
	w.ripples, w.reflections = true, true
	w.grid.Material.Reflectivity = .02
	w.grid.Material.NormalStrength = 1
	w.grid.Material.NormalRepeat = true
	w.grid.Tangents = make([]nt3d.Tangent, len(g.Vertices))
	for i := range w.grid.UV {
		w.grid.UV[i].U *= 4
		w.grid.UV[i].V *= 4
	}
	d.water = w
	w.mesh, err = d.scene.SetGeometry("water", g)
	if err != nil {
		return err
	}
	if err = d.scene.Reparent(w.mesh, d.group); err != nil {
		return err
	}
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
	d.scene.SetEnvironment(waterEnvironment())
	return d.stepWater()
}

func (d *demo) stepWater() error {
	w := d.water
	normal, err := waterRipples(d.animationTime)
	if err != nil {
		return err
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
	for i, p := range w.rest {
		s, err := w.surface.Sample(float64(p.X), float64(p.Y), d.animationTime)
		if err != nil {
			return err
		}
		w.grid.Vertices[i].Position, w.grid.Vertices[i].Normal = s.Position, s.Normal
		w.grid.Tangents[i] = nt3d.Tangent{Direction: s.TangentX.Normalize(), W: -1}
	}
	w.grid.Bounds = nt3d.GeometryBounds(w.grid)
	if err := d.scene.SetNodeGeometry(w.mesh, w.grid); err != nil {
		return err
	}
	if !w.moored {
		if err := w.craft.advanceEffects(w.surface, d.animationTime, &w.effects); err != nil {
			return err
		}
		if err := d.scene.SetTransform(w.float, w.craft.pose()); err != nil {
			return err
		}
		return d.drawWaterEffects()
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
	flight := "riding"
	if d.water.craft.airborne {
		flight = "airborne"
	}
	pilot := "manual"
	if d.water.craft.autopilot {
		pilot = "autopilot"
	}
	return "nt3d water · w/s throttle · j/l steer · space autopilot · n ripples · e reflections · f wake/spray · a pause · q quit\n" + content +
		fmt.Sprintf("\n%s · %s · %s · %s · %.1f m/s · %s · %d drops · %s", d.rendering(), state, pilot, flight, d.water.craft.speed, d.water.effects.modeName(), d.water.effects.live, d.status)
}
