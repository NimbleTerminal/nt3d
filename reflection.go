// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"math"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func reflectEnvironment(rgb *[3]float32, environment *Environment, f0, strength float32, n, view nt3dmath.Vec3, linear bool) {
	reflectEnvironmentAO(rgb, environment, f0, strength, n, view, linear, 1)
}

func reflectEnvironmentAO(rgb *[3]float32, environment *Environment, f0, strength float32, n, view nt3dmath.Vec3, linear bool, ao float32) {
	f := Frame{Environment: environment, EnvironmentStrength: strength, Linear: linear}
	reflectSceneEnvironment(rgb, &f, nt3dmath.Vec3{}, f0, n, view, true, ao)
}

func reflectSceneEnvironment(rgb *[3]float32, f *Frame, point nt3dmath.Vec3, f0 float32, n, view nt3dmath.Vec3, environmentReflect bool, ao float32) {
	// A surface seen from behind reflects like its front: a normal pointing
	// away from the viewer would mirror the sky at full strength.
	facing := n.Dot(view)
	if facing < 0 {
		n, facing = n.Scale(-1), -facing
	}
	reflected := n.Scale(2 * facing).Sub(view).Normalize()
	sample := sceneIndirectReflection(f, point, reflected, environmentReflect, ao)
	fresnel := f0 + (1-f0)*float32(math.Pow(float64(1-min(float32(1), facing)), 5))
	for k := range rgb {
		rgb[k] = rgb[k]*(1-fresnel*sample.coverage) + sample.radiance[k]*255*fresnel
	}
}
