// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"errors"
	"math"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// GenerateTangents returns a copy of g with a tangent frame at each vertex
// for normal mapping, found from its positions, normals, and normal-map
// coordinates (NormalUV when set, else UV) the way MikkTSpace does, with
// that library's default parameters: Blender, the glTF sample exporters,
// and most bakers produce or assume the same frames, so maps baked for
// them look right. Corners are welded by position, normal, and UV, so the
// result does not depend on how the mesh is indexed; around each vertex,
// faces joined through shared edges with the same UV handedness average
// their U directions, weighted by their angle at the vertex.
//
// Each vertex takes the frame of its first corner in index order. A
// vertex that faces of opposite handedness share, across a mirrored UV
// seam, so gets one side's frame: give such vertices to each side
// separately (the glTF loader does). Faces whose UVs collapse take their
// neighbors' frames, or an arbitrary one perpendicular to the normal
// where they have none.
//
// Scenes generate tangents this way for normal-mapped meshes set without
// them, up to MaxMeshTriangles/2 triangles; the renderers use a
// per-triangle basis past that, or for frames built without a scene.
func GenerateTangents(g Geometry) (Geometry, error) {
	if err := g.Validate(); err != nil {
		return Geometry{}, err
	}
	tangents, err := vertexTangents(g)
	if err != nil {
		return Geometry{}, err
	}
	g = cloneGeometry(g)
	g.Tangents = tangents
	return g, nil
}

// GenerateCornerTangents is GenerateTangents for a triangle list given
// corner by corner, as loaders hold meshes before indexing them: the
// slices are per corner, of one length, a multiple of three (else nil is
// returned), and the result has one frame per corner. Corners of one
// vertex may differ where MikkTSpace would split it.
func GenerateCornerTangents(positions, normals []nt3dmath.Vec3, uvs []UV) []Tangent {
	if len(positions)%3 != 0 || len(normals) != len(positions) || len(uvs) != len(positions) {
		return nil
	}
	corners := make([]tangentCorner, len(positions))
	for i := range corners {
		corners[i] = tangentCorner{positions[i], normals[i], uvs[i]}
	}
	return cornerTangents(corners)
}

// maxGeneratedIndices is the largest mesh a scene generates tangents
// for: the extension record halves the binding's triangles. Tests lower it.
var maxGeneratedIndices = MaxMeshIndices / 2

// generateTangentsOnUpload gives a normal-mapped mesh set on a scene its
// tangents, when it has UVs and none of its own and fits the extension
// record.
func generateTangentsOnUpload(g *Geometry) {
	if g.Material == nil || g.Material.NormalMap == nil || len(g.Tangents) != 0 || len(g.UV) != len(g.Vertices) || len(g.Indices) == 0 || len(g.Indices) > maxGeneratedIndices {
		return
	}
	if tangents, err := vertexTangents(*g); err == nil {
		g.Tangents = tangents
	}
}

// vertexTangents is a valid geometry's generated frames, one per vertex.
func vertexTangents(g Geometry) ([]Tangent, error) {
	if len(g.UV) != len(g.Vertices) || len(g.Indices) == 0 {
		return nil, errors.New("nt3d: tangents need vertex UVs and triangles")
	}
	uv := g.UV
	if len(g.NormalUV) > 0 {
		uv = g.NormalUV
	}
	corners := make([]tangentCorner, len(g.Indices))
	for k, i := range g.Indices {
		corners[k] = tangentCorner{g.Vertices[i].Position, g.Vertices[i].Normal, uv[i]}
	}
	byCorner := cornerTangents(corners)
	out := make([]Tangent, len(g.Vertices))
	set := make([]bool, len(g.Vertices))
	for k, i := range g.Indices {
		if !set[i] {
			set[i], out[i] = true, byCorner[k]
		}
	}
	for i := range out {
		if !set[i] {
			out[i] = Tangent{perpendicular(g.Vertices[i].Normal), 1}
		}
	}
	return out, nil
}

// tangentCorner is a triangle corner as the tangent generator sees it.
type tangentCorner struct {
	position, normal nt3dmath.Vec3
	uv               UV
}

// tangentFace is what the generator knows of one triangle.
type tangentFace struct {
	os, ot     nt3dmath.Vec3 // Unit dP/du and dP/dv; zero when the UVs collapse.
	orient     bool          // Positive UV area: the frame is right-handed.
	any        bool          // Collapsed UVs: joins any group, contributes nothing.
	degenerate bool          // Two corners at one position: set aside.
	neighbor   [3]int        // The face across each edge (corner k to k+1), or -1.
	group      [3]int        // Each corner's group, or -1.
}

type tangentGroup struct {
	vertex  int // The welded corner the group is around.
	orient  bool
	faces   []int
	tangent nt3dmath.Vec3
}

// cornerTangents is the frame at each corner of a triangle list (len a
// multiple of three), as MikkTSpace finds it with its default parameters.
func cornerTangents(corners []tangentCorner) []Tangent {
	n := len(corners) / 3
	if n == 0 {
		return nil
	}
	// Weld corners alike in every attribute, so indexing does not matter.
	welded := make([]int, 3*n)
	ids := map[tangentCorner]int{}
	for i, c := range corners {
		id, ok := ids[c]
		if !ok {
			id = len(ids)
			ids[c] = id
		}
		welded[i] = id
	}
	faces := make([]tangentFace, n)
	for f := range faces {
		face := &faces[f]
		face.neighbor, face.group = [3]int{-1, -1, -1}, [3]int{-1, -1, -1}
		c0, c1, c2 := corners[3*f], corners[3*f+1], corners[3*f+2]
		face.degenerate = welded[3*f] == welded[3*f+1] || welded[3*f] == welded[3*f+2] || welded[3*f+1] == welded[3*f+2] || c0.position == c1.position || c0.position == c2.position || c1.position == c2.position
		d1, d2 := c1.position.Sub(c0.position), c2.position.Sub(c0.position)
		t21, t31 := UV{c1.uv.U - c0.uv.U, c1.uv.V - c0.uv.V}, UV{c2.uv.U - c0.uv.U, c2.uv.V - c0.uv.V}
		area := t21.U*t31.V - t21.V*t31.U
		os := d1.Scale(t31.V).Sub(d2.Scale(t21.V))
		ot := d2.Scale(t21.U).Sub(d1.Scale(t31.U))
		face.orient = area > 0
		face.any = true
		if area != 0 {
			sign := float32(-1)
			if face.orient {
				sign = 1
			}
			if os.Dot(os) > 0 && ot.Dot(ot) > 0 {
				face.os, face.ot, face.any = os.Normalize().Scale(sign), ot.Normalize().Scale(sign), false
			}
		}
	}
	// Faces are neighbors across an edge they share, by welded corners,
	// run opposite ways (consistent winding): the first unpaired face
	// found takes the edge.
	type edge struct{ a, b int } // Directed, as the face runs it.
	type slot struct{ face, k int }
	edges := map[edge][]slot{}
	for f := range faces {
		if faces[f].degenerate {
			continue
		}
		for k := range 3 {
			e := edge{welded[3*f+k], welded[3*f+(k+1)%3]}
			edges[e] = append(edges[e], slot{f, k})
		}
	}
	for f := range faces {
		if faces[f].degenerate {
			continue
		}
		for k := range 3 {
			if faces[f].neighbor[k] >= 0 {
				continue
			}
			for _, s := range edges[edge{welded[3*f+(k+1)%3], welded[3*f+k]}] {
				if s.face != f && faces[s.face].neighbor[s.k] < 0 {
					faces[f].neighbor[k], faces[s.face].neighbor[s.k] = s.face, f
					break
				}
			}
		}
	}
	// Around each welded corner, faces joined through edges with the same
	// handedness form a group; faces with collapsed UVs join whichever
	// group reaches them first and take its handedness.
	var groups []tangentGroup
	cornerOf := func(f, vertex int) int {
		for i := range 3 {
			if welded[3*f+i] == vertex {
				return i
			}
		}
		return -1
	}
	for f := range faces {
		for i := range 3 {
			if faces[f].degenerate || faces[f].any || faces[f].group[i] >= 0 {
				continue
			}
			gi := len(groups)
			groups = append(groups, tangentGroup{vertex: welded[3*f+i], orient: faces[f].orient})
			stack := []int{f}
			for len(stack) > 0 {
				t := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if t < 0 {
					continue
				}
				j := cornerOf(t, groups[gi].vertex)
				if j < 0 || faces[t].group[j] >= 0 {
					continue
				}
				if faces[t].any && faces[t].group == [3]int{-1, -1, -1} {
					faces[t].orient = groups[gi].orient
				}
				if faces[t].orient != groups[gi].orient {
					continue
				}
				faces[t].group[j] = gi
				groups[gi].faces = append(groups[gi].faces, t)
				stack = append(stack, faces[t].neighbor[j], faces[t].neighbor[(j+2)%3])
			}
		}
	}
	// A group's tangent averages its faces' U directions, each projected
	// onto the corner's normal plane and weighted by the face's angle there.
	project := func(v, n nt3dmath.Vec3) nt3dmath.Vec3 {
		v = v.Sub(n.Scale(n.Dot(v)))
		if v.Dot(v) > 0 {
			return v.Normalize()
		}
		return v
	}
	for gi := range groups {
		g := &groups[gi]
		var sum nt3dmath.Vec3
		for _, f := range g.faces {
			if faces[f].any {
				continue
			}
			j := cornerOf(f, g.vertex)
			c, previous, next := corners[3*f+j], corners[3*f+(j+2)%3], corners[3*f+(j+1)%3]
			os := project(faces[f].os, c.normal)
			v1, v2 := project(previous.position.Sub(c.position), c.normal), project(next.position.Sub(c.position), c.normal)
			angle := math.Acos(float64(min(1, max(-1, v1.Dot(v2)))))
			sum = sum.Add(os.Scale(float32(angle)))
		}
		if sum.Dot(sum) > 0 {
			g.tangent = sum.Normalize()
		}
	}
	// Each corner takes its group's frame; a degenerate face's corners
	// take a grouped corner's at the same welded vertex.
	out := make([]Tangent, 3*n)
	byVertex := map[int]Tangent{}
	frame := func(gi, corner int) Tangent {
		t := Tangent{groups[gi].tangent, -1}
		if groups[gi].orient {
			t.W = 1
		}
		if t.Direction == (nt3dmath.Vec3{}) {
			t.Direction = perpendicular(corners[corner].normal)
		}
		return t
	}
	for f := range faces {
		for i := range 3 {
			if gi := faces[f].group[i]; gi >= 0 {
				out[3*f+i] = frame(gi, 3*f+i)
				if _, ok := byVertex[welded[3*f+i]]; !ok {
					byVertex[welded[3*f+i]] = out[3*f+i]
				}
			}
		}
	}
	for f := range faces {
		for i := range 3 {
			if faces[f].group[i] >= 0 {
				continue
			}
			if t, ok := byVertex[welded[3*f+i]]; ok {
				out[3*f+i] = t
			} else {
				out[3*f+i] = Tangent{perpendicular(corners[3*f+i].normal), 1}
			}
		}
	}
	return out
}

// perpendicular is a unit vector perpendicular to n: an arbitrary frame
// where the UVs give none. A zero normal takes +X.
func perpendicular(n nt3dmath.Vec3) nt3dmath.Vec3 {
	if n.Dot(n) == 0 {
		return nt3dmath.Vec3{X: 1}
	}
	axis := nt3dmath.Vec3{X: 1}
	if abs(n.Y) < abs(n.X) && abs(n.Y) <= abs(n.Z) {
		axis = nt3dmath.Vec3{Y: 1}
	} else if abs(n.Z) < abs(n.X) {
		axis = nt3dmath.Vec3{Z: 1}
	}
	return n.Cross(axis).Normalize()
}
