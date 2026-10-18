// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"image/color"
	"math"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// OutlineStyle draws an unlit, expanded back-face hull around indexed triangles.
// Width is in output pixels, 0 (disabled) to 32, independent of supersampling.
// Color uses RGB; alpha is ignored. Transparent meshes retain their vertex and
// texture opacity. Cutouts retain their alpha test. The hull is depth tested
// and fogged, but does not affect picking, shadows, or wireframe.
//
// Use closed meshes with outward, smooth vertex normals for continuous contours.
// Flat or split normals can leave gaps; open sheets have no back-face contour.
// This is an outer silhouette effect, not a sharp-crease or texture-edge detector.
type OutlineStyle struct {
	Width float32
	Color color.RGBA
}

func (o OutlineStyle) validate() error {
	if !validFloat(o.Width) || o.Width < 0 || o.Width > 32 {
		return fmt.Errorf("nt3d: outline width must be 0..32 pixels")
	}
	return nil
}

// SetOutline changes the outline of existing geometry in root's subtree.
// It preserves animation, deformation, shared mesh buffers, and old frames.
// Use Root to change the whole scene. Newly added nodes keep their own style.
func (s *Scene) SetOutline(root NodeID, style OutlineStyle) error {
	if err := style.validate(); err != nil {
		return err
	}
	if s.nodes[root] == nil {
		return fmt.Errorf("nt3d: unknown outline root %d", root)
	}
	changed := false
	pending := []NodeID{root}
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		n := s.nodes[id]
		pending = append(pending, n.Children...)
		if n.geometry != nil && n.geometry.Outline != style {
			g := *n.geometry
			g.Outline = style
			n.geometry = &g
			changed = true
		}
	}
	if changed {
		s.changed(false, false)
	}
	return nil
}

const (
	outlineOpaque      = -1
	outlineTransparent = -2
)

// expandOutline moves only clip X/Y along the projected world normal. Keeping
// Z/W unchanged gives the hull exactly the original back surface's depth.
// The shader uses the same projection derivative, including perspective W.
func expandOutline(c *clipCorner, n nt3dmath.Vec3, f *Frame, width float32) {
	if c.w <= .00001 {
		return
	}
	m := &f.Matrix
	x := m[0]*n.X + m[4]*n.Y + m[8]*n.Z
	y := m[1]*n.X + m[5]*n.Y + m[9]*n.Z
	w := m[3]*n.X + m[7]*n.Y + m[11]*n.Z
	dx, dy := (x*c.w-c.x*w)*float32(f.Width), (y*c.w-c.y*w)*float32(f.Height)
	l := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	if l <= .000001 {
		return
	}
	c.x += dx / l * width * 2 / float32(f.Width) * c.w
	c.y += dy / l * width * 2 / float32(f.Height) * c.w
}
