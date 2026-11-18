// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func crossedGlassQuads(t *testing.T) (Geometry, Geometry) {
	t.Helper()
	mat := &Material{Shading: ShadeUnlit, Alpha: AlphaTransparent}
	// quad(half, z, ...) puts vertices in XY plane around z; shift to z=0 first, then tilt around Y
	// around (0, 0, -2.5) so both intersect along x=0 at centroid (0, 0, -2.5).
	base := quad(0.75, 0, white, mat, false)

	redBase := base
	redBase.Vertices = append([]Vertex(nil), base.Vertices...)
	for i := range redBase.Vertices {
		redBase.Vertices[i].Color = color.RGBA{240, 45, 45, 165}
	}
	red, err := TransformGeometry(redBase, nt3dmath.Translation(nt3dmath.Vec3{Z: -2.5}).Mul(testRotationY(0.55)))
	check(t, err)

	blueBase := base
	blueBase.Vertices = append([]Vertex(nil), base.Vertices...)
	for i := range blueBase.Vertices {
		blueBase.Vertices[i].Color = color.RGBA{45, 95, 245, 165}
	}
	blue, err := TransformGeometry(blueBase, nt3dmath.Translation(nt3dmath.Vec3{Z: -2.5}).Mul(testRotationY(-0.55)))
	check(t, err)

	return red, blue
}

func TestOITIntersectingOrderInvarianceAndDepthWeighting(t *testing.T) {
	red, blue := crossedGlassQuads(t)

	// Both crossed quads share centroid (0, 0, -2.5), so sorted-slice transparency (OIT=false)
	// depends on submission order, whereas WBOIT (OIT=true) is strictly order-invariant!
	makeFrame := func(oit bool, geoms ...Geometry) Frame {
		f := depthFrame(80, 80, geoms...)
		f.Linear = true
		f.Background = color.RGBA{24, 26, 32, 255}
		f.OIT = oit
		return f
	}

	sortedRB := softwareRender(makeFrame(false, red, blue)).(*image.RGBA)
	sortedBR := softwareRender(makeFrame(false, blue, red)).(*image.RGBA)
	if bytes.Equal(sortedRB.Pix, sortedBR.Pix) {
		t.Fatal("expected non-OIT intersecting quads with identical centroids to depend on submission order")
	}

	oitRB := softwareRender(makeFrame(true, red, blue)).(*image.RGBA)
	oitBR := softwareRender(makeFrame(true, blue, red)).(*image.RGBA)
	if !bytes.Equal(oitRB.Pix, oitBR.Pix) {
		t.Fatal("OIT software render changed when intersecting mesh order was reversed")
	}

	// testRotationY(+0.55): for x < 0, z = -x*sin(0.55) - 2.5 > -2.5 (closer to camera -> smaller clip z -> higher weight);
	// for x > 0, blue (testRotationY(-0.55)) has z = +x*sin(0.55) - 2.5 > -2.5 (closer to camera)!
	left := at(oitRB, 24, 40)
	right := at(oitRB, 56, 40)
	if !(left.R > right.R && right.B > left.B) {
		t.Fatalf("expected depth-weighted OIT asymmetry across X-intersection: left=%v right=%v", left, right)
	}
}

func TestOITOpaqueOcclusionPointsTexturedOutlineAndWireframe(t *testing.T) {
	tex := testTexture(t, color.RGBA{50, 185, 100, 200})
	glassMat := &Material{
		Shading: ShadeUnlit,
		Alpha:   AlphaTransparent,
		Texture: tex,
	}
	glassQuad := quad(0.7, -2.5, color.RGBA{255, 255, 255, 200}, glassMat, false)
	glassQuad.UV = []UV{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	glassQuad.Outline = OutlineStyle{Width: 2, Color: color.RGBA{255, 220, 60, 255}}

	// Opaque blocker in front (z = -1.5) covering left half; behind-blocker glass must be depth-rejected.
	wallBase := quad(0.4, -1.5, color.RGBA{200, 40, 40, 255}, &Material{Shading: ShadeUnlit}, false)
	wall, err := TransformGeometry(wallBase, nt3dmath.Translation(nt3dmath.Vec3{X: -0.35}))
	check(t, err)

	// Alpha point cloud stays in planBlend slots when OIT=true.
	cloud := styled(
		PointStyle{Shape: SoftPoint, Units: PixelUnits, Blend: AlphaBlend},
		Point{Position: nt3dmath.Vec3{X: 0.35, Z: -3.2}, Radius: 6, Color: color.RGBA{255, 255, 255, 200}},
	)

	f := depthFrame(64, 64, wall, glassQuad, cloud)
	f.OIT = true
	f.Linear = true
	f.Background = color.RGBA{10, 12, 16, 255}

	plan, err := f.planBlend(f.drawn(1))
	check(t, err)
	if len(plan.slots) != 1 || plan.slots[0] != 2 {
		t.Fatalf("expected alpha point cloud to remain in blend slots under OIT, got %v", plan.slots)
	}
	for s, list := range plan.bySlice {
		if len(list) != 0 {
			t.Fatalf("expected OIT transparent mesh to be omitted from slice %d, got %v", s, list)
		}
	}

	img := softwareRender(f)
	wallPx := at(img, 20, 32)
	if wallPx.R < 180 || wallPx.G > 60 {
		t.Fatalf("opaque wall was contaminated by occluded OIT glass: %v", wallPx)
	}
	glassPx := at(img, 46, 20)
	if glassPx.G < 80 {
		t.Fatalf("expected visible textured OIT glass on unoccluded side, got %v", glassPx)
	}

	if _, err := RenderWireframe(f, 20, 10); err != nil {
		t.Fatalf("RenderWireframe failed with OIT=true: %v", err)
	}
}
