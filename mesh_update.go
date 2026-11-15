// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"fmt"
	"slices"
	"unsafe"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// meshUpdate identifies a validated fixed-topology vertex/tangent update on an
// existing scene node, allowing GPU renderers to overwrite the mesh buffer in
// place without reallocating buffers or bind groups.
type meshUpdate struct {
	revision uint64
}

// meshCertificate identifies immutable mesh slices already validated on upload
// or during a fixed-topology update so per-frame validation skips O(V+I) scans.
type meshCertificate struct {
	vertices       []Vertex
	indices        []uint32
	uv, normalUV   []UV
	tangents       []Tangent
	certifiedSplit bool
	splitOutlines  bool
}

func sameSlice[T any](a, b []T) bool {
	return len(a) == len(b) && (len(a) == 0 || unsafe.SliceData(a) == unsafe.SliceData(b))
}

func newMeshCertificate(g *Geometry, splitOutlines bool) meshCertificate {
	return meshCertificate{
		vertices:       g.Vertices,
		indices:        g.Indices,
		uv:             g.UV,
		normalUV:       g.NormalUV,
		tangents:       g.Tangents,
		certifiedSplit: true,
		splitOutlines:  splitOutlines,
	}
}

func (c meshCertificate) certifies(g *Geometry) bool {
	return c.certifiedSplit &&
		sameSlice(g.Vertices, c.vertices) &&
		sameSlice(g.Indices, c.indices) &&
		sameSlice(g.UV, c.uv) &&
		sameSlice(g.NormalUV, c.normalUV) &&
		sameSlice(g.Tangents, c.tangents)
}

func (g Geometry) splitOutlines() bool {
	if g.meshCert.certifiedSplit && sameSlice(g.Vertices, g.meshCert.vertices) {
		return g.meshCert.splitOutlines
	}
	return true
}

func hasDuplicatePositions(vertices []Vertex) bool {
	if len(vertices) < 2 {
		return false
	}
	seen := make(map[nt3dmath.Vec3]struct{}, len(vertices))
	for _, v := range vertices {
		if _, ok := seen[v.Position]; ok {
			return true
		}
		seen[v.Position] = struct{}{}
	}
	return false
}

// SetNodeVertices replaces all vertices of an existing single-mesh geometry
// without changing its vertex count, triangle topology, UVs, or material.
// Unchanged slices are shared with previous snapshots, and GPU renderers update
// the existing mesh buffer in place. bounds must be finite and enclose ALL the
// geometry's local primitive positions. If the mesh already carries tangent
// frames, they are updated to match the new vertices; call
// SetNodeVerticesWithTangents when supplying analytic tangents directly.
func (s *Scene) SetNodeVertices(id NodeID, vertices []Vertex, bounds nt3dmath.AABB) error {
	n := s.nodes[id]
	if n == nil || n.geometry == nil || len(n.geometry.Indices) == 0 || len(vertices) != len(n.geometry.Vertices) {
		return fmt.Errorf("nt3d: SetNodeVertices requires an existing indexed mesh with the same vertex count")
	}
	return s.UpdateNodeVertices(id, 0, vertices, bounds)
}

// UpdateNodeVertices replaces a fixed vertex range starting at first on an
// existing single-mesh geometry, preserving previous Frame snapshots by copying
// only the vertex slice (and recomputing tangent frames when the mesh already
// carries them). bounds must be finite and enclose ALL the geometry's local
// primitive positions. Use SetNodeGeometry when changing vertex/index counts,
// shared geometry, LOD chains, or skeletal/morph deformations.
func (s *Scene) UpdateNodeVertices(id NodeID, first int, vertices []Vertex, bounds nt3dmath.AABB) error {
	n := s.nodes[id]
	if id == Root || n == nil || n.geometry == nil || n.shared != nil || n.deformation != nil || len(n.lod) > 0 || len(n.geometry.Indices) == 0 {
		return fmt.Errorf("nt3d: invalid vertex update node %d", id)
	}
	old := n.geometry
	if first < 0 || first > len(old.Vertices) || len(vertices) > len(old.Vertices)-first {
		return fmt.Errorf("nt3d: vertex update range out of bounds")
	}
	if !bounds.IsValid() || !finiteVec(bounds.Min) || !finiteVec(bounds.Max) || bounds.Min.X > bounds.Max.X || bounds.Min.Y > bounds.Max.Y || bounds.Min.Z > bounds.Max.Z {
		return fmt.Errorf("nt3d: vertex update requires finite bounds")
	}
	for _, v := range vertices {
		if !finiteVec(v.Position) || !finiteVec(v.Normal) || !contains(bounds, v.Position) {
			return fmt.Errorf("nt3d: invalid vertex or vertex outside supplied bounds")
		}
	}
	if len(vertices) == 0 {
		return nil
	}
	g := *old
	if first == 0 && len(vertices) == len(old.Vertices) {
		g.Vertices = slices.Clone(vertices)
	} else {
		g.Vertices = slices.Clone(old.Vertices)
		copy(g.Vertices[first:], vertices)
	}
	if len(old.Tangents) > 0 {
		if tangents, err := vertexTangents(g); err == nil {
			g.Tangents = tangents
		} else {
			g.Tangents = slices.Clone(old.Tangents)
			for i := first; i < first+len(vertices); i++ {
				nrm := g.Vertices[i].Normal
				dir := g.Tangents[i].Direction
				proj := dir.Sub(nrm.Scale(nrm.Dot(dir)))
				if proj.Dot(proj) > 1e-12 {
					g.Tangents[i].Direction = proj.Normalize()
				} else {
					g.Tangents[i].Direction = perpendicular(nrm)
				}
			}
		}
	}
	s.changed(false, false)
	g.restBounds = bounds
	g.Bounds = padGeometryBounds(bounds, len(g.Vertices) > 0, g.Displacement.MaxDisplacement())
	g.meshUpdate = &meshUpdate{revision: s.stateRevision}
	g.meshCert = newMeshCertificate(&g, old.splitOutlines())
	n.geometry, n.pickIndex = &g, nil
	return nil
}

// SetNodeVerticesWithTangents replaces all vertices and tangent frames of an
// existing single-mesh geometry in place without running MikkTSpace tangent
// generation or reallocating GPU mesh buffers. Pass nil/empty tangents only
// when the existing mesh has no tangent frames; use SetNodeGeometry to add or
// remove tangent frames.
func (s *Scene) SetNodeVerticesWithTangents(id NodeID, vertices []Vertex, tangents []Tangent, bounds nt3dmath.AABB) error {
	n := s.nodes[id]
	if id == Root || n == nil || n.geometry == nil || n.shared != nil || n.deformation != nil || len(n.lod) > 0 || len(n.geometry.Indices) == 0 {
		return fmt.Errorf("nt3d: invalid vertex update node %d", id)
	}
	old := n.geometry
	if len(vertices) != len(old.Vertices) {
		return fmt.Errorf("nt3d: SetNodeVerticesWithTangents requires the same vertex count")
	}
	if (len(tangents) > 0) != (len(old.Tangents) > 0) || (len(tangents) > 0 && len(tangents) != len(vertices)) {
		return fmt.Errorf("nt3d: tangent presence and length must match existing mesh")
	}
	if !bounds.IsValid() || !finiteVec(bounds.Min) || !finiteVec(bounds.Max) || bounds.Min.X > bounds.Max.X || bounds.Min.Y > bounds.Max.Y || bounds.Min.Z > bounds.Max.Z {
		return fmt.Errorf("nt3d: vertex update requires finite bounds")
	}
	for _, v := range vertices {
		if !finiteVec(v.Position) || !finiteVec(v.Normal) || !contains(bounds, v.Position) {
			return fmt.Errorf("nt3d: invalid vertex or vertex outside supplied bounds")
		}
	}
	for _, t := range tangents {
		if !finiteVec(t.Direction) || t.W != 1 && t.W != -1 {
			return fmt.Errorf("nt3d: invalid tangent")
		}
	}
	g := *old
	g.Vertices = slices.Clone(vertices)
	if len(tangents) > 0 {
		g.Tangents = slices.Clone(tangents)
	}
	s.changed(false, false)
	g.restBounds = bounds
	g.Bounds = padGeometryBounds(bounds, len(g.Vertices) > 0, g.Displacement.MaxDisplacement())
	g.meshUpdate = &meshUpdate{revision: s.stateRevision}
	g.meshCert = newMeshCertificate(&g, old.splitOutlines())
	n.geometry, n.pickIndex = &g, nil
	return nil
}
