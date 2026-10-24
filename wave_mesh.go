// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"image/color"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// WaveGrid describes a finite XY water mesh split into independently bounded
// tiles. NX/NY count vertices over the entire extent; TileCells is the maximum
// cells per tile edge (default 32, at most 128). It does not implement LOD.
// Geometry.UV spans the whole grid for flow maps. NormalUV uses world XY with
// a Y flip and NormalTileSize world units per repeat (default 1).
type WaveGrid struct {
	NX, NY, TileCells      int
	MinX, MaxX, MinY, MaxY float32
	NormalTileSize         float32
	Color                  color.RGBA
	Material               *Material
}

// WaveTile owns mutable geometry with fixed topology and immutable rest-grid
// coordinates. Add each Geometry as a separate Scene node for frustum culling.
// Update then publish it with SetNodeGeometry. Updating a tile is not concurrent
// safe; distinct tiles can be updated independently against one WaveSurface.
type WaveTile struct {
	Geometry     Geometry
	grid         WaveGrid
	x, y, nx, ny int
}

// BuildWaveTiles allocates at most 1024 tiles and 1,048,576 vertices including
// shared borders. Neighbors evaluate identical global grid coordinates, giving
// exactly matching positions, normals, tangents, and UVs on every shared edge.
func BuildWaveTiles(d WaveGrid) ([]*WaveTile, error) {
	if d.TileCells == 0 {
		d.TileCells = 32
	}
	if d.NormalTileSize == 0 {
		d.NormalTileSize = 1
	}
	if d.NX < 2 || d.NY < 2 || d.NX > 4097 || d.NY > 4097 || d.TileCells < 1 || d.TileCells > 128 || !validFloat(d.MinX, d.MaxX, d.MinY, d.MaxY, d.NormalTileSize) || d.MinX >= d.MaxX || d.MinY >= d.MaxY || d.NormalTileSize <= 0 {
		return nil, fmt.Errorf("nt3d: invalid wave grid")
	}
	tx, ty := (d.NX-2)/d.TileCells+1, (d.NY-2)/d.TileCells+1
	if tx*ty > 1024 || (d.NX-1+tx)*(d.NY-1+ty) > 1<<20 {
		return nil, fmt.Errorf("nt3d: wave grid exceeds tile or vertex budget")
	}
	var tiles []*WaveTile
	for y := 0; y < d.NY-1; y += d.TileCells {
		for x := 0; x < d.NX-1; x += d.TileCells {
			nx, ny := min(d.TileCells, d.NX-1-x)+1, min(d.TileCells, d.NY-1-y)+1
			tile := &WaveTile{grid: d, x: x, y: y, nx: nx, ny: ny}
			tile.grid.Material = nil // Rest coordinates do not retain material snapshots.
			loX, loY := tile.rest(0, 0)
			hiX, hiY := tile.rest(nx-1, ny-1)
			g, err := BuildHeightfield(Heightfield{NX: nx, NY: ny, MinX: float32(loX), MaxX: float32(hiX), MinY: float32(loY), MaxY: float32(hiY), Sampler: func(float32, float32) float32 { return 0 }, Color: d.Color, Material: d.Material})
			if err != nil {
				return nil, err
			}
			g.NormalUV = make([]UV, nx*ny)
			g.Tangents = make([]Tangent, nx*ny)
			for j := range ny {
				for i := range nx {
					at := j*nx + i
					px, py := tile.rest(i, j)
					g.UV[at] = UV{float32(x+i) / float32(d.NX-1), 1 - float32(y+j)/float32(d.NY-1)}
					g.NormalUV[at] = UV{float32(px / float64(d.NormalTileSize)), float32(-py / float64(d.NormalTileSize))}
				}
			}
			tile.Geometry = g
			if err = tile.Update(&WaveSurface{}, 0); err != nil {
				return nil, err
			}
			if err = tile.Geometry.Validate(); err != nil {
				return nil, err
			}
			tiles = append(tiles, tile)
		}
	}
	return tiles, nil
}
func (t *WaveTile) rest(x, y int) (float64, float64) {
	d := t.grid
	return float64(d.MinX) + (float64(d.MaxX)-float64(d.MinX))*float64(t.x+x)/float64(d.NX-1), float64(d.MinY) + (float64(d.MaxY)-float64(d.MinY))*float64(t.y+y)/float64(d.NY-1)
}

// Update samples the analytic surface in world coordinates. It reuses vertex
// and tangent storage and updates exact bounds; indices and UVs stay unchanged.
func (t *WaveTile) Update(surface *WaveSurface, time float64) error {
	if t == nil || surface == nil || !waveFinite(time) || t.nx < 2 || t.ny < 2 || len(t.Geometry.Vertices) != t.nx*t.ny || len(t.Geometry.Tangents) != t.nx*t.ny {
		return fmt.Errorf("nt3d: invalid wave tile update")
	}
	for y := range t.ny {
		for x := range t.nx {
			px, py := t.rest(x, y)
			s, err := surface.Sample(px, py, time)
			if err != nil {
				return err
			}
			at := y*t.nx + x
			t.Geometry.Vertices[at].Position, t.Geometry.Vertices[at].Normal = s.Position, s.Normal
			t.Geometry.Tangents[at] = Tangent{Direction: s.TangentX.Normalize(), W: -1}
		}
	}
	t.Geometry.Bounds = GeometryBounds(t.Geometry)
	return nil
}

// RestBounds is this tile's undisplaced XY extent at mean water height zero.
func (t *WaveTile) RestBounds() nt3dmath.AABB {
	x, y := t.rest(0, 0)
	xx, yy := t.rest(t.nx-1, t.ny-1)
	return nt3dmath.AABB{Min: nt3dmath.Vec3{X: float32(x), Y: float32(y)}, Max: nt3dmath.Vec3{X: float32(xx), Y: float32(yy)}}
}
