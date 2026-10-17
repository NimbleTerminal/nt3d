// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"testing"

	"nimbleterminal.dev/nt3d"
)

// pointCount is how many points a scene frame holds, and the frame.
func pointCount(t *testing.T, s *nt3d.Scene) (int, nt3d.Frame) {
	t.Helper()
	f, err := s.Frame(nt3d.DefaultCamera(), 64, 48)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, g := range f.Geometry {
		n += len(g.Points)
	}
	return n, f
}

// Every blending, shape, and count makes as many valid points as asked,
// at any time, and they render.
func TestParticleModes(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupParticles(); err != nil {
		t.Fatal(err)
	}
	for b := range particleBlends {
		for _, hard := range []bool{false, true} {
			for c, want := range particleCounts {
				ps := d.particles
				ps.blend, ps.hard, ps.count = b, hard, c
				for _, at := range []float64{0, 2.5, 100} {
					ps.t = at
					if err := d.moveParticles(); err != nil {
						t.Fatalf("%s, hard %v, %d at %.1f: %v", particleBlends[b].name, hard, want, at, err)
					}
				}
				n, f := pointCount(t, d.scene)
				if n != want {
					t.Fatalf("%s: %d points, want %d", particleBlends[b].name, n, want)
				}
				if _, err := nt3d.NewSoftwareRenderer().Render(f); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

// The particles move with time, and the fire rises from the floor.
func TestParticlesMove(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupParticles(); err != nil {
		t.Fatal(err)
	}
	a, b := fireParticle(3, 0), fireParticle(3, .2)
	if a.Position == b.Position {
		t.Fatal("a fire particle stood still")
	}
	for i := range 200 {
		if p := fireParticle(i, 1.3); p.Position.Z < 0 || p.Radius <= 0 {
			t.Fatalf("fire particle %d: %+v", i, p)
		}
	}
}
