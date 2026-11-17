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

func hasShaderHook(g Geometry) bool {
	return g.Displacement != nil || (g.Material != nil && g.Material.Shader != nil)
}

func materialOrZero(m *Material) Material {
	if m == nil {
		return Material{}
	}
	return *m
}

func mapped(g Geometry) bool {
	return textured(g) || hasShaderHook(g) || g.Material != nil && (g.Material.LightMap != nil || g.Material.NormalMap != nil || g.Material.ORMMap != nil || g.Material.EmissiveMap != nil)
}

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
var defaultORMMap = func() *Texture {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{255, 255, 255, 255})
	t, _ := NewORMMap(img)
	return t
}()

func baseMap(m *Material) *Texture {
	if m != nil && m.Texture != nil {
		return m.Texture
	}
	return whiteMap
}
func normalMap(m *Material) *Texture {
	if m != nil && m.NormalMap != nil {
		return m.NormalMap
	}
	return neutralMap
}
func ormMap(m *Material) *Texture {
	if m != nil && m.ORMMap != nil {
		return m.ORMMap
	}
	return defaultORMMap
}
func emissiveMap(m *Material) *Texture {
	if m != nil && m.EmissiveMap != nil {
		return m.EmissiveMap
	}
	return whiteMap
}

// NewORMMap snapshots a linear occlusion/roughness/metallic RGB image without
// sRGB conversion: red encodes ambient occlusion, green encodes perceptual
// roughness, and blue encodes metalness in [0,1]. Alpha is ignored.
func NewORMMap(src image.Image) (*Texture, error) {
	return NewNormalMap(src)
}

// NewShaderORMMap creates a linear-data procedural ORM map with zero parameters.
func NewShaderORMMap(source string, fallback image.Image) (*Texture, error) {
	return NewShaderNormalMapWithParameters(source, [4]float32{}, fallback)
}

// NewShaderORMMapWithParameters generates linear ORM data on the GPU and uses
// fallback in software.
func NewShaderORMMapWithParameters(source string, parameters [4]float32, fallback image.Image) (*Texture, error) {
	return NewShaderNormalMapWithParameters(source, parameters, fallback)
}

// SetORMMap replaces an existing ORM map without reuploading mesh buffers.
func (s *Scene) SetORMMap(name string, t *Texture) error {
	id, ok := s.Find(name)
	if !ok {
		return fmt.Errorf("nt3d: unknown node %q", name)
	}
	n := s.nodes[id]
	if !t.valid() || !t.data || n.geometry == nil {
		return fmt.Errorf("nt3d: ORM replacement requires NewORMMap and ORM-mapped geometry")
	}
	if len(n.lod) > 0 {
		anyMapped, changed := false, false
		for i := range n.lod {
			lg := n.lod[i].geometry
			if lg.Material == nil || lg.Material.ORMMap == nil {
				continue
			}
			anyMapped = true
			if lg.Material.ORMMap == t {
				continue
			}
			g := *lg
			g.Material = cloneMaterial(g.Material)
			g.Material.ORMMap = t
			n.lod[i].geometry = &g
			changed = true
		}
		if !anyMapped {
			return fmt.Errorf("nt3d: ORM replacement requires NewORMMap and ORM-mapped geometry")
		}
		if changed {
			n.geometry = n.lod[0].geometry
			s.changed(false, true)
		}
		return nil
	}
	if n.geometry.Material == nil || n.geometry.Material.ORMMap == nil {
		return fmt.Errorf("nt3d: ORM replacement requires NewORMMap and ORM-mapped geometry")
	}
	if n.geometry.Material.ORMMap == t {
		return nil
	}
	g := *n.geometry
	g.Material = cloneMaterial(g.Material)
	g.Material.ORMMap = t
	n.geometry = &g
	s.changed(false, true)
	return nil
}

// SetEmissiveMap replaces an existing emissive map without reuploading mesh buffers.
func (s *Scene) SetEmissiveMap(name string, t *Texture) error {
	id, ok := s.Find(name)
	if !ok {
		return fmt.Errorf("nt3d: unknown node %q", name)
	}
	n := s.nodes[id]
	if !t.valid() || t.data || n.geometry == nil {
		return fmt.Errorf("nt3d: emissive replacement requires a color texture and emissive-mapped geometry")
	}
	if len(n.lod) > 0 {
		anyMapped, changed := false, false
		for i := range n.lod {
			lg := n.lod[i].geometry
			if lg.Material == nil || lg.Material.EmissiveMap == nil {
				continue
			}
			anyMapped = true
			if lg.Material.EmissiveMap == t {
				continue
			}
			g := *lg
			g.Material = cloneMaterial(g.Material)
			g.Material.EmissiveMap = t
			n.lod[i].geometry = &g
			changed = true
		}
		if !anyMapped {
			return fmt.Errorf("nt3d: emissive replacement requires a color texture and emissive-mapped geometry")
		}
		if changed {
			n.geometry = n.lod[0].geometry
			s.changed(false, true)
		}
		return nil
	}
	if n.geometry.Material == nil || n.geometry.Material.EmissiveMap == nil {
		return fmt.Errorf("nt3d: emissive replacement requires a color texture and emissive-mapped geometry")
	}
	if n.geometry.Material.EmissiveMap == t {
		return nil
	}
	g := *n.geometry
	g.Material = cloneMaterial(g.Material)
	g.Material.EmissiveMap = t
	n.geometry = &g
	s.changed(false, true)
	return nil
}

// SetNormalMap replaces an existing normal map without uploading mesh buffers.
// Strength and sampling mode remain unchanged; use SetGeometry to change them.
func (s *Scene) SetNormalMap(name string, t *Texture) error {
	id, ok := s.Find(name)
	if !ok {
		return fmt.Errorf("nt3d: unknown node %q", name)
	}
	n := s.nodes[id]
	if !t.valid() || !t.data || n.geometry == nil {
		return fmt.Errorf("nt3d: normal replacement requires a normal map and normal-mapped geometry")
	}
	if len(n.lod) > 0 {
		anyMapped, changed := false, false
		for i := range n.lod {
			lg := n.lod[i].geometry
			if lg.Material == nil || lg.Material.NormalMap == nil {
				continue
			}
			anyMapped = true
			if lg.Material.NormalMap == t {
				continue
			}
			g := *lg
			g.Material = cloneMaterial(g.Material)
			g.Material.NormalMap = t
			n.lod[i].geometry = &g
			changed = true
		}
		if !anyMapped {
			return fmt.Errorf("nt3d: normal replacement requires a normal map and normal-mapped geometry")
		}
		if changed {
			n.geometry = n.lod[0].geometry
			s.changed(false, true)
		}
		return nil
	}
	if n.geometry.Material == nil || n.geometry.Material.NormalMap == nil {
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
	u1, v1, u2, v2 := b.normalUV.U-a.normalUV.U, b.normalUV.V-a.normalUV.V, c.normalUV.U-a.normalUV.U, c.normalUV.V-a.normalUV.V
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

// NewShaderNormalMap creates a linear-data procedural normal map with zero
// parameters. The shader has the same nt3dTexture entry point as a color shader,
// but returns encoded tangent-space XYZ in RGB; alpha is ignored.
func NewShaderNormalMap(source string, fallback image.Image) (*Texture, error) {
	return NewShaderNormalMapWithParameters(source, [4]float32{}, fallback)
}

// NewShaderNormalMapWithParameters generates normal data on the GPU and uses
// the supplied matching image in software. Filtering and mipmaps remain linear;
// no sRGB conversion is applied. Use SetNormalMap to advance immutable snapshots.
func NewShaderNormalMapWithParameters(source string, parameters [4]float32, fallback image.Image) (*Texture, error) {
	t, err := NewShaderTextureWithParameters(source, parameters, fallback)
	if err != nil {
		return nil, err
	}
	t.data = true
	for i := 3; i < len(t.pixels); i += 4 {
		t.pixels[i] = 255
	}
	return t, nil
}
