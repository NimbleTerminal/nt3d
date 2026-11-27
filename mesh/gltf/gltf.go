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
	_ "golang.org/x/image/webp" // EXT_texture_webp and WebP textures.

	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

func init() { nt3dmesh.RegisterFormat(GLTF, []string{".gltf", ".glb"}, "glTF", Decode) }

// pieceTriangles is the most triangles one object holds, as nt3dmesh splits
// them. Tests lower it.
var pieceTriangles = nt3d.MeshTriangleLimit

// maxLineVertices is the most line ends a model may hold, as many as one
// geometry may.
const maxLineVertices = 2 * 512 * 512

var errNoMeshes = errors.New("nt3dmesh: glTF scene holds no meshes")

// GLTF is glTF 2.0: a .gltf JSON file with its buffers and images, or a
// binary .glb holding them.
const GLTF nt3dmesh.Format = "gltf"

// Decode reads a glTF 2.0 scene, .gltf or .glb. Model.Asset preserves its
// hierarchy, cameras and clips. Model.Objects remains a flat rest-pose view: each
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
// materials add each triangle facing the other way. Metallic and roughness
// factors and metallic-roughness/occlusion textures map to Cook-Torrance GGX
// PBR and a packed linear ORMMap, in Material.PBR. Emissive factors,
// emissive maps, and KHR_materials_emissive_strength map to
// Material.Emission; KHR_materials_ior to PBR.IOR; KHR_materials_transmission
// and KHR_materials_volume to Material.Transmission; and
// KHR_materials_clearcoat, KHR_materials_sheen, and KHR_materials_specular to
// the Clearcoat, Sheen, and Specular groups of Material.PBR.
// Normals the file omits are computed flat; triangle strips and fans become
// triangles, points points, and lines lines. A POINTS primitive carrying
// KHR_gaussian_splatting becomes a Gaussian splat cloud (gaussian.go).
//
// Buffers and images outside a .glb are read through Options.Open, within
// Options.MaxBytes. Files that require an extension nt3d cannot honor, such
// as Draco compression, are refused. KHR_mesh_quantization decodes quantized
// integer positions, normals, tangents, texture coordinates, and morph deltas;
// EXT_meshopt_compression decompresses ATTRIBUTES, TRIANGLES, and INDICES
// bufferViews (including OCTAHEDRAL, QUATERNION, and EXPONENTIAL filters);
// KHR_texture_basisu decodes KTX 2.0 BasisLZ/ETC1S, UASTC, and uncompressed
// textures in pure Go (reconstructing tangent Z on two-channel normal maps);
// EXT_texture_webp decodes WebP textures (both falling back to alternate
// texture sources when an external side file is missing); EXT_mesh_gpu_instancing expands
// per-instance TRS accessors; KHR_materials_variants populates Model.Variants
// and AssetNode.VariantMaterials for per-instance Scene.SetMaterialVariant
// colorway switching; and undeformed static triangle meshes in Model.Asset
// share *nt3d.SharedGeometry across repeated nodes and repeated
// Instantiate calls. Animation, skins and morphs populate Model.Asset; AddTo
// instantiates its hierarchy and generic clips.
// Normal maps use authored TANGENT frames, or MikkTSpace-compatible ones
// generated per corner (nt3d.GenerateCornerTangents), splitting vertices
// whose corners differ.
// KHR_texture_transform is baked into independent color and normal UVs.
func Decode(data []byte, o *nt3dmesh.Options) (*nt3dmesh.Model, error) {
	if int64(len(data)) > o.ByteLimit() {
		return nil, fmt.Errorf("%w: glTF file", nt3dmesh.ErrTooLarge)
	}
	d := &gltfDecoder{o: o, m: &nt3dmesh.Model{Format: GLTF}, budget: o.TriangleBudget(), assetBudget: o.TriangleBudget(), textures: map[int]*nt3d.Texture{}, warned: map[string]bool{}}
	d.files = sideFiles{bytes: o.ByteBudget()}
	if o != nil {
		d.files.open = o.Open
	}
	d.doc = new(gltf.Document)
	if err := d.decodeContainer(data); err != nil {
		return nil, err
	}
	if err := d.extensions(); err != nil {
		return nil, err
	}
	if err := d.decompressBuffers(); err != nil {
		return nil, err
	}
	if err := d.validateTextures(); err != nil {
		return nil, err
	}
	if err := d.decodeLights(); err != nil {
		return nil, err
	}
	if err := d.decodeVariants(); err != nil {
		return nil, err
	}
	// The model is read in two views: flat Objects, each primitive placed in
	// the scene, and the Asset, each in its own space under its node.
	// Options.AssetOnly skips the first.
	assetOnly := o != nil && o.AssetOnly
	if !assetOnly {
		if err := d.scene(); err != nil {
			return nil, err
		}
		if len(d.m.Objects) == 0 && len(d.doc.Cameras) == 0 && len(d.lights) == 0 {
			return nil, errNoMeshes
		}
		for i := range d.m.Objects {
			if err := d.m.Objects[i].Geometry.Validate(); err != nil {
				return nil, fmt.Errorf("nt3dmesh: glTF %s: %w", d.m.Objects[i].Name, err)
			}
		}
	}
	if err := d.sceneAsset(); err != nil {
		return nil, err
	}
	if assetOnly && !d.assetShows && len(d.doc.Cameras) == 0 && len(d.lights) == 0 {
		return nil, errNoMeshes
	}
	return d.m, nil
}

type gltfDecoder struct {
	// decoded is what the asset pass may hold in decoded accessors and
	// instances; nil, without a limit, in the flat pass.
	decoded   *nt3dmesh.ByteBudget
	budgetErr error
	depth     int
	capture   bool
	assetMode bool
	sources   [][]vertexOrigin
	doc       *gltf.Document
	o         *nt3dmesh.Options
	m         *nt3dmesh.Model
	budget    *nt3dmesh.TriangleBudget
	// The asset pass reads each primitive with a decoder of its own, and
	// totals what they held here, so that the model's limits hold when the
	// flat pass that also enforces them is skipped (Options.AssetOnly).
	assetBudget             *nt3dmesh.TriangleBudget
	assetPoints, assetLines int
	assetShows              bool // The asset has a node that shows something.
	files                   sideFiles
	images                  map[int]image.Image
	imageSources            map[int]decodedSource // By image index: textures share it.
	twoChannelTex           map[int]bool
	normalTextures          map[int]*nt3d.Texture
	ormTextures             map[[2]int]*nt3d.Texture
	textures                map[int]*nt3d.Texture // By texture index; nil when it failed.
	nodeInst                map[int]nodeInstances
	lights                  []punctualLight
	variantNames            []string
	primVariants            map[int]*nt3d.Material
	warned                  map[string]bool
	visits                  int
	points                  int // Across the model, within maxPoints.
	lines                   int // Line ends across the model, within maxLineVertices.
}

func (d *gltfDecoder) warn(format string, a ...any) {
	if msg := fmt.Sprintf(format, a...); !d.warned[msg] {
		d.warned[msg] = true
		d.m.Warnings = append(d.m.Warnings, msg)
	}
}

// sideFiles is the file system the glTF decoder reads external buffers
// from: Options.Open, within the byte budget, which decompressed buffers and
// decoded images share.
type sideFiles struct {
	open  func(string) ([]byte, error)
	bytes *nt3dmesh.ByteBudget
}

// Open rejects streaming reads; qmuntal/gltf calls ReadFile for side buffers.
func (s sideFiles) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
}

// ReadFile loads a named external glTF buffer or asset within the byte budget.
func (s sideFiles) ReadFile(name string) ([]byte, error) {
	if s.open == nil {
		return nil, fmt.Errorf("side file %q needs Options.Open", name)
	}
	data, err := s.open(name)
	if err != nil {
		return nil, err
	}
	if err := s.bytes.Take(int64(len(data)), "glTF side files"); err != nil {
		return nil, err
	}
	return data, nil
}

// mat4 is a column-major affine transform in float64, as glTF stores them:
// nt3dmath.Mat4d.
type mat4 = nt3dmath.Mat4d

// point32 places a vertex: the transform is applied in float64 and the
// result rounded once.
func point32(a mat4, p [3]float32) nt3dmath.Vec3 {
	return a.Point(nt3dmath.Vec3d{X: float64(p[0]), Y: float64(p[1]), Z: float64(p[2])}).Vec3()
}

// direction32 turns a direction by a's linear part, as positions turn:
// tangents, unlike normals, follow the surface.
func direction32(a mat4, v nt3dmath.Vec3) nt3dmath.Vec3 { return a.Direction(v.D()).Vec3() }

// transformNormal turns a normal by the inverse transpose of a's linear part, which
// is the cofactor matrix up to a positive scale (when a does not mirror)
// that normalizing removes.
func transformNormal(a mat4, n [3]float32) nt3dmath.Vec3 {
	c0 := [3]float64{a[0], a[1], a[2]}
	c1 := [3]float64{a[4], a[5], a[6]}
	c2 := [3]float64{a[8], a[9], a[10]}
	cross := func(u, v [3]float64) [3]float64 {
		return [3]float64{u[1]*v[2] - u[2]*v[1], u[2]*v[0] - u[0]*v[2], u[0]*v[1] - u[1]*v[0]}
	}
	r0, r1, r2 := cross(c1, c2), cross(c2, c0), cross(c0, c1)
	x, y, z := float64(n[0]), float64(n[1]), float64(n[2])
	v := nt3dmath.Vec3{X: float32(r0[0]*x + r1[0]*y + r2[0]*z), Y: float32(r0[1]*x + r1[1]*y + r2[1]*z), Z: float32(r0[2]*x + r1[2]*y + r2[2]*z)}
	if a.Determinant3() < 0 {
		v = v.Scale(-1)
	}
	if v.Dot(v) == 0 {
		return v
	}
	return v.Normalize()
}

// yUp turns glTF's Y-up to nt3d's Z-up: (x, y, z) becomes (x, -z, y).
var yUp = mat4{1, 0, 0, 0, 0, 0, 1, 0, 0, -1, 0, 0, 0, 0, 0, 1}

// local is a node's transform: its matrix, or translation, rotation, scale.
func local(n *gltf.Node) mat4 {
	if m := n.MatrixOrDefault(); m != gltf.DefaultMatrix {
		return mat4(m)
	}
	return trsMat4(n.TranslationOrDefault(), n.RotationOrDefault(), n.ScaleOrDefault())
}

// trsMat4 composes a node's translation, rotation (XYZW), and scale.
func trsMat4(t [3]float64, q [4]float64, s [3]float64) mat4 {
	return nt3dmath.TRSd(nt3dmath.Vec3d{X: t[0], Y: t[1], Z: t[2]}, nt3dmath.Quatd{X: q[0], Y: q[1], Z: q[2], W: q[3]}, nt3dmath.Vec3d{X: s[0], Y: s[1], Z: s[2]})
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
		return fmt.Errorf("%w: glTF hierarchy exceeds 1024 levels", nt3dmesh.ErrTooComplex)
	}
	if i < 0 || i >= len(d.doc.Nodes) || d.doc.Nodes[i] == nil {
		return fmt.Errorf("nt3dmesh: glTF names node %d of %d", i, len(d.doc.Nodes))
	}
	if onPath[i] {
		return fmt.Errorf("nt3dmesh: glTF node %d is its own ancestor", i)
	}
	if d.visits++; d.visits > maxVisits {
		return fmt.Errorf("%w: glTF scene reaches more than %d nodes", nt3dmesh.ErrTooComplex, maxVisits)
	}
	n := d.doc.Nodes[i]
	world := parent.Mul(local(n))
	if !world.IsFinite() {
		return fmt.Errorf("nt3dmesh: glTF node %d has a non-finite transform", i)
	}
	insts, instanced, err := d.nodeInstances(i)
	if err != nil {
		return err
	}
	if n.Mesh != nil {
		if *n.Mesh < 0 || *n.Mesh >= len(d.doc.Meshes) || d.doc.Meshes[*n.Mesh] == nil {
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
		if instanced {
			if d.visits += len(insts); d.visits > maxVisits {
				return fmt.Errorf("%w: glTF scene reaches more than %d nodes", nt3dmesh.ErrTooComplex, maxVisits)
			}
			for _, inst := range insts {
				instWorld := world.Mul(inst)
				if !instWorld.IsFinite() {
					return fmt.Errorf("nt3dmesh: glTF node %d has a non-finite instance transform", i)
				}
				for _, p := range mesh.Primitives {
					if err := d.primitive(name, p, instWorld); err != nil {
						return err
					}
				}
			}
		} else {
			for _, p := range mesh.Primitives {
				if err := d.primitive(name, p, world); err != nil {
					return err
				}
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
	if a == nil {
		return nil, fmt.Errorf("nt3dmesh: nil glTF %s accessor %d", what, index)
	}
	if a.Count < 0 || a.Count > 3*d.budget.Limit()+3 {
		return nil, fmt.Errorf("%w: glTF %s has %d elements", nt3dmesh.ErrTooManyTriangles, what, a.Count)
	}
	if err := d.decoded.Take(int64(a.Count)*64, "glTF "+what+" decoded"); err != nil {
		return nil, err
	}
	return a, nil
}

// gltfLook is what a primitive's material says, as nt3d draws it.

func (d *gltfDecoder) samplerRepeat(index int) bool {
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
	return repeat
}

// texture loads a texture by index, once, and whether it repeats.
func (d *gltfDecoder) texture(index int) (*nt3d.Texture, bool) { return d.textureKind(index, false) }
func (d *gltfDecoder) textureKind(index int, normal bool) (*nt3d.Texture, bool) {
	cache := d.textures
	if normal {
		if d.normalTextures == nil {
			d.normalTextures = map[int]*nt3d.Texture{}
		}
		cache = d.normalTextures
	}
	repeat := d.samplerRepeat(index)
	if t, ok := cache[index]; ok {
		return t, repeat
	}
	t, err := d.loadTextureKind(index, normal)
	if err != nil {
		if errors.Is(err, nt3dmesh.ErrTooLarge) && d.budgetErr == nil {
			d.budgetErr = err
		}
		d.warn("texture %d: %v", index, err)
	}
	cache[index] = t
	return t, repeat
}

// nrgbaPixel is a pixel as straight alpha, the conversion color.NRGBAModel
// makes. Images of the kinds decoders hand back are read through their own
// accessors, which return values: At boxes each color in an interface.
func nrgbaPixel(img image.Image, b image.Rectangle, x, y int) color.NRGBA {
	px, py := b.Min.X+x, b.Min.Y+y
	var r, g, bl, a uint32
	switch img := img.(type) {
	case *image.NRGBA:
		return img.NRGBAAt(px, py)
	case *image.RGBA:
		r, g, bl, a = img.RGBAAt(px, py).RGBA()
	case *image.YCbCr:
		r, g, bl, a = img.YCbCrAt(px, py).RGBA()
	case *image.NYCbCrA:
		r, g, bl, a = img.NYCbCrAAt(px, py).RGBA()
	case *image.Gray:
		r, g, bl, a = img.GrayAt(px, py).RGBA()
	default:
		// Any other image: its colors may already be straight alpha, which
		// the model passes through untouched.
		return color.NRGBAModel.Convert(img.At(px, py)).(color.NRGBA)
	}
	switch a {
	case 0xffff:
		return color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8), 0xff}
	case 0:
		return color.NRGBA{}
	}
	r, g, bl = r*0xffff/a, g*0xffff/a, bl*0xffff/a
	return color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8), uint8(a >> 8)}
}

func (d *gltfDecoder) ormTexture(mrIdx, occIdx int) (*nt3d.Texture, bool) {
	primary := mrIdx
	if primary < 0 {
		primary = occIdx
	}
	repeat := d.samplerRepeat(primary)
	if d.ormTextures == nil {
		d.ormTextures = map[[2]int]*nt3d.Texture{}
	}
	key := [2]int{mrIdx, occIdx}
	if t, ok := d.ormTextures[key]; ok {
		return t, repeat
	}
	var mrImg, occImg image.Image
	if mrIdx >= 0 {
		var err error
		if mrImg, err = d.loadImage(mrIdx); err != nil {
			if errors.Is(err, nt3dmesh.ErrTooLarge) && d.budgetErr == nil {
				d.budgetErr = err
			}
			d.warn("texture %d: %v", mrIdx, err)
		}
	}
	if occIdx >= 0 {
		if occIdx == mrIdx {
			occImg = mrImg
		} else {
			var err error
			if occImg, err = d.loadImage(occIdx); err != nil {
				if errors.Is(err, nt3dmesh.ErrTooLarge) && d.budgetErr == nil {
					d.budgetErr = err
				}
				d.warn("texture %d: %v", occIdx, err)
			}
		}
	}
	if mrImg == nil && occImg == nil {
		d.ormTextures[key] = nil
		return nil, repeat
	}
	var t *nt3d.Texture
	var err error
	if mrImg != nil && occImg != nil && mrIdx == occIdx {
		t, err = nt3d.NewORMMap(mrImg)
	} else {
		w, h := 1, 1
		var mrB, occB image.Rectangle
		if mrImg != nil {
			mrB = mrImg.Bounds()
			w, h = mrB.Dx(), mrB.Dy()
		}
		if occImg != nil {
			occB = occImg.Bounds()
			w, h = max(w, occB.Dx()), max(h, occB.Dy())
		}
		packed := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := range h {
			for x := range w {
				r, g, b := uint8(255), uint8(255), uint8(255)
				if occImg != nil {
					ox := min(occB.Dx()-1, x*occB.Dx()/w)
					oy := min(occB.Dy()-1, y*occB.Dy()/h)
					r = nrgbaPixel(occImg, occB, ox, oy).R
				}
				if mrImg != nil {
					mx := min(mrB.Dx()-1, x*mrB.Dx()/w)
					my := min(mrB.Dy()-1, y*mrB.Dy()/h)
					c := nrgbaPixel(mrImg, mrB, mx, my)
					g, b = c.G, c.B
				}
				off := y*packed.Stride + x*4
				packed.Pix[off], packed.Pix[off+1], packed.Pix[off+2], packed.Pix[off+3] = r, g, b, 255
			}
		}
		t, err = nt3d.NewORMMap(packed)
	}
	if err != nil {
		d.warn("ORM texture: %v", err)
	}
	d.ormTextures[key] = t
	return t, repeat
}

func (d *gltfDecoder) validateTextures() error {
	for i, tex := range d.doc.Textures {
		if tex == nil {
			continue
		}
		for _, extName := range [...]string{"KHR_texture_basisu", "EXT_texture_webp"} {
			var srcExt struct {
				Source *int `json:"source"`
			}
			ok, err := decodeExtension(tex.Extensions, extName, &srcExt)
			if err != nil {
				return err
			}
			if ok && (srcExt.Source == nil || *srcExt.Source < 0 || *srcExt.Source >= len(d.doc.Images) || d.doc.Images[*srcExt.Source] == nil) {
				return fmt.Errorf("nt3dmesh: texture %d has invalid %s source", i, extName)
			}
		}
	}
	return nil
}

func (d *gltfDecoder) loadImage(index int) (image.Image, error) {
	if d.images != nil {
		if img, ok := d.images[index]; ok {
			return img, nil
		}
	}
	if index < 0 || index >= len(d.doc.Textures) || d.doc.Textures[index] == nil {
		return nil, errors.New("does not exist")
	}
	tex := d.doc.Textures[index]
	var candidates []int
	addCandidate := func(src *int) {
		if src == nil || slices.Contains(candidates, *src) {
			return
		}
		candidates = append(candidates, *src)
	}
	for _, extName := range [...]string{"KHR_texture_basisu", "EXT_texture_webp"} {
		var srcExt struct {
			Source *int `json:"source"`
		}
		ok, err := decodeExtension(tex.Extensions, extName, &srcExt)
		if err != nil {
			return nil, err
		}
		if ok {
			addCandidate(srcExt.Source)
		}
	}
	addCandidate(tex.Source)
	if len(candidates) == 0 {
		return nil, errors.New("has no image nt3d can read")
	}

	var img image.Image
	var twoCh bool
	var firstErr error
	for _, src := range candidates {
		loaded, tc, err := d.loadImageSource(src)
		if err == nil {
			img, twoCh, firstErr = loaded, tc, nil
			break
		}
		if errors.Is(err, nt3dmesh.ErrTooLarge) {
			return nil, err
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	if d.images == nil {
		d.images = map[int]image.Image{}
	}
	d.images[index] = img
	if twoCh {
		if d.twoChannelTex == nil {
			d.twoChannelTex = map[int]bool{}
		}
		d.twoChannelTex[index] = true
	}
	return img, nil
}

// decodedSource is an image of the document once decoded, or why it could
// not be.
type decodedSource struct {
	img        image.Image
	twoChannel bool
	err        error
}

// loadImageSource decodes an image of the document once, however many
// textures name it, and charges its decoded pixels to the byte budget.
func (d *gltfDecoder) loadImageSource(src int) (image.Image, bool, error) {
	if s, ok := d.imageSources[src]; ok {
		return s.img, s.twoChannel, s.err
	}
	img, twoChannel, err := d.decodeImageSource(src)
	if d.imageSources == nil {
		d.imageSources = map[int]decodedSource{}
	}
	d.imageSources[src] = decodedSource{img, twoChannel, err}
	return img, twoChannel, err
}

func (d *gltfDecoder) decodeImageSource(src int) (image.Image, bool, error) {
	if src < 0 || src >= len(d.doc.Images) || d.doc.Images[src] == nil {
		return nil, false, errors.New("has no image nt3d can read")
	}
	im := d.doc.Images[src]
	var data []byte
	var err error
	switch {
	case im.BufferView != nil:
		if *im.BufferView < 0 || *im.BufferView >= len(d.doc.BufferViews) {
			return nil, false, errors.New("names a missing buffer view")
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
		return nil, false, err
	}
	if isKTX2(data) || im.MimeType == "image/ktx2" {
		return decodeKTX2(data, d.files.bytes)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, false, errors.New("not a PNG, JPEG, KTX2, or registered image format")
	}
	if config.Width > nt3d.MaxTextureSide || config.Height > nt3d.MaxTextureSide {
		return nil, false, fmt.Errorf("larger than %d pixels a side", nt3d.MaxTextureSide)
	}
	// Decoded pixels share the byte budget, as a KTX2 image's do.
	if err := d.files.bytes.Take(int64(config.Width)*int64(config.Height)*4, "glTF image decodes to more than MaxBytes has left"); err != nil {
		return nil, false, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, false, err
	}
	return img, false, nil
}

func (d *gltfDecoder) loadTextureKind(index int, normal bool) (*nt3d.Texture, error) {
	img, err := d.loadImage(index)
	if err != nil {
		return nil, err
	}
	if normal {
		if d.twoChannelTex[index] {
			img = reconstructNormalXYImage(img)
		}
		return nt3d.NewNormalMap(img)
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
	positions, err := d.readPositions(position)
	if err != nil {
		return fmt.Errorf("nt3dmesh: glTF %s positions: %w", name, err)
	}
	if splats, err := d.gaussians(name, p, world, positions); splats || err != nil {
		return err
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
	if l.err != nil {
		return l.err
	}
	d.primVariants = nil
	var varEntries []variantEntry
	var varLooks []gltfLook
	isTriangles := p.Mode == gltf.PrimitiveTriangles || p.Mode == gltf.PrimitiveTriangleStrip || p.Mode == gltf.PrimitiveTriangleFan
	if d.assetMode {
		varEntries, err = d.primitiveVariants(p)
		if err != nil {
			return err
		}
		if len(varEntries) > 0 && !isTriangles {
			d.warn("%s: KHR_materials_variants on points or lines is ignored", name)
			varEntries = nil
		}
		if len(varEntries) > 0 {
			varLooks = make([]gltfLook, len(varEntries))
			for k, ve := range varEntries {
				vl := d.look(&ve.material)
				if vl.err != nil {
					return vl.err
				}
				varLooks[k] = vl
			}
		}
	}
	anyNormal := l.normal != nil
	for k := range varLooks {
		if varLooks[k].normal != nil {
			anyNormal = true
		}
	}
	b := &gltfBuilder{
		d:            d,
		name:         name,
		look:         l,
		world:        world,
		positions:    positions,
		useBaseColor: len(varEntries) > 0,
		anyNormal:    anyNormal,
	}
	if a, ok, err := attr(gltf.NORMAL); err != nil {
		return err
	} else if ok {
		if b.normals, err = d.readNormals(a); err != nil || len(b.normals) != len(positions) {
			d.warn("%s: unreadable normals are computed", name)
			b.normals = nil
		}
		for _, n := range b.normals {
			if !nt3dmesh.ValidCoordinate(n[0], n[1], n[2]) {
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
	if b.anyNormal {
		if a, ok, err := attr(gltf.TANGENT); err != nil {
			return err
		} else if ok {
			tangents, err := d.readTangents(a)
			if err != nil || len(tangents) != len(positions) {
				d.warn("%s: unreadable tangents are generated", name)
			} else {
				b.tangents = make([]nt3d.Tangent, len(tangents))
				for i, t := range tangents {
					if !nt3dmesh.ValidCoordinate(t[0], t[1], t[2]) || !nt3dmesh.ValidCoordinate(t[3], 0, 0) {
						return fmt.Errorf("nt3dmesh: glTF %s has a non-finite tangent", name)
					}
					b.tangents[i] = nt3d.Tangent{Direction: nt3dmath.Vec3{X: t[0], Y: t[1], Z: t[2]}, W: 1}
					if t[3] < 0 {
						b.tangents[i].W = -1
					}
				}
			}
		}
	}
	readUV := func(mapping textureMapping) ([][2]float32, error) {
		a, ok, err := attr(fmt.Sprintf("TEXCOORD_%d", mapping.coord))
		if err != nil {
			return nil, err
		}
		if !ok {
			d.warn("%s: texture has no TEXCOORD_%d", name, mapping.coord)
			return nil, nil
		}
		uv, err := d.readTexCoords(a)
		if err != nil || len(uv) != len(positions) {
			return nil, fmt.Errorf("nt3dmesh: invalid texture coordinates")
		}
		for i, v := range uv {
			uv[i], err = mapping.apply(v)
			if err != nil {
				return nil, err
			}
		}
		return uv, nil
	}
	if l.texture != nil {
		b.uv, err = readUV(l.mapping)
		if err != nil {
			return err
		}
		if b.uv == nil {
			b.look.texture = nil
		}
	}
	if l.orm != nil {
		ormUV, err := readUV(l.ormMapping)
		if err != nil {
			return err
		}
		if ormUV == nil {
			b.look.orm = nil
		} else if b.uv == nil {
			b.uv = ormUV
		} else if !slices.Equal(b.uv, ormUV) {
			d.warn("%s: ORM texture uses distinct UVs; sharing base UVs", name)
		}
	}
	if l.emissiveMap != nil {
		emUV, err := readUV(l.emissiveMapping)
		if err != nil {
			return err
		}
		if emUV == nil {
			b.look.emissiveMap = nil
		} else if b.uv == nil {
			b.uv = emUV
		} else if !slices.Equal(b.uv, emUV) {
			d.warn("%s: emissive texture uses distinct UVs; sharing base UVs", name)
		}
	}
	if l.normal != nil {
		normalUV, err := readUV(l.normalMapping)
		if err != nil {
			return err
		}
		if normalUV == nil {
			b.look.normal = nil
		} else if b.uv == nil {
			b.uv = normalUV
		} else if !slices.Equal(b.uv, normalUV) {
			b.normalUV = normalUV
		}
	}
	for k := range varLooks {
		vl := &varLooks[k]
		if vl.double != b.look.double {
			d.warn("%s: variant material changes doubleSided; sharing default faces", name)
		}
		if vl.texture != nil {
			uv, err := readUV(vl.mapping)
			if err != nil {
				return err
			}
			if uv == nil {
				vl.texture = nil
			} else if b.uv == nil {
				b.uv = uv
			} else if !slices.Equal(b.uv, uv) {
				d.warn("%s: variant texture uses distinct UVs; sharing base UVs", name)
			}
		}
		if vl.orm != nil {
			ormUV, err := readUV(vl.ormMapping)
			if err != nil {
				return err
			}
			if ormUV == nil {
				vl.orm = nil
			} else if b.uv == nil {
				b.uv = ormUV
			} else if !slices.Equal(b.uv, ormUV) {
				d.warn("%s: variant ORM texture uses distinct UVs; sharing base UVs", name)
			}
		}
		if vl.emissiveMap != nil {
			emUV, err := readUV(vl.emissiveMapping)
			if err != nil {
				return err
			}
			if emUV == nil {
				vl.emissiveMap = nil
			} else if b.uv == nil {
				b.uv = emUV
			} else if !slices.Equal(b.uv, emUV) {
				d.warn("%s: variant emissive texture uses distinct UVs; sharing base UVs", name)
			}
		}
		if vl.normal != nil {
			normalUV, err := readUV(vl.normalMapping)
			if err != nil {
				return err
			}
			if normalUV == nil {
				vl.normal = nil
			} else if b.uv == nil {
				b.uv = normalUV
			} else if !slices.Equal(b.uv, normalUV) {
				if b.normalUV == nil {
					b.normalUV = normalUV
				} else if !slices.Equal(b.normalUV, normalUV) {
					d.warn("%s: variant normal texture uses distinct UVs; sharing normal UVs", name)
				}
			}
		}
	}
	b.anyNormal = b.look.normal != nil
	for k := range varLooks {
		if varLooks[k].normal != nil {
			b.anyNormal = true
		}
	}
	if len(varEntries) > 0 {
		d.primVariants = make(map[int]*nt3d.Material, len(varEntries))
		for k, ve := range varEntries {
			d.primVariants[ve.variant] = d.lookMaterial(varLooks[k], b.colors != nil, true)
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
	d            *gltfDecoder
	name         string
	look         gltfLook
	world        mat4
	positions    [][3]float32
	normals      [][3]float32
	colors       [][4]uint16
	uv           [][2]float32
	normalUV     [][2]float32
	tangents     []nt3d.Tangent // Authored, per source vertex, in the primitive's space.
	useBaseColor bool
	anyNormal    bool
	// Generated frames, per corner of the triangle list, placed by world
	// for a front face, and which of its source vertex's distinct frames
	// each corner has: corners that differ split the vertex.
	corners  []nt3d.Tangent
	variants []uint16
	material *nt3d.Material
	piece    *nt3dmesh.Object
	remap    map[remapKey]uint32 // Source vertex to the piece's.
}

// remapKey names a piece vertex: its source vertex, which side, and which
// of its tangent frames.
type remapKey struct {
	index   uint32
	back    bool
	variant uint16
}

// color is a vertex's sRGB color and alpha: the base color factor times the
// vertex color, both linear (or un-factored vertex color when BaseColor is
// carried per material for KHR_materials_variants).
func (b *gltfBuilder) color(i uint32) (color.RGBA, bool) {
	if b.useBaseColor {
		if b.colors == nil {
			return color.RGBA{255, 255, 255, 255}, false
		}
		c := b.colors[i]
		return color.RGBA{
			srgb8(float64(c[0]) / 65535),
			srgb8(float64(c[1]) / 65535),
			srgb8(float64(c[2]) / 65535),
			uint8(max(0, min(1, float64(c[3])/65535))*255 + .5),
		}, false
	}
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
	return b.d.lookMaterial(b.look, b.colors != nil, b.useBaseColor)
}

func (d *gltfDecoder) lookMaterial(l gltfLook, hasColors, useBaseColor bool) *nt3d.Material {
	m := nt3d.Material{
		Texture: l.texture,
		Repeat:  l.repeat && l.texture != nil,
		Normal:  nt3d.NormalStyle{Map: l.normal, Strength: l.normalStrength, Repeat: l.normalRepeat && l.normal != nil},
		Emission: nt3d.EmissionStyle{
			Color: l.emissive, Intensity: l.emissiveIntensity,
			Map: l.emissiveMap, Repeat: l.emissiveRepeat && l.emissiveMap != nil,
		},
		Transmission: nt3d.TransmissionStyle{Amount: l.transmission, Refraction: l.refraction, Absorption: l.absorption},
	}
	switch {
	case l.unlit:
		m.Shading = nt3d.ShadeUnlit
	case l.pbr:
		// Only a PBR material may carry the PBR group: nt3d refuses it on
		// any other.
		m.Shading = nt3d.ShadePBR
		m.PBR = nt3d.PBRStyle{
			Metallic: l.metallic, Roughness: l.roughness, IOR: l.ior,
			ORMMap: l.orm, ORMRepeat: l.ormRepeat && l.orm != nil,
			Specular:  l.specular,
			Clearcoat: nt3d.ClearcoatStyle{Amount: l.clearcoat, Roughness: l.clearcoatRoughness},
			Sheen:     nt3d.SheenStyle{Color: l.sheenColor, Roughness: l.sheenRoughness},
		}
		if l.orm != nil {
			m.PBR.OcclusionStrength = l.occlusionStrength
		}
	}
	switch l.alpha {
	case gltf.AlphaMask:
		if l.texture != nil {
			cut := l.cutoff
			if useBaseColor && l.factor[3] < 1 {
				if l.factor[3] <= 0 {
					cut = 1
				} else {
					cut = min(1, l.cutoff/l.factor[3])
				}
			}
			m.Alpha, m.Threshold = nt3d.AlphaCutout, float32(max(.001, min(1, cut)))
		} else if useBaseColor && !hasColors && l.factor[3] < l.cutoff {
			m.Alpha = nt3d.AlphaTransparent
			m.BaseColor = color.RGBA{R: 255, G: 255, B: 255, A: 0}
		}
	case gltf.AlphaBlend:
		m.Alpha = nt3d.AlphaTransparent
	}
	if useBaseColor {
		if m.Alpha != nt3d.AlphaTransparent {
			if l.plain {
				if !hasColors {
					bc := d.o.FaceColor()
					bc.A = 255
					if bc != (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
						m.BaseColor = bc
					}
				}
			} else {
				bc := color.RGBA{R: srgb8(l.factor[0]), G: srgb8(l.factor[1]), B: srgb8(l.factor[2]), A: 255}
				if bc != (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
					m.BaseColor = bc
				}
			}
		} else if m.BaseColor == (color.RGBA{}) {
			if l.plain {
				if !hasColors {
					bc := d.o.FaceColor()
					if bc == (color.RGBA{}) {
						bc = color.RGBA{R: 255, G: 255, B: 255, A: 0}
					}
					if bc != (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
						m.BaseColor = bc
					}
				}
			} else {
				bc := color.RGBA{
					R: srgb8(l.factor[0]),
					G: srgb8(l.factor[1]),
					B: srgb8(l.factor[2]),
					A: uint8(max(0, min(1, l.factor[3]))*255 + .5),
				}
				if bc == (color.RGBA{}) {
					bc = color.RGBA{R: 255, G: 255, B: 255, A: 0}
				}
				if bc != (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
					m.BaseColor = bc
				}
			}
		}
		if l.texture != nil {
			m.Tint = hasColors
		}
	} else if l.texture != nil {
		// The texture replaces vertex colors unless they tint it.
		m.Tint = hasColors || l.factor[0] != 1 || l.factor[1] != 1 || l.factor[2] != 1
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
	b.remap = map[remapKey]uint32{}
}

func (b *gltfBuilder) place(i uint32) (nt3dmath.Vec3, error) {
	p := point32(b.world, b.positions[i])
	if !nt3dmesh.ValidCoordinate(p.X, p.Y, p.Z) {
		return p, fmt.Errorf("nt3dmesh: glTF %s vertex is non-finite or exceeds coordinate limits", b.name)
	}
	return p, nil
}

func (b *gltfBuilder) triangles(v []uint32) error {
	b.material = b.nt3dMaterial()
	first := len(b.d.m.Objects)
	mirror := b.world.Determinant3() < 0 // Mirroring turns faces inside out; turn them back.
	if b.anyNormal && b.tangents == nil {
		b.generateTangents(v, mirror)
	}
	for _, back := range []bool{false, true} {
		if back && !b.look.double {
			break
		}
		for t := 0; t+2 < len(v); t += 3 {
			tri := [3]uint32{v[t], v[t+1], v[t+2]}
			if mirror != back {
				tri[1], tri[2] = tri[2], tri[1]
			}
			if !b.useBaseColor && b.look.alpha == gltf.AlphaMask && b.look.texture == nil {
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
			if err := b.d.budget.Take(1); err != nil {
				return err
			}
			at := [3]int{t, t + 1, t + 2}
			if mirror != back {
				at[1], at[2] = at[2], at[1]
			}
			if err := b.face(tri, at, back); err != nil {
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

// generateTangents finds a frame for each corner of the triangle list,
// MikkTSpace-compatibly under glTF's image-up convention, in the
// primitive's space with its authored normals (or each face's own), then
// places it by world. A mirrored placement turns the handedness, as it
// does the faces.
func (b *gltfBuilder) generateTangents(v []uint32, mirror bool) {
	uv := b.normalUV
	if uv == nil {
		uv = b.uv
	}
	if uv == nil {
		return
	}
	n := len(v) / 3 * 3
	positions, normals, uvs := make([]nt3dmath.Vec3, n), make([]nt3dmath.Vec3, n), make([]nt3d.UV, n)
	for t := 0; t < n; t += 3 {
		var flat nt3dmath.Vec3
		if b.normals == nil {
			p0, p1, p2 := vec(b.positions[v[t]]), vec(b.positions[v[t+1]]), vec(b.positions[v[t+2]])
			if flat = p1.Sub(p0).Cross(p2.Sub(p0)); flat.Dot(flat) > 0 {
				flat = flat.Normalize()
			}
		}
		for k := t; k < t+3; k++ {
			i := v[k]
			positions[k], normals[k], uvs[k] = vec(b.positions[i]), flat, nt3d.UV{U: uv[i][0], V: uv[i][1]}
			if b.normals != nil {
				normals[k] = vec(b.normals[i])
			}
		}
	}
	b.corners = nt3d.GenerateCornerTangents(positions, normals, uvs)
	b.variants = make([]uint16, n)
	seen := map[uint32][]nt3d.Tangent{}
	for k := range b.corners {
		c := &b.corners[k]
		if d := direction32(b.world, c.Direction); d.Dot(d) > 0 {
			c.Direction = d.Normalize()
		}
		// glTF's green points up the image, against V, where MikkTSpace
		// takes V as up: the handedness turns, as authored frames (Blender's,
		// found with V up) have it. A mirrored placement turns it back.
		if !mirror {
			c.W = -c.W
		}
		frames := seen[v[k]]
		variant := -1
		for j, f := range frames {
			if f.W == c.W && f.Direction.Dot(c.Direction) > 1-1e-6 {
				variant = j
				break
			}
		}
		if variant < 0 {
			variant = len(frames)
			seen[v[k]] = append(frames, *c)
		}
		b.variants[k] = uint16(min(variant, math.MaxUint16))
	}
}

// face adds a triangle, sharing vertices within the piece unless their
// normals are computed per face. at is each corner's index in the
// primitive's triangle list.
func (b *gltfBuilder) face(tri [3]uint32, at [3]int, back bool) error {
	limit := pieceTriangles()
	if b.anyNormal {
		// Tangents or independent normal UVs take an extension record.
		limit = min(limit, nt3d.MeshTriangleLimit()/2)
	}
	if b.piece == nil || len(b.piece.Geometry.Indices) >= 3*limit {
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
		key := remapKey{index: i, back: back}
		var tangent *nt3d.Tangent
		if b.tangents != nil {
			t := b.tangents[i]
			if d := direction32(b.world, t.Direction); d.Dot(d) > 0 {
				t.Direction = d.Normalize()
			}
			if b.world.Determinant3() < 0 {
				t.W = -t.W
			}
			tangent = &t
		} else if b.corners != nil {
			t := b.corners[at[k]]
			key.variant = b.variants[at[k]]
			tangent = &t
		}
		if tangent != nil && back {
			tangent.W = -tangent.W // Its normal is negated.
		}
		if j, ok := b.remap[key]; ok {
			g.Indices = append(g.Indices, j)
			continue
		}
		n := flat
		if b.normals != nil {
			n = transformNormal(b.world, b.normals[i])
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
		if b.normalUV != nil {
			g.NormalUV = append(g.NormalUV, nt3d.UV{U: b.normalUV[i][0], V: b.normalUV[i][1]})
		}
		if b.uv != nil {
			g.UV = append(g.UV, nt3d.UV{U: b.uv[i][0], V: b.uv[i][1]})
		}
		if tangent != nil {
			g.Tangents = append(g.Tangents, *tangent)
		}
	}
	return nil
}

// maxPoints caps a model's points, across its primitives. Tests lower it.
var maxPoints = nt3d.PointLimit

func (b *gltfBuilder) points(v []uint32) error {
	if b.d.points += len(v); b.d.points > maxPoints() {
		return fmt.Errorf("%w: glTF has more than %d points", nt3dmesh.ErrTooComplex, maxPoints())
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
		return fmt.Errorf("%w: glTF has more than %d line ends", nt3dmesh.ErrTooComplex, maxLineVertices)
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
