// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"image/color"
	"math"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// OutlineStyle draws an unlit, expanded back-face hull around indexed triangles.
// Width is in output pixels, 0 (disabled) to 32, independent of supersampling,
// when Units is PixelUnits (the default), or in world units (>= 0, clamped to
// 32 output pixels on screen) when Units is WorldUnits.
// Color uses RGB; alpha is ignored. Transparent meshes retain their vertex and
// texture opacity. Cutouts retain their alpha test. The hull is depth tested
// and fogged, but does not affect picking, shadows, or wireframe.
//
// Vertices that share a position with different normals are expanded along
// their averaged contour normal so faceted meshes (boxes, prisms, cylinders)
// form seamless silhouettes. Open sheets have no back-face contour.
type OutlineStyle struct {
	Width float32
	Color color.RGBA
	Units PointUnits
}

func (o OutlineStyle) validate() error {
	if o.Units > WorldUnits {
		return fmt.Errorf("nt3d: unknown outline units")
	}
	if !validFloat(o.Width) || o.Width < 0 || (o.Units == PixelUnits && o.Width > 32) {
		return fmt.Errorf("nt3d: outline width must be 0..32 pixels")
	}
	return nil
}

// outlineWidth is an outline's expansion on screen, in render pixels, with the
// vertex at clip w, in a frame drawn at scale times its pixel sizes.
func (f *Frame) outlineWidth(style OutlineStyle, w, scale float32) float32 {
	if style.Units != WorldUnits {
		return style.Width * scale
	}
	m := &f.Matrix
	k := float32(math.Sqrt(float64(m[1]*m[1] + m[5]*m[5] + m[9]*m[9])))
	return min(32*scale, style.Width*k*float32(f.Height)*.5/max(w, .00001))
}

// packOutlineNormal quantizes a unit normal into an 18-bit non-zero integer
// (6 bits per axis in 1..63, with 32 as zero) so it fits in vertex flags or
// spare float32 fields on the GPU and matches the software renderer.
func packOutlineNormal(n nt3dmath.Vec3) uint32 {
	q := func(v float32) uint32 {
		return uint32(min(63, max(1, int(math.Round(float64(v)*31+32)))))
	}
	return q(n.X) | q(n.Y)<<6 | q(n.Z)<<12
}

func unpackOutlineNormal(code uint32) nt3dmath.Vec3 {
	d := func(shift uint) float32 {
		return (float32((code>>shift)&63) - 32) / 31
	}
	return nt3dmath.Vec3{X: d(0), Y: d(6), Z: d(12)}.Normalize()
}

// outlineNormals returns per-vertex packed smooth contour normals (non-zero
// where vertices at the same position have different normals), or nil when
// every shared position already has a single normal.
func outlineNormals(vertices []Vertex) []uint32 {
	if len(vertices) < 2 {
		return nil
	}
	type group struct {
		first nt3dmath.Vec3
		split bool
	}
	byPos := make(map[nt3dmath.Vec3]group, len(vertices))
	anySplit := false
	for _, v := range vertices {
		n := v.Normal
		if n.Dot(n) == 0 {
			continue
		}
		g, ok := byPos[v.Position]
		if !ok {
			byPos[v.Position] = group{first: n}
			continue
		}
		if g.first != n {
			g.split = true
			anySplit = true
			byPos[v.Position] = g
		}
	}
	if !anySplit {
		return nil
	}
	type posNorm struct {
		pos, norm nt3dmath.Vec3
	}
	seen := make(map[posNorm]struct{}, len(vertices))
	sums := make(map[nt3dmath.Vec3]nt3dmath.Vec3, len(byPos))
	for _, v := range vertices {
		if !byPos[v.Position].split || v.Normal.Dot(v.Normal) == 0 {
			continue
		}
		pn := posNorm{pos: v.Position, norm: v.Normal}
		if _, dup := seen[pn]; dup {
			continue
		}
		seen[pn] = struct{}{}
		sums[v.Position] = sums[v.Position].Add(v.Normal.Normalize())
	}
	codes := make([]uint32, len(vertices))
	for i, v := range vertices {
		if !byPos[v.Position].split {
			continue
		}
		sum := sums[v.Position]
		if sum.Dot(sum) > 1e-6 {
			codes[i] = packOutlineNormal(sum.Normalize())
		}
	}
	return codes
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
