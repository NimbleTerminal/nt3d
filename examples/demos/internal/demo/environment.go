// Copyright (c) 2026 Neomantra Corp.
package demo

import (
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
