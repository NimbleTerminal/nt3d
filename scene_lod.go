// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"errors"
	"fmt"
	"math"

	"nimbleterminal.dev/nt3d/math"
)

// LODLevel describes one distance tier of a node's level-of-detail chain; see Scene.SetLOD.
//
// Either Geometry or Shared supplies the tier's primitives; when Shared is
// non-nil, Geometry must be zero. A nonzero Outline overrides the tier's
// outline style without mutating Shared.
//
// MaxDistance is the maximum world-space distance from Camera.Eye to the
// node's world-space reference bounds at which this tier is drawn. Every
// non-final level requires a finite positive MaxDistance, strictly increasing
// across levels. On the final level, zero or +Inf makes it an unbounded
// fallback; a finite positive MaxDistance hides the node beyond that distance.
type LODLevel struct {
	MaxDistance float32
	Geometry    Geometry
	Shared      *SharedGeometry
	Outline     OutlineStyle
}

type lodLevel struct {
	maxDistance      float32
	geometry         *Geometry
	shared           *SharedGeometry
	geometryRevision uint64
	pickIndex        *bvhSlot
}

// SetLOD replaces a non-root node's geometry with distance-based level-of-detail
// tiers. Geometry slices and materials are copied, SharedGeometry tiers share
// GPU uploads and BVH pick indexes across instances, and switching between
// tiers retains uploaded GPU buffers across frames.
func (s *Scene) SetLOD(id NodeID, levels []LODLevel) error {
	n, ok := s.nodes[id]
	if !ok || id == Root {
		return fmt.Errorf("nt3d: invalid geometry node %d", id)
	}
	if len(levels) == 0 {
		return errors.New("nt3d: empty LOD levels")
	}
	built := make([]lodLevel, len(levels))
	for i, lvl := range levels {
		d := lvl.MaxDistance
		if math.IsNaN(float64(d)) || d < 0 {
			return fmt.Errorf("nt3d: invalid LOD distance %v", d)
		}
		maxDist := d
		if i == len(levels)-1 && (d == 0 || math.IsInf(float64(d), 1)) {
			maxDist = float32(math.Inf(1))
		} else if !nt3dmath.IsFinite(float64(d)) || d <= 0 {
			return errors.New("nt3d: non-final LOD level requires a finite positive MaxDistance")
		}
		if i > 0 && !math.IsInf(float64(maxDist), 1) && maxDist <= built[i-1].maxDistance {
			return errors.New("nt3d: LOD MaxDistance must strictly increase")
		}
		if err := lvl.Outline.validate(); err != nil {
			return err
		}
		if lvl.Shared != nil {
			if !isZeroGeometry(lvl.Geometry) {
				return errors.New("nt3d: LOD level cannot set both Geometry and Shared")
			}
			g := lvl.Shared.geometry
			if lvl.Outline != (OutlineStyle{}) {
				g.Outline = lvl.Outline
			}
			built[i] = lodLevel{maxDistance: maxDist, geometry: &g, shared: lvl.Shared, geometryRevision: lvl.Shared.id}
			continue
		}
		g, err := prepareStoredGeometry(lvl.Geometry)
		if err != nil {
			return err
		}
		if lvl.Outline != (OutlineStyle{}) {
			g.Outline = lvl.Outline
		}
		built[i] = lodLevel{maxDistance: maxDist, geometry: &g}
	}
	retainKeys := make([]geometryKey, len(built))
	var lodBounds nt3dmath.AABB
	for i := range built {
		if built[i].shared == nil {
			built[i].geometryRevision = nextRevision.Add(1)
			retainKeys[i] = geometryKey{node: id, revision: built[i].geometryRevision}
		} else {
			retainKeys[i] = geometryKey{shared: built[i].shared.id}
		}
		built[i].geometry.retainKeys = retainKeys
		if b := built[i].geometry.Bounds; b.IsValid() {
			lodBounds.Include(b.Min)
			lodBounds.Include(b.Max)
		} else if built[i].geometry.Billboard != nil {
			lodBounds.Include(nt3dmath.Vec3{})
		}
	}
	n.deformation = nil
	n.lod, n.lodBounds = built, lodBounds
	n.geometry, n.shared, n.geometryRevision, n.pickIndex = built[0].geometry, built[0].shared, built[0].geometryRevision, nil
	s.changed(true, true)
	return nil
}

func isZeroGeometry(g Geometry) bool {
	return len(g.Points) == 0 && len(g.Vertices) == 0 && len(g.Indices) == 0 &&
		len(g.Boxes) == 0 && len(g.Lines) == 0 && len(g.Arrows) == 0 &&
		len(g.UV) == 0 && len(g.NormalUV) == 0 && len(g.Tangents) == 0 &&
		len(g.GaussianSH) == 0 && len(g.GaussianSHLabels) == 0 &&
		g.GaussianSHPalette == nil && g.Material == nil && g.Billboard == nil && g.Displacement == nil &&
		g.Clusters == nil && g.LineWidth == 0 && g.WireGrid == ([2]int{}) &&
		g.PointStyle == (PointStyle{}) && g.DropShadow == (DropShadow{}) &&
		g.Outline == (OutlineStyle{}) && g.GaussianSHTransform == (nt3dmath.Mat4{}) &&
		!g.Bounds.IsValid()
}

func aabbDistance(b nt3dmath.AABB, p nt3dmath.Vec3) float32 {
	dx := max(b.Min.X-p.X, 0, p.X-b.Max.X)
	dy := max(b.Min.Y-p.Y, 0, p.Y-b.Max.Y)
	dz := max(b.Min.Z-p.Z, 0, p.Z-b.Max.Z)
	return float32(math.Sqrt(float64(dx)*float64(dx) + float64(dy)*float64(dy) + float64(dz)*float64(dz)))
}

func (n *sceneNode) selectLOD(world nt3dmath.Mat4, eye nt3dmath.Vec3) (*lodLevel, bool) {
	var dist float32
	if box := n.lodBounds.Transformed(world); box.IsValid() {
		dist = aabbDistance(box, eye)
	} else {
		d := world.Point(nt3dmath.Vec3{}).Sub(eye)
		dist = float32(math.Sqrt(float64(d.X)*float64(d.X) + float64(d.Y)*float64(d.Y) + float64(d.Z)*float64(d.Z)))
	}
	for i := range n.lod {
		if dist <= n.lod[i].maxDistance {
			return &n.lod[i], true
		}
	}
	return &n.lod[len(n.lod)-1], false
}

func (n *sceneNode) pickTarget(rev uint64) (*Geometry, **bvhSlot) {
	if len(n.lod) > 0 {
		for i := range n.lod {
			l := &n.lod[i]
			if l.geometryRevision == rev {
				if l.shared != nil {
					return l.geometry, &l.shared.pick
				}
				return l.geometry, &l.pickIndex
			}
		}
		return nil, nil
	}
	if n.geometry == nil || n.geometryRevision != rev {
		return nil, nil
	}
	if n.shared != nil {
		return n.geometry, &n.shared.pick
	}
	return n.geometry, &n.pickIndex
}
