package nt3d

import (
	"math"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"testing"
)

func nearAnimation(t *testing.T, got, want float32) {
	t.Helper()
	if math.Abs(float64(got-want)) > 1e-5 {
		t.Fatalf("got %g, want %g", got, want)
	}
}
func TestAnimationPlayback(t *testing.T) {
	s := NewScene()
	id, err := s.Add(Root, "animated")
	check(t, err)
	a, err := NewAnimation(s, AnimationClip{Tracks: []Track{{Node: id, Channel: Translation, Width: 3, Times: []float32{0, 2}, Values: []float32{0, 0, 0, 2, 4, 6}}}})
	check(t, err)
	check(t, a.Sample(1))
	n, _ := s.Node(id)
	nearAnimation(t, n.Local[12], 1)
	nearAnimation(t, n.Local[14], 3)
	rev := s.Revision()
	check(t, a.Sample(1))
	if s.Revision() != rev {
		t.Fatal("same sample dirtied scene")
	}
	a.Mode = Loop
	check(t, a.Sample(-.5))
	n, _ = s.Node(id)
	nearAnimation(t, n.Local[12], 1.5)
	a.Mode = PingPong
	check(t, a.Sample(2.5))
	n, _ = s.Node(id)
	nearAnimation(t, n.Local[12], 1.5)
	a.Paused = true
	check(t, a.Step(10))
	if a.Time != 2.5 {
		t.Fatal("paused player advanced")
	}
	check(t, a.Sample(.5))
	n, _ = s.Node(id)
	nearAnimation(t, n.Local[12], .5)
	check(t, a.SampleBlend(1.5, .5))
	n, _ = s.Node(id)
	nearAnimation(t, n.Local[12], 1)
}
func TestTrackInterpolation(t *testing.T) {
	tr := Track{Channel: Rotation, Width: 4, Times: []float32{0, 1}, Values: []float32{0, 0, 0, 2, 0, 0, -2, 0}}
	q := tr.sample(.5)
	nearAnimation(t, dot4(q, q), 1)
	nearAnimation(t, float32(math.Abs(float64(q[2]))), float32(math.Sqrt(.5)))
	tr = Track{Channel: Translation, Width: 3, Interpolation: Step, Times: []float32{0, 1}, Values: []float32{0, 0, 0, 1, 0, 0}}
	nearAnimation(t, tr.sample(.99)[0], 0)
	nearAnimation(t, tr.sample(1)[0], 1)
	tr = Track{Channel: UserChannel, Width: 1, Interpolation: CubicSpline, Times: []float32{0, 2}, Values: []float32{0, 0, 1, 1, 2, 0}}
	nearAnimation(t, tr.sample(1)[0], 1)
	for _, bad := range []Track{{Width: 0}, {Width: 1, Times: []float32{1, 1}, Values: []float32{0, 1}}, {Width: 1, Times: []float32{0}, Values: []float32{float32(math.NaN())}}} {
		if (AnimationClip{Tracks: []Track{bad}}).Validate() == nil {
			t.Fatal("accepted bad track")
		}
	}
}
func animationSkinScene(t *testing.T) (*Scene, NodeID, NodeID) {
	t.Helper()
	s := NewScene()
	joint, e := s.Add(Root, "joint")
	check(t, e)
	g := Geometry{Vertices: []Vertex{{Position: nt3dmath.Vec3{}, Normal: nt3dmath.Vec3{Z: 1}}, {Position: nt3dmath.Vec3{X: 1}, Normal: nt3dmath.Vec3{Z: 1}}, {Position: nt3dmath.Vec3{Y: 1}, Normal: nt3dmath.Vec3{Z: 1}}}, Indices: []uint32{0, 1, 2}}
	id, e := s.SetGeometry("mesh", g)
	check(t, e)
	def := Deformation{Morphs: []MorphTarget{{Positions: []nt3dmath.Vec3{{Z: 1}, {Z: 1}, {Z: 1}}}}, Skeleton: &Skeleton{Joints: []NodeID{joint}}, Influences: []Influence{{Weights: [4]float32{2}}, {Weights: [4]float32{1}}, {Weights: [4]float32{1}}}}
	check(t, s.SetDeformation(id, def))
	return s, id, joint
}
func TestMorphSkinSnapshotAndBounds(t *testing.T) {
	s, id, joint := animationSkinScene(t)
	old := frame(t, s)
	rev := s.Revision()
	check(t, s.UpdateDeformations())
	if rev != s.Revision() {
		t.Fatal("unchanged skin recomputed")
	}
	check(t, s.SetMorphWeights(id, []float32{.5}))
	check(t, s.SetTransform(joint, nt3dmath.Translation(nt3dmath.Vec3{X: 2})))
	b := s.Bounds()
	nearAnimation(t, b.Min.X, 2)
	nearAnimation(t, b.Min.Z, .5)
	f := frame(t, s)
	nearAnimation(t, f.Geometry[0].Vertices[0].Position.X, 2)
	nearAnimation(t, old.Geometry[0].Vertices[0].Position.X, 0)
	if f.GeometryRevisions[0] == old.GeometryRevisions[0] {
		t.Fatal("posed mesh revision unchanged")
	}
	if _, err := NewSoftwareRenderer().Render(f); err != nil {
		t.Fatal(err)
	}
	if _, err := RenderWireframe(f, 40, 20); err != nil {
		t.Fatal(err)
	}
	// The mesh local transform cancels out of its palette; a joint remains in world space.
	check(t, s.SetTransform(id, nt3dmath.Translation(nt3dmath.Vec3{X: 3})))
	check(t, s.UpdateDeformations())
	nearAnimation(t, s.nodes[id].geometry.Vertices[0].Position.X, -1)
	check(t, s.Remove(joint))
	if s.UpdateDeformations() == nil {
		t.Fatal("missing joint accepted")
	}
}
func TestAnimatedAssetInstances(t *testing.T) {
	asset := SceneAsset{Nodes: []AssetNode{{Parent: -1, Local: nt3dmath.Identity()}}, Clips: []AnimationClip{{Tracks: []Track{{Node: 1, Width: 3, Channel: Translation, Times: []float32{0, 1}, Values: []float32{0, 0, 0, 1, 0, 0}}}}}}
	s := NewScene()
	a, e := asset.Instantiate(s, Root, "a")
	check(t, e)
	b, e := asset.Instantiate(s, Root, "b")
	check(t, e)
	player, e := NewAnimation(s, a.Clips[0])
	check(t, e)
	check(t, player.Sample(1))
	an, _ := s.Node(a.Nodes[0])
	bn, _ := s.Node(b.Nodes[0])
	nearAnimation(t, an.Local[12], 1)
	nearAnimation(t, bn.Local[12], 0)
	if len(s.AnimationClips(a.Root)) != 1 {
		t.Fatal("clips not exposed")
	}
}

func TestDeformedPickingCullingAndShadowBounds(t *testing.T) {
	s, id, joint := animationSkinScene(t)
	camera := DefaultCamera()
	check(t, s.SetMorphWeights(id, []float32{.3}))
	check(t, s.SetTransform(joint, nt3dmath.Translation(nt3dmath.Vec3{X: -.5, Y: -.5})))
	f := frame(t, s)
	p := nt3dmath.Vec3{X: -.2, Y: -.2, Z: .3}
	x, y, _, ok := f.Matrix.Project(p, f.Width, f.Height)
	if !ok {
		t.Fatal("test point outside camera")
	}
	hit := s.Pick(camera, f, x, y, 1)
	if hit == nil || hit.Node != id {
		t.Fatal("posed mesh not pickable")
	}
	nearAnimation(t, hit.Position.Z, .3)
	f.Lighting = Lighting{Lights: []Light{{Direction: nt3dmath.Vec3{X: 1, Z: 1}, Shadows: true}}}
	view, active := fitShadow(&f, f.drawn(1), 2048)
	if !active {
		t.Fatal("deformed mesh lost shadow")
	}
	for _, v := range f.Geometry[0].Vertices {
		_, _, z, w := view.matrix.Transform(v.Position)
		if z/w < -.001 || z/w > 1.001 {
			t.Fatal("posed mesh outside shadow bounds")
		}
	}
	check(t, s.SetTransform(joint, nt3dmath.Translation(nt3dmath.Vec3{X: 1000})))
	if frame(t, s).drawn(1)[0] {
		t.Fatal("offscreen pose not culled")
	}
	// An old frame still picks its old pose even after the current mesh moved.
	if hit := s.Pick(camera, f, x, y, 1); hit == nil || hit.Node != id {
		t.Fatal("old frame picking changed")
	}
}

func BenchmarkSkin10000Vertices(b *testing.B) {
	s := NewScene()
	j, _ := s.Add(Root, "joint")
	g := Geometry{Vertices: make([]Vertex, 10000)}
	in := make([]Influence, len(g.Vertices))
	for i := range g.Vertices {
		g.Vertices[i].Position = nt3dmath.Vec3{X: float32(i%100) / 100, Y: float32(i/100) / 100}
		in[i].Weights[0] = 1
	}
	id, e := s.SetGeometry("mesh", g)
	if e != nil {
		b.Fatal(e)
	}
	if e = s.SetDeformation(id, Deformation{Skeleton: &Skeleton{Joints: []NodeID{j}}, Influences: in}); e != nil {
		b.Fatal(e)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if e = s.SetTransform(j, nt3dmath.RotationZ(float64(i)*.01)); e != nil {
			b.Fatal(e)
		}
		if e = s.UpdateDeformations(); e != nil {
			b.Fatal(e)
		}
	}
}

func TestRotationTrackDoesNotAccumulateScale(t *testing.T) {
	s := NewScene()
	id, e := s.Add(Root, "rotating")
	check(t, e)
	a, e := NewAnimation(s, AnimationClip{Tracks: []Track{{Node: id, Channel: Rotation, Width: 4, Times: []float32{0, 1}, Values: []float32{0, 0, 0, 1, 0, 0, 1, 0}}}})
	check(t, e)
	a.Mode = Loop
	for i := range 10000 {
		check(t, a.Sample(float64(i)*.007))
	}
	n, _ := s.Node(id)
	nearAnimation(t, float32(n.Local.Determinant3()), 1)
}
