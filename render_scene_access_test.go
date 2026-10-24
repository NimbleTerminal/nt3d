//go:build webgpu

package nt3d

import (
	"image"
	"image/color"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"testing"
)

func TestGPUSceneAccess(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	for _, size := range [][2]int{{64, 48}, {91, 37}, {64, 48}} {
		for _, linear := range []bool{true, false} {
			for _, ss := range []int{1, 2} {
				f := transmissionFrame()
				f.Width, f.Height = size[0], size[1]
				f.Linear = linear
				f.Supersample = ss
				f.Geometry[1].Material.Absorption = nt3dmath.Vec3{X: 2, Y: 1, Z: .2}
				p := Geometry{Points: []Point{{Position: nt3dmath.Vec3{Z: .25}, Radius: 6, Color: color.RGBA{255, 255, 255, 255}}}, PointStyle: PointStyle{Blend: AlphaBlend, SoftDistance: .1}}
				p.Bounds = GeometryBounds(p)
				f.Geometry = append(f.Geometry, p)
				img, err := r.Render(f)
				check(t, err)
				soft := softwareRender(f)
				for _, xy := range [][2]int{{f.Width / 2, f.Height / 2}, {f.Width/2 + 12, f.Height / 2}, {1, 1}} {
					a, b := at(img, xy[0], xy[1]), at(soft, xy[0], xy[1])
					if !closeRGB(a, b, 3) {
						t.Fatalf("size %v linear %v ss %d at %v GPU %v software %v", size, linear, ss, xy, a, b)
					}
				}
			}
		}
	}
	// Returning to a frame without effects must not sample the old snapshot.
	f := flatFrame(32, 32, quad(1, .5, color.RGBA{30, 70, 100, 255}, nil, false))
	f.Revision = 1
	img, err := r.Render(f)
	check(t, err)
	if !closeRGB(at(img, 16, 16), at(softwareRender(f), 16, 16), 2) {
		t.Fatal("stale snapshot", at(img, 16, 16), at(softwareRender(f), 16, 16))
	}
	// Return to the previous capture size after a no-effects resize: bindings
	// must follow the recreated target views, not just their dimensions.
	for _, size := range [][2]int{{64, 48}, {1, 1}} {
		f = transmissionFrame()
		f.Width, f.Height = size[0], size[1]
		f.Revision = 2
		img, err = r.Render(f)
		check(t, err)
		if !closeRGB(at(img, f.Width/2, f.Height/2), at(softwareRender(f), f.Width/2, f.Height/2), 3) {
			t.Fatal("capture retained released target views")
		}
	}
}

func TestGPURefractionAndForeground(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	for i, foreground := range []bool{false, true} {
		f := refractionFrame(foreground)
		f.Revision = uint64(i + 1)
		img, err := r.Render(f)
		check(t, err)
		if a, b := at(img, 29, 24), at(softwareRender(f), 29, 24); !closeRGB(a, b, 2) {
			t.Fatal("distorted lookup mismatch", foreground, a, b)
		}
	}
	// Perspective and orthographic cameras reconstruct world-space thickness.
	for _, projection := range []Projection{Perspective, Orthographic} {
		s := NewScene()
		_, err := s.SetGeometry("floor", quad(3, -1, color.RGBA{210, 160, 100, 255}, nil, false))
		check(t, err)
		_, err = s.SetGeometry("water", quad(2, 0, color.RGBA{20, 60, 80, 255}, &Material{Transmission: .9, Absorption: nt3dmath.Vec3{X: 1, Y: .3, Z: .1}, Refraction: .02}, false))
		check(t, err)
		c := DefaultCamera()
		c.Projection = projection
		c.Alpha = 60
		f, err := s.Frame(c, 80, 60)
		check(t, err)
		f.Lighting = even
		f.Lighting.Lights = []Light{{Direction: nt3dmath.Vec3{X: 1, Z: 1}, Intensity: .1, Color: color.RGBA{255, 255, 255, 255}, Shadows: true}}
		img, err := r.Render(f)
		check(t, err)
		if a, b := at(img, 40, 30), at(softwareRender(f), 40, 30); !closeRGB(a, b, 3) {
			t.Fatal("projection mismatch", projection, a, b)
		}
	}
}

func TestGPUSoftParticleStyles(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{255, 255, 255, 255})
	tex, err := NewTexture(img)
	check(t, err)
	r := &gpuRenderer{}
	defer r.Close()
	for i, style := range []PointStyle{
		{Blend: AlphaBlend, SoftDistance: .1},
		{Blend: AdditiveBlend, Shape: SoftPoint, SoftDistance: .1},
		{Blend: AlphaBlend, Texture: tex, SoftDistance: .1},
		{Blend: AdditiveBlend, Texture: tex, SoftDistance: .1},
		{Blend: AlphaBlend, Units: WorldUnits, Shape: GaussianPoint, SoftDistance: .1},
	} {
		p := Point{Position: nt3dmath.Vec3{Z: .75}, Radius: 5, Color: color.RGBA{200, 150, 100, 255}, GaussianAxes: [3]nt3dmath.Vec3{{X: .2}, {Y: .2}, {Z: .1}}}
		f := flatFrame(64, 48, quad(1, .8, color.RGBA{20, 30, 40, 255}, nil, false), styled(style, p))
		f.Revision = uint64(i + 1)
		gpu, err := r.Render(f)
		check(t, err)
		if a, b := at(gpu, 32, 24), at(softwareRender(f), 32, 24); !closeRGB(a, b, 3) {
			t.Fatal("soft style mismatch", style, a, b)
		}
	}
}
