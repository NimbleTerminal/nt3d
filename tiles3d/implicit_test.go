// Copyright (c) 2026 Neomantra Corp.

package nt3dtiles3d

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

func implicitTileset(t *testing.T, scheme, volume string, subtreeLevels, availableLevels int) *Implicit {
	t.Helper()
	text := `{"asset": {"version": "1.1"}, "geometricError": 64, "root": {
		"boundingVolume": ` + volume + `, "geometricError": 32, "refine": "REPLACE",
		"contents": [{"uri": "content/{level}/{x}/{y}/{z}.glb"}, {"uri": "splats/{level}_{x}_{y}.glb"}],
		"implicitTiling": {"subdivisionScheme": "` + scheme + `", "subtreeLevels": ` + string(rune('0'+subtreeLevels)) + `, "availableLevels": ` + string(rune('0'+availableLevels)) + `,
			"subtrees": {"uri": "subtrees/{level}.{x}.{y}.{z}.subtree"}}}}`
	ts, err := ParseTileset([]byte(text), nil)
	if err != nil {
		t.Fatal(err)
	}
	if ts.Root.Implicit == nil {
		t.Fatal("the root is not implicit")
	}
	return ts.Root.Implicit
}

// An implicit tile's bounds, error, and URIs follow from the root's: a box
// halves along its first two axes each level, and along the third in an
// octree; the error halves; and the templates take the tile's coordinates.
func TestImplicitTilesFollowFromTheRoot(t *testing.T) {
	box := `{"box": [100,200,300, 80,0,0, 0,40,0, 0,0,16]}`
	quad := implicitTileset(t, "QUADTREE", box, 2, 5)
	c := Coord{Level: 2, X: 3, Y: 0}
	b := quad.Bounds(c).Box
	if b.Center != (nt3dmath.Vec3d{X: 100 + 80*.75, Y: 200 - 40*.75, Z: 300}) || b.Axes[0].X != 20 || b.Axes[1].Y != 10 || b.Axes[2].Z != 16 {
		t.Fatalf("quadtree tile %+v: %+v, want a quarter of the root across and its whole height", c, b)
	}
	if got := quad.GeometricError(c); got != 8 {
		t.Fatalf("error %g at level 2, want 32 halved twice", got)
	}
	if got := quad.ContentURIs(c); got[0] != "content/2/3/0/0.glb" || got[1] != "splats/2_3_0.glb" {
		t.Fatalf("content uris %q", got)
	}
	if got := quad.SubtreeURI(quad.SubtreeRoot(Coord{Level: 3, X: 7, Y: 2})); got != "subtrees/2.3.1.0.subtree" {
		t.Fatalf("subtree uri %q: a level-3 tile's subtree starts at its level-2 parent", got)
	}
	if kids := quad.Children(c); len(kids) != 4 || kids[3] != (Coord{Level: 3, X: 7, Y: 1}) {
		t.Fatalf("children %+v", kids)
	}
	if !quad.Valid(Coord{Level: 4, X: 15, Y: 15}) || quad.Valid(Coord{Level: 5}) || quad.Valid(Coord{Level: 1, X: 2}) || quad.Valid(Coord{Level: 1, Z: 1}) {
		t.Fatal("Valid disagrees with the tree's levels and sides")
	}
	oct := implicitTileset(t, "OCTREE", box, 2, 5)
	b = oct.Bounds(Coord{Level: 1, X: 1, Y: 1, Z: 0}).Box
	if b.Center != (nt3dmath.Vec3d{X: 140, Y: 220, Z: 292}) || b.Axes[2].Z != 8 {
		t.Fatalf("octree tile: %+v, want its height halved too", b)
	}
	if kids := oct.Children(Coord{}); len(kids) != 8 || kids[7] != (Coord{Level: 1, X: 1, Y: 1, Z: 1}) {
		t.Fatalf("octree children %+v", kids)
	}
	region := implicitTileset(t, "OCTREE", `{"region": [-1.3,0.7,-1.2,0.8,0,400]}`, 2, 5).Bounds(Coord{Level: 1, X: 1, Y: 0, Z: 1}).Region.Radians()
	for k, want := range [6]float64{-1.25, 0.7, -1.2, 0.75, 200, 400} {
		if math.Abs(region[k]-want) > 1e-12 {
			t.Fatalf("region tile %v, want the east, south, upper eighth", region)
		}
	}
}

// subtreeFile builds a binary subtree: the header, the JSON padded to eight
// bytes, and the buffer.
func subtreeFile(json string, buffer []byte) []byte {
	for len(json)%8 != 0 {
		json += " "
	}
	out := append([]byte("subt"), 1, 0, 0, 0)
	out = binary.LittleEndian.AppendUint64(out, uint64(len(json)))
	out = binary.LittleEndian.AppendUint64(out, uint64(len(buffer)))
	return append(append(out, json...), buffer...)
}

// A subtree says which tiles exist, which have content, and which subtrees
// follow, a bit each, in Morton order within each level: X in the low bit,
// then Y. Bits may be in the file's own buffer, in another file, or one
// constant for all.
func TestSubtreeAvailability(t *testing.T) {
	quad := implicitTileset(t, "QUADTREE", `{"box": [0,0,0, 8,0,0, 0,8,0, 0,0,1]}`, 2, 4)
	// Two levels are 5 tiles: the root, then (0,0) (1,0) (0,1) (1,1).
	// Tiles: root, (1,0), (1,1). Content 0: (1,0) only. Child subtrees, 16
	// bits for level 2: (2,0) is Morton 4 and (3,3) is Morton 15.
	buffer := []byte{0b10101, 0b00100, 0b00010000, 0b10000000}
	doc := `{"buffers": [{"byteLength": 4}],
		"bufferViews": [{"buffer": 0, "byteOffset": 0, "byteLength": 1}, {"buffer": 0, "byteOffset": 1, "byteLength": 1}, {"buffer": 0, "byteOffset": 2, "byteLength": 2}],
		"tileAvailability": {"bitstream": 0},
		"contentAvailability": [{"bitstream": 1}, {"constant": 1}],
		"childSubtreeAvailability": {"bitstream": 2},
		"subtreeMetadata": {"class": "block", "properties": {"name": "north"}}}`
	check := func(t *testing.T, s *Subtree) {
		t.Helper()
		for c, want := range map[Coord][2]bool{
			{}:                       {true, false},
			{Level: 1, X: 1}:         {true, true},
			{Level: 1, Y: 1}:         {false, false},
			{Level: 1, X: 1, Y: 1}:   {true, false},
			{Level: 2}:               {false, false}, // below the subtree
			{Level: 1, X: 2}:         {false, false}, // off the side
			{Level: 1, X: 1, Z: 1}:   {false, false}, // a quadtree has no Z
			{Level: -1}:              {false, false},
			{Level: 1, X: -1, Y: -1}: {false, false},
		} {
			if got := [2]bool{s.TileAvailable(c), s.ContentAvailable(c, 0)}; got != want {
				t.Errorf("%+v: tile and content %v, want %v", c, got, want)
			}
		}
		if !s.ContentAvailable(Coord{}, 1) || s.ContentAvailable(Coord{Level: 1, Y: 1}, 1) || s.ContentAvailable(Coord{}, 2) {
			t.Error("a constant content availability holds for tiles that exist, and for no content past the last")
		}
		for c, want := range map[Coord]bool{{Level: 2, X: 2}: true, {Level: 2, X: 3, Y: 3}: true, {Level: 2, X: 1}: false, {Level: 1, X: 1}: false, {Level: 2, X: 4}: false} {
			if got := s.ChildSubtreeAvailable(c); got != want {
				t.Errorf("child subtree at %+v: %v, want %v", c, got, want)
			}
		}
	}
	s, err := ParseSubtree(subtreeFile(doc, buffer), quad, Coord{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	check(t, s)
	if s.Metadata == nil || s.Metadata.Properties["name"] != "north" {
		t.Fatalf("metadata %+v", s.Metadata)
	}

	// The same bits kept in a file beside the subtree, which is plain JSON.
	external := bytes.Replace([]byte(doc), []byte(`{"byteLength": 4}`), []byte(`{"uri": "0.bin", "byteLength": 4}`), 1)
	asked := ""
	s, err = ParseSubtree(external, quad, Coord{}, func(uri string) ([]byte, error) { asked = uri; return buffer, nil }, nil)
	if err != nil || asked != "0.bin" {
		t.Fatalf("external buffer: asked for %q, %v", asked, err)
	}
	check(t, s)
	if _, err := ParseSubtree(external, quad, Coord{}, nil, nil); err == nil {
		t.Error("a subtree with a buffer in another file was read with nothing to fetch it")
	}

	// A subtree further down speaks for tiles by their own coordinates.
	all := `{"tileAvailability": {"constant": 1}, "childSubtreeAvailability": {"constant": 0}}`
	s, err = ParseSubtree([]byte(all), quad, Coord{Level: 2, X: 3, Y: 1}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !s.TileAvailable(Coord{Level: 3, X: 7, Y: 2}) || s.TileAvailable(Coord{Level: 3, X: 5, Y: 2}) || s.ContentAvailable(Coord{Level: 2, X: 3, Y: 1}, 0) || s.ChildSubtreeAvailable(Coord{Level: 4, X: 12, Y: 4}) {
		t.Error("a deeper subtree answers for its own tiles only, and has no content it does not list")
	}
}

// A subtree file that is cut short, points outside its buffers, or starts
// where no subtree does is an error, and one over the byte limit is too large.
func TestSubtreeRefusesWhatIsWrong(t *testing.T) {
	quad := implicitTileset(t, "QUADTREE", `{"box": [0,0,0, 8,0,0, 0,8,0, 0,0,1]}`, 2, 4)
	good := `{"buffers": [{"byteLength": 3}], "bufferViews": [{"buffer": 0, "byteOffset": 0, "byteLength": 1}, {"buffer": 0, "byteOffset": 1, "byteLength": 2}],
		"tileAvailability": {"bitstream": 0}, "childSubtreeAvailability": {"bitstream": 1}}`
	if _, err := ParseSubtree(subtreeFile(good, []byte{1, 0, 0}), quad, Coord{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	short := subtreeFile(good, []byte{1, 0, 0})
	binary.LittleEndian.PutUint64(short[16:], 99)
	for name, file := range map[string][]byte{
		"cut header":            []byte("subt\x01\x00"),
		"version 2":             append([]byte("subt\x02\x00\x00\x00"), make([]byte, 16)...),
		"chunk past the file":   short,
		"buffer too short":      subtreeFile(good, []byte{1}),
		"view past buffer":      subtreeFile(`{"buffers": [{"byteLength": 3}], "bufferViews": [{"buffer": 0, "byteOffset": 2, "byteLength": 9}], "tileAvailability": {"bitstream": 0}, "childSubtreeAvailability": {"constant": 0}}`, []byte{1, 0, 0}),
		"too few bits":          subtreeFile(`{"buffers": [{"byteLength": 3}], "bufferViews": [{"buffer": 0, "byteOffset": 0, "byteLength": 1}], "tileAvailability": {"constant": 1}, "childSubtreeAvailability": {"bitstream": 0}}`, []byte{1, 0, 0}),
		"no such view":          []byte(`{"tileAvailability": {"bitstream": 4}, "childSubtreeAvailability": {"constant": 0}}`),
		"no such buffer":        []byte(`{"bufferViews": [{"buffer": 2, "byteLength": 1}], "tileAvailability": {"bitstream": 0}, "childSubtreeAvailability": {"constant": 0}}`),
		"no tile availability":  []byte(`{"childSubtreeAvailability": {"constant": 0}}`),
		"constant of two":       []byte(`{"tileAvailability": {"constant": 2}, "childSubtreeAvailability": {"constant": 0}}`),
		"neither":               []byte(`{"tileAvailability": {}, "childSubtreeAvailability": {"constant": 0}}`),
		"three contents of two": []byte(`{"tileAvailability": {"constant": 1}, "contentAvailability": [{"constant": 1}, {"constant": 1}, {"constant": 1}], "childSubtreeAvailability": {"constant": 0}}`),
	} {
		if _, err := ParseSubtree(file, quad, Coord{}, nil, nil); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	all := []byte(`{"tileAvailability": {"constant": 1}, "childSubtreeAvailability": {"constant": 0}}`)
	if _, err := ParseSubtree(all, quad, Coord{Level: 1}, nil, nil); err == nil {
		t.Error("a subtree was read at level 1 of a tree whose subtrees start every 2 levels")
	}
	if _, err := ParseSubtree(all, quad, Coord{}, nil, &Options{MaxBytes: 8}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("over the byte limit: %v, want ErrTooLarge", err)
	}
}

// An implicit tiling that names no scheme, no levels, or a sphere to divide
// is an error, and one whose subtrees would be enormous is too large.
func TestImplicitRefusesWhatIsWrong(t *testing.T) {
	doc := func(volume, implicit string) string {
		return `{"asset": {"version": "1.1"}, "geometricError": 1, "root": {"boundingVolume": ` + volume + `, "geometricError": 1, "content": {"uri": "{level}.glb"}, "implicitTiling": ` + implicit + `}}`
	}
	box := `{"box": [0,0,0, 1,0,0, 0,1,0, 0,0,1]}`
	if _, err := ParseTileset([]byte(doc(box, `{"subdivisionScheme": "OCTREE", "subtreeLevels": 3, "availableLevels": 9, "subtrees": {"uri": "{level}.subtree"}}`)), nil); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"no scheme":   doc(box, `{"subtreeLevels": 3, "availableLevels": 9, "subtrees": {"uri": "s"}}`),
		"no levels":   doc(box, `{"subdivisionScheme": "OCTREE", "availableLevels": 9, "subtrees": {"uri": "s"}}`),
		"no subtrees": doc(box, `{"subdivisionScheme": "OCTREE", "subtreeLevels": 3, "availableLevels": 9}`),
		"too deep":    doc(box, `{"subdivisionScheme": "OCTREE", "subtreeLevels": 3, "availableLevels": 40, "subtrees": {"uri": "s"}}`),
		"sphere":      doc(`{"sphere": [0,0,0,1]}`, `{"subdivisionScheme": "OCTREE", "subtreeLevels": 3, "availableLevels": 9, "subtrees": {"uri": "s"}}`),
	} {
		if _, err := ParseTileset([]byte(text), nil); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := ParseTileset([]byte(doc(box, `{"subdivisionScheme": "OCTREE", "subtreeLevels": 12, "availableLevels": 24, "subtrees": {"uri": "s"}}`)), nil); !errors.Is(err, ErrTooLarge) {
		t.Errorf("subtrees of 12 octree levels: %v, want ErrTooLarge", err)
	}
}
