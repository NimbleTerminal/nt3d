// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"fmt"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

func (d *gltfDecoder) floats(index int) ([]float32, error) {
	a, err := d.accessor(index, "animation")
	if err != nil {
		return nil, err
	}
	return d.readFloatAnimation(a)
}

func (d *gltfDecoder) readFloatAnimation(a *gltf.Accessor) ([]float32, error) {
	if a.ComponentType != gltf.ComponentFloat || a.Normalized {
		return nil, fmt.Errorf("nt3dmesh: animation accessor must be float")
	}
	v, err := modeler.ReadAccessor(d.doc, a, nil)
	if err != nil {
		return nil, err
	}
	var out []float32
	switch x := v.(type) {
	case []float32:
		out = x
	case [][3]float32:
		for _, v := range x {
			out = append(out, v[:]...)
		}
	case [][4]float32:
		for _, v := range x {
			out = append(out, v[:]...)
		}
	default:
		return nil, fmt.Errorf("nt3dmesh: invalid animation accessor shape")
	}
	return out, nil
}

func (d *gltfDecoder) animationValues(index int, path gltf.TRSProperty) ([]float32, error) {
	a, err := d.accessor(index, "animation")
	if err != nil {
		return nil, err
	}
	if a.ComponentType == gltf.ComponentFloat {
		return d.readFloatAnimation(a)
	}
	if !a.Normalized {
		return nil, fmt.Errorf("nt3dmesh: integer animation accessor must be normalized")
	}
	switch path {
	case gltf.TRSRotation:
		if a.Type != gltf.AccessorVec4 || (a.ComponentType != gltf.ComponentByte && a.ComponentType != gltf.ComponentShort) {
			return nil, fmt.Errorf("nt3dmesh: invalid rotation animation accessor")
		}
		v, err := modeler.ReadAccessor(d.doc, a, nil)
		if err != nil {
			return nil, err
		}
		switch x := v.(type) {
		case [][4]int8:
			out := make([]float32, 0, len(x)*4)
			for _, q := range x {
				out = append(out,
					gltf.DenormalizeByte(q[0]),
					gltf.DenormalizeByte(q[1]),
					gltf.DenormalizeByte(q[2]),
					gltf.DenormalizeByte(q[3]),
				)
			}
			return out, nil
		case [][4]int16:
			out := make([]float32, 0, len(x)*4)
			for _, q := range x {
				out = append(out,
					gltf.DenormalizeShort(q[0]),
					gltf.DenormalizeShort(q[1]),
					gltf.DenormalizeShort(q[2]),
					gltf.DenormalizeShort(q[3]),
				)
			}
			return out, nil
		default:
			return nil, fmt.Errorf("nt3dmesh: invalid rotation animation accessor")
		}
	case gltf.TRSWeights:
		if a.Type != gltf.AccessorScalar {
			return nil, fmt.Errorf("nt3dmesh: invalid weights animation accessor")
		}
		v, err := modeler.ReadAccessor(d.doc, a, nil)
		if err != nil {
			return nil, err
		}
		switch x := v.(type) {
		case []int8:
			out := make([]float32, len(x))
			for i, w := range x {
				out[i] = gltf.DenormalizeByte(w)
			}
			return out, nil
		case []uint8:
			out := make([]float32, len(x))
			for i, w := range x {
				out[i] = gltf.DenormalizeUbyte(w)
			}
			return out, nil
		case []int16:
			out := make([]float32, len(x))
			for i, w := range x {
				out[i] = gltf.DenormalizeShort(w)
			}
			return out, nil
		case []uint16:
			out := make([]float32, len(x))
			for i, w := range x {
				out[i] = gltf.DenormalizeUshort(w)
			}
			return out, nil
		default:
			return nil, fmt.Errorf("nt3dmesh: invalid weights animation accessor")
		}
	default:
		return nil, fmt.Errorf("nt3dmesh: animation accessor must be float")
	}
}
