//go:build webgpu

// Copyright (c) 2026 Neomantra Corp.

package demo

import (
	"testing"

	"nimbleterminal.dev/nt3d"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

func TestGPUIntersectingGlassShowcase(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	group, err := d.scene.Add(nt3d.Root, "shapes")
	if err != nil {
		t.Fatal(err)
	}
	d.group = group
	if err := d.setupMeshes(nil, nt3dmesh.ZUp); err != nil {
		t.Fatal(err)
	}
	if err := d.toggleShapeScene(); err != nil {
		t.Fatal(err)
	}
	renderer := nt3d.NewGPURenderer()
	defer renderer.Close()

	cam := nt3d.DefaultCamera()
	cam.Distance = 2.6
	for _, msaa := range []bool{false, true} {
		fOIT, err := d.scene.Frame(cam, 96, 72)
		if err != nil {
			t.Fatal(err)
		}
		fOIT.MSAA = msaa
		d.decorate(&fOIT)
		imgOIT, err := renderer.Render(fOIT)
		if err != nil {
			t.Fatalf("msaa=%v OIT render: %v", msaa, err)
		}
		fSorted := fOIT
		fSorted.OIT = false
		imgSorted, err := renderer.Render(fSorted)
		if err != nil {
			t.Fatalf("msaa=%v sorted render: %v", msaa, err)
		}
		diff := 0
		for y := 0; y < 72; y++ {
			for x := 0; x < 96; x++ {
				if imgOIT.At(x, y) != imgSorted.At(x, y) {
					diff++
				}
			}
		}
		if diff < 50 {
			t.Fatalf("msaa=%v: expected GPU intersecting glass OIT vs sorted to differ visibly, got %d pixels", msaa, diff)
		}
	}
}
