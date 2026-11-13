// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image"
	"image/color"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

func TestFrameStatsCullingVisibilityAndBatches(t *testing.T) {
	s := NewScene()
	baseTex, err := NewTexture(image.NewRGBA(image.Rect(0, 0, 2, 2)))
	if err != nil {
		t.Fatal(err)
	}
	mesh, err := BuildSphere(Sphere{Radius: .5, Segments: 8, Rings: 4, Material: &Material{Texture: baseTex}})
	if err != nil {
		t.Fatal(err)
	}
	meshTris := len(mesh.Indices) / 3
	shared, err := NewSharedGeometry(mesh)
	if err != nil {
		t.Fatal(err)
	}

	// Three shared instances at/near origin, one hidden, one far outside frustum.
	a, err := s.SetShared("a", shared)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.SetShared("b", shared)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTransform(b, nt3dmath.Translation(nt3dmath.Vec3{X: .5})); err != nil {
		t.Fatal(err)
	}
	hidden, err := s.SetShared("hidden", shared)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetVisible(hidden, false); err != nil {
		t.Fatal(err)
	}
	offscreen, err := s.SetShared("offscreen", shared)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTransform(offscreen, nt3dmath.Translation(nt3dmath.Vec3{X: 500})); err != nil {
		t.Fatal(err)
	}

	// Empty placeholder geometry draws nothing.
	if _, err := s.SetGeometry("empty", Geometry{}); err != nil {
		t.Fatal(err)
	}

	cam := DefaultCamera()
	cam.Projection = Perspective
	cam.Distance = 6
	f, err := s.Frame(cam, 80, 60)
	if err != nil {
		t.Fatal(err)
	}
	st := f.Stats()
	if st.Instances != 5 {
		t.Fatalf("Instances = %d, want 5", st.Instances)
	}
	if st.VisibleInstances != 4 {
		t.Fatalf("VisibleInstances = %d, want 4", st.VisibleInstances)
	}
	if st.DrawnInstances != 2 {
		t.Fatalf("DrawnInstances = %d, want 2 (a and b)", st.DrawnInstances)
	}
	if st.Batches != 1 {
		t.Fatalf("Batches = %d, want 1 shared batch", st.Batches)
	}
	if st.Triangles != 2*meshTris {
		t.Fatalf("Triangles = %d, want %d", st.Triangles, 2*meshTris)
	}

	// Assigning a distinct texture to node b splits the shared batch into 2.
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	tex, err := NewTexture(img)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTexture("b", tex); err != nil {
		t.Fatal(err)
	}
	f, err = s.Frame(cam, 80, 60)
	if err != nil {
		t.Fatal(err)
	}
	if st = f.Stats(); st.Batches != 2 {
		t.Fatalf("Batches with distinct texture = %d, want 2", st.Batches)
	}

	// Disabling frustum culling includes the offscreen shared instance with a's untextured batch.
	f.Cull = false
	if st = f.Stats(); st.DrawnInstances != 3 || st.Batches != 2 || st.Triangles != 3*meshTris {
		t.Fatalf("Cull=false stats = %+v, want DrawnInstances=3 Batches=2 Triangles=%d", st, 3*meshTris)
	}

	// Transparent shared instances do not merge into one batch.
	glass := mesh
	glass.Material = &Material{Transparent: true}
	glassShared, err := NewSharedGeometry(glass)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeShared(a, glassShared); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeShared(b, glassShared); err != nil {
		t.Fatal(err)
	}
	f, err = s.Frame(cam, 80, 60)
	if err != nil {
		t.Fatal(err)
	}
	if st = f.Stats(); st.DrawnInstances != 2 || st.Batches != 2 {
		t.Fatalf("transparent shared stats = %+v, want DrawnInstances=2 Batches=2", st)
	}
}

func TestFrameStatsPrimitivesAndSplatBudget(t *testing.T) {
	s := NewScene()

	// Unindexed triangle + box + lines + arrow on one node.
	unindexed := Geometry{
		Vertices: []Vertex{
			{Position: nt3dmath.Vec3{X: 0, Y: 0, Z: 0}, Normal: nt3dmath.Vec3{Z: 1}, Color: color.RGBA{255, 0, 0, 255}},
			{Position: nt3dmath.Vec3{X: .5, Y: 0, Z: 0}, Normal: nt3dmath.Vec3{Z: 1}, Color: color.RGBA{255, 0, 0, 255}},
			{Position: nt3dmath.Vec3{X: 0, Y: .5, Z: 0}, Normal: nt3dmath.Vec3{Z: 1}, Color: color.RGBA{255, 0, 0, 255}},
		},
		Boxes: []Box{{Size: nt3dmath.Vec3{X: .2, Y: .2, Z: .2}, Color: color.RGBA{0, 255, 0, 255}}},
		Lines: []Vertex{
			{Position: nt3dmath.Vec3{X: 0}, Color: color.RGBA{255, 255, 255, 255}},
			{Position: nt3dmath.Vec3{X: .4}, Color: color.RGBA{255, 255, 255, 255}},
		},
		Arrows: []Arrow{{End: nt3dmath.Vec3{Z: .5}, HeadSize: 4, Color: color.RGBA{255, 255, 0, 255}}},
		Points: []Point{
			{Position: nt3dmath.Vec3{X: .1}, Radius: 2, Color: color.RGBA{255, 255, 255, 255}},
			{Position: nt3dmath.Vec3{X: .2}, Radius: 2, Color: color.RGBA{255, 255, 255, 255}},
		},
	}
	if _, err := s.SetGeometry("mixed", unindexed); err != nil {
		t.Fatal(err)
	}

	// Clustered alpha-blended splat cloud so SplatBudget thins Points.
	splats := lodCloud(t, 200, 64)
	if _, err := s.SetGeometry("splats", splats); err != nil {
		t.Fatal(err)
	}

	cam := DefaultCamera()
	cam.Distance = 8
	f, err := s.Frame(cam, 64, 64)
	if err != nil {
		t.Fatal(err)
	}
	f.SplatBudget = 20
	f.DynamicLines = []Vertex{
		{Position: nt3dmath.Vec3{X: -.5}, Color: color.RGBA{255, 255, 255, 255}},
		{Position: nt3dmath.Vec3{X: .5}, Color: color.RGBA{255, 255, 255, 255}},
	}

	drawnSplats, _ := f.SplatSelection()
	st := f.Stats()
	if st.Triangles != 1+12 {
		t.Fatalf("Triangles = %d, want 13 (1 unindexed + 12 box)", st.Triangles)
	}
	if st.Lines != 3 {
		t.Fatalf("Lines = %d, want 3 (1 line + 1 arrow + 1 dynamic line)", st.Lines)
	}
	if st.Points != 2+drawnSplats {
		t.Fatalf("Points = %d, want %d (2 opaque + %d budgeted splats)", st.Points, 2+drawnSplats, drawnSplats)
	}
}

func TestFrameStatsInvalidFramesReturnZero(t *testing.T) {
	if (Frame{}).Stats() != (FrameStats{}) {
		t.Fatal("zero Frame should report zero FrameStats")
	}
	bad := Frame{
		Width:     64,
		Height:    64,
		Matrix:    nt3dmath.Identity(),
		Geometry:  []Geometry{{}},
		Instances: []Instance{{}, {}}, // mismatched length
	}
	if bad.Stats() != (FrameStats{}) {
		t.Fatal("mismatched Instances should report zero FrameStats")
	}
}
