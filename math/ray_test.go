// Copyright (c) 2026 Neomantra Corp.

package nt3dmath

import (
	"math"
	"testing"
)

var inf = float32(math.Inf(1))

func box(lo, hi Vec3) AABB {
	var b AABB
	b.Include(lo)
	b.Include(hi)
	return b
}

// A probe is a ray prepared for many box tests. Enter says where along the
// ray it enters a box, counted from near, and whether it does before far.
func TestRayProbeEnter(t *testing.T) {
	unit := box(Vec3{-1, -1, -1}, Vec3{1, 1, 1})
	for _, c := range []struct {
		name      string
		ray       Ray
		near, far float32
		want      float32
		hit       bool
	}{
		{"head on", Ray{Vec3{X: -5}, Vec3{X: 1}}, 0, inf, 4, true},
		{"pointing away", Ray{Vec3{X: -5}, Vec3{X: -1}}, 0, inf, 0, false},
		{"origin inside enters at near", Ray{Vec3{}, Vec3{X: 1}}, 0, inf, 0, true},
		{"parallel to a face, inside its slab", Ray{Vec3{X: -5, Y: .5}, Vec3{X: 1}}, 0, inf, 4, true},
		{"parallel to a face, outside its slab", Ray{Vec3{X: -5, Y: 2}, Vec3{X: 1}}, 0, inf, 0, false},
		{"grazing a corner", Ray{Vec3{X: -3, Y: -3}, Vec3{X: 1, Y: 1}}, 0, inf, 2, true},
		{"diagonal miss", Ray{Vec3{X: -5, Y: 3}, Vec3{X: 1, Y: 1}}, 0, inf, 0, false},
		{"beyond far", Ray{Vec3{X: -5}, Vec3{X: 1}}, 0, 3, 0, false},
		{"near past the entry", Ray{Vec3{X: -5}, Vec3{X: 1}}, 5, inf, 5, true},
		{"near past the box", Ray{Vec3{X: -5}, Vec3{X: 1}}, 7, inf, 0, false},
		{"direction need not be unit", Ray{Vec3{X: -5}, Vec3{X: 2}}, 0, inf, 2, true},
	} {
		got, hit := c.ray.Probe().Enter(unit.Min, unit.Max, c.near, c.far)
		if hit != c.hit || hit && got != c.want {
			t.Errorf("%s: enters at %v, hit %v; want %v, %v", c.name, got, hit, c.want, c.hit)
		}
	}
}

// Hits is the whole-ray question, for a box that may be invalid.
func TestAABBHits(t *testing.T) {
	unit := box(Vec3{-1, -1, -1}, Vec3{1, 1, 1})
	if !unit.Hits(Ray{Vec3{Z: 9}, Vec3{Z: -1}}) {
		t.Error("a ray down the Z axis misses the unit box")
	}
	if unit.Hits(Ray{Vec3{Z: 9}, Vec3{Z: 1}}) {
		t.Error("a ray pointing away from the unit box hits it")
	}
	if (AABB{}).Hits(Ray{Vec3{Z: 9}, Vec3{Z: -1}}) {
		t.Error("an empty box is hit")
	}
}

// Outside is the culling test: a box wholly beyond one plane of the view
// volume a matrix maps to clip space. Under the identity that volume is x and
// y within ±1 and z within 0..1.
func TestMat4Outside(t *testing.T) {
	m := Identity()
	for _, c := range []struct {
		name       string
		lo, hi     Vec3
		padX, padY float32
		want       bool
	}{
		{"inside", Vec3{-.5, -.5, .2}, Vec3{.5, .5, .8}, 0, 0, false},
		{"straddling the right plane", Vec3{.5, 0, .5}, Vec3{3, .1, .6}, 0, 0, false},
		{"beyond the right plane", Vec3{2, 0, .5}, Vec3{3, .1, .6}, 0, 0, true},
		{"beyond the right plane, within the pad", Vec3{2, 0, .5}, Vec3{3, .1, .6}, 1.5, 0, false},
		{"the pad is per axis", Vec3{2, 0, .5}, Vec3{3, .1, .6}, 0, 1.5, true},
		{"beyond the top plane", Vec3{0, 4, .5}, Vec3{.1, 5, .6}, 0, 0, true},
		{"nearer than the near plane", Vec3{0, 0, -2}, Vec3{.1, .1, -1}, 0, 0, true},
		{"farther than the far plane", Vec3{0, 0, 2}, Vec3{.1, .1, 3}, 0, 0, true},
		{"past a corner but beyond no one plane", Vec3{-3, -3, .5}, Vec3{3, 3, .6}, 0, 0, false},
	} {
		if got := m.Outside(c.lo, c.hi, c.padX, c.padY); got != c.want {
			t.Errorf("%s: outside %v, want %v", c.name, got, c.want)
		}
	}
}
