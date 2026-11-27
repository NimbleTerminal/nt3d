// Copyright (c) 2026 Neomantra Corp.

// Package nt3dtiles3d reads OGC 3D Tiles 1.1: the tileset document, with its
// tree of tiles, bounding volumes, refinement, contents, and metadata;
// implicit tiling, with its subtree files and availability; and the legacy
// b3dm wrapper around a tile's glTF.
//
// It reads and describes; it draws nothing and fetches nothing on its own.
// [ParseTileset] takes the bytes of a tileset.json and returns a [Tileset] of
// plain data. A [Fetcher] is how bytes are asked for by URI, with [FS] for a
// directory and [HTTP] for a server, and [Resolve] makes a tile's relative
// URI absolute. Package nt3d/tiles3d/scene streams a tileset into a scene.
//
// Positions are Earth-centered meters in float64 (see nt3d/geo): a tile's
// transform, multiplied by its ancestors', carries its content there, and
// [Volume.Box] gives its bounds there.
//
// Every document is untrusted. Sizes, tile counts, and nesting are limited by
// [Options], a document over a limit is an error that wraps [ErrTooLarge],
// and text meant for display is cut to printable characters.
//
// The package imports nt3d/math, nt3d/geo, and the standard library.
package nt3dtiles3d

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"nimbleterminal.dev/nt3d/math"
)

// ErrTooLarge is wrapped by the error of a document that is over a limit of
// Options: more bytes, more tiles, or deeper nesting than allowed.
var ErrTooLarge = errors.New("nt3dtiles3d: document is over a limit")

// Options limits what a parse accepts. A zero field means its default.
type Options struct {
	// MaxBytes is the most bytes a document may have: 64 MiB by default.
	MaxBytes int64
	// MaxTiles is the most tiles an explicit tree may hold: 1,000,000 by default.
	MaxTiles int
	// MaxDepth is the most levels an explicit tree may nest: 256 by default.
	MaxDepth int
}

func (o *Options) maxBytes() int64 {
	if o == nil || o.MaxBytes <= 0 {
		return 64 << 20
	}
	return o.MaxBytes
}

func (o *Options) maxTiles() int {
	if o == nil || o.MaxTiles <= 0 {
		return 1_000_000
	}
	return o.MaxTiles
}

func (o *Options) maxDepth() int {
	if o == nil || o.MaxDepth <= 0 {
		return 256
	}
	return o.MaxDepth
}

// Refine is how a tile's children relate to it once they are drawn.
type Refine uint8

const (
	// RefineInherit is a tile that does not say: it refines as its parent
	// does, and a root that does not say replaces.
	RefineInherit Refine = iota
	// RefineReplace draws the children in place of the tile.
	RefineReplace
	// RefineAdd draws the children as well as the tile.
	RefineAdd
)

func (r Refine) String() string {
	switch r {
	case RefineReplace:
		return "REPLACE"
	case RefineAdd:
		return "ADD"
	}
	return "inherit"
}

// Tileset is a parsed tileset.json.
type Tileset struct {
	Asset Asset
	// GeometricError is the error, in meters, of drawing nothing at all.
	GeometricError float64
	Root           *Tile
	// Schema describes the metadata classes, when the tileset carries it
	// inline; SchemaURI names it when it is a file of its own, for ParseSchema.
	Schema    *Schema
	SchemaURI string
	// Metadata is the tileset's own metadata, and Groups the groups a
	// content may belong to.
	Metadata *Entity
	Groups   []Entity
	// ExtensionsUsed and ExtensionsRequired are as the document lists them.
	// ParseTileset refuses a document that requires an extension this
	// package does not read.
	ExtensionsUsed, ExtensionsRequired []string
}

// Asset is a tileset's header.
type Asset struct {
	// Version is the 3D Tiles version, "1.0" or "1.1".
	Version string
	// TilesetVersion is the application's version of this tileset.
	TilesetVersion string
	// Copyright and Attribution are who must be credited when the tileset is
	// shown, as printable text: the asset's copyright, and its
	// extras.attribution, a string or a list of them.
	Copyright   string
	Attribution []string
	// Extras is the asset's extras object as written, or nil.
	Extras json.RawMessage
}

// Credits returns the lines to show for a tileset on screen: its copyright
// and its attributions, without repeats.
func (a Asset) Credits() []string {
	var out []string
	add := func(s string) {
		for _, have := range out {
			if have == s {
				return
			}
		}
		if s != "" {
			out = append(out, s)
		}
	}
	add(a.Copyright)
	for _, s := range a.Attribution {
		add(s)
	}
	return out
}

// Tile is one tile of a tileset.
type Tile struct {
	// Transform carries the tile, its content, and its children into its
	// parent's space; the identity when the tile has none. The product down
	// from the root carries them into Earth-centered meters.
	Transform nt3dmath.Mat4d
	// Bounds holds the tile's content and its children's.
	Bounds Volume
	// GeometricError is the error, in meters, of drawing this tile and not
	// its children.
	GeometricError float64
	Refine         Refine
	// Contents are what the tile draws: none for a tile that only groups.
	Contents []Content
	Children []*Tile
	// Implicit, when set, makes this tile the root of an implicit tree: its
	// children come from subtree files, and its one content's URI is a
	// template.
	Implicit *Implicit
	// Metadata is the tile's metadata, or nil.
	Metadata *Entity
}

// Content is one thing a tile draws.
type Content struct {
	// URI names the content, relative to the tileset's own URI: a glTF or
	// glb, a legacy b3dm, or another tileset. For the root of an implicit
	// tree it is a template (see Implicit.URI).
	URI string
	// Bounds, when HasBounds, holds this content more tightly than the tile's.
	Bounds    Volume
	HasBounds bool
	// Group indexes Tileset.Groups, or is -1.
	Group int
	// Metadata is the content's metadata, or nil.
	Metadata *Entity
}

// The extensions ParseTileset reads, or that change nothing it reads.
var supportedExtensions = map[string]bool{
	"3DTILES_content_gltf":      true, // glTF as tile content: core in 1.1.
	"3DTILES_implicit_tiling":   true, // read as 1.1's implicitTiling.
	"3DTILES_multiple_contents": true, // read as 1.1's contents.
}

type rawTileset struct {
	Asset *struct {
		Version        string          `json:"version"`
		TilesetVersion string          `json:"tilesetVersion"`
		Copyright      string          `json:"copyright"`
		Extras         json.RawMessage `json:"extras"`
	} `json:"asset"`
	GeometricError     *float64     `json:"geometricError"`
	Root               *rawTile     `json:"root"`
	Schema             *rawSchema   `json:"schema"`
	SchemaURI          string       `json:"schemaUri"`
	Metadata           *rawEntity   `json:"metadata"`
	Groups             []rawEntity  `json:"groups"`
	ExtensionsUsed     []string     `json:"extensionsUsed"`
	ExtensionsRequired []string     `json:"extensionsRequired"`
	Extensions         rawExtension `json:"extensions"`
}

type rawExtension map[string]json.RawMessage

type rawTile struct {
	Transform      []float64    `json:"transform"`
	BoundingVolume *rawVolume   `json:"boundingVolume"`
	GeometricError *float64     `json:"geometricError"`
	Refine         string       `json:"refine"`
	Content        *rawContent  `json:"content"`
	Contents       []rawContent `json:"contents"`
	Children       []*rawTile   `json:"children"`
	Implicit       *rawImplicit `json:"implicitTiling"`
	Metadata       *rawEntity   `json:"metadata"`
	Extensions     rawExtension `json:"extensions"`
}

type rawContent struct {
	URI            string     `json:"uri"`
	URL            string     `json:"url"` // 3D Tiles 1.0 before it was renamed.
	BoundingVolume *rawVolume `json:"boundingVolume"`
	Group          *int       `json:"group"`
	Metadata       *rawEntity `json:"metadata"`
}

// ParseTileset reads a tileset.json. The tileset returned is plain data that
// shares nothing with data.
//
// What the specification requires is required: an asset of version 1.0 or
// 1.1, a root, a geometric error and a bounding volume on every tile, finite
// numbers, and an affine transform. A document that needs an extension this
// package does not read is refused.
func ParseTileset(data []byte, o *Options) (*Tileset, error) {
	if int64(len(data)) > o.maxBytes() {
		return nil, fmt.Errorf("%w: tileset of %d bytes", ErrTooLarge, len(data))
	}
	var raw rawTileset
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("nt3dtiles3d: tileset: %w", err)
	}
	if raw.Asset == nil {
		return nil, errors.New("nt3dtiles3d: tileset has no asset")
	}
	if raw.Asset.Version != "1.0" && raw.Asset.Version != "1.1" {
		return nil, fmt.Errorf("nt3dtiles3d: unsupported 3D Tiles version %q", printable(raw.Asset.Version, 16))
	}
	for _, name := range raw.ExtensionsRequired {
		if !supportedExtensions[name] {
			return nil, fmt.Errorf("nt3dtiles3d: tileset requires unsupported extension %q", printable(name, 64))
		}
	}
	if raw.Root == nil {
		return nil, errors.New("nt3dtiles3d: tileset has no root")
	}
	if raw.GeometricError == nil || !validError(*raw.GeometricError) {
		return nil, errors.New("nt3dtiles3d: tileset geometric error must be a finite number, zero or more")
	}
	t := &Tileset{
		Asset: Asset{
			Version:        raw.Asset.Version,
			TilesetVersion: printable(raw.Asset.TilesetVersion, 64),
			Copyright:      printable(raw.Asset.Copyright, maxCreditLength),
			Attribution:    attribution(raw.Asset.Extras),
			Extras:         append(json.RawMessage(nil), raw.Asset.Extras...),
		},
		GeometricError:     *raw.GeometricError,
		SchemaURI:          raw.SchemaURI,
		ExtensionsUsed:     printableList(raw.ExtensionsUsed),
		ExtensionsRequired: printableList(raw.ExtensionsRequired),
	}
	var err error
	if raw.Schema != nil {
		if t.Schema, err = raw.Schema.schema(); err != nil {
			return nil, err
		}
	}
	t.Metadata = raw.Metadata.entity()
	for i := range raw.Groups {
		t.Groups = append(t.Groups, *raw.Groups[i].entity())
	}
	count := 0
	if t.Root, err = raw.Root.tile(o, 0, &count, len(t.Groups)); err != nil {
		return nil, err
	}
	return t, nil
}

func (r *rawTile) tile(o *Options, depth int, count *int, groups int) (*Tile, error) {
	if depth >= o.maxDepth() {
		return nil, fmt.Errorf("%w: tiles nest deeper than %d", ErrTooLarge, o.maxDepth())
	}
	if *count++; *count > o.maxTiles() {
		return nil, fmt.Errorf("%w: more than %d tiles", ErrTooLarge, o.maxTiles())
	}
	t := &Tile{Transform: nt3dmath.IdentityD()}
	if r.Transform != nil {
		if len(r.Transform) != 16 {
			return nil, fmt.Errorf("nt3dtiles3d: tile transform has %d numbers, want 16", len(r.Transform))
		}
		copy(t.Transform[:], r.Transform)
		if _, ok := t.Transform.InverseAffine(); !ok {
			return nil, errors.New("nt3dtiles3d: tile transform must be finite, affine, and invertible")
		}
	}
	if r.BoundingVolume == nil {
		return nil, errors.New("nt3dtiles3d: tile has no bounding volume")
	}
	var err error
	if t.Bounds, err = r.BoundingVolume.volume(); err != nil {
		return nil, err
	}
	if r.GeometricError == nil || !validError(*r.GeometricError) {
		return nil, errors.New("nt3dtiles3d: tile geometric error must be a finite number, zero or more")
	}
	t.GeometricError = *r.GeometricError
	switch strings.ToUpper(r.Refine) {
	case "":
	case "REPLACE":
		t.Refine = RefineReplace
	case "ADD":
		t.Refine = RefineAdd
	default:
		return nil, fmt.Errorf("nt3dtiles3d: unknown refine %q", printable(r.Refine, 16))
	}
	contents := r.Contents
	if r.Content != nil {
		if len(contents) > 0 {
			return nil, errors.New("nt3dtiles3d: tile has both content and contents")
		}
		contents = []rawContent{*r.Content}
	}
	if len(contents) == 0 {
		// The 1.0 extension that 1.1's contents came from.
		if ext, ok := r.Extensions["3DTILES_multiple_contents"]; ok {
			var multiple struct {
				Content  []rawContent `json:"content"`
				Contents []rawContent `json:"contents"`
			}
			if err := json.Unmarshal(ext, &multiple); err != nil {
				return nil, fmt.Errorf("nt3dtiles3d: 3DTILES_multiple_contents: %w", err)
			}
			contents = append(multiple.Contents, multiple.Content...)
		}
	}
	for _, c := range contents {
		content := Content{URI: c.URI, Group: -1, Metadata: c.Metadata.entity()}
		if content.URI == "" {
			content.URI = c.URL
		}
		if content.URI == "" {
			return nil, errors.New("nt3dtiles3d: tile content has no uri")
		}
		if len(content.URI) > maxURILength {
			return nil, fmt.Errorf("%w: content uri of %d bytes", ErrTooLarge, len(content.URI))
		}
		if c.BoundingVolume != nil {
			if content.Bounds, err = c.BoundingVolume.volume(); err != nil {
				return nil, err
			}
			content.HasBounds = true
		}
		if c.Group != nil {
			if *c.Group < 0 || *c.Group >= groups {
				return nil, fmt.Errorf("nt3dtiles3d: content names group %d of %d", *c.Group, groups)
			}
			content.Group = *c.Group
		}
		t.Contents = append(t.Contents, content)
	}
	t.Metadata = r.Metadata.entity()
	implicit := r.Implicit
	if implicit == nil {
		if ext, ok := r.Extensions["3DTILES_implicit_tiling"]; ok {
			implicit = &rawImplicit{}
			if err := json.Unmarshal(ext, implicit); err != nil {
				return nil, fmt.Errorf("nt3dtiles3d: 3DTILES_implicit_tiling: %w", err)
			}
		}
	}
	if implicit != nil {
		if t.Implicit, err = implicit.implicit(t); err != nil {
			return nil, err
		}
		if len(r.Children) > 0 {
			return nil, errors.New("nt3dtiles3d: an implicit root has children of its own")
		}
	}
	for _, child := range r.Children {
		if child == nil {
			return nil, errors.New("nt3dtiles3d: tile has a null child")
		}
		c, err := child.tile(o, depth+1, count, groups)
		if err != nil {
			return nil, err
		}
		t.Children = append(t.Children, c)
	}
	return t, nil
}

func validError(v float64) bool { return nt3dmath.IsFinite(v) && v >= 0 }

const (
	maxURILength    = 4096
	maxCreditLength = 512
	maxCredits      = 32
)

// attribution reads extras.attribution: a string, or a list of strings.
func attribution(extras json.RawMessage) []string {
	var holder struct {
		Attribution json.RawMessage `json:"attribution"`
	}
	if len(extras) == 0 || json.Unmarshal(extras, &holder) != nil || len(holder.Attribution) == 0 {
		return nil
	}
	var one string
	var many []string
	if json.Unmarshal(holder.Attribution, &one) == nil {
		many = []string{one}
	} else if json.Unmarshal(holder.Attribution, &many) != nil {
		return nil
	}
	var out []string
	for _, s := range many {
		if s = printable(s, maxCreditLength); s != "" && len(out) < maxCredits {
			out = append(out, s)
		}
	}
	return out
}

// printable returns s as text safe to show: one line of printable
// characters, of at most limit bytes, with what was cut marked.
func printable(s string, limit int) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if r == '�' || r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == ' ' || r == ' ' || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			r = ' '
		}
		if r == ' ' {
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		if b.Len()+len(string(r)) > limit {
			return b.String() + "…"
		}
		b.WriteRune(r)
	}
	return b.String()
}

func printableList(in []string) []string {
	var out []string
	for _, s := range in {
		if len(out) == 256 {
			break
		}
		out = append(out, printable(s, 64))
	}
	return out
}
