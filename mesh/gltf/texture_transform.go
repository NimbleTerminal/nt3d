// Copyright (c) 2026 Neomantra Corp.
package nt3dgltf

import (
	"encoding/json"
	"fmt"
	"github.com/qmuntal/gltf"
	"math"
)

type textureMapping struct {
	coord         int
	offset, scale [2]float64
	rotation      float64
}

func readMapping(coord int, extensions gltf.Extensions) (textureMapping, error) {
	m := textureMapping{coord: coord, scale: [2]float64{1, 1}}
	if raw, ok := extensions["KHR_texture_transform"]; ok {
		data, err := json.Marshal(raw)
		if err != nil {
			return m, err
		}
		var ext struct {
			Offset, Scale []float64
			Rotation      float64
			TexCoord      *int
		}
		if err = json.Unmarshal(data, &ext); err != nil {
			return m, fmt.Errorf("nt3dmesh: texture transform: %w", err)
		}
		if ext.Offset != nil {
			if len(ext.Offset) != 2 {
				return m, fmt.Errorf("nt3dmesh: texture offset needs two values")
			}
			copy(m.offset[:], ext.Offset)
		}
		if ext.Scale != nil {
			if len(ext.Scale) != 2 {
				return m, fmt.Errorf("nt3dmesh: texture scale needs two values")
			}
			copy(m.scale[:], ext.Scale)
		}
		m.rotation = ext.Rotation
		if ext.TexCoord != nil {
			m.coord = *ext.TexCoord
		}
	}
	if m.coord < 0 || !validCoordinate(float32(m.offset[0]), float32(m.offset[1]), float32(m.scale[0]), float32(m.scale[1]), float32(m.rotation)) {
		return m, fmt.Errorf("nt3dmesh: invalid texture transform")
	}
	return m, nil
}
func (m textureMapping) apply(v [2]float32) ([2]float32, error) {
	if !validCoordinate(v[0], v[1]) {
		return v, fmt.Errorf("nt3dmesh: non-finite texture coordinate")
	}
	x, y := float64(v[0])*m.scale[0], float64(v[1])*m.scale[1]
	c, s := math.Cos(m.rotation), math.Sin(m.rotation)
	out := [2]float32{float32(m.offset[0] + c*x - s*y), float32(m.offset[1] + s*x + c*y)}
	if !validCoordinate(out[0], out[1]) {
		return out, fmt.Errorf("nt3dmesh: transformed UV exceeds coordinate limits")
	}
	return out, nil
}
