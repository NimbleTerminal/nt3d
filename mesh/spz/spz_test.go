// Copyright (c) 2026 Neomantra Corp.
package nt3dspz

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/klauspost/compress/zstd"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
)

// splat is one packed splat as a test writes it: its rotation is x, y, z, w.
type splat struct {
	position [3]float32
	alpha    uint8
	color    [3]uint8
	scale    [3]uint8
	rotation [4]float32
	sh       []uint8
}

// encodeSPZ packs splats as an SPZ file of the given version, 12 fractional
// bits, with the SH degree the first splat's coefficient count implies.
func encodeSPZ(t *testing.T, version int, splats []splat) []byte {
	t.Helper()
	degree := map[int]int{0: 0, 9: 1, 24: 2, 45: 3, 72: 4}[len(splats[0].sh)]
	var positions, alphas, colors, scales, rotations, sh []byte
	for _, s := range splats {
		for _, v := range s.position {
			fixed := int32(math.Round(float64(v) * 4096))
			positions = append(positions, byte(fixed), byte(fixed>>8), byte(fixed>>16))
		}
		alphas = append(alphas, s.alpha)
		colors = append(colors, s.color[:]...)
		scales = append(scales, s.scale[:]...)
		q := s.rotation
		n := float32(math.Sqrt(float64(q[0]*q[0] + q[1]*q[1] + q[2]*q[2] + q[3]*q[3])))
		for i := range q {
			q[i] /= n
		}
		if version < 3 {
			if q[3] < 0 {
				for i := range q {
					q[i] = -q[i]
				}
			}
			for _, v := range q[:3] {
				rotations = append(rotations, uint8(math.Round(float64(v)*127.5+127.5)))
			}
		} else {
			largest := 0
			for i := 1; i < 4; i++ {
				if math.Abs(float64(q[i])) > math.Abs(float64(q[largest])) {
					largest = i
				}
			}
			negate := q[largest] < 0
			packed := uint32(largest)
			for i := range 4 {
				if i == largest {
					continue
				}
				negative := (q[i] < 0) != negate
				magnitude := uint32(math.Abs(float64(q[i]))/math.Sqrt(.5)*511 + .5)
				packed = packed<<10 | boolBit(negative)<<9 | magnitude
			}
			rotations = binary.LittleEndian.AppendUint32(rotations, packed)
		}
		sh = append(sh, s.sh...)
	}
	header := make([]byte, 16)
	binary.LittleEndian.PutUint32(header, 0x5053474e)
	binary.LittleEndian.PutUint32(header[4:], uint32(version))
	binary.LittleEndian.PutUint32(header[8:], uint32(len(splats)))
	header[12], header[13] = uint8(degree), 12
	if version < 4 {
		var b bytes.Buffer
		w := gzip.NewWriter(&b)
		for _, part := range [][]byte{header, positions, alphas, colors, scales, rotations, sh} {
			w.Write(part)
		}
		w.Close()
		return b.Bytes()
	}
	var streams [][]byte
	for _, part := range [][]byte{positions, alphas, colors, scales, rotations, sh} {
		if len(part) > 0 {
			streams = append(streams, part)
		}
	}
	return ngsp(t, header, streams)
}

// ngsp assembles a version 4 file: the 16-byte header, the stream count,
// the table offset, reserved bytes, the table, and zstd streams.
func ngsp(t *testing.T, header []byte, streams [][]byte) []byte {
	t.Helper()
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	out := append([]byte{}, header[:15]...)
	out = append(out, uint8(len(streams)))
	out = binary.LittleEndian.AppendUint32(out, 32)
	out = append(out, make([]byte, 12)...)
	var compressed [][]byte
	for _, s := range streams {
		c := enc.EncodeAll(s, nil)
		compressed = append(compressed, c)
		out = binary.LittleEndian.AppendUint64(out, uint64(len(c)))
		out = binary.LittleEndian.AppendUint64(out, uint64(len(s)))
	}
	for _, c := range compressed {
		out = append(out, c...)
	}
	return out
}

func boolBit(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

// srgb8 is the DC color channel a byte decodes to: through the SPZ color
// scale and the 3DGS offset, as the PLY decoder does.
func srgb8(b uint8) uint8 {
	dc := (float64(b)/255 - .5) / .15
	return uint8(math.Round(255 * max(0, min(1, .5+.28209479177387814*dc))))
}

func near(a, b, tolerance float32) bool { return math.Abs(float64(a-b)) <= float64(tolerance) }

// One splat of known bytes decodes to the same point from every version:
// positions, scales, color, alpha, SH, and the Y-up rotation to Z-up.
func TestDecodeKnownValues(t *testing.T) {
	for _, version := range []int{2, 3, 4} {
		s := splat{position: [3]float32{1.5, -2, .25}, alpha: 255, color: [3]uint8{166, 128, 90}, scale: [3]uint8{160, 160, 128}, rotation: [4]float32{0, 0, 0, 1}, sh: bytes.Repeat([]byte{192}, 9)}
		m, err := Decode(encodeSPZ(t, version, []splat{s}), nil)
		if err != nil {
			t.Fatalf("version %d: %v", version, err)
		}
		if m.Format != SPZ || len(m.Objects) != 1 || len(m.Objects[0].Geometry.Points) != 1 {
			t.Fatalf("version %d: format %q, %d objects", version, m.Format, len(m.Objects))
		}
		g := m.Objects[0].Geometry
		p := g.Points[0]
		if !near(p.Position.X, 1.5, 1e-6) || !near(p.Position.Y, -.25, 1e-6) || !near(p.Position.Z, -2, 1e-6) {
			t.Fatalf("version %d: position %v, want (1.5, -0.25, -2) after the Y-up rotation", version, p.Position)
		}
		if p.Color.A != 255 || p.Color.R != srgb8(166) || p.Color.G != srgb8(128) || p.Color.B != srgb8(90) {
			t.Fatalf("version %d: color %v", version, p.Color)
		}
		// Identity rotation: axes along x, y, z (then rotated to Z-up) of
		// lengths e^0, e^0, e^-2.
		lengths := [3]float64{1, 1, math.Exp(-2)}
		for k, a := range p.GaussianAxes {
			if l := math.Sqrt(float64(a.Dot(a))); math.Abs(l-lengths[k])/lengths[k] > .02 {
				t.Fatalf("version %d: axis %d length %v, want %v", version, k, l, lengths[k])
			}
		}
		if g.PointStyle.Shape != nt3d.GaussianPoint || g.PointStyle.Units != nt3d.WorldUnits || g.PointStyle.Blend != nt3d.AlphaBlend || p.Radius != 1 {
			t.Fatalf("version %d: style %+v radius %v", version, g.PointStyle, p.Radius)
		}
		if len(g.GaussianSH) != 4 || !near(g.GaussianSH[1][0], .5, 1e-6) || !near(g.GaussianSH[3][2], .5, 1e-6) || !near(g.GaussianSH[0][0], (166./255-.5)/.15, 1e-5) {
			t.Fatalf("version %d: SH %v", version, g.GaussianSH)
		}
		if g.GaussianSHTransform == (nt3dmath.Mat4{}) || !g.Bounds.IsValid() || g.Validate() != nil {
			t.Fatalf("version %d: no SH transform, bounds, or validity", version)
		}
	}
}

// Malformed and unsupported files are refused without reading past their
// bytes or allocating for their claims.
func TestDecodeRejects(t *testing.T) {
	one := []splat{{alpha: 255, scale: [3]uint8{160, 160, 160}, rotation: [4]float32{0, 0, 0, 1}}}
	for _, c := range []struct {
		name string
		data []byte
		o    *nt3dmesh.Options
	}{
		{"version 1", patch(t, encodeSPZ(t, 2, one), 4, 1), nil},
		{"version 5", patch(t, encodeSPZ(t, 2, one), 4, 5), nil},
		{"degree 4", patch(t, encodeSPZ(t, 2, one), 12, 4), nil},
		{"no points", patch(t, encodeSPZ(t, 2, one), 8, 0), nil},
		{"a billion points over 40 bytes", patch(t, encodeSPZ(t, 2, one), 8, 1e9), nil},
		{"v4 a billion points", rawPatch(encodeSPZ(t, 4, one), 8, 1e9), nil},
		{"v4 stream count off", rawPatch(encodeSPZ(t, 4, one), 15, 6), nil},
		{"v4 stream size off", rawPatch(encodeSPZ(t, 4, one), 32+8, 10), nil},
		{"v4 table past the end", rawPatch(encodeSPZ(t, 4, one), 16, 1<<20), nil},
		{"truncated gzip", encodeSPZ(t, 2, one)[:20], nil},
		{"truncated v4", encodeSPZ(t, 4, one)[:60], nil},
		{"not spz", []byte("ply\nformat ascii 1.0\n"), nil},
		{"gzip past the byte budget", bigGzip(t), &nt3dmesh.Options{MaxBytes: 1 << 20}},
	} {
		if _, err := Decode(c.data, c.o); err == nil {
			t.Errorf("%s: decoded", c.name)
		} else if c.name == "gzip past the byte budget" && !errors.Is(err, nt3dmesh.ErrTooLarge) {
			t.Errorf("%s: %v, want ErrTooLarge", c.name, err)
		}
	}
	// Quantized rotation bytes past the unit sphere still give a finite
	// unit quaternion.
	data := encodeSPZ(t, 2, one)
	raw := gunzip(t, data)
	copy(raw[16+9+1+3+3:], []byte{255, 255, 255})
	m, err := Decode(gz(t, raw), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range m.Objects[0].Geometry.Points[0].GaussianAxes {
		if l := a.Dot(a); math.IsNaN(float64(l)) || math.IsInf(float64(l), 0) || l == 0 {
			t.Fatalf("axis %v from an overlong quaternion", a)
		}
	}
}

// patch rewrites a little-endian uint32 at an offset inside a gzip file's
// content; rawPatch does the same to the bytes of a raw file.
func patch(t *testing.T, data []byte, offset int, value uint32) []byte {
	t.Helper()
	raw := gunzip(t, data)
	binary.LittleEndian.PutUint32(raw[offset:], value)
	return gz(t, raw)
}

func rawPatch(data []byte, offset int, value uint32) []byte {
	out := append([]byte{}, data...)
	binary.LittleEndian.PutUint32(out[offset:], value)
	return out
}

func gunzip(t *testing.T, data []byte) []byte {
	t.Helper()
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if _, err := b.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func gz(t *testing.T, raw []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	w.Write(raw)
	w.Close()
	return b.Bytes()
}

// bigGzip is a header claiming 2 MiB of zero splats, gzipped to a few KB.
func bigGzip(t *testing.T) []byte {
	t.Helper()
	raw := make([]byte, 2<<20)
	binary.LittleEndian.PutUint32(raw, 0x5053474e)
	binary.LittleEndian.PutUint32(raw[4:], 2)
	binary.LittleEndian.PutUint32(raw[8:], (2<<20)/28)
	raw[12], raw[13] = 1, 12
	return gz(t, raw)
}

// The fixtures, written from the demos' ring by Niantic's reference encoder
// with no axis conversion, decode to the ring as the PLY decoder reads it
// under the same Y-up rotation, within quantization. Version 2 keeps three
// 8-bit quaternion components and recovers the fourth, which a small fourth
// component makes a few degrees off; the smallest-three packing from
// version 3 stays within one.
func TestFixturesMatchPLY(t *testing.T) {
	ply, err := os.ReadFile(filepath.Join("..", "..", "examples", "demos", "internal", "demo", "models", "gaussian-ring.ply"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := nt3dmesh.DecodePLY(ply, &nt3dmesh.Options{Up: nt3dmesh.YUp})
	if err != nil {
		t.Fatal(err)
	}
	wg := want.Objects[0].Geometry
	for _, version := range []string{"v2", "v3", "v4"} {
		turn := math.Pi / 180
		if version == "v2" {
			turn = 10 * math.Pi / 180
		}
		data, err := os.ReadFile(filepath.Join("testdata", "gaussian-ring."+version+".spz"))
		if err != nil {
			t.Fatal(err)
		}
		got, err := Decode(data, nil)
		if err != nil {
			t.Fatalf("%s: %v", version, err)
		}
		g := got.Objects[0].Geometry
		if len(got.Objects) != 1 || len(g.Points) != len(wg.Points) || len(g.GaussianSH) != len(wg.GaussianSH) || g.GaussianSHTransform != wg.GaussianSHTransform {
			t.Fatalf("%s: %d objects, %d points, %d SH", version, len(got.Objects), len(g.Points), len(g.GaussianSH))
		}
		for i, p := range g.Points {
			q := wg.Points[i]
			d := p.Position.Sub(q.Position)
			if math.Sqrt(float64(d.Dot(d))) > math.Sqrt(3)/4096 {
				t.Fatalf("%s: point %d at %v, PLY %v", version, i, p.Position, q.Position)
			}
			if int(p.Color.A)-int(q.Color.A) > 1 || int(q.Color.A)-int(p.Color.A) > 1 {
				t.Fatalf("%s: point %d alpha %d, PLY %d", version, i, p.Color.A, q.Color.A)
			}
			for _, c := range [][2]uint8{{p.Color.R, q.Color.R}, {p.Color.G, q.Color.G}, {p.Color.B, q.Color.B}} {
				if int(c[0])-int(c[1]) > 2 || int(c[1])-int(c[0]) > 2 {
					t.Fatalf("%s: point %d color %v, PLY %v", version, i, p.Color, q.Color)
				}
			}
			for k := range 3 {
				a, b := p.GaussianAxes[k], q.GaussianAxes[k]
				la, lb := math.Sqrt(float64(a.Dot(a))), math.Sqrt(float64(b.Dot(b)))
				angle := math.Acos(max(-1, min(1, float64(a.Dot(b))/(la*lb))))
				if math.Abs(la-lb)/lb > .04 || angle > turn {
					t.Fatalf("%s: point %d axis %d %v, PLY %v: lengths %v %v, angle %.2f°", version, i, k, a, b, la, lb, angle*180/math.Pi)
				}
			}
		}
		// DC color is a byte over 0.15 (steps of 0.026); the reference
		// encoder keeps 5 bits of a degree-1 coefficient (steps of 0.0625, after
		// rounding to 1/128).
		for i, c := range g.GaussianSH {
			tolerance := float32(.036)
			if i%4 == 0 {
				tolerance = .014
			}
			for ch := range 3 {
				if !near(c[ch], wg.GaussianSH[i][ch], tolerance) {
					t.Fatalf("%s: SH %d channel %d %v, PLY %v", version, i, ch, c[ch], wg.GaussianSH[i][ch])
				}
			}
		}
	}
}

// The format is known by extension and by content, decodes through
// nt3dmesh.Decode, and orients itself whatever Options.Up says.
func TestRegistration(t *testing.T) {
	v2, err := os.ReadFile(filepath.Join("testdata", "gaussian-ring.v2.spz"))
	if err != nil {
		t.Fatal(err)
	}
	v4, err := os.ReadFile(filepath.Join("testdata", "gaussian-ring.v4.spz"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		data []byte
	}{{"a.spz", nil}, {"a.bin", v2}, {"a.bin", v4}} {
		if f, _ := nt3dmesh.FormatOf(c.name, c.data); f != SPZ {
			t.Fatalf("%s with %d bytes: %q", c.name, len(c.data), f)
		}
	}
	if f, _ := nt3dmesh.FormatOf("a.bin", gz(t, []byte("not a splat file at all"))); f == SPZ {
		t.Fatal("a gzip of something else sniffed as SPZ")
	}
	m, err := nt3dmesh.Decode(v4, SPZ, nil)
	if err != nil || m.Format != SPZ {
		t.Fatalf("Decode: %v, format %q", err, m.Format)
	}
	up, err := Decode(v2, &nt3dmesh.Options{Up: nt3dmesh.YUp})
	if err != nil {
		t.Fatal(err)
	}
	flat, err := Decode(v2, &nt3dmesh.Options{Up: nt3dmesh.ZUp})
	if err != nil {
		t.Fatal(err)
	}
	if up.Objects[0].Geometry.Points[7] != flat.Objects[0].Geometry.Points[7] {
		t.Fatal("Options.Up changed an SPZ decode")
	}
}

// Niantic's lizard sample, when NT3D_SPZ_SAMPLES names its directory,
// decodes as two slabs of degree-3 splats.
func TestSamples(t *testing.T) {
	dir := os.Getenv("NT3D_SPZ_SAMPLES")
	if dir == "" {
		t.Skip("NT3D_SPZ_SAMPLES not set")
	}
	data, err := os.ReadFile(filepath.Join(dir, "hornedlizard.spz"))
	if err != nil {
		t.Skip(err)
	}
	m, err := Decode(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, o := range m.Objects {
		g := o.Geometry
		if len(g.GaussianSH) != 16*len(g.Points) || !g.Bounds.IsValid() || g.Validate() != nil {
			t.Fatalf("piece of %d points, %d SH", len(g.Points), len(g.GaussianSH))
		}
		total += len(g.Points)
	}
	if len(m.Objects) != 2 || total != 786233 {
		t.Fatalf("%d objects, %d points", len(m.Objects), total)
	}
}

// Each fixture decodes to what the reference decoder reads from it, to
// float precision: the bytes mean the same here as there.
func TestFixturesMatchReference(t *testing.T) {
	turn := func(v nt3dmath.Vec3) nt3dmath.Vec3 { return nt3dmath.Vec3{X: v.X, Y: -v.Z, Z: v.Y} }
	for _, version := range []string{"v2", "v3", "v4"} {
		data, err := os.ReadFile(filepath.Join("testdata", "gaussian-ring."+version+".spz"))
		if err != nil {
			t.Fatal(err)
		}
		ref, err := os.ReadFile(filepath.Join("testdata", "gaussian-ring."+version+".ref"))
		if err != nil {
			t.Fatal(err)
		}
		m, err := Decode(data, nil)
		if err != nil {
			t.Fatal(err)
		}
		g := m.Objects[0].Geometry
		const floats = 3 + 3 + 4 + 1 + 3 + 9
		if len(ref) != 4*floats*len(g.Points) || len(g.GaussianSH) != 4*len(g.Points) {
			t.Fatalf("%s: %d reference bytes for %d points, %d SH", version, len(ref), len(g.Points), len(g.GaussianSH))
		}
		at := func(i, k int) float64 {
			return float64(math.Float32frombits(binary.LittleEndian.Uint32(ref[4*(i*floats+k):])))
		}
		for i, p := range g.Points {
			var logScales, dc [3]float64
			for k := range 3 {
				logScales[k], dc[k] = at(i, 3+k), at(i, 11+k)
			}
			want, err := nt3dmesh.GaussianPoint(nt3dmath.Vec3{X: float32(at(i, 0)), Y: float32(at(i, 1)), Z: float32(at(i, 2))}, [4]float64{at(i, 9), at(i, 6), at(i, 7), at(i, 8)}, logScales, dc, 1/(1+math.Exp(-at(i, 10))))
			if err != nil {
				t.Fatal(err)
			}
			want.Position = turn(want.Position)
			for k := range want.GaussianAxes {
				want.GaussianAxes[k] = turn(want.GaussianAxes[k])
			}
			if p.Color != want.Color || p.Radius != want.Radius {
				t.Fatalf("%s: point %d color %v radius %v, reference %v %v", version, i, p.Color, p.Radius, want.Color, want.Radius)
			}
			d := p.Position.Sub(want.Position)
			if d.Dot(d) > 1e-10 {
				t.Fatalf("%s: point %d at %v, reference %v", version, i, p.Position, want.Position)
			}
			for k := range 3 {
				d := p.GaussianAxes[k].Sub(want.GaussianAxes[k])
				if d.Dot(d) > 1e-10*want.GaussianAxes[k].Dot(want.GaussianAxes[k]) {
					t.Fatalf("%s: point %d axis %d %v, reference %v", version, i, k, p.GaussianAxes[k], want.GaussianAxes[k])
				}
			}
			for ch := range 3 {
				if !near(g.GaussianSH[4*i][ch], float32(dc[ch]), 1e-5) {
					t.Fatalf("%s: point %d DC %v, reference %v", version, i, g.GaussianSH[4*i], dc)
				}
				for k := range 3 {
					if got, want := g.GaussianSH[4*i+1+k][ch], float32(at(i, 14+3*k+ch)); got != want {
						t.Fatalf("%s: point %d SH coefficient %d channel %d %v, reference %v", version, i, k, ch, got, want)
					}
				}
			}
		}
	}
}

// A version 4 stream declared small whose frame expands to far more is
// refused without decoding the lot: the table's sizes bound the memory.
func TestStreamsDecodeNoMoreThanDeclared(t *testing.T) {
	one := []splat{{alpha: 255, scale: [3]uint8{160, 160, 160}, rotation: [4]float32{0, 0, 0, 1}}}
	data := encodeSPZ(t, 4, one)
	header := append([]byte{}, data[:16]...)
	// Replace the positions stream (declared 9 bytes) with 32 MiB of zeros
	// in a frame that does not state its size, and keep the others.
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		t.Fatal(err)
	}
	var huge bytes.Buffer
	w := enc
	w.Reset(&huge)
	if _, err := w.Write(make([]byte, 32<<20)); err != nil {
		t.Fatal(err)
	}
	w.Close()
	streams := [][]byte{nil, {255}, {128, 128, 128}, {160, 160, 160}, {0, 0, 0, 0x40}}
	out := append([]byte{}, header[:15]...)
	out = append(out, 5)
	out = binary.LittleEndian.AppendUint32(out, 32)
	out = append(out, make([]byte, 12)...)
	dec, _ := zstd.NewWriter(nil)
	var compressed [][]byte
	for i, s := range streams {
		c := huge.Bytes()
		if i > 0 {
			c = dec.EncodeAll(s, nil)
		}
		compressed = append(compressed, c)
		out = binary.LittleEndian.AppendUint64(out, uint64(len(c)))
		out = binary.LittleEndian.AppendUint64(out, [5]uint64{9, 1, 3, 3, 4}[i])
	}
	for _, c := range compressed {
		out = append(out, c...)
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err = Decode(out, nil)
	runtime.ReadMemStats(&after)
	if err == nil {
		t.Fatal("an overlong stream decoded")
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 4<<20 {
		t.Fatalf("decoding a 9-byte stream allocated %d MiB", grew>>20)
	}
}

func TestDecodersCluster(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "gaussian-ring.v4.spz"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := Decode(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if g := m.Objects[0].Geometry; g.Clusters == nil || len(g.Clusters.Order) != 576 {
		t.Fatalf("clusters %v", g.Clusters)
	}
	m, err = Decode(data, &nt3dmesh.Options{NoClusters: true})
	if err != nil {
		t.Fatal(err)
	}
	if g := m.Objects[0].Geometry; g.Clusters != nil || len(g.Points) != 576 {
		t.Fatalf("NoClusters left clusters %v", g.Clusters)
	}
}

// SHPaletteBuild gives an SPZ a palette that expands to the coefficients it
// decodes to without one; the default builds none.
func TestDecodeBuildsPalette(t *testing.T) {
	for _, version := range []string{"v2", "v3", "v4"} {
		data, err := os.ReadFile(filepath.Join("testdata", "gaussian-ring."+version+".spz"))
		if err != nil {
			t.Fatal(err)
		}
		m, err := Decode(data, nil)
		if err != nil {
			t.Fatal(err)
		}
		plain := m.Objects[0].Geometry
		if plain.GaussianSHPalette != nil || len(plain.GaussianSH) != 4*576 {
			t.Fatalf("%s: default palette %v, %d SH", version, plain.GaussianSHPalette, len(plain.GaussianSH))
		}
		m, err = Decode(data, &nt3dmesh.Options{SHPalette: nt3dmesh.SHPaletteBuild})
		if err != nil {
			t.Fatal(err)
		}
		g := m.Objects[0].Geometry
		if g.GaussianSHPalette == nil || g.GaussianSHPalette.Degree != 1 || g.GaussianSHPalette.Count() > len(g.Points) || len(g.GaussianSHLabels) != 576 || len(g.GaussianSH) != 576 {
			t.Fatalf("%s: palette %v, %d labels, %d SH", version, g.GaussianSHPalette, len(g.GaussianSHLabels), len(g.GaussianSH))
		}
		if err := g.Validate(); err != nil {
			t.Fatalf("%s: %v", version, err)
		}
		nt3d.ExpandSHPalette(&g)
		if !reflect.DeepEqual(g.GaussianSH, plain.GaussianSH) {
			t.Fatalf("%s: the palette expands to different coefficients", version)
		}
	}
}

// SPZ always orients; its clusters must follow the turned points.
func TestClustersFollowOrientation(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "gaussian-ring.v4.spz"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := Decode(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	g := m.Objects[0].Geometry
	if g.Clusters == nil {
		t.Fatal("no clusters")
	}
	for c, cl := range g.Clusters.Clusters {
		for _, i := range g.Clusters.Items(c) {
			p := g.Points[i].Position
			if p.X < cl.Bounds.Min.X || p.X > cl.Bounds.Max.X || p.Y < cl.Bounds.Min.Y || p.Y > cl.Bounds.Max.Y || p.Z < cl.Bounds.Min.Z || p.Z > cl.Bounds.Max.Z {
				t.Fatalf("cluster %d bounds %v miss point %d at %v", c, cl.Bounds, i, p)
			}
		}
	}
}
