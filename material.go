// Copyright (c) 2026 Neomantra Corp.

package nt3d

import "fmt"

// validateMaterial checks intrinsic Material invariants independent of a
// specific mesh's vertex/UV slices.
func validateMaterial(m *Material) error {
	if m == nil {
		return nil
	}
	if m.Shader != nil && !m.Shader.valid() {
		return fmt.Errorf("nt3d: invalid material shader")
	}
	if m.Texture != nil && (!m.Texture.valid() || m.Texture.data) {
		return fmt.Errorf("nt3d: invalid texture or missing UVs")
	}
	if !validFloat(m.Transmission, m.Refraction, m.Absorption.X, m.Absorption.Y, m.Absorption.Z) || m.Transmission < 0 || m.Transmission > 1 || m.Refraction < 0 || m.Refraction > 1 || m.Absorption.X < 0 || m.Absorption.Y < 0 || m.Absorption.Z < 0 {
		return fmt.Errorf("nt3d: invalid transmission, refraction, or absorption")
	}
	if m.Transmission > 0 && (m.Transparent || m.Cutout || m.EdgeOn || m.Unlit) {
		return fmt.Errorf("nt3d: transmission requires a lit, non-blended surface")
	}
	if !validFloat(m.FlowPhase, m.FlowStrength) || m.FlowStrength < 0 {
		return fmt.Errorf("nt3d: flow phase must be finite and strength nonnegative")
	}
	if m.FlowMap != nil && (m.FlowMap.texture == nil || m.NormalMap == nil) {
		return fmt.Errorf("nt3d: flow requires a valid flow map and normal map")
	}
	if !validFloat(m.NormalStrength) {
		return fmt.Errorf("nt3d: non-finite normal strength")
	}
	if m.NormalMap != nil && (!m.NormalMap.valid() || !m.NormalMap.data) {
		return fmt.Errorf("nt3d: normal map requires NewNormalMap and vertex UVs")
	}
	if !validFloat(m.Metallic, m.Roughness, m.IOR, m.Clearcoat, m.ClearcoatRoughness, m.SheenRoughness, m.OcclusionStrength, m.EmissiveIntensity) {
		return fmt.Errorf("nt3d: non-finite PBR or emissive parameter")
	}
	if m.Metallic < 0 || m.Metallic > 1 || m.Roughness < 0 || m.Roughness > 1 {
		return fmt.Errorf("nt3d: metallic and roughness must be 0..1")
	}
	if m.Clearcoat < 0 || m.Clearcoat > 1 || m.ClearcoatRoughness < 0 || m.ClearcoatRoughness > 1 || m.SheenRoughness < 0 || m.SheenRoughness > 1 {
		return fmt.Errorf("nt3d: clearcoat and sheen parameters must be 0..1")
	}
	if m.IOR != 0 && (m.IOR < 1 || m.IOR > 3) {
		return fmt.Errorf("nt3d: IOR must be 0 or 1..3")
	}
	if m.OcclusionStrength < 0 || m.OcclusionStrength > 1 {
		return fmt.Errorf("nt3d: occlusion strength must be 0..1")
	}
	if m.ORMMap != nil && (!m.ORMMap.valid() || !m.ORMMap.data) {
		return fmt.Errorf("nt3d: ORM map requires NewORMMap and vertex UVs")
	}
	if m.EmissiveIntensity < 0 {
		return fmt.Errorf("nt3d: emissive intensity must be nonnegative")
	}
	if m.EmissiveMap != nil && (!m.EmissiveMap.valid() || m.EmissiveMap.data) {
		return fmt.Errorf("nt3d: emissive map requires a color texture and vertex UVs")
	}
	if !validFloat(m.Threshold) || m.Threshold < 0 || m.Threshold > 1 {
		return fmt.Errorf("nt3d: cutout threshold must be 0..1")
	}
	if !validFloat(m.Specular, m.Shininess) || m.Specular < 0 || m.Specular > 1 || m.Shininess != 0 && (m.Shininess < 1 || m.Shininess > 1024) {
		return fmt.Errorf("nt3d: specular must be 0..1 and shininess 1..1024")
	}
	if err := m.Toon.validate(); err != nil {
		return err
	}
	if !validFloat(m.Reflectivity) || m.Reflectivity < 0 || m.Reflectivity > 1 {
		return fmt.Errorf("nt3d: reflectivity must be 0..1")
	}
	if m.Transparent && (m.Cutout || m.EdgeOn) {
		return fmt.Errorf("nt3d: a transparent material cannot be a cutout")
	}
	return nil
}

func validateMaterialForMesh(g Geometry, m *Material) error {
	if m == nil || len(g.Indices) == 0 {
		return nil
	}
	hasUV := len(g.UV) == len(g.Vertices)
	if m.Texture != nil && !hasUV {
		return fmt.Errorf("nt3d: invalid texture or missing UVs")
	}
	if m.NormalMap != nil && !hasUV {
		return fmt.Errorf("nt3d: normal map requires NewNormalMap and vertex UVs")
	}
	if m.ORMMap != nil && !hasUV {
		return fmt.Errorf("nt3d: ORM map requires NewORMMap and vertex UVs")
	}
	if m.EmissiveMap != nil && !hasUV {
		return fmt.Errorf("nt3d: emissive map requires a color texture and vertex UVs")
	}
	g.Material = m
	if extended(g) && len(g.Indices) > MeshIndexLimit()/2 {
		return fmt.Errorf("nt3d: normal-mapped mesh with tangents or independent UVs exceeds GPU binding limit")
	}
	return nil
}

func materialsEqual(a, b *Material) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func textureBindingsEqual(a, b *Material) bool {
	if a == nil && b == nil {
		return true
	}
	var za, zb Material
	if a != nil {
		za = *a
	}
	if b != nil {
		zb = *b
	}
	return za.Texture == zb.Texture &&
		za.Repeat == zb.Repeat &&
		za.Cutout == zb.Cutout &&
		za.FullSize == zb.FullSize &&
		za.NormalMap == zb.NormalMap &&
		za.NormalRepeat == zb.NormalRepeat &&
		za.ORMMap == zb.ORMMap &&
		za.ORMRepeat == zb.ORMRepeat &&
		za.EmissiveMap == zb.EmissiveMap &&
		za.EmissiveRepeat == zb.EmissiveRepeat
}

// vertexMaterialEqual reports whether old and next encode identical GPU mesh
// vertex/extension records, allowing instance-uniform and texture-binding
// changes to reuse existing uploaded mesh buffers and SharedGeometry batches.
func vertexMaterialEqual(old, next Geometry) bool {
	if mapped(old) != mapped(next) || transparent(&old) != transparent(&next) {
		return false
	}
	if !mapped(next) {
		return true
	}
	if extended(old) != extended(next) || (len(old.Tangents) > 0) != (len(next.Tangents) > 0) {
		return false
	}
	om, nm := materialOrZero(old.Material), materialOrZero(next.Material)
	if om.NormalRepeat != nm.NormalRepeat || om.Repeat != nm.Repeat {
		return false
	}
	if (om.EdgeOn && om.Texture != nil) != (nm.EdgeOn && nm.Texture != nil) {
		return false
	}
	if (om.Tint || om.Texture == nil) != (nm.Tint || nm.Texture == nil) {
		return false
	}
	if !transparent(&next) && old.Material.threshold() != next.Material.threshold() {
		return false
	}
	oldStrength, newStrength := float32(0), float32(0)
	if om.NormalMap != nil {
		oldStrength = om.NormalStrength
	}
	if nm.NormalMap != nil {
		newStrength = nm.NormalStrength
	}
	if oldStrength != newStrength {
		return false
	}
	if extended(next) {
		if om.FlowMap != nm.FlowMap || (nm.FlowMap != nil && om.FlowRepeat != nm.FlowRepeat) {
			return false
		}
	}
	return true
}

// SetMaterial replaces the material of every indexed triangle mesh in root's
// subtree (including LOD tiers). Pass nil to clear materials back to default
// untextured surface shading.
//
// Input materials are copied and validated atomically across the subtree before
// mutating any node: applying a textured, normal-mapped, ORM-mapped, or
// emissive-mapped material to a mesh without vertex UVs returns an error and
// leaves the scene unchanged.
//
// Changes confined to shader/instance uniforms or texture bindings preserve
// uploaded GPU mesh buffers, fixed-topology vertex updates, SharedGeometry
// instancing, and skeletal/morph deformations; changes to vertex-encoded
// properties (such as Transparent, Cutout/Threshold, Repeat, Tint, FlowMap, or
// adding/removing maps altogether) automatically re-encode affected mesh nodes
// and detach modified SharedGeometry instances while preserving previous Frame
// snapshots. Use Root to update the whole scene.
func (s *Scene) SetMaterial(root NodeID, m *Material) error {
	if err := validateMaterial(m); err != nil {
		return err
	}
	if s.nodes[root] == nil {
		return fmt.Errorf("nt3d: unknown material root %d", root)
	}

	// Pre-validate every affected mesh in the subtree so incompatible meshes
	// fail atomically without partial mutation.
	var visit []NodeID
	pending := []NodeID{root}
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		n := s.nodes[id]
		pending = append(pending, n.Children...)
		if len(n.lod) > 0 {
			hasMesh := false
			for i := range n.lod {
				lg := n.lod[i].geometry
				if len(lg.Indices) == 0 {
					continue
				}
				hasMesh = true
				if err := validateMaterialForMesh(*lg, m); err != nil {
					return err
				}
			}
			if hasMesh {
				visit = append(visit, id)
			}
			continue
		}
		if n.geometry == nil || len(n.geometry.Indices) == 0 {
			continue
		}
		checkGeom := *n.geometry
		if n.deformation != nil {
			checkGeom = n.deformation.base
		}
		if err := validateMaterialForMesh(checkGeom, m); err != nil {
			return err
		}
		visit = append(visit, id)
	}

	anyChanged, anyVertexChanged, anyTexChanged := false, false, false
	for _, id := range visit {
		n := s.nodes[id]
		if len(n.lod) > 0 {
			lodRebuilt := false
			for i := range n.lod {
				lg := n.lod[i].geometry
				if len(lg.Indices) == 0 || materialsEqual(lg.Material, m) {
					continue
				}
				if !textureBindingsEqual(lg.Material, m) {
					anyTexChanged = true
				}
				g := *lg
				g.Material = cloneMaterial(m)
				if m != nil && m.NormalMap != nil && len(g.Tangents) == 0 {
					generateTangentsOnUpload(&g)
				}
				if !vertexMaterialEqual(*lg, g) {
					g.meshUpdate = nil
					g.meshCert = newMeshCertificate(&g, lg.splitOutlines())
					n.lod[i].shared = nil
					n.lod[i].geometryRevision = nextRevision.Add(1)
					n.lod[i].pickIndex = nil
					lodRebuilt = true
					anyVertexChanged = true
				}
				n.lod[i].geometry = &g
				anyChanged = true
			}
			if lodRebuilt {
				retainKeys := make([]geometryKey, len(n.lod))
				for i := range n.lod {
					if n.lod[i].shared != nil {
						retainKeys[i] = geometryKey{shared: n.lod[i].shared.id}
					} else {
						retainKeys[i] = geometryKey{node: id, revision: n.lod[i].geometryRevision}
					}
				}
				for i := range n.lod {
					g := *n.lod[i].geometry
					g.retainKeys = retainKeys
					n.lod[i].geometry = &g
				}
			}
			n.geometry, n.shared, n.geometryRevision = n.lod[0].geometry, n.lod[0].shared, n.lod[0].geometryRevision
			continue
		}

		old := n.geometry
		if materialsEqual(old.Material, m) {
			continue
		}
		if !textureBindingsEqual(old.Material, m) {
			anyTexChanged = true
		}

		if n.deformation != nil {
			d := n.deformation
			nextBase := d.base
			nextBase.Material = cloneMaterial(m)
			if m != nil && m.NormalMap != nil && len(nextBase.Tangents) == 0 {
				generateTangentsOnUpload(&nextBase)
			}
			oldBase := d.base
			oldBase.Material = old.Material
			d.base = nextBase
			if !vertexMaterialEqual(oldBase, nextBase) {
				d.weights = nil
				d.revision = nextRevision.Add(1)
				g := *old
				g.Material = cloneMaterial(m)
				n.geometry = &g
				anyVertexChanged = true
			} else {
				g := *old
				g.Material = cloneMaterial(m)
				if g.posed != nil {
					p := *g.posed
					p.source.Material = g.Material
					g.posed = &p
				}
				n.geometry = &g
			}
			anyChanged = true
			continue
		}

		g := *old
		g.Material = cloneMaterial(m)
		if m != nil && m.NormalMap != nil && len(g.Tangents) == 0 {
			generateTangentsOnUpload(&g)
		}
		if !vertexMaterialEqual(*old, g) {
			g.meshUpdate = nil
			g.meshCert = newMeshCertificate(&g, old.splitOutlines())
			n.shared = nil
			n.geometryRevision = nextRevision.Add(1)
			n.pickIndex = nil
			anyVertexChanged = true
		}
		n.geometry = &g
		anyChanged = true
	}

	if !anyChanged {
		return nil
	}
	s.changed(anyVertexChanged, anyTexChanged || anyVertexChanged)
	if anyVertexChanged {
		if err := s.UpdateDeformations(); err != nil {
			return err
		}
	}
	return nil
}
