// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg" // Decoders for OBJ textures.
	_ "image/png"
	"io"
	"path"
	"strconv"
	"strings"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
)

// objMaterial is what a material library says of one material.
type objMaterial struct {
	color   *color.RGBA // Kd.
	texture string      // map_Kd, relative to the model.
	loaded  *nt3d.Material
	tried   bool
}

// objPart accumulates the faces of one object or group in one material.
type objPart struct {
	Object
	shared map[[3]int]uint32 // Vertices whose corners carry normals are shared.
	hasUV  bool
}

type objDecoder struct {
	o         *Options
	m         *Model
	budget    triangleBudget
	line      int
	positions []nt3dmath.Vec3
	colors    []*color.RGBA // Per position, from the vertex-color extension.
	uvs       []nt3d.UV
	normals   []nt3dmath.Vec3
	materials map[string]*objMaterial
	name      string
	material  string
	parts     map[[2]string]*objPart
	order     []*objPart
	warned    map[string]bool
}

func (d *objDecoder) errorf(format string, a ...any) error {
	return fmt.Errorf("nt3dmesh: OBJ line %d: %s", d.line, fmt.Sprintf(format, a...))
}
func (d *objDecoder) warn(format string, a ...any) {
	message := fmt.Sprintf(format, a...)
	if !d.warned[message] {
		d.warned[message] = true
		d.m.Warnings = append(d.m.Warnings, message)
	}
}

// DecodeOBJ reads Wavefront OBJ: vertices, with the common r g b extension,
// texture coordinates, normals, polygons (split into triangle fans), and
// polylines. Each object or group becomes an object per material it uses.
// Material libraries give diffuse colors (Kd) and textures (map_Kd, PNG or
// JPEG) through Options.Open; transparent texels are flattened over Kd.
// Problems with side files are warnings, not errors. Free-form geometry is ignored.
func DecodeOBJ(data []byte, o *Options) (*Model, error) {
	d := &objDecoder{o: o, m: &Model{Format: OBJ}, budget: triangleBudget{limit: o.limit()}, materials: map[string]*objMaterial{}, parts: map[[2]string]*objPart{}, warned: map[string]bool{}}
	err := eachLine(data, func(n int, fields []string) error {
		d.line = n
		return d.statement(fields)
	})
	if err != nil {
		return nil, err
	}
	for _, p := range d.order {
		if len(p.Geometry.Indices) == 0 && len(p.Geometry.Lines) == 0 {
			continue
		}
		material := d.materials[p.Material]
		g := &p.Geometry
		if material != nil && material.texture != "" {
			g.Material = d.texture(material)
		}
		if !p.hasUV && (g.Material == nil || g.Material.Texture == nil) {
			g.UV = nil
		}
		g.Bounds = nt3d.GeometryBounds(*g)
		if err := g.Validate(); err != nil {
			return nil, err
		}
		d.m.Objects = append(d.m.Objects, p.Object)
	}
	if len(d.m.Objects) == 0 {
		return nil, errors.New("nt3dmesh: OBJ contains no faces or lines")
	}
	return d.m, nil
}

// eachLine calls fn with the fields of each statement, joining lines that end
// in a backslash and dropping comments.
func eachLine(data []byte, fn func(line int, fields []string) error) error {
	var statement []byte
	start := 0
	for n := 1; len(data) > 0; n++ {
		line := data
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = data[:i], data[i+1:]
		} else {
			data = nil
		}
		if i := bytes.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = bytes.TrimRight(line, " \t\r")
		if len(statement) == 0 {
			start = n
		}
		if bytes.HasSuffix(line, []byte(`\`)) {
			statement = append(append(statement, line[:len(line)-1]...), ' ')
			continue
		}
		statement = append(statement, line...)
		fields := strings.Fields(string(statement))
		statement = statement[:0]
		if len(fields) == 0 {
			continue
		}
		if err := fn(start, fields); err != nil {
			return err
		}
	}
	return nil
}

func (d *objDecoder) floats(fields []string, least, most int) ([]float32, error) {
	if len(fields) < least || len(fields) > most {
		return nil, d.errorf("%s needs %d to %d numbers", fields[0], least-1, most-1)
	}
	out := make([]float32, len(fields)-1)
	for i, f := range fields[1:] {
		v, err := strconv.ParseFloat(f, 32)
		if err != nil || !validCoordinate(float32(v)) {
			return nil, d.errorf("bad number %q", f)
		}
		out[i] = float32(v)
	}
	return out, nil
}

func unit8(v float32) uint8 { return uint8(min(max(v, 0), 1)*255 + .5) }

func (d *objDecoder) statement(fields []string) error {
	switch fields[0] {
	case "v":
		v, err := d.floats(fields, 4, 7)
		if err != nil {
			return err
		}
		d.positions = append(d.positions, nt3dmath.Vec3{X: v[0], Y: v[1], Z: v[2]})
		var shade *color.RGBA
		if len(v) == 6 {
			shade = &color.RGBA{unit8(v[3]), unit8(v[4]), unit8(v[5]), 255}
		}
		d.colors = append(d.colors, shade)
	case "vt":
		v, err := d.floats(fields, 2, 4)
		if err != nil {
			return err
		}
		if len(v) == 1 {
			v = append(v, 0)
		}
		// OBJ counts V up from the bottom; nt3d counts down from the top.
		d.uvs = append(d.uvs, nt3d.UV{U: v[0], V: 1 - v[1]})
	case "vn":
		v, err := d.floats(fields, 4, 4)
		if err != nil {
			return err
		}
		d.normals = append(d.normals, nt3dmath.Vec3{X: v[0], Y: v[1], Z: v[2]}.Normalize())
	case "f", "fo":
		return d.face(fields[1:])
	case "l":
		return d.polyline(fields[1:])
	case "o", "g":
		d.name = strings.Join(fields[1:], " ")
	case "usemtl":
		d.material = strings.Join(fields[1:], " ")
		if _, ok := d.materials[d.material]; !ok && d.material != "" {
			d.warn("material %q is not defined", d.material)
		}
	case "mtllib":
		for _, name := range fields[1:] {
			d.library(name)
		}
	case "s", "p", "vp", "mg", "lod", "bevel", "c_interp", "d_interp", "shadow_obj", "trace_obj", "usemap", "maplib":
	case "cstype", "deg", "bmat", "step", "curv", "curv2", "surf", "parm", "trim", "hole", "scrv", "sp", "end", "con":
		d.warn("free-form geometry is not supported and was skipped")
	default:
		d.warn("unknown OBJ statement %q was skipped", fields[0])
	}
	return nil
}

func (d *objDecoder) part() *objPart {
	key := [2]string{d.name, d.material}
	p := d.parts[key]
	if p == nil {
		p = &objPart{Object: Object{Name: d.name, Material: d.material}, shared: map[[3]int]uint32{}}
		d.parts[key] = p
		d.order = append(d.order, p)
	}
	return p
}

// index resolves a one-based or negative (relative) OBJ index.
func (d *objDecoder) index(word string, count int, kind string) (int, error) {
	i, err := strconv.Atoi(word)
	switch {
	case err != nil || i == 0:
		return 0, d.errorf("bad %s index %q", kind, word)
	case i < 0:
		i += count
	default:
		i--
	}
	if i < 0 || i >= count {
		return 0, d.errorf("%s index %s out of range", kind, word)
	}
	return i, nil
}

// corner resolves "v", "v/vt", "v//vn", or "v/vt/vn"; absent parts are -1.
func (d *objDecoder) corner(word string) (c [3]int, err error) {
	parts := strings.Split(word, "/")
	if len(parts) > 3 {
		return c, d.errorf("bad face vertex %q", word)
	}
	c = [3]int{-1, -1, -1}
	counts := []int{len(d.positions), len(d.uvs), len(d.normals)}
	for k, kind := range []string{"vertex", "texture", "normal"} {
		if k < len(parts) && parts[k] != "" {
			if c[k], err = d.index(parts[k], counts[k], kind); err != nil {
				return c, err
			}
		} else if k == 0 {
			return c, d.errorf("face vertex %q has no position", word)
		}
	}
	return c, nil
}

// shade is a vertex's color: its own, else its material's, else the default.
func (d *objDecoder) shade(position int) (color.RGBA, bool) {
	if c := d.colors[position]; c != nil {
		return *c, false
	}
	if m := d.materials[d.material]; m != nil && m.color != nil {
		return *m.color, false
	}
	return d.o.color(), true
}

func (d *objDecoder) vertex(p *objPart, c [3]int, flat *nt3dmath.Vec3) uint32 {
	if flat == nil {
		if i, ok := p.shared[c]; ok {
			return i
		}
	}
	g := &p.Geometry
	i := uint32(len(g.Vertices))
	shade, plain := d.shade(c[0])
	v := nt3d.Vertex{Position: d.positions[c[0]], Color: shade}
	if flat != nil {
		v.Normal = *flat
	} else {
		v.Normal = d.normals[c[2]]
		p.shared[c] = i
	}
	g.Vertices = append(g.Vertices, v)
	p.Plain = append(p.Plain, plain)
	uv := nt3d.UV{}
	if c[1] >= 0 {
		uv, p.hasUV = d.uvs[c[1]], true
	}
	g.UV = append(g.UV, uv)
	return i
}

func (d *objDecoder) face(words []string) error {
	if len(words) < 3 {
		return d.errorf("a face needs at least three vertices")
	}
	if err := d.budget.take(len(words) - 2); err != nil {
		return err
	}
	corners := make([][3]int, len(words))
	for i, w := range words {
		var err error
		if corners[i], err = d.corner(w); err != nil {
			return err
		}
	}
	p := d.part()
	for k := 1; k+1 < len(corners); k++ {
		tri := [3][3]int{corners[0], corners[k], corners[k+1]}
		var flat *nt3dmath.Vec3
		if tri[0][2] < 0 || tri[1][2] < 0 || tri[2][2] < 0 {
			a, b, c := d.positions[tri[0][0]], d.positions[tri[1][0]], d.positions[tri[2][0]]
			n := b.Sub(a).Cross(c.Sub(a)).Normalize()
			flat = &n
		}
		for _, c := range tri {
			p.Geometry.Indices = append(p.Geometry.Indices, d.vertex(p, c, flat))
		}
	}
	return nil
}

func (d *objDecoder) polyline(words []string) error {
	if len(words) < 2 {
		return d.errorf("a line needs at least two vertices")
	}
	p := d.part()
	var last nt3d.Vertex
	for i, w := range words {
		c, err := d.corner(w)
		if err != nil {
			return err
		}
		shade, _ := d.shade(c[0])
		v := nt3d.Vertex{Position: d.positions[c[0]], Color: shade}
		if i > 0 {
			if len(p.Geometry.Lines) >= 2*512*512 {
				return d.errorf("too many line segments")
			}
			p.Geometry.Lines = append(p.Geometry.Lines, last, v)
		}
		last = v
	}
	return nil
}

// library reads a material library, warning about whatever it cannot use.
func (d *objDecoder) library(name string) {
	if d.o == nil || d.o.Open == nil {
		d.warn("material library %q was not read: no side-file access", name)
		return
	}
	data, err := d.o.Open(name)
	if err != nil {
		d.warn("material library %q: %v", name, err)
		return
	}
	var current *objMaterial
	_ = eachLine(data, func(_ int, fields []string) error {
		switch fields[0] {
		case "newmtl":
			current = &objMaterial{}
			d.materials[strings.Join(fields[1:], " ")] = current
		case "Kd":
			if current != nil && len(fields) >= 4 {
				var c [3]float64
				for i := range c {
					c[i], err = strconv.ParseFloat(fields[i+1], 32)
					if err != nil {
						return nil
					}
				}
				current.color = &color.RGBA{unit8(float32(c[0])), unit8(float32(c[1])), unit8(float32(c[2])), 255}
			}
		case "map_Kd":
			if current != nil && len(fields) >= 2 {
				// Options such as -s u v precede the file name.
				texture := strings.Join(fields[1:], " ")
				if strings.HasPrefix(fields[1], "-") {
					texture = fields[len(fields)-1]
				}
				current.texture = path.Join(path.Dir(strings.ReplaceAll(name, `\`, "/")), strings.ReplaceAll(texture, `\`, "/"))
			}
		}
		return nil
	})
}

// maxTextureSide matches nt3d.NewTexture.
const maxTextureSide = 8192

// texture loads a material's texture once, falling back to its color.
func (d *objDecoder) texture(m *objMaterial) *nt3d.Material {
	if m.tried {
		return m.loaded
	}
	m.tried = true
	data, err := d.o.Open(m.texture)
	if err != nil {
		d.warn("texture %q: %v", m.texture, err)
		return nil
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width > maxTextureSide || config.Height > maxTextureSide {
		d.warn("texture %q is not a PNG or JPEG of at most %d pixels a side", m.texture, maxTextureSide)
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		d.warn("texture %q: %v", m.texture, err)
		return nil
	}
	background := color.RGBA{255, 255, 255, 255}
	if m.color != nil {
		background = *m.color
	}
	opaque := image.NewRGBA(img.Bounds())
	draw.Draw(opaque, opaque.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)
	draw.Draw(opaque, opaque.Bounds(), img, img.Bounds().Min, draw.Over)
	texture, err := nt3d.NewTexture(opaque)
	if err != nil {
		d.warn("texture %q: %v", m.texture, err)
		return nil
	}
	m.loaded = &nt3d.Material{Texture: texture}
	return m.loaded
}

// EncodeOBJ writes the triangles and lines of g as OBJ in local coordinates.
// Vertex colors use the r g b extension; texture coordinates and normals are
// written when present. Materials, points, boxes, and arrows are not written.
func EncodeOBJ(w io.Writer, g nt3d.Geometry) error {
	if err := g.Validate(); err != nil {
		return err
	}
	b := bufio.NewWriter(w)
	number := func(v float32) string { return strconv.FormatFloat(float64(v), 'g', -1, 32) }
	channel := func(c uint8) string { return strconv.FormatFloat(float64(c)/255, 'g', 4, 64) }
	fmt.Fprintln(b, "# OBJ written by nt3d")
	for _, vs := range [][]nt3d.Vertex{g.Vertices, g.Lines} {
		for _, v := range vs {
			p, c := v.Position, v.Color
			fmt.Fprintln(b, "v", number(p.X), number(p.Y), number(p.Z), channel(c.R), channel(c.G), channel(c.B))
		}
	}
	uv := len(g.UV) == len(g.Vertices) && len(g.UV) != 0
	if uv {
		for _, t := range g.UV {
			fmt.Fprintln(b, "vt", number(t.U), number(1-t.V))
		}
	}
	for _, v := range g.Vertices {
		n := v.Normal
		fmt.Fprintln(b, "vn", number(n.X), number(n.Y), number(n.Z))
	}
	for i := 0; i < len(g.Indices); i += 3 {
		b.WriteString("f")
		for _, k := range g.Indices[i : i+3] {
			if uv {
				fmt.Fprintf(b, " %d/%d/%d", k+1, k+1, k+1)
			} else {
				fmt.Fprintf(b, " %d//%d", k+1, k+1)
			}
		}
		b.WriteString("\n")
	}
	for i := 0; i < len(g.Lines); i += 2 {
		base := len(g.Vertices) + i + 1
		fmt.Fprintln(b, "l", base, base+1)
	}
	return b.Flush()
}
