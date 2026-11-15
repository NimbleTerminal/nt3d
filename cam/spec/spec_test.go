// Copyright (c) 2026 Neomantra Corp.

package nt3dcamspec

import (
	"math"
	"strings"
	"testing"
	"time"

	nt3dcam "nimbleterminal.dev/nt3d/cam"
)

const promptSketchYAML = `
camera_path:
  version: 1
  frame: scene
  duration: 12s
  loop: none
  position:
    interp: catmull-rom-centripetal
    points: [[0, -10, 4], [6, -6, 3.5], [8, 2, 2.5]]
  timing:
    - {to: 1.0, over: 12s, ease: ease-in-out}
  look:
    - {mode: target, target: "node:island", from: 0s}
    - {mode: tangent, from: 8s, blend: 1.5s}
  lens:
    fov: [{t: 0s, v: 55}, {t: 12s, v: 35, ease: ease-in-out}]
  markers:
    - {name: reveal, t: 6s}
`

const fullSpecYAML = `---
# Full camera_path v1 specification example
camera_path:
  version: 1
  name: island-flyover
  frame: enu
  origin: {lat: 40.7411, lon: -73.9897, alt: 10.5}
  up: [0, 0, 1]
  duration: 12s
  loop: pingpong

  position:
    interp: catmull-rom-centripetal
    closed: false
    points:
      - [0, -10, 4]
      - [6, -6, 3.5]
      - [8, 2, 2.5]
      - [14, 9, 3.0]

  timing:
    start: 0.05
    segments:
      - to: 0.45
        over: 5s
        ease: [0.25, 0.1, 0.25, 1.0]
      - to: 1.0
        over: 7s
        ease: smootherstep

  look:
    - mode: target
      target: node:island
      fallback: [10, 5, 1]
      from: 0s
    - mode: tangent
      reverse_with_motion: true
      from: 7s
      blend: 1.5s
      blend_ease: linear

  roll:
    - {t: 0s, v: 0}
    - {t: 8s, v: -6, ease: ease-in-out}
    - {t: 12s, v: 0, ease: ease-in-out}

  lens:
    focal_length:
      - {t: 0s, v: 24}
      - {t: 12s, v: 50, ease: ease-in-out}
    sensor_height: 24
    focus_dist: [{t: 0s, v: 10.0}, {t: 12s, v: 18.5, ease: smoothstep}]
    aperture: [{t: 0s, v: 2.8}]
    near: 0.05
    far: 1500.0

  markers:
    - {name: reveal, t: 6s}
    - {name: apex, t: 10.5s}
...`

const bezierAndQuatJSON = `{
  "camera_path": {
    "version": 1,
    "name": "bezier-quat-rig",
    "duration": 6.0,
    "loop": "loop",
    "position": {
      "interp": "bezier",
      "knots": [
        {"p": [0, 0, 2], "tangent_out": [6, 0, 3]},
        {"p": [6, 4, 3], "in": [4, 2, 3], "out": [8, 6, 3]},
        {"p": [10, 10, 2], "tangent_in": [3, 6, 0]}
      ]
    },
    "look": [
      {
        "mode": "keyframes",
        "from": "0s",
        "interp": "squad",
        "keys": [
          {"t": "0s", "q": [0, 0, 0, 1]},
          {"t": "3s", "q": [0, 0, 0.3826834323650898, 0.9238795325112867], "ease": "smoothstep"},
          {"t": "6s", "q": [0, 0, 0.7071067811865476, 0.7071067811865476], "ease": "ease-out"}
        ]
      }
    ],
    "lens": {
      "fov": [{"t": 0, "v": 45}]
    }
  }
}`

func TestParseAndRoundTripYAMLAndJSON(t *testing.T) {
	res := nt3dcam.ResolverFunc(func(ref string, _ time.Duration) (nt3dcam.TargetInfo, bool) {
		if ref == "node:island" {
			return nt3dcam.TargetInfo{Pos: nt3dcam.Vec3d{X: 12, Y: 4, Z: 1}}, true
		}
		return nt3dcam.TargetInfo{}, false
	})

	cases := []struct {
		name string
		raw  string
	}{
		{name: "prompt-sketch-yaml", raw: promptSketchYAML},
		{name: "full-spec-yaml", raw: fullSpecYAML},
		{name: "bezier-quat-json", raw: bezierAndQuatJSON},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p1, err := Compile([]byte(tc.raw), nt3dcam.WithResolver(res))
			if err != nil {
				t.Fatalf("Compile initial failed: %v", err)
			}

			doc := FromPath(p1)
			yamlOut, err := MarshalYAML(doc)
			if err != nil {
				t.Fatalf("MarshalYAML failed: %v", err)
			}
			jsonOut, err := MarshalJSON(doc)
			if err != nil {
				t.Fatalf("MarshalJSON failed: %v", err)
			}

			pYAML, err := Compile(yamlOut, nt3dcam.WithResolver(res))
			if err != nil {
				t.Fatalf("Compile round-tripped YAML failed: %v\nYAML:\n%s", err, string(yamlOut))
			}
			pJSON, err := Compile(jsonOut, nt3dcam.WithResolver(res))
			if err != nil {
				t.Fatalf("Compile round-tripped JSON failed: %v\nJSON:\n%s", err, string(jsonOut))
			}

			const tol = 1e-9
			for k := 0; k <= 24; k++ {
				ts := time.Duration(int64(p1.Duration()) * int64(k) / 16) // tests inside & past Duration!
				s1 := p1.Eval(ts)
				sY := pYAML.Eval(ts)
				sJ := pJSON.Eval(ts)
				assertStatesMatch(t, "YAML", ts, s1, sY, tol)
				assertStatesMatch(t, "JSON", ts, s1, sJ, tol)
			}
		})
	}
}

func assertStatesMatch(t *testing.T, label string, ts time.Duration, a, b nt3dcam.CameraState, tol float64) {
	t.Helper()
	if a.Pos.Sub(b.Pos).Len() > tol {
		t.Fatalf("%s round-trip Pos mismatch at %v: got %+v, want %+v", label, ts, b.Pos, a.Pos)
	}
	dot := math.Abs(a.Rot.Dot(b.Rot))
	if math.Abs(1-dot) > tol {
		t.Fatalf("%s round-trip Rot mismatch at %v: got %+v, want %+v (|dot|=%g)", label, ts, b.Rot, a.Rot, dot)
	}
	if math.Abs(a.FovY-b.FovY) > tol ||
		math.Abs(a.FocusDist-b.FocusDist) > tol ||
		math.Abs(a.Aperture-b.Aperture) > tol ||
		math.Abs(a.Near-b.Near) > tol ||
		math.Abs(a.Far-b.Far) > tol {
		t.Fatalf("%s round-trip Lens mismatch at %v: got %+v, want %+v", label, ts, b, a)
	}
}

func TestRejectInvalidSpecs(t *testing.T) {
	badSpecs := []struct {
		name    string
		spec    string
		wantSub string
	}{
		{
			name:    "unsupported-version",
			spec:    `{ "version": 2, "duration": "4s", "position": {"points": [[0,0,0],[1,1,1]]} }`,
			wantSub: "unsupported version",
		},
		{
			name:    "unknown-field",
			spec:    `{ "version": 1, "duration": "4s", "bogus": 123, "position": {"points": [[0,0,0],[1,1,1]]} }`,
			wantSub: "unknown field",
		},
		{
			name:    "v2-samples-reserved",
			spec:    `{ "version": 1, "duration": "4s", "position": {"samples": [{"t":"0s","p":[0,0,0]}]} }`,
			wantSub: "reserved for v2",
		},
		{
			name:    "enu-missing-origin",
			spec:    `{ "version": 1, "frame": "enu", "duration": "4s", "position": {"points": [[0,0,0],[1,1,1]]} }`,
			wantSub: "requires geo origin",
		},
		{
			name:    "both-fov-and-focal-length",
			spec:    `{ "version": 1, "duration": "4s", "position": {"points": [[0,0,0],[1,1,1]]}, "lens": {"fov":[{"t":"0s","v":45}], "focal_length":[{"t":"0s","v":35}]} }`,
			wantSub: "both fov and focal_length",
		},
		{
			name:    "invalid-target-prefix",
			spec:    `{ "version": 1, "duration": "4s", "position": {"points": [[0,0,0],[1,1,1]]}, "look": [{"mode":"target","target":"entity:foo"}] }`,
			wantSub: "invalid target reference",
		},
		{
			name:    "cubic-bezier-x-out-of-range",
			spec:    `{ "version": 1, "duration": "4s", "position": {"points": [[0,0,0],[1,1,1]]}, "timing": [{"to":1,"over":"4s","ease":[-0.2,0,0.8,1]}] }`,
			wantSub: "must be in [0, 1]",
		},
		{
			name:    "yaml-tab-indentation",
			spec:    "camera_path:\n\tversion: 1\n",
			wantSub: "tabs are not allowed",
		},
		{
			name:    "yaml-duplicate-key",
			spec:    "camera_path:\n  version: 1\n  version: 1\n",
			wantSub: "duplicate key",
		},
	}

	for _, tc := range badSpecs {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile([]byte(tc.spec))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("expected error containing %q, got: %v", tc.wantSub, err)
			}
		})
	}
}
