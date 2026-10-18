//go:build webgpu

package nt3d

import (
	"fmt"
	"image/color"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"testing"
)

func TestGPUNormalMaps(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	for i, kind := range []string{"normal only", "base color", "zero", "mirrored", "degenerate", "transparent", "unlit", "transform", "cutout", "mip"} {
		t.Run(kind, func(t *testing.T) {
			f := normalFrame(t)
			f.Revision = uint64(i + 1)
			m := f.Geometry[0].Material
			switch kind {
			case "base color":
				m.Texture = testTexture(t, color.RGBA{150, 80, 30, 255})
			case "zero":
				m.NormalStrength = 0
			case "mirrored":
				for j := range f.Geometry[0].UV {
					f.Geometry[0].UV[j].U = 1 - f.Geometry[0].UV[j].U
				}
			case "degenerate":
				for j := range f.Geometry[0].UV {
					f.Geometry[0].UV[j] = UV{}
				}
			case "transparent":
				m.Transparent = true
				for j := range f.Geometry[0].Vertices {
					f.Geometry[0].Vertices[j].Color.A = 128
				}
			case "unlit":
				m.Unlit = true
			case "transform":
				f.Instances = []Instance{{World: nt3dmath.RotationZ(.3).Mul(nt3dmath.Scaling(nt3dmath.Vec3{X: .8, Y: .5, Z: 1})), Visible: true}}
			case "cutout":
				m.Texture = testTexture(t, white)
				m.Cutout = true
			case "mip":
				m.NormalRepeat = true
				for j := range f.Geometry[0].UV {
					f.Geometry[0].UV[j].U *= 50
					f.Geometry[0].UV[j].V *= 50
				}
			}
			got, err := r.Render(f)
			check(t, err)
			want := softwareRender(f)
			for _, p := range [][2]int{{40, 40}, {38, 39}, {45, 42}} {
				if !closeRGB(at(got, p[0], p[1]), at(want, p[0], p[1]), 4) {
					t.Fatalf("%s at %v GPU %v software %v", kind, p, at(got, p[0], p[1]), at(want, p[0], p[1]))
				}
			}
		})
	}
	s := NewScene()
	_, err := s.SetGeometry("mesh", normalFrame(t).Geometry[0])
	check(t, err)
	f, err := s.Frame(DefaultCamera(), 80, 80)
	check(t, err)
	_, err = r.Render(f)
	check(t, err)
	uploads := r.uploads
	for i := 0; i < 2; i++ {
		check(t, s.SetNormalMap("mesh", testNormalMap(t, color.NRGBA{uint8(128 + i*80), 128, 255, 255})))
		f, err = s.Frame(DefaultCamera(), 80, 80)
		check(t, err)
		_, err = r.Render(f)
		check(t, err)
		if r.uploads != uploads {
			t.Fatal(fmt.Sprint("normal map replacement uploaded geometry: ", r.uploads, uploads))
		}
	}
}
