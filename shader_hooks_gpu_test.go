// Copyright (c) 2026 Neomantra Corp.

//go:build webgpu

package nt3d

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func subdividedGridMesh(nx, ny int, span float32, c color.RGBA) Geometry {
	verts := make([]Vertex, (nx+1)*(ny+1))
	uvs := make([]UV, len(verts))
	for y := 0; y <= ny; y++ {
		v := float32(y) / float32(ny)
		py := (v - 0.5) * span
		for x := 0; x <= nx; x++ {
			u := float32(x) / float32(nx)
			px := (u - 0.5) * span
			idx := y*(nx+1) + x
			verts[idx] = Vertex{
				Position: nt3dmath.Vec3{X: px, Y: py, Z: 0},
				Normal:   nt3dmath.Vec3{Z: 1},
				Color:    c,
			}
			uvs[idx] = UV{U: u, V: v}
		}
	}
	indices := make([]uint32, 0, nx*ny*6)
	for y := 0; y < ny; y++ {
		for x := 0; x < nx; x++ {
			i0 := uint32(y*(nx+1) + x)
			i1 := i0 + 1
			i2 := i0 + uint32(nx+1)
			i3 := i2 + 1
			indices = append(indices, i0, i1, i3, i0, i3, i2)
		}
	}
	g := Geometry{Vertices: verts, Indices: indices, UV: uvs}
	g.Bounds = GeometryBounds(g)
	return g
}

func testBumpNormalMap(t *testing.T, size int) *Texture {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		v := float64(y) / float64(size)
		for x := 0; x < size; x++ {
			u := float64(x) / float64(size)
			nx := 0.35 * math.Cos(u*2*math.Pi*2)
			ny := 0.35 * math.Cos(v*2*math.Pi*2)
			nz := math.Sqrt(max(0, 1-nx*nx-ny*ny))
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(math.Round((nx*0.5 + 0.5) * 255)),
				G: uint8(math.Round((ny*0.5 + 0.5) * 255)),
				B: uint8(math.Round((nz*0.5 + 0.5) * 255)),
				A: 255,
			})
		}
	}
	nmap, err := NewNormalMap(img)
	check(t, err)
	return nmap
}

func TestGPUShaderHooksInvalidWGSLReturnsError(t *testing.T) {
	_, _, dev := newTestHostDevice(t)
	sess, err := NewGPUSession(dev)
	check(t, err)
	defer sess.Close()

	badDisp, err := NewVertexDisplacement("fn broken( -> {", 0.5, func(in VertexDisplaceInput) VertexDisplaceOutput {
		return VertexDisplaceOutput{Position: in.Position, Normal: in.Normal}
	})
	check(t, err)

	s := NewScene()
	g := testCuboid(t, nt3dmath.Vec3{X: 1, Y: 1, Z: 1}, color.RGBA{255, 100, 80, 255})
	g.Displacement = badDisp
	_, err = s.SetGeometry("bad", g)
	check(t, err)
	f, err := s.Frame(DefaultCamera(), 64, 64)
	check(t, err)

	if _, err := sess.RenderToImage(f); err == nil {
		t.Fatal("expected invalid vertex displacement WGSL to return a compile error on GPU")
	}

	badMat, err := NewMaterialShader("fn nt3dMaterial(in: NT3DMaterialInput) -> vec4<f32> { return undeclaredSymbol; }", func(in MaterialColorInput) [4]float32 {
		return in.Color
	})
	check(t, err)
	s2 := NewScene()
	g2 := testCuboid(t, nt3dmath.Vec3{X: 1, Y: 1, Z: 1}, color.RGBA{255, 100, 80, 255})
	g2.Material = &Material{Shader: badMat}
	_, err = s2.SetGeometry("bad-mat", g2)
	check(t, err)
	f2, err := s2.Frame(DefaultCamera(), 64, 64)
	check(t, err)

	if _, err := sess.RenderToImage(f2); err == nil {
		t.Fatal("expected invalid material shader WGSL to return a compile error on GPU")
	}
}

func TestGPUVertexDisplacementSoftwareParityInstancingShadowsOutlineAndPick(t *testing.T) {
	_, _, dev := newTestHostDevice(t)
	sess, err := NewGPUSession(dev)
	check(t, err)
	defer sess.Close()

	view2, err := sess.NewView()
	check(t, err)
	defer view2.Close()

	// 1. Subdivided wave sheet with analytic normals, directional shadow onto floor, and outline hull.
	s := NewScene()
	floor := subdividedGridMesh(8, 8, 4.5, color.RGBA{205, 208, 215, 255})
	floorTrans, err := TransformGeometry(floor, nt3dmath.Translation(nt3dmath.Vec3{Z: -0.9}))
	check(t, err)
	_, err = s.SetGeometry("floor", floorTrans)
	check(t, err)

	sheet := subdividedGridMesh(28, 28, 2.2, color.RGBA{90, 175, 240, 255})
	sheet.Outline = OutlineStyle{Width: 2, Color: color.RGBA{18, 28, 48, 255}}
	sheetID, err := s.SetGeometry("sheet", sheet)
	check(t, err)

	disp0 := testWaveDisplacement(t, 0.35, 0.38, 2.6)
	check(t, s.ApplyDisplacement(sheetID, disp0))

	cam := DefaultCamera()
	cam.Projection = Perspective
	cam.Elevation = 38
	cam.Azimuth = 30
	cam.Distance = 4.8

	lighting := Lighting{
		Ambient: Ambient{
			Sky:    color.RGBA{95, 110, 135, 255},
			Ground: color.RGBA{45, 48, 55, 255},
		},
		Lights: []Light{{
			Kind:      DirectionalLight,
			Direction: nt3dmath.Vec3{X: 0.45, Y: 0.35, Z: 1.0}, // Toward light (+Z)
			Color:     color.RGBA{255, 248, 236, 255},
			Intensity: 1.0,
			Shadows:   true,
		}},
	}

	for _, msaa := range []bool{false, true} {
		f, err := s.Frame(cam, 128, 96)
		check(t, err)
		f.Linear = true
		f.MSAA = msaa
		f.Complete = true
		f.Background = color.RGBA{24, 28, 36, 255}
		f.Lighting = lighting

		gpuImg, err := sess.RenderToImage(f)
		check(t, err)
		viewImg, err := view2.RenderToImage(f)
		check(t, err)
		swImg := softwareRender(f)

		if mad := meanAbsDiff(gpuImg, viewImg); mad > 0.1 {
			t.Fatalf("msaa=%v: GPUSession views diverged on VertexDisplacement: %.4f", msaa, mad)
		}
		tol := 3.5
		if msaa {
			tol = 4.5
		}
		if mad := meanAbsDiff(gpuImg, swImg); mad > tol {
			t.Fatalf("msaa=%v: GPU vs Software VertexDisplacement+Shadow+Outline mean abs diff %.2f > %.2f", msaa, mad, tol)
		}
	}

	// Dynamic WithParameters update reuses uploaded geometry and updates visual output.
	fA, err := s.Frame(cam, 128, 96)
	check(t, err)
	fA.Linear = true
	fA.Lighting = lighting
	imgA, err := sess.RenderToImage(fA)
	check(t, err)
	uploadsBefore := sess.g.uploads

	disp1, err := disp0.WithParameters(1.9, [][4]float32{{0.42, 2.6, 0, 0}})
	check(t, err)
	check(t, s.ApplyDisplacement(sheetID, disp1))
	fB, err := s.Frame(cam, 128, 96)
	check(t, err)
	fB.Linear = true
	fB.Lighting = lighting
	imgB, err := sess.RenderToImage(fB)
	check(t, err)
	swB := softwareRender(fB)

	if sess.g.uploads != uploadsBefore {
		t.Fatalf("WithParameters + ApplyDisplacement re-uploaded geometry (%d -> %d)", uploadsBefore, sess.g.uploads)
	}
	if meanAbsDiff(imgA, imgB) < 2.0 {
		t.Fatal("expected wave phase update via WithParameters to change GPU output")
	}
	if mad := meanAbsDiff(imgB, swB); mad > 3.5 {
		t.Fatalf("updated VertexDisplacement GPU vs Software mean abs diff %.2f > 3.5", mad)
	}

	// GPUSession.Pick and Scene.Pick agree on a displaced peak.
	sampleX := float32(0.0)
	wantZ := float32(0.42 * math.Sin(float64(sampleX*2.6+1.9*2.0)))
	px, py, _, ok := fB.Matrix.Project(nt3dmath.Vec3{X: sampleX, Y: 0, Z: wantZ}, 128, 96)
	if !ok {
		t.Fatal("sample point on wave sheet not in view")
	}
	cpuHit := s.Pick(cam, fB, px, py, 2)
	gpuHit := sess.Pick(s, cam, fB, px, py, 2)
	if cpuHit == nil || gpuHit == nil || cpuHit.Node != sheetID || gpuHit.Node != sheetID {
		t.Fatalf("Pick mismatch on displaced wave: cpu=%+v gpu=%+v", cpuHit, gpuHit)
	}
	if d := cpuHit.Position.Sub(gpuHit.Position); d.Dot(d) > 2e-2 {
		t.Fatalf("CPU Pick %v and GPU Pick %v diverged on displaced wave", cpuHit.Position, gpuHit.Position)
	}

	// 2. Instanced SharedGeometry started untextured (batches[1]), then upgraded via ApplyDisplacement (batches[7]).
	sInst := NewScene()
	pillarMesh := testCuboid(t, nt3dmath.Vec3{X: 0.45, Y: 0.45, Z: 1.0}, color.RGBA{120, 215, 135, 255})
	sharedPillar, err := NewSharedGeometry(pillarMesh)
	check(t, err)
	row, err := sInst.Add(Root, "row")
	check(t, err)
	for i := -2; i <= 2; i++ {
		id, err := sInst.Add(row, fmt.Sprintf("pillar-%d", i+2))
		check(t, err)
		check(t, sInst.SetNodeShared(id, sharedPillar))
		check(t, sInst.SetTransform(id, nt3dmath.Translation(nt3dmath.Vec3{X: float32(i) * 0.85})))
	}
	camInst := DefaultCamera()
	camInst.Elevation = 28
	camInst.Azimuth = 20
	camInst.Distance = 5.8

	// Render first BEFORE ApplyDisplacement so batches[1] is uploaded first!
	fInit, err := sInst.Frame(camInst, 128, 96)
	check(t, err)
	fInit.Linear = true
	_, err = sess.RenderToImage(fInit)
	check(t, err)

	// Now apply world-position-dependent displacement across all 5 shared instances.
	sway, err := NewVertexDisplacementWithParameters(`
fn nt3dDisplace(in: NT3DVertexInput) -> NT3DVertexOutput {
	let h = clamp(in.position.z + 0.5, 0.0, 1.0);
	let bend = 0.35 * h * h * sin(in.world.x * 1.7 + in.time);
	return NT3DVertexOutput(in.position + vec3<f32>(bend, 0.0, 0.2 * sin(in.world.x)), in.normal);
}
`, 0.5, 0.8, nil, func(in VertexDisplaceInput) VertexDisplaceOutput {
		h := min(float32(1), max(float32(0), in.Position.Z+0.5))
		bend := 0.35 * h * h * float32(math.Sin(float64(in.World.X*1.7+in.Time)))
		dz := 0.2 * float32(math.Sin(float64(in.World.X)))
		return VertexDisplaceOutput{
			Position: in.Position.Add(nt3dmath.Vec3{X: bend, Z: dz}),
			Normal:   in.Normal,
		}
	})
	check(t, err)
	check(t, sInst.ApplyDisplacement(row, sway))

	fSway, err := sInst.Frame(camInst, 128, 96)
	check(t, err)
	fSway.Linear = true
	if stats := fSway.Stats(); stats.Batches != 1 || stats.DrawnInstances != 5 {
		t.Fatalf("expected 5 swaying shared instances in 1 batch, got %+v", stats)
	}
	gpuSway, err := sess.RenderToImage(fSway)
	check(t, err)
	swSway := softwareRender(fSway)
	if mad := meanAbsDiff(gpuSway, swSway); mad > 2.8 {
		t.Fatalf("SharedGeometry ApplyDisplacement GPU vs Software mean abs diff %.2f > 2.8", mad)
	}
}

func TestGPUMaterialShaderSoftwareParityCutoutOITUnlitAndCombined(t *testing.T) {
	_, _, dev := newTestHostDevice(t)
	sess, err := NewGPUSession(dev)
	check(t, err)
	defer sess.Close()

	// 1. Lit MaterialShader + VertexDisplacement combined on water-like grid.
	sCombo := NewScene()
	grid := subdividedGridMesh(28, 28, 2.4, color.RGBA{255, 255, 255, 255})
	grid.Material = &Material{
		Specular:  0.4,
		Shininess: 48,
		Shader:    testBandMaterialShader(t, 0.7, 2.5),
	}
	grid.Displacement = testWaveDisplacement(t, 0.7, 0.28, 2.8)
	_, err = sCombo.SetGeometry("water", grid)
	check(t, err)

	cam := DefaultCamera()
	cam.Elevation = 35
	cam.Azimuth = 28
	cam.Distance = 4.2
	fCombo, err := sCombo.Frame(cam, 128, 96)
	check(t, err)
	fCombo.Linear = true
	fCombo.Background = color.RGBA{18, 22, 30, 255}

	gpuCombo, err := sess.RenderToImage(fCombo)
	check(t, err)
	swCombo := softwareRender(fCombo)
	if mad := meanAbsDiff(gpuCombo, swCombo); mad > 3.0 {
		t.Fatalf("Combined VertexDisplacement + MaterialShader GPU vs Software mean abs diff %.2f > 3.0", mad)
	}

	// 2. Unlit MaterialShader using in.normal and in.view (stylized Gooch/rim ink) + NormalMap!
	nmap := testBumpNormalMap(t, 32)
	inkShader, err := NewMaterialShader(`
fn nt3dMaterial(in: NT3DMaterialInput) -> vec4<f32> {
	let ndv = clamp(dot(in.normal, in.view), 0.0, 1.0);
	let warm = vec3<f32>(0.92, 0.52, 0.22);
	let cool = vec3<f32>(0.12, 0.22, 0.58);
	let gooch = mix(cool, warm, in.normal.z * 0.5 + 0.5);
	let ink = smoothstep(0.22, 0.32, ndv);
	return vec4<f32>(gooch * ink, 1.0);
}
`, func(in MaterialColorInput) [4]float32 {
		ndv := min(float32(1), max(float32(0), in.Normal.Dot(in.View)))
		tMix := in.Normal.Z*0.5 + 0.5
		warm := [3]float32{0.92, 0.52, 0.22}
		cool := [3]float32{0.12, 0.22, 0.58}
		u := min(float32(1), max(float32(0), (ndv-0.22)/(0.32-0.22)))
		ink := u * u * (3 - 2*u)
		return [4]float32{
			(cool[0] + (warm[0]-cool[0])*tMix) * ink,
			(cool[1] + (warm[1]-cool[1])*tMix) * ink,
			(cool[2] + (warm[2]-cool[2])*tMix) * ink,
			1,
		}
	})
	check(t, err)

	sInk := NewScene()
	sphere, err := BuildSphere(Sphere{
		Radius:   0.85,
		Segments: 28,
		Rings:    16,
		Color:    color.RGBA{255, 255, 255, 255},
		Material: &Material{
			Unlit:          true,
			NormalMap:      nmap,
			NormalStrength: 0.6,
			NormalRepeat:   true,
			Shader:         inkShader,
		},
	})
	check(t, err)
	_, err = sInk.SetGeometry("sphere", sphere)
	check(t, err)
	fInk, err := sInk.Frame(DefaultCamera(), 120, 120)
	check(t, err)
	fInk.Linear = true
	fInk.Background = color.RGBA{30, 32, 38, 255}

	gpuInk, err := sess.RenderToImage(fInk)
	check(t, err)
	swInk := softwareRender(fInk)
	if mad := meanAbsDiff(gpuInk, swInk); mad > 3.0 {
		t.Fatalf("Unlit MaterialShader + NormalMap GPU vs Software mean abs diff %.2f > 3.0", mad)
	}

	// 3. Procedural Cutout (no image Texture!) casting perforated directional shadow onto floor.
	cutShader, err := NewMaterialShader(`
fn nt3dMaterial(in: NT3DMaterialInput) -> vec4<f32> {
	let cell = fract(in.uv * 4.0) - vec2<f32>(0.5);
	let a = select(0.0, 1.0, length(cell) < 0.34);
	return vec4<f32>(vec3<f32>(0.85, 0.25, 0.2), a);
}
`, func(in MaterialColorInput) [4]float32 {
		fu := float32(in.UV.U*4 - float32(math.Floor(float64(in.UV.U*4))) - 0.5)
		fv := float32(in.UV.V*4 - float32(math.Floor(float64(in.UV.V*4))) - 0.5)
		a := float32(0)
		if math.Hypot(float64(fu), float64(fv)) < 0.34 {
			a = 1
		}
		return [4]float32{0.85, 0.25, 0.2, a}
	})
	check(t, err)

	sCut := NewScene()
	cutFloor, err := TransformGeometry(subdividedGridMesh(4, 4, 4.0, color.RGBA{220, 220, 220, 255}), nt3dmath.Translation(nt3dmath.Vec3{Z: -0.7}))
	check(t, err)
	_, err = sCut.SetGeometry("floor", cutFloor)
	check(t, err)

	screen := subdividedGridMesh(2, 2, 1.6, color.RGBA{255, 255, 255, 255})
	screen.Material = &Material{Cutout: true, Threshold: 0.5, Shader: cutShader}
	_, err = sCut.SetGeometry("screen", screen)
	check(t, err)

	camCut := DefaultCamera()
	camCut.Elevation = 42
	camCut.Azimuth = 32
	camCut.Distance = 4.4
	fCut, err := sCut.Frame(camCut, 128, 96)
	check(t, err)
	fCut.Linear = true
	fCut.Complete = true
	fCut.Lighting = Lighting{
		Ambient: Ambient{
			Sky:    color.RGBA{85, 90, 100, 255},
			Ground: color.RGBA{45, 45, 50, 255},
		},
		Lights: []Light{{
			Kind:      DirectionalLight,
			Direction: nt3dmath.Vec3{X: 0.3, Y: -0.25, Z: 1.0}, // Toward light (+Z)
			Color:     color.RGBA{255, 255, 255, 255},
			Intensity: 1.0,
			Shadows:   true,
		}},
	}
	gpuCut, err := sess.RenderToImage(fCut)
	check(t, err)
	swCut := softwareRender(fCut)
	if mad := meanAbsDiff(gpuCut, swCut); mad > 3.5 {
		t.Fatalf("Procedural Cutout + Shadow GPU vs Software mean abs diff %.2f > 3.5", mad)
	}

	// 4. Transparent MaterialShader under WBOIT (Frame.OIT = true).
	glassShader, err := NewMaterialShader(`
fn nt3dMaterial(in: NT3DMaterialInput) -> vec4<f32> {
	let stripe = 0.35 + 0.45 * (0.5 + 0.5 * sin(in.uv.x * 12.56637));
	return vec4<f32>(in.color.rgb, stripe * in.color.a);
}
`, func(in MaterialColorInput) [4]float32 {
		stripe := float32(0.35 + 0.45*(0.5+0.5*math.Sin(float64(in.UV.U*12.56637))))
		return [4]float32{in.Color[0], in.Color[1], in.Color[2], stripe * in.Color[3]}
	})
	check(t, err)

	redQ, blueQ := crossedGlassQuads(t)
	redQ.UV = []UV{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	blueQ.UV = []UV{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	redQ.Material = &Material{Unlit: true, Transparent: true, Shader: glassShader}
	blueQ.Material = &Material{Unlit: true, Transparent: true, Shader: glassShader}

	fOIT := depthFrame(96, 96, redQ, blueQ)
	fOIT.Revision = 1
	fOIT.Linear = true
	fOIT.OIT = true
	fOIT.Background = color.RGBA{20, 24, 30, 255}

	gpuOIT, err := sess.RenderToImage(fOIT)
	check(t, err)
	swOIT := softwareRender(fOIT)
	if mad := meanAbsDiff(gpuOIT, swOIT); mad > 3.0 {
		t.Fatalf("Transparent MaterialShader WBOIT GPU vs Software mean abs diff %.2f > 3.0", mad)
	}
}
