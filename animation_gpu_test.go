//go:build webgpu

package nt3d

import (
	"fmt"
	"image/color"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"testing"
)

func TestGPUSkinMatchesCPU(t *testing.T) {
	for _, texture := range []bool{false, true} {
		t.Run(fmt.Sprint("textured=", texture), func(t *testing.T) { testGPUSkinMatchesCPU(t, texture) })
	}
}
func testGPUSkinMatchesCPU(t *testing.T, texture bool) {
	s, id, joint := animationSkinScene(t)
	// Color the bind mesh before rebinding deformation.
	n := s.nodes[id]
	for i := range n.deformation.base.Vertices {
		n.deformation.base.Vertices[i].Color = color.RGBA{220, 80, 40, 255}
	}
	if texture {
		tex := testTexture(t, color.RGBA{40, 200, 130, 255})
		n.deformation.base.UV = []UV{{0, 0}, {1, 0}, {0, 1}}
		n.deformation.base.Material = &Material{Texture: tex}
		n.geometry.Material = n.deformation.base.Material
	}
	n.deformation.weights = nil
	check(t, s.SetMorphWeights(id, []float32{.25}))
	check(t, s.SetTransform(joint, nt3dmath.Translation(nt3dmath.Vec3{X: -.5, Y: -.5}).Mul(nt3dmath.RotationZ(.4))))
	floor := quad(3, -.2, color.RGBA{180, 180, 180, 255}, nil, false)
	_, err := s.SetGeometry("floor", floor)
	check(t, err)
	f := frame(t, s)
	f.Lighting = Lighting{Ambient: DefaultLighting().Ambient, Lights: []Light{{Direction: nt3dmath.Vec3{X: 1, Y: 1, Z: 2}, Color: color.RGBA{255, 255, 255, 255}, Intensity: 1, Shadows: true}}}
	gpu := &gpuRenderer{}
	defer gpu.Close()
	img, err := gpu.Render(f)
	check(t, err)
	// Render the identical CPU pose without GPU skin metadata on the same backend:
	// image differences then measure deformation, not rasterizer edge conventions.
	reference := f
	reference.Geometry = append([]Geometry(nil), f.Geometry...)
	reference.Geometry[0].posed = nil
	other := &gpuRenderer{}
	defer other.Close()
	want, err := other.Render(reference)
	check(t, err)
	diff := 0
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			rr, gg, bb, _ := want.At(x, y).RGBA()
			if absInt(int(r)-int(rr))+absInt(int(g)-int(gg))+absInt(int(b)-int(bb)) > 1500 {
				diff++
			}
		}
	}
	if diff > 10 {
		t.Fatalf("GPU and CPU skin differ at %d pixels", diff)
	}
	uploads := gpu.uploads
	check(t, s.SetTransform(joint, nt3dmath.Translation(nt3dmath.Vec3{X: .2})))
	_, err = gpu.Render(frame(t, s))
	check(t, err)
	if gpu.uploads != uploads {
		t.Fatal("joint motion reuploaded mesh")
	}
	_, err = gpu.Render(f)
	check(t, err) // Old snapshots restore the old palette.
}
func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
