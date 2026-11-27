// Copyright (c) 2026 Neomantra Corp.

package nt3dtiles3d

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
)

// Subtree is one subtree file of an implicit tree: which of its tiles exist,
// which of those have content, and which subtrees continue below it.
type Subtree struct {
	// Root is the tile the subtree starts at.
	Root Coord
	// Metadata is the subtree's own metadata, or nil.
	Metadata *Entity

	scheme   Scheme
	levels   int
	tiles    availability
	contents []availability
	children availability
}

// availability is a bit for each of a run of tiles, or one answer for all.
type availability struct {
	constant, all bool
	bits          []byte
}

func (a availability) at(index int64) bool {
	if a.constant {
		return a.all
	}
	return index>>3 < int64(len(a.bits)) && a.bits[index>>3]>>(index&7)&1 == 1
}

type rawAvailability struct {
	Bitstream  *int `json:"bitstream"`
	BufferView *int `json:"bufferView"` // the 1.0 extension's name for bitstream.
	Constant   *int `json:"constant"`
}

type rawSubtree struct {
	Buffers []struct {
		URI        string `json:"uri"`
		ByteLength int64  `json:"byteLength"`
	} `json:"buffers"`
	BufferViews []struct {
		Buffer     int   `json:"buffer"`
		ByteOffset int64 `json:"byteOffset"`
		ByteLength int64 `json:"byteLength"`
	} `json:"bufferViews"`
	TileAvailability         *rawAvailability `json:"tileAvailability"`
	ContentAvailability      json.RawMessage  `json:"contentAvailability"`
	ChildSubtreeAvailability *rawAvailability `json:"childSubtreeAvailability"`
	SubtreeMetadata          *rawEntity       `json:"subtreeMetadata"`
}

// subtreeMagic is "subt", the first four bytes of a binary subtree file.
var subtreeMagic = []byte("subt")

// ParseSubtree reads a subtree file of the implicit tree i, whose root tile
// is root: either the binary form, a header with JSON and one buffer after
// it, or the JSON alone. A buffer the file names by URI is asked of external,
// which is given the URI as written, relative to the subtree file; with a
// nil external such a file is an error.
func ParseSubtree(data []byte, i *Implicit, root Coord, external func(uri string) ([]byte, error), o *Options) (*Subtree, error) {
	if int64(len(data)) > o.maxBytes() {
		return nil, fmt.Errorf("%w: subtree of %d bytes", ErrTooLarge, len(data))
	}
	if !i.Valid(root) || root.Level%i.SubtreeLevels != 0 {
		return nil, errors.New("nt3dtiles3d: no subtree starts at that tile")
	}
	document, chunk := data, []byte(nil)
	if bytes.HasPrefix(data, subtreeMagic) {
		if len(data) < 24 {
			return nil, errors.New("nt3dtiles3d: subtree header is cut short")
		}
		if version := binary.LittleEndian.Uint32(data[4:]); version != 1 {
			return nil, fmt.Errorf("nt3dtiles3d: unsupported subtree version %d", version)
		}
		jsonLength, binaryLength := binary.LittleEndian.Uint64(data[8:]), binary.LittleEndian.Uint64(data[16:])
		rest := uint64(len(data) - 24)
		if jsonLength > rest || binaryLength > rest-jsonLength {
			return nil, errors.New("nt3dtiles3d: subtree chunks run past the file")
		}
		document = data[24 : 24+jsonLength]
		chunk = data[24+jsonLength : 24+jsonLength+binaryLength]
	}
	var raw rawSubtree
	if err := json.Unmarshal(document, &raw); err != nil {
		return nil, fmt.Errorf("nt3dtiles3d: subtree: %w", err)
	}
	// Buffers are read when a view first asks for one, so a file that names
	// buffers it never uses costs nothing.
	buffers := make([][]byte, len(raw.Buffers))
	loaded := make([]bool, len(raw.Buffers))
	budget := o.maxBytes() - int64(len(data))
	view := func(index int, bits int64) ([]byte, error) {
		if index < 0 || index >= len(raw.BufferViews) {
			return nil, fmt.Errorf("nt3dtiles3d: subtree names buffer view %d of %d", index, len(raw.BufferViews))
		}
		v := raw.BufferViews[index]
		if v.Buffer < 0 || v.Buffer >= len(raw.Buffers) {
			return nil, fmt.Errorf("nt3dtiles3d: subtree names buffer %d of %d", v.Buffer, len(raw.Buffers))
		}
		if !loaded[v.Buffer] {
			loaded[v.Buffer] = true
			if uri := raw.Buffers[v.Buffer].URI; uri == "" {
				buffers[v.Buffer] = chunk
			} else {
				if external == nil {
					return nil, errors.New("nt3dtiles3d: subtree keeps its availability in another file")
				}
				if len(uri) > maxURILength {
					return nil, fmt.Errorf("%w: buffer uri of %d bytes", ErrTooLarge, len(uri))
				}
				b, err := external(uri)
				if err != nil {
					return nil, fmt.Errorf("nt3dtiles3d: subtree buffer: %w", err)
				}
				if budget -= int64(len(b)); budget < 0 {
					return nil, fmt.Errorf("%w: subtree buffers", ErrTooLarge)
				}
				buffers[v.Buffer] = b
			}
		}
		b := buffers[v.Buffer]
		if v.ByteOffset < 0 || v.ByteLength < 0 || v.ByteOffset > int64(len(b)) || v.ByteLength > int64(len(b))-v.ByteOffset {
			return nil, errors.New("nt3dtiles3d: subtree buffer view runs past its buffer")
		}
		if v.ByteLength < (bits+7)/8 {
			return nil, fmt.Errorf("nt3dtiles3d: subtree availability of %d bits has %d bytes", bits, v.ByteLength)
		}
		return append([]byte(nil), b[v.ByteOffset:v.ByteOffset+(bits+7)/8]...), nil
	}
	read := func(r *rawAvailability, bits int64, what string) (availability, error) {
		if r == nil {
			return availability{}, fmt.Errorf("nt3dtiles3d: subtree has no %s availability", what)
		}
		if r.Constant != nil {
			if *r.Constant != 0 && *r.Constant != 1 {
				return availability{}, fmt.Errorf("nt3dtiles3d: subtree %s availability constant must be 0 or 1", what)
			}
			return availability{constant: true, all: *r.Constant == 1}, nil
		}
		index := r.Bitstream
		if index == nil {
			index = r.BufferView
		}
		if index == nil {
			return availability{}, fmt.Errorf("nt3dtiles3d: subtree %s availability has neither a bitstream nor a constant", what)
		}
		b, err := view(*index, bits)
		return availability{bits: b}, err
	}
	s := &Subtree{Root: root, scheme: i.Scheme, levels: i.SubtreeLevels, Metadata: raw.SubtreeMetadata.entity()}
	tiles, below := i.Scheme.levelOffset(i.SubtreeLevels), i.Scheme.levelCount(i.SubtreeLevels)
	var err error
	if s.tiles, err = read(raw.TileAvailability, tiles, "tile"); err != nil {
		return nil, err
	}
	if s.children, err = read(raw.ChildSubtreeAvailability, below, "child subtree"); err != nil {
		return nil, err
	}
	// One availability for each content of a tile: a list in 1.1, and a
	// single object where the 1.0 extension allowed one content.
	var contents []rawAvailability
	if len(raw.ContentAvailability) > 0 {
		if json.Unmarshal(raw.ContentAvailability, &contents) != nil {
			var one rawAvailability
			if err := json.Unmarshal(raw.ContentAvailability, &one); err != nil {
				return nil, fmt.Errorf("nt3dtiles3d: subtree content availability: %w", err)
			}
			contents = []rawAvailability{one}
		}
	}
	if len(contents) > len(i.contents) {
		return nil, fmt.Errorf("nt3dtiles3d: subtree has availability for %d contents of %d", len(contents), len(i.contents))
	}
	for k := range contents {
		a, err := read(&contents[k], tiles, "content")
		if err != nil {
			return nil, err
		}
		s.contents = append(s.contents, a)
	}
	return s, nil
}

// index returns c's place in the subtree's run of tiles, and whether c is in
// the subtree at all.
func (s *Subtree) index(c Coord) (int64, bool) {
	level := c.Level - s.Root.Level
	if level < 0 || level >= s.levels {
		return 0, false
	}
	x, y, z := c.X-s.Root.X<<level, c.Y-s.Root.Y<<level, c.Z-s.Root.Z<<level
	side := 1 << level
	if x < 0 || x >= side || y < 0 || y >= side || z < 0 || z >= side || (s.scheme == Quadtree && z != 0) {
		return 0, false
	}
	return s.scheme.levelOffset(level) + s.scheme.morton(x, y, z, level), true
}

// Contains reports whether the tile c is one this subtree speaks for.
func (s *Subtree) Contains(c Coord) bool {
	_, ok := s.index(c)
	return ok
}

// TileAvailable reports whether the tile c exists. A tile outside the
// subtree does not.
func (s *Subtree) TileAvailable(c Coord) bool {
	index, ok := s.index(c)
	return ok && s.tiles.at(index)
}

// ContentAvailable reports whether the tile c exists and has its content
// number k, counting the implicit root's contents from 0.
func (s *Subtree) ContentAvailable(c Coord, k int) bool {
	index, ok := s.index(c)
	return ok && k >= 0 && k < len(s.contents) && s.tiles.at(index) && s.contents[k].at(index)
}

// ChildSubtreeAvailable reports whether a subtree file exists whose root is
// c, a tile one level below this subtree's last.
func (s *Subtree) ChildSubtreeAvailable(c Coord) bool {
	level := c.Level - s.Root.Level
	if level != s.levels {
		return false
	}
	x, y, z := c.X-s.Root.X<<level, c.Y-s.Root.Y<<level, c.Z-s.Root.Z<<level
	side := 1 << level
	if x < 0 || x >= side || y < 0 || y >= side || z < 0 || z >= side || (s.scheme == Quadtree && z != 0) {
		return false
	}
	return s.children.at(s.scheme.morton(x, y, z, level))
}
