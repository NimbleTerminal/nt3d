// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image/color"
	"math"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// (white is lighting_test.go's: opaque white.)

// styled is one geometry of points in a style.
func styled(style PointStyle, points ...Point) Geometry {
	return Geometry{PointStyle: style, Points: points}
}

// A soft point is full at its center and fades to the floor at its edge;
// a hard one is even to its edge.
func TestSoftPointFalloff(t *testing.T) {
	for _, shape := range []PointShape{HardPoint, SoftPoint} {
		img := softwareRender(blendScene(t, nil, styled(PointStyle{Shape: shape, Blend: AlphaBlend}, Point{Radius: 12, Color: white})))
		center, edge := at(img, 40, 40), at(img, 50, 40) // About .06 and .9 of the radius out.
		if shape == HardPoint && (center != white || edge != white) {
			t.Errorf("hard: center %v, edge %v", center, edge)
		}
		if shape == SoftPoint && (center.R < 240 || edge.R > 110 || edge.B < 150) {
			t.Errorf("soft: center %v, edge %v", center, edge)
		}
	}
}

// A world-unit point is larger nearer the camera; a pixel-unit point is the
// same size at both distances.
func TestWorldUnitPointSize(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	camera := DefaultCamera()
	camera.Projection = Perspective
	eye, _, _, _ := camera.basis()
	size := func(units PointUnits, radius float32, p nt3dmath.Vec3) int {
		s := NewScene()
		_, err := s.SetGeometry("p", styled(PointStyle{Units: units}, Point{Position: p, Radius: radius, Color: red}))
		check(t, err)
		f, err := s.Frame(camera, 128, 128)
		check(t, err)
		return coloredPixels(softwareRender(f), red)
	}
	near, far := eye.Scale(.4), eye.Scale(-.4) // Both on the view's axis.
	if n, f := size(WorldUnits, .1, near), size(WorldUnits, .1, far); n < f*3/2 || f == 0 {
		t.Errorf("world units: %d pixels near, %d far", n, f)
	}
	if n, f := size(PixelUnits, 6, near), size(PixelUnits, 6, far); n != f || n == 0 {
		t.Errorf("pixel units: %d pixels near, %d far", n, f)
	}
}

// A world-unit point is about as large on screen supersampled as not: its
// coverage, summed in linear light, is about the same.
func TestWorldUnitPointSupersampled(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	coverage := func(supersample int) float32 {
		f := flatFrame(128, 128, styled(PointStyle{Units: WorldUnits}, Point{Position: nt3dmath.Vec3{Z: .5}, Radius: .1, Color: red}))
		f.Supersample = supersample
		img, sum := softwareRender(f), float32(0)
		for y := range 128 {
			for x := range 128 {
				sum += srgbDecode[at(img, x, y).R]
			}
		}
		return sum
	}
	// A radius of 6.4 pixels covers about 129.
	area := float32(math.Pi * 6.4 * 6.4)
	for _, supersample := range []int{1, 2} {
		if got := coverage(supersample); got < .94*area || got > 1.06*area {
			t.Errorf("supersampled %d×: %.1f pixels covered, want about %.1f", supersample, got, area)
		}
	}
}

// A world-unit point behind the camera draws nothing.
func TestWorldUnitPointBehindCamera(t *testing.T) {
	camera := DefaultCamera()
	camera.Projection = Perspective
	eye, _, _, _ := camera.basis()
	for _, blend := range []PointBlend{OpaqueBlend, AlphaBlend, AdditiveBlend} {
		s := NewScene()
		_, err := s.SetGeometry("p", styled(PointStyle{Units: WorldUnits, Blend: blend}, Point{Position: eye.Scale(1.5), Radius: 1, Color: white}))
		check(t, err)
		f, err := s.Frame(camera, 64, 64)
		check(t, err)
		if n := coloredPixels(softwareRender(f), f.Background); n != 64*64 {
			t.Errorf("blend %d: %d of %d pixels background", blend, n, 64*64)
		}
	}
}

// A half-transparent point mixes with the floor behind it, is hidden by a
// nearer opaque pane, and lets an opaque point behind it show through.
func TestAlphaPoint(t *testing.T) {
	red, green, yellow := color.RGBA{255, 0, 0, 128}, color.RGBA{0, 200, 0, 255}, color.RGBA{255, 255, 0, 255}
	alpha := PointStyle{Blend: AlphaBlend}
	point := styled(alpha, Point{Radius: 8, Color: red})
	if got, want := at(softwareRender(blendScene(t, nil, point)), 40, 40), mixRGB(red, 128./255, blue); !closeRGB(got, want, 2) {
		t.Errorf("over the floor: %v, want %v", got, want)
	}
	if got := at(softwareRender(blendScene(t, nil, point, quad(1, .3, green, nil, false))), 40, 40); !closeRGB(got, green, 2) {
		t.Errorf("under a pane: %v, want %v", got, green)
	}
	below := styled(PointStyle{}, Point{Position: nt3dmath.Vec3{Z: -.5}, Radius: 8, Color: yellow})
	above := styled(alpha, Point{Position: nt3dmath.Vec3{Z: .3}, Radius: 8, Color: red})
	if got, want := at(softwareRender(blendScene(t, nil, above, below)), 40, 40), mixRGB(red, 128./255, yellow); !closeRGB(got, want, 2) {
		t.Errorf("over an opaque point: %v, want %v", got, want)
	}
}

// Additive points add up, in either order.
func TestAdditivePoints(t *testing.T) {
	add := PointStyle{Blend: AdditiveBlend}
	a := Point{Radius: 8, Color: color.RGBA{120, 40, 0, 255}}
	b := Point{Position: nt3dmath.Vec3{Z: .2}, Radius: 8, Color: color.RGBA{0, 40, 120, 255}}
	pixel := func(points ...Point) color.RGBA {
		return at(softwareRender(blendScene(t, nil, styled(add, points...))), 40, 40)
	}
	one, two, swapped := pixel(a), pixel(a, b), pixel(b, a)
	if two.G <= one.G || two.B <= one.B || two.R < one.R {
		t.Errorf("one point %v, two %v", one, two)
	}
	if !closeRGB(two, swapped, 1) {
		t.Errorf("order changed the sum: %v, swapped %v", two, swapped)
	}
}

// Alpha points sort within their geometry: the nearer shows on top,
// whichever is given first.
func TestAlphaPointOrder(t *testing.T) {
	near := Point{Position: nt3dmath.Vec3{Z: .4}, Radius: 8, Color: color.RGBA{255, 0, 0, 200}}
	far := Point{Radius: 8, Color: color.RGBA{0, 255, 0, 200}}
	for _, points := range [][]Point{{near, far}, {far, near}} {
		if got := at(softwareRender(blendScene(t, nil, styled(PointStyle{Blend: AlphaBlend}, points...))), 40, 40); got.R <= got.G {
			t.Errorf("given near first %v: %v, want mostly red", points[0] == near, got)
		}
	}
}

// Fog fades an additive point out, toward adding nothing, where it turns
// an alpha point toward the fog's color.
func TestAdditivePointFog(t *testing.T) {
	for _, blend := range []PointBlend{AdditiveBlend, AlphaBlend} {
		f := flatFrame(64, 64, styled(PointStyle{Blend: blend}, Point{Position: nt3dmath.Vec3{Z: .5}, Radius: 8, Color: white}))
		f.Forward = nt3dmath.Vec3{Z: 1} // Distance is z: fog .91 of the way.
		f.Fog = Fog{Near: 0, Far: .55, Color: color.RGBA{255, 0, 0, 255}}
		got := at(softwareRender(f), 32, 32)
		if blend == AdditiveBlend && (got.R > got.G+2 || got.G == 0 || got.G > 120) {
			t.Errorf("additive: %v, want a dim gray", got)
		}
		if blend == AlphaBlend && (got.R < 200 || got.G > 120) {
			t.Errorf("alpha: %v, want mostly the fog's red", got)
		}
	}
}

// Sampled interactive frames keep a cloud of alpha points about as bright
// as complete ones: each point drawn stands for those skipped.
func TestSampledPointsKeepBrightness(t *testing.T) {
	var points []Point
	for y := range 150 {
		for x := range 150 { // 22,500 points, three pixels apart.
			p := nt3dmath.Vec3{X: (float32(x*3)+1.5)/256 - 1, Y: (float32(y*3)+1.5)/256 - 1, Z: .5}
			points = append(points, Point{Position: p, Radius: 1, Color: color.RGBA{255, 255, 255, 64}})
		}
	}
	brightness := func(complete bool) float64 {
		f := flatFrame(512, 512, styled(PointStyle{Blend: AlphaBlend}, points...))
		f.Complete = complete
		img, sum := softwareRender(f), 0.0
		for y := range 512 {
			for x := range 512 {
				sum += float64(srgbDecode[at(img, x, y).R])
			}
		}
		return sum
	}
	if full, sampled := brightness(true), brightness(false); sampled < .9*full || sampled > 1.1*full {
		t.Fatalf("complete %.0f, sampled %.0f", full, sampled)
	}
}

// One geometry of a transparent pane and alpha points is one blend entry,
// pane then points: the same picture as the two as separate geometries.
func TestBlendedPointsWithPane(t *testing.T) {
	pane := quad(1, .3, color.RGBA{0, 220, 0, 128}, glass, false)
	points := []Point{{Position: nt3dmath.Vec3{Z: .5}, Radius: 8, Color: color.RGBA{255, 0, 0, 160}}}
	together := pane
	together.PointStyle, together.Points = PointStyle{Blend: AlphaBlend}, points
	one := at(softwareRender(blendScene(t, nil, together)), 40, 40)
	two := at(softwareRender(blendScene(t, nil, pane, styled(PointStyle{Blend: AlphaBlend}, points...))), 40, 40)
	if one != two || one.R == 0 || one.G == 0 {
		t.Fatalf("one geometry %v, two %v", one, two)
	}
}
