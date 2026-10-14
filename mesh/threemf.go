// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import (
	"archive/zip"
	"bufio"
	"bytes"
	"cmp"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"path"
	"strconv"
	"strings"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
)

const (
	rootModel3MF   = "3D/3dmodel.model"
	max3MFDepth    = 16      // Components within components.
	max3MFVisits   = 1 << 20 // Objects a build may place, counting every component.
	max3MFEntries  = 4096    // Entries a package may have.
	max3MFCounted  = 64 << 20
	zipEndSize     = 22     // The fixed part of a zip archive's end record.
	zipCommentMax  = 0xFFFF // The longest comment the end record may carry.
	zipSixtyFour   = 1 << 30
	thumbnailLimit = 8192
)

// zipEntries is the number of entries an archive's end record claims, found
// as the zip reader finds it. zip.NewReader records every entry before they
// can be counted, and a small archive's directory can name millions.
func zipEntries(data []byte) int {
	tail := data[len(data)-min(len(data), zipEndSize+zipCommentMax):]
	for i := len(tail) - zipEndSize; i >= 0; i-- {
		if !bytes.Equal(tail[i:i+4], []byte("PK\x05\x06")) {
			continue
		}
		if comment := int(binary.LittleEndian.Uint16(tail[i+20:])); i+zipEndSize+comment > len(tail) {
			continue
		}
		if count := int(binary.LittleEndian.Uint16(tail[i+10:])); count != 0xFFFF {
			return count
		}
		return zipSixtyFour
	}
	return 0
}

// package3MF reads parts of a 3MF archive as they are asked for, within a
// budget: an archive of a few kilobytes can unpack to gigabytes.
type package3MF struct {
	files  map[string]*zip.File
	budget int64
	limit  int64
	parts  map[string]*part3MF
}

func open3MF(data []byte, limit int64) (*package3MF, error) {
	if zipEntries(data) > max3MFEntries {
		return nil, fmt.Errorf("nt3dmesh: 3MF has more than %d entries", max3MFEntries)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("nt3dmesh: 3MF: %w", err)
	}
	p := &package3MF{files: map[string]*zip.File{}, budget: limit, limit: limit, parts: map[string]*part3MF{}}
	for _, f := range archive.File {
		p.files[strings.ToLower(f.Name)] = f
	}
	return p, nil
}

func key3MF(name string) string { return strings.ToLower(strings.TrimPrefix(name, "/")) }

func (p *package3MF) has(name string) bool { return p.files[key3MF(name)] != nil }

func (p *package3MF) read(name string) ([]byte, error) {
	f := p.files[key3MF(name)]
	if f == nil {
		return nil, fmt.Errorf("nt3dmesh: 3MF lacks %s", name)
	}
	r, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("nt3dmesh: 3MF %s: %w", name, err)
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, p.budget+1))
	if err != nil {
		return nil, fmt.Errorf("nt3dmesh: 3MF %s: %w", name, err)
	}
	if p.budget -= int64(len(data)); p.budget < 0 {
		return nil, fmt.Errorf("%w: 3MF unpacks to more than %d MiB", ErrTooLarge, p.limit>>20)
	}
	return data, nil
}

// matrix3MF is the format's 4×3 transform for row vectors: three rows turn
// and scale, and the fourth moves.
type matrix3MF [12]float64

var identity3MF = matrix3MF{1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0}

func (m matrix3MF) apply(v [3]float64) nt3dmath.Vec3 {
	x, y, z := v[0], v[1], v[2]
	return nt3dmath.Vec3{X: float32(x*m[0] + y*m[3] + z*m[6] + m[9]), Y: float32(x*m[1] + y*m[4] + z*m[7] + m[10]), Z: float32(x*m[2] + y*m[5] + z*m[8] + m[11])}
}

// then is the transform that applies m and afterwards next.
func (m matrix3MF) then(next matrix3MF) (out matrix3MF) {
	for row := range 4 {
		x, y, z := m[3*row], m[3*row+1], m[3*row+2]
		for col := range 3 {
			out[3*row+col] = x*next[col] + y*next[3+col] + z*next[6+col]
			if row == 3 {
				out[3*row+col] += next[9+col]
			}
		}
	}
	return out
}

// A mirror turns faces inside out.
func (m matrix3MF) mirrors() bool {
	return m[0]*(m[4]*m[8]-m[5]*m[7])-m[1]*(m[3]*m[8]-m[5]*m[6])+m[2]*(m[3]*m[7]-m[4]*m[6]) < 0
}

type placed3MF struct {
	object, part string // The part is empty for an object of the same one.
	transform    matrix3MF
}

type triangle3MF struct {
	v     [3]uint32
	group string // Overrides the object's colors when set.
	index [3]int // p1, p2, p3: a property for each corner; -1 when unset.
	paint int    // The filament a slicer painted it, from 1; 0 unpainted.
}

// texture3MF is a texture coordinate group: coordinates into one texture.
type texture3MF struct {
	texture string // The texture2d resource's id.
	coords  []nt3d.UV
}

type object3MF struct {
	name       string
	group      string
	index      int
	vertices   [][3]float64
	triangles  []triangle3MF
	components []placed3MF
}

type part3MF struct {
	unit     string
	metadata map[string]string
	objects  map[string]*object3MF
	colors   map[string][]color.RGBA
	textures map[string]string      // Texture paths in the package, by texture2d id.
	clamped  map[string]bool        // Texture2d ids whose tile styles both clamp.
	coords   map[string]*texture3MF // By texture2dgroup id.
	build    []placed3MF
}

func (p *package3MF) part(name string) (*part3MF, error) {
	if part, ok := p.parts[key3MF(name)]; ok {
		return part, nil
	}
	data, err := p.read(name)
	if err != nil {
		return nil, err
	}
	part, err := parsePart3MF(data)
	if err != nil {
		return nil, fmt.Errorf("nt3dmesh: 3MF %s: %w", path.Base(name), err)
	}
	p.parts[key3MF(name)] = part
	return part, nil
}

// slicer3MF holds what Bambu Studio and its relatives keep beside the model:
// each filament's color, which filament each object and part takes, and the
// names they give objects. The standard has its own place for colors, which
// these slicers leave empty. Whatever is missing or malformed is passed over.
type slicer3MF struct {
	filaments []*color.RGBA
	extruders map[string]int    // By "object" and "object/part"; filaments count from 1.
	names     map[string]string // By object.
}

// color is that of the filament a part is printed in, where that is known.
func (s slicer3MF) color(object, part string) *color.RGBA {
	extruder, ok := s.extruders[object+"/"+part]
	if !ok {
		extruder = s.extruders[object]
	}
	return s.filament(extruder)
}

// filament is the color of a filament, counted from 1, where it is known.
func (s slicer3MF) filament(n int) *color.RGBA {
	if n < 1 || n > len(s.filaments) {
		return nil
	}
	return s.filaments[n-1]
}

func (p *package3MF) slicer() slicer3MF {
	s := slicer3MF{extruders: map[string]int{}, names: map[string]string{}}
	if data, err := p.read("Metadata/project_settings.config"); err == nil {
		var project struct {
			Colors []any `json:"filament_colour"`
		}
		if json.Unmarshal(data, &project) == nil {
			for _, c := range project.Colors {
				var shade *color.RGBA
				if c, ok := color3MF(fmt.Sprint(c)); ok {
					shade = &c
				}
				s.filaments = append(s.filaments, shade)
			}
		}
	}
	data, err := p.read("Metadata/model_settings.config")
	if err != nil {
		return s
	}
	d := xml.NewDecoder(bytes.NewReader(data))
	var object, part string
	for {
		token, err := d.Token()
		if err != nil {
			return s
		}
		switch e := token.(type) {
		case xml.StartElement:
			switch e.Name.Local {
			case "object":
				object, part = attribute(e, "id"), ""
			case "part":
				part = attribute(e, "id")
			case "metadata":
				if attribute(e, "key") == "name" && object != "" && part == "" {
					s.names[object] = strings.TrimSpace(attribute(e, "value"))
					continue
				}
				extruder, err := strconv.Atoi(attribute(e, "value"))
				if attribute(e, "key") != "extruder" || err != nil || object == "" {
					continue
				}
				if part != "" {
					s.extruders[object+"/"+part] = extruder
				} else {
					s.extruders[object] = extruder
				}
			}
		case xml.EndElement:
			switch e.Name.Local {
			case "object":
				object = ""
			case "part":
				part = ""
			}
		}
	}
}

// relationships names the root model and the thumbnail, where declared.
func (p *package3MF) relationships() (model, thumbnail string) {
	data, err := p.read("_rels/.rels")
	if err != nil {
		return "", ""
	}
	var rels struct {
		Relationship []struct {
			Target string `xml:"Target,attr"`
			Type   string `xml:"Type,attr"`
		}
	}
	if xml.Unmarshal(data, &rels) != nil {
		return "", ""
	}
	for _, r := range rels.Relationship {
		switch {
		case strings.HasSuffix(r.Type, "/3dmodel") && model == "":
			model = r.Target
		case strings.HasSuffix(r.Type, "/metadata/thumbnail") && thumbnail == "":
			thumbnail = r.Target
		}
	}
	return model, thumbnail
}

// thumbnail is the package's picture of the model, if one decodes. A
// slicer's rendering of the plate stands in for the model better than the
// declared picture, which may be a photograph of a print.
func (p *package3MF) thumbnail(declared string) image.Image {
	for _, name := range []string{"Metadata/plate_1.png", declared, "Metadata/thumbnail.png", "Auxiliaries/.thumbnails/thumbnail_3mf.png"} {
		if name == "" || !p.has(name) {
			continue
		}
		data, err := p.read(name)
		if err != nil {
			continue
		}
		if config, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil || config.Width > thumbnailLimit || config.Height > thumbnailLimit {
			continue
		}
		if img, _, err := image.Decode(bytes.NewReader(data)); err == nil {
			return img
		}
	}
	return nil
}

// decoder3MF assembles the parts of a build.
type decoder3MF struct {
	pkg       *package3MF
	build     []placed3MF
	settings  slicer3MF
	model     *Model
	textures  map[string]*nt3d.Texture // By path; nil where one did not load.
	materials map[material3MF]*nt3d.Material
}

// material3MF names a texture's material: its path and whether it repeats.
type material3MF struct {
	path   string
	repeat bool
}

// material is a texture's material, made once for each way it tiles.
func (d *decoder3MF) material(path string, repeat bool) *nt3d.Material {
	key := material3MF{path, repeat}
	if m, ok := d.materials[key]; ok {
		return m
	}
	var m *nt3d.Material
	if t := d.texture(path); t != nil {
		m = &nt3d.Material{Texture: t, Repeat: repeat}
	}
	d.materials[key] = m
	return m
}

// texture loads a texture of the package once, flattening transparency over
// white. A texture that cannot be used is a warning, and its faces are drawn
// in their colors instead.
func (d *decoder3MF) texture(path string) *nt3d.Texture {
	if t, ok := d.textures[path]; ok {
		return t
	}
	d.textures[path] = nil
	warn := func(err error) { d.model.Warnings = append(d.model.Warnings, fmt.Sprintf("texture %s: %v", path, err)) }
	data, err := d.pkg.read(path)
	if err != nil {
		warn(err)
		return nil
	}
	img, err := decodeTexture(data, color.RGBA{255, 255, 255, 255})
	if err != nil {
		warn(err)
		return nil
	}
	texture, err := nt3d.NewTexture(img)
	if err != nil {
		warn(err)
		return nil
	}
	d.textures[path] = texture
	return texture
}

// walk gives emit each object placed by at, where it is placed, and the
// slicer color for faces that name none; then the objects it holds.
func (d *decoder3MF) walk(at placed3MF, from, top string, transform matrix3MF, depth int, visits *int, emit func(*part3MF, *object3MF, matrix3MF, *color.RGBA) error) error {
	// Sixteen levels of fifty components each is fifty to the sixteenth:
	// count the objects placed, not only the depth.
	if *visits++; *visits > max3MFVisits {
		return fmt.Errorf("nt3dmesh: 3MF places more than %d objects", max3MFVisits)
	}
	if depth > max3MFDepth {
		return errors.New("nt3dmesh: 3MF objects nest too deeply, or within themselves")
	}
	if at.part != "" {
		from = at.part
	}
	part, err := d.pkg.part(from)
	if err != nil {
		return err
	}
	object := part.objects[at.object]
	if object == nil {
		return fmt.Errorf("nt3dmesh: 3MF places object %s, which it does not define", at.object)
	}
	transform = at.transform.then(transform)
	if err := emit(part, object, transform, d.settings.color(top, at.object)); err != nil {
		return err
	}
	for _, c := range object.components {
		if err := d.walk(c, from, top, transform, depth+1, visits, emit); err != nil {
			return err
		}
	}
	return nil
}

// Decode3MF reads a 3MF package: the core mesh specification, the production
// extension's objects in other model parts, base materials and color groups
// (one color a face), and the filament colors and object names Bambu Studio,
// OrcaSlicer, and their relatives keep beside the model. Each build item is
// a Part; the chosen parts become Objects with their placements baked in,
// mirrored placements rewound so faces stay outward. Faces have vertices of
// their own and flat normals. When the triangles or the package's size pass
// their limits, Decode3MF returns what it learned of the model with the error.
func Decode3MF(data []byte, o *Options) (*Model, error) {
	pkg, err := open3MF(data, o.maxBytes())
	if err != nil {
		return nil, err
	}
	root, declared := pkg.relationships()
	if root == "" || !pkg.has(root) {
		root = rootModel3MF
	}
	if !pkg.has(root) {
		return nil, errors.New("nt3dmesh: 3MF has no model")
	}
	// The picture is read first: it stands in for a model too large to read.
	m := &Model{Format: ThreeMF, Thumbnail: pkg.thumbnail(declared), Metadata: map[string]string{}}
	model, err := pkg.part(root)
	if err != nil {
		if errors.Is(err, ErrTooLarge) {
			return m, err
		}
		return nil, err
	}
	m.Unit, m.Metadata = model.unit, model.metadata
	d := &decoder3MF{pkg: pkg, build: model.build, settings: pkg.slicer(), model: m, textures: map[string]*nt3d.Texture{}, materials: map[material3MF]*nt3d.Material{}}
	if len(d.build) == 0 {
		// Without a build, show what there is.
		for id := range model.objects {
			d.build = append(d.build, placed3MF{object: id, transform: identity3MF})
		}
	}
	// Count before assembling: a model over the limit is measured, not stored.
	visits, counted, chosen := 0, 0, 0
	for i, item := range d.build {
		part := Part{Name: d.settings.names[item.object]}
		named := false
		err := d.walk(item, root, item.object, identity3MF, 0, &visits, func(_ *part3MF, obj *object3MF, _ matrix3MF, _ *color.RGBA) error {
			if !named {
				part.Name, named = cmp.Or(part.Name, obj.name), true
			}
			if part.Triangles += len(obj.triangles); counted+part.Triangles > max3MFCounted {
				return fmt.Errorf("%w: 3MF has more than %d", ErrTooManyTriangles, max3MFCounted)
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, ErrTooLarge) || errors.Is(err, ErrTooManyTriangles) {
				return m, err
			}
			return nil, err
		}
		part.Name = cmp.Or(part.Name, "Object "+item.object)
		counted += part.Triangles
		if o.chosen(i) {
			chosen += part.Triangles
		}
		m.Parts = append(m.Parts, part)
	}
	if counted == 0 {
		return nil, errors.New("nt3dmesh: 3MF contains no triangles")
	}
	if chosen == 0 {
		return nil, errors.New("nt3dmesh: no 3MF parts chosen have triangles")
	}
	if limit := o.limit(); chosen > limit {
		return m, fmt.Errorf("%w: 3MF parts have %d; the limit is %d", ErrTooManyTriangles, chosen, limit)
	}
	budget := &triangleBudget{limit: o.limit()}
	visits = 0
	for i, item := range d.build {
		if !o.chosen(i) || m.Parts[i].Triangles == 0 {
			continue
		}
		// A part's faces gather by material: each texture is an object of its own.
		faces := map[*nt3d.Material]*faceObject{}
		var order []*faceObject
		gather := func(material *nt3d.Material, name string) *faceObject {
			f := faces[material]
			if f == nil {
				f = &faceObject{Object: Object{Name: m.Parts[i].Name, Material: name, Part: i, Geometry: nt3d.Geometry{Material: material}}, budget: budget, format: "3MF"}
				faces[material] = f
				order = append(order, f)
			}
			return f
		}
		err := d.walk(item, root, item.object, identity3MF, 0, &visits, func(part *part3MF, obj *object3MF, transform matrix3MF, slicer *color.RGBA) error {
			mirrored := transform.mirrors()
			for _, t := range obj.triangles {
				var v [3]nt3dmath.Vec3
				for k, index := range t.v {
					v[k] = transform.apply(obj.vertices[index])
				}
				// Colors the file declares win; then the filament the face was
				// painted, the part's filament, and the default.
				shade, plain := o.color(), true
				if slicer != nil {
					shade, plain = *slicer, false
				}
				if painted := d.settings.filament(t.paint); painted != nil {
					shade, plain = *painted, false
				}
				shades := [3]color.RGBA{shade, shade, shade}
				group, index := obj.group, [3]int{obj.index, obj.index, obj.index}
				if t.group != "" {
					group = t.group
				}
				if t.index[0] >= 0 {
					index = t.index
				}
				if colors := part.colors[group]; colors != nil {
					for k, n := range index {
						if n >= 0 && n < len(colors) {
							shades[k], plain = colors[n], false
						}
					}
				}
				f, uv := gather(nil, ""), (*[3]nt3d.UV)(nil)
				if coords := part.coords[group]; coords != nil {
					path := part.textures[coords.texture]
					if material := d.material(path, !part.clamped[coords.texture]); material != nil {
						var corners [3]nt3d.UV
						for k, n := range index {
							if n >= 0 && n < len(coords.coords) {
								corners[k] = coords.coords[n]
							}
						}
						f, uv, plain = gather(material, path), &corners, false
					}
				}
				if mirrored {
					v[1], v[2] = v[2], v[1]
					shades[1], shades[2] = shades[2], shades[1]
					if uv != nil {
						uv[1], uv[2] = uv[2], uv[1]
					}
				}
				if err := f.add(v, shades, plain, uv); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		var pieces []Object
		for _, f := range order {
			pieces = append(pieces, f.objects()...)
		}
		for _, piece := range pieces {
			if err := piece.Geometry.Validate(); err != nil {
				return nil, err
			}
			m.Objects = append(m.Objects, piece)
		}
	}
	o.orient(m)
	return m, nil
}

func attribute(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func transform3MF(s string) (matrix3MF, error) {
	if s == "" {
		return identity3MF, nil
	}
	fields := strings.Fields(s)
	if len(fields) != 12 {
		return matrix3MF{}, errors.New("transform must have 12 numbers")
	}
	var m matrix3MF
	for i, f := range fields {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return matrix3MF{}, fmt.Errorf("transform is not a number: %q", f)
		}
		m[i] = v
	}
	return m, nil
}

// color3MF reads #RRGGBB or #RRGGBBAA; transparency is not drawn.
func color3MF(s string) (color.RGBA, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 && len(s) != 8 {
		return color.RGBA{}, false
	}
	v, err := strconv.ParseUint(s[:6], 16, 32)
	if err != nil {
		return color.RGBA{}, false
	}
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}, true
}

func place3MF(e xml.StartElement) (placed3MF, error) {
	transform, err := transform3MF(attribute(e, "transform"))
	return placed3MF{object: attribute(e, "objectid"), part: attribute(e, "path"), transform: transform}, err
}

// parsePart3MF reads a model part as a stream: a mesh is millions of small elements.
func parsePart3MF(data []byte) (*part3MF, error) {
	part := &part3MF{unit: "millimeter", metadata: map[string]string{}, objects: map[string]*object3MF{}, colors: map[string][]color.RGBA{}, textures: map[string]string{}, clamped: map[string]bool{}, coords: map[string]*texture3MF{}}
	d := xml.NewDecoder(bytes.NewReader(data))
	var object *object3MF
	var group, name string
	var text *strings.Builder
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch e := token.(type) {
		case xml.StartElement:
			switch e.Name.Local {
			case "model":
				if unit := attribute(e, "unit"); unit != "" {
					part.unit = unit
				}
			case "metadata":
				name, text = attribute(e, "name"), &strings.Builder{}
			case "basematerials", "colorgroup":
				group = attribute(e, "id")
			case "texture2d":
				id := attribute(e, "id")
				part.textures[id] = attribute(e, "path")
				// Tile styles default to wrap; mirror is drawn as wrap.
				clamp := func(style string) bool { return style == "clamp" || style == "none" }
				part.clamped[id] = clamp(attribute(e, "tilestyleu")) && clamp(attribute(e, "tilestylev"))
			case "texture2dgroup":
				group = attribute(e, "id")
				part.coords[group] = &texture3MF{texture: attribute(e, "texid")}
			case "tex2coord":
				u, err1 := strconv.ParseFloat(attribute(e, "u"), 32)
				v, err2 := strconv.ParseFloat(attribute(e, "v"), 32)
				if coords := part.coords[group]; coords != nil && err1 == nil && err2 == nil && validCoordinate(float32(u), float32(v)) {
					// 3MF counts v up from the bottom; nt3d counts down from the top.
					coords.coords = append(coords.coords, nt3d.UV{U: float32(u), V: 1 - float32(v)})
				}
			case "base", "color":
				shade, ok := color3MF(attribute(e, "displaycolor") + attribute(e, "color"))
				if !ok {
					shade = DefaultColor
				}
				part.colors[group] = append(part.colors[group], shade)
			case "object":
				object = &object3MF{name: strings.TrimSpace(attribute(e, "name")), group: attribute(e, "pid"), index: -1}
				if index, err := strconv.Atoi(attribute(e, "pindex")); err == nil {
					object.index = index
				}
				part.objects[attribute(e, "id")] = object
			case "vertex":
				if object == nil {
					continue
				}
				var v [3]float64
				for i, axis := range []string{"x", "y", "z"} {
					// Coordinates are kept to the precision they are drawn with.
					if v[i], err = strconv.ParseFloat(attribute(e, axis), 32); err != nil {
						return nil, fmt.Errorf("vertex %s=%q is not a usable number", axis, attribute(e, axis))
					}
				}
				object.vertices = append(object.vertices, v)
			case "triangle":
				if object == nil {
					continue
				}
				t := triangle3MF{group: attribute(e, "pid"), index: [3]int{-1, -1, -1}}
				for i, corner := range []string{"v1", "v2", "v3"} {
					index, err := strconv.ParseUint(attribute(e, corner), 10, 32)
					if err != nil || index >= uint64(len(object.vertices)) {
						return nil, fmt.Errorf("triangle names vertex %q, which does not exist", attribute(e, corner))
					}
					t.v[i] = uint32(index)
				}
				for k, name := range []string{"p1", "p2", "p3"} {
					if index, err := strconv.Atoi(attribute(e, name)); err == nil {
						t.index[k] = index
					}
				}
				// Corners without properties of their own take the first's.
				for k := 1; k < 3; k++ {
					if t.index[k] < 0 {
						t.index[k] = t.index[0]
					}
				}
				t.paint = paintState(cmp.Or(attribute(e, "paint_color"), attribute(e, "mmu_segmentation")))
				object.triangles = append(object.triangles, t)
			case "component":
				if object == nil {
					continue
				}
				c, err := place3MF(e)
				if err != nil {
					return nil, err
				}
				object.components = append(object.components, c)
			case "item":
				item, err := place3MF(e)
				if err != nil {
					return nil, err
				}
				part.build = append(part.build, item)
			}
		case xml.CharData:
			if text != nil {
				text.Write(e)
			}
		case xml.EndElement:
			switch e.Name.Local {
			case "metadata":
				if text != nil {
					part.metadata[name] = text.String()
				}
				text = nil
			case "object":
				object = nil
			}
		}
	}
	return part, nil
}

// Encode3MF writes the triangles of g as a 3MF package of one object, in
// millimeters and local coordinates. Vertices are shared as g shares them.
// Each face takes its first vertex's color, from one base-materials group.
func Encode3MF(w io.Writer, g nt3d.Geometry) error {
	if err := g.Validate(); err != nil {
		return err
	}
	if len(g.Indices) == 0 {
		return errors.New("nt3dmesh: 3MF needs at least one triangle")
	}
	z := zip.NewWriter(w)
	write := func(name, content string) error {
		f, err := z.Create(name)
		if err == nil {
			_, err = io.WriteString(f, content)
		}
		return err
	}
	err := write("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml"/></Types>`)
	if err == nil {
		err = write("_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Target="/3D/3dmodel.model" Id="rel0" Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"/></Relationships>`)
	}
	if err != nil {
		return err
	}
	f, err := z.Create(rootModel3MF)
	if err != nil {
		return err
	}
	b := bufio.NewWriter(f)
	number := func(v float32) string { return strconv.FormatFloat(float64(v), 'g', -1, 32) }
	colors := map[color.RGBA]int{}
	var palette []color.RGBA
	for i := 0; i < len(g.Indices); i += 3 {
		c := g.Vertices[g.Indices[i]].Color
		c.A = 255
		if _, ok := colors[c]; !ok {
			colors[c] = len(palette)
			palette = append(palette, c)
		}
	}
	fmt.Fprint(b, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"+`<model unit="millimeter" xml:lang="en-US" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02">`+"\n"+`<metadata name="Application">nt3d</metadata>`+"\n<resources>\n<basematerials id=\"1\">")
	for i, c := range palette {
		fmt.Fprintf(b, `<base name="color %d" displaycolor="#%02X%02X%02X"/>`, i, c.R, c.G, c.B)
	}
	fmt.Fprint(b, "</basematerials>\n<object id=\"2\" type=\"model\" pid=\"1\" pindex=\"0\"><mesh>\n<vertices>\n")
	for _, v := range g.Vertices {
		fmt.Fprintf(b, "<vertex x=\"%s\" y=\"%s\" z=\"%s\"/>\n", number(v.Position.X), number(v.Position.Y), number(v.Position.Z))
	}
	fmt.Fprint(b, "</vertices>\n<triangles>\n")
	for i := 0; i < len(g.Indices); i += 3 {
		c := g.Vertices[g.Indices[i]].Color
		c.A = 255
		fmt.Fprintf(b, "<triangle v1=\"%d\" v2=\"%d\" v3=\"%d\"", g.Indices[i], g.Indices[i+1], g.Indices[i+2])
		if index := colors[c]; index != 0 {
			fmt.Fprintf(b, " pid=\"1\" p1=\"%d\"", index)
		}
		fmt.Fprint(b, "/>\n")
	}
	fmt.Fprint(b, "</triangles>\n</mesh></object>\n</resources>\n<build><item objectid=\"2\"/></build>\n</model>\n")
	if err := b.Flush(); err != nil {
		return err
	}
	return z.Close()
}

// paintState returns the filament, from 1, that covers most of a face a slicer
// painted, or 0 for none. Bambu Studio's paint_color and PrusaSlicer's
// mmu_segmentation share an encoding: hex digits, read from the last, each a
// node of a tree that splits the face. A node's low two bits count the sides
// it splits, 0 to 3; a split into n+1 parts is followed by its children, and
// a leaf's high two bits are its filament, 3 meaning the next digit plus 3.
// Children's areas are taken as equal shares of their parent's.
func paintState(code string) int {
	at := len(code)
	next := func() (int, bool) {
		for at > 0 {
			at--
			if v, err := strconv.ParseUint(code[at:at+1], 16, 8); err == nil {
				return int(v), true
			}
		}
		return 0, false
	}
	share := map[int]float64{}
	type pending struct{ weight float64 }
	stack := []pending{{1}}
	for len(stack) > 0 && len(stack) <= len(code)+1 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		digit, ok := next()
		if !ok {
			break
		}
		if sides := digit & 3; sides > 0 {
			for range sides + 1 {
				stack = append(stack, pending{node.weight / float64(sides+1)})
			}
			continue
		}
		state := digit >> 2
		if state == 3 {
			extra, ok := next()
			if !ok {
				break
			}
			state = extra + 3
		}
		share[state] += node.weight
	}
	best, most := 0, 0.0
	for state, weight := range share {
		if weight > most || weight == most && state < best {
			best, most = state, weight
		}
	}
	return best
}
