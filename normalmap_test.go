package nt3d

import (
	"bytes"
	"image"
	"image/color"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"testing"
)

func testNormalMap(t *testing.T, c color.NRGBA) *Texture {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	tex, err := NewNormalMap(img)
	check(t, err)
	return tex
}
func normalFrame(t *testing.T) Frame {
	m := &Material{NormalMap: testNormalMap(t, color.NRGBA{218, 128, 218, 255}), NormalStrength: 1}
	g := quad(.9, .4, white, m, false)
	g.UV = []UV{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	f := flatFrame(80, 80, g)
	f.Lighting = Lighting{Lights: []Light{{Direction: nt3dmath.Vec3{X: 1}}}}
	return f
}
func TestNormalMapShading(t *testing.T) {
	f := normalFrame(t)
	lit := at(softwareRender(f), 40, 40)
	if lit.R < 180 {
		t.Fatalf("normal map did not tilt toward light: %v", lit)
	}
	f.Geometry[0].Material.NormalStrength = 0
	if c := at(softwareRender(f), 40, 40); c.R != 0 {
		t.Fatalf("zero strength did not restore geometric normal: %v", c)
	}
	f.Geometry[0].Material.NormalStrength = 1
	for i := range f.Geometry[0].UV {
		f.Geometry[0].UV[i].U = 1 - f.Geometry[0].UV[i].U
	}
	if c := at(softwareRender(f), 40, 40); c.R > 3 {
		t.Fatalf("mirrored tangent ignored: %v", c)
	}
	for i := range f.Geometry[0].UV {
		f.Geometry[0].UV[i] = UV{}
	}
	if c := at(softwareRender(f), 40, 40); c.R != 0 {
		t.Fatalf("degenerate UV fallback: %v", c)
	}
}
func TestNormalMapDataAndOwnership(t *testing.T) {
	img := image.NewNRGBA(image.Rect(-2, 3, 6, 11))
	for y := 3; y < 11; y++ {
		for x := -2; x < 6; x++ {
			img.SetNRGBA(x, y, color.NRGBA{128, 128, 255, 0})
		}
	}
	n, err := NewNormalMap(img.SubImage(image.Rect(-1, 4, 5, 10)))
	check(t, err)
	saved := bytes.Clone(n.pixels)
	img.SetNRGBA(0, 5, color.NRGBA{})
	if !bytes.Equal(saved, n.pixels) {
		t.Fatal("normal map aliases source")
	}
	for lod := float32(0); lod < 4; lod++ {
		c := n.sampleLinear(UV{.5, .5}, false, lod)
		if abs(c[0]-128.0/255) > 1e-5 || c[2] != 1 || c[3] != 1 {
			t.Fatalf("normal data gamma/alpha corruption at mip %v: %v", lod, c)
		}
	}
	g := normalFrame(t).Geometry[0]
	g.Material.NormalMap = testTexture(t, white)
	if g.Validate() == nil {
		t.Fatal("accepted color texture in normal slot")
	}
	g.Material.NormalMap = n
	g.UV = nil
	if g.Validate() == nil {
		t.Fatal("accepted normal map without UVs")
	}
}
func TestNormalMapReplacement(t *testing.T) {
	s := NewScene()
	g := normalFrame(t).Geometry[0]
	_, err := s.SetGeometry("mesh", g)
	check(t, err)
	before, err := s.Frame(DefaultCamera(), 80, 80)
	check(t, err)
	next := testNormalMap(t, color.NRGBA{128, 218, 218, 255})
	check(t, s.SetNormalMap("mesh", next))
	after, err := s.Frame(DefaultCamera(), 80, 80)
	check(t, err)
	if before.Revision != after.Revision || before.TextureRevision == after.TextureRevision || before.Geometry[0].Material.NormalMap == next {
		t.Fatal("normal replacement invalidated geometry or mutated snapshot")
	}
}

func TestIndependentNormalUV(t *testing.T) {
	f := normalFrame(t)
	g := &f.Geometry[0]
	g.NormalUV = append([]UV(nil), g.UV...)
	for i := range g.NormalUV {
		g.NormalUV[i].U = 1 - g.NormalUV[i].U
	}
	check(t, g.Validate())
	if c := at(softwareRender(f), 40, 40); c.R > 3 {
		t.Fatal("independent normal UV ignored", c)
	}
	clone := cloneGeometry(*g)
	clone.NormalUV[0].U = 7
	if g.NormalUV[0].U == 7 {
		t.Fatal("normal UV slice aliased")
	}
	var b nt3dmath.AABB
	b.Include(nt3dmath.Vec3{X: -.3, Y: -.3, Z: 0})
	b.Include(nt3dmath.Vec3{X: .3, Y: .3, Z: 1})
	clipped, err := Clip(*g, b)
	check(t, err)
	for i, uv := range clipped.NormalUV {
		if abs(uv.U+clipped.UV[i].U-1) > 1e-5 {
			t.Fatal("clipped normal UV not interpolated")
		}
	}
	smooth, err := SmoothNormals(*g, 1)
	check(t, err)
	if len(smooth.NormalUV) != len(smooth.Vertices) {
		t.Fatal("smoothing lost normal UV")
	}
	merged, err := MergeGeometry(*g, *g)
	check(t, err)
	if len(merged.NormalUV) != 2*len(g.NormalUV) {
		t.Fatal("merge lost normal UV")
	}
}
