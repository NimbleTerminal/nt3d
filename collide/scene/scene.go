// Copyright (c) 2026 Neomantra Corp.

// Package nt3dcollidescene joins collision queries (package nt3dcollide) to
// the engine: it turns an nt3d.Geometry into the triangles and the Mesh that
// nt3dcollide works on.
//
// It is the only package that imports both; nt3dcollide knows nothing of the
// engine, so collision can be used without the renderer.
package nt3dcollidescene

import (
	"nimbleterminal.dev/nt3d"
	nt3dcollide "nimbleterminal.dev/nt3d/collide"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// Triangles appends g's triangles, moved into the world by world, to dst. The
// zero matrix means identity. A triangle whose index is out of range is left
// out; points and lines are not triangles and are ignored.
func Triangles(dst []nt3dcollide.Triangle, g nt3d.Geometry, world nt3dmath.Mat4d) []nt3dcollide.Triangle {
	if world == (nt3dmath.Mat4d{}) {
		world = nt3dmath.IdentityD()
	}
	for i := 0; i+2 < len(g.Indices); i += 3 {
		i0, i1, i2 := int(g.Indices[i]), int(g.Indices[i+1]), int(g.Indices[i+2])
		if i0 >= len(g.Vertices) || i1 >= len(g.Vertices) || i2 >= len(g.Vertices) {
			continue
		}
		dst = append(dst, nt3dcollide.Triangle{
			A: transformPoint(world, g.Vertices[i0].Position.D()),
			B: transformPoint(world, g.Vertices[i1].Position.D()),
			C: transformPoint(world, g.Vertices[i2].Position.D()),
		})
	}
	return dst
}

// NewMesh builds a collision mesh from g moved into the world by world. The
// zero matrix means identity.
func NewMesh(g nt3d.Geometry, world nt3dmath.Mat4d) *nt3dcollide.Mesh {
	return nt3dcollide.NewMeshFromTriangles(Triangles(nil, g, world))
}

// NewMeshFromGeometries builds one collision mesh from several geometries.
// worlds[i] places gs[i]; a missing or zero matrix means identity.
func NewMeshFromGeometries(gs []nt3d.Geometry, worlds []nt3dmath.Mat4d) *nt3dcollide.Mesh {
	var tris []nt3dcollide.Triangle
	for i, g := range gs {
		var world nt3dmath.Mat4d
		if i < len(worlds) {
			world = worlds[i]
		}
		tris = Triangles(tris, g, world)
	}
	return nt3dcollide.NewMeshFromTriangles(tris)
}

// transformPoint is m applied to p. Each product is rounded before it is
// summed, so the result is the same on every platform.
func transformPoint(m nt3dmath.Mat4d, p nt3dmath.Vec3d) nt3dmath.Vec3d {
	x := float64(m[0]*p.X) + float64(m[4]*p.Y) + float64(m[8]*p.Z) + m[12]
	y := float64(m[1]*p.X) + float64(m[5]*p.Y) + float64(m[9]*p.Z) + m[13]
	z := float64(m[2]*p.X) + float64(m[6]*p.Y) + float64(m[10]*p.Z) + m[14]
	return nt3dmath.Vec3d{X: x, Y: y, Z: z}
}
