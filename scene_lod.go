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
// node's world-space reference bounds at which this tier is drawn, scaled by
// Scene.LODBias and stabilized across frames by Scene.LODHysteresis. Every
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
	geometry         *sceneGeometry
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
		built[i].geometry.lodTierPlusOne = i + 1
		if b := built[i].geometry.Bounds; b.IsValid() {
			lodBounds.Include(b.Min)
			lodBounds.Include(b.Max)
		} else if built[i].geometry.Billboard != nil {
			lodBounds.Include(nt3dmath.Vec3{})
		}
	}
	n.deformation = nil
	n.lod, n.lodBounds, n.lodRefBounds, n.lodRefNode, n.lodDispPad, n.lodEpoch, n.lodTier = built, lodBounds, nt3dmath.AABB{}, 0, 0, 0, 0
	n.geometry, n.shared, n.geometryRevision, n.pickIndex = built[0].geometry, built[0].shared, built[0].geometryRevision, nil
	s.changed(true, true)
	return nil
}

func (s *Scene) setLODRefBounds(id, refNode NodeID, b nt3dmath.AABB) error {
	n, ok := s.nodes[id]
	if !ok || len(n.lod) == 0 {
		return fmt.Errorf("nt3d: node %d has no LOD levels", id)
	}
	if !b.IsValid() || !validFloat(b.Min.X, b.Min.Y, b.Min.Z, b.Max.X, b.Max.Y, b.Max.Z) {
		return errors.New("nt3d: invalid LOD reference bounds")
	}
	n.lodRefBounds = b
	n.lodRefNode = refNode
	n.lodEpoch = 0
	return nil
}

// SetLODReferenceBounds makes a node measure its LOD distance from b instead of
// from the bounds of its own tiers, so several nodes given the same b switch
// tiers together at the same camera distance. Use it for the parts of one
// chunk (terrain, roads, buildings) whose tiers must never disagree.
//
// b is expressed in the node's local space and is carried to the world by the
// node's own world transform, like the bounds it replaces. Nodes that share b
// therefore share a reference box only when they share a world transform, for
// example siblings under one parent with identical local transforms; give each
// node bounds in its own frame otherwise. b must be finite and valid, which
// means built with nt3dmath.AABB.Include (a struct literal is empty).
//
// The node must already have LOD levels from Scene.SetLOD, and calling SetLOD
// again clears the reference bounds. Per-node LOD hysteresis memory is reset,
// so the next frame selects a tier without hysteresis. Scene.LODBias and
// Scene.LODHysteresis apply as usual. Errors leave the node unchanged.
func (s *Scene) SetLODReferenceBounds(id NodeID, b nt3dmath.AABB) error {
	// refNode 0: measure through the node's own world transform.
	if err := s.setLODRefBounds(id, 0, b); err != nil {
		return err
	}
	s.changed(false, false)
	return nil
}

func (s *Scene) lodBiasValue() float32 {
	if s.lodBias == 0 {
		return 1
	}
	return s.lodBias
}

func (s *Scene) lodEpochValue() uint64 {
	if s.lodBiasEpoch == 0 {
		return 1
	}
	return s.lodBiasEpoch
}

// LODBias returns the scene-wide multiplier applied to every LODLevel.MaxDistance
// during Scene.Frame (1 by default).
func (s *Scene) LODBias() float32 { return s.lodBiasValue() }

// SetLODBias sets the scene-wide multiplier applied to every LODLevel.MaxDistance
// (including finite final hide-beyond cutoffs) on subsequent Scene.Frame calls.
//
// f must be finite and > 0 (default 1); invalid values return an error and leave
// the bias unchanged. Setting the bias runs in O(1) without rebuilding node
// geometries or invalidating GPU/BVH caches, and resets per-node LOD hysteresis
// when the bias changes so frame-budget steps take effect immediately.
func (s *Scene) SetLODBias(f float32) error {
	if !nt3dmath.IsFinite(float64(f)) || f <= 0 {
		return fmt.Errorf("nt3d: invalid LOD bias %v", f)
	}
	if f == s.lodBiasValue() {
		return nil
	}
	if s.lodBiasEpoch == 0 {
		s.lodBiasEpoch = 1
	}
	s.lodBias = f
	s.lodBiasEpoch++
	s.changed(false, false)
	return nil
}

// LODHysteresis returns the fractional deadband in [0, 0.5] used to suppress
// tier popping across Scene.SetLOD boundaries (0 by default).
func (s *Scene) LODHysteresis() float32 { return s.lodHysteresis }

// SetLODHysteresis sets the fractional deadband in [0, 0.5] applied around each
// effective LODLevel.MaxDistance boundary D (including finite hide-beyond
// cutoffs): moving to a coarser tier requires distance > D*(1+band), and moving
// to a finer tier requires distance < D*(1-band).
//
// Zero (the default) disables hysteresis. First frames, SetLOD, re-showing a
// hidden node via SetVisible, and SetLODBias changes select without hysteresis.
// Invalid values return an error and leave the hysteresis band unchanged.
func (s *Scene) SetLODHysteresis(band float32) error {
	if !nt3dmath.IsFinite(float64(band)) || band < 0 || band > 0.5 {
		return fmt.Errorf("nt3d: invalid LOD hysteresis %v", band)
	}
	if s.lodHysteresis != band {
		s.lodHysteresis = band
		s.changed(false, false)
	}
	return nil
}

func isZeroGeometry(g Geometry) bool {
	return len(g.Points) == 0 && len(g.Vertices) == 0 && len(g.Indices) == 0 &&
		len(g.Boxes) == 0 && len(g.Lines) == 0 && len(g.Arrows) == 0 &&
		len(g.UV) == 0 && len(g.NormalUV) == 0 && len(g.LightMapUV) == 0 && len(g.Tangents) == 0 &&
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

func effectiveLODDistance(maxDist, bias float32) float32 {
	if bias == 1 || math.IsInf(float64(maxDist), 1) {
		return maxDist
	}
	return float32(float64(maxDist) * float64(bias))
}

func hysteresisUpper(d, band float32) float32 {
	if band == 0 || math.IsInf(float64(d), 1) {
		return d
	}
	scale := float64(1.0 + float64(band))
	return float32(float64(d) * scale)
}

func hysteresisLower(d, band float32) float32 {
	if band == 0 || math.IsInf(float64(d), 1) {
		return d
	}
	scale := float64(1.0 - float64(band))
	return float32(float64(d) * scale)
}

// selectLOD chooses the level to draw from eye. An interactive frame holds
// the level its node drew last inside the hysteresis band, and remembers the
// one it chooses; a still chooses by distance alone and remembers nothing.
func (n *sceneNode) selectLOD(world nt3dmath.Mat4, eye nt3dmath.Vec3, bias, band float32, epoch uint64, visible, still bool) (*lodLevel, bool) {
	var dist float32
	refBounds := n.lodBounds
	if n.lodRefBounds.IsValid() {
		refBounds = n.lodRefBounds
		if n.lodDispPad > 0 {
			p := nt3dmath.Vec3{X: n.lodDispPad, Y: n.lodDispPad, Z: n.lodDispPad}
			refBounds.Include(n.lodRefBounds.Min.Sub(p))
			refBounds.Include(n.lodRefBounds.Max.Add(p))
		}
	}
	if box := refBounds.Transformed(world); box.IsValid() {
		dist = aabbDistance(box, eye)
	} else {
		d := world.Point(nt3dmath.Vec3{}).Sub(eye)
		dist = float32(math.Sqrt(float64(d.X)*float64(d.X) + float64(d.Y)*float64(d.Y) + float64(d.Z)*float64(d.Z)))
	}
	state := len(n.lod)
	if !still && visible && band > 0 && n.lodEpoch == epoch {
		cur := n.lodTier
		movedCoarser := false
		for cur < len(n.lod) && dist > hysteresisUpper(effectiveLODDistance(n.lod[cur].maxDistance, bias), band) {
			cur++
			movedCoarser = true
		}
		if !movedCoarser {
			for cur > 0 && dist < hysteresisLower(effectiveLODDistance(n.lod[cur-1].maxDistance, bias), band) {
				cur--
			}
		}
		state = cur
	} else {
		for i := range n.lod {
			if dist <= effectiveLODDistance(n.lod[i].maxDistance, bias) {
				state = i
				break
			}
		}
	}
	switch {
	case still:
	case visible:
		n.lodTier = state
		n.lodEpoch = epoch
	default:
		n.lodEpoch = 0
	}
	if state < len(n.lod) {
		return &n.lod[state], true
	}
	return &n.lod[len(n.lod)-1], false
}

func (n *sceneNode) pickTarget(rev uint64) (*Geometry, **bvhSlot) {
	if len(n.lod) > 0 {
		for i := range n.lod {
			l := &n.lod[i]
			if l.geometryRevision == rev {
				if l.shared != nil {
					return &l.geometry.Geometry, &l.shared.pick
				}
				return &l.geometry.Geometry, &l.pickIndex
			}
		}
		return nil, nil
	}
	if n.geometry == nil || n.geometryRevision != rev {
		return nil, nil
	}
	if n.shared != nil {
		return &n.geometry.Geometry, &n.shared.pick
	}
	return &n.geometry.Geometry, &n.pickIndex
}
