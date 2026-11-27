// Copyright (c) 2026 Neomantra Corp.

// Package nt3dtiles3dscene streams a 3D Tiles tileset into a scene: it joins
// package nt3dtiles3d, which reads tilesets, to the engine, which draws.
//
// Open reads a tileset and returns a Tileset bound to a scene. Each frame,
// Update is given the camera: it chooses the tiles the view needs, by how
// large each tile's error would look on screen, asks for the ones that are
// missing, adds the ones that have arrived as nodes, shows and hides nodes
// to match the choice, and removes tiles that have gone unused when their
// bytes exceed a budget.
//
// Fetching and decoding run on background goroutines. The scene is touched
// only inside Update, Settle, and Close, which belong on the goroutine that
// owns the scene, as every change to a scene does.
//
// The scene is drawn in a local east-north-up frame (nt3dgeo.ENU). Each tile
// is placed in it exactly: its transform into Earth-centered meters is
// composed in float64 and split into a node origin and a float32 matrix, so
// a tile kilometers away is where it belongs to well under a millimeter.
package nt3dtiles3dscene

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/geo"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
	"nimbleterminal.dev/nt3d/tiles3d"
)

// Options configures a Tileset. A zero field means its default.
type Options struct {
	// Frame is the east-north-up frame the scene is drawn in. Tiles are
	// placed in it, and the camera's position is taken to be in it.
	Frame nt3dgeo.ENU
	// AtCenter draws the scene around the tileset itself instead: Frame is
	// ignored, and the frame's origin is the middle of the root tile's
	// bounds. It is for showing a tileset whose place is not known ahead.
	// Tileset.Frame says which frame was used.
	AtCenter bool
	// Parent is the node the tileset's nodes are added under: nt3d.Root
	// when zero.
	Parent nt3d.NodeID
	// Name names the node every tile is added under: "tileset" when empty.
	// A scene's node names are unique, so a second tileset in one scene
	// needs a name of its own.
	Name string
	// MaxScreenSpaceError is the largest error, in pixels, a drawn tile may
	// show before its children are wanted: 16 when zero. Smaller is finer.
	MaxScreenSpaceError float64
	// MaxBytes is how many bytes of fetched content may stay loaded while
	// unused: 512 MiB when zero. Tiles in use are never removed for it.
	MaxBytes int64
	// Workers is how many tiles are fetched and decoded at once: 4 when zero.
	Workers int
	// SplatBudget, when positive, is the most Gaussian splats the drawn
	// tiles may hold together. Past it the selection is made coarser, so
	// that whole tiles are left at a lower level rather than every tile
	// thinned. Set the scene's own budget (nt3d.Scene.SetSplatBudget) to
	// the same number.
	SplatBudget int
	// MaxExternalDepth is how many tilesets may be nested as content, one
	// inside another: 8 when zero.
	MaxExternalDepth int
	// Tiles limits the tileset documents read, and Mesh the tile content
	// decoded; nil means the defaults of those packages. Mesh's Open and
	// AssetOnly are set by the Tileset.
	Tiles *nt3dtiles3d.Options
	Mesh  *nt3dmesh.Options
	// OnTileLoaded is called, on the goroutine that calls Update, after a
	// tile's content has been added to the scene, hidden, and before it is
	// first shown: the place to restyle it, or to take its triangles for
	// collision. OnTileUnloaded is called before a tile's nodes are removed.
	OnTileLoaded, OnTileUnloaded func(*Tile)
}

func (o *Options) maxSSE() float64 {
	if o.MaxScreenSpaceError > 0 {
		return o.MaxScreenSpaceError
	}
	return 16
}

func (o *Options) maxBytes() int64 {
	if o.MaxBytes > 0 {
		return o.MaxBytes
	}
	return 512 << 20
}

func (o *Options) workers() int {
	if o.Workers > 0 {
		return o.Workers
	}
	return 4
}

func (o *Options) maxExternalDepth() int {
	if o.MaxExternalDepth > 0 {
		return o.MaxExternalDepth
	}
	return 8
}

// Tile is what a loaded tile's node carries as its data (nt3d.Scene.NodeData,
// and so a Draw's and a Hit's Data): what the tile is and where it came from.
type Tile struct {
	// Node is the scene node the tile's content hangs under.
	Node nt3d.NodeID
	// URIs are the tile's contents, absolute.
	URIs []string
	// Level is how many tiles lie between this one and the tileset's root.
	Level int
	// Implicit says the tile belongs to an implicit tree, at Coord.
	Implicit bool
	Coord    nt3dtiles3d.Coord
	// GeometricError is the tile's own, in meters.
	GeometricError float64
	// Credits are who the tile's tileset must be credited to, as text safe
	// to show.
	Credits []string
	// TilesetMetadata is the metadata of the tileset the tile is in;
	// Metadata the tile's own; Contents each content's, and Groups each
	// content's group's, index for index with URIs; Subtree the metadata of
	// the implicit subtree the tile is in. Each is nil where the file has none.
	TilesetMetadata *nt3dtiles3d.Entity
	Metadata        *nt3dtiles3d.Entity
	Contents        []*nt3dtiles3d.Entity
	Groups          []*nt3dtiles3d.Entity
	Subtree         *nt3dtiles3d.Entity
	// Bytes is the size of the tile's content as fetched, and Splats how
	// many Gaussian splats it holds.
	Bytes  int64
	Splats int
}

// Stats says what a Tileset is doing, as of the last Update.
type Stats struct {
	// Selected is how many tiles are shown, and Visited how many the
	// selection looked at.
	Selected, Visited int
	// Loading is how many tiles and subtrees are being fetched or decoded.
	Loading int
	// Resident is how many tiles are loaded, shown or not, and
	// ResidentBytes their content as fetched.
	Resident      int
	ResidentBytes int64
	// Evictions counts tiles removed to stay within MaxBytes, and Failed
	// tiles whose content could not be loaded, since Open.
	Evictions, Failed int
	// SplatsResident and SplatsSelected count Gaussian splats in loaded and
	// in shown tiles.
	SplatsResident, SplatsSelected int
	// ScreenSpaceError is the threshold the last selection used: the
	// option's, or more when the splat budget made it coarser.
	ScreenSpaceError float64
	// LastError is the most recent failure to load a tile, or nil.
	LastError error
}

// Tileset is a tileset streaming into a scene.
type Tileset struct {
	scene *nt3d.Scene
	fetch nt3dtiles3d.Fetcher
	o     Options
	// fromECEF carries Earth-centered meters into the scene's frame.
	fromECEF nt3dmath.Mat4d
	group    nt3d.NodeID
	root     *tile

	ctx      context.Context
	cancel   context.CancelFunc
	results  chan result
	loading  int
	inflight map[*tile]struct{}
	serial   int
	tiles    int // tiles met so far
	deepest  int // the deepest level of any tile met
	levelCap int // with a splat budget, the level past which nothing refines; -1 for none
	// refused is the view the splat budget last made the selection coarser
	// for, or refused to ease it for. While the view is the same, the
	// selection is not eased again.
	refused viewKey
	closed  bool

	frame    uint64
	draw     []*tile
	wanted   []*tile
	loaded   map[*tile]struct{}
	coarsen  float64
	stats    Stats
	resident int64
	credits  []string
}

// viewKey is a camera and a picture size: what a selection is made for.
type viewKey struct {
	camera        nt3d.Camera
	width, height int
}

// document is one tileset file: the root's, or one nested as content.
type document struct {
	uri      string
	credits  []string
	metadata *nt3dtiles3d.Entity
	groups   []nt3dtiles3d.Entity
	depth    int
}

type loadState uint8

const (
	idle loadState = iota
	loadingNow
	loadedNow
	failedNow
)

// tile is the runtime's view of one tile, explicit or implicit.
type tile struct {
	parent *tile
	doc    *document
	src    *nt3dtiles3d.Tile // nil below an implicit root
	level  int

	toECEF         nt3dmath.Mat4d
	box            nt3dmath.OBBd // in the scene's frame
	boxless        bool          // the bounds could not be placed: never culled, always near
	geometricError float64
	refine         nt3dtiles3d.Refine

	uris    []string
	content []nt3dtiles3d.Content // index for index with uris, for explicit tiles

	// Implicit tiles: the tree, the transform of its root, this tile's
	// coordinate, and the subtree that speaks for it once fetched.
	implicit     *nt3dtiles3d.Implicit
	implicitRoot nt3dmath.Mat4d
	coord        nt3dtiles3d.Coord
	subtree      *nt3dtiles3d.Subtree
	subtreeState loadState

	children []*tile
	expanded bool
	external bool // the content was a tileset, whose root is the one child

	state   loadState
	stop    context.CancelFunc
	node    nt3d.NodeID
	visible bool
	record  *Tile

	used, drawn uint64 // the frames it was last visited and last chosen
	sse         float64
	id          int // the order tiles were first met in, which is the order they are added in
}

// Open fetches and reads the tileset at uri and binds it to scene. Nothing
// is drawn until Update is called with a camera.
func Open(ctx context.Context, scene *nt3d.Scene, fetch nt3dtiles3d.Fetcher, uri string, o Options) (*Tileset, error) {
	if scene == nil || fetch == nil {
		return nil, errors.New("nt3dtiles3dscene: a tileset needs a scene and a fetcher")
	}
	if err := o.Frame.Origin.Validate(); err != nil && !o.AtCenter {
		return nil, fmt.Errorf("nt3dtiles3dscene: frame origin: %w", err)
	}
	data, err := fetch.Fetch(ctx, uri)
	if err != nil {
		return nil, fmt.Errorf("nt3dtiles3dscene: %w", err)
	}
	parsed, err := nt3dtiles3d.ParseTileset(data, o.Tiles)
	if err != nil {
		return nil, err
	}
	if o.AtCenter {
		center, err := parsed.Center()
		if err != nil {
			return nil, err
		}
		o.Frame = nt3dgeo.ENU{Origin: center}
	}
	parent := o.Parent
	if parent == 0 {
		parent = nt3d.Root
	}
	name := o.Name
	if name == "" {
		name = "tileset"
	}
	group, err := scene.Add(parent, name)
	if err != nil {
		return nil, err
	}
	ts := &Tileset{scene: scene, fetch: fetch, o: o, fromECEF: o.Frame.FromECEF(), group: group, loaded: map[*tile]struct{}{}, coarsen: 1, levelCap: -1}
	ts.ctx, ts.cancel = context.WithCancel(context.Background())
	ts.results = make(chan result, o.workers())
	doc := &document{uri: uri, credits: parsed.Asset.Credits(), metadata: parsed.Metadata, groups: parsed.Groups}
	ts.root = ts.explicit(parsed.Root, nil, doc, nt3dmath.IdentityD(), nt3dtiles3d.RefineReplace, 0)
	ts.credits = doc.credits
	return ts, nil
}

// explicit builds the runtime tile for a tile written in a tileset.
func (ts *Tileset) explicit(src *nt3dtiles3d.Tile, parent *tile, doc *document, parentToECEF nt3dmath.Mat4d, inherited nt3dtiles3d.Refine, level int) *tile {
	ts.tiles++
	ts.deepest = max(ts.deepest, level)
	t := &tile{id: ts.tiles, parent: parent, doc: doc, src: src, level: level, refine: src.Refine}
	t.toECEF = parentToECEF.Mul(src.Transform)
	// A tile's error is written in the tile's own units: a transform that
	// scales the tile scales its error.
	t.geometricError = src.GeometricError * largestScale(t.toECEF)
	if t.refine == nt3dtiles3d.RefineInherit {
		t.refine = inherited
	}
	ts.bound(t, src.Bounds, t.toECEF)
	if src.Implicit != nil {
		// The written tile is the root of an implicit tree: what it shows
		// and what lies below it are for its subtree to say.
		t.implicit, t.implicitRoot = src.Implicit, t.toECEF
		return t
	}
	for _, c := range src.Contents {
		uri, err := nt3dtiles3d.Resolve(doc.uri, c.URI)
		if err != nil {
			continue
		}
		t.uris = append(t.uris, uri)
		t.content = append(t.content, c)
	}
	return t
}

// largestScale is the most a transform stretches a length: the longest of
// its three axes.
func largestScale(m nt3dmath.Mat4d) float64 {
	x := m.Direction(nt3dmath.Vec3d{X: 1}).Len()
	y := m.Direction(nt3dmath.Vec3d{Y: 1}).Len()
	z := m.Direction(nt3dmath.Vec3d{Z: 1}).Len()
	return math.Max(x, math.Max(y, z))
}

// bound places a tile's bounding volume in the scene's frame.
func (ts *Tileset) bound(t *tile, v nt3dtiles3d.Volume, toECEF nt3dmath.Mat4d) {
	box, err := v.World(toECEF)
	if err != nil {
		t.boxless = true
		return
	}
	t.box = box.Transformed(ts.fromECEF)
	t.boxless = !t.box.IsFinite()
}

// expand builds a tile's children the first time they are asked for.
func (ts *Tileset) expand(t *tile) {
	if t.expanded {
		return
	}
	t.expanded = true
	if t.implicit == nil {
		for _, child := range t.src.Children {
			t.children = append(t.children, ts.explicit(child, t, t.doc, t.toECEF, t.refine, t.level+1))
		}
		return
	}
	for _, c := range t.implicit.Children(t.coord) {
		if !t.implicit.Valid(c) {
			continue
		}
		ts.tiles++
		ts.deepest = max(ts.deepest, t.level+1)
		child := &tile{id: ts.tiles, parent: t, doc: t.doc, level: t.level + 1, refine: t.refine, toECEF: t.implicitRoot,
			implicit: t.implicit, implicitRoot: t.implicitRoot, coord: c, geometricError: t.implicit.GeometricError(c) * largestScale(t.implicitRoot)}
		if c.Level%t.implicit.SubtreeLevels == 0 {
			// The child starts a subtree of its own, which must be fetched
			// before anything is known of it but that it exists.
			if !t.subtree.ChildSubtreeAvailable(c) {
				continue
			}
		} else {
			if !t.subtree.TileAvailable(c) {
				continue
			}
			ts.adopt(child, t.subtree)
		}
		ts.bound(child, t.implicit.Bounds(c), t.implicitRoot)
		t.children = append(t.children, child)
	}
}

// adopt gives an implicit tile the subtree that speaks for it, and with it
// the contents it has.
func (ts *Tileset) adopt(t *tile, s *nt3dtiles3d.Subtree) {
	t.subtree, t.subtreeState = s, loadedNow
	t.uris = nil
	for k, ref := range t.implicit.ContentURIs(t.coord) {
		if !s.ContentAvailable(t.coord, k) {
			continue
		}
		if uri, err := nt3dtiles3d.Resolve(t.doc.uri, ref); err == nil {
			t.uris = append(t.uris, uri)
		}
	}
}

// Close stops loading and removes everything the tileset added to the scene.
// It belongs on the scene's goroutine. A closed Tileset does nothing more.
func (ts *Tileset) Close() {
	if ts.closed {
		return
	}
	ts.closed = true
	ts.cancel()
	for t := range ts.loaded {
		if ts.o.OnTileUnloaded != nil && t.record != nil {
			ts.o.OnTileUnloaded(t.record)
		}
	}
	ts.loaded = map[*tile]struct{}{}
	_ = ts.scene.Remove(ts.group)
}

// Frame returns the east-north-up frame the scene is drawn in: the one
// given to Open, or with Options.AtCenter the one at the tileset's middle.
func (ts *Tileset) Frame() nt3dgeo.ENU { return ts.o.Frame }

// MaxScreenSpaceError returns the error, in pixels, past which a tile's
// children are wanted.
func (ts *Tileset) MaxScreenSpaceError() float64 { return ts.o.maxSSE() }

// SetMaxScreenSpaceError changes it, from the next Update on: smaller is
// finer. Zero or less returns to the default of 16.
func (ts *Tileset) SetMaxScreenSpaceError(pixels float64) {
	if !(pixels > 0) {
		pixels = 0
	}
	ts.o.MaxScreenSpaceError = pixels
}

// Node returns the scene node every tile is added under.
func (ts *Tileset) Node() nt3d.NodeID { return ts.group }

// Stats returns what the tileset was doing as of the last Update.
func (ts *Tileset) Stats() Stats { return ts.stats }

// Credits returns who must be credited for what is on screen: the tileset's
// own credits and those of every nested tileset with a tile shown, without
// repeats, as text safe to show.
func (ts *Tileset) Credits() []string { return append([]string(nil), ts.credits...) }

// Bounds returns the root tile's bounds in the scene's frame, as an oriented
// box, and false when they could not be placed. It is where to point a
// camera at first.
func (ts *Tileset) Bounds() (nt3dmath.OBBd, bool) { return ts.root.box, !ts.root.boxless }

// Update makes the scene show what camera needs, for a viewport of width by
// height pixels. It applies whatever finished loading since the last call,
// chooses tiles, starts loading the missing ones, shows and hides nodes, and
// evicts. It never waits: call it each frame, before Scene.Frame.
func (ts *Tileset) Update(camera nt3d.Camera, width, height int) error {
	if ts.closed {
		return errors.New("nt3dtiles3dscene: the tileset is closed")
	}
	if width < 1 || height < 1 {
		return errors.New("nt3dtiles3dscene: the viewport must be at least one pixel each way")
	}
	if err := camera.Validate(); err != nil {
		return err
	}
	var arrived []result
	for {
		select {
		case r := <-ts.results:
			arrived = append(arrived, r)
			continue
		default:
		}
		break
	}
	ts.applyAll(arrived)
	ts.selectTiles(camera.Valid(), width, height)
	return nil
}

// applyAll brings finished loads into the scene in the order their tiles
// were first met, not the order they happened to finish in: nodes are drawn
// in the order they are added, and where two tiles meet at the same depth
// the later one wins.
func (ts *Tileset) applyAll(arrived []result) {
	sort.SliceStable(arrived, func(i, j int) bool { return arrived[i].t.id < arrived[j].t.id })
	for _, r := range arrived {
		ts.apply(r)
	}
}

// Settle calls Update until nothing more is loading or wanted, waiting for
// loads in between: after it the scene shows everything camera needs. It is
// for a still picture, a test, or a first frame. It returns ctx's error if
// ctx ends first.
//
// Settle is repeatable: each round it waits for every load in flight before
// adding any of them, in the order their tiles were first met, so the same
// tileset and camera leave the same scene, node for node, however the loads
// raced.
func (ts *Tileset) Settle(ctx context.Context, camera nt3d.Camera, width, height int) error {
	if ts.closed {
		return errors.New("nt3dtiles3dscene: the tileset is closed")
	}
	if width < 1 || height < 1 {
		return errors.New("nt3dtiles3dscene: the viewport must be at least one pixel each way")
	}
	if err := camera.Validate(); err != nil {
		return err
	}
	for {
		// Choose first: that stops loads the view no longer wants, which
		// would otherwise be waited for.
		ts.selectTiles(camera.Valid(), width, height)
		if ts.loading == 0 {
			if len(ts.wanted) == 0 {
				return nil
			}
			continue
		}
		arrived := make([]result, 0, ts.loading)
		for len(arrived) < cap(arrived) {
			select {
			case r := <-ts.results:
				arrived = append(arrived, r)
			case <-ctx.Done():
				ts.applyAll(arrived)
				return ctx.Err()
			}
		}
		ts.applyAll(arrived)
	}
}

// selectTiles chooses the tiles to draw and makes the scene match.
func (ts *Tileset) selectTiles(camera nt3d.Camera, width, height int) {
	view := newView(camera, width, height)
	limit := ts.o.maxSSE()
	// Past the splat budget the selection is made coarser a step at a time:
	// first by raising the error a tile may show, which keeps what is near
	// finer than what is far, and, where that cannot help, because the eye
	// is inside a tile and its error has no bound, by refusing to refine
	// past a level. When there is room again one step is given back, tried
	// first so that the picture does not swing between two selections, and
	// only for a view the budget has not already refused: easing a still
	// view would ask again for the tiles it was just denied, load them,
	// refuse them, and evict them, over and over.
	key := viewKey{camera, width, height}
	relaxed, denied := false, false
	heldCoarsen, heldCap := ts.coarsen, ts.levelCap
	for attempt := 0; ; attempt++ {
		ts.frame++
		ts.draw, ts.wanted = ts.draw[:0], ts.wanted[:0]
		ts.stats.Visited = 0
		ts.visit(ts.root, view, limit*ts.coarsen)
		splats, drawnTo := 0, 0
		for _, t := range ts.draw {
			splats += t.record.Splats
			drawnTo = max(drawnTo, t.level)
		}
		ts.stats.SplatsSelected = splats
		if ts.o.SplatBudget <= 0 {
			ts.coarsen, ts.levelCap = 1, -1
			break
		}
		if attempt >= 64 {
			break
		}
		if splats > ts.o.SplatBudget {
			if relaxed && !denied {
				// The step given back does not fit: take it again.
				ts.coarsen, ts.levelCap, denied, ts.refused = heldCoarsen, heldCap, true, key
				continue
			}
			ts.refused = key
			if ts.coarsen < 8 {
				ts.coarsen *= 1.25
				continue
			}
			if drawnTo == 0 {
				break // Nothing coarser than the root exists.
			}
			ts.levelCap = drawnTo - 1
			continue
		}
		// A step that fit may be followed by another, so a view with room
		// for everything gets back to the plain threshold in one update.
		if !denied && key != ts.refused && float64(splats) < .6*float64(ts.o.SplatBudget) && (ts.levelCap >= 0 || ts.coarsen > 1) {
			heldCoarsen, heldCap, relaxed = ts.coarsen, ts.levelCap, true
			if ts.levelCap >= 0 {
				if ts.levelCap++; ts.levelCap > ts.deepest {
					ts.levelCap = -1
				}
			} else {
				ts.coarsen = math.Max(1, ts.coarsen/1.1)
			}
			continue
		}
		break
	}
	for _, t := range ts.draw {
		t.drawn = ts.frame
	}
	credits := append([]string(nil), ts.root.doc.credits...)
	for t := range ts.loaded {
		show := t.drawn == ts.frame
		if show != t.visible {
			if err := ts.scene.SetVisible(t.node, show); err == nil {
				t.visible = show
			}
		}
		if show {
			for _, c := range t.doc.credits {
				if !contains(credits, c) {
					credits = append(credits, c)
				}
			}
		}
	}
	ts.credits = credits
	ts.dispatch()
	ts.evict()
	ts.stats.Selected = len(ts.draw)
	ts.stats.Loading = ts.loading
	ts.stats.Resident = len(ts.loaded)
	ts.stats.ResidentBytes = ts.resident
	ts.stats.ScreenSpaceError = limit * ts.coarsen
	ts.stats.SplatsResident = 0
	for t := range ts.loaded {
		ts.stats.SplatsResident += t.record.Splats
	}
}

func contains(list []string, s string) bool {
	for _, have := range list {
		if have == s {
			return true
		}
	}
	return false
}

// visit decides what to draw of t and below, appending to ts.draw, and
// reports whether that covers t: whether nothing more needs to arrive for
// t's part of the view to be whole.
func (ts *Tileset) visit(t *tile, view view, limit float64) bool {
	ts.stats.Visited++
	if !t.boxless && view.outside(t.box) {
		// Out of view: nothing to cover, and not in use, so it may be
		// evicted and a load of it stopped.
		return true
	}
	t.used = ts.frame
	t.sse = view.screenSpaceError(t)
	if t.implicit != nil && t.subtree == nil {
		// Nothing is known of the tile until its subtree says. A subtree
		// that could not be read is a branch that cannot be drawn: it is
		// not asked for again, and is not covered either.
		if t.subtreeState == idle {
			ts.wanted = append(ts.wanted, t)
		}
		return false
	}
	// A tile's own content comes first: it is what holds the view while
	// finer tiles arrive, and for a nested tileset it is where the
	// children are.
	// A tile whose content failed is not asked for again, and is not whole
	// either: where it can, its parent goes on standing in for it.
	if len(t.uris) > 0 && t.state != loadedNow {
		if t.state == idle {
			ts.wanted = append(ts.wanted, t)
		}
		return false
	}
	ts.expand(t)
	renderable := t.state == loadedNow && t.node != 0
	// A tile with nothing of its own to draw is only a way to its children,
	// and is walked through whatever its error. One that can be drawn
	// refines while its error shows, and not past the level a splat budget
	// has cut the selection off at.
	refine := len(t.children) > 0 && (!renderable || (t.sse > limit && (ts.levelCap < 0 || t.level < ts.levelCap)))
	if !refine {
		if renderable {
			ts.draw = append(ts.draw, t)
		}
		return true
	}
	if t.refine == nt3dtiles3d.RefineAdd {
		if renderable {
			ts.draw = append(ts.draw, t)
		}
		whole := true
		for _, child := range t.children {
			whole = ts.visit(child, view, limit) && whole
		}
		return whole
	}
	// Replace: the children stand in for the tile only when every one of
	// them that is in view is whole. Until then the tile itself is drawn,
	// and what the children had chosen is set aside.
	mark := len(ts.draw)
	whole := true
	for _, child := range t.children {
		whole = ts.visit(child, view, limit) && whole
	}
	if whole {
		return true
	}
	if renderable {
		ts.draw = append(ts.draw[:mark], t)
		return true
	}
	// A tile with nothing of its own to show cannot hold the view: an
	// ancestor does, or, with none, what has arrived so far is shown.
	return false
}

// dispatch starts loading the tiles the selection wanted, the ones whose
// error is largest on screen first, and stops loads nothing wants any more.
func (ts *Tileset) dispatch() {
	for t := range ts.loading_() {
		if t.used != ts.frame && t.stop != nil {
			t.stop()
			t.stop = nil
		}
	}
	sort.SliceStable(ts.wanted, func(i, j int) bool { return ts.wanted[i].sse > ts.wanted[j].sse })
	kept := ts.wanted[:0]
	for _, t := range ts.wanted {
		if ts.loading >= ts.o.workers() {
			kept = append(kept, t)
			continue
		}
		ts.start(t)
	}
	ts.wanted = kept
}

// evict removes loaded tiles the selection did not touch, the longest
// unused first, until the loaded bytes fit the budget.
func (ts *Tileset) evict() {
	if ts.resident <= ts.o.maxBytes() {
		return
	}
	var idleTiles []*tile
	for t := range ts.loaded {
		if t.used != ts.frame {
			idleTiles = append(idleTiles, t)
		}
	}
	sort.Slice(idleTiles, func(i, j int) bool {
		if idleTiles[i].used != idleTiles[j].used {
			return idleTiles[i].used < idleTiles[j].used
		}
		return idleTiles[i].level > idleTiles[j].level
	})
	for _, t := range idleTiles {
		if ts.resident <= ts.o.maxBytes() {
			break
		}
		ts.unload(t)
		ts.stats.Evictions++
	}
}

// unload removes a loaded tile's nodes; it can be loaded again.
func (ts *Tileset) unload(t *tile) {
	if ts.o.OnTileUnloaded != nil {
		ts.o.OnTileUnloaded(t.record)
	}
	_ = ts.scene.Remove(t.node)
	ts.resident -= t.record.Bytes
	delete(ts.loaded, t)
	t.state, t.node, t.visible, t.record = idle, 0, false, nil
}
