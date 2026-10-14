// Copyright (c) 2026 Neomantra Corp.
package nt3d

// clipCorner is a triangle corner in clip space with what is interpolated
// across it: its lit color and texture coordinate.
type clipCorner struct {
	x, y, z, w float32
	shade      [3]float32
	uv         UV
	cut        float32 // A cutout's threshold here.
}

func lerpCorner(a, b clipCorner, t float32) clipCorner {
	mix := func(x, y float32) float32 { return x + (y-x)*t }
	return clipCorner{mix(a.x, b.x), mix(a.y, b.y), mix(a.z, b.z), mix(a.w, b.w),
		[3]float32{mix(a.shade[0], b.shade[0]), mix(a.shade[1], b.shade[1]), mix(a.shade[2], b.shade[2])},
		UV{mix(a.uv.U, b.uv.U), mix(a.uv.V, b.uv.V)}, mix(a.cut, b.cut)}
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
