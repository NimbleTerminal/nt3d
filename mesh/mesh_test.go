// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
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

func TestSTLBinaryRoundTrip(t *testing.T) {
	sphere, err := nt3d.BuildSphere(nt3d.Sphere{Radius: 2, Color: color.RGBA{200, 80, 16, 255}})
	check(t, err)
	var buf bytes.Buffer
	check(t, EncodeSTL(&buf, sphere))
	if f, ok := FormatOf("anything", buf.Bytes()); !ok || f != STL {
		t.Fatal("binary STL not detected")
	}
	m, err := DecodeSTL(buf.Bytes(), nil)
	check(t, err)
	g := m.Objects[0].Geometry
	if m.Triangles() != len(sphere.Indices)/3 || len(g.Vertices) != len(sphere.Indices) {
		t.Fatal(m.Triangles())
	}
	for i, k := range sphere.Indices {
		if g.Vertices[i].Position != sphere.Vertices[k].Position {
			t.Fatal("position", i)
		}
	}
	// Colors keep five bits a channel.
	if c := g.Vertices[0].Color; c != (color.RGBA{206, 82, 16, 255}) || m.Objects[0].Plain[0] {
		t.Fatal(c)
	}
	if b := m.Bounds(); b.Max.Z != 2 || b.Min.Z != -2 {
		t.Fatal(b)
	}
}

func binarySTLData(header string, attributes ...uint16) []byte {
	data := make([]byte, 84+50*len(attributes))
	copy(data, header)
	binary.LittleEndian.PutUint32(data[80:], uint32(len(attributes)))
	for i, a := range attributes {
		r := data[84+50*i:]
		for j, v := range []float32{0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0} {
			binary.LittleEndian.PutUint32(r[j*4:], math.Float32bits(v))
		}
		binary.LittleEndian.PutUint16(r[48:], a)
	}
	return data
}

func TestSTLBinaryColorsAndDetection(t *testing.T) {
	// A binary header may begin with "solid"; the length decides.
	m, err := DecodeSTL(binarySTLData("solid but binary", 0, 0x8000|31<<10), &Options{Color: color.RGBA{1, 2, 3, 255}})
	check(t, err)
	v := m.Objects[0].Geometry.Vertices
	if v[0].Color != (color.RGBA{1, 2, 3, 255}) || !m.Objects[0].Plain[0] || v[3].Color != (color.RGBA{255, 0, 0, 255}) || m.Objects[0].Plain[3] {
		t.Fatal(v[0].Color, v[3].Color)
	}
	if n := v[0].Normal; n != (nt3dmath.Vec3{Z: 1}) {
		t.Fatal("normal not recomputed", n)
	}
	magics := "COLOR=\x10\x20\x30\xff"
	m, err = DecodeSTL(binarySTLData(magics, 31, 0x8000), nil)
	check(t, err)
	v = m.Objects[0].Geometry.Vertices
	if v[0].Color != (color.RGBA{255, 0, 0, 255}) || v[3].Color != (color.RGBA{0x10, 0x20, 0x30, 255}) {
		t.Fatal(v[0].Color, v[3].Color)
	}
	_, err = DecodeSTL(binarySTLData("", 0, 0, 0), &Options{MaxTriangles: 2})
	fails(t, err, "limit is 2")
	_, err = DecodeSTL(binarySTLData("")[:84], nil)
	fails(t, err, "no triangles")
	_, err = DecodeSTL(binarySTLData("", 0)[:100], nil)
	fails(t, err, "invalid STL header")
	bad := binarySTLData("", 0)
	binary.LittleEndian.PutUint32(bad[84+12:], math.Float32bits(float32(math.Inf(1))))
	_, err = DecodeSTL(bad, nil)
	fails(t, err, "non-finite")
}

const asciiSTL = `solid first part
  facet normal 0 0 0
    outer loop
      vertex 0 0 0
      vertex 1 0 0
      vertex 0 1 0
    endloop
  endfacet
endsolid first part
SOLID second
FACET NORMAL 0 0 1 OUTER LOOP VERTEX 0 0 1 VERTEX 1 0 1 VERTEX 0 1 1e0 ENDLOOP ENDFACET
ENDSOLID
`

func TestSTLASCII(t *testing.T) {
	m, err := DecodeSTL([]byte(asciiSTL), nil)
	check(t, err)
	if len(m.Objects) != 2 || m.Objects[0].Name != "first part" || m.Objects[1].Name != "second" || m.Triangles() != 2 {
		t.Fatal(m.Objects)
	}
	if m.Objects[0].Geometry.Vertices[0].Color != DefaultColor || !m.Objects[0].Plain[0] {
		t.Fatal("default color")
	}
	for _, c := range []struct{ data, want string }{
		{asciiSTL + "junk", "line 13: unexpected data after endsolid"},
		{"solid x\nfacet normal 0 0 0\n outer loop\n vertex 0 0 nope", "line 4: bad number"},
		{"solid x\nfacet normal 0 0 0\n", "end of file"},
		{"solid x\n", "missing endsolid"},
		{"solid x\nendsolid\n", "no triangles"},
		{"hello", "invalid STL header"},
	} {
		_, err := DecodeSTL([]byte(c.data), nil)
		fails(t, err, c.want)
	}
	_, err = DecodeSTL([]byte(asciiSTL), &Options{MaxTriangles: 1})
	fails(t, err, "exceeds 1")
	if !errors.Is(err, ErrTooManyTriangles) {
		t.Fatal("not ErrTooManyTriangles")
	}
}

const cubeOBJ = `# A unit cube: quads, shared normals, negative indices.
mtllib cube.mtl
v 0 0 0
v 1 0 0
v 1 1 0
v 0 1 0
v 0 0 1 1 0 0
v 1 0 1 1 0 0
v 1 1 1 1 0 0
v 0 1 1 1 0 0
vt 0 0
vt 1 0
vt 1 1
vt 0 1
vn 0 0 -1
vn 0 0 1
o box
usemtl wood
f 1/1/1 4/4/1 3/3/1 2/2/1
f -4/1/2 -3/2/2 \
  -2/3/2 -1/4/2
usemtl paint
f 1 2 6 5
f 2 3 7 6
f 3 4 8 7
f 4 1 5 8
g wire
l 1 2 3
`

func cubeFiles(t *testing.T) map[string][]byte {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{0, 255, 0, 255})
	var texture bytes.Buffer
	check(t, png.Encode(&texture, img))
	return map[string][]byte{
		"cube.mtl":         []byte("newmtl wood\nKd 0.5 0.25 0\nmap_Kd -s 1 1 1 tex/wood.png\nnewmtl paint\nKd 0 0 1\n"),
		"tex/wood.png":     texture.Bytes(),
		"cube.obj":         []byte(cubeOBJ),
		"elsewhere/x.data": nil,
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

func TestOBJ(t *testing.T) {
	files := cubeFiles(t)
	m, err := DecodeOBJ([]byte(cubeOBJ), &Options{Open: opener(files)})
	check(t, err)
	if len(m.Objects) != 3 || len(m.Warnings) != 0 {
		t.Fatal(len(m.Objects), m.Warnings)
	}
	wood, paint, wire := m.Objects[0], m.Objects[1], m.Objects[2]
	if wood.Name != "box" || wood.Material != "wood" || paint.Material != "paint" || wire.Name != "wire" {
		t.Fatal(wood.Name, wood.Material, paint.Material, wire.Name)
	}
	// Two quads with normals share their eight corners.
	if g := wood.Geometry; len(g.Indices) != 12 || len(g.Vertices) != 8 || g.Material == nil || g.Material.Texture == nil || g.UV[1] != (nt3d.UV{U: 0, V: 0}) {
		t.Fatal(len(g.Indices), len(g.Vertices), g.Material, g.UV)
	}
	// The transparent texels were flattened over the wood color.
	if c := wood.Geometry.Material.Texture.At(1, 1); c != (color.RGBA{128, 64, 0, 255}) {
		t.Fatal(c)
	}
	// Faces without normals have flat ones, vertices of their own, and no UVs.
	g := paint.Geometry
	if len(g.Indices) != 24 || len(g.Vertices) != 24 || g.UV != nil || g.Vertices[0].Normal != (nt3dmath.Vec3{Y: -1}) {
		t.Fatal(len(g.Indices), len(g.Vertices), g.Vertices[0].Normal)
	}
	if g.Vertices[0].Color != (color.RGBA{0, 0, 255, 255}) || g.Vertices[2].Color != (color.RGBA{255, 0, 0, 255}) || paint.Plain[2] {
		t.Fatal("material or vertex colors", g.Vertices[0].Color, g.Vertices[2].Color)
	}
	if len(wire.Geometry.Lines) != 4 || len(wire.Geometry.Indices) != 0 {
		t.Fatal(wire.Geometry.Lines)
	}
	if m.Bounds().Max != (nt3dmath.Vec3{X: 1, Y: 1, Z: 1}) {
		t.Fatal(m.Bounds())
	}
	if _, err = m.Geometry(); err == nil {
		t.Fatal("different materials merged")
	}

	// Without side files, colors fall back and a warning says why.
	m, err = DecodeOBJ([]byte(cubeOBJ), nil)
	check(t, err)
	if len(m.Warnings) == 0 || m.Objects[0].Geometry.Material != nil || !m.Objects[0].Plain[0] {
		t.Fatal(m.Warnings)
	}
	merged, err := m.Geometry()
	check(t, err)
	if len(merged.Indices) != 36 || len(merged.Lines) != 4 {
		t.Fatal(len(merged.Indices))
	}
	delete(files, "tex/wood.png")
	m, err = DecodeOBJ([]byte(cubeOBJ), &Options{Open: opener(files)})
	check(t, err)
	if len(m.Warnings) != 1 || !strings.Contains(m.Warnings[0], "tex/wood.png") || m.Objects[0].Geometry.Vertices[0].Color != (color.RGBA{128, 64, 0, 255}) {
		t.Fatal(m.Warnings)
	}
}

func TestOBJErrors(t *testing.T) {
	for _, c := range []struct{ data, want string }{
		{"v 0 0 0\nv 1 0 0\nf 1 2 3", "line 3: vertex index 3 out of range"},
		{"v 0 0 0\nf 1 1 0", "bad vertex index"},
		{"v 0 0 0\nf 1 1", "at least three"},
		{"v 0 0 nan", "bad number"},
		{"v 0 0 1e20", "bad number"},
		{"v 0 0 0\nf 1/1/1 1 1", "texture index"},
		{"v 0 0 0", "no faces"},
		{"curv 0 1 2 3\n", "no faces"},
	} {
		_, err := DecodeOBJ([]byte(c.data), nil)
		fails(t, err, c.want)
	}
	_, err := DecodeOBJ([]byte("v 0 0 0\nv 1 0 0\nv 1 1 0\nv 0 1 0\nf 1 2 3 4\n"), &Options{MaxTriangles: 1})
	fails(t, err, "exceeds 1")
	if !errors.Is(err, ErrTooManyTriangles) {
		t.Fatal("not ErrTooManyTriangles")
	}
}

func TestOBJRoundTrip(t *testing.T) {
	torus, err := nt3d.BuildTorus(nt3d.Torus{MajorRadius: 1, MinorRadius: .25, Color: color.RGBA{51, 102, 153, 255}})
	check(t, err)
	torus.Lines = []nt3d.Vertex{{Color: color.RGBA{255, 255, 255, 255}}, {Position: nt3dmath.Vec3{Z: 1}, Color: color.RGBA{255, 255, 255, 255}}}
	var buf bytes.Buffer
	check(t, EncodeOBJ(&buf, torus))
	m, err := DecodeOBJ(buf.Bytes(), nil)
	check(t, err)
	g := m.Objects[0].Geometry
	if len(g.Indices) != len(torus.Indices) || len(g.Vertices) != len(torus.Vertices) || len(g.Lines) != 2 {
		t.Fatal(len(g.Indices), len(g.Vertices), len(g.Lines))
	}
	for i, k := range g.Indices {
		v, w := g.Vertices[k], torus.Vertices[torus.Indices[i]]
		if v.Position != w.Position || v.Color != w.Color || g.UV[k] != torus.UV[torus.Indices[i]] || v.Normal.Sub(w.Normal).Dot(v.Normal.Sub(w.Normal)) > 1e-10 {
			t.Fatal(i, v, w)
		}
	}
}

func TestReadFileAndScene(t *testing.T) {
	dir := t.TempDir()
	for name, data := range cubeFiles(t) {
		check(t, os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755))
		check(t, os.WriteFile(filepath.Join(dir, name), data, 0o644))
	}
	m, err := ReadFile(filepath.Join(dir, "cube.obj"), nil)
	check(t, err)
	if m.Format != OBJ || m.Objects[0].Geometry.Material == nil || len(m.Warnings) != 0 {
		t.Fatal(m.Warnings)
	}
	open := DirOpener(filepath.Join(dir, "tex"))
	if _, err := open("../cube.mtl"); err == nil {
		t.Fatal("escaped the directory")
	}
	if _, err := open(`..\cube.mtl`); err == nil {
		t.Fatal("escaped with a backslash")
	}

	s := nt3d.NewScene()
	group, err := m.AddTo(s, nt3d.Root, "cube")
	check(t, err)
	node, _ := s.Node(group)
	if len(node.Children) != 3 || !s.Bounds().IsValid() {
		t.Fatal(node.Children)
	}
	before := s.Revision()
	check(t, s.Remove(group))
	_, err = s.Add(nt3d.Root, "cube/1")
	check(t, err)
	if _, err = m.AddTo(s, nt3d.Root, "cube"); err == nil {
		t.Fatal("name collision accepted")
	}
	if _, ok := s.Find("cube"); ok || before == s.Revision() {
		t.Fatal("failed AddTo left nodes")
	}
}
