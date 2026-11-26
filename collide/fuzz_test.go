// Copyright (c) 2026 Neomantra Corp.

package nt3dcollide

import (
	"math"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// fuzzTriangles decodes nine bytes a triangle into corners on a half-meter
// grid within about ±16 m, at most 48 triangles. Degenerate triangles come
// out of it often, which is the point.
func fuzzTriangles(data []byte) []Triangle {
	var tris []Triangle
	c := func(b byte) float64 { return float64(int8(b)) / 8 }
	for len(data) >= 9 && len(tris) < 48 {
		tris = append(tris, Triangle{
			A: nt3dmath.Vec3d{X: c(data[0]), Y: c(data[1]), Z: c(data[2])},
			B: nt3dmath.Vec3d{X: c(data[3]), Y: c(data[4]), Z: c(data[5])},
			C: nt3dmath.Vec3d{X: c(data[6]), Y: c(data[7]), Z: c(data[8])},
		})
		data = data[9:]
	}
	return tris
}

// FuzzRayCast casts a ray into a triangle soup and checks the answer against
// every triangle tested one by one: a hit lies within range on the ray, its
// normal is unit length and faces the ray's origin, and no triangle is nearer
// than the one the tree found.
func FuzzRayCast(f *testing.F) {
	f.Add([]byte{0, 0, 0, 80, 0, 0, 0, 80, 0}, 1.0, 1.0, 5.0, 0.0, 0.0, -1.0, 20.0)
	f.Add([]byte{0, 0, 0, 80, 0, 0, 0, 80, 0, 0, 0, 16, 80, 0, 16, 0, 80, 16}, 2.0, 2.0, -5.0, 0.1, 0.0, 1.0, 50.0)
	f.Add([]byte{1, 2, 3, 1, 2, 3, 1, 2, 3}, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 1.0)
	f.Fuzz(func(t *testing.T, soup []byte, ox, oy, oz, dx, dy, dz, maxDist float64) {
		m := NewMeshFromTriangles(fuzzTriangles(soup))
		origin := nt3dmath.Vec3d{X: ox, Y: oy, Z: oz}
		dir := nt3dmath.Vec3d{X: dx, Y: dy, Z: dz}
		hit, ok := m.RayCast(origin, dir, maxDist)

		valid := origin.IsFinite() && dir.IsFinite() && maxDist > 0 && vecLen(dir) > 1e-12 && !math.IsInf(vecLen(dir), 0)
		if !valid {
			if ok && (!hit.Position.IsFinite() || math.IsNaN(hit.Distance)) {
				t.Fatalf("a ray that is not valid returned a hit that is not finite: %+v", hit)
			}
			return
		}
		unit := vecScale(dir, 1/vecLen(dir))
		nearest, any := math.Inf(1), false
		for _, tri := range m.tris {
			if d, hitTri := rayTriangleTwoSided(origin, unit, tri, maxDist); hitTri && d < nearest {
				nearest, any = d, true
			}
		}
		if ok != any {
			t.Fatalf("RayCast found a hit: %v; testing every triangle: %v", ok, any)
		}
		if !ok {
			return
		}
		if !(hit.Distance >= 0 && hit.Distance <= maxDist) || !hit.Position.IsFinite() {
			t.Fatalf("hit %+v is outside [0, %v] or not finite", hit, maxDist)
		}
		if math.Abs(hit.Distance-nearest) > 1e-9*math.Max(1, nearest) {
			t.Fatalf("RayCast hit at %v, but a triangle is hit at %v", hit.Distance, nearest)
		}
		if l := vecLen(hit.Normal); math.Abs(l-1) > 1e-6 {
			t.Fatalf("hit normal %v has length %v", hit.Normal, l)
		}
		if dot(hit.Normal, unit) > 1e-9 {
			t.Fatalf("hit normal %v faces away from the ray %v", hit.Normal, unit)
		}
	})
}

// FuzzMoveCapsule moves a capsule through a triangle soup. Input that is
// wrong is refused with the pose unchanged; otherwise the result is finite,
// the capsule gains no speed from a collision, and a ground normal is unit
// length and walkable.
func FuzzMoveCapsule(f *testing.F) {
	f.Add([]byte{176, 176, 0, 80, 176, 0, 80, 80, 0, 176, 176, 0, 80, 80, 0, 176, 80, 0}, 0.4, 1.8, 0.0, 0.0, 2.0, 1.0, 0.0, -5.0, 0.1, true)
	f.Add([]byte{16, 176, 0, 16, 80, 0, 16, 80, 40}, 0.3, 1.0, 0.0, 0.0, 0.5, 60.0, 5.0, 0.0, 0.25, false)
	f.Add([]byte{}, 0.5, 1.0, 1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 0.016, false)
	f.Fuzz(func(t *testing.T, soup []byte, radius, height, px, py, pz, vx, vy, vz, dt float64, grounded bool) {
		m := NewMeshFromTriangles(fuzzTriangles(soup))
		cap := Capsule{Radius: radius, Height: height}
		pos := nt3dmath.Vec3d{X: px, Y: py, Z: pz}
		vel := nt3dmath.Vec3d{X: vx, Y: vy, Z: vz}
		cfg := MoveConfig{StepHeight: 0.3, GroundSnap: 0.2}
		// Keep the work of one input small: the fuzzer is after wrong
		// answers, not long runs.
		if radius > 0 && vecLen(vel)*math.Abs(dt) > 400*radius {
			t.Skip()
		}
		res, err := MoveCapsule(m, cap, pos, vel, dt, grounded, cfg)
		if err != nil {
			same := func(a, b float64) bool { return a == b || (math.IsNaN(a) && math.IsNaN(b)) }
			if !same(res.Position.X, px) || !same(res.Position.Y, py) || !same(res.Position.Z, pz) {
				t.Fatalf("refused input (%v) moved the capsule from %v to %v", err, pos, res.Position)
			}
			return
		}
		if !res.Position.IsFinite() || !res.Velocity.IsFinite() || !res.GroundNormal.IsFinite() ||
			!res.WallNormal.IsFinite() || !nt3dmath.IsFinite(res.ImpactSpeed) {
			t.Fatalf("result is not finite: %+v", res)
		}
		if got, was := vecLen(res.Velocity), vecLen(vel); got > was+1e-9*math.Max(1, was) {
			t.Fatalf("a collision sped the capsule up: %v m/s became %v", was, got)
		}
		if res.Grounded {
			if l := vecLen(res.GroundNormal); math.Abs(l-1) > 1e-6 {
				t.Fatalf("ground normal %v has length %v", res.GroundNormal, l)
			}
			if res.GroundNormal.Z < math.Cos(50*math.Pi/180)-1e-9 {
				t.Fatalf("grounded on a slope steeper than the limit: normal %v", res.GroundNormal)
			}
		}
	})
}
