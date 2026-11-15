// Copyright (c) 2026 Neomantra Corp.

package nt3dcamspec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	nt3dcam "nimbleterminal.dev/nt3d/cam"
)

const degToRad = math.Pi / 180.0
const radToDeg = 180.0 / math.Pi

// Time returns d as a standard time.Duration.
func (d Duration) Time() time.Duration { return time.Duration(d) }

// String formats d compactly ("0s", "500ms", "12s").
func (d Duration) String() string {
	td := time.Duration(d)
	if td == 0 {
		return "0s"
	}
	if td%time.Second == 0 {
		return strconv.FormatInt(int64(td/time.Second), 10) + "s"
	}
	if td%time.Millisecond == 0 && td < time.Minute {
		// Prefer fractional seconds if clean (e.g. 1500ms -> "1.5s"), else ms.
		if td%(100*time.Millisecond) == 0 {
			sec := float64(td) / float64(time.Second)
			return strconv.FormatFloat(sec, 'f', -1, 64) + "s"
		}
		return strconv.FormatInt(int64(td/time.Millisecond), 10) + "ms"
	}
	return td.String()
}

// MarshalJSON encodes d as a compact duration string (e.g. "12s").
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

// UnmarshalJSON decodes a duration from either a string ("12s", "500ms") or a
// non-negative numeric value in seconds.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") {
			return fmt.Errorf("nt3dcamspec: duration %q must be an unsigned non-negative duration", s)
		}
		td, err := time.ParseDuration(s)
		if err != nil || td < 0 {
			return fmt.Errorf("nt3dcamspec: invalid duration %q", s)
		}
		*d = Duration(td)
		return nil
	}
	var sec float64
	if err := json.Unmarshal(b, &sec); err == nil {
		if math.IsNaN(sec) || math.IsInf(sec, 0) || sec < 0 || sec > 1e9 {
			return fmt.Errorf("nt3dcamspec: invalid numeric duration %v", sec)
		}
		*d = Duration(time.Duration(math.Round(sec * float64(time.Second))))
		return nil
	}
	return fmt.Errorf("nt3dcamspec: duration must be string or non-negative number, got %s", string(b))
}

// MarshalJSON encodes EaseSpec as a preset string or a 4-number array.
func (e EaseSpec) MarshalJSON() ([]byte, error) {
	if e.Bezier != nil {
		return json.Marshal(*e.Bezier)
	}
	name := e.Name
	if name == "" {
		name = "linear"
	}
	return json.Marshal(name)
}

// UnmarshalJSON decodes EaseSpec from a preset string or a 4-number array.
func (e *EaseSpec) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*e = EaseSpec{Name: s}
		return nil
	}
	var slice []float64
	if err := json.Unmarshal(b, &slice); err == nil {
		if len(slice) != 4 {
			return fmt.Errorf("nt3dcamspec: cubic-bezier array must have 4 elements, got %d", len(slice))
		}
		arr := [4]float64{slice[0], slice[1], slice[2], slice[3]}
		*e = EaseSpec{Bezier: &arr}
		return nil
	}
	return fmt.Errorf("nt3dcamspec: invalid ease spec %s", string(b))
}

// MarshalJSON encodes TimingSpec as a bare segment list when Start == 0, or as
// `{start, segments}` when Start > 0.
func (t TimingSpec) MarshalJSON() ([]byte, error) {
	if t.Start == 0 {
		return json.Marshal(t.Segments)
	}
	type alias struct {
		Start    float64             `json:"start"`
		Segments []TimingSegmentSpec `json:"segments"`
	}
	return json.Marshal(alias{Start: t.Start, Segments: t.Segments})
}

// UnmarshalJSON decodes TimingSpec from either a segment array or `{start, segments}`.
func (t *TimingSpec) UnmarshalJSON(b []byte) error {
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var segs []TimingSegmentSpec
		if err := strictJSONUnmarshal(trimmed, &segs); err != nil {
			return err
		}
		if len(segs) == 0 {
			return fmt.Errorf("nt3dcamspec: timing segment list must not be empty")
		}
		*t = TimingSpec{Segments: segs}
		return nil
	}
	type rawTiming struct {
		Start    float64             `json:"start"`
		Segments []TimingSegmentSpec `json:"segments"`
	}
	var rt rawTiming
	if err := strictJSONUnmarshal(trimmed, &rt); err != nil {
		return err
	}
	if len(rt.Segments) == 0 {
		return fmt.Errorf("nt3dcamspec: timing.segments must not be empty")
	}
	*t = TimingSpec{Start: rt.Start, Segments: rt.Segments}
	return nil
}

// Parse decodes and validates a YAML or JSON camera_path v1 document.
func Parse(data []byte) (Document, error) {
	if len(data) > MaxSpecBytes {
		return Document{}, fmt.Errorf("nt3dcamspec: spec exceeds %d byte limit", MaxSpecBytes)
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return Document{}, fmt.Errorf("nt3dcamspec: empty spec")
	}
	if trimmed[0] == '{' {
		if doc, err := ParseJSON(trimmed); err == nil {
			return doc, nil
		}
	}
	return ParseYAML(data)
}

// ParseJSON decodes and validates a strict JSON camera_path v1 document.
func ParseJSON(data []byte) (Document, error) {
	if len(data) > MaxSpecBytes {
		return Document{}, fmt.Errorf("nt3dcamspec: spec exceeds %d byte limit", MaxSpecBytes)
	}
	var rawMap map[string]json.RawMessage
	if err := strictJSONUnmarshal(data, &rawMap); err != nil {
		return Document{}, fmt.Errorf("nt3dcamspec: invalid JSON: %w", err)
	}
	var bodyBytes []byte
	if cpBytes, hasWrapper := rawMap["camera_path"]; hasWrapper {
		if len(rawMap) != 1 {
			return Document{}, fmt.Errorf("nt3dcamspec: unexpected sibling keys alongside camera_path")
		}
		bodyBytes = cpBytes
	} else {
		bodyBytes = data
	}

	var rawBody map[string]json.RawMessage
	if err := strictJSONUnmarshal(bodyBytes, &rawBody); err != nil {
		return Document{}, fmt.Errorf("nt3dcamspec: invalid camera_path object: %w", err)
	}
	for _, req := range []string{"version", "duration", "position"} {
		if _, ok := rawBody[req]; !ok {
			return Document{}, fmt.Errorf("nt3dcamspec: missing required field %q", req)
		}
	}

	var cp CameraPath
	if err := strictJSONUnmarshal(bodyBytes, &cp); err != nil {
		return Document{}, err
	}
	doc := Document{CameraPath: cp}
	if _, err := ToConfig(doc); err != nil {
		return Document{}, err
	}
	return doc, nil
}

// ParseYAML decodes and validates a YAML (or JSON) camera_path v1 document.
func ParseYAML(data []byte) (Document, error) {
	val, err := parseYAMLValue(data)
	if err != nil {
		return Document{}, err
	}
	m, ok := val.(map[string]any)
	if !ok {
		return Document{}, fmt.Errorf("nt3dcamspec: top-level YAML value must be a mapping")
	}
	jsonBytes, err := json.Marshal(m)
	if err != nil {
		return Document{}, fmt.Errorf("nt3dcamspec: YAML normalization failed: %w", err)
	}
	return ParseJSON(jsonBytes)
}

// Compile parses data (YAML or JSON) and compiles it into an immutable *nt3dcam.Path.
func Compile(data []byte, opts ...nt3dcam.Option) (*nt3dcam.Path, error) {
	doc, err := Parse(data)
	if err != nil {
		return nil, err
	}
	cfg, err := ToConfig(doc)
	if err != nil {
		return nil, err
	}
	return nt3dcam.NewPath(cfg, opts...)
}

// ToConfig validates doc and converts its degree/millimeter spec fields into a
// radian/meter nt3dcam.PathConfig.
func ToConfig(doc Document) (nt3dcam.PathConfig, error) {
	cp := doc.CameraPath
	if cp.Version != SchemaVersion {
		return nt3dcam.PathConfig{}, fmt.Errorf("nt3dcamspec: unsupported version %d (want %d)", cp.Version, SchemaVersion)
	}
	if cp.Duration < 0 {
		return nt3dcam.PathConfig{}, fmt.Errorf("nt3dcamspec: negative duration")
	}

	var cfg nt3dcam.PathConfig
	cfg.Name = cp.Name
	cfg.Duration = cp.Duration.Time()

	switch cp.Frame {
	case "", "scene":
		cfg.Frame = nt3dcam.FrameScene
	case "enu":
		cfg.Frame = nt3dcam.FrameENU
	default:
		return nt3dcam.PathConfig{}, fmt.Errorf("nt3dcamspec: unknown frame %q", cp.Frame)
	}

	if cp.Origin != nil {
		cfg.Origin = &nt3dcam.GeoOrigin{
			LatDeg: cp.Origin.Lat,
			LonDeg: cp.Origin.Lon,
			AltM:   cp.Origin.Alt,
		}
	}
	if cp.Up != nil {
		cfg.Up = nt3dcam.Vec3d{X: cp.Up[0], Y: cp.Up[1], Z: cp.Up[2]}
	}

	switch cp.Loop {
	case "", "none":
		cfg.Loop = nt3dcam.LoopNone
	case "loop":
		cfg.Loop = nt3dcam.LoopRepeat
	case "pingpong":
		cfg.Loop = nt3dcam.LoopPingPong
	default:
		return nt3dcam.PathConfig{}, fmt.Errorf("nt3dcamspec: unknown loop mode %q", cp.Loop)
	}

	posTrack, err := convertPositionSpec(cp.Position)
	if err != nil {
		return nt3dcam.PathConfig{}, err
	}
	cfg.Position = posTrack

	if cp.Timing != nil {
		cfg.Timing.Start = cp.Timing.Start
		cfg.Timing.Segments = make([]nt3dcam.TimingSegment, len(cp.Timing.Segments))
		for i, seg := range cp.Timing.Segments {
			es, err := convertEaseSpec(seg.Ease, nt3dcam.EaseLinear)
			if err != nil {
				return nt3dcam.PathConfig{}, fmt.Errorf("nt3dcamspec: timing[%d]: %w", i, err)
			}
			cfg.Timing.Segments[i] = nt3dcam.TimingSegment{
				To:   seg.To,
				Over: seg.Over.Time(),
				Ease: es,
			}
		}
	}

	if len(cp.Look) > 0 {
		cfg.Look = make([]nt3dcam.LookSegment, len(cp.Look))
		for i, ls := range cp.Look {
			conv, err := convertLookSpec(i, ls)
			if err != nil {
				return nt3dcam.PathConfig{}, err
			}
			cfg.Look[i] = conv
		}
	}

	if len(cp.Roll) > 0 {
		cfg.Roll, err = convertScalarKeys(cp.Roll, degToRad)
		if err != nil {
			return nt3dcam.PathConfig{}, fmt.Errorf("nt3dcamspec: roll: %w", err)
		}
	}

	if cp.Lens != nil {
		lens, err := convertLensSpec(*cp.Lens)
		if err != nil {
			return nt3dcam.PathConfig{}, err
		}
		cfg.Lens = lens
	}

	if len(cp.Markers) > 0 {
		cfg.Markers = make([]nt3dcam.Marker, len(cp.Markers))
		for i, m := range cp.Markers {
			cfg.Markers[i] = nt3dcam.Marker{Name: m.Name, T: m.T.Time()}
		}
	}

	// Run full Path compilation invariants so Parse catches every semantic error.
	if _, err := nt3dcam.NewPath(cfg); err != nil {
		return nt3dcam.PathConfig{}, err
	}
	return cfg, nil
}

// FromPath converts a compiled *nt3dcam.Path back into a serializable Document.
func FromPath(p *nt3dcam.Path) Document {
	if p == nil {
		return Document{CameraPath: CameraPath{Version: SchemaVersion}}
	}
	return FromConfig(p.Config())
}

// FromConfig converts an nt3dcam.PathConfig into a canonical serializable Document.
func FromConfig(cfg nt3dcam.PathConfig) Document {
	cp := CameraPath{
		Version:  SchemaVersion,
		Name:     cfg.Name,
		Frame:    cfg.Frame.String(),
		Duration: Duration(cfg.Duration),
		Loop:     cfg.Loop.String(),
	}
	if cfg.Origin != nil {
		cp.Origin = &GeoOriginSpec{
			Lat: cfg.Origin.LatDeg,
			Lon: cfg.Origin.LonDeg,
			Alt: cfg.Origin.AltM,
		}
	}
	if cfg.Up != (nt3dcam.Vec3d{}) && cfg.Up != nt3dcam.DefaultUp {
		up := [3]float64{cfg.Up.X, cfg.Up.Y, cfg.Up.Z}
		cp.Up = &up
	}

	cp.Position = PositionSpec{
		Interp: cfg.Position.Interp.String(),
		Closed: cfg.Position.Closed,
	}
	if cfg.Position.Interp == nt3dcam.InterpBezier {
		cp.Position.Knots = make([]BezierKnotSpec, len(cfg.Position.Knots))
		for i, k := range cfg.Position.Knots {
			in := [3]float64{k.In.X, k.In.Y, k.In.Z}
			out := [3]float64{k.Out.X, k.Out.Y, k.Out.Z}
			cp.Position.Knots[i] = BezierKnotSpec{
				P:   [3]float64{k.P.X, k.P.Y, k.P.Z},
				In:  &in,
				Out: &out,
			}
		}
	} else {
		cp.Position.Points = make([][3]float64, len(cfg.Position.Points))
		for i, pt := range cfg.Position.Points {
			cp.Position.Points[i] = [3]float64{pt.X, pt.Y, pt.Z}
		}
	}

	if len(cfg.Timing.Segments) > 0 || cfg.Timing.Start > 0 {
		ts := &TimingSpec{
			Start:    cfg.Timing.Start,
			Segments: make([]TimingSegmentSpec, len(cfg.Timing.Segments)),
		}
		for i, seg := range cfg.Timing.Segments {
			ts.Segments[i] = TimingSegmentSpec{
				To:   seg.To,
				Over: Duration(seg.Over),
				Ease: easeSpecFromCam(seg.Ease, nt3dcam.EaseLinear),
			}
		}
		cp.Timing = ts
	}

	if len(cfg.Look) > 0 {
		cp.Look = make([]LookSpec, len(cfg.Look))
		for i, ls := range cfg.Look {
			spec := LookSpec{
				Mode:              ls.Mode.String(),
				From:              Duration(ls.From),
				Blend:             Duration(ls.Blend),
				BlendEase:         easeSpecFromCam(ls.BlendEase, nt3dcam.EaseInOut),
				ReverseWithMotion: ls.ReverseWithMotion,
			}
			switch ls.Mode {
			case nt3dcam.LookTarget:
				if ls.TargetRef != "" {
					spec.Target = ls.TargetRef
					if ls.HasFallback {
						fb := [3]float64{ls.Fallback.X, ls.Fallback.Y, ls.Fallback.Z}
						spec.Fallback = &fb
					}
				} else {
					pt := [3]float64{ls.Point.X, ls.Point.Y, ls.Point.Z}
					spec.Point = &pt
				}
			case nt3dcam.LookKeyframes:
				spec.Interp = ls.Interp.String()
				spec.Keys = make([]QuatKey, len(ls.Keys))
				for k, qk := range ls.Keys {
					spec.Keys[k] = QuatKey{
						T:    Duration(qk.T),
						Q:    [4]float64{qk.Q.X, qk.Q.Y, qk.Q.Z, qk.Q.W},
						Ease: easeSpecFromCam(qk.Ease, nt3dcam.EaseLinear),
					}
				}
			}
			cp.Look[i] = spec
		}
	}

	if len(cfg.Roll) > 0 {
		cp.Roll = scalarKeysFromCam(cfg.Roll, radToDeg)
	}

	if len(cfg.Lens.FovY) > 0 || len(cfg.Lens.FocusDist) > 0 || len(cfg.Lens.Aperture) > 0 || cfg.Lens.Near > 0 || cfg.Lens.Far > 0 {
		cp.Lens = &LensSpec{
			Fov:       scalarKeysFromCam(cfg.Lens.FovY, radToDeg),
			FocusDist: scalarKeysFromCam(cfg.Lens.FocusDist, 1.0),
			Aperture:  scalarKeysFromCam(cfg.Lens.Aperture, 1.0),
			Near:      cfg.Lens.Near,
			Far:       cfg.Lens.Far,
		}
	}

	if len(cfg.Markers) > 0 {
		cp.Markers = make([]MarkerSpec, len(cfg.Markers))
		for i, m := range cfg.Markers {
			cp.Markers[i] = MarkerSpec{Name: m.Name, T: Duration(m.T)}
		}
	}

	return Document{CameraPath: cp}
}

// MarshalJSON encodes doc as formatted JSON.
func MarshalJSON(doc Document) ([]byte, error) {
	if _, err := ToConfig(doc); err != nil {
		return nil, err
	}
	return json.MarshalIndent(doc, "", "  ")
}

// MarshalYAML encodes doc as clean, canonical camera_path YAML.
func MarshalYAML(doc Document) ([]byte, error) {
	if _, err := ToConfig(doc); err != nil {
		return nil, err
	}
	cp := doc.CameraPath
	var b strings.Builder
	b.WriteString("camera_path:\n")
	fmt.Fprintf(&b, "  version: %d\n", cp.Version)
	if cp.Name != "" {
		fmt.Fprintf(&b, "  name: %s\n", yamlQuoteIfNeeded(cp.Name))
	}
	frame := cp.Frame
	if frame == "" {
		frame = "scene"
	}
	fmt.Fprintf(&b, "  frame: %s\n", frame)
	if cp.Origin != nil {
		fmt.Fprintf(&b, "  origin: {lat: %s, lon: %s, alt: %s}\n",
			fmtFloat(cp.Origin.Lat), fmtFloat(cp.Origin.Lon), fmtFloat(cp.Origin.Alt))
	}
	if cp.Up != nil {
		fmt.Fprintf(&b, "  up: %s\n", fmtVec3(*cp.Up))
	}
	fmt.Fprintf(&b, "  duration: %s\n", cp.Duration.String())
	loop := cp.Loop
	if loop == "" {
		loop = "none"
	}
	fmt.Fprintf(&b, "  loop: %s\n", loop)

	b.WriteString("  position:\n")
	interp := cp.Position.Interp
	if interp == "" {
		interp = "catmull-rom-centripetal"
	}
	fmt.Fprintf(&b, "    interp: %s\n", interp)
	if cp.Position.Closed {
		b.WriteString("    closed: true\n")
	}
	if len(cp.Position.Knots) > 0 {
		b.WriteString("    knots:\n")
		for _, k := range cp.Position.Knots {
			var parts []string
			parts = append(parts, "p: "+fmtVec3(k.P))
			if k.In != nil {
				parts = append(parts, "in: "+fmtVec3(*k.In))
			}
			if k.Out != nil {
				parts = append(parts, "out: "+fmtVec3(*k.Out))
			}
			if k.TangentIn != nil {
				parts = append(parts, "tangent_in: "+fmtVec3(*k.TangentIn))
			}
			if k.TangentOut != nil {
				parts = append(parts, "tangent_out: "+fmtVec3(*k.TangentOut))
			}
			fmt.Fprintf(&b, "      - {%s}\n", strings.Join(parts, ", "))
		}
	} else {
		b.WriteString("    points:\n")
		for _, pt := range cp.Position.Points {
			fmt.Fprintf(&b, "      - %s\n", fmtVec3(pt))
		}
	}

	if cp.Timing != nil && len(cp.Timing.Segments) > 0 {
		if cp.Timing.Start > 0 {
			b.WriteString("  timing:\n")
			fmt.Fprintf(&b, "    start: %s\n", fmtFloat(cp.Timing.Start))
			b.WriteString("    segments:\n")
			for _, seg := range cp.Timing.Segments {
				fmt.Fprintf(&b, "      - %s\n", fmtTimingSeg(seg))
			}
		} else {
			b.WriteString("  timing:\n")
			for _, seg := range cp.Timing.Segments {
				fmt.Fprintf(&b, "    - %s\n", fmtTimingSeg(seg))
			}
		}
	}

	if len(cp.Look) > 0 {
		b.WriteString("  look:\n")
		for _, ls := range cp.Look {
			fmt.Fprintf(&b, "    - %s\n", fmtLookSeg(ls))
		}
	}

	if len(cp.Roll) > 0 {
		b.WriteString("  roll:\n")
		for _, rk := range cp.Roll {
			fmt.Fprintf(&b, "    - %s\n", fmtScalarKey(rk))
		}
	}

	if cp.Lens != nil {
		b.WriteString("  lens:\n")
		if len(cp.Lens.Fov) > 0 {
			b.WriteString("    fov:\n")
			for _, fk := range cp.Lens.Fov {
				fmt.Fprintf(&b, "      - %s\n", fmtScalarKey(fk))
			}
		}
		if len(cp.Lens.FocalLength) > 0 {
			b.WriteString("    focal_length:\n")
			for _, fk := range cp.Lens.FocalLength {
				fmt.Fprintf(&b, "      - %s\n", fmtScalarKey(fk))
			}
			if cp.Lens.SensorHeight > 0 {
				fmt.Fprintf(&b, "    sensor_height: %s\n", fmtFloat(cp.Lens.SensorHeight))
			}
		}
		if len(cp.Lens.FocusDist) > 0 {
			b.WriteString("    focus_dist:\n")
			for _, fk := range cp.Lens.FocusDist {
				fmt.Fprintf(&b, "      - %s\n", fmtScalarKey(fk))
			}
		}
		if len(cp.Lens.Aperture) > 0 {
			b.WriteString("    aperture:\n")
			for _, fk := range cp.Lens.Aperture {
				fmt.Fprintf(&b, "      - %s\n", fmtScalarKey(fk))
			}
		}
		if cp.Lens.Near > 0 {
			fmt.Fprintf(&b, "    near: %s\n", fmtFloat(cp.Lens.Near))
		}
		if cp.Lens.Far > 0 {
			fmt.Fprintf(&b, "    far: %s\n", fmtFloat(cp.Lens.Far))
		}
	}

	if len(cp.Markers) > 0 {
		b.WriteString("  markers:\n")
		for _, m := range cp.Markers {
			fmt.Fprintf(&b, "    - {name: %s, t: %s}\n", yamlQuoteIfNeeded(m.Name), m.T.String())
		}
	}

	return []byte(b.String()), nil
}

func convertPositionSpec(ps PositionSpec) (nt3dcam.PositionTrack, error) {
	if len(ps.Samples) > 0 {
		return nt3dcam.PositionTrack{}, fmt.Errorf("nt3dcamspec: position.samples is reserved for v2 and unsupported in v1")
	}
	var out nt3dcam.PositionTrack
	out.Closed = ps.Closed
	switch ps.Interp {
	case "", "catmull-rom-centripetal":
		out.Interp = nt3dcam.InterpCatmullRomCentripetal
	case "bezier":
		out.Interp = nt3dcam.InterpBezier
	case "bspline":
		out.Interp = nt3dcam.InterpBSpline
	case "linear":
		out.Interp = nt3dcam.InterpLinear
	default:
		return nt3dcam.PositionTrack{}, fmt.Errorf("nt3dcamspec: unknown position.interp %q", ps.Interp)
	}

	if len(ps.Points) > 0 && len(ps.Knots) > 0 {
		return nt3dcam.PositionTrack{}, fmt.Errorf("nt3dcamspec: position cannot specify both points and knots")
	}
	if out.Interp == nt3dcam.InterpBezier {
		if len(ps.Knots) == 0 {
			return nt3dcam.PositionTrack{}, fmt.Errorf("nt3dcamspec: bezier position requires knots")
		}
		out.Knots = make([]nt3dcam.BezierKnot, len(ps.Knots))
		for i, k := range ps.Knots {
			if k.In != nil && k.TangentIn != nil {
				return nt3dcam.PositionTrack{}, fmt.Errorf("nt3dcamspec: knot %d sets both in and tangent_in", i)
			}
			if k.Out != nil && k.TangentOut != nil {
				return nt3dcam.PositionTrack{}, fmt.Errorf("nt3dcamspec: knot %d sets both out and tangent_out", i)
			}
			p := nt3dcam.Vec3d{X: k.P[0], Y: k.P[1], Z: k.P[2]}
			in := p
			if k.In != nil {
				in = nt3dcam.Vec3d{X: k.In[0], Y: k.In[1], Z: k.In[2]}
			} else if k.TangentIn != nil {
				tin := nt3dcam.Vec3d{X: k.TangentIn[0], Y: k.TangentIn[1], Z: k.TangentIn[2]}
				in = p.Sub(tin.Scale(1.0 / 3.0))
			}
			outH := p
			if k.Out != nil {
				outH = nt3dcam.Vec3d{X: k.Out[0], Y: k.Out[1], Z: k.Out[2]}
			} else if k.TangentOut != nil {
				tout := nt3dcam.Vec3d{X: k.TangentOut[0], Y: k.TangentOut[1], Z: k.TangentOut[2]}
				outH = p.Add(tout.Scale(1.0 / 3.0))
			}
			out.Knots[i] = nt3dcam.BezierKnot{P: p, In: in, Out: outH}
		}
		return out, nil
	}

	if len(ps.Knots) > 0 {
		return nt3dcam.PositionTrack{}, fmt.Errorf("nt3dcamspec: position.knots requires interp=bezier")
	}
	if len(ps.Points) == 0 {
		return nt3dcam.PositionTrack{}, fmt.Errorf("nt3dcamspec: position.points is required")
	}
	out.Points = make([]nt3dcam.Vec3d, len(ps.Points))
	for i, pt := range ps.Points {
		out.Points[i] = nt3dcam.Vec3d{X: pt[0], Y: pt[1], Z: pt[2]}
	}
	return out, nil
}

func convertLookSpec(idx int, ls LookSpec) (nt3dcam.LookSegment, error) {
	var out nt3dcam.LookSegment
	out.From = ls.From.Time()
	out.Blend = ls.Blend.Time()
	if ls.BlendEase != nil {
		be, err := convertEaseSpec(ls.BlendEase, nt3dcam.EaseInOut)
		if err != nil {
			return nt3dcam.LookSegment{}, fmt.Errorf("nt3dcamspec: look[%d].blend_ease: %w", idx, err)
		}
		out.BlendEase = be
	}
	switch ls.Mode {
	case "target":
		out.Mode = nt3dcam.LookTarget
		if (ls.Target == "") == (ls.Point == nil) {
			return nt3dcam.LookSegment{}, fmt.Errorf("nt3dcamspec: look[%d] mode=target requires exactly one of target or point", idx)
		}
		if len(ls.Keys) > 0 || ls.Interp != "" {
			return nt3dcam.LookSegment{}, fmt.Errorf("nt3dcamspec: look[%d] mode=target cannot set keys or interp", idx)
		}
		if ls.Target != "" {
			if !validTargetRef(ls.Target) {
				return nt3dcam.LookSegment{}, fmt.Errorf("nt3dcamspec: look[%d] invalid target reference %q (expected node:..., panel:..., or path:...)", idx, ls.Target)
			}
			out.TargetRef = ls.Target
			if ls.Fallback != nil {
				out.HasFallback = true
				out.Fallback = nt3dcam.Vec3d{X: ls.Fallback[0], Y: ls.Fallback[1], Z: ls.Fallback[2]}
			}
		} else {
			out.Point = nt3dcam.Vec3d{X: ls.Point[0], Y: ls.Point[1], Z: ls.Point[2]}
		}
	case "tangent":
		out.Mode = nt3dcam.LookTangent
		out.ReverseWithMotion = ls.ReverseWithMotion
		if ls.Target != "" || ls.Point != nil || len(ls.Keys) > 0 {
			return nt3dcam.LookSegment{}, fmt.Errorf("nt3dcamspec: look[%d] mode=tangent cannot set target, point, or keys", idx)
		}
	case "keyframes":
		out.Mode = nt3dcam.LookKeyframes
		if ls.Target != "" || ls.Point != nil {
			return nt3dcam.LookSegment{}, fmt.Errorf("nt3dcamspec: look[%d] mode=keyframes cannot set target or point", idx)
		}
		switch ls.Interp {
		case "", "squad":
			out.Interp = nt3dcam.QuatSquad
		case "slerp":
			out.Interp = nt3dcam.QuatSlerp
		default:
			return nt3dcam.LookSegment{}, fmt.Errorf("nt3dcamspec: look[%d] unknown quaternion interp %q", idx, ls.Interp)
		}
		if len(ls.Keys) == 0 {
			return nt3dcam.LookSegment{}, fmt.Errorf("nt3dcamspec: look[%d] mode=keyframes requires keys", idx)
		}
		out.Keys = make([]nt3dcam.QuatKeyframe, len(ls.Keys))
		for k, qk := range ls.Keys {
			es, err := convertEaseSpec(qk.Ease, nt3dcam.EaseLinear)
			if err != nil {
				return nt3dcam.LookSegment{}, fmt.Errorf("nt3dcamspec: look[%d].keys[%d]: %w", idx, k, err)
			}
			out.Keys[k] = nt3dcam.QuatKeyframe{
				T:    qk.T.Time(),
				Q:    nt3dcam.Quatd{X: qk.Q[0], Y: qk.Q[1], Z: qk.Q[2], W: qk.Q[3]},
				Ease: es,
			}
		}
	default:
		return nt3dcam.LookSegment{}, fmt.Errorf("nt3dcamspec: look[%d] unknown mode %q", idx, ls.Mode)
	}
	return out, nil
}

func validTargetRef(ref string) bool {
	for _, prefix := range []string{"node:", "panel:", "path:"} {
		if strings.HasPrefix(ref, prefix) && len(ref) > len(prefix) {
			return true
		}
	}
	return false
}

func convertLensSpec(ls LensSpec) (nt3dcam.LensTrack, error) {
	if len(ls.Fov) > 0 && len(ls.FocalLength) > 0 {
		return nt3dcam.LensTrack{}, fmt.Errorf("nt3dcamspec: lens cannot set both fov and focal_length")
	}
	var out nt3dcam.LensTrack
	out.Near = ls.Near
	out.Far = ls.Far
	var err error
	if len(ls.Fov) > 0 {
		out.FovY, err = convertScalarKeys(ls.Fov, degToRad)
		if err != nil {
			return nt3dcam.LensTrack{}, fmt.Errorf("nt3dcamspec: lens.fov: %w", err)
		}
	} else if len(ls.FocalLength) > 0 {
		sh := ls.SensorHeight
		if sh == 0 {
			sh = nt3dcam.DefaultSensorHeightMM
		}
		if math.IsNaN(sh) || math.IsInf(sh, 0) || sh <= 0 {
			return nt3dcam.LensTrack{}, fmt.Errorf("nt3dcamspec: invalid lens.sensor_height %g", ls.SensorHeight)
		}
		out.FovY = make([]nt3dcam.Keyframe, len(ls.FocalLength))
		for i, k := range ls.FocalLength {
			if math.IsNaN(k.V) || math.IsInf(k.V, 0) || k.V <= 0 {
				return nt3dcam.LensTrack{}, fmt.Errorf("nt3dcamspec: lens.focal_length[%d] must be > 0, got %g", i, k.V)
			}
			es, err := convertEaseSpec(k.Ease, nt3dcam.EaseLinear)
			if err != nil {
				return nt3dcam.LensTrack{}, fmt.Errorf("nt3dcamspec: lens.focal_length[%d]: %w", i, err)
			}
			out.FovY[i] = nt3dcam.Keyframe{
				T:    k.T.Time(),
				V:    nt3dcam.FocalLengthToFovY(k.V, sh),
				Ease: es,
			}
		}
	}
	if len(ls.FocusDist) > 0 {
		out.FocusDist, err = convertScalarKeys(ls.FocusDist, 1.0)
		if err != nil {
			return nt3dcam.LensTrack{}, fmt.Errorf("nt3dcamspec: lens.focus_dist: %w", err)
		}
	}
	if len(ls.Aperture) > 0 {
		out.Aperture, err = convertScalarKeys(ls.Aperture, 1.0)
		if err != nil {
			return nt3dcam.LensTrack{}, fmt.Errorf("nt3dcamspec: lens.aperture: %w", err)
		}
	}
	return out, nil
}

func convertScalarKeys(keys []ScalarKey, scale float64) ([]nt3dcam.Keyframe, error) {
	out := make([]nt3dcam.Keyframe, len(keys))
	for i, k := range keys {
		es, err := convertEaseSpec(k.Ease, nt3dcam.EaseLinear)
		if err != nil {
			return nil, fmt.Errorf("keyframe %d: %w", i, err)
		}
		out[i] = nt3dcam.Keyframe{
			T:    k.T.Time(),
			V:    k.V * scale,
			Ease: es,
		}
	}
	return out, nil
}

func scalarKeysFromCam(keys []nt3dcam.Keyframe, scale float64) []ScalarKey {
	if len(keys) == 0 {
		return nil
	}
	out := make([]ScalarKey, len(keys))
	for i, k := range keys {
		out[i] = ScalarKey{
			T:    Duration(k.T),
			V:    cleanDecimal(k.V * scale),
			Ease: easeSpecFromCam(k.Ease, nt3dcam.EaseLinear),
		}
	}
	return out
}

func cleanDecimal(v float64) float64 {
	// Eliminate 1e-15 degree<->radian round-trip noise around exact decimals.
	rounded := math.Round(v*1e12) / 1e12
	if math.Abs(v-rounded) < 1e-13 {
		return rounded
	}
	return v
}

func convertEaseSpec(es *EaseSpec, def nt3dcam.EaseKind) (nt3dcam.EaseSpec, error) {
	if es == nil {
		return nt3dcam.EaseSpec{Kind: def}, nil
	}
	if es.Bezier != nil {
		return nt3dcam.EaseSpec{Kind: nt3dcam.EaseCubicBezier, P: *es.Bezier}, nil
	}
	switch es.Name {
	case "linear":
		return nt3dcam.EaseSpec{Kind: nt3dcam.EaseLinear}, nil
	case "ease":
		return nt3dcam.EaseSpec{Kind: nt3dcam.EaseCSS}, nil
	case "ease-in":
		return nt3dcam.EaseSpec{Kind: nt3dcam.EaseIn}, nil
	case "ease-out":
		return nt3dcam.EaseSpec{Kind: nt3dcam.EaseOut}, nil
	case "ease-in-out":
		return nt3dcam.EaseSpec{Kind: nt3dcam.EaseInOut}, nil
	case "smoothstep":
		return nt3dcam.EaseSpec{Kind: nt3dcam.EaseSmoothstep}, nil
	case "smootherstep":
		return nt3dcam.EaseSpec{Kind: nt3dcam.EaseSmootherstep}, nil
	case "hold":
		return nt3dcam.EaseSpec{Kind: nt3dcam.EaseHold}, nil
	default:
		return nt3dcam.EaseSpec{}, fmt.Errorf("unknown ease %q", es.Name)
	}
}

func easeSpecFromCam(e nt3dcam.EaseSpec, def nt3dcam.EaseKind) *EaseSpec {
	if e.Kind == nt3dcam.EaseDefault || (e.Kind == def && e.Kind != nt3dcam.EaseCubicBezier) {
		return nil
	}
	switch e.Kind {
	case nt3dcam.EaseLinear:
		return &EaseSpec{Name: "linear"}
	case nt3dcam.EaseCSS:
		return &EaseSpec{Name: "ease"}
	case nt3dcam.EaseIn:
		return &EaseSpec{Name: "ease-in"}
	case nt3dcam.EaseOut:
		return &EaseSpec{Name: "ease-out"}
	case nt3dcam.EaseInOut:
		return &EaseSpec{Name: "ease-in-out"}
	case nt3dcam.EaseSmoothstep:
		return &EaseSpec{Name: "smoothstep"}
	case nt3dcam.EaseSmootherstep:
		return &EaseSpec{Name: "smootherstep"}
	case nt3dcam.EaseHold:
		return &EaseSpec{Name: "hold"}
	case nt3dcam.EaseCubicBezier:
		arr := e.P
		return &EaseSpec{Bezier: &arr}
	default:
		return nil
	}
}

func strictJSONUnmarshal(data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("unexpected trailing JSON data")
	}
	return nil
}

func fmtFloat(v float64) string {
	if v == 0 {
		return "0"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

func fmtVec3(v [3]float64) string {
	return fmt.Sprintf("[%s, %s, %s]", fmtFloat(v[0]), fmtFloat(v[1]), fmtFloat(v[2]))
}

func fmtQuat(q [4]float64) string {
	return fmt.Sprintf("[%s, %s, %s, %s]", fmtFloat(q[0]), fmtFloat(q[1]), fmtFloat(q[2]), fmtFloat(q[3]))
}

func fmtEase(e *EaseSpec) string {
	if e == nil {
		return ""
	}
	if e.Bezier != nil {
		b := *e.Bezier
		return fmt.Sprintf("[%s, %s, %s, %s]", fmtFloat(b[0]), fmtFloat(b[1]), fmtFloat(b[2]), fmtFloat(b[3]))
	}
	return e.Name
}

func fmtTimingSeg(seg TimingSegmentSpec) string {
	if es := fmtEase(seg.Ease); es != "" {
		return fmt.Sprintf("{to: %s, over: %s, ease: %s}", fmtFloat(seg.To), seg.Over.String(), es)
	}
	return fmt.Sprintf("{to: %s, over: %s}", fmtFloat(seg.To), seg.Over.String())
}

func fmtScalarKey(k ScalarKey) string {
	if es := fmtEase(k.Ease); es != "" {
		return fmt.Sprintf("{t: %s, v: %s, ease: %s}", k.T.String(), fmtFloat(k.V), es)
	}
	return fmt.Sprintf("{t: %s, v: %s}", k.T.String(), fmtFloat(k.V))
}

func fmtLookSeg(ls LookSpec) string {
	var parts []string
	parts = append(parts, "mode: "+ls.Mode)
	if ls.Target != "" {
		parts = append(parts, "target: "+strconv.Quote(ls.Target))
	}
	if ls.Point != nil {
		parts = append(parts, "point: "+fmtVec3(*ls.Point))
	}
	if ls.Fallback != nil {
		parts = append(parts, "fallback: "+fmtVec3(*ls.Fallback))
	}
	if ls.ReverseWithMotion {
		parts = append(parts, "reverse_with_motion: true")
	}
	parts = append(parts, "from: "+ls.From.String())
	if ls.Blend > 0 {
		parts = append(parts, "blend: "+ls.Blend.String())
		if es := fmtEase(ls.BlendEase); es != "" {
			parts = append(parts, "blend_ease: "+es)
		}
	}
	if ls.Mode == "keyframes" {
		if ls.Interp != "" {
			parts = append(parts, "interp: "+ls.Interp)
		}
		var kp []string
		for _, qk := range ls.Keys {
			if es := fmtEase(qk.Ease); es != "" {
				kp = append(kp, fmt.Sprintf("{t: %s, q: %s, ease: %s}", qk.T.String(), fmtQuat(qk.Q), es))
			} else {
				kp = append(kp, fmt.Sprintf("{t: %s, q: %s}", qk.T.String(), fmtQuat(qk.Q)))
			}
		}
		parts = append(parts, "keys: ["+strings.Join(kp, ", ")+"]")
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func yamlQuoteIfNeeded(s string) string {
	if s == "" || looksNumeric(s) || s == "true" || s == "false" || s == "null" || s == "~" {
		return strconv.Quote(s)
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return strconv.Quote(s)
		}
	}
	return s
}
