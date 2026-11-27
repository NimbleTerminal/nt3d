// Copyright (c) 2026 Neomantra Corp.

package nt3dtiles3dscene

import (
	"math"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
)

// view is a camera as the selection asks of it: where the eye is, what is in
// sight, and how large a meter of error looks.
type view struct {
	eye    nt3dmath.Vec3d // in the scene's frame
	origin nt3dmath.Vec3d // the camera's Origin, which its matrix is relative to
	clip   nt3dmath.Mat4  // world, relative to origin, to clip space
	ortho  bool
	// perPixel turns meters of error into pixels: divided by the distance
	// for a perspective camera, as it is for an orthographic one.
	perPixel float64
}

func newView(c nt3d.Camera, width, height int) view {
	eye := c.WorldEye()
	v := view{
		eye:    nt3dmath.Vec3d{X: eye[0], Y: eye[1], Z: eye[2]},
		origin: nt3dmath.Vec3d{X: c.Origin[0], Y: c.Origin[1], Z: c.Origin[2]},
		clip:   c.Matrix(float32(width) / float32(height)),
		ortho:  c.Projection == nt3d.Orthographic,
	}
	if v.ortho {
		half := c.Ymag
		if !(c.Xmag > 0 && c.Ymag > 0) {
			half = c.Distance * .5
		}
		v.perPixel = float64(height) / (2 * half)
		return v
	}
	fov := c.FOV
	if fov <= 0 {
		fov = 45
	}
	v.perPixel = float64(height) / (2 * math.Tan(fov*math.Pi/360))
	return v
}

// screenSpaceError is how many pixels t's geometric error covers: the error
// seen at the nearest point of the tile's bounds, never at their middle, so
// a large tile the eye stands inside counts as near as it is. Inside the
// bounds a perspective camera sees the error as boundless.
func (v view) screenSpaceError(t *tile) float64 {
	if t.geometricError == 0 {
		return 0
	}
	if v.ortho {
		return t.geometricError * v.perPixel
	}
	distance := 0.0
	if !t.boxless {
		distance = t.box.Distance(v.eye)
	}
	if distance <= 0 {
		return math.Inf(1)
	}
	return t.geometricError * v.perPixel / distance
}

// outside reports whether box is wholly out of view: every corner beyond the
// same side of the view, or all of it behind a perspective camera. Near and
// far are not tested: a camera's clipping planes follow what it is shown.
func (v view) outside(box nt3dmath.OBBd) bool {
	m := v.clip
	var beyond [5]int
	for _, corner := range box.Corners() {
		p := corner.Sub(v.origin)
		x := float64(m[0])*p.X + float64(m[4])*p.Y + float64(m[8])*p.Z + float64(m[12])
		y := float64(m[1])*p.X + float64(m[5])*p.Y + float64(m[9])*p.Z + float64(m[13])
		w := float64(m[3])*p.X + float64(m[7])*p.Y + float64(m[11])*p.Z + float64(m[15])
		for k, out := range [5]bool{x < -w, x > w, y < -w, y > w, w <= 0} {
			if out {
				beyond[k]++
			}
		}
	}
	for _, n := range beyond {
		if n == 8 {
			return true
		}
	}
	return false
}
