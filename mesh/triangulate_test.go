// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import (
	"math"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// shape places 2D corners on a plane through the origin with axes u and v.
func shape(u, v nt3dmath.Vec3, corners ...[2]float32) []nt3dmath.Vec3 {
	out := make([]nt3dmath.Vec3, len(corners))
	for i, c := range corners {
		out[i] = u.Scale(c[0]).Add(v.Scale(c[1]))
	}
	return out
}

func area(a, b, c nt3dmath.Vec3, normal nt3dmath.Vec3) float64 {
	return float64(b.Sub(a).Cross(c.Sub(a)).Dot(normal)) / 2
}

func TestTriangulateConcavePolygons(t *testing.T) {
	l := [][2]float32{{0, 0}, {3, 0}, {3, 1}, {1, 1}, {1, 3}, {0, 3}}                                                    // area 5
	dart := [][2]float32{{0, 0}, {2, 1}, {4, 0}, {2, 4}}                                                                 // area 4
	comb := [][2]float32{{0, 0}, {5, 0}, {5, 3}, {4, 3}, {4, 1}, {3, 1}, {3, 3}, {2, 3}, {2, 1}, {1, 1}, {1, 3}, {0, 3}} // area 9
	star := make([][2]float32, 10)
	for i := range star {
		r := float64(1)
		if i%2 == 1 {
			r = .4
		}
		angle := float64(i) * math.Pi / 5
		star[i] = [2]float32{float32(r * math.Cos(angle)), float32(r * math.Sin(angle))}
	}
	starArea := 0.0
	for i := range star {
		p, q := star[i], star[(i+1)%10]
		starArea += float64(p[0]*q[1]-q[0]*p[1]) / 2
	}
	planes := map[string][2]nt3dmath.Vec3{
		"xy":        {{X: 1}, {Y: 1}},
		"xz":        {{X: 1}, {Z: 1}},
		"clockwise": {{Y: 1}, {X: 1}},
		"tilted":    {nt3dmath.Vec3{X: 1, Z: .5}.Normalize(), nt3dmath.Vec3{Y: 1, Z: -.3}.Normalize()},
	}
	for name, corners := range map[string][][2]float32{"L": l, "dart": dart, "comb": comb, "star": star} {
		for plane, axes := range planes {
			points := shape(axes[0], axes[1], corners...)
			normal := axes[0].Cross(axes[1]).Normalize()
			want := 0.0
			for i := range points {
				want += area(nt3dmath.Vec3{}, points[i], points[(i+1)%len(points)], normal)
			}
			tris := triangulate(points)
			if len(tris) != len(points)-2 {
				t.Fatalf("%s in %s: %d triangles", name, plane, len(tris))
			}
			got := 0.0
			for _, tri := range tris {
				a := area(points[tri[0]], points[tri[1]], points[tri[2]], normal)
				if a <= 0 {
					t.Fatalf("%s in %s: triangle %v wound against the polygon", name, plane, tri)
				}
				got += a
			}
			// Equal area with every triangle the polygon's way round means
			// they tile it: none overlap or reach outside.
			if math.Abs(got-want) > 1e-4*math.Abs(want) {
				t.Fatalf("%s in %s: triangles cover %v of %v", name, plane, got, want)
			}
		}
	}
}

func TestTriangulateDegeneratePolygons(t *testing.T) {
	xy := func(c ...[2]float32) []nt3dmath.Vec3 { return shape(nt3dmath.Vec3{X: 1}, nt3dmath.Vec3{Y: 1}, c...) }
	for name, points := range map[string][]nt3dmath.Vec3{
		"bowtie":     xy([2]float32{0, 0}, [2]float32{2, 2}, [2]float32{2, 0}, [2]float32{0, 2}),
		"collinear":  xy([2]float32{0, 0}, [2]float32{1, 0}, [2]float32{2, 0}, [2]float32{2, 2}, [2]float32{1, 1}, [2]float32{0, 2}),
		"flat":       xy([2]float32{0, 0}, [2]float32{1, 0}, [2]float32{2, 0}, [2]float32{3, 0}),
		"repeated":   xy([2]float32{0, 0}, [2]float32{2, 0}, [2]float32{2, 0}, [2]float32{1, 1}, [2]float32{2, 2}, [2]float32{0, 2}),
		"many sides": shape(nt3dmath.Vec3{X: 1}, nt3dmath.Vec3{Y: 1}, make([][2]float32, maxEarClip+1)...),
	} {
		for _, tri := range triangulate(points) {
			for _, k := range tri {
				if k < 0 || k >= len(points) {
					t.Fatalf("%s: corner %d", name, k)
				}
			}
		}
	}
}

func TestOBJConcaveFace(t *testing.T) {
	m, err := DecodeOBJ([]byte(`v 0 0 0
v 2 1 0
v 4 0 0
v 2 4 0
f 1 2 3 4
`), nil)
	check(t, err)
	g := m.Objects[0].Geometry
	total := 0.0
	for i := 0; i < len(g.Indices); i += 3 {
		a := area(g.Vertices[g.Indices[i]].Position, g.Vertices[g.Indices[i+1]].Position, g.Vertices[g.Indices[i+2]].Position, nt3dmath.Vec3{Z: 1})
		if a <= 0 {
			t.Fatal("triangle wound backward")
		}
		total += a
	}
	// A fan from the first corner would include a backward triangle, outside the dart.
	if len(g.Indices) != 6 || math.Abs(total-6) > 1e-5 {
		t.Fatalf("%d indices covering %v", len(g.Indices), total)
	}
}
