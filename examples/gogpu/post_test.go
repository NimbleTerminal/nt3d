// Copyright (c) 2026 Neomantra Corp.

package main

import (
	"testing"

	"nimbleterminal.dev/nt3d"
)

func TestPostPresetsAndFrameBuilder(t *testing.T) {
	lighting := newStudioLighting()
	if err := lighting.setPostByName("studio"); err != nil {
		t.Fatalf("setPostByName(studio): %v", err)
	}
	if !lighting.overridePostInDemo || lighting.postName() != "Studio" {
		t.Fatalf("unexpected post state: override=%v name=%q", lighting.overridePostInDemo, lighting.postName())
	}
	if err := lighting.setPostByName("unknown"); err == nil {
		t.Fatal("expected error for unknown post preset")
	}

	v, err := buildDemoScene()
	if err != nil {
		t.Fatal(err)
	}
	defer v.close()

	f, err := v.buildFrame(nt3d.DefaultCamera(), 320, 200, true, 2)
	if err != nil {
		t.Fatal(err)
	}
	lighting.applyFramePost(&f, false)
	if !f.MSAA || !f.OIT || f.Supersample != 2 || f.PostProcess.ToneMap != nt3d.ACESToneMap || f.PostProcess.SSAO.Strength <= 0 {
		t.Fatalf("frame settings not applied: msaa=%v oit=%v ss=%d post=%+v", f.MSAA, f.OIT, f.Supersample, f.PostProcess)
	}
	if got := formatAAStatus(true, 2); got != "4x MSAA+2x SS" {
		t.Fatalf("formatAAStatus(true, 2) = %q", got)
	}
	if _, err := nt3d.NewSoftwareRenderer().Render(f); err != nil {
		t.Fatalf("software render of PBR+OIT demo scene: %v", err)
	}

	// Verify HDR Studio Sun environment + Spotlights rig with perspective shadow maps!
	lighting.envIndex = 1 // HDR Studio Sun
	lighting.rigIndex = 2 // Spotlights
	lighting.shadows = true
	lighting.applyEnvironment(v)
	f2, err := v.buildFrame(nt3d.DefaultCamera(), 160, 100, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	lighting.applyFrameLighting(&f2, v)
	lighting.applyFramePost(&f2, false)
	if len(f2.Lighting.Lights) != 3 || f2.Lighting.Lights[0].Kind != nt3d.SpotLight || !f2.Lighting.Lights[0].Shadows {
		t.Fatalf("expected Spotlights rig with key shadow, got %+v", f2.Lighting.Lights)
	}
	if _, err := nt3d.NewSoftwareRenderer().Render(f2); err != nil {
		t.Fatalf("software render of HDR+Spotlights+OIT scene: %v", err)
	}

	// Verify authored node lights (KHR_lights_punctual) are preserved until overridden.
	ln, err := v.scene.Add(nt3d.Root, "gltf-spot")
	if err != nil {
		t.Fatal(err)
	}
	if err := v.scene.SetNodeLight(ln, nt3d.Light{Kind: nt3d.SpotLight, Intensity: 2.0, InnerCone: 0.2, OuterCone: 0.5}); err != nil {
		t.Fatal(err)
	}
	lighting.overrideInDemo = false
	f3, err := v.buildFrame(nt3d.DefaultCamera(), 160, 100, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	lighting.applyFrameLighting(&f3, v)
	if len(f3.Lighting.Lights) != 1 || f3.Lighting.Lights[0].Kind != nt3d.SpotLight || !f3.Lighting.Lights[0].Shadows {
		t.Fatalf("expected preserved scene spotlight, got %+v", f3.Lighting.Lights)
	}

	// Verify intersecting crystal-glass rings in default scene show distinct OIT vs sorted output.
	fOIT, err := v.buildFrame(nt3d.DefaultCamera(), 120, 90, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	lighting.oit = true
	lighting.applyFramePost(&fOIT, false)
	fSorted := fOIT
	lighting.toggleOIT()
	lighting.applyFramePost(&fSorted, false)
	if !fOIT.OIT || fSorted.OIT {
		t.Fatalf("unexpected OIT toggle flags: oit=%v sorted=%v", fOIT.OIT, fSorted.OIT)
	}
	sw := nt3d.NewSoftwareRenderer()
	imgOIT, err := sw.Render(fOIT)
	if err != nil {
		t.Fatal(err)
	}
	imgSorted, err := sw.Render(fSorted)
	if err != nil {
		t.Fatal(err)
	}
	diff := 0
	for y := 0; y < 90; y++ {
		for x := 0; x < 120; x++ {
			if imgOIT.At(x, y) != imgSorted.At(x, y) {
				diff++
			}
		}
	}
	if diff < 20 {
		t.Fatalf("expected intersecting glass rings in gogpu scene to differ between OIT and sorted, got %d pixels", diff)
	}
}
