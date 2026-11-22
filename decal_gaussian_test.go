// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image"
	"image/color"
	"testing"

	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/sh"
)

// BuildDecal clips front-facing wall triangles to an oriented box, assigns
// planar [0,1]^2 UVs, lifts vertices along the normal, and ignores back faces.
func TestBuildDecalClipsWallAndSkipsBackfaces(t *testing.T) {
	box, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 10, Y: 2, Z: 6}})
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.SetNRGBA(x, y, color.NRGBA{R: 30, G: 230, B: 210, A: 255})
		}
	}
	tex, err := NewTexture(img)
	if err != nil {
		t.Fatal(err)
	}
	mat := &Material{
		Shading: ShadeUnlit,
		Alpha:   AlphaTransparent,
		Texture: tex,
	}
	decal, err := BuildDecal(box, Decal{
		Center:       nt3dmath.Vec3{X: 0, Y: -1, Z: 0},
		Normal:       nt3dmath.Vec3{Y: -1},
		Up:           nt3dmath.Vec3{Z: 1},
		Width:        3,
		Height:       2,
		Depth:        0.5,
		NormalOffset: 0.02,
		Material:     mat,
	})
	if err != nil {
		t.Fatalf("BuildDecal: %v", err)
	}
	if err := decal.Validate(); err != nil {
		t.Fatalf("decal Validate: %v", err)
	}
	if len(decal.Vertices) == 0 || len(decal.Indices) == 0 {
		t.Fatalf("expected clipped decal triangles, got %d verts, %d indices", len(decal.Vertices), len(decal.Indices))
	}
	for i, v := range decal.Vertices {
		if v.Position.X < -1.501 || v.Position.X > 1.501 {
			t.Fatalf("vertex %d X=%v outside clipped [-1.5, 1.5]", i, v.Position.X)
		}
		if v.Position.Z < -1.001 || v.Position.Z > 1.001 {
			t.Fatalf("vertex %d Z=%v outside clipped [-1.0, 1.0]", i, v.Position.Z)
		}
		if v.Position.Y > -1.019 || v.Position.Y < -1.021 {
			t.Fatalf("vertex %d Y=%v, want lifted -1.02", i, v.Position.Y)
		}
		uv := decal.UV[i]
		if uv.U < -1e-4 || uv.U > 1+1e-4 || uv.V < -1e-4 || uv.V > 1+1e-4 {
			t.Fatalf("vertex %d UV=%+v outside [0, 1]^2", i, uv)
		}
	}
}

// GaussianCloud inserts and removes flat surface Gaussians into an SH-palette
// Gaussian cloud without mutating the original palette, and renders them in
// front of wall splats.
func TestGaussianCloudInsertRemoveWithSHPalette(t *testing.T) {
	origCoeffs := []nt3dsh.RGB{{0.2, -0.1, 0.05}, {0.1, 0.2, -0.1}, {-0.05, 0.1, 0.15}}
	pal := &SHPalette{
		Degree:       1,
		Coefficients: append([]nt3dsh.RGB(nil), origCoeffs...),
	}
	wall := Geometry{
		PointStyle: PointStyle{
			Shape:     GaussianPoint,
			Units:     WorldUnits,
			Blend:     AlphaBlend,
			ToonBands: 4,
		},
		Points: []Point{{
			Position: nt3dmath.Vec3{X: 0, Y: 0, Z: 0},
			Radius:   1.8,
			Color:    color.RGBA{R: 120, G: 110, B: 100, A: 255},
			GaussianAxes: [3]nt3dmath.Vec3{
				{X: 0.6},
				{Z: 0.6},
				{Y: 0.02},
			},
		}},
		GaussianSH:        []nt3dsh.RGB{{-0.1, -0.15, -0.2}},
		GaussianSHPalette: pal,
		GaussianSHLabels:  []uint16{0},
	}
	cloud, err := NewGaussianCloud(wall)
	if err != nil {
		t.Fatalf("NewGaussianCloud: %v", err)
	}
	if len(pal.Coefficients) != len(origCoeffs) {
		t.Fatalf("caller SHPalette was mutated: len=%d, want %d", len(pal.Coefficients), len(origCoeffs))
	}

	tagImg := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			tagImg.SetNRGBA(x, y, color.NRGBA{R: 250, G: 40, B: 160, A: 255})
		}
	}
	patchPts, err := BuildFlatGaussians(FlatGaussians{
		Image:        tagImg,
		Center:       nt3dmath.Vec3{X: 0, Y: 0, Z: 0},
		Normal:       nt3dmath.Vec3{Y: -1},
		Up:           nt3dmath.Vec3{Z: 1},
		Width:        1.0,
		Height:       1.0,
		NormalOffset: 0.05,
		Columns:      8,
		Rows:         8,
	})
	if err != nil {
		t.Fatalf("BuildFlatGaussians: %v", err)
	}
	patchID, err := cloud.Insert(patchPts)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	merged := cloud.Geometry()
	if err := merged.Validate(); err != nil {
		t.Fatalf("merged Validate: %v", err)
	}
	if got, want := len(merged.Points), 1+len(patchPts); got != want {
		t.Fatalf("merged points = %d, want %d", got, want)
	}

	scene := NewScene()
	if _, err := scene.SetGeometry("cloud", merged); err != nil {
		t.Fatalf("SetGeometry: %v", err)
	}
	cam := DefaultCamera().LookAt(nt3dmath.Vec3{Y: -2.2}, nt3dmath.Vec3{})
	cam.Projection = Perspective
	frame, err := scene.StillFrame(cam, 64, 64)
	if err != nil {
		t.Fatalf("StillFrame: %v", err)
	}
	sw := NewSoftwareRenderer()
	defer sw.Close()
	rendered, err := sw.Render(frame)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	center := color.RGBAModel.Convert(rendered.At(32, 32)).(color.RGBA)
	if int(center.R) <= int(center.G)+60 {
		t.Fatalf("expected magenta tag Gaussian in front of wall at center pixel, got %+v", center)
	}

	if !cloud.Remove(patchID) {
		t.Fatalf("Remove(%d) returned false", patchID)
	}
	restored := cloud.Geometry()
	if len(restored.Points) != 1 {
		t.Fatalf("after Remove, points = %d, want 1", len(restored.Points))
	}
}
