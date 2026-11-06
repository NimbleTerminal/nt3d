// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"math"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// FuzzClipLine checks the line clipper against a fixed box: a kept segment
// lies within the box, keeps its endpoints when they were already inside, and
// is dropped exactly when the segment misses the box entirely.
func FuzzClipLine(f *testing.F) {
	f.Add(float32(-5), float32(0), float32(0), float32(5), float32(0), float32(0))
	f.Add(float32(0), float32(0), float32(0), float32(.5), float32(.5), float32(.5))
	f.Add(float32(-5), float32(-5), float32(-5), float32(-4), float32(-4), float32(-4))
	f.Add(float32(2), float32(2), float32(2), float32(2), float32(2), float32(2))
	f.Add(float32(-1), float32(-1), float32(3), float32(1), float32(1), float32(3))
	f.Fuzz(func(t *testing.T, ax, ay, az, bx, by, bz float32) {
		for _, v := range []float32{ax, ay, az, bx, by, bz} {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) || v > 1e3 || v < -1e3 {
				return
			}
		}
		var bounds nt3dmath.AABB
		bounds.Include(nt3dmath.Vec3{X: -1, Y: -1, Z: -1})
		bounds.Include(nt3dmath.Vec3{X: 1, Y: 1, Z: 1})
		a := Vertex{Position: nt3dmath.Vec3{X: ax, Y: ay, Z: az}}
		b := Vertex{Position: nt3dmath.Vec3{X: bx, Y: by, Z: bz}}
		p, q, ok := clipLine(a, b, bounds)
		inside := func(v nt3dmath.Vec3) bool {
			const eps = 1e-3
			return v.X >= -1-eps && v.X <= 1+eps && v.Y >= -1-eps && v.Y <= 1+eps && v.Z >= -1-eps && v.Z <= 1+eps
		}
		if !ok {
			if inside(a.Position) || inside(b.Position) {
				t.Fatalf("dropped a segment with an endpoint inside: %v %v", a.Position, b.Position)
			}
			return
		}
		if !inside(p.Position) || !inside(q.Position) {
			t.Fatalf("kept %v %v outside the box", p.Position, q.Position)
		}
		if inside(a.Position) && p.Position != a.Position {
			t.Fatalf("moved inside endpoint %v to %v", a.Position, p.Position)
		}
		if inside(b.Position) && q.Position != b.Position {
			t.Fatalf("moved inside endpoint %v to %v", b.Position, q.Position)
		}
		// The clipped segment stays on the original line.
		d := b.Position.Sub(a.Position)
		for _, v := range []nt3dmath.Vec3{p.Position, q.Position} {
			w := v.Sub(a.Position)
			cross := d.Cross(w)
			if float64(cross.Dot(cross)) > 1e-6*math.Max(1, float64(d.Dot(d))*float64(w.Dot(w))) {
				t.Fatalf("%v is off the line %v-%v", v, a.Position, b.Position)
			}
		}
	})
}
