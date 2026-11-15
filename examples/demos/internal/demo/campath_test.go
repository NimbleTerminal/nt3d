// Copyright (c) 2026 Neomantra Corp.

package demo

import (
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

func TestCamPathDemoPresetsScrubAndOverlay(t *testing.T) {
	r, err := NewRunner("campath", nil, nil, nt3dmesh.ZUp, false)
	if err != nil {
		t.Fatalf("NewRunner(campath): %v", err)
	}
	defer r.Close()

	// Initial state rides the flythrough path.
	cam0 := r.Camera()
	f0, err := r.Frame(cam0, 200, 120)
	if err != nil {
		t.Fatal(err)
	}
	if len(f0.DynamicLines) == 0 {
		t.Fatal("expected companion camera path overlay DynamicLines in riding mode")
	}

	sw := nt3d.NewSoftwareRenderer()
	defer sw.Close()
	if _, err := sw.Render(f0); err != nil {
		t.Fatalf("Render initial campath frame: %v", err)
	}

	// Scrub forward with "." and verify deterministic camera advancement.
	r.SendKey(".")
	cam1 := r.Camera()
	if cam1.WorldEye() == cam0.WorldEye() {
		t.Fatalf("expected '.' scrub to advance camera position from %v", cam0.WorldEye())
	}

	// Scrub backward with "," returns to initial camera position!
	r.SendKey(",")
	camBack := r.Camera()
	for c := range 3 {
		if d := camBack.WorldEye()[c] - cam0.WorldEye()[c]; d < -1e-9 || d > 1e-9 {
			t.Fatalf("bidirectional scrub mismatch: got %v, want %v", camBack.WorldEye(), cam0.WorldEye())
		}
	}

	// Cycle through orbit and switcher presets and toggle director orbit view ("k").
	for _, key := range []string{"]", "]", "k"} {
		r.SendKey(key)
		f, err := r.Frame(r.Camera(), 160, 100)
		if err != nil {
			t.Fatalf("Frame after key %q: %v", key, err)
		}
		if len(f.DynamicLines) == 0 {
			t.Fatalf("expected overlay lines after key %q", key)
		}
	}

	header, status := r.HeaderAndStatus()
	if !strings.Contains(header, "camera paths") || !strings.Contains(status, "director orbit") {
		t.Fatalf("unexpected header/status: %q / %q", header, status)
	}
}
