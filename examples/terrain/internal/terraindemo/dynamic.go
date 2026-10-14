// Copyright (c) 2026 Neomantra Corp.
package terraindemo

import (
	"fmt"
	"image/color"
	"math"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
)

// DynamicMain runs a scene with independently moving nodes and a changing mesh.
func DynamicMain() { mainDemo(false, sceneDynamic) }

type dynamicScene struct {
	bodies []nt3d.NodeID
	serial int
	mesh   nt3d.NodeID
	deform bool
}

func (d *demo) setupDynamic() error {
	d.dynamic = &dynamicScene{deform: true}
	texture, err := nt3d.NewTexture(mapImage(true))
	if err != nil {
		return err
	}
	floor, err := nt3d.BuildHeightfield(nt3d.Heightfield{NX: 2, NY: 2, MinX: -1.8, MaxX: 1.8, MinY: -1.8, MaxY: 1.8, Heights: []float32{-.08, -.08, -.08, -.08}, Material: &nt3d.Material{Texture: texture}})
	if err != nil {
		return err
	}
	id, err := d.scene.SetGeometry("floor", floor)
	if err != nil {
		return err
	}
	if err = d.scene.Reparent(id, d.group); err != nil {
		return err
	}
	d.dynamic.mesh, err = d.scene.SetGeometry("wave", waveGeometry(0))
	if err != nil {
		return err
	}
	if err = d.scene.Reparent(d.dynamic.mesh, d.group); err != nil {
		return err
	}
	for range 12 {
		if err = d.addBody(); err != nil {
			return err
		}
	}
	return d.stepDynamic()
}

func waveGeometry(phase float64) nt3d.Geometry {
	// Change topology as well as vertex positions; only this node is replaced.
	size := 18
	if math.Sin(phase) > .3 {
		size = 26
	}
	g, err := nt3d.BuildHeightfield(nt3d.Heightfield{NX: size, NY: size, MinX: -.72, MaxX: .72, MinY: -.72, MaxY: .72, Sampler: func(x, y float32) float32 {
		r := math.Hypot(float64(x), float64(y))
		return float32(.35 + .2*math.Cos(r*8-phase*2)*math.Exp(-r*r))
	}})
	if err != nil {
		panic(err)
	} // Fixed valid dimensions and finite animation phase.
	for i := range g.Vertices {
		z := g.Vertices[i].Position.Z
		g.Vertices[i].Color = color.RGBA{uint8(35 + 90*z), uint8(140 + 150*z), uint8(180 + 100*z), 255}
	}
	return g
}

func (d *demo) addBody() error {
	if len(d.dynamic.bodies) >= 48 {
		return nil
	}
	index := d.dynamic.serial
	d.dynamic.serial++
	palette := []color.RGBA{{238, 133, 64, 255}, {160, 119, 224, 255}, {234, 196, 83, 255}}
	g := nt3d.Geometry{Boxes: []nt3d.Box{{Min: nt3dmath.Vec3{X: -.09, Y: -.09}, Size: nt3dmath.Vec3{X: .18, Y: .18, Z: .2}, Color: palette[index%len(palette)]}}}
	id, err := d.scene.SetGeometry(fmt.Sprintf("orbiter-%d", index+1), g)
	if err != nil {
		return err
	}
	if err = d.scene.Reparent(id, d.group); err != nil {
		return err
	}
	d.dynamic.bodies = append(d.dynamic.bodies, id)
	return nil
}
func (d *demo) removeBody() error {
	if len(d.dynamic.bodies) == 0 {
		return nil
	}
	// Remove the first entry to exercise changes in subsequent frame indices.
	if err := d.scene.Remove(d.dynamic.bodies[0]); err != nil {
		return err
	}
	d.dynamic.bodies = d.dynamic.bodies[1:]
	return nil
}
func (d *demo) stepDynamic() error {
	if d.dynamic.deform {
		if err := d.scene.SetNodeGeometry(d.dynamic.mesh, waveGeometry(d.phase)); err != nil {
			return err
		}
	}
	for i, id := range d.dynamic.bodies {
		angle := d.phase + float64(i)*2*math.Pi/float64(len(d.dynamic.bodies))
		m := nt3dmath.Translation(nt3dmath.Vec3{X: float32(1.3 * math.Cos(angle)), Y: float32(1.3 * math.Sin(angle)), Z: float32(.25 + .18*math.Sin(angle*3+d.phase))}).Mul(nt3dmath.RotationZ(angle))
		if err := d.scene.SetTransform(id, m); err != nil {
			return err
		}
	}
	return nil
}
