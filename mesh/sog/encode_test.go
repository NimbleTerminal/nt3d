// Copyright (c) 2026 Neomantra Corp.

package nt3dsog

import (
	"bytes"
	"context"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
	"nimbleterminal.dev/nt3d/sh"
)

func TestUniform(t *testing.T) {
	u := rangeOf([]float64{-1, 3, 0.5})
	if u.lo != -1 || u.hi != 3 {
		t.Fatalf("range %+v", u)
	}
	for v, want := range map[float64]uint8{-1: 0, 3: 255, 1: 128, 10: 255, -10: 0} {
		if got := u.index(v); got != want {
			t.Errorf("index(%g) = %d, want %d", v, got, want)
		}
	}
	e := u.entries()
	if len(e) != 256 || e[0] != -1 || e[255] != 3 {
		t.Fatalf("entries: %d, first %g, last %g", len(e), e[0], e[255])
	}
	if got := quantize16(1, -1, 3); got != 32768 {
		t.Errorf("quantize16(1) = %d", got)
	}
	if got := quantize16(3, -1, 3); got != 65535 {
		t.Errorf("quantize16(3) = %d", got)
	}
}

func TestUniformDegenerate(t *testing.T) {
	u := rangeOf([]float64{2, 2, 2})
	if u.index(2) != 0 {
		t.Errorf("index(2) = %d", u.index(2))
	}
	for i, v := range u.entries() {
		if v != 2 {
			t.Fatalf("entry %d is %g", i, v)
		}
	}
	if got := quantize16(2, 2, 2); got != 0 {
		t.Errorf("quantize16 = %d", got)
	}
	if rangeOf(nil) != (uniform{}) {
		t.Errorf("rangeOf(nil) = %+v", rangeOf(nil))
	}
}

// quaternions are TestSplatQuaternion's inputs: random unit quaternions,
// the four axes, and one with w = 0.
func quaternions() [][4]float64 {
	r := rand.New(rand.NewPCG(1, 2))
	qs := [][4]float64{{1, 0, 0, 0}, {0, 1, 0, 0}, {0, 0, 1, 0}, {0, 0, 0, 1}, {0, 1 / math.Sqrt2, 1 / math.Sqrt2, 0}}
	for range 200 {
		q := [4]float64{r.NormFloat64(), r.NormFloat64(), r.NormFloat64(), r.NormFloat64()}
		n := math.Sqrt(q[0]*q[0] + q[1]*q[1] + q[2]*q[2] + q[3]*q[3])
		for k := range q {
			q[k] /= n
		}
		qs = append(qs, q)
	}
	return qs
}

func axesOf(t *testing.T, q [4]float64, logScales [3]float64) [3]nt3dmath.Vec3 {
	t.Helper()
	p, err := nt3dmesh.GaussianPoint(nt3dmath.Vec3{}, q, logScales, [3]float64{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	return p.GaussianAxes
}

func TestSplatQuaternion(t *testing.T) {
	logScales := [3]float64{-3, 0, 2}
	for _, q := range quaternions() {
		axes := axesOf(t, q, logScales)
		got, scales := splat(axes)
		for k := range 3 {
			if math.Abs(scales[k]-logScales[k]) > 1e-4 {
				t.Fatalf("%v: log scale %d is %g, want %g", q, k, scales[k], logScales[k])
			}
		}
		back := axesOf(t, got, scales)
		for k := range 3 {
			d := back[k].Sub(axes[k])
			if math.Abs(float64(d.X)) > 1e-4 || math.Abs(float64(d.Y)) > 1e-4 || math.Abs(float64(d.Z)) > 1e-4 {
				t.Fatalf("%v: axis %d came back as %v, want %v (quaternion %v)", q, k, back[k], axes[k], got)
			}
		}
	}
}

// unpackQuat decodes SOG's packed quaternion as unpack does.
func unpackQuat(b [4]uint8) [4]float64 {
	a := (float64(b[0])/255 - .5) * math.Sqrt2
	c := (float64(b[1])/255 - .5) * math.Sqrt2
	d := (float64(b[2])/255 - .5) * math.Sqrt2
	e := math.Sqrt(max(0, 1-(a*a+c*c+d*d)))
	switch b[3] - 252 {
	case 0:
		return [4]float64{e, a, c, d}
	case 1:
		return [4]float64{a, e, c, d}
	case 2:
		return [4]float64{a, c, e, d}
	}
	return [4]float64{a, c, d, e}
}

func TestPackQuat(t *testing.T) {
	b := packQuat([4]float64{1, 0, 0, 0})
	for k := range 3 {
		if b[k] < 127 || b[k] > 129 {
			t.Fatalf("identity packs to %v", b)
		}
	}
	if b[3] != 252 {
		t.Fatalf("identity packs to %v", b)
	}
	if b := packQuat([4]float64{0, 0, 0, 1}); b[3] != 255 {
		t.Fatalf("z packs to %v", b)
	}
	if b := packQuat([4]float64{0.5, -0.5, 0.5, 0.5}); b[3] != 252 {
		t.Fatalf("tie packs to %v, want the first largest dropped", b)
	}
	for _, q := range quaternions() {
		got := unpackQuat(packQuat(q))
		sign := 1.0
		if got[0]*q[0]+got[1]*q[1]+got[2]*q[2]+got[3]*q[3] < 0 {
			sign = -1
		}
		for k := range 4 {
			if math.Abs(got[k]-sign*q[k]) > 0.01 {
				t.Fatalf("%v packs to %v and back to %v", q, packQuat(q), got)
			}
		}
	}
}

// paletted decodes the demos' ring PLY and builds a 64-centroid palette on it.
func paletted(t *testing.T) nt3d.Geometry {
	t.Helper()
	g := ringPLY(t, nil)
	if _, err := nt3dmesh.BuildSHPalette(context.Background(), &g, nt3dmesh.PaletteOptions{Centroids: 64}); err != nil {
		t.Fatal(err)
	}
	if g.GaussianSHPalette.Count() != 64 {
		t.Fatalf("%d centroids", g.GaussianSHPalette.Count())
	}
	return g
}

// step is the largest dequantization error a uniform codebook over values allows.
func step(values []float64) float64 {
	u := rangeOf(values)
	return (u.hi-u.lo)/255/2 + 1e-6
}

func channels(coefficients []nt3dsh.RGB) []float64 {
	var out []float64
	for _, c := range coefficients {
		out = append(out, float64(c[0]), float64(c[1]), float64(c[2]))
	}
	return out
}

func encode(t *testing.T, objects []nt3dmesh.Object) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := Encode(&buf, objects); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decodeOne(t *testing.T, bundle []byte, o *nt3dmesh.Options) nt3d.Geometry {
	t.Helper()
	m, err := Decode(bundle, o)
	if err != nil {
		t.Fatal(err)
	}
	if m.Format != SOG || len(m.Objects) != 1 {
		t.Fatalf("format %v, %d objects", m.Format, len(m.Objects))
	}
	g := m.Objects[0].Geometry
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	return g
}

// assertPoints checks got against want splat by splat, in order, within the
// quantization the format applies.
func assertPoints(t *testing.T, got, want []nt3d.Point) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d points, want %d", len(got), len(want))
	}
	lo, hi := want[0].Position, want[0].Position
	for _, p := range want {
		lo = nt3dmath.Vec3{X: min(lo.X, p.Position.X), Y: min(lo.Y, p.Position.Y), Z: min(lo.Z, p.Position.Z)}
		hi = nt3dmath.Vec3{X: max(hi.X, p.Position.X), Y: max(hi.Y, p.Position.Y), Z: max(hi.Z, p.Position.Z)}
	}
	tolerance := [3]float64{float64(hi.X-lo.X)/4096 + 1e-6, float64(hi.Y-lo.Y)/4096 + 1e-6, float64(hi.Z-lo.Z)/4096 + 1e-6}
	for i, p := range got {
		q := want[i]
		d := p.Position.Sub(q.Position)
		for k, v := range [3]float32{d.X, d.Y, d.Z} {
			if math.Abs(float64(v)) > tolerance[k] {
				t.Fatalf("splat %d at %v, want %v: axis %d off by %g, limit %g", i, p.Position, q.Position, k, v, tolerance[k])
			}
		}
		if !near(p.Color.A, q.Color.A, 1) || !near(p.Color.R, q.Color.R, 3) || !near(p.Color.G, q.Color.G, 3) || !near(p.Color.B, q.Color.B, 3) {
			t.Fatalf("splat %d color %v, want %v", i, p.Color, q.Color)
		}
		for k := range 3 {
			assertAxis(t, i, k, p.GaussianAxes[k], q.GaussianAxes[k], .06, 1.5*math.Pi/180)
		}
	}
}

func assertSH(t *testing.T, what string, got, want []nt3dsh.RGB, tolerance float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d coefficients, want %d", what, len(got), len(want))
	}
	for i := range got {
		for c := range 3 {
			if math.Abs(float64(got[i][c]-want[i][c])) > tolerance {
				t.Fatalf("%s: coefficient %d channel %d is %g, want %g within %g", what, i, c, got[i][c], want[i][c], tolerance)
			}
		}
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	g := paletted(t)
	bundle := encode(t, []nt3dmesh.Object{{Geometry: g}})
	got := decodeOne(t, bundle, nil)
	if len(got.Points) != 576 {
		t.Fatalf("%d points", len(got.Points))
	}
	p, q := got.GaussianSHPalette, g.GaussianSHPalette
	if p == nil || p.Degree != 1 || p.Count() != 64 || p.Count() != q.Count() {
		t.Fatalf("palette %+v", p)
	}
	if !slices.Equal(got.GaussianSHLabels, g.GaussianSHLabels) {
		t.Fatal("labels differ")
	}
	bands := step(channels(q.Coefficients))
	assertSH(t, "palette", p.Coefficients, q.Coefficients, bands)
	dc := step(channels(g.GaussianSH))
	assertSH(t, "DC", got.GaussianSH, g.GaussianSH, dc)
	assertPoints(t, got.Points, g.Points)

	expanded := decodeOne(t, bundle, &nt3dmesh.Options{SHPalette: nt3dmesh.SHPaletteExpand})
	want := g
	nt3d.ExpandSHPalette(&want)
	if len(expanded.GaussianSH) != 4*576 {
		t.Fatalf("%d expanded SH", len(expanded.GaussianSH))
	}
	assertSH(t, "expanded", expanded.GaussianSH, want.GaussianSH, max(bands, dc))
}

func TestEncodePieces(t *testing.T) {
	g := paletted(t)
	split := func(from, to int) nt3dmesh.Object {
		piece := g
		piece.Points = g.Points[from:to]
		piece.GaussianSH = g.GaussianSH[from:to]
		piece.GaussianSHLabels = g.GaussianSHLabels[from:to]
		piece.Bounds = nt3d.GeometryBounds(piece)
		return nt3dmesh.Object{Geometry: piece}
	}
	got := decodeOne(t, encode(t, []nt3dmesh.Object{split(0, 300), split(300, 576)}), nil)
	if len(got.Points) != 576 || !slices.Equal(got.GaussianSHLabels, g.GaussianSHLabels) {
		t.Fatalf("%d points, labels equal %v", len(got.Points), slices.Equal(got.GaussianSHLabels, g.GaussianSHLabels))
	}
	assertPoints(t, got.Points, g.Points)
}

func TestEncodeDegreeZero(t *testing.T) {
	g := ringPLY(t, nil)
	dc := make([]nt3dsh.RGB, len(g.Points))
	for i := range dc {
		dc[i] = g.GaussianSH[i*4]
	}
	for _, sh := range [][]nt3dsh.RGB{nil, dc} {
		h := g
		h.GaussianSH = sh
		got := decodeOne(t, encode(t, []nt3dmesh.Object{{Geometry: h}}), nil)
		if got.GaussianSHPalette != nil || len(got.GaussianSH) != 576 {
			t.Fatalf("palette %v, %d SH", got.GaussianSHPalette, len(got.GaussianSH))
		}
		assertPoints(t, got.Points, g.Points)
		for i, c := range got.GaussianSH {
			for k := range 3 {
				want := g.Points[i].Color
				channel := uint8(math.Round(255 * max(0, min(1, .5+.28209479177387814*float64(c[k])))))
				if !near(channel, [3]uint8{want.R, want.G, want.B}[k], 1) {
					t.Fatalf("splat %d channel %d: DC %g gives %d, color %v", i, k, c[k], channel, want)
				}
			}
		}
		if sh != nil {
			assertSH(t, "DC", got.GaussianSH, dc, step(channels(dc)))
		}
	}
}

func TestEncodeDegenerateRanges(t *testing.T) {
	g := nt3d.Geometry{PointStyle: nt3d.PointStyle{Shape: nt3d.GaussianPoint, Units: nt3d.WorldUnits, Blend: nt3d.AlphaBlend}}
	for range 3 {
		p, err := nt3dmesh.GaussianPoint(nt3dmath.Vec3{X: 1, Y: -2, Z: 3}, [4]float64{1, 0, 0, 0}, [3]float64{}, [3]float64{0.2, 0.2, 0.2}, 1)
		if err != nil {
			t.Fatal(err)
		}
		g.Points = append(g.Points, p)
	}
	g.Bounds = nt3d.GeometryBounds(g)
	bundle := encode(t, []nt3dmesh.Object{{Geometry: g}})
	meta := string(readEntry(t, bundle, "meta.json"))
	if strings.Contains(meta, "NaN") || strings.Contains(meta, "null") {
		t.Fatalf("meta.json: %s", meta)
	}
	got := decodeOne(t, bundle, nil)
	if len(got.Points) != 3 {
		t.Fatalf("%d points", len(got.Points))
	}
	for i, p := range got.Points {
		d := p.Position.Sub(nt3dmath.Vec3{X: 1, Y: -2, Z: 3})
		if math.Abs(float64(d.X)) > 1e-3 || math.Abs(float64(d.Y)) > 1e-3 || math.Abs(float64(d.Z)) > 1e-3 {
			t.Fatalf("splat %d at %v", i, p.Position)
		}
		for k := range 3 {
			// Rotations are 8-bit in the format: within 1.5°, as the ring tests allow.
			assertAxis(t, i, k, p.GaussianAxes[k], g.Points[i].GaussianAxes[k], 1e-3, 1.5*math.Pi/180)
		}
	}
}

type countWriter struct{ n int }

func (c *countWriter) Write(p []byte) (int, error) { c.n += len(p); return len(p), nil }

func TestEncodeRejects(t *testing.T) {
	g := paletted(t)
	plain := ringPLY(t, nil)
	other := g
	other.GaussianSHPalette = &nt3d.SHPalette{Degree: g.GaussianSHPalette.Degree, Coefficients: slices.Clone(g.GaussianSHPalette.Coefficients)}
	other.GaussianSHPalette.Coefficients[0][0] += 1
	short := g
	short.GaussianSHLabels = g.GaussianSHLabels[:575]
	past := g
	past.GaussianSHLabels = slices.Clone(g.GaussianSHLabels)
	past.GaussianSHLabels[0] = uint16(g.GaussianSHPalette.Count())
	cases := map[string][]nt3dmesh.Object{
		"nil":            nil,
		"not Gaussian":   {{Geometry: nt3d.Geometry{Points: g.Points}}},
		"per-splat SH":   {{Geometry: plain}},
		"other palette":  {{Geometry: g}, {Geometry: other}},
		"short labels":   {{Geometry: short}},
		"label past end": {{Geometry: past}},
	}
	for name, objects := range cases {
		w := &countWriter{}
		if err := Encode(w, objects); err == nil {
			t.Errorf("%s: no error", name)
		} else if w.n != 0 {
			t.Errorf("%s: wrote %d bytes before failing: %v", name, w.n, err)
		}
	}
}
