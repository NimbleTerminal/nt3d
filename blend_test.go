// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image/color"
	"math/rand/v2"
	"slices"
	"sort"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// depthFrame is flatFrame seen orthographically down -Z: world z of -1 to -4
// lands at clip depth .25 to 1, which grows away from the camera.
func depthFrame(w, h int, geometry ...Geometry) Frame {
	f := flatFrame(w, h, geometry...)
	f.Matrix[10] = -.25
	return f
}

// A frame's blended work: alpha points as slots, transparent meshes and
// additive points by the slice of their center's depth, keys that order by
// slice, then slot, then depth, far first.
func TestPlanBlend(t *testing.T) {
	cloud := styled(PointStyle{Shape: GaussianPoint, Units: WorldUnits, Blend: AlphaBlend}, Point{Position: nt3dmath.Vec3{Z: -3}, Radius: 1, Color: white})
	cube := quad(.5, -2, color.RGBA{255, 255, 255, 128}, glass, false)
	emitter := styled(PointStyle{Units: WorldUnits, Blend: AdditiveBlend}, Point{Position: nt3dmath.Vec3{Z: -1}, Radius: .1, Color: white})
	opaque := quad(.5, -2, white, nil, false)
	f := depthFrame(32, 32, emitter, opaque, cube, cloud)
	plan, err := f.planBlend(f.drawn(1))
	check(t, err)
	if !slices.Equal(plan.slots, []int{3}) || plan.points != 1 {
		t.Fatalf("slots %v, points %d, want [3], 1", plan.slots, plan.points)
	}
	if plan.near > .25 || plan.far < .75 || plan.near >= plan.far {
		t.Fatalf("depth range %v to %v does not span .25 to .75", plan.near, plan.far)
	}
	if len(plan.bySlice) != blendSlices {
		t.Fatalf("%d slices, want %d", len(plan.bySlice), blendSlices)
	}
	where := func(gi int) int {
		for s, list := range plan.bySlice {
			if slices.Contains(list, gi) {
				return s
			}
		}
		return -1
	}
	cubeSlice, emitterSlice := where(2), where(0)
	if cubeSlice != plan.sliceOf(.5) || emitterSlice != plan.sliceOf(.25) {
		t.Fatalf("cube in slice %d, emitter in %d, want %d, %d", cubeSlice, emitterSlice, plan.sliceOf(.5), plan.sliceOf(.25))
	}
	if cubeSlice >= emitterSlice {
		t.Fatalf("cube (farther) in slice %d, emitter in %d", cubeSlice, emitterSlice)
	}
	if where(1) != -1 || where(3) != -1 {
		t.Fatal("an opaque mesh or an alpha cloud is in a slice")
	}

	// Depth quantizes far first: the far plane is 0, the near plane 65535.
	p := blendPlan{near: .25, far: .75}
	if p.depth16(.75) != 0 || p.depth16(.25) != 65535 || p.depth16(2) != 0 || p.depth16(-1) != 65535 {
		t.Fatalf("depth16: far %d, near %d, beyond %d, before %d", p.depth16(.75), p.depth16(.25), p.depth16(2), p.depth16(-1))
	}
	last := uint32(0)
	for z := float32(.75); z >= .25; z -= .001 {
		if d := p.depth16(z); d < last {
			t.Fatalf("depth16 fell to %d from %d at %v", d, last, z)
		} else {
			last = d
		}
	}
	if p.sliceOf(.75) != 0 || p.sliceOf(.25) != blendSlices-1 {
		t.Fatalf("slices %d and %d", p.sliceOf(.75), p.sliceOf(.25))
	}
	if !(p.key(0, .75) < p.key(0, .25)) {
		t.Error("a far key does not sort before a near one")
	}
	if !(p.key(1, .5) > p.key(0, .5)) {
		t.Error("slot 1 does not sort after slot 0 in a slice")
	}
	if !(p.key(0, .25) > p.key(1, .75)) {
		t.Error("slice does not come before slot")
	}
	if k := p.key(maxBlendSlots-1, .25); k == 0xFFFFFFFF || k>>22 != blendSlices-1 || k>>10&4095 != 4094 {
		t.Errorf("key %#x for the last slot, slice, and depth", k)
	}
	// The last slot's nearest key stays clear of the sentinel's sorted bits.
	if k := p.key(4095, .25); k != p.key(4094, .25) || k == 0x0FFFFFFF {
		t.Errorf("slot 4095 keyed %#x, want it clamped to 4094", k)
	}
	// One depth, near == far: everything is at the far end of 16 bits.
	same := blendPlan{near: .5, far: .5}
	if same.depth16(.1) != 65535 || same.depth16(.9) != 65535 {
		t.Errorf("flat range: %d, %d, want 65535", same.depth16(.1), same.depth16(.9))
	}

	// More alpha geometries than slots fail as the GPU's frame limit does.
	var many []Geometry
	for range maxBlendSlots + 1 {
		many = append(many, styled(PointStyle{Blend: AlphaBlend}, Point{Position: nt3dmath.Vec3{Z: -2}, Radius: 4, Color: white}))
	}
	big := depthFrame(8, 8, many...)
	if _, err := big.planBlend(big.drawn(1)); err == nil {
		t.Error("planned more alpha geometries than slots")
	}
	big.Geometry = big.Geometry[:maxBlendSlots]
	if plan, err := big.planBlend(big.drawn(1)); err != nil || len(plan.slots) != maxBlendSlots {
		t.Errorf("%d slots, error %v", len(plan.slots), err)
	}

	if p.key(maxBlendSlots+5, .5) != p.key(maxBlendSlots-1, .5) {
		t.Error("a slot past the limit spills into the slice bits")
	}

	// With the camera inside the bounds, the range starts at the near plane:
	// points from there to the far end of the bounds spread over the slices.
	rng := rand.New(rand.NewPCG(1, 2))
	slab := make([]Point, 2000)
	for i := range slab {
		slab[i] = Point{Position: nt3dmath.Vec3{X: rng.Float32()*10 - 5, Y: rng.Float32()*10 - 5, Z: rng.Float32()*2 - 1}, Radius: 1, Color: white}
	}
	inside := flatFrame(64, 36, styled(PointStyle{Shape: GaussianPoint, Units: WorldUnits, Blend: AlphaBlend}, slab...))
	inside.Matrix = Camera{Projection: Perspective, Distance: 3, Alpha: 10, Beta: 30}.Matrix(16. / 9)
	plan, err = inside.planBlend(inside.drawn(1))
	check(t, err)
	if plan.near < 0 || plan.far <= plan.near {
		t.Fatalf("inside the bounds: depth range %v to %v", plan.near, plan.far)
	}
	hit := map[int]bool{}
	for i := range 10_000 {
		hit[plan.sliceOf(plan.far*float32(i)/9999)] = true
	}
	if len(hit) < 50 {
		t.Errorf("inside the bounds: depths from the near plane to the far end use %d of %d slices", len(hit), blendSlices)
	}
	// Everything in front of the near plane: one depth, no NaN.
	front := depthFrame(8, 8, cloud)
	front.Matrix[14] = -2
	plan, err = front.planBlend(front.drawn(1))
	check(t, err)
	if plan.near != 0 || plan.far != 0 || plan.key(0, -1) != plan.key(0, -.5) {
		t.Errorf("in front of the near plane: range %v to %v", plan.near, plan.far)
	}

	// A slot draws in the slices its bounds span, when the frame trusts its
	// bounds: it culls by them, or computes them.
	nearCloud := styled(PointStyle{Blend: AlphaBlend, Units: WorldUnits}, Point{Position: nt3dmath.Vec3{X: -.2, Z: -1.2}, Radius: .1, Color: white}, Point{Position: nt3dmath.Vec3{X: .2, Z: -1.4}, Radius: .1, Color: white})
	farCloud := styled(PointStyle{Blend: AlphaBlend, Units: WorldUnits}, Point{Position: nt3dmath.Vec3{Z: -3.5}, Radius: .1, Color: white}, Point{Position: nt3dmath.Vec3{Z: -3.9}, Radius: .1, Color: white})
	spanned := depthFrame(8, 8, nearCloud, farCloud)
	for _, c := range []struct {
		name  string
		cull  bool
		clear bool
	}{{"culled", true, false}, {"computed bounds", false, true}, {"untrusted bounds", false, false}} {
		f := spanned
		f.Cull = c.cull
		f.Geometry = slices.Clone(spanned.Geometry)
		if c.clear {
			for i := range f.Geometry {
				f.Geometry[i].Bounds = nt3dmath.AABB{}
			}
		}
		plan, err := f.planBlend(f.drawn(1))
		check(t, err)
		if len(plan.slotSlices) != len(plan.slots) {
			t.Fatalf("%s: %d slot ranges for %d slots", c.name, len(plan.slotSlices), len(plan.slots))
		}
		for slot, gi := range plan.slots {
			span := plan.slotSlices[slot]
			if c.name == "untrusted bounds" {
				if span != [2]int{0, blendSlices - 1} {
					t.Errorf("%s: slot %d spans %v, want every slice", c.name, slot, span)
				}
				continue
			}
			b := blendBounds(&f.Geometry[gi])
			_, _, zNear, _ := f.Matrix.Transform(b.Max)
			_, _, zFar, _ := f.Matrix.Transform(b.Min)
			if want := [2]int{max(0, plan.sliceOf(zFar)-1), min(blendSlices-1, plan.sliceOf(zNear)+1)}; span != want || span[1]-span[0] > blendSlices/2 {
				t.Errorf("%s: slot %d spans %v, want %v", c.name, slot, span, want)
			}
			for _, p := range f.Geometry[gi].Points {
				_, _, z, _ := f.Matrix.Transform(p.Position)
				if s := plan.sliceOf(z); s < span[0] || s > span[1] {
					t.Errorf("%s: slot %d point in slice %d outside %v", c.name, slot, s, span)
				}
			}
		}
	}

	// Nothing blended, nothing planned.
	none := depthFrame(8, 8, opaque)
	plan, err = none.planBlend(none.drawn(1))
	check(t, err)
	if len(plan.slots) != 0 || plan.points != 0 {
		t.Errorf("slots %v with nothing blended", plan.slots)
	}
	for s, list := range plan.bySlice {
		if len(list) != 0 {
			t.Errorf("slice %d holds %v with nothing blended", s, list)
		}
	}
}

// Sorting keys ascending is stable, treats all 32 bits as the key, and
// returns the permutation.
func TestSortKeysStable(t *testing.T) {
	var s pointSorter
	if got, want := s.sortKeys([]uint32{5, 1, 5, 0, 1}), []uint32{3, 1, 4, 0, 2}; !slices.Equal(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
	if got, want := s.sortKeys([]uint32{1 << 31, 3, 1<<31 | 1, 2}), []uint32{3, 1, 0, 2}; !slices.Equal(got, want) {
		t.Fatalf("with top bits %v, want %v", got, want)
	}
	if got := s.sortKeys(nil); len(got) != 0 {
		t.Fatalf("no keys sorted to %v", got)
	}
	r := rand.New(rand.NewPCG(3, 4))
	keys := make([]uint32, 10_000)
	for i := range keys {
		keys[i] = r.Uint32() >> uint(r.IntN(32)) // Plenty of ties and small keys.
	}
	original := slices.Clone(keys)
	want := make([]uint32, len(keys))
	for i := range want {
		want[i] = uint32(i)
	}
	sort.SliceStable(want, func(a, b int) bool { return keys[want[a]] < keys[want[b]] })
	if got := s.sortKeys(keys); !slices.Equal(got, want) {
		t.Fatal("random keys differ from a stable sort")
	}
	if !slices.Equal(keys, original) {
		t.Fatal("sorting changed the caller's keys")
	}
}

// A frame over the slot limit fails in every renderer that validates; the
// software draw, reached without validation, still draws all of it: the
// first slots sorted, the rest unsorted.
func TestBlendSlotOverflow(t *testing.T) {
	dot := func(x float32, c color.RGBA) Geometry {
		return styled(PointStyle{Blend: AlphaBlend}, Point{Position: nt3dmath.Vec3{X: x, Z: -2}, Radius: 4, Color: c})
	}
	red := color.RGBA{255, 0, 0, 255}
	var many []Geometry
	for range maxBlendSlots {
		many = append(many, dot(-.5, white))
	}
	many = append(many, dot(.5, red))
	f := depthFrame(32, 32, many...)
	if _, err := RenderSoftware(f); err == nil {
		t.Error("RenderSoftware accepted a frame over the slot limit")
	}
	r := NewSoftwareRenderer()
	defer r.Close()
	if _, err := r.Render(f); err == nil {
		t.Error("Render accepted a frame over the slot limit")
	}
	img := softwareRender(f)
	if got := at(img, 8, 16); got != white {
		t.Errorf("sorted slots: %v at the left dot, want %v", got, white)
	}
	if got := at(img, 24, 16); got != red {
		t.Errorf("overflow slot: %v at the right dot, want %v", got, red)
	}
}
