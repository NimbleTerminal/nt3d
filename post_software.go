// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image"
	"math"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// applyPostSoftware applies f.PostProcess in linear light to the w*h float
// buffer acc (0..255+ in the frame's working space) and writes 8-bit sRGB
// pixels into img. SSAO runs only when f.Complete is true.
func applyPostSoftware(f Frame, img *image.RGBA, acc, depth []float32) {
	w, h := f.Width, f.Height
	if w <= 0 || h <= 0 || len(acc) < w*h*3 {
		return
	}
	hdr := make([]float32, w*h*3)
	if f.Linear {
		for i, v := range acc[:w*h*3] {
			if v > 0 {
				hdr[i] = v * (1.0 / 255.0)
			}
		}
	} else {
		for i, v := range acc[:w*h*3] {
			if v > 0 {
				hdr[i] = float32(decodeSRGBExtended(float64(v) * (1.0 / 255.0)))
			}
		}
	}

	p := f.PostProcess
	if p.SSAO.on() && f.Complete && len(depth) >= w*h {
		if ao := softwareSSAO(f, depth); ao != nil {
			for i, occ := range ao {
				off := i * 3
				hdr[off] *= occ
				hdr[off+1] *= occ
				hdr[off+2] *= occ
			}
		}
	}

	if p.Bloom.on() {
		softwareBloom(w, h, hdr, p.Bloom)
	}

	expScale := exposureScale(p.Exposure)
	fw, fh := float32(w), float32(h)
	for y := range h {
		dy := (float32(y)+0.5)/fh - 0.5
		row := img.Pix[y*img.Stride:]
		for x := range w {
			dx := (float32(x)+0.5)/fw - 0.5
			vig := vignetteFactor(dx, dy, p.Vignette)
			src := hdr[(y*w+x)*3:]
			dst := row[x*4:]
			for c := range 3 {
				v := toneMapChannel(src[c]*expScale, p.ToneMap) * vig
				dst[c] = encode8(v)
			}
			dst[3] = 255
		}
	}
}

func decodeSRGBExtended(v float64) float64 {
	if v <= 0 {
		return 0
	}
	if v <= 1 {
		return decodeSRGB(v)
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

// shrinkPost averages an s × s supersampled float buffer and min-reduces its
// depth buffer down to w × h before software post-processing runs.
func shrinkPost(w, h, s int, bigAcc, bigDepth []float32, linear bool) (acc, depth []float32) {
	bw := w * s
	n := float32(s * s)
	acc = make([]float32, w*h*3)
	depth = make([]float32, w*h)
	for y := range h {
		for x := range w {
			var sum [3]float32
			d := float32(noDepth)
			for j := range s {
				row := (y*s + j) * bw
				for i := range s {
					idx := row + x*s + i
					if idx < len(bigDepth) && bigDepth[idx] < d {
						d = bigDepth[idx]
					}
					src := bigAcc[idx*3:]
					if linear {
						sum[0] += src[0]
						sum[1] += src[1]
						sum[2] += src[2]
					} else {
						for c := range 3 {
							sum[c] += float32(decodeSRGBExtended(float64(src[c])*(1.0/255.0))) * 255
						}
					}
				}
			}
			out := acc[(y*w+x)*3:]
			out[0], out[1], out[2] = sum[0]/n, sum[1]/n, sum[2]/n
			depth[y*w+x] = d
		}
	}
	return acc, depth
}

// softwareBloom builds a fast quarter-resolution two-level tent-blurred glow
// pyramid and adds it back into hdr in place.
func softwareBloom(w, h int, hdr []float32, bloom Bloom) {
	w0, h0 := max(1, (w+3)/4), max(1, (h+3)/4)
	thresh := bloom.threshold()
	radius := bloom.radius()
	mip0 := make([]float32, w0*h0*3)

	// Downsample WxH -> (W/4)x(H/4) with soft-knee prefilter and Karis firefly damping.
	for y := range h0 {
		yStart, yEnd := y*h/h0, max(y*h/h0+1, (y+1)*h/h0)
		for x := range w0 {
			xStart, xEnd := x*w/w0, max(x*w/w0+1, (x+1)*w/w0)
			var sum [3]float32
			var wsum float32
			for sy := yStart; sy < yEnd && sy < h; sy++ {
				for sx := xStart; sx < xEnd && sx < w; sx++ {
					p := hdr[(sy*w+sx)*3:]
					wt := bloomSoftWeight(p[0], p[1], p[2], thresh)
					r, g, b := p[0]*wt, p[1]*wt, p[2]*wt
					kw := 1 / (1 + max(r, g, b))
					sum[0] += r * kw
					sum[1] += g * kw
					sum[2] += b * kw
					wsum += kw
				}
			}
			if wsum > 0 {
				dst := mip0[(y*w0+x)*3:]
				dst[0], dst[1], dst[2] = sum[0]/wsum, sum[1]/wsum, sum[2]/wsum
			}
		}
	}

	blurSeparable := func(buf []float32, bw, bh, passes int) {
		tmp := make([]float32, len(buf))
		for range passes {
			for y := range bh {
				for x := range bw {
					x0, x2 := max(0, x-1), min(bw-1, x+1)
					a, b, c := buf[(y*bw+x0)*3:], buf[(y*bw+x)*3:], buf[(y*bw+x2)*3:]
					dst := tmp[(y*bw+x)*3:]
					for k := range 3 {
						dst[k] = (a[k] + 2*b[k] + c[k]) * 0.25
					}
				}
			}
			for y := range bh {
				y0, y2 := max(0, y-1), min(bh-1, y+1)
				for x := range bw {
					a, b, c := tmp[(y0*bw+x)*3:], tmp[(y*bw+x)*3:], tmp[(y2*bw+x)*3:]
					dst := buf[(y*bw+x)*3:]
					for k := range 3 {
						dst[k] = (a[k] + 2*b[k] + c[k]) * 0.25
					}
				}
			}
		}
	}

	blurSeparable(mip0, w0, h0, 2)

	levels := 1
	if w0 >= 4 && h0 >= 4 {
		levels = 2
		w1, h1 := max(1, (w0+1)/2), max(1, (h0+1)/2)
		mip1 := make([]float32, w1*h1*3)
		for y := range h1 {
			for x := range w1 {
				var sum [3]float32
				var count float32
				for dy := range 2 {
					sy := min(h0-1, y*2+dy)
					for dx := range 2 {
						sx := min(w0-1, x*2+dx)
						src := mip0[(sy*w0+sx)*3:]
						sum[0] += src[0]
						sum[1] += src[1]
						sum[2] += src[2]
						count++
					}
				}
				dst := mip1[(y*w1+x)*3:]
				dst[0], dst[1], dst[2] = sum[0]/count, sum[1]/count, sum[2]/count
			}
		}
		blurSeparable(mip1, w1, h1, 2)
		for y := range h0 {
			u := (float32(y)+0.5)/float32(h0)*float32(h1) - 0.5
			for x := range w0 {
				v := (float32(x)+0.5)/float32(w0)*float32(w1) - 0.5
				c := sampleRGBBilinear(mip1, w1, h1, v, u)
				dst := mip0[(y*w0+x)*3:]
				dst[0] += c[0] * radius
				dst[1] += c[1] * radius
				dst[2] += c[2] * radius
			}
		}
	}

	gain := bloom.Strength * bloomMipGain(levels, radius)
	for y := range h {
		fy := (float32(y)+0.5)/float32(h)*float32(h0) - 0.5
		for x := range w {
			fx := (float32(x)+0.5)/float32(w)*float32(w0) - 0.5
			g := sampleRGBBilinear(mip0, w0, h0, fx, fy)
			dst := hdr[(y*w+x)*3:]
			dst[0] += g[0] * gain
			dst[1] += g[1] * gain
			dst[2] += g[2] * gain
		}
	}
}

func sampleRGBBilinear(buf []float32, w, h int, fx, fy float32) [3]float32 {
	fx = max(0, min(float32(w-1), fx))
	fy = max(0, min(float32(h-1), fy))
	x0, y0 := int(fx), int(fy)
	x1, y1 := min(w-1, x0+1), min(h-1, y0+1)
	tx, ty := fx-float32(x0), fy-float32(y0)
	p00 := buf[(y0*w+x0)*3:]
	p10 := buf[(y0*w+x1)*3:]
	p01 := buf[(y1*w+x0)*3:]
	p11 := buf[(y1*w+x1)*3:]
	var out [3]float32
	for k := range 3 {
		top := p00[k]*(1-tx) + p10[k]*tx
		bot := p01[k]*(1-tx) + p11[k]*tx
		out[k] = top*(1-ty) + bot*ty
	}
	return out
}

// softwareSSAO estimates screen-space ambient occlusion from the depth buffer
// for Complete software frames, matching the GPU estimator and bilateral blur.
func softwareSSAO(f Frame, depth []float32) []float32 {
	w, h := f.Width, f.Height
	inv, ok := inverseProjection(f.Matrix)
	if !ok || w < 2 || h < 2 {
		return nil
	}
	fw, fh := float32(w), float32(h)
	pos := make([]nt3dmath.Vec3, w*h)
	dist := make([]float32, w*h)
	valid := make([]bool, w*h)

	for y := range h {
		ndcY := 1 - 2*(float32(y)+0.5)/fh
		for x := range w {
			i := y*w + x
			d := depth[i]
			if d < 0 || d >= 1 {
				continue
			}
			ndcX := 2*(float32(x)+0.5)/fw - 1
			px, py, pz, pw := inv.Transform(nt3dmath.Vec3{X: ndcX, Y: ndcY, Z: d})
			if abs(pw) < 1e-8 {
				continue
			}
			p := nt3dmath.Vec3{X: px / pw, Y: py / pw, Z: pz / pw}
			pos[i] = p
			dist[i] = f.eyeDistance(p)
			valid[i] = true
		}
	}

	const taps = 16
	const goldenAngle = 2.39996323
	var spiral [taps][2]float32
	for k := range taps {
		r := float32(math.Sqrt(float64(k)+0.5) / math.Sqrt(taps))
		a := float64(k) * goldenAngle
		spiral[k] = [2]float32{r * float32(math.Cos(a)), r * float32(math.Sin(a))}
	}

	raw := make([]float32, w*h)
	worldScale := make([]float32, w*h)
	bias := f.PostProcess.SSAO.bias()
	strength := f.PostProcess.SSAO.Strength
	explicitRadius := f.PostProcess.SSAO.Radius
	defaultPx := min(40, max(3, 0.06*float32(min(w, h))))

	for y := range h {
		for x := range w {
			i := y*w + x
			raw[i] = 1
			if !valid[i] {
				continue
			}
			p := pos[i]
			d := depth[i]

			// Pick the smaller depth step on each axis so silhouettes do not
			// fabricate bent normals.
			var dxVec, dyVec nt3dmath.Vec3
			var haveX, haveY bool
			hasL, hasR := x > 0 && valid[i-1], x+1 < w && valid[i+1]
			switch {
			case hasL && hasR:
				if abs(depth[i+1]-d) <= abs(d-depth[i-1]) {
					dxVec = pos[i+1].Sub(p)
				} else {
					dxVec = p.Sub(pos[i-1])
				}
				haveX = true
			case hasR:
				dxVec, haveX = pos[i+1].Sub(p), true
			case hasL:
				dxVec, haveX = p.Sub(pos[i-1]), true
			}

			hasU, hasD := y > 0 && valid[i-w], y+1 < h && valid[i+w]
			switch {
			case hasU && hasD:
				if abs(depth[i+w]-d) <= abs(d-depth[i-w]) {
					dyVec = pos[i+w].Sub(p)
				} else {
					dyVec = p.Sub(pos[i-w])
				}
				haveY = true
			case hasD:
				dyVec, haveY = pos[i+w].Sub(p), true
			case hasU:
				dyVec, haveY = p.Sub(pos[i-w]), true
			}
			if !haveX || !haveY {
				continue
			}

			n := dyVec.Cross(dxVec)
			nLen2 := n.Dot(n)
			if nLen2 <= 1e-16 {
				continue
			}
			n = n.Scale(1 / float32(math.Sqrt(float64(nLen2))))
			if n.Dot(f.viewAt(p)) < 0 {
				n = n.Scale(-1)
			}

			pxWorld := max(1e-5, max(float32(math.Sqrt(float64(dxVec.Dot(dxVec)))), float32(math.Sqrt(float64(dyVec.Dot(dyVec))))))
			var radiusPx, radiusWorld float32
			if explicitRadius > 0 {
				radiusWorld = explicitRadius
				radiusPx = min(40, max(3, radiusWorld/pxWorld))
			} else {
				radiusPx = defaultPx
				radiusWorld = radiusPx * pxWorld
			}
			worldScale[i] = radiusWorld

			// Jimenez Interleaved Gradient Noise per pixel.
			ign := float64(52.9829189 * frac(0.06711056*(float64(x)+0.5)+0.00583715*(float64(y)+0.5)))
			ang := 2 * math.Pi * frac(ign)
			cs, sn := float32(math.Cos(ang)), float32(math.Sin(ang))

			var occ float32
			invR2 := 1 / max(1e-8, radiusWorld*radiusWorld)
			for k := range taps {
				ox := (spiral[k][0]*cs - spiral[k][1]*sn) * radiusPx
				oy := (spiral[k][0]*sn + spiral[k][1]*cs) * radiusPx
				sx := int(math.Round(float64(float32(x) + ox)))
				sy := int(math.Round(float64(float32(y) + oy)))
				if sx < 0 || sx >= w || sy < 0 || sy >= h {
					continue
				}
				si := sy*w + sx
				if !valid[si] || si == i {
					continue
				}
				v := pos[si].Sub(p)
				v2 := v.Dot(v)
				if v2 <= 1e-12 || v2 >= radiusWorld*radiusWorld {
					continue
				}
				vLen := float32(math.Sqrt(float64(v2)))
				ndotv := n.Dot(v.Scale(1 / vLen))
				if ndotv > bias {
					falloff := 1 - v2*invR2
					occ += (ndotv - bias) * falloff
				}
			}
			ao := 1 - strength*(occ*(2.2/taps))
			raw[i] = min(1, max(0, ao))
		}
	}

	// Depth-aware separable 5-tap bilateral blur.
	blurWeights := [5]float32{1, 4, 6, 4, 1}
	tmp := make([]float32, w*h)
	out := make([]float32, w*h)
	bilateral := func(src, dst []float32, stepX, stepY int) {
		for y := range h {
			for x := range w {
				i := y*w + x
				if !valid[i] {
					dst[i] = 1
					continue
				}
				centerD := dist[i]
				sigma := max(1e-4, worldScale[i]*0.35)
				var sum, wsum float32
				for k := -2; k <= 2; k++ {
					nx, ny := min(w-1, max(0, x+k*stepX)), min(h-1, max(0, y+k*stepY))
					ni := ny*w + nx
					if !valid[ni] {
						continue
					}
					dd := abs(dist[ni]-centerD) / sigma
					wt := blurWeights[k+2] * float32(math.Exp(float64(-dd*dd)))
					sum += src[ni] * wt
					wsum += wt
				}
				if wsum > 0 {
					dst[i] = sum / wsum
				} else {
					dst[i] = src[i]
				}
			}
		}
	}
	bilateral(raw, tmp, 1, 0)
	bilateral(tmp, out, 0, 1)
	return out
}

func frac(x float64) float64 { return x - math.Floor(x) }
