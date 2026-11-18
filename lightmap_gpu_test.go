// Copyright (c) 2026 Neomantra Corp.

//go:build webgpu

package nt3d

import (
	"bytes"
	"image/color"
	"testing"
)

func TestGPULightMap(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	var revision uint64
	pix := make([]float32, 8*8*3)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			i := (y*8 + x) * 3
			pix[i] = float32(x) * .7
			pix[i+1] = float32(y) * .5
			pix[i+2] = .2
		}
	}
	lm, err := NewLightMap(8, 8, pix)
	check(t, err)
	black := testLightMap(t, [3]float32{})
	for _, mat := range []Material{
		{}, {PBR: true, Roughness: .5}, {PBR: true, Metallic: 1, Roughness: .5}, {Unlit: true},
		{PBR: true, Roughness: .5, Clearcoat: 1, NormalMap: testNormalMap(t, color.NRGBA{150, 128, 250, 255}), NormalStrength: 1, ORMMap: testORMMap(t, color.NRGBA{128, 128, 0, 255}), EmissiveMap: whiteMap, Emissive: color.RGBA{20, 40, 60, 255}, EmissiveIntensity: .2},
		{Transparent: true}, {Transmission: .5},
	} {
		for _, linear := range []bool{true, false} {
			r.Close()
			r = &gpuRenderer{}
			mat.LightMap = lm
			f := lightMapFrame(&mat)
			f.Revision = revision
			f.Linear = linear
			f.Complete = true
			f.ReflectionProbes = []ReflectionProbe{testProbe(t, color.NRGBA{50, 80, 120, 255})}
			f.Draws[0].Geometry.NormalUV = append([]UV(nil), f.Draws[0].Geometry.UV...)
			for i := range f.Draws[0].Geometry.LightMapUV {
				uv := &f.Draws[0].Geometry.LightMapUV[i]
				uv.U = 1 - uv.U
				uv.V = .2 + uv.V*.6
			}
			if mat.NormalMap == nil {
				f.Draws[0].Geometry.UV = nil
			}
			gpu, err := r.Render(f)
			check(t, err)
			if mad := meanAbsDiff(gpu, softwareRender(f)); mad > 3 {
				t.Logf("center gpu=%v cpu=%v", gpu.At(40, 40), softwareRender(f).At(40, 40))
				t.Fatalf("lightmap parity mat=%+v linear=%v: %.2f", mat, linear, mad)
			}
			before := nrgbaBytes(gpu)
			cached := r.textures[textureKey{src: lm.texture, aniso: f.anisotropy()}]
			_, err = r.Render(f)
			check(t, err)
			if cached != r.textures[textureKey{src: lm.texture, aniso: f.anisotropy()}] {
				t.Fatal("reuploaded lightmap")
			}
			mat.LightMap = black
			_, err = r.Render(f)
			check(t, err)
			f.Revision++
			mat.LightMap = nil
			_, err = r.Render(f)
			check(t, err)
			f.Revision++
			mat.LightMap = lm
			gpu, err = r.Render(f)
			check(t, err)
			if !bytes.Equal(before, nrgbaBytes(gpu)) {
				t.Fatal("lightmap replay changed pixels")
			}
		}
	}
	f := lightMapFrame(&Material{LightMap: lm, PBR: true, Roughness: .6})
	f.Revision = revision + 10
	f.Complete = true
	f.MSAA = true
	f.Supersample = 2
	f.PostProcess = PostProcess{SSAO: SSAO{Strength: .5, Radius: .4}}
	_, err = r.Render(f)
	check(t, err)
	// Near-plane clipping must interpolate the independent chart coordinates.
	f.MSAA = false
	f.Supersample = 1
	f.PostProcess = PostProcess{}
	f.Draws[0].Geometry.Vertices[0].Position.Z = -.3
	f.Revision++
	gpu, err := r.Render(f)
	check(t, err)
	if mad := meanAbsDiff(gpu, softwareRender(f)); mad > 3 {
		t.Fatalf("near clip parity %.2f", mad)
	}
}

func TestGPULightMapSharedBindings(t *testing.T) {
	a := testLightMap(t, [3]float32{2, 0, 0})
	b := testLightMap(t, [3]float32{0, 2, 0})
	g := lightMapFrame(&Material{LightMap: a}).Draws[0].Geometry
	shared, err := NewSharedGeometry(g)
	check(t, err)
	s := NewScene()
	_, err = s.SetShared("a", shared)
	check(t, err)
	id, err := s.SetShared("b", shared)
	check(t, err)
	check(t, s.ApplyMaterial(id, &Material{LightMap: b}))
	f := frame(t, s)
	r := &gpuRenderer{}
	defer r.Close()
	_, err = r.Render(f)
	check(t, err)
	if len(r.draws) != 2 {
		t.Fatalf("different lightmaps batched into %d draw(s)", len(r.draws))
	}
	check(t, s.ApplyMaterial(id, &Material{LightMap: a}))
	_, err = r.Render(frame(t, s))
	check(t, err)
	if len(r.draws) != 1 {
		t.Fatalf("identical lightmaps did not batch: %d", len(r.draws))
	}
}
