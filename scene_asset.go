// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"errors"
	"fmt"
	"slices"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// AssetNode is a reusable hierarchy node. Parent is -1 for a root, otherwise
// an earlier node index. Skeleton and clip NodeIDs are one-based node indices.
// Geometry and Shared are mutually exclusive; Shared attaches a SharedGeometry
// via Scene.SetNodeShared so repeated nodes and repeated Instantiate calls
// share one GPU mesh upload, instanced draw batch, and BVH pick index.
type AssetNode struct {
	Camera      *Camera
	Light       *Light
	Name        string
	Parent      int
	Local       nt3dmath.Mat4
	Geometry    *Geometry
	Shared      *SharedGeometry
	Deformation *Deformation
	// VariantMaterials optionally maps indices into SceneAsset.Variants to
	// alternate materials for this triangle mesh node. Unmapped variants (or
	// resetting with SetMaterialVariant(root, "")) restore the node's default
	// material from Shared or Geometry.
	VariantMaterials map[int]*Material
}

// SceneAsset is a format-independent scene template. Instantiate remaps all
// joints, track targets, and material variants so multiple copies animate and
// switch colorways independently.
type SceneAsset struct {
	Nodes    []AssetNode
	Clips    []AnimationClip
	Variants []string
}

// AnimatedInstance holds the root group, remapped node IDs, remapped animation
// clips, and material variant names created by SceneAsset.Instantiate.
type AnimatedInstance struct {
	Root     NodeID
	Nodes    []NodeID
	Clips    []AnimationClip
	Variants []string
}

type variantBinding struct {
	node          NodeID
	defaultShared *SharedGeometry
	defaultMat    *Material
	variants      map[int]*Material
}

type variantInstance struct {
	names    []string
	active   string
	bindings []variantBinding
}

// Instantiate adds a copy of the asset's hierarchy under parent with a root group named name.
func (a *SceneAsset) Instantiate(s *Scene, parent NodeID, name string) (out *AnimatedInstance, err error) {
	seenVariants := make(map[string]bool, len(a.Variants))
	for _, v := range a.Variants {
		if v == "" {
			return nil, errors.New("nt3d: asset material variant name cannot be empty")
		}
		if seenVariants[v] {
			return nil, fmt.Errorf("nt3d: duplicate asset material variant %q", v)
		}
		seenVariants[v] = true
	}
	root, err := s.Add(parent, name)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = s.Remove(root)
		}
	}()
	out = &AnimatedInstance{Root: root, Variants: slices.Clone(a.Variants)}
	var bindings []variantBinding
	for i, n := range a.Nodes {
		p := root
		if n.Parent >= i || n.Parent < -1 {
			return nil, fmt.Errorf("nt3d: invalid asset parent")
		}
		if n.Parent >= 0 {
			p = out.Nodes[n.Parent]
		}
		id, e := s.Add(p, fmt.Sprintf("%s/%d:%s", name, i, n.Name))
		if e != nil {
			return nil, e
		}
		out.Nodes = append(out.Nodes, id)
		if e = s.SetTransform(id, n.Local); e != nil {
			return nil, e
		}
		if n.Camera != nil {
			if e = s.SetNodeCamera(id, *n.Camera); e != nil {
				return nil, e
			}
		}
		if n.Light != nil {
			if e = s.SetNodeLight(id, *n.Light); e != nil {
				return nil, e
			}
		}
		if n.Shared != nil && n.Geometry != nil {
			return nil, errors.New("nt3d: asset node cannot set both Geometry and Shared")
		}
		if n.Shared != nil && n.Deformation != nil {
			return nil, errors.New("nt3d: shared asset node cannot deform")
		}
		if n.Shared != nil {
			if e = s.SetNodeShared(id, n.Shared); e != nil {
				return nil, e
			}
		} else if n.Geometry != nil {
			if e = s.SetNodeGeometry(id, *n.Geometry); e != nil {
				return nil, e
			}
		}
		if len(n.VariantMaterials) > 0 {
			var baseGeom Geometry
			var defaultMat *Material
			switch {
			case n.Shared != nil:
				baseGeom = n.Shared.geometry
				defaultMat = cloneMaterial(n.Shared.geometry.Material)
			case n.Geometry != nil && len(n.Geometry.Indices) > 0:
				baseGeom = *n.Geometry
				defaultMat = cloneMaterial(n.Geometry.Material)
			default:
				return nil, errors.New("nt3d: material variants require an indexed triangle mesh node")
			}
			vmap := make(map[int]*Material, len(n.VariantMaterials))
			for vi, vm := range n.VariantMaterials {
				if vi < 0 || vi >= len(a.Variants) {
					return nil, fmt.Errorf("nt3d: invalid asset material variant index %d", vi)
				}
				if e := validateMaterial(vm); e != nil {
					return nil, e
				}
				if e := validateMaterialForMesh(baseGeom, vm); e != nil {
					return nil, e
				}
				vmap[vi] = cloneMaterial(vm)
			}
			bindings = append(bindings, variantBinding{
				node:          id,
				defaultShared: n.Shared,
				defaultMat:    defaultMat,
				variants:      vmap,
			})
		}
	}
	remap := func(id NodeID) (NodeID, error) {
		if id == 0 || int(id) > len(out.Nodes) {
			return 0, fmt.Errorf("nt3d: invalid asset target")
		}
		return out.Nodes[int(id)-1], nil
	}
	for i, n := range a.Nodes {
		if n.Deformation == nil {
			continue
		}
		d := *n.Deformation
		if d.Skeleton != nil {
			sk := *d.Skeleton
			sk.Joints = make([]NodeID, len(d.Skeleton.Joints))
			for k, j := range d.Skeleton.Joints {
				if sk.Joints[k], err = remap(j); err != nil {
					return nil, err
				}
			}
			d.Skeleton = &sk
		}
		if err = s.SetDeformation(out.Nodes[i], d); err != nil {
			return nil, err
		}
	}
	for _, c := range a.Clips {
		clip := AnimationClip{Name: c.Name}
		for _, t := range c.Tracks {
			if t.Channel != UserChannel {
				if t.Node, err = remap(t.Node); err != nil {
					return nil, err
				}
			}
			clip.Tracks = append(clip.Tracks, t)
		}
		if err = clip.Validate(); err != nil {
			return nil, err
		}
		out.Clips = append(out.Clips, clip)
	}
	if s.clips == nil {
		s.clips = map[NodeID][]AnimationClip{}
	}
	s.clips[root] = out.Clips
	if len(out.Variants) > 0 {
		if s.variants == nil {
			s.variants = map[NodeID]*variantInstance{}
		}
		s.variants[root] = &variantInstance{
			names:    slices.Clone(out.Variants),
			bindings: bindings,
		}
	}
	return out, nil
}

// AnimationClips returns the generic clips bound to an instantiated subtree.
// Treat the returned tracks as read-only; NewAnimation makes a private copy.
func (s *Scene) AnimationClips(root NodeID) []AnimationClip {
	return append([]AnimationClip(nil), s.clips[root]...)
}

// MaterialVariants returns the named material variants bound to an instantiated
// subtree (such as glTF KHR_materials_variants), or nil when it defines none.
func (s *Scene) MaterialVariants(root NodeID) []string {
	vi := s.variants[root]
	if vi == nil {
		return nil
	}
	return slices.Clone(vi.names)
}

// MaterialVariant returns the active material variant name on root, or "" when
// root uses its default materials or defines no variants.
func (s *Scene) MaterialVariant(root NodeID) string {
	vi := s.variants[root]
	if vi == nil {
		return ""
	}
	return vi.active
}

// SetMaterialVariant applies a named material variant across an instantiated
// subtree root, or restores its authored default materials when variant is "".
// Primitives that do not map the chosen variant revert to their own default
// material, and nodes backed by SharedGeometry preserve (or rejoin) their
// shared GPU mesh buffer and BVH pick index whenever vertex-compatible.
func (s *Scene) SetMaterialVariant(root NodeID, variant string) error {
	if s.nodes[root] == nil {
		return fmt.Errorf("nt3d: unknown material variant root %d", root)
	}
	vi := s.variants[root]
	if vi == nil {
		if variant == "" {
			return nil
		}
		return fmt.Errorf("nt3d: unknown material variant %q", variant)
	}
	idx := -1
	if variant != "" {
		idx = slices.Index(vi.names, variant)
		if idx < 0 {
			return fmt.Errorf("nt3d: unknown material variant %q", variant)
		}
	}
	for _, b := range vi.bindings {
		n := s.nodes[b.node]
		if n == nil {
			continue
		}
		target := b.defaultMat
		if idx >= 0 {
			if vm, ok := b.variants[idx]; ok {
				target = vm
			}
		}
		if b.defaultShared != nil && n.shared == nil && n.deformation == nil && len(n.lod) == 0 {
			cand := b.defaultShared.geometry
			cand.Material = cloneMaterial(target)
			if vertexMaterialEqual(b.defaultShared.geometry, cand) {
				if err := s.SetNodeShared(b.node, b.defaultShared); err != nil {
					return err
				}
			}
		}
		if err := s.SetMaterial(b.node, target); err != nil {
			return err
		}
	}
	vi.active = variant
	return nil
}
