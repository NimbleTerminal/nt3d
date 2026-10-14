// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// grid places n×n nodes a spacing apart, each showing either one shared
// sphere or its own copy of it.
func grid(t *testing.T, n int, spacing float32, shared bool) (*Scene, []NodeID, *SharedGeometry) {
	t.Helper()
	ball, err := BuildSphere(Sphere{Radius: .12, Segments: 24, Rings: 12, Color: color.RGBA{220, 120, 40, 255}})
	check(t, err)
	sg, err := NewSharedGeometry(ball)
	check(t, err)
	s := NewScene()
	var ids []NodeID
	for y := range n {
		for x := range n {
			name := fmt.Sprint("ball ", x, y)
			var id NodeID
			if shared {
				id, err = s.SetShared(name, sg)
			} else {
				id, err = s.SetGeometry(name, ball)
			}
			check(t, err)
			at := nt3dmath.Vec3{X: (float32(x) - float32(n-1)/2) * spacing, Y: (float32(y) - float32(n-1)/2) * spacing}
			check(t, s.SetTransform(id, nt3dmath.Translation(at)))
			ids = append(ids, id)
		}
	}
	return s, ids, sg
}

func TestSharedGeometry(t *testing.T) {
	if _, err := NewSharedGeometry(pointGeometry()); err == nil {
		t.Fatal("shared points accepted")
	}
	ball, err := BuildSphere(Sphere{Radius: .5})
	check(t, err)
	sg, err := NewSharedGeometry(ball)
	check(t, err)
	ball.Vertices[0].Position.X = 99 // The shared copy is its own.
	if sg.Bounds().Max.X != .5 {
		t.Fatalf("bounds %v", sg.Bounds())
	}
	s := NewScene()
	a, err := s.SetShared("a", sg)
	check(t, err)
	b, err := s.Add(Root, "b")
	check(t, err)
	check(t, s.SetNodeShared(b, sg))
	c, err := s.SetGeometry("c", ball)
	check(t, err)
	if s.SetNodeShared(Root, sg) == nil || s.SetNodeShared(b, nil) == nil {
		t.Fatal("bad shared nodes accepted")
	}
	f := frame(t, s)
	index := map[NodeID]int{}
	for i, inst := range f.Instances {
		index[inst.Node] = i
	}
	ia, ib, ic := index[a], index[b], index[c]
	if f.Shared[ia] == 0 || f.Shared[ia] != f.Shared[ib] || f.Shared[ic] != 0 {
		t.Fatalf("shared marks %v", f.Shared)
	}
	if &f.Geometry[ia].Vertices[0] != &f.Geometry[ib].Vertices[0] {
		t.Fatal("shared nodes hold copies")
	}
	// A node's own geometry ends its sharing.
	check(t, s.SetNodeGeometry(b, ball))
	if f = frame(t, s); f.Shared[index[b]] != 0 {
		t.Fatal("own geometry still shared")
	}
}

// Shared geometry renders as copies do, and textures stay per node.
func TestSharedGeometryRendersAsCopies(t *testing.T) {
	shared, _, _ := grid(t, 4, .4, true)
	copies, _, _ := grid(t, 4, .4, false)
	if !bytes.Equal(softwareRender(frame(t, shared)).(*image.RGBA).Pix, softwareRender(frame(t, copies)).(*image.RGBA).Pix) {
		t.Fatal("shared and copied pictures differ")
	}
	a, err := RenderWireframe(frame(t, shared), 60, 20)
	check(t, err)
	c, err := RenderWireframe(frame(t, copies), 60, 20)
	check(t, err)
	if a != c {
		t.Fatal("shared and copied wireframes differ")
	}
	tex := testTexture(t, color.RGBA{10, 220, 80, 255})
	plane, err := BuildHeightfield(Heightfield{NX: 2, NY: 2, MinX: -.2, MaxX: .2, MinY: -.2, MaxY: .2, Heights: make([]float32, 4), Material: &Material{Texture: tex}})
	check(t, err)
	sg, err := NewSharedGeometry(plane)
	check(t, err)
	s := NewScene()
	_, err = s.SetShared("a", sg)
	check(t, err)
	_, err = s.SetShared("b", sg)
	check(t, err)
	other := testTexture(t, color.RGBA{200, 0, 0, 255})
	check(t, s.SetTexture("b", other))
	f := frame(t, s)
	if f.Shared[0] != f.Shared[1] || f.Geometry[0].Material.Texture != tex || f.Geometry[1].Material.Texture != other {
		t.Fatal("a node's texture leaked to the shared geometry")
	}
}

// Picks hit the right node of shared geometry, indexed once for all.
func TestSharedGeometryPicking(t *testing.T) {
	s, ids, sg := grid(t, 3, .5, true)
	camera := DefaultCamera()
	camera.Projection, camera.Alpha = Perspective, 89
	f, err := s.Frame(camera, 300, 300)
	check(t, err)
	for _, id := range ids {
		w := s.nodes[id].Node
		x, y, _, ok := f.Matrix.Project(s.world(w.ID).Point(nt3dmath.Vec3{Z: .1}), f.Width, f.Height)
		if !ok {
			t.Fatal("ball out of view")
		}
		hit := s.Pick(camera, f, x, y, 1)
		if hit == nil || hit.Node != id {
			t.Fatalf("pick at node %d hit %+v", id, hit)
		}
	}
	if sg.pick == nil {
		t.Fatal("shared geometry was not indexed")
	}
	<-sg.pick.done
	for _, id := range ids {
		if s.nodes[id].pickIndex != nil {
			t.Fatal("a node indexed shared geometry for itself")
		}
	}
}
