//go:build webgpu

package nt3d

import (
	"nimbleterminal.dev/nt3d/math"
	"testing"
)

func TestGPUSpritesMatchSoftware(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	tex := spriteAtlas(t)
	for _, linear := range []bool{false, true} {
		for _, ss := range []int{1, 2} {
			for _, units := range []PointUnits{PixelUnits, WorldUnits} {
				for _, blend := range []PointBlend{OpaqueBlend, AlphaBlend, AdditiveBlend} {
					radius := float32(12)
					if units == WorldUnits {
						radius = .375
					}
					style := PointStyle{Texture: tex, Columns: 2, Rows: 2, Units: units, Blend: blend}
					g := styled(style, Point{Position: nt3dmath.Vec3{X: -.25, Z: .5}, Radius: radius, Color: white, Frame: 2}, Point{Position: nt3dmath.Vec3{X: .25, Z: .4}, Radius: radius, Color: white, Frame: 1})
					f := flatFrame(64, 64, g, styled(PointStyle{}, Point{Position: nt3dmath.Vec3{Z: .25}, Radius: 3, Color: blue}))
					f.Linear, f.Supersample = linear, ss
					// Manual frames require a fresh revision when their geometry changes.
					f.Revision = nextRevision.Add(1)
					gpu, err := r.Render(f)
					check(t, err)
					sw := softwareRender(f)
					off := 0
					for y := range 64 {
						for x := range 64 {
							if !closeRGB(at(gpu, x, y), at(sw, x, y), 4) {
								off++
							}
						}
					}
					if off > 10 {
						t.Errorf("linear %v ss %d units %d blend %d: %d pixels differ, center gpu %v software %v", linear, ss, units, blend, off, at(gpu, 24, 32), at(sw, 24, 32))
					}
				}
			}
		}
	}
}

func TestGPUPointUpdatesReuseBuffersAndReplay(t *testing.T) {
	s := NewScene()
	style := PointStyle{Texture: spriteAtlas(t), Columns: 2, Rows: 2, Blend: AlphaBlend}
	points := []Point{{Radius: 12, Color: white}, {Radius: 12, Color: white, Position: nt3dmath.Vec3{X: .2}, Frame: 1}}
	id, err := s.SetGeometry("points", styled(style, points...))
	check(t, err)
	r := &gpuRenderer{}
	defer r.Close()
	first := frame(t, s)
	_, err = r.Render(first)
	check(t, err)
	node := r.nodes[0]
	buffer := node.batches[0].buffer
	sprite := node.sprite.buffer
	uploads := r.uploads
	points[0].Frame = 2
	check(t, s.UpdateNodePoints(id, 0, points[:1], pointBounds()))
	second := frame(t, s)
	for _, f := range []Frame{second, first, second} {
		gpu, err := r.Render(f)
		check(t, err)
		sw := softwareRender(f)
		if r.uploads != uploads || r.nodes[0].batches[0].buffer != buffer || r.nodes[0].sprite.buffer != sprite {
			t.Fatal("rebuilt buffers")
		}
		if !closeRGB(at(gpu, f.Width/2, f.Height/2), at(sw, f.Width/2, f.Height/2), 4) {
			t.Fatal("stale points/sort/atlas on replay")
		}
	}
	// Skipped updates require a full upload, not just the last changed range.
	points[0].Frame = 3
	check(t, s.UpdateNodePoints(id, 0, points[:1], pointBounds()))
	points[1].Frame = 2
	check(t, s.UpdateNodePoints(id, 1, points[1:], pointBounds()))
	f := frame(t, s)
	gpu, err := r.Render(f)
	check(t, err)
	sw := softwareRender(f)
	if !closeRGB(at(gpu, f.Width/2, f.Height/2), at(sw, f.Width/2, f.Height/2), 4) {
		t.Fatal("skipped update lost")
	}
}

func TestGPUPointMotionResortsAlpha(t *testing.T) {
	s := NewScene()
	p := []Point{{Radius: 12, Color: blue}, {Radius: 12, Color: white}}
	p[0].Color.A = 128
	p[1].Color.A = 128
	id, err := s.SetGeometry("points", styled(PointStyle{Blend: AlphaBlend}, p...))
	check(t, err)
	r := &gpuRenderer{}
	defer r.Close()
	camera := DefaultCamera()
	eye, _, _, _ := camera.basis()
	for _, depth := range []float32{.25, -.25, .25} {
		p[0].Position = eye.Scale(depth)
		check(t, s.SetNodePoints(id, p, pointBounds()))
		f, err := s.Frame(camera, 64, 64)
		check(t, err)
		gpu, err := r.Render(f)
		check(t, err)
		if a, b := at(gpu, 32, 32), at(softwareRender(f), 32, 32); !closeRGB(a, b, 4) {
			t.Fatalf("depth %v: GPU %v software %v", depth, a, b)
		}
	}
}
