// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"image/color"
	"io"
	"math"
	"strconv"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
)

// binarySTL reports whether data is exactly as long as the binary STL its
// header counts. Binary headers may begin with "solid", so the record length,
// not the first word, tells binary from ASCII.
func binarySTL(data []byte) bool {
	if len(data) < 84 {
		return false
	}
	n := uint64(binary.LittleEndian.Uint32(data[80:84]))
	return 84+50*n == uint64(len(data))
}

// faceObject accumulates an STL solid or a 3MF part. Each face has vertices
// of its own, carrying its recomputed normal: exporters often leave facet
// normals zero or stale. A full piece is set aside and another begun.
type faceObject struct {
	Object
	pieces []Object
	budget *triangleBudget
	format string // For errors; empty is STL.
}

// objects returns the pieces, ending with the current one if it has faces.
func (s *faceObject) objects() []Object {
	if len(s.Geometry.Indices) != 0 {
		return append(s.pieces, s.Object)
	}
	return s.pieces
}

func (s *faceObject) add(v [3]nt3dmath.Vec3, shade color.RGBA, plain bool) error {
	if err := s.budget.take(1); err != nil {
		return err
	}
	if len(s.Geometry.Indices) >= 3*pieceTriangles {
		s.pieces = append(s.pieces, s.Object)
		s.Object = Object{Name: s.Name, Material: s.Material, Part: s.Part}
	}
	for _, p := range v {
		if !validCoordinate(p.X, p.Y, p.Z) {
			return fmt.Errorf("nt3dmesh: %s vertex is non-finite or exceeds coordinate limits", cmp.Or(s.format, "STL"))
		}
	}
	n := v[1].Sub(v[0]).Cross(v[2].Sub(v[0])).Normalize()
	g := &s.Geometry
	for _, p := range v {
		g.Indices = append(g.Indices, uint32(len(g.Vertices)))
		g.Vertices = append(g.Vertices, nt3d.Vertex{Position: p, Normal: n, Color: shade})
		g.Bounds.Include(p)
		s.Plain = append(s.Plain, plain)
	}
	return nil
}

// DecodeSTL reads binary or ASCII STL. An ASCII file may hold several solids,
// each an object. Binary face colors follow the VisCAM/SolidView convention,
// or Materialise Magics when the header names a COLOR=.
func DecodeSTL(data []byte, o *Options) (*Model, error) {
	budget := &triangleBudget{limit: o.limit()}
	var m *Model
	var err error
	if binarySTL(data) {
		m, err = decodeBinarySTL(data, o, budget)
	} else {
		m, err = decodeASCIISTL(data, o, budget)
	}
	if err != nil {
		return nil, err
	}
	if m.Triangles() == 0 {
		return nil, errors.New("nt3dmesh: STL contains no triangles")
	}
	for _, obj := range m.Objects {
		if err := obj.Geometry.Validate(); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func expand5(v uint16) uint8 { v &= 31; return uint8(v<<3 | v>>2) }

func decodeBinarySTL(data []byte, o *Options, budget *triangleBudget) (*Model, error) {
	n := int(binary.LittleEndian.Uint32(data[80:84]))
	if n > budget.limit {
		return nil, fmt.Errorf("%w: STL has %d; the limit is %d", ErrTooManyTriangles, n, budget.limit)
	}
	header := data[:80]
	// Magics stores a default color after COLOR= and inverts the validity bit.
	magics, objectColor := false, o.color()
	if i := bytes.Index(header, []byte("COLOR=")); i >= 0 && i+10 <= len(header) {
		magics = true
		objectColor = color.RGBA{header[i+6], header[i+7], header[i+8], 255}
	}
	s := &faceObject{budget: budget}
	first := 3 * min(n, pieceTriangles)
	s.Geometry.Vertices = make([]nt3d.Vertex, 0, first)
	s.Geometry.Indices = make([]uint32, 0, first)
	s.Plain = make([]bool, 0, first)
	for off := 84; off < len(data); off += 50 {
		var v [3]nt3dmath.Vec3
		for j := range v {
			p := data[off+12+j*12:]
			v[j] = nt3dmath.Vec3{X: math.Float32frombits(binary.LittleEndian.Uint32(p)), Y: math.Float32frombits(binary.LittleEndian.Uint32(p[4:])), Z: math.Float32frombits(binary.LittleEndian.Uint32(p[8:]))}
		}
		attribute := binary.LittleEndian.Uint16(data[off+48:])
		shade, plain := o.color(), true
		switch {
		case magics && attribute&0x8000 == 0:
			shade, plain = color.RGBA{expand5(attribute), expand5(attribute >> 5), expand5(attribute >> 10), 255}, false
		case magics:
			shade, plain = objectColor, false
		case attribute&0x8000 != 0:
			shade, plain = color.RGBA{expand5(attribute >> 10), expand5(attribute >> 5), expand5(attribute), 255}, false
		}
		if err := s.add(v, shade, plain); err != nil {
			return nil, err
		}
	}
	return &Model{Format: STL, Objects: s.objects()}, nil
}

// stlWords splits ASCII STL into whitespace-separated words, counting lines.
type stlWords struct {
	data []byte
	at   int
	line int
}

func (w *stlWords) next() ([]byte, bool) {
	for w.at < len(w.data) && isSpace(w.data[w.at]) {
		if w.data[w.at] == '\n' {
			w.line++
		}
		w.at++
	}
	start := w.at
	for w.at < len(w.data) && !isSpace(w.data[w.at]) {
		w.at++
	}
	return w.data[start:w.at], w.at > start
}

// rest returns the remainder of the current line, trimmed.
func (w *stlWords) rest() string {
	start := w.at
	for w.at < len(w.data) && w.data[w.at] != '\n' {
		w.at++
	}
	return string(bytes.TrimSpace(w.data[start:w.at]))
}
func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' || c == '\v'
}

func (w *stlWords) errorf(format string, a ...any) error {
	return fmt.Errorf("nt3dmesh: STL line %d: %s", w.line+1, fmt.Sprintf(format, a...))
}
func (w *stlWords) expect(want string) error {
	got, ok := w.next()
	if !ok {
		return w.errorf("expected %s, found end of file", want)
	}
	if !bytes.EqualFold(got, []byte(want)) {
		return w.errorf("expected %s, got %q", want, got)
	}
	return nil
}
func (w *stlWords) vector() (v nt3dmath.Vec3, err error) {
	for _, c := range []*float32{&v.X, &v.Y, &v.Z} {
		word, ok := w.next()
		if !ok {
			return v, w.errorf("expected a number, found end of file")
		}
		f, err := strconv.ParseFloat(string(word), 32)
		if err != nil {
			return v, w.errorf("bad number %q", word)
		}
		*c = float32(f)
	}
	return v, nil
}

func decodeASCIISTL(data []byte, o *Options, budget *triangleBudget) (*Model, error) {
	w := &stlWords{data: data}
	m := &Model{Format: STL}
	for solids := 0; ; solids++ {
		word, ok := w.next()
		if !ok {
			if solids == 0 {
				return nil, errors.New("nt3dmesh: invalid STL header or truncated binary STL")
			}
			return m, nil
		}
		if !bytes.EqualFold(word, []byte("solid")) {
			if solids == 0 {
				return nil, errors.New("nt3dmesh: invalid STL header or truncated binary STL")
			}
			return nil, w.errorf("unexpected data after endsolid")
		}
		s := &faceObject{budget: budget}
		s.Name = w.rest()
		for {
			word, ok := w.next()
			if !ok {
				return nil, w.errorf("missing endsolid")
			}
			if bytes.EqualFold(word, []byte("endsolid")) {
				w.rest()
				break
			}
			if !bytes.EqualFold(word, []byte("facet")) {
				return nil, w.errorf("expected facet, got %q", word)
			}
			if err := w.expect("normal"); err != nil {
				return nil, err
			}
			if _, err := w.vector(); err != nil {
				return nil, err
			}
			if err := w.expect("outer"); err != nil {
				return nil, err
			}
			if err := w.expect("loop"); err != nil {
				return nil, err
			}
			var v [3]nt3dmath.Vec3
			for i := range v {
				if err := w.expect("vertex"); err != nil {
					return nil, err
				}
				var err error
				if v[i], err = w.vector(); err != nil {
					return nil, err
				}
			}
			if err := w.expect("endloop"); err != nil {
				return nil, err
			}
			if err := w.expect("endfacet"); err != nil {
				return nil, err
			}
			if err := s.add(v, o.color(), true); err != nil {
				return nil, err
			}
		}
		m.Objects = append(m.Objects, s.objects()...)
	}
}

// EncodeSTL writes the triangles of g as binary STL in local coordinates,
// with each face's first vertex color in the VisCAM/SolidView convention.
// Points, boxes, lines, and arrows are not written.
func EncodeSTL(w io.Writer, g nt3d.Geometry) error {
	if err := g.Validate(); err != nil {
		return err
	}
	if len(g.Indices) == 0 {
		return errors.New("nt3dmesh: STL needs at least one triangle")
	}
	b := bufio.NewWriter(w)
	var header [84]byte
	copy(header[:], "binary STL written by nt3d")
	binary.LittleEndian.PutUint32(header[80:], uint32(len(g.Indices)/3))
	b.Write(header[:])
	var record [50]byte
	put := func(at int, v nt3dmath.Vec3) {
		binary.LittleEndian.PutUint32(record[at:], math.Float32bits(v.X))
		binary.LittleEndian.PutUint32(record[at+4:], math.Float32bits(v.Y))
		binary.LittleEndian.PutUint32(record[at+8:], math.Float32bits(v.Z))
	}
	for i := 0; i < len(g.Indices); i += 3 {
		a, c, d := g.Vertices[g.Indices[i]], g.Vertices[g.Indices[i+1]], g.Vertices[g.Indices[i+2]]
		put(0, c.Position.Sub(a.Position).Cross(d.Position.Sub(a.Position)).Normalize())
		put(12, a.Position)
		put(24, c.Position)
		put(36, d.Position)
		k := a.Color
		binary.LittleEndian.PutUint16(record[48:], 0x8000|uint16(k.R>>3)<<10|uint16(k.G>>3)<<5|uint16(k.B>>3))
		b.Write(record[:])
	}
	return b.Flush()
}
