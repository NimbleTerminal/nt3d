// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image/color"
	"nimbleterminal.dev/nt3d/math"
	"slices"
)

// MaxArrows is the maximum number of arrows in a single Geometry.
const MaxArrows = 100_000

// Arrow is an unlit screen-facing stroke. Width and HeadSize are pixels.
// Datum is an application-defined picking identifier.
type Arrow struct {
	Start, End      nt3dmath.Vec3
	Width, HeadSize float32
	Color           color.RGBA
	Datum           int
}

func validFloat(v ...float32) bool {
	for _, x := range v {
		if !nt3dmath.IsFinite(float64(x)) {
			return false
		}
	}
	return true
}
func finiteVec(v nt3dmath.Vec3) bool { return validFloat(v.X, v.Y, v.Z) }
func validWidth(w float32) bool      { return validFloat(w) && (w == 0 || w >= 1 && w <= 32) }
func lineWidth(w float32) float32 {
	if w == 0 {
		return 1
	}
	return w
}
func components(v nt3dmath.Vec3) [3]float32 { return [3]float32{v.X, v.Y, v.Z} }
func vector(v [3]float32) nt3dmath.Vec3     { return nt3dmath.Vec3{X: v[0], Y: v[1], Z: v[2]} }
func lerpRGBA(a, b color.RGBA, t float64) color.RGBA {
	mix := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t + .5) }
	return color.RGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), mix(a.A, b.A)}
}

// Clone returns a copy of g that can be changed without changing g: every
// slice is copied, and so are the Material and Billboard. What a geometry
// only points at and never changes, its textures, shaders, Displacement,
// Clusters, and GaussianSHPalette, is shared.
//
// A scene clones what it is given, so Clone is for an application that keeps
// geometry of its own to edit, or hands a geometry from a frame back to a
// scene after changing it.
func (g Geometry) Clone() Geometry {
	g.Points = slices.Clone(g.Points)
	g.GaussianSH = slices.Clone(g.GaussianSH)
	g.GaussianSHLabels = slices.Clone(g.GaussianSHLabels)
	g.Vertices = slices.Clone(g.Vertices)
	g.Indices = slices.Clone(g.Indices)
	g.Boxes = slices.Clone(g.Boxes)
	g.Lines = slices.Clone(g.Lines)
	g.Arrows = slices.Clone(g.Arrows)
	g.UV = slices.Clone(g.UV)
	g.NormalUV = slices.Clone(g.NormalUV)
	g.LightMapUV = slices.Clone(g.LightMapUV)
	g.Tangents = slices.Clone(g.Tangents)
	g.Material = cloneMaterial(g.Material)
	if g.Billboard != nil {
		b := *g.Billboard
		g.Billboard = &b
	}
	return g
}

// Validate checks geometry before it is used by a renderer.
func (g Geometry) Validate() error { return validateGeometry(g) }

// GeometryBounds computes bounds from primitives; caller-supplied bounds are not trusted.
// When g.Displacement is set on a mesh with vertices, the vertex AABB is padded
// conservatively by ±g.Displacement.MaxDisplacement().
func GeometryBounds(g Geometry) nt3dmath.AABB {
	padded, _ := geometryBoundsPair(g)
	return padded
}

func geometryBoundsPair(g Geometry) (padded, rest nt3dmath.AABB) {
	for _, p := range g.Points {
		rest.Include(p.Position)
	}
	var mesh nt3dmath.AABB
	for _, v := range g.Vertices {
		mesh.Include(v.Position)
	}
	for _, v := range g.Lines {
		rest.Include(v.Position)
	}
	for _, a := range g.Arrows {
		rest.Include(a.Start)
		rest.Include(a.End)
	}
	for _, v := range g.Boxes {
		rest.Include(v.Min)
		rest.Include(v.Min.Add(v.Size))
	}
	padded = rest
	if mesh.IsValid() {
		rest.Include(mesh.Min)
		rest.Include(mesh.Max)
		meshPadded := padGeometryBounds(mesh, true, g.Displacement.MaxDisplacement())
		padded.Include(meshPadded.Min)
		padded.Include(meshPadded.Max)
	}
	return padded, rest
}

// Clip returns geometry clipped to a local-space box. It may share unchanged
// input storage, and drops Clusters when it removes points. Recompile selection metadata if mesh triangle indices matter.
func Clip(g Geometry, b nt3dmath.AABB) (Geometry, error) {
	if err := g.Validate(); err != nil {
		return Geometry{}, err
	}
	if !b.IsValid() || !finiteVec(b.Min) || !finiteVec(b.Max) || b.Min.X > b.Max.X || b.Min.Y > b.Max.Y || b.Min.Z > b.Max.Z {
		return Geometry{}, errBounds
	}
	g.Bounds = GeometryBounds(g)
	out := clipGeometry(g, b)
	out.Bounds = GeometryBounds(out)
	return out, out.Validate()
}
