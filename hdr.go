// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	nt3dmath "nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/sh"
)

type hdrImage struct {
	width, height int
	pix           []float32 // 3 linear RGB floats per texel, clamped to binary16 range [0, 65504].
}

func (img hdrImage) valid() bool {
	return img.width > 0 && img.height > 0 && len(img.pix) == img.width*img.height*3
}

// NewHDREnvironment decodes a Radiance RGBE (.hdr / .pic) equirectangular
// panorama and builds an unclamped linear-light Environment for diffuse SH
// illumination, GGX roughness-filtered specular IBL, and HDR sky rendering.
//
// Orientation follows NewEnvironment: +Z is up (top row), the image center
// faces +X, and U increases toward +Y. Dimensions must be in 1..8192 per side.
func NewHDREnvironment(data []byte) (*Environment, error) {
	base, err := decodeRadianceHDR(data)
	if err != nil {
		return nil, err
	}
	e := &Environment{
		hdr:     true,
		hdrBase: base,
	}
	radiance, err := nt3dsh.Project(2, func(d nt3dmath.Vec3) nt3dsh.RGB {
		c := e.sample(d)
		return nt3dsh.RGB{c[0], c[1], c[2]}
	})
	if err != nil {
		return nil, err
	}
	e.diffuse = nt3dsh.Diffuse(radiance)
	return e, nil
}

func decodeRadianceHDR(data []byte) (hdrImage, error) {
	if len(data) == 0 {
		return hdrImage{}, fmt.Errorf("nt3d: empty HDR image")
	}
	pos := 0
	readLine := func() (string, bool) {
		if pos >= len(data) {
			return "", false
		}
		nl := bytes.IndexByte(data[pos:], '\n')
		if nl < 0 {
			line := strings.TrimSuffix(string(data[pos:]), "\r")
			pos = len(data)
			return line, true
		}
		line := strings.TrimSuffix(string(data[pos:pos+nl]), "\r")
		pos += nl + 1
		return line, true
	}

	first, ok := readLine()
	if !ok {
		return hdrImage{}, fmt.Errorf("nt3d: truncated HDR header")
	}
	exposure := 1.0
	hasMagic := strings.HasPrefix(first, "#?")
	hasFormat := false
	checkHeaderLine := func(line string) error {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			return nil
		}
		eq := strings.IndexByte(trimmed, '=')
		if eq < 0 {
			return nil
		}
		key := strings.TrimSpace(trimmed[:eq])
		val := strings.TrimSpace(trimmed[eq+1:])
		switch key {
		case "FORMAT":
			if val != "32-bit_rle_rgbe" {
				return fmt.Errorf("nt3d: unsupported HDR format %q", val)
			}
			hasFormat = true
		case "EXPOSURE":
			v, err := strconv.ParseFloat(val, 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
				return fmt.Errorf("nt3d: invalid HDR exposure %q", val)
			}
			exposure *= v
			if math.IsInf(exposure, 0) || exposure <= 0 {
				return fmt.Errorf("nt3d: HDR exposure out of range")
			}
		}
		return nil
	}

	if !hasMagic {
		if err := checkHeaderLine(first); err != nil {
			return hdrImage{}, err
		}
		if !hasFormat {
			return hdrImage{}, fmt.Errorf("nt3d: invalid Radiance HDR header")
		}
	}

	if first != "" {
		foundBlank := false
		for {
			line, ok := readLine()
			if !ok {
				return hdrImage{}, fmt.Errorf("nt3d: missing HDR header terminator")
			}
			if line == "" {
				foundBlank = true
				break
			}
			if err := checkHeaderLine(line); err != nil {
				return hdrImage{}, err
			}
		}
		if !foundBlank {
			return hdrImage{}, fmt.Errorf("nt3d: missing HDR header terminator")
		}
	}

	resLine, ok := readLine()
	if !ok {
		return hdrImage{}, fmt.Errorf("nt3d: missing HDR resolution string")
	}
	w, h, flipX, flipY, err := parseHDRResolution(resLine)
	if err != nil {
		return hdrImage{}, err
	}
	if w < 1 || h < 1 || w > 8192 || h > 8192 {
		return hdrImage{}, fmt.Errorf("nt3d: HDR dimensions must be 1..8192")
	}
	minScanline := w * 4
	if w >= 8 && w <= 0x7fff {
		minScanline = 4 + 8*((w+126)/127)
	}
	if len(data)-pos < h*minScanline {
		return hdrImage{}, fmt.Errorf("nt3d: truncated HDR scanline data")
	}

	pix := make([]float32, w*h*3)
	scanline := make([]byte, w*4)
	invExposure := 1.0 / exposure

	for sy := 0; sy < h; sy++ {
		if pos+4 > len(data) {
			return hdrImage{}, fmt.Errorf("nt3d: unexpected EOF in HDR scanline %d", sy)
		}
		if w >= 8 && w <= 0x7fff && data[pos] == 2 && data[pos+1] == 2 && data[pos+2]&0x80 == 0 {
			encW := int(data[pos+2])<<8 | int(data[pos+3])
			if encW != w {
				return hdrImage{}, fmt.Errorf("nt3d: HDR scanline width %d does not match %d", encW, w)
			}
			pos += 4
			for ch := 0; ch < 4; ch++ {
				x := 0
				for x < w {
					if pos >= len(data) {
						return hdrImage{}, fmt.Errorf("nt3d: unexpected EOF in HDR RLE stream")
					}
					code := int(data[pos])
					pos++
					if code > 128 {
						count := code - 128
						if pos >= len(data) || x+count > w {
							return hdrImage{}, fmt.Errorf("nt3d: corrupt HDR RLE run")
						}
						val := data[pos]
						pos++
						for k := 0; k < count; k++ {
							scanline[(x+k)*4+ch] = val
						}
						x += count
					} else {
						if code == 0 || pos+code > len(data) || x+code > w {
							return hdrImage{}, fmt.Errorf("nt3d: corrupt HDR RLE literal")
						}
						for k := 0; k < code; k++ {
							scanline[(x+k)*4+ch] = data[pos+k]
						}
						pos += code
						x += code
					}
				}
			}
		} else {
			need := w * 4
			if pos+need > len(data) {
				return hdrImage{}, fmt.Errorf("nt3d: unexpected EOF in uncompressed HDR scanline")
			}
			copy(scanline, data[pos:pos+need])
			pos += need
		}

		dy := sy
		if flipY {
			dy = h - 1 - sy
		}
		for sx := 0; sx < w; sx++ {
			dx := sx
			if flipX {
				dx = w - 1 - sx
			}
			rb := scanline[sx*4]
			gb := scanline[sx*4+1]
			bb := scanline[sx*4+2]
			eb := scanline[sx*4+3]
			off := (dy*w + dx) * 3
			if eb == 0 {
				continue
			}
			scale := math.Ldexp(1.0, int(eb)-(128+8)) * invExposure
			pix[off] = clampHalfFloat32((float64(rb) + 0.5) * scale)
			pix[off+1] = clampHalfFloat32((float64(gb) + 0.5) * scale)
			pix[off+2] = clampHalfFloat32((float64(bb) + 0.5) * scale)
		}
	}

	return hdrImage{width: w, height: h, pix: pix}, nil
}

func parseHDRResolution(line string) (w, h int, flipX, flipY bool, err error) {
	fields := strings.Fields(line)
	if len(fields) != 4 {
		return 0, 0, false, false, fmt.Errorf("nt3d: invalid HDR resolution %q", line)
	}
	aAxis, aStr, bAxis, bStr := fields[0], fields[1], fields[2], fields[3]
	if (aAxis != "-Y" && aAxis != "+Y") || (bAxis != "+X" && bAxis != "-X") {
		return 0, 0, false, false, fmt.Errorf("nt3d: unsupported HDR orientation %q", line)
	}
	h, err = strconv.Atoi(aStr)
	if err != nil {
		return 0, 0, false, false, fmt.Errorf("nt3d: invalid HDR height %q", aStr)
	}
	w, err = strconv.Atoi(bStr)
	if err != nil {
		return 0, 0, false, false, fmt.Errorf("nt3d: invalid HDR width %q", bStr)
	}
	return w, h, bAxis == "-X", aAxis == "+Y", nil
}

func clampHalfFloat32(v float64) float32 {
	if !(v > 0) {
		return 0
	}
	if v > 65504 {
		v = 65504
	}
	return float16value(float16bits(float32(v)))
}

func sampleHDRImg(img hdrImage, u, v float32) [4]float32 {
	w, h := img.width, img.height
	half := .5 / float32(h)
	vc := float64(max(half, min(1-half, v)))
	px := float64(u)*float64(w) - .5
	py := vc*float64(h) - .5
	ix := int(math.Floor(px))
	iy := int(math.Floor(py))
	fx := float32(px - float64(ix))
	fy := float32(py - float64(iy))
	x0, x1 := wrapTexel(ix, w), wrapTexel(ix+1, w)
	y0, y1 := max(0, min(h-1, iy)), max(0, min(h-1, iy+1))

	w00 := (1 - fx) * (1 - fy)
	w10 := fx * (1 - fy)
	w01 := (1 - fx) * fy
	w11 := fx * fy
	p00 := (y0*w + x0) * 3
	p10 := (y0*w + x1) * 3
	p01 := (y1*w + x0) * 3
	p11 := (y1*w + x1) * 3
	return [4]float32{
		img.pix[p00]*w00 + img.pix[p10]*w10 + img.pix[p01]*w01 + img.pix[p11]*w11,
		img.pix[p00+1]*w00 + img.pix[p10+1]*w10 + img.pix[p01+1]*w01 + img.pix[p11+1]*w11,
		img.pix[p00+2]*w00 + img.pix[p10+2]*w10 + img.pix[p01+2]*w01 + img.pix[p11+2]*w11,
		1,
	}
}

func sampleHDRChain(chain []hdrImage, d nt3dmath.Vec3, lod float32) [4]float32 {
	u := float32(math.Atan2(float64(d.Y), float64(d.X))/(2*math.Pi) + .5)
	v := float32(math.Acos(float64(max(-1, min(1, d.Z)))) / math.Pi)
	lod = max(0, min(lod, float32(len(chain)-1)))
	l0 := int(lod)
	c := sampleHDRImg(chain[l0], u, v)
	if f := lod - float32(l0); f > 0 && l0+1 < len(chain) {
		b := sampleHDRImg(chain[l0+1], u, v)
		for i := 0; i < 3; i++ {
			c[i] += (b[i] - c[i]) * f
		}
	}
	return c
}

func (e *Environment) hdrLevels() []hdrImage {
	e.hdrMipOnce.Do(func() {
		mips := []hdrImage{e.hdrBase}
		cur := e.hdrBase
		for cur.width > 1 || cur.height > 1 {
			nw, nh := max(1, cur.width/2), max(1, cur.height/2)
			next := hdrImage{width: nw, height: nh, pix: make([]float32, nw*nh*3)}
			for y := 0; y < nh; y++ {
				sy0 := min(cur.height-1, y*2)
				sy1 := min(cur.height-1, y*2+1)
				for x := 0; x < nw; x++ {
					sx0 := min(cur.width-1, x*2)
					sx1 := min(cur.width-1, x*2+1)
					p00 := (sy0*cur.width + sx0) * 3
					p10 := (sy0*cur.width + sx1) * 3
					p01 := (sy1*cur.width + sx0) * 3
					p11 := (sy1*cur.width + sx1) * 3
					dst := (y*nw + x) * 3
					for c := 0; c < 3; c++ {
						avg := .25 * (cur.pix[p00+c] + cur.pix[p10+c] + cur.pix[p01+c] + cur.pix[p11+c])
						next.pix[dst+c] = float16value(float16bits(avg))
					}
				}
			}
			mips = append(mips, next)
			cur = next
		}
		e.hdrMips = mips
	})
	return e.hdrMips
}

func (e *Environment) hdrSpecularLevels() []hdrImage {
	e.specOnce.Do(func() {
		srcMips := e.hdrLevels()
		n := min(maxEnvironmentSpecMips, len(srcMips))
		levels := make([]hdrImage, n)
		levels[0] = e.hdrBase
		if n <= 1 {
			e.hdrSpec = levels
			return
		}
		const taps = 24
		type ggxTap struct {
			hx, hy, hz float32
			nl, lod    float32
		}
		omegaP := 4 * math.Pi / float64(max(1, e.hdrBase.width*e.hdrBase.height))
		for m := 1; m < n; m++ {
			mw, mh := srcMips[m].width, srcMips[m].height
			roughness := float64(m) / float64(n-1)
			alpha := roughness * roughness
			a2 := alpha * alpha
			var pre [taps]ggxTap
			used := 0
			for s := uint32(0); s < taps; s++ {
				u1 := (float64(s) + .5) / taps
				u2 := radicalInverseVdC(s)
				phi := 2 * math.Pi * u2
				cosT := math.Sqrt((1 - u1) / (1 + (a2-1)*u1))
				nl := 2*cosT*cosT - 1
				if nl <= 0 {
					continue
				}
				sinT := math.Sqrt(max(0, 1-cosT*cosT))
				denom := cosT*cosT*(a2-1) + 1
				dTerm := a2 / (math.Pi * denom * denom)
				pdf := max(1e-6, dTerm*.25)
				omegaS := 1.0 / (taps * pdf)
				lod := max(0, min(float64(len(srcMips)-1), .5*math.Log2(max(1, omegaS/omegaP))))
				pre[used] = ggxTap{
					hx:  float32(sinT * math.Cos(phi)),
					hy:  float32(sinT * math.Sin(phi)),
					hz:  float32(cosT),
					nl:  float32(nl),
					lod: float32(lod),
				}
				used++
			}
			dst := hdrImage{width: mw, height: mh, pix: make([]float32, mw*mh*3)}
			for y := 0; y < mh; y++ {
				theta := (float64(y) + .5) / float64(mh) * math.Pi
				sinTh, cosTh := math.Sin(theta), math.Cos(theta)
				for x := 0; x < mw; x++ {
					phi := ((float64(x)+.5)/float64(mw) - .5) * 2 * math.Pi
					nrm := nt3dmath.Vec3{
						X: float32(sinTh * math.Cos(phi)),
						Y: float32(sinTh * math.Sin(phi)),
						Z: float32(cosTh),
					}
					up := nt3dmath.Vec3{Z: 1}
					if abs(nrm.Z) > .999 {
						up = nt3dmath.Vec3{X: 1}
					}
					tangent := up.Cross(nrm).Normalize()
					bitangent := nrm.Cross(tangent)
					var sum [3]float32
					var weight float32
					for k := 0; k < used; k++ {
						tp := pre[k]
						h := tangent.Scale(tp.hx).Add(bitangent.Scale(tp.hy)).Add(nrm.Scale(tp.hz))
						l := h.Scale(2 * tp.hz).Sub(nrm)
						c := sampleHDRChain(srcMips, l, tp.lod)
						sum[0] += c[0] * tp.nl
						sum[1] += c[1] * tp.nl
						sum[2] += c[2] * tp.nl
						weight += tp.nl
					}
					off := (y*mw + x) * 3
					if weight > 0 {
						inv := 1 / weight
						dst.pix[off] = clampHalfFloat32(float64(sum[0] * inv))
						dst.pix[off+1] = clampHalfFloat32(float64(sum[1] * inv))
						dst.pix[off+2] = clampHalfFloat32(float64(sum[2] * inv))
					}
				}
			}
			levels[m] = dst
		}
		e.hdrSpec = levels
	})
	return e.hdrSpec
}

// packHDRRows packs an hdrImage into RGBA16Float rows aligned to rowStride bytes.
func packHDRRows(img hdrImage, rowStride int) []byte {
	const f16One = 0x3C00
	buf := make([]byte, rowStride*img.height)
	for y := 0; y < img.height; y++ {
		row := buf[y*rowStride:]
		src := img.pix[y*img.width*3:]
		for x := 0; x < img.width; x++ {
			binary.LittleEndian.PutUint16(row[x*8:], float16bits(src[x*3]))
			binary.LittleEndian.PutUint16(row[x*8+2:], float16bits(src[x*3+1]))
			binary.LittleEndian.PutUint16(row[x*8+4:], float16bits(src[x*3+2]))
			binary.LittleEndian.PutUint16(row[x*8+6:], f16One)
		}
	}
	return buf
}
