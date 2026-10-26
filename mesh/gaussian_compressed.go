// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/sh"
	"slices"
)

// compressedChunkProperties are the chunk element's float properties, in file order.
var compressedChunkProperties = []string{
	"min_x", "min_y", "min_z", "max_x", "max_y", "max_z",
	"min_scale_x", "min_scale_y", "min_scale_z", "max_scale_x", "max_scale_y", "max_scale_z",
	"min_r", "min_g", "min_b", "max_r", "max_g", "max_b",
}

var compressedVertexProperties = []string{"packed_position", "packed_rotation", "packed_scale", "packed_color"}

// compressedChunk is the bounds of 256 splats; color is set when the chunk has color bounds.
type compressedChunk struct {
	min, max, minScale, maxScale, minColor, maxColor [3]float64
	color                                            bool
}

func plyScalars(e plyElement, names []string, kinds ...string) bool {
	if len(e.properties) != len(names) {
		return false
	}
	for i, p := range e.properties {
		if p.name != names[i] || p.count != "" || !slices.Contains(kinds, p.kind) {
			return false
		}
	}
	return true
}

// compressedPLY reports whether elements are PlayCanvas compressed PLY:
// chunk and vertex elements with the packed layout, then optionally sh.
func compressedPLY(elements []plyElement) bool {
	if len(elements) < 2 || len(elements) > 3 || elements[0].name != "chunk" || elements[1].name != "vertex" {
		return false
	}
	chunk := elements[0]
	if !plyScalars(chunk, compressedChunkProperties[:12], "float", "float32") && !plyScalars(chunk, compressedChunkProperties, "float", "float32") {
		return false
	}
	return plyScalars(elements[1], compressedVertexProperties, "uint", "uint32") && (len(elements) == 2 || elements[2].name == "sh")
}

// compressedUnorm is a bits-wide field as a number from 0 to 1.
func compressedUnorm(v uint32, bits uint) float64 {
	return float64(v&(1<<bits-1)) / float64(uint32(1)<<bits-1)
}

func compressedLerp(a, b, t float64) float64 { return a*(1-t) + b*t }

// DecodeCompressedPLY reads PlayCanvas (SuperSplat) compressed PLY: 256-splat
// chunks that bound a position, a log scale, and optionally a color, and per
// splat four packed uint32 (11-10-11 position and scale, a smallest-three
// rotation, and 8-bit color and opacity). An sh element of uchar f_rest_N
// bytes, if present, holds degree 1 through 3 coefficients, each byte b meaning
// 8b/255 - 4. Without color bounds a color byte is the color itself. The
// result matches DecodeGaussianPLY's. DecodePLY detects this layout.
func DecodeCompressedPLY(data []byte, o *Options) (*Model, error) {
	if int64(len(data)) > o.maxBytes() {
		return nil, fmt.Errorf("nt3dmesh: compressed PLY exceeds byte limit")
	}
	elements, format, _, body, err := plyHeader(data)
	if err != nil {
		return nil, err
	}
	if !compressedPLY(elements) {
		return nil, fmt.Errorf("nt3dmesh: not a compressed Gaussian PLY")
	}
	r := &plyReader{data: data, at: body, line: bytes.Count(data[:body], []byte("\n")) + 1}
	switch format {
	case "ascii":
	case "binary_little_endian":
		r.order = binary.LittleEndian
	case "binary_big_endian":
		r.order = binary.BigEndian
	default:
		return nil, fmt.Errorf("nt3dmesh: unsupported PLY format %q", format)
	}
	chunkElement, vertex := elements[0], elements[1]
	var shElement *plyElement
	rest := 0
	if len(elements) == 3 {
		shElement = &elements[2]
		rest = len(shElement.properties)
		if shElement.count != vertex.count || rest%3 != 0 || nt3dsh.Degree(1+rest/3) < 1 {
			return nil, r.errorf("compressed PLY sh needs one record per vertex, with 9, 24, or 45 f_rest properties")
		}
		for j, p := range shElement.properties {
			if p.name != fmt.Sprintf("f_rest_%d", j) || p.count != "" || (p.kind != "uchar" && p.kind != "uint8") {
				return nil, r.errorf("compressed PLY sh properties must be uchar f_rest_0 onward")
			}
		}
	}
	if vertex.count < 1 || chunkElement.count != (vertex.count+255)/256 {
		return nil, r.errorf("compressed PLY needs at least one vertex, and a chunk for each 256")
	}
	// Each element is checked against the bytes that remain before anything is
	// allocated for it; a binary scalar takes its size, an ASCII one at least two bytes.
	remaining := len(data) - r.at
	for _, e := range elements {
		record := 0
		for _, p := range e.properties {
			if r.order == nil {
				record += 2
			} else {
				record += plySize(p.kind)
			}
		}
		if e.count > remaining/record {
			return nil, r.errorf("element count exceeds remaining bytes")
		}
		remaining -= e.count * record
	}
	chunks := make([]compressedChunk, 0, chunkElement.count)
	for range chunkElement.count {
		var v [18]float64
		for i := range chunkElement.properties {
			if v[i], err = r.scalar(chunkElement.properties[i].kind); err != nil {
				return nil, err
			}
		}
		c := compressedChunk{color: len(chunkElement.properties) == 18}
		for k := range 3 {
			c.min[k], c.max[k], c.minScale[k], c.maxScale[k] = v[k], v[3+k], v[6+k], v[9+k]
			c.minColor[k], c.maxColor[k] = v[12+k], v[15+k]
		}
		chunks = append(chunks, c)
	}
	const c0 = 0.28209479177387814
	g := nt3d.Geometry{PointStyle: nt3d.PointStyle{Shape: nt3d.GaussianPoint, Units: nt3d.WorldUnits, Blend: nt3d.AlphaBlend}}
	var dcs [][3]float64
	// Append only after reading: a dishonest header cannot trigger a huge allocation.
	for i := range vertex.count {
		var packed [4]uint32
		for j := range packed {
			v, err := r.scalar(vertex.properties[j].kind)
			if err != nil {
				return nil, err
			}
			if v < 0 || v > math.MaxUint32 || v != math.Trunc(v) {
				return nil, r.errorf("bad packed value %g", v)
			}
			packed[j] = uint32(v)
		}
		c := chunks[i/256]
		var position, logScale, dc [3]float64
		for k, bits := range [3]uint{11, 10, 11} {
			shift := [3]uint{21, 11, 0}[k]
			position[k] = compressedLerp(c.min[k], c.max[k], compressedUnorm(packed[0]>>shift, bits))
			logScale[k] = compressedLerp(c.minScale[k], c.maxScale[k], compressedUnorm(packed[2]>>shift, bits))
		}
		// The three stored components are the quaternion's others, in order, around the largest.
		var rotation [4]float64
		var others [3]float64
		sum := 0.0
		for k := range others {
			others[k] = (compressedUnorm(packed[1]>>(20-10*uint(k)), 10) - .5) * math.Sqrt2
			sum += others[k] * others[k]
		}
		rotation[packed[1]>>30] = math.Sqrt(math.Max(0, 1-sum))
		next := 0
		for k := range rotation {
			if k != int(packed[1]>>30) {
				rotation[k] = others[next]
				next++
			}
		}
		for k := range 3 {
			color := compressedUnorm(packed[3]>>(24-8*uint(k)), 8)
			if c.color {
				color = compressedLerp(c.minColor[k], c.maxColor[k], color)
			}
			dc[k] = (color - .5) / c0
		}
		p, err := GaussianPoint(nt3dmath.Vec3{X: float32(position[0]), Y: float32(position[1]), Z: float32(position[2])},
			rotation, logScale, dc, compressedUnorm(packed[3], 8))
		if err != nil {
			return nil, r.errorf("%v", err)
		}
		g.Points = append(g.Points, p)
		dcs = append(dcs, dc)
	}
	restValues := make([]float64, rest)
	for i := range vertex.count {
		for j := range restValues {
			b, err := r.scalar(shElement.properties[j].kind)
			if err != nil {
				return nil, err
			}
			restValues[j] = b*(8.0/255) - 4
		}
		coefficients, err := GaussianSH(dcs[i], restValues)
		if err != nil {
			return nil, r.errorf("%v", err)
		}
		g.GaussianSH = append(g.GaussianSH, coefficients...)
	}
	g.Bounds = nt3d.GeometryBounds(g)
	m := &Model{Format: PLY}
	if err := paletteCloud(&g, o); err != nil {
		return nil, err
	}
	m.Objects = PointPieces(Object{Geometry: g})
	if err := ClusterPieces(m.Objects, o); err != nil {
		return nil, err
	}
	for _, piece := range m.Objects {
		if err := piece.Geometry.Validate(); err != nil {
			return nil, err
		}
	}
	o.orient(m)
	return m, nil
}
