// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"math"
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
)

// fuzzOptions keeps decoding cheap enough for a shared fuzzing host.
func fuzzOptions() *nt3dmesh.Options {
	return &nt3dmesh.Options{MaxTriangles: 2048, MaxBytes: 1 << 20, NoClusters: true}
}

// checkModel is the decoder oracle: refusing input is fine, but an accepted
// model respects its budget, indexes its own vertices, holds finite
// coordinates, validates as nt3d geometry, and instantiates cleanly through
// SceneAsset, material variants, multi-LOD compilation, and Scene.Frame.
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
	if m.Asset != nil {
		checkAsset(t, m)
	}
}

func checkAsset(t *testing.T, m *nt3dmesh.Model) {
	s := nt3d.NewScene()
	inst, err := m.Asset.Instantiate(s, nt3d.Root, "fuzz")
	if err != nil {
		t.Fatalf("Asset.Instantiate failed on accepted glTF model: %v", err)
	}
	checkSceneFrame(t, s)

	if vars := m.Variants(); len(vars) > 0 {
		if err := s.SetMaterialVariant(inst.Root, vars[0]); err != nil {
			t.Fatalf("SetMaterialVariant(%q): %v", vars[0], err)
		}
		checkSceneFrame(t, s)
		if err := s.SetMaterialVariant(inst.Root, ""); err != nil {
			t.Fatalf("SetMaterialVariant reset: %v", err)
		}
	}

	if tiers := m.LODTiers(); tiers >= 2 && tiers <= 8 {
		dists := make([]float32, tiers-1)
		for i := range dists {
			dists[i] = float32(i+1) * 10
		}
		lodAsset, err := m.LODAsset(dists...)
		if err != nil {
			t.Fatalf("LODAsset(%v) failed after LODTiers()=%d: %v", dists, tiers, err)
		}
		sLOD := nt3d.NewScene()
		lodInst, err := lodAsset.Instantiate(sLOD, nt3d.Root, "fuzz-lod")
		if err != nil {
			t.Fatalf("lodAsset.Instantiate: %v", err)
		}
		if vars := m.Variants(); len(vars) > 0 {
			if err := sLOD.SetMaterialVariant(lodInst.Root, vars[0]); err != nil {
				t.Fatalf("LOD SetMaterialVariant(%q): %v", vars[0], err)
			}
		}
		checkSceneFrame(t, sLOD)
	}
}

func checkSceneFrame(t *testing.T, s *nt3d.Scene) {
	f, err := s.Frame(nt3d.DefaultCamera(), 64, 48)
	if err != nil {
		t.Fatalf("Scene.Frame: %v", err)
	}
	for i := range f.Geometry {
		if err := f.Geometry[i].Validate(); err != nil {
			t.Fatalf("Frame.Geometry[%d]: %v", i, err)
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
