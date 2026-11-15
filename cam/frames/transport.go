// Copyright (c) 2026 Neomantra Corp.

package nt3dframes

import (
	"math"
	"sort"

	nt3dcurve "nimbleterminal.dev/nt3d/cam/curve"
)

// ParallelTransport precomputes twist-minimizing Bishop frames (Hanson & Ma, 1995)
// along an arc-length LUT so tangent-aligned cameras never flip at inflection points.
type ParallelTransport struct {
	lut     *nt3dcurve.LUT
	sGrid   []float64
	ups     []nt3dcurve.Vec3
	worldUp nt3dcurve.Vec3
}

// NewParallelTransport builds a parallel-transport up-vector table aligned with lut.
func NewParallelTransport(lut *nt3dcurve.LUT, worldUp nt3dcurve.Vec3) *ParallelTransport {
	uRef := worldUp.Normalize()
	if uRef.LenSq() == 0 {
		uRef = nt3dcurve.Vec3{Z: 1}
	}
	m := lut.SampleCount()
	sGrid := make([]float64, m)
	ups := make([]nt3dcurve.Vec3, m)
	if m == 0 {
		return &ParallelTransport{lut: lut, worldUp: uRef}
	}

	s0, _, t0 := lut.SampleAt(0)
	sGrid[0] = s0
	ups[0] = initialUp(t0, uRef)

	prevT := t0
	prevU := ups[0]
	for k := 1; k < m; k++ {
		sk, _, tk := lut.SampleAt(k)
		sGrid[k] = sk
		nextU := transportUp(prevU, prevT, tk)
		ups[k] = nextU
		prevT = tk
		prevU = nextU
	}
	return &ParallelTransport{
		lut:     lut,
		sGrid:   sGrid,
		ups:     ups,
		worldUp: uRef,
	}
}

func initialUp(t0, worldUp nt3dcurve.Vec3) nt3dcurve.Vec3 {
	proj := worldUp.Sub(t0.Scale(worldUp.Dot(t0)))
	if proj.LenSq() > 1e-16 {
		return proj.Normalize()
	}
	alt := nt3dcurve.Vec3{Y: 1}
	if math.Abs(t0.Y) > 0.9 {
		alt = nt3dcurve.Vec3{X: 1}
	}
	return alt.Sub(t0.Scale(alt.Dot(t0))).Normalize()
}

func transportUp(uPrev, tPrev, tNext nt3dcurve.Vec3) nt3dcurve.Vec3 {
	b := tPrev.Cross(tNext)
	sinTheta := b.Len()
	cosTheta := math.Max(-1, math.Min(1, tPrev.Dot(tNext)))

	var rotated nt3dcurve.Vec3
	if sinTheta < 1e-12 {
		// Either identical tangents (cosTheta > 0) or a 180-degree reversal
		// (where rotating around uPrev keeps uPrev unchanged while flipping T and R).
		rotated = uPrev
	} else {
		axis := b.Scale(1.0 / sinTheta)
		// Rodrigues' rotation formula using exact sinTheta, cosTheta directly.
		rotated = uPrev.Scale(cosTheta).
			Add(axis.Cross(uPrev).Scale(sinTheta)).
			Add(axis.Scale(axis.Dot(uPrev) * (1 - cosTheta)))
	}

	// Gram-Schmidt orthogonalization against tNext eliminates cumulative drift.
	ortho := rotated.Sub(tNext.Scale(rotated.Dot(tNext)))
	if ortho.LenSq() < 1e-18 {
		return initialUp(tNext, uPrev)
	}
	return ortho.Normalize()
}

// FrameBasis returns the orthonormal world-space (right, up, forward) basis at
// normalized arc-length s in [0, 1]. If reverse is true, forward faces -T(s)
// while keeping up aligned with the transported up vector.
func (pt *ParallelTransport) FrameBasis(s float64, reverse bool) (right, up, forward nt3dcurve.Vec3) {
	if pt == nil || pt.lut == nil || len(pt.ups) == 0 {
		q := LookRotation(nt3dcurve.Vec3{Y: 1}, nt3dcurve.Vec3{Z: 1})
		return q.Basis()
	}
	if pt.lut.TotalLength() == 0 {
		q := LookRotation(nt3dcurve.Vec3{Y: 1}, pt.worldUp)
		return q.Basis()
	}

	sc := math.Max(0, math.Min(1, s))
	_, tan := pt.lut.Eval(sc)

	var interpU nt3dcurve.Vec3
	n := len(pt.sGrid)
	if sc <= pt.sGrid[0] || n == 1 {
		interpU = pt.ups[0]
	} else if sc >= pt.sGrid[n-1] {
		interpU = pt.ups[n-1]
	} else {
		hi := sort.Search(n, func(i int) bool { return pt.sGrid[i] > sc })
		lo := hi - 1
		ds := pt.sGrid[hi] - pt.sGrid[lo]
		if ds <= 1e-15 {
			interpU = pt.ups[lo]
		} else {
			w := (sc - pt.sGrid[lo]) / ds
			interpU = pt.ups[lo].Scale(1 - w).Add(pt.ups[hi].Scale(w))
		}
	}

	fwd := tan
	if reverse {
		fwd = fwd.Scale(-1)
	}
	uProj := interpU.Sub(fwd.Scale(interpU.Dot(fwd)))
	if uProj.LenSq() < 1e-16 {
		uProj = initialUp(fwd, pt.worldUp)
	} else {
		uProj = uProj.Normalize()
	}
	right = fwd.Cross(uProj).Normalize()
	up = right.Cross(fwd).Normalize()
	return right, up, fwd
}

// Eval returns the unit camera-to-world quaternion at normalized arc-length s in [0, 1].
func (pt *ParallelTransport) Eval(s float64, reverse bool) Quat {
	r, u, f := pt.FrameBasis(s, reverse)
	return FromBasis(r, u, f)
}
