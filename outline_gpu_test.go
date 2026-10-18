//go:build webgpu

package nt3d

import (
	"image/color"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"testing"
)

func TestGPUOutlines(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	for i, kind := range []string{"unlit", "lit", "texture", "normal map", "cutout", "transparent", "texture transparent", "perspective", "transform", "supersample", "encoded", "fog", "hidden alpha", "occluded"} {
		t.Run(kind, func(t *testing.T) {
			f := outlineFrame(t)
			// Manual revisions give each test's altered geometry a distinct cache key.
			f.GeometryRevisions = nil
			f.Instances = nil
			f.Shared = nil
			f.Revision = uint64(i + 1)
			g := &f.Geometry[0]
			switch kind {
			case "lit":
				g.Material.Unlit = false
			case "texture":
				g.Material.Texture = testTexture(t, color.RGBA{50, 170, 210, 255})
			case "normal map":
				g.Material.Unlit = false
				g.Material.NormalMap = testNormalMap(t, color.NRGBA{180, 128, 240, 255})
				g.Material.NormalStrength = 1
			case "cutout", "hidden alpha":
				c := white
				if kind == "hidden alpha" {
					c = color.RGBA{}
				}
				g.Material.Texture = testTexture(t, c)
				g.Material.Cutout = true
			case "transparent", "texture transparent":
				g.Material.Transparent = true
				for j := range g.Vertices {
					g.Vertices[j].Color.A = 120
				}
				if kind == "texture transparent" {
					g.Material.Texture = testTexture(t, color.RGBA{100, 100, 100, 180})
				}
			case "perspective":
				c := DefaultCamera()
				c.Projection = Perspective
				f.Matrix = c.Matrix(1)
				f.Perspective = true
			case "transform":
				f.Instances = []Instance{{World: nt3dmath.RotationZ(.6).Mul(nt3dmath.Scaling(nt3dmath.Vec3{X: .9, Y: 1.2, Z: .6})), Visible: true}}
			case "supersample":
				f.Supersample = 2
			case "encoded":
				f.Linear = false
			case "fog":
				f.Fog = Fog{Near: 1, Far: 6, Color: color.RGBA{80, 90, 100, 255}}
			case "occluded":
				pane := quad(.95, .001, white, &Material{Unlit: true}, false)
				inv, _ := f.Matrix.InverseAffine()
				for j := range pane.Vertices {
					pane.Vertices[j].Position = inv.Point(pane.Vertices[j].Position)
				}
				pane.Bounds = GeometryBounds(pane)
				f.Geometry = append(f.Geometry, pane)
			}
			got, err := r.Render(f)
			check(t, err)
			want := softwareRender(f)
			differing := 0
			for y := 0; y < 96; y++ {
				for x := 0; x < 96; x++ {
					if !closeRGB(at(got, x, y), at(want, x, y), 5) {
						differing++
					}
				}
			}
			if differing > 40 {
				t.Fatalf("%s: GPU/software disagree at %d pixels", kind, differing)
			}
			if kind == "unlit" && coloredPixels(got, outlineInk) < 150 {
				t.Fatal("GPU has no outline")
			}
		})
	}
}

func TestGPUOutlineDeformation(t *testing.T) {
	s := NewScene()
	joint, err := s.Add(Root, "joint")
	check(t, err)
	base := outlineFrame(t).Geometry[0]
	id, err := s.SetGeometry("ball", base)
	check(t, err)
	influences := make([]Influence, len(base.Vertices))
	morph := MorphTarget{Positions: make([]nt3dmath.Vec3, len(base.Vertices))}
	for i, v := range base.Vertices {
		influences[i].Weights[0] = 1
		morph.Positions[i] = nt3dmath.Vec3{Z: v.Position.Z * .4}
	}
	check(t, s.SetDeformation(id, Deformation{Skeleton: &Skeleton{Joints: []NodeID{joint}}, Influences: influences, Morphs: []MorphTarget{morph}}))
	gpu, reference := &gpuRenderer{}, &gpuRenderer{}
	defer gpu.Close()
	defer reference.Close()
	for step := 0; step < 3; step++ {
		check(t, s.SetMorphWeights(id, []float32{float32(step) * .4}))
		check(t, s.SetTransform(joint, nt3dmath.Translation(nt3dmath.Vec3{Z: float32(step) * .2}).Mul(nt3dmath.RotationZ(float64(step)*.3))))
		f, err := s.Frame(DefaultCamera(), 96, 96)
		check(t, err)
		got, err := gpu.Render(f)
		check(t, err)
		cpu := f
		cpu.Geometry = append([]Geometry(nil), f.Geometry...)
		cpu.Geometry[0].posed = nil
		want, err := reference.Render(cpu)
		check(t, err)
		diff := 0
		for y := 0; y < 96; y++ {
			for x := 0; x < 96; x++ {
				if !closeRGB(at(got, x, y), at(want, x, y), 3) {
					diff++
				}
			}
		}
		if diff > 10 {
			t.Fatalf("pose %d: CPU/GPU hull deformation differs at %d pixels", step, diff)
		}
		if coloredPixels(got, outlineInk) < 100 {
			t.Fatalf("pose %d lost outline", step)
		}
		if gpu.uploads != 1 {
			t.Fatalf("pose %d uploaded mesh again: %d", step, gpu.uploads)
		}
		check(t, s.SetOutline(id, OutlineStyle{}))
		off, err := s.Frame(DefaultCamera(), 96, 96)
		check(t, err)
		noOutline, err := gpu.Render(off)
		check(t, err)
		if coloredPixels(noOutline, outlineInk) != 0 {
			t.Fatal("GPU toggle retained outline")
		}
		check(t, s.SetOutline(id, base.Outline))
		if gpu.uploads != 1 {
			t.Fatal("outline toggle reuploaded mesh")
		}
	}
}
