// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image"
	"image/color"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"testing"
)

// Exercise composition through the renderer, including its prepass and resolve.
func testSSAOLighting(t *testing.T, render func(Frame) (image.Image, error)) {
	white := color.RGBA{255, 255, 255, 255}
	ambient := Lighting{Ambient: Ambient{Sky: white, Ground: white, Intensity: .6}}
	ormImage := image.NewRGBA(image.Rect(0, 0, 1, 1))
	ormImage.SetRGBA(0, 0, color.RGBA{64, 255, 0, 255})
	ormMap, err := NewORMMap(ormImage)
	check(t, err)
	direct := Lighting{Lights: []Light{{Direction: nt3dmath.Vec3{Y: -1, Z: 1}, Intensity: .6, Shadows: true}}}
	for _, tc := range []struct {
		name     string
		material Material
		light    Lighting
		darkens  bool
	}{
		{"classic ambient", Material{}, ambient, true},
		{"PBR ambient", Material{Shading: ShadePBR, PBR: PBRStyle{Roughness: .7}}, ambient, true},
		{"toon ambient", Material{Toon: ToonStyle{Bands: 3}}, ambient, true},
		{"classic direct", Material{Phong: PhongStyle{Specular: .3}}, direct, false},
		{"PBR direct", Material{Shading: ShadePBR, PBR: PBRStyle{Roughness: .7, Clearcoat: ClearcoatStyle{Amount: .5}}}, direct, false},
		{"toon direct", Material{Toon: ToonStyle{Bands: 3}}, direct, false},
		{"emissive", Material{Shading: ShadePBR, Emission: EmissionStyle{Color: color.RGBA{100, 140, 180, 255}}}, Lighting{}, false},
		{"unlit", Material{Shading: ShadeUnlit}, ambient, false},
		{"material AO", Material{Shading: ShadePBR, PBR: PBRStyle{Roughness: .7, ORMMap: ormMap}}, ambient, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := cornerSSAOFrame(t)
			f.Complete = true
			f.Lighting = tc.light
			for i := range f.Draws {
				f.Draws[i].Geometry.Material = &tc.material
				if tc.material.PBR.ORMMap != nil {
					f.Draws[i].Geometry.UV = make([]UV, len(f.Draws[i].Geometry.Vertices))
				}
			}
			before, err := render(f)
			check(t, err)
			f.PostProcess.SSAO = SSAO{Strength: .9, Radius: .6}
			if tc.name == "material AO" {
				f.PostProcess.SSAO.Strength = .1
			}
			after, err := render(f)
			check(t, err)
			a, b := rgbaAt(before, 48, 48), rgbaAt(after, 48, 48)
			if tc.darkens {
				if int(a.R)-int(b.R) < 10 {
					t.Fatalf("indirect crease did not darken: %v -> %v", a, b)
				}
			} else {
				for y := 8; y < 88; y++ {
					for x := 8; x < 88; x++ {
						a, b := rgbaAt(before, x, y), rgbaAt(after, x, y)
						for _, d := range []int{int(a.R) - int(b.R), int(a.G) - int(b.G), int(a.B) - int(b.B)} {
							if d < -1 || d > 1 {
								t.Fatalf("AO changed protected light at %d,%d: %v -> %v", x, y, a, b)
							}
						}
					}
				}
			}
			// Replaying an older frame must not retain the AO binding.
			f.PostProcess = PostProcess{}
			replay, err := render(f)
			check(t, err)
			if rgbaAt(replay, 48, 48) != rgbaAt(before, 48, 48) {
				t.Fatal("AO persisted after disabling")
			}
		})
	}
}
func TestSoftwareSSAOLighting(t *testing.T) {
	testSSAOLighting(t, func(f Frame) (image.Image, error) { return softwareRender(f), nil })
}

func TestSSAOMaterialOcclusionMinimum(t *testing.T) {
	f := cornerSSAOFrame(t)
	rig := newRig(&f)
	ormImage := image.NewRGBA(image.Rect(0, 0, 1, 1))
	ormImage.SetRGBA(0, 0, color.RGBA{64, 255, 255, 255})
	ormMap, err := NewORMMap(ormImage)
	check(t, err)
	m := Material{Shading: ShadePBR, PBR: PBRStyle{Roughness: .6, OcclusionStrength: 1, ORMMap: ormMap}}
	n := nt3dmath.Vec3{Z: 1}
	base := [3]float32{.3, .4, .5}
	orm := [3]float32{.25, 1, 1}
	a, b := pbrShade(base, &rig, &f, &m, orm, n, nt3dmath.Vec3{}, n, n)
	rig.screenOcclusion = .5
	c, d := pbrShade(base, &rig, &f, &m, orm, n, nt3dmath.Vec3{}, n, n)
	if a != c || b != d {
		t.Fatal("SSAO compounded stronger material AO")
	}
	rig.screenOcclusion = .9
	c, d = pbrShade(base, &rig, &f, &m, orm, n, nt3dmath.Vec3{}, n, n)
	if c[0] >= a[0] || d[0] >= b[0] {
		t.Fatal("stronger SSAO did not attenuate indirect light")
	}
}
