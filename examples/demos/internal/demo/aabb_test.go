// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"image/color"
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/aabbtree"
	"nimbleterminal.dev/nt3d/math"
)

func newAABBDemo(t *testing.T) *demo {
	t.Helper()
	d := &demo{scene: nt3d.NewScene()}
	var err error
	if d.group, err = d.scene.Add(nt3d.Root, "island"); err != nil {
		t.Fatal(err)
	}
	if err := d.setupAABB(); err != nil {
		t.Fatal(err)
	}
	return d
}

// hierarchyLines returns the hierarchy node's line vertices.
func hierarchyLines(t *testing.T, d *demo) []nt3d.Vertex {
	t.Helper()
	f, err := d.scene.Frame(nt3d.DefaultCamera(), 64, 48)
	if err != nil {
		t.Fatal(err)
	}
	for i, inst := range f.Instances {
		if inst.Name == "hierarchy" {
			return f.Geometry[i].Lines
		}
	}
	t.Fatal("no hierarchy node")
	return nil
}

// nodeColor returns the color of the first box of the named node.
func nodeColor(t *testing.T, d *demo, name string) color.RGBA {
	t.Helper()
	f, err := d.scene.Frame(nt3d.DefaultCamera(), 64, 48)
	if err != nil {
		t.Fatal(err)
	}
	for i, inst := range f.Instances {
		if inst.Name == name {
			return f.Geometry[i].Boxes[0].Color
		}
	}
	t.Fatalf("no node %q", name)
	return color.RGBA{}
}

// lineBounds is the box spanned by 24 line vertices.
func lineBounds(v []nt3d.Vertex) nt3dmath.AABB {
	var b nt3dmath.AABB
	for _, x := range v {
		b.Include(x.Position)
	}
	return b
}

func contains(outer, inner nt3dmath.AABB, slack float32) bool {
	return outer.Min.X <= inner.Min.X+slack && outer.Min.Y <= inner.Min.Y+slack && outer.Min.Z <= inner.Min.Z+slack &&
		outer.Max.X >= inner.Max.X-slack && outer.Max.Y >= inner.Max.Y-slack && outer.Max.Z >= inner.Max.Z-slack
}

func pathDepth(d *demo) int {
	depth := 0
	d.aabb.tree.Ancestors(d.aabb.picked, func(n nt3daabbtree.Node) bool { depth = n.Depth; return true })
	return depth
}

func TestAABBPickAndPath(t *testing.T) {
	d := newAABBDemo(t)
	a := d.aabb
	if !a.hasPick || a.tree.Len() != len(a.boxes) || len(a.boxes) != aabbCounts[1] {
		t.Fatalf("setup: pick %v, %d leaves, %d boxes", a.hasPick, a.tree.Len(), len(a.boxes))
	}
	if err := d.clearAABBPick(); err != nil {
		t.Fatal(err)
	}
	if a.hasPick || len(hierarchyLines(t, d)) != 0 {
		t.Fatal("clear left a pick or lines")
	}
	// The topmost box in box 3's vertical column is what a ray from above meets.
	target := a.boxes[3]
	center := aabbCenter(target, 0)
	column := aabbWorld(nt3dmath.Vec3{X: center.X, Y: center.Y, Z: 50}, nt3dmath.Vec3{Z: 100})
	top, topZ := -1, float32(-1)
	a.tree.QueryOverlap(column, func(_ nt3daabbtree.ID, i int) bool {
		if z := aabbCenter(a.boxes[i], 0).Z; z > topZ {
			top, topZ = i, z
		}
		return true
	})
	if top < 0 {
		t.Fatal("no box in the column")
	}
	ray := nt3dmath.Ray{Origin: nt3dmath.Vec3{X: center.X, Y: center.Y, Z: 20}, Direction: nt3dmath.Vec3{Z: -1}}
	if err := d.pickAABB(ray); err != nil {
		t.Fatal(err)
	}
	if !a.hasPick || a.picked != a.boxes[top].id {
		t.Fatalf("picked %v %d, want %d", a.hasPick, a.picked, a.boxes[top].id)
	}
	if c := nodeColor(t, d, a.boxes[top].name()); c != (color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("picked box color %v", c)
	}
	lines := hierarchyLines(t, d)
	depth := pathDepth(d)
	if len(lines) != 24*(depth+1) {
		t.Fatalf("%d line vertices for depth %d", len(lines), depth)
	}
	slack := float32(.02) * float32(a.tree.Height())
	for i := 24; i < len(lines); i += 24 {
		if !contains(lineBounds(lines[i-24:i]), lineBounds(lines[i:i+24]), slack) {
			t.Fatalf("path box %d does not contain box %d", i/24-1, i/24)
		}
	}
	world := aabbWorld(aabbCenter(a.boxes[top], 0), a.boxes[top].size)
	if !contains(lineBounds(lines[len(lines)-24:]), world, slack) {
		t.Fatal("the last path box does not contain the picked box")
	}
	// Clicking empty space clears.
	if err := d.pickAABB(nt3dmath.Ray{Origin: nt3dmath.Vec3{Z: 50}, Direction: nt3dmath.Vec3{Z: 1}}); err != nil {
		t.Fatal(err)
	}
	if a.hasPick || len(hierarchyLines(t, d)) != 0 {
		t.Fatal("a miss left a pick")
	}
	if c := nodeColor(t, d, a.boxes[top].name()); c != a.boxes[top].color {
		t.Fatalf("unpicked box color %v, want %v", c, a.boxes[top].color)
	}
}

func TestAABBDriftKeepsTree(t *testing.T) {
	d := newAABBDemo(t)
	a := d.aabb
	for range 5 {
		d.animationTime += .1
		if err := d.stepAABB(); err != nil {
			t.Fatal(err)
		}
	}
	if a.tree.Len() != len(a.boxes) {
		t.Fatalf("%d leaves, %d boxes", a.tree.Len(), len(a.boxes))
	}
	for _, b := range a.boxes {
		want := aabbWorld(aabbCenter(b, d.animationTime), b.size)
		if got := a.tree.Bounds(b.id); got.Min != want.Min || got.Max != want.Max {
			t.Fatalf("box %d: tree %v, want %v", b.index, got, want)
		}
		if a.tree.Value(b.id) != b.index {
			t.Fatalf("box %d carries %d", b.index, a.tree.Value(b.id))
		}
	}
	if !a.hasPick {
		t.Fatal("drift lost the pick")
	}
	if lines := hierarchyLines(t, d); len(lines) != 24*(pathDepth(d)+1) {
		t.Fatalf("%d line vertices after drift", len(lines))
	}
}

func TestAABBAddRemoveCycle(t *testing.T) {
	d := newAABBDemo(t)
	a := d.aabb
	n := len(a.boxes)
	if _, err := d.addAABBBox(); err != nil {
		t.Fatal(err)
	}
	if a.tree.Len() != n+1 || len(a.boxes) != n+1 {
		t.Fatalf("after add: %d leaves, %d boxes", a.tree.Len(), len(a.boxes))
	}
	if err := d.removePickedAABB(); err != nil {
		t.Fatal(err)
	}
	if a.tree.Len() != n || len(a.boxes) != n || a.hasPick || len(hierarchyLines(t, d)) != 0 {
		t.Fatalf("after remove: %d leaves, %d boxes, pick %v", a.tree.Len(), len(a.boxes), a.hasPick)
	}
	if err := d.removePickedAABB(); err != nil {
		t.Fatal(err)
	}
	if a.tree.Len() != n {
		t.Fatal("removing nothing changed the tree")
	}
	if err := d.cycleAABBCount(); err != nil {
		t.Fatal(err)
	}
	if len(a.boxes) != aabbCounts[2] || a.tree.Len() != len(a.boxes) {
		t.Fatalf("after cycle: %d boxes, %d leaves", len(a.boxes), a.tree.Len())
	}
	for _, b := range a.boxes {
		if a.tree.Value(b.id) != b.index {
			t.Fatalf("box %d carries %d", b.index, a.tree.Value(b.id))
		}
	}
	if !a.hasPick {
		t.Fatal("a rebuilt scene should start with a pick")
	}
	if err := d.cycleAABBShow(); err != nil {
		t.Fatal(err)
	}
	if a.show != showAll || len(hierarchyLines(t, d)) != 24*(2*len(a.boxes)-1) {
		t.Fatalf("show all: %v, %d vertices", a.show, len(hierarchyLines(t, d)))
	}
	if err := d.cycleAABBShow(); err != nil {
		t.Fatal(err)
	}
	if a.show != showNone || len(hierarchyLines(t, d)) != 0 {
		t.Fatalf("show none: %v, %d vertices", a.show, len(hierarchyLines(t, d)))
	}
	if err := d.cycleAABBShow(); err != nil {
		t.Fatal(err)
	}
	if a.show != showPath || len(hierarchyLines(t, d)) != 24*(pathDepth(d)+1) {
		t.Fatalf("show path: %v, %d vertices", a.show, len(hierarchyLines(t, d)))
	}
}

func TestAABBPickRay(t *testing.T) {
	cam := nt3d.DefaultCamera()
	cam.Alpha, cam.Distance, cam.Projection = 30, 15, nt3d.Perspective
	hit := nt3dmath.Vec3{X: 1, Y: 2, Z: .5}
	ray := aabbPickRay(cam, hit)
	if ray.Origin != cam.Eye() {
		t.Fatalf("perspective ray starts at %v, not the eye %v", ray.Origin, cam.Eye())
	}
	if want := hit.Sub(cam.Eye()); ray.Direction != want {
		t.Fatalf("perspective ray direction %v, want %v", ray.Direction, want)
	}

	cam.Projection = nt3d.Orthographic
	ray = aabbPickRay(cam, hit)
	view := cam.Target.Sub(cam.Eye()).Normalize()
	dir := ray.Direction.Normalize()
	if d := dir.Dot(view); d < .9999 {
		t.Fatalf("orthographic ray direction %v is not the view direction %v", dir, view)
	}
	// The origin lies behind the hit along the view direction, on the ray.
	back := hit.Sub(ray.Origin)
	if along := back.Dot(view); along < float32(cam.Distance) {
		t.Fatalf("origin only %g behind the hit", along)
	}
	off := back.Sub(view.Scale(back.Dot(view)))
	if off.Dot(off) > 1e-6 {
		t.Fatalf("hit is %v off the orthographic ray", off)
	}
}
