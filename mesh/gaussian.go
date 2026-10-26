// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image/color"
	"math"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/sh"
	"strings"
)

var gaussianProperties = []string{"x", "y", "z", "f_dc_0", "f_dc_1", "f_dc_2", "opacity", "scale_0", "scale_1", "scale_2", "rot_0", "rot_1", "rot_2", "rot_3"}

// GaussianSH assembles the point-major spherical-harmonic coefficients of one
// Gaussian from its DC term and its higher-order terms. rest is channel-major:
// the coefficients of channel 0 (f_rest_0 onward), then channel 1, then channel 2.
// It must hold 0, 9, 24, or 45 values, and the result has 1 + len(rest)/3
// coefficients, the first being the DC term. A length that is not a degree's
// coefficient count, or a non-finite value, is an error.
func GaussianSH(dc [3]float64, rest []float64) ([]nt3dsh.RGB, error) {
	count := 1 + len(rest)/3
	if len(rest)%3 != 0 || nt3dsh.Degree(count) < 0 {
		return nil, fmt.Errorf("nt3dmesh: Gaussian SH needs 0, 9, 24, or 45 higher-order values, got %d", len(rest))
	}
	coefficients := make([]nt3dsh.RGB, count)
	for channel := 0; channel < 3; channel++ {
		coefficients[0][channel] = float32(dc[channel])
		for j := 1; j < count; j++ {
			coefficients[j][channel] = float32(rest[channel*(count-1)+j-1])
		}
	}
	if !nt3dsh.Valid(coefficients) {
		return nil, errors.New("nt3dmesh: nonfinite Gaussian SH coefficient")
	}
	return coefficients, nil
}

func gaussianPLY(elements []plyElement) bool {
	for _, e := range elements {
		if e.name == "vertex" {
			for _, p := range e.properties {
				if p.name == "scale_0" || p.name == "f_dc_0" || p.name == "rot_0" {
					return true
				}
			}
		}
	}
	return false
}

// GaussianPoint is a splat as the Gaussian decoders build one: at position,
// with one-sigma axes from a w, x, y, z quaternion (normalized here) and
// the logarithms of its three scales (scales must lie within [1e-20, 1e6]),
// its DC spherical harmonic coefficients as a clamped sRGB8 color (the 3DGS
// convention, 0.5 + 0.2820948·dc), and an opacity, 0 to 1, as alpha.
// Decoders share it so a splat reads the same from any format.
func GaussianPoint(position nt3dmath.Vec3, rotation [4]float64, logScales [3]float64, dc [3]float64, opacity float64) (nt3d.Point, error) {
	for _, v := range append(append(append(rotation[:], logScales[:]...), dc[:]...), opacity) {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nt3d.Point{}, errors.New("nt3dmesh: non-finite Gaussian value")
		}
	}
	p := nt3d.Point{Position: position, Radius: 1}
	if !validCoordinate(p.Position.X, p.Position.Y, p.Position.Z) {
		return nt3d.Point{}, errors.New("nt3dmesh: invalid Gaussian position")
	}
	w, x, y, z := rotation[0], rotation[1], rotation[2], rotation[3]
	norm := math.Hypot(math.Hypot(w, x), math.Hypot(y, z))
	if norm == 0 || math.IsInf(norm, 0) {
		return nt3d.Point{}, errors.New("nt3dmesh: invalid Gaussian quaternion")
	}
	w /= norm
	x /= norm
	y /= norm
	z /= norm
	axes := [3][3]float64{{1 - 2*(y*y+z*z), 2 * (x*y + w*z), 2 * (x*z - w*y)}, {2 * (x*y - w*z), 1 - 2*(x*x+z*z), 2 * (y*z + w*x)}, {2 * (x*z + w*y), 2 * (y*z - w*x), 1 - 2*(x*x+y*y)}}
	for k, a := range axes {
		s := math.Exp(logScales[k])
		if s < 1e-20 || s > 1e6 {
			return nt3d.Point{}, errors.New("nt3dmesh: Gaussian scale outside [1e-20,1e6]")
		}
		p.GaussianAxes[k] = nt3dmath.Vec3{X: float32(a[0] * s), Y: float32(a[1] * s), Z: float32(a[2] * s)}
	}
	channel := func(k int) uint8 {
		return uint8(math.Round(255 * max(0, min(1, .5+.28209479177387814*dc[k]))))
	}
	p.Color = color.RGBA{R: channel(0), G: channel(1), B: channel(2), A: uint8(math.Round(255 * opacity))}
	return p, nil
}

// DecodeGaussianPLY reads uncompressed 3DGS PLY (ASCII or either binary byte
// order). Scales are logarithms, opacity is a logit, and rot_0..3 is a wxyz
// quaternion. DC spherical harmonics become clamped sRGB8 point colors;
// degrees 0 through 3 are retained for view-dependent color. DecodePLY detects this subset.
func DecodeGaussianPLY(data []byte, o *Options) (*Model, error) {
	if int64(len(data)) > o.maxBytes() {
		return nil, fmt.Errorf("nt3dmesh: Gaussian PLY exceeds byte limit")
	}
	elements, format, _, body, err := plyHeader(data)
	if err != nil {
		return nil, err
	}
	r := &plyReader{data: data, at: body, line: bytes.Count(data[:body], []byte("\n")) + 1}
	switch format {
	case "ascii":
	case "binary_little_endian":
		r.order = binary.LittleEndian
	case "binary_big_endian":
		r.order = binary.BigEndian
	default:
		return nil, fmt.Errorf("nt3dmesh: unsupported PLY format %q", format)
	}
	g := nt3d.Geometry{PointStyle: nt3d.PointStyle{Shape: nt3d.GaussianPoint, Units: nt3d.WorldUnits, Blend: nt3d.AlphaBlend}}
	m := &Model{Format: PLY}
	seen := false
	for _, e := range elements {
		if e.count > len(data)-r.at {
			return nil, r.errorf("element count exceeds remaining bytes")
		}
		if e.name != "vertex" {
			for range e.count {
				if err := plySkip(r, e); err != nil {
					return nil, err
				}
			}
			continue
		}
		if seen || e.count == 0 {
			return nil, r.errorf("Gaussian vertices must be in one element, with at least one")
		}
		seen = true
		has := map[string]bool{}
		for _, p := range e.properties {
			if has[p.name] || p.count != "" {
				return nil, r.errorf("duplicate or list vertex property %q", p.name)
			}
			has[p.name] = true

		}
		for _, name := range gaussianProperties {
			if !has[name] {
				return nil, r.errorf("Gaussian PLY lacks %s", name)
			}
		}
		rest := 0
		for name := range has {
			if strings.HasPrefix(name, "f_rest_") {
				rest++
			}
		}
		count := 1 + rest/3
		if rest%3 != 0 || nt3dsh.Degree(count) < 0 {
			return nil, r.errorf("Gaussian SH needs 0, 9, 24, or 45 f_rest properties")
		}
		for j := 0; j < rest; j++ {
			if !has[fmt.Sprintf("f_rest_%d", j)] {
				return nil, r.errorf("Gaussian SH properties must be contiguous from f_rest_0")
			}
		}
		values, lists := map[string]float64{}, map[string][]float64{}
		restValues := make([]float64, rest)
		// Append only after reading: a dishonest header cannot trigger a huge allocation.
		for i := 0; i < e.count; i++ {
			if err := plyValues(r, e, values, lists); err != nil {
				return nil, err
			}
			for _, name := range gaussianProperties {
				v := values[name]
				if math.IsNaN(v) || math.IsInf(v, 0) {
					return nil, r.errorf("non-finite %s", name)
				}
			}
			p, err := GaussianPoint(nt3dmath.Vec3{X: float32(values["x"]), Y: float32(values["y"]), Z: float32(values["z"])},
				[4]float64{values["rot_0"], values["rot_1"], values["rot_2"], values["rot_3"]},
				[3]float64{values["scale_0"], values["scale_1"], values["scale_2"]},
				[3]float64{values["f_dc_0"], values["f_dc_1"], values["f_dc_2"]},
				1/(1+math.Exp(-values["opacity"])))
			if err != nil {
				return nil, r.errorf("%v", err)
			}
			for j := range restValues {
				restValues[j] = values[fmt.Sprintf("f_rest_%d", j)]
			}
			coefficients, err := GaussianSH([3]float64{values["f_dc_0"], values["f_dc_1"], values["f_dc_2"]}, restValues)
			if err != nil {
				return nil, r.errorf("%v", err)
			}
			g.GaussianSH = append(g.GaussianSH, coefficients...)
			g.Points = append(g.Points, p)
		}
	}
	if !seen {
		return nil, fmt.Errorf("nt3dmesh: Gaussian PLY lacks vertices")
	}
	g.Bounds = nt3d.GeometryBounds(g)
	m.Objects = PointPieces(Object{Geometry: g})
	if err := ClusterPieces(m.Objects, o); err != nil {
		return nil, err
	}
	for _, piece := range m.Objects {
		if err := piece.Geometry.Validate(); err != nil {
			return nil, err
		}
	}
	o.orient(m)
	return m, nil
}
