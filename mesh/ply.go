// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image/color"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
)

// PLY is the Stanford polygon format.
const PLY Format = "ply"

// maxPLYHeader bounds the header a decoder reads before giving up.
const maxPLYHeader = 64 << 10

// plyProperty is a property of an element: a scalar, or a list with a count.
type plyProperty struct {
	name        string
	kind, count string // count is set for lists.
}

type plyElement struct {
	name       string
	count      int
	properties []plyProperty
}

// plySize is the bytes a binary scalar takes, or 0 for an unknown type.
func plySize(kind string) int {
	switch kind {
	case "char", "int8", "uchar", "uint8":
		return 1
	case "short", "int16", "ushort", "uint16":
		return 2
	case "int", "int32", "uint", "uint32", "float", "float32":
		return 4
	case "double", "float64":
		return 8
	}
	return 0
}

// plyReader reads scalars from the body, in the file's encoding.
type plyReader struct {
	data  []byte
	at    int
	order binary.ByteOrder // Nil for ASCII.
	line  int
}

func (r *plyReader) errorf(format string, a ...any) error {
	if r.order == nil {
		return fmt.Errorf("nt3dmesh: PLY line %d: %s", r.line, fmt.Sprintf(format, a...))
	}
	return fmt.Errorf("nt3dmesh: PLY byte %d: %s", r.at, fmt.Sprintf(format, a...))
}

// scalar reads one value of a type as a float64.
func (r *plyReader) scalar(kind string) (float64, error) {
	if r.order == nil {
		for r.at < len(r.data) && isSpace(r.data[r.at]) {
			if r.data[r.at] == '\n' {
				r.line++
			}
			r.at++
		}
		start := r.at
		for r.at < len(r.data) && !isSpace(r.data[r.at]) {
			r.at++
		}
		if start == r.at {
			return 0, r.errorf("expected a number, found end of file")
		}
		v, err := strconv.ParseFloat(string(r.data[start:r.at]), 64)
		if err != nil {
			return 0, r.errorf("bad number %q", r.data[start:r.at])
		}
		return v, nil
	}
	n := plySize(kind)
	if r.at+n > len(r.data) {
		return 0, r.errorf("truncated")
	}
	b := r.data[r.at : r.at+n]
	r.at += n
	switch kind {
	case "char", "int8":
		return float64(int8(b[0])), nil
	case "uchar", "uint8":
		return float64(b[0]), nil
	case "short", "int16":
		return float64(int16(r.order.Uint16(b))), nil
	case "ushort", "uint16":
		return float64(r.order.Uint16(b)), nil
	case "int", "int32":
		return float64(int32(r.order.Uint32(b))), nil
	case "uint", "uint32":
		return float64(r.order.Uint32(b)), nil
	case "float", "float32":
		return float64(math.Float32frombits(r.order.Uint32(b))), nil
	default:
		return math.Float64frombits(r.order.Uint64(b)), nil
	}
}

// plyVertex is what a PLY vertex can say.
type plyVertex struct {
	position nt3dmath.Vec3
	normal   *nt3dmath.Vec3
	color    *color.RGBA
	uv       *nt3d.UV
}

// DecodePLY reads Stanford PLY, ASCII or binary in either byte order. It
// reads vertex positions, normals, colors (bytes, or floats in [0, 1]), and
// texture coordinates (u v, s t, or texture_u texture_v); faces as polygons
// of any size, concave ones ear-clipped, with face colors and per-corner
// texcoord lists; and the texture a "comment TextureFile" names, through
// Options.Open, as MeshLab writes it. Other elements, such as a scanner's
// range grid, are skipped. Faces without vertex normals are flat. A raw
// scan with a range grid (obj_info num_cols and num_rows, as the Stanford
// scans have) becomes a surface joining neighboring samples, leaving out
// triangles that would bridge a jump in depth; one without is a cloud of
// points, split into pieces past nt3d.MaxPoints. 3DGS attributes select DecodeGaussianPLY.
func DecodePLY(data []byte, o *Options) (*Model, error) {
	elements, format, comments, body, err := plyHeader(data)
	if err != nil {
		return nil, err
	}
	if gaussianPLY(elements) {
		return DecodeGaussianPLY(data, o)
	}
	r := &plyReader{data: data, at: body, line: bytes.Count(data[:body], []byte("\n")) + 1}
	switch format {
	case "ascii":
	case "binary_little_endian":
		r.order = binary.LittleEndian
	case "binary_big_endian":
		r.order = binary.BigEndian
	default:
		return nil, fmt.Errorf("nt3dmesh: PLY format %q is not ascii or binary", format)
	}
	m := &Model{Format: PLY}
	budget := &triangleBudget{limit: o.limit()}
	s := &faceObject{budget: budget, format: "PLY"}
	// A texture the header names colors the faces that have UVs.
	for _, c := range comments {
		if name, ok := strings.CutPrefix(c, "TextureFile "); ok {
			name = strings.TrimSpace(name)
			if o == nil || o.Open == nil {
				m.Warnings = append(m.Warnings, fmt.Sprintf("texture %q was not read: no side-file access", name))
				break
			}
			if data, err := o.Open(name); err != nil {
				m.Warnings = append(m.Warnings, fmt.Sprintf("texture %q: %v", name, err))
			} else if img, err := decodeTexture(data, color.RGBA{255, 255, 255, 255}); err != nil {
				m.Warnings = append(m.Warnings, fmt.Sprintf("texture %q: %v", name, err))
			} else if t, err := nt3d.NewTexture(img); err == nil {
				s.Geometry.Material = &nt3d.Material{Texture: t, Repeat: true}
				s.Material = name
			}
			break
		}
	}
	var vertices []plyVertex
	var grid *rangeGrid
	for _, e := range elements {
		// Each element takes at least a byte a value; a count the rest of
		// the file cannot hold is refused before anything is allocated.
		least := 0
		for _, p := range e.properties {
			if r.order == nil {
				least += 2
			} else if p.count != "" {
				least += plySize(p.count)
			} else {
				least += plySize(p.kind)
			}
		}
		// An element without properties still takes a byte, so that its
		// count cannot run on while reading nothing.
		if least = max(least, 1); e.count > (len(data)-r.at)/least+1 {
			return nil, fmt.Errorf("nt3dmesh: PLY claims %d %s, more than the file holds", e.count, e.name)
		}
		switch e.name {
		case "vertex":
			if vertices, err = plyVertices(r, e); err != nil {
				return nil, err
			}
		case "face":
			if err := plyFaces(r, e, vertices, s, o); err != nil {
				return nil, err
			}
		case "range_grid":
			if grid, err = plyRangeGrid(r, e, comments); err != nil {
				return nil, err
			}
		default:
			for range e.count {
				if err := plySkip(r, e); err != nil {
					return nil, err
				}
			}
		}
	}
	// A range scan's grid joins neighboring samples into a surface.
	if grid != nil && len(s.Geometry.Indices) == 0 {
		if err := grid.mesh(vertices, s, o); err != nil {
			return nil, err
		}
	}
	m.Objects = s.objects()
	if m.Triangles() == 0 {
		// A scan without faces is a point cloud, which nt3d draws as points.
		if len(vertices) == 0 {
			return nil, errors.New("nt3dmesh: PLY contains no faces or vertices")
		}
		var g nt3d.Geometry
		for _, v := range vertices {
			c := o.color()
			if v.color != nil {
				c = *v.color
			}
			g.Points = append(g.Points, nt3d.Point{Position: v.position, Radius: 1, Color: c})
		}
		g.Bounds = nt3d.GeometryBounds(g)
		m.Objects = PointPieces(Object{Geometry: g})
	}
	for i := range m.Objects {
		g := &m.Objects[i].Geometry
		if g.Material != nil && len(g.UV) != len(g.Vertices) {
			g.Material = nil // Texture named but faces lack UVs.
		}
		if err := g.Validate(); err != nil {
			return nil, err
		}
	}
	o.orient(m)
	return m, nil
}

// plyHeader reads the header: its elements, format, and comments, and where
// the body starts.
func plyHeader(data []byte) (elements []plyElement, format string, comments []string, body int, err error) {
	// The header ends at a line of end_header alone, not at the words in a
	// comment. Its line ends in \n, or \r\n from some writers.
	end := -1
	for at := 0; at < min(len(data), maxPLYHeader); {
		line, next := data[at:], len(data)
		if i := bytes.IndexByte(line, '\n'); i >= 0 {
			line, next = line[:i], at+i+1
		}
		if string(bytes.TrimSpace(line)) == "end_header" {
			end, body = at, next
			break
		}
		at = next
	}
	if !bytes.HasPrefix(data, []byte("ply")) || end < 0 {
		return nil, "", nil, 0, errors.New("nt3dmesh: not a PLY file, or its header is too long")
	}
	for n, line := range strings.Split(string(data[:end]), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		bad := func(what string) error { return fmt.Errorf("nt3dmesh: PLY header line %d: %s", n+1, what) }
		switch fields[0] {
		case "format":
			if len(fields) < 2 {
				return nil, "", nil, 0, bad("format without a type")
			}
			format = fields[1]
		case "comment", "obj_info":
			comments = append(comments, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), fields[0])))
		case "element":
			if len(fields) != 3 {
				return nil, "", nil, 0, bad("element needs a name and count")
			}
			count, err := strconv.Atoi(fields[2])
			if err != nil || count < 0 {
				return nil, "", nil, 0, bad("bad element count")
			}
			elements = append(elements, plyElement{name: fields[1], count: count})
		case "property":
			if len(elements) == 0 {
				return nil, "", nil, 0, bad("property before any element")
			}
			e := &elements[len(elements)-1]
			switch {
			case len(fields) == 5 && fields[1] == "list" && plySize(fields[2]) > 0 && plySize(fields[3]) > 0:
				e.properties = append(e.properties, plyProperty{name: fields[4], kind: fields[3], count: fields[2]})
			case len(fields) == 3 && plySize(fields[1]) > 0:
				e.properties = append(e.properties, plyProperty{name: fields[2], kind: fields[1]})
			default:
				return nil, "", nil, 0, bad("bad property")
			}
		}
	}
	return elements, format, comments, body, nil
}

// plyValues reads one element's values: scalars by name, lists by name.
func plyValues(r *plyReader, e plyElement, scalars map[string]float64, lists map[string][]float64) error {
	for _, p := range e.properties {
		if p.count == "" {
			v, err := r.scalar(p.kind)
			if err != nil {
				return err
			}
			scalars[p.name] = v
			continue
		}
		n, err := r.scalar(p.count)
		if err != nil {
			return err
		}
		if n < 0 || n > 1<<16 || n != math.Trunc(n) {
			return r.errorf("bad list length %v", n)
		}
		list := lists[p.name][:0]
		for range int(n) {
			v, err := r.scalar(p.kind)
			if err != nil {
				return err
			}
			list = append(list, v)
		}
		lists[p.name] = list
	}
	return nil
}

func plySkip(r *plyReader, e plyElement) error {
	return plyValues(r, e, map[string]float64{}, map[string][]float64{})
}

// channel reads a color channel, a byte or a float in [0, 1].
func channel(e plyElement, name string, v float64) uint8 {
	for _, p := range e.properties {
		if p.name == name && (p.kind == "float" || p.kind == "float32" || p.kind == "double" || p.kind == "float64") {
			v *= 255
		}
	}
	return uint8(math.Round(max(0, min(255, v))))
}

func plyVertices(r *plyReader, e plyElement) ([]plyVertex, error) {
	has := map[string]bool{}
	for _, p := range e.properties {
		has[p.name] = true
	}
	if !has["x"] || !has["y"] || !has["z"] {
		return nil, errors.New("nt3dmesh: PLY vertices lack x, y, or z")
	}
	// Texture coordinates go by several names.
	var u, v string
	for _, pair := range [][2]string{{"u", "v"}, {"s", "t"}, {"texture_u", "texture_v"}, {"texture_s", "texture_t"}} {
		if has[pair[0]] && has[pair[1]] {
			u, v = pair[0], pair[1]
			break
		}
	}
	scalars, lists := map[string]float64{}, map[string][]float64{}
	out := make([]plyVertex, e.count)
	for i := range out {
		if err := plyValues(r, e, scalars, lists); err != nil {
			return nil, err
		}
		x, y, z := float32(scalars["x"]), float32(scalars["y"]), float32(scalars["z"])
		if !validCoordinate(x, y, z) {
			return nil, r.errorf("vertex %d is non-finite or exceeds coordinate limits", i)
		}
		out[i].position = nt3dmath.Vec3{X: x, Y: y, Z: z}
		if has["nx"] && has["ny"] && has["nz"] {
			n := nt3dmath.Vec3{X: float32(scalars["nx"]), Y: float32(scalars["ny"]), Z: float32(scalars["nz"])}.Normalize()
			if validCoordinate(n.X, n.Y, n.Z) {
				out[i].normal = &n
			}
		}
		if has["red"] && has["green"] && has["blue"] {
			out[i].color = &color.RGBA{channel(e, "red", scalars["red"]), channel(e, "green", scalars["green"]), channel(e, "blue", scalars["blue"]), 255}
		}
		if u != "" {
			// PLY, like OBJ, counts v up from the bottom.
			out[i].uv = &nt3d.UV{U: float32(scalars[u]), V: 1 - float32(scalars[v])}
		}
	}
	return out, nil
}

func plyFaces(r *plyReader, e plyElement, vertices []plyVertex, s *faceObject, o *Options) error {
	indices := "vertex_indices"
	for _, p := range e.properties {
		if p.name == "vertex_index" {
			indices = p.name
		}
	}
	faceColor := false
	for _, p := range e.properties {
		faceColor = faceColor || p.name == "red"
	}
	scalars, lists := map[string]float64{}, map[string][]float64{}
	// UVs are kept when the file has them, textured or not, as for OBJ:
	// fur, and textures applied later, need them.
	mapped := s.Geometry.Material != nil || len(vertices) > 0 && vertices[0].uv != nil
	for _, p := range e.properties {
		mapped = mapped || p.name == "texcoord"
	}
	for i := range e.count {
		if err := plyValues(r, e, scalars, lists); err != nil {
			return err
		}
		corners := lists[indices]
		if len(corners) < 3 {
			return r.errorf("face %d has fewer than three corners", i)
		}
		points := make([]nt3dmath.Vec3, len(corners))
		for k, c := range corners {
			if c < 0 || int(c) >= len(vertices) || c != math.Trunc(c) {
				return r.errorf("face %d names vertex %v of %d", i, c, len(vertices))
			}
			points[k] = vertices[int(c)].position
		}
		// MeshLab writes texture coordinates per face corner.
		texcoord := lists["texcoord"]
		for _, t := range triangulate(points) {
			var v [3]nt3dmath.Vec3
			var normals [3]nt3dmath.Vec3
			var shades [3]color.RGBA
			var uv [3]nt3d.UV
			smooth, plain := true, true
			for k, c := range t {
				p := vertices[int(corners[c])]
				v[k] = p.position
				if p.normal != nil {
					normals[k] = *p.normal
				} else {
					smooth = false
				}
				shades[k] = o.color()
				switch {
				case p.color != nil:
					shades[k], plain = *p.color, false
				case faceColor:
					shades[k], plain = color.RGBA{channel(e, "red", scalars["red"]), channel(e, "green", scalars["green"]), channel(e, "blue", scalars["blue"]), 255}, false
				}
				switch {
				case len(texcoord) == 2*len(corners):
					uv[k] = nt3d.UV{U: float32(texcoord[2*c]), V: 1 - float32(texcoord[2*c+1])}
				case p.uv != nil:
					uv[k] = *p.uv
				}
			}
			var n *[3]nt3dmath.Vec3
			if smooth {
				n = &normals
			}
			var corner *[3]nt3d.UV
			if mapped {
				corner = &uv
			}
			if err := s.addNormals(v, n, shades, plain, corner); err != nil {
				return err
			}
		}
	}
	return nil
}

// EncodePLY writes the triangles of g as binary little-endian PLY in local
// coordinates, sharing vertices as g does: positions, normals, colors, and
// texture coordinates when g has them. Points, boxes, lines, and arrows are
// not written.
func EncodePLY(w io.Writer, g nt3d.Geometry) error {
	if err := g.Validate(); err != nil {
		return err
	}
	if len(g.Indices) == 0 {
		return errors.New("nt3dmesh: PLY needs at least one triangle")
	}
	uv := len(g.UV) == len(g.Vertices)
	b := bufio.NewWriter(w)
	fmt.Fprintf(b, "ply\nformat binary_little_endian 1.0\ncomment written by nt3d\nelement vertex %d\n", len(g.Vertices))
	fmt.Fprint(b, "property float x\nproperty float y\nproperty float z\nproperty float nx\nproperty float ny\nproperty float nz\n")
	fmt.Fprint(b, "property uchar red\nproperty uchar green\nproperty uchar blue\n")
	if uv {
		fmt.Fprint(b, "property float s\nproperty float t\n")
	}
	fmt.Fprintf(b, "element face %d\nproperty list uchar int vertex_indices\nend_header\n", len(g.Indices)/3)
	put := func(v float32) { _ = binary.Write(b, binary.LittleEndian, v) }
	for i, v := range g.Vertices {
		put(v.Position.X)
		put(v.Position.Y)
		put(v.Position.Z)
		put(v.Normal.X)
		put(v.Normal.Y)
		put(v.Normal.Z)
		b.Write([]byte{v.Color.R, v.Color.G, v.Color.B})
		if uv {
			put(g.UV[i].U)
			put(1 - g.UV[i].V)
		}
	}
	for i := 0; i < len(g.Indices); i += 3 {
		b.WriteByte(3)
		for _, k := range g.Indices[i : i+3] {
			_ = binary.Write(b, binary.LittleEndian, int32(k))
		}
	}
	return b.Flush()
}

// rangeGrid is a scanner's grid of samples: rows of columns, each naming
// the vertex sampled there, or -1 where the scanner saw nothing.
type rangeGrid struct {
	cols, rows int
	cells      []int32
}

// plyRangeGrid reads a range_grid element, when the header's obj_info gives
// its size; otherwise it skips it.
func plyRangeGrid(r *plyReader, e plyElement, comments []string) (*rangeGrid, error) {
	g := &rangeGrid{}
	for _, c := range comments {
		fields := strings.Fields(c)
		if len(fields) == 2 && fields[0] == "num_cols" {
			g.cols, _ = strconv.Atoi(fields[1])
		}
		if len(fields) == 2 && fields[0] == "num_rows" {
			g.rows, _ = strconv.Atoi(fields[1])
		}
	}
	scalars, lists := map[string]float64{}, map[string][]float64{}
	if g.cols < 2 || g.rows < 2 || g.cols*g.rows != e.count {
		for range e.count {
			if err := plyValues(r, e, scalars, lists); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	g.cells = make([]int32, e.count)
	for i := range g.cells {
		if err := plyValues(r, e, scalars, lists); err != nil {
			return nil, err
		}
		g.cells[i] = -1
		for _, list := range lists {
			if len(list) > 0 && list[0] >= 0 && list[0] == math.Trunc(list[0]) {
				g.cells[i] = int32(list[0])
			}
		}
	}
	return g, nil
}

// mesh joins each cell's samples with the next row's and column's into
// triangles facing the scanner, as the Stanford scans' rows run. A triangle with an edge over four times the
// grid's usual spacing would bridge a jump in depth, from one surface to
// another behind it, and is left out.
func (g *rangeGrid) mesh(vertices []plyVertex, s *faceObject, o *Options) error {
	at := func(r, c int) int {
		v := int(g.cells[r*g.cols+c])
		if v >= len(vertices) {
			return -1
		}
		return v
	}
	// The usual spacing: the median distance between neighbors in a row.
	var gaps []float64
	for r := 0; r < g.rows; r++ {
		for c := 0; c+1 < g.cols; c++ {
			if a, b := at(r, c), at(r, c+1); a >= 0 && b >= 0 {
				d := vertices[a].position.Sub(vertices[b].position)
				gaps = append(gaps, math.Sqrt(float64(d.Dot(d))))
			}
		}
	}
	if len(gaps) == 0 {
		return nil
	}
	slices.Sort(gaps)
	limit := float32(4 * gaps[len(gaps)/2])
	near := func(a, b int) bool {
		d := vertices[a].position.Sub(vertices[b].position)
		return d.Dot(d) <= limit*limit
	}
	triangle := func(a, b, c int) error {
		if a < 0 || b < 0 || c < 0 || !near(a, b) || !near(b, c) || !near(c, a) {
			return nil
		}
		v := [3]nt3dmath.Vec3{vertices[a].position, vertices[b].position, vertices[c].position}
		var shades [3]color.RGBA
		plain := true
		for k, i := range []int{a, b, c} {
			shades[k] = o.color()
			if vertices[i].color != nil {
				shades[k], plain = *vertices[i].color, false
			}
		}
		return s.add(v, shades, plain, nil)
	}
	for r := 0; r+1 < g.rows; r++ {
		for c := 0; c+1 < g.cols; c++ {
			p, q, u, w := at(r, c), at(r, c+1), at(r+1, c), at(r+1, c+1)
			// Two triangles a cell, or one where a corner is missing.
			switch {
			case p >= 0 && w >= 0:
				if err := triangle(p, w, u); err != nil {
					return err
				}
				if err := triangle(p, q, w); err != nil {
					return err
				}
			case q >= 0 && u >= 0:
				if err := triangle(p, q, u); err != nil {
					return err
				}
				if err := triangle(q, w, u); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
