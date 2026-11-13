// Copyright (c) 2026 Neomantra Corp.
package nt3d

// FrameStats summarizes what renderers draw for a Frame after visibility,
// frustum culling, shared-geometry instancing, and splat budgeting.
type FrameStats struct {
	// Instances is the total number of geometry entries in Frame (len(Frame.Geometry)).
	Instances int
	// VisibleInstances is the number of entries whose Instance.Visible is true
	// (after Scene.SetVisible, Scene.SetLOD distance cutoffs, and billboard depth checks).
	VisibleInstances int
	// DrawnInstances is the number of non-empty visible entries that remain after
	// frustum culling when Frame.Cull is true.
	DrawnInstances int
	// Batches is the number of geometry draw groups across DrawnInstances: opaque
	// shared instances with matching outline and textures batch together, while
	// unshared or blended instances each form their own group.
	Batches int
	// Triangles is the total mesh and box triangle count across DrawnInstances
	// (3 indices per indexed triangle, 3 vertices per unindexed triangle, and 12 per Box).
	Triangles int
	// Lines is the total line segment and arrow count across DrawnInstances plus
	// world-space segments in Frame.DynamicLines.
	Lines int
	// Points is the total point and Gaussian splat count selected for drawing across
	// DrawnInstances, honoring Frame.SplatBudget on clustered alpha-blended clouds.
	Points int
}

func hasPrimitives(g *Geometry) bool {
	return len(g.Vertices) > 0 || len(g.Boxes) > 0 || len(g.Lines) > 0 || len(g.Arrows) > 0 || len(g.Points) > 0
}

func geometryTriangles(g *Geometry) int {
	tris := len(g.Boxes) * 12
	if len(g.Indices) > 0 {
		tris += len(g.Indices) / 3
	} else {
		tris += len(g.Vertices) / 3
	}
	return tris
}

// Stats reports the instances, batches, and primitives that renderers select
// for drawing from f, using the same visibility, frustum-culling, batching, and
// splat-budget rules as WebGPU and Software. A frame with invalid dimensions or
// mismatched slices reports zeros.
func (f Frame) Stats() FrameStats {
	if f.Width < 1 || f.Height < 1 ||
		(len(f.Instances) != 0 && len(f.Instances) != len(f.Geometry)) ||
		(len(f.Shared) != 0 && len(f.Shared) != len(f.Geometry)) ||
		(len(f.GeometryRevisions) != 0 && (len(f.GeometryRevisions) != len(f.Geometry) || len(f.Instances) != len(f.Geometry))) ||
		!validFloat(f.Matrix[:]...) ||
		len(f.DynamicLines)%2 != 0 {
		return FrameStats{}
	}
	type batchKey struct {
		geometry geometryKey
		outline  OutlineStyle
		texture  *Texture
		normal   *Texture
	}
	drawn := f.drawn(1)
	stats := FrameStats{
		Instances: len(f.Geometry),
		Lines:     len(f.DynamicLines) / 2,
	}
	var batches map[batchKey]struct{}
	nonAlphaPoints := 0
	hasAlphaPoints := false
	for i := range f.Geometry {
		if instance(f, i).Visible {
			stats.VisibleInstances++
		}
		if !drawn[i] {
			continue
		}
		geom := &f.Geometry[i]
		if !hasPrimitives(geom) {
			continue
		}
		stats.DrawnInstances++
		stats.Triangles += geometryTriangles(geom)
		stats.Lines += len(geom.Lines)/2 + len(geom.Arrows)
		if len(geom.Points) > 0 {
			if blendedPoints(geom) && geom.PointStyle.Blend == AlphaBlend {
				hasAlphaPoints = true
			} else {
				nonAlphaPoints += len(geom.Points)
			}
		}
		if transparent(geom) || blendedPoints(geom) {
			stats.Batches++
			continue
		}
		if batches == nil {
			batches = make(map[batchKey]struct{})
		}
		k := batchKey{geometry: frameGeometryKey(f, i), outline: geom.Outline}
		if mapped(*geom) {
			k.texture = baseMap(geom.Material)
			k.normal = normalMap(geom.Material)
		}
		if _, ok := batches[k]; !ok {
			batches[k] = struct{}{}
			stats.Batches++
		}
	}
	if hasAlphaPoints {
		plan, err := f.planBlend(drawn)
		if err != nil {
			return FrameStats{}
		}
		stats.Points = nonAlphaPoints + f.planLOD(&plan).drawn
	} else {
		stats.Points = nonAlphaPoints
	}
	return stats
}
