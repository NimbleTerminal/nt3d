// Copyright (c) 2026 Neomantra Corp.
package nt3d

import "nimbleterminal.dev/nt3d/math"

// clipCorner is a triangle corner in clip space with what is interpolated
// across it: its lit color, texture coordinate, and the world position and
// normal that highlights and fog need.
type clipCorner struct {
	x, y, z, w    float32
	shade         [3]float32
	uv            UV
	normalUV      UV
	cut           float32 // A cutout's threshold here.
	world, normal nt3dmath.Vec3
	tangent       nt3dmath.Vec3 // World-space tangent frame, when the mesh has one:
	tangentW      float32       // its handedness, zero without.
}

func lerpCorner(a, b clipCorner, t float32) clipCorner {
	mix := func(x, y float32) float32 { return x + (y-x)*t }
	vec := func(x, y nt3dmath.Vec3) nt3dmath.Vec3 { return x.Add(y.Sub(x).Scale(t)) }
	return clipCorner{mix(a.x, b.x), mix(a.y, b.y), mix(a.z, b.z), mix(a.w, b.w),
		[3]float32{mix(a.shade[0], b.shade[0]), mix(a.shade[1], b.shade[1]), mix(a.shade[2], b.shade[2])},
		UV{mix(a.uv.U, b.uv.U), mix(a.uv.V, b.uv.V)}, UV{mix(a.normalUV.U, b.normalUV.U), mix(a.normalUV.V, b.normalUV.V)}, mix(a.cut, b.cut), vec(a.world, b.world), vec(a.normal, b.normal), vec(a.tangent, b.tangent), mix(a.tangentW, b.tangentW)}
}

// clipNear cuts a triangle at the near plane, z = 0 in WebGPU's depth range,
// as the GPU does: what is nearer is discarded. Attributes are affine in clip
// space, so cut edges interpolate them linearly. It returns 0, 3, or 4
// corners, wound as the triangle was, in out's storage.
func clipNear(in [3]clipCorner, out *[4]clipCorner) []clipCorner {
	if in[0].z >= 0 && in[1].z >= 0 && in[2].z >= 0 {
		*out = [4]clipCorner{in[0], in[1], in[2]}
		return out[:3]
	}
	n := 0
	previous := in[2]
	for _, current := range in {
		if (previous.z >= 0) != (current.z >= 0) {
			out[n] = lerpCorner(previous, current, previous.z/(previous.z-current.z))
			out[n].z = 0
			n++
		}
		if current.z >= 0 {
			out[n] = current
			n++
		}
		previous = current
	}
	return out[:n]
}

// facing reports whether a clip-space triangle before the eye winds
// counter-clockwise on screen, as front faces do.
func facing(a, b, c *clipCorner) bool {
	x0, y0 := a.x/a.w, a.y/a.w
	return (b.x/b.w-x0)*(c.y/c.w-y0)-(b.y/b.w-y0)*(c.x/c.w-x0) > 1e-12
}
