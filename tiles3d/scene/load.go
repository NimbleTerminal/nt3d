// Copyright (c) 2026 Neomantra Corp.

package nt3dtiles3dscene

import (
	"context"
	"errors"
	"fmt"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
	"nimbleterminal.dev/nt3d/mesh/gltf"
	"nimbleterminal.dev/nt3d/tiles3d"
)

// result is what a background load hands back to the scene's goroutine.
type result struct {
	t       *tile
	subtree *nt3dtiles3d.Subtree // a subtree load's
	models  []model
	nested  []nested
	bytes   int64
	err     error
	stopped bool // the load was cancelled: nothing failed
	forTree bool // it was a subtree load
}

// model is one decoded content, and the center its positions are relative
// to, in the tile's space.
type model struct {
	asset  *nt3d.SceneAsset
	center nt3dmath.Vec3d
}

// nested is a content that turned out to be a tileset.
type nested struct {
	tileset *nt3dtiles3d.Tileset
	uri     string
}

// loading_ is the tiles with a load in flight.
func (ts *Tileset) loading_() map[*tile]struct{} {
	if ts.inflight == nil {
		ts.inflight = map[*tile]struct{}{}
	}
	return ts.inflight
}

// start begins loading what t is missing: its subtree if it is an implicit
// tile that has none, and otherwise its contents.
func (ts *Tileset) start(t *tile) {
	ctx, stop := context.WithCancel(ts.ctx)
	t.stop = stop
	ts.loading++
	ts.loading_()[t] = struct{}{}
	forTree := t.implicit != nil && t.subtree == nil
	if forTree {
		t.subtreeState = loadingNow
	} else {
		t.state = loadingNow
	}
	// What the worker needs is copied out here: it must not read the tile,
	// which belongs to the scene's goroutine.
	uris := append([]string(nil), t.uris...)
	base := t.doc.uri
	implicit, coord := t.implicit, t.coord
	go func() {
		r := result{t: t, forTree: forTree}
		if forTree {
			r.subtree, r.err = ts.loadSubtree(ctx, base, implicit, coord)
		} else {
			r.models, r.nested, r.bytes, r.err = ts.loadContents(ctx, uris)
		}
		r.stopped = ctx.Err() != nil
		select {
		case ts.results <- r:
		case <-ts.ctx.Done():
		}
	}()
}

func (ts *Tileset) loadSubtree(ctx context.Context, base string, implicit *nt3dtiles3d.Implicit, root nt3dtiles3d.Coord) (*nt3dtiles3d.Subtree, error) {
	uri, err := nt3dtiles3d.Resolve(base, implicit.SubtreeURI(root))
	if err != nil {
		return nil, err
	}
	data, err := ts.fetch.Fetch(ctx, uri)
	if err != nil {
		return nil, err
	}
	external := func(ref string) ([]byte, error) {
		buffer, err := nt3dtiles3d.Resolve(uri, ref)
		if err != nil {
			return nil, err
		}
		return ts.fetch.Fetch(ctx, buffer)
	}
	return nt3dtiles3d.ParseSubtree(data, implicit, root, external, ts.o.Tiles)
}

// loadContents fetches and decodes a tile's contents. A content that fails
// does not lose the others; the first error is returned beside them.
func (ts *Tileset) loadContents(ctx context.Context, uris []string) (models []model, tilesets []nested, bytes int64, first error) {
	for _, uri := range uris {
		data, err := ts.fetch.Fetch(ctx, uri)
		if err == nil {
			bytes += int64(len(data))
			var m *model
			var n *nested
			if m, n, err = ts.decode(ctx, uri, data); m != nil {
				models = append(models, *m)
			} else if n != nil {
				tilesets = append(tilesets, *n)
			}
		}
		if err != nil && first == nil {
			first = fmt.Errorf("nt3dtiles3dscene: %s: %w", uri, err)
		}
	}
	return models, tilesets, bytes, first
}

func (ts *Tileset) decode(ctx context.Context, uri string, data []byte) (*model, *nested, error) {
	var center nt3dmath.Vec3d
	switch nt3dtiles3d.SniffContent(data) {
	case nt3dtiles3d.ContentTileset:
		parsed, err := nt3dtiles3d.ParseTileset(data, ts.o.Tiles)
		if err != nil {
			return nil, nil, err
		}
		return nil, &nested{tileset: parsed, uri: uri}, nil
	case nt3dtiles3d.ContentB3DM:
		var err error
		if data, center, err = nt3dtiles3d.UnwrapB3DM(data); err != nil {
			return nil, nil, err
		}
	case nt3dtiles3d.ContentGLB, nt3dtiles3d.ContentGLTF:
	default:
		return nil, nil, errors.New("content is not glTF, b3dm, or a tileset")
	}
	var o nt3dmesh.Options
	if ts.o.Mesh != nil {
		o = *ts.o.Mesh
	}
	o.AssetOnly = true
	// A glTF's buffers and images are beside it.
	o.Open = func(name string) ([]byte, error) {
		side, err := nt3dtiles3d.Resolve(uri, name)
		if err != nil {
			return nil, err
		}
		return ts.fetch.Fetch(ctx, side)
	}
	decoded, err := nt3dgltf.Decode(data, &o)
	if err != nil {
		return nil, nil, err
	}
	if decoded.Asset == nil {
		return nil, nil, errors.New("content has nothing to show")
	}
	// The glb's positions are relative to the b3dm's center and to the
	// model's own origin, both in the tile's Z-up space.
	return &model{asset: decoded.Asset, center: center.Add(decoded.Origin)}, nil, nil
}

// apply brings a finished load into the scene. It runs on the scene's
// goroutine.
func (ts *Tileset) apply(r result) {
	t := r.t
	ts.loading--
	delete(ts.loading_(), t)
	if t.stop != nil {
		t.stop()
		t.stop = nil
	}
	if ts.closed {
		return
	}
	fail := func(err error) {
		ts.stats.Failed++
		ts.stats.LastError = err
	}
	if r.forTree {
		switch {
		case r.stopped:
			t.subtreeState = idle
		case r.err != nil:
			t.subtreeState = failedNow
			fail(r.err)
		default:
			ts.adopt(t, r.subtree)
		}
		return
	}
	if r.stopped {
		t.state = idle
		return
	}
	if len(r.models) == 0 && len(r.nested) == 0 {
		t.state = failedNow
		if r.err == nil {
			r.err = errors.New("nt3dtiles3dscene: tile has no content that could be read")
		}
		fail(r.err)
		return
	}
	if r.err != nil {
		fail(r.err)
	}
	for _, n := range r.nested {
		if t.doc.depth+1 > ts.o.maxExternalDepth() {
			fail(fmt.Errorf("nt3dtiles3dscene: %s: tilesets nest deeper than %d", n.uri, ts.o.maxExternalDepth()))
			continue
		}
		// The nested tileset's root continues this tile: in its space, and
		// refining as it does unless it says otherwise.
		ts.expand(t)
		doc := &document{uri: n.uri, credits: n.tileset.Asset.Credits(), metadata: n.tileset.Metadata, groups: n.tileset.Groups, depth: t.doc.depth + 1}
		t.children = append(t.children, ts.explicit(n.tileset.Root, t, doc, t.toECEF, t.refine, t.level+1))
		t.external = true
	}
	t.state = loadedNow
	if len(r.models) == 0 {
		if !t.external {
			// Its only content was a tileset nested too deep.
			t.state = failedNow
		}
		return
	}
	node, parts, splats, err := ts.place(t, r.models)
	if err != nil {
		if len(r.nested) == 0 {
			t.state = failedNow
		}
		fail(fmt.Errorf("nt3dtiles3dscene: %v: %w", t.uris, err))
		return
	}
	t.node, t.visible = node, false
	t.record = ts.describe(t, r.bytes, splats)
	// The record rides on the tile's node and on every node under it, so a
	// draw or a pick of any part of the tile says which tile it is.
	_ = ts.scene.SetNodeData(node, t.record)
	for _, part := range parts {
		_ = ts.scene.SetNodeData(part, t.record)
	}
	ts.loaded[t] = struct{}{}
	ts.resident += r.bytes
	if ts.o.OnTileLoaded != nil {
		ts.o.OnTileLoaded(t.record)
	}
}

// place adds a tile's models to the scene, hidden, each where its tile's
// transform puts it in the scene's frame, and counts their splats.
func (ts *Tileset) place(t *tile, models []model) (node nt3d.NodeID, parts []nt3d.NodeID, splats int, err error) {
	// Every node of a scene needs a name of its own; the tileset's node and
	// a count give one. What a tile is rides on its record, not in its name.
	ts.serial++
	name := fmt.Sprintf("tileset %d tile %d", ts.group, ts.serial)
	if node, err = ts.scene.Add(ts.group, name); err != nil {
		return 0, nil, 0, err
	}
	undo := func(err error) (nt3d.NodeID, []nt3d.NodeID, int, error) {
		_ = ts.scene.Remove(node)
		return 0, nil, 0, err
	}
	if err := ts.scene.SetVisible(node, false); err != nil {
		return undo(err)
	}
	for _, m := range models {
		// Tile space to Earth-centered meters, with the model's center
		// added while everything is still float64; then into the scene's
		// frame, split so that only the turn reaches float32.
		toECEF := t.toECEF.Mul(nt3dmath.TRSd(m.center, nt3dmath.Quatd{W: 1}, nt3dmath.Vec3d{X: 1, Y: 1, Z: 1}))
		origin, local, err := ts.o.Frame.Place(toECEF)
		if err != nil {
			return undo(err)
		}
		content, err := ts.scene.Add(node, fmt.Sprintf("%s content %d", name, len(parts)))
		if err == nil {
			err = ts.scene.SetTransform(content, local)
		}
		if err == nil {
			err = ts.scene.SetNodeOrigin(content, origin)
		}
		if err != nil {
			return undo(err)
		}
		instance, err := m.asset.Instantiate(ts.scene, content, fmt.Sprintf("%s model %d", name, len(parts)))
		if err != nil {
			return undo(err)
		}
		parts = append(append(parts, content), instance.Nodes...)
		for _, n := range m.asset.Nodes {
			if n.Geometry != nil && n.Geometry.PointStyle.Shape == nt3d.GaussianPoint {
				splats += len(n.Geometry.Points)
			}
		}
	}
	return node, parts, splats, nil
}

// describe builds the record a tile's node carries.
func (ts *Tileset) describe(t *tile, bytes int64, splats int) *Tile {
	record := &Tile{
		URIs:            append([]string(nil), t.uris...),
		Level:           t.level,
		Implicit:        t.implicit != nil,
		Coord:           t.coord,
		GeometricError:  t.geometricError,
		Credits:         append([]string(nil), t.doc.credits...),
		TilesetMetadata: t.doc.metadata,
		Bytes:           bytes,
		Splats:          splats,
		Node:            t.node,
	}
	if t.src != nil {
		record.Metadata = t.src.Metadata
	}
	for _, c := range t.content {
		record.Contents = append(record.Contents, c.Metadata)
		var group *nt3dtiles3d.Entity
		if c.Group >= 0 && c.Group < len(t.doc.groups) {
			group = &t.doc.groups[c.Group]
		}
		record.Groups = append(record.Groups, group)
	}
	if t.subtree != nil {
		record.Subtree = t.subtree.Metadata
	}
	return record
}
