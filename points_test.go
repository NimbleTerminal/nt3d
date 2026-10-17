// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image/color"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// A style is valid only with known values, and a radius only in its
// units' range: pixels 1 to 32, world units any finite positive size.
func TestPointStyleValidation(t *testing.T) {
	world := PointStyle{Units: WorldUnits}
	radius := func(r float32) []Point { return []Point{{Radius: r}} }
	for name, g := range map[string]Geometry{
		"unknown shape":           {PointStyle: PointStyle{Shape: 2}, Points: radius(4)},
		"unknown units":           {PointStyle: PointStyle{Units: 2}, Points: radius(4)},
		"unknown blend":           {PointStyle: PointStyle{Blend: 3}, Points: radius(4)},
		"unknown, without points": {PointStyle: PointStyle{Blend: 3}},
		"zero world radius":       {PointStyle: world, Points: radius(0)},
		"negative world radius":   {PointStyle: world, Points: radius(-1)},
		"NaN world radius":        {PointStyle: world, Points: radius(float32(math.NaN()))},
		"infinite world radius":   {PointStyle: world, Points: radius(float32(math.Inf(1)))},
		"pixel radius under 1":    {Points: radius(.5)},
		"pixel radius over 32":    {Points: radius(33)},
	} {
		if g.Validate() == nil {
			t.Errorf("%s: valid", name)
		}
	}
	for name, g := range map[string]Geometry{
		"small world radius":     {PointStyle: world, Points: radius(.001)},
		"large world radius":     {PointStyle: world, Points: radius(1000)},
		"every field set":        {PointStyle: PointStyle{SoftPoint, WorldUnits, AdditiveBlend}, Points: radius(.1)},
		"a style without points": {PointStyle: PointStyle{Blend: AlphaBlend}},
	} {
		if err := g.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Copies, clips, and transforms keep the style; merging keeps the merged
// points' style and refuses points of different styles.
func TestPointStyleCarried(t *testing.T) {
	soft := PointStyle{Shape: SoftPoint, Units: WorldUnits, Blend: AdditiveBlend}
	g := Geometry{PointStyle: soft, Points: []Point{{Radius: .1}, {Position: nt3dmath.Vec3{X: 2}, Radius: .1}}}
	var box nt3dmath.AABB
	box.Include(nt3dmath.Vec3{X: -1, Y: -1, Z: -1})
	box.Include(nt3dmath.Vec3{X: 1, Y: 1, Z: 1})
	clipped, err := Clip(g, box)
	check(t, err)
	moved, err := TransformGeometry(g, nt3dmath.Translation(nt3dmath.Vec3{Z: 1}))
	check(t, err)
	line := Geometry{PointStyle: PointStyle{Blend: AlphaBlend}, Lines: []Vertex{{}, {Position: nt3dmath.Vec3{X: 1}}}}
	merged, err := MergeGeometry(line, g, Geometry{PointStyle: soft, Points: []Point{{Radius: .2}}})
	check(t, err)
	for name, got := range map[string]Geometry{"clipped": clipped, "moved": moved, "merged": merged} {
		if got.PointStyle != soft {
			t.Errorf("%s: style %+v, want %+v", name, got.PointStyle, soft)
		}
	}
	if len(clipped.Points) != 1 || len(merged.Points) != 3 {
		t.Fatalf("clipped %d points, merged %d", len(clipped.Points), len(merged.Points))
	}
	if _, err := MergeGeometry(g, Geometry{Points: []Point{{Radius: 2}}}); err == nil {
		t.Fatal("merged points of different styles")
	}
}

// A pixel radius is the same at any depth, times the scale; a world radius
// shrinks with depth by the projection's vertical scale, clamped to half a
// pixel and MaxPointPixels, times the scale.
func TestPointRadius(t *testing.T) {
	f := Frame{Height: 100, Matrix: nt3dmath.Identity()} // Clip y is y: 50 pixels a unit.
	world := PointStyle{Units: WorldUnits}
	for _, c := range []struct {
		style                  PointStyle
		radius, w, scale, want float32
	}{
		{PointStyle{}, 4, 1, 1, 4}, {PointStyle{}, 4, 5, 1, 4}, {PointStyle{}, 4, 1, 2, 8},
		{world, .1, 1, 1, 5}, {world, .1, 2, 1, 2.5},
		{world, 1e-4, 1, 1, .5}, {world, 1e-4, 1, 2, 1},
		{world, 100, 1, 1, MaxPointPixels}, {world, 100, 1, 2, 2 * MaxPointPixels},
	} {
		if got := f.pointRadius(c.style, c.radius, c.w, c.scale); math.Abs(float64(got-c.want)) > 1e-4 {
			t.Errorf("%+v radius %v at w %v, scale %v: %v, want %v", c.style, c.radius, c.w, c.scale, got, c.want)
		}
	}
	// Under a perspective camera, a world radius is the height on screen of
	// a segment that long along the camera's up axis.
	camera := DefaultCamera()
	camera.Projection = Perspective
	f = Frame{Height: 300, Matrix: camera.Matrix(1.5)}
	_, _, up, _ := camera.basis()
	p := nt3dmath.Vec3{X: .3, Y: -.2, Z: .1}
	x0, y0, _, w0 := f.Matrix.Transform(p)
	x1, y1, _, w1 := f.Matrix.Transform(p.Add(up.Scale(.2)))
	dy := float64(y1/w1-y0/w0) * 150
	dx := float64(x1/w1-x0/w0) * 225
	if got, want := f.pointRadius(world, .2, w0, 1), math.Hypot(dx, dy); math.Abs(float64(got)-want) > .02*want {
		t.Fatalf("perspective world radius: %v pixels, want %v", got, want)
	}
}

// A hard point covers fully to its edge; a soft one falls as (1-r²)².
func TestSoftness(t *testing.T) {
	for _, c := range []struct {
		shape    PointShape
		r2, want float32
	}{{HardPoint, 0, 1}, {HardPoint, .99, 1}, {SoftPoint, 0, 1}, {SoftPoint, .25, .5625}, {SoftPoint, 1, 0}, {SoftPoint, 1.2, 0}} {
		if got := softness(c.shape, c.r2); math.Abs(float64(got-c.want)) > 1e-6 {
			t.Errorf("shape %d at r² %v: %v, want %v", c.shape, c.r2, got, c.want)
		}
	}
}

// Points sort farthest first by clip z, equal depths in their given order.
func TestPointSort(t *testing.T) {
	m := nt3dmath.Identity() // Clip z is z.
	var points []Point
	for _, z := range []float32{.2, .9, -.5, .9, .4} {
		points = append(points, Point{Position: nt3dmath.Vec3{Z: z}})
	}
	var s pointSorter
	want := []uint32{1, 3, 4, 0, 2}
	for range 2 { // A reused sorter sorts the same.
		if got := s.sort(points, &m); !slices.Equal(got, want) {
			t.Fatalf("order %v, want %v", got, want)
		}
	}
	// Many points, under a camera: depths never increase along the order,
	// and ties keep their indices increasing.
	camera := DefaultCamera()
	camera.Projection = Perspective
	m = camera.Matrix(1)
	r := rand.New(rand.NewPCG(1, 2))
	points = points[:0]
	for range 5000 {
		v := float32(r.IntN(50)) / 10 // Coarse, so depths tie.
		points = append(points, Point{Position: nt3dmath.Vec3{X: v - 2.5, Y: float32(r.IntN(3)), Z: 0}})
	}
	order := s.sort(points, &m)
	depth := func(i uint32) float32 { _, _, z, _ := m.Transform(points[i].Position); return z }
	for k := 1; k < len(order); k++ {
		a, b := depth(order[k-1]), depth(order[k])
		if b > a || b == a && order[k] < order[k-1] {
			t.Fatalf("at %d: point %d (z %v) after %d (z %v)", k, order[k], b, order[k-1], a)
		}
	}
}

// Sorting 100,000 points takes under a millisecond (the spec's budget).
func BenchmarkPointSort(b *testing.B) {
	camera := DefaultCamera()
	camera.Projection = Perspective
	m := camera.Matrix(1)
	r := rand.New(rand.NewPCG(1, 2))
	points := make([]Point, 100_000)
	for i := range points {
		points[i].Position = nt3dmath.Vec3{X: r.Float32()*2 - 1, Y: r.Float32()*2 - 1, Z: r.Float32()*2 - 1}
	}
	var s pointSorter
	b.ReportAllocs()
	for b.Loop() {
		s.sort(points, &m)
	}
}

// A world-unit point can hang past its bounds by up to MaxPointPixels on
// screen, so culling keeps one whose center is just out of view.
func TestCullKeepsWorldUnitPoint(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	g := Geometry{PointStyle: PointStyle{Units: WorldUnits}, Points: []Point{{Position: nt3dmath.Vec3{X: 1 + 6./64, Z: .5}, Radius: .2, Color: red}}}
	f := flatFrame(128, 128, g) // A radius of 12.8 pixels, its center 6 pixels past the right edge.
	f.Cull = true
	if !f.drawn(1)[0] {
		t.Fatal("culled a point that hangs into view")
	}
	if coloredPixels(softwareRender(f), red) == 0 {
		t.Fatal("the point does not show")
	}
}

// Picking a world-unit point hits within its radius on screen, not past it.
func TestPickWorldUnitPoint(t *testing.T) {
	style := PointStyle{Units: WorldUnits}
	s := NewScene()
	_, err := s.SetGeometry("p", Geometry{PointStyle: style, Points: []Point{{Radius: .2, Datum: 7}}})
	check(t, err)
	camera := DefaultCamera()
	camera.Projection = Perspective
	f, err := s.Frame(camera, 200, 200)
	check(t, err)
	_, _, _, w := f.Matrix.Transform(nt3dmath.Vec3{})
	r := f.pointRadius(style, .2, w, 1)
	if r < 3 {
		t.Fatalf("radius %v pixels: too small to test", r)
	}
	if hit := s.Pick(camera, f, 100+.8*r, 100, 0); hit == nil || hit.Datum != 7 {
		t.Fatalf("inside the radius: %+v", hit)
	}
	if hit := s.Pick(camera, f, 100+1.2*r, 100, 0); hit != nil {
		t.Fatalf("past the radius: %+v", hit)
	}
}
