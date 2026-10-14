// Copyright (c) 2026 Neomantra Corp.
package terraindemo

import (
	"bytes"
	"fmt"
	"image/png"
	"io/fs"
	"math"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/examples/terrain/internal/terraindemo/models"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
)

// FurMain shows shell fur on Spot the cow, swaying in the wind.
func FurMain() { mainDemo(false, sceneFur) }

var (
	furShells  = []int{8, 16, 32}
	furLengths = []float32{.05, .09}
)

type furScene struct {
	base           nt3d.Geometry
	coat           *nt3d.FurCoat
	nodes          []nt3d.NodeID
	shells, length int // Indexes into furShells and furLengths.
}

// spot reads Spot, by Keenan Crane (public domain), with its texture, and
// smooths its normals: shells pushed out along flat normals would crack
// apart at every edge.
func spot() (nt3d.Geometry, error) {
	data, err := fs.ReadFile(models.Files, "spot_triangulated.obj")
	if err != nil {
		return nt3d.Geometry{}, err
	}
	m, err := nt3dmesh.DecodeOBJ(data, &nt3dmesh.Options{Up: nt3dmesh.YUp})
	if err != nil {
		return nt3d.Geometry{}, err
	}
	pixels, err := fs.ReadFile(models.Files, "spot_texture.png")
	if err != nil {
		return nt3d.Geometry{}, err
	}
	img, err := png.Decode(bytes.NewReader(pixels))
	if err != nil {
		return nt3d.Geometry{}, err
	}
	texture, err := nt3d.NewTexture(img)
	if err != nil {
		return nt3d.Geometry{}, err
	}
	g, err := nt3d.SmoothNormals(m.Objects[0].Geometry, 180)
	if err != nil {
		return nt3d.Geometry{}, err
	}
	g.Material = &nt3d.Material{Texture: texture}
	return g, nil
}

func (d *demo) setupFur() error {
	base, err := spot()
	if err != nil {
		return err
	}
	d.fur = &furScene{base: base, shells: 1}
	return d.growFur()
}

// growFur grows the coat's shells, replacing the last ones. Making layer
// textures is the slow part; swaying only moves the shells' nodes.
func (d *demo) growFur() error {
	for _, id := range d.fur.nodes {
		if err := d.scene.Remove(id); err != nil {
			return err
		}
	}
	d.fur.nodes = nil
	// Spot's texture colors the fur: black and white, darker at the roots.
	coat, err := nt3d.NewFur(nt3d.Fur{Length: furLengths[d.fur.length], Shells: furShells[d.fur.shells], Spacing: .011, Seed: 7})
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
	s := furLengths[d.fur.length]
	wind := nt3dmath.Vec3{X: s * float32(.45*math.Sin(d.phase*1.3)), Y: s * float32(.35*math.Sin(d.phase*.9+1)), Z: -.2 * s}
	for k, id := range d.fur.nodes {
		if err := d.scene.SetTransform(id, d.fur.coat.Sway(k, wind)); err != nil {
			return err
		}
	}
	return nil
}

func (d *demo) furView(content string) string {
	return fmt.Sprintf("nt3d fur · Spot by Keenan Crane · a wind · s shells · l length · c fit · n rotate · 1/2/3 backend · q quit\n%s\n%s · %d shells × %d triangles · %s",
		content, d.view.RenderMode(), furShells[d.fur.shells], len(d.fur.base.Indices)/3, d.status)
}
