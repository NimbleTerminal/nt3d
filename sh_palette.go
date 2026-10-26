// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"nimbleterminal.dev/nt3d/sh"
	"slices"
)

// MaxSHPaletteCentroids is the most centroids an SHPalette may hold: a
// splat's label is 16 bits.
const MaxSHPaletteCentroids = 65536

// SHPalette is a set of spherical-harmonic band vectors that splats share:
// each splat names one by a 16-bit label (Geometry.GaussianSHLabels) instead
// of carrying its own. It holds the bands above DC; the DC coefficient stays
// per splat in Geometry.GaussianSH.
type SHPalette struct {
	Degree       int          // 1 to 3.
	Coefficients []nt3dsh.RGB // Centroid-major, nt3dsh.Count(Degree)-1 per centroid, in nt3dsh order.
}

// Stride is the number of coefficients a centroid holds, nt3dsh.Count(Degree)-1,
// or zero for a degree outside 1 to 3.
func (p *SHPalette) Stride() int {
	if p == nil || p.Degree < 1 {
		return 0
	}
	return max(0, nt3dsh.Count(p.Degree)-1)
}

// Count is the number of centroids, or zero for an invalid degree.
func (p *SHPalette) Count() int {
	s := p.Stride()
	if s == 0 {
		return 0
	}
	return len(p.Coefficients) / s
}

// ExpandSHPalette replaces g's palette and labels with per-splat coefficients:
// GaussianSH becomes nt3dsh.Count(Degree) entries a splat (the DC it had, then
// its centroid's bands), in a new slice. A geometry without a palette is
// unchanged. The palette itself is not altered, so other geometries may keep
// sharing it. It assumes g is valid.
func ExpandSHPalette(g *Geometry) {
	p := g.GaussianSHPalette
	if p == nil {
		return
	}
	stride := p.Stride()
	out := make([]nt3dsh.RGB, 0, len(g.GaussianSH)*(stride+1))
	for i, dc := range g.GaussianSH {
		l := int(g.GaussianSHLabels[i])
		out = append(out, dc)
		out = append(out, p.Coefficients[l*stride:(l+1)*stride]...)
	}
	g.GaussianSH, g.GaussianSHPalette, g.GaussianSHLabels = out, nil, nil
}

// gaussianSHTotal is the number of coefficients evaluated for a splat: the
// palette's DC plus bands, or what GaussianSH holds per point without one.
func gaussianSHTotal(g *Geometry) int {
	if p := g.GaussianSHPalette; p != nil {
		return nt3dsh.Count(p.Degree)
	}
	return gaussianSHCount(g)
}

// validateSHPalette checks a geometry's palette and labels together; it passes
// a geometry with neither. certified skips the scans a scene already made of
// this very palette and label slice (see storedGeometry).
func validateSHPalette(g Geometry, certified bool) error {
	p, labels := g.GaussianSHPalette, g.GaussianSHLabels
	if p == nil && labels == nil {
		return nil
	}
	if p == nil || labels == nil {
		return fmt.Errorf("nt3d: SH palette and labels go together")
	}
	count := p.Count()
	if g.PointStyle.Shape != GaussianPoint || count == 0 || count > MaxSHPaletteCentroids || len(p.Coefficients) != count*p.Stride() {
		return fmt.Errorf("nt3d: invalid Gaussian SH palette")
	}
	if len(labels) != len(g.Points) || len(g.GaussianSH) != len(g.Points) {
		return fmt.Errorf("nt3d: an SH palette needs one label and one DC coefficient per Gaussian")
	}
	if certified {
		return nil
	}
	for _, l := range labels {
		if int(l) >= count {
			return fmt.Errorf("nt3d: SH label outside the palette")
		}
	}
	for _, c := range p.Coefficients {
		if !validFloat(c[:]...) {
			return fmt.Errorf("nt3d: nonfinite Gaussian SH palette")
		}
	}
	return nil
}

// samePalette reports whether two palettes hold the same bands.
func samePalette(a, b *SHPalette) bool {
	return a == b || a.Degree == b.Degree && slices.Equal(a.Coefficients, b.Coefficients)
}
