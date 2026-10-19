// Copyright (c) 2026 Neomantra Corp.

// Package nt3dsh evaluates, projects, rotates, and convolves real spherical
// harmonics through degree three. Coefficients are ordered by l*(l+1)+m,
// m=-l..l, with the orthonormal real basis used by standard 3DGS PLY files:
// Y00=0.28209479, then -0.48860251*y, 0.48860251*z, -0.48860251*x.
// RGB values are unrestricted floats: callers choose their color space.
// Lighting should use linear radiance. No clamping, gamma conversion, or
// 3DGS-specific 0.5 color offset is applied by this package.
package nt3dsh

import (
	"fmt"
	"math"
	"strings"

	"nimbleterminal.dev/nt3d/math"
)

// RGB is one RGB coefficient or evaluated color.
type RGB = [3]float32

const MaxDegree = 3

// Count is the number of coefficients for degree 0..3, or zero if unsupported.
func Count(degree int) int {
	if degree < 0 || degree > MaxDegree {
		return 0
	}
	return (degree + 1) * (degree + 1)
}

// Degree returns the degree for a complete coefficient count, or -1.
func Degree(count int) int {
	for d := 0; d <= MaxDegree; d++ {
		if Count(d) == count {
			return d
		}
	}
	return -1
}

// Each polynomial term is scale*x^x*y^y*z^z. This table is also the source
// of the WGSL evaluator, keeping signs, ordering, and constants identical.
type term struct {
	scale   float32
	x, y, z int
}

var polynomials = [16][]term{
	{{.28209479177387814, 0, 0, 0}},
	{{-.4886025119029199, 0, 1, 0}}, {{.4886025119029199, 0, 0, 1}}, {{-.4886025119029199, 1, 0, 0}},
	{{1.0925484305920792, 1, 1, 0}}, {{-1.0925484305920792, 0, 1, 1}},
	{{.6307831305050401, 0, 0, 2}, {-.31539156525252005, 2, 0, 0}, {-.31539156525252005, 0, 2, 0}},
	{{-1.0925484305920792, 1, 0, 1}}, {{.5462742152960396, 2, 0, 0}, {-.5462742152960396, 0, 2, 0}},
	{{-1.7701307697799304, 2, 1, 0}, {.5900435899266435, 0, 3, 0}},
	{{2.890611442640554, 1, 1, 1}},
	{{-1.828183197857863, 0, 1, 2}, {.4570457994644658, 2, 1, 0}, {.4570457994644658, 0, 3, 0}},
	{{.7463526651802308, 0, 0, 3}, {-1.1195289977703462, 2, 0, 1}, {-1.1195289977703462, 0, 2, 1}},
	{{-1.828183197857863, 1, 0, 2}, {.4570457994644658, 3, 0, 0}, {.4570457994644658, 1, 2, 0}},
	{{1.445305721320277, 2, 0, 1}, {-1.445305721320277, 0, 2, 1}},
	{{-.5900435899266435, 3, 0, 0}, {1.7701307697799304, 1, 2, 0}},
}

// Basis returns an orthonormal real SH basis for direction d, normalized
// internally. A zero or nonfinite direction returns only the constant term.
// Unsupported degrees return zero. Entries above the degree are zero.
func Basis(degree int, d nt3dmath.Vec3) [16]float32 {
	n := math.Hypot(math.Hypot(float64(d.X), float64(d.Y)), float64(d.Z))
	if n == 0 || math.IsNaN(n) || math.IsInf(n, 0) {
		var b [16]float32
		if Count(degree) > 0 {
			b[0] = polynomials[0][0].scale
		}
		return b
	}
	return BasisUnit(degree, nt3dmath.Vec3{X: float32(float64(d.X) / n), Y: float32(float64(d.Y) / n), Z: float32(float64(d.Z) / n)})
}

// BasisUnit avoids normalization when d is already a finite unit vector.
func BasisUnit(degree int, d nt3dmath.Vec3) (b [16]float32) {
	x := [4]float32{1, d.X, d.X * d.X, d.X * d.X * d.X}
	y := [4]float32{1, d.Y, d.Y * d.Y, d.Y * d.Y * d.Y}
	z := [4]float32{1, d.Z, d.Z * d.Z, d.Z * d.Z * d.Z}
	for i := 0; i < Count(degree); i++ {
		for _, t := range polynomials[i] {
			b[i] += t.scale * x[t.x] * y[t.y] * z[t.z]
		}
	}
	return
}

// Valid reports whether coefficients contain 1, 4, 9, or 16 finite RGBs.
func Valid(coefficients []RGB) bool {
	if Degree(len(coefficients)) < 0 {
		return false
	}
	for _, c := range coefficients {
		for _, v := range c {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return false
			}
		}
	}
	return true
}

// Evaluate evaluates a complete set of coefficients in direction d.
// An unsupported coefficient count returns black; values are not clamped.
func Evaluate(coefficients []RGB, d nt3dmath.Vec3) (rgb RGB) {
	b := Basis(Degree(len(coefficients)), d)
	if Degree(len(coefficients)) < 0 {
		return
	}
	for i, c := range coefficients {
		for k := range rgb {
			rgb[k] += c[k] * b[i]
		}
	}
	return
}

// Project integrates a directional function using 2048 Gauss-Legendre/azimuth samples.
// The callback receives unit directions; projection is deterministic and
// intended for smooth lighting, not sharp specular highlights. Invalid
// degrees, nil callbacks, or nonfinite samples return an error.
func Project(degree int, sample func(nt3dmath.Vec3) RGB) ([]RGB, error) {
	if Count(degree) == 0 || sample == nil {
		return nil, fmt.Errorf("nt3dsh: invalid projection")
	}
	out := make([]RGB, Count(degree))
	var sums [16][3]float64
	const nx, ny = 64, 32
	for y := 0; y < ny; y++ {
		z := projectionNodes[y]
		r := math.Sqrt(1 - z*z)
		for x := 0; x < nx; x++ {
			phi := 2 * math.Pi * (float64(x) + .5) / nx
			d := nt3dmath.Vec3{X: float32(r * math.Cos(phi)), Y: float32(r * math.Sin(phi)), Z: float32(z)}
			c := sample(d)
			b := BasisUnit(degree, d)
			for k, v := range c {
				if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
					return nil, fmt.Errorf("nt3dsh: nonfinite sample")
				}
				for i := range out {
					sums[i][k] += float64(v) * float64(b[i]) * 2 * math.Pi / nx * projectionWeights[y]
				}
			}
		}
	}
	for i := range out {
		for k := range out[i] {
			out[i][k] = float32(sums[i][k])
		}
	}
	return out, nil
}

// Diffuse convolves radiance with the clamped cosine and divides by pi,
// producing degree-two diffuse lighting ready to multiply by linear albedo.
// The band factors are 1, 2/3, and 1/4; higher bands are discarded.
// A constant unit environment therefore produces unit diffuse lighting.
func Diffuse(radiance []RGB) (out [9]RGB) {
	if Degree(len(radiance)) < 0 {
		return
	}
	for i := 0; i < min(9, len(radiance)); i++ {
		factor := float32(1)
		if i >= 4 {
			factor = .25
		} else if i >= 1 {
			factor = 2.0 / 3
		}
		for k := range out[i] {
			out[i][k] = radiance[i][k] * factor
		}
	}
	return
}

// Rotate returns coefficients for f(R^-1*d). R must be a proper orthonormal
// affine rotation (translation is ignored). Per-band quadrature integrates
// degree-six products exactly, apart from float roundoff; bands do not mix.
func Rotate(coefficients []RGB, r nt3dmath.Mat4) ([]RGB, error) {
	if !Valid(coefficients) {
		return nil, fmt.Errorf("nt3dsh: invalid coefficients")
	}
	inv, ok := r.InverseAffine()
	if !ok || math.Abs(float64(r.Determinant3()-1)) > 1e-4 {
		return nil, fmt.Errorf("nt3dsh: expected rotation")
	}
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			dot := float32(0)
			for k := 0; k < 3; k++ {
				dot += r[i*4+k] * r[j*4+k]
			}
			want := float32(0)
			if i == j {
				want = 1
			}
			if math.Abs(float64(dot-want)) > 1e-4 {
				return nil, fmt.Errorf("nt3dsh: expected orthonormal rotation")
			}
		}
	}
	out := make([]RGB, len(coefficients))
	out[0] = coefficients[0]
	nodes := [4]float64{-.8611363115940526, -.3399810435848563, .3399810435848563, .8611363115940526}
	weights := [4]float64{.34785484513745385, .6521451548625461, .6521451548625461, .34785484513745385}
	for band := 1; band <= Degree(len(out)); band++ {
		var sums [16][3]float64
		for j, z := range nodes {
			rad := math.Sqrt(1 - z*z)
			for k := 0; k < 8; k++ {
				phi := float64(k) * math.Pi / 4
				d := nt3dmath.Vec3{X: float32(rad * math.Cos(phi)), Y: float32(rad * math.Sin(phi)), Z: float32(z)}
				a, b := BasisUnit(band, d), Basis(band, inv.Direction(d))
				var c [3]float64
				for i := band * band; i < (band+1)*(band+1); i++ {
					for ch := range c {
						c[ch] += float64(coefficients[i][ch]) * float64(b[i])
					}
				}
				for i := band * band; i < (band+1)*(band+1); i++ {
					for ch := range c {
						sums[i][ch] += c[ch] * float64(a[i]) * weights[j] * math.Pi / 4
					}
				}
			}
		}
		for i := band * band; i < (band+1)*(band+1); i++ {
			for ch := range out[i] {
				out[i][ch] = float32(sums[i][ch])
			}
		}
	}
	return out, nil
}

// WGSL provides nt3dSHBasis(index:u32,d:vec3<f32>)->f32. The direction
// must be a finite unit vector. Generated from the same polynomial table
// as BasisUnit; indices 0..15 address the basis, other indices return zero.
func WGSL() string {
	var s strings.Builder
	s.WriteString("fn nt3dSHBasis(index:u32,d:vec3<f32>)->f32 {\n switch index {\n")
	for i, terms := range polynomials {
		fmt.Fprintf(&s, " case %du: { return ", i)
		for j, t := range terms {
			if j > 0 {
				s.WriteByte('+')
			}
			fmt.Fprintf(&s, "(%#.9g", t.scale)
			for a, n := range []int{t.x, t.y, t.z} {
				for k := 0; k < n; k++ {
					fmt.Fprintf(&s, "*d.%s", []string{"x", "y", "z"}[a])
				}
			}
			s.WriteByte(')')
		}
		s.WriteString("; }\n")
	}
	s.WriteString(" default: { return 0.0; }\n }\n}\n")
	return s.String()
}

// Gauss-Legendre weights avoid polar integration errors in higher bands.
var projectionNodes, projectionWeights = quadrature(32)

func quadrature(n int) ([]float64, []float64) {
	nodes, weights := make([]float64, n), make([]float64, n)
	for i := 0; i < (n+1)/2; i++ {
		z := math.Cos(math.Pi * (float64(i) + .75) / (float64(n) + .5))
		var derivative float64
		for iterations := 0; iterations < 20; iterations++ {
			p0, p1 := 1.0, z
			for k := 2; k <= n; k++ {
				p0, p1 = p1, ((2*float64(k)-1)*z*p1-(float64(k)-1)*p0)/float64(k)
			}
			derivative = float64(n) * (z*p1 - p0) / (z*z - 1)
			next := z - p1/derivative
			if math.Abs(next-z) < 1e-15 {
				z = next
				break
			}
			z = next
		}
		nodes[i], nodes[n-1-i] = -z, z
		weights[i] = 2 / ((1 - z*z) * derivative * derivative)
		weights[n-1-i] = weights[i]
	}
	return nodes, weights
}
