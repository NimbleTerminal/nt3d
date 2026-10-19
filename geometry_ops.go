// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"errors"
	"fmt"
	"image/color"
	"math"

	"nimbleterminal.dev/nt3d/math"
)

// TransformGeometry bakes an affine transform into a copy of g. Unlike node
// transforms, it accepts reflections: a mirroring transform reverses triangle
// winding so faces stay outward. Normals use the inverse transpose. Boxes stay
// axis-aligned, so geometry with boxes accepts only scale and translation.
func TransformGeometry(g Geometry, m nt3dmath.Mat4) (Geometry, error) {
	inverse, ok := m.InverseAffine()
	if !ok {
		return Geometry{}, errors.New("nt3d: transform must be finite, invertible, and affine")
	}
	if len(g.Boxes) != 0 && (m[1] != 0 || m[2] != 0 || m[4] != 0 || m[6] != 0 || m[8] != 0 || m[9] != 0) {
		return Geometry{}, errors.New("nt3d: boxes accept only scale and translation")
	}
	normals := inverse.Transpose()
	g = cloneGeometry(g)
	if len(g.GaussianSH) > 0 {
		g.GaussianSHTransform = gaussianSHMatrix(&g).Mul(inverse)
	}
	for i, v := range g.Vertices {
		g.Vertices[i].Position = m.Point(v.Position)
		if v.Normal != (nt3dmath.Vec3{}) {
			g.Vertices[i].Normal = normals.Direction(v.Normal).Normalize()
		}
	}
	for i, t := range g.Tangents {
		if t.Direction != (nt3dmath.Vec3{}) {
			g.Tangents[i].Direction = m.Direction(t.Direction).Normalize()
		}
	}
	if m.Determinant3() < 0 {
		for i := 0; i+2 < len(g.Indices); i += 3 {
			g.Indices[i+1], g.Indices[i+2] = g.Indices[i+2], g.Indices[i+1]
		}
		// A reflection turns the frame's handedness.
		for i := range g.Tangents {
			g.Tangents[i].W = -g.Tangents[i].W
		}
	}
	for i := range g.Points {
		g.Points[i].Position = m.Point(g.Points[i].Position)
		for k, axis := range g.Points[i].GaussianAxes {
			g.Points[i].GaussianAxes[k] = m.Direction(axis)
		}
	}
	for i := range g.Lines {
		g.Lines[i].Position = m.Point(g.Lines[i].Position)
	}
	for i, a := range g.Arrows {
		g.Arrows[i].Start, g.Arrows[i].End = m.Point(a.Start), m.Point(a.End)
	}
	for i, b := range g.Boxes {
		var bounds nt3dmath.AABB
		bounds.Include(m.Point(b.Min))
		bounds.Include(m.Point(b.Min.Add(b.Size)))
		g.Boxes[i].Min, g.Boxes[i].Size = bounds.Min, bounds.Max.Sub(bounds.Min)
	}
	g.Bounds = GeometryBounds(g)
	return g, g.Validate()
}

func sameMaterial(a, b *Material) bool {
	return a == b || a != nil && b != nil && *a == *b
}

// MergeGeometry concatenates geometries into one, offsetting triangle indices.
// Meshes must share a material and outline style, lines a width, and points a style. UVs are
// kept only when every mesh has them. WireGrid survives only when a single geometry is given.
// The first geometry with a positive drop shadow strength supplies the drop shadow.
func MergeGeometry(gs ...Geometry) (Geometry, error) {
	var out Geometry
	if len(gs) == 1 {
		out.WireGrid = gs[0].WireGrid
	}
	var mesh, points *Geometry
	uv, tangents := true, true
	normalUV := false
	for i := range gs {
		g := &gs[i]
		normalUV = normalUV || len(g.NormalUV) > 0
		if out.DropShadow.Strength == 0 && g.DropShadow.Strength > 0 {
			out.DropShadow = g.DropShadow
		}
		if len(g.Vertices) != 0 {
			if mesh != nil && mesh.Outline != g.Outline {
				return Geometry{}, errors.New("nt3d: cannot merge meshes with different outline styles")
			}
			if mesh != nil && !sameMaterial(mesh.Material, g.Material) {
				return Geometry{}, errors.New("nt3d: cannot merge meshes with different materials")
			}
			if mesh == nil {
				mesh = g
				out.Outline = g.Outline
			}
			uv = uv && len(g.UV) == len(g.Vertices)
			tangents = tangents && len(g.Tangents) == len(g.Vertices)
		}
		if len(g.Lines) != 0 {
			if out.LineWidth != 0 && lineWidth(g.LineWidth) != out.LineWidth {
				return Geometry{}, errors.New("nt3d: cannot merge different line widths")
			}
			out.LineWidth = lineWidth(g.LineWidth)
		}
		if len(g.Points) != 0 {
			if points != nil && (g.PointStyle != points.PointStyle || gaussianSHCount(g) != gaussianSHCount(points) || gaussianSHMatrix(g) != gaussianSHMatrix(points)) {
				return Geometry{}, errors.New("nt3d: cannot merge points with different styles or SH layouts/frames")
			}
			if points == nil {
				points = g
			}
		}
		if len(out.Indices)+len(g.Indices) > MaxMeshIndices || len(out.Vertices)+len(g.Vertices) > MaxMeshIndices {
			return Geometry{}, fmt.Errorf("nt3d: merged mesh exceeds %d triangles", MaxMeshTriangles)
		}
	}
	if mesh != nil {
		out.Material = cloneMaterial(mesh.Material)
	}
	if points != nil {
		out.PointStyle = points.PointStyle
		out.GaussianSHTransform = points.GaussianSHTransform
	}
	for _, g := range gs {
		base := uint32(len(out.Vertices))
		for _, i := range g.Indices {
			out.Indices = append(out.Indices, base+i)
		}
		out.Vertices = append(out.Vertices, g.Vertices...)
		if normalUV {
			if len(g.NormalUV) > 0 {
				out.NormalUV = append(out.NormalUV, g.NormalUV...)
			} else {
				out.NormalUV = append(out.NormalUV, g.UV...)
			}
		}
		if uv && mesh != nil {
			out.UV = append(out.UV, g.UV...)
		}
		if tangents && mesh != nil {
			out.Tangents = append(out.Tangents, g.Tangents...)
		}
		out.Points = append(out.Points, g.Points...)
		out.GaussianSH = append(out.GaussianSH, g.GaussianSH...)
		out.Boxes = append(out.Boxes, g.Boxes...)
		out.Lines = append(out.Lines, g.Lines...)
		out.Arrows = append(out.Arrows, g.Arrows...)
	}
	out.Bounds = GeometryBounds(out)
	return out, out.Validate()
}

func faceNormal(g Geometry, f int) nt3dmath.Vec3 {
	a, b, c := g.Vertices[g.Indices[f]].Position, g.Vertices[g.Indices[f+1]].Position, g.Vertices[g.Indices[f+2]].Position
	return b.Sub(a).Cross(c.Sub(a))
}

// FlatNormals returns a copy of g in which every triangle carries its face
// normal. Vertices are split where faces meet at an angle; WireGrid is cleared.
func FlatNormals(g Geometry) (Geometry, error) {
	if err := g.Validate(); err != nil {
		return Geometry{}, err
	}
	return rebuildMesh(g, func(f, _ int) nt3dmath.Vec3 { return faceNormal(g, f).Normalize() })
}

// SmoothNormals returns a copy of g whose normals average the faces that meet
// at each position and lie within creaseDegrees of one another, weighting each
// face by its angle at that corner so the result does not depend on how
// polygons were split into triangles. Faces meet where vertex positions are
// equal, so meshes that give each face vertices of their own, as STL does, are
// smoothed too. Edges sharper than the crease stay hard; 180 smooths
// everything. Corners identical in every attribute share vertices.
func SmoothNormals(g Geometry, creaseDegrees float64) (Geometry, error) {
	if err := g.Validate(); err != nil {
		return Geometry{}, err
	}
	if math.IsNaN(creaseDegrees) {
		return Geometry{}, errors.New("nt3d: crease angle is NaN")
	}
	creaseCos := float32(math.Cos(min(max(creaseDegrees, 0), 180) * math.Pi / 180))
	type incident struct {
		face   int
		weight nt3dmath.Vec3 // Unit face normal times the corner angle.
	}
	unit := make([]nt3dmath.Vec3, len(g.Indices)/3)
	faces := map[nt3dmath.Vec3][]incident{}
	for f := range unit {
		unit[f] = faceNormal(g, 3*f).Normalize()
		for k := range 3 {
			p := g.Vertices[g.Indices[3*f+k]].Position
			u := g.Vertices[g.Indices[3*f+(k+1)%3]].Position.Sub(p).Normalize()
			v := g.Vertices[g.Indices[3*f+(k+2)%3]].Position.Sub(p).Normalize()
			angle := math.Acos(min(max(float64(u.Dot(v)), -1), 1))
			faces[p] = append(faces[p], incident{f, unit[f].Scale(float32(angle))})
		}
	}
	return rebuildMesh(g, func(f, corner int) nt3dmath.Vec3 {
		var sum nt3dmath.Vec3
		for _, other := range faces[g.Vertices[g.Indices[corner]].Position] {
			if other.face == f/3 || unit[f/3].Dot(unit[other.face]) >= creaseCos {
				sum = sum.Add(other.weight)
			}
		}
		if sum == (nt3dmath.Vec3{}) {
			return unit[f/3]
		}
		return sum.Normalize()
	})
}

// rebuildMesh gives each triangle corner the normal chosen for it, then shares
// corners that are identical in every attribute.
func rebuildMesh(g Geometry, normal func(face, corner int) nt3dmath.Vec3) (Geometry, error) {
	type key struct {
		position, normal nt3dmath.Vec3
		color            color.RGBA
		uv               UV
		normalUV         UV
		tangent          Tangent
	}
	out := cloneGeometry(g)
	out.WireGrid = [2]int{}
	out.Vertices, out.UV, out.NormalUV, out.Tangents = nil, nil, nil, nil
	out.Indices = make([]uint32, 0, len(g.Indices))
	seen := map[key]uint32{}
	textured := len(g.UV) == len(g.Vertices) && len(g.UV) != 0
	for f := 0; f+2 < len(g.Indices); f += 3 {
		for corner := f; corner < f+3; corner++ {
			v := g.Vertices[g.Indices[corner]]
			v.Normal = normal(f, corner)
			k := key{position: v.Position, normal: v.Normal, color: v.Color}
			if textured {
				k.uv = g.UV[g.Indices[corner]]
			}
			if len(g.NormalUV) > 0 {
				k.normalUV = g.NormalUV[g.Indices[corner]]
			}
			if len(g.Tangents) > 0 {
				k.tangent = g.Tangents[g.Indices[corner]]
			}
			index, ok := seen[k]
			if !ok {
				index = uint32(len(out.Vertices))
				seen[k] = index
				out.Vertices = append(out.Vertices, v)
				if len(g.NormalUV) > 0 {
					out.NormalUV = append(out.NormalUV, k.normalUV)
				}
				if len(g.Tangents) > 0 {
					out.Tangents = append(out.Tangents, k.tangent)
				}
				if textured {
					out.UV = append(out.UV, k.uv)
				}
			}
			out.Indices = append(out.Indices, index)
		}
	}
	out.Bounds = GeometryBounds(out)
	return out, out.Validate()
}
