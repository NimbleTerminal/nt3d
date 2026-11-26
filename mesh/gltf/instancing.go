// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"errors"
	"fmt"
	"strings"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"

	"nimbleterminal.dev/nt3d/mesh"
)

type nodeInstances struct {
	transforms []mat4
	active     bool
}

// nodeInstances decodes EXT_mesh_gpu_instancing on doc.Nodes[nodeIndex].
// When the extension is absent, active is false. When present, transforms holds
// each instance's local TRS transform (empty when attributes are empty or count is 0).
func (d *gltfDecoder) nodeInstances(nodeIndex int) ([]mat4, bool, error) {
	if d.nodeInst != nil {
		if entry, ok := d.nodeInst[nodeIndex]; ok {
			return entry.transforms, entry.active, nil
		}
	}
	n := d.doc.Nodes[nodeIndex]
	if n == nil {
		return nil, false, errors.New("nt3dmesh: nil glTF node")
	}
	var ext struct {
		Attributes map[string]*int `json:"attributes"`
	}
	ok, err := decodeExtension(n.Extensions, "EXT_mesh_gpu_instancing", &ext)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	if n.Skin != nil {
		return nil, false, errors.New("nt3dmesh: EXT_mesh_gpu_instancing does not support skinned meshes")
	}
	cache := func(transforms []mat4) ([]mat4, bool, error) {
		if d.nodeInst == nil {
			d.nodeInst = map[int]nodeInstances{}
		}
		d.nodeInst[nodeIndex] = nodeInstances{transforms: transforms, active: true}
		return transforms, true, nil
	}
	if len(ext.Attributes) == 0 {
		return cache(nil)
	}

	count := -1
	maxCount := min(maxVisits, 3*d.o.TriangleLimit()+3)
	checkAttr := func(name string, idx *int) (*gltf.Accessor, error) {
		if idx == nil || *idx < 0 || *idx >= len(d.doc.Accessors) || d.doc.Accessors[*idx] == nil {
			return nil, fmt.Errorf("nt3dmesh: EXT_mesh_gpu_instancing %s names invalid accessor", name)
		}
		a := d.doc.Accessors[*idx]
		if a.Count < 0 || a.Count > maxCount {
			return nil, fmt.Errorf("%w: EXT_mesh_gpu_instancing %s has %d elements", nt3dmesh.ErrTooManyTriangles, name, a.Count)
		}
		if int64(a.Count)*64 > d.o.ByteLimit() {
			return nil, fmt.Errorf("%w: EXT_mesh_gpu_instancing %s", nt3dmesh.ErrTooLarge, name)
		}
		if count < 0 {
			count = a.Count
		} else if a.Count != count {
			return nil, errors.New("nt3dmesh: EXT_mesh_gpu_instancing attribute counts differ")
		}
		return a, nil
	}

	var transAcc, rotAcc, scaleAcc *gltf.Accessor
	for key, idx := range ext.Attributes {
		a, err := checkAttr(key, idx)
		if err != nil {
			return nil, false, err
		}
		switch {
		case key == "TRANSLATION":
			transAcc = a
		case key == "ROTATION":
			rotAcc = a
		case key == "SCALE":
			scaleAcc = a
		case strings.HasPrefix(key, "_"):
			// Custom per-instance attribute allowed by EXT_mesh_gpu_instancing.
		default:
			return nil, false, fmt.Errorf("nt3dmesh: unknown EXT_mesh_gpu_instancing attribute %q", key)
		}
	}
	if count <= 0 {
		return cache(nil)
	}
	if err := d.decoded.Take(int64(count)*64, "EXT_mesh_gpu_instancing instances decoded"); err != nil {
		return nil, false, err
	}

	var translations [][3]float32
	if transAcc != nil {
		translations, err = modeler.ReadPosition(d.doc, transAcc, nil)
		if err != nil {
			return nil, false, fmt.Errorf("nt3dmesh: EXT_mesh_gpu_instancing TRANSLATION: %w", err)
		}
	}
	var rotations [][4]float32
	if rotAcc != nil {
		rotations, err = d.readInstanceRotations(rotAcc)
		if err != nil {
			return nil, false, err
		}
	}
	var scales [][3]float32
	if scaleAcc != nil {
		scales, err = modeler.ReadPosition(d.doc, scaleAcc, nil)
		if err != nil {
			return nil, false, fmt.Errorf("nt3dmesh: EXT_mesh_gpu_instancing SCALE: %w", err)
		}
	}

	transforms := make([]mat4, count)
	for k := range count {
		t := [3]float64{0, 0, 0}
		if translations != nil {
			v := translations[k]
			if !nt3dmesh.ValidCoordinate(v[0], v[1], v[2]) {
				return nil, false, errors.New("nt3dmesh: non-finite EXT_mesh_gpu_instancing translation")
			}
			t = [3]float64{float64(v[0]), float64(v[1]), float64(v[2])}
		}
		q := [4]float64{0, 0, 0, 1}
		if rotations != nil {
			v := rotations[k]
			if !nt3dmesh.ValidCoordinate(v[0], v[1], v[2], v[3]) {
				return nil, false, errors.New("nt3dmesh: non-finite EXT_mesh_gpu_instancing rotation")
			}
			q = [4]float64{float64(v[0]), float64(v[1]), float64(v[2]), float64(v[3])}
			if q[0]*q[0]+q[1]*q[1]+q[2]*q[2]+q[3]*q[3] == 0 {
				return nil, false, errors.New("nt3dmesh: zero EXT_mesh_gpu_instancing rotation quaternion")
			}
		}
		s := [3]float64{1, 1, 1}
		if scales != nil {
			v := scales[k]
			if !nt3dmesh.ValidCoordinate(v[0], v[1], v[2]) || v[0] == 0 || v[1] == 0 || v[2] == 0 {
				return nil, false, errors.New("nt3dmesh: invalid EXT_mesh_gpu_instancing scale")
			}
			s = [3]float64{float64(v[0]), float64(v[1]), float64(v[2])}
		}
		inst := trsMat4(t, q, s)
		if !inst.IsFinite() {
			return nil, false, errors.New("nt3dmesh: non-finite EXT_mesh_gpu_instancing transform")
		}
		if _, invOK := inst.Mat4().InverseAffine(); !invOK {
			return nil, false, errors.New("nt3dmesh: singular EXT_mesh_gpu_instancing transform")
		}
		transforms[k] = inst
	}
	return cache(transforms)
}

func (d *gltfDecoder) readInstanceRotations(a *gltf.Accessor) ([][4]float32, error) {
	if a.Type != gltf.AccessorVec4 {
		return nil, errors.New("nt3dmesh: EXT_mesh_gpu_instancing ROTATION must be VEC4")
	}
	switch a.ComponentType {
	case gltf.ComponentFloat:
		if a.Normalized {
			return nil, errors.New("nt3dmesh: EXT_mesh_gpu_instancing float ROTATION must not be normalized")
		}
	case gltf.ComponentByte, gltf.ComponentShort:
		if !a.Normalized {
			return nil, errors.New("nt3dmesh: EXT_mesh_gpu_instancing integer ROTATION must be normalized")
		}
	default:
		return nil, fmt.Errorf("nt3dmesh: unsupported EXT_mesh_gpu_instancing ROTATION component type %v", a.ComponentType)
	}
	raw, err := modeler.ReadAccessor(d.doc, a, nil)
	if err != nil {
		return nil, fmt.Errorf("nt3dmesh: EXT_mesh_gpu_instancing ROTATION: %w", err)
	}
	out := make([][4]float32, a.Count)
	switch data := raw.(type) {
	case [][4]float32:
		copy(out, data)
	case [][4]int8:
		for i, v := range data {
			out[i] = [4]float32{
				gltf.DenormalizeByte(v[0]),
				gltf.DenormalizeByte(v[1]),
				gltf.DenormalizeByte(v[2]),
				gltf.DenormalizeByte(v[3]),
			}
		}
	case [][4]int16:
		for i, v := range data {
			out[i] = [4]float32{
				gltf.DenormalizeShort(v[0]),
				gltf.DenormalizeShort(v[1]),
				gltf.DenormalizeShort(v[2]),
				gltf.DenormalizeShort(v[3]),
			}
		}
	default:
		return nil, errors.New("nt3dmesh: unexpected EXT_mesh_gpu_instancing ROTATION data")
	}
	return out, nil
}
