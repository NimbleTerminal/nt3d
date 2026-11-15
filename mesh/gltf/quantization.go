// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"errors"
	"fmt"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// readPositions reads a VEC3 POSITION accessor as float32 coordinates,
// honoring KHR_mesh_quantization signed and unsigned 8/16-bit integer types
// (both normalized and unnormalized).
func (d *gltfDecoder) readPositions(a *gltf.Accessor) ([][3]float32, error) {
	if a.Type != gltf.AccessorVec3 {
		return nil, fmt.Errorf("POSITION accessor type %s is not VEC3", a.Type)
	}
	switch a.ComponentType {
	case gltf.ComponentFloat:
		if a.Normalized {
			return nil, errors.New("float POSITION accessor must not be normalized")
		}
	case gltf.ComponentByte, gltf.ComponentUbyte, gltf.ComponentShort, gltf.ComponentUshort:
	default:
		return nil, fmt.Errorf("unsupported POSITION component type %v", a.ComponentType)
	}
	raw, err := modeler.ReadAccessor(d.doc, a, nil)
	if err != nil {
		return nil, err
	}
	return convertVec3(raw, a.Normalized)
}

// readNormals reads a VEC3 NORMAL accessor as float32 vectors, honoring
// KHR_mesh_quantization normalized signed 8/16-bit integer types.
func (d *gltfDecoder) readNormals(a *gltf.Accessor) ([][3]float32, error) {
	if a.Type != gltf.AccessorVec3 {
		return nil, fmt.Errorf("NORMAL accessor type %s is not VEC3", a.Type)
	}
	switch a.ComponentType {
	case gltf.ComponentFloat:
		if a.Normalized {
			return nil, errors.New("float NORMAL accessor must not be normalized")
		}
	case gltf.ComponentByte, gltf.ComponentShort:
		if !a.Normalized {
			return nil, errors.New("integer NORMAL accessor must be normalized")
		}
	default:
		return nil, fmt.Errorf("unsupported NORMAL component type %v", a.ComponentType)
	}
	raw, err := modeler.ReadAccessor(d.doc, a, nil)
	if err != nil {
		return nil, err
	}
	return convertVec3(raw, a.Normalized)
}

// readTangents reads a VEC4 TANGENT accessor as float32 vectors, honoring
// KHR_mesh_quantization normalized signed 8/16-bit integer types.
func (d *gltfDecoder) readTangents(a *gltf.Accessor) ([][4]float32, error) {
	if a.Type != gltf.AccessorVec4 {
		return nil, fmt.Errorf("TANGENT accessor type %s is not VEC4", a.Type)
	}
	switch a.ComponentType {
	case gltf.ComponentFloat:
		if a.Normalized {
			return nil, errors.New("float TANGENT accessor must not be normalized")
		}
	case gltf.ComponentByte, gltf.ComponentShort:
		if !a.Normalized {
			return nil, errors.New("integer TANGENT accessor must be normalized")
		}
	default:
		return nil, fmt.Errorf("unsupported TANGENT component type %v", a.ComponentType)
	}
	raw, err := modeler.ReadAccessor(d.doc, a, nil)
	if err != nil {
		return nil, err
	}
	switch v := raw.(type) {
	case [][4]float32:
		return v, nil
	case [][4]int8:
		out := make([][4]float32, len(v))
		for i, c := range v {
			out[i] = [4]float32{
				gltf.DenormalizeByte(c[0]),
				gltf.DenormalizeByte(c[1]),
				gltf.DenormalizeByte(c[2]),
				gltf.DenormalizeByte(c[3]),
			}
		}
		return out, nil
	case [][4]int16:
		out := make([][4]float32, len(v))
		for i, c := range v {
			out[i] = [4]float32{
				gltf.DenormalizeShort(c[0]),
				gltf.DenormalizeShort(c[1]),
				gltf.DenormalizeShort(c[2]),
				gltf.DenormalizeShort(c[3]),
			}
		}
		return out, nil
	default:
		return nil, errors.New("unexpected TANGENT accessor data")
	}
}

// readTexCoords reads a VEC2 TEXCOORD_n accessor as float32 UVs, honoring
// core glTF normalized UBYTE/USHORT and KHR_mesh_quantization signed/unsigned
// 8/16-bit integer types (both normalized and unnormalized).
func (d *gltfDecoder) readTexCoords(a *gltf.Accessor) ([][2]float32, error) {
	if a.Type != gltf.AccessorVec2 {
		return nil, fmt.Errorf("TEXCOORD accessor type %s is not VEC2", a.Type)
	}
	switch a.ComponentType {
	case gltf.ComponentFloat:
		if a.Normalized {
			return nil, errors.New("float TEXCOORD accessor must not be normalized")
		}
	case gltf.ComponentByte, gltf.ComponentUbyte, gltf.ComponentShort, gltf.ComponentUshort:
	default:
		return nil, fmt.Errorf("unsupported TEXCOORD component type %v", a.ComponentType)
	}
	raw, err := modeler.ReadAccessor(d.doc, a, nil)
	if err != nil {
		return nil, err
	}
	switch v := raw.(type) {
	case [][2]float32:
		return v, nil
	case [][2]int8:
		out := make([][2]float32, len(v))
		if a.Normalized {
			for i, c := range v {
				out[i] = [2]float32{gltf.DenormalizeByte(c[0]), gltf.DenormalizeByte(c[1])}
			}
		} else {
			for i, c := range v {
				out[i] = [2]float32{float32(c[0]), float32(c[1])}
			}
		}
		return out, nil
	case [][2]uint8:
		out := make([][2]float32, len(v))
		if a.Normalized {
			for i, c := range v {
				out[i] = [2]float32{gltf.DenormalizeUbyte(c[0]), gltf.DenormalizeUbyte(c[1])}
			}
		} else {
			for i, c := range v {
				out[i] = [2]float32{float32(c[0]), float32(c[1])}
			}
		}
		return out, nil
	case [][2]int16:
		out := make([][2]float32, len(v))
		if a.Normalized {
			for i, c := range v {
				out[i] = [2]float32{gltf.DenormalizeShort(c[0]), gltf.DenormalizeShort(c[1])}
			}
		} else {
			for i, c := range v {
				out[i] = [2]float32{float32(c[0]), float32(c[1])}
			}
		}
		return out, nil
	case [][2]uint16:
		out := make([][2]float32, len(v))
		if a.Normalized {
			for i, c := range v {
				out[i] = [2]float32{gltf.DenormalizeUshort(c[0]), gltf.DenormalizeUshort(c[1])}
			}
		} else {
			for i, c := range v {
				out[i] = [2]float32{float32(c[0]), float32(c[1])}
			}
		}
		return out, nil
	default:
		return nil, errors.New("unexpected TEXCOORD accessor data")
	}
}

// readMorphDeltas reads a morph target's POSITION, NORMAL, or TANGENT VEC3
// delta accessor, honoring KHR_mesh_quantization component rules.
func (d *gltfDecoder) readMorphDeltas(a *gltf.Accessor, key string) ([][3]float32, error) {
	if a.Type != gltf.AccessorVec3 {
		return nil, fmt.Errorf("nt3dmesh: morph %s accessor must be VEC3", key)
	}
	switch a.ComponentType {
	case gltf.ComponentFloat:
		if a.Normalized {
			return nil, fmt.Errorf("nt3dmesh: float morph %s accessor must not be normalized", key)
		}
	case gltf.ComponentByte, gltf.ComponentShort:
		if key != gltf.POSITION && !a.Normalized {
			return nil, fmt.Errorf("nt3dmesh: integer morph %s accessor must be normalized", key)
		}
	default:
		return nil, fmt.Errorf("nt3dmesh: unsupported morph %s component type %v", key, a.ComponentType)
	}
	raw, err := modeler.ReadAccessor(d.doc, a, nil)
	if err != nil {
		return nil, err
	}
	out, err := convertVec3(raw, a.Normalized)
	if err != nil {
		return nil, err
	}
	for _, c := range out {
		if !validCoordinate(c[0], c[1], c[2]) {
			return nil, fmt.Errorf("nt3dmesh: non-finite morph %s delta", key)
		}
	}
	return out, nil
}

func convertVec3(raw any, normalized bool) ([][3]float32, error) {
	switch v := raw.(type) {
	case [][3]float32:
		return v, nil
	case [][3]int8:
		out := make([][3]float32, len(v))
		if normalized {
			for i, c := range v {
				out[i] = [3]float32{gltf.DenormalizeByte(c[0]), gltf.DenormalizeByte(c[1]), gltf.DenormalizeByte(c[2])}
			}
		} else {
			for i, c := range v {
				out[i] = [3]float32{float32(c[0]), float32(c[1]), float32(c[2])}
			}
		}
		return out, nil
	case [][3]uint8:
		out := make([][3]float32, len(v))
		if normalized {
			for i, c := range v {
				out[i] = [3]float32{gltf.DenormalizeUbyte(c[0]), gltf.DenormalizeUbyte(c[1]), gltf.DenormalizeUbyte(c[2])}
			}
		} else {
			for i, c := range v {
				out[i] = [3]float32{float32(c[0]), float32(c[1]), float32(c[2])}
			}
		}
		return out, nil
	case [][3]int16:
		out := make([][3]float32, len(v))
		if normalized {
			for i, c := range v {
				out[i] = [3]float32{gltf.DenormalizeShort(c[0]), gltf.DenormalizeShort(c[1]), gltf.DenormalizeShort(c[2])}
			}
		} else {
			for i, c := range v {
				out[i] = [3]float32{float32(c[0]), float32(c[1]), float32(c[2])}
			}
		}
		return out, nil
	case [][3]uint16:
		out := make([][3]float32, len(v))
		if normalized {
			for i, c := range v {
				out[i] = [3]float32{gltf.DenormalizeUshort(c[0]), gltf.DenormalizeUshort(c[1]), gltf.DenormalizeUshort(c[2])}
			}
		} else {
			for i, c := range v {
				out[i] = [3]float32{float32(c[0]), float32(c[1]), float32(c[2])}
			}
		}
		return out, nil
	default:
		return nil, errors.New("unexpected VEC3 accessor data")
	}
}
