// Copyright (c) 2026 Neomantra Corp.

package demo

import (
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

func TestProceduralHooksDemoToggleAndStep(t *testing.T) {
	r, err := NewRunner("procedural", nil, nil, nt3dmesh.ZUp, false)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if !r.d.procedural || !r.d.hooks {
		t.Fatalf("expected procedural demo to start with procedural=true hooks=true, got procedural=%v hooks=%v", r.d.procedural, r.d.hooks)
	}
	header, status := r.HeaderAndStatus()
	if !strings.Contains(header, "w hooks") || !strings.Contains(status, "shader hooks") {
		t.Fatalf("unexpected procedural HUD: header=%q status=%q", header, status)
	}

	sw := nt3d.NewSoftwareRenderer()
	f0, err := r.Frame(r.Camera(), 128, 96)
	if err != nil {
		t.Fatal(err)
	}
	if f0.Draws[0].Geometry.Displacement == nil || f0.Draws[0].Geometry.Material == nil || f0.Draws[0].Geometry.Material.Shader == nil {
		t.Fatal("expected terrain geometry in procedural demo to carry both VertexDisplacement and MaterialShader")
	}
	if _, err := sw.Render(f0); err != nil {
		t.Fatal(err)
	}

	r.Step(0.25)
	f1, err := r.Frame(r.Camera(), 128, 96)
	if err != nil {
		t.Fatal(err)
	}
	if f1.Draws[0].Geometry.Displacement.Time() <= f0.Draws[0].Geometry.Displacement.Time() {
		t.Fatal("expected Step to advance VertexDisplacement time via WithParameters")
	}
	if _, err := sw.Render(f1); err != nil {
		t.Fatal(err)
	}

	// Toggle hooks off via "w", then back on.
	r.SendKey("w")
	fOff, err := r.Frame(r.Camera(), 128, 96)
	if err != nil {
		t.Fatal(err)
	}
	if fOff.Draws[0].Geometry.Displacement != nil || fOff.Draws[0].Geometry.Material.Shader != nil {
		t.Fatal("expected 'w' key to clear shader hooks")
	}
	r.SendKey("w")
	fOn, err := r.Frame(r.Camera(), 128, 96)
	if err != nil {
		t.Fatal(err)
	}
	if fOn.Draws[0].Geometry.Displacement == nil || fOn.Draws[0].Geometry.Material.Shader == nil {
		t.Fatal("expected second 'w' key to restore shader hooks")
	}
}
