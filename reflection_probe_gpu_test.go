// Copyright (c) 2026 Neomantra Corp.

//go:build webgpu

package nt3d

import (
	"bytes"
	"image/color"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"testing"
)

func TestGPUReflectionProbes(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	red := testProbe(t, color.NRGBA{255, 20, 10, 255})
	blue := testProbe(t, color.NRGBA{10, 30, 255, 255})
	gradient := red
	gradient.Environment = testEnvironment(t)
	gradient.Position = nt3dmath.Vec3{X: -1, Y: .5}
	for _, mat := range []*Material{
		{PBR: true, Metallic: 1, Roughness: .1},
		{PBR: true, Metallic: 1, Roughness: .8},
		{PBR: true, Roughness: .5, Clearcoat: 1, ClearcoatRoughness: .15},
		{Reflectivity: 1},
		{Unlit: true},
	} {
		for _, linear := range []bool{true, false} {
			f := pbrQuadFrame(mat, color.RGBA{160, 150, 140, 255})
			f.Linear = linear
			f.ReflectionProbes = []ReflectionProbe{gradient}
			f.Environment = blue.Environment
			f.EnvironmentBackgroundOnly = true
			gpu, err := r.Render(f)
			check(t, err)
			cpu := softwareRender(f)
			if mad := meanAbsDiff(gpu, cpu); mad > 3 {
				t.Fatalf("probe parity (mat=%+v linear=%v): %.2f", mat, linear, mad)
			}
			// Probe-only scenes still require trilinear GGX mip filtering.
			f.Environment = nil
			gpu, err = r.Render(f)
			check(t, err)
			if mad := meanAbsDiff(gpu, softwareRender(f)); mad > 3 {
				t.Fatalf("probe-only parity (mat=%+v linear=%v): %.2f", mat, linear, mad)
			}

		}
	}
	// Verify every binding slot, exclusion outside a box, and first-probe priority.
	f := pbrQuadFrame(&Material{Reflectivity: 1}, color.RGBA{120, 120, 120, 255})
	for slot := 0; slot < MaxReflectionProbes; slot++ {
		f.ReflectionProbes = make([]ReflectionProbe, slot+1)
		for i := 0; i < slot; i++ {
			p := blue
			p.Position.X += 10
			p.Bounds.Min.X += 10
			p.Bounds.Max.X += 10
			f.ReflectionProbes[i] = p
		}
		f.ReflectionProbes[slot] = red
		gpu, err := r.Render(f)
		check(t, err)
		if c := rgbaAt(gpu, 40, 40); c.R < 200 || c.B > 30 {
			t.Fatalf("probe slot %d: %v", slot, c)
		}
	}
	f.ReflectionProbes = []ReflectionProbe{red, blue}
	f.MSAA = true
	f.Supersample = 2
	f.Complete = true
	f.PostProcess = PostProcess{SSAO: SSAO{Strength: .5, Radius: .4}}
	before, err := r.Render(f)
	check(t, err)
	cached := r.probeTextures[0]
	moved := f
	moved.ReflectionProbes = append([]ReflectionProbe(nil), f.ReflectionProbes...)
	moved.ReflectionProbes[0].Position.X = .5
	_, err = r.Render(moved)
	check(t, err)
	if r.probeTextures[0] != cached {
		t.Fatal("moving probe reuploaded panorama")
	}
	removed := f
	removed.ReflectionProbes = nil
	_, err = r.Render(removed)
	check(t, err)
	if r.probeTextures[0] != nil {
		t.Fatal("removed probe retained texture")
	}
	replay, err := r.Render(f)
	check(t, err)
	if !bytes.Equal(nrgbaBytes(before), nrgbaBytes(replay)) {
		t.Fatal("probe frame replay changed pixels")
	}
	// A probe and global environment with the same source share the GPU image.
	f.Environment = red.Environment
	_, err = r.Render(f)
	check(t, err)
	if r.environmentTexture != r.probeTextures[0] {
		t.Fatal("probe panorama not shared with environment")
	}
}

func TestGPUReflectionProbeHDRSharedViews(t *testing.T) {
	_, _, dev := newTestHostDevice(t)
	session, err := NewGPUSession(dev)
	check(t, err)
	defer session.Close()
	view, err := session.NewView()
	check(t, err)
	defer view.Close()
	data := encodeRadianceHDR(16, 8, true, 1, "", func(x, y int) [3]float64 { return [3]float64{4, 2, 1} })
	env, err := NewHDREnvironment(data)
	check(t, err)
	probe := testProbe(t, color.NRGBA{0, 0, 0, 255})
	probe.Environment = env
	f := pbrQuadFrame(&Material{PBR: true, Metallic: 1, Roughness: .4}, color.RGBA{255, 255, 255, 255})
	f.ReflectionProbes = []ReflectionProbe{probe}
	f.PostProcess = PostProcess{ToneMap: ReinhardToneMap, Exposure: -1}
	a, err := session.RenderToImage(f)
	check(t, err)
	b, err := view.RenderToImage(f)
	check(t, err)
	if !bytes.Equal(nrgbaBytes(a), nrgbaBytes(b)) {
		t.Fatal("shared probe views disagree")
	}
	if session.g.probeTextures[0] != view.g.probeTextures[0] {
		t.Fatal("shared views reuploaded the same probe")
	}
	if mad := meanAbsDiff(a, softwareRender(f)); mad > 2 {
		t.Fatalf("HDR probe parity: %.2f", mad)
	}
	// Clearing one view must not release a panorama still used by another.
	f.ReflectionProbes = nil
	_, err = session.RenderToImage(f)
	check(t, err)
	f.ReflectionProbes = []ReflectionProbe{probe}
	replay, err := view.RenderToImage(f)
	check(t, err)
	if !bytes.Equal(nrgbaBytes(b), nrgbaBytes(replay)) {
		t.Fatal("clearing sibling view invalidated probe")
	}
}
