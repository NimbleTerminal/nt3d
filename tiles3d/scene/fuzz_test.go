// Copyright (c) 2026 Neomantra Corp.

package nt3dtiles3dscene

import (
	"context"
	"testing"
	"time"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
	"nimbleterminal.dev/nt3d/tiles3d"
)

// fuzzTileset is a small tileset around the Earth's point 0, 0: a root that
// replaces, with one child, their content beside them.
const fuzzTileset = `{"asset":{"version":"1.1"},"geometricError":50,"root":{"transform":[0,1,0,0, 0,0,1,0, 1,0,0,0, 6378137,0,0,1],"boundingVolume":{"box":[0,0,5, 50,0,0, 0,50,0, 0,0,5]},"geometricError":10,"refine":"REPLACE","content":{"uri":"a.glb"},"children":[{"boundingVolume":{"sphere":[0,0,5,40]},"geometricError":0,"content":{"uri":"b.glb"}}]}}`

// fuzzImplicit is an implicit quadtree over the same ground.
const fuzzImplicit = `{"asset":{"version":"1.1"},"geometricError":50,"root":{"transform":[0,1,0,0, 0,0,1,0, 1,0,0,0, 6378137,0,0,1],"boundingVolume":{"box":[0,0,5, 50,0,0, 0,50,0, 0,0,5]},"geometricError":20,"refine":"ADD","content":{"uri":"{level}_{x}_{y}.glb"},"implicitTiling":{"subdivisionScheme":"QUADTREE","subtreeLevels":2,"availableLevels":4,"subtrees":{"uri":"{level}_{x}_{y}.subtree"}}}}`

// FuzzStream streams an arbitrary tileset whose every other file, content
// and subtree alike, is one arbitrary blob, for an arbitrary camera. Nothing
// may panic or hang, the scene must still make a frame, what the tileset
// reports must add up, and closing it must leave nothing behind.
func FuzzStream(f *testing.F) {
	mesh := triangle(0, 0, 0, "", "")
	f.Add([]byte(fuzzTileset), mesh, float32(0), float32(-60), float32(30), false)
	// From far off, through an orthographic camera. (A splat tile's JSON
	// alone is over the kilobyte a seed should stay under; splat content is
	// fuzzed where it is decoded, in mesh/gltf.)
	f.Add([]byte(fuzzTileset), mesh, float32(10), float32(-400), float32(100), true)
	f.Add([]byte(fuzzImplicit), []byte(`{"tileAvailability":{"constant":1},"contentAvailability":[{"constant":1}],"childSubtreeAvailability":{"constant":1}}`), float32(0), float32(-20), float32(8), false)
	// A tileset whose content is itself.
	f.Add([]byte(fuzzTileset), []byte(fuzzTileset), float32(0), float32(-60), float32(30), false)
	f.Add([]byte(fuzzTileset), b3dm(nt3dmath.Vec3d{X: 1, Y: 2, Z: 3}, mesh), float32(0), float32(0), float32(5), false)
	f.Fuzz(func(t *testing.T, tileset, other []byte, x, y, z float32, ortho bool) {
		fetch := nt3dtiles3d.FetcherFunc(func(_ context.Context, uri string) ([]byte, error) {
			if uri == "tileset.json" {
				return tileset, nil
			}
			return other, nil
		})
		scene := nt3d.NewScene()
		ts, err := Open(context.Background(), scene, fetch, "tileset.json", Options{
			AtCenter:         true,
			Workers:          2,
			MaxExternalDepth: 3,
			MaxBytes:         1 << 16,
			SplatBudget:      64,
			Tiles:            &nt3dtiles3d.Options{MaxBytes: 1 << 16, MaxTiles: 64, MaxDepth: 8},
			Mesh:             &nt3dmesh.Options{MaxBytes: 1 << 20, MaxTriangles: 1 << 12},
		})
		if err != nil {
			return
		}
		defer ts.Close()
		camera := nt3d.DefaultCamera().LookAt(nt3dmath.Vec3{X: x, Y: y, Z: z}, nt3dmath.Vec3{})
		if !ortho {
			camera.Projection = nt3d.Perspective
		}
		if camera.Validate() != nil {
			return
		}
		// Everything is in memory, so a tileset that has not settled in this
		// long is one that never will.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := ts.Settle(ctx, camera, 64, 48); err != nil {
			t.Fatalf("the tileset did not settle: %v", err)
		}
		s := ts.Stats()
		if s.Loading != 0 || s.Selected > s.Resident || s.SplatsSelected > s.SplatsResident || s.ResidentBytes < 0 {
			t.Fatalf("stats do not add up: %+v", s)
		}
		frame, err := scene.StillFrame(camera, 64, 48)
		if err != nil {
			t.Fatalf("the scene no longer makes a frame: %v", err)
		}
		shown := map[*Tile]bool{}
		for _, d := range frame.Draws {
			if tile, ok := d.Data.(*Tile); ok && !d.Hidden {
				shown[tile] = true
			}
		}
		if len(shown) > s.Selected {
			t.Fatalf("%d tiles are drawn and %d selected", len(shown), s.Selected)
		}
		_, _ = ts.Credits(), ts.Frame()
		// The same view again asks for nothing new.
		if err := ts.Update(camera, 64, 48); err != nil {
			t.Fatal(err)
		}
		if again := ts.Stats(); again.Loading != 0 || again.Selected != s.Selected {
			t.Fatalf("a settled view changed on the next update: %+v, then %+v", s, again)
		}
		ts.Close()
		frame, err = scene.StillFrame(camera, 64, 48)
		if err != nil || len(frame.Draws) != 0 {
			t.Fatalf("after Close the scene draws %d things: %v", len(frame.Draws), err)
		}
	})
}
