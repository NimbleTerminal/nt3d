// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image/color"
	"math"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"strings"
)

var gaussianProperties = []string{"x", "y", "z", "f_dc_0", "f_dc_1", "f_dc_2", "opacity", "scale_0", "scale_1", "scale_2", "rot_0", "rot_1", "rot_2", "rot_3"}

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

// DecodeGaussianPLY reads uncompressed 3DGS PLY (ASCII or either binary byte
// order). Scales are logarithms, opacity is a logit, and rot_0..3 is a wxyz
// quaternion. DC spherical harmonics become clamped sRGB8 point colors;
// higher harmonics are ignored with a warning. DecodePLY detects this subset.
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
		if seen || e.count == 0 || e.count > nt3d.MaxPoints {
			return nil, r.errorf("Gaussian vertex count must be 1..%d, in one element", nt3d.MaxPoints)
		}
		seen = true
		has := map[string]bool{}
		for _, p := range e.properties {
			if has[p.name] || p.count != "" {
				return nil, r.errorf("duplicate or list vertex property %q", p.name)
			}
			has[p.name] = true
			if strings.HasPrefix(p.name, "f_rest_") && len(m.Warnings) == 0 {
				m.Warnings = append(m.Warnings, "Gaussian PLY: higher SH ignored; rendering DC color only")
			}
		}
		for _, name := range gaussianProperties {
			if !has[name] {
				return nil, r.errorf("Gaussian PLY lacks %s", name)
			}
		}
		values, lists := map[string]float64{}, map[string][]float64{}
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
			p := nt3d.Point{Position: nt3dmath.Vec3{X: float32(values["x"]), Y: float32(values["y"]), Z: float32(values["z"])}, Radius: 1}
			if !validCoordinate(p.Position.X, p.Position.Y, p.Position.Z) {
				return nil, r.errorf("invalid Gaussian position")
			}
			w, x, y, z := values["rot_0"], values["rot_1"], values["rot_2"], values["rot_3"]
			norm := math.Hypot(math.Hypot(w, x), math.Hypot(y, z))
			if norm == 0 || math.IsInf(norm, 0) {
				return nil, r.errorf("invalid Gaussian quaternion")
			}
			w /= norm
			x /= norm
			y /= norm
			z /= norm
			axes := [3][3]float64{{1 - 2*(y*y+z*z), 2 * (x*y + w*z), 2 * (x*z - w*y)}, {2 * (x*y - w*z), 1 - 2*(x*x+z*z), 2 * (y*z + w*x)}, {2 * (x*z + w*y), 2 * (y*z - w*x), 1 - 2*(x*x+y*y)}}
			for k, a := range axes {
				s := math.Exp(values[fmt.Sprintf("scale_%d", k)])
				if s < 1e-20 || s > 1e6 {
					return nil, r.errorf("Gaussian scale outside [1e-20,1e6]")
				}
				p.GaussianAxes[k] = nt3dmath.Vec3{X: float32(a[0] * s), Y: float32(a[1] * s), Z: float32(a[2] * s)}
			}
			channel := func(k int) uint8 {
				return uint8(math.Round(255 * max(0, min(1, .5+.28209479177387814*values[fmt.Sprintf("f_dc_%d", k)]))))
			}
			p.Color = color.RGBA{R: channel(0), G: channel(1), B: channel(2), A: uint8(math.Round(255 / (1 + math.Exp(-values["opacity"]))))}
			g.Points = append(g.Points, p)
		}
	}
	if !seen {
		return nil, fmt.Errorf("nt3dmesh: Gaussian PLY lacks vertices")
	}
	g.Bounds = nt3d.GeometryBounds(g)
	if err := g.Validate(); err != nil {
		return nil, err
	}
	m.Objects = []Object{{Geometry: g}}
	o.orient(m)
	return m, nil
}
