// Copyright (c) 2026 Neomantra Corp.

package demo

import (
	"bytes"
	"fmt"
	"image/png"
	"io/fs"
	"math"

	tea "charm.land/bubbletea/v2"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/fur"
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
	fins           nt3d.NodeID // Zero when fins are off.
	noFins         bool
	fullSize       bool // Shells and fins sample full size, as before cutout mipmaps.
	effect         int  // Index into effects.
	toon           bool // Cel shaded, the skin inked at the fur's tips.
	base           nt3d.Geometry
	coat           *nt3dfur.Coat
	nodes          []nt3d.NodeID
	shells, length int // Indexes into furShells and furLengths.
}

// spot reads Spot, by Keenan Crane (public domain), with its texture, and
// smooths its normals: shells pushed out along flat normals would crack
// apart at every edge.
func spot() (nt3d.Geometry, error) {
	data, err := fs.ReadFile(SampleFiles, "spot_triangulated.obj")
	if err != nil {
		return nt3d.Geometry{}, err
	}
	m, err := nt3dmesh.DecodeOBJ(data, &nt3dmesh.Options{Up: nt3dmesh.YUp})
	if err != nil {
		return nt3d.Geometry{}, err
	}
	pixels, err := fs.ReadFile(SampleFiles, "spot_texture.png")
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
	if d.fur.fins != 0 {
		if err := d.scene.Remove(d.fur.fins); err != nil {
			return err
		}
		d.fur.fins = 0
	}
	// Spot's texture colors the fur: black and white, darker at the roots.
	coat, err := nt3dfur.New(nt3dfur.Config{Length: furLengths[d.fur.length], Shells: furShells[d.fur.shells], Spacing: .011, Seed: 7})
	if err != nil {
		return err
	}
	shells, err := coat.Shells(d.fur.base)
	if err != nil {
		return err
	}
	for k, g := range shells {
		if k > 0 { // The skin is opaque; the shells are cutouts.
			g.Material.FullSize = d.fur.fullSize
		}
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
	// Fins fill the silhouette, where the shells' layers would show as dashes.
	if !d.fur.noFins {
		fins, err := coat.Fins(d.fur.base)
		if err != nil {
			return err
		}
		fins.Material.FullSize = d.fur.fullSize
		if d.fur.fins, err = d.scene.SetGeometry("fur fins", fins); err != nil {
			return err
		}
		if err = d.scene.Reparent(d.fur.fins, d.group); err != nil {
			return err
		}
	}
	if err := d.applyFurToon(); err != nil {
		return err
	}
	return d.stepFur()
}

// applyFurToon gives the coat its look: toon bands on the skin, shells, and
// fins alike, which share the skin's normals and so its bands, and ink on the
// skin alone, a hull pushed out as far as the fur is long, so the line sits at
// the tips. A hull on the shells and fins would fringe every strand.
func (d *demo) applyFurToon() error {
	style, ink := nt3d.ToonStyle{}, nt3d.OutlineStyle{}
	if d.fur.toon {
		style = nt3d.ToonStyle{Bands: 3, Shade: toonShade}
		ink = nt3d.OutlineStyle{Width: furLengths[d.fur.length], Units: nt3d.WorldUnits, Color: toonInk}
	}
	if err := d.scene.SetToon(d.group, style); err != nil {
		return err
	}
	return d.scene.SetOutline(d.fur.nodes[0], ink)
}

func (d *demo) toggleFurToon() error {
	d.fur.toon = !d.fur.toon
	return d.applyFurToon()
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

// cycleSupersample steps GPU anti-aliasing through 1×, 4× MSAA, 2× SS, and 3× SS.
func (d *demo) cycleSupersample() tea.Cmd {
	msaa, ss := d.view.MSAA(), d.view.Supersample()
	switch {
	case !msaa && ss == 1:
		_ = d.view.SetSupersample(1)
		return d.view.SetMSAA(true)
	case msaa && ss == 1:
		_ = d.view.SetMSAA(false)
		return d.view.SetSupersample(2)
	case !msaa && ss == 2:
		_ = d.view.SetMSAA(false)
		return d.view.SetSupersample(3)
	default:
		_ = d.view.SetMSAA(false)
		return d.view.SetSupersample(1)
	}
}

func (d *demo) aaLabel() string {
	if d.view.MSAA() {
		if d.view.Supersample() > 1 {
			return fmt.Sprintf("4× MSAA + %d× SS", d.view.Supersample())
		}
		return "4× MSAA"
	}
	return fmt.Sprintf("supersample %d×", d.view.Supersample())
}

// cycleEffect steps through the post-processing effects.
func (d *demo) cycleEffect() tea.Cmd {
	d.fur.effect = (d.fur.effect + 1) % len(effects)
	e := effects[d.fur.effect]
	_ = d.view.SetEffects(e.post)
	return d.view.SetPostProcess(e.apply)
}

func (d *demo) furView(content string) string {
	fins, mips, look := "fins", "mipmaps", "smooth"
	if d.fur.toon {
		look = "toon"
	}
	if d.fur.noFins {
		fins = "no fins"
	}
	if d.fur.fullSize {
		mips = "full size"
	}
	return fmt.Sprintf("nt3d fur · Spot by Keenan Crane · a wind · s shells · l length · f fins · m mipmaps · t toon · x AA · e effect · c fit · 1/2/3 backend · q quit\n%s\n%s · %d shells × %d triangles · %s · %s · %s · %s · %s · %s",
		content, d.rendering(), furShells[d.fur.shells], len(d.fur.base.Indices)/3, fins, mips, look, d.aaLabel(), effects[d.fur.effect].name, d.status)
}
