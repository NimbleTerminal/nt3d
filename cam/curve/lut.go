// Copyright (c) 2026 Neomantra Corp.

package nt3dcurve

import (
	"math"
	"sort"
)

// DefaultSamplesPerSpan is the default arc-length table subdivision count per cubic span.
const DefaultSamplesPerSpan = 32

// 5-point Gauss-Legendre quadrature nodes and weights on [-1, 1].
var (
	glNodes = [5]float64{
		-0.9061798459386640,
		-0.5384693101056831,
		0.0,
		0.5384693101056831,
		0.9061798459386640,
	}
	glWeights = [5]float64{
		0.2369268850561891,
		0.4786286704993665,
		0.5688888888888889,
		0.4786286704993665,
		0.2369268850561891,
	}
)

func spanArcLength(span CubicSpan, u0, u1 float64) float64 {
	if u1 <= u0 {
		return 0
	}
	half := 0.5 * (u1 - u0)
	mid := 0.5 * (u0 + u1)
	var sum float64
	for i := range 5 {
		v := span.Deriv(mid + half*glNodes[i])
		sum += glWeights[i] * v.Len()
	}
	return half * sum
}

// LUT is an immutable arc-length lookup table over a slice of CubicSpans.
// Every sample stores cumulative distance cumLen[k] at (spanIdx[k], uSample[k]).
// Adjacent entries k and k+1 either lie in the same span or mark a zero-length
// boundary step between span i at u=1 and span i+1 at u=0.
type LUT struct {
	spans       []CubicSpan
	cumLen      []float64
	spanIdx     []int32
	uSample     []float64
	totalLength float64
	closed      bool
}

// NewLUT precomputes a monotonic arc-length table across spans.
func NewLUT(spans []CubicSpan, samplesPerSpan int, closed bool) *LUT {
	if samplesPerSpan <= 0 {
		samplesPerSpan = DefaultSamplesPerSpan
	}
	if len(spans) == 0 {
		spans = []CubicSpan{{}}
	}
	capHint := len(spans)*(samplesPerSpan+1) + 1
	cumLen := make([]float64, 0, capHint)
	spanIdx := make([]int32, 0, capHint)
	uSample := make([]float64, 0, capHint)

	total := 0.0
	invSamples := 1.0 / float64(samplesPerSpan)
	for si, sp := range spans {
		cumLen = append(cumLen, total)
		spanIdx = append(spanIdx, int32(si))
		uSample = append(uSample, 0)
		uPrev := 0.0
		for step := 1; step <= samplesPerSpan; step++ {
			uNext := float64(step) * invSamples
			if step == samplesPerSpan {
				uNext = 1.0
			}
			total += spanArcLength(sp, uPrev, uNext)
			cumLen = append(cumLen, total)
			spanIdx = append(spanIdx, int32(si))
			uSample = append(uSample, uNext)
			uPrev = uNext
		}
	}
	return &LUT{
		spans:       spans,
		cumLen:      cumLen,
		spanIdx:     spanIdx,
		uSample:     uSample,
		totalLength: total,
		closed:      closed,
	}
}

// TotalLength returns the total arc length of the curve in world meters.
func (l *LUT) TotalLength() float64 { return l.totalLength }

// Closed reports whether the underlying curve is a closed loop.
func (l *LUT) Closed() bool { return l.closed }

// Spans returns the number of cubic spans in l.
func (l *LUT) Spans() int { return len(l.spans) }

// SampleCount returns the number of entries in the precomputed table.
func (l *LUT) SampleCount() int { return len(l.cumLen) }

// SampleAt returns the normalized arc-length s in [0, 1], position, and unit
// tangent at LUT table index idx (0 <= idx < SampleCount).
func (l *LUT) SampleAt(idx int) (s float64, pos, tangent Vec3) {
	si := int(l.spanIdx[idx])
	u := l.uSample[idx]
	if l.totalLength > 0 {
		s = l.cumLen[idx] / l.totalLength
	}
	return s, l.spans[si].Eval(u), l.unitTangent(si, u)
}

// Eval evaluates position and unit forward tangent at normalized arc-length
// s in [0, 1] in O(log M) time.
func (l *LUT) Eval(s float64) (pos, tangent Vec3) {
	si, u := l.locate(s)
	return l.spans[si].Eval(u), l.unitTangent(si, u)
}

func (l *LUT) locate(s float64) (span int, u float64) {
	if !(s > 0) || l.totalLength == 0 {
		return 0, 0
	}
	if s >= 1 {
		last := len(l.spans) - 1
		return last, 1
	}
	target := s * l.totalLength
	n := len(l.cumLen)
	hi := sort.Search(n, func(i int) bool { return l.cumLen[i] > target })
	if hi <= 0 {
		return 0, 0
	}
	if hi >= n {
		return len(l.spans) - 1, 1
	}
	lo := hi - 1
	// Walk past any zero-length span boundary step where cumLen[lo] == cumLen[hi].
	for lo > 0 && l.spanIdx[lo] != l.spanIdx[hi] {
		hi = lo
		lo--
	}
	if l.spanIdx[lo] != l.spanIdx[hi] {
		return int(l.spanIdx[hi]), l.uSample[hi]
	}

	si := int(l.spanIdx[lo])
	uLo, uHi := l.uSample[lo], l.uSample[hi]
	lLo, lHi := l.cumLen[lo], l.cumLen[hi]
	segLen := lHi - lLo
	if segLen <= 1e-15 {
		return si, uLo
	}

	u = uLo + ((target-lLo)/segLen)*(uHi-uLo)
	sp := l.spans[si]
	need := target - lLo
	// Two safeguarded Newton-Raphson steps refine u to < 1e-13 arc-length accuracy.
	for range 2 {
		cur := spanArcLength(sp, uLo, u)
		err := cur - need
		if math.Abs(err) <= 1e-13*l.totalLength {
			break
		}
		if err < 0 {
			// Tighten lower bracket without shifting the quadrature base uLo.
			speed := sp.Deriv(u).Len()
			if speed > 1e-12 {
				next := u - err/speed
				if next > u && next < uHi {
					u = next
					continue
				}
			}
			u = 0.5 * (u + uHi)
		} else {
			uHi = u
			speed := sp.Deriv(u).Len()
			if speed > 1e-12 {
				next := u - err/speed
				if next > uLo && next < uHi {
					u = next
					continue
				}
			}
			u = 0.5 * (uLo + uHi)
		}
	}
	return si, u
}

func (l *LUT) unitTangent(si int, u float64) Vec3 {
	sp := l.spans[si]
	d := sp.Deriv(u)
	if n := d.Len(); n > 1e-11 {
		inv := 1.0 / n
		return Vec3{X: d.X * inv, Y: d.Y * inv, Z: d.Z * inv}
	}
	// Handle zero-velocity Bezier handles or degenerate spans deterministically.
	const eps = 1e-5
	if u+eps <= 1 {
		if d2 := sp.Deriv(u + eps); d2.LenSq() > 1e-22 {
			return d2.Normalize()
		}
	}
	if u-eps >= 0 {
		if d2 := sp.Deriv(u - eps); d2.LenSq() > 1e-22 {
			return d2.Normalize()
		}
	}
	if chord := sp.Eval(1).Sub(sp.Eval(0)); chord.LenSq() > 1e-22 {
		return chord.Normalize()
	}
	// Search neighboring spans if an entire span collapsed to a single point.
	for off := 1; off < len(l.spans); off++ {
		if si+off < len(l.spans) {
			if c := l.spans[si+off].Eval(1).Sub(l.spans[si+off].Eval(0)); c.LenSq() > 1e-22 {
				return c.Normalize()
			}
		}
		if si-off >= 0 {
			if c := l.spans[si-off].Eval(1).Sub(l.spans[si-off].Eval(0)); c.LenSq() > 1e-22 {
				return c.Normalize()
			}
		}
	}
	return Vec3{Y: 1}
}
