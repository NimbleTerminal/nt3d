// Copyright (c) 2026 Neomantra Corp.
package nt3dgltf

import (
	"fmt"
	"math"
	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func (d *gltfDecoder) camera(index int) (*nt3d.Camera, error) {
	if index < 0 || index >= len(d.doc.Cameras) {
		return nil, fmt.Errorf("nt3dmesh: invalid camera index")
	}
	source := d.doc.Cameras[index]
	c := nt3d.DefaultCamera()
	finite := func(v ...float64) bool {
		for _, x := range v {
			if math.IsNaN(x) || math.IsInf(x, 0) || math.Abs(x) > nt3dmathLimit {
				return false
			}
		}
		return true
	}
	switch source.Type {
	case "perspective":
		p := source.Perspective
		if p == nil || !finite(p.Yfov, p.Znear) || p.Yfov <= 0 || p.Yfov >= math.Pi || p.Znear <= 0 {
			return nil, fmt.Errorf("nt3dmesh: invalid perspective camera")
		}
		c.Projection, c.YFOV, c.Near = nt3d.Perspective, p.Yfov, p.Znear
		if p.Zfar != nil {
			c.Far = *p.Zfar
			if !finite(c.Far) || c.Far <= c.Near {
				return nil, fmt.Errorf("nt3dmesh: invalid camera far plane")
			}
		}
		if p.AspectRatio != nil {
			c.AspectRatio = *p.AspectRatio
			if !finite(c.AspectRatio) || c.AspectRatio <= 0 {
				return nil, fmt.Errorf("nt3dmesh: invalid camera aspect ratio")
			}
		}
	case "orthographic":
		p := source.Orthographic
		if p == nil || !finite(p.Xmag, p.Ymag, p.Znear, p.Zfar) || p.Xmag <= 0 || p.Ymag <= 0 || p.Znear < 0 || p.Zfar <= p.Znear {
			return nil, fmt.Errorf("nt3dmesh: invalid orthographic camera")
		}
		c.Xmag, c.Ymag, c.Near, c.Far = p.Xmag, p.Ymag, p.Znear, p.Zfar
	default:
		return nil, fmt.Errorf("nt3dmesh: unknown camera type %q", source.Type)
	}
	for _, v := range c.Matrix(1) {
		if !validCoordinate(v) {
			return nil, fmt.Errorf("nt3dmesh: camera projection exceeds coordinate limits")
		}
	}
	return &c, nil
}

const nt3dmathLimit = 1e15

// Static reflections are pushed into primitive vertices and descendant frames.
// This preserves the tree and world-space surface while Scene transforms remain
// orientation-preserving. Animated reflections still use the existing rejection.
func staticReflections(asset *nt3d.SceneAsset) error {
	if len(asset.Clips) > 0 {
		return nil
	}
	for _, n := range asset.Nodes {
		if n.Deformation != nil {
			return nil
		}
	}
	parity := make([]bool, len(asset.Nodes))
	reflection := nt3dmath.Identity()
	reflection[0] = -1
	for i := range asset.Nodes {
		n := &asset.Nodes[i]
		if n.Parent >= 0 && parity[n.Parent] {
			n.Local = reflection.Mul(n.Local)
		}
		if n.Local.Determinant3() < 0 {
			parity[i] = true
			n.Local = n.Local.Mul(reflection)
		}
		if parity[i] && n.Geometry != nil {
			g, err := nt3d.TransformGeometry(*n.Geometry, reflection)
			if err != nil {
				return err
			}
			n.Geometry = &g
		}
	}
	return nil
}
