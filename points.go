// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"math"

	"nimbleterminal.dev/nt3d/math"
)

// PointStyle is how a geometry's points are drawn. The zero value is hard
// discs, radius in pixels, opaque.
type PointStyle struct {
	// SoftDistance fades blended particles as their view-space depth approaches
	// the scene surface, in world units. Zero disables intersection fading.
	// Includes transmissive surfaces; ignored for opaque points and wireframe.
	SoftDistance float32

	// Texture draws square camera-facing sprites, tinted by Point.Color.
	// It replaces the disc shape. Opaque sprites cut out alpha below 0.5;
	// blended sprites multiply texture alpha by Point.Color.A.
	// Only image color textures are supported. Filtering stays within each cell.
	Texture *Texture
	// Columns and Rows divide an atlas into equal cells; zero means one.
	Columns, Rows uint32
	// Frame is added to each Point.Frame, wrapping through row-major cells.
	Frame uint32
	// SHDCOnly disables directional coefficients without discarding them.
	SHDCOnly bool
	// Precise keeps a Gaussian geometry's float GPU records: 48-byte axes and
	// float spherical harmonics, as the software renderer draws them. Without
	// it, the default, the GPU stores half-float axes and byte coefficients at
	// SPZ's precision, so a geometry holds about four times the splats at
	// degree 3; see MaxGaussians. Compact axis components are half floats,
	// from about 6e-5 to 65504 local units; one beyond clamps and one below
	// about 3e-8 becomes zero. The DC coefficient saturates beyond about ±3.33
	// and the bands clamp to ±1, as in SPZ. Set Precise for scenes in unusual
	// units or with extreme colors.
	Precise bool
	Shape   PointShape
	Units   PointUnits
	Blend   PointBlend
}

// PointShape is the shape of a point's disc.
type PointShape uint8

const (
	HardPoint     PointShape = iota // A disc with a hard edge.
	SoftPoint                       // A disc whose alpha fades to nothing at its edge; drawn hard when opaque.
	GaussianPoint                   // An anisotropic Gaussian, truncated at three sigma; requires AlphaBlend and WorldUnits.
)

// PointUnits is what a point's Radius measures.
type PointUnits uint8

const (
	// PixelUnits is a radius of 1 to 32 pixels, the same at any distance.
	PixelUnits PointUnits = iota
	// WorldUnits is any positive radius in world units, smaller farther
	// away. Node transforms move points but do not scale their radii.
	WorldUnits
)

// PointBlend is how a point's color combines with what is behind it.
// Blended points are hidden by nearer opaque surfaces but hide nothing.
type PointBlend uint8

const (
	OpaqueBlend   PointBlend = iota // Opaque; color alpha is ignored.
	AlphaBlend                      // Over what is behind, by color alpha, farthest first, sorted together with every other alpha point geometry.
	AdditiveBlend                   // Added to what is behind, times color alpha: brighter where points overlap.
)

// MaxPointPixels is the largest radius on screen, in pixels, of a point in
// world units, so a particle at the camera does not fill the frame. The
// smallest is half a pixel, so far particles thin out rather than vanish.
const MaxPointPixels = 256

func validPointStyle(s PointStyle) error {
	if !validFloat(s.SoftDistance) || s.SoftDistance < 0 {
		return fmt.Errorf("nt3d: soft particle distance must be finite and nonnegative")
	}

	if s.Shape > GaussianPoint || s.Units > WorldUnits || s.Blend > AdditiveBlend {
		return fmt.Errorf("nt3d: unknown point style")
	}
	if s.Shape == GaussianPoint && (s.Blend != AlphaBlend || s.Units != WorldUnits || s.Texture != nil) {
		return fmt.Errorf("nt3d: Gaussians require world units, alpha blending, and no texture")
	}
	if s.Texture != nil {
		t := s.Texture
		if !t.valid() || t.data || t.shader != "" || max(1, s.Columns) > uint32(t.width) || max(1, s.Rows) > uint32(t.height) || uint32(t.width)%max(1, s.Columns) != 0 || uint32(t.height)%max(1, s.Rows) != 0 {
			return fmt.Errorf("nt3d: sprite requires an image texture and an evenly divided atlas")
		}
	}
	return nil
}

// validPointRadius reports whether a point's radius suits its units.
func validPointRadius(units PointUnits, r float32) bool {
	if !validFloat(r) {
		return false
	}
	if units == WorldUnits {
		return r > 0
	}
	return r >= 1 && r <= 32
}

// blendedPoints reports whether a geometry's points blend, drawn after
// everything opaque.
func blendedPoints(g *Geometry) bool { return len(g.Points) > 0 && g.PointStyle.Blend != OpaqueBlend }

// pointRadius is a point's radius on screen, in render pixels, with the
// point at clip w, in a frame drawn at scale times its pixel sizes (a
// supersampled software frame's factor; 1 elsewhere). A pixel radius is
// scaled. A world radius becomes pixels by the projection's vertical scale
// at that depth, the length of the matrix's clip-y row (which a camera's up
// axis spans) over w, clamped to half a pixel and MaxPointPixels, scaled.
func (f *Frame) pointRadius(style PointStyle, radius, w, scale float32) float32 {
	if style.Units != WorldUnits {
		return radius * scale
	}
	m := &f.Matrix
	k := float32(math.Sqrt(float64(m[1]*m[1] + m[5]*m[5] + m[9]*m[9])))
	return max(.5*scale, min(MaxPointPixels*scale, radius*k*float32(f.Height)*.5/w))
}

// softness is how much of a point's alpha reaches a pixel at r2, its
// squared distance from the center over the squared radius: all of a hard
// point's; a soft point's falling as (1-r²)² to nothing at its edge;
// a Gaussian's exp(-4.5*r²), with radius measuring three sigma.
func softness(shape PointShape, r2 float32) float32 {
	if shape == GaussianPoint {
		return float32(math.Exp(float64(-4.5 * r2)))
	}
	if shape == HardPoint {
		return 1
	}
	s := max(0, 1-r2)
	return s * s
}

// pointSorter orders points back to front, reusing its buffers.
type pointSorter struct{ keys, order, spareKeys, spareOrder []uint32 }

// buffers sizes the sorter's buffers for n keys.
func (s *pointSorter) buffers(n int) {
	grow := func(b []uint32) []uint32 {
		if cap(b) < n {
			return make([]uint32, n)
		}
		return b[:n]
	}
	s.keys, s.order, s.spareKeys, s.spareOrder = grow(s.keys), grow(s.order), grow(s.spareKeys), grow(s.spareOrder)
}

// sort returns the indices of points, farthest from the viewer first by
// clip z under m, which grows away from the viewer in both projections;
// equal depths keep their order. The slice is the sorter's until its next
// sort.
func (s *pointSorter) sort(points []Point, m *nt3dmath.Mat4) []uint32 {
	s.buffers(len(points))
	for i, p := range points {
		z := m[2]*p.Position.X + m[6]*p.Position.Y + m[10]*p.Position.Z + m[14]
		// Float bits ordered as unsigned integers, then reversed: farthest first.
		b := math.Float32bits(z)
		if b&(1<<31) != 0 {
			b = ^b
		} else {
			b |= 1 << 31
		}
		s.keys[i] = ^b
	}
	return s.radix()
}

// sortKeys returns the indices of keys in ascending order of key; equal
// keys keep their order. The slice is the sorter's until its next sort; keys
// are left as they were.
func (s *pointSorter) sortKeys(keys []uint32) []uint32 {
	s.buffers(len(keys))
	copy(s.keys, keys)
	return s.radix()
}

// radix sorts s.keys, filled, by a stable radix sort, a byte a pass, so
// linear, returning the permutation.
func (s *pointSorter) radix() []uint32 {
	for i := range s.order {
		s.order[i] = uint32(i)
	}
	keys, order, spareKeys, spareOrder := s.keys, s.order, s.spareKeys, s.spareOrder
	for shift := 0; shift < 32; shift += 8 {
		var start [257]int
		for _, k := range keys {
			start[(k>>shift)&255+1]++
		}
		for i := 1; i < len(start); i++ {
			start[i] += start[i-1]
		}
		for i, k := range keys {
			b := (k >> shift) & 255
			spareKeys[start[b]], spareOrder[start[b]] = k, order[i]
			start[b]++
		}
		keys, spareKeys = spareKeys, keys
		order, spareOrder = spareOrder, order
	}
	return order // Four passes: back in s.order.
}

// spriteRect returns normalized cell offset and size, with overflow-safe wrap.
func (s PointStyle) spriteRect(p Point) [4]float32 {
	c, r := max(1, s.Columns), max(1, s.Rows)
	frame := uint32((uint64(s.Frame) + uint64(p.Frame)) % (uint64(c) * uint64(r)))
	return [4]float32{float32(frame%c) / float32(c), float32(frame/c) / float32(r), 1 / float32(c), 1 / float32(r)}
}
func (s PointStyle) spriteSample(p Point, u, v float32) [4]float32 {
	rect := s.spriteRect(p)
	dx, dy := .5/float32(s.Texture.width), .5/float32(s.Texture.height)
	return s.Texture.sampleLinear(UV{max(rect[0]+dx, min(rect[0]+rect[2]-dx, rect[0]+u*rect[2])), max(rect[1]+dy, min(rect[1]+rect[3]-dy, rect[1]+v*rect[3]))}, false, 0, false)
}
