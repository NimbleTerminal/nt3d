// Copyright (c) 2026 Neomantra Corp.

package nt3dtiles3d

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"nimbleterminal.dev/nt3d/math"
)

// ContentKind is what a tile's content turned out to be.
type ContentKind uint8

const (
	// ContentUnknown is nothing this package knows: among others the legacy
	// i3dm, pnts, and cmpt formats, which are not read.
	ContentUnknown ContentKind = iota
	// ContentGLB is binary glTF.
	ContentGLB
	// ContentGLTF is glTF as JSON.
	ContentGLTF
	// ContentB3DM is the legacy batched model: a header around a glb, for
	// UnwrapB3DM.
	ContentB3DM
	// ContentTileset is another tileset, whose root continues this tile.
	ContentTileset
)

// SniffContent says what data is, from its first bytes and, for JSON, from
// which of a tileset's and a glTF's members it has.
func SniffContent(data []byte) ContentKind {
	switch {
	case bytes.HasPrefix(data, []byte("glTF")):
		return ContentGLB
	case bytes.HasPrefix(data, []byte("b3dm")):
		return ContentB3DM
	}
	trimmed := bytes.TrimLeft(data, " \t\r\n\xef\xbb\xbf")
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return ContentUnknown
	}
	var probe struct {
		Root           json.RawMessage `json:"root"`
		GeometricError json.RawMessage `json:"geometricError"`
		Asset          json.RawMessage `json:"asset"`
	}
	if json.Unmarshal(trimmed, &probe) != nil || probe.Asset == nil {
		return ContentUnknown
	}
	if probe.Root != nil && probe.GeometricError != nil {
		return ContentTileset
	}
	return ContentGLTF
}

// UnwrapB3DM returns the glb inside a legacy batched 3D model, sharing data's
// bytes, and the model's RTC_CENTER when it has one: a center, in the tile's
// space, that the glb's positions are relative to. Add it to the tile's
// transform in float64 and the positions stay small enough for float32.
//
// The feature and batch tables, which carried per-building properties before
// glTF metadata did, are otherwise skipped.
func UnwrapB3DM(data []byte) (glb []byte, center nt3dmath.Vec3d, err error) {
	const header = 28
	if len(data) < header || !bytes.HasPrefix(data, []byte("b3dm")) {
		return nil, center, errors.New("nt3dtiles3d: not a b3dm")
	}
	if version := binary.LittleEndian.Uint32(data[4:]); version != 1 {
		return nil, center, fmt.Errorf("nt3dtiles3d: unsupported b3dm version %d", version)
	}
	total := uint64(binary.LittleEndian.Uint32(data[8:]))
	if total > uint64(len(data)) || total < header {
		return nil, center, errors.New("nt3dtiles3d: b3dm length runs past the file")
	}
	var lengths [4]uint64 // feature table JSON and binary, batch table JSON and binary
	at := uint64(header)
	var parts [4][]byte
	for k := range lengths {
		lengths[k] = uint64(binary.LittleEndian.Uint32(data[12+4*k:]))
		if lengths[k] > total-at {
			return nil, center, errors.New("nt3dtiles3d: b3dm tables run past the file")
		}
		parts[k] = data[at : at+lengths[k]]
		at += lengths[k]
	}
	glb = data[at:total]
	if !bytes.HasPrefix(glb, []byte("glTF")) {
		return nil, center, errors.New("nt3dtiles3d: b3dm holds no glb")
	}
	if len(bytes.TrimSpace(parts[0])) > 0 {
		var table struct {
			RTC json.RawMessage `json:"RTC_CENTER"`
		}
		if err := json.Unmarshal(parts[0], &table); err != nil {
			return nil, center, fmt.Errorf("nt3dtiles3d: b3dm feature table: %w", err)
		}
		if len(table.RTC) > 0 {
			var values []float64
			var ref struct {
				ByteOffset *uint64 `json:"byteOffset"`
			}
			switch {
			case json.Unmarshal(table.RTC, &values) == nil:
				if len(values) != 3 {
					return nil, center, errors.New("nt3dtiles3d: b3dm RTC_CENTER is three numbers")
				}
			case json.Unmarshal(table.RTC, &ref) == nil && ref.ByteOffset != nil:
				if *ref.ByteOffset > uint64(len(parts[1])) || uint64(len(parts[1]))-*ref.ByteOffset < 12 {
					return nil, center, errors.New("nt3dtiles3d: b3dm RTC_CENTER runs past its table")
				}
				for k := range 3 {
					values = append(values, float64(math.Float32frombits(binary.LittleEndian.Uint32(parts[1][*ref.ByteOffset+uint64(4*k):]))))
				}
			default:
				return nil, center, errors.New("nt3dtiles3d: b3dm RTC_CENTER is three numbers")
			}
			center = nt3dmath.Vec3d{X: values[0], Y: values[1], Z: values[2]}
			if !center.IsFinite() {
				return nil, center, errors.New("nt3dtiles3d: b3dm RTC_CENTER is not finite")
			}
		}
	}
	return glb, center, nil
}
