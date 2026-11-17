// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"math/bits"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/sh"
	"sync"
)

const maxEnvironmentSpecMips = 6

// Environment is an immutable distant sky, diffuse light, and reflection source.
// Construct one with NewEnvironment, NewHDREnvironment, NewSHEnvironment, or
// GradientEnvironment and share it between scenes. It does not contribute
// geometry, bounds, picking, or shadow casters.
type Environment struct {
	texture    *Texture
	panorama   *image.NRGBA
	hdr        bool
	hdrBase    hdrImage
	hdrMipOnce sync.Once
	hdrMips    []hdrImage
	// Degree-two diffuse SH, cosine-convolved and divided by pi.
	diffuse  [9]nt3dsh.RGB
	specOnce sync.Once
	spec     []*image.NRGBA
	hdrSpec  []hdrImage
}

func (e *Environment) valid() bool {
	if e == nil {
		return false
	}
	if e.hdr {
		return e.hdrBase.valid()
	}
	return e.panorama != nil && e.texture != nil && e.texture.valid()
}

func (e *Environment) specMipCount() int {
	if e == nil {
		return 0
	}
	if e.hdr {
		return len(e.hdrSpecularLevels())
	}
	return len(e.specularLevels())
}

// NewEnvironment snapshots an sRGB equirectangular image (normally 2:1).
// +Z is up (top row); the image center faces +X, and U increases toward +Y.
// Alpha is ignored. Dimensions have the same limits as NewTexture.
// Diffuse lighting uses a low-frequency approximation; PBR materials sample a
// GGX-prefiltered roughness mip chain, while classic Reflectivity samples level 0.
func NewEnvironment(src image.Image) (*Environment, error) {
	t, err := NewTexture(src)
	if err != nil {
		return nil, err
	}
	for i := 3; i < len(t.pixels); i += 4 {
		t.pixels[i] = 255
	}
	e := &Environment{texture: t, panorama: &image.NRGBA{Pix: t.pixels, Stride: t.width * 4, Rect: image.Rect(0, 0, t.width, t.height)}}
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

// GradientEnvironment creates a cheap sky, blending zenith to horizon to
// ground in linear light. Colors are opaque sRGB; their alpha is ignored.
func GradientEnvironment(zenith, horizon, ground color.RGBA) *Environment {
	img := image.NewNRGBA(image.Rect(0, 0, 128, 64))
	for y := 0; y < 64; y++ {
		z := math.Cos((float64(y) + .5) / 64 * math.Pi)
		end := zenith
		if z < 0 {
			end = ground
		}
		k := float32(math.Abs(z))
		c := color.NRGBA{R: encode8(srgbDecode[horizon.R]*(1-k) + srgbDecode[end.R]*k), G: encode8(srgbDecode[horizon.G]*(1-k) + srgbDecode[end.G]*k), B: encode8(srgbDecode[horizon.B]*(1-k) + srgbDecode[end.B]*k), A: 255}
		for x := 0; x < 128; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	e, _ := NewEnvironment(img)
	return e
}

// SetEnvironment selects a shared sky and diffuse light; nil restores the
// solid background. Existing frames retain their environment. It sets the
// Environment of the scene's Look and nothing else.
func (s *Scene) SetEnvironment(e *Environment) {
	if s.look.Environment != e {
		s.look.Environment = e
		s.changed(false, false)
	}
}

func (e *Environment) sample(d nt3dmath.Vec3) [4]float32 {
	u := float32(math.Atan2(float64(d.Y), float64(d.X))/(2*math.Pi) + .5)
	v := float32(math.Acos(float64(max(-1, min(1, d.Z)))) / math.Pi)
	if e.hdr {
		return sampleHDRImg(e.hdrBase, u, v)
	}
	half := .5 / float32(e.texture.height)
	return sampleLevel(e.panorama, UV{u, max(half, min(1-half, v))}, true)
}

// Perspective skies follow the projection and camera roll. Orthographic
// skies use a fixed 45 degree field of view; translation leaves them fixed.
func (f Frame) skyBasis() (right, up, forward nt3dmath.Vec3) {
	forward = f.Forward.Normalize()
	if forward == (nt3dmath.Vec3{}) {
		forward = nt3dmath.Vec3{Y: 1}
	}
	right = nt3dmath.Vec3{X: f.Matrix[0], Y: f.Matrix[4], Z: f.Matrix[8]}.Normalize()
	up = nt3dmath.Vec3{X: f.Matrix[1], Y: f.Matrix[5], Z: f.Matrix[9]}.Normalize()
	if right == (nt3dmath.Vec3{}) || up == (nt3dmath.Vec3{}) {
		right = forward.Cross(nt3dmath.Vec3{Z: 1}).Normalize()
		if right == (nt3dmath.Vec3{}) {
			right = nt3dmath.Vec3{X: 1}
		}
		up = right.Cross(forward)
	}
	if f.Perspective {
		x := float32(math.Sqrt(float64(f.Matrix[0]*f.Matrix[0] + f.Matrix[4]*f.Matrix[4] + f.Matrix[8]*f.Matrix[8])))
		y := float32(math.Sqrt(float64(f.Matrix[1]*f.Matrix[1] + f.Matrix[5]*f.Matrix[5] + f.Matrix[9]*f.Matrix[9])))
		if x > 0 && y > 0 {
			return right.Scale(1 / x), up.Scale(1 / y), forward
		}
	}
	return right.Scale(float32(math.Tan(halfFOV)) * float32(f.Width) / float32(f.Height)), up.Scale(float32(math.Tan(halfFOV))), forward
}

func (f Frame) skyPixel(x, y int) color.RGBA {
	r, u, v := f.skyBasis()
	d := v.Add(r.Scale(2*(float32(x)+.5)/float32(f.Width) - 1)).Add(u.Scale(1 - 2*(float32(y)+.5)/float32(f.Height))).Normalize()
	return f.Environment.pixel(d)
}

func (e *Environment) pixel(d nt3dmath.Vec3) color.RGBA {
	c := e.sample(d)
	return color.RGBA{encode8(c[0]), encode8(c[1]), encode8(c[2]), 255}
}

// NewSHEnvironment creates a sky and diffuse illumination from 1, 4, 9,
// or 16 linear-radiance RGB coefficients in nt3dsh ordering. It snapshots
// the coefficients. Diffuse lighting retains HDR values; the generated
// 128x64 sRGB sky clamps values to [0,1], like other image environments.
// Coefficients do not include the 3DGS 0.5 offset.
func NewSHEnvironment(radiance []nt3dsh.RGB) (*Environment, error) {
	if !nt3dsh.Valid(radiance) {
		return nil, fmt.Errorf("nt3d: invalid SH environment")
	}
	img := image.NewNRGBA(image.Rect(0, 0, 128, 64))
	for y := 0; y < 64; y++ {
		theta := (float64(y) + .5) * math.Pi / 64
		z := math.Cos(theta)
		r := math.Sin(theta)
		for x := 0; x < 128; x++ {
			phi := ((float64(x)+.5)/128 - .5) * 2 * math.Pi
			c := nt3dsh.Evaluate(radiance, nt3dmath.Vec3{X: float32(r * math.Cos(phi)), Y: float32(r * math.Sin(phi)), Z: float32(z)})
			img.SetNRGBA(x, y, color.NRGBA{R: encode8(c[0]), G: encode8(c[1]), B: encode8(c[2]), A: 255})
		}
	}
	e, err := NewEnvironment(img)
	if err != nil {
		return nil, err
	}
	e.diffuse = nt3dsh.Diffuse(radiance)
	return e, nil
}

// DiffuseSH returns a copy of the degree-two, cosine-convolved lighting.
func (e *Environment) DiffuseSH() [9]nt3dsh.RGB { return e.diffuse }

func samplePanoramaImg(img *image.NRGBA, u, v float32) [4]float32 {
	half := .5 / float32(img.Rect.Dy())
	return sampleLevel(img, UV{u, max(half, min(1-half, v))}, true)
}

func samplePanoramaChain(chain []*image.NRGBA, d nt3dmath.Vec3, lod float32) [4]float32 {
	u := float32(math.Atan2(float64(d.Y), float64(d.X))/(2*math.Pi) + .5)
	v := float32(math.Acos(float64(max(-1, min(1, d.Z)))) / math.Pi)
	lod = max(0, min(lod, float32(len(chain)-1)))
	l0 := int(lod)
	c := samplePanoramaImg(chain[l0], u, v)
	if f := lod - float32(l0); f > 0 && l0+1 < len(chain) {
		b := samplePanoramaImg(chain[l0+1], u, v)
		for i := range c {
			c[i] += (b[i] - c[i]) * f
		}
	}
	return c
}

func (e *Environment) sampleSpecular(d nt3dmath.Vec3, roughness float32) [4]float32 {
	if roughness <= 0 {
		return e.sample(d)
	}
	if e.hdr {
		levels := e.hdrSpecularLevels()
		return sampleHDRChain(levels, d, min(1, roughness)*float32(len(levels)-1))
	}
	levels := e.specularLevels()
	return samplePanoramaChain(levels, d, min(1, roughness)*float32(len(levels)-1))
}

func radicalInverseVdC(i uint32) float64 {
	return float64(bits.Reverse32(i)) / float64(1<<32)
}

// specularLevels builds and caches a GGX-prefiltered equirectangular mip chain
// (level 0 is the sharp panorama; higher levels blur by perceptual roughness).
func (e *Environment) specularLevels() []*image.NRGBA {
	e.specOnce.Do(func() {
		srcMips := e.texture.levels()
		n := min(maxEnvironmentSpecMips, len(srcMips))
		levels := make([]*image.NRGBA, n)
		levels[0] = e.panorama
		if n <= 1 {
			e.spec = levels
			return
		}
		const taps = 24
		type ggxTap struct {
			hx, hy, hz float32
			nl, lod    float32
		}
		omegaP := 4 * math.Pi / float64(max(1, e.texture.width*e.texture.height))
		for m := 1; m < n; m++ {
			mw, mh := srcMips[m].Rect.Dx(), srcMips[m].Rect.Dy()
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
			dst := image.NewNRGBA(image.Rect(0, 0, mw, mh))
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
						c := samplePanoramaChain(srcMips, l, tp.lod)
						sum[0] += c[0] * tp.nl
						sum[1] += c[1] * tp.nl
						sum[2] += c[2] * tp.nl
						weight += tp.nl
					}
					var r, g, b uint8
					if weight > 0 {
						inv := 1 / weight
						r, g, b = encode8(sum[0]*inv), encode8(sum[1]*inv), encode8(sum[2]*inv)
					}
					off := y*dst.Stride + x*4
					dst.Pix[off], dst.Pix[off+1], dst.Pix[off+2], dst.Pix[off+3] = r, g, b, 255
				}
			}
			levels[m] = dst
		}
		e.spec = levels
	})
	return e.spec
}
