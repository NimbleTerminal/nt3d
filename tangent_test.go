// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image/color"
	"math"
	"strings"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// tangentQuad is normalFrame's quad with an authored +X frame at each vertex.
func tangentQuad(t *testing.T) Geometry {
	t.Helper()
	g := normalFrame(t).Geometry[0]
	for range g.Vertices {
		g.Tangents = append(g.Tangents, Tangent{Direction: nt3dmath.Vec3{X: 1}, W: 1})
	}
	return g
}

func nearVec(a, b nt3dmath.Vec3, tolerance float32) bool {
	d := a.Sub(b)
	return d.Dot(d) <= tolerance*tolerance
}

func wantError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error %v, want one containing %q", err, want)
	}
}

func TestTangentValidation(t *testing.T) {
	g := tangentQuad(t)
	check(t, g.Validate())
	g.Tangents = g.Tangents[:3]
	wantError(t, g.Validate(), "tangent length")
	g = tangentQuad(t)
	g.Tangents[1].W = .5
	wantError(t, g.Validate(), "invalid tangent")
	g = tangentQuad(t)
	g.Tangents[2].Direction.Y = float32(math.NaN())
	wantError(t, g.Validate(), "invalid tangent")
	g = tangentQuad(t)
	g.Tangents[0].W = -1
	check(t, g.Validate())

	big := tangentQuad(t)
	for len(big.Indices) < MaxMeshIndices/2+3 {
		big.Indices = append(big.Indices, 0, 1, 2)
	}
	wantError(t, big.Validate(), "exceeds GPU binding limit")
	big.Material.NormalMap = nil
	check(t, big.Validate())
}

func TestTangentMeshOps(t *testing.T) {
	g := tangentQuad(t)
	clone := cloneGeometry(g)
	clone.Tangents[0].W = -1
	if g.Tangents[0].W != 1 {
		t.Fatal("tangents aliased by clone")
	}

	turned, err := TransformGeometry(g, nt3dmath.RotationZ(math.Pi/2))
	check(t, err)
	if !nearVec(turned.Tangents[0].Direction, nt3dmath.Vec3{Y: 1}, 1e-5) || turned.Tangents[0].W != 1 {
		t.Fatalf("rotated tangent %v", turned.Tangents[0])
	}
	mirrored, err := TransformGeometry(g, nt3dmath.Scaling(nt3dmath.Vec3{X: -1, Y: 1, Z: 1}))
	check(t, err)
	if !nearVec(mirrored.Tangents[0].Direction, nt3dmath.Vec3{X: -1}, 1e-5) || mirrored.Tangents[0].W != -1 {
		t.Fatalf("mirrored tangent %v", mirrored.Tangents[0])
	}

	merged, err := MergeGeometry(g, g)
	check(t, err)
	if len(merged.Tangents) != 8 {
		t.Fatalf("merge kept %d tangents", len(merged.Tangents))
	}
	bare := cloneGeometry(g)
	bare.Tangents = nil
	merged, err = MergeGeometry(g, bare)
	check(t, err)
	if len(merged.Tangents) != 0 {
		t.Fatal("merge invented tangents")
	}

	smooth, err := SmoothNormals(g, 1)
	check(t, err)
	if len(smooth.Tangents) != len(smooth.Vertices) {
		t.Fatal("smoothing lost tangents")
	}
	for _, tan := range smooth.Tangents {
		if !nearVec(tan.Direction, nt3dmath.Vec3{X: 1}, 1e-6) || tan.W != 1 {
			t.Fatalf("smoothing changed tangent %v", tan)
		}
	}

	var b nt3dmath.AABB
	b.Include(nt3dmath.Vec3{X: -.3, Y: -.3, Z: 0})
	b.Include(nt3dmath.Vec3{X: .3, Y: .3, Z: 1})
	clipped, err := Clip(g, b)
	check(t, err)
	if len(clipped.Tangents) != len(clipped.Vertices) || len(clipped.Vertices) == 0 {
		t.Fatalf("clip kept %d tangents for %d vertices", len(clipped.Tangents), len(clipped.Vertices))
	}
	for _, tan := range clipped.Tangents {
		if !nearVec(tan.Direction, nt3dmath.Vec3{X: 1}, 1e-6) || tan.W != 1 {
			t.Fatalf("clip changed tangent %v", tan)
		}
	}
	check(t, clipped.Validate())
}

// tangentQuadCorners is a unit quad in XY as six corners, its UVs mapping
// U along X and V along Y.
func tangentQuadCorners(mirror bool) []tangentCorner {
	p := [][2]float32{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	var corners []tangentCorner
	for _, i := range []int{0, 1, 2, 0, 2, 3} {
		u := p[i][0]
		if mirror {
			u = 1 - u
		}
		corners = append(corners, tangentCorner{position: nt3dmath.Vec3{X: p[i][0], Y: p[i][1]}, normal: nt3dmath.Vec3{Z: 1}, uv: UV{u, p[i][1]}})
	}
	return corners
}

func TestCornerTangentsQuad(t *testing.T) {
	for _, tan := range cornerTangents(tangentQuadCorners(false)) {
		if !nearVec(tan.Direction, nt3dmath.Vec3{X: 1}, 1e-5) || tan.W != 1 {
			t.Fatalf("quad tangent %v", tan)
		}
	}
	for _, tan := range cornerTangents(tangentQuadCorners(true)) {
		if !nearVec(tan.Direction, nt3dmath.Vec3{X: -1}, 1e-5) || tan.W != -1 {
			t.Fatalf("mirrored quad tangent %v", tan)
		}
	}
	corners := tangentQuadCorners(false)
	for i := range corners {
		corners[i].uv = UV{}
	}
	for _, tan := range cornerTangents(corners) {
		if tan.W != 1 || abs(tan.Direction.Dot(tan.Direction)-1) > 1e-5 || abs(tan.Direction.Z) > 1e-5 {
			t.Fatalf("degenerate UV tangent %v", tan)
		}
	}
	if cornerTangents(nil) != nil {
		t.Fatal("empty input")
	}
}

// cylinder is an open cylinder about Z of the given segments and two rows,
// its seam vertex duplicated, with U around and V up.
func cylinder(segments int) Geometry {
	var g Geometry
	for row := range 2 {
		for i := 0; i <= segments; i++ {
			a := 2 * math.Pi * float64(i) / float64(segments)
			n := nt3dmath.Vec3{X: float32(math.Cos(a)), Y: float32(math.Sin(a))}
			g.Vertices = append(g.Vertices, Vertex{Position: n.Add(nt3dmath.Vec3{Z: float32(row)}), Normal: n, Color: white})
			g.UV = append(g.UV, UV{float32(i) / float32(segments), float32(row)})
		}
	}
	w := uint32(segments + 1)
	for i := uint32(0); i < uint32(segments); i++ {
		g.Indices = append(g.Indices, i, i+1, i+1+w, i, i+1+w, i+w)
	}
	return g
}

func TestGenerateTangentsCylinder(t *testing.T) {
	g := cylinder(16)
	out, err := GenerateTangents(g)
	check(t, err)
	if len(out.Tangents) != len(g.Vertices) || len(g.Tangents) != 0 {
		t.Fatalf("generated %d tangents for %d vertices, source %d", len(out.Tangents), len(g.Vertices), len(g.Tangents))
	}
	for i, tan := range out.Tangents {
		n := g.Vertices[i].Normal
		want := nt3dmath.Vec3{X: -n.Y, Y: n.X}
		if !nearVec(tan.Direction, want, 1e-3) || tan.W != 1 || abs(tan.Direction.Dot(n)) > 1e-5 {
			t.Fatalf("vertex %d tangent %v, want %v", i, tan, want)
		}
	}
	// Every face its own vertices: welding gives the same frames.
	var split Geometry
	for _, i := range g.Indices {
		split.Indices = append(split.Indices, uint32(len(split.Vertices)))
		split.Vertices = append(split.Vertices, g.Vertices[i])
		split.UV = append(split.UV, g.UV[i])
	}
	welded, err := GenerateTangents(split)
	check(t, err)
	for k, i := range g.Indices {
		if welded.Tangents[k] != out.Tangents[i] {
			t.Fatalf("corner %d: split %v, indexed %v", k, welded.Tangents[k], out.Tangents[i])
		}
	}
	g.UV = nil
	wantError(t, func() error { _, err := GenerateTangents(g); return err }(), "tangents need vertex UVs")
}

func TestGenerateTangentsSeamAndDegenerate(t *testing.T) {
	// Two quads sharing the edge x = 1; the right one's U runs backward.
	var g Geometry
	for _, p := range [][2]float32{{0, 0}, {1, 0}, {1, 1}, {0, 1}, {2, 0}, {2, 1}} {
		g.Vertices = append(g.Vertices, Vertex{Position: nt3dmath.Vec3{X: p[0], Y: p[1]}, Normal: nt3dmath.Vec3{Z: 1}, Color: white})
		u := p[0]
		if u > 1 {
			u = 2 - u
		}
		g.UV = append(g.UV, UV{u, p[1]})
	}
	g.Indices = []uint32{0, 1, 2, 0, 2, 3, 1, 4, 5, 1, 5, 2}
	out, err := GenerateTangents(g)
	check(t, err)
	for i, want := range []float32{1, 1, 1, 1, -1, -1} {
		if out.Tangents[i].W != want {
			t.Fatalf("vertex %d W %v, want %v: %v", i, out.Tangents[i].W, want, out.Tangents)
		}
	}
	check(t, out.Validate())
	// A degenerate triangle still gets unit frames.
	g.Vertices = append(g.Vertices, g.Vertices[4])
	g.UV = append(g.UV, UV{.5, .5})
	g.Indices = append(g.Indices, 4, 6, 5)
	out, err = GenerateTangents(g)
	check(t, err)
	check(t, out.Validate())
	for i, tan := range out.Tangents {
		if abs(tan.Direction.Dot(tan.Direction)-1) > 1e-5 {
			t.Fatalf("vertex %d tangent %v is not unit", i, tan)
		}
	}
}

func TestSceneGeneratesTangents(t *testing.T) {
	s := NewScene()
	g := normalFrame(t).Geometry[0]
	_, err := s.SetGeometry("mesh", g)
	check(t, err)
	f := frame(t, s)
	if len(f.Geometry[0].Tangents) != 4 || !nearVec(f.Geometry[0].Tangents[0].Direction, nt3dmath.Vec3{X: 1}, 1e-5) {
		t.Fatalf("scene tangents %v", f.Geometry[0].Tangents)
	}
	plain := cloneGeometry(g)
	plain.Material.NormalMap = nil
	_, err = s.SetGeometry("mesh", plain)
	check(t, err)
	if len(frame(t, s).Geometry[0].Tangents) != 0 {
		t.Fatal("tangents generated without a normal map")
	}
	authored := tangentQuad(t)
	for i := range authored.Tangents {
		authored.Tangents[i] = Tangent{Direction: nt3dmath.Vec3{Y: 1}, W: -1}
	}
	_, err = s.SetGeometry("mesh", authored)
	check(t, err)
	if tan := frame(t, s).Geometry[0].Tangents[2]; tan != (Tangent{Direction: nt3dmath.Vec3{Y: 1}, W: -1}) {
		t.Fatalf("authored tangent replaced: %v", tan)
	}
	saved := maxGeneratedIndices
	maxGeneratedIndices = 3
	t.Cleanup(func() { maxGeneratedIndices = saved })
	_, err = s.SetGeometry("mesh", g)
	check(t, err)
	if len(frame(t, s).Geometry[0].Tangents) != 0 {
		t.Fatal("tangents generated past the limit")
	}
	maxGeneratedIndices = saved
	shared, err := NewSharedGeometry(g)
	check(t, err)
	_, err = s.SetShared("shared", shared)
	check(t, err)
	f = frame(t, s)
	if len(f.Geometry[len(f.Geometry)-1].Tangents) != 4 {
		t.Fatal("shared geometry tangents not generated")
	}
}

func TestAuthoredTangentShading(t *testing.T) {
	f := normalFrame(t)
	g := &f.Geometry[0]
	for range g.Vertices {
		g.Tangents = append(g.Tangents, Tangent{Direction: nt3dmath.Vec3{X: 1}, W: 1})
	}
	red := func() uint8 { return at(softwareRender(f), 40, 40).R }
	if r := red(); r < 180 {
		t.Fatalf("authored +X tangent did not tilt toward the light: %d", r)
	}
	for i := range g.Tangents {
		g.Tangents[i].Direction = nt3dmath.Vec3{X: -1}
	}
	if r := red(); r > 3 {
		t.Fatalf("authored -X tangent ignored: %d", r)
	}
	// A map tilted +Y under a +Y light: the handedness picks the bitangent.
	g.Material.NormalMap = testNormalMap(t, color.NRGBA{128, 218, 218, 255})
	f.Lighting.Lights[0].Direction = nt3dmath.Vec3{Y: 1}
	for i := range g.Tangents {
		g.Tangents[i] = Tangent{Direction: nt3dmath.Vec3{X: 1}, W: 1}
	}
	if r := red(); r < 180 {
		t.Fatalf("W +1 did not tilt toward the light: %d", r)
	}
	for i := range g.Tangents {
		g.Tangents[i].W = -1
	}
	if r := red(); r > 3 {
		t.Fatalf("W -1 ignored: %d", r)
	}
	// A tangent along the normal leaves it unperturbed.
	for i := range g.Tangents {
		g.Tangents[i] = Tangent{Direction: nt3dmath.Vec3{Z: 1}, W: 1}
	}
	flat := softwareRender(f)
	g.Material.NormalStrength = 0
	if got, want := at(softwareRender(f), 40, 40), at(flat, 40, 40); got != want {
		t.Fatalf("parallel tangent perturbed: %v, want %v", want, got)
	}
	g.Material.NormalStrength = 1
	// The frame turns with the instance.
	for i := range g.Tangents {
		g.Tangents[i] = Tangent{Direction: nt3dmath.Vec3{X: 1}, W: 1}
	}
	g.Material.NormalMap = testNormalMap(t, color.NRGBA{218, 128, 218, 255})
	f.Instances = []Instance{{World: nt3dmath.RotationZ(math.Pi / 2), Visible: true}}
	if r := red(); r < 180 {
		t.Fatalf("rotated frame did not follow the instance: %d", r)
	}
}

func TestDeformedTangents(t *testing.T) {
	s := NewScene()
	joint, err := s.Add(Root, "joint")
	check(t, err)
	g := tangentQuad(t)
	id, err := s.SetGeometry("mesh", g)
	check(t, err)
	in := make([]Influence, len(g.Vertices))
	for i := range in {
		in[i].Weights[0] = 1
	}
	check(t, s.SetDeformation(id, Deformation{Skeleton: &Skeleton{Joints: []NodeID{joint}}, Influences: in}))
	check(t, s.SetTransform(joint, nt3dmath.RotationZ(math.Pi/2)))
	f, err := s.Frame(DefaultCamera(), 80, 80)
	check(t, err)
	if tan := f.Geometry[0].Tangents[0]; !nearVec(tan.Direction, nt3dmath.Vec3{Y: 1}, 1e-5) || tan.W != 1 {
		t.Fatalf("skinned tangent %v", tan)
	}
	f.Lighting = Lighting{Lights: []Light{{Direction: nt3dmath.Vec3{Y: 1}}}}
	if r := at(softwareRender(f), 40, 40).R; r < 180 {
		t.Fatalf("skinned frame did not follow the joint: %d", r)
	}

	id, err = s.SetGeometry("morphed", g)
	check(t, err)
	deltas := make([]nt3dmath.Vec3, len(g.Vertices))
	for i := range deltas {
		deltas[i] = nt3dmath.Vec3{X: -2}
	}
	check(t, s.SetDeformation(id, Deformation{Morphs: []MorphTarget{{Tangents: deltas}}, Weights: []float32{1}}))
	f, err = s.Frame(DefaultCamera(), 80, 80)
	check(t, err)
	if tan := f.Geometry[1].Tangents[1]; !nearVec(tan.Direction, nt3dmath.Vec3{X: -1}, 1e-5) || tan.W != 1 {
		t.Fatalf("morphed tangent %v", tan)
	}
	wantError(t, s.SetDeformation(id, Deformation{Morphs: []MorphTarget{{Tangents: deltas[:3]}}}), "morph vertex count mismatch")
}

// Faces are neighbors only across an edge they wind opposite ways, as
// MikkTSpace pairs them: a face wound the other way, even with its UVs
// mirrored to keep its handedness, does not share a group.
func TestGenerateTangentsWindingNeighbors(t *testing.T) {
	var g Geometry
	for _, p := range [][2]float32{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		g.Vertices = append(g.Vertices, Vertex{Position: nt3dmath.Vec3{X: p[0], Y: p[1]}, Normal: nt3dmath.Vec3{Z: 1}, Color: white})
	}
	g.UV = []UV{{0, 0}, {1, 0}, {0, 1}, {0, 0}}
	// The second face runs the shared edge 1→2 the same way as the first.
	g.Indices = []uint32{0, 1, 2, 1, 2, 3}
	out, err := GenerateTangents(g)
	check(t, err)
	for _, i := range []int{1, 2} {
		if !nearVec(out.Tangents[i].Direction, nt3dmath.Vec3{X: 1}, 1e-5) {
			t.Fatalf("vertex %d averaged across a fold: %v", i, out.Tangents[i])
		}
	}
	if !nearVec(out.Tangents[3].Direction, nt3dmath.Vec3{Y: -1}, 1e-5) {
		t.Fatalf("second face's own frame %v", out.Tangents[3])
	}
}
