// Copyright (c) 2026 Neomantra Corp.

// Package nt3dmesh reads and writes mesh files as nt3d geometry.
//
// Decoders bound their work: the triangle count is limited (by default to
// DefaultMaxTriangles), coordinates must be finite and within ±1e15, 3MF
// packages are limited in entries and unpacked bytes, and side files such
// as OBJ materials and textures are read only through Options.Open.
//
// Faces keep their file's winding; normals the file omits are computed flat.
// An object with more triangles than one geometry may hold
// (nt3d.MaxMeshTriangles, the largest buffer every WebGPU device binds) is
// split into consecutive Objects with the same Name, Material, and Part, as
// a point cloud past nt3d.MaxPoints or nt3d.MaxGaussians is (PointPieces).
// Units are whatever the file uses; nothing is normalized.
//
// STL, OBJ, 3MF, PLY (meshes, point clouds, and 3DGS Gaussian splats), and
// Stanford .conf are built in; glTF, SPZ, and SOG register themselves from
// nt3dmesh/gltf, nt3dmesh/spz, and nt3dmesh/sog, or all at once from
// nt3dmesh/formats.
package nt3dmesh

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/clod"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/sh"
)

// Format names a mesh file format.
type Format string

const (
	STL     Format = "stl"
	OBJ     Format = "obj"
	ThreeMF Format = "3mf"
)

// DefaultColor is given to faces whose file names no color, unless Options.Color is set.
var DefaultColor = color.RGBA{255, 255, 255, 255}

// MaxCoordinate keeps bounds and transforms of decoded meshes finite:
// decoders refuse coordinates beyond it.
const MaxCoordinate = 1e15

const maxCoordinate = MaxCoordinate

// ErrTooManyTriangles is wrapped by errors for models over Options.MaxTriangles.
var ErrTooManyTriangles = errors.New("nt3dmesh: too many triangles")

// ErrTooLarge is wrapped by errors for packages that unpack past Options.MaxBytes.
var ErrTooLarge = errors.New("nt3dmesh: package unpacks to too many bytes")

// DefaultMaxTriangles is the model's triangle limit when Options.MaxTriangles
// is zero: about 600 MB of GPU buffers, and somewhat less of decoded memory.
const DefaultMaxTriangles = 4 << 20

// pieceTriangles is the most triangles one Object holds, read when a mesh
// is split so it follows nt3d.SetBindingLimit. Tests lower it.
var pieceTriangles = nt3d.MeshTriangleLimit

// piecePoints, maxGaussians, and maxPreciseGaussians are the most points, and
// Gaussian splats with a count of SH coefficients each (compact, or Precise),
// one Object holds. Tests lower them.
var (
	piecePoints         = nt3d.PointLimit
	maxGaussians        = nt3d.MaxGaussians
	maxPreciseGaussians = nt3d.MaxPreciseGaussians
)

// PointPieces splits a point cloud larger than one geometry holds (see
// nt3d.MaxPoints, nt3d.MaxGaussians, and nt3d.MaxPreciseGaussians, by the
// geometry's PointStyle.Precise) into consecutive Objects with the object's
// Name, Material, and Part, each with its own Points, GaussianSH, and
// Bounds, sharing the rest. The pieces are slabs along the cloud's
// longest axis: blended slabs sort together with every other alpha point
// geometry to within one of 64 depth slices, so a cloud split into slabs
// blends in order where the pieces meet. A cloud with an SH palette
// (nt3d.Geometry.GaussianSHPalette) holds one coefficient a splat, so pieces
// are sized for that; each piece takes its splats' labels and shares the
// palette. Clusters are dropped from split pieces. A cloud that fits is
// returned as it is, in a slice of one.
func PointPieces(o Object) []Object {
	g := &o.Geometry
	most := piecePoints()
	per := 0
	if len(g.Points) > 0 {
		per = len(g.GaussianSH) / len(g.Points)
	}
	if g.PointStyle.Shape == nt3d.GaussianPoint {
		most = maxGaussians(per)
		if g.PointStyle.Precise {
			most = maxPreciseGaussians(per)
		}
	}
	if most <= 0 || len(g.Points) <= most {
		return []Object{o}
	}
	bounds := g.Bounds
	if !bounds.IsValid() {
		bounds = nt3d.GeometryBounds(*g)
	}
	size := bounds.Max.Sub(bounds.Min)
	along := func(v nt3dmath.Vec3) float32 { return v.X }
	if size.Y > size.X && size.Y >= size.Z {
		along = func(v nt3dmath.Vec3) float32 { return v.Y }
	} else if size.Z > size.X && size.Z > size.Y {
		along = func(v nt3dmath.Vec3) float32 { return v.Z }
	}
	order := make([]int, len(g.Points))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return cmp.Compare(along(g.Points[a].Position), along(g.Points[b].Position))
	})
	var pieces []Object
	for start := 0; start < len(order); start += most {
		end := min(len(order), start+most)
		piece := o
		piece.Geometry.Points = make([]nt3d.Point, 0, end-start)
		piece.Geometry.GaussianSH = nil
		piece.Geometry.GaussianSHLabels = nil
		piece.Geometry.Clusters = nil
		if per > 0 {
			piece.Geometry.GaussianSH = make([]nt3dsh.RGB, 0, (end-start)*per)
		}
		if g.GaussianSHLabels != nil {
			piece.Geometry.GaussianSHLabels = make([]uint16, 0, end-start)
		}
		for _, i := range order[start:end] {
			piece.Geometry.Points = append(piece.Geometry.Points, g.Points[i])
			if per > 0 {
				piece.Geometry.GaussianSH = append(piece.Geometry.GaussianSH, g.GaussianSH[i*per:(i+1)*per]...)
			}
			if g.GaussianSHLabels != nil {
				piece.Geometry.GaussianSHLabels = append(piece.Geometry.GaussianSHLabels, g.GaussianSHLabels[i])
			}
		}
		piece.Geometry.Bounds = nt3d.GeometryBounds(piece.Geometry)
		pieces = append(pieces, piece)
	}
	return pieces
}

// DefaultMaxBytes is how much a 3MF package may unpack to when Options.MaxBytes is zero.
const DefaultMaxBytes = 256 << 20

// Up names the axis a file treats as up.
type Up uint8

const (
	// ZUp, the default, keeps coordinates as they are: nt3d, STL from most
	// CAD and slicing tools, and 3MF (by its specification) are Z-up.
	ZUp Up = iota
	// YUp turns a Y-up model, as many OBJ and some STL files are, to Z-up:
	// (x, y, z) becomes (x, -z, y), a rotation that keeps faces outward.
	YUp
)

// Options control decoding. The zero value, or nil, uses the defaults.
type Options struct {
	// MaxTriangles limits the triangles of the whole model, a budget for
	// memory and time. Zero uses DefaultMaxTriangles. Device memory, not the
	// format, sets how much a GPU can draw; a GPU renderer that runs out fails
	// the frame, and the widget reports it (see nt3dwidget.WithGPUFailure).
	MaxTriangles int
	// Color paints faces the file leaves uncolored. Zero uses DefaultColor.
	Color color.RGBA
	// Open reads a side file named by the model, such as an OBJ material
	// library or texture, by a slash-separated name relative to the model.
	// Nil skips side files. ReadFile supplies one confined to the model's directory.
	Open func(name string) ([]byte, error)
	// Parts chooses which 3MF build items to assemble, by index into
	// Model.Parts; nil assembles all. The triangle limit applies to those chosen.
	Parts []bool
	// MaxBytes limits unpacked 3MF data, Gaussian PLY input bytes,
	// decompressed SPZ, and SOG files and images. Zero uses DefaultMaxBytes.
	MaxBytes int64
	// Up is the file's up axis. Most files do not say, so the caller must;
	// SPZ states its axes, and ignores Up.
	Up Up
	// NoClusters skips clustering Gaussian clouds for level of detail;
	// loading is faster and the renderers draw every splat.
	NoClusters bool
	// SHPalette chooses how Gaussian clouds carry their higher-order
	// spherical harmonics. The zero value, SHPaletteKeep, leaves each format
	// as it comes: a SOG's palette stays a palette, other formats keep
	// per-splat coefficients.
	SHPalette SHPaletteMode
}

// SHPaletteMode says whether a decoded Gaussian cloud shares its spherical
// harmonics above DC through a palette (nt3d.Geometry.GaussianSHPalette) or
// carries them per splat.
type SHPaletteMode int

const (
	// SHPaletteKeep, the zero value, keeps what the file holds: a SOG's
	// palette stays a palette (and GaussianSH is then DC only); other
	// formats give per-splat coefficients and no palette is built.
	SHPaletteKeep SHPaletteMode = iota
	// SHPaletteBuild gives every cloud with higher-order bands a palette,
	// building one with DefaultPaletteOptions (see BuildSHPalette) when the
	// file has none. The build runs on the whole cloud before it is split
	// into pieces, so the pieces share one palette, and clouds too large for
	// one geometry build as well. Clouds of degree 0 are left alone.
	SHPaletteBuild
	// SHPaletteExpand gives every cloud per-splat coefficients, expanding a
	// SOG's palette. The geometry is what v0.5.0 decoded.
	SHPaletteExpand
)

// ApplySHPalette applies o's SHPalette mode to a whole decoded Gaussian
// cloud, as the built-in decoders do. Format packages call it once the cloud
// is complete and before PointPieces splits it, so every piece shares one
// palette. A nil o, or SHPaletteKeep, leaves g as it is.
func ApplySHPalette(g *nt3d.Geometry, o *Options) error { return paletteCloud(g, o) }

// paletteCloud applies o's SHPalette mode to a whole Gaussian cloud, before
// PointPieces splits it so that every piece shares one palette.
func paletteCloud(g *nt3d.Geometry, o *Options) error {
	if o == nil {
		return nil
	}
	switch o.SHPalette {
	case SHPaletteBuild:
		if g.GaussianSHPalette == nil && len(g.Points) > 0 && len(g.GaussianSH)/len(g.Points) >= 4 {
			_, err := BuildSHPalette(context.Background(), g, DefaultPaletteOptions())
			return err
		}
	case SHPaletteExpand:
		nt3d.ExpandSHPalette(g)
	}
	return nil
}

// ClusterPieces builds level-of-detail clusters for each Gaussian piece, as
// the decoders do unless Options.NoClusters is set.
func ClusterPieces(objects []Object, o *Options) error {
	if o != nil && o.NoClusters {
		return nil
	}
	for i := range objects {
		g := &objects[i].Geometry
		if g.PointStyle.Shape != nt3d.GaussianPoint {
			continue
		}
		positions := make([]nt3dmath.Vec3, len(g.Points))
		for j, p := range g.Points {
			positions[j] = p.Position
		}
		set, err := nt3dclod.Build(positions, nt3dclod.Options{})
		if err != nil {
			return err
		}
		g.Clusters = set
	}
	return nil
}

// orient turns a decoded model to Z-up.
func (o *Options) orient(m *Model) {
	if o == nil || o.Up != YUp {
		return
	}
	m.OrientYUp()
}

// OrientYUp turns a Y-up model to Z-up, as Options.Up YUp does: (x, y, z)
// becomes (x, -z, y) for every vertex, normal, point, Gaussian axis, line,
// arrow, and box, and Gaussian spherical harmonics turn with them through
// GaussianSHTransform. A decoder of a format that states its axes calls it
// itself. Level-of-detail Clusters are refit to the turned points.
func (m *Model) OrientYUp() {
	turn := func(v nt3dmath.Vec3) nt3dmath.Vec3 { return nt3dmath.Vec3{X: v.X, Y: -v.Z, Z: v.Y} }
	for i := range m.Objects {
		g := &m.Objects[i].Geometry
		if len(g.GaussianSH) > 0 {
			basis := g.GaussianSHTransform
			if basis == (nt3dmath.Mat4{}) {
				basis = nt3dmath.Identity()
			}
			// Inverse of the Y-up to Z-up rotation: (x,y,z) -> (x,z,-y).
			inverse := nt3dmath.Identity()
			inverse[5], inverse[6], inverse[9], inverse[10] = 0, -1, 1, 0
			g.GaussianSHTransform = basis.Mul(inverse)
		}
		for j := range g.Vertices {
			g.Vertices[j].Position, g.Vertices[j].Normal = turn(g.Vertices[j].Position), turn(g.Vertices[j].Normal)
		}
		for j := range g.Lines {
			g.Lines[j].Position = turn(g.Lines[j].Position)
		}
		for j := range g.Points {
			g.Points[j].Position = turn(g.Points[j].Position)
			for k, axis := range g.Points[j].GaussianAxes {
				g.Points[j].GaussianAxes[k] = turn(axis)
			}
		}
		for j := range g.Arrows {
			g.Arrows[j].Start, g.Arrows[j].End = turn(g.Arrows[j].Start), turn(g.Arrows[j].End)
		}
		for j := range g.Boxes {
			// The corner that turns to the new minimum is the old one at the top of z.
			b := &g.Boxes[j]
			b.Min = turn(b.Min.Add(nt3dmath.Vec3{Z: b.Size.Z}))
			b.Size = nt3dmath.Vec3{X: b.Size.X, Y: b.Size.Z, Z: b.Size.Y}
		}
		if g.Clusters != nil {
			// Refit fails only on a set that does not match the points; one
			// that no longer describes them is dropped.
			positions := make([]nt3dmath.Vec3, len(g.Points))
			for j, p := range g.Points {
				positions[j] = p.Position
			}
			if g.Clusters.Refit(positions) != nil {
				g.Clusters = nil
			}
		}
		g.Bounds = nt3d.GeometryBounds(*g)
	}
}

// orientBounds turns bounds to Z-up as orient turns the objects.
func (o *Options) orientBounds(b nt3dmath.AABB) nt3dmath.AABB {
	if o == nil || o.Up != YUp || !b.IsValid() {
		return b
	}
	var out nt3dmath.AABB
	out.Include(nt3dmath.Vec3{X: b.Min.X, Y: -b.Max.Z, Z: b.Min.Y})
	out.Include(nt3dmath.Vec3{X: b.Max.X, Y: -b.Min.Z, Z: b.Max.Y})
	return out
}

func (o *Options) maxBytes() int64 {
	if o == nil || o.MaxBytes <= 0 {
		return DefaultMaxBytes
	}
	return o.MaxBytes
}
func (o *Options) chosen(part int) bool {
	return o == nil || o.Parts == nil || part < len(o.Parts) && o.Parts[part]
}

func (o *Options) limit() int {
	if o == nil || o.MaxTriangles <= 0 {
		return DefaultMaxTriangles
	}
	return o.MaxTriangles
}
func (o *Options) color() color.RGBA {
	if o == nil || o.Color == (color.RGBA{}) {
		return DefaultColor
	}
	return o.Color
}

// TriangleLimit is the model's triangle limit: MaxTriangles, or the default.
// Registered decoders honor it.
func (o *Options) TriangleLimit() int { return o.limit() }

// ByteLimit is how much a package and its side files may hold: MaxBytes,
// or the default. Registered decoders honor it.
func (o *Options) ByteLimit() int64 { return o.maxBytes() }

// FaceColor paints faces the file leaves uncolored: Color, or DefaultColor.
func (o *Options) FaceColor() color.RGBA { return o.color() }

// Object is one named part of a model.
type Object struct {
	Name     string // The STL solid, OBJ object/group, or 3MF part name; may be empty.
	Material string // The OBJ material name, when the part has one.
	Part     int    // For 3MF, the index in Model.Parts; zero otherwise.
	Geometry nt3d.Geometry
	// Plain marks, per vertex, those the file gave no color. A viewer may
	// repaint them without disturbing the file's own colors.
	Plain []bool
}

// Model is what a mesh file holds.
type Model struct {
	// Asset preserves a reusable scene hierarchy, cameras and clips;
	// Objects remain the flat rest-pose view. glTF always supplies Asset.
	Asset    *nt3d.SceneAsset
	Format   Format
	Objects  []Object
	Warnings []string // Problems passed over, such as a missing texture.

	// What 3MF packages also say. Parts and the rest are filled even when
	// decoding fails with ErrTooManyTriangles, so a caller may choose fewer
	// parts or show the thumbnail instead; with ErrTooLarge, as far as known.
	Parts     []Part
	Unit      string            // Such as "millimeter"; empty when unstated.
	Metadata  map[string]string // The root model's metadata, by name.
	Thumbnail image.Image       // The slicer's plate picture, else the declared thumbnail.
	// Summary measures every part, chosen or not, from the file without
	// assembling it, so it is there with ErrTooManyTriangles too; nil for
	// other formats, and when a package is too large to measure.
	Summary *Summary
}

// Part is one item a 3MF build places, with whatever it holds.
type Part struct {
	Name      string // The slicer's name, else the object's, else "Object <id>".
	Triangles int
	// Bounds and Area measure the part where the build places it, as
	// Summary does.
	Bounds nt3dmath.AABB
	Area   float64
}

// Summary measures a 3MF model's whole build, every part whether chosen or
// not, as the file places it, without assembling its geometry.
type Summary struct {
	Triangles int // In every part.
	// Meshes counts the distinct mesh objects the build places, however
	// often: objects that only assemble components are not counted.
	Meshes int
	// Bounds encloses every triangle in world space: after build and
	// component transforms, and turned as Options.Up turns the objects.
	Bounds nt3dmath.AABB
	// Area is the surface area, in the file's unit (Model.Unit) squared,
	// after the transforms.
	Area float64
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
// different materials, as textured OBJ parts do, or together exceed
// nt3d.MaxMeshTriangles; add those with AddTo.
func (m *Model) Geometry() (nt3d.Geometry, error) {
	gs := make([]nt3d.Geometry, len(m.Objects))
	for i, o := range m.Objects {
		gs[i] = o.Geometry
	}
	return nt3d.MergeGeometry(gs...)
}

// AddTo adds a group node named name under parent, with one child node per
// object, named name/0, name/1, and so on. Models with Asset instead instantiate
// its hierarchy and bind its generic clips (Scene.AnimationClips).
// On failure nothing is added.
func (m *Model) AddTo(s *nt3d.Scene, parent nt3d.NodeID, name string) (nt3d.NodeID, error) {
	if m.Asset != nil {
		instance, err := m.Asset.Instantiate(s, parent, name)
		if err != nil {
			return 0, err
		}
		return instance.Root, nil
	}
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
	case ".3mf":
		return ThreeMF, true
	case ".ply":
		return PLY, true
	case ".conf":
		return Conf, true
	}
	if f, ok := registeredExtension(name); ok {
		return f, true
	}
	if bytes.HasPrefix(data, []byte("ply\n")) || bytes.HasPrefix(data, []byte("ply\r\n")) {
		return PLY, true
	}
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		// A registered format may be a zip bundle; any other zip is 3MF.
		if f, ok := registeredMagic(data); ok {
			return f, true
		}
		return ThreeMF, true
	}
	if binarySTL(data) || bytes.HasPrefix(bytes.TrimLeft(data, " \t\r\n"), []byte("solid")) {
		return STL, true
	}
	return registeredMagic(data)
}

// Decode reads a model in the given format.
func Decode(data []byte, f Format, o *Options) (*Model, error) {
	switch f {
	case STL:
		return DecodeSTL(data, o)
	case OBJ:
		return DecodeOBJ(data, o)
	case ThreeMF:
		return Decode3MF(data, o)
	case PLY:
		return DecodePLY(data, o)
	case Conf:
		return DecodeConf(data, o)
	}
	if r, ok := registeredFormat(f); ok {
		return r.decode(data, o)
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
		return fmt.Errorf("%w: model exceeds %d", ErrTooManyTriangles, b.limit)
	}
	b.used += n
	return nil
}
