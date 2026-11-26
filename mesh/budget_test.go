// Copyright (c) 2026 Neomantra Corp.

package nt3dmesh

import (
	"errors"
	"strings"
	"testing"
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
