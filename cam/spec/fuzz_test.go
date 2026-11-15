// Copyright (c) 2026 Neomantra Corp.

package nt3dcamspec

import (
	"math"
	"testing"
	"time"
)

func FuzzParse(f *testing.F) {
	f.Add([]byte(promptSketchYAML))
	f.Add([]byte(fullSpecYAML))
	f.Add([]byte(bezierAndQuatJSON))
	f.Add([]byte(`{"version":1,"duration":"1s","position":{"points":[[0,0,0]]}}`))
	f.Add([]byte("camera_path:\n  version: 1\n  duration: 0s\n  position:\n    points: [[1, 2, 3]]\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := Compile(data)
		if err != nil {
			return
		}
		for _, ts := range []time.Duration{-time.Second, 0, p.Duration() / 2, p.Duration(), p.Duration() + time.Second} {
			st := p.Eval(ts)
			if !st.Pos.IsFinite() {
				t.Fatalf("non-finite position %+v at %v", st.Pos, ts)
			}
			if math.IsNaN(st.Rot.Norm()) || math.Abs(st.Rot.Norm()-1) > 1e-9 {
				t.Fatalf("non-unit quaternion %+v (norm=%g) at %v", st.Rot, st.Rot.Norm(), ts)
			}
			if !(st.FovY > 0 && st.FovY < math.Pi) {
				t.Fatalf("invalid FovY %g at %v", st.FovY, ts)
			}
		}

		// Every valid parsed Path must round-trip cleanly through YAML and JSON.
		doc := FromPath(p)
		if yamlOut, err := MarshalYAML(doc); err != nil {
			t.Fatalf("MarshalYAML failed on valid Path: %v", err)
		} else if _, err := Compile(yamlOut); err != nil {
			t.Fatalf("round-trip YAML Compile failed: %v\n%s", err, string(yamlOut))
		}
		if jsonOut, err := MarshalJSON(doc); err != nil {
			t.Fatalf("MarshalJSON failed on valid Path: %v", err)
		} else if _, err := Compile(jsonOut); err != nil {
			t.Fatalf("round-trip JSON Compile failed: %v\n%s", err, string(jsonOut))
		}
	})
}
