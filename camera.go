// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"math"

	"nimbleterminal.dev/nt3d/math"
)

type Projection uint8

const (
	Orthographic Projection = iota
	Perspective
)

func (p Projection) String() string {
	if p == Perspective {
		return "perspective"
	}
	return "ortho"
}

// Camera uses degrees for Alpha (elevation) and Beta (azimuth). Distance and
// Target use scene world units. The default camera suits a scene centered
// at the origin with a longest extent of roughly two units; Fit frames any
// other. Unless explicitly overridden, clipping planes scale with Distance,
// so a scene in millimeters or meters draws as well as one in units.
type Camera struct {
	// Pose optionally supplies a camera-to-world transform: local -Z is forward,
	// +Y is up. Zero retains the orbit camera. Scale is ignored.
	Pose nt3dmath.Mat4
	// YFOV is vertical perspective FOV in radians; zero means pi/4.
	YFOV float64
	// AspectRatio overrides viewport aspect when positive.
	AspectRatio float64
	// Near and Far override automatic clipping when either is positive. Far == 0
	// then means infinite perspective far plane.
	Near, Far float64
	// Xmag and Ymag are orthographic half extents; both zero use Distance.
	Xmag, Ymag            float64
	Alpha, Beta, Distance float64
	Target                nt3dmath.Vec3
	Projection            Projection
	AutoRotate            bool
	ResumeAfter           float64 // idle seconds before rotation resumes after input
}

// Distance is kept within this range. Near and far planes are this fraction
// and multiple of it: at the default distance, 0.01 and 200 units.
const (
	minDistance, maxDistance = 1e-4, 1e7
	nearPlane, farPlane      = .01 / 3.4, 200 / 3.4
	// halfFOV is half the perspective camera's vertical field of view.
	halfFOV = math.Pi / 8
)

// DefaultCamera returns a Camera initialized with some typical default values.
func DefaultCamera() Camera {
	return Camera{Alpha: 25, Beta: 40, Distance: 3.4, ResumeAfter: 2}
}

func (c Camera) valid() Camera {
	if !nt3dmath.IsFinite(c.Alpha) {
		c.Alpha = 25
	}
	if !nt3dmath.IsFinite(c.Beta) {
		c.Beta = 40
	}
	c.Alpha = math.Max(-89, math.Min(89, c.Alpha))
	c.Beta = math.Mod(c.Beta, 360)
	if !nt3dmath.IsFinite(c.Distance) || c.Distance <= 0 {
		c.Distance = 3.4
	}
	c.Distance = math.Max(minDistance, math.Min(maxDistance, c.Distance))
	if !nt3dmath.IsFinite(float64(c.Target.X)) || !nt3dmath.IsFinite(float64(c.Target.Y)) || !nt3dmath.IsFinite(float64(c.Target.Z)) {
		c.Target = nt3dmath.Vec3{}
	}
	if c.Projection != Perspective {
		c.Projection = Orthographic
	}
	if !nt3dmath.IsFinite(c.ResumeAfter) || c.ResumeAfter <= 0 {
		c.ResumeAfter = 2
	}
	if _, ok := c.Pose.InverseAffine(); c.Pose != (nt3dmath.Mat4{}) && (!ok || c.Pose.Determinant3() <= 0) {
		c.Pose = nt3dmath.Mat4{}
	}
	if !nt3dmath.IsFinite(c.YFOV) || c.YFOV < 0 || c.YFOV >= math.Pi {
		c.YFOV = 0
	}
	if !nt3dmath.IsFinite(c.AspectRatio) || c.AspectRatio < 0 {
		c.AspectRatio = 0
	}
	if !nt3dmath.IsFinite(c.Near) || !nt3dmath.IsFinite(c.Far) || c.Near < 0 || c.Far < 0 || c.Far > 0 && c.Far <= c.Near {
		c.Near, c.Far = 0, 0
	}
	if !nt3dmath.IsFinite(c.Xmag) || !nt3dmath.IsFinite(c.Ymag) || c.Xmag <= 0 || c.Ymag <= 0 {
		c.Xmag, c.Ymag = 0, 0
	}
	return c
}

func (c Camera) halfFOV() float64 {
	if c.YFOV > 0 {
		return c.YFOV / 2
	}
	return halfFOV
}
func (c Camera) extents(aspect float32) (float32, float32) {
	if c.AspectRatio > 0 {
		aspect = float32(c.AspectRatio)
	}
	if c.Xmag > 0 && c.Ymag > 0 {
		return float32(c.Xmag), float32(c.Ymag)
	}
	h := float32(c.Distance * .5)
	return h * aspect, h
}

func (c Camera) basis() (eye, right, up, forward nt3dmath.Vec3) {
	c = c.valid()
	if c.Pose != (nt3dmath.Mat4{}) {
		eye = c.Pose.Point(nt3dmath.Vec3{})
		forward = c.Pose.Direction(nt3dmath.Vec3{Z: -1}).Normalize()
		right = forward.Cross(c.Pose.Direction(nt3dmath.Vec3{Y: 1})).Normalize()
		up = right.Cross(forward)
		return
	}
	a, b := c.Alpha*math.Pi/180, c.Beta*math.Pi/180
	eye = c.Target.Add(nt3dmath.Vec3{X: float32(c.Distance * math.Cos(a) * math.Cos(b)), Y: float32(c.Distance * math.Cos(a) * math.Sin(b)), Z: float32(c.Distance * math.Sin(a))})
	forward = c.Target.Sub(eye).Normalize()
	right = forward.Cross(nt3dmath.Vec3{Z: 1}).Normalize()
	up = right.Cross(forward)
	return
}

func (c Camera) Matrix(aspect float32) nt3dmath.Mat4 {
	c = c.valid()
	if aspect <= 0 {
		aspect = 1
	}
	if c.AspectRatio > 0 {
		aspect = float32(c.AspectRatio)
	}
	e, r, u, f := c.basis()
	view := nt3dmath.Mat4{r.X, u.X, -f.X, 0, r.Y, u.Y, -f.Y, 0, r.Z, u.Z, -f.Z, 0, -r.Dot(e), -u.Dot(e), f.Dot(e), 1}
	near, far := float32(c.Distance*nearPlane), float32(c.Distance*farPlane)
	if c.Near > 0 || c.Far > 0 {
		near, far = float32(c.Near), float32(c.Far)
	}
	var p nt3dmath.Mat4
	if c.Projection == Perspective {
		q := float32(1 / math.Tan(c.halfFOV()))
		p[0] = q / aspect
		p[5] = q
		p[10] = far / (near - far)
		p[11] = -1
		p[14] = near * far / (near - far)
		if c.Near > 0 && c.Far == 0 {
			p[10], p[14] = -1, -near
		}
	} else {
		w, h := c.extents(aspect)
		p = nt3dmath.Identity()
		p[0] = 1 / w
		p[5] = 1 / h
		p[10] = 1 / (near - far)
		p[14] = near / (near - far)
	}
	return p.Mul(view)
}

// RayFromCell returns a ray through a cell center, accounting for cell dimensions.
func (c Camera) RayFromCell(x, y, cols, rows, cellW, cellH int) nt3dmath.Ray {
	return c.ray(float32(x)+.5, float32(y)+.5, cols, rows, cellW, cellH)
}

func (c Camera) ray(x, y float32, cols, rows, cw, ch int) nt3dmath.Ray {
	c = c.valid()
	e, r, u, f := c.basis()
	if cols < 1 || rows < 1 || cw < 1 || ch < 1 {
		return nt3dmath.Ray{Origin: e, Direction: f}
	}
	nx, ny := 2*x/float32(cols)-1, 1-2*y/float32(rows)
	aspect := float32(cols*cw) / float32(rows*ch)
	if c.AspectRatio > 0 {
		aspect = float32(c.AspectRatio)
	}
	if c.Projection == Perspective {
		h := float32(math.Tan(c.halfFOV()))
		return nt3dmath.Ray{Origin: e, Direction: f.Add(r.Scale(nx * aspect * h).Add(u.Scale(ny * h))).Normalize()}
	}
	width, h := c.extents(aspect)
	return nt3dmath.Ray{Origin: e.Add(r.Scale(nx * width).Add(u.Scale(ny * h))), Direction: f}
}

// Valid returns a sanitized camera.
func (c Camera) Valid() Camera { return c.valid() }

// Orbit returns the camera rotated by azimuth and elevation degrees.
func (c Camera) Orbit(azimuth, elevation float64) Camera {
	c.Pose = nt3dmath.Mat4{}
	c.Beta += azimuth
	c.Alpha += elevation
	return c.valid()
}

// Zoom multiplies distance by factor; invalid factors leave the camera unchanged.
func (c Camera) Zoom(factor float64) Camera {
	if factor > 0 && nt3dmath.IsFinite(factor) {
		if c.Pose != (nt3dmath.Mat4{}) {
			e := c.Target.Add(c.Eye().Sub(c.Target).Scale(float32(factor)))
			c.Pose[12], c.Pose[13], c.Pose[14] = e.X, e.Y, e.Z
		}
		if c.Xmag > 0 {
			c.Xmag *= factor
			c.Ymag *= factor
		}
		c.Distance *= factor
	}
	return c.valid()
}

// Pan moves the target by viewport pixels. Positive dx/dy follow a dragged scene.
func (c Camera) Pan(dx, dy float32, width, height int) Camera {
	if width < 1 || height < 1 || !validFloat(dx, dy) {
		return c
	}
	c = c.valid()
	_, r, u, _ := c.basis()
	aspect := float32(width) / float32(height)
	if c.AspectRatio > 0 {
		aspect = float32(c.AspectRatio)
	}
	w, h := c.extents(aspect)
	if c.Projection == Perspective {
		h = float32(c.Distance * math.Tan(c.halfFOV()))
		w = h * aspect
	}
	delta := r.Scale(-2 * dx * w / float32(width)).Add(u.Scale(2 * dy * h / float32(height)))
	c.Target = c.Target.Add(delta)
	if c.Pose != (nt3dmath.Mat4{}) {
		c.Pose[12] += delta.X
		c.Pose[13] += delta.Y
		c.Pose[14] += delta.Z
	}
	return c.valid()
}

// RayFromPixel returns a world-space ray through a pixel coordinate (not centered).
func (c Camera) RayFromPixel(x, y float32, width, height int) nt3dmath.Ray {
	return c.ray(x, y, width, height, 1, 1)
}

// Step advances automatic orbit using an explicit elapsed and idle duration in seconds.
func (c Camera) Step(elapsed, idle float64) Camera {
	c = c.valid()
	if c.AutoRotate && idle >= c.ResumeAfter && elapsed > 0 && nt3dmath.IsFinite(elapsed) {
		c = c.Orbit(30*elapsed, 0)
	}
	return c.valid()
}

// Fit returns the camera aimed at the center of b from the same direction
// and projection, at the distance that shows all of b in a view of the given
// aspect (width over height): its bounding sphere just fits the narrower
// side. An invalid box or aspect leaves the camera unchanged.
func (c Camera) Fit(b nt3dmath.AABB, aspect float64) Camera {
	c = c.valid()
	if !b.IsValid() || !(aspect > 0) || math.IsInf(aspect, 0) {
		return c
	}
	center := b.Min.Add(b.Max).Scale(.5)
	diagonal := b.Max.Sub(b.Min)
	radius := .5 * math.Sqrt(float64(diagonal.Dot(diagonal)))
	if !nt3dmath.IsFinite(radius) || !validFloat(center.X, center.Y, center.Z) {
		return c
	}
	c.Pose = nt3dmath.Mat4{}
	c.Xmag, c.Ymag = 0, 0
	if c.AspectRatio > 0 {
		aspect = c.AspectRatio
	}
	radius = math.Max(radius, minDistance)
	narrow := math.Min(1, aspect)
	if c.Projection == Perspective {
		c.Distance = radius / math.Sin(math.Atan(math.Tan(c.halfFOV())*narrow))
	} else {
		// The orthographic view is Distance tall.
		c.Distance = 2 * radius / narrow
	}
	c.Target = center
	return c.valid()
}

// LookAt returns the camera placed at eye and aimed at target, keeping its
// projection. Elevation is limited to 89° either way, so a camera straight
// above or below its target is tilted slightly. Equal points leave the
// camera unchanged.
func (c Camera) LookAt(eye, target nt3dmath.Vec3) Camera {
	d := eye.Sub(target)
	distance := math.Sqrt(float64(d.Dot(d)))
	if !(distance > 0) || !nt3dmath.IsFinite(distance) || !validFloat(target.X, target.Y, target.Z) {
		return c.valid()
	}
	c.Pose = nt3dmath.Mat4{}
	c.Target, c.Distance = target, distance
	c.Alpha = math.Asin(float64(d.Z)/distance) * 180 / math.Pi
	c.Beta = math.Atan2(float64(d.Y), float64(d.X)) * 180 / math.Pi
	return c.valid()
}

// Eye returns the camera's position in world space.
func (c Camera) Eye() nt3dmath.Vec3 {
	eye, _, _, _ := c.valid().basis()
	return eye
}
