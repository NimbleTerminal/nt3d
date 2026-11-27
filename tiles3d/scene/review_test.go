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
