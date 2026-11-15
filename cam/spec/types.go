// Copyright (c) 2026 Neomantra Corp.

package nt3dcamspec

import (
	_ "embed"
	"time"
)

// SchemaVersion is the supported camera_path specification version.
const SchemaVersion = 1

// SchemaJSON holds the canonical Draft 2020-12 JSON Schema for camera_path v1.
//
//go:embed ntcamera.schema.json
var SchemaJSON []byte

// Duration represents a non-negative duration encoded in YAML/JSON as either a
// Go duration string ("12s", "500ms") or numeric seconds (12.0).
type Duration time.Duration

// EaseSpec represents an easing curve encoded as either a named preset string
// ("linear", "ease-in-out", "hold", ...) or a 4-element CSS cubic-bezier array
// [x1, y1, x2, y2].
type EaseSpec struct {
	Name   string      // Preset name when Bezier == nil.
	Bezier *[4]float64 // [x1, y1, x2, y2] when custom cubic-bezier is used.
}

// Document wraps a top-level `camera_path:` root mapping in YAML or JSON.
type Document struct {
	CameraPath CameraPath `json:"camera_path" yaml:"camera_path"`
}

// CameraPath is the serializable v1 representation of a declarative camera path.
// Angles (Roll, Lens.Fov) are in degrees; distances and positions are in meters.
type CameraPath struct {
	Version  int            `json:"version" yaml:"version"`
	Name     string         `json:"name,omitempty" yaml:"name,omitempty"`
	Frame    string         `json:"frame,omitempty" yaml:"frame,omitempty"`
	Origin   *GeoOriginSpec `json:"origin,omitempty" yaml:"origin,omitempty"`
	Up       *[3]float64    `json:"up,omitempty" yaml:"up,omitempty"`
	Duration Duration       `json:"duration" yaml:"duration"`
	Loop     string         `json:"loop,omitempty" yaml:"loop,omitempty"`
	Position PositionSpec   `json:"position" yaml:"position"`
	Timing   *TimingSpec    `json:"timing,omitempty" yaml:"timing,omitempty"`
	Look     []LookSpec     `json:"look,omitempty" yaml:"look,omitempty"`
	Roll     []ScalarKey    `json:"roll,omitempty" yaml:"roll,omitempty"`
	Lens     *LensSpec      `json:"lens,omitempty" yaml:"lens,omitempty"`
	Markers  []MarkerSpec   `json:"markers,omitempty" yaml:"markers,omitempty"`
}

// GeoOriginSpec anchors an "enu" frame in WGS84 degrees and meters.
type GeoOriginSpec struct {
	Lat float64 `json:"lat" yaml:"lat"`
	Lon float64 `json:"lon" yaml:"lon"`
	Alt float64 `json:"alt,omitempty" yaml:"alt,omitempty"`
}

// BezierKnotSpec describes one cubic Bezier anchor with either world-space
// handles (In, Out) or Hermite tangents (TangentIn, TangentOut).
type BezierKnotSpec struct {
	P          [3]float64  `json:"p" yaml:"p"`
	In         *[3]float64 `json:"in,omitempty" yaml:"in,omitempty"`
	Out        *[3]float64 `json:"out,omitempty" yaml:"out,omitempty"`
	TangentIn  *[3]float64 `json:"tangent_in,omitempty" yaml:"tangent_in,omitempty"`
	TangentOut *[3]float64 `json:"tangent_out,omitempty" yaml:"tangent_out,omitempty"`
}

// PositionSpec describes the spatial curve in a CameraPath document.
type PositionSpec struct {
	Interp  string           `json:"interp,omitempty" yaml:"interp,omitempty"`
	Closed  bool             `json:"closed,omitempty" yaml:"closed,omitempty"`
	Points  [][3]float64     `json:"points,omitempty" yaml:"points,omitempty"`
	Knots   []BezierKnotSpec `json:"knots,omitempty" yaml:"knots,omitempty"`
	Samples []any            `json:"samples,omitempty" yaml:"samples,omitempty"` // Reserved for v2 GNSS/IMU poses.
}

// TimingSegmentSpec describes one segment in the timing track (`t -> s in [0,1]`).
type TimingSegmentSpec struct {
	To   float64   `json:"to" yaml:"to"`
	Over Duration  `json:"over" yaml:"over"`
	Ease *EaseSpec `json:"ease,omitempty" yaml:"ease,omitempty"`
}

// TimingSpec accepts either a bare list of segments (`timing: [...]`) or an
// object `{start, segments}` during YAML/JSON unmarshaling.
type TimingSpec struct {
	Start    float64             `json:"start,omitempty" yaml:"start,omitempty"`
	Segments []TimingSegmentSpec `json:"segments" yaml:"segments"`
}

// ScalarKey is a `{t, v, ease}` scalar keyframe in spec units (degrees for
// angles, meters for focus distance, millimeters for focal length).
type ScalarKey struct {
	T    Duration  `json:"t" yaml:"t"`
	V    float64   `json:"v" yaml:"v"`
	Ease *EaseSpec `json:"ease,omitempty" yaml:"ease,omitempty"`
}

// QuatKey is a `{t, q: [x, y, z, w], ease}` quaternion keyframe.
type QuatKey struct {
	T    Duration   `json:"t" yaml:"t"`
	Q    [4]float64 `json:"q" yaml:"q"`
	Ease *EaseSpec  `json:"ease,omitempty" yaml:"ease,omitempty"`
}

// LookSpec describes one segment in the orientation (`look`) track.
type LookSpec struct {
	Mode              string      `json:"mode" yaml:"mode"`
	From              Duration    `json:"from,omitempty" yaml:"from,omitempty"`
	Blend             Duration    `json:"blend,omitempty" yaml:"blend,omitempty"`
	BlendEase         *EaseSpec   `json:"blend_ease,omitempty" yaml:"blend_ease,omitempty"`
	Target            string      `json:"target,omitempty" yaml:"target,omitempty"`
	Point             *[3]float64 `json:"point,omitempty" yaml:"point,omitempty"`
	Fallback          *[3]float64 `json:"fallback,omitempty" yaml:"fallback,omitempty"`
	ReverseWithMotion bool        `json:"reverse_with_motion,omitempty" yaml:"reverse_with_motion,omitempty"`
	Interp            string      `json:"interp,omitempty" yaml:"interp,omitempty"`
	Keys              []QuatKey   `json:"keys,omitempty" yaml:"keys,omitempty"`
}

// LensSpec describes the optical track of a CameraPath.
type LensSpec struct {
	Fov          []ScalarKey `json:"fov,omitempty" yaml:"fov,omitempty"`
	FocalLength  []ScalarKey `json:"focal_length,omitempty" yaml:"focal_length,omitempty"`
	SensorHeight float64     `json:"sensor_height,omitempty" yaml:"sensor_height,omitempty"`
	FocusDist    []ScalarKey `json:"focus_dist,omitempty" yaml:"focus_dist,omitempty"`
	Aperture     []ScalarKey `json:"aperture,omitempty" yaml:"aperture,omitempty"`
	Near         float64     `json:"near,omitempty" yaml:"near,omitempty"`
	Far          float64     `json:"far,omitempty" yaml:"far,omitempty"`
}

// MarkerSpec names a synchronization timestamp on the camera path.
type MarkerSpec struct {
	Name string   `json:"name" yaml:"name"`
	T    Duration `json:"t" yaml:"t"`
}
