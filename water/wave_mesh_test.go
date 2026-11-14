// Copyright (c) 2026 Neomantra Corp.

package nt3dwater

import (
	"math"
	"testing"

	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func TestWaveTilesMatchBordersAndQueries(t *testing.T) {
	surface, err := NewWaveSurface([]GerstnerWave{{Direction: nt3dmath.Vec3{X: 1, Y: .3}, Wavelength: 3, Amplitude: .2}}, 9.81)
	check(t, err)
	tiles, err := BuildWaveTiles(WaveGrid{NX: 74, NY: 66, TileCells: 24, MinX: -15, MaxX: 15, MinY: -12, MaxY: 12, NormalTileSize: 2.5})
	check(t, err)
	if len(tiles) != 12 {
		t.Fatal("partial border tiles lost", len(tiles))
	}
	for _, time := range []float64{0, .7, 123} {
		type vertex struct {
			v            nt3d.Vertex
			t            nt3d.Tangent
			uv, normalUV nt3d.UV
		}
		seen := map[[2]int]vertex{}
		for _, tile := range tiles {
			p := &tile.Geometry.Vertices[0]
			index := &tile.Geometry.Indices[0]
			check(t, tile.Update(surface, time))
			if p != &tile.Geometry.Vertices[0] || index != &tile.Geometry.Indices[0] {
				t.Fatal("update allocated mesh storage")
			}
			for y := range tile.ny {
				for x := range tile.nx {
					i := y*tile.nx + x
					v := tile.Geometry.Vertices[i]
					key := [2]int{tile.x + x, tile.y + y}
					got := vertex{v, tile.Geometry.Tangents[i], tile.Geometry.UV[i], tile.Geometry.NormalUV[i]}
					if previous, ok := seen[key]; ok && previous != got {
						t.Fatal("tile crack", key, previous, got)
					}
					seen[key] = got
					q, err := surface.AtWorld(float64(v.Position.X), float64(v.Position.Y), time)
					check(t, err)
					if math.Abs(float64(v.Position.Z-q.Position.Z)) > 1e-5 {
						t.Fatal("mesh/query disagreement")
					}
				}
			}
			check(t, tile.Geometry.Validate())
		}
		if len(seen) != 74*66 {
			t.Fatal("missing global cells", len(seen))
		}
	}
}

func TestWaveGridBudgets(t *testing.T) {
	for _, d := range []WaveGrid{{NX: 1, NY: 2, MaxX: 1, MaxY: 1}, {NX: 4097, NY: 4097, MaxX: 1, MaxY: 1}, {NX: 100, NY: 100, TileCells: 1, MaxX: 1, MaxY: 1}, {NX: 2, NY: 2, MaxX: 1, MaxY: 1, NormalTileSize: -1}} {
		if _, err := BuildWaveTiles(d); err == nil {
			t.Fatal("accepted invalid/budget-exceeding grid", d)
		}
	}
}

func TestWaveTileUpdateAllocations(t *testing.T) {
	tiles, err := BuildWaveTiles(WaveGrid{NX: 17, NY: 17, MinX: -2, MaxX: 2, MinY: -2, MaxY: 2})
	check(t, err)
	surface := &WaveSurface{}
	if allocations := testing.AllocsPerRun(10, func() {
		if err := tiles[0].Update(surface, 1); err != nil {
			panic(err)
		}
	}); allocations != 0 {
		t.Fatal("tile updates allocate", allocations)
	}
}

func TestWaveTileApplyPreservesSceneTopology(t *testing.T) {
	surface, err := NewWaveSurface([]GerstnerWave{{Direction: nt3dmath.Vec3{X: 1}, Wavelength: 3, Amplitude: .25}}, 9.81)
	check(t, err)
	tiles, err := BuildWaveTiles(WaveGrid{NX: 9, NY: 9, MinX: -2, MaxX: 2, MinY: -2, MaxY: 2})
	check(t, err)
	s := nt3d.NewScene()
	id, err := s.SetGeometry("tile-0", tiles[0].Geometry)
	check(t, err)
	f0, err := s.Frame(nt3d.DefaultCamera(), 64, 64)
	check(t, err)
	check(t, tiles[0].Update(surface, 1.3))
	check(t, tiles[0].Apply(s, id))
	f1, err := s.Frame(nt3d.DefaultCamera(), 64, 64)
	check(t, err)
	if f0.GeometryRevisions[0] != f1.GeometryRevisions[0] {
		t.Fatal("WaveTile.Apply changed geometryRevision")
	}
	if &f0.Geometry[0].Indices[0] != &f1.Geometry[0].Indices[0] {
		t.Fatal("WaveTile.Apply cloned static indices")
	}
	if f0.Geometry[0].Vertices[40].Position == f1.Geometry[0].Vertices[40].Position {
		t.Fatal("WaveTile.Apply did not advance wave vertices")
	}
}
