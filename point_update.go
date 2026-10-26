// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"nimbleterminal.dev/nt3d/clod"
	"nimbleterminal.dev/nt3d/math"
	"slices"
)

// pointUpdate identifies an immutable, validated snapshot and its changed range.
// It holds no previous snapshot, so animation cannot retain a chain of frames.
type pointUpdate struct {
	revision, previous uint64
	first, end         int
	points             []Point
	style              PointStyle
}

// SetNodePoints replaces an existing geometry's points without changing their
// count or style. Only points are copied and checked; other primitives are shared
// with previous snapshots. bounds must enclose ALL the geometry's local primitive
// positions (point radii are handled separately by culling). Construct it with
// AABB.Include. A fixed conservative bound avoids a bounds pass each frame.
// Use SetNodeGeometry when changing the point count/style or a deformed mesh.
// The updated geometry has no Clusters, which describe the old positions.
func (s *Scene) SetNodePoints(id NodeID, points []Point, bounds nt3dmath.AABB) error {
	n := s.nodes[id]
	if n == nil || n.geometry == nil || len(points) != len(n.geometry.Points) {
		return fmt.Errorf("nt3d: SetNodePoints requires an existing geometry with the same point count")
	}
	return s.UpdateNodePoints(id, 0, points, bounds)
}

// UpdateNodePoints replaces a fixed range starting at first. It preserves old
// frame snapshots by copying only the point slice. The GPU uploads this range
// when it rendered the preceding update, or the whole point buffer if frames
// were skipped. bounds has the same contract as SetNodePoints. The updated
// geometry has no Clusters, which describe the old positions.
func (s *Scene) UpdateNodePoints(id NodeID, first int, points []Point, bounds nt3dmath.AABB) error {
	n := s.nodes[id]
	if id == Root || n == nil || n.geometry == nil || n.shared != nil || n.deformation != nil {
		return fmt.Errorf("nt3d: invalid point update node %d", id)
	}
	old := n.geometry
	if first < 0 || first > len(old.Points) || len(points) > len(old.Points)-first {
		return fmt.Errorf("nt3d: point update range out of bounds")
	}
	if !bounds.IsValid() || !finiteVec(bounds.Min) || !finiteVec(bounds.Max) || bounds.Min.X > bounds.Max.X || bounds.Min.Y > bounds.Max.Y || bounds.Min.Z > bounds.Max.Z {
		return fmt.Errorf("nt3d: point update requires finite bounds")
	}
	for _, p := range points {
		if !finiteVec(p.Position) || !validPointRadius(old.PointStyle.Units, p.Radius) || !validGaussian(old.PointStyle, p) || !contains(bounds, p.Position) {
			return fmt.Errorf("nt3d: invalid point or point outside supplied bounds")
		}
	}
	if len(points) == 0 {
		return nil
	}
	g := *old
	g.Clusters, g.stored = nil, nil
	if first == 0 && len(points) == len(old.Points) {
		g.Points = slices.Clone(points)
	} else {
		g.Points = slices.Clone(old.Points)
		copy(g.Points[first:], points)
	}
	previous := uint64(0)
	if old.pointUpdate != nil {
		previous = old.pointUpdate.revision
	}
	s.changed(false, false)
	g.pointUpdate = &pointUpdate{s.stateRevision, previous, first, first + len(points), g.Points, g.PointStyle}
	g.Bounds = bounds
	n.geometry, n.pickIndex = &g, nil
	return nil
}

// storedGeometry certifies the points, style and clusters a scene stored
// after Validate passed on them, as pointUpdate certifies an update's.
type storedGeometry struct {
	points   []Point
	style    PointStyle
	clusters *nt3dclod.Set
}

// certifies reports whether points and style are the very ones validated.
func certifies(points, validated []Point, style, validatedStyle PointStyle) bool {
	return len(points) == len(validated) && (len(points) == 0 || &points[0] == &validated[0]) && style == validatedStyle
}

func validateFrameGeometry(g Geometry) error {
	// Scene-owned arrays are immutable by contract. Public Geometry.Validate
	// still checks every point; manually constructed frames have no certificate.
	n := len(g.Points)
	u, c := g.pointUpdate, g.stored
	stored := c != nil && certifies(g.Points, c.points, g.PointStyle, c.style)
	if stored || u != nil && n > 0 && certifies(g.Points, u.points, g.PointStyle, u.style) {
		if err := validateGaussianSH(g); err != nil {
			return err
		}
		g.Points = nil
		g.GaussianSH = nil
		g.GaussianSHTransform = nt3dmath.Mat4{}
	}
	if g.Clusters != nil {
		// A stored set is trusted as long as it still describes n points: one
		// Rebuilt in place over other positions is checked again.
		if !stored || g.Clusters != c.clusters || len(g.Clusters.Order) != n {
			if err := validateClusterBounds(g.Clusters, n); err != nil {
				return err
			}
		}
		g.Clusters = nil
	}
	return g.Validate()
}
