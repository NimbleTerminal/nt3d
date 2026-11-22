// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image"
	"image/color"
	"testing"
)

// Splitting a square along its diagonal gives each pixel center to exactly
// one triangle, including when the projected corners are wound in reverse.
func TestScreenTrianglesOwnSharedEdgeInEitherWinding(t *testing.T) {
	corners := [4]clipCorner{{x: -1, y: 1, w: 1}, {x: 1, y: 1, w: 1}, {x: 1, y: -1, w: 1}, {x: -1, y: -1, w: 1}}
	for _, reverse := range []bool{false, true} {
		if reverse {
			corners[1], corners[3] = corners[3], corners[1]
		}
		first := projectTriangle(&corners[0], &corners[1], &corners[2], 8, 8)
		second := projectTriangle(&corners[0], &corners[2], &corners[3], 8, 8)
		for y := range 8 {
			for x := range 8 {
				px, py := float32(x)+.5, float32(y)+.5
				_, a := first.coverage(px, py)
				_, b := second.coverage(px, py)
				// In screen coordinates the upper-right triangle owns
				// the diagonal, which is its left edge.
				wantFirst := x >= y
				if reverse {
					wantFirst = !wantFirst
				}
				if a != wantFirst || b == wantFirst {
					t.Fatalf("pixel (%d, %d), reversed=%t: covered by first=%t second=%t", x, y, reverse, a, b)
				}
			}
		}
	}
}

// A hook can keep a transparent texel and discard an opaque one. Both the
// color pass and the AO depth prepass use the hook's final alpha for cutout.
func TestTexturedCutoutHookControlsDepthInBothPasses(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(1, 0, color.NRGBA{A: 255})
	texture, err := NewTexture(img)
	check(t, err)
	hook, err := NewMaterialShader(`fn nt3dMaterial(in: NT3DMaterialInput) -> vec4<f32> {
		return vec4<f32>(1.0, 0.0, 0.0, 1.0 - in.color.a);
	}`, func(in MaterialColorInput) [4]float32 {
		return [4]float32{1, 0, 0, 1 - in.Color[3]}
	})
	check(t, err)
	g := quad(1, .25, white, &Material{Shading: ShadeUnlit, Texture: texture, FullSize: true, Shader: hook, Alpha: AlphaCutout}, false)
	g.UV = []UV{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	f := flatFrame(8, 8, g)
	for _, prepass := range []bool{false, true} {
		colorImage, depth, _ := softwareDrawWithAO(f, 1, make([]float32, 64), false, nil, prepass)
		if left, right := depth[4*8+1], depth[4*8+6]; left != .25 || right != noDepth {
			t.Fatalf("prepass=%t: left and right depth = %v, %v", prepass, left, right)
		}
		if !prepass && (colorImage.RGBAAt(1, 4) != (color.RGBA{R: 255, A: 255}) || colorImage.RGBAAt(6, 4) != f.Background) {
			t.Fatalf("left and right color = %v, %v", colorImage.RGBAAt(1, 4), colorImage.RGBAAt(6, 4))
		}
	}
}
