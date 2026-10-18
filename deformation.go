// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"slices"
)

// MorphTarget contains object-space deltas, one per vertex. Normals may be omitted.
type MorphTarget struct{ Positions, Normals []nt3dmath.Vec3 }

// Influence is a vertex's four joint indices and nonnegative weights.
// SetDeformation normalizes weights. A zero sum leaves the vertex unskinned.
type Influence struct {
	Joints  [4]uint32
	Weights [4]float32
}

// Skeleton references scene nodes, including joints outside the mesh subtree.
// InverseBind maps bind mesh coordinates into joint coordinates; nil means identity.
type Skeleton struct {
	Joints      []NodeID
	InverseBind []nt3dmath.Mat4
}
type Deformation struct {
	Morphs     []MorphTarget
	Weights    []float32
	Skeleton   *Skeleton
	Influences []Influence
}
type deformationState struct {
	morphed        []Vertex
	sourceRevision uint64
	base           Geometry
	definition     Deformation
	palette        []nt3dmath.Mat4
	weights        []float32
	revision       uint64
}

// posedGeometry is immutable metadata for the GPU deformation path.
type posedGeometry struct {
	sourceRevision uint64
	source         Geometry
	influences     []Influence
	palette        []nt3dmath.Mat4
	identity       uint64
	revision       uint64
}

// SetDeformation binds the node's current geometry as its rest mesh. Inputs are copied.
// Each instance may bind the same rest geometry to a different skeleton.
func (s *Scene) SetDeformation(id NodeID, d Deformation) error {
	n := s.nodes[id]
	if n == nil || n.geometry == nil {
		return fmt.Errorf("nt3d: deformation requires geometry")
	}
	count := len(n.geometry.Vertices)
	if len(d.Weights) != 0 && len(d.Weights) != len(d.Morphs) {
		return fmt.Errorf("nt3d: morph weights mismatch")
	}
	d.Morphs = slices.Clone(d.Morphs)
	d.Weights = slices.Clone(d.Weights)
	if len(d.Weights) == 0 {
		d.Weights = make([]float32, len(d.Morphs))
	}
	for i, m := range d.Morphs {
		if len(m.Positions) != 0 && len(m.Positions) != count || len(m.Normals) != 0 && len(m.Normals) != count {
			return fmt.Errorf("nt3d: morph vertex count mismatch")
		}
		for _, vs := range [][]nt3dmath.Vec3{m.Positions, m.Normals} {
			for _, v := range vs {
				if !finiteVec(v) {
					return fmt.Errorf("nt3d: non-finite morph")
				}
			}
		}
		d.Morphs[i] = MorphTarget{slices.Clone(m.Positions), slices.Clone(m.Normals)}
	}
	for _, w := range d.Weights {
		if !validFloat(w) {
			return fmt.Errorf("nt3d: invalid morph weight")
		}
	}
	if d.Skeleton != nil {
		sk := *d.Skeleton
		sk.Joints = slices.Clone(sk.Joints)
		sk.InverseBind = slices.Clone(sk.InverseBind)
		if len(sk.Joints) == 0 || len(sk.Joints) > 65536 || len(d.Influences) != count {
			return fmt.Errorf("nt3d: invalid skeleton or influences")
		}
		if len(sk.InverseBind) == 0 {
			for range sk.Joints {
				sk.InverseBind = append(sk.InverseBind, nt3dmath.Identity())
			}
		}
		if len(sk.InverseBind) != len(sk.Joints) {
			return fmt.Errorf("nt3d: inverse bind count mismatch")
		}
		for i, j := range sk.Joints {
			if s.nodes[j] == nil {
				return fmt.Errorf("nt3d: missing joint")
			}
			if _, ok := sk.InverseBind[i].InverseAffine(); !ok {
				return fmt.Errorf("nt3d: invalid inverse bind")
			}
		}
		d.Skeleton = &sk
		d.Influences = slices.Clone(d.Influences)
		for i, in := range d.Influences {
			sum := float32(0)
			for k, w := range in.Weights {
				if !validFloat(w) || w < 0 || int(in.Joints[k]) >= len(sk.Joints) {
					return fmt.Errorf("nt3d: invalid joint influence")
				}
				sum += w
			}
			if !validFloat(sum) {
				return fmt.Errorf("nt3d: influence overflow")
			}
			if sum > 0 {
				for k := range in.Weights {
					d.Influences[i].Weights[k] /= sum
				}
			}
		}
	} else if len(d.Influences) > 0 {
		return fmt.Errorf("nt3d: influences require a skeleton")
	}
	base := cloneGeometry(*n.geometry)
	base.posed = nil
	old := n.deformation
	n.deformation = &deformationState{base: base, definition: d, revision: nextRevision.Add(1)}
	if err := s.UpdateDeformations(); err != nil {
		n.deformation = old
		return err
	}
	return nil
}
func (s *Scene) MorphWeights(id NodeID) ([]float32, error) {
	n := s.nodes[id]
	if n == nil || n.deformation == nil {
		return nil, fmt.Errorf("nt3d: node has no deformation")
	}
	return slices.Clone(n.deformation.definition.Weights), nil
}
func (s *Scene) SetMorphWeights(id NodeID, w []float32) error {
	n := s.nodes[id]
	if n == nil || n.deformation == nil || len(w) != len(n.deformation.definition.Morphs) {
		return fmt.Errorf("nt3d: morph weight count mismatch")
	}
	for _, v := range w {
		if !validFloat(v) {
			return fmt.Errorf("nt3d: invalid morph weight")
		}
	}
	if !slices.Equal(w, n.deformation.definition.Weights) {
		n.deformation.definition.Weights = slices.Clone(w)
		s.changed(false, false)
	}
	return nil
}

// UpdateDeformations evaluates changed palettes/weights only. Frame calls it
// automatically; call it explicitly before querying Bounds after procedural edits.
func (s *Scene) UpdateDeformations() error {
	for _, id := range s.order {
		n := s.nodes[id]
		d := n.deformation
		if d == nil {
			continue
		}
		def := d.definition
		var palette []nt3dmath.Mat4
		if sk := def.Skeleton; sk != nil {
			inv, ok := s.world(id).InverseAffine()
			if !ok {
				return fmt.Errorf("nt3d: singular mesh transform")
			}
			palette = make([]nt3dmath.Mat4, len(sk.Joints))
			for i, j := range sk.Joints {
				if s.nodes[j] == nil {
					return fmt.Errorf("nt3d: skeleton joint removed")
				}
				palette[i] = inv.Mul(s.world(j)).Mul(sk.InverseBind[i])
				if _, ok := palette[i].InverseAffine(); !ok {
					return fmt.Errorf("nt3d: invalid joint palette")
				}
			}
		}
		if d.weights != nil && slices.Equal(d.weights, def.Weights) && slices.Equal(d.palette, palette) {
			continue
		}
		weightsDirty := d.weights == nil || !slices.Equal(d.weights, def.Weights)
		source := d.base
		source.Material = n.geometry.Material
		source.Outline = n.geometry.Outline
		if !weightsDirty {
			source.Vertices = d.morphed
		} else if len(def.Morphs) > 0 {
			source.Vertices = slices.Clone(source.Vertices)
			for i := range source.Vertices {
				v := &source.Vertices[i]
				for k, m := range def.Morphs {
					w := def.Weights[k]
					if w == 0 {
						continue
					}
					if len(m.Positions) > 0 {
						v.Position = v.Position.Add(m.Positions[i].Scale(w))
					}
					if len(m.Normals) > 0 {
						v.Normal = v.Normal.Add(m.Normals[i].Scale(w))
					}
				}
			}
		}
		posed := source
		posed.Vertices = slices.Clone(source.Vertices)
		for i, v := range source.Vertices {
			if len(palette) > 0 {
				in := def.Influences[i]
				var p, normal nt3dmath.Vec3
				sum := float32(0)
				for k, w := range in.Weights {
					if w == 0 {
						continue
					}
					m := palette[in.Joints[k]]
					p = p.Add(m.Point(v.Position).Scale(w))
					normal = normal.Add(m.Direction(v.Normal).Scale(w))
					sum += w
				}
				if sum > 0 {
					v.Position, v.Normal = p, normal
				}
			}
			if v.Normal.Dot(v.Normal) > 0 {
				v.Normal = v.Normal.Normalize()
			}
			posed.Vertices[i] = v
		}
		posed.Bounds = GeometryBounds(posed)
		if err := posed.Validate(); err != nil {
			return err
		}
		s.changed(true, false)
		d.morphed = source.Vertices
		if weightsDirty {
			d.sourceRevision = s.revision
		}
		d.weights = slices.Clone(def.Weights)
		if d.weights == nil {
			d.weights = []float32{}
		}
		d.palette = palette
		if len(palette) > 0 {
			posed.posed = &posedGeometry{sourceRevision: d.sourceRevision, source: source, influences: def.Influences, palette: palette, identity: d.revision, revision: s.revision}
		}
		n.geometry = &posed
		n.shared = nil
		n.pickIndex = nil
		n.geometryRevision = s.revision
	}
	return nil
}
