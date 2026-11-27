// Copyright (c) 2026 Neomantra Corp.

package nt3dtiles3dscene

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
)

// A tile with nothing of its own to draw is only a way to its children: it
// is walked through however small its error looks, or a tileset whose root
// groups its content would show nothing from far off.
func TestATileWithNothingToDrawIsWalkedThrough(t *testing.T) {
	data := block("REPLACE")
	data["city/tileset.json"] = []byte(strings.Replace(string(data["city/tileset.json"]), `"content":{"uri":"root.glb"},`, ``, 1))
	scene := nt3d.NewScene()
	ts := open(t, scene, newFiles(data), "city/tileset.json", Options{})
	settle(t, ts, farAway)
	if got := shown(t, scene, farAway); !same(got, []string{"city/tiles/east.glb", "city/tiles/west.glb"}) {
		t.Fatalf("from far off, under an empty root, the scene shows %v, want both children", got)
	}
}

// An implicit tile whose subtree file is missing is a branch that cannot be
// drawn. It does not count as covered: the tile it would have replaced
// stays on screen, as for a child whose content failed.
func TestAMissingSubtreeLeavesItsParentShowing(t *testing.T) {
	scene := nt3d.NewScene()
	all := `{"tileAvailability":{"constant":1},"contentAvailability":[{"constant":1}],"childSubtreeAvailability":{"constant":1}}`
	leaf := `{"tileAvailability":{"constant":1},"contentAvailability":[{"constant":1}],"childSubtreeAvailability":{"constant":0}}`
	f := newFiles(map[string][]byte{
		"grid/tileset.json": []byte(`{"asset":{"version":"1.1"},"geometricError":80,"root":{
			"transform":` + matrix(stamford.ToECEF()) + `,
			"boundingVolume":{"box":[0,0,5, 50,0,0, 0,50,0, 0,0,5]},"geometricError":10,"refine":"REPLACE",
			"content":{"uri":"content/{level}_{x}_{y}.glb"},
			"implicitTiling":{"subdivisionScheme":"QUADTREE","subtreeLevels":1,"availableLevels":2,"subtrees":{"uri":"subtrees/{level}_{x}_{y}.subtree"}}}}`),
		"grid/subtrees/0_0_0.subtree": []byte(all),
		// Three of the four subtrees below exist; the fourth file is missing.
		"grid/subtrees/1_0_0.subtree": []byte(leaf),
		"grid/subtrees/1_1_0.subtree": []byte(leaf),
		"grid/subtrees/1_0_1.subtree": []byte(leaf),
		"grid/content/0_0_0.glb":      triangle(0, 0, 0, "", ""),
		"grid/content/1_0_0.glb":      triangle(-25, -25, 0, "", ""),
		"grid/content/1_1_0.glb":      triangle(25, -25, 0, "", ""),
		"grid/content/1_0_1.glb":      triangle(-25, 25, 0, "", ""),
	})
	ts := open(t, scene, f, "grid/tileset.json", Options{})
	settle(t, ts, nearBy)
	if got := shown(t, scene, nearBy); !same(got, []string{"grid/content/0_0_0.glb"}) {
		t.Fatalf("with one subtree missing the scene shows %v, want the root standing in for all four", got)
	}
	if s := ts.Stats(); s.Failed != 1 {
		t.Fatalf("stats %+v, want the missing subtree counted once", s)
	}
}

// A tile out of view is not in use: it may be evicted, and a load of it is
// stopped. Only what the view reaches counts as used.
func TestTilesOutOfViewAreNotInUse(t *testing.T) {
	scene := nt3d.NewScene()
	f := newFiles(block("REPLACE"))
	ts := open(t, scene, f, "city/tileset.json", Options{MaxBytes: 1})
	settle(t, ts, nearBy)
	if s := ts.Stats(); s.Resident != 3 {
		t.Fatalf("stats %+v, want all three loaded", s)
	}
	// Close to the east half and looking east: the west half is behind.
	east := lookAt(nt3dmath.Vec3{X: 20, Y: 0, Z: 6}, nt3dmath.Vec3{X: 60, Y: 0, Z: 5})
	settle(t, ts, east)
	if s := ts.Stats(); s.Evictions != 1 || s.Resident != 2 {
		t.Fatalf("stats %+v, want the tile behind the camera evicted and the two in use kept", s)
	}
	if got := shown(t, scene, east); !same(got, []string{"city/tiles/east.glb"}) {
		t.Fatalf("the scene shows %v", got)
	}

	// A load in flight for a tile the view turns away from is stopped.
	scene = nt3d.NewScene()
	f = newFiles(block("REPLACE"))
	f.hold("city/tiles/west.glb")
	ts = open(t, scene, f, "city/tileset.json", Options{})
	settle(t, ts, farAway)
	if err := ts.Update(nearBy, 100, 100); err != nil {
		t.Fatal(err)
	}
	<-f.started
	settle(t, ts, east) // Returns only because the held load of the west tile is stopped.
	if s := ts.Stats(); s.Loading != 0 || s.Failed != 0 {
		t.Fatalf("stats %+v, want the off-screen load stopped", s)
	}
}

// The splat budget holds when the camera is inside a tile, where the tile's
// error on screen has no bound and no threshold would stop it refining: the
// selection is cut off a level higher instead.
func TestTheSplatBudgetHoldsInsideATile(t *testing.T) {
	data := block("REPLACE")
	data["city/root.glb"] = splats(10)
	data["city/tiles/west.glb"] = splats(80)
	data["city/tiles/east.glb"] = splats(80)
	scene := nt3d.NewScene()
	ts := open(t, scene, newFiles(data), "city/tileset.json", Options{SplatBudget: 50})
	// Inside the root's bounds and both children's reach.
	inside := lookAt(nt3dmath.Vec3{X: 0, Y: -20, Z: 6}, nt3dmath.Vec3{X: 0, Y: 30, Z: 5})
	settle(t, ts, inside)
	settle(t, ts, inside)
	if s := ts.Stats(); s.SplatsSelected > 50 || s.SplatsSelected == 0 {
		t.Fatalf("inside the tile with a budget of 50: %d splats selected (%+v), want the root's 10", s.SplatsSelected, s)
	}
	if got := shown(t, scene, inside); !same(got, []string{"city/root.glb"}) {
		t.Fatalf("the scene shows %v, want the root", got)
	}
}

// A tile's geometric error is in the tile's own units, and a transform that
// scales the tile scales its error with it: content drawn four times the
// size is four times as wrong.
func TestGeometricErrorScalesWithTheTile(t *testing.T) {
	scale := func(s float64) []byte {
		transform := stamford.ToECEF().Mul(nt3dmath.TRSd(nt3dmath.Vec3d{}, nt3dmath.Quatd{W: 1}, nt3dmath.Vec3d{X: s, Y: s, Z: s}))
		// Bounds and positions are written a quarter size for a scale of 4,
		// so both tilesets fill the same space.
		u := 1 / s
		return []byte(fmt.Sprintf(`{"asset":{"version":"1.1"},"geometricError":100,"root":{
			"transform":%s,"boundingVolume":{"box":[0,0,%g, %g,0,0, 0,%g,0, 0,0,%g]},"geometricError":%g,"refine":"REPLACE","content":{"uri":"root.glb"},
			"children":[
				{"boundingVolume":{"box":[%g,0,%g, %g,0,0, 0,%g,0, 0,0,%g]},"geometricError":0,"content":{"uri":"tiles/west.glb"}},
				{"boundingVolume":{"box":[%g,0,%g, %g,0,0, 0,%g,0, 0,0,%g]},"geometricError":0,"content":{"uri":"tiles/east.glb"}}]}}`,
			matrix(transform), 5*u, 50*u, 50*u, 5*u, 10.0, -25*u, 5*u, 25*u, 50*u, 5*u, 25*u, 5*u, 25*u, 50*u, 5*u))
	}
	// 150 m off: an error of 10 m is 8 pixels there, under the threshold.
	view := lookAt(nt3dmath.Vec3{X: 0, Y: -190, Z: 40}, nt3dmath.Vec3{Z: 5})
	for _, c := range []struct {
		name  string
		scale float64
		want  int
	}{{"as written", 1, 1}, {"scaled four times", 4, 2}} {
		data := block("REPLACE")
		data["city/tileset.json"] = scale(c.scale)
		// An authored error of 10 either way: 10 m as written, and 40 m,
		// 32 pixels, when the tile is scaled four times.
		scene := nt3d.NewScene()
		ts := open(t, scene, newFiles(data), "city/tileset.json", Options{})
		settle(t, ts, view)
		if got := shown(t, scene, view); len(got) != c.want {
			t.Errorf("%s: the scene shows %v, want %d tiles", c.name, got, c.want)
		}
	}
}

// What a tile holds includes the buffers and images its glTF names beside
// it, not only the file the tileset points at.
func TestATilesBytesIncludeItsSideFiles(t *testing.T) {
	buffer := floatBytes(0, 0, 0, 1, 0, 0, 0, 1, 0)
	document := []byte(`{"asset":{"version":"2.0"},"buffers":[{"uri":"tri.bin","byteLength":36}],"bufferViews":[{"buffer":0,"byteLength":36}],"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}],"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}],"nodes":[{"mesh":0}],"scenes":[{"nodes":[0]}],"scene":0}`)
	f := newFiles(map[string][]byte{
		"t/tileset.json":    []byte(`{"asset":{"version":"1.1"},"geometricError":10,"root":{"transform":` + matrix(stamford.ToECEF()) + `,"boundingVolume":{"sphere":[0,0,0,50]},"geometricError":0,"content":{"uri":"models/tri.gltf"}}}`),
		"t/models/tri.gltf": document,
		"t/models/tri.bin":  buffer,
	})
	scene := nt3d.NewScene()
	ts := open(t, scene, f, "t/tileset.json", Options{})
	settle(t, ts, nearBy)
	want := int64(len(document) + len(buffer))
	if s := ts.Stats(); s.Resident != 1 || s.ResidentBytes != want {
		t.Fatalf("stats %+v (%v), want one tile of %d bytes: its glTF and the buffer beside it", s, s.LastError, want)
	}
}

// A selection the splat budget has made coarser stays as it is while the
// view stays as it is. Easing it back on every update would ask again for
// the tiles the budget had refused, load them, refuse them and evict them,
// for as long as the camera sat still.
func TestACoarsenedSelectionIsStableWhileTheViewIs(t *testing.T) {
	data := block("REPLACE")
	data["city/root.glb"] = splats(10)
	data["city/tiles/west.glb"] = splats(400)
	data["city/tiles/east.glb"] = splats(400)
	f := newFiles(data)
	scene := nt3d.NewScene()
	// So little may stay loaded that the refused children are evicted.
	ts := open(t, scene, f, "city/tileset.json", Options{SplatBudget: 500, MaxBytes: 1})
	settle(t, ts, nearBy)
	if got := shown(t, scene, nearBy); !same(got, []string{"city/root.glb"}) {
		t.Fatalf("over budget the scene shows %v, want the root", got)
	}
	threshold, fetched := ts.Stats().ScreenSpaceError, f.count("city/tiles/west.glb")
	for range 60 {
		settle(t, ts, nearBy)
	}
	if got := f.count("city/tiles/west.glb"); got != fetched {
		t.Fatalf("with the camera still, the refused child was fetched %d more times", got-fetched)
	}
	if got := ts.Stats().ScreenSpaceError; got != threshold {
		t.Fatalf("with the camera still, the threshold drifted from %g to %g", threshold, got)
	}
	// A view with room for everything gets everything back.
	settle(t, ts, farAway)
	settle(t, ts, farAway)
	if s := ts.Stats(); s.SplatsSelected != 10 || s.ScreenSpaceError != 16 {
		t.Fatalf("from far off: %+v, want the root at the plain threshold", s)
	}
}

// A tile that carries both a model and a nested tileset can be evicted and
// loaded again without growing a second copy of the nested tileset under it.
func TestReloadingATileDoesNotRepeatItsNestedTileset(t *testing.T) {
	f := newFiles(map[string][]byte{
		"world/tileset.json": []byte(`{"asset":{"version":"1.1"},"geometricError":500,"root":{
			"transform":` + matrix(stamford.ToECEF()) + `,"boundingVolume":{"sphere":[0,0,0,60]},"geometricError":200,"refine":"ADD",
			"contents":[{"uri":"ground.glb"},{"uri":"district.json"}]}}`),
		"world/district.json": []byte(`{"asset":{"version":"1.1"},"geometricError":200,"root":{"boundingVolume":{"sphere":[0,0,0,60]},"geometricError":0,"content":{"uri":"house.glb"}}}`),
		"world/ground.glb":    triangle(0, 0, 0, "", ""),
		"world/house.glb":     triangle(3, 4, 5, "", ""),
	})
	scene := nt3d.NewScene()
	ts := open(t, scene, f, "world/tileset.json", Options{MaxBytes: 1})
	settle(t, ts, nearBy)
	if s := ts.Stats(); s.Selected != 2 || s.Failed != 0 {
		t.Fatalf("stats %+v (%v), want the ground and the nested house", s, s.LastError)
	}
	// Looking away from everything: both are out of view, and evicted.
	away := lookAt(nt3dmath.Vec3{X: 0, Y: -200, Z: 30}, nt3dmath.Vec3{X: 0, Y: -400, Z: 30})
	settle(t, ts, away)
	if s := ts.Stats(); s.Resident != 0 {
		t.Fatalf("stats %+v, want everything evicted", s)
	}
	settle(t, ts, nearBy)
	if s := ts.Stats(); s.Selected != 2 || s.Resident != 2 {
		t.Fatalf("after loading again: %+v, want the ground and one house, not a second nested tileset", s)
	}
}

// A tileset is drawn where its parent node puts it, and chosen for the same
// place: moving or turning Options.Parent carries the tiles' bounds with
// their geometry, so a camera that follows sees what it saw.
func TestSelectionFollowsTheParentNode(t *testing.T) {
	// Close to the east half and looking east: only the east half is in view.
	eye, target := nt3dmath.Vec3{X: 20, Y: 0, Z: 6}, nt3dmath.Vec3{X: 60, Y: 0, Z: 5}
	for _, c := range []struct {
		name   string
		place  func(*nt3d.Scene, nt3d.NodeID) error
		camera nt3d.Camera
	}{
		{"an origin 5 km east", func(s *nt3d.Scene, n nt3d.NodeID) error {
			return s.SetNodeOrigin(n, [3]float64{5000, 0, 0})
		}, func() nt3d.Camera { c := lookAt(eye, target); c.Origin = [3]float64{5000, 0, 0}; return c }()},
		{"a translation 5 km north", func(s *nt3d.Scene, n nt3d.NodeID) error {
			return s.SetTransform(n, nt3dmath.Translation(nt3dmath.Vec3{Y: 5000}))
		}, func() nt3d.Camera { c := lookAt(eye, target); c.Origin = [3]float64{0, 5000, 0}; return c }()},
		{"a half turn", func(s *nt3d.Scene, n nt3d.NodeID) error {
			return s.SetTransform(n, nt3dmath.RotationZ(math.Pi))
		}, lookAt(nt3dmath.Vec3{X: -20, Y: 0, Z: 6}, nt3dmath.Vec3{X: -60, Y: 0, Z: 5})},
	} {
		scene := nt3d.NewScene()
		parent, err := scene.Add(nt3d.Root, "district")
		if err != nil {
			t.Fatal(err)
		}
		ts := open(t, scene, newFiles(block("REPLACE")), "city/tileset.json", Options{Parent: parent})
		// The parent is placed after the tileset is open, as a moving one is.
		if err := c.place(scene, parent); err != nil {
			t.Fatal(err)
		}
		settle(t, ts, c.camera)
		if got := shown(t, scene, c.camera); !same(got, []string{"city/tiles/east.glb"}) {
			t.Errorf("under a parent with %s the scene shows %v, want the east half", c.name, got)
		}
	}
}

// A child whose content URI cannot be read as one is a failed tile, not an
// absent one: it is counted, and the tile it would have replaced stays on
// screen rather than leaving a hole beside its sibling.
func TestAMalformedContentURILeavesItsParentShowing(t *testing.T) {
	data := block("REPLACE")
	data["city/tileset.json"] = []byte(strings.Replace(string(data["city/tileset.json"]), `tiles/west.glb`, `tiles/%zz.glb`, 1))
	scene := nt3d.NewScene()
	ts := open(t, scene, newFiles(data), "city/tileset.json", Options{})
	settle(t, ts, nearBy)
	settle(t, ts, nearBy)
	if got := shown(t, scene, nearBy); !same(got, []string{"city/root.glb"}) {
		t.Fatalf("with a child's URI malformed the scene shows %v, want the root", got)
	}
	if s := ts.Stats(); s.Failed != 1 || s.LastError == nil {
		t.Fatalf("stats %+v, want the malformed URI counted once, with its error", s)
	}
}

// A tile's record lists a content's metadata and its group index for index
// with its URIs, nil where there is none: an implicit tile's too, so that
// code which reads Contents[i] for URIs[i] holds for every tile.
func TestAnImplicitTilesRecordIndexesWithItsURIs(t *testing.T) {
	scene := nt3d.NewScene()
	f := newFiles(map[string][]byte{
		"grid/tileset.json": []byte(`{"asset":{"version":"1.1"},"geometricError":80,"root":{
			"transform":` + matrix(stamford.ToECEF()) + `,
			"boundingVolume":{"box":[0,0,5, 50,0,0, 0,50,0, 0,0,5]},"geometricError":10,"refine":"REPLACE",
			"content":{"uri":"content/{level}_{x}_{y}.glb"},
			"implicitTiling":{"subdivisionScheme":"QUADTREE","subtreeLevels":1,"availableLevels":1,"subtrees":{"uri":"subtrees/{level}_{x}_{y}.subtree"}}}}`),
		"grid/subtrees/0_0_0.subtree": []byte(`{"tileAvailability":{"constant":1},"contentAvailability":[{"constant":1}],"childSubtreeAvailability":{"constant":0}}`),
		"grid/content/0_0_0.glb":      triangle(0, 0, 0, "", ""),
	})
	var records []*Tile
	ts := open(t, scene, f, "grid/tileset.json", Options{OnTileLoaded: func(tile *Tile) { records = append(records, tile) }})
	settle(t, ts, farAway)
	if len(records) != 1 {
		t.Fatalf("%d tiles loaded, want the implicit root", len(records))
	}
	if r := records[0]; len(r.URIs) != 1 || len(r.Contents) != len(r.URIs) || len(r.Groups) != len(r.URIs) {
		t.Fatalf("the record has %d URIs, %d contents and %d groups, want one of each", len(r.URIs), len(r.Contents), len(r.Groups))
	}
}

// 3D Tiles 1.0 writes a tile's geometric error in meters whatever the
// tile's transform; only from 1.1 does a transform that scales the tile
// scale its error. Each tileset is read by the rule of its own version.
func TestGeometricErrorIsMetersInVersionOne(t *testing.T) {
	transform := stamford.ToECEF().Mul(nt3dmath.TRSd(nt3dmath.Vec3d{}, nt3dmath.Quatd{W: 1}, nt3dmath.Vec3d{X: 4, Y: 4, Z: 4}))
	data := block("REPLACE")
	// Written a quarter size and scaled four times, with an error of 10.
	data["city/tileset.json"] = []byte(`{"asset":{"version":"1.0"},"geometricError":100,"root":{
		"transform":` + matrix(transform) + `,"boundingVolume":{"box":[0,0,1.25, 12.5,0,0, 0,12.5,0, 0,0,1.25]},"geometricError":10,"refine":"REPLACE","content":{"uri":"root.glb"},
		"children":[
			{"boundingVolume":{"box":[-6.25,0,1.25, 6.25,0,0, 0,12.5,0, 0,0,1.25]},"geometricError":0,"content":{"uri":"tiles/west.glb"}},
			{"boundingVolume":{"box":[6.25,0,1.25, 6.25,0,0, 0,12.5,0, 0,0,1.25]},"geometricError":0,"content":{"uri":"tiles/east.glb"}}]}}`)
	// 150 m off: 10 m of error is 8 pixels there, under the threshold; the
	// 40 m a 1.1 tileset would mean is 32, over it.
	view := lookAt(nt3dmath.Vec3{X: 0, Y: -190, Z: 40}, nt3dmath.Vec3{Z: 5})
	scene := nt3d.NewScene()
	ts := open(t, scene, newFiles(data), "city/tileset.json", Options{})
	settle(t, ts, view)
	if got := shown(t, scene, view); !same(got, []string{"city/root.glb"}) {
		t.Fatalf("a 1.0 tileset scaled four times shows %v, want its root", got)
	}
}

// A tile with several contents is whole only when every one of them loaded.
// One that is missing a content is still worth showing where nothing else
// can stand in, but it does not take the place of a tile that can: the
// replaced tile stays on screen rather than giving way to part of a child.
func TestAPartlyLoadedTileDoesNotReplaceItsParent(t *testing.T) {
	data := block("REPLACE")
	data["city/tileset.json"] = []byte(strings.Replace(string(data["city/tileset.json"]),
		`"content":{"uri":"tiles/west.glb"}`, `"contents":[{"uri":"tiles/west.glb"},{"uri":"tiles/roof.glb"}]`, 1))
	scene := nt3d.NewScene()
	ts := open(t, scene, newFiles(data), "city/tileset.json", Options{})
	settle(t, ts, nearBy)
	settle(t, ts, nearBy)
	if got := shown(t, scene, nearBy); !same(got, []string{"city/root.glb"}) {
		t.Fatalf("with a child missing one of its two contents the scene shows %v, want the root", got)
	}
	if s := ts.Stats(); s.Failed != 1 {
		t.Fatalf("stats %+v, want the missing content counted once", s)
	}

	// With nothing to stand in for it, what arrived is shown.
	data["city/tileset.json"] = []byte(strings.Replace(string(data["city/tileset.json"]), `"content":{"uri":"root.glb"},`, ``, 1))
	scene = nt3d.NewScene()
	ts = open(t, scene, newFiles(data), "city/tileset.json", Options{})
	settle(t, ts, nearBy)
	if got := shown(t, scene, nearBy); !same(got, []string{"city/tiles/east.glb", "city/tiles/west.glb"}) {
		t.Fatalf("under an empty root the scene shows %v, want both children, the partial one included", got)
	}
}

// What the splat budget refused, it refused for a view of the tileset where
// it stood. A parent that moves changes what is in view as a camera does,
// so the selection is eased again as it would be for a camera that moved.
func TestTheSplatBudgetEasesWhenTheParentMoves(t *testing.T) {
	data := block("REPLACE")
	data["city/root.glb"] = splats(10)
	data["city/tiles/west.glb"] = splats(400)
	data["city/tiles/east.glb"] = splats(400)
	// Close to the east half and looking east: only the east half is in
	// view. With the tileset 25 m further east the eye is inside the west
	// half too, and both are.
	view := lookAt(nt3dmath.Vec3{X: 20, Y: 0, Z: 6}, nt3dmath.Vec3{X: 60, Y: 0, Z: 5})
	run := func(move bool) int {
		scene := nt3d.NewScene()
		parent, err := scene.Add(nt3d.Root, "district")
		if err != nil {
			t.Fatal(err)
		}
		ts := open(t, scene, newFiles(data), "city/tileset.json", Options{Parent: parent, SplatBudget: 500})
		if move {
			// Start where both halves are in view and over the budget.
			check(t, scene.SetTransform(parent, nt3dmath.Translation(nt3dmath.Vec3{X: 25})))
			settle(t, ts, view)
			settle(t, ts, view)
			if got := ts.Stats().SplatsSelected; got != 10 {
				t.Fatalf("with both halves in view the selection holds %d splats, want the root's 10", got)
			}
			check(t, scene.SetTransform(parent, nt3dmath.Identity()))
		}
		settle(t, ts, view)
		settle(t, ts, view)
		return ts.Stats().SplatsSelected
	}
	opened, moved := run(false), run(true)
	if opened != 400 {
		t.Fatalf("opened in place the selection holds %d splats, want the east half's 400", opened)
	}
	if moved != opened {
		t.Fatalf("after the parent moved the selection holds %d splats, want the %d of a tileset opened there", moved, opened)
	}
}

// A parent that stretches the tileset unevenly chooses tiles as the same
// stretch written in the tileset's own transform does: distance is measured
// to the bounds as drawn, and the error is scaled as the tile is.
func TestAnUnevenParentScaleChoosesAsTheSameTransformInTheTileset(t *testing.T) {
	stretch := nt3dmath.Vec3d{X: 16, Y: 1, Z: 1}
	view := lookAt(nt3dmath.Vec3{X: 0, Y: -400, Z: 100}, nt3dmath.Vec3{Z: 5})
	var got [2][]string
	for i, onParent := range []bool{false, true} {
		data := block("REPLACE")
		scene := nt3d.NewScene()
		parent, err := scene.Add(nt3d.Root, "district")
		if err != nil {
			t.Fatal(err)
		}
		if onParent {
			check(t, scene.SetTransform(parent, nt3dmath.Scaling(nt3dmath.Vec3{X: 16, Y: 1, Z: 1})))
		} else {
			transform := stamford.ToECEF().Mul(nt3dmath.TRSd(nt3dmath.Vec3d{}, nt3dmath.Quatd{W: 1}, stretch))
			data["city/tileset.json"] = []byte(strings.Replace(string(data["city/tileset.json"]), matrix(stamford.ToECEF()), matrix(transform), 1))
		}
		ts := open(t, scene, newFiles(data), "city/tileset.json", Options{Parent: parent})
		settle(t, ts, view)
		got[i] = shown(t, scene, view)
	}
	if len(got[0]) != 2 {
		t.Fatalf("stretched in the tileset the scene shows %v, want both children", got[0])
	}
	if !same(got[0], got[1]) {
		t.Fatalf("stretched by the parent the scene shows %v, and in the tileset %v", got[1], got[0])
	}
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// tiered is the block with a third level: each half has an error of 1 and
// one child of its own, and every tile is splats, 10 in the root, 40 in
// each half, 400 in each of the finest.
func tiered() map[string][]byte {
	data := block("REPLACE")
	text := string(data["city/tileset.json"])
	for _, side := range []string{"west", "east"} {
		old := `"geometricError":0,"content":{"uri":"tiles/` + side + `.glb"}}`
		box := `[-25,0,5, 25,0,0, 0,50,0, 0,0,5]`
		if side == "east" {
			box = `[25,0,5, 25,0,0, 0,50,0, 0,0,5]`
		}
		text = strings.Replace(text, old, `"geometricError":1,"content":{"uri":"tiles/`+side+`.glb"},"children":[{"boundingVolume":{"box":`+box+`},"geometricError":0,"content":{"uri":"tiles/`+side+`-fine.glb"}}]}`, 1)
		data["city/tiles/"+side+".glb"] = splats(40)
		data["city/tiles/"+side+"-fine.glb"] = splats(400)
	}
	data["city/tileset.json"] = []byte(text)
	data["city/root.glb"] = splats(10)
	return data
}

// What the splat budget refused, it refused at the threshold then in force.
// Changing the threshold asks a new question of the same view, and gets the
// answer a tileset opened at that threshold gets.
func TestTheSplatBudgetLooksAgainWhenTheThresholdChanges(t *testing.T) {
	run := func(first float64) int {
		scene := nt3d.NewScene()
		ts := open(t, scene, newFiles(tiered()), "city/tileset.json", Options{SplatBudget: 100, MaxScreenSpaceError: first})
		settle(t, ts, nearBy)
		settle(t, ts, nearBy)
		ts.SetMaxScreenSpaceError(16)
		for range 3 {
			settle(t, ts, nearBy)
		}
		return ts.Stats().SplatsSelected
	}
	opened, changed := run(16), run(.25)
	if opened != 80 {
		t.Fatalf("opened at 16 pixels the selection holds %d splats, want the two halves' 80", opened)
	}
	if changed != opened {
		t.Fatalf("changed from a quarter pixel to 16 the selection holds %d splats, want the %d of a tileset opened at 16", changed, opened)
	}
}

// An eye inside a tile sees its error without bound, and the tile refines,
// when the tile is turned and its parent stretches it, which leaves its
// bounds askew, as when they are square.
func TestAnEyeInsideASkewedTileRefinesIt(t *testing.T) {
	data := block("REPLACE")
	turned := stamford.ToECEF().Mul(nt3dmath.TRSd(nt3dmath.Vec3d{}, nt3dmath.QuatdAxisAngle(nt3dmath.Vec3d{Z: 1}, math.Pi/4), nt3dmath.Vec3d{X: 1, Y: 1, Z: 1}))
	text := strings.Replace(string(data["city/tileset.json"]), matrix(stamford.ToECEF()), matrix(turned), 1)
	// A small error, so that only being inside refines the root.
	data["city/tileset.json"] = []byte(strings.Replace(text, `"geometricError":10,"refine"`, `"geometricError":0.01,"refine"`, 1))
	scene := nt3d.NewScene()
	parent, err := scene.Add(nt3d.Root, "district")
	check(t, err)
	check(t, scene.SetTransform(parent, nt3dmath.Scaling(nt3dmath.Vec3{X: 4, Y: 1, Z: 1})))
	ts := open(t, scene, newFiles(data), "city/tileset.json", Options{Parent: parent})
	// The root as drawn is a long diamond, corners at (±283, 0) and (0, ±71).
	inside := lookAt(nt3dmath.Vec3{X: 200, Y: 10, Z: 5}, nt3dmath.Vec3{X: 0, Y: 0, Z: 5})
	settle(t, ts, inside)
	if got := shown(t, scene, inside); len(got) == 0 || got[0] == "city/root.glb" {
		t.Fatalf("from inside the skewed root the scene shows %v, want its children", got)
	}
}

// A tile's error is stretched by what stretches the tile as drawn: its own
// transform and its parent node's together. A stretch in one undone by the
// other leaves the tile, and its error, as written.
func TestScalesThatCancelLeaveTheErrorAsWritten(t *testing.T) {
	// 150 m off: 10 m of error is 8 pixels, under the threshold.
	view := lookAt(nt3dmath.Vec3{X: 0, Y: -190, Z: 40}, nt3dmath.Vec3{Z: 5})
	data := block("REPLACE")
	stretched := stamford.ToECEF().Mul(nt3dmath.TRSd(nt3dmath.Vec3d{}, nt3dmath.Quatd{W: 1}, nt3dmath.Vec3d{X: 16, Y: 1, Z: 1}))
	data["city/tileset.json"] = []byte(strings.Replace(string(data["city/tileset.json"]), matrix(stamford.ToECEF()), matrix(stretched), 1))
	scene := nt3d.NewScene()
	parent, err := scene.Add(nt3d.Root, "district")
	check(t, err)
	check(t, scene.SetTransform(parent, nt3dmath.Scaling(nt3dmath.Vec3{X: 1. / 16, Y: 1, Z: 1})))
	ts := open(t, scene, newFiles(data), "city/tileset.json", Options{Parent: parent})
	settle(t, ts, view)
	if got := shown(t, scene, view); !same(got, []string{"city/root.glb"}) {
		t.Fatalf("with the tile's stretch undone by its parent the scene shows %v, want the root as for no stretch", got)
	}
}

// A tile's error is stretched by the most its transform stretches any
// length, in whatever direction that is: a tile turned and then stretched
// along one axis is stretched that much, though none of its own three axes
// is.
func TestErrorScalesByTheGreatestStretch(t *testing.T) {
	if got := largestScale(nt3dmath.TRSd(nt3dmath.Vec3d{}, nt3dmath.Quatd{W: 1}, nt3dmath.Vec3d{X: 2, Y: 1, Z: 1}).
		Mul(nt3dmath.TRSd(nt3dmath.Vec3d{}, nt3dmath.QuatdAxisAngle(nt3dmath.Vec3d{Z: 1}, math.Pi/4), nt3dmath.Vec3d{X: 1, Y: 1, Z: 1}))); math.Abs(got-2) > 1e-12 {
		t.Errorf("a turn then a stretch of 2 has a greatest stretch of %g, want 2", got)
	}
	for _, c := range []struct {
		scale nt3dmath.Vec3d
		want  float64
	}{{nt3dmath.Vec3d{X: 1, Y: 1, Z: 1}, 1}, {nt3dmath.Vec3d{X: 3, Y: 3, Z: 3}, 3}, {nt3dmath.Vec3d{X: .5, Y: 7, Z: 2}, 7}, {nt3dmath.Vec3d{X: 1e-3, Y: 1e-3, Z: 1e-3}, 1e-3}} {
		m := stamford.ToECEF().Mul(nt3dmath.TRSd(nt3dmath.Vec3d{X: 5}, nt3dmath.QuatdAxisAngle(nt3dmath.Vec3d{X: 1, Y: 2, Z: 3}, 1.1), c.scale))
		if got := largestScale(m); math.Abs(got-c.want) > 1e-9*c.want {
			t.Errorf("scale %v turned: greatest stretch %g, want %g", c.scale, got, c.want)
		}
	}

	// On screen: an orthographic view 100 pixels and 100 m tall shows a
	// meter as a pixel. An error of 10 stretched by 2 is 20 pixels, over the
	// threshold of 16, where the 15.8 of the tile's longest axis is under.
	data := block("REPLACE")
	stretched := stamford.ToECEF().Mul(nt3dmath.TRSd(nt3dmath.Vec3d{}, nt3dmath.Quatd{W: 1}, nt3dmath.Vec3d{X: 2, Y: 1, Z: 1})).
		Mul(nt3dmath.TRSd(nt3dmath.Vec3d{}, nt3dmath.QuatdAxisAngle(nt3dmath.Vec3d{Z: 1}, math.Pi/4), nt3dmath.Vec3d{X: 1, Y: 1, Z: 1}))
	data["city/tileset.json"] = []byte(strings.Replace(string(data["city/tileset.json"]), matrix(stamford.ToECEF()), matrix(stretched), 1))
	view := nt3d.DefaultCamera().LookAt(nt3dmath.Vec3{X: 0, Y: -400, Z: 400}, nt3dmath.Vec3{Z: 5})
	view.Projection, view.Xmag, view.Ymag = nt3d.Orthographic, 50, 50
	scene := nt3d.NewScene()
	ts := open(t, scene, newFiles(data), "city/tileset.json", Options{})
	settle(t, ts, view)
	if got := shown(t, scene, view); len(got) != 2 {
		t.Fatalf("a root whose error shows as 20 pixels is shown as %v, want its two children", got)
	}
}
