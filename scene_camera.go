// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"nimbleterminal.dev/nt3d/math"
)

// SetNodeCamera attaches projection settings to a node. Its world transform
// supplies the camera pose (-Z forward, +Y up), including animated ancestors.
// Pose, Target and orbit angles in camera are ignored. Zero lens fields retain
// Camera defaults. NodeCamera returns a detached snapshot for Frame or a widget.
func (s *Scene) SetNodeCamera(id NodeID, camera Camera) error {
	n := s.nodes[id]
	if n == nil || id == Root {
		return fmt.Errorf("nt3d: invalid camera node")
	}
	camera = camera.Valid()
	camera.Pose = nt3dmath.Mat4{}
	n.camera = &camera
	s.changed(false, false)
	return nil
}

// Cameras lists camera nodes in a subtree, in hierarchy order.
func (s *Scene) Cameras(root NodeID) []NodeID {
	var out []NodeID
	var visit func(NodeID)
	visit = func(id NodeID) {
		n := s.nodes[id]
		if n == nil {
			return
		}
		if n.camera != nil {
			out = append(out, id)
		}
		for _, c := range n.Children {
			visit(c)
		}
	}
	visit(root)
	return out
}

// NodeCamera resolves a camera's current pose. Call again after animating or
// moving its ancestors; the returned value does not retain a link to the scene.
func (s *Scene) NodeCamera(id NodeID) (Camera, error) {
	n := s.nodes[id]
	if n == nil || n.camera == nil {
		return Camera{}, fmt.Errorf("nt3d: node has no camera")
	}
	pose := s.world(id)
	eye := pose.Point(nt3dmath.Vec3{})
	forward := pose.Direction(nt3dmath.Vec3{Z: -1}).Normalize()
	c := n.camera.LookAt(eye, eye.Add(forward))
	c.Pose = pose
	c.AutoRotate = false
	return c, nil
}
