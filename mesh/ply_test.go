// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image/color"
	"math"
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
)

// The unit cube, wound outward, as six quads.
var cubeCorners = [8][3]float32{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}, {0, 0, 1}, {1, 0, 1}, {1, 1, 1}, {0, 1, 1}}
var cubeQuads = [6][4]int{{0, 3, 2, 1}, {4, 5, 6, 7}, {0, 1, 5, 4}, {3, 7, 6, 2}, {0, 4, 7, 3}, {1, 2, 6, 5}}

// cubePLY writes the cube with red vertices on top, and a range grid, which
// readers skip, between the vertices and faces.
func cubePLY(format string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "ply\nformat %s 1.0\ncomment a cube\nelement vertex 8\nproperty float x\nproperty float y\nproperty float z\nproperty uchar red\nproperty uchar green\nproperty uchar blue\n", format)
	b.WriteString("element range_grid 2\nproperty list uchar int vertex_indices\nelement face 6\nproperty list uchar int vertex_indices\nend_header\n")
	var order binary.ByteOrder = binary.LittleEndian
	if format == "binary_big_endian" {
		order = binary.BigEndian
	}
	color := func(i int) [3]uint8 {
		if cubeCorners[i][2] == 1 {
			return [3]uint8{255, 0, 0}
		}
		return [3]uint8{0, 0, 255}
	}
	for i, c := range cubeCorners {
		if format == "ascii" {
			k := color(i)
			fmt.Fprintf(&b, "%g %g %g %d %d %d\n", c[0], c[1], c[2], k[0], k[1], k[2])
			continue
		}
		for _, v := range c {
			binary.Write(&b, order, v)
		}
		k := color(i)
		b.Write(k[:])
	}
	lists := [][]int{{1, 7}, {0}}
	for _, q := range cubeQuads {
		lists = append(lists, q[:])
	}
	for _, l := range lists {
		if format == "ascii" {
			fmt.Fprint(&b, len(l))
			for _, v := range l {
				fmt.Fprint(&b, " ", v)
			}
			b.WriteString("\n")
			continue
		}
		b.WriteByte(byte(len(l)))
		for _, v := range l {
			binary.Write(&b, order, int32(v))
		}
	}
	return b.Bytes()
}

func TestPLYFormats(t *testing.T) {
	var first nt3d.Geometry
	for _, format := range []string{"ascii", "binary_little_endian", "binary_big_endian"} {
		data := cubePLY(format)
		if f, ok := FormatOf("anything", data); !ok || f != PLY {
			t.Fatalf("%s: not detected", format)
		}
		m, err := Decode(data, PLY, nil)
		check(t, err)
		g := m.Objects[0].Geometry
		if m.Triangles() != 12 || len(m.Warnings) != 0 {
			t.Fatalf("%s: %d triangles", format, m.Triangles())
		}
		closedOutward(t, g)
		for i, v := range g.Vertices {
			want := color.RGBA{0, 0, 255, 255}
			if v.Position.Z == 1 {
				want = color.RGBA{255, 0, 0, 255}
			}
			if v.Color != want || m.Objects[0].Plain[i] {
				t.Fatalf("%s: vertex %v is %v", format, v.Position, v.Color)
			}
		}
		// Every encoding reads the same.
		if format == "ascii" {
			first = g
		} else if fmt.Sprint(g.Vertices) != fmt.Sprint(first.Vertices) {
			t.Fatalf("%s differs from ascii", format)
		}
	}
}

func TestPLYProperties(t *testing.T) {
	// Float colors, normals, texture coordinates, a face color, and a
	// concave face, from a writer using \r\n.
	data := strings.ReplaceAll(`ply
format ascii 1.0
comment TextureFile wood.png
element vertex 4
property float x
property float y
property float z
property float nx
property float ny
property float nz
property float s
property float t
element face 1
property list uchar uint vertex_indices
property uchar red
property uchar green
property uchar blue
end_header
0 0 0 0 0 1 0 0
2 1 0 0 0 1 .5 .25
4 0 0 0 0 1 1 0
2 4 0 0 0 1 .5 1
4 0 1 2 3 10 200 30
`, "\n", "\r\n")
	files := cubeFiles(t)
	m, err := DecodePLY([]byte(data), &Options{Open: opener(map[string][]byte{"wood.png": files["tex/wood.png"]})})
	check(t, err)
	g := m.Objects[0].Geometry
	if len(m.Warnings) != 0 || g.Material == nil || g.Material.Texture == nil || m.Objects[0].Material != "wood.png" {
		t.Fatalf("texture: %v", m.Warnings)
	}
	// The dart is concave: ear-clipped, every triangle faces up.
	total := 0.0
	for i := 0; i < len(g.Indices); i += 3 {
		a, b, c := g.Vertices[g.Indices[i]].Position, g.Vertices[g.Indices[i+1]].Position, g.Vertices[g.Indices[i+2]].Position
		area := float64(b.Sub(a).Cross(c.Sub(a)).Z) / 2
		if area <= 0 {
			t.Fatal("triangle wound backward")
		}
		total += area
	}
	if len(g.Indices) != 6 || math.Abs(total-6) > 1e-5 {
		t.Fatalf("%d indices covering %v", len(g.Indices), total)
	}
	for i, v := range g.Vertices {
		if v.Normal != (nt3dmath.Vec3{Z: 1}) || v.Color != (color.RGBA{10, 200, 30, 255}) {
			t.Fatalf("vertex %d: %+v", i, v)
		}
	}
	// The corner at the origin has t = 0, the bottom: V = 1, from the top.
	for i, v := range g.Vertices {
		if v.Position == (nt3dmath.Vec3{}) && g.UV[i] != (nt3d.UV{U: 0, V: 1}) {
			t.Fatalf("UV %v: t counts up", g.UV[i])
		}
	}
	// Without side files, the texture is a warning and the UVs remain.
	m, err = DecodePLY([]byte(data), nil)
	check(t, err)
	if len(m.Warnings) != 1 || m.Objects[0].Geometry.Material != nil || len(m.Objects[0].Geometry.UV) != 6 {
		t.Fatalf("%v", m.Warnings)
	}
}

func TestPLYRoundTrip(t *testing.T) {
	torus, err := nt3d.BuildTorus(nt3d.Torus{MajorRadius: 1, MinorRadius: .25, Color: color.RGBA{51, 102, 153, 255}})
	check(t, err)
	var buf bytes.Buffer
	check(t, EncodePLY(&buf, torus))
	m, err := DecodePLY(buf.Bytes(), nil)
	check(t, err)
	g := m.Objects[0].Geometry
	if len(g.Indices) != len(torus.Indices) {
		t.Fatal(len(g.Indices))
	}
	for i, k := range torus.Indices {
		v, w := g.Vertices[i], torus.Vertices[k]
		if v.Position != w.Position || v.Color != w.Color || g.UV[i] != torus.UV[k] || v.Normal.Sub(w.Normal).Dot(v.Normal.Sub(w.Normal)) > 1e-10 {
			t.Fatalf("corner %d: %+v, want %+v", i, v, w)
		}
	}
}

func TestDamagedPLY(t *testing.T) {
	header := "ply\nformat ascii 1.0\nelement vertex 3\nproperty float x\nproperty float y\nproperty float z\nelement face 1\nproperty list uchar int vertex_indices\nend_header\n"
	for name, c := range map[string]struct{ data, want string }{
		"not PLY":         {"solid x", "not a PLY"},
		"no end":          {"ply\nformat ascii 1.0\n", "not a PLY"},
		"bad format":      {strings.Replace(header, "ascii", "zip", 1) + "0 0 0\n1 0 0\n0 1 0\n3 0 1 2\n", "format"},
		"bad property":    {strings.Replace(header, "property float x", "property quaternion x", 1), "bad property"},
		"index too large": {header + "0 0 0\n1 0 0\n0 1 0\n3 0 1 7\n", "names vertex 7"},
		"too few corners": {header + "0 0 0\n1 0 0\n0 1 0\n2 0 1\n", "fewer than three"},
		"not a number":    {header + "0 0 wide\n1 0 0\n0 1 0\n3 0 1 2\n", "bad number"},
		"infinite":        {header + "0 0 1e999\n1 0 0\n0 1 0\n3 0 1 2\n", "bad number"},
		"huge":            {header + "0 0 1e20\n1 0 0\n0 1 0\n3 0 1 2\n", "coordinate limits"},
		"truncated":       {header + "0 0 0\n1 0", "more than the file holds"},
		"ends early":      {header + "0 0 0\n1 0 0\n0 1 0\n3 0 1", "end of file"},
		"empty":           {strings.Replace(strings.Replace(header, "element face 1", "element face 0", 1), "element vertex 3", "element vertex 0", 1), "no faces or vertices"},
		"claims millions": {strings.Replace(header, "element vertex 3", "element vertex 900000000", 1), "more than the file holds"},
		"empty elements":  {strings.Replace(header, "element vertex 3", "element padding 900000000\nelement vertex 3", 1) + "0 0 0\n1 0 0\n0 1 0\n3 0 1 2\n", "more than the file holds"},
		"no xyz":          {strings.Replace(header, "property float z\n", "", 1) + "0 0\n1 0\n0 1\n3 0 1 2\n", "lack x, y, or z"},
	} {
		if _, err := DecodePLY([]byte(c.data), nil); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error naming %q", name, err, c.want)
		}
	}
	// Only a line of its own ends the header, not a comment that says so.
	m, err := DecodePLY([]byte(strings.Replace(header, "format ascii 1.0\n", "format ascii 1.0\ncomment end_header\n", 1)+"0 0 0\n1 0 0\n0 1 0\n3 0 1 2\n"), nil)
	check(t, err)
	if m.Triangles() != 1 {
		t.Fatalf("%d triangles", m.Triangles())
	}
	// The binary length check applies to binary files too.
	bin := cubePLY("binary_little_endian")
	if _, err := DecodePLY(bin[:len(bin)-20], nil); err == nil {
		t.Error("truncated binary accepted")
	}
	_, err = DecodePLY(cubePLY("ascii"), &Options{MaxTriangles: 11})
	if !errors.Is(err, ErrTooManyTriangles) {
		t.Errorf("limit: %v", err)
	}
}

// A scan of vertices without faces is a point cloud.
func TestPLYPoints(t *testing.T) {
	data := "ply\nformat ascii 1.0\nelement vertex 3\nproperty float x\nproperty float y\nproperty float z\nproperty uchar red\nproperty uchar green\nproperty uchar blue\nend_header\n0 0 0 255 0 0\n1 0 0 0 255 0\n0 1 0 0 0 255\n"
	m, err := DecodePLY([]byte(data), nil)
	check(t, err)
	g := m.Objects[0].Geometry
	if m.Triangles() != 0 || len(g.Points) != 3 || g.Points[1].Color != (color.RGBA{0, 255, 0, 255}) || g.Bounds.Max.X != 1 {
		t.Fatalf("%+v", g.Points)
	}
	check(t, g.Validate())
}

// gridPLY is a 3 × 3 range scan on z = 0, x along columns and y along rows,
// with sample (row 2, col 2) at the given height, and (0, 0) missing if asked.
func gridPLY(corner float32, missing bool) []byte {
	var b strings.Builder
	type sample struct{ r, c int }
	var samples []sample
	for r := range 3 {
		for c := range 3 {
			if missing && r == 0 && c == 0 {
				continue
			}
			samples = append(samples, sample{r, c})
		}
	}
	fmt.Fprintf(&b, "ply\nformat ascii 1.0\nobj_info num_cols 3\nobj_info num_rows 3\nelement vertex %d\nproperty float x\nproperty float y\nproperty float z\nelement range_grid 9\nproperty list uchar int vertex_indices\nend_header\n", len(samples))
	index := map[sample]int{}
	for i, s := range samples {
		z := float32(0)
		if s.r == 2 && s.c == 2 {
			z = corner
		}
		fmt.Fprintf(&b, "%d %d %g\n", s.c, s.r, z)
		index[s] = i
	}
	for r := range 3 {
		for c := range 3 {
			if i, ok := index[sample{r, c}]; ok {
				fmt.Fprintf(&b, "1 %d\n", i)
			} else {
				b.WriteString("0\n")
			}
		}
	}
	return []byte(b.String())
}

func TestPLYRangeGrid(t *testing.T) {
	m, err := DecodePLY(gridPLY(0, false), nil)
	check(t, err)
	g := m.Objects[0].Geometry
	// Four cells, two triangles each, facing up, toward the scanner.
	if m.Triangles() != 8 || len(g.Points) != 0 {
		t.Fatalf("%d triangles", m.Triangles())
	}
	for _, v := range g.Vertices {
		if v.Normal != (nt3dmath.Vec3{Z: 1}) {
			t.Fatalf("normal %v", v.Normal)
		}
	}
	// A jump in depth at one corner leaves out the triangles that would bridge it.
	if m, err = DecodePLY(gridPLY(50, false), nil); err != nil || m.Triangles() != 6 {
		t.Fatalf("%d triangles across a depth jump, %v", m.Triangles(), err)
	}
	// A missing sample leaves its cell one triangle.
	if m, err = DecodePLY(gridPLY(0, true), nil); err != nil || m.Triangles() != 7 {
		t.Fatalf("%d triangles with a sample missing, %v", m.Triangles(), err)
	}
}

func TestConf(t *testing.T) {
	scan := gridPLY(0, false)
	// The second scan is moved 10 along x and turned a quarter about z. Its
	// quaternion is listed as Stanford's files list them, conjugated.
	s := math.Sqrt(.5)
	conf := fmt.Sprintf("camera 0 0 0 0 0 0 1\nbmesh a.ply 0 0 0 0 0 0 1\nbmesh b.ply 10 0 0 0 0 %g %g\nbmesh gone.ply 0 0 0 0 0 0 1\n", -s, s)
	files := map[string][]byte{"a.ply": scan, "b.ply": scan}
	m, err := DecodeConf([]byte(conf), &Options{Open: opener(files)})
	check(t, err)
	if len(m.Objects) != 2 || m.Objects[1].Name != "b.ply" || len(m.Warnings) != 1 || !strings.Contains(m.Warnings[0], "gone.ply") {
		t.Fatalf("%d objects, warnings %v", len(m.Objects), m.Warnings)
	}
	// Turned a quarter counter-clockwise, (x, y) goes to (-y, x), then moves.
	b := m.Objects[1].Geometry.Bounds
	if math.Abs(float64(b.Min.X-8)) > 1e-5 || math.Abs(float64(b.Max.X-10)) > 1e-5 || math.Abs(float64(b.Max.Y-2)) > 1e-5 {
		t.Fatalf("placed at %+v", b)
	}
	// The model turns up after its scans are placed.
	up, err := DecodeConf([]byte(conf), &Options{Open: opener(files), Up: YUp})
	check(t, err)
	if b := up.Objects[1].Geometry.Bounds; math.Abs(float64(b.Max.Z-2)) > 1e-5 {
		t.Fatalf("turned up to %+v", b)
	}
	if f, _ := FormatOf("bun.conf", nil); f != Conf {
		t.Fatal("not detected")
	}
	for _, bad := range []string{"bmesh a.ply 0 0 0", "bmesh a.ply 0 0 x 0 0 0 1", "camera 0 0 0 0 0 0 1\n"} {
		if _, err := DecodeConf([]byte(bad), &Options{Open: opener(files)}); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	// A line too long to scan is an error, not the end of the file.
	long := "bmesh a.ply 0 0 0 0 0 0 1\n# " + strings.Repeat("x", 70<<10) + "\nbmesh b.ply 0 0 0 0 0 0 1\n"
	if _, err := DecodeConf([]byte(long), &Options{Open: opener(files)}); err == nil {
		t.Fatal("stopped at a long line without an error")
	}
	if _, err := DecodeConf([]byte(conf), nil); err == nil {
		t.Fatal("read scans without side-file access")
	}
}
