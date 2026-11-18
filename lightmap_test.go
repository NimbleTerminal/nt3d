// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"bytes"
	"image"
	"image/color"
	"math"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"testing"
)

func testLightMap(t *testing.T, c [3]float32) *LightMap {
	t.Helper()
	m, err := NewLightMap(1, 1, c[:])
	check(t, err)
	return m
}

func lightMapFrame(m *Material) Frame {
	f := pbrQuadFrame(m, color.RGBA{200, 200, 200, 255})
	f.Draws[0].Geometry.LightMapUV = append([]UV(nil), f.Draws[0].Geometry.UV...)
	return f
}

func TestLightMapData(t *testing.T) {
	data := []float32{8, 2, 0, 0, 4, 16}
	m, err := NewLightMap(2, 1, data)
	check(t, err)
	data[0] = 0
	for _, tc := range []struct {
		uv   UV
		lod  float32
		want [4]float32
	}{
		{UV{-1, 2}, 0, [4]float32{8, 2, 0, 1}},
		{UV{2, -1}, 0, [4]float32{0, 4, 16, 1}},
		{UV{.5, .5}, 0, [4]float32{4, 3, 8, 1}},
		{UV{0, 0}, 1, [4]float32{4, 3, 8, 1}},
	} {
		if got := m.texture.sampleLinear(tc.uv, false, tc.lod, false); got != tc.want {
			t.Fatalf("sample %+v: %v", tc, got)
		}
	}
	for _, bad := range []float32{-1, float32(math.NaN()), float32(math.Inf(1)), 65505} {
		if _, err := NewLightMap(1, 1, []float32{bad, 0, 0}); err == nil {
			t.Fatal("accepted invalid irradiance", bad)
		}
	}
	for _, size := range [][2]int{{0, 1}, {8193, 1}, {1, 1}} {
		if _, err := NewLightMap(size[0], size[1], nil); err == nil {
			t.Fatal("accepted invalid dimensions/length")
		}
	}
	dataHDR := encodeRadianceHDR(2, 1, false, 1, "", func(x, y int) [3]float64 { return [3]float64{8, 2, 1} })
	hdr, err := NewHDRLightMap(dataHDR)
	check(t, err)
	if got := hdr.texture.sampleLinear(UV{}, false, 0, false); abs(got[0]-8) > .04 || abs(got[1]-2) > .04 || abs(got[2]-1) > .04 {
		t.Fatal(got)
	}
}

func TestLightMapDiffuseComposition(t *testing.T) {
	r := lightRig{sky: [3]float32{10, 10, 10}, ground: [3]float32{10, 10, 10}}
	r.bakedDiffuse = lightMapDiffuse([4]float32{math.Pi, 2 * math.Pi, 0, 1}, true)
	for _, got := range []indirectSample{r.indirectDiffuse(nt3dmath.Vec3{Z: 1}, .5), r.indirectToonDiffuse(.5)} {
		if got != (indirectSample{[3]float32{.5, 1, 0}, 1}) {
			t.Fatal(got)
		}
	}
	r.bakedDiffuse = lightMapDiffuse([4]float32{}, true)
	if got := r.indirectDiffuse(nt3dmath.Vec3{Z: 1}, 1); got.radiance != ([3]float32{}) || got.coverage != 1 {
		t.Fatal("black bake exposed ambient", got)
	}
	black := testLightMap(t, [3]float32{})
	bright := testLightMap(t, [3]float32{math.Pi, 0, 0})
	for _, mat := range []Material{{}, {PBR: true, Roughness: .5}, {Unlit: true}, {PBR: true, Metallic: 1, Roughness: .5}} {
		mat.LightMap = black
		f := lightMapFrame(&mat)
		f.Lighting = Lighting{Ambient: Ambient{Sky: color.RGBA{255, 255, 255, 255}, Ground: color.RGBA{255, 255, 255, 255}}}
		before := softwareRender(f)
		mat.LightMap = bright
		after := softwareRender(f)
		if mat.Unlit || mat.Metallic == 1 {
			if !bytes.Equal(before.(*image.RGBA).Pix, after.(*image.RGBA).Pix) {
				t.Fatal("lightmap changed unlit/metal surface")
			}
		} else {
			if at(after, 40, 40).R <= at(before, 40, 40).R+20 {
				t.Fatal("lightmap did not light surface")
			}
		}
	}
}

func TestLightMapUVOperations(t *testing.T) {
	f := lightMapFrame(&Material{LightMap: testLightMap(t, [3]float32{1, 1, 1})})
	g := f.Draws[0].Geometry
	for i := range g.LightMapUV {
		g.LightMapUV[i].U = 1 - g.UV[i].U
	}
	check(t, g.Validate())
	clone := g.Clone()
	clone.LightMapUV[0].U = 7
	if g.LightMapUV[0].U == 7 {
		t.Fatal("UV alias")
	}
	var b nt3dmath.AABB
	b.Include(nt3dmath.Vec3{X: -.3, Y: -.3})
	b.Include(nt3dmath.Vec3{X: .3, Y: .3, Z: 1})
	clipped, err := Clip(g, b)
	check(t, err)
	if len(clipped.LightMapUV) == 0 {
		t.Fatal("empty clip")
	}
	for i, uv := range clipped.LightMapUV {
		if abs(uv.U+clipped.UV[i].U-1) > 1e-5 {
			t.Fatal("clip UV interpolation")
		}
	}
	smooth, err := SmoothNormals(g, 1)
	check(t, err)
	if len(smooth.LightMapUV) != len(smooth.Vertices) {
		t.Fatal("smooth lost UVs")
	}
	merged, err := MergeGeometry(g, g)
	check(t, err)
	if len(merged.LightMapUV) != 2*len(g.LightMapUV) {
		t.Fatal("merge lost UVs")
	}
	g.UV = nil
	check(t, g.Validate()) // Lightmaps do not require base UVs.
	g.LightMapUV = nil
	if g.Validate() == nil {
		t.Fatal("missing bake UV accepted")
	}
}

func TestLightMapMaterialSnapshots(t *testing.T) {
	s := NewScene()
	a := testLightMap(t, [3]float32{1, 0, 0})
	b := testLightMap(t, [3]float32{0, 1, 0})
	g := lightMapFrame(&Material{LightMap: a}).Draws[0].Geometry
	id, err := s.SetGeometry("baked", g)
	check(t, err)
	before := frame(t, s)
	check(t, s.SetMaterial(id, &Material{LightMap: b}))
	after := frame(t, s)
	if before.Revision != after.Revision || before.TextureRevision == after.TextureRevision {
		t.Fatal("lightmap replacement must refresh bindings only")
	}
	if before.Draws[0].Geometry.Material.LightMap != a || after.Draws[0].Geometry.Material.LightMap != b {
		t.Fatal("material snapshot changed")
	}
	check(t, s.SetMaterial(id, nil))
	removed := frame(t, s)
	if removed.Revision == after.Revision {
		t.Fatal("removing UV extension did not invalidate mesh")
	}
	g.LightMapUV = nil
	g.Material = nil
	bad, err := s.SetGeometry("missing-uv", g)
	check(t, err)
	if s.SetMaterial(bad, &Material{LightMap: a}) == nil {
		t.Fatal("material override accepted missing UVs")
	}
}
