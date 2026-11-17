//go:build webgpu

// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"nimbleterminal.dev/nt3d"
	"testing"
)

func TestGPURoom(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupRoom(); err != nil {
		t.Fatal(err)
	}
	gpu := nt3d.NewGPURenderer()
	defer gpu.Close()
	sw := nt3d.NewSoftwareRenderer()
	defer sw.Close()
	for _, key := range []string{"", "p", "l", "p"} {
		if key != "" {
			if _, err := d.roomKey(key); err != nil {
				t.Fatal(err)
			}
		}
		f := roomTestFrame(t, d, 192, 128)
		a, err := gpu.Render(f)
		if err != nil {
			t.Fatal(err)
		}
		b, err := sw.Render(f)
		if err != nil {
			t.Fatal(err)
		}
		if mad := demoMeanAbsDiff(a, b); mad > 4 {
			t.Fatalf("room GPU/software difference %.2f", mad)
		}
	}
}
