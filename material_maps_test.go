// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image"
	"testing"
)

// A normal map and an ORM map are both linear data, but each is refused in
// the other's place: a normal read as occlusion, roughness and metalness, or
// the reverse, draws without complaint and is wrong.
func TestNormalAndORMMapsAreNotInterchangeable(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	normal, err := NewNormalMap(img)
	check(t, err)
	orm, err := NewORMMap(img)
	check(t, err)
	const source = "fn nt3dTexture(uv: vec2<f32>) -> vec4<f32> { return vec4<f32>(uv, 1.0, 1.0); }"
	shaderNormal, err := NewShaderNormalMap(source, img)
	check(t, err)
	shaderORM, err := NewShaderORMMap(source, img)
	check(t, err)

	for _, c := range []struct {
		name string
		m    Material
		ok   bool
	}{
		{"normal map as normal", Material{Normal: NormalStyle{Map: normal}}, true},
		{"shader normal map as normal", Material{Normal: NormalStyle{Map: shaderNormal}}, true},
		{"ORM map as normal", Material{Normal: NormalStyle{Map: orm}}, false},
		{"shader ORM map as normal", Material{Normal: NormalStyle{Map: shaderORM}}, false},
		{"ORM map as ORM", Material{Shading: ShadePBR, PBR: PBRStyle{ORMMap: orm}}, true},
		{"shader ORM map as ORM", Material{Shading: ShadePBR, PBR: PBRStyle{ORMMap: shaderORM}}, true},
		{"normal map as ORM", Material{Shading: ShadePBR, PBR: PBRStyle{ORMMap: normal}}, false},
		{"shader normal map as ORM", Material{Shading: ShadePBR, PBR: PBRStyle{ORMMap: shaderNormal}}, false},
	} {
		if err := c.m.Validate(); (err == nil) != c.ok {
			t.Errorf("%s: Validate() = %v, want ok %v", c.name, err, c.ok)
		}
	}
}

// Swapping a node's map holds the same rule as setting it.
func TestSetNodeMapRefusesTheOtherKindOfDataMap(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	normal, err := NewNormalMap(img)
	check(t, err)
	orm, err := NewORMMap(img)
	check(t, err)
	g := normalFrame(t).Draws[0].Geometry
	g.Material = &Material{Shading: ShadePBR, Normal: NormalStyle{Map: normal}, PBR: PBRStyle{ORMMap: orm}}
	s := NewScene()
	id, err := s.SetGeometry("plane", g)
	check(t, err)

	otherNormal, err := NewNormalMap(img)
	check(t, err)
	otherORM, err := NewORMMap(img)
	check(t, err)
	if err := s.SetNodeORMMap(id, otherNormal); err == nil {
		t.Error("SetNodeORMMap accepted a normal map")
	}
	if err := s.SetNodeNormalMap(id, otherORM); err == nil {
		t.Error("SetNodeNormalMap accepted an ORM map")
	}
	check(t, s.SetNodeORMMap(id, otherORM))
	check(t, s.SetNodeNormalMap(id, otherNormal))
}
