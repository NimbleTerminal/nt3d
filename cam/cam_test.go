// Copyright (c) 2026 Neomantra Corp.

package nt3dcam

import (
	"encoding/json"
	"flag"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var updateGolden = flag.Bool("update-golden", false, "regenerate cam/testdata/golden.json")

type goldenSample struct {
	TimeMS    int64      `json:"time_ms"`
	Pos       [3]float64 `json:"pos"`
	Rot       [4]float64 `json:"rot"`
	FovY      float64    `json:"fov_y"`
	FocusDist float64    `json:"focus_dist"`
	Aperture  float64    `json:"aperture"`
	Near      float64    `json:"near"`
	Far       float64    `json:"far"`
}

type goldenFixture struct {
	Name        string         `json:"name"`
	TotalLength float64        `json:"total_length"`
	Samples     []goldenSample `json:"samples"`
}

func buildGoldenFixtures(t *testing.T) ([]*Path, Resolver) {
	t.Helper()
	res := ResolverFunc(func(ref string, ts time.Duration) (TargetInfo, bool) {
		if ref == "node:island" {
			return TargetInfo{Pos: Vec3d{X: 12, Y: 10, Z: 1.5}}, true
		}
		return TargetInfo{}, false
	})

	// 1. Closed orbit with FOV pull & subtle bank.
	orbit, err := NewPath(PathConfig{
		Name:     "orbit-turntable",
		Duration: 8 * time.Second,
		Loop:     LoopRepeat,
		Position: PositionTrack{
			Interp: InterpCatmullRomCentripetal,
			Closed: true,
			Points: []Vec3d{
				{X: 0, Y: -10, Z: 3},
				{X: 10, Y: 0, Z: 3.5},
				{X: 0, Y: 10, Z: 4},
				{X: -10, Y: 0, Z: 3.5},
			},
		},
		Look: []LookSegment{
			{Mode: LookTarget, Point: Vec3d{X: 0, Y: 0, Z: 1}},
		},
		Roll: []Keyframe{
			{T: 0, V: 0},
			{T: 4 * time.Second, V: 5 * math.Pi / 180, Ease: EaseSpec{Kind: EaseSmoothstep}},
			{T: 8 * time.Second, V: 0, Ease: EaseSpec{Kind: EaseSmoothstep}},
		},
		Lens: LensTrack{
			FovY: []Keyframe{
				{T: 0, V: 50 * math.Pi / 180},
				{T: 4 * time.Second, V: 32 * math.Pi / 180, Ease: EaseSpec{Kind: EaseInOut}},
				{T: 8 * time.Second, V: 50 * math.Pi / 180, Ease: EaseSpec{Kind: EaseInOut}},
			},
			FocusDist: []Keyframe{{T: 0, V: 10.2}},
			Aperture:  []Keyframe{{T: 0, V: 2.8}},
			Near:      0.1,
			Far:       500,
		},
	}, WithResolver(res))
	if err != nil {
		t.Fatalf("build orbit fixture: %v", err)
	}

	// 2. S-curve flythrough with resolved target -> tangent blend, bank, and DoF pull.
	flythrough, err := NewPath(PathConfig{
		Name:     "flythrough-island",
		Duration: 12 * time.Second,
		Loop:     LoopNone,
		Position: PositionTrack{
			Interp: InterpCatmullRomCentripetal,
			Points: []Vec3d{
				{X: 0, Y: -15, Z: 8},
				{X: 6, Y: -6, Z: 5},
				{X: 12, Y: 4, Z: 3},
				{X: 20, Y: 14, Z: 4},
			},
		},
		Timing: TimingTrack{
			Segments: []TimingSegment{
				{To: 0.35, Over: 4 * time.Second, Ease: EaseSpec{Kind: EaseIn}},
				{To: 0.80, Over: 5 * time.Second, Ease: EaseSpec{Kind: EaseLinear}},
				{To: 1.00, Over: 3 * time.Second, Ease: EaseSpec{Kind: EaseOut}},
			},
		},
		Look: []LookSegment{
			{Mode: LookTarget, TargetRef: "node:island", From: 0},
			{Mode: LookTangent, From: 4 * time.Second, Blend: 2 * time.Second, BlendEase: EaseSpec{Kind: EaseInOut}},
		},
		Roll: []Keyframe{
			{T: 0, V: 0},
			{T: 6 * time.Second, V: -8 * math.Pi / 180, Ease: EaseSpec{Kind: EaseInOut}},
			{T: 12 * time.Second, V: 0, Ease: EaseSpec{Kind: EaseOut}},
		},
		Lens: LensTrack{
			FovY: []Keyframe{
				{T: 0, V: 55 * math.Pi / 180},
				{T: 8 * time.Second, V: 38 * math.Pi / 180, Ease: EaseSpec{Kind: EaseInOut}},
			},
			FocusDist: []Keyframe{
				{T: 0, V: 25},
				{T: 6 * time.Second, V: 12, Ease: EaseSpec{Kind: EaseSmoothstep}},
			},
			Aperture: []Keyframe{{T: 0, V: 2.0}},
			Near:     0.1,
			Far:      2000,
		},
		Markers: []Marker{
			{Name: "reveal", T: 4 * time.Second},
			{Name: "settle", T: 10 * time.Second},
		},
	}, WithResolver(res))
	if err != nil {
		t.Fatalf("build flythrough fixture: %v", err)
	}

	// 3. Bezier Hermite path with squad quaternion keyframes.
	q0 := LookRotationQuat(Vec3d{X: 1, Y: 0, Z: 0}, DefaultUp)
	q1 := LookRotationQuat(Vec3d{X: 0, Y: 1, Z: -0.2}, DefaultUp)
	q2 := LookRotationQuat(Vec3d{X: -0.5, Y: 1, Z: 0.1}, DefaultUp)
	bezierPath, err := NewPath(PathConfig{
		Name:     "bezier-squad",
		Duration: 6 * time.Second,
		Position: PositionTrack{
			Interp: InterpBezier,
			Knots: []BezierKnot{
				{P: Vec3d{X: 0, Y: 0, Z: 2}, In: Vec3d{X: 0, Y: 0, Z: 2}, Out: Vec3d{X: 2, Y: 0, Z: 3}},
				{P: Vec3d{X: 6, Y: 4, Z: 3}, In: Vec3d{X: 4, Y: 2, Z: 3}, Out: Vec3d{X: 8, Y: 6, Z: 3}},
				{P: Vec3d{X: 10, Y: 10, Z: 2}, In: Vec3d{X: 9, Y: 8, Z: 2}, Out: Vec3d{X: 10, Y: 10, Z: 2}},
			},
		},
		Look: []LookSegment{
			{
				Mode:   LookKeyframes,
				Interp: QuatSquad,
				Keys: []QuatKeyframe{
					{T: 0, Q: q0},
					{T: 3 * time.Second, Q: q1, Ease: EaseSpec{Kind: EaseSmoothstep}},
					{T: 6 * time.Second, Q: q2, Ease: EaseSpec{Kind: EaseOut}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("build bezier fixture: %v", err)
	}

	// 4. Approximating B-spline path.
	bsplinePath, err := NewPath(PathConfig{
		Name:     "bspline-glide",
		Duration: 5 * time.Second,
		Position: PositionTrack{
			Interp: InterpBSpline,
			Points: []Vec3d{
				{X: -8, Y: -8, Z: 4},
				{X: -2, Y: 4, Z: 6},
				{X: 4, Y: -2, Z: 5},
				{X: 10, Y: 8, Z: 3},
				{X: 14, Y: 12, Z: 3},
			},
		},
	})
	if err != nil {
		t.Fatalf("build bspline fixture: %v", err)
	}

	return []*Path{orbit, flythrough, bezierPath, bsplinePath}, res
}

func TestGoldenFixtures(t *testing.T) {
	paths, _ := buildGoldenFixtures(t)
	goldenPath := filepath.Join("testdata", "golden.json")

	actual := make([]goldenFixture, len(paths))
	for i, p := range paths {
		const steps = 8
		samples := make([]goldenSample, steps+1)
		for k := 0; k <= steps; k++ {
			ts := time.Duration(int64(p.Duration()) * int64(k) / steps)
			st := p.Eval(ts)
			samples[k] = goldenSample{
				TimeMS:    ts.Milliseconds(),
				Pos:       [3]float64{st.Pos.X, st.Pos.Y, st.Pos.Z},
				Rot:       [4]float64{st.Rot.X, st.Rot.Y, st.Rot.Z, st.Rot.W},
				FovY:      st.FovY,
				FocusDist: st.FocusDist,
				Aperture:  st.Aperture,
				Near:      st.Near,
				Far:       st.Far,
			}
		}
		actual[i] = goldenFixture{
			Name:        p.Config().Name,
			TotalLength: p.TotalLength(),
			Samples:     samples,
		}
	}

	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		raw, err := json.MarshalIndent(actual, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read %s (run with -update-golden to generate): %v", goldenPath, err)
	}
	var expected []goldenFixture
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatalf("unmarshal %s: %v", goldenPath, err)
	}
	if len(expected) != len(actual) {
		t.Fatalf("fixture count mismatch: got %d, want %d", len(actual), len(expected))
	}

	const tol = 1e-9
	for i := range actual {
		got, want := actual[i], expected[i]
		if got.Name != want.Name {
			t.Fatalf("fixture %d name: got %q, want %q", i, got.Name, want.Name)
		}
		if math.Abs(got.TotalLength-want.TotalLength) > tol {
			t.Errorf("%s total_length: got %.12f, want %.12f", got.Name, got.TotalLength, want.TotalLength)
		}
		if len(got.Samples) != len(want.Samples) {
			t.Fatalf("%s sample count mismatch: got %d, want %d", got.Name, len(got.Samples), len(want.Samples))
		}
		for k := range got.Samples {
			gs, ws := got.Samples[k], want.Samples[k]
			for c := range 3 {
				if math.Abs(gs.Pos[c]-ws.Pos[c]) > tol {
					t.Errorf("%s t=%dms pos[%d]: got %.12f, want %.12f", got.Name, gs.TimeMS, c, gs.Pos[c], ws.Pos[c])
				}
			}
			// Compare unit quaternions up to antipodal sign (and raw components within tol).
			for c := range 4 {
				if math.Abs(gs.Rot[c]-ws.Rot[c]) > tol {
					t.Errorf("%s t=%dms rot[%d]: got %.12f, want %.12f", got.Name, gs.TimeMS, c, gs.Rot[c], ws.Rot[c])
				}
			}
			if math.Abs(gs.FovY-ws.FovY) > tol ||
				math.Abs(gs.FocusDist-ws.FocusDist) > tol ||
				math.Abs(gs.Aperture-ws.Aperture) > tol ||
				math.Abs(gs.Near-ws.Near) > tol ||
				math.Abs(gs.Far-ws.Far) > tol {
				t.Errorf("%s t=%dms lens mismatch: got %+v, want %+v", got.Name, gs.TimeMS, gs, ws)
			}
		}
	}
}

func TestPropertyConstantSpeedAndUnitQuaternions(t *testing.T) {
	// Highly non-uniform control point spacing: 20 m straight followed by tight 1.5 m hook.
	p, err := NewPath(PathConfig{
		Name:     "constant-speed-check",
		Duration: 10 * time.Second,
		Position: PositionTrack{
			Interp: InterpCatmullRomCentripetal,
			Points: []Vec3d{
				{X: 0, Y: 0, Z: 2},
				{X: 20, Y: 0, Z: 5},
				{X: 21, Y: 1.2, Z: 5.2},
				{X: 20, Y: 2.5, Z: 4.5},
				{X: 5, Y: 8, Z: 1},
			},
		},
		Look: []LookSegment{{Mode: LookTangent}},
		Roll: []Keyframe{
			{T: 0, V: -0.2},
			{T: 10 * time.Second, V: 0.35, Ease: EaseSpec{Kind: EaseSmootherstep}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	const N = 200
	const subSteps = 16
	dt := p.Duration() / N
	subDt := dt / subSteps
	wantStep := p.TotalLength() / N

	prev := p.Eval(0)
	for i := 1; i <= N; i++ {
		tStart := time.Duration(i-1) * dt
		var arcDist float64
		cur := prev
		for s := 1; s <= subSteps; s++ {
			next := p.Eval(tStart + time.Duration(s)*subDt)
			arcDist += next.Pos.Sub(cur.Pos).Len()
			cur = next
		}
		st := cur
		if math.Abs(st.Rot.Norm()-1) > 1e-12 {
			t.Fatalf("step %d: quaternion norm %g != 1", i, st.Rot.Norm())
		}
		r, u, f := st.Rot.Basis()
		if math.Abs(r.Dot(u)) > 1e-12 || math.Abs(r.Dot(f)) > 1e-12 || math.Abs(u.Dot(f)) > 1e-12 {
			t.Fatalf("step %d: non-orthonormal basis", i)
		}
		relErr := math.Abs(arcDist-wantStep) / wantStep
		if relErr > 5e-4 {
			t.Fatalf("step %d: non-constant speed arcDist=%.6f want=%.6f (relErr=%.6f)", i, arcDist, wantStep, relErr)
		}
		prev = st
	}
}

func TestPropertyPureDeterministicAndConcurrentSafe(t *testing.T) {
	cfg := PathConfig{
		Name:     "concurrency-check",
		Duration: 10 * time.Second,
		Loop:     LoopPingPong,
		Position: PositionTrack{
			Interp: InterpCatmullRomCentripetal,
			Points: []Vec3d{
				{X: 0, Y: 0, Z: 2},
				{X: 4, Y: 5, Z: 3},
				{X: 9, Y: 2, Z: 4},
				{X: 14, Y: 8, Z: 2},
			},
		},
		Look: []LookSegment{
			{Mode: LookTarget, Point: Vec3d{X: 7, Y: 4, Z: 1}, From: 0},
			{Mode: LookTangent, From: 3 * time.Second, Blend: 2 * time.Second, ReverseWithMotion: true},
		},
		Roll: []Keyframe{{T: 0, V: 0}, {T: 5 * time.Second, V: 0.25}},
	}
	p, err := NewPath(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Mutate caller's slices after NewPath to prove deep-copy immutability.
	cfg.Position.Points[0] = Vec3d{X: 999, Y: 999, Z: 999}
	cfg.Look[0].Point = Vec3d{X: -999, Y: -999, Z: -999}
	cfg.Roll[1].V = 99

	const count = 64
	times := make([]time.Duration, count)
	forward := make([]CameraState, count)
	for i := range count {
		times[i] = time.Duration(i*350-2000) * time.Millisecond
		forward[i] = p.Eval(times[i])
	}

	// Backward order must match bit-for-bit.
	for i := count - 1; i >= 0; i-- {
		got := p.Eval(times[i])
		if got != forward[i] {
			t.Fatalf("backward eval mismatch at %v: got %+v, want %+v", times[i], got, forward[i])
		}
	}

	// Concurrent random-order evaluation across 16 goroutines under -race.
	var wg sync.WaitGroup
	for g := range 16 {
		wg.Add(1)
		go func(seed uint64) {
			defer wg.Done()
			rng := rand.New(rand.NewPCG(seed+1, 0xcafe))
			for range 200 {
				idx := rng.IntN(count)
				got := p.Eval(times[idx])
				if got != forward[idx] {
					t.Errorf("concurrent eval mismatch at %v: got %+v, want %+v", times[idx], got, forward[idx])
				}
			}
		}(uint64(g))
	}
	wg.Wait()
}

func TestRollRightHandedConventionAndLookOverlap(t *testing.T) {
	// Camera at (0, -5, 0) looking straight along +Y with world +Z up:
	// unrolled basis has Right = +X, Up = +Z, Forward = +Y.
	// Positive right-handed roll (+90 deg) around Forward (+Y, i.e. local -Z)
	// rotates local +X (Right) -> -Z (world) or let's verify exact right-hand rule:
	// Right-hand thumb along Forward = (0, +1, 0): curling fingers from +Z toward +X!
	// Wait: let's check exp((theta/2)*(0,0,-1)) in local camera coords:
	// In local camera coords (+X right, +Y up, -Z forward):
	// Rotation by +90 deg around axis a = (0, 0, -1):
	// v' = cos(90)v + (1-cos(90))(a·v)a + sin(90)(a × v)
	// For local Up = (0, 1, 0): a × Up = (0, 0, -1) × (0, 1, 0) = (+1, 0, 0) = +Right!
	// So positive roll tilts camera Up (+Y local) toward camera Right (+X local) — classic right-bank!
	p, err := NewPath(PathConfig{
		Duration: 2 * time.Second,
		Position: PositionTrack{
			Interp: InterpLinear,
			Points: []Vec3d{{X: 0, Y: -5, Z: 0}, {X: 0, Y: -4, Z: 0}},
		},
		Look: []LookSegment{
			{Mode: LookTarget, Point: Vec3d{X: 0, Y: 10, Z: 0}, From: 0},
			// Overlapping blends to verify C0 continuity across stacked crossfades.
			{Mode: LookTarget, Point: Vec3d{X: 1, Y: 10, Z: 0}, From: 500 * time.Millisecond, Blend: 1200 * time.Millisecond},
			{Mode: LookTarget, Point: Vec3d{X: 0, Y: 10, Z: 0}, From: 1000 * time.Millisecond, Blend: 800 * time.Millisecond},
		},
		Roll: []Keyframe{
			{T: 0, V: math.Pi / 2},
			{T: 2 * time.Second, V: math.Pi / 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	st0 := p.Eval(0)
	// At t=0: Forward = (0, 1, 0), Up tilted +90 deg around Forward -> (+1, 0, 0)!
	if st0.Forward().Sub(Vec3d{Y: 1}).Len() > 1e-12 {
		t.Fatalf("expected forward +Y, got %+v", st0.Forward())
	}
	if st0.Up().Sub(Vec3d{X: 1}).Len() > 1e-12 {
		t.Fatalf("expected +90 deg right-bank to map Up -> +X, got %+v", st0.Up())
	}

	// Verify C0 continuity across overlapping look blends.
	prev := p.Eval(0)
	for ms := int64(10); ms <= 2000; ms += 10 {
		cur := p.Eval(time.Duration(ms) * time.Millisecond)
		if cur.Forward().Dot(prev.Forward()) < 0.999 {
			t.Fatalf("orientation jump across overlapping look blends at %dms", ms)
		}
		prev = cur
	}
}

func TestLoopModesBlendSwitchAndFitRect(t *testing.T) {
	p, err := NewPath(PathConfig{
		Duration: 4 * time.Second,
		Loop:     LoopPingPong,
		Position: PositionTrack{
			Interp: InterpLinear,
			Points: []Vec3d{{X: 0, Y: 0, Z: 0}, {X: 0, Y: 10, Z: 0}},
		},
		Look: []LookSegment{{Mode: LookTangent, ReverseWithMotion: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	fwd := p.Eval(1 * time.Second)
	bwd := p.Eval(7 * time.Second) // 8s period: 7s wraps to 1s moving backward!
	if fwd.Pos.Sub(bwd.Pos).Len() > 1e-12 {
		t.Fatalf("pingpong position mismatch: %+v vs %+v", fwd.Pos, bwd.Pos)
	}
	if fwd.Forward().Add(bwd.Forward()).Len() > 1e-12 {
		t.Fatalf("expected ReverseWithMotion to flip tangent on return leg: fwd=%+v bwd=%+v", fwd.Forward(), bwd.Forward())
	}

	// Focal length <-> FovY round-trip.
	fov35 := FocalLengthToFovY(35, 24)
	if got := FovYToFocalLength(fov35, 24); math.Abs(got-35) > 1e-12 {
		t.Fatalf("focal length round-trip: got %g, want 35", got)
	}

	// Blend & Switch.
	b := Blend(fwd, bwd, 0.5)
	if math.Abs(b.Rot.Norm()-1) > 1e-12 {
		t.Fatalf("Blend produced non-unit quaternion: %g", b.Rot.Norm())
	}
	sw := Switch([]CameraState{fwd, bwd}, 0.5, EaseSpec{Kind: EaseSmoothstep})
	if math.Abs(sw.Rot.Norm()-1) > 1e-12 {
		t.Fatalf("Switch produced non-unit quaternion: %g", sw.Rot.Norm())
	}

	// FitRect: framing a 4m x 2m panel at 90 deg vertical FOV and 2:1 aspect with margin 1.0
	// requires distance = (height/2)/tan(45 deg) = 1.0 m along normal.
	pos, rot := FitRect(Vec3d{X: 5, Y: 2, Z: 1.5}, Vec3d{Y: -1}, DefaultUp, 4, 2, math.Pi/2, 2.0, 1.0)
	if pos.Sub(Vec3d{X: 5, Y: 1, Z: 1.5}).Len() > 1e-12 {
		t.Fatalf("FitRect pos: got %+v, want (5, 1, 1.5)", pos)
	}
	stFit := CameraState{Pos: pos, Rot: rot}
	if stFit.Forward().Sub(Vec3d{Y: 1}).Len() > 1e-12 {
		t.Fatalf("FitRect forward: got %+v, want +Y", stFit.Forward())
	}
}
