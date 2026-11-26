// Copyright (c) 2026 Neomantra Corp.

package nt3dmesh

import (
	"errors"
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
)

// A byte budget starts at Options.MaxBytes (the default when zero), gives out
// what fits, and refuses the rest with an error that wraps ErrTooLarge and
// says what did not fit. A refusal takes nothing.
func TestByteBudgetRefusesWhatDoesNotFit(t *testing.T) {
	if got := (*Options)(nil).ByteBudget().Left(); got != DefaultMaxBytes {
		t.Fatalf("a nil Options' budget holds %d, want DefaultMaxBytes", got)
	}
	b := (&Options{MaxBytes: 10}).ByteBudget()
	if err := b.Take(6, "first"); err != nil {
		t.Fatal(err)
	}
	err := b.Take(5, "the second file")
	if !errors.Is(err, ErrTooLarge) || !strings.Contains(err.Error(), "the second file") {
		t.Fatalf("5 bytes of the 4 left: error %v, want ErrTooLarge naming what did not fit", err)
	}
	if b.Left() != 4 {
		t.Fatalf("%d left after a refusal, want 4", b.Left())
	}
	if err := b.Take(4, "third"); err != nil || b.Left() != 0 {
		t.Fatalf("taking exactly what is left: error %v, %d left", err, b.Left())
	}
	if err := b.Take(-1, "negative"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("a negative size (an overflowed product): error %v, want ErrTooLarge", err)
	}
}

// A triangle budget starts at Options.MaxTriangles and refuses, with an error
// wrapping ErrTooManyTriangles, the faces that would pass it.
func TestTriangleBudgetRefusesFacesPastTheLimit(t *testing.T) {
	b := (&Options{MaxTriangles: 3}).TriangleBudget()
	if b.Limit() != 3 {
		t.Fatalf("limit %d, want 3", b.Limit())
	}
	if err := b.Take(2); err != nil {
		t.Fatal(err)
	}
	if err := b.Take(2); !errors.Is(err, ErrTooManyTriangles) {
		t.Fatalf("a fourth face under a limit of three: error %v", err)
	}
	if b.Used() != 2 {
		t.Fatalf("%d used after a refusal, want 2", b.Used())
	}
}

// Input over MaxBytes is refused with ErrTooLarge by both Gaussian PLY
// decoders, as every other decoder refuses it.
func TestGaussianPLYOverMaxBytesIsTooLarge(t *testing.T) {
	plain := gaussianPLYSeed()
	if _, err := DecodeGaussianPLY(plain, &Options{MaxBytes: int64(len(plain)) - 1}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Gaussian PLY one byte over MaxBytes: error %v, want ErrTooLarge", err)
	}
	if _, err := DecodeGaussianPLY(plain, &Options{MaxBytes: int64(len(plain))}); err != nil {
		t.Fatalf("Gaussian PLY of exactly MaxBytes: %v", err)
	}
	packed := encodeCompressedPLY(t, []csplat{{}}, false)
	if _, err := DecodeCompressedPLY(packed, &Options{MaxBytes: int64(len(packed)) - 1}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("compressed PLY one byte over MaxBytes: error %v, want ErrTooLarge", err)
	}
}

// GaussianModel finishes a decoded cloud the way every Gaussian decoder
// does: the SH palette mode is applied, the cloud is split into pieces that
// fit one geometry, each piece is clustered unless NoClusters is set, and a
// cloud that is not a valid geometry is an error.
func TestGaussianModelFinishesACloud(t *testing.T) {
	cloud := func(n int) nt3d.Geometry {
		g := nt3d.Geometry{PointStyle: nt3d.PointStyle{Shape: nt3d.GaussianPoint, Units: nt3d.WorldUnits, Blend: nt3d.AlphaBlend}}
		for i := range n {
			p, err := GaussianPoint(nt3dmath.Vec3{X: float32(i)}, [4]float64{1, 0, 0, 0}, [3]float64{}, [3]float64{}, 1)
			if err != nil {
				t.Fatal(err)
			}
			g.Points = append(g.Points, p)
		}
		g.Bounds = nt3d.GeometryBounds(g)
		return g
	}
	m, err := GaussianModel(PLY, cloud(4), nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Format != PLY || len(m.Objects) != 1 || m.Objects[0].Geometry.Clusters == nil {
		t.Fatalf("format %q, %d objects, clusters %v: want one clustered PLY object", m.Format, len(m.Objects), m.Objects[0].Geometry.Clusters)
	}
	m, err = GaussianModel(PLY, cloud(4), &Options{NoClusters: true})
	if err != nil {
		t.Fatal(err)
	}
	if m.Objects[0].Geometry.Clusters != nil {
		t.Fatal("NoClusters left clusters on the cloud")
	}

	defer func(f func(int) int) { maxGaussians = f }(maxGaussians)
	maxGaussians = func(int) int { return 3 }
	m, err = GaussianModel(PLY, cloud(7), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Objects) != 3 {
		t.Fatalf("7 splats at 3 a piece: %d pieces, want 3", len(m.Objects))
	}

	bad := cloud(2)
	bad.GaussianSH = bad.GaussianSH[:0]
	bad.Points[0].Radius = -1
	if _, err := GaussianModel(PLY, bad, nil); err == nil {
		t.Fatal("a cloud that is not a valid geometry was accepted")
	}
}
