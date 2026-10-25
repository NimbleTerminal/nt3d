// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"math"
	"math/rand"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func TestFloat16RoundTrip(t *testing.T) {
	for _, v := range []float32{0, 1, -1, 0.5, 65504, -9.8, 1e-3, 3.14159, 1e-6} {
		got := float16value(float16bits(v))
		tol := math.Abs(float64(v)) / 2048
		if math.Abs(float64(v)) < 0x1p-14 {
			tol = 0x1p-24
		}
		if math.Abs(float64(got)-float64(v)) > tol {
			t.Errorf("float16 round trip of %v = %v, error beyond %v", v, got, tol)
		}
	}
	for _, c := range []struct {
		v    float32
		bits uint16
	}{{0, 0}, {1, 0x3C00}, {-2, 0xC000}, {1e9, 0x7BFF}, {-1e9, 0xFBFF}} {
		if got := float16bits(c.v); got != c.bits {
			t.Errorf("float16bits(%v) = %#04x, want %#04x", c.v, got, c.bits)
		}
	}
}

func TestPackGaussianAxesRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	// The columns GaussianPoint builds from a unit quaternion.
	frame := func() [3]nt3dmath.Vec3 {
		w, x, y, z := rng.NormFloat64(), rng.NormFloat64(), rng.NormFloat64(), rng.NormFloat64()
		n := math.Sqrt(w*w + x*x + y*y + z*z)
		w, x, y, z = w/n, x/n, y/n, z/n
		cols := [3][3]float64{{1 - 2*(y*y+z*z), 2 * (x*y + w*z), 2 * (x*z - w*y)}, {2 * (x*y - w*z), 1 - 2*(x*x+z*z), 2 * (y*z + w*x)}, {2 * (x*z + w*y), 2 * (y*z - w*x), 1 - 2*(x*x+y*y)}}
		var axes [3]nt3dmath.Vec3
		for k, c := range cols {
			s := math.Exp(-6 + 8*rng.Float64())
			axes[k] = nt3dmath.Vec3{X: float32(c[0] * s), Y: float32(c[1] * s), Z: float32(c[2] * s)}
		}
		return axes
	}
	check := func(axes [3]nt3dmath.Vec3) {
		t.Helper()
		h := packGaussianAxes(axes)
		if h[9] != 0 || h[10] != 0 || h[11] != 0 {
			t.Fatalf("trailing halves = %v, want zeros", h[9:])
		}
		got := unpackGaussianAxes(h)
		for k := range axes {
			in := [3]float32{axes[k].X, axes[k].Y, axes[k].Z}
			out := [3]float32{got[k].X, got[k].Y, got[k].Z}
			for c := range in {
				tol := math.Max(math.Abs(float64(in[c]))/2048, 1e-6)
				if math.Abs(float64(out[c])-float64(in[c])) > tol {
					t.Fatalf("axis %d component %d: %v -> %v, error beyond %v", k, c, in[c], out[c], tol)
				}
			}
		}
	}
	for range 1000 {
		check(frame())
	}
	for range 300 {
		axes := frame()
		var m [3][3]float64
		for i := range m {
			for j := range m[i] {
				m[i][j] = rng.NormFloat64()
			}
		}
		for k, a := range axes {
			v := [3]float64{float64(a.X), float64(a.Y), float64(a.Z)}
			var r [3]float64
			for i := range r {
				r[i] = m[i][0]*v[0] + m[i][1]*v[1] + m[i][2]*v[2]
			}
			axes[k] = nt3dmath.Vec3{X: float32(r[0]), Y: float32(r[1]), Z: float32(r[2])}
		}
		check(axes)
	}
}

func TestSHByteRoundTrip(t *testing.T) {
	const dcLimit = 0.5 / 0.15
	for _, dc := range []float32{-3.3, -1, 0, 0.7, 1.77, 3.3} {
		want := math.Max(-dcLimit, math.Min(dcLimit, float64(dc)))
		got := float64(unpackSHDC(packSHDC(dc)))
		if math.Abs(got-want) > 1/(255*0.15)+1e-6 {
			t.Errorf("dc %v -> %v, want within one step of %v", dc, got, want)
		}
	}
	if b := packSHDC(0); b != 128 {
		t.Errorf("packSHDC(0) = %d, want 128", b)
	}
	for _, v := range []float32{-1.5, -1, -0.3, 0, 0.3, 0.99, 1, 1.5} {
		want := math.Max(-1, math.Min(1, float64(v)))
		got := float64(unpackSHBand(packSHBand(v)))
		if math.Abs(got-want) > 1.0/128+1e-6 {
			t.Errorf("band %v -> %v, want within 1/128 of %v", v, got, want)
		}
	}
	if b := packSHBand(0); b != 128 {
		t.Errorf("packSHBand(0) = %d, want 128", b)
	}
	if b := packSHBand(1); b != 255 {
		t.Errorf("packSHBand(1) = %d, want 255", b)
	}
	if b := packSHBand(-1); b != 0 {
		t.Errorf("packSHBand(-1) = %d, want 0", b)
	}
	for i := range 256 {
		b := uint8(i)
		if got, want := unpackSHDC(b), (float32(b)/255-0.5)/0.15; got != want {
			t.Errorf("unpackSHDC(%d) = %v, want SPZ %v", b, got, want)
		}
		if got, want := unpackSHBand(b), (float32(b)-128)/128; got != want {
			t.Errorf("unpackSHBand(%d) = %v, want SPZ %v", b, got, want)
		}
	}
}
