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
// at the origin with a longest extent of roughly two units.
type Camera struct {
	Alpha, Beta, Distance float64
	Target                nt3dmath.Vec3
	Projection            Projection
	AutoRotate            bool
	ResumeAfter           float64 // idle seconds before rotation resumes after input
}

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
	c.Distance = math.Max(.15, math.Min(100, c.Distance))
	if !nt3dmath.IsFinite(float64(c.Target.X)) || !nt3dmath.IsFinite(float64(c.Target.Y)) || !nt3dmath.IsFinite(float64(c.Target.Z)) {
		c.Target = nt3dmath.Vec3{}
	}
	if c.Projection != Perspective {
		c.Projection = Orthographic
	}
	if !nt3dmath.IsFinite(c.ResumeAfter) || c.ResumeAfter <= 0 {
		c.ResumeAfter = 2
	}
	return c
}

func (c Camera) basis() (eye, right, up, forward nt3dmath.Vec3) {
	c = c.valid()
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
	e, r, u, f := c.basis()
	view := nt3dmath.Mat4{r.X, u.X, -f.X, 0, r.Y, u.Y, -f.Y, 0, r.Z, u.Z, -f.Z, 0, -r.Dot(e), -u.Dot(e), f.Dot(e), 1}
	near, far := float32(.01), float32(200)
	var p nt3dmath.Mat4
	if c.Projection == Perspective {
		q := float32(1 / math.Tan(math.Pi/8))
		p[0] = q / aspect
		p[5] = q
		p[10] = far / (near - far)
		p[11] = -1
		p[14] = near * far / (near - far)
	} else {
		h := float32(c.Distance * .5)
		p = nt3dmath.Identity()
		p[0] = 1 / (h * aspect)
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
	if c.Projection == Perspective {
		h := float32(math.Tan(math.Pi / 8))
		return nt3dmath.Ray{Origin: e, Direction: f.Add(r.Scale(nx * aspect * h).Add(u.Scale(ny * h))).Normalize()}
	}
	h := float32(c.Distance * .5)
	return nt3dmath.Ray{Origin: e.Add(r.Scale(nx * aspect * h).Add(u.Scale(ny * h))), Direction: f}
}

// Valid returns a sanitized camera.
func (c Camera) Valid() Camera { return c.valid() }

// Orbit returns the camera rotated by azimuth and elevation degrees.
func (c Camera) Orbit(azimuth, elevation float64) Camera {
	c.Beta += azimuth
	c.Alpha += elevation
	return c.valid()
}

// Zoom multiplies distance by factor; invalid factors leave the camera unchanged.
func (c Camera) Zoom(factor float64) Camera {
	if factor > 0 && nt3dmath.IsFinite(factor) {
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
	h := float32(c.Distance)
	if c.Projection == Perspective {
		h *= float32(2 * math.Tan(math.Pi/8))
	}
	c.Target = c.Target.Add(r.Scale(-dx * h / float32(height))).Add(u.Scale(dy * h / float32(height)))
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
		c.Beta += 30 * elapsed
	}
	return c.valid()
}
