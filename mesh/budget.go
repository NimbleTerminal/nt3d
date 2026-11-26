// Copyright (c) 2026 Neomantra Corp.

package nt3dmesh

import (
	"errors"
	"fmt"
	"math"

	"nimbleterminal.dev/nt3d/math"
)

// ErrTooManyTriangles is wrapped by errors for models over Options.MaxTriangles.
var ErrTooManyTriangles = errors.New("nt3dmesh: too many triangles")

// ErrTooLarge is wrapped by errors for input, and for what it unpacks or
// decodes to, past Options.MaxBytes.
var ErrTooLarge = errors.New("nt3dmesh: model takes more bytes than MaxBytes allows")

// ErrTooComplex is wrapped by errors for models past a limit no option
// raises: more points or line ends than nt3d draws, more nodes or variants
// than a decoder walks, or a hierarchy too deep.
var ErrTooComplex = errors.New("nt3dmesh: model is too complex")

// ByteBudget is what a decode may still take of Options.MaxBytes. A decoder
// makes one with Options.ByteBudget and charges it for everything the input
// can make large: files it opens, data it decompresses, and pixels it
// decodes, at four bytes a pixel. A nil *ByteBudget has no limit.
type ByteBudget struct{ left int64 }

// ByteBudget is a budget of MaxBytes, or of DefaultMaxBytes when that is zero.
func (o *Options) ByteBudget() *ByteBudget { return &ByteBudget{left: o.maxBytes()} }

// Take charges n bytes, or refuses them with an error that wraps ErrTooLarge
// and names what did not fit. A refusal charges nothing. A negative n, as a
// size that overflowed is, is refused.
func (b *ByteBudget) Take(n int64, what string) error {
	if b == nil && n >= 0 {
		return nil
	}
	if n < 0 || n > b.left {
		return fmt.Errorf("%w: %s", ErrTooLarge, what)
	}
	b.left -= n
	return nil
}

// Left is how many bytes remain.
func (b *ByteBudget) Left() int64 {
	if b == nil {
		return math.MaxInt64
	}
	return b.left
}

// TriangleBudget counts a model's faces against Options.MaxTriangles.
type TriangleBudget struct{ used, limit int }

// TriangleBudget is a budget of MaxTriangles, or of DefaultMaxTriangles when
// that is zero.
func (o *Options) TriangleBudget() *TriangleBudget { return &TriangleBudget{limit: o.limit()} }

// Take counts n more faces, or refuses them with an error wrapping
// ErrTooManyTriangles. A refusal counts nothing.
func (b *TriangleBudget) Take(n int) error {
	if n < 0 || b.used+n > b.limit {
		return fmt.Errorf("%w: model exceeds %d", ErrTooManyTriangles, b.limit)
	}
	b.used += n
	return nil
}

// Used is how many faces have been counted.
func (b *TriangleBudget) Used() int { return b.used }

// Limit is the most faces the budget allows.
func (b *TriangleBudget) Limit() int { return b.limit }

// ValidCoordinate reports whether every value is finite and within
// MaxCoordinate, as decoders require of what they read.
func ValidCoordinate(v ...float32) bool {
	for _, x := range v {
		if !nt3dmath.IsFinite(float64(x)) || x > MaxCoordinate || x < -MaxCoordinate {
			return false
		}
	}
	return true
}
