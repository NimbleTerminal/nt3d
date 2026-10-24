package nt3d

import (
	"image/color"
	"math"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"slices"
	"testing"
)

func transmissionFrame() Frame {
	return flatFrame(64, 48, quad(1, .8, color.RGBA{240, 180, 120, 255}, nil, false), quad(.8, .3, color.RGBA{20, 60, 80, 255}, &Material{Transmission: 1}, false))
}
func TestSceneTransmissionAndAttenuation(t *testing.T) {
	f := transmissionFrame()
	got := at(softwareRender(f), 32, 24)
	if !closeRGB(got, color.RGBA{240, 180, 120, 255}, 1) {
		t.Fatal("zero extinction must transmit floor", got)
	}
	f.Geometry[1].Material.Absorption = nt3dmath.Vec3{X: 2, Y: 1, Z: .2}
	got = at(softwareRender(f), 32, 24)
	want := color.RGBA{A: 255}
	values := []*uint8{&want.R, &want.G, &want.B}
	for k, a := range []float64{2, 1, .2} {
		base := []uint8{20, 60, 80}[k]
		back := []uint8{240, 180, 120}[k]
		v := float64(srgbDecode[base]) + (float64(srgbDecode[back])-float64(srgbDecode[base]))*math.Exp(-a*.5)
		*values[k] = encode8(float32(v))
	}
	if !closeRGB(got, want, 1) {
		t.Fatal("Beer-Lambert mismatch", got, want)
	}
	f.Geometry[0], f.Geometry[1] = f.Geometry[1], f.Geometry[0]
	if other := at(softwareRender(f), 32, 24); other != got {
		t.Fatal("submission order changed transmission", other, got)
	}
	_, depth := softwareRenderDepth(f)
	if math.Abs(float64(depth[24*64+32]-.3)) > 1e-5 {
		t.Fatal("transmission did not write depth")
	}
}
func TestSoftParticlesUseTransmissiveDepth(t *testing.T) {
	f := transmissionFrame()
	particle := Geometry{Points: []Point{{Position: nt3dmath.Vec3{Z: .25}, Radius: 5, Color: color.RGBA{255, 255, 255, 255}}}, PointStyle: PointStyle{Blend: AlphaBlend, SoftDistance: .1}}
	particle.Bounds = GeometryBounds(particle)
	f.Geometry = append(f.Geometry, particle)
	soft := at(softwareRender(f), 32, 24)
	f.Geometry[2].PointStyle.SoftDistance = 0
	hard := at(softwareRender(f), 32, 24)
	if soft.R >= hard.R || soft.B >= hard.B || soft.B <= 120 {
		t.Fatal("particle did not fade at water", soft, hard)
	}
	f.Geometry[2].PointStyle.SoftDistance = .1
	f.Geometry[2].Points[0].Position.Z = .3
	if got := at(softwareRender(f), 32, 24); !closeRGB(got, color.RGBA{240, 180, 120, 255}, 1) {
		t.Fatal("contact must fade to zero", got)
	}
}
func TestSceneInverseAndGap(t *testing.T) {
	for _, projection := range []Projection{Perspective, Orthographic} {
		s := NewScene()
		_, err := s.SetGeometry("box", Geometry{Boxes: []Box{{Size: nt3dmath.Vec3{X: 1, Y: 1, Z: 1}}}})
		check(t, err)
		c := DefaultCamera()
		c.Projection = projection
		f, err := s.Frame(c, 80, 60)
		check(t, err)
		inverse, ok := inverseProjection(f.Matrix)
		if !ok {
			t.Fatal("no inverse")
		}
		p := nt3dmath.Vec3{X: .2, Y: .3, Z: .4}
		x, y, z, _ := f.Matrix.Project(p, 80, 60)
		q := sceneWorld(inverse, x, y, z, 80, 60)
		if q.Sub(p).Dot(q.Sub(p)) > 1e-6 {
			t.Fatal("depth reconstruction", projection, p, q)
		}
	}
	if _, ok := inverseProjection(nt3dmath.Mat4{}); ok {
		t.Fatal("singular inverse")
	}
}
func TestOpticsValidation(t *testing.T) {
	for _, m := range []Material{{Transmission: -1}, {Transmission: 1, Transparent: true}, {Transmission: 1, Unlit: true}, {Refraction: 2}, {Absorption: nt3dmath.Vec3{X: -1}}, {Transmission: float32(math.NaN())}} {
		s := NewScene()
		if _, err := s.SetGeometry("bad", quad(1, .3, color.RGBA{}, &m, false)); err == nil {
			t.Fatal("accepted invalid optics", m)
		}
	}
	if err := validPointStyle(PointStyle{SoftDistance: -1}); err == nil {
		t.Fatal("accepted negative fade distance")
	}
}

func refractionFrame(foreground bool) Frame {
	floor := quad(1, .8, color.RGBA{0, 0, 255, 255}, nil, false)
	if !foreground {
		for i := range floor.Vertices {
			if floor.Vertices[i].Position.X < 0 {
				floor.Vertices[i].Color = color.RGBA{255, 0, 0, 255}
			}
		}
	}
	water := quad(.95, .3, color.RGBA{0, 20, 30, 255}, &Material{Transmission: 1, Refraction: .2}, false)
	for i := range water.Vertices {
		water.Vertices[i].Normal = nt3dmath.Vec3{X: .8, Z: .6}
	}
	f := flatFrame(64, 48, floor, water)
	if foreground {
		occluder := quad(.5, .1, color.RGBA{255, 0, 0, 255}, nil, false)
		for i := range occluder.Vertices {
			occluder.Vertices[i].Position.X += .5
		}
		occluder.Bounds = GeometryBounds(occluder)
		f.Geometry = append(f.Geometry, occluder)
	}
	return f
}
func TestRefractionDistortionAndForegroundRejection(t *testing.T) {
	f := refractionFrame(false)
	distorted := at(softwareRender(f), 29, 24)
	f.Geometry[1].Material.Refraction = 0
	clear := at(softwareRender(f), 29, 24)
	if distorted.R >= clear.R || distorted.B <= clear.B {
		t.Fatal("normal did not distort the scene", distorted, clear)
	}
	f = refractionFrame(true)
	if got := at(softwareRender(f), 29, 24); got.R != 0 || got.B != 255 {
		t.Fatal("foreground leaked into water", got)
	}
}

func TestTransmissionDoesNotCastOpaqueShadow(t *testing.T) {
	f := blendScene(t, nil, quad(1, 0, color.RGBA{255, 255, 255, 255}, &Material{Transmission: 1}, false))
	f.Lighting = Lighting{Lights: []Light{{Direction: nt3dmath.Vec3{X: 1, Z: 1}, Shadows: true}}}
	a := buildShadowMap(&f, f.drawn(1), 64)
	f.Geometry[1].Material = &Material{Transparent: true}
	b := buildShadowMap(&f, f.drawn(1), 64)
	if a == nil || b == nil || !slices.Equal(a.depth, b.depth) {
		t.Fatal("transmission cast an opaque shadow")
	}
}
