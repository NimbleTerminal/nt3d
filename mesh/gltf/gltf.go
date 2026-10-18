// Copyright (c) 2026 Neomantra Corp.

// Package nt3dgltf reads glTF 2.0 models for nt3dmesh. Importing it
// registers the format, so nt3dmesh.ReadFile, Decode, and FormatOf know
// .gltf and .glb files:
//
//	import _ "nimbleterminal.dev/nt3d/mesh/gltf"
//
// It is kept apart from nt3dmesh so programs that read no glTF do not carry
// it; nimbleterminal.dev/nt3d/mesh/formats imports it with every other
// optional format.
package nt3dgltf

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io/fs"
	"math"
	"net/url"
	"slices"
	"strings"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
)

func init() { nt3dmesh.RegisterFormat(GLTF, []string{".gltf", ".glb"}, "glTF", Decode) }

// maxTextureSide is the largest texture side nt3d draws.
const maxTextureSide = 8192

// pieceTriangles is the most triangles one object holds, as nt3dmesh splits
// them. Tests lower it.
var pieceTriangles = nt3d.MaxMeshTriangles

// maxLineVertices is the most line ends a model may hold, as many as one
// geometry may.
const maxLineVertices = 2 * 512 * 512

// budget counts faces against the model's limit.
type budget struct{ used, limit int }

func (b *budget) take(n int) error {
	if b.used+n > b.limit {
		return fmt.Errorf("%w: model exceeds %d", nt3dmesh.ErrTooManyTriangles, b.limit)
	}
	b.used += n
	return nil
}

func validCoordinate(v ...float32) bool {
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) || x > nt3dmesh.MaxCoordinate || x < -nt3dmesh.MaxCoordinate {
			return false
		}
	}
	return true
}

// GLTF is glTF 2.0: a .gltf JSON file with its buffers and images, or a
// binary .glb holding them.
const GLTF nt3dmesh.Format = "gltf"

// Decode reads a glTF 2.0 scene, .gltf or .glb, as a flat model: each
// mesh primitive of each node in the default scene becomes an object, named
// for its node (else its mesh), with the node's world transform applied.
// glTF is Y-up by its specification, so the model is always turned to Z-up;
// Options.Up is ignored. Distances are the file's meters.
//
// Materials: the base color factor and vertex colors (linear in glTF) become
// sRGB vertex colors; a base color texture becomes the material's texture,
// tinted by those colors when they are not white, and repeating unless its
// sampler clamps. MASK becomes a cutout at the alpha cutoff, BLEND a
// transparent material, and KHR_materials_unlit an unlit one. Double-sided
// materials add each triangle facing the other way. Roughness and
// metalness are approximated by a specular highlight, none when fully rough.
// Normals the file omits are computed flat; triangle strips and fans become
// triangles, points points, and lines lines.
//
// Buffers and images outside a .glb are read through Options.Open, within
// Options.MaxBytes. Files that require an extension nt3d cannot honor, such
// as Draco or meshopt compression, are refused. Animation, skins and morphs
// populate Model.Asset; AddTo instantiates its hierarchy and generic clips.
// Cameras, normal and emissive maps, and texture transforms are
// passed over, with a warning for those that change how the model looks.
func Decode(data []byte, o *nt3dmesh.Options) (*nt3dmesh.Model, error) {
	if int64(len(data)) > o.ByteLimit() {
		return nil, nt3dmesh.ErrTooLarge
	}
	d := &gltfDecoder{o: o, m: &nt3dmesh.Model{Format: GLTF}, budget: budget{limit: o.TriangleLimit()}, textures: map[int]*nt3d.Texture{}, warned: map[string]bool{}}
	left := o.ByteLimit()
	d.files = sideFiles{left: &left}
	if o != nil {
		d.files.open = o.Open
	}
	d.doc = new(gltf.Document)
	if err := gltf.NewDecoderFS(bytes.NewReader(data), d.files).Decode(d.doc); err != nil {
		if errors.Is(err, nt3dmesh.ErrTooLarge) {
			return nil, err
		}
		return nil, fmt.Errorf("nt3dmesh: glTF: %w", err)
	}
	for i, b := range d.doc.Buffers {
		if len(b.Data) < b.ByteLength {
			return nil, fmt.Errorf("nt3dmesh: glTF buffer %d is missing or short", i)
		}
	}
	if err := d.extensions(); err != nil {
		return nil, err
	}
	if err := d.scene(); err != nil {
		return nil, err
	}
	if len(d.m.Objects) == 0 {
		return nil, errors.New("nt3dmesh: glTF scene holds no meshes")
	}
	for i := range d.m.Objects {
		if err := d.m.Objects[i].Geometry.Validate(); err != nil {
			return nil, fmt.Errorf("nt3dmesh: glTF %s: %w", d.m.Objects[i].Name, err)
		}
	}
	if err := d.animatedAsset(); err != nil {
		return nil, err
	}
	return d.m, nil
}

// supportedExtensions are those whose meaning Decode honors.
var supportedExtensions = []string{"KHR_materials_unlit"}

// ignoredExtensions are passed over silently: they refine looks nt3d does not draw.
var ignoredExtensions = []string{"KHR_lights_punctual", "KHR_materials_emissive_strength", "KHR_materials_ior", "KHR_materials_specular", "KHR_materials_clearcoat", "KHR_materials_sheen", "KHR_materials_volume", "KHR_materials_transmission", "KHR_materials_variants"}

type gltfDecoder struct {
	decodedLeft *int64
	depth       int
	capture     bool
	sources     [][]vertexOrigin
	doc         *gltf.Document
	o           *nt3dmesh.Options
	m           *nt3dmesh.Model
	budget      budget
	files       sideFiles
	textures    map[int]*nt3d.Texture // By texture index; nil when it failed.
	warned      map[string]bool
	visits      int
	points      int // Across the model, within nt3d.MaxPoints.
	lines       int // Line ends across the model, within maxLineVertices.
}

func (d *gltfDecoder) warn(format string, a ...any) {
	if msg := fmt.Sprintf(format, a...); !d.warned[msg] {
		d.warned[msg] = true
		d.m.Warnings = append(d.m.Warnings, msg)
	}
}

func (d *gltfDecoder) extensions() error {
	for _, e := range d.doc.ExtensionsRequired {
		if !slices.Contains(supportedExtensions, e) {
			return fmt.Errorf("nt3dmesh: glTF requires unsupported extension %s", e)
		}
	}
	for _, e := range d.doc.ExtensionsUsed {
		if !slices.Contains(supportedExtensions, e) && !slices.Contains(ignoredExtensions, e) && !slices.Contains(d.doc.ExtensionsRequired, e) {
			d.warn("extension %s is ignored", e)
		}
	}
	return nil
}

// sideFiles is the file system the glTF decoder reads external buffers
// from: Options.Open, within the byte budget.
type sideFiles struct {
	open func(string) ([]byte, error)
	left *int64
}

func (s sideFiles) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
}

func (s sideFiles) ReadFile(name string) ([]byte, error) {
	if s.open == nil {
		return nil, fmt.Errorf("side file %q needs Options.Open", name)
	}
	data, err := s.open(name)
	if err != nil {
		return nil, err
	}
	if *s.left -= int64(len(data)); *s.left < 0 {
		return nil, fmt.Errorf("%w: glTF side files", nt3dmesh.ErrTooLarge)
	}
	return data, nil
}

// mat4 is a column-major affine transform in float64, as glTF stores them.
type mat4 [16]float64

func (a mat4) mul(b mat4) (m mat4) {
	for c := range 4 {
		for r := range 4 {
			for k := range 4 {
				m[c*4+r] += a[k*4+r] * b[c*4+k]
			}
		}
	}
	return m
}

func (a mat4) point(p [3]float32) nt3dmath.Vec3 {
	x, y, z := float64(p[0]), float64(p[1]), float64(p[2])
	return nt3dmath.Vec3{X: float32(a[0]*x + a[4]*y + a[8]*z + a[12]), Y: float32(a[1]*x + a[5]*y + a[9]*z + a[13]), Z: float32(a[2]*x + a[6]*y + a[10]*z + a[14])}
}

// normal turns a normal by the inverse transpose of a's linear part, which
// is the cofactor matrix up to a positive scale (when a does not mirror)
// that normalizing removes.
func (a mat4) normal(n [3]float32) nt3dmath.Vec3 {
	c0 := [3]float64{a[0], a[1], a[2]}
	c1 := [3]float64{a[4], a[5], a[6]}
	c2 := [3]float64{a[8], a[9], a[10]}
	cross := func(u, v [3]float64) [3]float64 {
		return [3]float64{u[1]*v[2] - u[2]*v[1], u[2]*v[0] - u[0]*v[2], u[0]*v[1] - u[1]*v[0]}
	}
	r0, r1, r2 := cross(c1, c2), cross(c2, c0), cross(c0, c1)
	x, y, z := float64(n[0]), float64(n[1]), float64(n[2])
	v := nt3dmath.Vec3{X: float32(r0[0]*x + r1[0]*y + r2[0]*z), Y: float32(r0[1]*x + r1[1]*y + r2[1]*z), Z: float32(r0[2]*x + r1[2]*y + r2[2]*z)}
	if a.det() < 0 {
		v = v.Scale(-1)
	}
	if v.Dot(v) == 0 {
		return v
	}
	return v.Normalize()
}

func (a mat4) det() float64 {
	return a[0]*(a[5]*a[10]-a[9]*a[6]) - a[4]*(a[1]*a[10]-a[9]*a[2]) + a[8]*(a[1]*a[6]-a[5]*a[2])
}

func (a mat4) finite() bool {
	for _, v := range a {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// yUp turns glTF's Y-up to nt3d's Z-up: (x, y, z) becomes (x, -z, y).
var yUp = mat4{1, 0, 0, 0, 0, 0, 1, 0, 0, -1, 0, 0, 0, 0, 0, 1}

// local is a node's transform: its matrix, or translation, rotation, scale.
func local(n *gltf.Node) mat4 {
	if m := n.MatrixOrDefault(); m != gltf.DefaultMatrix {
		return mat4(m)
	}
	t, q, s := n.TranslationOrDefault(), n.RotationOrDefault(), n.ScaleOrDefault()
	x, y, z, w := q[0], q[1], q[2], q[3]
	if l := math.Sqrt(x*x + y*y + z*z + w*w); l > 0 {
		x, y, z, w = x/l, y/l, z/l, w/l
	}
	r := mat4{
		1 - 2*(y*y+z*z), 2 * (x*y + z*w), 2 * (x*z - y*w), 0,
		2 * (x*y - z*w), 1 - 2*(x*x+z*z), 2 * (y*z + x*w), 0,
		2 * (x*z + y*w), 2 * (y*z - x*w), 1 - 2*(x*x+y*y), 0,
		0, 0, 0, 1,
	}
	for c := range 3 {
		for k := range 3 {
			r[c*4+k] *= s[c]
		}
	}
	r[12], r[13], r[14] = t[0], t[1], t[2]
	return r
}

// maxVisits bounds the nodes walked, as nodes reached by several paths are
// walked once for each.
const maxVisits = 1 << 20

// scene walks the default scene's nodes, or, without scenes, every root node.
func (d *gltfDecoder) scene() error {
	var roots []int
	if len(d.doc.Scenes) > 0 {
		s := 0
		if d.doc.Scene != nil && *d.doc.Scene >= 0 && *d.doc.Scene < len(d.doc.Scenes) {
			s = *d.doc.Scene
		}
		roots = d.doc.Scenes[s].Nodes
	} else {
		child := make([]bool, len(d.doc.Nodes))
		for _, n := range d.doc.Nodes {
			for _, c := range n.Children {
				if c >= 0 && c < len(child) {
					child[c] = true
				}
			}
		}
		for i, c := range child {
			if !c {
				roots = append(roots, i)
			}
		}
	}
	onPath := make([]bool, len(d.doc.Nodes))
	for _, r := range roots {
		if err := d.node(r, yUp, onPath); err != nil {
			return err
		}
	}
	return nil
}

func (d *gltfDecoder) node(i int, parent mat4, onPath []bool) error {
	d.depth++
	defer func() { d.depth-- }()
	if d.depth > 1024 {
		return fmt.Errorf("%w: glTF hierarchy exceeds 1024 levels", nt3dmesh.ErrTooLarge)
	}
	if i < 0 || i >= len(d.doc.Nodes) {
		return fmt.Errorf("nt3dmesh: glTF names node %d of %d", i, len(d.doc.Nodes))
	}
	if onPath[i] {
		return fmt.Errorf("nt3dmesh: glTF node %d is its own ancestor", i)
	}
	if d.visits++; d.visits > maxVisits {
		return fmt.Errorf("nt3dmesh: glTF scene reaches more than %d nodes", maxVisits)
	}
	n := d.doc.Nodes[i]
	world := parent.mul(local(n))
	if !world.finite() {
		return fmt.Errorf("nt3dmesh: glTF node %d has a non-finite transform", i)
	}
	if n.Mesh != nil {
		if *n.Mesh < 0 || *n.Mesh >= len(d.doc.Meshes) {
			return fmt.Errorf("nt3dmesh: glTF node %d names mesh %d of %d", i, *n.Mesh, len(d.doc.Meshes))
		}
		mesh := d.doc.Meshes[*n.Mesh]
		name := n.Name
		if name == "" {
			name = mesh.Name
		}
		if name == "" {
			name = fmt.Sprintf("mesh %d", *n.Mesh)
		}
		for _, p := range mesh.Primitives {
			if err := d.primitive(name, p, world); err != nil {
				return err
			}
		}
	}
	onPath[i] = true
	for _, c := range n.Children {
		if err := d.node(c, world, onPath); err != nil {
			return err
		}
	}
	onPath[i] = false
	return nil
}

// accessor finds an accessor, bounding the count the reader will allocate.
func (d *gltfDecoder) accessor(index int, what string) (*gltf.Accessor, error) {
	if index < 0 || index >= len(d.doc.Accessors) {
		return nil, fmt.Errorf("nt3dmesh: glTF %s names accessor %d of %d", what, index, len(d.doc.Accessors))
	}
	a := d.doc.Accessors[index]
	if a.Count < 0 || a.Count > 3*d.budget.limit+3 {
		return nil, fmt.Errorf("%w: glTF %s has %d elements", nt3dmesh.ErrTooManyTriangles, what, a.Count)
	}
	if d.decodedLeft != nil {
		cost := int64(a.Count) * 64
		if cost > *d.decodedLeft {
			return nil, nt3dmesh.ErrTooLarge
		}
		*d.decodedLeft -= cost
	}
	return a, nil
}

// gltfLook is what a primitive's material says, as nt3d draws it.
type gltfLook struct {
	name     string
	factor   [4]float64 // Linear base color and alpha.
	texture  *nt3d.Texture
	texCoord int
	repeat   bool
	alpha    gltf.AlphaMode
	cutoff   float64
	double   bool
	unlit    bool
	specular float32
	shiny    float32
	plain    bool // No material: Options.Color paints it.
}

func (d *gltfDecoder) look(index *int) gltfLook {
	l := gltfLook{factor: [4]float64{1, 1, 1, 1}, cutoff: .5, plain: index == nil}
	if index == nil || *index < 0 || *index >= len(d.doc.Materials) {
		if index != nil {
			d.warn("material %d does not exist", *index)
			l.plain = true
		}
		return l
	}
	m := d.doc.Materials[*index]
	l.name, l.alpha, l.cutoff, l.double = m.Name, m.AlphaMode, m.AlphaCutoffOrDefault(), m.DoubleSided
	_, l.unlit = m.Extensions["KHR_materials_unlit"]
	if m.NormalTexture != nil {
		d.warn("normal maps are not drawn")
	}
	if m.EmissiveTexture != nil || m.EmissiveFactor != ([3]float64{}) {
		d.warn("emission is not drawn")
	}
	if p := m.PBRMetallicRoughness; p != nil {
		l.factor = p.BaseColorFactorOrDefault()
		// Smooth surfaces get a highlight, tighter as they are smoother:
		// the Blinn-Phong exponent for roughness r is about 2/r⁴ - 2.
		if r := p.RoughnessFactorOrDefault(); r < 1 && !l.unlit {
			r = max(r, .05)
			l.specular = float32(min(1, (1-r)*(.5+.5*p.MetallicFactorOrDefault())))
			l.shiny = float32(min(1024, max(1, 2/(r*r*r*r)-2)))
		}
		if t := p.BaseColorTexture; t != nil {
			l.texture, l.repeat = d.texture(t.Index)
			l.texCoord = t.TexCoord
			if _, ok := t.Extensions["KHR_texture_transform"]; ok {
				d.warn("texture transforms are not applied")
			}
		}
	}
	return l
}

// texture loads a texture by index, once, and whether it repeats.
func (d *gltfDecoder) texture(index int) (*nt3d.Texture, bool) {
	repeat := true
	if index >= 0 && index < len(d.doc.Textures) {
		if s := d.doc.Textures[index].Sampler; s != nil && *s >= 0 && *s < len(d.doc.Samplers) {
			sampler := d.doc.Samplers[*s]
			repeat = sampler.WrapS != gltf.WrapClampToEdge || sampler.WrapT != gltf.WrapClampToEdge
			if sampler.WrapS == gltf.WrapMirroredRepeat || sampler.WrapT == gltf.WrapMirroredRepeat {
				d.warn("mirrored textures repeat unmirrored")
			}
		}
	}
	if t, ok := d.textures[index]; ok {
		return t, repeat
	}
	t, err := d.loadTexture(index)
	if err != nil {
		d.warn("texture %d: %v", index, err)
	}
	d.textures[index] = t
	return t, repeat
}

func (d *gltfDecoder) loadTexture(index int) (*nt3d.Texture, error) {
	if index < 0 || index >= len(d.doc.Textures) {
		return nil, errors.New("does not exist")
	}
	src := d.doc.Textures[index].Source
	if src == nil || *src < 0 || *src >= len(d.doc.Images) {
		return nil, errors.New("has no image nt3d can read")
	}
	im := d.doc.Images[*src]
	var data []byte
	var err error
	switch {
	case im.BufferView != nil:
		if *im.BufferView < 0 || *im.BufferView >= len(d.doc.BufferViews) {
			return nil, errors.New("names a missing buffer view")
		}
		data, err = modeler.ReadBufferView(d.doc, d.doc.BufferViews[*im.BufferView])
	case im.IsEmbeddedResource():
		data, err = im.MarshalData()
	default:
		name := strings.ReplaceAll(im.URI, `\`, "/")
		if unescaped, e := url.PathUnescape(name); e == nil {
			name = unescaped
		}
		data, err = d.files.ReadFile(name)
	}
	if err != nil {
		return nil, err
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("not a PNG, JPEG, or registered image format")
	}
	if config.Width > maxTextureSide || config.Height > maxTextureSide {
		return nil, fmt.Errorf("larger than %d pixels a side", maxTextureSide)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return nt3d.NewTexture(img)
}

// srgb8 encodes linear light, 0 to 1, as an 8-bit sRGB value.
func srgb8(v float64) uint8 {
	v = max(0, min(1, v))
	if v <= .0031308 {
		v *= 12.92
	} else {
		v = 1.055*math.Pow(v, 1/2.4) - .055
	}
	return uint8(v*255 + .5)
}

// primitive adds one mesh primitive as objects, placed by world.
func (d *gltfDecoder) primitive(name string, p *gltf.Primitive, world mat4) error {
	attr := func(key string) (*gltf.Accessor, bool, error) {
		i, ok := p.Attributes[key]
		if !ok {
			return nil, false, nil
		}
		a, err := d.accessor(i, key)
		return a, err == nil, err
	}
	position, ok, err := attr(gltf.POSITION)
	if err != nil {
		return err
	}
	if !ok {
		d.warn("a primitive of %s has no positions", name)
		return nil
	}
	positions, err := modeler.ReadPosition(d.doc, position, nil)
	if err != nil {
		return fmt.Errorf("nt3dmesh: glTF %s positions: %w", name, err)
	}
	var indices []uint32
	if p.Indices != nil {
		a, err := d.accessor(*p.Indices, "indices")
		if err != nil {
			return err
		}
		if indices, err = modeler.ReadIndices(d.doc, a, nil); err != nil {
			return fmt.Errorf("nt3dmesh: glTF %s indices: %w", name, err)
		}
		for _, i := range indices {
			if int(i) >= len(positions) {
				return fmt.Errorf("nt3dmesh: glTF %s index %d is past its %d vertices", name, i, len(positions))
			}
		}
	} else {
		indices = make([]uint32, len(positions))
		for i := range indices {
			indices[i] = uint32(i)
		}
	}
	l := d.look(p.Material)
	b := &gltfBuilder{d: d, name: name, look: l, world: world, positions: positions}
	if a, ok, err := attr(gltf.NORMAL); err != nil {
		return err
	} else if ok {
		if b.normals, err = modeler.ReadNormal(d.doc, a, nil); err != nil || len(b.normals) != len(positions) {
			d.warn("%s: unreadable normals are computed", name)
			b.normals = nil
		}
		for _, n := range b.normals {
			if !validCoordinate(n[0], n[1], n[2]) {
				return fmt.Errorf("nt3dmesh: glTF %s has a non-finite normal", name)
			}
		}
	}
	if a, ok, err := attr(gltf.COLOR_0); err != nil {
		return err
	} else if ok {
		if b.colors, err = modeler.ReadColor64(d.doc, a, nil); err != nil || len(b.colors) != len(positions) {
			d.warn("%s: unreadable vertex colors are passed over", name)
			b.colors = nil
		}
	}
	if l.texture != nil {
		a, ok, err := attr(fmt.Sprintf("TEXCOORD_%d", l.texCoord))
		if err != nil {
			return err
		}
		if ok {
			b.uv, err = modeler.ReadTextureCoord(d.doc, a, nil)
		}
		if !ok || err != nil || len(b.uv) != len(positions) {
			d.warn("%s: its texture has no texture coordinates", name)
			b.look.texture, b.uv = nil, nil
		}
		for _, uv := range b.uv {
			if !validCoordinate(uv[0], uv[1]) {
				return fmt.Errorf("nt3dmesh: glTF %s has a non-finite texture coordinate", name)
			}
		}
	}
	switch p.Mode {
	case gltf.PrimitiveTriangles, gltf.PrimitiveTriangleStrip, gltf.PrimitiveTriangleFan:
		return b.triangles(gltfTriangles(p.Mode, indices))
	case gltf.PrimitivePoints:
		return b.points(indices)
	default:
		return b.lines(lineSegments(p.Mode, indices))
	}
}

// gltfTriangles turns strips and fans into a triangle list.
func gltfTriangles(mode gltf.PrimitiveMode, v []uint32) []uint32 {
	var out []uint32
	switch mode {
	case gltf.PrimitiveTriangleStrip:
		for i := 0; i+2 < len(v); i++ {
			if i%2 == 0 {
				out = append(out, v[i], v[i+1], v[i+2])
			} else {
				out = append(out, v[i+1], v[i], v[i+2])
			}
		}
	case gltf.PrimitiveTriangleFan:
		for i := 1; i+1 < len(v); i++ {
			out = append(out, v[i], v[i+1], v[0])
		}
	default:
		out = v[:len(v)/3*3]
	}
	return out
}

// lineSegments turns line strips and loops into pairs.
func lineSegments(mode gltf.PrimitiveMode, v []uint32) []uint32 {
	if mode == gltf.PrimitiveLines {
		return v[:len(v)/2*2]
	}
	var out []uint32
	for i := 0; i+1 < len(v); i++ {
		out = append(out, v[i], v[i+1])
	}
	if mode == gltf.PrimitiveLineLoop && len(v) > 2 {
		out = append(out, v[len(v)-1], v[0])
	}
	return out
}

// gltfBuilder turns one primitive's vertices into objects.
type gltfBuilder struct {
	d         *gltfDecoder
	name      string
	look      gltfLook
	world     mat4
	positions [][3]float32
	normals   [][3]float32
	colors    [][4]uint16
	uv        [][2]float32
	material  *nt3d.Material
	piece     *nt3dmesh.Object
	remap     map[uint32]uint32 // Source vertex (twice, plus one for the back) to the piece's.
}

// color is a vertex's sRGB color and alpha: the base color factor times the
// vertex color, both linear.
func (b *gltfBuilder) color(i uint32) (color.RGBA, bool) {
	f := b.look.factor
	if b.colors != nil {
		c := b.colors[i]
		for k := range 4 {
			f[k] *= float64(c[k]) / 65535
		}
	}
	if b.look.plain && b.colors == nil {
		return b.d.o.FaceColor(), true
	}
	return color.RGBA{srgb8(f[0]), srgb8(f[1]), srgb8(f[2]), uint8(max(0, min(1, f[3]))*255 + .5)}, false
}

// nt3dMaterial is the primitive's nt3d material, or nil when plain will do.
func (b *gltfBuilder) nt3dMaterial() *nt3d.Material {
	l := b.look
	m := nt3d.Material{Texture: l.texture, Repeat: l.repeat && l.texture != nil, Unlit: l.unlit, Specular: l.specular, Shininess: l.shiny}
	switch l.alpha {
	case gltf.AlphaMask:
		if l.texture != nil {
			m.Cutout, m.Threshold = true, float32(max(.001, min(1, l.cutoff)))
		}
	case gltf.AlphaBlend:
		m.Transparent = true
	}
	if l.texture != nil {
		// The texture replaces vertex colors unless they tint it.
		m.Tint = b.colors != nil || l.factor[0] != 1 || l.factor[1] != 1 || l.factor[2] != 1
	}
	if m == (nt3d.Material{}) {
		return nil
	}
	return &m
}

// start begins a new piece of the object.
func (b *gltfBuilder) start() {
	if b.d.capture {
		b.d.sources = append(b.d.sources, nil)
	}
	b.d.m.Objects = append(b.d.m.Objects, nt3dmesh.Object{Name: b.name, Material: b.look.name, Geometry: nt3d.Geometry{Material: b.material}})
	b.piece = &b.d.m.Objects[len(b.d.m.Objects)-1]
	b.remap = map[uint32]uint32{}
}

func (b *gltfBuilder) place(i uint32) (nt3dmath.Vec3, error) {
	p := b.world.point(b.positions[i])
	if !validCoordinate(p.X, p.Y, p.Z) {
		return p, fmt.Errorf("nt3dmesh: glTF %s vertex is non-finite or exceeds coordinate limits", b.name)
	}
	return p, nil
}

func (b *gltfBuilder) triangles(v []uint32) error {
	b.material = b.nt3dMaterial()
	first := len(b.d.m.Objects)
	mirror := b.world.det() < 0 // Mirroring turns faces inside out; turn them back.
	for _, back := range []bool{false, true} {
		if back && !b.look.double {
			break
		}
		for t := 0; t+2 < len(v); t += 3 {
			tri := [3]uint32{v[t], v[t+1], v[t+2]}
			if mirror != back {
				tri[1], tri[2] = tri[2], tri[1]
			}
			if b.look.alpha == gltf.AlphaMask && b.look.texture == nil {
				// No texture to cut: a face shows if its corners average
				// past the cutoff.
				a := 0.0
				for _, i := range tri {
					c, _ := b.color(i)
					a += float64(c.A) / 255 / 3
				}
				if a < b.look.cutoff {
					continue
				}
			}
			// Faces count against the limit as they are kept.
			if err := b.d.budget.take(1); err != nil {
				return err
			}
			if err := b.face(tri, back); err != nil {
				return err
			}
		}
	}
	for i := first; i < len(b.d.m.Objects); i++ {
		g := &b.d.m.Objects[i].Geometry
		g.Bounds = nt3d.GeometryBounds(*g)
	}
	return nil
}

// face adds a triangle, sharing vertices within the piece unless their
// normals are computed per face.
func (b *gltfBuilder) face(tri [3]uint32, back bool) error {
	if b.piece == nil || len(b.piece.Geometry.Indices) >= 3*pieceTriangles {
		b.start()
	}
	var corners [3]nt3dmath.Vec3
	for k, i := range tri {
		p, err := b.place(i)
		if err != nil {
			return err
		}
		corners[k] = p
	}
	flat := corners[1].Sub(corners[0]).Cross(corners[2].Sub(corners[0]))
	if flat.Dot(flat) > 0 {
		flat = flat.Normalize()
	}
	g := &b.piece.Geometry
	for k, i := range tri {
		key := i * 2
		if back {
			key++
		}
		if j, ok := b.remap[key]; ok {
			g.Indices = append(g.Indices, j)
			continue
		}
		n := flat
		if b.normals != nil {
			n = b.world.normal(b.normals[i])
			if back {
				n = n.Scale(-1)
			}
		}
		c, plain := b.color(i)
		j := uint32(len(g.Vertices))
		if b.normals != nil {
			b.remap[key] = j // Shared; computed normals are the face's own.
		}
		g.Indices = append(g.Indices, j)
		g.Vertices = append(g.Vertices, nt3d.Vertex{Position: corners[k], Normal: n, Color: c})
		if b.d.capture {
			last := len(b.d.sources) - 1
			b.d.sources[last] = append(b.d.sources[last], vertexOrigin{i, back})
		}
		b.piece.Plain = append(b.piece.Plain, plain)
		if b.uv != nil {
			g.UV = append(g.UV, nt3d.UV{U: b.uv[i][0], V: b.uv[i][1]})
		}
	}
	return nil
}

func (b *gltfBuilder) points(v []uint32) error {
	if b.d.points += len(v); b.d.points > nt3d.MaxPoints {
		return fmt.Errorf("%w: glTF has more than %d points", nt3dmesh.ErrTooManyTriangles, nt3d.MaxPoints)
	}
	b.start()
	g := &b.piece.Geometry
	for _, i := range v {
		p, err := b.place(i)
		if err != nil {
			return err
		}
		c, _ := b.color(i)
		g.Points = append(g.Points, nt3d.Point{Position: p, Radius: 2, Color: c})
	}
	g.Bounds = nt3d.GeometryBounds(*g)
	return nil
}

func (b *gltfBuilder) lines(v []uint32) error {
	if b.d.lines += len(v); b.d.lines > maxLineVertices {
		return fmt.Errorf("%w: glTF has more than %d line ends", nt3dmesh.ErrTooManyTriangles, maxLineVertices)
	}
	b.start()
	g := &b.piece.Geometry
	for _, i := range v {
		p, err := b.place(i)
		if err != nil {
			return err
		}
		c, _ := b.color(i)
		g.Lines = append(g.Lines, nt3d.Vertex{Position: p, Color: c})
	}
	g.Bounds = nt3d.GeometryBounds(*g)
	return nil
}
