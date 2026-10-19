package nt3dsh

import (
	"math"
	"nimbleterminal.dev/nt3d/math"
	"testing"
)

func close(t *testing.T, a, b float32, tolerance float64) {
	t.Helper()
	if math.Abs(float64(a-b)) > tolerance {
		t.Fatalf("got %g want %g", a, b)
	}
}
func TestBasisAxesAndAdditionTheorem(t *testing.T) {
	y := Basis(3, nt3dmath.Vec3{Y: 10})
	close(t, y[1], -.4886025119, 1e-7)
	close(t, y[9], .5900435899, 1e-7)
	close(t, y[15], 0, 1e-7)
	for _, d := range []nt3dmath.Vec3{{X: 1}, {Z: -1}, {X: .2, Y: -.7, Z: .4}} {
		b := Basis(3, d)
		for l := 0; l <= 3; l++ {
			sum := float32(0)
			for j := l * l; j < (l+1)*(l+1); j++ {
				sum += b[j] * b[j]
			}
			close(t, sum, float32(float64(2*l+1)/(4*math.Pi)), 2e-6)
		}
	}
	if Basis(4, nt3dmath.Vec3{X: 1}) != ([16]float32{}) {
		t.Fatal("invalid degree")
	}
	b := Basis(3, nt3dmath.Vec3{})
	for i := 1; i < 16; i++ {
		if b[i] != 0 {
			t.Fatal("zero direction should have DC only")
		}
	}
}
func TestProjectionAndDiffuse(t *testing.T) {
	coeff, err := Project(2, func(d nt3dmath.Vec3) RGB { return RGB{1, .5 + .4*d.Z, 2 + d.X*d.Y} })
	if err != nil {
		t.Fatal(err)
	}
	diffuse := Diffuse(coeff)
	for _, d := range []nt3dmath.Vec3{{Z: 1}, {X: 1}, {X: .6, Y: .8}} {
		rgb := Evaluate(diffuse[:], d)
		close(t, rgb[0], 1, .002)
		close(t, rgb[1], .5+.4*(2.0/3)*d.Z, .002)
		close(t, rgb[2], 2+.25*d.X*d.Y, .004)
	}
	// Each basis projected back should recover only its own coefficient.
	for i := 0; i < 16; i++ {
		c, err := Project(3, func(d nt3dmath.Vec3) RGB { b := BasisUnit(3, d); return RGB{b[i], 0, 0} })
		if err != nil {
			t.Fatal(err)
		}
		for j := range c {
			want := float32(0)
			if j == i {
				want = 1
			}
			close(t, c[j][0], want, .008)
		}
	}
}
func TestRotationEvaluationAndRoundTrip(t *testing.T) {
	c := make([]RGB, 16)
	for i := range c {
		c[i] = RGB{float32(i%5-2) * .1, float32(i) * .03, -.1}
	}
	// A general axis-angle rotation, not just a permutation of axes.
	axis := nt3dmath.Vec3{X: 1, Y: 2, Z: 3}.Normalize()
	x, y, z := axis.X, axis.Y, axis.Z
	co, si := float32(math.Cos(.73)), float32(math.Sin(.73))
	k := 1 - co
	r := nt3dmath.Identity()
	r[0], r[1], r[2] = co+x*x*k, y*x*k+z*si, z*x*k-y*si
	r[4], r[5], r[6] = x*y*k-z*si, co+y*y*k, z*y*k+x*si
	r[8], r[9], r[10] = x*z*k+y*si, y*z*k-x*si, co+z*z*k
	rotated, err := Rotate(c, r)
	if err != nil {
		t.Fatal(err)
	}
	inv, _ := r.InverseAffine()
	for _, d := range []nt3dmath.Vec3{{X: 1}, {Z: -1}, {X: .3, Y: .4, Z: -.8}} {
		got, want := Evaluate(rotated, d), Evaluate(c, inv.Direction(d))
		for k := range got {
			close(t, got[k], want[k], 2e-6)
		}
	}
	back, err := Rotate(rotated, inv)
	if err != nil {
		t.Fatal(err)
	}
	for i := range c {
		for k := range c[i] {
			close(t, back[i][k], c[i][k], 2e-6)
		}
	}
	if _, err := Rotate(c, nt3dmath.Scaling(nt3dmath.Vec3{X: 2, Y: 1, Z: 1})); err == nil {
		t.Fatal("accepted scaling")
	}
}
func TestInvalidInputs(t *testing.T) {
	if Valid(make([]RGB, 2)) || Valid([]RGB{{float32(math.NaN()), 0, 0}}) {
		t.Fatal("invalid coefficients accepted")
	}
	if _, err := Project(2, func(nt3dmath.Vec3) RGB { return RGB{float32(math.Inf(1)), 0, 0} }); err == nil {
		t.Fatal("accepted nonfinite sample")
	}
	if _, err := Project(4, func(nt3dmath.Vec3) RGB { return RGB{} }); err == nil {
		t.Fatal("accepted unsupported degree")
	}
}
