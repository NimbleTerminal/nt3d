// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	_ "embed"
	"nimbleterminal.dev/nt3d"
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
	g := m.Objects[0].Geometry
	id, err := d.scene.SetGeometry("Gaussian ring (576 splats)", g)
	if err != nil {
		return err
	}
	if err = d.scene.Reparent(id, d.group); err != nil {
		return err
	}
	d.meshes = &meshScene{title: "Gaussian ring", hasSH: true, nodes: []nt3d.NodeID{id}, sources: []nt3d.Geometry{g}}
	return nil
}
