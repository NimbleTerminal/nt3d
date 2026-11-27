// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
	"nimbleterminal.dev/nt3d/mesh/spz"
)

// twoSplatSPZ is a version 2 SPZ of two splats with degree-1 harmonics,
// neither of them symmetric about any axis, so a flip or a turn shows.
func twoSplatSPZ(t testing.TB) []byte {
	t.Helper()
	raw := make([]byte, 16)
	binary.LittleEndian.PutUint32(raw, 0x5053474e)
	binary.LittleEndian.PutUint32(raw[4:], 2)
	binary.LittleEndian.PutUint32(raw[8:], 2)
	raw[12], raw[13] = 1, 12 // SH degree, fractional bits of a position
	fixed := func(v float64) []byte { n := int32(v * 4096); return []byte{byte(n), byte(n >> 8), byte(n >> 16)} }
	for _, p := range [][3]float64{{1, 2, 3}, {-4, .5, 6}} {
		raw = append(raw, append(append(fixed(p[0]), fixed(p[1])...), fixed(p[2])...)...)
	}
	raw = append(raw, 191, 64)                      // alphas
	raw = append(raw, 200, 128, 60, 30, 90, 220)    // colors
	raw = append(raw, 171, 160, 149, 150, 165, 180) // log scales
	raw = append(raw, 150, 100, 218, 90, 200, 140)  // rotations: x, y, z
	raw = append(raw, 140, 100, 180, 90, 128, 200, 60, 170, 110, 128, 128, 128, 200, 60, 90, 100, 150, 250)
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// spzSplatDoc is a glTF whose one splat primitive keeps its data in an SPZ
// payload, side.bin, named by extension as the primitive's JSON.
func spzSplatDoc(payload []byte, count int, extension, used, node string) []byte {
	if node == "" {
		node = `{"mesh":0}`
	}
	return []byte(fmt.Sprintf(`{"asset":{"version":"2.0"},"extensionsUsed":[%s],"extensionsRequired":[%s],"buffers":[{"uri":"side.bin","byteLength":%d}],"bufferViews":[{"buffer":0,"byteLength":%d}],"accessors":[{"componentType":5126,"count":%d,"type":"VEC3"}],"meshes":[{"primitives":[{"attributes":{"POSITION":0},"mode":0,"extensions":%s}]}],"nodes":[%s],"scenes":[{"nodes":[0]}],"scene":0}`,
		used, used, len(payload), len(payload), count, extension, node))
}

const (
	spzNested = `{"KHR_gaussian_splatting":{"kernel":"ellipse","colorSpace":"srgb_rec709_display","extensions":{"KHR_gaussian_splatting_compression_spz_2":{"bufferView":0}}}}`
	spzUsed   = `"KHR_gaussian_splatting","KHR_gaussian_splatting_compression_spz_2"`
)

// A splat primitive compressed with SPZ decodes to the cloud the same bytes
// give as an .spz file: the payload's values are glTF's Y-up coordinates,
// turned to Z-up once with the scene, and nothing is flipped on the way.
func TestGaussianSplattingSPZMatchesTheSPZFile(t *testing.T) {
	payload := twoSplatSPZ(t)
	file, err := nt3dspz.Decode(payload, nil)
	check(t, err)
	want := file.Objects[0].Geometry
	for name, c := range map[string][2]string{
		"the draft's name":            {spzNested, spzUsed},
		"the name its examples use":   {strings.Replace(spzNested, "_spz_2", "_spz", 1), `"KHR_gaussian_splatting","KHR_gaussian_splatting_compression_spz"`},
		"the earlier primitive draft": {`{"KHR_spz_gaussian_splats_compression":{"bufferView":0}}`, `"KHR_spz_gaussian_splats_compression"`},
	} {
		t.Run(name, func(t *testing.T) {
			m, err := Decode(spzSplatDoc(payload, 2, c[0], c[1], ""), &nt3dmesh.Options{Open: opener(map[string][]byte{"side.bin": payload})})
			check(t, err)
			if len(m.Objects) != 1 {
				t.Fatalf("%d objects, want one cloud", len(m.Objects))
			}
			got := m.Objects[0].Geometry
			if len(got.Points) != 2 || len(got.GaussianSH) != len(want.GaussianSH) || got.PointStyle != want.PointStyle {
				t.Fatalf("%d points and %d coefficients styled %+v", len(got.Points), len(got.GaussianSH), got.PointStyle)
			}
			for i := range got.Points {
				a, b := got.Points[i], want.Points[i]
				if !near(a.Position, b.Position) || a.Color != b.Color || !near(a.GaussianAxes[0], b.GaussianAxes[0]) || !near(a.GaussianAxes[1], b.GaussianAxes[1]) || !near(a.GaussianAxes[2], b.GaussianAxes[2]) {
					t.Fatalf("splat %d: glTF %+v, SPZ file %+v", i, a, b)
				}
			}
			for i := range got.GaussianSH {
				if got.GaussianSH[i] != want.GaussianSH[i] {
					t.Fatalf("coefficient %d: glTF %v, SPZ file %v", i, got.GaussianSH[i], want.GaussianSH[i])
				}
			}
			for k := range 16 {
				if d := got.GaussianSHTransform[k] - want.GaussianSHTransform[k]; d > 1e-6 || d < -1e-6 {
					t.Fatalf("harmonics frame: glTF %v, SPZ file %v", got.GaussianSHTransform, want.GaussianSHTransform)
				}
			}
			if got.Clusters == nil && want.Clusters != nil {
				t.Fatal("the tile's cloud has no clusters for level of detail, and the file's has")
			}
		})
	}
	// The first splat is stored at (1, 2, 3): Z-up, that is (1, -3, 2).
	if p := want.Points[0].Position; !near(p, nt3dmath.Vec3{X: 1, Y: -3, Z: 2}) {
		t.Fatalf("the SPZ file's first splat is at %v, want (1, -3, 2)", p)
	}
}

// A compressed splat primitive follows its node like any other, and lands in
// the scene asset too.
func TestGaussianSplattingSPZFollowsItsNode(t *testing.T) {
	payload := twoSplatSPZ(t)
	doc := spzSplatDoc(payload, 2, spzNested, spzUsed, `{"mesh":0,"translation":[10,0,0]}`)
	m, err := Decode(doc, &nt3dmesh.Options{Open: opener(map[string][]byte{"side.bin": payload})})
	check(t, err)
	if p := m.Objects[0].Geometry.Points[0].Position; !near(p, nt3dmath.Vec3{X: 11, Y: -3, Z: 2}) {
		t.Fatalf("first splat at %v, want (11, -3, 2)", p)
	}
	if m.Asset == nil || len(m.Asset.Nodes) == 0 {
		t.Fatal("no asset")
	}
}

// A compressed splat primitive whose payload is missing, is not SPZ, or holds
// a different number of splats than the primitive says is an error.
func TestGaussianSplattingSPZRefusals(t *testing.T) {
	payload := twoSplatSPZ(t)
	for name, c := range map[string]struct {
		doc  []byte
		side []byte
		want string
	}{
		"no buffer view":      {spzSplatDoc(payload, 2, strings.Replace(spzNested, `{"bufferView":0}`, `{}`, 1), spzUsed, ""), payload, "buffer view"},
		"buffer view 7":       {spzSplatDoc(payload, 2, strings.Replace(spzNested, `"bufferView":0`, `"bufferView":7`, 1), spzUsed, ""), payload, "buffer view"},
		"count differs":       {spzSplatDoc(payload, 3, spzNested, spzUsed, ""), payload, "holds 2"},
		"not SPZ":             {spzSplatDoc(payload, 2, spzNested, spzUsed, ""), bytes.Repeat([]byte{7}, len(payload)), "SPZ"},
		"triangles":           {bytes.Replace(spzSplatDoc(payload, 2, spzNested, spzUsed, ""), []byte(`"mode":0`), []byte(`"mode":4`), 1), payload, "POINTS"},
		"legacy on triangles": {bytes.Replace(spzSplatDoc(payload, 2, `{"KHR_spz_gaussian_splats_compression":{"bufferView":0}}`, `"KHR_spz_gaussian_splats_compression"`, ""), []byte(`"mode":0`), []byte(`"mode":4`), 1), payload, "POINTS"},
	} {
		_, err := Decode(c.doc, &nt3dmesh.Options{Open: opener(map[string][]byte{"side.bin": c.side})})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error naming %q", name, err, c.want)
		}
	}
}

// The shared Gaussian packed as SPZ decodes to the same splat as its plain
// attributes, to within what SPZ's bytes hold: a position on a 1/4096 grid,
// log scales in sixteenths, rotations and harmonics in 1/128 steps. The
// packing here is the one a writer of tiles uses: values as glTF's Y-up
// coordinates, nothing flipped.
func TestGaussianSplattingSPZReadsTheSharedVector(t *testing.T) {
	v := readGaussianVector(t)
	raw := make([]byte, 16)
	binary.LittleEndian.PutUint32(raw, 0x5053474e)
	binary.LittleEndian.PutUint32(raw[4:], 2)
	binary.LittleEndian.PutUint32(raw[8:], 1)
	raw[12], raw[13] = 3, 12
	quantize := func(x float64) byte { return byte(math.Round(max(0, min(255, x)))) }
	for _, c := range v.Position {
		n := int32(math.Round(c * 4096))
		raw = append(raw, byte(n), byte(n>>8), byte(n>>16))
	}
	raw = append(raw, quantize(255*v.Opacity))
	for _, dc := range v.SH[0] {
		raw = append(raw, quantize(255*(.5+.15*dc)))
	}
	for _, s := range v.Scale {
		raw = append(raw, quantize(16*(math.Log(s)+10)))
	}
	for _, q := range v.Rotation[:3] { // x, y, z; w is recovered, and is positive here.
		raw = append(raw, quantize((q+1)*127.5))
	}
	for _, coefficient := range v.SH[1:] {
		for _, c := range coefficient {
			raw = append(raw, quantize(c*128+128))
		}
	}
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, err := w.Write(raw)
	check(t, err)
	check(t, w.Close())
	payload := buf.Bytes()
	m, err := Decode(spzSplatDoc(payload, 1, spzNested, spzUsed, ""), &nt3dmesh.Options{Open: opener(map[string][]byte{"side.bin": payload})})
	check(t, err)
	g := m.Objects[0].Geometry
	p := g.Points[0]
	if !near(p.Position, vec3(v.ZUp.Position)) {
		t.Fatalf("position %v, want %v", p.Position, v.ZUp.Position)
	}
	for k := range 3 {
		if d := p.GaussianAxes[k].Sub(vec3(v.ZUp.Axes[k])).D().Len(); d > .03 {
			t.Fatalf("axis %d is %v, %g from %v", k, p.GaussianAxes[k], d, v.ZUp.Axes[k])
		}
	}
	for k, got := range [4]uint8{p.Color.R, p.Color.G, p.Color.B, p.Color.A} {
		want := 255 * v.Opacity
		if k < 3 {
			want = 255 * v.Diffuse[k]
		}
		if math.Abs(float64(got)-want) > 2 {
			t.Fatalf("color channel %d is %d, want about %g", k, got, want)
		}
	}
	if len(g.GaussianSH) != 16 {
		t.Fatalf("%d SH coefficients, want 16", len(g.GaussianSH))
	}
	for i := 1; i < 16; i++ {
		for k := range 3 {
			if math.Abs(float64(g.GaussianSH[i][k])-v.SH[i][k]) > 1.0/128 {
				t.Fatalf("SH coefficient %d is %v, want %v", i, g.GaussianSH[i], v.SH[i])
			}
		}
	}
}
