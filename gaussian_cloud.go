// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"fmt"
	"image"
	"image/color"
	"slices"

	"nimbleterminal.dev/nt3d/clod"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/sh"
)

// GaussianPatchID identifies a batch of runtime-inserted Gaussians in a
// GaussianCloud.
type GaussianPatchID uint64

type gaussianPatch struct {
	id     GaussianPatchID
	points []Point
}

// GaussianCloud manages runtime insertion and removal of 3D Gaussian splats
// inside a single Gaussian Geometry so inserted patches share one depth-sort
// slot with the surrounding cloud and sort cleanly back-to-front per splat.
//
// If the base cloud uses spherical harmonics (per-point or SHPalette), newly
// inserted points automatically synthesize view-independent DC coefficients
// from Point.Color and bind to a zero higher-order band entry without mutating
// the caller's palette.
type GaussianCloud struct {
	base         Geometry
	baseClusters *nt3dclod.Set
	shStride     int // RGB entries per point in GaussianSH (0 when color-only)
	zeroLabel    uint16
	nextID       GaussianPatchID
	patches      []gaussianPatch
	dirty        bool
	built        Geometry
}

const shY00 = 0.28209479177387814

// NewGaussianCloud wraps base for runtime Gaussian insertion and removal.
// base may have zero points as long as its PointStyle is configured for
// GaussianPoint (WorldUnits, AlphaBlend, no texture).
func NewGaussianCloud(base Geometry) (*GaussianCloud, error) {
	if base.PointStyle.Shape != GaussianPoint {
		return nil, fmt.Errorf("nt3d: GaussianCloud requires PointStyle.Shape == GaussianPoint")
	}
	if err := validPointStyle(base.PointStyle); err != nil {
		return nil, err
	}
	if len(base.Points) > 0 {
		if err := base.Validate(); err != nil {
			return nil, err
		}
	}
	g := base.Clone()
	c := &GaussianCloud{
		baseClusters: g.Clusters,
		nextID:       1,
		dirty:        true,
	}
	if g.GaussianSHPalette != nil {
		label, pal, ok := ensureZeroSHCentroid(g.GaussianSHPalette)
		if ok {
			g.GaussianSHPalette = pal
			c.zeroLabel = label
			c.shStride = 1
		} else {
			ExpandSHPalette(&g)
		}
	}
	if g.GaussianSHPalette == nil && len(g.GaussianSH) > 0 && len(g.Points) > 0 {
		c.shStride = len(g.GaussianSH) / len(g.Points)
	}
	c.base = g
	return c, nil
}

func ensureZeroSHCentroid(p *SHPalette) (uint16, *SHPalette, bool) {
	stride := p.Stride()
	count := p.Count()
	if stride <= 0 || count <= 0 {
		return 0, nil, false
	}
	for i := range count {
		allZero := true
		for _, rgb := range p.Coefficients[i*stride : (i+1)*stride] {
			if rgb != (nt3dsh.RGB{}) {
				allZero = false
				break
			}
		}
		if allZero {
			return uint16(i), p, true
		}
	}
	if count >= 65536 {
		return 0, nil, false
	}
	coeffs := make([]nt3dsh.RGB, len(p.Coefficients)+stride)
	copy(coeffs, p.Coefficients)
	return uint16(count), &SHPalette{Degree: p.Degree, Coefficients: coeffs}, true
}

// Insert validates and adds points as a removable patch in the cloud. No
// points is an empty patch, which adds nothing and is removed as any other.
func (c *GaussianCloud) Insert(points []Point) (GaussianPatchID, error) {
	total := c.Count() + len(points)
	limit := c.maxPoints()
	if total > limit {
		return 0, fmt.Errorf("nt3d: GaussianCloud exceeds point limit (%d > %d)", total, limit)
	}
	for i, p := range points {
		if !finiteVec(p.Position) || !validPointRadius(WorldUnits, p.Radius) || !validGaussian(c.base.PointStyle, p) {
			return 0, fmt.Errorf("nt3d: invalid Gaussian at patch index %d", i)
		}
	}
	id := c.nextID
	c.nextID++
	c.patches = append(c.patches, gaussianPatch{
		id:     id,
		points: slices.Clone(points),
	})
	c.dirty = true
	return id, nil
}

// Remove deletes a previously inserted patch, reporting whether id was present.
func (c *GaussianCloud) Remove(id GaussianPatchID) bool {
	for i := range c.patches {
		if c.patches[i].id == id {
			c.patches = slices.Delete(c.patches, i, i+1)
			c.dirty = true
			return true
		}
	}
	return false
}

// ClearPatches removes every runtime-inserted patch while keeping the base cloud.
func (c *GaussianCloud) ClearPatches() {
	if len(c.patches) == 0 {
		return
	}
	c.patches = nil
	c.dirty = true
}

// Count returns the total number of Gaussians (base plus inserted patches).
func (c *GaussianCloud) Count() int {
	n := len(c.base.Points)
	for _, p := range c.patches {
		n += len(p.points)
	}
	return n
}

func (c *GaussianCloud) maxPoints() int {
	coeffs := c.shStride
	if c.base.GaussianSHPalette != nil {
		coeffs = 1
	}
	if c.base.PointStyle.Precise {
		return MaxPreciseGaussians(coeffs)
	}
	return MaxGaussians(coeffs)
}

// Geometry returns a Geometry snapshot containing the base Gaussians and all
// currently inserted patches.
func (c *GaussianCloud) Geometry() Geometry {
	if !c.dirty {
		return c.built
	}
	if len(c.patches) == 0 {
		g := c.base.Clone()
		g.Clusters = c.baseClusters
		g.Bounds = GeometryBounds(g)
		c.built = g
		c.dirty = false
		return g
	}
	total := c.Count()
	g := c.base.Clone()
	g.Clusters = nil
	pts := make([]Point, 0, total)
	pts = append(pts, c.base.Points...)
	var shOut []nt3dsh.RGB
	if c.shStride > 0 {
		shOut = make([]nt3dsh.RGB, 0, total*c.shStride)
		shOut = append(shOut, c.base.GaussianSH...)
	}
	var labelsOut []uint16
	if g.GaussianSHPalette != nil {
		labelsOut = make([]uint16, 0, total)
		labelsOut = append(labelsOut, c.base.GaussianSHLabels...)
	}
	for _, patch := range c.patches {
		pts = append(pts, patch.points...)
		if c.shStride > 0 {
			for _, p := range patch.points {
				dc := nt3dsh.RGB{
					float32((float64(p.Color.R)/255.0 - 0.5) / shY00),
					float32((float64(p.Color.G)/255.0 - 0.5) / shY00),
					float32((float64(p.Color.B)/255.0 - 0.5) / shY00),
				}
				shOut = append(shOut, dc)
				for k := 1; k < c.shStride; k++ {
					shOut = append(shOut, nt3dsh.RGB{})
				}
			}
		}
		if g.GaussianSHPalette != nil {
			for range patch.points {
				labelsOut = append(labelsOut, c.zeroLabel)
			}
		}
	}
	g.Points = pts
	g.GaussianSH = shOut
	g.GaussianSHLabels = labelsOut
	g.Bounds = GeometryBounds(g)
	c.built = g
	c.dirty = false
	return g
}

// FlatGaussians describes a planar grid of thin, surface-aligned Gaussian
// splats sampled from Image, suitable for inserting surface marks, graffiti,
// or decals directly into a 3DGS scene.
type FlatGaussians struct {
	Image        image.Image
	Center       nt3dmath.Vec3
	Normal       nt3dmath.Vec3
	Up           nt3dmath.Vec3
	Width        float32
	Height       float32
	NormalOffset float32
	// Columns and Rows cap the sampling grid resolution; zero defaults to the
	// image dimensions clamped to 64×64.
	Columns, Rows int
	// AlphaCutoff skips texels with alpha below this byte threshold (default 16).
	AlphaCutoff uint8
}

// BuildFlatGaussians converts non-transparent texels of f.Image into flat
// anisotropic Gaussian Points aligned to the plane at f.Center lifted by
// f.NormalOffset along the normal. An image with no texel at or above the
// alpha cutoff is not an error: it yields no points, and GaussianCloud.Insert
// takes them as an empty patch.
func BuildFlatGaussians(f FlatGaussians) ([]Point, error) {
	if f.Image == nil {
		return nil, fmt.Errorf("nt3d: FlatGaussians requires an image")
	}
	b := f.Image.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, fmt.Errorf("nt3d: FlatGaussians image must be non-empty")
	}
	frame, err := newSurfaceFrame("FlatGaussians", f.Center, f.Normal, f.Up, f.NormalOffset)
	if err != nil {
		return nil, err
	}
	if !positive(f.Width, f.Height) {
		return nil, fmt.Errorf("nt3d: FlatGaussians dimensions must be positive and finite")
	}
	n, right, up := frame.normal, frame.right, frame.up

	cols, rows := f.Columns, f.Rows
	if cols == 0 {
		cols = min(b.Dx(), 64)
	}
	if rows == 0 {
		rows = min(b.Dy(), 64)
	}
	if cols < 1 || cols > 512 || rows < 1 || rows > 512 {
		return nil, fmt.Errorf("nt3d: FlatGaussians grid must be 1..512 per side")
	}
	cutoff := f.AlphaCutoff
	if cutoff == 0 {
		cutoff = 16
	}

	cellW := f.Width / float32(cols)
	cellH := f.Height / float32(rows)
	// Overlap neighboring Gaussians slightly (~0.68 sigma per cell) for solid
	// strokes without gaps, with a thin 2 mm normal sigma (>= half-float 6e-5).
	sigmaX := max(cellW*0.68, 0.001)
	sigmaY := max(cellH*0.68, 0.001)
	sigmaZ := float32(0.002)
	axes := [3]nt3dmath.Vec3{
		right.Scale(sigmaX),
		up.Scale(sigmaY),
		n.Scale(sigmaZ),
	}
	radius := 3 * max(sigmaX, sigmaY, sigmaZ)
	origin := f.Center.Add(n.Scale(f.NormalOffset))

	var pts []Point
	for ry := range rows {
		vFrac := (float64(ry) + 0.5) / float64(rows)
		srcY := b.Min.Y + min(b.Dy()-1, int(vFrac*float64(b.Dy())))
		yOff := float32(0.5-vFrac) * f.Height
		for cx := range cols {
			uFrac := (float64(cx) + 0.5) / float64(cols)
			srcX := b.Min.X + min(b.Dx()-1, int(uFrac*float64(b.Dx())))
			nrgba := color.NRGBAModel.Convert(f.Image.At(srcX, srcY)).(color.NRGBA)
			if nrgba.A < cutoff {
				continue
			}
			xOff := float32(uFrac-0.5) * f.Width
			pos := origin.Add(right.Scale(xOff)).Add(up.Scale(yOff))
			pts = append(pts, Point{
				Position:     pos,
				Radius:       radius,
				Color:        color.RGBA(nrgba),
				GaussianAxes: axes,
			})
		}
	}
	return pts, nil
}
