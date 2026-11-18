//go:build webgpu

package nt3d

import (
	"fmt"
	"image"
	"image/color"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"testing"
)

func TestGPUReflections(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	env := testEnvironment(t)
	alt := GradientEnvironment(color.RGBA{230, 60, 30, 255}, color.RGBA{20, 220, 60, 255}, color.RGBA{30, 40, 240, 255})
	for _, linear := range []bool{false, true} {
		for _, background := range []bool{false, true} {
			for _, perspective := range []bool{false, true} {
				for _, e := range []*Environment{env, alt, nil, env} {
					f := normalFrame(t)
					f.Revision++
					f.Environment = e
					f.Linear = linear
					f.EnvironmentBackgroundOnly = background
					f.EnvironmentStrength = .7
					f.Forward = nt3dmath.Vec3{Z: -1}
					f.Eye = nt3dmath.Vec3{Z: 3}
					f.Perspective = perspective
					f.Draws[0].Geometry.Material.Phong.Reflectivity = .3
					got, err := r.Render(f)
					check(t, err)
					want := softwareRender(f)
					for y := 28; y < 52; y += 5 {
						for x := 28; x < 52; x += 5 {
							if !closeRGB(at(got, x, y), at(want, x, y), 4) {
								t.Fatalf("reflection linear %v bg %v persp %v: %v != %v", linear, background, perspective, at(got, x, y), at(want, x, y))
							}
						}
					}
				}
			}
		}
	}
}

func TestGPUShaderNormalMaps(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	const source = `fn nt3dTexture(uv:vec2<f32>)->vec4<f32>{return vec4<f32>(nt3dTextureParams.x,uv.x,1.0,1.0);}`
	var saved Frame
	revision := uint64(10)
	for i, phase := range []float32{.3, .8, .5} {
		img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
		for y := range 32 {
			for x := range 32 {
				img.SetNRGBA(x, y, color.NRGBA{uint8(phase*255 + .5), uint8((float32(x)+.5)/32*255 + .5), 255, 255})
			}
		}
		tex, err := NewShaderNormalMapWithParameters(source, [4]float32{phase}, img)
		check(t, err)
		f := normalFrame(t)
		f.Revision = uint64(i + 1)
		f.Draws[0].Geometry.Material.Normal.Map = tex
		for j := range f.Draws[0].Geometry.UV {
			f.Draws[0].Geometry.UV[j].U = f.Draws[0].Geometry.UV[j].U*3 - 1
		}
		f.Environment = testEnvironment(t)
		f.Draws[0].Geometry.Material.Phong.Reflectivity = .2
		f.Forward = nt3dmath.Vec3{Z: -1}
		if i == 0 {
			saved = f
		}
		for _, repeat := range []bool{false, true} {
			// Independent snapshots avoid mutating saved frame materials.
			f.Draws = append([]Draw(nil), f.Draws...)
			m := *f.Draws[0].Geometry.Material
			f.Draws[0].Geometry.Material = &m
			m.Normal.Repeat = repeat
			revision++
			f.Revision = revision
			got, err := r.Render(f)
			check(t, err)
			want := softwareRender(f)
			for x := 26; x < 54; x += 4 {
				if !closeRGB(at(got, x, 40), at(want, x, 40), 5) {
					t.Fatal(fmt.Sprint("procedural normal mismatch ", i, " ", repeat, ": ", at(got, x, 40), " != ", at(want, x, 40)))
				}
			}
		}
	}
	got, err := r.Render(saved)
	check(t, err)
	if !closeRGB(at(got, 40, 40), at(softwareRender(saved), 40, 40), 5) {
		t.Fatal("old normal snapshot changed")
	}
}

// The GPU agrees with software about a background-only environment: it is not
// reflected, by classic or PBR materials, in either working space.
func TestGPUBackgroundOnlyEnvironmentIsNotReflected(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	env := GradientEnvironment(color.RGBA{240, 70, 35, 255}, color.RGBA{200, 180, 60, 255}, color.RGBA{35, 70, 235, 255})
	var rev uint64 = 100
	for _, linear := range []bool{false, true} {
		for _, pbr := range []bool{false, true} {
			rev++
			f := normalFrame(t)
			f.Revision = rev
			f.Linear = linear
			f.Environment = env
			f.EnvironmentBackgroundOnly = true
			f.EnvironmentStrength = .85
			f.Forward = nt3dmath.Vec3{Z: -1}
			f.Eye = nt3dmath.Vec3{Z: 3}
			f.Perspective = true
			if pbr {
				f.Draws[0].Geometry.Material = &Material{Shading: ShadePBR, PBR: PBRStyle{Metallic: .9, Roughness: .1}}
			} else {
				f.Draws[0].Geometry.Material.Phong.Reflectivity = .6
			}
			got, err := r.Render(f)
			check(t, err)
			want := softwareRender(f)
			for y := 28; y < 52; y += 6 {
				for x := 28; x < 52; x += 6 {
					if !closeRGB(at(got, x, y), at(want, x, y), 5) {
						t.Fatalf("linear=%v pbr=%v (%d,%d): gpu=%v sw=%v", linear, pbr, x, y, at(got, x, y), at(want, x, y))
					}
				}
			}
		}
	}
}
