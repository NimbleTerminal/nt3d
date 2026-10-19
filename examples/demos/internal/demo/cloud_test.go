// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/clod"
)

// cloudView is what a camera at the given distance sees of a 200×150 frame.
func cloudView(distance float64) nt3dclod.View {
	c := nt3d.Camera{Alpha: 30, Beta: 40, Distance: distance, Projection: nt3d.Perspective, ResumeAfter: 1}
	return nt3dclod.View{Matrix: c.Matrix(200. / 150), Eye: c.Eye(), Perspective: true, Width: 200, Height: 150, Margin: cloudMargin}
}

func TestCloudBuildsAndDrawsEveryPointUnclustered(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupCloud(); err != nil {
		t.Fatal(err)
	}
	d.cloud.count, d.cloud.lod = 0, false
	if err := d.buildCloud(); err != nil {
		t.Fatal(err)
	}
	n, f := pointCount(t, d.scene)
	if n != 100_000 {
		t.Fatalf("%d points, want 100,000", n)
	}
	for _, g := range f.Geometry {
		if len(g.Points) > nt3d.MaxPoints {
			t.Fatalf("a geometry holds %d points", len(g.Points))
		}
	}
	if _, err := nt3d.NewSoftwareRenderer().Render(f); err != nil {
		t.Fatal(err)
	}
}

func TestCloudLODDrawsFewerWhenFarther(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupCloud(); err != nil {
		t.Fatal(err)
	}
	cs := d.cloud
	cs.count = 0
	if err := d.buildCloud(); err != nil {
		t.Fatal(err)
	}
	cs.view, cs.have = cloudView(3), true
	if changed, err := d.stepCloud(); err != nil || !changed {
		t.Fatalf("near view: changed %v, %v", changed, err)
	}
	near, _ := pointCount(t, d.scene)
	if near != cs.drawn {
		t.Fatalf("near %d drawn %d", near, cs.drawn)
	}
	if changed, err := d.stepCloud(); err != nil || changed {
		t.Fatalf("same view: changed %v, %v", changed, err)
	}
	cs.view = cloudView(60)
	if changed, err := d.stepCloud(); err != nil || !changed {
		t.Fatalf("far view: changed %v, %v", changed, err)
	}
	far, f := pointCount(t, d.scene)
	if far >= near || near > len(cs.points) || far != cs.drawn {
		t.Fatalf("near %d, far %d, total %d, drawn %d", near, far, len(cs.points), cs.drawn)
	}
	if _, err := nt3d.NewSoftwareRenderer().Render(f); err != nil {
		t.Fatal(err)
	}
}

func TestCloudRepresentativesForFarClusters(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupCloud(); err != nil {
		t.Fatal(err)
	}
	cs := d.cloud
	cs.count = 0
	if err := d.buildCloud(); err != nil {
		t.Fatal(err)
	}
	cs.view, cs.have = cloudView(60), true
	if _, err := d.stepCloud(); err != nil {
		t.Fatal(err)
	}
	if len(cs.sel.Picks) == 0 || len(cs.sel.Items) != 0 {
		t.Fatalf("%d picks, %d items; want picks with no items", len(cs.sel.Picks), len(cs.sel.Items))
	}
	for _, p := range cs.sel.Picks {
		if p.Level != 3 || p.Count != 0 {
			t.Fatalf("far pick %+v", p)
		}
	}
	n, f := pointCount(t, d.scene)
	if n != len(cs.sel.Picks) {
		t.Fatalf("%d points for %d far clusters", n, len(cs.sel.Picks))
	}
	for _, g := range f.Geometry {
		for _, p := range g.Points {
			if p.Radius < 1 || p.Radius > 32 {
				t.Fatalf("representative radius %v", p.Radius)
			}
		}
	}
}

func TestCloudCountsCycle(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupCloud(); err != nil {
		t.Fatal(err)
	}
	if err := d.cycleCloudCount(); err != nil { // From the default 300,000 to a million.
		t.Fatal(err)
	}
	cs := d.cloud
	if len(cs.points) != 1_000_000 || len(cs.set.Clusters) < 2 {
		t.Fatalf("%d points in %d clusters", len(cs.points), len(cs.set.Clusters))
	}
	n, _ := pointCount(t, d.scene)
	if n != 1_000_000 {
		t.Fatalf("%d points drawn before any view, want all", n)
	}
	if commas(1234567) != "1,234,567" || commas(999) != "999" {
		t.Fatal(commas(1234567), commas(999))
	}
}
