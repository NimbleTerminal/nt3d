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
	if !f.MSAA || f.Supersample != 2 || f.PostProcess.ToneMap != nt3d.ACESToneMap || f.PostProcess.SSAO.Strength <= 0 {
		t.Fatalf("frame settings not applied: msaa=%v ss=%d post=%+v", f.MSAA, f.Supersample, f.PostProcess)
	}
	if got := formatAAStatus(true, 2); got != "4x MSAA+2x SS" {
		t.Fatalf("formatAAStatus(true, 2) = %q", got)
	}
	if _, err := nt3d.NewSoftwareRenderer().Render(f); err != nil {
		t.Fatalf("software render of PBR demo scene: %v", err)
	}
}
