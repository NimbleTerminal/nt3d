// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image/color"
	"math/rand"
	"sort"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// splatBlock is n blended Gaussian splats scattered through a box.
func splatBlock(seed int64, n int, lo, hi nt3dmath.Vec3) Geometry {
	rng := rand.New(rand.NewSource(seed))
	g := Geometry{PointStyle: PointStyle{Shape: GaussianPoint, Units: WorldUnits, Blend: AlphaBlend}}
	for range n {
		p := nt3dmath.Vec3{X: lo.X + rng.Float32()*(hi.X-lo.X), Y: lo.Y + rng.Float32()*(hi.Y-lo.Y), Z: lo.Z + rng.Float32()*(hi.Z-lo.Z)}
		g.Points = append(g.Points, Point{Position: p, Radius: 1, Color: color.RGBA{200, 100, 50, 128},
			GaussianAxes: [3]nt3dmath.Vec3{{X: .1}, {Y: .1}, {Z: .1}}})
	}
	g.Bounds = GeometryBounds(g)
	return g
}

// blendBackstep draws the frame's blended points in key order and returns
// the farthest any point is drawn behind one already drawn: how many units
// of depth the blending order is wrong by at worst, and on average over the
// points that are out of order at all.
func blendBackstep(t *testing.T, f Frame) (worst, mean float32, wrong int) {
	t.Helper()
	drawn := make([]bool, len(f.Draws))
	for i := range drawn {
		drawn[i] = true
	}
	plan, err := f.planBlend(drawn)
	check(t, err)
	type keyed struct {
		key uint32
		z   float32
	}
	var points []keyed
	for slot, gi := range plan.slots {
		m := f.Matrix.Mul(f.Draws[gi].world())
		for _, p := range f.Draws[gi].Geometry.Points {
			_, _, z, w := m.Transform(p.Position)
			if w <= 0 || z < 0 || z > w {
				continue
			}
			points = append(points, keyed{plan.key(slot, z), z})
		}
	}
	sort.SliceStable(points, func(i, j int) bool { return points[i].key < points[j].key })
	nearest := float32(1e30)
	var sum float32
	for _, p := range points {
		// Drawn far to near: a point farther than the nearest so far is
		// blended over splats that should have covered it.
		if back := p.z - nearest; back > 0 {
			worst = max(worst, back)
			sum += back
			wrong++
		}
		nearest = min(nearest, p.z)
	}
	if wrong > 0 {
		mean = sum / float32(wrong)
	}
	return worst, mean, wrong
}

// Two splat tiles that overlap, as neighboring captures do, are blended slot
// by slot within each of the frame's 64 depth slices, so where they share a
// slice one tile's splats all go down before the other's. This measures how
// far out of depth order that leaves the picture, alone and with one blended
// draw far behind, which stretches the slices. It reports; the numbers are
// what a fix is judged against.
func TestBlendOrderAcrossOverlappingSplatTiles(t *testing.T) {
	build := func(distant bool) Frame {
		s := NewScene()
		// Two 20 m tiles sharing a 10 m overlap, 20 to 50 m from the eye.
		_, err := s.SetGeometry("west", splatBlock(1, 4000, nt3dmath.Vec3{X: -15, Y: 20, Z: 0}, nt3dmath.Vec3{X: 5, Y: 50, Z: 6}))
		check(t, err)
		_, err = s.SetGeometry("east", splatBlock(2, 4000, nt3dmath.Vec3{X: -5, Y: 20, Z: 0}, nt3dmath.Vec3{X: 15, Y: 50, Z: 6}))
		check(t, err)
		if distant {
			_, err = s.SetGeometry("horizon", splatBlock(3, 100, nt3dmath.Vec3{X: -50, Y: 10000, Z: 0}, nt3dmath.Vec3{X: 50, Y: 10100, Z: 50}))
			check(t, err)
		}
		c := DefaultCamera().LookAt(nt3dmath.Vec3{Y: 0, Z: 3}, nt3dmath.Vec3{Y: 35, Z: 3})
		c.Projection = Perspective
		f, err := s.StillFrame(c, 640, 480)
		check(t, err)
		return f
	}
	for _, c := range []struct {
		name    string
		distant bool
	}{{"two tiles", false}, {"two tiles and a draw 10 km behind", true}} {
		worst, mean, wrong := blendBackstep(t, build(c.distant))
		t.Logf("%s: %d of 8000 splats drawn behind a nearer one; worst by %.2f, mean %.2f depth units", c.name, wrong, worst, mean)
	}
}
