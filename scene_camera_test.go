package nt3d

import (
	"math"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"testing"
)

func TestAuthoredCameraProjectionAndRays(t *testing.T) {
	for _, projection := range []Projection{Perspective, Orthographic} {
		c := DefaultCamera()
		c.Projection = projection
		c.YFOV = math.Pi / 2
		c.Near = .2
		c.Far = 20
		c.AspectRatio = 2
		c.Xmag, c.Ymag = 3, 2
		c.Pose = nt3dmath.Translation(nt3dmath.Vec3{Z: 4}).Mul(nt3dmath.RotationZ(.7))
		for _, xy := range [][2]float32{{50, 50}, {10, 20}, {90, 80}} {
			ray := c.RayFromPixel(xy[0], xy[1], 100, 100)
			point := ray.Origin.Add(ray.Direction.Scale(3))
			x, y, _, w := c.Matrix(1).Transform(point)
			if abs((x/w+1)*50-xy[0]) > .001 || abs((1-y/w)*50-xy[1]) > .001 {
				t.Fatal("ray and projection disagree", projection, xy)
			}
		}
		c.Far = 0
		if projection == Perspective {
			_, _, z, w := c.Matrix(1).Transform(nt3dmath.Vec3{Z: -100000})
			if z/w >= 1 || z/w < .99 {
				t.Fatal("infinite perspective far plane", z/w)
			}
		}
	}
	c := DefaultCamera()
	c.Pose = nt3dmath.Identity()
	c.Near = 0
	c.Far = 10
	c.Xmag = 2
	c.Ymag = 1
	_, _, z, w := c.Matrix(1).Transform(nt3dmath.Vec3{})
	if z/w != 0 {
		t.Fatal("orthographic zero near plane lost")
	}
}

func TestCameraSnapshotsAndRemoval(t *testing.T) {
	s := NewScene()
	p, err := s.Add(Root, "parent")
	check(t, err)
	id, err := s.Add(p, "camera")
	check(t, err)
	check(t, s.SetNodeCamera(id, Camera{Projection: Perspective, YFOV: 1, Near: .1, Far: 20}))
	c, err := s.NodeCamera(id)
	check(t, err)
	check(t, s.SetTransform(p, nt3dmath.Translation(nt3dmath.Vec3{X: 2})))
	moved, err := s.NodeCamera(id)
	check(t, err)
	if c.Eye().X != 0 || moved.Eye().X != 2 {
		t.Fatal("camera snapshot changed")
	}
	check(t, s.Remove(p))
	if len(s.Cameras(Root)) != 0 {
		t.Fatal("removed camera retained")
	}
	if _, err = s.NodeCamera(id); err == nil {
		t.Fatal("removed camera resolved")
	}
}
