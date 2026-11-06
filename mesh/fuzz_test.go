// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import (
	"archive/zip"
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// fuzzOptions keeps every decoder cheap enough for a shared fuzzing host:
// a small triangle budget and a small byte budget for unpacked data.
func fuzzOptions(files map[string][]byte) *Options {
	return &Options{MaxTriangles: 2048, MaxBytes: 1 << 20, Open: opener(files)}
}

// checkModel is the oracle shared by the decoder targets. A decoder may
// refuse any input; what it accepts must respect its budget, index its own
// vertices, hold finite coordinates, and validate as nt3d geometry.
func checkModel(t *testing.T, m *Model, err error, o *Options) {
	if err != nil {
		// Refusing input is always allowed. 3MF fills Parts and Metadata
		// even when over budget, but promises nothing else about the model.
		return
	}
	if m == nil {
		t.Fatal("no model and no error")
	}
	if n := m.Triangles(); n > o.MaxTriangles {
		t.Fatalf("%d triangles over the %d budget", n, o.MaxTriangles)
	}
	for i, obj := range m.Objects {
		g := obj.Geometry
		if len(obj.Plain) != 0 && len(obj.Plain) != len(g.Vertices) {
			t.Fatalf("object %d: %d plain flags for %d vertices", i, len(obj.Plain), len(g.Vertices))
		}
		if len(g.Indices)%3 != 0 {
			t.Fatalf("object %d: %d indices", i, len(g.Indices))
		}
		for _, k := range g.Indices {
			if int(k) >= len(g.Vertices) {
				t.Fatalf("object %d: index %d of %d vertices", i, k, len(g.Vertices))
			}
		}
		for _, v := range g.Vertices {
			if !finite(v.Position) || !finite(v.Normal) {
				t.Fatalf("object %d: non-finite vertex %+v", i, v)
			}
		}
		for _, p := range g.Points {
			if !finite(p.Position) {
				t.Fatalf("object %d: non-finite point %+v", i, p.Position)
			}
		}
		if g.GaussianSH != nil && len(g.GaussianSH)%max(1, len(g.Points)) != 0 {
			t.Fatalf("object %d: %d SH coefficients for %d points", i, len(g.GaussianSH), len(g.Points))
		}
		if err := g.Validate(); err != nil {
			t.Fatalf("object %d: %v", i, err)
		}
	}
}

func finite(v nt3dmath.Vec3) bool {
	return !math.IsNaN(float64(v.X)) && !math.IsInf(float64(v.X), 0) &&
		!math.IsNaN(float64(v.Y)) && !math.IsInf(float64(v.Y), 0) &&
		!math.IsNaN(float64(v.Z)) && !math.IsInf(float64(v.Z), 0)
}

// trianglesOf counts a model's triangles the way a round trip must preserve them.
func trianglesOf(m *Model) (n int) {
	for _, o := range m.Objects {
		n += len(o.Geometry.Indices) / 3
	}
	return n
}

func FuzzDecodeSTL(f *testing.F) {
	f.Add([]byte(asciiSTL))
	f.Add(binarySTLData("binary cube"))
	f.Add(binarySTLData("colored", 0x8000|31, 0x8000|(31<<5)))
	f.Add([]byte("solid x\nendsolid\n"))
	f.Add([]byte("solid x\nfacet normal 0 0 0\n outer loop\n vertex 0 0 nope"))
	f.Add(binarySTLData("short")[:60])
	f.Fuzz(func(t *testing.T, data []byte) {
		o := fuzzOptions(nil)
		m, err := DecodeSTL(data, o)
		checkModel(t, m, err, o)
		if err != nil {
			return
		}
		// What STL can carry survives a round trip: the same triangles.
		for _, obj := range m.Objects {
			var buf bytes.Buffer
			if err := EncodeSTL(&buf, obj.Geometry); err != nil {
				t.Fatalf("encode: %v", err)
			}
			again, err := DecodeSTL(buf.Bytes(), o)
			if err != nil {
				t.Fatalf("re-decode: %v", err)
			}
			if trianglesOf(again) != len(obj.Geometry.Indices)/3 {
				t.Fatalf("round trip: %d triangles became %d", len(obj.Geometry.Indices)/3, trianglesOf(again))
			}
		}
	})
}

func FuzzDecodeOBJ(f *testing.F) {
	f.Add([]byte(cubeOBJ), []byte("newmtl red\nKd 1 0 0\n"))
	f.Add([]byte("v 0 0 0\nv 1 0 0\nv 0 1 0\nf 1 2 3\n"), []byte(""))
	f.Add([]byte("v 0 0 0\nv 1 0 0\nv 0 1 0\nv 1 1 0\nf 1 2 3 4\nl 1 2\np 1\n"), []byte(""))
	f.Add([]byte("mtllib m.mtl\nusemtl red\nv 0 0 0\nv 1 0 0\nv 0 1 0\nvt 0 0\nvt 1 0\nvt 0 1\nvn 0 0 1\nf 1/1/1 2/2/1 3/3/1\n"), []byte("newmtl red\nKd 1 0 0\nmap_Kd t.png\n"))
	f.Add([]byte("v 0 0 0\nf 1 2 9\n"), []byte(""))
	f.Add([]byte("v 1e40 0 0\nv 1 0 0\nv 0 1 0\nf -1 -2 -3\n"), []byte(""))
	f.Fuzz(func(t *testing.T, data, mtl []byte) {
		// Any material library the file names resolves to the second input.
		o := fuzzOptions(map[string][]byte{"m.mtl": mtl, "cube.mtl": mtl})
		m, err := DecodeOBJ(data, o)
		checkModel(t, m, err, o)
		if err != nil {
			return
		}
		for _, obj := range m.Objects {
			var buf bytes.Buffer
			if err := EncodeOBJ(&buf, obj.Geometry); err != nil {
				t.Fatalf("encode: %v", err)
			}
			again, err := DecodeOBJ(buf.Bytes(), o)
			if err != nil {
				t.Fatalf("re-decode: %v", err)
			}
			if trianglesOf(again) != len(obj.Geometry.Indices)/3 {
				t.Fatalf("round trip: %d triangles became %d", len(obj.Geometry.Indices)/3, trianglesOf(again))
			}
		}
	})
}

// FuzzDecodePLY covers plain PLY in each encoding, range-grid scans, point
// clouds, and the Gaussian and compressed variants DecodePLY dispatches to.
func FuzzDecodePLY(f *testing.F) {
	header := "ply\nformat ascii 1.0\nelement vertex 3\nproperty float x\nproperty float y\nproperty float z\nelement face 1\nproperty list uchar int vertex_indices\nend_header\n"
	f.Add([]byte(header + "0 0 0\n1 0 0\n0 1 0\n3 0 1 2\n"))
	f.Add([]byte(header + "0 0 0\n1 0 0\n0 1 0\n3 0 1 7\n"))
	f.Add([]byte(strings.Replace(header, "element vertex 3", "element vertex 900000000", 1)))
	f.Add(cubePLY("ascii"))
	f.Add(cubePLY("binary_little_endian"))
	f.Add(cubePLY("binary_big_endian"))
	f.Add(gridPLY(0, true))
	f.Add([]byte("ply\nformat ascii 1.0\nelement vertex 3\nproperty float x\nproperty float y\nproperty float z\nproperty uchar red\nproperty uchar green\nproperty uchar blue\nend_header\n0 0 0 255 0 0\n1 0 0 0 255 0\n0 1 0 0 0 255\n"))
	f.Add(gaussianPLYSeed())
	f.Add(compressedPLYFixture(f))
	f.Fuzz(func(t *testing.T, data []byte) {
		o := fuzzOptions(nil)
		o.NoClusters = true
		m, err := DecodePLY(data, o)
		checkModel(t, m, err, o)
		if err != nil {
			return
		}
		for _, obj := range m.Objects {
			if len(obj.Geometry.Indices) == 0 {
				continue // EncodePLY writes meshes; clouds have their own formats.
			}
			var buf bytes.Buffer
			if err := EncodePLY(&buf, obj.Geometry); err != nil {
				t.Fatalf("encode: %v", err)
			}
			again, err := DecodePLY(buf.Bytes(), o)
			if err != nil {
				t.Fatalf("re-decode: %v", err)
			}
			if trianglesOf(again) != len(obj.Geometry.Indices)/3 {
				t.Fatalf("round trip: %d triangles became %d", len(obj.Geometry.Indices)/3, trianglesOf(again))
			}
		}
	})
}

// gaussianPLYSeed is the smallest 3DGS cloud: one splat with degree-0 SH.
func gaussianPLYSeed() []byte {
	return []byte("ply\nformat ascii 1.0\nelement vertex 2\nproperty float x\nproperty float y\nproperty float z\n" +
		"property float f_dc_0\nproperty float f_dc_1\nproperty float f_dc_2\nproperty float opacity\n" +
		"property float scale_0\nproperty float scale_1\nproperty float scale_2\n" +
		"property float rot_0\nproperty float rot_1\nproperty float rot_2\nproperty float rot_3\nend_header\n" +
		"0 0 0 0.5 0.2 -0.1 1 -1 -1 -1 1 0 0 0\n1 0 0 0.1 0.2 0.3 0 -2 -1 -0.5 0.7 0.7 0 0\n")
}

func compressedPLYFixture(f *testing.F) []byte {
	data, err := os.ReadFile(filepath.Join("testdata", "gaussian-ring.compressed.ply"))
	if err != nil {
		f.Fatal(err)
	}
	return data
}

// stored3MF zips the parts uncompressed, so the fuzzer's byte mutations
// reach the XML rather than a deflate stream, and seed minimization is quick.
func stored3MF(f *testing.F, parts map[string]string) []byte {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, content := range parts {
		e, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			f.Fatal(err)
		}
		if _, err := e.Write([]byte(content)); err != nil {
			f.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		f.Fatal(err)
	}
	return b.Bytes()
}

func FuzzDecode3MF(f *testing.F) {
	// Few, small seeds: the fuzzer re-minimizes each seed every round, and a
	// zip takes it seconds, even stored.
	tri := `<resources><object id="1" type="model"><mesh><vertices><vertex x="0" y="0" z="0"/><vertex x="1" y="0" z="0"/><vertex x="0" y="1" z="0"/></vertices><triangles><triangle v1="0" v2="1" v3="2"/></triangles></mesh></object></resources><build><item objectid="1"/></build>`
	rels := `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Target="/3D/3dmodel.model" Id="rel-1" Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"/></Relationships>`
	f.Add(stored3MF(f, map[string]string{"_rels/.rels": rels, "3D/3dmodel.model": model3MF(`unit="millimeter"`, tri)}))
	f.Add(stored3MF(f, map[string]string{"_rels/.rels": rels, "3D/3dmodel.model": model3MF("", strings.Replace(tri, `v3="2"`, `v3="9"`, 1))}))
	f.Add([]byte("PK\x03\x04 not really a zip"))
	f.Fuzz(func(t *testing.T, data []byte) {
		o := fuzzOptions(nil)
		o.MaxBytes = 1 << 16
		m, err := Decode3MF(data, o)
		checkModel(t, m, err, o)
		if m != nil {
			for name := range m.Metadata {
				if name == "" {
					t.Fatal("metadata with an empty name")
				}
			}
			for _, obj := range m.Objects {
				if obj.Part < 0 || obj.Part >= max(1, len(m.Parts)) {
					t.Fatalf("object names part %d of %d", obj.Part, len(m.Parts))
				}
			}
		}
	})
}

// FuzzDecodeConf feeds the Stanford .conf placement file; every scan it
// names resolves to one small PLY, so the target exercises placement, not PLY.
func FuzzDecodeConf(f *testing.F) {
	s := math.Sqrt(.5)
	f.Add("camera 0 0 0 0 0 0 1\nbmesh a.ply 0 0 0 0 0 0 1\nbmesh b.ply 10 0 0 0 0 " + ftoa(-s) + " " + ftoa(s) + "\nbmesh gone.ply 0 0 0 0 0 0 1\n")
	f.Add("bmesh a.ply 0 0 0")
	f.Add("bmesh a.ply 0 0 x 0 0 0 1")
	f.Add("camera 0 0 0 0 0 0 1\n")
	f.Add("bmesh a.ply 1e308 0 0 0 0 0 1\n")
	f.Add("bmesh a.ply 0 0 0 0 0 0 0\n")
	scan := gridPLY(0, false)
	f.Fuzz(func(t *testing.T, conf string) {
		o := fuzzOptions(map[string][]byte{"a.ply": scan, "b.ply": scan})
		m, err := DecodeConf([]byte(conf), o)
		checkModel(t, m, err, o)
	})
}

func ftoa(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// FuzzTriangulate checks the polygon splitter: n corners give between one
// and n-2 triangles that index only those corners and never repeat a corner.
func FuzzTriangulate(f *testing.F) {
	f.Add([]byte{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0})
	f.Add([]byte{0, 0, 0, 4, 0, 0, 4, 4, 0, 2, 1, 0, 0, 4, 0}) // concave
	f.Add([]byte{0, 0, 0, 1, 0, 0, 2, 0, 0})                   // collinear
	f.Add([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})          // coincident
	f.Fuzz(func(t *testing.T, raw []byte) {
		n := len(raw) / 3
		if n > 300 {
			n = 300 // past maxEarClip falls back to a fan; a few over is enough
		}
		points := make([]nt3dmath.Vec3, n)
		for i := range points {
			points[i] = nt3dmath.Vec3{X: float32(int8(raw[3*i])), Y: float32(int8(raw[3*i+1])), Z: float32(int8(raw[3*i+2]))}
		}
		tris := triangulate(points)
		// Corners on a straight run add no area and may be dropped, so a
		// polygon gives at most n-2 triangles, and at least one.
		if n < 3 && len(tris) != 0 || n >= 3 && (len(tris) < 1 || len(tris) > n-2) {
			t.Fatalf("%d corners: %d triangles", n, len(tris))
		}
		for _, tri := range tris {
			for _, k := range tri {
				if k < 0 || k >= n {
					t.Fatalf("corner %d of %d", k, n)
				}
			}
			if tri[0] == tri[1] || tri[1] == tri[2] || tri[0] == tri[2] {
				t.Fatalf("degenerate triangle %v", tri)
			}
		}
	})
}
