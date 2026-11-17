// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image/color"
	"math"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// FuzzSceneAssetLOD drives (*SceneAsset).LODTiers, Bounds, Triangles, WithLOD,
// CombineAssetLOD, SceneAsset.Instantiate, SetMaterialVariant, SetLODBias,
// SetLODHysteresis, and Scene.Frame across byte-coded node hierarchies,
// sibling *_LOD0..N names, showcase offsets, shared/unshared meshes, and
// fuzzed parent links (including cyclic/invalid graphs that must be rejected
// cleanly without panicking or looping).
func FuzzSceneAssetLOD(f *testing.F) {
	// Seed 0: valid two-tier Bench_LOD0 + Bench_LOD1 under root 0.
	f.Add([]byte{
		255, 0, 0, 0, 0, 1,
		0, 1, 0, 0, 0, 1, // Bench_LOD0
		0, 2, 4, 0, 0, 2, // Bench_LOD1 (showcase offset +4X)
	}, float32(25), float32(80), float32(1), float32(0.05))
	// Seed 1: three-tier kitbash + non-LOD sibling.
	f.Add([]byte{
		255, 0, 0, 0, 0, 0,
		0, 3, 0, 0, 0, 1, // Lamp-lod0
		0, 4, 0, 0, 0, 2, // Lamp-lod1
		0, 5, 0, 0, 0, 3, // Lamp-lod2
		0, 6, 2, 0, 0, 1, // Static Curb
	}, float32(15), float32(50), float32(1.2), float32(0.1))
	// Seed 2: cyclic parent links (must not loop or panic).
	f.Add([]byte{
		1, 1, 0, 0, 0, 1,
		0, 2, 0, 0, 0, 1,
	}, float32(20), float32(60), float32(1), float32(0))
	// Seed 3: lone *_LOD0 parent container wrapping a 3-tier Lamp-lod0..2 group.
	f.Add([]byte("0100001X00001\x9d00011Y0000"), float32(19.5), float32(80), float32(1), float32(0.01))

	baseTri := Geometry{
		Vertices: []Vertex{
			{Position: nt3dmath.Vec3{X: 0, Y: 0, Z: 0}, Normal: nt3dmath.Vec3{Z: 1}, Color: color.RGBA{220, 220, 220, 255}},
			{Position: nt3dmath.Vec3{X: 1, Y: 0, Z: 0}, Normal: nt3dmath.Vec3{Z: 1}, Color: color.RGBA{220, 220, 220, 255}},
			{Position: nt3dmath.Vec3{X: 0, Y: 1, Z: 0}, Normal: nt3dmath.Vec3{Z: 1}, Color: color.RGBA{220, 220, 220, 255}},
		},
		Indices: []uint32{0, 1, 2},
	}
	baseTri.Bounds = GeometryBounds(baseTri)
	sharedA, err := NewSharedGeometry(baseTri)
	if err != nil {
		f.Fatal(err)
	}
	coarseTri := baseTri
	coarseTri.Vertices = append([]Vertex(nil), baseTri.Vertices...)
	coarseTri.Vertices[1].Position.X = 0.9
	coarseTri.Bounds = GeometryBounds(coarseTri)
	sharedB, err := NewSharedGeometry(coarseTri)
	if err != nil {
		f.Fatal(err)
	}

	names := [...]string{
		"Root",
		"Bench_LOD0",
		"Bench_LOD1",
		"Lamp-lod0",
		"Lamp-lod1",
		"Lamp-lod2",
		"Curb",
		"LOD0",
		"LOD1",
		"Tree.LOD00",
		"Tree.LOD01",
		"Prop_LOD99999999999999999999",
		"",
		"_LOD0",
	}

	matTaxi := &Material{PBR: true, Roughness: .4, BaseColor: color.RGBA{255, 200, 20, 255}}
	matMatte := &Material{Roughness: .8, SpecularColor: color.RGBA{255, 255, 255, 0}}

	f.Fuzz(func(t *testing.T, program []byte, d0, d1, bias, hyst float32) {
		nNodes := min(20, len(program)/6)
		asset := &SceneAsset{
			Nodes:    make([]AssetNode, nNodes),
			Variants: []string{"Taxi", "Matte"},
		}
		for i := range nNodes {
			rec := program[i*6 : (i+1)*6]
			parent := int(int8(rec[0]))
			if rec[0] == 255 {
				parent = -1
			} else if nNodes > 0 {
				parent = int(rec[0])%max(1, nNodes+2) - 1
			}
			name := names[int(rec[1])%len(names)]
			tx := float32(int8(rec[2])) * 0.5
			ty := float32(int8(rec[3])) * 0.5
			scale := float32(1)
			if rec[4]&3 == 1 {
				scale = 2
			} else if rec[4]&3 == 2 {
				scale = 0 // Singular matrix test case!
			}
			local := nt3dmath.Mat4{
				scale, 0, 0, 0,
				0, scale, 0, 0,
				0, 0, scale, 0,
				tx, ty, 0, 1,
			}
			node := AssetNode{
				Name:   name,
				Parent: parent,
				Local:  local,
			}
			switch rec[5] & 3 {
			case 1:
				node.Shared = sharedA
				node.VariantMaterials = map[int]*Material{0: matTaxi, 1: matMatte}
			case 2:
				node.Shared = sharedB
				node.VariantMaterials = map[int]*Material{0: matTaxi}
			case 3:
				g := baseTri
				node.Geometry = &g
			}
			asset.Nodes[i] = node
		}

		// Inspection helpers must never panic or loop even on cyclic/singular graphs.
		_ = asset.Bounds()
		if tris := asset.Triangles(); tris < 0 {
			t.Fatalf("negative asset.Triangles() = %d", tris)
		}
		tiers := asset.LODTiers()

		// Test WithLOD with caller-fuzzed distances first.
		_, _ = asset.WithLOD(d0, d1)

		// Oracle: whenever LODTiers() reports K >= 2, WithLOD with K-1 valid
		// strictly increasing positive distances MUST succeed and instantiate cleanly!
		if tiers >= 2 && tiers <= 16 {
			dists := make([]float32, tiers-1)
			for i := range dists {
				dists[i] = float32(i+1) * 15
			}
			compiled, err := asset.WithLOD(dists...)
			if err != nil {
				t.Fatalf("WithLOD(%v) failed when LODTiers()=%d: %v", dists, tiers, err)
			}
			if got := compiled.LODTiers(); got > tiers {
				t.Fatalf("compiled.LODTiers() = %d > %d", got, tiers)
			}
			verifyCompiledLODAsset(t, compiled, bias, hyst)
		}

		// Also exercise CombineAssetLOD when asset itself instantiates cleanly on a test scene.
		sCheck := NewScene()
		if _, err := asset.Instantiate(sCheck, Root, "ok"); err == nil && asset.Triangles() > 0 && tiers == 0 {
			combined, err := CombineAssetLOD([]AssetLOD{
				{MaxDistance: 25, Asset: asset},
				{MaxDistance: 0, Asset: asset},
			})
			if err == nil {
				verifyCompiledLODAsset(t, combined, bias, hyst)
			}
		}
	})
}

func verifyCompiledLODAsset(t *testing.T, compiled *SceneAsset, bias, hyst float32) {
	t.Helper()
	s := NewScene()
	inst, err := compiled.Instantiate(s, Root, "lod")
	if err != nil {
		t.Fatalf("compiled LOD asset Instantiate failed: %v", err)
	}
	if !math.IsNaN(float64(bias)) && !math.IsInf(float64(bias), 0) && bias > 0.05 && bias < 20 {
		if err := s.SetLODBias(bias); err != nil {
			t.Fatalf("SetLODBias(%v): %v", bias, err)
		}
	}
	if !math.IsNaN(float64(hyst)) && !math.IsInf(float64(hyst), 0) && hyst >= 0 && hyst <= 0.5 {
		if err := s.SetLODHysteresis(hyst); err != nil {
			t.Fatalf("SetLODHysteresis(%v): %v", hyst, err)
		}
	}
	if len(compiled.Variants) > 0 {
		if err := s.SetMaterialVariant(inst.Root, compiled.Variants[0]); err != nil {
			t.Fatalf("SetMaterialVariant: %v", err)
		}
	}
	for _, dist := range []float64{2, 25, 120} {
		cam := DefaultCamera()
		cam.Distance = dist
		f, err := s.Frame(cam, 64, 48)
		if err != nil {
			t.Fatalf("Scene.Frame(distance=%v): %v", dist, err)
		}
		st := f.Stats()
		tierSum := 0
		for _, c := range st.LODTierInstances {
			if c < 0 {
				t.Fatalf("negative LODTierInstances count %v", st.LODTierInstances)
			}
			tierSum += c
		}
		if tierSum > st.DrawnInstances {
			t.Fatalf("LODTierInstances sum %d > DrawnInstances %d", tierSum, st.DrawnInstances)
		}
		for i := range f.Draws {
			if err := f.Draws[i].Geometry.Validate(); err != nil {
				t.Fatalf("Frame.Draws[%d].Geometry: %v", i, err)
			}
		}
	}
}
