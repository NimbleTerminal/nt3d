// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"image"
	"image/color"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// NewNormalMap snapshots a tangent-space RGB normal image as linear data.
// RGB encodes XYZ in [0,1], decoded to [-1,1]; alpha is ignored. Filtering
// and mipmaps average encoded vectors without sRGB conversion. The sampled
// vector is normalized after applying Material.NormalStrength to X and Y.
func NewNormalMap(src image.Image) (*Texture, error) {
	t, err := NewTexture(src)
	if err != nil {
		return nil, err
	}
	t.data = true
	for i := 3; i < len(t.pixels); i += 4 {
		t.pixels[i] = 255
	}
	return t, nil
}

func mapped(g Geometry) bool { return textured(g) || g.Material != nil && g.Material.NormalMap != nil }

var whiteMap = func() *Texture {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{255, 255, 255, 255})
	t, _ := NewTexture(img)
	return t
}()
var neutralMap = func() *Texture {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{128, 128, 255, 255})
	t, _ := NewNormalMap(img)
	return t
}()

func baseMap(m *Material) *Texture {
	if m.Texture != nil {
		return m.Texture
	}
	return whiteMap
}
func normalMap(m *Material) *Texture {
	if m.NormalMap != nil {
		return m.NormalMap
	}
	return neutralMap
}

// SetNormalMap replaces an existing normal map without uploading mesh buffers.
// Strength and sampling mode remain unchanged; use SetGeometry to change them.
func (s *Scene) SetNormalMap(name string, t *Texture) error {
	id, ok := s.Find(name)
	if !ok {
		return fmt.Errorf("nt3d: unknown node %q", name)
	}
	n := s.nodes[id]
	if !t.valid() || !t.data || n.geometry == nil || n.geometry.Material == nil || n.geometry.Material.NormalMap == nil {
		return fmt.Errorf("nt3d: normal replacement requires a normal map and normal-mapped geometry")
	}
	if n.geometry.Material.NormalMap == t {
		return nil
	}
	g := *n.geometry
	g.Material = cloneMaterial(g.Material)
	g.Material.NormalMap = t
	n.geometry = &g
	s.changed(false, true)
	return nil
}

// The basis is derived from the current triangle, including deformed positions.
func normalBasis(a, b, c *clipCorner) (tangent, bitangent nt3dmath.Vec3) {
	e1, e2 := b.world.Sub(a.world), c.world.Sub(a.world)
	u1, v1, u2, v2 := b.uv.U-a.uv.U, b.uv.V-a.uv.V, c.uv.U-a.uv.U, c.uv.V-a.uv.V
	det := u1*v2 - v1*u2
	if abs(det) <= 1e-8*max(abs(u1*v2), abs(v1*u2), 1e-30) {
		return
	}
	return e1.Scale(v2).Sub(e2.Scale(v1)).Scale(1 / det), e2.Scale(u1).Sub(e1.Scale(u2)).Scale(1 / det)
}
func perturbNormal(n, t, b nt3dmath.Vec3, c [4]float32, strength float32) nt3dmath.Vec3 {
	if strength == 0 {
		return n
	}
	t = t.Sub(n.Scale(n.Dot(t)))
	if t.Dot(t) < 1e-12 || b.Dot(b) < 1e-12 {
		return n
	}
	t = t.Normalize()
	cross := n.Cross(t)
	if cross.Dot(b) < 0 {
		cross = cross.Scale(-1)
	}
	out := t.Scale((2*c[0] - 1) * strength).Add(cross.Scale((2*c[1] - 1) * strength)).Add(n.Scale(2*c[2] - 1))
	if out.Dot(out) < 1e-12 {
		return n
	}
	return out.Normalize()
}
