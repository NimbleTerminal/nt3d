// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
)

// Conf is a Stanford scan alignment: a list of range scans and where each goes.
const Conf Format = "conf"

// confPlacement is the transform a .conf line gives a scan: a translation
// and a quaternion, its scalar last. The Stanford files' scans line up when
// each is turned by the conjugate of that quaternion, then moved.
func confPlacement(t [3]float64, q [4]float64) nt3dmath.Mat4 {
	n := math.Sqrt(q[0]*q[0] + q[1]*q[1] + q[2]*q[2] + q[3]*q[3])
	if n == 0 {
		return nt3dmath.Translation(nt3dmath.Vec3{X: float32(t[0]), Y: float32(t[1]), Z: float32(t[2])})
	}
	i, j, k, r := -q[0]/n, -q[1]/n, -q[2]/n, q[3]/n
	rows := [3][3]float64{
		{1 - 2*(j*j+k*k), 2 * (i*j - k*r), 2 * (i*k + j*r)},
		{2 * (i*j + k*r), 1 - 2*(i*i+k*k), 2 * (j*k - i*r)},
		{2 * (i*k - j*r), 2 * (j*k + i*r), 1 - 2*(i*i+j*j)},
	}
	m := nt3dmath.Identity()
	for row := range 3 {
		for col := range 3 {
			m[col*4+row] = float32(rows[row][col])
		}
		m[12+row] = float32(t[row])
	}
	return m
}

// DecodeConf reads a Stanford scanalyze .conf file, which places range scans
// together: each "bmesh name tx ty tz qi qj qk qr" line names a PLY scan and
// the translation and rotation (a quaternion, its scalar last) that bring it
// into place. Each scan is read through Options.Open, as DecodePLY reads it,
// and becomes the objects of the model, named for its file and placed. The
// triangle limit applies to all scans together. A scan that cannot be read
// is a warning. A "camera" line, the viewer's, is ignored.
func DecodeConf(data []byte, o *Options) (*Model, error) {
	if o == nil || o.Open == nil {
		return nil, errors.New("nt3dmesh: a .conf needs Options.Open to read its scans")
	}
	m := &Model{Format: Conf}
	// Scans are placed before the model turns up: the placements assume the
	// scans' own axes.
	scan := *o
	scan.Up = ZUp
	limit := o.limit()
	lines := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; lines.Scan(); n++ {
		fields := strings.Fields(lines.Text())
		if len(fields) == 0 || fields[0] != "bmesh" {
			continue
		}
		if len(fields) != 9 {
			return nil, fmt.Errorf("nt3dmesh: .conf line %d: bmesh needs a name and seven numbers", n)
		}
		var v [7]float64
		for i := range v {
			x, err := strconv.ParseFloat(fields[2+i], 64)
			if err != nil || math.IsNaN(x) || math.IsInf(x, 0) || math.Abs(x) > maxCoordinate {
				return nil, fmt.Errorf("nt3dmesh: .conf line %d: bad number %q", n, fields[2+i])
			}
			v[i] = x
		}
		name := fields[1]
		raw, err := o.Open(name)
		if err != nil {
			m.Warnings = append(m.Warnings, fmt.Sprintf("scan %s: %v", name, err))
			continue
		}
		scan.MaxTriangles = limit - m.Triangles()
		if scan.MaxTriangles <= 0 {
			return nil, fmt.Errorf("%w: the scans exceed %d", ErrTooManyTriangles, limit)
		}
		part, err := DecodePLY(raw, &scan)
		if err != nil {
			if errors.Is(err, ErrTooManyTriangles) {
				return nil, err
			}
			m.Warnings = append(m.Warnings, fmt.Sprintf("scan %s: %v", name, err))
			continue
		}
		place := confPlacement([3]float64{v[0], v[1], v[2]}, [4]float64{v[3], v[4], v[5], v[6]})
		for _, obj := range part.Objects {
			if obj.Geometry, err = nt3d.TransformGeometry(obj.Geometry, place); err != nil {
				return nil, fmt.Errorf("nt3dmesh: scan %s: %w", name, err)
			}
			obj.Name = name
			m.Objects = append(m.Objects, obj)
		}
		for _, w := range part.Warnings {
			m.Warnings = append(m.Warnings, name+": "+w)
		}
	}
	if err := lines.Err(); err != nil {
		return nil, fmt.Errorf("nt3dmesh: .conf: %w", err)
	}
	if len(m.Objects) == 0 {
		return nil, fmt.Errorf("nt3dmesh: .conf placed no scans (%d warnings)", len(m.Warnings))
	}
	o.orient(m)
	return m, nil
}
