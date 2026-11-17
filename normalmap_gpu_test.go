//go:build webgpu

package nt3d

import (
	"fmt"
	"image"
	"image/color"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"strings"
	"testing"
)

func TestGPUNormalMaps(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	for i, kind := range []string{"normal only", "base color", "zero", "mirrored", "degenerate", "transparent", "unlit", "transform", "cutout", "mip", "independent", "independent cutout", "authored", "authored flipped", "authored w", "authored independent", "authored transform", "authored parallel", "authored transparent", "independent bright", "authored mixed"} {
		t.Run(kind, func(t *testing.T) {
			f := normalFrame(t)
			f.Revision = uint64(i + 1)
			m := f.Draws[0].Geometry.Material
			if strings.HasPrefix(kind, "authored") {
				for range f.Draws[0].Geometry.Vertices {
					f.Draws[0].Geometry.Tangents = append(f.Draws[0].Geometry.Tangents, Tangent{Direction: nt3dmath.Vec3{X: 1}, W: 1})
				}
			}
			switch kind {
			case "authored flipped":
				for j := range f.Draws[0].Geometry.Tangents {
					f.Draws[0].Geometry.Tangents[j].Direction = nt3dmath.Vec3{X: -1}
				}
			case "authored w":
				m.NormalMap = testNormalMap(t, color.NRGBA{128, 218, 218, 255})
				f.Lighting.Lights[0].Direction = nt3dmath.Vec3{Y: 1}
				for j := range f.Draws[0].Geometry.Tangents {
					f.Draws[0].Geometry.Tangents[j].W = -1
				}
			case "authored independent":
				f.Draws[0].Geometry.NormalUV = append([]UV(nil), f.Draws[0].Geometry.UV...)
				for j := range f.Draws[0].Geometry.NormalUV {
					f.Draws[0].Geometry.NormalUV[j].U = 1 - f.Draws[0].Geometry.NormalUV[j].U
				}
			case "authored transform":
				placeAlone(&f, nt3dmath.RotationZ(.3).Mul(nt3dmath.Scaling(nt3dmath.Vec3{X: .8, Y: .5, Z: 1})))
			case "authored parallel":
				for j := range f.Draws[0].Geometry.Tangents {
					f.Draws[0].Geometry.Tangents[j].Direction = nt3dmath.Vec3{Z: 1}
				}
			case "independent bright":
				// The base UVs run backward and the normal UVs forward: only
				// a read of the extension record lights the quad.
				f.Draws[0].Geometry.NormalUV = append([]UV(nil), f.Draws[0].Geometry.UV...)
				for j := range f.Draws[0].Geometry.UV {
					f.Draws[0].Geometry.UV[j].U = 1 - f.Draws[0].Geometry.UV[j].U
				}
			case "authored mixed":
				// Handedness differs across the quad: both renderers use the
				// vertices' frame with the interpolated sign, all the way.
				m.NormalMap = testNormalMap(t, color.NRGBA{128, 218, 218, 255})
				f.Lighting.Lights[0].Direction = nt3dmath.Vec3{Y: 1}
				f.Draws[0].Geometry.Tangents[0].W, f.Draws[0].Geometry.Tangents[1].W = -1, -1
			case "authored transparent":
				m.Transparent = true
				for j := range f.Draws[0].Geometry.Vertices {
					f.Draws[0].Geometry.Vertices[j].Color.A = 128
				}
			case "base color":
				m.Texture = testTexture(t, color.RGBA{150, 80, 30, 255})
			case "independent", "independent cutout":
				f.Draws[0].Geometry.NormalUV = append([]UV(nil), f.Draws[0].Geometry.UV...)
				for j := range f.Draws[0].Geometry.NormalUV {
					f.Draws[0].Geometry.NormalUV[j].U = 1 - f.Draws[0].Geometry.NormalUV[j].U
				}
				if kind == "independent cutout" {
					m.Texture = testTexture(t, white)
					m.Cutout = true
				}
			case "zero":
				m.NormalStrength = 0
			case "mirrored":
				for j := range f.Draws[0].Geometry.UV {
					f.Draws[0].Geometry.UV[j].U = 1 - f.Draws[0].Geometry.UV[j].U
				}
			case "degenerate":
				for j := range f.Draws[0].Geometry.UV {
					f.Draws[0].Geometry.UV[j] = UV{}
				}
			case "transparent":
				m.Transparent = true
				for j := range f.Draws[0].Geometry.Vertices {
					f.Draws[0].Geometry.Vertices[j].Color.A = 128
				}
			case "unlit":
				m.Unlit = true
			case "transform":
				placeAlone(&f, nt3dmath.RotationZ(.3).Mul(nt3dmath.Scaling(nt3dmath.Vec3{X: .8, Y: .5, Z: 1})))
			case "cutout":
				m.Texture = testTexture(t, white)
				m.Cutout = true
			case "mip":
				m.NormalRepeat = true
				for j := range f.Draws[0].Geometry.UV {
					f.Draws[0].Geometry.UV[j].U *= 50
					f.Draws[0].Geometry.UV[j].V *= 50
				}
			}
			got, err := r.Render(f)
			check(t, err)
			want := softwareRender(f)
			if (kind == "independent bright" || kind == "authored") && at(got, 40, 40).R < 180 {
				t.Fatalf("%s: GPU did not tilt toward the light: %v", kind, at(got, 40, 40))
			}
			pixels := [][2]int{{40, 40}, {38, 39}, {45, 42}}
			if kind == "authored mixed" {
				// Off the line where the sign crosses zero.
				pixels = [][2]int{{40, 30}, {40, 50}, {38, 35}, {45, 46}}
			}
			for _, p := range pixels {
				if !closeRGB(at(got, p[0], p[1]), at(want, p[0], p[1]), 4) {
					t.Fatalf("%s at %v GPU %v software %v", kind, p, at(got, p[0], p[1]), at(want, p[0], p[1]))
				}
			}
		})
	}
	s := NewScene()
	_, err := s.SetGeometry("mesh", normalFrame(t).Draws[0].Geometry)
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

// Minified footprints at the right UV border must use the same addressing
// for procedural and uploaded normals, including simultaneous shared use.
func TestGPUProceduralNormalAddressing(t *testing.T) {
	pixels := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			c := color.NRGBA{218, 128, 218, 255}
			if x < 32 {
				c.R = 38
			}
			pixels.SetNRGBA(x, y, c)
		}
	}
	uploaded, err := NewNormalMap(pixels)
	check(t, err)
	shader, err := NewShaderNormalMap(`fn nt3dTexture(uv:vec2<f32>)->vec4<f32>{return vec4<f32>(select(38.0,218.0,uv.x>=0.5)/255.0,128.0/255.0,218.0/255.0,1.0);}`, pixels)
	check(t, err)
	r := &gpuRenderer{}
	defer r.Close()
	var border [2]uint8
	for i, repeat := range []bool{false, true} {
		f := normalFrame(t)
		f.Anisotropy = 1
		f.Revision = uint64(i*2 + 1)
		g := &f.Draws[0].Geometry
		g.NormalUV = make([]UV, len(g.UV))
		for j, uv := range g.UV {
			g.NormalUV[j] = UV{U: .99 + uv.U*.01, V: uv.V * 16}
			g.Tangents = append(g.Tangents, Tangent{Direction: nt3dmath.Vec3{X: 1}, W: 1})
		}
		g.Material.NormalRepeat = repeat
		g.Material.NormalMap = uploaded
		want, err := r.Render(f)
		check(t, err)
		f.Revision++
		g.Material.NormalMap = shader
		got, err := r.Render(f)
		check(t, err)
		for _, p := range [][2]int{{40, 40}, {38, 39}, {45, 42}} {
			// Normal-image mips renormalize vectors; shader targets use
			// hardware color mips. Compare the constant clamped edge.
			if !repeat && !closeRGB(at(got, p[0], p[1]), at(want, p[0], p[1]), 4) {
				t.Fatalf("repeat=%v at %v: shader %v image %v", repeat, p, at(got, p[0], p[1]), at(want, p[0], p[1]))
			}
		}
		border[i] = at(got, 40, 40).R
		// Both sampler variants must coexist for the same shader in one frame.
		other := *g
		m := *g.Material
		m.NormalRepeat = !repeat
		other.Material = &m
		f.Draws = append(f.Draws, Draw{Geometry: other})
		f.Revision++
		_, err = r.Render(f)
		check(t, err)
		clamp, tile := r.procedural[proceduralKey{shader, false}], r.procedural[proceduralKey{shader, true}]
		if clamp == nil || tile == nil || clamp == tile {
			t.Fatal("shared normal addressing variants aliased")
		}
	}
	if int(border[0])-int(border[1]) < 10 {
		t.Fatalf("fixture did not distinguish clamp and repeat: %v", border)
	}
}
