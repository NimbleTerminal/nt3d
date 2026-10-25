// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"math"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// float16bits converts v to IEEE 754 binary16, rounding to nearest even and
// producing denormals below 2^-14. Magnitudes past 65504 (infinities
// included) clamp to ±65504 so the GPU never sees a non-finite axis; NaN
// becomes 0x7E00.
func float16bits(v float32) uint16 {
	if v != v {
		return 0x7E00
	}
	bits := math.Float32bits(v)
	sign := uint16(bits>>16) & 0x8000
	if math.Abs(float64(v)) > 65504 {
		return sign | 0x7BFF
	}
	exp := int(bits>>23&0xFF) - 127
	mant := bits & 0x7FFFFF
	if exp < -25 {
		return sign
	}
	var h, rem, half uint32
	if exp < -14 {
		// Denormal: the 24-bit significand shifted down to units of 2^-24.
		m := mant | 0x800000
		shift := uint(-1 - exp)
		h, rem, half = m>>shift, m&(1<<shift-1), 1<<(shift-1)
	} else {
		h, rem, half = uint32(exp+15)<<10|mant>>13, mant&0x1FFF, 0x1000
	}
	if rem > half || rem == half && h&1 == 1 {
		h++ // a carry out of the mantissa correctly bumps the exponent
	}
	return sign | uint16(h)
}

// float16value converts an IEEE 754 binary16 to float32 exactly.
func float16value(h uint16) float32 {
	sign := float32(1)
	if h&0x8000 != 0 {
		sign = -1
	}
	exp, mant := int(h>>10&0x1F), float32(h&0x3FF)
	switch exp {
	case 0:
		return sign * mant * 0x1p-24
	case 31:
		if mant != 0 {
			return float32(math.NaN())
		}
		return float32(math.Inf(int(sign)))
	}
	return sign * (1 + mant/1024) * float32(math.Ldexp(1, exp-15))
}

// packGaussianAxes packs the three one-sigma axes of a splat as the compact
// GPU record holds them: nine halves in axis-major order (axis 0 x, y, z,
// then axis 1, then axis 2) and three zero halves of padding. Serialized
// little-endian the array is the order vs_gaussian reads, two halves per
// 32-bit word with the low half first.
func packGaussianAxes(axes [3]nt3dmath.Vec3) [12]uint16 {
	var h [12]uint16
	for k, a := range axes {
		h[3*k], h[3*k+1], h[3*k+2] = float16bits(a.X), float16bits(a.Y), float16bits(a.Z)
	}
	return h
}

// unpackGaussianAxes is the Go mirror of the axis decode in vs_gaussian.
func unpackGaussianAxes(h [12]uint16) [3]nt3dmath.Vec3 {
	var axes [3]nt3dmath.Vec3
	for k := range axes {
		axes[k] = nt3dmath.Vec3{X: float16value(h[3*k]), Y: float16value(h[3*k+1]), Z: float16value(h[3*k+2])}
	}
	return axes
}

// packSHDC quantizes a degree-0 spherical harmonic coefficient to the byte
// SPZ stores: round(255·clamp(0.5 + 0.15·dc, 0, 1)). It is the encoding
// behind the DC term gaussianSHColor reads.
func packSHDC(dc float32) uint8 {
	return uint8(math.Round(255 * max(0, min(1, 0.5+0.15*float64(dc)))))
}

// unpackSHDC is the SPZ decode of a DC byte, (b/255 − 0.5)/0.15, as in
// mesh/spz unpack and gaussianSHColor.
func unpackSHDC(b uint8) float32 {
	return (float32(b)/255 - 0.5) / 0.15
}

// packSHBand quantizes a degree 1 to 3 spherical harmonic coefficient to the
// byte SPZ stores: clamp(round(128·v + 128), 0, 255).
func packSHBand(v float32) uint8 {
	return uint8(max(0, min(255, math.Round(float64(v)*128+128))))
}

// unpackSHBand is the SPZ decode of a higher-band byte, (b − 128)/128, as in
// mesh/spz unpack and gaussianSHColor.
func unpackSHBand(b uint8) float32 {
	return (float32(b) - 128) / 128
}
