// Copyright (c) 2026 Neomantra Corp.

package nt3dtiles3dscene

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/geo"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/tiles3d"
)

// From far off a tile's error is small on screen and the tile itself is
// drawn; its children are not even asked for. Up close the children are
// drawn in its place.
func TestTheViewChoosesHowFineToDraw(t *testing.T) {
	scene := nt3d.NewScene()
	f := newFiles(block("REPLACE"))
	ts := open(t, scene, f, "city/tileset.json", Options{})
	settle(t, ts, farAway)
	if got := shown(t, scene, farAway); !same(got, []string{"city/root.glb"}) {
		t.Fatalf("from far off the scene shows %v, want the root alone", got)
	}
	if f.count("city/tiles/west.glb") != 0 {
		t.Fatal("a child was fetched that the view did not need")
	}
	settle(t, ts, nearBy)
	if got := shown(t, scene, nearBy); !same(got, []string{"city/tiles/east.glb", "city/tiles/west.glb"}) {
		t.Fatalf("up close the scene shows %v, want the two children in the root's place", got)
	}
	if s := ts.Stats(); s.Selected != 2 || s.Resident != 3 || s.Loading != 0 || s.Failed != 0 {
		t.Fatalf("stats %+v, want 2 selected of 3 resident", s)
	}
	settle(t, ts, farAway)
	if got := shown(t, scene, farAway); !same(got, []string{"city/root.glb"}) {
		t.Fatalf("far off again the scene shows %v, want the root", got)
	}
}

// While a replacing tile's children are still arriving the tile itself stays
// on screen: there is never a hole, and never half the children. They take
// its place only once every one is in.
func TestReplaceHoldsTheParentUntilEveryChildIsIn(t *testing.T) {
	scene := nt3d.NewScene()
	f := newFiles(block("REPLACE"))
	f.hold("city/tiles/east.glb")
	ts := open(t, scene, f, "city/tileset.json", Options{})
	settle(t, ts, farAway)
	// Up close: the west child arrives, the east one is held back.
	deadline := time.Now().Add(5 * time.Second)
	for ts.Stats().Resident < 2 {
		if err := ts.Update(nearBy, 100, 100); err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("the west child never arrived")
		}
		time.Sleep(time.Millisecond)
	}
	if err := ts.Update(nearBy, 100, 100); err != nil {
		t.Fatal(err)
	}
	if got := shown(t, scene, nearBy); !same(got, []string{"city/root.glb"}) {
		t.Fatalf("with one child of two in, the scene shows %v, want the root still", got)
	}
	f.release("city/tiles/east.glb")
	settle(t, ts, nearBy)
	if got := shown(t, scene, nearBy); !same(got, []string{"city/tiles/east.glb", "city/tiles/west.glb"}) {
		t.Fatalf("with both in, the scene shows %v", got)
	}
}

// A tile that adds is drawn together with its children.
func TestAddDrawsParentAndChildrenTogether(t *testing.T) {
	scene := nt3d.NewScene()
	ts := open(t, scene, newFiles(block("ADD")), "city/tileset.json", Options{})
	settle(t, ts, nearBy)
	if got := shown(t, scene, nearBy); !same(got, []string{"city/root.glb", "city/tiles/east.glb", "city/tiles/west.glb"}) {
		t.Fatalf("the scene shows %v, want the root and both children", got)
	}
}

// Error on screen is measured to the nearest point of a tile's bounds. An
// eye inside a large tile, far from its middle, is as near as can be, and
// the tile refines.
func TestErrorIsMeasuredToTheNearestPointOfTheBounds(t *testing.T) {
	scene := nt3d.NewScene()
	data := block("REPLACE")
	// A root 4 km across: its middle is 2 km from an eye at its east edge.
	data["city/tileset.json"] = []byte(strings.Replace(string(data["city/tileset.json"]), `"box":[0,0,5, 50,0,0, 0,50,0, 0,0,5]`, `"box":[0,0,5, 2000,0,0, 0,2000,0, 0,0,500]`, 1))
	ts := open(t, scene, newFiles(data), "city/tileset.json", Options{})
	inside := lookAt(nt3dmath.Vec3{X: 60, Y: -60, Z: 30}, nt3dmath.Vec3{X: 25, Z: 5})
	settle(t, ts, inside)
	if got := shown(t, scene, inside); len(got) == 0 || got[0] == "city/root.glb" {
		t.Fatalf("an eye inside the root's bounds sees %v, want its children", got)
	}
}

// A tile out of view is neither loaded nor drawn.
func TestTilesOutOfViewAreLeftAlone(t *testing.T) {
	scene := nt3d.NewScene()
	f := newFiles(block("REPLACE"))
	ts := open(t, scene, f, "city/tileset.json", Options{})
	// Close to the east half and looking east: the west half is behind.
	east := lookAt(nt3dmath.Vec3{X: 20, Y: 0, Z: 6}, nt3dmath.Vec3{X: 60, Y: 0, Z: 5})
	settle(t, ts, east)
	if got := shown(t, scene, east); !same(got, []string{"city/tiles/east.glb"}) {
		t.Fatalf("looking east the scene shows %v, want the east child alone", got)
	}
	if f.count("city/tiles/west.glb") != 0 {
		t.Fatal("the tile behind the camera was fetched")
	}
}

// Tiles with nothing of their own to draw, and tilesets nested as a tile's
// content, are walked through: what lies below them is found, placed by
// every transform on the way, and credited to the tileset it came from.
func TestRoutersAndNestedTilesetsAreWalkedThrough(t *testing.T) {
	scene := nt3d.NewScene()
	// The outer tileset places its root 1 km east; the nested one adds 200 m north.
	outer := stamford.ToECEF().Mul(nt3dmath.TRSd(nt3dmath.Vec3d{X: 1000}, nt3dmath.Quatd{W: 1}, nt3dmath.Vec3d{X: 1, Y: 1, Z: 1}))
	inner := nt3dmath.TRSd(nt3dmath.Vec3d{Y: 200}, nt3dmath.Quatd{W: 1}, nt3dmath.Vec3d{X: 1, Y: 1, Z: 1})
	f := newFiles(map[string][]byte{
		"world/tileset.json": []byte(`{"asset":{"version":"1.1","copyright":"© World"},"geometricError":500,"root":{
			"transform":` + matrix(outer) + `,"boundingVolume":{"sphere":[0,200,0,300]},"geometricError":200,"refine":"ADD",
			"children":[{"boundingVolume":{"sphere":[0,200,0,300]},"geometricError":200,"content":{"uri":"district/tileset.json"}}]}}`),
		"world/district/tileset.json": []byte(`{"asset":{"version":"1.1","extras":{"attribution":"District data (ODbL)"}},"geometricError":200,"root":{
			"transform":` + matrix(inner) + `,"boundingVolume":{"sphere":[0,0,0,100]},"geometricError":0,"content":{"uri":"house.glb"}}}`),
		"world/district/house.glb": triangle(3, 4, 5, "", ""),
	})
	ts := open(t, scene, f, "world/tileset.json", Options{})
	camera := lookAt(nt3dmath.Vec3{X: 1000, Y: 0, Z: 80}, nt3dmath.Vec3{X: 1000, Y: 200, Z: 0})
	settle(t, ts, camera)
	if got := shown(t, scene, camera); !same(got, []string{"world/district/house.glb"}) {
		t.Fatalf("the scene shows %v, want the house in the nested tileset", got)
	}
	frame, err := scene.StillFrame(camera, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range frame.Draws {
		if len(d.Geometry.Vertices) == 0 {
			continue
		}
		if p := d.World.Point(d.Geometry.Vertices[0].Position); p.Sub(nt3dmath.Vec3{X: 1003, Y: 204, Z: 5}).D().Len() > 1e-3 {
			t.Fatalf("the house's first vertex is drawn at %v, want (1003, 204, 5)", p)
		}
		if got := d.Data.(*Tile).Credits; len(got) != 1 || got[0] != "District data (ODbL)" {
			t.Fatalf("the house is credited to %q", got)
		}
	}
	if got := ts.Credits(); len(got) != 2 || got[0] != "© World" || got[1] != "District data (ODbL)" {
		t.Fatalf("credits on screen %q, want the outer tileset's and the shown nested one's", got)
	}
}

// A tileset that nests itself is followed only so deep, and says so. (Each
// round applies the transform again, so the bounds are made to hold it all.)
func TestNestedTilesetsStopAtADepth(t *testing.T) {
	scene := nt3d.NewScene()
	f := newFiles(map[string][]byte{"loop.json": []byte(`{"asset":{"version":"1.1"},"geometricError":500,"root":{
		"transform":` + matrix(stamford.ToECEF()) + `,"boundingVolume":{"sphere":[0,0,0,1e9]},"geometricError":200,"content":{"uri":"loop.json"}}}`)})
	ts := open(t, scene, f, "loop.json", Options{MaxExternalDepth: 3})
	settle(t, ts, nearBy)
	if s := ts.Stats(); s.Failed == 0 || s.LastError == nil || !strings.Contains(s.LastError.Error(), "nest deeper") {
		t.Fatalf("stats %+v, want a failure that says tilesets nest too deep", s)
	}
	if n := f.count("loop.json"); n > 6 {
		t.Fatalf("the looping tileset was fetched %d times", n)
	}
}

// An implicit tree is walked by its subtrees: a tile is drawn only where its
// subtree says it exists and has content, each from the URI its coordinates
// give, and a subtree below is fetched when the view reaches it.
func TestImplicitTilesFollowTheirSubtrees(t *testing.T) {
	scene := nt3d.NewScene()
	// A 200 m square quadtree, two levels to a subtree, three levels deep.
	// Root subtree: the root and the two south quarters exist, with content;
	// below the south-west quarter a subtree continues.
	rootSubtree := `{"buffers":[{"byteLength":3}],"bufferViews":[{"buffer":0,"byteOffset":0,"byteLength":1},{"buffer":0,"byteOffset":1,"byteLength":2}],
		"tileAvailability":{"bitstream":0},"contentAvailability":[{"bitstream":0}],"childSubtreeAvailability":{"bitstream":1}}`
	bits := []byte{0b00111, 0b0001, 0}
	header := append([]byte("subt"), 1, 0, 0, 0)
	for len(rootSubtree)%8 != 0 {
		rootSubtree += " "
	}
	header = append(header, byte(len(rootSubtree)), byte(len(rootSubtree)>>8), 0, 0, 0, 0, 0, 0, 3, 0, 0, 0, 0, 0, 0, 0)
	f := newFiles(map[string][]byte{
		"grid/tileset.json": []byte(`{"asset":{"version":"1.1"},"geometricError":80,"root":{
			"transform":` + matrix(stamford.ToECEF()) + `,
			"boundingVolume":{"box":[0,0,5, 100,0,0, 0,100,0, 0,0,5]},"geometricError":40,"refine":"REPLACE",
			"content":{"uri":"content/{level}_{x}_{y}.glb"},
			"implicitTiling":{"subdivisionScheme":"QUADTREE","subtreeLevels":2,"availableLevels":3,"subtrees":{"uri":"subtrees/{level}_{x}_{y}.subtree"}}}}`),
		"grid/subtrees/0_0_0.subtree": append(append(header, rootSubtree...), bits...),
		"grid/subtrees/2_0_0.subtree": []byte(`{"tileAvailability":{"constant":1},"contentAvailability":[{"constant":1}],"childSubtreeAvailability":{"constant":0}}`),
		"grid/content/0_0_0.glb":      triangle(0, 0, 0, "", ""),
		"grid/content/1_0_0.glb":      triangle(-50, -50, 0, "", ""),
		"grid/content/1_1_0.glb":      triangle(50, -50, 0, "", ""),
		"grid/content/2_0_0.glb":      triangle(-75, -75, 0, "", ""),
	})
	ts := open(t, scene, f, "grid/tileset.json", Options{})
	far := lookAt(nt3dmath.Vec3{X: 0, Y: -900, Z: 300}, nt3dmath.Vec3{Z: 5})
	settle(t, ts, far)
	if got := shown(t, scene, far); !same(got, []string{"grid/content/0_0_0.glb"}) {
		t.Fatalf("from far off the scene shows %v, want the implicit root", got)
	}
	// Above the middle: an error of 20 m at level 1 wants no more than the
	// two quarters that exist.
	mid := lookAt(nt3dmath.Vec3{X: 0, Y: -260, Z: 120}, nt3dmath.Vec3{Y: -50, Z: 5})
	settle(t, ts, mid)
	if got := shown(t, scene, mid); !same(got, []string{"grid/content/1_0_0.glb", "grid/content/1_1_0.glb"}) {
		t.Fatalf("nearer, the scene shows %v, want the two quarters that exist", got)
	}
	if f.count("grid/subtrees/2_0_0.subtree") != 0 {
		t.Fatal("the lower subtree was fetched before the view reached it")
	}
	// Close over the south-west corner: its quarter refines into the
	// subtree below, whose root tile is the corner sixteenth.
	corner := lookAt(nt3dmath.Vec3{X: -80, Y: -110, Z: 12}, nt3dmath.Vec3{X: -85, Y: -85, Z: 5})
	settle(t, ts, corner)
	got := shown(t, scene, corner)
	if !contains(got, "grid/content/2_0_0.glb") {
		t.Fatalf("over the corner the scene shows %v, want the tile from the lower subtree", got)
	}
	frame, _ := scene.StillFrame(corner, 100, 100)
	for _, d := range frame.Draws {
		if tile, ok := d.Data.(*Tile); ok && tile.URIs[0] == "grid/content/2_0_0.glb" {
			if !tile.Implicit || tile.Coord != (nt3dtiles3d.Coord{Level: 2}) || tile.GeometricError != 10 {
				t.Fatalf("the corner tile's record is %+v", tile)
			}
		}
	}
	if s := ts.Stats(); s.Failed != 0 {
		t.Fatalf("a tile failed: %v", s.LastError)
	}
}

// Tiles land where float64 puts them, to under a millimeter, however far
// they are and however their placement is written: a tile transform 10 km
// out, a b3dm's RTC_CENTER, a glTF's CESIUM_RTC, and a glTF node placed in
// Earth-centered meters.
func TestTilesArePlacedToTheMillimeter(t *testing.T) {
	// Where the content belongs: a frame about 14 km from the scene's.
	there := nt3dgeo.ENU{Origin: nt3dgeo.Geodetic{Lat: 41.1365, Lon: -73.4228, Height: 100}}
	place := there.ToECEF()
	center := nt3dmath.Vec3d{X: place[12], Y: place[13], Z: place[14]}
	// The same turn without the translation, for content that carries the
	// translation itself.
	turn := place
	turn[12], turn[13], turn[14] = 0, 0, 0
	inverse, _ := turn.InverseAffine()
	local := inverse.Direction(center) // the center in the turned tile's space
	// A glTF node holds the center in glTF's Y-up: (x, z, -y) of the Z-up point.
	node := fmt.Sprintf(`,"translation":[%.17g,%.17g,%.17g]`, local.X, local.Z, -local.Y)
	rtc := fmt.Sprintf(`"extensionsUsed":["CESIUM_RTC"],"extensionsRequired":["CESIUM_RTC"],"extensions":{"CESIUM_RTC":{"center":[%.17g,%.17g,%.17g]}},`, local.X, local.Y, local.Z)
	// Each tile is bounded by a 50 m sphere around its content, as a real
	// tile is: bounds that small are in view only if the camera's own far
	// place is taken off once, and not twice.
	tileset := func(transform nt3dmath.Mat4d, sphere nt3dmath.Vec3d, uri string) []byte {
		return []byte(`{"asset":{"version":"1.1"},"geometricError":100,"root":{"transform":` + matrix(transform) + `,
			"boundingVolume":{"sphere":[` + fmt.Sprintf("%.17g,%.17g,%.17g", sphere.X, sphere.Y, sphere.Z) + `,50]},"geometricError":0,"content":{"uri":"` + uri + `"}}}`)
	}
	for name, c := range map[string]struct {
		transform nt3dmath.Mat4d
		sphere    nt3dmath.Vec3d // the bounds' center, in the tile's space
		content   []byte
	}{
		"tile transform": {place, nt3dmath.Vec3d{}, triangle(.001, .002, .003, "", "")},
		"b3dm RTC":       {turn, local, b3dm(local, triangle(.001, .002, .003, "", ""))},
		"CESIUM_RTC":     {turn, local, triangle(.001, .002, .003, "", rtc)},
		"far glTF node":  {turn, local, triangle(.001, .002, .003, node, "")},
	} {
		t.Run(name, func(t *testing.T) {
			scene := nt3d.NewScene()
			f := newFiles(map[string][]byte{"tileset.json": tileset(c.transform, c.sphere, "tile.bin"), "tile.bin": c.content})
			ts := open(t, scene, f, "tileset.json", Options{})
			// The first vertex, where float64 puts it in the scene's frame.
			want := stamford.FromECEF().Point(place.Point(nt3dmath.Vec3d{X: .001, Y: .002, Z: .003}))
			if want.Len() < 14000 {
				t.Fatalf("the tile is only %g m out", want.Len())
			}
			// A camera beside it, its own position kept in float64.
			camera := lookAt(nt3dmath.Vec3{X: 0, Y: -10, Z: 5}, nt3dmath.Vec3{})
			camera.Origin = [3]float64{want.X, want.Y, want.Z}
			settle(t, ts, camera)
			if s := ts.Stats(); s.Failed != 0 || s.Selected != 1 {
				t.Fatalf("stats %+v: %v", s, s.LastError)
			}
			frame, err := scene.StillFrame(camera, 100, 100)
			if err != nil {
				t.Fatal(err)
			}
			checked := false
			for _, d := range frame.Draws {
				if len(d.Geometry.Vertices) == 0 || d.Hidden {
					continue
				}
				// Draws are relative to the camera's origin, which is the
				// vertex's own place: it should be drawn at zero.
				if off := d.World.Point(d.Geometry.Vertices[0].Position).D().Len(); off > 1e-3 {
					t.Fatalf("the vertex is drawn %g m from where it belongs", off)
				}
				checked = true
			}
			if !checked {
				t.Fatal("nothing was drawn")
			}
		})
	}
}

// Tiles the view has left stay loaded until their bytes exceed the budget;
// then the longest unused go, and never one the view is using.
func TestUnusedTilesAreEvictedOverTheByteBudget(t *testing.T) {
	scene := nt3d.NewScene()
	f := newFiles(block("REPLACE"))
	size := int64(len(f.data["city/root.glb"]))
	var unloaded []string
	ts := open(t, scene, f, "city/tileset.json", Options{MaxBytes: 2*size + size/2, OnTileUnloaded: func(tile *Tile) { unloaded = append(unloaded, tile.URIs[0]) }})
	settle(t, ts, nearBy)
	// All three are loaded, and all three were touched: the root is the
	// children's way in. Over budget, and nothing may go.
	if s := ts.Stats(); s.Resident != 3 || s.Evictions != 0 {
		t.Fatalf("stats %+v, want all three kept while in use", s)
	}
	settle(t, ts, farAway)
	if s := ts.Stats(); s.Resident != 2 || s.Evictions != 1 || s.ResidentBytes != 2*size {
		t.Fatalf("stats %+v, want one child evicted to fit %d bytes", s, 2*size+size/2)
	}
	if len(unloaded) != 1 || !strings.HasPrefix(unloaded[0], "city/tiles/") {
		t.Fatalf("unloaded %v, want one child", unloaded)
	}
	if got := shown(t, scene, farAway); !same(got, []string{"city/root.glb"}) {
		t.Fatalf("after eviction the scene shows %v", got)
	}
	// Wanted again, the evicted tile comes back.
	settle(t, ts, nearBy)
	if got := shown(t, scene, nearBy); !same(got, []string{"city/tiles/east.glb", "city/tiles/west.glb"}) {
		t.Fatalf("up close again the scene shows %v", got)
	}
}

// A loaded tile's nodes carry a record of the tile: its URI, error, level,
// credits, and metadata. OnTileLoaded sees it before the tile is first
// shown, with its nodes in the scene, so the tile can be restyled.
func TestTilesCarryTheirRecordAndCallbacksSeeThem(t *testing.T) {
	scene := nt3d.NewScene()
	data := block("REPLACE")
	text := string(data["city/tileset.json"])
	text = strings.Replace(text, `"geometricError":100,"root"`, `"geometricError":100,"schema":{"classes":{"block":{"properties":{"name":{"type":"STRING"}}}}},"metadata":{"class":"block","properties":{"name":"city"}},"groups":[{"class":"block","properties":{"name":"west side"}}],"root"`, 1)
	text = strings.Replace(text, `"content":{"uri":"tiles/west.glb"}`, `"content":{"uri":"tiles/west.glb","group":0,"metadata":{"class":"block","properties":{"name":"content"}}},"metadata":{"class":"block","properties":{"name":"west"}}`, 1)
	data["city/tileset.json"] = []byte(text)
	var loaded []*Tile
	ts := open(t, scene, newFiles(data), "city/tileset.json", Options{OnTileLoaded: func(tile *Tile) {
		loaded = append(loaded, tile)
		got, err := scene.NodeData(tile.Node)
		if err != nil || got != any(tile) {
			t.Errorf("in OnTileLoaded the tile's node carries %v, %v", got, err)
		}
	}})
	settle(t, ts, nearBy)
	if len(loaded) != 3 {
		t.Fatalf("OnTileLoaded saw %d tiles, want 3", len(loaded))
	}
	var west *Tile
	for _, tile := range loaded {
		if tile.URIs[0] == "city/tiles/west.glb" {
			west = tile
		}
	}
	if west == nil || west.Level != 1 || west.GeometricError != 0 || west.Bytes == 0 || west.Implicit {
		t.Fatalf("the west tile's record is %+v", west)
	}
	if len(west.Credits) != 1 || west.Credits[0] != "© City Survey" {
		t.Fatalf("credits %q", west.Credits)
	}
	if west.Metadata.Properties["name"] != "west" || west.TilesetMetadata.Properties["name"] != "city" || west.Contents[0].Properties["name"] != "content" || west.Groups[0].Properties["name"] != "west side" {
		t.Fatalf("metadata: tile %+v, tileset %+v, content %+v, group %+v", west.Metadata, west.TilesetMetadata, west.Contents[0], west.Groups[0])
	}
	// A draw of the tile's mesh reports the same record.
	frame, _ := scene.StillFrame(nearBy, 100, 100)
	found := false
	for _, d := range frame.Draws {
		if d.Data == any(west) && len(d.Geometry.Vertices) > 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("no draw of the west tile's mesh carries its record")
	}
}

// Past the splat budget the selection is made coarser, so whole tiles stay
// at a lower level, and the threshold used says so.
func TestTheSplatBudgetCoarsensTheSelection(t *testing.T) {
	data := block("REPLACE")
	data["city/root.glb"] = splats(10)
	data["city/tiles/west.glb"] = splats(400)
	data["city/tiles/east.glb"] = splats(400)
	for _, c := range []struct {
		budget int
		want   []string
	}{
		{0, []string{"city/tiles/east.glb", "city/tiles/west.glb"}},
		{1000, []string{"city/tiles/east.glb", "city/tiles/west.glb"}},
		{500, []string{"city/root.glb"}},
	} {
		scene := nt3d.NewScene()
		ts := open(t, scene, newFiles(data), "city/tileset.json", Options{SplatBudget: c.budget})
		settle(t, ts, nearBy)
		settle(t, ts, nearBy)
		if got := shown(t, scene, nearBy); !same(got, c.want) {
			t.Errorf("with a budget of %d splats the scene shows %v, want %v", c.budget, got, c.want)
		}
		s := ts.Stats()
		if c.budget == 500 && (s.SplatsSelected != 10 || s.ScreenSpaceError <= 16) {
			t.Errorf("over budget: stats %+v, want 10 splats selected at a raised threshold", s)
		}
		if c.budget == 0 && (s.SplatsSelected != 800 || s.ScreenSpaceError != 16 || s.SplatsResident != 810) {
			t.Errorf("without a budget: stats %+v", s)
		}
	}
}

// A tile whose content is missing fails once and is counted. It does not
// leave a hole: its parent goes on standing in for it, and Settle returns.
func TestAFailedTileLeavesItsParentShowing(t *testing.T) {
	scene := nt3d.NewScene()
	data := block("REPLACE")
	delete(data, "city/tiles/west.glb")
	f := newFiles(data)
	ts := open(t, scene, f, "city/tileset.json", Options{})
	settle(t, ts, nearBy)
	if got := shown(t, scene, nearBy); !same(got, []string{"city/root.glb"}) {
		t.Fatalf("the scene shows %v, want the root standing in for the child that failed", got)
	}
	settle(t, ts, nearBy)
	if s := ts.Stats(); s.Failed != 1 || s.LastError == nil || f.count("city/tiles/west.glb") != 1 {
		t.Fatalf("stats %+v after %d fetches, want the missing tile to fail once", s, f.count("city/tiles/west.glb"))
	}
}

// A load the view no longer wants is stopped, and Close removes every node
// the tileset added and stops what is still loading.
func TestStaleLoadsStopAndCloseRemovesEverything(t *testing.T) {
	scene := nt3d.NewScene()
	f := newFiles(block("REPLACE"))
	f.hold("city/tiles/east.glb")
	f.hold("city/tiles/west.glb")
	ts, err := Open(context.Background(), scene, f, "city/tileset.json", Options{Frame: stamford})
	if err != nil {
		t.Fatal(err)
	}
	settle(t, ts, farAway)
	if err := ts.Update(nearBy, 100, 100); err != nil {
		t.Fatal(err)
	}
	<-f.started
	<-f.started
	if s := ts.Stats(); s.Loading != 2 {
		t.Fatalf("stats %+v, want two children loading", s)
	}
	// The view leaves: the held loads are stopped, not left to finish.
	settle(t, ts, farAway)
	if s := ts.Stats(); s.Loading != 0 || s.Failed != 0 || s.Resident != 1 {
		t.Fatalf("stats %+v, want the stale loads stopped and nothing failed", s)
	}
	before, _ := scene.StillFrame(farAway, 100, 100)
	if len(before.Draws) == 0 {
		t.Fatal("nothing drawn before Close")
	}
	ts.Close()
	after, _ := scene.StillFrame(farAway, 100, 100)
	if len(after.Draws) != 0 {
		t.Fatalf("%d draws remain after Close", len(after.Draws))
	}
	if err := ts.Update(farAway, 100, 100); err == nil {
		t.Fatal("a closed tileset updated")
	}
}

// Open refuses what cannot work: no scene, a frame off the Earth, a tileset
// that is missing or is not one.
func TestOpenRefusals(t *testing.T) {
	f := newFiles(map[string][]byte{"bad.json": []byte(`{"asset":{"version":"9"}}`)})
	scene := nt3d.NewScene()
	if _, err := Open(context.Background(), nil, f, "bad.json", Options{}); err == nil {
		t.Error("opened without a scene")
	}
	if _, err := Open(context.Background(), scene, f, "bad.json", Options{Frame: nt3dgeo.ENU{Origin: nt3dgeo.Geodetic{Lat: 91}}}); err == nil {
		t.Error("opened in a frame off the Earth")
	}
	if _, err := Open(context.Background(), scene, f, "missing.json", Options{}); err == nil {
		t.Error("opened a missing tileset")
	}
	if _, err := Open(context.Background(), scene, f, "bad.json", Options{}); err == nil {
		t.Error("opened a tileset of an unknown version")
	}
}

// Settle leaves the same scene however the loads race: tiles are added in
// the order they were first met, so nodes, and with them what wins where two
// tiles meet at one depth, do not depend on which fetch finished first.
func TestSettleIsRepeatableHoweverLoadsRace(t *testing.T) {
	order := func(seed int64) []string {
		scene := nt3d.NewScene()
		inner := newFiles(block("ADD"))
		// Each fetch takes its own time, different every run.
		delay := map[string]time.Duration{"city/root.glb": time.Duration(seed%3) * time.Millisecond, "city/tiles/west.glb": time.Duration((seed+1)%3) * 2 * time.Millisecond, "city/tiles/east.glb": time.Duration((seed+2)%3) * 2 * time.Millisecond}
		slow := nt3dtiles3d.FetcherFunc(func(ctx context.Context, uri string) ([]byte, error) {
			time.Sleep(delay[uri])
			return inner.Fetch(ctx, uri)
		})
		ts, err := Open(context.Background(), scene, slow, "city/tileset.json", Options{Frame: stamford})
		if err != nil {
			t.Fatal(err)
		}
		defer ts.Close()
		settle(t, ts, nearBy)
		frame, err := scene.StillFrame(nearBy, 100, 100)
		if err != nil {
			t.Fatal(err)
		}
		var drawn []string
		for _, d := range frame.Draws {
			if tile, ok := d.Data.(*Tile); ok && len(d.Geometry.Vertices) > 0 {
				drawn = append(drawn, tile.URIs[0])
			}
		}
		return drawn
	}
	want := order(0)
	if len(want) != 3 {
		t.Fatalf("drew %v, want three tiles", want)
	}
	for seed := int64(1); seed < 6; seed++ {
		if got := order(seed); !same(got, want) {
			t.Fatalf("with other timings the scene draws %v, and before %v", got, want)
		}
	}
}

// The error threshold can be changed while the tileset runs: raised, the
// same view keeps the coarse tile; lowered again, it refines.
func TestTheErrorThresholdCanBeChanged(t *testing.T) {
	scene := nt3d.NewScene()
	ts := open(t, scene, newFiles(block("REPLACE")), "city/tileset.json", Options{})
	if ts.MaxScreenSpaceError() != 16 {
		t.Fatalf("the default threshold is %g, want 16", ts.MaxScreenSpaceError())
	}
	ts.SetMaxScreenSpaceError(500)
	settle(t, ts, nearBy)
	if got := shown(t, scene, nearBy); !same(got, []string{"city/root.glb"}) {
		t.Fatalf("at a threshold of 500 pixels the scene shows %v, want the root", got)
	}
	ts.SetMaxScreenSpaceError(0)
	settle(t, ts, nearBy)
	if got := shown(t, scene, nearBy); len(got) != 2 || ts.MaxScreenSpaceError() != 16 {
		t.Fatalf("back at the default the scene shows %v at %g pixels", got, ts.MaxScreenSpaceError())
	}
}

// With AtCenter the scene is drawn around the tileset's own middle, whatever
// frame is given: the root's bounds are centered on the scene's origin, and
// Frame says where that is on the Earth.
func TestAtCenterDrawsAroundTheTileset(t *testing.T) {
	scene := nt3d.NewScene()
	ts, err := Open(context.Background(), scene, newFiles(block("REPLACE")), "city/tileset.json", Options{AtCenter: true, Frame: nt3dgeo.ENU{Origin: nt3dgeo.Geodetic{Lat: -33, Lon: 151}}})
	if err != nil {
		t.Fatal(err)
	}
	defer ts.Close()
	box, ok := ts.Bounds()
	if !ok || box.Center.Len() > 1e-6 {
		t.Fatalf("the root's bounds are centered at %+v, want the scene's origin", box.Center)
	}
	// The block's bounds are centered 5 m above the Stamford frame's origin.
	want := stamford.Origin
	want.Height = 5
	if got := ts.Frame().Origin; got.ECEF().Dist(want.ECEF()) > 1e-6 {
		t.Fatalf("the frame is at %+v, want %+v", got, want)
	}
}
