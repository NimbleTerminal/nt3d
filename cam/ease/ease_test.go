// Copyright (c) 2026 Neomantra Corp.

package nt3dease

import (
	"math"
	"testing"
	"time"
)

func TestEasingEndpointsAndMonotonicity(t *testing.T) {
	specs := []Spec{
		{Kind: Linear},
		{Kind: Ease},
		{Kind: EaseIn},
		{Kind: EaseOut},
		{Kind: EaseInOut},
		{Kind: Smoothstep},
		{Kind: Smootherstep},
		{Kind: Hold},
		{Kind: CubicBezier, P: [4]float64{0, 0, 1, 1}},          // steep zero-derivative endpoints
		{Kind: CubicBezier, P: [4]float64{1, 0, 0, 1}},          // S-inflection with vertical tangent
		{Kind: CubicBezier, P: [4]float64{0.34, 1.56, 0.64, 1}}, // overshoot allowed for scalar tracks
	}
	for i, s := range specs {
		if err := s.Validate(); err != nil {
			t.Fatalf("spec %d validate: %v", i, err)
		}
		if got := s.Eval(0); got != 0 {
			t.Fatalf("spec %d Eval(0) = %g, want 0", i, got)
		}
		if got := s.Eval(1); got != 1 {
			t.Fatalf("spec %d Eval(1) = %g, want 1", i, got)
		}
		if s.Kind == CubicBezier && s.P[1] > 1 {
			if mid := s.Eval(0.6); mid <= 1.0 {
				t.Fatalf("expected overshoot cubic-bezier > 1 at u=0.6, got %g", mid)
			}
			continue
		}
		prev := 0.0
		for step := 1; step <= 200; step++ {
			u := float64(step) / 200.0
			y := s.Eval(u)
			if y < prev-1e-12 {
				t.Fatalf("spec %d non-monotonic at u=%g: %g < %g", i, u, y, prev)
			}
			prev = y
		}
	}
}

func TestSampleKeyframes(t *testing.T) {
	keys := []Key{
		{T: 0, V: 10},
		{T: 2 * time.Second, V: 30, Ease: Spec{Kind: Linear}},
		{T: 4 * time.Second, V: 50, Ease: Spec{Kind: Hold}},
		{T: 6 * time.Second, V: 90, Ease: Spec{Kind: Smoothstep}},
	}
	if err := ValidateKeys(keys); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		t    time.Duration
		want float64
	}{
		{-time.Second, 10},
		{0, 10},
		{time.Second, 20},
		{2 * time.Second, 30},
		{3999 * time.Millisecond, 30}, // hold until 4s
		{4 * time.Second, 50},
		{5 * time.Second, 70}, // smoothstep(0.5) == 0.5
		{6 * time.Second, 90},
		{10 * time.Second, 90},
	}
	for _, tc := range cases {
		got := Sample(keys, tc.t, -1)
		if math.Abs(got-tc.want) > 1e-12 {
			t.Errorf("Sample(%v) = %g, want %g", tc.t, got, tc.want)
		}
	}
}
