// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"math"
	"testing"

	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func TestWaterSurfaceAndFloat(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	var err error
	d.group, err = d.scene.Add(nt3d.Root, "water-demo")
	if err != nil {
		t.Fatal(err)
	}
	if err = d.setupWater(); err != nil {
		t.Fatal(err)
	}
	d.water.moored = true
	if d.terrainScene() {
		t.Fatal("water received terrain controls")
	}
	var first nt3dmath.Mat4
	for i, time := range []float64{0, .8, 6.2831, 6.2833, 17} {
		d.animationTime = time
		if err = d.stepWater(); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < len(d.water.grid.Vertices); j += 137 {
			v := d.water.grid.Vertices[j]
			s, err := d.water.surface.AtWorld(float64(v.Position.X), float64(v.Position.Y), time)
			if err != nil || math.Abs(float64(v.Position.Z-s.Position.Z)) > 1e-5 {
				t.Fatal("mesh differs from world sample", v, s, err)
			}
		}
		n, _ := d.scene.Node(d.water.float)
		if i == 0 {
			first = n.Local
		} else if n.Local == first {
			t.Fatal("float did not move")
		}
		// Four-probe mean gives the float's waterline, with fixed draft offset.
		var height float32
		for _, p := range [][2]float64{{-.6, -.25}, {.6, -.25}, {-.6, .25}, {.6, .25}} {
			s, err := d.water.surface.AtWorld(p[0], p[1], time)
			if err != nil {
				t.Fatal(err)
			}
			height += s.Position.Z / 4
		}
		if math.Abs(float64(n.Local[14]-height-.065)) > 1e-6 {
			t.Fatal("float waterline mismatch")
		}
		x := nt3dmath.Vec3{X: n.Local[0], Y: n.Local[1], Z: n.Local[2]}
		y := nt3dmath.Vec3{X: n.Local[4], Y: n.Local[5], Z: n.Local[6]}
		z := nt3dmath.Vec3{X: n.Local[8], Y: n.Local[9], Z: n.Local[10]}
		if math.Abs(float64(x.Dot(y))) > 1e-6 || x.Cross(y).Sub(z).Dot(x.Cross(y).Sub(z)) > 1e-10 {
			t.Fatal("float basis is not orthonormal")
		}
		f, err := d.scene.Frame(nt3d.DefaultCamera(), 160, 100)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = nt3d.NewSoftwareRenderer().Render(f); err != nil {
			t.Fatal(err)
		}
	}
	pose, err := waterFloatPose(&nt3d.WaveSurface{}, 0)
	if err != nil || pose[14] != .065 || pose[0] != 1 || pose[5] != 1 || pose[10] != 1 {
		t.Fatal("flat-water pose", pose, err)
	}
}

func TestWaterTimeContinuity(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupWater(); err != nil {
		t.Fatal(err)
	}
	a, err := waterFloatPose(d.water.surface, 2*math.Pi-1e-5)
	if err != nil {
		t.Fatal(err)
	}
	b, err := waterFloatPose(d.water.surface, 2*math.Pi+1e-5)
	if err != nil {
		t.Fatal(err)
	}
	for i := range a {
		if math.Abs(float64(a[i]-b[i])) > .0001 {
			t.Fatal("pose jumps across legacy demo phase wrap")
		}
	}
}
