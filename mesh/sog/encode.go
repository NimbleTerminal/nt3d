// Copyright (c) 2026 Neomantra Corp.

package nt3dsog

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"slices"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
)

// uniform is a v2 codebook of 256 evenly spaced entries over [lo, hi]: the
// linear range SOG v1 wrote, in v2's form.
type uniform struct{ lo, hi float64 }

// rangeOf is the uniform codebook spanning values; none gives the zero value.
func rangeOf(values []float64) uniform {
	if len(values) == 0 {
		return uniform{}
	}
	u := uniform{values[0], values[0]}
	for _, v := range values[1:] {
		u.lo, u.hi = min(u.lo, v), max(u.hi, v)
	}
	return u
}

// fraction is where v lies in [lo, hi], clamped to [0, 1]; 0 for an empty range.
func fraction(v, lo, hi float64) float64 {
	if !(hi > lo) {
		return 0
	}
	return max(0, min(1, (v-lo)/(hi-lo)))
}

// index is the byte whose entry is nearest v.
func (u uniform) index(v float64) uint8 { return uint8(math.Round(255 * fraction(v, u.lo, u.hi))) }

// entries is the codebook: lo + (hi-lo)*i/255 for each byte i.
func (u uniform) entries() []float64 {
	e := make([]float64, 256)
	for i := range e {
		e[i] = u.lo + (u.hi-u.lo)*float64(i)/255
	}
	return e
}

// quantize16 is index at 16 bits over [lo, hi], as the means are stored.
func quantize16(v, lo, hi float64) uint16 { return uint16(math.Round(65535 * fraction(v, lo, hi))) }

// splat inverts nt3dmesh.GaussianPoint's axes: the wxyz quaternion whose
// largest component is positive, and the logarithms of the three scales
// (clamped to the [1e-20, 1e6] GaussianPoint accepts). The axes are the rows
// GaussianPoint builds, the transpose of the usual rotation matrix, read
// through the largest quaternion component so a rotation near 180° is as
// exact as one near 0.
func splat(axes [3]nt3dmath.Vec3) (q [4]float64, logScales [3]float64) {
	var a [3][3]float64
	for k, v := range axes {
		s := max(1e-20, min(1e6, math.Sqrt(float64(v.Dot(v)))))
		logScales[k] = math.Log(s)
		a[k] = [3]float64{float64(v.X) / s, float64(v.Y) / s, float64(v.Z) / s}
	}
	tr := a[0][0] + a[1][1] + a[2][2]
	squares := [4]float64{(1 + tr) / 4, (1 + 2*a[0][0] - tr) / 4, (1 + 2*a[1][1] - tr) / 4, (1 + 2*a[2][2] - tr) / 4}
	largest := 0
	for k, s := range squares {
		if s > squares[largest] {
			largest = k
		}
	}
	c := math.Sqrt(max(0, squares[largest]))
	if c == 0 {
		return [4]float64{1, 0, 0, 0}, logScales
	}
	switch largest {
	case 0:
		q = [4]float64{c, (a[1][2] - a[2][1]) / (4 * c), (a[2][0] - a[0][2]) / (4 * c), (a[0][1] - a[1][0]) / (4 * c)}
	case 1:
		q = [4]float64{(a[1][2] - a[2][1]) / (4 * c), c, (a[0][1] + a[1][0]) / (4 * c), (a[0][2] + a[2][0]) / (4 * c)}
	case 2:
		q = [4]float64{(a[2][0] - a[0][2]) / (4 * c), (a[0][1] + a[1][0]) / (4 * c), c, (a[1][2] + a[2][1]) / (4 * c)}
	default:
		q = [4]float64{(a[0][1] - a[1][0]) / (4 * c), (a[0][2] + a[2][0]) / (4 * c), (a[1][2] + a[2][1]) / (4 * c), c}
	}
	n := math.Sqrt(q[0]*q[0] + q[1]*q[1] + q[2]*q[2] + q[3]*q[3])
	for k := range q {
		q[k] /= n
	}
	return q, logScales
}

// packQuat is SOG's smallest-three packing of a unit quaternion: the three
// components other than the largest (the first of equals), negated so the
// largest is positive, each round((v/√2+0.5)*255), then 252 plus the index
// of the dropped component, as unpack reads them.
func packQuat(q [4]float64) [4]uint8 {
	largest := 0
	for k, v := range q {
		if math.Abs(v) > math.Abs(q[largest]) {
			largest = k
		}
	}
	if q[largest] < 0 {
		for k := range q {
			q[k] = -q[k]
		}
	}
	var b [4]uint8
	n := 0
	for k, v := range q {
		if k == largest {
			continue
		}
		b[n] = uint8(math.Round(255 * max(0, min(1, v/math.Sqrt2+0.5))))
		n++
	}
	b[3] = uint8(252 + largest)
	return b
}

// Encode writes objects, the pieces of one Gaussian cloud as Decode or
// nt3dmesh.PointPieces give them, as a SOG v2 bundle: a zip of meta.json and
// lossless PNG images (Go has no lossless WebP encoder; Decode reads any
// registered image format). Every object must be Gaussian splats, and their
// bands above DC must already be one palette shared by every piece (see
// nt3dmesh.BuildSHPalette) or absent: per-splat bands are an error. Splats
// are written in order, piece by piece, so labels and points keep their
// places. Positions are stored to 16 bits in SOG's log domain and scales, DC,
// and palette coefficients to 8 bits over each attribute's range, so a
// decode matches within that quantization. Nothing is written until the
// whole input has been checked.
func Encode(w io.Writer, objects []nt3dmesh.Object) error {
	n := 0
	var palette *nt3d.SHPalette
	for i := range objects {
		g := &objects[i].Geometry
		if g.PointStyle.Shape != nt3d.GaussianPoint {
			return errors.New("nt3dmesh: SOG holds Gaussian splats only")
		}
		if p := g.GaussianSHPalette; p != nil {
			if p.Stride() <= 0 || p.Count() <= 0 {
				return errors.New("nt3dmesh: SOG piece has an invalid SH palette")
			}
			if palette == nil {
				palette = p
			} else if p != palette && (p.Degree != palette.Degree || !slices.Equal(p.Coefficients, palette.Coefficients)) {
				return errors.New("nt3dmesh: SOG pieces must share one SH palette")
			}
		}
		n += len(g.Points)
	}
	if n == 0 {
		return errors.New("nt3dmesh: SOG needs at least one splat")
	}
	for i := range objects {
		g := &objects[i].Geometry
		count := 0
		if len(g.Points) > 0 {
			count = len(g.GaussianSH) / len(g.Points)
		}
		if count*len(g.Points) != len(g.GaussianSH) || count > 1 {
			if palette == nil {
				return errors.New("nt3dmesh: SOG needs an SH palette for bands above DC: build one first")
			}
			return errors.New("nt3dmesh: SOG piece has per-splat SH beside a palette")
		}
		if palette != nil {
			if g.GaussianSHPalette == nil && len(g.Points) > 0 {
				return errors.New("nt3dmesh: SOG pieces must share one SH palette")
			}
			if len(g.GaussianSHLabels) != len(g.Points) {
				return errors.New("nt3dmesh: SOG piece needs one SH label a splat")
			}
			for _, l := range g.GaussianSHLabels {
				if int(l) >= palette.Count() {
					return errors.New("nt3dmesh: SOG label past the palette")
				}
			}
		}
	}

	// Gather every splat's values, then the ranges they are quantized over.
	means := make([]float64, 0, 3*n)
	logScales := make([]float64, 0, 3*n)
	quats := make([][4]uint8, 0, n)
	dc := make([]float64, 0, 3*n)
	alpha := make([]uint8, 0, n)
	var labels []uint16
	if palette != nil {
		labels = make([]uint16, 0, n)
	}
	for i := range objects {
		g := &objects[i].Geometry
		for j, p := range g.Points {
			for _, v := range [3]float64{float64(p.Position.X), float64(p.Position.Y), float64(p.Position.Z)} {
				means = append(means, math.Copysign(math.Log1p(math.Abs(v)), v))
			}
			q, s := splat(p.GaussianAxes)
			logScales = append(logScales, s[0], s[1], s[2])
			quats = append(quats, packQuat(q))
			if len(g.GaussianSH) > 0 {
				c := g.GaussianSH[j]
				dc = append(dc, float64(c[0]), float64(c[1]), float64(c[2]))
			} else {
				for _, c := range [3]uint8{p.Color.R, p.Color.G, p.Color.B} {
					dc = append(dc, (float64(c)/255-0.5)/0.28209479177387814)
				}
			}
			alpha = append(alpha, p.Color.A)
		}
		labels = append(labels, g.GaussianSHLabels...)
	}
	var meansLo, meansHi [3]float64
	for k := range 3 {
		axis := make([]float64, 0, n)
		for i := range n {
			axis = append(axis, means[3*i+k])
		}
		u := rangeOf(axis)
		meansLo[k], meansHi[k] = u.lo, u.hi
	}
	scales, sh0 := rangeOf(logScales), rangeOf(dc)

	// The per-splat images: splat i at (i % width, i / width).
	width := int(math.Ceil(math.Sqrt(float64(n))))
	height := (n + width - 1) / width
	rect := image.Rect(0, 0, width, height)
	meansL, meansU, scalesImg, quatsImg, sh0Img := image.NewNRGBA(rect), image.NewNRGBA(rect), image.NewNRGBA(rect), image.NewNRGBA(rect), image.NewNRGBA(rect)
	var labelsImg *image.NRGBA
	if palette != nil {
		labelsImg = image.NewNRGBA(rect)
	}
	for i := range n {
		o := meansL.PixOffset(i%width, i/width)
		for k := range 3 {
			q := quantize16(means[3*i+k], meansLo[k], meansHi[k])
			meansL.Pix[o+k], meansU.Pix[o+k] = uint8(q), uint8(q>>8)
			scalesImg.Pix[o+k] = scales.index(logScales[3*i+k])
			sh0Img.Pix[o+k] = sh0.index(dc[3*i+k])
		}
		meansL.Pix[o+3], meansU.Pix[o+3], scalesImg.Pix[o+3] = 255, 255, 255
		copy(quatsImg.Pix[o:o+4], quats[i][:])
		sh0Img.Pix[o+3] = alpha[i]
		if palette != nil {
			labelsImg.Pix[o], labelsImg.Pix[o+1], labelsImg.Pix[o+3] = uint8(labels[i]), uint8(labels[i]>>8), 255
		}
	}

	meta := encodedMeta{Version: 2, Count: n}
	meta.Means.Mins, meta.Means.Maxs = meansLo, meansHi
	meta.Means.Files = []string{"means_l.png", "means_u.png"}
	meta.Scales = encodedTable{Codebook: scales.entries(), Files: []string{"scales.png"}}
	meta.Quats.Files = []string{"quats.png"}
	meta.Sh0 = encodedTable{Codebook: sh0.entries(), Files: []string{"sh0.png"}}
	files := []encodedFile{{"means_l.png", meansL}, {"means_u.png", meansU}, {"scales.png", scalesImg}, {"quats.png", quatsImg}, {"sh0.png", sh0Img}}
	if palette != nil {
		coeffs, count := palette.Stride(), palette.Count()
		values := make([]float64, 0, 3*len(palette.Coefficients))
		for _, c := range palette.Coefficients {
			values = append(values, float64(c[0]), float64(c[1]), float64(c[2]))
		}
		shN := rangeOf(values)
		centroids := image.NewNRGBA(image.Rect(0, 0, 64*coeffs, (count+63)/64))
		for c := range count {
			for k := range coeffs {
				o := centroids.PixOffset((c%64)*coeffs+k, c/64)
				v := palette.Coefficients[c*coeffs+k]
				centroids.Pix[o], centroids.Pix[o+1], centroids.Pix[o+2], centroids.Pix[o+3] = shN.index(float64(v[0])), shN.index(float64(v[1])), shN.index(float64(v[2])), 255
			}
		}
		meta.ShN = &encodedShN{Count: count, Bands: palette.Degree, encodedTable: encodedTable{Codebook: shN.entries(), Files: []string{"shN_centroids.png", "shN_labels.png"}}}
		files = append(files, encodedFile{"shN_centroids.png", centroids}, encodedFile{"shN_labels.png", labelsImg})
	}

	// Encode every file in memory, then write the bundle.
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("nt3dmesh: SOG meta.json: %w", err)
	}
	encoded := make([][]byte, len(files))
	encoder := png.Encoder{CompressionLevel: png.DefaultCompression}
	for i, f := range files {
		var buf bytes.Buffer
		if err := encoder.Encode(&buf, f.image); err != nil {
			return fmt.Errorf("nt3dmesh: SOG %s: %w", f.name, err)
		}
		encoded[i] = buf.Bytes()
	}
	z := zip.NewWriter(w)
	write := func(name string, data []byte) error {
		f, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		return err
	}
	if err := write("meta.json", metaJSON); err != nil {
		return fmt.Errorf("nt3dmesh: SOG bundle: %w", err)
	}
	for i, f := range files {
		if err := write(f.name, encoded[i]); err != nil {
			return fmt.Errorf("nt3dmesh: SOG bundle: %w", err)
		}
	}
	if err := z.Close(); err != nil {
		return fmt.Errorf("nt3dmesh: SOG bundle: %w", err)
	}
	return nil
}

// encodedFile is one image of a bundle being written.
type encodedFile struct {
	name  string
	image *image.NRGBA
}

// encodedMeta is the meta.json Encode writes: SOG v2, as parseMeta reads it.
type encodedMeta struct {
	Version int `json:"version"`
	Count   int `json:"count"`
	Means   struct {
		Mins  [3]float64 `json:"mins"`
		Maxs  [3]float64 `json:"maxs"`
		Files []string   `json:"files"`
	} `json:"means"`
	Scales encodedTable `json:"scales"`
	Quats  struct {
		Files []string `json:"files"`
	} `json:"quats"`
	Sh0 encodedTable `json:"sh0"`
	ShN *encodedShN  `json:"shN,omitempty"`
}

// encodedTable is a v2 attribute: a 256-entry codebook and its files.
type encodedTable struct {
	Codebook []float64 `json:"codebook"`
	Files    []string  `json:"files"`
}

// encodedShN is the palette: its centroid count, band count, and table.
type encodedShN struct {
	Count int `json:"count"`
	Bands int `json:"bands"`
	encodedTable
}
