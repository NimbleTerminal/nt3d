// Copyright (c) 2026 Neomantra Corp.

package nt3dcamscene

import (
	"image/color"
	"math"
	"testing"
	"time"

	"nimbleterminal.dev/nt3d"
	nt3dcam "nimbleterminal.dev/nt3d/cam"
	nt3dcamspec "nimbleterminal.dev/nt3d/cam/spec"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func TestCameraFromStateAndStateFromCameraRoundTrip(t *testing.T) {
	wantPos := nt3dcam.Vec3d{X: 583_960.25, Y: 4_507_523.75, Z: 42.5}
	wantDir := nt3dcam.Vec3d{X: 0.6, Y: 0.7, Z: -0.3872983346207417}.Normalize()
	baseRot := nt3dcam.LookRotationQuat(wantDir, nt3dcam.DefaultUp)
	// Apply 12 deg roll.
	rollHalf := 6 * math.Pi / 180
	rollQ := nt3dcam.Quatd{Z: -math.Sin(rollHalf), W: math.Cos(rollHalf)}

	st := nt3dcam.CameraState{
		Pos:       wantPos,
		Rot:       baseRot.Mul(rollQ).Normalize(),
		FovY:      38 * math.Pi / 180,
		FocusDist: 14.5,
		Near:      0.15,
		Far:       1200,
	}

	cam := CameraFromState(st)
	eye64 := cam.WorldEye()
	if math.Abs(eye64[0]-wantPos.X) > 1e-12 || math.Abs(eye64[1]-wantPos.Y) > 1e-12 || math.Abs(eye64[2]-wantPos.Z) > 1e-12 {
		t.Fatalf("WorldEye lost float64 precision: got %v, want %+v", eye64, wantPos)
	}

	got := StateFromCamera(cam)
	if got.Pos.Sub(st.Pos).Len() > 1e-9 {
		t.Fatalf("StateFromCamera Pos mismatch: got %+v, want %+v", got.Pos, st.Pos)
	}
	if got.Forward().Sub(st.Forward()).Len() > 1e-6 || got.Up().Sub(st.Up()).Len() > 1e-6 {
		t.Fatalf("StateFromCamera basis mismatch: got fwd=%+v up=%+v, want fwd=%+v up=%+v",
			got.Forward(), got.Up(), st.Forward(), st.Up())
	}
	if math.Abs(got.FovY-st.FovY) > 1e-9 || math.Abs(got.Near-st.Near) > 1e-9 || math.Abs(got.Far-st.Far) > 1e-9 {
		t.Fatalf("StateFromCamera lens mismatch: got %+v, want %+v", got, st)
	}
}

func TestGeographicCameraOriginZeroJitterParity(t *testing.T) {
	// At 5,839,600 m, float32 ULP spacing is 0.5 m (50 cm quantization!).
	// nt3d.Camera-relative float64 Origin subtraction must produce pixel-identical
	// renders and sub-micrometer Pick WorldPosition parity with a local (0,0,0) scene.
	const geoX, geoY, geoZ = 5_839_600.125, -4_507_520.375, 250.625

	buildScene := func(offset [3]float64) (*nt3d.Scene, nt3d.Camera) {
		s := nt3d.NewScene()
		tile, err := s.Add(nt3d.Root, "tile")
		if err != nil {
			t.Fatal(err)
		}
		if offset != ([3]float64{}) {
			if err := s.SetNodeOrigin(tile, offset); err != nil {
				t.Fatal(err)
			}
		}

		boxNode, err := s.Add(tile, "monument")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SetTransform(boxNode, nt3dmath.Translation(nt3dmath.Vec3{X: 1.25, Y: 3.5, Z: 0.75})); err != nil {
			t.Fatal(err)
		}
		mat := nt3d.Material{Roughness: 0.7}
		g := nt3d.Geometry{
			Boxes:    []nt3d.Box{{Min: nt3dmath.Vec3{X: -0.8, Y: -0.8, Z: -0.8}, Size: nt3dmath.Vec3{X: 1.6, Y: 1.6, Z: 1.6}, Color: color.RGBA{R: 220, G: 90, B: 50, A: 255}, Datum: 42}},
			Material: &mat,
		}
		if err := s.SetNodeGeometry(boxNode, g); err != nil {
			t.Fatal(err)
		}

		lightNode, err := s.Add(tile, "key-lamp")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SetTransform(lightNode, nt3dmath.Translation(nt3dmath.Vec3{X: -2.0, Y: 0.5, Z: 4.0})); err != nil {
			t.Fatal(err)
		}
		if err := s.SetNodeLight(lightNode, nt3d.Light{
			Kind:      nt3d.PointLight,
			Color:     color.RGBA{R: 255, G: 245, B: 230, A: 255},
			Intensity: 18,
			Range:     30,
		}); err != nil {
			t.Fatal(err)
		}

		p, err := nt3dcam.NewPath(nt3dcam.PathConfig{
			Duration: 4 * time.Second,
			Position: nt3dcam.PositionTrack{
				Interp: nt3dcam.InterpCatmullRomCentripetal,
				Points: []nt3dcam.Vec3d{
					{X: offset[0] - 2.5, Y: offset[1] - 3.0, Z: offset[2] + 2.2},
					{X: offset[0] + 1.25, Y: offset[1] - 2.5, Z: offset[2] + 1.8},
				},
			},
			Look: []nt3dcam.LookSegment{
				{Mode: nt3dcam.LookTarget, TargetRef: "node:monument"},
			},
		}, nt3dcam.WithResolver(SceneResolver(s, nil)))
		if err != nil {
			t.Fatal(err)
		}

		st := p.Eval(2 * time.Second)
		return s, CameraFromState(st)
	}

	localScene, localCam := buildScene([3]float64{})
	geoScene, geoCam := buildScene([3]float64{geoX, geoY, geoZ})

	const W, H = 160, 120
	fLocal, err := localScene.Frame(localCam, W, H)
	if err != nil {
		t.Fatal(err)
	}
	fGeo, err := geoScene.Frame(geoCam, W, H)
	if err != nil {
		t.Fatal(err)
	}

	sw := nt3d.NewSoftwareRenderer()
	defer sw.Close()
	imgLocal, err := sw.Render(fLocal)
	if err != nil {
		t.Fatal(err)
	}
	imgGeo, err := sw.Render(fGeo)
	if err != nil {
		t.Fatal(err)
	}

	for y := range H {
		for x := range W {
			r1, g1, b1, a1 := imgLocal.At(x, y).RGBA()
			r2, g2, b2, a2 := imgGeo.At(x, y).RGBA()
			if r1 != r2 || g1 != g2 || b1 != b2 || a1 != a2 {
				t.Fatalf("pixel mismatch at (%d,%d): local=(%d,%d,%d,%d) geo=(%d,%d,%d,%d)",
					x, y, r1, g1, b1, a1, r2, g2, b2, a2)
			}
		}
	}

	// Verify Pick hits the center of the screen identically and preserves sub-mm WorldPosition!
	hitLocal := localScene.Pick(localCam, fLocal, W/2, H/2, 2)
	hitGeo := geoScene.Pick(geoCam, fGeo, W/2, H/2, 2)
	if hitLocal == nil || hitGeo == nil {
		t.Fatalf("expected center pick hit on both scenes: local=%v geo=%v", hitLocal, hitGeo)
	}
	if hitLocal.Datum != 42 || hitGeo.Datum != 42 {
		t.Fatalf("unexpected hit datum: local=%d geo=%d", hitLocal.Datum, hitGeo.Datum)
	}
	dx := (hitGeo.WorldPosition[0] - geoX) - hitLocal.WorldPosition[0]
	dy := (hitGeo.WorldPosition[1] - geoY) - hitLocal.WorldPosition[1]
	dz := (hitGeo.WorldPosition[2] - geoZ) - hitLocal.WorldPosition[2]
	if math.Sqrt(dx*dx+dy*dy+dz*dz) > 1e-5 {
		t.Fatalf("geographic Pick WorldPosition drifted: delta=(%g, %g, %g)", dx, dy, dz)
	}
}

func TestSceneResolverPathTargetAndOverlay(t *testing.T) {
	s := nt3d.NewScene()
	beacon, err := s.Add(nt3d.Root, "beacon")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTransform(beacon, nt3dmath.Translation(nt3dmath.Vec3{X: 4, Y: 5, Z: 2})); err != nil {
		t.Fatal(err)
	}

	leadPath, err := nt3dcamspec.Compile([]byte(`
camera_path:
  version: 1
  name: lead-drone
  duration: 6s
  position:
    points: [[0, 0, 2], [4, 5, 3], [8, 10, 2]]
`))
	if err != nil {
		t.Fatal(err)
	}

	paths := map[string]*nt3dcam.Path{"lead": leadPath}
	res := SceneResolver(s, paths)

	chasePath, err := nt3dcamspec.Compile([]byte(`
camera_path:
  version: 1
  name: chase-cam
  duration: 6s
  position:
    points: [[-4, -4, 4], [0, 1, 5], [4, 6, 4]]
  look:
    - {mode: target, target: "path:lead", from: 0s}
    - {mode: target, target: "node:beacon", from: 4s, blend: 1s}
  markers:
    - {name: lock-beacon, t: 4s}
`), nt3dcam.WithResolver(res))
	if err != nil {
		t.Fatal(err)
	}

	// Verify cyclic path references terminate cleanly and fall back without stack overflow.
	cycA, err := nt3dcamspec.Compile([]byte(`
camera_path:
  version: 1
  duration: 2s
  position: {points: [[0, -5, 2], [0, -4, 2]]}
  look: [{mode: target, target: "path:cycB", fallback: [0, 10, 2]}]
`))
	if err != nil {
		t.Fatal(err)
	}
	cycB, err := nt3dcamspec.Compile([]byte(`
camera_path:
  version: 1
  duration: 2s
  position: {points: [[2, -5, 2], [2, -4, 2]]}
  look: [{mode: target, target: "path:cycA", fallback: [2, 10, 2]}]
`))
	if err != nil {
		t.Fatal(err)
	}
	paths["cycA"] = cycA
	paths["cycB"] = cycB
	stCyc := cycA.EvalWith(time.Second, res)
	if !stCyc.Pos.IsFinite() || math.Abs(stCyc.Rot.Norm()-1) > 1e-12 {
		t.Fatalf("cyclic path resolution failed: %+v", stCyc)
	}

	// Render chasePath overlay from an external observer camera and verify it draws cleanly.
	obsCam := nt3d.DefaultCamera().LookAt(nt3dmath.Vec3{X: -12, Y: -10, Z: 10}, nt3dmath.Vec3{X: 2, Y: 3, Z: 3})
	obsCam.Projection = nt3d.Perspective
	f, err := s.Frame(obsCam, 200, 150)
	if err != nil {
		t.Fatal(err)
	}
	f.Background = color.RGBA{R: 15, G: 18, B: 24, A: 255}
	AppendCameraPathOverlay(&f, chasePath, 3*time.Second)
	if len(f.DynamicLines) == 0 || len(f.DynamicLines)%2 != 0 {
		t.Fatalf("expected non-empty paired DynamicLines, got %d vertices", len(f.DynamicLines))
	}

	sw := nt3d.NewSoftwareRenderer()
	defer sw.Close()
	img, err := sw.Render(f)
	if err != nil {
		t.Fatalf("Render with CameraPathOverlay failed: %v", err)
	}
	// Count non-background pixels to prove the 3D curve/frustum/ray overlay drew onto the frame.
	nonBG := 0
	for y := range 150 {
		for x := range 200 {
			r, g, b, _ := img.At(x, y).RGBA()
			if r>>8 != 15 || g>>8 != 18 || b>>8 != 24 {
				nonBG++
			}
		}
	}
	if nonBG < 25 {
		t.Fatalf("expected visible overlay lines, got only %d non-background pixels", nonBG)
	}

	// Verify DynamicLineWidth scales stroke coverage cleanly and rejects out-of-range widths.
	fThin := f
	fThin.DynamicLineWidth = 1
	imgThin, err := sw.Render(fThin)
	if err != nil {
		t.Fatal(err)
	}
	fThick := f
	fThick.DynamicLineWidth = 4
	imgThick, err := sw.Render(fThick)
	if err != nil {
		t.Fatal(err)
	}
	countNonBG := func(im interface {
		At(int, int) color.Color
	}) int {
		n := 0
		for y := range 150 {
			for x := range 200 {
				r, g, b, _ := im.At(x, y).RGBA()
				if r>>8 != 15 || g>>8 != 18 || b>>8 != 24 {
					n++
				}
			}
		}
		return n
	}
	if thinN, thickN := countNonBG(imgThin), countNonBG(imgThick); thickN <= thinN*2 {
		t.Fatalf("expected DynamicLineWidth=4 (%d px) to cover >2x DynamicLineWidth=1 (%d px)", thickN, thinN)
	}
	fBad := f
	fBad.DynamicLineWidth = 64
	if _, err := sw.Render(fBad); err == nil {
		t.Fatal("expected out-of-range DynamicLineWidth to fail validation")
	}
}
