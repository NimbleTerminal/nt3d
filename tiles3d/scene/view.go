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
	eye    nt3dmath.Vec3d // in the tileset's frame
	origin nt3dmath.Vec3d // the camera's Origin, which clip is relative to
	clip   nt3dmath.Mat4  // the scene's world, less origin, to clip space
	// place carries the tileset's frame, which tiles' bounds are kept in,
	// into the scene's world, where the camera is: the placement of the node
	// the tiles hang under. placed is false while it is the identity.
	place  nt3dmath.Mat4d
	placed bool
	scale  float64 // how much place stretches a length
	ortho  bool
	// perPixel turns meters of error into pixels: divided by the distance
	// for a perspective camera, as it is for an orthographic one.
	perPixel float64
}

func newView(c nt3d.Camera, width, height int, place nt3dmath.Mat4d) view {
	eye := c.WorldEye()
	// The matrix is taken of the camera without its Origin, as a frame's
	// is: Camera.Matrix places the eye at Origin itself, in float32, and
	// outside takes Origin off each corner in float64 before using it.
	// With Origin left in, it would be taken off twice.
	relative := c
	relative.Origin = [3]float64{}
	v := view{
		eye:    nt3dmath.Vec3d{X: eye[0], Y: eye[1], Z: eye[2]},
		origin: nt3dmath.Vec3d{X: c.Origin[0], Y: c.Origin[1], Z: c.Origin[2]},
		clip:   relative.Matrix(float32(width) / float32(height)),
		ortho:  c.Projection == nt3d.Orthographic,
		scale:  1,
	}
	// A parent that is moved, turned or scaled carries the tiles with it.
	// Their bounds stay in the tileset's frame: the eye is brought into that
	// frame to measure distance, and corners are carried out of it to be
	// tested against the view. A placement that cannot be undone, a scale of
	// zero, draws nothing whatever is chosen, and is left out.
	if inverse, ok := place.InverseAffine(); ok && place != nt3dmath.IdentityD() {
		v.place, v.placed, v.scale = place, true, largestScale(place)
		v.eye = inverse.Point(v.eye)
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
// bounds a perspective camera sees the error as boundless. Error and
// distance are both measured in the tileset's frame, so a parent that scales
// the tileset evenly changes neither's share of the other.
func (v view) screenSpaceError(t *tile) float64 {
	if t.geometricError == 0 {
		return 0
	}
	if v.ortho {
		return t.geometricError * v.scale * v.perPixel
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
		if v.placed {
			corner = v.place.Point(corner)
		}
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

// placement is the transform from the frame of node id to the scene's world,
// composed in float64 from each ancestor's transform and origin, as the scene
// composes a node's place.
func placement(scene *nt3d.Scene, id nt3d.NodeID) nt3dmath.Mat4d {
	m := nt3dmath.IdentityD()
	for range 1 << 16 { // A scene's tree is finite; the bound is a guard.
		n, ok := scene.Node(id)
		if !ok {
			break
		}
		local := n.Local.D()
		if origin, err := scene.NodeOrigin(id); err == nil {
			local[12] += origin[0]
			local[13] += origin[1]
			local[14] += origin[2]
		}
		m = local.Mul(m)
		if id == nt3d.Root || n.Parent == id {
			break
		}
		id = n.Parent
	}
	return m
}
