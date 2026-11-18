//go:build webgpu

package nt3d

import (
	"image/color"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// The GPU agrees with software about which of two coincident surfaces shows.
func TestGPUDrawOrderDecidesCoincidentSurfaces(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	for _, c := range []struct {
		redLayer int
		want     color.RGBA
	}{{0, color.RGBA{0, 0, 255, 255}}, {1, color.RGBA{255, 0, 0, 255}}} {
		img, err := r.Render(coincident(t, c.redLayer))
		check(t, err)
		if got := at(img, 32, 32); got != c.want {
			t.Fatalf("with red in layer %d the center is %v, want %v", c.redLayer, got, c.want)
		}
	}
}

// The GPU keeps the frame's order between a vertex-colored mesh and a
// textured one, which it draws with different pipelines, and between
// instances of a shared mesh and a mesh drawn between them.
func TestGPUDrawOrderHoldsAcrossKindsAndInstances(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	red, blue := color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255}
	for name, c := range map[string]struct {
		frame Frame
		want  color.RGBA
	}{
		"textured added last":             {coincidentMixed(t, 0), blue},
		"vertex-colored in a later layer": {coincidentMixed(t, 1), red},
		"red, blue, red":                  {coincidentShared(t), red},
	} {
		img, err := r.Render(c.frame)
		check(t, err)
		if got := at(img, 32, 32); got != c.want {
			t.Errorf("%s: the center is %v, want %v", name, got, c.want)
		}
	}
}

// Instances of a shared mesh are still drawn together when what is drawn
// between them lies somewhere else, where no order between them can show:
// only a draw that overlaps one keeps it out of the group.
func TestGPUInstancesStayTogetherAcrossDistantDraws(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	ball := func(c color.RGBA) Geometry {
		g, err := BuildSphere(Sphere{Radius: .3, Segments: 12, Rings: 6, Color: c, Material: &Material{Shading: ShadeUnlit}})
		check(t, err)
		return g
	}
	for name, c := range map[string]struct {
		blueAt nt3dmath.Vec3
		draws  int
	}{
		"blue apart":        {nt3dmath.Vec3{Y: 3}, 2}, // Red twice in one draw, and blue.
		"blue on a red one": {nt3dmath.Vec3{X: 1}, 3}, // The second red must follow blue.
	} {
		s := NewScene()
		red, err := NewSharedGeometry(ball(color.RGBA{255, 0, 0, 255}))
		check(t, err)
		place := func(name string, at nt3dmath.Vec3, shared bool) {
			var id NodeID
			var err error
			if shared {
				id, err = s.SetShared(name, red)
			} else {
				id, err = s.SetGeometry(name, ball(color.RGBA{0, 0, 255, 255}))
			}
			check(t, err)
			check(t, s.SetTransform(id, nt3dmath.Translation(at)))
		}
		place("red 1", nt3dmath.Vec3{X: -1}, true)
		place("blue", c.blueAt, false)
		place("red 2", nt3dmath.Vec3{X: 1}, true)
		f, err := s.StillFrame(DefaultCamera(), 64, 64)
		check(t, err)
		f.Cull = false
		_, err = r.Render(f)
		check(t, err)
		if len(r.draws) != c.draws {
			t.Errorf("%s: %d draws, want %d", name, len(r.draws), c.draws)
		}
	}
}
