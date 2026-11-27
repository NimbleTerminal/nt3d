// Copyright (c) 2026 Neomantra Corp.

package nt3dtiles3d

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"nimbleterminal.dev/nt3d/math"
)

// FuzzParseTileset holds that no bytes make ParseTileset panic, and that a
// tileset it accepts can be walked: every tile's bounds place, and every
// implicit root answers for its tiles.
func FuzzParseTileset(f *testing.F) {
	f.Add([]byte(explicitTileset))
	f.Add([]byte(`{"asset": {"version": "1.1"}, "geometricError": 64, "root": {"boundingVolume": {"region": [-1.3,0.7,-1.2,0.8,0,400]}, "geometricError": 32, "content": {"uri": "{level}/{x}/{y}.glb"}, "implicitTiling": {"subdivisionScheme": "QUADTREE", "subtreeLevels": 2, "availableLevels": 5, "subtrees": {"uri": "{level}.subtree"}}}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		ts, err := ParseTileset(data, &Options{MaxBytes: 1 << 16, MaxTiles: 256, MaxDepth: 32})
		if err != nil {
			return
		}
		_ = ts.Asset.Credits()
		if center, err := ts.Center(); err == nil && center.Validate() != nil {
			t.Fatalf("the tileset's center is off the Earth: %+v", center)
		}
		var walk func(tile *Tile, toECEF nt3dmath.Mat4d)
		walk = func(tile *Tile, toECEF nt3dmath.Mat4d) {
			toECEF = toECEF.Mul(tile.Transform)
			_, _ = tile.Bounds.World(toECEF)
			for _, c := range tile.Contents {
				if c.HasBounds {
					_, _ = c.Bounds.World(toECEF)
				}
				_, _ = Resolve("https://example.com/tileset.json", c.URI)
			}
			if i := tile.Implicit; i != nil {
				c := Coord{Level: i.AvailableLevels - 1, X: 1<<(i.AvailableLevels-1) - 1}
				_, _ = i.Bounds(c).World(toECEF)
				_, _, _, _ = i.GeometricError(c), i.ContentURIs(c), i.SubtreeURI(i.SubtreeRoot(c)), i.Children(c)
			}
			for _, child := range tile.Children {
				walk(child, toECEF)
			}
		}
		walk(ts.Root, nt3dmath.IdentityD())
	})
}

// FuzzParseSubtree holds that no bytes make ParseSubtree panic, in a
// quadtree or an octree, and that a subtree it accepts answers for any tile.
func FuzzParseSubtree(f *testing.F) {
	f.Add(subtreeFile(`{"buffers": [{"byteLength": 4}], "bufferViews": [{"buffer": 0, "byteOffset": 0, "byteLength": 1}, {"buffer": 0, "byteOffset": 2, "byteLength": 2}], "tileAvailability": {"bitstream": 0}, "contentAvailability": [{"constant": 1}], "childSubtreeAvailability": {"bitstream": 1}}`, []byte{21, 4, 16, 128}), 3, 1, false)
	f.Add([]byte(`{"tileAvailability": {"constant": 1}, "childSubtreeAvailability": {"constant": 0}}`), 0, 0, true)
	f.Fuzz(func(t *testing.T, data []byte, x, y int, octree bool) {
		i := &Implicit{Scheme: Quadtree, SubtreeLevels: 2, AvailableLevels: 6, Subtrees: "s", contents: []string{"a", "b"}}
		if octree {
			i.Scheme = Octree
		}
		external := func(string) ([]byte, error) { return data, nil }
		for _, root := range []Coord{{}, {Level: 2, X: x & 3, Y: y & 3}} {
			s, err := ParseSubtree(data, i, root, external, &Options{MaxBytes: 1 << 16})
			if err != nil {
				continue
			}
			for _, c := range []Coord{root, {Level: root.Level + 1, X: x, Y: y, Z: x ^ y}, {Level: root.Level + 2, X: x, Y: y}, {Level: -x, X: y}} {
				_, _, _, _ = s.Contains(c), s.TileAvailable(c), s.ContentAvailable(c, x&3), s.ChildSubtreeAvailable(c)
			}
		}
	})
}

// FuzzUnwrapB3DM holds that no bytes make UnwrapB3DM panic, and that the glb
// it returns lies within the file.
func FuzzUnwrapB3DM(f *testing.F) {
	f.Add(b3dmFile(`{"RTC_CENTER":[1,2,3]}`, nil, []byte("glTF\x02\x00\x00\x00")))
	f.Add(b3dmFile(`{"RTC_CENTER":{"byteOffset":0}}`, make([]byte, 12), []byte("glTF")))
	f.Fuzz(func(t *testing.T, data []byte) {
		glb, center, err := UnwrapB3DM(data)
		if err != nil {
			return
		}
		if len(glb) > len(data) || !center.IsFinite() {
			t.Fatalf("glb of %d bytes from %d, center %+v", len(glb), len(data), center)
		}
		_ = SniffContent(data)
	})
}

// FuzzResolve holds that no base and reference make Resolve panic, that a
// reference resolved beside a plain path never climbs above the path's
// root, and that the file-system fetcher reads nothing outside its own.
func FuzzResolve(f *testing.F) {
	f.Add("city/tileset.json", "tiles/0.glb")
	f.Add("https://tiles.example/city/tileset.json?v=3", "../shared/a.glb")
	f.Add("tileset.json", "../../etc/passwd")
	f.Add("a/b/c.json", "/%2e%2e/%2e%2e/secret")
	f.Add("tileset.json", "data:model/gltf-binary;base64,AAAA")
	inside := fstest.MapFS{"city/tiles/0.glb": {Data: []byte("in")}}
	fetch := FS(inside)
	f.Fuzz(func(t *testing.T, base, ref string) {
		got, err := Resolve(base, ref)
		if err == nil && !strings.Contains(base, ":") && !strings.Contains(ref, ":") && !strings.HasPrefix(base, "//") && !strings.HasPrefix(ref, "//") {
			if got == ".." || strings.HasPrefix(got, "../") {
				t.Fatalf("Resolve(%q, %q) = %q climbs above the root", base, ref, got)
			}
		}
		// Whatever the reference, the fetcher answers only from its files.
		if data, err := fetch.Fetch(context.Background(), ref); err == nil && string(data) != "in" {
			t.Fatalf("the fetcher read %q for %q", data, ref)
		}
		if err == nil {
			_, _ = fetch.Fetch(context.Background(), got)
		}
	})
}
