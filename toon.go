// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"fmt"
	"image/color"
)

// ToonStyle quantizes a material's direct light into flat bands, for a cel
// (toon) look. Bands is the number of tones from shade to fully lit, 2 to 8;
// zero disables it. Each light's cosine snaps to the nearest of Bands even
// levels, and cast shadows step cleanly from lit to shadowed; a point light's
// distance falloff stays smooth. Softness, 0 to 1, widens the blend between
// N·L cel bands as a fraction of one band; at zero the edge is about a pixel.
// ShadowSoftness, 0 to 1, widens both the shadow-map filter footprint and the
// cast-shadow band transition across shadow penumbras without softening curved
// surfaces' N·L cel bands; at zero cast shadows keep a crisp cel edge.
//
// A toon surface takes flat ambient light, the midpoint of the sky and
// ground colors (and an environment's constant term), tinted by Shade: the
// color of the unlit band, so shadows can turn cool or warm rather than only
// darker. Shade's alpha is ignored, except that zero means white. Specular
// highlights become flat spots, and Rim, 0 to 1, adds a flat highlight where
// a lit surface turns away from the viewer.
//
// Unlit materials ignore it. It applies to indexed triangles, as Material
// does. Points and Gaussian splats carry no material and are not lit this
// way; PointStyle.ToonBands posterizes their colors into the same number of
// bands. Scene.ApplyToon changes both on geometry already in a scene.
type ToonStyle struct {
	Bands          int
	Softness       float32
	ShadowSoftness float32
	Shade          color.RGBA
	Rim            float32
}

func (t ToonStyle) on() bool { return t.Bands > 0 }

func (t ToonStyle) validate() error {
	if t.Bands != 0 && (t.Bands < 2 || t.Bands > 8) {
		return fmt.Errorf("nt3d: toon bands must be 0 or 2..8")
	}
	if !validFloat(t.Softness, t.ShadowSoftness, t.Rim) || t.Softness < 0 || t.Softness > 1 || t.ShadowSoftness < 0 || t.ShadowSoftness > 1 || t.Rim < 0 || t.Rim > 1 {
		return fmt.Errorf("nt3d: toon softness, shadow softness, and rim must be 0..1")
	}
	return nil
}

// shade is the tint of the unlit band, white when unset.
func (t ToonStyle) shade() [3]float32 {
	if t.Shade.A == 0 {
		return [3]float32{1, 1, 1}
	}
	return [3]float32{float32(t.Shade.R) / 255, float32(t.Shade.G) / 255, float32(t.Shade.B) / 255}
}

// toonStep is the smooth step from 0 below edge-w/2 to 1 above edge+w/2.
func toonStep(edge, w, x float32) float32 {
	t := min(1, max(0, (x-edge+w/2)/w))
	return t * t * (3 - 2*t)
}

// toonBand snaps x in 0..1 to the nearest below of bands even levels from 0
// to 1, stepping up at each k/bands across a blend w wide. The GPU's
// toonBand computes the same.
func toonBand(x float32, bands int, w float32) float32 {
	var sum float32
	for k := 1; k < bands; k++ {
		sum += toonStep(float32(k)/float32(bands), w, x)
	}
	return sum / float32(bands-1)
}

// toonEdge is the software renderer's blend width between bands: it has no
// screen derivatives, so a small constant stands in for the GPU's pixel.
func toonEdge(softness float32, bands int) float32 {
	return .01 + softness/float32(bands)
}

// toonShadowEdge is the software renderer's cast-shadow transition width
// around visibility 0.5: crisp (.01) when ShadowSoftness is zero, widening up
// to the full visibility range at 1.
func toonShadowEdge(shadowSoftness float32) float32 {
	return max(.01, shadowSoftness)
}

// toonRim is where the rim highlight starts, as one minus the cosine between
// the normal and the view, and toonSpot where a specular lobe becomes a spot.
const (
	toonRim  = .7
	toonSpot = .5
)

// ApplyToon changes the toon style of what is now in root's subtree. Indexed
// meshes take the whole style, and those without a material are given one.
// Points and Gaussian splats, which have no light to band, take its Bands as
// PointStyle.ToonBands, and their colors are posterized. The zero style
// turns both off. It preserves animation, deformation, shared mesh buffers,
// level-of-detail clusters, and old frames, and each node's other material
// settings; a material several nodes hold is copied, never altered. Use Root
// to change the whole scene. Newly added nodes keep their own style.
func (s *Scene) ApplyToon(root NodeID, style ToonStyle) error {
	if err := style.validate(); err != nil {
		return err
	}
	if s.nodes[root] == nil {
		return fmt.Errorf("nt3d: unknown toon root %d", root)
	}
	// restyle is old with the style, or nil when it has it already.
	restyle := func(old *sceneGeometry) *sceneGeometry {
		if old == nil {
			return nil
		}
		mesh := len(old.Indices) > 0
		if m := old.Material; m != nil && m.Toon == style || m == nil && style == (ToonStyle{}) {
			mesh = false
		}
		points := len(old.Points) > 0 && old.PointStyle.ToonBands != style.Bands
		if !mesh && !points {
			return nil
		}
		g := *old
		if mesh {
			if g.Material = cloneMaterial(g.Material); g.Material == nil {
				g.Material = &Material{}
			}
			g.Material.Toon = style
		}
		if points {
			g.PointStyle.ToonBands = style.Bands
			if g.stored != nil {
				st := *g.stored
				st.style = g.PointStyle
				g.stored = &st
			}
			if g.pointUpdate != nil {
				pu := *g.pointUpdate
				pu.style = g.PointStyle
				g.pointUpdate = &pu
			}
		}
		return &g
	}
	changed := false
	pending := []NodeID{root}
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		n := s.nodes[id]
		pending = append(pending, n.Children...)
		if len(n.lod) > 0 {
			for i := range n.lod {
				if g := restyle(n.lod[i].geometry); g != nil {
					n.lod[i].geometry = g
					changed = true
				}
			}
			n.geometry = n.lod[0].geometry
			continue
		}
		if g := restyle(n.geometry); g != nil {
			n.geometry = g
			changed = true
		}
	}
	if changed {
		s.changed(false, false)
	}
	return nil
}

// toonParams is the style a material's lit triangles are shaded with: zero
// unless it is toon and lit.
func toonParams(m *Material) ToonStyle {
	if m == nil || m.unlit() || !m.Toon.on() {
		return ToonStyle{}
	}
	return m.Toon
}
