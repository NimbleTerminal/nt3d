package nt3dmath

import (
	"math"
	"testing"
)

func TestNormalizeAcrossFloat32Range(t *testing.T) {
	diagonal := float32(1 / math.Sqrt(3))
	for _, tt := range []struct {
		name string
		in   Vec3
		want Vec3
	}{
		{"zero", Vec3{}, Vec3{}},
		{"ordinary", Vec3{3, -4, 0}, Vec3{.6, -.8, 0}},
		{"large", Vec3{math.MaxFloat32, -math.MaxFloat32, math.MaxFloat32}, Vec3{diagonal, -diagonal, diagonal}},
		{"small", Vec3{3e-30, -4e-30, 0}, Vec3{.6, -.8, 0}},
		{"subnormal", Vec3{Z: math.SmallestNonzeroFloat32}, Vec3{Z: 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in.Normalize()
			for i, pair := range [][2]float32{{got.X, tt.want.X}, {got.Y, tt.want.Y}, {got.Z, tt.want.Z}} {
				if !(math.Abs(float64(pair[0])-float64(pair[1])) <= 1e-7) {
					t.Fatalf("component %d: %v.Normalize() = %v, want %v", i, tt.in, got, tt.want)
				}
			}
		})
	}
}
