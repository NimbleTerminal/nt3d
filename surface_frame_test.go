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
