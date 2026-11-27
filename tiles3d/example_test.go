// Copyright (c) 2026 Neomantra Corp.

package nt3dtiles3d_test

import (
	"context"
	"fmt"
	"testing/fstest"

	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/tiles3d"
)

// A tileset is fetched, parsed, and walked: each tile's transform multiplies
// into its parent's, its bounds become a box in Earth-centered meters, and
// its content's URI resolves beside the tileset.
func ExampleParseTileset() {
	files := fstest.MapFS{"city/tileset.json": {Data: []byte(`{
		"asset": {"version": "1.1", "copyright": "© Example Survey"},
		"geometricError": 200,
		"root": {
			"transform": [1,0,0,0, 0,1,0,0, 0,0,1,0, 1364789,-4619825,4166319,1],
			"boundingVolume": {"box": [0,0,0, 100,0,0, 0,100,0, 0,0,20]},
			"geometricError": 50, "refine": "REPLACE",
			"content": {"uri": "low.glb"},
			"children": [{"boundingVolume": {"box": [50,0,0, 50,0,0, 0,100,0, 0,0,20]}, "geometricError": 0, "content": {"uri": "tiles/east.glb"}}]
		}}`)}}
	fetch := nt3dtiles3d.FS(files)

	data, _ := fetch.Fetch(context.Background(), "city/tileset.json")
	tileset, err := nt3dtiles3d.ParseTileset(data, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("credit:", tileset.Asset.Credits())

	var walk func(tile *nt3dtiles3d.Tile, parent nt3dmath.Mat4d, refine nt3dtiles3d.Refine)
	walk = func(tile *nt3dtiles3d.Tile, parent nt3dmath.Mat4d, refine nt3dtiles3d.Refine) {
		toECEF := parent.Mul(tile.Transform)
		if tile.Refine != nt3dtiles3d.RefineInherit {
			refine = tile.Refine
		}
		box, _ := tile.Bounds.World(toECEF)
		uri, _ := nt3dtiles3d.Resolve("city/tileset.json", tile.Contents[0].URI)
		fmt.Printf("%s: error %g m, %v, centered at %.0f %.0f %.0f\n", uri, tile.GeometricError, refine, box.Center.X, box.Center.Y, box.Center.Z)
		for _, child := range tile.Children {
			walk(child, toECEF, refine)
		}
	}
	walk(tileset.Root, nt3dmath.IdentityD(), nt3dtiles3d.RefineReplace)
	// Output:
	// credit: [© Example Survey]
	// city/low.glb: error 50 m, REPLACE, centered at 1364789 -4619825 4166319
	// city/tiles/east.glb: error 0 m, REPLACE, centered at 1364839 -4619825 4166319
}

// An implicit tree writes no tiles out. A subtree file says which exist, and
// each tile's bounds, error, and content URI follow from its coordinates.
func ExampleParseSubtree() {
	tileset, err := nt3dtiles3d.ParseTileset([]byte(`{
		"asset": {"version": "1.1"}, "geometricError": 400,
		"root": {
			"boundingVolume": {"box": [0,0,0, 400,0,0, 0,400,0, 0,0,50]},
			"geometricError": 200, "refine": "REPLACE",
			"content": {"uri": "content/{level}/{x}/{y}.glb"},
			"implicitTiling": {"subdivisionScheme": "QUADTREE", "subtreeLevels": 2, "availableLevels": 4, "subtrees": {"uri": "subtrees/{level}/{x}/{y}.subtree"}}
		}}`), nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	implicit := tileset.Root.Implicit
	root := nt3dtiles3d.Coord{}
	fmt.Println("subtree file:", implicit.SubtreeURI(root))

	// A subtree as JSON: every tile exists and has content, and nothing lies below.
	subtree, err := nt3dtiles3d.ParseSubtree([]byte(`{
		"tileAvailability": {"constant": 1},
		"contentAvailability": [{"constant": 1}],
		"childSubtreeAvailability": {"constant": 0}}`), implicit, root, nil, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, c := range implicit.Children(root)[:2] {
		if !subtree.ContentAvailable(c, 0) {
			continue
		}
		box := implicit.Bounds(c).Box
		fmt.Printf("%s: error %g m, centered at %g %g, %g m wide\n", implicit.ContentURIs(c)[0], implicit.GeometricError(c), box.Center.X, box.Center.Y, 2*box.Axes[0].X)
	}
	// Output:
	// subtree file: subtrees/0/0/0.subtree
	// content/1/0/0.glb: error 100 m, centered at -200 -200, 400 m wide
	// content/1/1/0.glb: error 100 m, centered at 200 -200, 400 m wide
}
