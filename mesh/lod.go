// Copyright (c) 2026 Neomantra Corp.

package nt3dmesh

import (
	"errors"
	"fmt"

	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// ModelLOD pairs a distance threshold with a decoded Model for CombineLOD and AddLODModels.
//
// MaxDistance follows nt3d.LODLevel.MaxDistance: every non-final tier requires a
// finite positive distance strictly increasing across tiers; on the final tier,
// zero or +Inf makes it an unbounded fallback, while a finite positive distance
// hides the model beyond that distance.
type ModelLOD struct {
	MaxDistance float32
	Model       *Model
}

// LODAsset compiles sibling LOD subtrees in m.Asset (such as Mesh_LOD0,
// Mesh_LOD1, Mesh_LOD2 in a single glTF/GLB file) into a reusable
// *nt3d.SceneAsset backed by nt3d.LODLevel slices; see (*nt3d.SceneAsset).WithLOD.
//
// Call asset.Instantiate(s, parent, name) repeatedly to place thousands of
// distance-tiered props that share one set of GPU vertex/index buffers and BVH
// pick indexes per LOD tier.
func (m *Model) LODAsset(maxDistances ...float32) (*nt3d.SceneAsset, error) {
	if m == nil {
		return nil, errors.New("nt3dmesh: nil model")
	}
	if m.Asset == nil {
		return nil, errors.New("nt3dmesh: model has no hierarchy asset for LOD grouping; use CombineLOD for separate files")
	}
	return m.Asset.WithLOD(maxDistances...)
}

// AddLODTo compiles m's sibling LOD subtrees with m.LODAsset(maxDistances...)
// and instantiates the resulting hierarchy under parent as name, binding any
// material variants on the returned root node. On failure nothing is added.
func (m *Model) AddLODTo(s *nt3d.Scene, parent nt3d.NodeID, name string, maxDistances ...float32) (nt3d.NodeID, error) {
	asset, err := m.LODAsset(maxDistances...)
	if err != nil {
		return 0, err
	}
	inst, err := asset.Instantiate(s, parent, name)
	if err != nil {
		return 0, err
	}
	return inst.Root, nil
}

// CombineLOD combines separate per-tier decoded Models (such as glTF, OBJ, STL,
// 3MF, or PLY files for LOD0, LOD1, LOD2) into one reusable *nt3d.SceneAsset
// whose nodes bind nt3d.Scene.SetLOD.
//
// Models with Asset preserve their SharedGeometry pointers and named material
// variants; flat Object-only models automatically wrap triangle meshes in
// SharedGeometry so repeated Instantiate calls share GPU buffers and pick BVHs.
func CombineLOD(tiers []ModelLOD) (*nt3d.SceneAsset, error) {
	if len(tiers) == 0 {
		return nil, errors.New("nt3dmesh: empty LOD tiers")
	}
	assets := make([]nt3d.AssetLOD, len(tiers))
	for i, t := range tiers {
		if t.Model == nil {
			return nil, fmt.Errorf("nt3dmesh: nil model in LOD tier %d", i)
		}
		a := t.Model.Asset
		if a == nil {
			var err error
			a, err = objectsToSceneAsset(t.Model.Objects)
			if err != nil {
				return nil, err
			}
		}
		assets[i] = nt3d.AssetLOD{
			MaxDistance: t.MaxDistance,
			Asset:       a,
		}
	}
	return nt3d.CombineAssetLOD(assets)
}

// AddLODModels combines separate per-tier Models with CombineLOD and
// instantiates the resulting LOD asset under parent as name. On failure nothing
// is added.
func AddLODModels(s *nt3d.Scene, parent nt3d.NodeID, name string, tiers []ModelLOD) (nt3d.NodeID, error) {
	asset, err := CombineLOD(tiers)
	if err != nil {
		return 0, err
	}
	inst, err := asset.Instantiate(s, parent, name)
	if err != nil {
		return 0, err
	}
	return inst.Root, nil
}

func objectsToSceneAsset(objs []Object) (*nt3d.SceneAsset, error) {
	nodes := make([]nt3d.AssetNode, 0, len(objs))
	for i, o := range objs {
		g := o.Geometry
		name := o.Name
		if name == "" {
			name = fmt.Sprintf("part_%d", i)
		}
		an := nt3d.AssetNode{
			Name:   name,
			Parent: -1,
			Local:  nt3dmath.Identity(),
		}
		if len(g.Points)+len(g.Boxes)+len(g.Lines)+len(g.Arrows) == 0 {
			sg, err := nt3d.NewSharedGeometry(g)
			if err != nil {
				return nil, err
			}
			an.Shared = sg
		} else {
			gc := g
			an.Geometry = &gc
		}
		nodes = append(nodes, an)
	}
	return &nt3d.SceneAsset{Nodes: nodes}, nil
}
