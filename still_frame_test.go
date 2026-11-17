package nt3d

import (
	"image/color"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// stillScene has one node with two levels of detail that switch 20 units
// from the camera, and a hysteresis band of 18..22 around the switch. tier
// reports which level a frame of it shows.
func stillScene(t *testing.T) (s *Scene, camAt func(x float32) Camera, tier func(Frame) int) {
	t.Helper()
	s = NewScene()
	id, err := s.Add(Root, "item")
	check(t, err)
	check(t, s.SetLOD(id, []LODLevel{
		{MaxDistance: 20, Geometry: Geometry{Points: []Point{{Radius: 3, Color: color.RGBA{220, 40, 40, 255}}}}},
		{MaxDistance: 40, Geometry: Geometry{Points: []Point{{Radius: 3, Color: color.RGBA{40, 200, 60, 255}}}}},
	}))
	check(t, s.SetLODHysteresis(0.1))
	camAt = func(x float32) Camera {
		c := DefaultCamera()
		c.Projection = Perspective
		return c.LookAt(nt3dmath.Vec3{X: x}, nt3dmath.Vec3{})
	}
	tier = func(f Frame) int {
		t.Helper()
		f.Cull = false
		if f.Stats().LODTierInstances[0] == 1 {
			return 0
		}
		return 1
	}
	return s, camAt, tier
}

// A still shows what its camera sees, whatever was drawn before it: the
// level of detail comes from distance alone.
func TestStillFrameIgnoresWhatWasDrawnBefore(t *testing.T) {
	s, camAt, tier := stillScene(t)
	f, err := s.Frame(camAt(19), 64, 64)
	check(t, err)
	if tier(f) != 0 {
		t.Fatal("the first frame at 19 shows the coarse level")
	}
	// At 21 an interactive frame holds the fine level, inside the band.
	f, err = s.Frame(camAt(21), 64, 64)
	check(t, err)
	if tier(f) != 0 {
		t.Fatal("hysteresis did not hold the fine level at 21; the test's premise is wrong")
	}
	still, err := s.StillFrame(camAt(21), 64, 64)
	check(t, err)
	if tier(still) != 1 {
		t.Fatal("a still at 21 kept the level the frames before it had chosen")
	}
	if !still.Complete {
		t.Fatal("a still is not marked Complete")
	}
}

// A still leaves the scene as it found it, so the frames after it draw what
// they would have drawn without it.
func TestStillFrameLeavesLaterFramesAlone(t *testing.T) {
	s, camAt, tier := stillScene(t)
	rev := s.Revision()
	_, err := s.Frame(camAt(19), 64, 64)
	check(t, err)
	// A still from far away would, as a frame, move the node to its coarse level.
	far, err := s.StillFrame(camAt(30), 64, 64)
	check(t, err)
	if tier(far) != 1 {
		t.Fatal("a still at 30 shows the fine level")
	}
	f, err := s.Frame(camAt(21), 64, 64)
	check(t, err)
	if tier(f) != 0 {
		t.Fatal("a still changed the level the next frame holds")
	}
	if s.Revision() != rev {
		t.Fatal("making frames changed the scene's revision")
	}
}

// With no hysteresis to set aside, a still and a frame draw the same nodes.
func TestStillFrameMatchesAFirstFrame(t *testing.T) {
	s := frameScene(t, 20, false)
	f, err := s.Frame(DefaultCamera(), 64, 64)
	check(t, err)
	still, err := s.StillFrame(DefaultCamera(), 64, 64)
	check(t, err)
	if len(still.Instances) != len(f.Instances) || still.Matrix != f.Matrix {
		t.Fatalf("a still has %d instances, a frame %d", len(still.Instances), len(f.Instances))
	}
	for i := range f.Instances {
		if still.Instances[i] != f.Instances[i] || still.GeometryRevisions[i] != f.GeometryRevisions[i] {
			t.Fatalf("instance %d differs between a still and a frame", i)
		}
	}
}
