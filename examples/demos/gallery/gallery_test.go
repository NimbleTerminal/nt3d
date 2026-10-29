// Copyright (c) 2026 Neomantra Corp.
package gallery

import (
	"testing"

	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

func TestAllDemosBuildAndStep(t *testing.T) {
	names := Names()
	if len(names) != 18 {
		t.Fatalf("Names() len = %d, want 18", len(names))
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			r, err := NewRunner(name, nil, nt3dmesh.ZUp, false)
			if err != nil {
				t.Fatalf("NewRunner(%q): %v", name, err)
			}
			defer r.Close()

			r.SetViewportSize(960, 640)
			f, err := r.Frame(r.Camera(), 320, 200)
			if err != nil {
				t.Fatalf("Frame(%q): %v", name, err)
			}
			if f.Width != 320 || f.Height != 200 {
				t.Fatalf("Frame size = %dx%d, want 320x200", f.Width, f.Height)
			}
			r.Step(1.0 / 60.0)
			header, status := r.HeaderAndStatus()
			if header == "" || status == "" {
				t.Fatalf("HeaderAndStatus(%q) = (%q, %q), want non-empty", name, header, status)
			}
			_ = r.Pick(r.Camera(), f, 160, 100, 6)
		})
	}
}
