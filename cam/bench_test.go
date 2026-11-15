// Copyright (c) 2026 Neomantra Corp.

package nt3dcam

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func makeBenchConfig(numPoints int) PathConfig {
	pts := make([]Vec3d, numPoints)
	for i := range numPoints {
		theta := float64(i) * 0.35
		pts[i] = Vec3d{
			X: 25 * math.Cos(theta),
			Y: 25 * math.Sin(theta),
			Z: 4 + 2*math.Sin(theta*0.5),
		}
	}
	return PathConfig{
		Name:     fmt.Sprintf("bench-%d", numPoints),
		Duration: 30 * time.Second,
		Loop:     LoopRepeat,
		Position: PositionTrack{
			Interp: InterpCatmullRomCentripetal,
			Points: pts,
		},
		Timing: TimingTrack{
			Segments: []TimingSegment{
				{To: 0.4, Over: 12 * time.Second, Ease: EaseSpec{Kind: EaseInOut}},
				{To: 1.0, Over: 18 * time.Second, Ease: EaseSpec{Kind: EaseOut}},
			},
		},
		Look: []LookSegment{
			{Mode: LookTarget, Point: Vec3d{Z: 2}, From: 0},
			{Mode: LookTangent, From: 10 * time.Second, Blend: 3 * time.Second},
		},
		Roll: []Keyframe{
			{T: 0, V: 0},
			{T: 15 * time.Second, V: 0.18, Ease: EaseSpec{Kind: EaseSmoothstep}},
			{T: 30 * time.Second, V: 0, Ease: EaseSpec{Kind: EaseSmoothstep}},
		},
		Lens: LensTrack{
			FovY: []Keyframe{
				{T: 0, V: 50 * math.Pi / 180},
				{T: 30 * time.Second, V: 35 * math.Pi / 180, Ease: EaseSpec{Kind: EaseInOut}},
			},
			FocusDist: []Keyframe{{T: 0, V: 25}, {T: 30 * time.Second, V: 15}},
			Aperture:  []Keyframe{{T: 0, V: 2.8}},
			Near:      0.1,
			Far:       2000,
		},
	}
}

func BenchmarkEval(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		p, err := NewPath(makeBenchConfig(n))
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("Points_%d", n), func(b *testing.B) {
			b.ReportAllocs()
			var sink CameraState
			dur := int64(p.Duration())
			for i := range b.N {
				ts := time.Duration((int64(i) * 16_666_667) % dur)
				sink = p.Eval(ts)
			}
			_ = sink
		})
	}
}

func BenchmarkNewPath(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		cfg := makeBenchConfig(n)
		b.Run(fmt.Sprintf("Points_%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				p, err := NewPath(cfg)
				if err != nil {
					b.Fatal(err)
				}
				_ = p
			}
		})
	}
}
