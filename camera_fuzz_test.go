// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"encoding/binary"
	"image/color"
	"math"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// FuzzCamera holds two things for any camera at all.
//
// Valid, which corrects a camera, always yields one that Validate, which
// checks it, accepts: the camera-path bridge and the widget correct a camera
// with Valid and hand it to Scene.Frame, which refuses what Validate refuses.
//
// And a camera Validate accepts is one a scene can draw with: Scene.Frame
// takes a frame, and the frame is one a renderer accepts, with a finite view
// matrix, however extreme the accepted values are. The scene's one node
// shares the camera's Origin, as nodes that far out do.
func FuzzCamera(f *testing.F) {
	pose := func(m nt3dmath.Mat4) []byte {
		b := make([]byte, 64)
		for i, v := range m {
			binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(v))
		}
		return b
	}
	d := DefaultCamera()
	f.Add(d.Elevation, d.Azimuth, d.Distance, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, float32(0), float32(0), float32(0), 0.0, 0.0, 0.0, uint8(0), []byte{})
	f.Add(89.0, 725.0, 1e-9, 179.0, 2.0, .1, 0.0, 0.0, 0.0, float32(1), float32(2), float32(3), 4e5, 5e6, 80.0, uint8(1), []byte{})
	f.Add(0.0, 0.0, 1e9, 60.0, 0.0, 0.0, 10.0, 2.0, 1.0, float32(0), float32(0), float32(0), 0.0, 0.0, 0.0, uint8(0), pose(nt3dmath.Translation(nt3dmath.Vec3{X: 3, Y: -2, Z: 5})))
	f.Add(math.NaN(), math.Inf(1), -1.0, 200.0, -1.0, 10.0, 5.0, 2.0, 0.0, float32(math.NaN()), float32(0), float32(0), math.Inf(-1), 0.0, 0.0, uint8(7), pose(nt3dmath.Mat4{0: 1}))
	// Orthographic with Near alone, at the automatic far plane: once no depth
	// at all, and a view matrix that was not finite.
	f.Add(25.0, 40.0, 3.4, 0.0, 26.0, 200.0, 0.0, 0.0, 0.0, float32(0), float32(0), float32(100), 0.0, 54.0, 59.0, uint8(0), []byte{})
	// Values that are finite and still more than a float32 projection or
	// view holds, alone or together.
	f.Add(0.0, 0.0, 0.0, 1e-40, 1e-300, 1e20, 1e21, 0.0, 0.0, float32(3e38), float32(3e38), float32(3e38), 1e308, -1e308, 1e308, uint8(1), []byte{})
	f.Add(0.0, 0.0, 0.0, 0.0, 1e300, 1e6, 1e6+1e-3, 1e-300, 1e300, float32(1e9), float32(0), float32(0), 0.0, 0.0, 0.0, uint8(0), pose(nt3dmath.Translation(nt3dmath.Vec3{X: 3e38, Y: 3e38, Z: 3e38})))

	s := NewScene()
	ball, err := BuildSphere(Sphere{Radius: .5, Segments: 8, Rings: 4, Color: color.RGBA{A: 255}})
	if err != nil {
		f.Fatal(err)
	}
	node, err := s.SetGeometry("ball", ball)
	if err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, elevation, azimuth, distance, fov, aspect, near, far, xmag, ymag float64, tx, ty, tz float32, ox, oy, oz float64, projection uint8, poseBytes []byte) {
		c := Camera{
			Elevation: elevation, Azimuth: azimuth, Distance: distance,
			FOV: fov, AspectRatio: aspect, Near: near, Far: far, Xmag: xmag, Ymag: ymag,
			Target: nt3dmath.Vec3{X: tx, Y: ty, Z: tz}, Origin: [3]float64{ox, oy, oz},
			Projection: Projection(projection),
		}
		if len(poseBytes) >= 64 {
			for i := range c.Pose {
				c.Pose[i] = math.Float32frombits(binary.LittleEndian.Uint32(poseBytes[i*4:]))
			}
		}
		if err := c.Valid().Validate(); err != nil {
			t.Fatalf("Valid returned a camera Validate refuses: %v\n%+v", err, c.Valid())
		}
		if c.Validate() != nil {
			if _, err := s.Frame(c, 32, 24); err == nil {
				t.Fatalf("Scene.Frame drew with a camera Validate refuses: %+v", c)
			}
			return
		}
		if err := s.SetNodeOrigin(node, c.Origin); err != nil {
			t.Fatalf("a node refused the Origin a camera accepts: %v", err)
		}
		frame, err := s.Frame(c, 32, 24)
		if err != nil {
			t.Fatalf("Scene.Frame refused a camera Validate accepts: %v\n%+v", err, c)
		}
		if err := validateFrame(frame); err != nil {
			t.Fatalf("a camera Validate accepts made a frame no renderer draws: %v\n%+v", err, c)
		}
	})
}
