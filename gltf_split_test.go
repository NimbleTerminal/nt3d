// Copyright (c) 2026 Neomantra Corp.

package nt3d_test

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/mesh"
	"nimbleterminal.dev/nt3d/mesh/gltf"
)

// A glTF splat cloud too large for one geometry is split into pieces that
// each fit, in the flat objects as in the scene asset: the turn from Y-up
// to Z-up is made piece by piece, never of the whole cloud, which no single
// geometry may hold.
func TestGLTFSplatCloudsSplitInBothViews(t *testing.T) {
	defer nt3d.SetBindingLimitForTest(nt3d.DefaultBindingLimit)
	nt3d.SetBindingLimitForTest(24 * 10) // Ten compact axis records.

	const n = 25
	var buffer []byte
	var views, accessors []string
	add := func(kind string, row ...float32) {
		start := len(buffer)
		for i := range n {
			for k, v := range row {
				if kind == "VEC3" && k == 0 && len(views) == 0 {
					v = float32(i) // Positions in a row along x.
				}
				buffer = binary.LittleEndian.AppendUint32(buffer, math.Float32bits(v))
			}
		}
		views = append(views, fmt.Sprintf(`{"buffer":0,"byteOffset":%d,"byteLength":%d}`, start, len(buffer)-start))
		accessors = append(accessors, fmt.Sprintf(`{"bufferView":%d,"componentType":5126,"count":%d,"type":%q}`, len(views)-1, n, kind))
	}
	add("VEC3", 0, 1, 0) // A row along x, one unit up in glTF's Y-up.
	add("VEC4", 0, 0, 0, 1)
	add("VEC3", .5, .5, .5)
	add("SCALAR", .9)
	add("VEC3", .5, .2, -.3)
	doc := []byte(fmt.Sprintf(`{"asset":{"version":"2.0"},"extensionsUsed":["KHR_gaussian_splatting"],"buffers":[{"uri":"side.bin","byteLength":%d}],"bufferViews":[%s],"accessors":[%s],"meshes":[{"primitives":[{"attributes":{"POSITION":0,"KHR_gaussian_splatting:ROTATION":1,"KHR_gaussian_splatting:SCALE":2,"KHR_gaussian_splatting:OPACITY":3,"KHR_gaussian_splatting:SH_DEGREE_0_COEF_0":4},"mode":0,"extensions":{"KHR_gaussian_splatting":{"kernel":"ellipse","colorSpace":"srgb_rec709_display"}}}]}],"nodes":[{"mesh":0}],"scenes":[{"nodes":[0]}],"scene":0}`,
		len(buffer), strings.Join(views, ","), strings.Join(accessors, ",")))
	open := func(string) ([]byte, error) { return buffer, nil }

	for _, assetOnly := range []bool{true, false} {
		m, err := nt3dgltf.Decode(doc, &nt3dmesh.Options{Open: open, AssetOnly: assetOnly, NoClusters: true})
		if err != nil {
			t.Errorf("asset only %v: %v", assetOnly, err)
			continue
		}
		pieces, splats := 0, 0
		for _, node := range m.Asset.Nodes {
			if node.Geometry != nil && node.Geometry.PointStyle.Shape == nt3d.GaussianPoint {
				pieces++
				splats += len(node.Geometry.Points)
			}
		}
		if pieces < 3 || splats != n {
			t.Errorf("asset only %v: the asset holds %d splats in %d pieces, want %d in three or more", assetOnly, splats, pieces, n)
		}
		if assetOnly {
			continue
		}
		pieces, splats = 0, 0
		for _, o := range m.Objects {
			if err := o.Geometry.Validate(); err != nil {
				t.Errorf("a flat piece is not a valid geometry: %v", err)
			}
			// Turned to Z-up: one unit up is z.
			for _, p := range o.Geometry.Points {
				if p.Position.Y != 0 || p.Position.Z != 1 {
					t.Fatalf("a flat piece holds a splat at %v, not one unit up in z", p.Position)
				}
			}
			pieces++
			splats += len(o.Geometry.Points)
		}
		if pieces < 3 || splats != n {
			t.Errorf("the flat objects hold %d splats in %d pieces, want %d in three or more", splats, pieces, n)
		}
	}
}
