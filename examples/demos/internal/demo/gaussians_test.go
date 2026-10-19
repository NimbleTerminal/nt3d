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
	if count != 576 || len(f.Geometry[0].GaussianSH) != 4*count || f.Geometry[0].PointStyle.Shape != nt3d.GaussianPoint {
		t.Fatal("sample not loaded as Gaussians")
	}
	if _, err := nt3d.RenderSoftware(f); err != nil {
		t.Fatal(err)
	}
	if _, err := nt3d.RenderWireframe(f, 40, 20); err != nil {
		t.Fatal(err)
	}
}

func TestGaussianSHControls(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupGaussians(); err != nil {
		t.Fatal(err)
	}
	d.meshes.dcOnly = true
	if err := d.rebuildMeshes(); err != nil {
		t.Fatal(err)
	}
	_, f := pointCount(t, d.scene)
	if !f.Geometry[0].PointStyle.SHDCOnly || len(f.Geometry[0].GaussianSH) != 576*4 {
		t.Fatal("DC toggle discarded coefficients")
	}
}
