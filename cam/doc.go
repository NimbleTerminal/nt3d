// Copyright (c) 2026 Neomantra Corp.

// Package nt3dcam evaluates declarative 3D camera paths into pure, deterministic
// float64 camera states.
//
// A Path separates spatial curve geometry (PositionTrack, arc-length
// parameterized at load time), speed profile (TimingTrack), orientation
// (LookSegment with target tracking, parallel-transport curve tangents, or
// slerp/squad quaternion keyframes, plus roll), and optics (LensTrack).
//
// Calling Path.Eval or Path.EvalWith at any timestamp t is stateless,
// frame-rate independent, and safe for concurrent callers without locks.
// Package nt3dcam depends only on the Go standard library.
package nt3dcam
