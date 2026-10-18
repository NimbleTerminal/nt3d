package demo

import (
	"nimbleterminal.dev/nt3d"
	"testing"
)

func TestGaussianSample(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupGaussians(); err != nil {
		t.Fatal(err)
	}
	count, f := pointCount(t, d.scene)
	if count != 576 || f.Geometry[0].PointStyle.Shape != nt3d.GaussianPoint {
		t.Fatal("sample not loaded as Gaussians")
	}
	if _, err := nt3d.RenderSoftware(f); err != nil {
		t.Fatal(err)
	}
	if _, err := nt3d.RenderWireframe(f, 40, 20); err != nil {
		t.Fatal(err)
	}
}
