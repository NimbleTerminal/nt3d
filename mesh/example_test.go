// Copyright (c) 2026 Neomantra Corp.

package nt3dmesh_test

import (
	"errors"
	"fmt"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
)

// A decoder of a splat format of your own charges what it reads to the byte
// budget and finishes its cloud with GaussianModel, so its limits, pieces,
// palette, and clusters are those of the built-in decoders.
func ExampleGaussianModel() {
	// The format here is one byte a splat: its position along X.
	decode := func(data []byte, o *nt3dmesh.Options) (*nt3dmesh.Model, error) {
		if err := o.ByteBudget().Take(int64(len(data)), "splat file"); err != nil {
			return nil, err
		}
		g := nt3d.Geometry{PointStyle: nt3d.PointStyle{Shape: nt3d.GaussianPoint, Units: nt3d.WorldUnits, Blend: nt3d.AlphaBlend}}
		for _, b := range data {
			p, err := nt3dmesh.GaussianPoint(nt3dmath.Vec3{X: float32(b)}, [4]float64{1, 0, 0, 0}, [3]float64{}, [3]float64{}, 1)
			if err != nil {
				return nil, err
			}
			g.Points = append(g.Points, p)
		}
		g.Bounds = nt3d.GeometryBounds(g)
		return nt3dmesh.GaussianModel("bytes", g, o)
	}

	m, err := decode([]byte{1, 2, 3}, nil)
	fmt.Println(len(m.Objects[0].Geometry.Points), m.Objects[0].Geometry.Clusters != nil, err)

	_, err = decode([]byte{1, 2, 3}, &nt3dmesh.Options{MaxBytes: 2})
	fmt.Println(errors.Is(err, nt3dmesh.ErrTooLarge))
	// Output:
	// 3 true <nil>
	// true
}
