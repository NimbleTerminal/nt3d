// Copyright (c) 2026 Neomantra Corp.

package nt3dsog

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
	"nimbleterminal.dev/nt3d/sh"
)

func read(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func ringPLY(t *testing.T, o *nt3dmesh.Options) nt3d.Geometry {
	t.Helper()
	m, err := nt3dmesh.DecodePLY(read(t, filepath.Join("..", "..", "examples", "demos", "internal", "demo", "models", "gaussian-ring.ply")), o)
	if err != nil {
		t.Fatal(err)
	}
	return m.Objects[0].Geometry
}

// decodeRing decodes the v2 bundle fixture and checks its shape: one object
// of 576 degree-1 splats, their SH turned only by a Y-up orientation.
func decodeRing(t *testing.T, o *nt3dmesh.Options) nt3d.Geometry {
	t.Helper()
	m, err := Decode(read(t, filepath.Join("testdata", "gaussian-ring.sog")), o)
	if err != nil {
		t.Fatal(err)
	}
	if m.Format != SOG || len(m.Objects) != 1 {
		t.Fatalf("format %v, %d objects", m.Format, len(m.Objects))
	}
	g := m.Objects[0].Geometry
	if len(g.Points) != 576 || len(g.GaussianSH) != 4*576 || (o == nil) != (g.GaussianSHTransform == nt3dmath.Mat4{}) {
		t.Fatalf("%d points, %d SH, SH transform %v", len(g.Points), len(g.GaussianSH), g.GaussianSHTransform)
	}
	if g.PointStyle != (nt3d.PointStyle{Shape: nt3d.GaussianPoint, Units: nt3d.WorldUnits, Blend: nt3d.AlphaBlend}) {
		t.Fatalf("style %v", g.PointStyle)
	}
	return g
}

// nearest returns, for each of got's points, the index of the closest of
// want's, failing unless the match is one to one.
func nearest(t *testing.T, got, want []nt3d.Point) []int {
	t.Helper()
	index := make([]int, len(got))
	seen := make([]bool, len(want))
	for i, p := range got {
		best := math.Inf(1)
		for j, q := range want {
			d := p.Position.Sub(q.Position)
			if s := float64(d.Dot(d)); s < best {
				best, index[i] = s, j
			}
		}
		if seen[index[i]] {
			t.Fatalf("splat %d and another both match %d", i, index[i])
		}
		seen[index[i]] = true
	}
	return index
}

func length(v nt3dmath.Vec3) float64 {
	x, y, z := float64(v.X), float64(v.Y), float64(v.Z)
	return math.Sqrt(x*x + y*y + z*z)
}

// assertAxis checks got against want within a relative length and an angle
// in radians.
func assertAxis(t *testing.T, i, k int, got, want nt3dmath.Vec3, relative, angle float64) {
	t.Helper()
	lg, lw := length(got), length(want)
	if math.Abs(lg-lw) > relative*lw {
		t.Fatalf("splat %d axis %d %v, want %v: length %g vs %g", i, k, got, want, lg, lw)
	}
	dot := float64(got.X)*float64(want.X) + float64(got.Y)*float64(want.Y) + float64(got.Z)*float64(want.Z)
	if a := math.Acos(max(-1, min(1, dot/(lg*lw)))); a > angle {
		t.Fatalf("splat %d axis %d %v, want %v: angle %g° exceeds %g°", i, k, got, want, a*180/math.Pi, angle*180/math.Pi)
	}
}

func near(a, b uint8, tolerance int) bool {
	return int(a)-int(b) <= tolerance && int(b)-int(a) <= tolerance
}

// The bundle decodes to what the reference tool decodes from it: both sides
// read the same codebooks, so only float32 rounding differs.
func TestBundleMatchesReference(t *testing.T) {
	ref, err := nt3dmesh.DecodePLY(read(t, filepath.Join("testdata", "gaussian-ring.sog.ref.ply")), nil)
	if err != nil {
		t.Fatal(err)
	}
	g, w := decodeRing(t, nil), ref.Objects[0].Geometry
	if len(w.Points) != 576 || len(w.GaussianSH) != 4*576 {
		t.Fatalf("reference: %d points, %d SH", len(w.Points), len(w.GaussianSH))
	}
	match := nearest(t, g.Points, w.Points)
	for i, p := range g.Points {
		j := match[i]
		q := w.Points[j]
		d := p.Position.Sub(q.Position)
		if length(d) > 1e-5*(1+length(q.Position)) {
			t.Fatalf("splat %d at %v, reference %d at %v", i, p.Position, j, q.Position)
		}
		if p.Color.R != q.Color.R || p.Color.G != q.Color.G || p.Color.B != q.Color.B || !near(p.Color.A, q.Color.A, 1) {
			t.Fatalf("splat %d color %v, reference %d %v", i, p.Color, j, q.Color)
		}
		for k := range 3 {
			assertAxis(t, i, k, p.GaussianAxes[k], q.GaussianAxes[k], 1e-4, .01*math.Pi/180)
		}
		for k := range 4 {
			for ch := range 3 {
				if a, b := g.GaussianSH[4*i+k][ch], w.GaussianSH[4*j+k][ch]; math.Abs(float64(a-b)) > 1e-5 {
					t.Fatalf("splat %d SH %d channel %d: %v, reference %d %v", i, k, ch, a, j, b)
				}
			}
		}
	}
}

// The bundle decodes to the ring it was made from, within quantization, in
// either orientation. The SH palette is lossy, so SH is not compared.
func TestBundleMatchesSourcePLY(t *testing.T) {
	for _, o := range []*nt3dmesh.Options{nil, {Up: nt3dmesh.YUp}} {
		g, w := decodeRing(t, o), ringPLY(t, o)
		if g.GaussianSHTransform != w.GaussianSHTransform {
			t.Fatalf("%+v: SH transform %v, source %v", o, g.GaussianSHTransform, w.GaussianSHTransform)
		}
		lo, hi := w.Points[0].Position, w.Points[0].Position
		for _, p := range w.Points {
			lo = nt3dmath.Vec3{X: min(lo.X, p.Position.X), Y: min(lo.Y, p.Position.Y), Z: min(lo.Z, p.Position.Z)}
			hi = nt3dmath.Vec3{X: max(hi.X, p.Position.X), Y: max(hi.Y, p.Position.Y), Z: max(hi.Z, p.Position.Z)}
		}
		tolerance := [3]float64{float64(hi.X-lo.X) / 4096, float64(hi.Y-lo.Y) / 4096, float64(hi.Z-lo.Z) / 4096}
		match := nearest(t, g.Points, w.Points)
		for i, p := range g.Points {
			j := match[i]
			q := w.Points[j]
			d := p.Position.Sub(q.Position)
			for k, v := range [3]float32{d.X, d.Y, d.Z} {
				if math.Abs(float64(v)) > tolerance[k] {
					t.Fatalf("%+v: splat %d at %v, source %d at %v: axis %d off by %g, limit %g", o, i, p.Position, j, q.Position, k, v, tolerance[k])
				}
			}
			if !near(p.Color.A, q.Color.A, 1) || !near(p.Color.R, q.Color.R, 3) || !near(p.Color.G, q.Color.G, 3) || !near(p.Color.B, q.Color.B, 3) {
				t.Fatalf("%+v: splat %d color %v, source %d %v", o, i, p.Color, j, q.Color)
			}
			for k := range 3 {
				assertAxis(t, i, k, p.GaussianAxes[k], q.GaussianAxes[k], .06, 1.5*math.Pi/180)
			}
		}
	}
}

// A degree-3 SOG export, when NT3D_SOG_SAMPLE names one, decodes to valid
// pieces with 16 SH coefficients per splat.
func TestSample(t *testing.T) {
	name := os.Getenv("NT3D_SOG_SAMPLE")
	if name == "" {
		t.Skip("NT3D_SOG_SAMPLE not set")
	}
	// ReadFile takes a .sog bundle or a folder's meta.json alike.
	m, err := nt3dmesh.ReadFile(name, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Format != SOG || len(m.Objects) == 0 {
		t.Fatalf("format %q, %d objects", m.Format, len(m.Objects))
	}
	points, sh := 0, 0
	for _, o := range m.Objects {
		g := o.Geometry
		if err := g.Validate(); err != nil {
			t.Fatal(err)
		}
		points += len(g.Points)
		sh += len(g.GaussianSH)
	}
	per := sh / max(1, points)
	if sh != per*points || nt3dsh.Degree(per) < 0 {
		t.Fatalf("%d SH for %d points", sh, points)
	}
	t.Logf("%d objects, %d splats, %d SH coefficients each", len(m.Objects), points, per)
}

// unzip writes the bundle's files into a new directory, as an unbundled
// SOG export lays them out, and returns the directory.
func unzip(t *testing.T, bundle []byte) string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f.Name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// zipOf builds a zip of the named files, in order.
func zipOf(t *testing.T, files map[string]string, order ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range order {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// An unbundled meta.json reads its images from beside it, through ReadFile,
// to the same points the bundle decodes to; without a way to open them it
// fails, and an opener's error is the decoder's.
func TestUnbundled(t *testing.T) {
	bundle := read(t, filepath.Join("testdata", "gaussian-ring.sog"))
	dir := unzip(t, bundle)
	want, err := Decode(bundle, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := nt3dmesh.ReadFile(filepath.Join(dir, "meta.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format != SOG || len(got.Objects) != 1 {
		t.Fatalf("format %v, %d objects", got.Format, len(got.Objects))
	}
	if !reflect.DeepEqual(got.Objects[0].Geometry.Points, want.Objects[0].Geometry.Points) {
		t.Fatal("unbundled points differ from the bundle's")
	}

	meta := read(t, filepath.Join(dir, "meta.json"))
	if _, err := Decode(meta, nil); err == nil {
		t.Fatal("meta.json decoded without a way to open its images")
	}
	broken := errors.New("quats.webp is unreadable")
	_, err = Decode(meta, &nt3dmesh.Options{Open: func(name string) ([]byte, error) {
		if name == "quats.webp" {
			return nil, broken
		}
		return os.ReadFile(filepath.Join(dir, name))
	}})
	if !errors.Is(err, broken) {
		t.Fatalf("opener's error not returned: %v", err)
	}
}

func TestRegistration(t *testing.T) {
	bundle := read(t, filepath.Join("testdata", "gaussian-ring.sog"))
	meta := read(t, filepath.Join(unzip(t, bundle), "meta.json"))
	for _, c := range []struct {
		name string
		data []byte
	}{{"a.sog", nil}, {"a.bin", bundle}, {"a.json", meta}, {"a.bin", meta}} {
		if f, _ := nt3dmesh.FormatOf(c.name, c.data); f != SOG {
			t.Fatalf("%s with %d bytes: %q", c.name, len(c.data), f)
		}
	}
	v1 := read(t, filepath.Join("testdata", "gaussian-ring.v1.sog"))
	if f, _ := nt3dmesh.FormatOf("a.bin", v1); f != SOG {
		t.Fatalf("version 1 bundle: %q", f)
	}
	other := zipOf(t, map[string]string{"3D/3dmodel.model": "<model/>"}, "3D/3dmodel.model")
	if f, _ := nt3dmesh.FormatOf("a.bin", other); f != nt3dmesh.ThreeMF {
		t.Fatalf("a zip without meta.json: %q", f)
	}
	// A zip that merely names meta.json in a directory is not a bundle.
	nested := zipOf(t, map[string]string{"x/meta.json": "{}"}, "x/meta.json")
	if f, _ := nt3dmesh.FormatOf("a.bin", nested); f != nt3dmesh.ThreeMF {
		t.Fatalf("a zip with only x/meta.json: %q", f)
	}
	for _, data := range []string{"{}", `{"means": {}}`, `  {"quats": {}}`, `[{"means":1,"quats":2}]`} {
		if f, _ := nt3dmesh.FormatOf("a.json", []byte(data)); f == SOG {
			t.Fatalf("%q sniffed as SOG", data)
		}
	}
	if f, _ := nt3dmesh.FormatOf("a.json", append([]byte("\n\t "), meta...)); f != SOG {
		t.Fatalf("meta.json after whitespace: %q", f)
	}
	// Keys far from the start are not looked for.
	late := append([]byte("{"+strings.Repeat(" ", sniffWindow)), meta[1:]...)
	if sniff(late) {
		t.Fatal("keys past the sniff window sniffed as SOG")
	}
	if sniff([]byte("PK\x03\x04 truncated")) || sniff(nil) {
		t.Fatal("garbage sniffed as SOG")
	}
	m, err := nt3dmesh.Decode(bundle, SOG, nil)
	if err != nil || m.Format != SOG {
		t.Fatalf("Decode: %v", err)
	}
}

// The v1 bundle decodes to the v2 bundle's splats: v1 quantizes scales, SH0,
// and SH-N linearly over a range, where v2 uses codebooks, so those agree
// only within one range step; positions and rotations are encoded alike.
func TestV1MatchesV2(t *testing.T) {
	v1 := read(t, filepath.Join("testdata", "gaussian-ring.v1.sog"))
	var raw struct {
		Scales, Sh0 struct{ Mins, Maxs []float64 }
		ShN         struct{ Mins, Maxs float64 }
	}
	if err := json.Unmarshal(readEntry(t, v1, "meta.json"), &raw); err != nil {
		t.Fatal(err)
	}
	// A degenerate range (mins == maxs) has a zero step.
	var scaleStep, sh0Step [3]float64
	for k := range 3 {
		scaleStep[k] = math.Exp((raw.Scales.Maxs[k]-raw.Scales.Mins[k])/255) - 1
		sh0Step[k] = (raw.Sh0.Maxs[k] - raw.Sh0.Mins[k]) / 255
	}
	shNStep := (raw.ShN.Maxs - raw.ShN.Mins) / 255

	m, err := Decode(v1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Format != SOG || len(m.Objects) != 1 {
		t.Fatalf("format %v, %d objects", m.Format, len(m.Objects))
	}
	g, w := m.Objects[0].Geometry, decodeRing(t, nil)
	if len(g.Points) != 576 || len(g.GaussianSH) != 4*576 {
		t.Fatalf("%d points, %d SH", len(g.Points), len(g.GaussianSH))
	}
	match := nearest(t, g.Points, w.Points)
	for i, p := range g.Points {
		j := match[i]
		q := w.Points[j]
		if d := length(p.Position.Sub(q.Position)); d > 1e-6 {
			t.Fatalf("splat %d at %v, v2 %d at %v: %g apart", i, p.Position, j, q.Position, d)
		}
		if !near(p.Color.R, q.Color.R, 2) || !near(p.Color.G, q.Color.G, 2) || !near(p.Color.B, q.Color.B, 2) || !near(p.Color.A, q.Color.A, 1) {
			t.Fatalf("splat %d color %v, v2 %d %v", i, p.Color, j, q.Color)
		}
		for k := range 3 {
			assertAxis(t, i, k, p.GaussianAxes[k], q.GaussianAxes[k], scaleStep[k]+1e-4, .01*math.Pi/180)
		}
		for k := range 4 {
			for ch := range 3 {
				step := shNStep
				if k == 0 {
					step = sh0Step[ch]
				}
				if a, b := g.GaussianSH[4*i+k][ch], w.GaussianSH[4*j+k][ch]; math.Abs(float64(a-b)) > step+1e-5 {
					t.Fatalf("splat %d SH %d channel %d: %v, v2 %d %v, step %g", i, k, ch, a, j, b, step)
				}
			}
		}
	}
}

// readEntry returns the named file from a bundle.
func readEntry(t *testing.T, bundle []byte, name string) []byte {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	if err != nil {
		t.Fatal(err)
	}
	data, err := entry(z, name, &budget{left: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	return data
}
