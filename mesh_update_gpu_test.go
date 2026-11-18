//go:build webgpu

// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image"
	"image/color"
	"slices"
	"testing"

	"github.com/gogpu/gputypes"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func TestGPUMeshUpdatesInPlaceAndSnapshotReplay(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()

	s := NewScene()
	nmap := testNormalMap(t, color.NRGBA{128, 128, 255, 255})
	flowImg := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			flowImg.SetNRGBA(x, y, color.NRGBA{180, 90, 0, 255})
		}
	}
	flow, err := NewFlowMap(flowImg)
	check(t, err)

	base, err := BuildHeightfield(Heightfield{
		NX: 8, NY: 8,
		MinX: -.9, MaxX: .9, MinY: -.9, MaxY: .9,
		Sampler:  func(x, y float32) float32 { return 0 },
		Color:    color.RGBA{40, 150, 210, 255},
		Material: &Material{Normal: NormalStyle{Map: nmap, Strength: 1, Flow: FlowStyle{Map: flow, Strength: .3}}, Phong: PhongStyle{Specular: .4, Shininess: 32}},
	})
	check(t, err)
	id, err := s.SetGeometry("water", base)
	check(t, err)

	cam := Camera{Elevation: 38, Azimuth: 0, Distance: 2.4, Projection: Perspective}
	f0, err := s.Frame(cam, 80, 80)
	check(t, err)
	got0, err := r.Render(f0)
	check(t, err)
	uploads0 := r.uploads

	// Displace vertices & tangents across multiple frames; GPU reuses the single
	// uploaded buffer in place without allocating new gpuGeometry entries.
	bounds := meshTestBounds()
	verts := slices.Clone(f0.Draws[0].Geometry.Vertices)
	tans := slices.Clone(f0.Draws[0].Geometry.Tangents)
	for step := 1; step <= 3; step++ {
		for i := range verts {
			verts[i].Position.Z = float32(step) * .18 * (1 - abs(verts[i].Position.X))
			verts[i].Normal = nt3dmath.Vec3{X: .2 * float32(step), Z: 1}.Normalize()
			tans[i] = Tangent{Direction: nt3dmath.Vec3{X: 1, Z: -.15 * float32(step)}.Normalize(), W: -1}
		}
		check(t, s.SetNodeVerticesWithTangents(id, verts, tans, bounds))
		fi, err := s.Frame(cam, 80, 80)
		check(t, err)
		got, err := r.Render(fi)
		check(t, err)
		if r.uploads != uploads0 {
			t.Fatalf("step %d re-uploaded mesh buffer (%d != %d)", step, r.uploads, uploads0)
		}
		want := softwareRender(fi)
		if !closeRGB(at(got, 40, 40), at(want, 40, 40), 6) {
			t.Fatalf("step %d GPU/SW mismatch: %v != %v", step, at(got, 40, 40), at(want, 40, 40))
		}
	}

	// Re-rendering saved f0 restores the original flat vertices cleanly.
	replay0, err := r.Render(f0)
	check(t, err)
	if !closeRGB(at(replay0, 40, 40), at(got0, 40, 40), 2) {
		t.Fatalf("saved snapshot replay changed: %v != %v", at(replay0, 40, 40), at(got0, 40, 40))
	}
}

func TestGPUMeshUpdatesInvalidateCreasesAcrossSessionViews(t *testing.T) {
	_, _, dev := newTestHostDevice(t)
	sess, err := NewGPUSession(dev)
	check(t, err)
	defer sess.Close()
	v1, err := sess.NewView()
	check(t, err)
	defer v1.Close()
	v2, err := sess.NewView()
	check(t, err)
	defer v2.Close()

	s := NewScene()
	cube, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 1, Y: 1, Z: 1}, Color: color.RGBA{220, 220, 220, 255}})
	check(t, err)
	cube.Outline = OutlineStyle{Width: 2, Crease: 30, Color: color.RGBA{10, 10, 10, 255}}
	id, err := s.SetGeometry("box", cube)
	check(t, err)

	cam := Camera{Elevation: 32, Azimuth: 38, Distance: 3.0, Projection: Perspective}
	f0, err := s.Frame(cam, 80, 80)
	check(t, err)
	format := gputypes.TextureFormatRGBA8Unorm
	tex1, view1 := createHostTarget(t, dev, 80, 80, format)
	tex2, view2 := createHostTarget(t, dev, 80, 80, format)

	check(t, v1.Render(f0, GPUTarget{Color: view1, Format: format, Width: 80, Height: 80}))

	moved := slices.Clone(f0.Draws[0].Geometry.Vertices)
	for i := range moved {
		moved[i].Position.Z += .9
	}
	check(t, s.SetNodeVertices(id, moved, meshTestBounds()))
	f1, err := s.Frame(cam, 80, 80)
	check(t, err)
	check(t, v2.Render(f1, GPUTarget{Color: view2, Format: format, Width: 80, Height: 80}))
	_ = tex2
	check(t, v1.Render(f1, GPUTarget{Color: view1, Format: format, Width: 80, Height: 80}))
	got1 := readHostTarget(t, dev, tex1, 80, 80, format, true)
	want1 := softwareRender(f1)
	if !closeRGB(at(got1, 40, 68), f1.Background, 2) {
		t.Fatalf("stale crease lines remained in lower frame after SetNodeVertices: %v", at(got1, 40, 68))
	}
	off := 0
	for y := range 80 {
		for x := range 80 {
			if !closeRGB(at(got1, x, y), at(want1, x, y), 12) {
				off++
			}
		}
	}
	if off > 120 {
		t.Fatalf("session view mismatch after SetNodeVertices: %d pixels differ", off)
	}
}

func TestGPUSetMaterialSharedGeometryParity(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()

	s := NewScene()
	s.SetEnvironment(GradientEnvironment(color.RGBA{230, 120, 70, 255}, color.RGBA{180, 200, 230, 255}, color.RGBA{40, 50, 70, 255}))
	cube, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: .7, Y: .7, Z: .7}, Color: color.RGBA{60, 160, 230, 255}})
	check(t, err)
	sg, err := NewSharedGeometry(cube)
	check(t, err)

	backID, err := s.SetShared("back", sg)
	check(t, err)
	check(t, s.SetTransform(backID, nt3dmath.Translation(nt3dmath.Vec3{Y: .6})))
	frontID, err := s.SetShared("front", sg)
	check(t, err)
	check(t, s.SetTransform(frontID, nt3dmath.Translation(nt3dmath.Vec3{Y: -.5})))

	// Make only front transmissive while keeping SharedGeometry intact!
	check(t, s.ApplyMaterial(frontID, &Material{
		Transmission: TransmissionStyle{Amount: .85, Refraction: .02},
		Phong:        PhongStyle{Reflectivity: .08},
	}))
	check(t, s.ApplyMaterial(backID, &Material{Shading: ShadePBR, PBR: PBRStyle{Metallic: .2, Roughness: .35}}))

	cam := Camera{Elevation: 10, Azimuth: 0, Distance: 2.5, Projection: Perspective}
	f, err := s.Frame(cam, 96, 96)
	check(t, err)
	f.EnvironmentBackgroundOnly = true
	if f.Draws[0].Shared == 0 || f.Draws[0].Shared != f.Draws[1].Shared {
		t.Fatal("SharedGeometry was unnecessarily detached by instance-level ApplyMaterial")
	}

	got, err := r.Render(f)
	check(t, err)
	want := softwareRender(f)
	if !closeRGB(at(got, 48, 48), at(want, 48, 48), 8) {
		t.Fatalf("ApplyMaterial SharedGeometry transmission parity mismatch: gpu=%v sw=%v", at(got, 48, 48), at(want, 48, 48))
	}
}
