// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image"
	"image/color"
	"math"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"testing"
)

func probeBounds(lo, hi nt3dmath.Vec3) nt3dmath.AABB {
	var b nt3dmath.AABB
	b.Include(lo)
	b.Include(hi)
	return b
}
func testProbe(t *testing.T, c color.NRGBA) ReflectionProbe {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 16, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 16; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	e, err := NewEnvironment(img)
	check(t, err)
	return ReflectionProbe{Environment: e, Bounds: probeBounds(nt3dmath.Vec3{X: -2, Y: -2, Z: -2}, nt3dmath.Vec3{X: 2, Y: 2, Z: 2})}
}

func TestReflectionProbeBoxProjection(t *testing.T) {
	probe := testProbe(t, color.NRGBA{255, 0, 0, 255})
	for _, tc := range []struct {
		name       string
		p, d, want nt3dmath.Vec3
		hit        bool
	}{
		{"capture position", nt3dmath.Vec3{}, nt3dmath.Vec3{Z: 1}, nt3dmath.Vec3{Z: 1}, true},
		{"off center", nt3dmath.Vec3{X: 1}, nt3dmath.Vec3{Z: 1}, nt3dmath.Vec3{X: 1, Z: 2}.Normalize(), true},
		{"negative direction", nt3dmath.Vec3{X: 1}, nt3dmath.Vec3{Z: -1}, nt3dmath.Vec3{X: 1, Z: -2}.Normalize(), true},
		{"boundary outward", nt3dmath.Vec3{X: 2}, nt3dmath.Vec3{X: 1}, nt3dmath.Vec3{X: 1}, true},
		{"boundary inward", nt3dmath.Vec3{X: 2}, nt3dmath.Vec3{X: -1}, nt3dmath.Vec3{X: -1}, true},
		{"outside", nt3dmath.Vec3{X: 2.01}, nt3dmath.Vec3{Z: 1}, nt3dmath.Vec3{Z: 1}, false},
		{"zero direction", nt3dmath.Vec3{}, nt3dmath.Vec3{}, nt3dmath.Vec3{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, hit := boxProjectedDirection(probe, tc.p, tc.d)
			if hit != tc.hit || got.Sub(tc.want).Dot(got.Sub(tc.want)) > 1e-12 {
				t.Fatalf("got %v,%v want %v,%v", got, hit, tc.want, tc.hit)
			}
		})
	}
	probe.Position = nt3dmath.Vec3{X: -1}
	got, _ := boxProjectedDirection(probe, nt3dmath.Vec3{X: 1}, nt3dmath.Vec3{Z: 1})
	if got.Dot((nt3dmath.Vec3{X: 2, Z: 2}).Normalize()) < .99999 {
		t.Fatal("capture position ignored")
	}
}

func TestReflectionProbeValidationAndSnapshots(t *testing.T) {
	p := testProbe(t, color.NRGBA{255, 0, 0, 255})
	s := NewScene()
	probes := []ReflectionProbe{p}
	check(t, s.SetReflectionProbes(probes))
	probes[0].Strength = 3
	snapshot := s.ReflectionProbes()
	snapshot[0].Strength = 4
	if s.ReflectionProbes()[0].Strength != 0 {
		t.Fatal("scene aliases caller storage")
	}
	f, err := s.Frame(DefaultCamera(), 80, 80)
	check(t, err)
	f.ReflectionProbes[0].Strength = 5
	if s.ReflectionProbes()[0].Strength != 0 {
		t.Fatal("frame aliases scene")
	}
	for _, mutate := range []func(*ReflectionProbe){
		func(p *ReflectionProbe) { p.Environment = nil },
		func(p *ReflectionProbe) { p.Strength = -1 },
		func(p *ReflectionProbe) { p.Strength = float32(math.NaN()) },
		func(p *ReflectionProbe) { p.Position.X = 2 },
		func(p *ReflectionProbe) { p.Bounds = nt3dmath.AABB{} },
		func(p *ReflectionProbe) { p.Bounds.Max.Z = p.Bounds.Min.Z },
		func(p *ReflectionProbe) { p.Position.Y = float32(math.Inf(1)) },
	} {
		bad := p
		mutate(&bad)
		if s.SetReflectionProbes([]ReflectionProbe{bad}) == nil {
			t.Fatal("accepted invalid probe")
		}
		if s.ReflectionProbes()[0] != p {
			t.Fatal("invalid edit mutated scene")
		}
		f.ReflectionProbes = []ReflectionProbe{bad}
		if validateFrame(f) == nil {
			t.Fatal("frame accepted invalid probe")
		}
	}
	if s.SetReflectionProbes(make([]ReflectionProbe, MaxReflectionProbes+1)) == nil {
		t.Fatal("accepted too many probes")
	}
	old, err := s.Frame(DefaultCamera(), 80, 80)
	check(t, err)
	check(t, s.SetReflectionProbes(nil))
	if len(old.ReflectionProbes) != 1 {
		t.Fatal("clearing probes changed old frame")
	}
	// Scene world coordinates are converted once into camera-relative space.
	p.Position.X += 1000
	p.Bounds.Min.X += 1000
	p.Bounds.Max.X += 1000
	check(t, s.SetReflectionProbes([]ReflectionProbe{p}))
	cam := DefaultCamera()
	cam.Origin = [3]float64{1000, 0, 0}
	relative, err := s.Frame(cam, 80, 80)
	check(t, err)
	if relative.ReflectionProbes[0].Position.X != 0 || relative.ReflectionProbes[0].Bounds.Min.X != -2 {
		t.Fatal("probe did not follow frame origin")
	}
}

func TestReflectionProbeCoverageAndFallback(t *testing.T) {
	red := testProbe(t, color.NRGBA{255, 0, 0, 255})
	blue := testProbe(t, color.NRGBA{0, 0, 255, 255})
	f := Frame{ReflectionProbes: []ReflectionProbe{red, blue}, Environment: blue.Environment, Linear: true}
	rig := lightRig{sky: [3]float32{1, 1, 1}, ground: [3]float32{1, 1, 1}}
	point := nt3dmath.Vec3{}
	ray := nt3dmath.Vec3{Z: 1}
	got := rig.indirectSpecular(&f, point, ray, .5, 1)
	if got.coverage != 1 || got.radiance != ([3]float32{1, 0, 0}) {
		t.Fatalf("priority or fallback doubled: %+v", got)
	}
	f.ReflectionProbes[0].Strength = 2
	got = rig.indirectSpecular(&f, point, ray, .5, .25)
	if got.radiance != ([3]float32{.5, 0, 0}) || got.coverage != 1 {
		t.Fatalf("strength/occlusion: %+v", got)
	}
	f.ReflectionProbes[0] = testProbe(t, color.NRGBA{0, 0, 0, 255})
	got = rig.indirectSpecular(&f, point, ray, .5, 1)
	if got.radiance != ([3]float32{}) || got.coverage != 1 {
		t.Fatal("black probe leaked fallback")
	}
	outside := nt3dmath.Vec3{X: 3}
	got = rig.indirectSpecular(&f, outside, ray, .5, 1)
	if got.radiance != ([3]float32{1, 1, 2}) {
		t.Fatalf("outside fallback: %+v", got)
	}
	// With no global environment, a classic material outside every box keeps
	// its base color instead of blending it with a black reflection miss.
	f.Environment = nil
	rgb := [3]float32{50, 60, 70}
	want := rgb
	reflectSceneEnvironment(&rgb, &f, outside, 1, ray, ray, 1)
	if rgb != want {
		t.Fatalf("classic miss darkened surface: %v", rgb)
	}
}

func TestReflectionProbeHDRAndDiffuseIsolation(t *testing.T) {
	data := encodeRadianceHDR(16, 8, true, 1, "", func(x, y int) [3]float64 { return [3]float64{4, 2, 1} })
	env, err := NewHDREnvironment(data)
	check(t, err)
	probe := testProbe(t, color.NRGBA{0, 0, 0, 255})
	probe.Environment = env
	f := Frame{ReflectionProbes: []ReflectionProbe{probe}, Linear: true}
	got := localReflection(&f, nt3dmath.Vec3{}, nt3dmath.Vec3{Z: 1}, .5, true)
	for i, want := range [3]float32{4, 2, 1} {
		if abs(got.radiance[i]-want) > .03 {
			t.Fatalf("HDR probe clamped or decoded incorrectly: %+v", got)
		}
	}
	before := newRig(&Frame{})
	after := newRig(&f)
	if before.indirectDiffuse(nt3dmath.Vec3{Z: 1}, 1) != after.indirectDiffuse(nt3dmath.Vec3{Z: 1}, 1) {
		t.Fatal("probe changed diffuse lighting")
	}
}

// A reflection probe far from the camera's Origin cannot keep its box once
// it is rebased into the frame's float32 coordinates: its bounds and capture
// position round to the same number. Such a probe lights nothing the frame
// can show, so it is left out of the frame, which still draws.
func TestReflectionProbeFarFromOriginIsLeftOut(t *testing.T) {
	s := NewScene()
	ball, err := BuildSphere(Sphere{Radius: .5, Segments: 12, Rings: 6, Color: color.RGBA{200, 200, 200, 255}})
	check(t, err)
	id, err := s.SetGeometry("ball", ball)
	check(t, err)
	far := [3]float64{1e9, 0, 0}
	check(t, s.SetNodeOrigin(id, far))
	near, away := testProbe(t, color.NRGBA{255, 0, 0, 255}), testProbe(t, color.NRGBA{0, 255, 0, 255})
	near.Position, near.Bounds = nt3dmath.Vec3{X: 1e9}, probeBounds(nt3dmath.Vec3{X: 1e9 - 128, Y: -2, Z: -2}, nt3dmath.Vec3{X: 1e9 + 128, Y: 2, Z: 2})
	check(t, s.SetReflectionProbes([]ReflectionProbe{away, near})) // away spans X -2..2, around the world's origin.

	camera := DefaultCamera()
	camera.Origin = far
	f, err := s.StillFrame(camera, 32, 32)
	check(t, err)
	if err := validateFrame(f); err != nil {
		t.Fatalf("a far probe keeps the whole frame from drawing: %v", err)
	}
	if len(f.ReflectionProbes) != 1 || f.ReflectionProbes[0].Environment != near.Environment {
		t.Fatalf("want only the probe around the camera's origin, got %d probes", len(f.ReflectionProbes))
	}
	if _, err := NewSoftwareRenderer().Render(f); err != nil {
		t.Fatal(err)
	}
}
