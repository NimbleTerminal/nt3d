// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"fmt"
	"image/color"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
)

// The terrain gallery demonstrates pixel and world sizes, multiline text,
// parent motion, depth occlusion, picking, and updates when flattening terrain.
func (d *demo) terrainLabels(terrain nt3d.NodeID) error {
	peak := float32(.6)
	if d.flat {
		peak = 0
	}
	for i, annotation := range []struct {
		text     string
		position nt3dmath.Vec3
		pixels   bool
		height   float32
	}{
		{fmt.Sprintf("Peak\nz = %.2f", peak), nt3dmath.Vec3{Z: peak + .2}, true, 32},
		{"West ridge", nt3dmath.Vec3{X: -1, Y: .35, Z: .25}, false, .22},
		{"Valley", nt3dmath.Vec3{X: 1, Y: -.4, Z: .12}, true, 18},
	} {
		id, err := d.scene.SetLabel(fmt.Sprintf("terrain-label-%d", i), nt3d.Label{
			Text: annotation.text, Height: annotation.height, Pixels: annotation.pixels,
			Color: color.RGBA{245, 248, 255, 255}, Background: color.RGBA{24, 32, 48, 255},
			BorderColor: color.RGBA{90, 160, 210, 255}, Padding: 2, Border: 1,
		})
		if err != nil {
			return err
		}
		if err = d.scene.Reparent(id, terrain); err != nil {
			return err
		}
		if err = d.scene.SetTransform(id, nt3dmath.Translation(annotation.position)); err != nil {
			return err
		}
	}
	return nil
}
