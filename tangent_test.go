// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
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
