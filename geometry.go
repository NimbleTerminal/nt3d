// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"fmt"
	"image/color"

	"nimbleterminal.dev/nt3d/clod"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/sh"
)

type Point struct {
	// GaussianAxes are the three rotated one-sigma axes in local coordinates.
	// Used only by GaussianPoint; affine transforms scale and rotate these axes.
	GaussianAxes [3]nt3dmath.Vec3
	Frame        uint32 // Atlas frame offset, added to PointStyle.Frame (row-major, wrapping).
	Position     nt3dmath.Vec3
	Radius       float32
	Color        color.RGBA
	Datum        int
}

type Vertex struct {
	Position, Normal nt3dmath.Vec3
	Color            color.RGBA
}

// Tangent is a vertex's tangent frame for normal mapping: the direction
// in which the normal map's U grows along the surface, and W, +1 or -1,
// its handedness: the bitangent is W × (normal × Direction), as glTF
// defines it. A zero Direction perturbs nothing at that vertex.
type Tangent struct {
	Direction nt3dmath.Vec3
	W         float32
}

type Box struct {
	Min, Size nt3dmath.Vec3
	Color     color.RGBA
	Datum     int
}

// DropShadow lays a soft dark disc under a geometry, on the plane at the
// bottom of its world bounds (or FixedFloor at FloorZ), centered under them: as if lit from
// straight above by nothing in particular. It needs no light. Strength is
// how dark the disc's center is, 0..1; zero means no drop shadow. Radius
// is the disc's radius in world units; zero means the bounds' larger
// horizontal half extent. Every instance of a shared geometry gets one.
// Wireframe and picking ignore it.
type DropShadow struct {
	Strength float32
	Radius   float32
	// FixedFloor places the disc at world-space FloorZ instead of the bounds bottom.
	// The plane is not transformed with the instance; geometry still supplies its XY center.
	FixedFloor bool
	FloorZ     float32
}

// Geometry contains points, indexed triangles, boxes, line pairs, and arrow instances.
// Scene setters copy geometry; frame consumers must treat snapshots as read-only.
type Geometry struct {
	pointUpdate *pointUpdate
	posed       *posedGeometry
	Billboard   *Billboard // Optional camera-facing transform, resolved by Scene.Frame.

	// WireGrid optionally describes a row-major rectangular vertex lattice (NX, NY).
	// Wireframe draws viewport-adaptive rows/columns instead of triangle edges.
	// BuildHeightfield sets this automatically; clear it if changing mesh topology.
	WireGrid [2]int

	NormalUV []UV // Optional independent normal-map UVs; nil uses UV.
	// Tangents optionally gives each vertex its tangent frame for normal
	// mapping. Nil generates them when the geometry is set on a scene, or
	// falls back to a per-triangle basis: see GenerateTangents.
	Tangents []Tangent
	UV       []UV      // One per vertex when textured.
	Material *Material // Applies to indexed triangles only.
	Points   []Point
	// GaussianSH optionally holds 1, 4, 9, or 16 RGB coefficients per point,
	// point-major, in nt3dsh ordering. PLY coefficients include DC, but not the
	// 0.5 display offset. Nil uses Point.Color. Alpha remains Point.Color.A.
	GaussianSH []nt3dsh.RGB
	// GaussianSHTransform maps local view rays to the SH coordinate frame.
	// Zero means identity. TransformGeometry preserves this frame exactly,
	// including under nonuniform scale, without reprojecting coefficients.
	GaussianSHTransform nt3dmath.Mat4
	// PointStyle is how Points are drawn: their shape, the units of their
	// radii, and how they blend.
	PointStyle PointStyle
	// Clusters groups the points for level of detail: the renderers draw a
	// budgeted prefix of each cluster's progressive order (see
	// Frame.SplatBudget). Decoders build it for Gaussian clouds;
	// nt3dclod.Build makes one. Point updates and geometry operations drop
	// it, since it describes the positions it was built from.
	Clusters   *nt3dclod.Set
	Vertices   []Vertex
	Indices    []uint32
	Boxes      []Box
	Lines      []Vertex
	LineWidth  float32 // Render pixels for Lines; zero defaults to 1.
	Arrows     []Arrow
	Bounds     nt3dmath.AABB
	DropShadow DropShadow
	Outline    OutlineStyle
}

// MaxMeshIndices is the most indices one geometry's mesh may have, three to a
// triangle. The GPU renderer keeps a 48-byte record for each index, vertices
// shared or not, in a storage buffer; 128 MiB is the largest binding that
// WebGPU promises, and so the largest mesh that can be drawn wherever WebGPU
// can. MaxMeshTriangles is the same limit, counted in faces. MaxPoints is
// the same limit for points, whose records are 20 bytes; textured points keep
// a 48-byte record too. Gaussian splats keep an axis record and spherical
// harmonics as well, compact by default (24-byte axes, one byte a
// coefficient channel) and float with PointStyle.Precise (48-byte axes, 12
// bytes a coefficient), so those geometries hold fewer: see MaxGaussians and
// MaxPreciseGaussians.
const (
	MaxMeshTriangles = (128 << 20) / 48 / 3
	MaxMeshIndices   = 3 * MaxMeshTriangles
	MaxPoints        = (128 << 20) / 20
)

// bindingLimit is the storage binding WebGPU guarantees, in bytes, that
// Validate measures a geometry's buffers against. Tests lower it.
var bindingLimit = 128 << 20

// MaxGaussians is the most Gaussian splats one geometry holds with that many
// RGB spherical harmonic coefficients each (0 for none; else 1, 4, 9, or 16,
// degrees 0 to 3), in the default compact records: each buffer must fit the
// binding, the 24-byte axis records and the SH, three bytes a coefficient.
// Other counts give 0. PointStyle.Precise keeps float records and holds
// fewer: see MaxPreciseGaussians.
func MaxGaussians(coefficients int) int {
	most := min(MaxPoints, (128<<20)/24)
	switch coefficients {
	case 0:
		return most
	case 1, 4, 9, 16:
		return min(most, (128<<20)/(3*coefficients))
	}
	return 0
}

// MaxPreciseGaussians is MaxGaussians for a geometry with PointStyle.Precise:
// the 48-byte axis records and the SH, 48 bytes plus 12 a coefficient, must
// each fit the binding.
func MaxPreciseGaussians(coefficients int) int {
	most := min(MaxPoints, (128<<20)/48)
	switch coefficients {
	case 0:
		return most
	case 1, 4, 9, 16:
		return min(most, ((128<<20)-48)/(12*coefficients))
	}
	return 0
}

// validateClusters checks that a set describes n points: its Order holds each
// index once, and its clusters tile Order in sequence.
func validateClusters(s *nt3dclod.Set, n int) error {
	if len(s.Order) != n {
		return fmt.Errorf("nt3d: clusters order has %d items for %d points", len(s.Order), n)
	}
	seen := make([]bool, n)
	for _, i := range s.Order {
		if int64(i) >= int64(n) || seen[i] {
			return fmt.Errorf("nt3d: clusters order is not a permutation of the points")
		}
		seen[i] = true
	}
	next := int64(0)
	for _, c := range s.Clusters {
		if c.Count == 0 || int64(c.First) != next {
			return fmt.Errorf("nt3d: clusters do not tile the order")
		}
		next += int64(c.Count)
	}
	if next != int64(n) {
		return fmt.Errorf("nt3d: clusters do not tile the order")
	}
	return nil
}

func validateGeometry(g Geometry) error {
	if err := g.Outline.validate(); err != nil {
		return err
	}
	if !validFloat(g.DropShadow.Strength, g.DropShadow.Radius, g.DropShadow.FloorZ) || g.DropShadow.Strength < 0 || g.DropShadow.Strength > 1 || g.DropShadow.Radius < 0 {
		return fmt.Errorf("nt3d: invalid drop shadow")
	}
	if g.WireGrid != ([2]int{}) {
		nx, ny := g.WireGrid[0], g.WireGrid[1]
		if nx < 2 || ny < 2 || nx > len(g.Vertices) || ny > len(g.Vertices)/nx || nx*ny != len(g.Vertices) {
			return fmt.Errorf("nt3d: invalid wire grid dimensions")
		}
	}

	if !validWidth(g.LineWidth) {
		return fmt.Errorf("nt3d: invalid line width")
	}
	if len(g.Arrows) > MaxVectors {
		return fmt.Errorf("nt3d: arrow count exceeds cap")
	}
	for _, a := range g.Arrows {
		if !finiteVec(a.Start) || !finiteVec(a.End) || !validWidth(a.Width) || !validFloat(a.HeadSize) || a.HeadSize < 0 || a.HeadSize > 64 {
			return fmt.Errorf("nt3d: invalid arrow")
		}
	}

	if len(g.NormalUV) != 0 && len(g.NormalUV) != len(g.Vertices) {
		return fmt.Errorf("nt3d: normal UV length mismatch")
	}
	if len(g.Tangents) != 0 && len(g.Tangents) != len(g.Vertices) {
		return fmt.Errorf("nt3d: tangent length mismatch")
	}
	for _, t := range g.Tangents {
		if !finiteVec(t.Direction) || t.W != 1 && t.W != -1 {
			return fmt.Errorf("nt3d: invalid tangent")
		}
	}
	if extended(g) && len(g.Indices) > MaxMeshIndices/2 {
		return fmt.Errorf("nt3d: normal-mapped mesh with tangents or independent UVs exceeds GPU binding limit")
	}
	for _, uv := range g.NormalUV {
		if !validFloat(uv.U, uv.V) {
			return fmt.Errorf("nt3d: non-finite normal UV")
		}
	}
	if len(g.UV) != 0 && len(g.UV) != len(g.Vertices) {
		return fmt.Errorf("nt3d: UV length mismatch")
	}
	if textured(g) && (!g.Material.Texture.valid() || g.Material.Texture.data || len(g.UV) != len(g.Vertices)) {
		return fmt.Errorf("nt3d: invalid texture or missing UVs")
	}
	if m := g.Material; m != nil {
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
		if m.NormalMap != nil && (!m.NormalMap.valid() || !m.NormalMap.data || len(g.UV) != len(g.Vertices)) {
			return fmt.Errorf("nt3d: normal map requires NewNormalMap and vertex UVs")
		}
	}
	if g.Material != nil && (!validFloat(g.Material.Threshold) || g.Material.Threshold < 0 || g.Material.Threshold > 1) {
		return fmt.Errorf("nt3d: cutout threshold must be 0..1")
	}
	if m := g.Material; m != nil && (!validFloat(m.Specular, m.Shininess) || m.Specular < 0 || m.Specular > 1 || m.Shininess != 0 && (m.Shininess < 1 || m.Shininess > 1024)) {
		return fmt.Errorf("nt3d: specular must be 0..1 and shininess 1..1024")
	}
	if m := g.Material; m != nil && (!validFloat(m.Reflectivity) || m.Reflectivity < 0 || m.Reflectivity > 1) {
		return fmt.Errorf("nt3d: reflectivity must be 0..1")
	}
	if m := g.Material; m != nil && m.Transparent && (m.Cutout || m.EdgeOn) {
		return fmt.Errorf("nt3d: a transparent material cannot be a cutout")
	}
	for _, uv := range g.UV {
		if !validFloat(uv.U, uv.V) {
			return fmt.Errorf("nt3d: non-finite UV")
		}
	}

	if err := validateGaussianSH(g); err != nil {
		return err
	}
	if err := validPointStyle(g.PointStyle); err != nil {
		return err
	}
	for _, p := range g.Points {
		if !validFloat(p.Position.X, p.Position.Y, p.Position.Z) || !validPointRadius(g.PointStyle.Units, p.Radius) || !validGaussian(g.PointStyle, p) {
			return fmt.Errorf("nt3d: invalid point geometry")
		}
	}
	if g.Clusters != nil {
		if err := validateClusters(g.Clusters, len(g.Points)); err != nil {
			return err
		}
	}
	for _, v := range g.Vertices {
		if !validFloat(v.Position.X, v.Position.Y, v.Position.Z, v.Normal.X, v.Normal.Y, v.Normal.Z) {
			return fmt.Errorf("nt3d: non-finite vertex")
		}
	}
	for _, v := range g.Lines {
		if !validFloat(v.Position.X, v.Position.Y, v.Position.Z) {
			return fmt.Errorf("nt3d: non-finite line")
		}
	}
	for _, b := range g.Boxes {
		if !validFloat(b.Min.X, b.Min.Y, b.Min.Z, b.Size.X, b.Size.Y, b.Size.Z) || min(b.Size.X, b.Size.Y, b.Size.Z) < 0 || !finiteVec(b.Min.Add(b.Size)) {
			return fmt.Errorf("nt3d: invalid box")
		}
	}
	if g.Bounds.IsValid() && (!validFloat(g.Bounds.Min.X, g.Bounds.Min.Y, g.Bounds.Min.Z, g.Bounds.Max.X, g.Bounds.Max.Y, g.Bounds.Max.Z) || g.Bounds.Max.X < g.Bounds.Min.X || g.Bounds.Max.Y < g.Bounds.Min.Y || g.Bounds.Max.Z < g.Bounds.Min.Z) {
		return fmt.Errorf("nt3d: invalid bounds")
	}

	// A mesh may give each face vertices of its own, so it may have as many
	// vertices as indices.
	if len(g.Points) > MaxPoints || len(g.Vertices) > MaxMeshIndices || len(g.Indices) > MaxMeshIndices || len(g.Boxes) > 512*512 || len(g.Lines) > 2*512*512 {
		return fmt.Errorf("nt3d: geometry exceeds cap")
	}
	// Each per-point buffer is one storage binding: the 20-byte records;
	// the axis or sprite records, 24 bytes a Gaussian splat, or 48 bytes
	// for a Precise one or a textured sprite; and the SH, 3 bytes a
	// coefficient (rounded up to 4) beside a 48-byte header buffer in
	// the compact records, or 48 bytes and 12 a coefficient with Precise.
	// The compact header is always small enough to need no check.
	axis := 48
	if g.PointStyle.Shape == GaussianPoint && !g.PointStyle.Precise {
		axis = 24
	}
	sprites := g.PointStyle.Shape == GaussianPoint || g.PointStyle.Texture != nil
	sh := 48 + 12*len(g.GaussianSH)
	if !g.PointStyle.Precise {
		sh = (3*len(g.GaussianSH) + 3) &^ 3
	}
	if len(g.Points)*20 > bindingLimit || sprites && len(g.Points)*axis > bindingLimit || len(g.GaussianSH) > 0 && sh > bindingLimit {
		return fmt.Errorf("nt3d: geometry exceeds the GPU binding limit")
	}
	if len(g.Indices)%3 != 0 || len(g.Lines)%2 != 0 {
		return fmt.Errorf("nt3d: incomplete triangle or line")
	}
	for _, i := range g.Indices {
		if int(i) >= len(g.Vertices) {
			return fmt.Errorf("nt3d: invalid vertex index")
		}
	}
	return nil
}

// extended reports whether a geometry's mesh needs the GPU's extension
// record for each index, beside its primary one: a normal-mapped mesh
// with tangents, independent normal UVs, or a flow field. Such meshes hold at most half
// the triangles of others (MaxMeshTriangles/2).
func extended(g Geometry) bool {
	return g.Material != nil && g.Material.NormalMap != nil && (len(g.NormalUV) > 0 || len(g.Tangents) > 0 || g.Material.FlowMap != nil)
}

// boxVertices uses outward counterclockwise faces, matching the GPU box shader.
func boxVertices(b Box) []Vertex {
	corners := []nt3dmath.Vec3{{X: 0, Y: 0, Z: 0}, {X: 1, Y: 0, Z: 0}, {X: 1, Y: 1, Z: 0}, {X: 0, Y: 1, Z: 0}, {X: 0, Y: 0, Z: 1}, {X: 1, Y: 0, Z: 1}, {X: 1, Y: 1, Z: 1}, {X: 0, Y: 1, Z: 1}}
	faces := [][4]int{{0, 3, 2, 1}, {4, 5, 6, 7}, {0, 1, 5, 4}, {3, 7, 6, 2}, {0, 4, 7, 3}, {1, 2, 6, 5}}
	out := make([]Vertex, 0, 36)
	for _, f := range faces {
		n := corners[f[1]].Sub(corners[f[0]]).Cross(corners[f[2]].Sub(corners[f[0]])).Normalize()
		for _, j := range []int{0, 1, 2, 0, 2, 3} {
			p := corners[f[j]]
			p = b.Min.Add(nt3dmath.Vec3{X: p.X * b.Size.X, Y: p.Y * b.Size.Y, Z: p.Z * b.Size.Z})
			out = append(out, Vertex{p, n, b.Color})
		}
	}
	return out
}
