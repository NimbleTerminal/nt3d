// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"fmt"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// SetNodeLight attaches punctual light settings to a node. Its world transform
// supplies the light's world position and orientation (-Z forward, matching
// glTF KHR_lights_punctual and SetNodeCamera), including animated ancestors.
// Position and Direction in light are ignored. NodeLight and SceneLights
// return detached snapshots for Frame or a widget.
func (s *Scene) SetNodeLight(id NodeID, light Light) error {
	n := s.nodes[id]
	if n == nil || id == Root {
		return fmt.Errorf("nt3d: invalid light node")
	}
	check := light
	check.Position = nt3dmath.Vec3{}
	check.Direction = nt3dmath.Vec3{Z: -1}
	if !validLighting(Lighting{Lights: []Light{check}}) {
		return fmt.Errorf("nt3d: invalid node light")
	}
	stored := check
	n.light = &stored
	s.changed(false, false)
	return nil
}

// Lights lists light nodes in a subtree, in hierarchy order.
func (s *Scene) Lights(root NodeID) []NodeID {
	var out []NodeID
	var visit func(NodeID)
	visit = func(id NodeID) {
		n := s.nodes[id]
		if n == nil {
			return
		}
		if n.light != nil {
			out = append(out, id)
		}
		for _, c := range n.Children {
			visit(c)
		}
	}
	visit(root)
	return out
}

// NodeLight resolves a light node's current world position and direction.
// Call again after animating or moving its ancestors; the returned Light does
// not retain a link to the scene.
func (s *Scene) NodeLight(id NodeID) (Light, error) {
	n := s.nodes[id]
	if n == nil || n.light == nil {
		return Light{}, fmt.Errorf("nt3d: node has no light")
	}
	return s.resolveNodeLight(id, n), nil
}

// SceneLights resolves up to MaxLights visible light nodes in root's subtree,
// in hierarchy order.
func (s *Scene) SceneLights(root NodeID) []Light {
	return s.sceneLightsRelative(root, [3]float64{})
}

func (s *Scene) sceneLightsRelative(root NodeID, ref [3]float64) []Light {
	var out []Light
	var visit func(NodeID)
	visit = func(id NodeID) {
		if len(out) >= MaxLights {
			return
		}
		n := s.nodes[id]
		if n == nil || !n.Visible {
			return
		}
		if n.light != nil {
			out = append(out, s.resolveNodeLightRelative(id, n, ref))
		}
		for _, c := range n.Children {
			visit(c)
			if len(out) >= MaxLights {
				return
			}
		}
	}
	if root != Root && !s.visible(root) {
		return nil
	}
	visit(root)
	return out
}

func (s *Scene) resolveNodeLight(id NodeID, n *sceneNode) Light {
	return s.resolveNodeLightRelative(id, n, [3]float64{})
}

func (s *Scene) resolveNodeLightRelative(id NodeID, n *sceneNode, ref [3]float64) Light {
	pose := s.worldRelative(id, ref)
	out := *n.light
	out.Position = pose.Point(nt3dmath.Vec3{})
	switch out.Kind {
	case DirectionalLight:
		dir := pose.Direction(nt3dmath.Vec3{Z: 1}).Normalize()
		if dir == (nt3dmath.Vec3{}) {
			dir = nt3dmath.Vec3{Z: 1}
		}
		out.Direction = dir
	case SpotLight:
		dir := pose.Direction(nt3dmath.Vec3{Z: -1}).Normalize()
		if dir == (nt3dmath.Vec3{}) {
			dir = nt3dmath.Vec3{Z: -1}
		}
		out.Direction = dir
	default:
		out.Direction = nt3dmath.Vec3{}
	}
	return out
}
