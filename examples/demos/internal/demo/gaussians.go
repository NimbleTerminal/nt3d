// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	_ "embed"
	"nimbleterminal.dev/nt3d/mesh"
)

//go:embed models/gaussian-ring.ply
var gaussianSample []byte

// GaussiansMain shows a small uncompressed 3DGS PLY with the shared orbit controls.
func GaussiansMain() { mainDemo(false, sceneGaussians) }
func (d *demo) setupGaussians() error {
	m, err := nt3dmesh.DecodePLY(gaussianSample, nil)
	if err != nil {
		return err
	}
	_, err = d.scene.SetGeometry("Gaussian ring (576 splats)", m.Objects[0].Geometry)
	return err
}
