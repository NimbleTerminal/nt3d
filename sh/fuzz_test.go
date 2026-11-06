// Copyright (c) 2026 Neomantra Corp.
package nt3dsh

import (
	"math"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// FuzzRotateEvaluate checks the rotation identity on arbitrary coefficients
// and rotations: evaluating rotated coefficients in a direction equals
// evaluating the originals in the inversely rotated direction, and rotating
// back recovers the coefficients. Non-finite or over-long input is refused.
func FuzzRotateEvaluate(f *testing.F) {
	f.Add(uint8(16), float32(1), float32(2), float32(3), float32(.73), float32(.3), float32(.4), float32(-.8), float32(.1), float32(-.05), float32(.02))
	f.Add(uint8(9), float32(0), float32(0), float32(1), float32(math.Pi), float32(1), float32(0), float32(0), float32(1), float32(1), float32(1))
	f.Add(uint8(4), float32(1), float32(0), float32(0), float32(0), float32(0), float32(1), float32(0), float32(.5), float32(.5), float32(.5))
	f.Add(uint8(1), float32(0), float32(0), float32(0), float32(1), float32(0), float32(0), float32(1), float32(.5), float32(.5), float32(.5))
	f.Add(uint8(7), float32(1), float32(1), float32(1), float32(1), float32(1), float32(1), float32(1), float32(1), float32(1), float32(1))
	f.Fuzz(func(t *testing.T, count uint8, ax, ay, az, angle, dx, dy, dz, c0, c1, c2 float32) {
		n := int(count % 17)
		c := make([]RGB, n)
		for i := range c {
			k := float32(i + 1)
			c[i] = RGB{c0 * k, c1 / k, c2 * float32(math.Sin(float64(k)))}
		}
		if !Valid(c) {
			if _, err := Rotate(c, nt3dmath.Identity()); err == nil && n > 0 {
				t.Fatalf("rotated %d invalid coefficients", n)
			}
			return
		}
		axis := nt3dmath.Vec3{X: ax, Y: ay, Z: az}
		if !finite(axis) || axis.Dot(axis) < 1e-6 || axis.Dot(axis) > 1e12 || math.IsNaN(float64(angle)) || math.IsInf(float64(angle), 0) || angle > 1e4 || angle < -1e4 {
			return
		}
		r := axisAngle(axis.Normalize(), float64(angle))
		rotated, err := Rotate(c, r)
		if err != nil {
			t.Fatalf("rotate: %v", err)
		}
		if len(rotated) != len(c) || !Valid(rotated) {
			t.Fatalf("rotated %d coefficients to %d, valid %v", len(c), len(rotated), Valid(rotated))
		}
		inv, ok := r.InverseAffine()
		if !ok {
			t.Fatal("rotation not invertible")
		}
		d := nt3dmath.Vec3{X: dx, Y: dy, Z: dz}
		if finite(d) && d.Dot(d) > 1e-6 && d.Dot(d) < 1e12 {
			d = d.Normalize()
			got, want := Evaluate(rotated, d), Evaluate(c, inv.Direction(d))
			scale := 1.0
			for _, v := range c {
				scale = math.Max(scale, math.Abs(float64(v[0]))+math.Abs(float64(v[1]))+math.Abs(float64(v[2])))
			}
			for k := range got {
				if math.Abs(float64(got[k]-want[k])) > 1e-4*scale {
					t.Fatalf("direction %v: %v, want %v", d, got, want)
				}
			}
		}
		back, err := Rotate(rotated, inv)
		if err != nil {
			t.Fatalf("rotate back: %v", err)
		}
		for i := range c {
			for k := range c[i] {
				tolerance := 1e-4 * math.Max(1, math.Abs(float64(c[i][k])))
				if math.Abs(float64(back[i][k]-c[i][k])) > tolerance {
					t.Fatalf("coefficient %d: %v, want %v", i, back[i], c[i])
				}
			}
		}
	})
}

func finite(v nt3dmath.Vec3) bool {
	for _, x := range [3]float32{v.X, v.Y, v.Z} {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return false
		}
	}
	return true
}

// axisAngle is Rodrigues' rotation about a unit axis.
func axisAngle(axis nt3dmath.Vec3, radians float64) nt3dmath.Mat4 {
	x, y, z := axis.X, axis.Y, axis.Z
	co, si := float32(math.Cos(radians)), float32(math.Sin(radians))
	k := 1 - co
	r := nt3dmath.Identity()
	r[0], r[1], r[2] = co+x*x*k, y*x*k+z*si, z*x*k-y*si
	r[4], r[5], r[6] = x*y*k-z*si, co+y*y*k, z*y*k+x*si
	r[8], r[9], r[10] = x*z*k+y*si, y*z*k-x*si, co+z*z*k
	return r
}
