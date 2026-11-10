// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"math"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// creased reports whether a geometry draws crease lines: an outlined, opaque,
// rigid triangle mesh with a crease angle. Transparent, cutout, transmitting,
// and deformed meshes draw only their hull.
func creased(g *Geometry) bool {
	if g.Outline.Width <= 0 || g.Outline.Crease <= 0 || len(g.Indices) == 0 || g.posed != nil {
		return false
	}
	return !transparent(g) && !transmitting(g) && (g.Material == nil || !g.Material.Cutout)
}

// creaseEdges returns the edges of an indexed triangle mesh that get a crease
// line, as local-space endpoint pairs: boundary edges, with one face; edges
// whose two faces turn by more than degrees; and edges with three or more
// faces. It is nil when degrees is not positive or no edge qualifies.
//
// Edges join vertices by position, as outlineNormals does, so a mesh whose
// faces have their own vertices still shares its edges; a face's direction is
// its geometric normal, not its vertices'; triangles without area are
// skipped. Edges come in the order the indices first name them.
func creaseEdges(vertices []Vertex, indices []uint32, degrees float32) []nt3dmath.Vec3 {
	if !(degrees > 0) || len(indices) < 3 {
		return nil
	}
	type key [2]nt3dmath.Vec3
	type edge struct {
		normal nt3dmath.Vec3 // The first face's.
		faces  int
		sharp  bool
	}
	less := func(a, b nt3dmath.Vec3) bool {
		if a.X != b.X {
			return a.X < b.X
		}
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.Z < b.Z
	}
	limit := float32(math.Cos(float64(degrees) * math.Pi / 180))
	edges := make(map[key]*edge, len(indices))
	order := make([]key, 0, len(indices))
	for i := 0; i+2 < len(indices); i += 3 {
		p := [3]nt3dmath.Vec3{vertices[indices[i]].Position, vertices[indices[i+1]].Position, vertices[indices[i+2]].Position}
		n := p[1].Sub(p[0]).Cross(p[2].Sub(p[0]))
		l := float32(math.Sqrt(float64(n.Dot(n))))
		if !(l > 0) || !validFloat(l) {
			continue
		}
		n = n.Scale(1 / l)
		for c := range 3 {
			k := key{p[c], p[(c+1)%3]}
			if k[0] == k[1] {
				continue
			}
			if less(k[1], k[0]) {
				k[0], k[1] = k[1], k[0]
			}
			e := edges[k]
			if e == nil {
				edges[k] = &edge{normal: n, faces: 1}
				order = append(order, k)
				continue
			}
			e.faces++
			e.sharp = e.sharp || e.normal.Dot(n) < limit
		}
	}
	var out []nt3dmath.Vec3
	for _, k := range order {
		if e := edges[k]; e.faces != 2 || e.sharp {
			out = append(out, k[0], k[1])
		}
	}
	return out
}
