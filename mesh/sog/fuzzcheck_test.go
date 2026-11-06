// Copyright (c) 2026 Neomantra Corp.
package nt3dsog

import (
	"math"
	"testing"

	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
)

// fuzzOptions keeps decoding cheap enough for a shared fuzzing host.
func fuzzOptions() *nt3dmesh.Options {
	return &nt3dmesh.Options{MaxTriangles: 2048, MaxBytes: 1 << 20, NoClusters: true}
}

// checkModel is the decoder oracle: refusing input is fine, but an accepted
// model respects its budget, indexes its own vertices, holds finite
// coordinates, and validates as nt3d geometry.
func checkModel(t *testing.T, m *nt3dmesh.Model, err error, o *nt3dmesh.Options) {
	if err != nil {
		return
	}
	if m == nil {
		t.Fatal("no model and no error")
	}
	if n := m.Triangles(); n > o.MaxTriangles {
		t.Fatalf("%d triangles over the %d budget", n, o.MaxTriangles)
	}
	for i, obj := range m.Objects {
		g := obj.Geometry
		if len(obj.Plain) != 0 && len(obj.Plain) != len(g.Vertices) {
			t.Fatalf("object %d: %d plain flags for %d vertices", i, len(obj.Plain), len(g.Vertices))
		}
		if len(g.Indices)%3 != 0 {
			t.Fatalf("object %d: %d indices", i, len(g.Indices))
		}
		for _, k := range g.Indices {
			if int(k) >= len(g.Vertices) {
				t.Fatalf("object %d: index %d of %d vertices", i, k, len(g.Vertices))
			}
		}
		for _, v := range g.Vertices {
			if !finiteVec(v.Position) || !finiteVec(v.Normal) {
				t.Fatalf("object %d: non-finite vertex %+v", i, v)
			}
		}
		for _, p := range g.Points {
			if !finiteVec(p.Position) {
				t.Fatalf("object %d: non-finite point %+v", i, p.Position)
			}
		}
		if g.GaussianSH != nil && len(g.GaussianSH)%max(1, len(g.Points)) != 0 {
			t.Fatalf("object %d: %d SH coefficients for %d points", i, len(g.GaussianSH), len(g.Points))
		}
		if err := g.Validate(); err != nil {
			t.Fatalf("object %d: %v", i, err)
		}
	}
}

func finiteVec(v nt3dmath.Vec3) bool {
	for _, x := range [3]float32{v.X, v.Y, v.Z} {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return false
		}
	}
	return true
}
