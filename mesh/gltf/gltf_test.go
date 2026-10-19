// Copyright (c) 2026 Neomantra Corp.
package nt3dgltf

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/fs"
	"math"
	"strings"
	"testing"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
)

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func fails(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error %v, want one containing %q", err, want)
	}
}

func opener(files map[string][]byte) func(string) ([]byte, error) {
	return func(name string) ([]byte, error) {
		if data, ok := files[name]; ok {
			return data, nil
		}
		return nil, errors.New("not found")
	}
}

// prim is a test primitive: positions (glTF Y-up), and what else it has.
type prim struct {
	positions [][3]float32
	indices   []uint16
	normals   [][3]float32
	colors    [][4]uint8
	uv        [][2]float32
	material  *int
	mode      gltf.PrimitiveMode
}

// gltfDoc is a document with one mesh of the primitives and one node
// showing it.
func gltfDoc(prims ...prim) *gltf.Document {
	doc := gltf.NewDocument()
	mesh := &gltf.Mesh{Name: "thing"}
	for _, p := range prims {
		attrs := gltf.PrimitiveAttributes{gltf.POSITION: modeler.WritePosition(doc, p.positions)}
		if p.normals != nil {
			attrs[gltf.NORMAL] = modeler.WriteNormal(doc, p.normals)
		}
		if p.colors != nil {
			attrs[gltf.COLOR_0] = modeler.WriteColor(doc, p.colors)
		}
		if p.uv != nil {
			attrs[gltf.TEXCOORD_0] = modeler.WriteTextureCoord(doc, p.uv)
		}
		gp := &gltf.Primitive{Attributes: attrs, Material: p.material, Mode: p.mode}
		if p.indices != nil {
			gp.Indices = gltf.Index(modeler.WriteIndices(doc, p.indices))
		}
		mesh.Primitives = append(mesh.Primitives, gp)
	}
	doc.Meshes = []*gltf.Mesh{mesh}
	doc.Nodes = []*gltf.Node{{Name: "node", Mesh: gltf.Index(0)}}
	doc.Scenes = []*gltf.Scene{{Nodes: []int{0}}}
	doc.Scene = gltf.Index(0)
	return doc
}

func glb(t *testing.T, doc *gltf.Document) []byte {
	t.Helper()
	var buf bytes.Buffer
	check(t, gltf.NewEncoder(&buf).Encode(doc))
	return buf.Bytes()
}

// upTriangle faces +Z in glTF (toward the viewer of its XY plane).
var upTriangle = prim{positions: [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}}

func near(a, b nt3dmath.Vec3) bool {
	d := a.Sub(b)
	return d.Dot(d) < 1e-8
}

func TestGLTFTriangle(t *testing.T) {
	data := glb(t, gltfDoc(upTriangle))
	if f, ok := nt3dmesh.FormatOf("x.bin", data); !ok || f != GLTF {
		t.Fatalf("binary glTF detected as %q", f)
	}
	for _, name := range []string{"a.gltf", "a.GLB"} {
		if f, _ := nt3dmesh.FormatOf(name, nil); f != GLTF {
			t.Fatalf("%s is %q", name, f)
		}
	}
	m, err := nt3dmesh.Decode(data, GLTF, &nt3dmesh.Options{Color: color.RGBA{9, 8, 7, 255}, Up: nt3dmesh.YUp})
	check(t, err)
	if len(m.Objects) != 1 || m.Triangles() != 1 || m.Objects[0].Name != "node" {
		t.Fatalf("objects %+v", m.Objects)
	}
	g := m.Objects[0].Geometry
	// Y-up turned Z-up (Options.Up ignored): (0, 1, 0) is (0, 0, 1), and the
	// face's +Z normal is -Y.
	if !near(g.Vertices[2].Position, nt3dmath.Vec3{Z: 1}) || !near(g.Vertices[0].Normal, nt3dmath.Vec3{Y: -1}) {
		t.Fatalf("vertices %+v", g.Vertices)
	}
	if g.Vertices[0].Color != (color.RGBA{9, 8, 7, 255}) || !m.Objects[0].Plain[0] || g.Material != nil {
		t.Fatalf("plain triangle: %+v, plain %v, material %v", g.Vertices[0].Color, m.Objects[0].Plain, g.Material)
	}
	if !g.Bounds.IsValid() {
		t.Fatal("no bounds")
	}
}

// Nodes place meshes by their world transforms; a mesh shown by two nodes
// becomes two objects.
func TestGLTFHierarchy(t *testing.T) {
	doc := gltfDoc(upTriangle)
	s := math.Sqrt(.5)
	doc.Nodes = []*gltf.Node{
		{Name: "parent", Translation: [3]float64{10, 0, 0}, Children: []int{1, 2}},
		{Name: "turned", Mesh: gltf.Index(0), Rotation: [4]float64{0, 0, s, s}, Scale: [3]float64{2, 2, 2}}, // 90° about Z.
		{Name: "moved", Mesh: gltf.Index(0), Matrix: [16]float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 5, 0, 1}},
	}
	m, err := Decode(glb(t, doc), nil)
	check(t, err)
	if len(m.Objects) != 2 || m.Objects[0].Name != "turned" || m.Objects[1].Name != "moved" {
		t.Fatalf("objects %+v", m.Objects)
	}
	// glTF (1,0,0), scaled 2 and turned 90° about Z, is (0,2,0), moved to
	// (10,2,0); Z-up, (10,0,2).
	if p := m.Objects[0].Geometry.Vertices[1].Position; !near(p, nt3dmath.Vec3{X: 10, Z: 2}) {
		t.Fatalf("turned vertex at %v", p)
	}
	// glTF (0,1,0) moved by (10,5,0) is (10,6,0); Z-up, (10,0,6).
	if p := m.Objects[1].Geometry.Vertices[2].Position; !near(p, nt3dmath.Vec3{X: 10, Z: 6}) {
		t.Fatalf("moved vertex at %v", p)
	}
}

// A mirroring transform keeps faces facing out: the winding follows the
// normals the file gives.
func TestGLTFMirror(t *testing.T) {
	p := upTriangle
	p.normals = [][3]float32{{0, 0, 1}, {0, 0, 1}, {0, 0, 1}}
	doc := gltfDoc(p)
	doc.Nodes[0].Scale = [3]float64{1, 1, -1}
	m, err := Decode(glb(t, doc), nil)
	check(t, err)
	g := m.Objects[0].Geometry
	v := g.Vertices
	a, b, c := v[g.Indices[0]].Position, v[g.Indices[1]].Position, v[g.Indices[2]].Position
	face := b.Sub(a).Cross(c.Sub(a))
	if face.Dot(v[0].Normal) <= 0 {
		t.Fatalf("mirrored face %v against normal %v", face, v[0].Normal)
	}
}

func pngTexture(t *testing.T, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for i := 0; i < 4; i++ {
		img.SetNRGBA(i%2, i/2, c)
	}
	var buf bytes.Buffer
	check(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestGLTFMaterials(t *testing.T) {
	quad := func(material int) prim {
		return prim{
			positions: [][3]float32{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}},
			indices:   []uint16{0, 1, 2, 0, 2, 3},
			uv:        [][2]float32{{0, 0}, {1, 0}, {1, 1}, {0, 1}},
			material:  gltf.Index(material),
		}
	}
	doc := gltfDoc(quad(0), quad(1), quad(2), quad(3), quad(4), quad(5))
	img, err := modeler.WriteImage(doc, "leaf", "image/png", bytes.NewReader(pngTexture(t, color.NRGBA{200, 100, 50, 128})))
	check(t, err)
	doc.Samplers = []*gltf.Sampler{{WrapS: gltf.WrapClampToEdge, WrapT: gltf.WrapClampToEdge}}
	doc.Textures = []*gltf.Texture{{Source: gltf.Index(img)}, {Source: gltf.Index(img), Sampler: gltf.Index(0)}}
	pbr := func(c [4]float64, rough float64, texture *int) *gltf.PBRMetallicRoughness {
		p := &gltf.PBRMetallicRoughness{BaseColorFactor: &c, RoughnessFactor: gltf.Float(rough), MetallicFactor: gltf.Float(0)}
		if texture != nil {
			p.BaseColorTexture = &gltf.TextureInfo{Index: *texture}
		}
		return p
	}
	doc.Materials = []*gltf.Material{
		{Name: "gray", PBRMetallicRoughness: pbr([4]float64{.5, .5, .5, 1}, 1, nil)},
		{Name: "leaf", PBRMetallicRoughness: pbr([4]float64{1, 1, 1, 1}, 1, gltf.Index(0)), AlphaMode: gltf.AlphaMask, AlphaCutoff: gltf.Float(.4), DoubleSided: true},
		{Name: "glass", PBRMetallicRoughness: pbr([4]float64{1, 1, 1, .25}, .2, nil), AlphaMode: gltf.AlphaBlend},
		{Name: "flat", PBRMetallicRoughness: pbr([4]float64{1, 0, 0, 1}, .2, nil), Extensions: gltf.Extensions{"KHR_materials_unlit": map[string]any{}}},
		{Name: "tinted", PBRMetallicRoughness: pbr([4]float64{1, 0, 0, 1}, 1, gltf.Index(1))},
		{Name: "missing texture", PBRMetallicRoughness: pbr([4]float64{1, 1, 1, 1}, 1, gltf.Index(7))},
	}
	m, err := Decode(glb(t, doc), nil)
	check(t, err)
	byName := map[string]nt3dmesh.Object{}
	for _, o := range m.Objects {
		byName[o.Material] = o
	}
	// Linear 0.5 is sRGB 188.
	if g := byName["gray"].Geometry; g.Vertices[0].Color != (color.RGBA{188, 188, 188, 255}) || g.Material != nil || byName["gray"].Plain[0] {
		t.Errorf("gray: %v, material %+v", g.Vertices[0].Color, g.Material)
	}
	leaf := byName["leaf"].Geometry
	if m := leaf.Material; m == nil || !m.Cutout || m.Threshold != .4 || m.Texture == nil || !m.Repeat || m.Tint || m.Specular != 0 {
		t.Errorf("leaf material %+v", m)
	} else if a := m.Texture.At(0, 0).(color.RGBA).A; a != 128 {
		t.Errorf("leaf texture lost its alpha: %d", a)
	}
	if len(leaf.Indices) != 12 || leaf.Vertices[leaf.Indices[6]].Normal.Dot(leaf.Vertices[leaf.Indices[0]].Normal) >= 0 {
		t.Errorf("double-sided leaf: %d indices", len(leaf.Indices))
	}
	if g := byName["glass"].Geometry; g.Material == nil || !g.Material.Transparent || g.Vertices[0].Color.A != 64 || g.Material.Specular == 0 {
		t.Errorf("glass: %+v, color %v", g.Material, g.Vertices[0].Color)
	}
	if g := byName["flat"].Geometry; g.Material == nil || !g.Material.Unlit || g.Material.Specular != 0 || g.Vertices[0].Color != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("unlit: %+v", g.Material)
	}
	if g := byName["tinted"].Geometry; g.Material == nil || !g.Material.Tint || g.Material.Repeat || g.Vertices[0].Color != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("tinted, clamped: %+v", g.Material)
	}
	if g := byName["missing texture"].Geometry; g.Material != nil || len(m.Warnings) == 0 {
		t.Errorf("missing texture: %+v, warnings %v", g.Material, m.Warnings)
	}
	// Every object must be valid nt3d geometry.
	for _, o := range m.Objects {
		check(t, o.Geometry.Validate())
	}
}

// Vertex colors are linear, multiplied by the base color.
func TestGLTFVertexColors(t *testing.T) {
	p := upTriangle
	p.colors = [][4]uint8{{255, 0, 0, 255}, {0, 128, 0, 255}, {0, 0, 0, 255}}
	m, err := Decode(glb(t, gltfDoc(p)), nil)
	check(t, err)
	v := m.Objects[0].Geometry.Vertices
	if v[0].Color != (color.RGBA{255, 0, 0, 255}) || v[1].Color.G != srgb8(128./255) || m.Objects[0].Plain[0] {
		t.Fatalf("colors %v %v, plain %v", v[0].Color, v[1].Color, m.Objects[0].Plain)
	}
}

func TestGLTFModes(t *testing.T) {
	// Strips zigzag across the square; fans go around it.
	zigzag := [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {1, 1, 0}}
	around := [][3]float32{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}}
	for _, c := range []struct {
		mode            gltf.PrimitiveMode
		triangles, ends int // Ends: line vertices, or points.
	}{
		{gltf.PrimitiveTriangleStrip, 2, 0},
		{gltf.PrimitiveTriangleFan, 2, 0},
		{gltf.PrimitivePoints, 0, 4},
		{gltf.PrimitiveLines, 0, 4},
		{gltf.PrimitiveLineStrip, 0, 6},
		{gltf.PrimitiveLineLoop, 0, 8},
	} {
		square := zigzag
		if c.mode == gltf.PrimitiveTriangleFan {
			square = around
		}
		m, err := Decode(glb(t, gltfDoc(prim{positions: square, mode: c.mode})), nil)
		check(t, err)
		g := m.Objects[0].Geometry
		if m.Triangles() != c.triangles || len(g.Points)+len(g.Lines) != c.ends {
			t.Errorf("mode %v: %d triangles, %d points, %d line ends", c.mode, m.Triangles(), len(g.Points), len(g.Lines))
		}
		// Every strip and fan triangle faces glTF's +Z: Z-up's -Y.
		for i := 0; i+2 < len(g.Indices); i += 3 {
			a, b, d := g.Vertices[g.Indices[i]].Position, g.Vertices[g.Indices[i+1]].Position, g.Vertices[g.Indices[i+2]].Position
			if b.Sub(a).Cross(d.Sub(a)).Y >= 0 {
				t.Errorf("mode %v: triangle %d faces away", c.mode, i/3)
			}
		}
	}
}

// memFS collects what the encoder writes beside a .gltf.
type memFS map[string]*bytes.Buffer

func (m memFS) Open(name string) (fs.File, error) { return nil, fs.ErrNotExist }
func (m memFS) Create(name string) (io.WriteCloser, error) {
	m[name] = new(bytes.Buffer)
	return nopCloser{m[name]}, nil
}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

// A .gltf's buffers are side files, read through Options.Open within MaxBytes.
func TestGLTFExternalBuffers(t *testing.T) {
	doc := gltfDoc(upTriangle)
	doc.Buffers[0].URI = "thing data.bin"
	files := memFS{}
	var main bytes.Buffer
	e := gltf.NewEncoderFS(&main, files)
	e.AsBinary = false
	check(t, e.Encode(doc))
	side := map[string][]byte{}
	for name, b := range files {
		side[name] = b.Bytes()
	}
	m, err := Decode(main.Bytes(), &nt3dmesh.Options{Open: opener(side)})
	check(t, err)
	if m.Triangles() != 1 {
		t.Fatalf("%d triangles", m.Triangles())
	}
	_, err = Decode(main.Bytes(), nil)
	fails(t, err, "Options.Open")
	_, err = Decode(main.Bytes(), &nt3dmesh.Options{Open: opener(side), MaxBytes: 8})
	if !errors.Is(err, nt3dmesh.ErrTooLarge) {
		t.Fatalf("over MaxBytes: %v", err)
	}
}

func TestGLTFRefusals(t *testing.T) {
	draco := gltfDoc(upTriangle)
	draco.ExtensionsRequired = []string{"KHR_draco_mesh_compression"}
	_, err := Decode(glb(t, draco), nil)
	fails(t, err, "KHR_draco_mesh_compression")

	cycle := gltfDoc(upTriangle)
	cycle.Nodes = append(cycle.Nodes, &gltf.Node{Children: []int{0}})
	cycle.Nodes[0].Children = []int{1}
	_, err = Decode(glb(t, cycle), nil)
	fails(t, err, "its own ancestor")

	bad := gltfDoc(prim{positions: upTriangle.positions, indices: []uint16{0, 1, 9}})
	_, err = Decode(glb(t, bad), nil)
	fails(t, err, "past its 3 vertices")

	_, err = Decode(glb(t, gltfDoc(upTriangle, upTriangle)), &nt3dmesh.Options{MaxTriangles: 1})
	if !errors.Is(err, nt3dmesh.ErrTooManyTriangles) {
		t.Fatalf("over the budget: %v", err)
	}

	empty := gltfDoc(upTriangle)
	empty.Nodes[0].Mesh = nil
	_, err = Decode(glb(t, empty), nil)
	fails(t, err, "no meshes")

	_, err = Decode([]byte("glTF garbage"), nil)
	fails(t, err, "glTF")

	// Faces a mask drops do not count against the limit.
	clear, solid := [4]uint8{255, 255, 255, 0}, [4]uint8{255, 255, 255, 255}
	masked := gltfDoc(prim{
		positions: [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {2, 0, 0}, {3, 0, 0}, {2, 1, 0}},
		colors:    [][4]uint8{clear, clear, clear, solid, solid, solid},
		material:  gltf.Index(0),
	})
	masked.Materials = []*gltf.Material{{AlphaMode: gltf.AlphaMask, AlphaCutoff: gltf.Float(.5)}}
	m, err := Decode(glb(t, masked), &nt3dmesh.Options{MaxTriangles: 1})
	check(t, err)
	if m.Triangles() != 1 {
		t.Fatalf("%d triangles kept", m.Triangles())
	}

	nan := float32(math.NaN())
	_, err = Decode(glb(t, gltfDoc(prim{positions: upTriangle.positions, normals: [][3]float32{{0, 0, 1}, {nan, 0, 1}, {0, 0, 1}}})), nil)
	fails(t, err, "non-finite normal")

	textured := gltfDoc(prim{positions: upTriangle.positions, uv: [][2]float32{{0, 0}, {nan, 0}, {0, 1}}, material: gltf.Index(0)})
	img, err := modeler.WriteImage(textured, "leaf", "image/png", bytes.NewReader(pngTexture(t, color.NRGBA{200, 100, 50, 255})))
	check(t, err)
	textured.Textures = []*gltf.Texture{{Source: gltf.Index(img)}}
	textured.Materials = []*gltf.Material{{PBRMetallicRoughness: &gltf.PBRMetallicRoughness{BaseColorTexture: &gltf.TextureInfo{Index: 0}}}}
	_, err = Decode(glb(t, textured), nil)
	fails(t, err, "non-finite texture coordinate")

	// Points and lines count against caps of their own, across primitives.
	defer func(n int) { maxPoints = n }(maxPoints)
	maxPoints = 10
	cloud := make([][3]float32, maxPoints/2+1)
	_, err = Decode(glb(t, gltfDoc(prim{positions: cloud, mode: gltf.PrimitivePoints}, prim{positions: cloud, mode: gltf.PrimitivePoints})), nil)
	if !errors.Is(err, nt3dmesh.ErrTooManyTriangles) {
		t.Fatalf("over the point cap: %v", err)
	}
	ends := make([][3]float32, maxLineVertices/2+2)
	_, err = Decode(glb(t, gltfDoc(prim{positions: ends, mode: gltf.PrimitiveLines}, prim{positions: ends, mode: gltf.PrimitiveLines})), nil)
	if !errors.Is(err, nt3dmesh.ErrTooManyTriangles) {
		t.Fatalf("over the line cap: %v", err)
	}
}

// Decoded glTF adds to a scene like any model.
func TestGLTFAddTo(t *testing.T) {
	m, err := Decode(glb(t, gltfDoc(upTriangle)), nil)
	check(t, err)
	s := nt3d.NewScene()
	_, err = m.AddTo(s, nt3d.Root, "gltf")
	check(t, err)
}

// A primitive larger than one geometry may hold is split into objects of
// the same name, sharing vertices within each.
func TestGLTFSplit(t *testing.T) {
	defer func(n int) { pieceTriangles = n }(pieceTriangles)
	pieceTriangles = 2
	p := prim{
		positions: [][3]float32{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}, {2, 0, 0}, {2, 1, 0}},
		indices:   []uint16{0, 1, 2, 0, 2, 3, 1, 4, 5, 1, 5, 2, 0, 1, 3},
		normals:   [][3]float32{{0, 0, 1}, {0, 0, 1}, {0, 0, 1}, {0, 0, 1}, {0, 0, 1}, {0, 0, 1}},
	}
	m, err := Decode(glb(t, gltfDoc(p)), nil)
	check(t, err)
	if len(m.Objects) != 3 || m.Triangles() != 5 {
		t.Fatalf("%d objects, %d triangles", len(m.Objects), m.Triangles())
	}
	if g := m.Objects[0].Geometry; len(g.Vertices) != 4 || m.Objects[1].Name != m.Objects[0].Name {
		t.Fatalf("first piece shares no vertices: %d", len(g.Vertices))
	}
	for _, o := range m.Objects {
		check(t, o.Geometry.Validate())
	}
}

// A normal map coexists with base color and preserves authored normals.
// Missing vertex normals still get the flat fallback.
func TestGLTFNormalMaps(t *testing.T) {
	for _, authored := range []bool{false, true} {
		p := upTriangle
		p.material = gltf.Index(0)
		p.uv = [][2]float32{{0, 0}, {1, 0}, {0, 1}}
		if authored {
			p.normals = [][3]float32{{.6, 0, .8}, {.6, 0, .8}, {.6, 0, .8}}
		}
		doc := gltfDoc(p, p)
		base, err := modeler.WriteImage(doc, "base", "image/png", bytes.NewReader(pngTexture(t, color.NRGBA{200, 100, 50, 255})))
		check(t, err)
		normal, err := modeler.WriteImage(doc, "normal", "image/png", bytes.NewReader(pngTexture(t, color.NRGBA{128, 128, 255, 255})))
		check(t, err)
		doc.Textures = []*gltf.Texture{{Source: gltf.Index(base)}, {Source: gltf.Index(normal)}}
		doc.Materials = []*gltf.Material{{NormalTexture: &gltf.NormalTexture{Index: gltf.Index(1), Scale: gltf.Float(.5)}, PBRMetallicRoughness: &gltf.PBRMetallicRoughness{BaseColorTexture: &gltf.TextureInfo{Index: 0}}}}
		m, err := Decode(glb(t, doc), nil)
		check(t, err)
		if len(m.Warnings) != 0 {
			t.Fatalf("unexpected normal-map warnings: %v", m.Warnings)
		}
		want := nt3dmath.Vec3{Y: -1}
		if authored {
			want = nt3dmath.Vec3{X: .6, Y: -.8}
		}
		for _, obj := range m.Objects {
			g := obj.Geometry
			for _, v := range g.Vertices {
				if !near(v.Normal, want) {
					t.Fatalf("authored=%v: normal %v, want %v", authored, v.Normal, want)
				}
			}
			if g.Material == nil || g.Material.Texture == nil || g.Material.NormalMap == nil || g.Material.NormalStrength != .5 {
				t.Fatal("base texture or normal map lost")
			}
			if c := g.Material.Texture.At(0, 0).(color.RGBA); c != (color.RGBA{200, 100, 50, 255}) {
				t.Fatalf("base texture replaced: %v", c)
			}
		}
	}
}

func TestGLTFNormalMapOptions(t *testing.T) {
	for _, kind := range []string{"normal only", "zero scale", "shared image", "missing UV", "different UV", "transform", "authored tangent"} {
		t.Run(kind, func(t *testing.T) {
			p := upTriangle
			p.material = gltf.Index(0)
			p.uv = [][2]float32{{0, 0}, {1, 0}, {0, 1}}
			doc := gltfDoc(p)
			img, err := modeler.WriteImage(doc, "normal", "image/png", bytes.NewReader(pngTexture(t, color.NRGBA{180, 128, 230, 255})))
			check(t, err)
			doc.Textures = []*gltf.Texture{{Source: gltf.Index(img)}}
			normal := &gltf.NormalTexture{Index: gltf.Index(0)}
			material := &gltf.Material{NormalTexture: normal}
			doc.Materials = []*gltf.Material{material}
			wantWarning := ""
			switch kind {
			case "zero scale":
				normal.Scale = gltf.Float(0)
			case "shared image":
				material.PBRMetallicRoughness = &gltf.PBRMetallicRoughness{BaseColorTexture: &gltf.TextureInfo{Index: 0}}
			case "missing UV":
				delete(doc.Meshes[0].Primitives[0].Attributes, gltf.TEXCOORD_0)
				wantWarning = "no TEXCOORD_0"
			case "different UV":
				normal.TexCoord = 1
				material.PBRMetallicRoughness = &gltf.PBRMetallicRoughness{BaseColorTexture: &gltf.TextureInfo{Index: 0}}
				wantWarning = "no TEXCOORD_1"
			case "transform":
				normal.Extensions = gltf.Extensions{"KHR_texture_transform": map[string]any{"offset": []float64{.2, 0}}}
			case "authored tangent":
				doc.Meshes[0].Primitives[0].Attributes[gltf.TANGENT] = modeler.WriteTangent(doc, [][4]float32{{1, 0, 0, 1}, {1, 0, 0, 1}, {1, 0, 0, 1}})
			}
			m, err := Decode(glb(t, doc), nil)
			check(t, err)
			g := m.Objects[0].Geometry
			check(t, g.Validate())
			if wantWarning != "" && !strings.Contains(strings.Join(m.Warnings, ";"), wantWarning) {
				t.Fatalf("warnings %v lack %q", m.Warnings, wantWarning)
			}
			if kind == "authored tangent" {
				if len(g.Tangents) != 3 || !near(g.Tangents[0].Direction, nt3dmath.Vec3{X: 1}) || g.Tangents[0].W != 1 {
					t.Fatalf("authored tangents %v", g.Tangents)
				}
				if strings.Contains(strings.Join(m.Warnings, ";"), "tangent") {
					t.Fatalf("authored tangents warned: %v", m.Warnings)
				}
			}
			if wantWarning == "" {
				if g.Material == nil || g.Material.NormalMap == nil {
					t.Fatal("normal map missing")
				}
				strength := float32(1)
				if kind == "zero scale" {
					strength = 0
				}
				if g.Material.NormalStrength != strength {
					t.Fatal("normal scale lost")
				}
				if kind == "shared image" && g.Material.Texture == g.Material.NormalMap {
					t.Fatal("color and vector textures share gamma interpretation")
				}
			} else if g.Material != nil && g.Material.NormalMap != nil {
				t.Fatal("unsupported map was silently sampled")
			}
		})
	}
}

// normalMapped gives doc one normal-mapped material and its primitives
// that material.
func normalMapped(t *testing.T, doc *gltf.Document) {
	t.Helper()
	img, err := modeler.WriteImage(doc, "normal", "image/png", bytes.NewReader(pngTexture(t, color.NRGBA{128, 128, 255, 255})))
	check(t, err)
	doc.Textures = []*gltf.Texture{{Source: gltf.Index(img)}}
	doc.Materials = []*gltf.Material{{NormalTexture: &gltf.NormalTexture{Index: gltf.Index(0)}}}
	for _, p := range doc.Meshes[0].Primitives {
		p.Material = gltf.Index(0)
	}
}

func TestGLTFTangents(t *testing.T) {
	up := upTriangle
	up.uv = [][2]float32{{0, 0}, {1, 0}, {0, 1}}
	up.normals = [][3]float32{{0, 0, 1}, {0, 0, 1}, {0, 0, 1}}
	t.Run("generated", func(t *testing.T) {
		doc := gltfDoc(up)
		normalMapped(t, doc)
		m, err := Decode(glb(t, doc), nil)
		check(t, err)
		g := m.Objects[0].Geometry
		if len(g.Tangents) != 3 {
			t.Fatalf("generated %d tangents: %v", len(g.Tangents), m.Warnings)
		}
		// V grows up this triangle, so its image hangs upside down: a
		// left-handed frame under glTF's image-up convention.
		for _, tan := range g.Tangents {
			if !near(tan.Direction, nt3dmath.Vec3{X: 1}) || tan.W != -1 {
				t.Fatalf("generated tangent %v", tan)
			}
		}
		if strings.Contains(strings.Join(m.Warnings, ";"), "tangent") {
			t.Fatalf("generation warned: %v", m.Warnings)
		}
	})
	t.Run("mirrored node", func(t *testing.T) {
		doc := gltfDoc(up)
		normalMapped(t, doc)
		doc.Meshes[0].Primitives[0].Attributes[gltf.TANGENT] = modeler.WriteTangent(doc, [][4]float32{{1, 0, 0, 1}, {1, 0, 0, 1}, {1, 0, 0, 1}})
		doc.Nodes[0].Scale = [3]float64{1, 1, -1}
		m, err := Decode(glb(t, doc), nil)
		check(t, err)
		if len(m.Objects[0].Geometry.Tangents) != 3 {
			t.Fatal("authored tangents lost")
		}
		for _, tan := range m.Objects[0].Geometry.Tangents {
			if !near(tan.Direction, nt3dmath.Vec3{X: 1}) || tan.W != -1 {
				t.Fatalf("mirrored tangent %v", tan)
			}
		}
	})
	t.Run("double sided", func(t *testing.T) {
		doc := gltfDoc(up)
		normalMapped(t, doc)
		doc.Materials[0].DoubleSided = true
		m, err := Decode(glb(t, doc), nil)
		check(t, err)
		g := m.Objects[0].Geometry
		if len(g.Tangents) != len(g.Vertices) || len(g.Vertices) != 6 {
			t.Fatalf("%d tangents for %d vertices", len(g.Tangents), len(g.Vertices))
		}
		for i, v := range g.Vertices {
			want := float32(-1)
			if v.Normal.Y > 0 { // The back copy; the front faces -Y.
				want = 1
			}
			if g.Tangents[i].W != want {
				t.Fatalf("vertex %d (normal %v) W %v", i, v.Normal, g.Tangents[i].W)
			}
		}
	})
	t.Run("seam split", func(t *testing.T) {
		p := prim{positions: [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {1, 1, 0}}, indices: []uint16{0, 1, 2, 1, 3, 2}, uv: [][2]float32{{0, 0}, {1, 0}, {0, 1}, {0, 0}}, normals: [][3]float32{{0, 0, 1}, {0, 0, 1}, {0, 0, 1}, {0, 0, 1}}}
		doc := gltfDoc(p)
		normalMapped(t, doc)
		m, err := Decode(glb(t, doc), nil)
		check(t, err)
		g := m.Objects[0].Geometry
		if len(g.Vertices) != 6 || len(g.Tangents) != 6 {
			t.Fatalf("%d vertices, %d tangents, want the seam split", len(g.Vertices), len(g.Tangents))
		}
		for f, want := range []float32{-1, 1} {
			for k := range 3 {
				if w := g.Tangents[g.Indices[3*f+k]].W; w != want {
					t.Fatalf("face %d corner %d W %v, want %v", f, k, w, want)
				}
			}
		}
		check(t, g.Validate())
	})
	t.Run("bad tangent", func(t *testing.T) {
		doc := gltfDoc(up)
		normalMapped(t, doc)
		doc.Meshes[0].Primitives[0].Attributes[gltf.TANGENT] = modeler.WriteTangent(doc, [][4]float32{{1, 0, 0, 1}, {1, 0, 0, 1}})
		m, err := Decode(glb(t, doc), nil)
		check(t, err)
		if !strings.Contains(strings.Join(m.Warnings, ";"), "unreadable tangents") {
			t.Fatalf("warnings %v", m.Warnings)
		}
		if len(m.Objects[0].Geometry.Tangents) != 3 {
			t.Fatal("tangents not generated after bad data")
		}
	})
}

// Generated frames agree with authored ones under glTF's convention: a
// quad textured upright (V down the image as Y rises) carries +X, W +1.
func TestGLTFGeneratedMatchesAuthored(t *testing.T) {
	p := prim{positions: [][3]float32{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}}, indices: []uint16{0, 1, 2, 0, 2, 3}, uv: [][2]float32{{0, 1}, {1, 1}, {1, 0}, {0, 0}}, normals: [][3]float32{{0, 0, 1}, {0, 0, 1}, {0, 0, 1}, {0, 0, 1}}}
	doc := gltfDoc(p)
	normalMapped(t, doc)
	generated, err := Decode(glb(t, doc), nil)
	check(t, err)
	doc.Meshes[0].Primitives[0].Attributes[gltf.TANGENT] = modeler.WriteTangent(doc, [][4]float32{{1, 0, 0, 1}, {1, 0, 0, 1}, {1, 0, 0, 1}, {1, 0, 0, 1}})
	authored, err := Decode(glb(t, doc), nil)
	check(t, err)
	g, a := generated.Objects[0].Geometry, authored.Objects[0].Geometry
	if len(g.Tangents) != 4 || len(a.Tangents) != 4 {
		t.Fatalf("%d generated, %d authored tangents", len(g.Tangents), len(a.Tangents))
	}
	for i := range g.Tangents {
		if !near(g.Tangents[i].Direction, a.Tangents[i].Direction) || g.Tangents[i].W != a.Tangents[i].W {
			t.Fatalf("vertex %d: generated %v, authored %v", i, g.Tangents[i], a.Tangents[i])
		}
	}
}
