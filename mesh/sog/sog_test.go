// Copyright (c) 2026 Neomantra Corp.

package nt3dsog

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
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
	m, err := Decode(read(t, name), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Objects) == 0 {
		t.Fatal("no objects")
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
	if sh != 16*points {
		t.Fatalf("%d SH for %d points", sh, points)
	}
}
