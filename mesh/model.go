// Copyright (c) 2026 Neomantra Corp.

// Package nt3dmesh reads and writes mesh files as nt3d geometry.
//
// Decoders bound their work: the triangle count is limited (by default to
// nt3d.MaxMeshTriangles), coordinates must be finite and within ±1e15, and
// side files such as OBJ materials and textures are read only through
// Options.Open. Faces keep their file's winding; normals the file omits are
// computed flat. Units are whatever the file uses; nothing is normalized.
package nt3dmesh

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
)

// Format names a mesh file format.
type Format string

const (
	STL Format = "stl"
	OBJ Format = "obj"
)

// DefaultColor is given to faces whose file names no color, unless Options.Color is set.
var DefaultColor = color.RGBA{255, 255, 255, 255}

// maxCoordinate keeps bounds and transforms of decoded meshes finite.
const maxCoordinate = 1e15

// Options control decoding. The zero value, or nil, uses the defaults.
type Options struct {
	// MaxTriangles limits the triangles of the whole model. Zero, or a value
	// above nt3d.MaxMeshTriangles, uses nt3d.MaxMeshTriangles.
	MaxTriangles int
	// Color paints faces the file leaves uncolored. Zero uses DefaultColor.
	Color color.RGBA
	// Open reads a side file named by the model, such as an OBJ material
	// library or texture, by a slash-separated name relative to the model.
	// Nil skips side files. ReadFile supplies one confined to the model's directory.
	Open func(name string) ([]byte, error)
}

func (o *Options) limit() int {
	if o == nil || o.MaxTriangles <= 0 || o.MaxTriangles > nt3d.MaxMeshTriangles {
		return nt3d.MaxMeshTriangles
	}
	return o.MaxTriangles
}
func (o *Options) color() color.RGBA {
	if o == nil || o.Color == (color.RGBA{}) {
		return DefaultColor
	}
	return o.Color
}

// Object is one named part of a model.
type Object struct {
	Name     string // The STL solid or OBJ object/group name; may be empty.
	Material string // The OBJ material name, when the part has one.
	Geometry nt3d.Geometry
	// Plain marks, per vertex, those the file gave no color. A viewer may
	// repaint them without disturbing the file's own colors.
	Plain []bool
}

// Model is what a mesh file holds.
type Model struct {
	Format   Format
	Objects  []Object
	Warnings []string // Problems passed over, such as a missing texture.
}

// Triangles counts the model's faces.
func (m *Model) Triangles() (n int) {
	for _, o := range m.Objects {
		n += len(o.Geometry.Indices) / 3
	}
	return n
}

// Bounds encloses every object.
func (m *Model) Bounds() (b nt3dmath.AABB) {
	for _, o := range m.Objects {
		if o.Geometry.Bounds.IsValid() {
			b.Include(o.Geometry.Bounds.Min)
			b.Include(o.Geometry.Bounds.Max)
		}
	}
	return b
}

// Geometry merges the objects into one geometry. It fails when objects have
// different materials, as textured OBJ parts do; add those with AddTo.
func (m *Model) Geometry() (nt3d.Geometry, error) {
	gs := make([]nt3d.Geometry, len(m.Objects))
	for i, o := range m.Objects {
		gs[i] = o.Geometry
	}
	return nt3d.MergeGeometry(gs...)
}

// AddTo adds a group node named name under parent, with one child node per
// object, named name/0, name/1, and so on. On failure nothing is added.
func (m *Model) AddTo(s *nt3d.Scene, parent nt3d.NodeID, name string) (nt3d.NodeID, error) {
	group, err := s.Add(parent, name)
	if err != nil {
		return 0, err
	}
	for i, o := range m.Objects {
		id, err := s.Add(group, fmt.Sprintf("%s/%d", name, i))
		if err == nil {
			err = s.SetNodeGeometry(id, o.Geometry)
		}
		if err != nil {
			_ = s.Remove(group)
			return 0, err
		}
	}
	return group, nil
}

// FormatOf chooses a format by file extension, then by content.
func FormatOf(name string, data []byte) (Format, bool) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".stl":
		return STL, true
	case ".obj":
		return OBJ, true
	}
	if binarySTL(data) || bytes.HasPrefix(bytes.TrimLeft(data, " \t\r\n"), []byte("solid")) {
		return STL, true
	}
	return "", false
}

// Decode reads a model in the given format.
func Decode(data []byte, f Format, o *Options) (*Model, error) {
	switch f {
	case STL:
		return DecodeSTL(data, o)
	case OBJ:
		return DecodeOBJ(data, o)
	}
	return nil, fmt.Errorf("nt3dmesh: unsupported format %q", f)
}

// ReadFile reads and decodes a mesh file, choosing its format with FormatOf.
// Unless o supplies Open, side files are read from the file's directory and
// its subdirectories only.
func ReadFile(name string, o *Options) (*Model, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	f, ok := FormatOf(name, data)
	if !ok {
		return nil, fmt.Errorf("nt3dmesh: %s: unknown mesh format", filepath.Base(name))
	}
	options := Options{}
	if o != nil {
		options = *o
	}
	if options.Open == nil {
		options.Open = DirOpener(filepath.Dir(name))
	}
	return Decode(data, f, &options)
}

// DirOpener returns an Options.Open that reads files within dir. Names that
// are absolute or climb out of dir are refused; backslashes count as slashes.
func DirOpener(dir string) func(string) ([]byte, error) {
	fsys := os.DirFS(dir)
	return func(name string) ([]byte, error) {
		name = path.Clean(strings.ReplaceAll(name, `\`, "/"))
		if !fs.ValidPath(name) {
			return nil, errors.New("nt3dmesh: side file outside the model's directory")
		}
		return fs.ReadFile(fsys, name)
	}
}

func validCoordinate(v ...float32) bool {
	for _, x := range v {
		if !nt3dmath.IsFinite(float64(x)) || x > maxCoordinate || x < -maxCoordinate {
			return false
		}
	}
	return true
}

// triangleBudget counts faces against the model's limit.
type triangleBudget struct{ used, limit int }

func (b *triangleBudget) take(n int) error {
	if b.used+n > b.limit {
		return fmt.Errorf("nt3dmesh: model exceeds %d triangles", b.limit)
	}
	b.used += n
	return nil
}
