// Copyright (c) 2026 Neomantra Corp.

package nt3dtiles3dscene

import (
	"context"
	"fmt"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/geo"
	"nimbleterminal.dev/nt3d/math"
)

// A tileset is opened into a scene drawn east-north-up of a point on the
// Earth. Each frame Update is told the camera, and the scene comes to show
// what the camera needs; Settle waits for it, for a still picture.
func ExampleOpen() {
	scene := nt3d.NewScene()
	// A tileset of one block and its two halves, read from memory here;
	// nt3dtiles3d.FS reads a directory and nt3dtiles3d.HTTP a server.
	fetch := newFiles(block("REPLACE"))

	tileset, err := Open(context.Background(), scene, fetch, "city/tileset.json", Options{
		Frame: nt3dgeo.ENU{Origin: nt3dgeo.Geodetic{Lat: 41.0465, Lon: -73.5418}},
		OnTileLoaded: func(tile *Tile) {
			// The tile's nodes are in the scene, not yet shown.
			_ = scene.ApplyToon(tile.Node, nt3d.ToonStyle{Bands: 3})
		},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	defer tileset.Close()

	for _, eye := range []nt3dmath.Vec3{{Y: -400, Z: 100}, {Y: -60, Z: 30}} {
		camera := nt3d.DefaultCamera().LookAt(eye, nt3dmath.Vec3{Z: 5})
		camera.Projection = nt3d.Perspective
		if err := tileset.Settle(context.Background(), camera, 100, 100); err != nil {
			fmt.Println(err)
			return
		}
		stats := tileset.Stats()
		fmt.Printf("%.0f m away: %d shown of %d loaded\n", -eye.Y, stats.Selected, stats.Resident)
	}
	fmt.Println("credit:", tileset.Credits())
	// Output:
	// 400 m away: 1 shown of 1 loaded
	// 60 m away: 2 shown of 3 loaded
	// credit: [© City Survey]
}
