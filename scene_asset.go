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
// Geometry, Shared, and LOD are mutually exclusive; Shared and Shared-backed
// LOD levels share one GPU mesh upload, instanced draw batch, and BVH pick
// index across repeated nodes and repeated Instantiate calls.
type AssetNode struct {
	Camera      *Camera
	Light       *Light
	Name        string
	Parent      int
	Local       nt3dmath.Mat4
	Geometry    *Geometry
	Shared      *SharedGeometry
	LOD         []LODLevel
	LODBounds   nt3dmath.AABB // Optional reference AABB for synchronized multi-part LOD switching.
	Deformation *Deformation
	// VariantMaterials optionally maps indices into SceneAsset.Variants to
	// alternate materials for this triangle mesh node (or across all non-empty
	// LOD tiers when LODVariantMaterials is nil). Unmapped variants (or
	// resetting with SetMaterialVariant(root, "")) restore the node's default
	// material.
	VariantMaterials map[int]*Material
	// LODVariantMaterials optionally supplies per-tier VariantMaterials maps
	// parallel to LOD (for example when separate per-tier files carry distinct
	// texture resolutions per LOD level).
	LODVariantMaterials []map[int]*Material

	lodBoundsParent bool
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
	lodShared     []*SharedGeometry
	lodDefaults   []*Material
	lodVariants   []map[int]*Material
}

type variantInstance struct {
	names    []string
	active   string
	bindings []variantBinding
}

// Instantiate adds a copy of the asset's hierarchy under parent with a root
// group named name. The returned Nodes are the scene's nodes for the asset's
// Nodes, index for index, and are how to reach them. Node i is named
// "<name>/<i>:<asset node's name>", which keeps names unique when one asset is
// instantiated many times; the names are for people and for Find. To mark what
// an instance stands for, attach it with Scene.SetNodeData rather than writing
// it into name.
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
		hasGeom, hasShared, hasLOD := n.Geometry != nil, n.Shared != nil, len(n.LOD) > 0
		if (hasGeom && hasShared) || (hasGeom && hasLOD) || (hasShared && hasLOD) {
			return nil, errors.New("nt3d: asset node cannot set more than one of Geometry, Shared, or LOD")
		}
		if (hasShared || hasLOD) && n.Deformation != nil {
			return nil, errors.New("nt3d: shared or LOD asset node cannot deform")
		}
		if hasLOD {
			if e = s.SetLOD(id, n.LOD); e != nil {
				return nil, e
			}
			if n.LODBounds.IsValid() {
				var refNode NodeID
				if n.lodBoundsParent {
					refNode = p
				}
				if e = s.setLODRefBounds(id, refNode, n.LODBounds); e != nil {
					return nil, e
				}
			}
		} else if hasShared {
			if e = s.SetNodeShared(id, n.Shared); e != nil {
				return nil, e
			}
		} else if hasGeom {
			if e = s.SetNodeGeometry(id, *n.Geometry); e != nil {
				return nil, e
			}
		}
		if len(n.LODVariantMaterials) > 0 && !hasLOD {
			return nil, errors.New("nt3d: LODVariantMaterials requires LOD levels")
		}
		if len(n.LODVariantMaterials) > len(n.LOD) {
			return nil, errors.New("nt3d: LODVariantMaterials exceeds LOD level count")
		}
		if len(n.VariantMaterials) > 0 || len(n.LODVariantMaterials) > 0 {
			cloneAndValidateMap := func(baseGeom Geometry, src map[int]*Material) (map[int]*Material, error) {
				if len(src) == 0 {
					return nil, nil
				}
				vmap := make(map[int]*Material, len(src))
				for vi, vm := range src {
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
				return vmap, nil
			}
			if hasLOD {
				sn := s.nodes[id]
				lodShared := make([]*SharedGeometry, len(sn.lod))
				lodDefaults := make([]*Material, len(sn.lod))
				var lodVariants []map[int]*Material
				if len(n.LODVariantMaterials) > 0 {
					lodVariants = make([]map[int]*Material, len(sn.lod))
				}
				var sharedVMap map[int]*Material
				hasAnyMesh := false
				for t := range sn.lod {
					lg := *sn.lod[t].geometry
					lodShared[t] = sn.lod[t].shared
					lodDefaults[t] = cloneMaterial(lg.Material)
					if len(lg.Indices) == 0 {
						continue
					}
					hasAnyMesh = true
					if len(n.VariantMaterials) > 0 {
						vm, e := cloneAndValidateMap(lg, n.VariantMaterials)
						if e != nil {
							return nil, e
						}
						if sharedVMap == nil {
							sharedVMap = vm
						}
					}
					if t < len(n.LODVariantMaterials) && len(n.LODVariantMaterials[t]) > 0 {
						vm, e := cloneAndValidateMap(lg, n.LODVariantMaterials[t])
						if e != nil {
							return nil, e
						}
						lodVariants[t] = vm
					}
				}
				if !hasAnyMesh {
					return nil, errors.New("nt3d: material variants require an indexed triangle mesh node")
				}
				bindings = append(bindings, variantBinding{
					node:        id,
					variants:    sharedVMap,
					lodShared:   lodShared,
					lodDefaults: lodDefaults,
					lodVariants: lodVariants,
				})
			} else {
				var baseGeom Geometry
				var defaultMat *Material
				switch {
				case hasShared:
					baseGeom = n.Shared.geometry
					defaultMat = cloneMaterial(n.Shared.geometry.Material)
				case hasGeom && len(n.Geometry.Indices) > 0:
					baseGeom = *n.Geometry
					defaultMat = cloneMaterial(n.Geometry.Material)
				default:
					return nil, errors.New("nt3d: material variants require an indexed triangle mesh node")
				}
				vmap, e := cloneAndValidateMap(baseGeom, n.VariantMaterials)
				if e != nil {
					return nil, e
				}
				bindings = append(bindings, variantBinding{
					node:          id,
					defaultShared: n.Shared,
					defaultMat:    defaultMat,
					variants:      vmap,
				})
			}
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
		if len(b.lodDefaults) > 0 && len(n.lod) == len(b.lodDefaults) {
			s.applyLODVariantBinding(b.node, n, &b, idx)
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

func (s *Scene) applyLODVariantBinding(id NodeID, n *sceneNode, b *variantBinding, idx int) {
	anyChanged, anyVertexChanged, anyTexChanged, lodRebuilt := false, false, false, false
	for t := range n.lod {
		lg := n.lod[t].geometry
		if len(lg.Indices) == 0 {
			continue
		}
		target := b.lodDefaults[t]
		if idx >= 0 {
			if t < len(b.lodVariants) && b.lodVariants[t] != nil {
				if vm, ok := b.lodVariants[t][idx]; ok {
					target = vm
				}
			} else if vm, ok := b.variants[idx]; ok {
				target = vm
			}
		}
		if materialsEqual(lg.Material, target) && (b.lodShared[t] == nil || n.lod[t].shared != nil) {
			continue
		}
		if !textureBindingsEqual(lg.Material, target) {
			anyTexChanged = true
		}
		if sg := b.lodShared[t]; sg != nil {
			cand := sg.geometry
			cand.Material = cloneMaterial(target)
			cand.Outline = lg.Outline
			if vertexMaterialEqual(sg.geometry, cand) {
				cand.retainKeys = lg.retainKeys
				cand.lodTierPlusOne = t + 1
				if n.lod[t].shared == nil {
					lodRebuilt = true
					anyVertexChanged = true
				}
				n.lod[t].geometry = &cand
				n.lod[t].shared = sg
				n.lod[t].geometryRevision = sg.id
				n.lod[t].pickIndex = nil
				anyChanged = true
				continue
			}
		}
		g := *lg
		g.Material = cloneMaterial(target)
		if target != nil && target.NormalMap != nil && len(g.Tangents) == 0 {
			generateTangentsOnUpload(&g)
		}
		if !vertexMaterialEqual(*lg, g) {
			g.meshUpdate = nil
			g.meshCert = newMeshCertificate(&g, lg.splitOutlines())
			n.lod[t].shared = nil
			n.lod[t].geometryRevision = nextRevision.Add(1)
			n.lod[t].pickIndex = nil
			lodRebuilt = true
			anyVertexChanged = true
		}
		n.lod[t].geometry = &g
		anyChanged = true
	}
	if !anyChanged {
		return
	}
	if lodRebuilt {
		retainKeys := make([]geometryKey, len(n.lod))
		for t := range n.lod {
			if n.lod[t].shared != nil {
				retainKeys[t] = geometryKey{shared: n.lod[t].shared.id}
			} else {
				retainKeys[t] = geometryKey{node: id, revision: n.lod[t].geometryRevision}
			}
		}
		for t := range n.lod {
			g := *n.lod[t].geometry
			g.retainKeys = retainKeys
			n.lod[t].geometry = &g
		}
	}
	n.geometry, n.shared, n.geometryRevision = n.lod[0].geometry, n.lod[0].shared, n.lod[0].geometryRevision
	s.changed(anyVertexChanged, anyTexChanged || anyVertexChanged)
}
