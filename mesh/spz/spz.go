// Copyright (c) 2026 Neomantra Corp.

// Package nt3dspz reads Niantic's SPZ, the compressed 3D Gaussian splat
// format, as nt3d Gaussian geometry: the same points, axes, colors, and
// spherical harmonics the PLY decoder makes, about ten times smaller on
// disk. Importing it registers the format with nt3dmesh, as
// nt3dmesh/formats does:
//
//	import _ "nimbleterminal.dev/nt3d/mesh/spz"
//
// Versions 2 and 3 (one gzip stream) and 4 (zstd streams behind an NGSP
// header) decode; version 1, never released, and spherical harmonics of
// degree 4 are refused. SPZ stores right-up-back (Y-up) coordinates, so the
// decoder turns them to Z-up itself and ignores Options.Up. The extensions
// block some files carry (a named coordinate system, quantization notes) is
// skipped, and the antialiased flag is ignored. A cloud larger than one
// geometry holds is split as nt3dmesh.PointPieces splits it.
package nt3dspz

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/klauspost/compress/zstd"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
	"nimbleterminal.dev/nt3d/sh"
)

// SPZ is the format's name, as nt3dmesh.FormatOf reports it.
const SPZ nt3dmesh.Format = "spz"

// magic is "NGSP", little-endian, at the start of a file's content.
const magic = 0x5053474e

func init() { nt3dmesh.RegisterFormatSniff(SPZ, []string{".spz"}, sniff, Decode) }

// sniff recognizes a raw NGSP file, or a gzip stream whose content starts
// with NGSP.
func sniff(data []byte) bool {
	if len(data) >= 4 && binary.LittleEndian.Uint32(data) == magic {
		return true
	}
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		return false
	}
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return false
	}
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return false
	}
	return binary.LittleEndian.Uint32(head[:]) == magic
}

// header is the 16 bytes every version starts with; version 4 follows it
// with a stream count, a table offset, and twelve reserved bytes.
type header struct {
	version, points     uint32
	degree, fraction    uint8
	flags, streams      uint8
	table               uint32
	rotation, dimension int // Bytes per rotation; SH coefficients per splat beyond DC.
}

func readHeader(raw []byte) (header, error) {
	if len(raw) < 16 || binary.LittleEndian.Uint32(raw) != magic {
		return header{}, errors.New("nt3dmesh: not an SPZ file")
	}
	h := header{version: binary.LittleEndian.Uint32(raw[4:]), points: binary.LittleEndian.Uint32(raw[8:]), degree: raw[12], fraction: raw[13], flags: raw[14], streams: raw[15]}
	switch {
	case h.version < 2 || h.version > 4:
		return header{}, fmt.Errorf("nt3dmesh: SPZ version %d is not supported", h.version)
	case h.degree > 3:
		return header{}, fmt.Errorf("nt3dmesh: SPZ spherical harmonics of degree %d are not supported", h.degree)
	case h.points == 0:
		return header{}, errors.New("nt3dmesh: SPZ has no splats")
	case h.fraction > 24:
		return header{}, fmt.Errorf("nt3dmesh: SPZ has %d fractional bits", h.fraction)
	}
	h.rotation = 3
	if h.version >= 3 {
		h.rotation = 4
	}
	h.dimension = nt3dsh.Count(int(h.degree)) - 1
	return h, nil
}

// sizes are the bytes of each array for the header's splats: positions,
// alphas, colors, scales, rotations, SH.
func (h header) sizes() [6]uint64 {
	n := uint64(h.points)
	return [6]uint64{9 * n, n, 3 * n, 3 * n, uint64(h.rotation) * n, 3 * uint64(h.dimension) * n}
}

// Decode reads an SPZ file. Its byte budget is o.MaxBytes (the decompressed
// size), nt3dmesh.DefaultMaxBytes when zero; o.Up is ignored.
func Decode(data []byte, o *nt3dmesh.Options) (*nt3dmesh.Model, error) {
	budget := int64(nt3dmesh.DefaultMaxBytes)
	if o != nil && o.MaxBytes > 0 {
		budget = o.MaxBytes
	}
	var h header
	var arrays [6][]byte
	var err error
	switch {
	case len(data) >= 4 && binary.LittleEndian.Uint32(data) == magic:
		h, arrays, err = readStreams(data, budget)
	case len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b:
		h, arrays, err = readGzip(data, budget)
	default:
		err = errors.New("nt3dmesh: not an SPZ file")
	}
	if err != nil {
		return nil, err
	}
	g, err := unpack(h, arrays)
	if err != nil {
		return nil, err
	}
	m := &nt3dmesh.Model{Format: SPZ, Objects: nt3dmesh.PointPieces(nt3dmesh.Object{Geometry: g})}
	for _, piece := range m.Objects {
		if err := piece.Geometry.Validate(); err != nil {
			return nil, err
		}
	}
	m.OrientYUp()
	return m, nil
}

// readGzip reads a version 2 or 3 file: the header and arrays in one gzip
// stream, read no further than the budget.
func readGzip(data []byte, budget int64) (header, [6][]byte, error) {
	var arrays [6][]byte
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return header{}, arrays, fmt.Errorf("nt3dmesh: SPZ: %w", err)
	}
	var b bytes.Buffer
	if _, err := b.ReadFrom(io.LimitReader(r, budget+1)); err != nil {
		return header{}, arrays, fmt.Errorf("nt3dmesh: SPZ: %w", err)
	}
	if int64(b.Len()) > budget {
		return header{}, arrays, fmt.Errorf("%w: SPZ decompresses past %d bytes", nt3dmesh.ErrTooLarge, budget)
	}
	raw := b.Bytes()
	h, err := readHeader(raw)
	if err != nil {
		return header{}, arrays, err
	}
	at := uint64(16)
	for i, size := range h.sizes() {
		if size > uint64(len(raw))-at {
			return header{}, arrays, fmt.Errorf("nt3dmesh: SPZ claims %d splats, more than its %d bytes hold", h.points, len(raw))
		}
		arrays[i] = raw[at : at+size]
		at += size
	}
	return h, arrays, nil
}

// readStreams reads a version 4 file: a 32-byte header, an extensions
// block (skipped), a table of the streams' compressed and decompressed
// sizes, and the zstd streams, which must end where the data ends. Every
// size is checked against the header and the budget before any stream is
// decoded.
func readStreams(data []byte, budget int64) (header, [6][]byte, error) {
	var arrays [6][]byte
	if len(data) < 32 {
		return header{}, arrays, errors.New("nt3dmesh: SPZ is shorter than its header")
	}
	h, err := readHeader(data)
	if err != nil {
		return header{}, arrays, err
	}
	if h.version != 4 {
		return header{}, arrays, fmt.Errorf("nt3dmesh: SPZ version %d with a version 4 container", h.version)
	}
	h.table = binary.LittleEndian.Uint32(data[16:])
	sizes := h.sizes()
	var expected []int // Arrays with bytes, in order.
	for i, size := range sizes {
		if size > 0 {
			expected = append(expected, i)
		}
	}
	if int(h.streams) != len(expected) {
		return header{}, arrays, fmt.Errorf("nt3dmesh: SPZ has %d streams, not %d", h.streams, len(expected))
	}
	table := uint64(h.table)
	if table < 32 || table+16*uint64(h.streams) > uint64(len(data)) {
		return header{}, arrays, errors.New("nt3dmesh: SPZ stream table lies outside the file")
	}
	at := table + 16*uint64(h.streams)
	var total uint64
	compressed := make([][]byte, len(expected))
	for k, i := range expected {
		entry := data[table+16*uint64(k):]
		packed, size := binary.LittleEndian.Uint64(entry), binary.LittleEndian.Uint64(entry[8:])
		if size != sizes[i] {
			return header{}, arrays, fmt.Errorf("nt3dmesh: SPZ stream %d holds %d bytes, not %d", k, size, sizes[i])
		}
		if packed > uint64(len(data))-at {
			return header{}, arrays, errors.New("nt3dmesh: SPZ stream runs past the file")
		}
		compressed[k] = data[at : at+packed]
		at += packed
		total += size
	}
	if at != uint64(len(data)) {
		return header{}, arrays, errors.New("nt3dmesh: SPZ has bytes after its streams")
	}
	if total > uint64(budget) {
		return header{}, arrays, fmt.Errorf("%w: SPZ decompresses to %d bytes", nt3dmesh.ErrTooLarge, total)
	}
	dec, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(uint64(budget)))
	if err != nil {
		return header{}, arrays, err
	}
	defer dec.Close()
	for k, i := range expected {
		out, err := dec.DecodeAll(compressed[k], make([]byte, 0, sizes[i]))
		if err != nil {
			return header{}, arrays, fmt.Errorf("nt3dmesh: SPZ stream %d: %w", k, err)
		}
		if uint64(len(out)) != sizes[i] {
			return header{}, arrays, fmt.Errorf("nt3dmesh: SPZ stream %d decodes to %d bytes, not %d", k, len(out), sizes[i])
		}
		arrays[i] = out
	}
	return h, arrays, nil
}

// unpack builds the geometry from the arrays, which hold the header's
// splats. Positions are 24-bit fixed point, alpha is the opacity itself,
// colors and SH are offset bytes, scales are logarithms in sixteenths from
// -10, and rotations are three signed bytes (w recovered) or, from version
// 3, a "smallest three" packing.
func unpack(h header, arrays [6][]byte) (nt3d.Geometry, error) {
	positions, alphas, colors, scales, rotations, harmonics := arrays[0], arrays[1], arrays[2], arrays[3], arrays[4], arrays[5]
	n := int(h.points)
	g := nt3d.Geometry{PointStyle: nt3d.PointStyle{Shape: nt3d.GaussianPoint, Units: nt3d.WorldUnits, Blend: nt3d.AlphaBlend}, Points: make([]nt3d.Point, 0, n)}
	count := 1 + h.dimension
	g.GaussianSH = make([]nt3dsh.RGB, 0, n*count)
	scale := float32(1 / float64(uint64(1)<<h.fraction))
	for i := range n {
		var position [3]float32
		for k := range position {
			b := positions[9*i+3*k:]
			fixed := int32(b[0]) | int32(b[1])<<8 | int32(b[2])<<16
			if fixed&0x800000 != 0 {
				fixed |= -1 << 24
			}
			position[k] = float32(fixed) * scale
		}
		var rotation [4]float64 // w, x, y, z.
		if h.rotation == 3 {
			b := rotations[3*i:]
			x, y, z := float64(b[0])/127.5-1, float64(b[1])/127.5-1, float64(b[2])/127.5-1
			rotation = [4]float64{math.Sqrt(max(0, 1-x*x-y*y-z*z)), x, y, z}
		} else {
			packed := binary.LittleEndian.Uint32(rotations[4*i:])
			largest := int(packed >> 30)
			var q [4]float64 // x, y, z, w.
			sum := 0.0
			for k := 3; k >= 0; k-- {
				if k == largest {
					continue
				}
				magnitude, negative := packed&511, packed>>9&1 == 1
				packed >>= 10
				q[k] = math.Sqrt(.5) * float64(magnitude) / 511
				if negative {
					q[k] = -q[k]
				}
				sum += q[k] * q[k]
			}
			q[largest] = math.Sqrt(max(0, 1-sum))
			rotation = [4]float64{q[3], q[0], q[1], q[2]}
		}
		var logScales, dc [3]float64
		for k := range 3 {
			logScales[k] = float64(scales[3*i+k])/16 - 10
			dc[k] = (float64(colors[3*i+k])/255 - .5) / .15
		}
		p, err := nt3dmesh.GaussianPoint(nt3dmath.Vec3{X: position[0], Y: position[1], Z: position[2]}, rotation, logScales, dc, float64(alphas[i])/255)
		if err != nil {
			return nt3d.Geometry{}, fmt.Errorf("%w (splat %d)", err, i)
		}
		g.Points = append(g.Points, p)
		coefficients := make([]nt3dsh.RGB, count)
		coefficients[0] = nt3dsh.RGB{float32(dc[0]), float32(dc[1]), float32(dc[2])}
		for k := 1; k < count; k++ {
			b := harmonics[3*(i*h.dimension+k-1):]
			coefficients[k] = nt3dsh.RGB{(float32(b[0]) - 128) / 128, (float32(b[1]) - 128) / 128, (float32(b[2]) - 128) / 128}
		}
		g.GaussianSH = append(g.GaussianSH, coefficients...)
	}
	g.Bounds = nt3d.GeometryBounds(g)
	return g, nil
}
