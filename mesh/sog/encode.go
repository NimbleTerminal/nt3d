// Copyright (c) 2026 Neomantra Corp.

package nt3dsog

import (
	"math"

	"nimbleterminal.dev/nt3d/math"
)

// uniform is a v2 codebook of 256 evenly spaced entries over [lo, hi]: the
// linear range SOG v1 wrote, in v2's form.
type uniform struct{ lo, hi float64 }

// rangeOf is the uniform codebook spanning values; none gives the zero value.
func rangeOf(values []float64) uniform {
	if len(values) == 0 {
		return uniform{}
	}
	u := uniform{values[0], values[0]}
	for _, v := range values[1:] {
		u.lo, u.hi = min(u.lo, v), max(u.hi, v)
	}
	return u
}

// fraction is where v lies in [lo, hi], clamped to [0, 1]; 0 for an empty range.
func fraction(v, lo, hi float64) float64 {
	if !(hi > lo) {
		return 0
	}
	return max(0, min(1, (v-lo)/(hi-lo)))
}

// index is the byte whose entry is nearest v.
func (u uniform) index(v float64) uint8 { return uint8(math.Round(255 * fraction(v, u.lo, u.hi))) }

// entries is the codebook: lo + (hi-lo)*i/255 for each byte i.
func (u uniform) entries() []float64 {
	e := make([]float64, 256)
	for i := range e {
		e[i] = u.lo + (u.hi-u.lo)*float64(i)/255
	}
	return e
}

// quantize16 is index at 16 bits over [lo, hi], as the means are stored.
func quantize16(v, lo, hi float64) uint16 { return uint16(math.Round(65535 * fraction(v, lo, hi))) }

// splat inverts nt3dmesh.GaussianPoint's axes: the wxyz quaternion whose
// largest component is positive, and the logarithms of the three scales
// (clamped to the [1e-20, 1e6] GaussianPoint accepts). The axes are the rows
// GaussianPoint builds, the transpose of the usual rotation matrix, read
// through the largest quaternion component so a rotation near 180° is as
// exact as one near 0.
func splat(axes [3]nt3dmath.Vec3) (q [4]float64, logScales [3]float64) {
	var a [3][3]float64
	for k, v := range axes {
		s := max(1e-20, min(1e6, math.Sqrt(float64(v.Dot(v)))))
		logScales[k] = math.Log(s)
		a[k] = [3]float64{float64(v.X) / s, float64(v.Y) / s, float64(v.Z) / s}
	}
	tr := a[0][0] + a[1][1] + a[2][2]
	squares := [4]float64{(1 + tr) / 4, (1 + 2*a[0][0] - tr) / 4, (1 + 2*a[1][1] - tr) / 4, (1 + 2*a[2][2] - tr) / 4}
	largest := 0
	for k, s := range squares {
		if s > squares[largest] {
			largest = k
		}
	}
	c := math.Sqrt(max(0, squares[largest]))
	if c == 0 {
		return [4]float64{1, 0, 0, 0}, logScales
	}
	switch largest {
	case 0:
		q = [4]float64{c, (a[1][2] - a[2][1]) / (4 * c), (a[2][0] - a[0][2]) / (4 * c), (a[0][1] - a[1][0]) / (4 * c)}
	case 1:
		q = [4]float64{(a[1][2] - a[2][1]) / (4 * c), c, (a[0][1] + a[1][0]) / (4 * c), (a[0][2] + a[2][0]) / (4 * c)}
	case 2:
		q = [4]float64{(a[2][0] - a[0][2]) / (4 * c), (a[0][1] + a[1][0]) / (4 * c), c, (a[1][2] + a[2][1]) / (4 * c)}
	default:
		q = [4]float64{(a[0][1] - a[1][0]) / (4 * c), (a[0][2] + a[2][0]) / (4 * c), (a[1][2] + a[2][1]) / (4 * c), c}
	}
	n := math.Sqrt(q[0]*q[0] + q[1]*q[1] + q[2]*q[2] + q[3]*q[3])
	for k := range q {
		q[k] /= n
	}
	return q, logScales
}

// packQuat is SOG's smallest-three packing of a unit quaternion: the three
// components other than the largest (the first of equals), negated so the
// largest is positive, each round((v/√2+0.5)*255), then 252 plus the index
// of the dropped component, as unpack reads them.
func packQuat(q [4]float64) [4]uint8 {
	largest := 0
	for k, v := range q {
		if math.Abs(v) > math.Abs(q[largest]) {
			largest = k
		}
	}
	if q[largest] < 0 {
		for k := range q {
			q[k] = -q[k]
		}
	}
	var b [4]uint8
	n := 0
	for k, v := range q {
		if k == largest {
			continue
		}
		b[n] = uint8(math.Round(255 * max(0, min(1, v/math.Sqrt2+0.5))))
		n++
	}
	b[3] = uint8(252 + largest)
	return b
}
