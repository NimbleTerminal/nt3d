// Copyright (c) 2026 Neomantra Corp.

package nt3dsog

import (
	"math"
	"math/rand/v2"
	"testing"

	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
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
