// Copyright (c) 2026 Neomantra Corp.

package nt3dcam

import (
	"fmt"
	"math"
	"slices"
	"time"

	nt3dcurve "nimbleterminal.dev/nt3d/cam/curve"
	nt3dease "nimbleterminal.dev/nt3d/cam/ease"
	nt3dframes "nimbleterminal.dev/nt3d/cam/frames"
)

// FrameKind identifies the coordinate frame of a camera path.
type FrameKind uint8

// Supported path coordinate frames.
const (
	FrameScene FrameKind = iota // Scene-local meters (default).
	FrameENU                    // East-North-Up meters anchored at GeoOrigin.
)

// String returns the canonical spec name of k ("scene" or "enu").
func (k FrameKind) String() string {
	if k == FrameENU {
		return "enu"
	}
	return "scene"
}

// LoopMode controls how evaluation time wraps outside [0, Duration].
type LoopMode uint8

// Supported playback loop modes.
const (
	LoopNone     LoopMode = iota // Clamp t to [0, Duration].
	LoopRepeat                   // Wrap t modulo Duration.
	LoopPingPong                 // Reflect t back and forth across [0, Duration].
)

// String returns the canonical spec name of m ("none", "loop", or "pingpong").
func (m LoopMode) String() string {
	switch m {
	case LoopRepeat:
		return "loop"
	case LoopPingPong:
		return "pingpong"
	default:
		return "none"
	}
}

// InterpMode selects spatial curve interpolation for PositionTrack.
type InterpMode uint8

// Supported spatial curve interpolations.
const (
	InterpCatmullRomCentripetal InterpMode = iota // Through-point centripetal Catmull-Rom (alpha = 0.5, default).
	InterpBezier                                  // Piecewise cubic Bezier / Hermite knots.
	InterpBSpline                                 // Approximating uniform clamped/periodic cubic B-spline.
	InterpLinear                                  // Piecewise linear polyline.
)

// String returns the canonical spec name of m.
func (m InterpMode) String() string {
	switch m {
	case InterpBezier:
		return "bezier"
	case InterpBSpline:
		return "bspline"
	case InterpLinear:
		return "linear"
	default:
		return "catmull-rom-centripetal"
	}
}

// BezierKnot is an anchor point P with explicit cubic Bezier handles In and Out
// in world coordinates. Hermite tangents (mIn, mOut) convert via
// In = P - mIn/3 and Out = P + mOut/3.
type BezierKnot struct {
	P   Vec3d
	In  Vec3d
	Out Vec3d
}

// PositionTrack defines the spatial curve traversed by the camera.
// Exactly one of Points (for Catmull-Rom, B-spline, or Linear) or Knots
// (for InterpBezier) is populated.
type PositionTrack struct {
	Interp InterpMode
	Closed bool
	Points []Vec3d
	Knots  []BezierKnot
}

// EaseKind identifies the easing curve applied across a timing segment,
// keyframe interval, or orientation crossfade.
type EaseKind uint8

// Supported easing curve kinds.
// The zero value (EaseDefault) selects each track's natural default
// (EaseLinear for timing, scalar, quaternion, and switcher tracks;
// EaseInOut for LookSegment.BlendEase).
const (
	EaseDefault      EaseKind = iota // Context default (linear, or ease-in-out for look blends).
	EaseLinear                       // Explicit f(u) = u.
	EaseCSS                          // CSS ease: cubic-bezier(0.25, 0.1, 0.25, 1.0).
	EaseIn                           // CSS ease-in: cubic-bezier(0.42, 0.0, 1.0, 1.0).
	EaseOut                          // CSS ease-out: cubic-bezier(0.0, 0.0, 0.58, 1.0).
	EaseInOut                        // CSS ease-in-out: cubic-bezier(0.42, 0.0, 0.58, 1.0).
	EaseSmoothstep                   // 3u^2 - 2u^3.
	EaseSmootherstep                 // 6u^5 - 15u^4 + 10u^3.
	EaseHold                         // Step: 0 for u < 1, 1 at u == 1.
	EaseCubicBezier                  // Custom CSS cubic-bezier(P[0], P[1], P[2], P[3]).
)

// EaseSpec parameterizes a 1D easing curve over u in [0, 1].
type EaseSpec struct {
	Kind EaseKind
	P    [4]float64 // Control points [x1, y1, x2, y2] when Kind == EaseCubicBezier.
}

func (e EaseSpec) toEase(def nt3dease.Kind) nt3dease.Spec {
	switch e.Kind {
	case EaseDefault:
		return nt3dease.Spec{Kind: def}
	case EaseLinear:
		return nt3dease.Spec{Kind: nt3dease.Linear}
	case EaseCSS:
		return nt3dease.Spec{Kind: nt3dease.Ease}
	case EaseIn:
		return nt3dease.Spec{Kind: nt3dease.EaseIn}
	case EaseOut:
		return nt3dease.Spec{Kind: nt3dease.EaseOut}
	case EaseInOut:
		return nt3dease.Spec{Kind: nt3dease.EaseInOut}
	case EaseSmoothstep:
		return nt3dease.Spec{Kind: nt3dease.Smoothstep}
	case EaseSmootherstep:
		return nt3dease.Spec{Kind: nt3dease.Smootherstep}
	case EaseHold:
		return nt3dease.Spec{Kind: nt3dease.Hold}
	case EaseCubicBezier:
		return nt3dease.Spec{Kind: nt3dease.CubicBezier, P: e.P}
	default:
		return nt3dease.Spec{Kind: nt3dease.Kind(255)}
	}
}

// TimingSegment advances normalized arc-length s to To in [0, 1] over Over duration.
type TimingSegment struct {
	To   float64
	Over time.Duration
	Ease EaseSpec
}

// TimingTrack maps wrapped time t in [0, Duration] to normalized arc-length s in [0, 1].
// Empty Segments defaults to constant speed from 0 to 1 over PathConfig.Duration.
type TimingTrack struct {
	Start    float64
	Segments []TimingSegment
}

// Keyframe is a scalar keyframe at timestamp T. On keyframe i > 0, Ease governs
// interpolation over [keys[i-1].T, keys[i].T]. Internal angle keyframes store
// radians; spec loaders convert degrees to radians at parse time.
type Keyframe struct {
	T    time.Duration
	V    float64
	Ease EaseSpec
}

// QuatInterp selects spherical interpolation across QuatKeyframe tracks.
type QuatInterp uint8

// Supported quaternion keyframe interpolations.
const (
	QuatSquad QuatInterp = iota // C1 spherical cubic quadrangle interpolation (default).
	QuatSlerp                   // Piecewise spherical linear interpolation.
)

// String returns "squad" or "slerp".
func (q QuatInterp) String() string {
	if q == QuatSlerp {
		return "slerp"
	}
	return "squad"
}

// QuatKeyframe is a unit-quaternion keyframe at timestamp T.
type QuatKeyframe struct {
	T    time.Duration
	Q    Quatd
	Ease EaseSpec
}

// LookMode selects how a LookSegment computes base camera orientation.
type LookMode uint8

// Supported camera orientation modes.
const (
	LookTarget    LookMode = iota // Aim local -Z at a literal point or resolved reference.
	LookTangent                   // Align local -Z with the curve's parallel-transport frame.
	LookKeyframes                 // Interpolate explicit unit-quaternion keyframes.
)

// String returns "target", "tangent", or "keyframes".
func (m LookMode) String() string {
	switch m {
	case LookTangent:
		return "tangent"
	case LookKeyframes:
		return "keyframes"
	default:
		return "target"
	}
}

// LookSegment defines camera orientation starting at From, optionally
// crossfading from the preceding segment over [From, From+Blend].
type LookSegment struct {
	Mode              LookMode
	From              time.Duration
	Blend             time.Duration
	BlendEase         EaseSpec
	TargetRef         string         // External reference ("node:...", "panel:...", "path:...") for LookTarget.
	Point             Vec3d          // Literal target point when TargetRef == "".
	HasFallback       bool           // Whether Fallback overrides (0,0,0) when TargetRef is unresolved.
	Fallback          Vec3d          // Deterministic fallback target when TargetRef is unresolved.
	ReverseWithMotion bool           // Flip LookTangent to -T(s) when ds/dt < 0.
	Interp            QuatInterp     // Interpolation for LookKeyframes.
	Keys              []QuatKeyframe // Keyframes when Mode == LookKeyframes.
}

// LensTrack holds keyframed optical parameters and static clip planes.
// FovY stores vertical field of view in radians; empty FovY defaults to DefaultFovY.
type LensTrack struct {
	FovY      []Keyframe // Vertical FOV in radians.
	FocusDist []Keyframe // Focus distance in meters (0 = unset).
	Aperture  []Keyframe // F-stop number (0 = pinhole/disabled).
	Near      float64    // Static near clip plane in meters (0 = host auto).
	Far       float64    // Static far clip plane in meters (0 = host auto).
}

// Marker names a timestamp along the path for cuts, cues, and synchronization.
type Marker struct {
	Name string
	T    time.Duration
}

// PathConfig is the validated, radian/meter input representation compiled into
// an immutable Path with precomputed arc-length and parallel-transport tables.
type PathConfig struct {
	Name     string
	Frame    FrameKind
	Origin   *GeoOrigin
	Up       Vec3d // Zero uses DefaultUp (+Z).
	Duration time.Duration
	Loop     LoopMode
	Position PositionTrack
	Timing   TimingTrack
	Look     []LookSegment
	Roll     []Keyframe // Roll angle in radians around local forward (-Z).
	Lens     LensTrack
	Markers  []Marker
}

// EvalOverrides supplies optional per-evaluation dynamic inputs (used by Nebula
// inlets) without mutating a compiled Path.
type EvalOverrides struct {
	Resolver   Resolver // Per-call reference resolver (overrides Path's default resolver when non-nil).
	Target     *Vec3d   // Overrides LookTarget aim position when non-nil.
	RollRad    *float64 // Overrides the Roll track (in radians) when non-nil.
	RollAddRad float64  // Added to evaluated roll (in radians).
}

// Option configures Path compilation in NewPath.
type Option func(*buildOptions)

type buildOptions struct {
	resolver       Resolver
	samplesPerSpan int
}

// WithResolver binds a default Resolver used by Path.Eval when no per-call
// Resolver is passed.
func WithResolver(r Resolver) Option {
	return func(o *buildOptions) { o.resolver = r }
}

// WithSamplesPerSpan sets the arc-length and parallel-transport table density
// per curve span (default 32 when <= 0).
func WithSamplesPerSpan(n int) Option {
	return func(o *buildOptions) { o.samplesPerSpan = n }
}

type compiledTimingSeg struct {
	t0, t1 time.Duration
	s0, s1 float64
	ease   nt3dease.Spec
}

type compiledLookSeg struct {
	mode              LookMode
	from              time.Duration
	blend             time.Duration
	blendEase         nt3dease.Spec
	targetRef         string
	point             Vec3d
	fallback          Vec3d
	reverseWithMotion bool
	quatTrack         *nt3dframes.Track
}

// Path is an immutable, precompiled camera path safe for concurrent evaluation.
type Path struct {
	cfg        PathConfig
	worldUp    Vec3d
	lut        *nt3dcurve.LUT
	transport  *nt3dframes.ParallelTransport
	timingSegs []compiledTimingSeg
	timingInit float64
	lookSegs   []compiledLookSeg
	rollKeys   []nt3dease.Key
	fovKeys    []nt3dease.Key
	focusKeys  []nt3dease.Key
	apertKeys  []nt3dease.Key
	resolver   Resolver
}

// NewPath validates cfg, precomputes arc-length and parallel-transport lookup
// tables, and returns an immutable Path.
func NewPath(cfg PathConfig, opts ...Option) (*Path, error) {
	var bo buildOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&bo)
		}
	}

	if cfg.Duration < 0 {
		return nil, fmt.Errorf("nt3dcam: negative duration %v", cfg.Duration)
	}
	if cfg.Frame > FrameENU {
		return nil, fmt.Errorf("nt3dcam: invalid frame kind %d", cfg.Frame)
	}
	if cfg.Loop > LoopPingPong {
		return nil, fmt.Errorf("nt3dcam: invalid loop mode %d", cfg.Loop)
	}
	if cfg.Frame == FrameENU {
		if cfg.Origin == nil {
			return nil, fmt.Errorf("nt3dcam: enu frame requires geo origin")
		}
		o := cfg.Origin
		if math.IsNaN(o.LatDeg) || math.IsInf(o.LatDeg, 0) || o.LatDeg < -90 || o.LatDeg > 90 ||
			math.IsNaN(o.LonDeg) || math.IsInf(o.LonDeg, 0) || o.LonDeg < -180 || o.LonDeg > 180 ||
			math.IsNaN(o.AltM) || math.IsInf(o.AltM, 0) {
			return nil, fmt.Errorf("nt3dcam: invalid geo origin %+v", *o)
		}
	}

	worldUp := cfg.Up
	if worldUp == (Vec3d{}) {
		worldUp = DefaultUp
	}
	if !worldUp.IsFinite() || worldUp.LenSq() < 1e-24 {
		return nil, fmt.Errorf("nt3dcam: invalid world up vector %+v", cfg.Up)
	}
	worldUp = worldUp.Normalize()

	spans, err := compilePosition(cfg.Position)
	if err != nil {
		return nil, err
	}
	lut := nt3dcurve.NewLUT(spans, bo.samplesPerSpan, cfg.Position.Closed)
	transport := nt3dframes.NewParallelTransport(lut, worldUp)

	timingInit, timingSegs, err := compileTiming(cfg.Timing, cfg.Duration)
	if err != nil {
		return nil, err
	}

	lookSegs, err := compileLook(cfg.Look)
	if err != nil {
		return nil, err
	}

	rollKeys, err := compileScalarKeys("roll", cfg.Roll, func(float64) error { return nil })
	if err != nil {
		return nil, err
	}
	fovKeys, err := compileScalarKeys("lens.fov", cfg.Lens.FovY, func(v float64) error {
		if v <= 0 || v >= math.Pi {
			return fmt.Errorf("fov %g rad out of (0, pi)", v)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	focusKeys, err := compileScalarKeys("lens.focus_dist", cfg.Lens.FocusDist, func(v float64) error {
		if v < 0 {
			return fmt.Errorf("negative focus_dist %g", v)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	apertKeys, err := compileScalarKeys("lens.aperture", cfg.Lens.Aperture, func(v float64) error {
		if v < 0 {
			return fmt.Errorf("negative aperture %g", v)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if math.IsNaN(cfg.Lens.Near) || math.IsInf(cfg.Lens.Near, 0) || cfg.Lens.Near < 0 ||
		math.IsNaN(cfg.Lens.Far) || math.IsInf(cfg.Lens.Far, 0) || cfg.Lens.Far < 0 ||
		(cfg.Lens.Near > 0 && cfg.Lens.Far > 0 && cfg.Lens.Far <= cfg.Lens.Near) {
		return nil, fmt.Errorf("nt3dcam: invalid clip planes near=%g far=%g", cfg.Lens.Near, cfg.Lens.Far)
	}

	seenMarkers := make(map[string]struct{}, len(cfg.Markers))
	for i, m := range cfg.Markers {
		if m.Name == "" {
			return nil, fmt.Errorf("nt3dcam: marker %d has empty name", i)
		}
		if _, dup := seenMarkers[m.Name]; dup {
			return nil, fmt.Errorf("nt3dcam: duplicate marker %q", m.Name)
		}
		seenMarkers[m.Name] = struct{}{}
		if m.T < 0 || (cfg.Duration > 0 && m.T > cfg.Duration) {
			return nil, fmt.Errorf("nt3dcam: marker %q time %v outside [0, %v]", m.Name, m.T, cfg.Duration)
		}
	}

	return &Path{
		cfg:        clonePathConfig(cfg),
		worldUp:    worldUp,
		lut:        lut,
		transport:  transport,
		timingSegs: timingSegs,
		timingInit: timingInit,
		lookSegs:   lookSegs,
		rollKeys:   rollKeys,
		fovKeys:    fovKeys,
		focusKeys:  focusKeys,
		apertKeys:  apertKeys,
		resolver:   bo.resolver,
	}, nil
}

// Config returns a deep copy of the path's configuration.
func (p *Path) Config() PathConfig {
	return clonePathConfig(p.cfg)
}

// Duration returns the path's nominal duration.
func (p *Path) Duration() time.Duration {
	return p.cfg.Duration
}

// TotalLength returns the precomputed arc length of the position curve in meters.
func (p *Path) TotalLength() float64 {
	return p.lut.TotalLength()
}

// Markers returns a copy of the path's named timeline markers.
func (p *Path) Markers() []Marker {
	return slices.Clone(p.cfg.Markers)
}

// ControlPoints returns the spatial anchor points of the path's position track.
func (p *Path) ControlPoints() []Vec3d {
	if p.cfg.Position.Interp == InterpBezier {
		out := make([]Vec3d, len(p.cfg.Position.Knots))
		for i, k := range p.cfg.Position.Knots {
			out[i] = k.P
		}
		return out
	}
	return slices.Clone(p.cfg.Position.Points)
}

// SampleCurve returns count evenly spaced arc-length samples along s in [0, 1]
// (useful for rendering path polylines in debug overlays and viewports).
func (p *Path) SampleCurve(count int) []Vec3d {
	if count <= 0 {
		return nil
	}
	if count == 1 {
		pos, _ := p.lut.Eval(0)
		return []Vec3d{pos}
	}
	out := make([]Vec3d, count)
	inv := 1.0 / float64(count-1)
	for i := range count {
		pos, _ := p.lut.Eval(float64(i) * inv)
		out[i] = pos
	}
	return out
}

func compilePosition(pos PositionTrack) ([]nt3dcurve.CubicSpan, error) {
	if len(pos.Points) > 0 && len(pos.Knots) > 0 {
		return nil, fmt.Errorf("nt3dcam: position track cannot set both points and knots")
	}
	if pos.Interp == InterpBezier {
		if len(pos.Knots) == 0 {
			return nil, fmt.Errorf("nt3dcam: bezier position track requires knots")
		}
		if pos.Closed && len(pos.Knots) < 2 {
			return nil, fmt.Errorf("nt3dcam: closed bezier curve requires at least 2 knots")
		}
		knots := make([]nt3dcurve.Knot, len(pos.Knots))
		for i, k := range pos.Knots {
			knots[i] = nt3dcurve.Knot{P: k.P, In: k.In, Out: k.Out}
		}
		return nt3dcurve.BuildBezier(knots, pos.Closed)
	}

	if len(pos.Knots) > 0 {
		return nil, fmt.Errorf("nt3dcam: knots are only valid with interp=bezier")
	}
	if pos.Closed && len(pos.Points) < 3 && pos.Interp != InterpLinear {
		return nil, fmt.Errorf("nt3dcam: closed spline requires at least 3 points")
	}
	pts := make([]nt3dcurve.Vec3, len(pos.Points))
	for i, pt := range pos.Points {
		pts[i] = pt
	}
	switch pos.Interp {
	case InterpCatmullRomCentripetal:
		return nt3dcurve.BuildCatmullRomCentripetal(pts, pos.Closed)
	case InterpBSpline:
		return nt3dcurve.BuildBSpline(pts, pos.Closed)
	case InterpLinear:
		return nt3dcurve.BuildLinear(pts, pos.Closed)
	default:
		return nil, fmt.Errorf("nt3dcam: unknown position interp %d", pos.Interp)
	}
}

func compileTiming(tt TimingTrack, duration time.Duration) (float64, []compiledTimingSeg, error) {
	if math.IsNaN(tt.Start) || math.IsInf(tt.Start, 0) || tt.Start < 0 || tt.Start > 1 {
		return 0, nil, fmt.Errorf("nt3dcam: timing start %g must be in [0, 1]", tt.Start)
	}
	if len(tt.Segments) == 0 {
		if duration <= 0 {
			return tt.Start, nil, nil
		}
		return tt.Start, []compiledTimingSeg{{
			t0:   0,
			t1:   duration,
			s0:   tt.Start,
			s1:   1.0,
			ease: nt3dease.Spec{Kind: nt3dease.Linear},
		}}, nil
	}
	segs := make([]compiledTimingSeg, len(tt.Segments))
	var curT time.Duration
	curS := tt.Start
	for i, seg := range tt.Segments {
		if math.IsNaN(seg.To) || math.IsInf(seg.To, 0) || seg.To < 0 || seg.To > 1 {
			return 0, nil, fmt.Errorf("nt3dcam: timing segment %d target %g must be in [0, 1]", i, seg.To)
		}
		if seg.Over <= 0 {
			return 0, nil, fmt.Errorf("nt3dcam: timing segment %d duration %v must be > 0", i, seg.Over)
		}
		es := seg.Ease.toEase(nt3dease.Linear)
		if err := es.Validate(); err != nil {
			return 0, nil, fmt.Errorf("nt3dcam: timing segment %d: %w", i, err)
		}
		segs[i] = compiledTimingSeg{
			t0:   curT,
			t1:   curT + seg.Over,
			s0:   curS,
			s1:   seg.To,
			ease: es,
		}
		curT += seg.Over
		curS = seg.To
	}
	return tt.Start, segs, nil
}

func compileLook(segments []LookSegment) ([]compiledLookSeg, error) {
	if len(segments) == 0 {
		return []compiledLookSeg{{mode: LookTangent}}, nil
	}
	out := make([]compiledLookSeg, len(segments))
	for i, s := range segments {
		if s.From < 0 {
			return nil, fmt.Errorf("nt3dcam: look segment %d has negative from %v", i, s.From)
		}
		if i > 0 && s.From < segments[i-1].From {
			return nil, fmt.Errorf("nt3dcam: look segment %d from %v precedes previous %v", i, s.From, segments[i-1].From)
		}
		if s.Blend < 0 {
			return nil, fmt.Errorf("nt3dcam: look segment %d has negative blend %v", i, s.Blend)
		}
		be := s.BlendEase.toEase(nt3dease.EaseInOut)
		if err := be.Validate(); err != nil {
			return nil, fmt.Errorf("nt3dcam: look segment %d blend_ease: %w", i, err)
		}

		cs := compiledLookSeg{
			mode:              s.Mode,
			from:              s.From,
			blend:             s.Blend,
			blendEase:         be,
			targetRef:         s.TargetRef,
			point:             s.Point,
			fallback:          s.Fallback,
			reverseWithMotion: s.ReverseWithMotion,
		}
		switch s.Mode {
		case LookTarget:
			if !s.Point.IsFinite() || (s.HasFallback && !s.Fallback.IsFinite()) {
				return nil, fmt.Errorf("nt3dcam: look segment %d has non-finite target point", i)
			}
		case LookTangent:
		case LookKeyframes:
			fkeys := make([]nt3dframes.Key, len(s.Keys))
			for k, qk := range s.Keys {
				fkeys[k] = nt3dframes.Key{
					T:    qk.T,
					Q:    qk.Q,
					Ease: qk.Ease.toEase(nt3dease.Linear),
				}
			}
			tr, err := nt3dframes.NewTrack(fkeys, s.Interp == QuatSquad)
			if err != nil {
				return nil, fmt.Errorf("nt3dcam: look segment %d: %w", i, err)
			}
			cs.quatTrack = tr
		default:
			return nil, fmt.Errorf("nt3dcam: look segment %d unknown mode %d", i, s.Mode)
		}
		out[i] = cs
	}
	return out, nil
}

func compileScalarKeys(name string, keys []Keyframe, checkVal func(float64) error) ([]nt3dease.Key, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	out := make([]nt3dease.Key, len(keys))
	for i, k := range keys {
		out[i] = nt3dease.Key{T: k.T, V: k.V, Ease: k.Ease.toEase(nt3dease.Linear)}
	}
	if err := nt3dease.ValidateKeys(out); err != nil {
		return nil, fmt.Errorf("nt3dcam: %s: %w", name, err)
	}
	for i, k := range out {
		if err := checkVal(k.V); err != nil {
			return nil, fmt.Errorf("nt3dcam: %s keyframe %d: %w", name, i, err)
		}
	}
	return out, nil
}

func clonePathConfig(c PathConfig) PathConfig {
	out := c
	if c.Origin != nil {
		orig := *c.Origin
		out.Origin = &orig
	}
	out.Position.Points = slices.Clone(c.Position.Points)
	out.Position.Knots = slices.Clone(c.Position.Knots)
	out.Timing.Segments = slices.Clone(c.Timing.Segments)
	if len(c.Look) > 0 {
		out.Look = make([]LookSegment, len(c.Look))
		for i, ls := range c.Look {
			out.Look[i] = ls
			out.Look[i].Keys = slices.Clone(ls.Keys)
		}
	}
	out.Roll = slices.Clone(c.Roll)
	out.Lens.FovY = slices.Clone(c.Lens.FovY)
	out.Lens.FocusDist = slices.Clone(c.Lens.FocusDist)
	out.Lens.Aperture = slices.Clone(c.Lens.Aperture)
	out.Markers = slices.Clone(c.Markers)
	return out
}
