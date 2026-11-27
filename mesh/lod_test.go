// Copyright (c) 2026 Neomantra Corp.

package nt3dmesh

import (
	"image/color"
	"testing"

	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func TestMeshCombineLODAndValidation(t *testing.T) {
	const objFine = `o Bench_LOD0
v -1 0 -1
v  1 0 -1
v  1 0  1
v -1 0  1
v  0 0  2
f 1 2 3 4
f 4 3 5
`
	const objCoarse = `o Bench_LOD1
v -1 0 -1
v  1 0 -1
v  0 0  1
f 1 2 3
`
	fine, err := DecodeOBJ([]byte(objFine), nil)
	if err != nil {
		t.Fatal(err)
	}
	coarse, err := DecodeOBJ([]byte(objCoarse), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Non-triangle point-cloud impostor tier!
	ptImpostor := &Model{
		Objects: []Object{
			{
				Name: "Bench_LOD2",
				Geometry: nt3d.Geometry{
					Points: []nt3d.Point{{Position: nt3dmath.Vec3{}, Color: color.RGBA{200, 200, 200, 255}, Radius: 4}},
				},
			},
		},
	}

	// Flat OBJ has nil Asset -> LODAsset returns helpful error pointing to CombineLOD.
	if _, err := fine.LODAsset(20); err == nil {
		t.Fatal("expected error calling LODAsset on flat OBJ without Asset")
	}
	var nilModel *Model
	if _, err := nilModel.LODAsset(20); err == nil {
		t.Fatal("expected error calling LODAsset on nil Model")
	}
	if _, err := CombineLOD(nil); err == nil {
		t.Fatal("expected error for empty CombineLOD")
	}
	if _, err := CombineLOD([]ModelLOD{{MaxDistance: 10, Model: nil}}); err == nil {
		t.Fatal("expected error for nil Model in CombineLOD")
	}

	asset, err := CombineLOD([]ModelLOD{
		{MaxDistance: 20, Model: fine},
		{MaxDistance: 60, Model: coarse},
		{MaxDistance: 0, Model: ptImpostor},
	})
	if err != nil {
		t.Fatal(err)
	}

	s := nt3d.NewScene()
	if _, err := asset.Instantiate(s, nt3d.Root, "bench-a"); err != nil {
		t.Fatal(err)
	}
	instB, err := asset.Instantiate(s, nt3d.Root, "bench-b")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTransform(instB.Root, nt3dmath.Translation(nt3dmath.Vec3{Y: 30})); err != nil {
		t.Fatal(err)
	}
	// AddLODModels one-liner at Y = 90 (selects point impostor tier 2).
	farRoot, err := AddLODModels(s, nt3d.Root, "bench-c", []ModelLOD{
		{MaxDistance: 20, Model: fine},
		{MaxDistance: 60, Model: coarse},
		{MaxDistance: 0, Model: ptImpostor},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTransform(farRoot, nt3dmath.Translation(nt3dmath.Vec3{Y: 90})); err != nil {
		t.Fatal(err)
	}

	cam := nt3d.Camera{
		Target:     nt3dmath.Vec3{},
		Distance:   10,
		Azimuth:    -90, // Eye at (0, -10, 0) looking +Y
		Near:       0.1,
		Far:        300,
		Projection: nt3d.Perspective,
	}
	f, err := s.Frame(cam, 200, 150)
	if err != nil {
		t.Fatal(err)
	}
	st := f.Stats()
	// bench-a at Y=0 (dist=10 <= 20): 3 tris (fine)
	// bench-b at Y=30 (dist=40 in (20,60]): 1 tri (coarse)
	// bench-c at Y=90 (dist=100 > 60): 1 point (impostor)
	if st.Triangles != 4 || st.Points != 1 {
		t.Fatalf("got tris=%d points=%d, want tris=4 points=1 (stats=%+v)", st.Triangles, st.Points, st)
	}
}

// Tiers placed far from zero keep their placement through AddLODModels, as
// through AddTo: the node takes their shared Origin. Tiers of one thing
// placed at different points are refused.
func TestAddLODModelsKeepsTheModelsOrigin(t *testing.T) {
	tier := func(origin nt3dmath.Vec3d) *Model {
		g := nt3d.Geometry{Vertices: []nt3d.Vertex{{Position: nt3dmath.Vec3{}}, {Position: nt3dmath.Vec3{X: 1}}, {Position: nt3dmath.Vec3{Y: 1}}}, Indices: []uint32{0, 1, 2}}
		return &Model{Objects: []Object{{Name: "part", Geometry: g}}, Origin: origin}
	}
	place := nt3dmath.Vec3d{X: 1364789.133, Y: -4619824.799, Z: 4166319.166}
	s := nt3d.NewScene()
	root, err := AddLODModels(s, nt3d.Root, "far", []ModelLOD{{MaxDistance: 20, Model: tier(place)}, {MaxDistance: 0, Model: tier(place)}})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.NodeOrigin(root); got != [3]float64{place.X, place.Y, place.Z} {
		t.Fatalf("the node's origin is %v, want the tiers' Origin", got)
	}
	if _, err := AddLODModels(s, nt3d.Root, "apart", []ModelLOD{{MaxDistance: 20, Model: tier(place)}, {MaxDistance: 0, Model: tier(nt3dmath.Vec3d{})}}); err == nil {
		t.Fatal("tiers placed at different origins were combined")
	}
	if _, found := s.Find("apart"); found {
		t.Fatal("a refused LOD left a node behind")
	}
}
