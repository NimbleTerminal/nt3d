package nt3d

import (
	"image/color"
	"math"
	"nimbleterminal.dev/nt3d/math"
	"testing"
)

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func frame(t *testing.T, s *Scene) Frame {
	t.Helper()
	f, err := s.Frame(DefaultCamera(), 200, 120)
	check(t, err)
	return f
}
func pointGeometry() Geometry {
	return Geometry{Points: []Point{{Radius: 4, Color: color.RGBA{255, 0, 0, 255}, Datum: 42}}}
}
func TestHierarchyAndTransactions(t *testing.T) {
	s := NewScene()
	parent, err := s.Add(Root, "group")
	check(t, err)
	id, err := s.SetGeometry("point", pointGeometry())
	check(t, err)
	check(t, s.Reparent(id, parent))
	check(t, s.SetTransform(parent, nt3dmath.Translation(nt3dmath.Vec3{X: 2})))
	check(t, s.SetTransform(id, nt3dmath.Translation(nt3dmath.Vec3{Y: 3})))
	f := frame(t, s)
	if p := f.Instances[0].World.Point(nt3dmath.Vec3{}); p != (nt3dmath.Vec3{X: 2, Y: 3}) {
		t.Fatal(p)
	}
	revision := s.Revision()
	if err = s.Reparent(parent, id); err == nil {
		t.Fatal("cycle accepted")
	}
	if revision != s.Revision() {
		t.Fatal("failed edit changed scene")
	}
	for _, m := range []nt3dmath.Mat4{{}, nt3dmath.Scaling(nt3dmath.Vec3{X: -1, Y: 1, Z: 1})} {
		if s.SetTransform(id, m) == nil {
			t.Fatal("invalid transform accepted")
		}
	}
	node, _ := s.Node(parent)
	node.Children[0] = 999
	if n, _ := s.Node(parent); n.Children[0] != id {
		t.Fatal("mutable children escaped")
	}
	check(t, s.SetVisible(parent, false))
	hidden := frame(t, s)
	if hidden.Instances[0].Visible || s.Bounds().IsValid() {
		t.Fatal("hidden parent")
	}
	if hidden.Revision != f.Revision {
		t.Fatal("visibility invalidated meshes")
	}
	check(t, s.SetVisible(parent, true))
	check(t, s.SetTransform(parent, nt3dmath.Identity()))
	moved := frame(t, s)
	if moved.Revision != f.Revision {
		t.Fatal("transform invalidated meshes")
	}
	if f.Instances[0].World[12] != 2 {
		t.Fatal("old frame mutated")
	}
	check(t, s.Remove(parent))
	if _, ok := s.Node(id); ok {
		t.Fatal("subtree survived")
	}
	newID, err := s.Add(Root, "point")
	check(t, err)
	if newID == id {
		t.Fatal("ID reused")
	}
	if s.Remove(Root) == nil {
		t.Fatal("root removed")
	}
}
func TestGeometryAndTextureSnapshotOwnership(t *testing.T) {
	s := NewScene()
	g, err := BuildHeightfield(Heightfield{NX: 3, NY: 3, MinX: -1, MaxX: 1, MinY: -1, MaxY: 1, Sampler: func(x, y float32) float32 { return x + y }, Material: &Material{Texture: testTexture(t, color.RGBA{255, 0, 0, 255}), Unlit: true}})
	check(t, err)
	_, err = s.SetGeometry("terrain", g)
	check(t, err)
	before := frame(t, s)
	g.Vertices[0].Position.X = 999
	g.Indices[0] = 999
	g.Material.Unlit = false
	if before.Geometry[0].Vertices[0].Position.X == 999 || before.Geometry[0].Indices[0] == 999 || !before.Geometry[0].Material.Unlit {
		t.Fatal("caller storage retained")
	}
	green := testTexture(t, color.RGBA{0, 255, 0, 255})
	check(t, s.SetTexture("terrain", green))
	after := frame(t, s)
	if before.Revision != after.Revision || before.TextureRevision == after.TextureRevision {
		t.Fatal("wrong texture revisions")
	}
	if before.Geometry[0].Material.Texture == green || after.Geometry[0].Material.Texture != green {
		t.Fatal("texture snapshot mutated")
	}
	if &before.Geometry[0].Vertices[0] != &after.Geometry[0].Vertices[0] {
		t.Fatal("texture change copied mesh")
	}
	revision := s.Revision()
	bad := pointGeometry()
	bad.Points[0].Position.X = float32(math.NaN())
	if _, err = s.SetGeometry("bad", bad); err == nil {
		t.Fatal("NaN accepted")
	}
	if _, ok := s.Find("bad"); ok || s.Revision() != revision {
		t.Fatal("failed setter mutated scene")
	}
	other := NewScene()
	_, err = other.SetGeometry("terrain", pointGeometry())
	check(t, err)
	if frame(t, other).Revision == after.Revision {
		t.Fatal("cross-scene GPU cache collision")
	}
}
func TestHeightfieldNormalsAndClippedUV(t *testing.T) {
	g, err := BuildHeightfield(Heightfield{NX: 4, NY: 3, MinX: -1, MaxX: 1, MinY: -1, MaxY: 1, Sampler: func(x, y float32) float32 { return x + y }})
	check(t, err)
	if len(g.Vertices) != 12 || len(g.Indices) != 36 {
		t.Fatal("topology")
	}
	want := nt3dmath.Vec3{X: -1, Y: -1, Z: 1}.Normalize()
	for _, v := range g.Vertices {
		if v.Normal.Sub(want).Dot(v.Normal.Sub(want)) > 1e-6 {
			t.Fatal(v.Normal)
		}
	}
	if g.UV[0] != (UV{0, 1}) || g.UV[len(g.UV)-1] != (UV{1, 0}) {
		t.Fatal("UV orientation")
	}
	var box nt3dmath.AABB
	box.Include(nt3dmath.Vec3{X: -.5, Y: -.5, Z: -2})
	box.Include(nt3dmath.Vec3{X: .5, Y: .5, Z: 2})
	clipped, err := Clip(g, box)
	check(t, err)
	if len(clipped.Vertices) == 0 || len(clipped.UV) != len(clipped.Vertices) {
		t.Fatal("clipped UVs missing")
	}
	for _, v := range clipped.Vertices {
		if !contains(box, v.Position) {
			t.Fatal(v.Position)
		}
	}
}
func TestPickTransformedPrimitives(t *testing.T) {
	for _, kind := range []Primitive{PointPrimitive, TrianglePrimitive, BoxPrimitive, LinePrimitive, ArrowPrimitive} {
		t.Run([]string{"point", "triangle", "box", "line", "arrow"}[kind], func(t *testing.T) {
			s := NewScene()
			g := Geometry{}
			white := color.RGBA{255, 255, 255, 255}
			switch kind {
			case PointPrimitive:
				g = pointGeometry()
			case TrianglePrimitive:
				g.Vertices = []Vertex{{Position: nt3dmath.Vec3{X: -.3, Y: -.3}, Color: white}, {Position: nt3dmath.Vec3{X: .3, Y: -.3}, Color: white}, {Position: nt3dmath.Vec3{Y: .3}, Color: white}}
				g.Indices = []uint32{0, 1, 2}
			case BoxPrimitive:
				g.Boxes = []Box{{Min: nt3dmath.Vec3{X: -.1, Y: -.1, Z: -.1}, Size: nt3dmath.Vec3{X: .2, Y: .2, Z: .2}, Datum: 42}}
			case LinePrimitive:
				g.Lines = []Vertex{{Position: nt3dmath.Vec3{X: -.3}}, {Position: nt3dmath.Vec3{X: .3}}}
			case ArrowPrimitive:
				g.Arrows = []Arrow{{Start: nt3dmath.Vec3{X: -.3}, End: nt3dmath.Vec3{X: .3}, Width: 2, HeadSize: 8, Datum: 42}}
			}
			id, err := s.SetGeometry("object", g)
			check(t, err)
			transform := nt3dmath.Translation(nt3dmath.Vec3{X: .4, Y: .1}).Mul(nt3dmath.RotationZ(.7)).Mul(nt3dmath.Scaling(nt3dmath.Vec3{X: 1.4, Y: .8, Z: 2}))
			check(t, s.SetTransform(id, transform))
			camera := DefaultCamera()
			camera.Alpha = 70
			f, err := s.Frame(camera, 320, 240)
			check(t, err)
			x, y, _, ok := f.Matrix.Project(transform.Point(nt3dmath.Vec3{}), 320, 240)
			if !ok {
				t.Fatal("target outside")
			}
			hit := s.Pick(camera, f, x, y, 3)
			if hit == nil || hit.Node != id || hit.Primitive != kind {
				t.Fatalf("wrong pick %#v", hit)
			}
			if kind == PointPrimitive || kind == BoxPrimitive || kind == ArrowPrimitive {
				if hit.Datum != 42 {
					t.Fatal("lost datum")
				}
			}
			check(t, s.SetVisible(id, false))
			f, err = s.Frame(camera, 320, 240)
			check(t, err)
			if s.Pick(camera, f, x, y, 3) != nil {
				t.Fatal("hidden pick")
			}
		})
	}
}
