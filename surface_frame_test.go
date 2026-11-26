// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image"
	"math"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

func opaqueImage(side int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, side, side))
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	return img
}

// A flat Gaussian patch is the same shape whatever the scene's unit: every
// splat's axes and radius follow the patch's size, with no length built in.
func TestFlatGaussiansScaleWithTheScene(t *testing.T) {
	build := func(scale float32) []Point {
		pts, err := BuildFlatGaussians(FlatGaussians{
			Image:  opaqueImage(4),
			Normal: nt3dmath.Vec3{Y: -1},
			Up:     nt3dmath.Vec3{Z: 1},
			Width:  2 * scale,
			Height: 1 * scale,
		})
		check(t, err)
		if len(pts) != 16 {
			t.Fatalf("%d splats at scale %v, want 16", len(pts), scale)
		}
		return pts
	}
	unit := build(1)
	for _, scale := range []float32{1e-3, 1e3} {
		for i, p := range build(scale) {
			for k, axis := range p.GaussianAxes {
				want := unit[i].GaussianAxes[k].Scale(scale)
				if d := axis.Sub(want); math.Sqrt(float64(d.Dot(d))) > 1e-5*float64(scale) {
					t.Fatalf("at scale %v splat %d axis %d is %v, want %v", scale, i, k, axis, want)
				}
			}
			if want := unit[i].Radius * scale; abs(p.Radius-want) > 1e-5*scale {
				t.Fatalf("at scale %v splat %d radius is %v, want %v", scale, i, p.Radius, want)
			}
		}
	}
}

// Thickness is the splats' sigma along the normal; zero means an eighth of
// the smaller grid cell, and a value that cannot be a length is an error.
func TestFlatGaussiansThickness(t *testing.T) {
	f := FlatGaussians{Image: opaqueImage(4), Normal: nt3dmath.Vec3{Y: -1}, Up: nt3dmath.Vec3{Z: 1}, Width: 2, Height: 1}
	along := func(p Point) float32 { return abs(p.GaussianAxes[2].Y) }
	pts, err := BuildFlatGaussians(f)
	check(t, err)
	if got, want := along(pts[0]), float32(.25/8); abs(got-want) > 1e-6 {
		t.Fatalf("default thickness %v, want an eighth of the 0.25 cell, %v", got, want)
	}
	f.Thickness = .5
	pts, err = BuildFlatGaussians(f)
	check(t, err)
	if got := along(pts[0]); got != .5 {
		t.Fatalf("thickness %v, want 0.5", got)
	}
	for _, bad := range []float32{-1, float32(math.NaN()), float32(math.Inf(1))} {
		f.Thickness = bad
		if _, err := BuildFlatGaussians(f); err == nil {
			t.Fatalf("accepted thickness %v", bad)
		}
	}
}

// A mark that lands on nothing is an empty result, not an error, all the way
// to the cloud: a decal that misses its target, an image with no opaque
// texel, and a patch of no splats.
func TestNothingToPlaceIsEmptyNotAnError(t *testing.T) {
	wall, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 8, Y: 1, Z: 4}})
	check(t, err)
	decal, err := BuildDecal(wall, Decal{Center: nt3dmath.Vec3{X: 100, Y: -.5}, Normal: nt3dmath.Vec3{Y: -1}, Up: nt3dmath.Vec3{Z: 1}, Width: 1, Height: 1, Depth: 1})
	if err != nil || len(decal.Vertices) != 0 {
		t.Fatalf("a decal that misses: %d vertices, err %v", len(decal.Vertices), err)
	}

	clear := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	pts, err := BuildFlatGaussians(FlatGaussians{Image: clear, Normal: nt3dmath.Vec3{Y: -1}, Up: nt3dmath.Vec3{Z: 1}, Width: 1, Height: 1})
	if err != nil || len(pts) != 0 {
		t.Fatalf("a clear image: %d splats, err %v", len(pts), err)
	}

	cloud, err := NewGaussianCloud(Geometry{PointStyle: PointStyle{Shape: GaussianPoint, Units: WorldUnits, Blend: AlphaBlend}})
	check(t, err)
	id, err := cloud.Insert(pts)
	if err != nil || id == 0 || cloud.Count() != 0 {
		t.Fatalf("an empty patch: id %d, count %d, err %v", id, cloud.Count(), err)
	}
	if !cloud.Remove(id) {
		t.Fatal("the empty patch could not be removed")
	}
}

// Decal and FlatGaussians are placed by the same center, normal and up, and
// refuse the same wrong ones.
func TestDecalAndFlatGaussiansRefuseTheSameFrames(t *testing.T) {
	wall, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 8, Y: 1, Z: 4}})
	check(t, err)
	nan := float32(math.NaN())
	for name, c := range map[string]struct {
		center, normal, up nt3dmath.Vec3
		offset             float32
	}{
		"center not finite": {nt3dmath.Vec3{X: nan}, nt3dmath.Vec3{Y: -1}, nt3dmath.Vec3{Z: 1}, 0},
		"normal not finite": {nt3dmath.Vec3{}, nt3dmath.Vec3{Y: nan}, nt3dmath.Vec3{Z: 1}, 0},
		"normal zero":       {nt3dmath.Vec3{}, nt3dmath.Vec3{}, nt3dmath.Vec3{Z: 1}, 0},
		"up zero":           {nt3dmath.Vec3{}, nt3dmath.Vec3{Y: -1}, nt3dmath.Vec3{}, 0},
		"up along normal":   {nt3dmath.Vec3{}, nt3dmath.Vec3{Y: -1}, nt3dmath.Vec3{Y: 2}, 0},
		"offset negative":   {nt3dmath.Vec3{}, nt3dmath.Vec3{Y: -1}, nt3dmath.Vec3{Z: 1}, -1},
		"offset not finite": {nt3dmath.Vec3{}, nt3dmath.Vec3{Y: -1}, nt3dmath.Vec3{Z: 1}, nan},
	} {
		if _, err := BuildDecal(wall, Decal{Center: c.center, Normal: c.normal, Up: c.up, NormalOffset: c.offset, Width: 1, Height: 1, Depth: 1}); err == nil {
			t.Errorf("BuildDecal accepted %s", name)
		}
		if _, err := BuildFlatGaussians(FlatGaussians{Image: opaqueImage(2), Center: c.center, Normal: c.normal, Up: c.up, NormalOffset: c.offset, Width: 1, Height: 1}); err == nil {
			t.Errorf("BuildFlatGaussians accepted %s", name)
		}
	}
	// A normal and up of any length place the same decal.
	a, err := BuildDecal(wall, Decal{Center: nt3dmath.Vec3{Y: -.5}, Normal: nt3dmath.Vec3{Y: -1}, Up: nt3dmath.Vec3{Z: 1}, Width: 1, Height: 1, Depth: 1})
	check(t, err)
	b, err := BuildDecal(wall, Decal{Center: nt3dmath.Vec3{Y: -.5}, Normal: nt3dmath.Vec3{Y: -7}, Up: nt3dmath.Vec3{Z: 3}, Width: 1, Height: 1, Depth: 1})
	check(t, err)
	if len(a.Vertices) == 0 || len(a.Vertices) != len(b.Vertices) || a.Bounds != b.Bounds {
		t.Fatalf("a longer normal and up changed the decal: %d and %d vertices", len(a.Vertices), len(b.Vertices))
	}
}

// A decal's target is checked as any geometry is: a vertex that is not
// finite, or an index out of range, is an error and not a decal of NaNs.
func TestBuildDecalRefusesAWrongTarget(t *testing.T) {
	wall, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 8, Y: 1, Z: 4}})
	check(t, err)
	d := Decal{Center: nt3dmath.Vec3{Y: -.5}, Normal: nt3dmath.Vec3{Y: -1}, Up: nt3dmath.Vec3{Z: 1}, Width: 1, Height: 1, Depth: 1}
	if _, err := BuildDecal(wall, d); err != nil {
		t.Fatal(err)
	}
	notFinite := wall.Clone()
	notFinite.Vertices[0].Position.X = float32(math.NaN())
	if _, err := BuildDecal(notFinite, d); err == nil {
		t.Error("accepted a target with a vertex that is not finite")
	}
	badIndex := wall.Clone()
	badIndex.Indices[0] = uint32(len(badIndex.Vertices))
	if _, err := BuildDecal(badIndex, d); err == nil {
		t.Error("accepted a target with an index out of range")
	}
}
