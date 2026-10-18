// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"image"
	"image/color"
	"math"
	"nimbleterminal.dev/nt3d"
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
