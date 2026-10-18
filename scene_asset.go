// Copyright (c) 2026 Neomantra Corp.
package nt3d

import "fmt"
import nt3dmath "nimbleterminal.dev/nt3d/math"

// AssetNode is a reusable hierarchy node. Parent is -1 for a root, otherwise
// an earlier node index. Skeleton and clip NodeIDs are one-based node indices.
type AssetNode struct {
	Camera      *Camera
	Name        string
	Parent      int
	Local       nt3dmath.Mat4
	Geometry    *Geometry
	Deformation *Deformation
}

// SceneAsset is a format-independent scene template. Instantiate remaps all
// joints and track targets so multiple copies animate independently.
type SceneAsset struct {
	Nodes []AssetNode
	Clips []AnimationClip
}
type AnimatedInstance struct {
	Root  NodeID
	Nodes []NodeID
	Clips []AnimationClip
}

func (a *SceneAsset) Instantiate(s *Scene, parent NodeID, name string) (out *AnimatedInstance, err error) {
	root, err := s.Add(parent, name)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = s.Remove(root)
		}
	}()
	out = &AnimatedInstance{Root: root}
	for i, n := range a.Nodes {
		p := root
		if n.Parent >= i || n.Parent < -1 {
			return nil, fmt.Errorf("nt3d: invalid asset parent")
		}
		if n.Parent >= 0 {
			p = out.Nodes[n.Parent]
		}
		id, e := s.Add(p, fmt.Sprintf("%s/%d:%s", name, i, n.Name))
		if e != nil {
			return nil, e
		}
		out.Nodes = append(out.Nodes, id)
		if e = s.SetTransform(id, n.Local); e != nil {
			return nil, e
		}
		if n.Camera != nil {
			if e = s.SetNodeCamera(id, *n.Camera); e != nil {
				return nil, e
			}
		}
		if n.Geometry != nil {
			if e = s.SetNodeGeometry(id, *n.Geometry); e != nil {
				return nil, e
			}
		}
	}
	remap := func(id NodeID) (NodeID, error) {
		if id == 0 || int(id) > len(out.Nodes) {
			return 0, fmt.Errorf("nt3d: invalid asset target")
		}
		return out.Nodes[int(id)-1], nil
	}
	for i, n := range a.Nodes {
		if n.Deformation == nil {
			continue
		}
		d := *n.Deformation
		if d.Skeleton != nil {
			sk := *d.Skeleton
			sk.Joints = make([]NodeID, len(d.Skeleton.Joints))
			for k, j := range d.Skeleton.Joints {
				if sk.Joints[k], err = remap(j); err != nil {
					return nil, err
				}
			}
			d.Skeleton = &sk
		}
		if err = s.SetDeformation(out.Nodes[i], d); err != nil {
			return nil, err
		}
	}
	for _, c := range a.Clips {
		clip := AnimationClip{Name: c.Name}
		for _, t := range c.Tracks {
			if t.Channel != UserChannel {
				if t.Node, err = remap(t.Node); err != nil {
					return nil, err
				}
			}
			clip.Tracks = append(clip.Tracks, t)
		}
		if err = clip.Validate(); err != nil {
			return nil, err
		}
		out.Clips = append(out.Clips, clip)
	}
	if s.clips == nil {
		s.clips = map[NodeID][]AnimationClip{}
	}
	s.clips[root] = out.Clips
	return out, nil
}

// AnimationClips returns the generic clips bound to an instantiated subtree.
// Treat the returned tracks as read-only; NewAnimation makes a private copy.
func (s *Scene) AnimationClips(root NodeID) []AnimationClip {
	return append([]AnimationClip(nil), s.clips[root]...)
}
