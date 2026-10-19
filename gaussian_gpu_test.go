//go:build webgpu

package nt3d

import (
	"nimbleterminal.dev/nt3d/math"
	"testing"
)

func TestGPUGaussiansMatchSoftware(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	for _, linear := range []bool{false, true} {
		for _, ss := range []int{1, 2} {
			for _, perspective := range []bool{false, true} {
				g := gaussianGeometry()
				p := g.Points[0]
				p.Position.X = .1
				p.Position.Z = .7
				p.Color = blue
				g.Points = append(g.Points, p)
				f := flatFrame(80, 80, g)
				f.Linear = linear
				f.Supersample = ss
				f.Matrix = nt3dmath.RotationZ(.7)
				if perspective {
					f.Matrix[11] = .5
					f.Matrix[15] = 1
				}
				f.Revision = nextRevision.Add(1)
				gpu, err := r.Render(f)
				check(t, err)
				sw := softwareRender(f)
				off := 0
				for y := range 80 {
					for x := range 80 {
						if !closeRGB(at(gpu, x, y), at(sw, x, y), 4) {
							off++
						}
					}
				}
				if off > 12 {
					t.Errorf("linear %v ss %d perspective %v: %d pixels differ", linear, ss, perspective, off)
				}
			}
		}
	}
}

func TestGPUGaussianTransformsUpdatesAndClipping(t *testing.T) {
	s := NewScene()
	g := gaussianGeometry()
	id, err := s.SetGeometry("splat", g)
	check(t, err)
	transform := nt3dmath.RotationZ(.6).Mul(nt3dmath.Scaling(nt3dmath.Vec3{X: 2, Y: .7, Z: 1}))
	check(t, s.SetTransform(id, transform))
	r := &gpuRenderer{}
	defer r.Close()
	first := frame(t, s)
	compare := func(f Frame) {
		t.Helper()
		gpu, err := r.Render(f)
		check(t, err)
		sw := softwareRender(f)
		off := 0
		for y := range f.Height {
			for x := range f.Width {
				if !closeRGB(at(gpu, x, y), at(sw, x, y), 4) {
					off++
				}
			}
		}
		if off > 15 {
			t.Fatalf("%d pixels differ", off)
		}
	}
	compare(first)
	g.Points[0].GaussianAxes[0] = nt3dmath.Vec3{X: .04, Y: .25}
	check(t, s.SetNodePoints(id, g.Points, GeometryBounds(g)))
	compare(frame(t, s))
	compare(first)
	// Center outside the viewport still contributes, but outside depth does not.
	for _, p := range []nt3dmath.Vec3{{X: 1.1, Z: .5}, {Z: -.1}, {Z: 1.1}} {
		g.Points[0].Position = p
		f := flatFrame(80, 80, g)
		f.Revision = nextRevision.Add(1)
		compare(f)
	}
}

func TestGPUGaussianSHMatchesSoftware(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	for degree := 0; degree <= 3; degree++ {
		for _, linear := range []bool{false, true} {
			for _, perspective := range []bool{false, true} {
				for _, dc := range []bool{false, true} {
					s := NewScene()
					g := shGaussianGeometry(degree)
					g.PointStyle.SHDCOnly = dc
					id, err := s.SetGeometry("SH", g)
					check(t, err)
					check(t, s.SetTransform(id, nt3dmath.RotationZ(.8).Mul(nt3dmath.Scaling(nt3dmath.Vec3{X: 1.7, Y: .8, Z: 1}))))
					camera := DefaultCamera()
					if perspective {
						camera.Projection = Perspective
					}
					f, err := s.Frame(camera, 96, 80)
					check(t, err)
					f.Linear = linear
					f.Supersample = 2
					gpu, err := r.Render(f)
					check(t, err)
					sw := softwareRender(f)
					off := 0
					for y := range f.Height {
						for x := range f.Width {
							if !closeRGB(at(gpu, x, y), at(sw, x, y), 4) {
								if off < 3 {
									t.Logf("at %d,%d GPU %v SW %v", x, y, at(gpu, x, y), at(sw, x, y))
								}
								off++
							}
						}
					}
					if off > 8 {
						t.Fatalf("degree %d linear %v perspective %v DC %v: %d differing pixels", degree, linear, perspective, dc, off)
					}
				}
			}
		}
	}
}
