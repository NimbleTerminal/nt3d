// Copyright (c) 2026 Neomantra Corp.

package demo

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/sh"
	"sync"
)

// Construct only when the lighting gallery uses the preset. Other commands
// neither build panoramas nor allocate their texture and lighting data.
var dayEnvironment = sync.OnceValue(func() *nt3d.Environment {
	return nt3d.GradientEnvironment(color.RGBA{72, 130, 220, 255}, color.RGBA{218, 231, 245, 255}, color.RGBA{115, 100, 77, 255})
})

// An original procedural panorama demonstrates the image API without an asset
// download: a warm broad panel opposite a cool blue sky. Either can be replaced
// with a decoded PNG or JPEG passed directly to NewEnvironment.
var imageEnvironment = sync.OnceValue(func() *nt3d.Environment {
	img := image.NewNRGBA(image.Rect(0, 0, 128, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 128; x++ {
			phi := ((float64(x)+.5)/128 - .5) * 2 * math.Pi
			z := math.Cos((float64(y) + .5) / 64 * math.Pi)
			warm := math.Max(0, math.Cos(phi)) * math.Sqrt(1-z*z)
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(65 + 185*warm), G: uint8(95 + 120*warm), B: uint8(145 + 70*z), A: 255})
		}
	}
	e, _ := nt3d.NewEnvironment(img)
	return e
})

// hdrEnvironment encodes a synthetic Radiance RGBE (.hdr) panorama with an
// unclamped solar disk (radiance ~12) and warm/cool studio sky, exercising
// NewHDREnvironment's RGBA16Float GGX specular mip chain, SH sky, and HDR bloom.
var hdrEnvironment = sync.OnceValue(func() *nt3d.Environment {
	const w, h = 128, 64
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y %d +X %d\n", h, w)
	rgbe := func(r, g, b float64) [4]byte {
		v := max(r, max(g, b))
		if v < 1e-32 {
			return [4]byte{}
		}
		m, exp := math.Frexp(v)
		s := m * 256 / v
		c := func(x float64) byte { return byte(max(0, min(255, int(x*s)))) }
		return [4]byte{c(r), c(g), c(b), byte(exp + 128)}
	}
	sunDir := nt3dmath.Vec3{X: .65, Y: -.45, Z: .61}.Normalize()
	for y := range h {
		theta := (float64(y) + .5) / h * math.Pi
		sinT, cosT := math.Sin(theta), math.Cos(theta)
		for x := range w {
			phi := ((float64(x)+.5)/w - .5) * 2 * math.Pi
			d := nt3dmath.Vec3{X: float32(sinT * math.Cos(phi)), Y: float32(sinT * math.Sin(phi)), Z: float32(cosT)}
			sky := max(0, float64(d.Z))
			warm := max(0, float64(d.X)) * sinT
			r := .08 + .28*warm + .06*(1-sky)
			g := .11 + .20*warm + .14*sky
			b := .18 + .08*warm + .42*sky
			if dot := float64(d.Dot(sunDir)); dot > .975 {
				s := math.Pow((dot-.975)/.025, 2)
				r += 12.0 * s
				g += 9.8 * s
				b += 6.8 * s
			}
			px := rgbe(r, g, b)
			buf.Write(px[:])
		}
	}
	e, err := nt3d.NewHDREnvironment(buf.Bytes())
	if err != nil {
		panic(err)
	}
	return e
})

// Encoded tangent vectors for a periodic relief pattern, with no image assets.
var reliefNormalMap = sync.OnceValue(func() *nt3d.Texture {
	img := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			u, v := (float64(x)+.5)/128*8*math.Pi, (float64(y)+.5)/128*8*math.Pi
			nx, ny := -.65*math.Cos(u)*math.Sin(v), -.65*math.Sin(u)*math.Cos(v)
			length := math.Sqrt(nx*nx + ny*ny + 1)
			enc := func(v float64) uint8 { return uint8((v*.5+.5)*255 + .5) }
			img.SetNRGBA(x, y, color.NRGBA{enc(nx / length), enc(ny / length), enc(1 / length), 255})
		}
	}
	tex, _ := nt3d.NewNormalMap(img)
	return tex
})

// Packed linear ORM map aligned with reliefNormalMap: R = ambient occlusion in
// valleys, G = smoother crests and rougher valleys, B = metallic crests.
var reliefORMMap = sync.OnceValue(func() *nt3d.Texture {
	img := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			u, v := (float64(x)+.5)/128*8*math.Pi, (float64(y)+.5)/128*8*math.Pi
			crest := .5 + .5*math.Sin(u)*math.Sin(v)
			ao := uint8(95 + 160*crest)
			rough := uint8(215 - 165*crest)
			metal := uint8(45 + 210*math.Pow(crest, 1.4))
			img.SetNRGBA(x, y, color.NRGBA{ao, rough, metal, 255})
		}
	}
	tex, _ := nt3d.NewORMMap(img)
	return tex
})

// Emissive inlay map: glowing filaments nestled in the recessed relief valleys.
var inlayEmissiveMap = sync.OnceValue(func() *nt3d.Texture {
	img := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			u, v := (float64(x)+.5)/128*8*math.Pi, (float64(y)+.5)/128*8*math.Pi
			valley := math.Max(0, -math.Sin(u)*math.Sin(v))
			glow := math.Pow(valley, 5)
			img.SetNRGBA(x, y, color.NRGBA{
				uint8(255 * glow),
				uint8(145 * glow),
				uint8(45 * glow),
				255,
			})
		}
	}
	tex, _ := nt3d.NewTexture(img)
	return tex
})

// Twelve rotations share a single projected analytic studio environment.
// The lights demo changes only the environment pointer when h is pressed.
var shEnvironments = sync.OnceValue(func() []*nt3d.Environment {
	radiance, err := nt3dsh.Project(2, func(d nt3dmath.Vec3) nt3dsh.RGB {
		warm := max(float32(0), d.X) * max(float32(0), d.X)
		cool := max(float32(0), -d.Y) * max(float32(0), -d.Y)
		return nt3dsh.RGB{.12 + 1.5*warm, .12 + .65*warm + .3*cool, .16 + .15*warm + 1.5*cool}
	})
	if err != nil {
		panic(err)
	}
	out := make([]*nt3d.Environment, 12)
	for i := range out {
		rotated, err := nt3dsh.Rotate(radiance, nt3dmath.RotationZ(float64(i)*math.Pi/6))
		if err != nil {
			panic(err)
		}
		out[i], err = nt3d.NewSHEnvironment(rotated)
		if err != nil {
			panic(err)
		}
	}
	return out
})
