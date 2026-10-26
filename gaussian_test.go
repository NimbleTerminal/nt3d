package nt3d

import (
	"image/color"
	"math"
	"nimbleterminal.dev/nt3d/clod"
	"nimbleterminal.dev/nt3d/math"
	"slices"
	"testing"
)

func gaussianGeometry() Geometry {
	return styled(PointStyle{Shape: GaussianPoint, Units: WorldUnits, Blend: AlphaBlend}, Point{Position: nt3dmath.Vec3{Z: .5}, Radius: 1, Color: color.RGBA{240, 90, 20, 220}, GaussianAxes: [3]nt3dmath.Vec3{{X: .18}, {Y: .05}, {Z: .03}}})
}
func TestGaussianProjection(t *testing.T) {
	p := gaussianGeometry().Points[0]
	m := nt3dmath.Identity()
	e := projectGaussian(p, &m, 100, 100, 1)
	if math.Abs(float64(e.a*e.a-81.3)) > 1e-4 || math.Abs(float64(e.c*e.c-6.55)) > 1e-4 || e.b != 0 {
		t.Fatalf("covariance: %+v", e)
	}
	rotated := nt3dmath.RotationZ(math.Pi / 4)
	e = projectGaussian(p, &rotated, 100, 100, 1)
	if e.b >= 0 {
		t.Fatal("rotation must tilt down-screen covariance negative")
	}
	transformed, err := TransformGeometry(gaussianGeometry(), rotated)
	check(t, err)
	want := projectGaussian(transformed.Points[0], &m, 100, 100, 1)
	if e != want {
		t.Fatalf("baked and node transforms differ: %+v %+v", e, want)
	}
	e2 := projectGaussian(p, &m, 200, 200, 2)
	e1 := projectGaussian(p, &m, 100, 100, 1)
	if e2.a != 2*e1.a || e2.c != 2*e1.c {
		t.Fatal("supersampling changes covariance")
	}
	// A perspective Jacobian includes the depth-axis contribution off-axis.
	m[11] = 1
	m[15] = 1
	p.Position.X = .4
	e = projectGaussian(p, &m, 100, 100, 1)
	if e.a >= e1.a || e.a <= .18*50/1.5 {
		t.Fatalf("perspective covariance: %+v", e)
	}
}
func TestGaussianSoftwareAndSort(t *testing.T) {
	g := gaussianGeometry()
	p := g.Points[0]
	p.Position.Z = .7
	p.Color = color.RGBA{0, 0, 255, 180}
	g.Points = append(g.Points, p)
	f := flatFrame(80, 80, g)
	a := softwareRender(f)
	g.Points[0], g.Points[1] = g.Points[1], g.Points[0]
	f.Geometry[0] = g
	b := softwareRender(f)
	for y := range 80 {
		for x := range 80 {
			if at(a, x, y) != at(b, x, y) {
				t.Fatal("input order affected sorted blending")
			}
		}
	}
	if at(a, 46, 40) == at(a, 40, 46) {
		t.Fatal("anisotropy missing")
	}
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	g.Points[0].GaussianAxes[0].X = float32(math.NaN())
	if g.Validate() == nil {
		t.Fatal("accepted nonfinite axis")
	}
}

func TestGaussianWireframeCenters(t *testing.T) {
	g := gaussianGeometry()
	f := flatFrame(80, 80, g)
	gaussian, err := RenderWireframe(f, 40, 20)
	check(t, err)
	g.PointStyle = PointStyle{}
	g.Points[0].Radius = 1
	f.Geometry[0] = g
	points, err := RenderWireframe(f, 40, 20)
	check(t, err)
	if gaussian != points {
		t.Fatal("Gaussian wireframe differs from point centers")
	}
}

func clusteredGaussians(t *testing.T, n int) (Geometry, []nt3dmath.Vec3) {
	t.Helper()
	g := gaussianGeometry()
	g.Points = nil
	positions := make([]nt3dmath.Vec3, n)
	for i := range positions {
		positions[i] = nt3dmath.Vec3{X: float32(i%10) * .1, Y: float32(i/10) * .1, Z: .5}
		p := gaussianGeometry().Points[0]
		p.Position = positions[i]
		g.Points = append(g.Points, p)
	}
	g.Bounds = GeometryBounds(g)
	return g, positions
}

func TestGeometryClustersValidate(t *testing.T) {
	g, positions := clusteredGaussians(t, 100)
	set, err := nt3dclod.Build(positions, nt3dclod.Options{ClusterSize: 16})
	check(t, err)
	g.Clusters = set
	check(t, g.Validate())

	short, err := nt3dclod.Build(positions[:99], nt3dclod.Options{})
	check(t, err)
	g.Clusters = short
	if g.Validate() == nil {
		t.Fatal("accepted a set over 99 of 100 points")
	}

	dup := *set
	dup.Order = slices.Clone(set.Order)
	dup.Order[1] = dup.Order[0]
	g.Clusters = &dup
	if g.Validate() == nil {
		t.Fatal("accepted an Order with a duplicate")
	}

	out := *set
	out.Order = slices.Clone(set.Order)
	out.Order[0] = 100
	g.Clusters = &out
	if g.Validate() == nil {
		t.Fatal("accepted an Order index out of range")
	}

	gap := *set
	gap.Clusters = slices.Clone(set.Clusters)
	gap.Clusters[1].First++
	g.Clusters = &gap
	if g.Validate() == nil {
		t.Fatal("accepted clusters that do not tile Order")
	}

	short2 := *set
	short2.Clusters = set.Clusters[:len(set.Clusters)-1]
	g.Clusters = &short2
	if g.Validate() == nil {
		t.Fatal("accepted clusters that do not cover Order")
	}

	empty := *set
	empty.Clusters = slices.Clone(set.Clusters)
	empty.Clusters[0].Count = 0
	g.Clusters = &empty
	if g.Validate() == nil {
		t.Fatal("accepted an empty cluster")
	}
}

func TestPointUpdateDropsClusters(t *testing.T) {
	g, positions := clusteredGaussians(t, 100)
	set, err := nt3dclod.Build(positions, nt3dclod.Options{ClusterSize: 16})
	check(t, err)
	g.Clusters = set
	s := NewScene()
	id, err := s.SetGeometry("splats", g)
	check(t, err)
	if frame(t, s).Geometry[0].Clusters == nil {
		t.Fatal("SetGeometry dropped the clusters")
	}
	update := []Point{g.Points[3]}
	check(t, s.UpdateNodePoints(id, 3, update, g.Bounds))
	if frame(t, s).Geometry[0].Clusters != nil {
		t.Fatal("UpdateNodePoints kept the clusters")
	}

	id, err = s.SetGeometry("again", g)
	check(t, err)
	check(t, s.SetNodePoints(id, g.Points, g.Bounds))
	f := frame(t, s)
	for _, fg := range f.Geometry {
		if fg.Clusters != nil {
			t.Fatal("SetNodePoints kept the clusters")
		}
	}
}

func TestMergeAndTransformDropClusters(t *testing.T) {
	g, positions := clusteredGaussians(t, 100)
	set, err := nt3dclod.Build(positions, nt3dclod.Options{ClusterSize: 16})
	check(t, err)
	g.Clusters = set
	moved, err := TransformGeometry(g, nt3dmath.Translation(nt3dmath.Vec3{X: 1}))
	check(t, err)
	if moved.Clusters != nil {
		t.Fatal("TransformGeometry kept the clusters")
	}
	if g.Clusters != set {
		t.Fatal("TransformGeometry changed its input")
	}
	one, err := MergeGeometry(g)
	check(t, err)
	if one.Clusters != nil {
		t.Fatal("MergeGeometry of one kept the clusters")
	}
	two, err := MergeGeometry(g, g)
	check(t, err)
	if two.Clusters != nil || len(two.Points) != 200 {
		t.Fatal("MergeGeometry kept the clusters")
	}
}
