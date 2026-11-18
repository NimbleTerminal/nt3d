// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"math"
	"strings"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// invalidCameras are cameras with a field that cannot mean anything, each
// with the field's name, which the error must mention.
func invalidCameras() map[string]Camera {
	nan, inf := math.NaN(), math.Inf(1)
	mirrored := nt3dmath.Identity()
	mirrored[0] = -1
	return map[string]Camera{
		"FOV":         {FOV: 200},
		"FOV ":        {FOV: -10},
		"FOV  ":       {FOV: nan},
		"Far":         {Near: 10, Far: 5},
		"Near":        {Near: -1},
		"Far ":        {Far: inf},
		"Xmag":        {Xmag: 2},
		"Ymag":        {Ymag: 2},
		"Xmag ":       {Xmag: -1, Ymag: 1},
		"AspectRatio": {AspectRatio: -1},
		"Distance":    {Distance: -3},
		"Distance ":   {Distance: nan},
		"Elevation":   {Elevation: nan},
		"Azimuth":     {Azimuth: inf},
		"Target":      {Target: nt3dmath.Vec3{Y: float32(nan)}},
		"Origin":      {Origin: [3]float64{0, inf, 0}},
		"ResumeAfter": {ResumeAfter: -1},
		"Projection":  {Projection: Perspective + 1},
		"Pose":        {Pose: mirrored},
		"Pose ":       {Pose: nt3dmath.Mat4{0: 1}}, // Flattened: no inverse.
		"Pose  ":      {Pose: nt3dmath.Mat4{0: float32(nan), 5: 1, 10: 1, 15: 1}},
	}
}

// Validate reports a field that cannot mean anything, by name: a number that
// is not finite, a negative where only a size makes sense, a field of view of
// a half turn or more, a far plane at or before the near one, half an
// orthographic extent, a projection that is not one, or a pose that is
// flattened or mirrored.
func TestCameraValidateRejectsWhatCannotMeanAnything(t *testing.T) {
	for field, c := range invalidCameras() {
		field = strings.TrimSpace(field)
		err := c.Validate()
		if err == nil {
			t.Errorf("%s: %+v was accepted", field, c)
		} else if !strings.Contains(err.Error(), field) {
			t.Errorf("%s: the error does not name the field: %v", field, err)
		}
	}
}

// Validate accepts what a camera corrects on purpose: zeros, which mean the
// default, and orbit values outside their limits, which are held to them.
func TestCameraValidateAcceptsDefaultsAndOrbitLimits(t *testing.T) {
	for name, c := range map[string]Camera{
		"zero":            {},
		"default":         DefaultCamera(),
		"past the pole":   {Elevation: 95},
		"many turns":      {Azimuth: 725},
		"very far":        {Distance: 1e9},
		"very near":       {Distance: 1e-9},
		"infinite far":    {Projection: Perspective, Near: .1},
		"ortho from zero": {Far: 10},
		"extents":         {Xmag: 2, Ymag: 1},
		"posed":           {Pose: nt3dmath.Translation(nt3dmath.Vec3{X: 3}).Mul(nt3dmath.Scaling(nt3dmath.Vec3{X: 2, Y: 2, Z: 2}))},
	} {
		if err := c.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A scene refuses a camera that Validate refuses, where it refuses a bad
// geometry: taking a frame and setting a node's camera return the error
// instead of drawing some other view.
func TestSceneRejectsAnInvalidCamera(t *testing.T) {
	s := NewScene()
	id, err := s.Add(Root, "camera")
	check(t, err)
	for field, c := range invalidCameras() {
		if _, err := s.Frame(c, 16, 16); err == nil {
			t.Errorf("%s: Frame drew with %+v", field, c)
		}
		if _, err := s.StillFrame(c, 16, 16); err == nil {
			t.Errorf("%s: StillFrame drew with %+v", field, c)
		}
		if strings.HasPrefix(field, "Pose") {
			continue // A node's transform places its camera; Pose is not used.
		}
		if err := s.SetNodeCamera(id, c); err == nil {
			t.Errorf("%s: SetNodeCamera kept %+v", field, c)
		}
	}
	if _, err := s.NodeCamera(id); err == nil {
		t.Error("a refused camera was kept on the node")
	}
}
