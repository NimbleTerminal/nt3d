// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"math"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func reflectEnvironment(rgb *[3]float32, environment *Environment, f0, strength float32, n, view nt3dmath.Vec3, linear bool) {
	if strength == 0 {
		strength = 1
	}
	// A surface seen from behind reflects like its front: a normal pointing
	// away from the viewer would mirror the sky at full strength.
	facing := n.Dot(view)
	if facing < 0 {
		n, facing = n.Scale(-1), -facing
	}
	reflected := n.Scale(2 * facing).Sub(view).Normalize()
	sky := environment.sample(reflected)
	fresnel := f0 + (1-f0)*float32(math.Pow(float64(1-min(float32(1), facing)), 5))
	for k := range rgb {
		c := sky[k]
		if !linear {
			c = float32(encodeSRGB(float64(c)))
		}
		rgb[k] = rgb[k]*(1-fresnel) + c*strength*255*fresnel
	}
}
