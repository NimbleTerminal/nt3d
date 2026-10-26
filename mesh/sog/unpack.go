// Copyright (c) 2026 Neomantra Corp.

package nt3dsog

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"math"

	_ "golang.org/x/image/webp" // SOG images are lossless WebP.
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
	"nimbleterminal.dev/nt3d/sh"
)

// images are a SOG's decoded images. The per-splat images (all but
// centroids) share one width and height and hold splat i at pixel
// (i % width, i / width).
type images struct {
	width, height                      int
	meansL, meansU, scales, quats, sh0 *image.NRGBA
	centroids, labels                  *image.NRGBA // nil without shN
}

// coefficients is the number of SH coefficients per channel beyond DC for
// each band count.
var coefficients = [4]int{0, 3, 8, 15}

// v1Bands maps a v1 centroid image's width to its band count.
var v1Bands = map[int]int{64 * 3: 1, 64 * 8: 2, 64 * 15: 3}

// loadImages reads and decodes the images m names through open, which charges
// each file's length to b; each image's width × height × 4 is charged here.
// Every size is checked before any pixels are decoded. For a v1 shN, it sets
// m.shN.bands from the centroid image's width, and its count, when meta.json
// gave none, to every slot the centroid image holds.
func loadImages(m *meta, open func(name string) ([]byte, error), b *budget) (*images, error) {
	names := []string{m.means.low, m.means.high, m.scalesFile, m.quats, m.sh0File}
	if m.shN != nil {
		names = append(names, m.shN.centroids, m.shN.labels)
	}
	files := make([][]byte, len(names))
	configs := make([]image.Config, len(names))
	for i, name := range names {
		data, err := open(name)
		if err != nil {
			return nil, metaErr("%s: %w", name, err)
		}
		c, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, metaErr("%s: %w", name, err)
		}
		if c.Width < 1 || c.Height < 1 {
			return nil, metaErr("%s is %d×%d", name, c.Width, c.Height)
		}
		if err := b.take(int64(c.Width)*int64(c.Height)*4, name+" decoded"); err != nil {
			return nil, metaErr("%w", err)
		}
		files[i], configs[i] = data, c
	}

	// Sizes: every image but the centroids (index 5) matches the first.
	width, height := configs[0].Width, configs[0].Height
	for i, c := range configs {
		if i != 5 && (c.Width != width || c.Height != height) {
			return nil, metaErr("%s is %d×%d, not %d×%d", names[i], c.Width, c.Height, width, height)
		}
	}
	if m.count > width*height {
		return nil, metaErr("%d splats do not fit %d×%d images", m.count, width, height)
	}
	if m.shN != nil {
		c := configs[5]
		if m.shN.bands == 0 {
			if m.shN.bands = v1Bands[c.Width]; m.shN.bands == 0 {
				return nil, metaErr("%s width %d is not 192, 512, or 960", names[5], c.Width)
			}
		}
		if c.Width != 64*coefficients[m.shN.bands] {
			return nil, metaErr("%s width %d is not %d for %d bands", names[5], c.Width, 64*coefficients[m.shN.bands], m.shN.bands)
		}
		if m.shN.count == 0 {
			m.shN.count = 64 * c.Height // No count in meta.json: every slot is valid.
		}
		if c.Height*64 < m.shN.count {
			return nil, metaErr("%s height %d holds fewer than %d centroids", names[5], c.Height, m.shN.count)
		}
	}

	decoded := make([]*image.NRGBA, len(names))
	for i, data := range files {
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, metaErr("%s: %w", names[i], err)
		}
		if b := img.Bounds(); b.Dx() != configs[i].Width || b.Dy() != configs[i].Height {
			return nil, metaErr("%s decodes to %d×%d, not the %d×%d its header says", names[i], b.Dx(), b.Dy(), configs[i].Width, configs[i].Height)
		}
		n, ok := img.(*image.NRGBA)
		if !ok || n.Rect.Min != (image.Point{}) {
			n = image.NewNRGBA(image.Rect(0, 0, configs[i].Width, configs[i].Height))
			draw.Draw(n, n.Rect, img, img.Bounds().Min, draw.Src)
		}
		decoded[i] = n
	}
	im := &images{width: width, height: height, meansL: decoded[0], meansU: decoded[1], scales: decoded[2], quats: decoded[3], sh0: decoded[4]}
	if m.shN != nil {
		im.centroids, im.labels = decoded[5], decoded[6]
	}
	return im, nil
}

// pixel is the raw R, G, B, A bytes of img at (x, y).
func pixel(img *image.NRGBA, x, y int) []uint8 {
	o := img.PixOffset(x, y)
	return img.Pix[o : o+4 : o+4]
}

// unpack turns the images into m.count Gaussian splats by m's tables. With a
// shN, mode SHPaletteExpand gives each splat its coefficients; otherwise
// the geometry keeps the file's palette: GaussianSH holds each splat's DC,
// and GaussianSHPalette and GaussianSHLabels the rest.
func unpack(m *meta, im *images, mode nt3dmesh.SHPaletteMode) (nt3d.Geometry, error) {
	g := nt3d.Geometry{
		PointStyle: nt3d.PointStyle{Shape: nt3d.GaussianPoint, Units: nt3d.WorldUnits, Blend: nt3d.AlphaBlend},
		Points:     make([]nt3d.Point, m.count),
	}
	coeffs := 0
	var rest []float64
	if m.shN != nil {
		coeffs = coefficients[m.shN.bands]
		rest = make([]float64, 3*coeffs)
	}
	palette := m.shN != nil && mode != nt3dmesh.SHPaletteExpand
	if palette {
		g.GaussianSH = make([]nt3dsh.RGB, 0, m.count)
		g.GaussianSHLabels = make([]uint16, m.count)
		// Dequantize each centroid once, in nt3dsh order: the image holds
		// coeffs pixels a centroid, one coefficient each, in R, G, B. Labels
		// are 16 bits, so centroids past 65536 are unreachable.
		n := min(m.shN.count, nt3d.MaxSHPaletteCentroids)
		g.GaussianSHPalette = &nt3d.SHPalette{Degree: nt3dsh.Degree(1 + coeffs), Coefficients: make([]nt3dsh.RGB, n*coeffs)}
		for c := range n {
			for k := range coeffs {
				v := pixel(im.centroids, (c%64)*coeffs+k, c/64)
				g.GaussianSHPalette.Coefficients[c*coeffs+k] = nt3dsh.RGB{float32(m.shN.values.value(0, v[0])), float32(m.shN.values.value(1, v[1])), float32(m.shN.values.value(2, v[2]))}
			}
		}
	} else {
		g.GaussianSH = make([]nt3dsh.RGB, 0, (1+coeffs)*m.count)
	}
	zero := false // Some label is at or past the palette: it gets a centroid of zeros.
	for i := range m.count {
		x, y := i%im.width, i/im.width

		// Position: 16 bits per axis over the log-domain range.
		l, u := pixel(im.meansL, x, y), pixel(im.meansU, x, y)
		var p [3]float64
		for k := range 3 {
			q := float64(uint16(u[k])<<8 | uint16(l[k]))
			n := m.means.mins[k] + (m.means.maxs[k]-m.means.mins[k])*q/65535
			p[k] = math.Copysign(math.Expm1(math.Abs(n)), n)
		}

		s := pixel(im.scales, x, y)
		logScales := [3]float64{m.scales.value(0, s[0]), m.scales.value(1, s[1]), m.scales.value(2, s[2])}

		// Rotation: the three smallest components and which was dropped.
		r := pixel(im.quats, x, y)
		rotation := [4]float64{1, 0, 0, 0}
		if r[3] >= 252 {
			a := (float64(r[0])/255 - .5) * math.Sqrt2
			b := (float64(r[1])/255 - .5) * math.Sqrt2
			c := (float64(r[2])/255 - .5) * math.Sqrt2
			d := math.Sqrt(max(0, 1-(a*a+b*b+c*c)))
			switch r[3] - 252 {
			case 0:
				rotation = [4]float64{d, a, b, c}
			case 1:
				rotation = [4]float64{a, d, b, c}
			case 2:
				rotation = [4]float64{a, b, d, c}
			case 3:
				rotation = [4]float64{a, b, c, d}
			}
		}

		c := pixel(im.sh0, x, y)
		dc := [3]float64{m.sh0.value(0, c[0]), m.sh0.value(1, c[1]), m.sh0.value(2, c[2])}
		opacity := float64(c[3]) / 255
		if m.version == 1 {
			opacity = 1 / (1 + math.Exp(-m.sh0.value(3, c[3])))
		}

		point, err := nt3dmesh.GaussianPoint(nt3dmath.Vec3{X: float32(p[0]), Y: float32(p[1]), Z: float32(p[2])}, rotation, logScales, dc, opacity)
		if err != nil {
			return nt3d.Geometry{}, fmt.Errorf("nt3dmesh: SOG splat %d: %w", i, err)
		}
		g.Points[i] = point

		// Higher-order SH: a palette entry, channel-major.
		if palette {
			lp := pixel(im.labels, x, y)
			label := int(lp[0]) | int(lp[1])<<8
			if label >= m.shN.count {
				label, zero = len(g.GaussianSHPalette.Coefficients)/coeffs, true
			}
			g.GaussianSHLabels[i] = uint16(label)
			g.GaussianSH = append(g.GaussianSH, nt3dsh.RGB{float32(dc[0]), float32(dc[1]), float32(dc[2])})
			continue
		}
		if m.shN != nil {
			clear(rest)
			lp := pixel(im.labels, x, y)
			if label := int(lp[0]) | int(lp[1])<<8; label < m.shN.count {
				cx, cy := (label%64)*coeffs, label/64
				for k := range coeffs {
					v := pixel(im.centroids, cx+k, cy)
					rest[k] = m.shN.values.value(0, v[0])
					rest[k+coeffs] = m.shN.values.value(1, v[1])
					rest[k+2*coeffs] = m.shN.values.value(2, v[2])
				}
			}
		}
		sh, err := nt3dmesh.GaussianSH(dc, rest)
		if err != nil {
			return nt3d.Geometry{}, fmt.Errorf("nt3dmesh: SOG splat %d: %w", i, err)
		}
		g.GaussianSH = append(g.GaussianSH, sh...)
	}
	if zero {
		p := g.GaussianSHPalette
		p.Coefficients = append(p.Coefficients, make([]nt3dsh.RGB, coeffs)...)
	}
	g.Bounds = nt3d.GeometryBounds(g)
	return g, nil
}
