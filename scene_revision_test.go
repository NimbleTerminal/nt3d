// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image/color"
	"nimbleterminal.dev/nt3d/math"
	"testing"
)

func TestSceneGeometryRevisions(t *testing.T) {
	s := NewScene()
	a, err := s.SetGeometry("a", pointGeometry())
	check(t, err)
	b, err := s.SetGeometry("b", pointGeometry())
	check(t, err)
	old := frame(t, s)
	check(t, s.SetTransform(a, nt3dmath.Translation(nt3dmath.Vec3{X: .5})))
	check(t, s.SetVisible(a, false))
	moved := frame(t, s)
	for i := range old.Draws {
		if old.Draws[i].Revision != moved.Draws[i].Revision {
			t.Fatal("transform/visibility invalidated geometry")
		}
	}
	check(t, s.SetNodeGeometry(b, pointGeometry()))
	updated := frame(t, s)
	if updated.Draws[0].Revision != old.Draws[0].Revision || updated.Draws[1].Revision == old.Draws[1].Revision {
		t.Fatal("geometry revisions not independent")
	}
	check(t, s.Remove(a))
	if f := frame(t, s); f.Draws[0].Revision != updated.Draws[1].Revision {
		t.Fatal("removal invalidated survivor")
	}
	tex := testTexture(t, color.RGBA{255, 0, 0, 255})
	g, err := BuildHeightfield(Heightfield{NX: 2, NY: 2, MinX: -1, MaxX: 1, MinY: -1, MaxY: 1, Heights: make([]float32, 4), Material: &Material{Texture: tex}})
	check(t, err)
	check(t, s.SetNodeGeometry(b, g))
	before := frame(t, s)
	check(t, s.SetTexture("b", testTexture(t, color.RGBA{0, 255, 0, 255})))
	after := frame(t, s)
	if before.Draws[0].Revision != after.Draws[0].Revision || before.Draws[0].Geometry.Material.Texture != tex {
		t.Fatal("texture edit invalidated mesh or mutated snapshot")
	}
	bad := after
	bad.Draws = append(append([]Draw(nil), after.Draws...), after.Draws[0])
	if err := validateFrame(bad); err == nil {
		t.Fatal("accepted two cached draws of one node")
	}
	bad.Draws = append([]Draw(nil), after.Draws...)
	bad.Draws[0].Node = Root
	if err := validateFrame(bad); err == nil {
		t.Fatal("accepted missing cache identity")
	}
}
