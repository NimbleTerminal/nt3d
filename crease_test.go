// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"math"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func creaseCube(t *testing.T) Geometry {
	t.Helper()
	g, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 1, Y: 1, Z: 1}, Color: white})
	check(t, err)
	return g
}

// hinge is two unit quads sharing the Y axis edge, the second turned up by
// degrees from the first's plane.
func hinge(degrees float64) Geometry {
	r := degrees * math.Pi / 180
	c, s := float32(math.Cos(r)), float32(math.Sin(r))
	p := []nt3dmath.Vec3{{X: -1}, {}, {Y: 1}, {X: -1, Y: 1}, {X: c, Z: s}, {X: c, Y: 1, Z: s}}
	g := Geometry{Indices: []uint32{0, 1, 2, 0, 2, 3, 1, 4, 5, 1, 5, 2}}
	for _, v := range p {
		g.Vertices = append(g.Vertices, Vertex{Position: v, Normal: up, Color: white})
	}
	return g
}

func hasEdge(edges []nt3dmath.Vec3, a, b nt3dmath.Vec3) int {
	n := 0
	for i := 0; i+1 < len(edges); i += 2 {
		if edges[i] == a && edges[i+1] == b || edges[i] == b && edges[i+1] == a {
			n++
		}
	}
	return n
}

func TestCreaseEdgesCube(t *testing.T) {
	g := creaseCube(t)
	edges := creaseEdges(g.Vertices, g.Indices, 30)
	if len(edges) != 24 {
		t.Fatalf("%d endpoints, want 24", len(edges))
	}
	for i := 0; i < len(edges); i += 2 {
		d := edges[i+1].Sub(edges[i])
		axes := 0
		for _, v := range []float32{d.X, d.Y, d.Z} {
			if v != 0 {
				axes++
				if abs(abs(v)-1) > 1e-6 {
					t.Fatalf("edge %v is not a side long", d)
				}
			}
		}
		if axes != 1 {
			t.Fatalf("edge %v is not along an axis", d)
		}
	}
}

func TestCreaseEdgesSphere(t *testing.T) {
	g, err := BuildSphere(Sphere{Radius: 1, Segments: 40, Rings: 24, Color: white})
	check(t, err)
	if edges := creaseEdges(g.Vertices, g.Indices, 30); edges != nil {
		t.Fatalf("smooth sphere has %d crease endpoints", len(edges))
	}
	if creaseEdges(g.Vertices, g.Indices, 1) == nil {
		t.Fatal("a one degree threshold found no edges on a faceted sphere")
	}
}

func TestCreaseEdgesOpenQuad(t *testing.T) {
	g := quad(1, 0, white, nil, false)
	edges := creaseEdges(g.Vertices, g.Indices, 30)
	if len(edges) != 8 {
		t.Fatalf("%d endpoints, want 8", len(edges))
	}
	if hasEdge(edges, g.Vertices[0].Position, g.Vertices[2].Position) != 0 {
		t.Fatal("the diagonal got a line")
	}
}

func TestCreaseEdgesSplitNormals(t *testing.T) {
	g := creaseCube(t)
	if len(g.Vertices) != 24 {
		t.Fatalf("cuboid has %d vertices; the test wants per-face normals", len(g.Vertices))
	}
	// The same cube welded to eight vertices.
	welded := Geometry{}
	index := map[nt3dmath.Vec3]uint32{}
	for _, i := range g.Indices {
		p := g.Vertices[i].Position
		if _, ok := index[p]; !ok {
			index[p] = uint32(len(welded.Vertices))
			welded.Vertices = append(welded.Vertices, Vertex{Position: p, Normal: up, Color: white})
		}
		welded.Indices = append(welded.Indices, index[p])
	}
	split, joined := creaseEdges(g.Vertices, g.Indices, 30), creaseEdges(welded.Vertices, welded.Indices, 30)
	if len(welded.Vertices) != 8 || len(split) != 24 || len(joined) != 24 {
		t.Fatalf("%d welded vertices, %d and %d endpoints", len(welded.Vertices), len(split), len(joined))
	}
	for i := 0; i < len(split); i += 2 {
		if hasEdge(joined, split[i], split[i+1]) != 1 {
			t.Fatalf("edge %v %v is not in the welded cube's", split[i], split[i+1])
		}
	}
}

func TestCreaseEdgesThreshold(t *testing.T) {
	g := hinge(40)
	a, b := g.Vertices[1].Position, g.Vertices[2].Position
	if hasEdge(creaseEdges(g.Vertices, g.Indices, 30), a, b) != 1 {
		t.Fatal("a 40 degree hinge has no line at 30")
	}
	if hasEdge(creaseEdges(g.Vertices, g.Indices, 50), a, b) != 0 {
		t.Fatal("a 40 degree hinge has a line at 50")
	}
}

func TestCreaseEdgesDegenerate(t *testing.T) {
	// Three fins on one edge, and a triangle of no area.
	g := Geometry{Indices: []uint32{0, 1, 2, 0, 1, 3, 0, 1, 4, 2, 2, 3, 0, 5, 1}}
	for _, p := range []nt3dmath.Vec3{{}, {Y: 1}, {X: 1}, {X: -1}, {Z: 1}, {Y: .5}} {
		g.Vertices = append(g.Vertices, Vertex{Position: p, Normal: up, Color: white})
	}
	edges := creaseEdges(g.Vertices, g.Indices, 30)
	for _, p := range edges {
		if !validFloat(p.X, p.Y, p.Z) {
			t.Fatalf("endpoint %v", p)
		}
	}
	if n := hasEdge(edges, g.Vertices[0].Position, g.Vertices[1].Position); n != 1 {
		t.Fatalf("the three-face edge appears %d times", n)
	}
}

func TestCreaseDisabled(t *testing.T) {
	g := creaseCube(t)
	if creaseEdges(g.Vertices, g.Indices, 0) != nil || creaseEdges(g.Vertices, nil, 30) != nil {
		t.Fatal("edges without a threshold or without triangles")
	}
}

func TestCreaseValidation(t *testing.T) {
	s := NewScene()
	_, err := s.SetGeometry("cube", creaseCube(t))
	check(t, err)
	for _, c := range []float32{-1, 181, float32(math.NaN())} {
		style := OutlineStyle{Width: 2, Crease: c}
		if style.validate() == nil || s.SetOutline(Root, style) == nil {
			t.Fatalf("accepted crease %v", c)
		}
	}
	for _, c := range []float32{0, 180} {
		check(t, s.SetOutline(Root, OutlineStyle{Width: 2, Crease: c}))
	}
}

func TestCreasedEligibility(t *testing.T) {
	style := OutlineStyle{Width: 2, Crease: 30}
	g := creaseCube(t)
	g.Outline = style
	if !creased(&g) {
		t.Fatal("an opaque rigid outlined mesh draws no crease lines")
	}
	for name, change := range map[string]func(*Geometry){
		"no width":     func(g *Geometry) { g.Outline.Width = 0 },
		"no crease":    func(g *Geometry) { g.Outline.Crease = 0 },
		"no triangles": func(g *Geometry) { g.Indices = nil },
		"transparent":  func(g *Geometry) { g.Material = &Material{Transparent: true} },
		"cutout":       func(g *Geometry) { g.Material = &Material{Cutout: true} },
		"transmitting": func(g *Geometry) { g.Material = &Material{Transmission: .5} },
	} {
		other := g
		change(&other)
		if creased(&other) {
			t.Errorf("%s: draws crease lines", name)
		}
	}
	s, _, _ := animationSkinScene(t)
	check(t, s.SetOutline(Root, style))
	if f := frame(t, s); f.Geometry[0].Outline != style || creased(&f.Geometry[0]) {
		t.Fatal("a deformed mesh draws crease lines")
	}
}
