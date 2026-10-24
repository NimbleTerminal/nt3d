// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"nimbleterminal.dev/nt3d"
)

type waterTile struct {
	mesh, foam nt3d.NodeID
	wave       *nt3d.WaveTile
}

func waterCoast(y float64) float64 { return -3.9 + .25*math.Sin(y*.9) }
func waterBedHeight(x, y float64) float64 {
	return math.Min(.9, math.Max(-2.8, .65*(waterCoast(y)-x))) + .035*math.Sin(y*2)*math.Min(1, math.Max(0, (x-waterCoast(y))*.5))
}

// Keep the hull clear of dry land. This is an arcade bank constraint, not
// shallow-water hydrodynamics; the craft still samples the same WaveSurface.
func waterBank(c *waterCraft) {
	limit := waterCoast(c.y) + .95
	if c.x >= limit {
		return
	}
	c.x = limit
	n := vec(1, -.225*math.Cos(c.y*.9), 0).Normalize()
	v := vec(math.Cos(c.heading), math.Sin(c.heading), 0)
	if dot := v.Dot(n); dot < 0 {
		v = v.Sub(n.Scale(2 * dot))
		c.heading = math.Atan2(float64(v.Y), float64(v.X))
	}
}

func waterCurrent(x, y float64) (u, v float64) {
	// Flow bends along the bank, accelerating across the channel. V flips to
	// match the normal texture's downward image coordinate.
	strength := .25 + .35*math.Min(1, math.Max(0, (x-waterCoast(y))/3))
	return strength * .225 * math.Cos(y*.9), -strength
}
func makeWaterFlow(extent float64) (*nt3d.FlowMap, error) {
	const size = 64
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	encode := func(x float64) uint8 { return uint8(math.Round((x*.5 + .5) * 255)) }
	for y := range size {
		for x := range size {
			wx, wy := ((float64(x)+.5)/size*2-1)*extent, (1-(float64(y)+.5)/size*2)*extent
			u, v := waterCurrent(wx, wy)
			img.SetNRGBA(x, y, color.NRGBA{encode(u), encode(v), 0, 255})
		}
	}
	return nt3d.NewFlowMap(img)
}

func (w *waterScene) shoreName() string {
	return [...]string{"shore+flow", "shore", "flow", "plain"}[w.shoreMode]
}
func (w *waterScene) shoreOn() bool { return w.shoreMode < 2 }
func (w *waterScene) flowOn() bool  { return w.shoreMode == 0 || w.shoreMode == 2 }

func (d *demo) rebuildWaterTiles() error {
	w := d.water
	extent, n := float32(5), 81
	if w.large {
		extent, n = 15, 161
	}
	flow, err := makeWaterFlow(float64(extent))
	if err != nil {
		return err
	}
	// A valid normal map is needed when the material carries flow data.
	normal, err := waterRipples(d.animationTime)
	if err != nil {
		return err
	}
	material := *w.grid.Material
	material.NormalMap = normal
	material.FlowMap = flow
	tiles, err := nt3d.BuildWaveTiles(nt3d.WaveGrid{NX: n, NY: n, TileCells: 40, MinX: -extent, MaxX: extent, MinY: -extent, MaxY: extent, NormalTileSize: 2.5, Color: color.RGBA{28, 127, 155, 255}, Material: &material})
	if err != nil {
		return err
	}
	for _, t := range w.tiles {
		if err = d.scene.Remove(t.mesh); err != nil {
			return err
		}
		if err = d.scene.Remove(t.foam); err != nil {
			return err
		}
	}
	w.tiles = nil
	for i, t := range tiles {
		mesh, err := d.scene.SetGeometry(fmt.Sprintf("water-%d", i), t.Geometry)
		if err != nil {
			return err
		}
		if err = d.scene.Reparent(mesh, d.group); err != nil {
			return err
		}
		foam, err := d.scene.SetGeometry(fmt.Sprintf("shore-foam-%d", i), nt3d.Geometry{})
		if err != nil {
			return err
		}
		if err = d.scene.Reparent(foam, d.group); err != nil {
			return err
		}
		w.tiles = append(w.tiles, waterTile{mesh: mesh, foam: foam, wave: t})
	}
	w.extent = float64(extent)
	w.effects.extent = w.extent - .35
	w.craft.bound = w.extent - .9
	w.craft.shoreline = true
	if math.Abs(w.craft.x) > w.craft.bound || math.Abs(w.craft.y) > w.craft.bound {
		w.craft.x = math.Max(-w.craft.bound, math.Min(w.craft.bound, w.craft.x))
		w.craft.y = math.Max(-w.craft.bound, math.Min(w.craft.bound, w.craft.y))
		waterBank(&w.craft)
		w.craft.ready = false
		w.craft.vz, w.craft.airborne = 0, false
		w.effects.count, w.effects.first = 0, 0
		w.effects.contact = false
		w.effects.drops = [waterSprayCapacity]sprayDrop{}
	}

	// The first tile remains available to the existing analytic-surface checks.
	w.mesh, w.grid = w.tiles[0].mesh, w.tiles[0].wave.Geometry
	if err = d.setupWaterBed(); err != nil {
		return err
	}
	return d.stepWater()
}

func shoreFoam(g nt3d.Geometry, time float64) nt3d.Geometry {
	out := nt3d.Geometry{Material: &nt3d.Material{Texture: wakeFoam(), Repeat: true, Tint: true, Transparent: true, Unlit: true}}
	for i := 0; i < len(g.Indices); i += 3 {
		var depth [3]float64
		for k := range 3 {
			p := g.Vertices[g.Indices[i+k]].Position
			depth[k] = float64(p.Z) - waterBedHeight(float64(p.X), float64(p.Y))
		}
		if min(depth[0], depth[1], depth[2]) > .55 || max(depth[0], depth[1], depth[2]) <= .015 {
			continue
		}
		for k := range 3 {
			v := g.Vertices[g.Indices[i+k]]
			x, y := float64(v.Position.X), float64(v.Position.Y)
			fade := math.Min(1, math.Max(0, (depth[k]-.015)/.08)) * math.Min(1, math.Max(0, (.55-depth[k])/.35))
			pulse := .75 + .25*math.Sin(y*9+x*4-time*1.6)
			v.Position.Z += .018
			v.Color = color.RGBA{240, 250, 246, uint8(235 * fade * pulse)}
			out.Indices = append(out.Indices, uint32(len(out.Vertices)))
			out.Vertices = append(out.Vertices, v)
			out.UV = append(out.UV, nt3d.UV{U: float32(y*.8 - time*.06), V: float32(x * .8)})
		}
	}
	out.Bounds = nt3d.GeometryBounds(out)
	return out
}

func (d *demo) stepWaterTiles(material *nt3d.Material) error {
	w := d.water
	for _, tile := range w.tiles {
		m := *material
		m.FlowMap = tile.wave.Geometry.Material.FlowMap
		m.FlowPhase = float32(math.Mod(d.animationTime/3, 1))
		m.FlowStrength = 0
		if w.flowOn() {
			m.FlowStrength = .7
		}
		tile.wave.Geometry.Material = &m
		if err := tile.wave.Update(w.surface, d.animationTime); err != nil {
			return err
		}
		if err := d.scene.SetNodeGeometry(tile.mesh, tile.wave.Geometry); err != nil {
			return err
		}
		if err := d.scene.SetVisible(tile.foam, w.shoreOn()); err != nil {
			return err
		}
		if w.shoreOn() {
			if err := d.scene.SetNodeGeometry(tile.foam, shoreFoam(tile.wave.Geometry, d.animationTime)); err != nil {
				return err
			}
		}
	}
	w.grid = w.tiles[0].wave.Geometry
	return nil
}

// Sloping terrain intersects the wave mesh naturally through the scene depth
// buffer, including refraction and soft-particle depth captures.
func (d *demo) setupWaterBed() error {
	e := float32(d.water.extent)
	if e == 0 {
		e = 5
	}
	n := int(e*8) + 1
	g, err := nt3d.BuildHeightfield(nt3d.Heightfield{NX: n, NY: n, MinX: -e, MaxX: e, MinY: -e, MaxY: e, Sampler: func(x, y float32) float32 { return float32(waterBedHeight(float64(x), float64(y))) }, Color: color.RGBA{190, 172, 125, 255}})
	if err != nil {
		return err
	}
	for i := range g.Vertices {
		p := g.Vertices[i].Position
		switch {
		case p.Z > .3:
			g.Vertices[i].Color = color.RGBA{96, 132, 69, 255}
		case p.Z > -.08:
			g.Vertices[i].Color = color.RGBA{198, 182, 133, 255}
		case (int(math.Floor(float64(p.X+15)*2))+int(math.Floor(float64(p.Y+15)*2)))%2 == 0:
			g.Vertices[i].Color = color.RGBA{117, 132, 112, 255}
		}
	}
	id, err := d.scene.SetGeometry("seabed", g)
	if err != nil {
		return err
	}
	return d.scene.Reparent(id, d.group)
}
