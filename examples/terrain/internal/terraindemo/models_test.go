// Copyright (c) 2026 Neomantra Corp.
package terraindemo

import (
	"testing"

	"nimbleterminal.dev/nt3d"
)

// Every embedded sample decodes cleanly into the scene, with what it shows.
func TestSamplesLoad(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	var err error
	if d.group, err = d.scene.Add(nt3d.Root, "island"); err != nil {
		t.Fatal(err)
	}
	if err = d.setupModels(); err != nil {
		t.Fatal(err)
	}
	for i, s := range samples {
		if err := d.loadSample(i); err != nil {
			t.Fatalf("%s: %v", s.file, err)
		}
		m := d.models.model
		if len(m.Warnings) != 0 || m.Triangles() == 0 || d.status != s.about {
			t.Fatalf("%s: %d triangles, warnings %v", s.file, m.Triangles(), m.Warnings)
		}
		textured := false
		for _, o := range m.Objects {
			textured = textured || o.Geometry.Material != nil && o.Geometry.Material.Texture != nil
		}
		if s.file != "knot.stl" && !textured {
			t.Fatalf("%s: texture missing", s.file)
		}
	}
	// One model is shown at a time.
	if node, _ := d.scene.Node(d.group); len(node.Children) != 1 {
		t.Fatalf("%d models in the scene", len(node.Children))
	}
}
