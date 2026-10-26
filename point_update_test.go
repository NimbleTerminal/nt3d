package nt3d

import (
	"math"
	"nimbleterminal.dev/nt3d/math"
	"slices"
	"testing"
)

func pointBounds() (b nt3dmath.AABB) {
	b.Include(nt3dmath.Vec3{X: -2, Y: -2, Z: -2})
	b.Include(nt3dmath.Vec3{X: 2, Y: 2, Z: 2})
	return
}

func TestPointUpdatesPreserveSnapshots(t *testing.T) {
	s := NewScene()
	g := quad(.5, .5, white, nil, false)
	g.Points = []Point{{Radius: 2, Color: white}, {Radius: 3, Color: blue}}
	id, err := s.SetGeometry("points", g)
	check(t, err)
	before := frame(t, s)
	update := []Point{{Position: nt3dmath.Vec3{X: 1}, Radius: 4, Color: white, Datum: 42}}
	check(t, s.UpdateNodePoints(id, 1, update, pointBounds()))
	after := frame(t, s)
	update[0].Radius = 20
	if before.Geometry[0].Points[1].Radius != 3 || after.Geometry[0].Points[1].Radius != 4 {
		t.Fatal("snapshot or input ownership")
	}
	if &before.Geometry[0].Vertices[0] != &after.Geometry[0].Vertices[0] || before.GeometryRevisions[0] != after.GeometryRevisions[0] {
		t.Fatal("unrelated mesh rebuilt")
	}
	if after.Geometry[0].Bounds != pointBounds() {
		t.Fatal("bounds recomputed")
	}
	if after.Geometry[0].pointUpdate.first != 1 || after.Geometry[0].pointUpdate.end != 2 {
		t.Fatal("changed range")
	}
	check(t, validateFrame(after))
	invalid := []Point{{Radius: float32(math.NaN())}}
	if s.UpdateNodePoints(id, 0, invalid, pointBounds()) == nil {
		t.Fatal("accepted invalid point")
	}
	if s.UpdateNodePoints(id, 2, update, pointBounds()) == nil {
		t.Fatal("accepted invalid range")
	}
	if s.SetNodePoints(id, update, pointBounds()) == nil {
		t.Fatal("accepted changed count")
	}
	if s.UpdateNodePoints(id, 0, update, nt3dmath.AABB{}) == nil {
		t.Fatal("accepted unknown bounds")
	}
	if frame(t, s).Geometry[0].Points[1].Radius != 4 {
		t.Fatal("failed update mutated geometry")
	}
}

func BenchmarkPointUpdate100K(b *testing.B) {
	points := make([]Point, 100000)
	for i := range points {
		points[i] = Point{Position: nt3dmath.Vec3{X: float32(i%100) / 100}, Radius: 2, Color: white}
	}
	for _, fast := range []bool{false, true} {
		name := "SetNodeGeometry"
		if fast {
			name = "SetNodePoints"
		}
		b.Run(name, func(b *testing.B) {
			s := NewScene()
			id, err := s.SetGeometry("points", Geometry{Points: points})
			if err != nil {
				b.Fatal(err)
			}
			bounds := pointBounds()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if fast {
					err = s.SetNodePoints(id, points, bounds)
				} else {
					err = s.SetNodeGeometry(id, Geometry{Points: points})
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// A scene's stored geometry is trusted per frame by pointer identity:
// replacing its points or clusters in a frame copy is checked again.
func TestStoredGeometryCertificate(t *testing.T) {
	s := NewScene()
	_, err := s.SetGeometry("cloud", lodCloud(t, 256, 64))
	check(t, err)
	f, err := s.Frame(DefaultCamera(), 40, 40)
	check(t, err)
	gi := -1
	for i, g := range f.Geometry {
		if g.Clusters != nil {
			gi = i
		}
	}
	if gi < 0 || f.Geometry[gi].stored == nil {
		t.Fatal("the scene's frame carries no certified cloud")
	}
	if err := validateFrame(f); err != nil {
		t.Fatalf("scene frame: %v", err)
	}

	nan := float32(math.NaN())
	bad := lodCloud(t, 256, 64)
	bad.Points[3].Position.X = nan
	if validateFrame(lodOrtho(bad)) == nil {
		t.Fatal("a hand-built frame with a NaN point passed")
	}

	points := slices.Clone(f.Geometry[gi].Points)
	points[3].Position.X = nan
	swapped := f
	swapped.Geometry = slices.Clone(f.Geometry)
	swapped.Geometry[gi].Points = points
	if validateFrame(swapped) == nil {
		t.Fatal("a frame copy with other points holding a NaN passed")
	}

	set := *f.Geometry[gi].Clusters
	set.Clusters = slices.Clone(set.Clusters)
	set.Clusters[0].First++
	swapped.Geometry = slices.Clone(f.Geometry)
	swapped.Geometry[gi].Clusters = &set
	if validateFrame(swapped) == nil {
		t.Fatal("a frame copy with a non-tiling cluster set passed")
	}
}
