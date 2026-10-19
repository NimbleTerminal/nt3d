// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"math"
	"math/rand/v2"
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
)

// A splat capture frames its subject, the dense middle of the cloud, not
// the shell of background splats around it; a mesh has no such framing.
func TestSplatFraming(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	style := nt3d.PointStyle{Shape: nt3d.GaussianPoint, Units: nt3d.WorldUnits, Blend: nt3d.AlphaBlend}
	var subject, shell nt3d.Geometry
	subject.PointStyle, shell.PointStyle = style, style
	for range 1000 {
		subject.Points = append(subject.Points, nt3d.Point{Position: nt3dmath.Vec3{X: 1 + .1*float32(rng.NormFloat64()), Y: 2 + .1*float32(rng.NormFloat64()), Z: 3 + .1*float32(rng.NormFloat64())}, Radius: 1})
	}
	for range 100 {
		shell.Points = append(shell.Points, nt3d.Point{Position: nt3dmath.Vec3{X: 50 * float32(rng.NormFloat64()), Y: 50 * float32(rng.NormFloat64()), Z: 50 * float32(rng.NormFloat64())}, Radius: 1})
	}
	m := &nt3dmesh.Model{Objects: []nt3dmesh.Object{{Geometry: subject}, {Geometry: shell}}}
	box, ok := splatFraming(m)
	if !ok {
		t.Fatal("a splat cloud has no framing")
	}
	center, size := box.Min.Add(box.Max).Scale(.5), box.Max.Sub(box.Min)
	if math.Abs(float64(center.X-1)) > .05 || math.Abs(float64(center.Y-2)) > .05 || math.Abs(float64(center.Z-3)) > .05 {
		t.Fatalf("center %v, want about (1, 2, 3)", center)
	}
	// Points scatter about 0.17 from the center on median; the box spans
	// about that in all.
	if size.X < .15 || size.X > .4 || math.Abs(float64(size.X-size.Y)) > 1e-5 || math.Abs(float64(size.Y-size.Z)) > 1e-5 {
		t.Fatalf("box size %v", size)
	}
	mesh := &nt3dmesh.Model{Objects: []nt3dmesh.Object{{Geometry: nt3d.Geometry{Vertices: make([]nt3d.Vertex, 3), Indices: []uint32{0, 1, 2}}}}}
	if _, ok := splatFraming(mesh); ok {
		t.Fatal("a mesh has a splat framing")
	}
}
