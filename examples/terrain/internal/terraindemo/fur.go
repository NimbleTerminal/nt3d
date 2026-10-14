// Copyright (c) 2026 Neomantra Corp.
package terraindemo

import (
	"fmt"
	"image/color"
	"math"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
)

// FurMain shows shell fur on a torus, swaying in the wind.
func FurMain() { mainDemo(false, sceneFur) }

var (
	furShells  = []int{8, 16, 32}
	furLengths = []float32{.16, .3}
)

type furScene struct {
	base           nt3d.Geometry
	coat           *nt3d.FurCoat
	nodes          []nt3d.NodeID
	shells, length int // Indexes into furShells and furLengths.
}

func (d *demo) setupFur() error {
	base, err := nt3d.BuildTorus(nt3d.Torus{MajorRadius: .75, MinorRadius: .32, Segments: 64, Sides: 24})
	if err != nil {
		return err
	}
	d.fur = &furScene{base: base, shells: 1}
	return d.growFur()
}

// growFur makes the coat's textures and shells, replacing the last ones.
// The textures are the slow part; swaying only moves the shells' nodes.
func (d *demo) growFur() error {
	for _, id := range d.fur.nodes {
		if err := d.scene.Remove(id); err != nil {
			return err
		}
	}
	d.fur.nodes = nil
	coat, err := nt3d.NewFur(nt3d.Fur{Length: furLengths[d.fur.length], Shells: furShells[d.fur.shells], Density: 96,
		Root: color.RGBA{96, 44, 18, 255}, Tip: color.RGBA{250, 182, 104, 255}, Seed: 7})
	if err != nil {
		return err
	}
	shells, err := coat.Shells(d.fur.base)
	if err != nil {
		return err
	}
	for k, g := range shells {
		id, err := d.scene.SetGeometry(fmt.Sprintf("fur shell %d", k), g)
		if err != nil {
			return err
		}
		if err = d.scene.Reparent(id, d.group); err != nil {
			return err
		}
		d.fur.nodes = append(d.fur.nodes, id)
	}
	d.fur.coat = coat
	return d.stepFur()
}

// stepFur sways the fur with a slowly turning breeze.
func (d *demo) stepFur() error {
	wind := nt3dmath.Vec3{X: float32(.08 * math.Sin(d.phase*1.3)), Y: float32(.06 * math.Sin(d.phase*.9+1)), Z: -.03}
	for k, id := range d.fur.nodes {
		if err := d.scene.SetTransform(id, d.fur.coat.Sway(k, wind)); err != nil {
			return err
		}
	}
	return nil
}

func (d *demo) furView(content string) string {
	shells := furShells[d.fur.shells]
	return fmt.Sprintf("nt3d fur · a wind · s shells · l length · c fit · n rotate · 1/2/3 backend · q quit\n%s\n%s · %d shells × %d triangles · %s",
		content, d.view.RenderMode(), shells, len(d.fur.base.Indices)/3, d.status)
}
