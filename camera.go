// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"errors"
	"fmt"
	"math"

	"nimbleterminal.dev/nt3d/math"
)

// Projection selects orthographic or perspective camera projection.
type Projection uint8

// Supported camera projections.
const (
	Orthographic Projection = iota
	Perspective
)

// String returns "perspective" or "ortho".
func (p Projection) String() string {
	if p == Perspective {
		return "perspective"
	}
	return "ortho"
}

// Camera's angles are in degrees: Elevation, Azimuth, and FOV, as every angle
// set on a camera or a light is. (Package nt3dmath's functions take radians,
// as Go's math does.) Distance and Target use scene world units. The default
// camera suits a scene centered at the origin with a longest extent of
// roughly two units; Fit frames any other. Unless explicitly overridden,
// clipping planes scale with Distance, so a scene in millimeters or meters
// draws as well as one in units.
//
// A zero field means its default, and the orbit values are held to their
// limits, so a drag may add to them freely. Anything else that cannot mean
// what it says is an error, which Validate reports and a scene returns when
// it is given the camera.
type Camera struct {
	// Origin is an optional float64 world-space reference offset (e.g. ENU/UTM
	// meters) added to the local Pose or Target/orbit camera position. Scene.Frame
	// and Scene.Pick subtract Origin in float64 from node Origins and light
	// positions before converting transforms to float32, preventing precision
	// jitter at geographic scales.
	Origin [3]float64
	// Pose optionally supplies a camera-to-world transform (relative to Origin):
	// local -Z is forward, +Y is up. Zero retains the orbit camera. Scale is ignored.
	Pose nt3dmath.Mat4
	// FOV is a perspective camera's vertical field of view, in degrees, below
	// 180; zero means 45.
	FOV float64
	// AspectRatio overrides viewport aspect when positive.
	AspectRatio float64
	// Near and Far independently override automatic clipping when positive.
	// Near > 0 with Far == 0 means an infinite perspective far plane; an
	// orthographic camera keeps its automatic far plane, moved out beyond
	// Near when Near reaches it. Orthographic cameras with explicit Far allow
	// Near == 0.
	Near, Far float64
	// Xmag and Ymag are orthographic half extents; both zero use Distance.
	Xmag, Ymag float64
	// Elevation is the angle of the orbit camera above the horizon, in
	// degrees, kept within ±89. Azimuth is its angle around the Z axis, in
	// degrees, counted from +X toward +Y. Distance is how far it is from
	// Target, in world units.
	Elevation, Azimuth, Distance float64
	Target                       nt3dmath.Vec3
	Projection                   Projection
	AutoRotate                   bool
	ResumeAfter                  float64 // idle seconds before rotation resumes after input
}

// Distance is kept within this range. Near and far planes are this fraction
// and multiple of it: at the default distance, 0.05 and 200 units.
const (
	minDistance, maxDistance = 1e-4, 1e7
	nearPlane, farPlane      = .05 / 3.4, 200 / 3.4
	// halfFOV is half the perspective camera's vertical field of view.
	halfFOV = math.Pi / 8
)

// DefaultCamera returns a Camera initialized with some typical default values.
func DefaultCamera() Camera {
	return Camera{Elevation: 25, Azimuth: 40, Distance: 3.4, ResumeAfter: 2}
}

// Validate reports a field whose value cannot mean anything: a number that is
// not finite, a negative where only a size makes sense, a FOV of 180 or more,
// a Far at or before Near, only one of Xmag and Ymag, an unknown Projection,
// a Pose that is flattened or mirrored, or a lens value so large or small
// that the projection, which is computed in float32, does not come out
// finite. Scene.Frame, Scene.StillFrame, and
// Scene.SetNodeCamera return its error, as they do for a geometry that fails
// its own Validate.
//
// Two things a camera corrects on purpose are not errors. A zero means the
// default (Distance, FOV, clipping, extents, ResumeAfter). And orbit values
// past their limits are held to them: Elevation within ±89 degrees, Azimuth
// within a turn, Distance within what can be drawn, which is what lets a
// drag add to them freely.
func (c Camera) Validate() error {
	for _, f := range []struct {
		name     string
		value    float64
		negative bool // Whether a negative value means something.
	}{
		{"Elevation", c.Elevation, true}, {"Azimuth", c.Azimuth, true},
		{"Distance", c.Distance, false}, {"FOV", c.FOV, false}, {"AspectRatio", c.AspectRatio, false},
		{"Near", c.Near, false}, {"Far", c.Far, false}, {"Xmag", c.Xmag, false}, {"Ymag", c.Ymag, false},
		{"ResumeAfter", c.ResumeAfter, false},
		{"Target", float64(c.Target.X), true}, {"Target", float64(c.Target.Y), true}, {"Target", float64(c.Target.Z), true},
		{"Origin", c.Origin[0], true}, {"Origin", c.Origin[1], true}, {"Origin", c.Origin[2], true},
	} {
		if !nt3dmath.IsFinite(f.value) {
			return fmt.Errorf("nt3d: camera %s is not finite", f.name)
		}
		if f.value < 0 && !f.negative {
			return fmt.Errorf("nt3d: camera %s is negative: %g", f.name, f.value)
		}
	}
	// The projection is computed in float32, where a value that is finite as
	// a float64 can still be zero or infinite.
	for _, f := range []struct {
		name  string
		value float64
	}{{"FOV", c.FOV}, {"AspectRatio", c.AspectRatio}, {"Near", c.Near}, {"Far", c.Far}, {"Xmag", c.Xmag}, {"Ymag", c.Ymag}} {
		if v := float32(f.value); f.value != 0 && (v == 0 || math.IsInf(float64(v), 0)) {
			return fmt.Errorf("nt3d: camera %s is outside what a float32 projection holds: %g", f.name, f.value)
		}
	}
	switch {
	case c.FOV >= 180:
		return fmt.Errorf("nt3d: camera FOV must be below 180 degrees: %g", c.FOV)
	case c.Far > 0 && float32(c.Far) <= float32(c.Near): // As the projection sees them.
		return fmt.Errorf("nt3d: camera Far (%g) must be beyond Near (%g)", c.Far, c.Near)
	case (c.Xmag > 0) != (c.Ymag > 0):
		return errors.New("nt3d: camera Xmag and Ymag must be set together")
	case c.Projection > Perspective:
		return fmt.Errorf("nt3d: unknown camera Projection %d", c.Projection)
	}
	if c.Pose != (nt3dmath.Mat4{}) {
		for _, v := range c.Pose {
			if !nt3dmath.IsFinite(float64(v)) {
				return errors.New("nt3d: camera Pose is not finite")
			}
		}
		if _, ok := c.Pose.InverseAffine(); !ok {
			return errors.New("nt3d: camera Pose is flattened and has no inverse")
		}
		if c.Pose.Determinant3() <= 0 {
			return errors.New("nt3d: camera Pose is mirrored")
		}
	}
	// Whatever the checks above let through in combination, the projection
	// and the view themselves have to come out usable.
	k := c.corrected() // Defaults filled in; the checks above leave nothing else to correct.
	if !usable(k.projection(1)) {
		return errors.New("nt3d: camera projection is out of range: FOV, AspectRatio, Near, Far, Xmag, or Ymag is too large or too small together")
	}
	if !k.placed() {
		return errors.New("nt3d: camera Target, Distance, or Pose puts it too far out for float32 coordinates; use Origin for large ones")
	}
	return nil
}

// cameraLimit is the largest magnitude an entry of a camera's view or
// projection matrix may have: far past any use, and small enough that the
// two multiply, and scale by a viewport's aspect, without overflowing
// float32.
const cameraLimit = 1e15

// usable reports whether a view or projection matrix is finite and within
// cameraLimit.
func usable(m nt3dmath.Mat4) bool {
	for _, v := range m {
		if !(v >= -cameraLimit && v <= cameraLimit) { // False for NaN, too.
			return false
		}
	}
	return true
}

// inFloat32 reports whether a lens value survives conversion to float32,
// where the projection is computed: zero stays zero, and nothing else becomes
// zero or infinite.
func inFloat32(v float64) bool {
	f := float32(v)
	return v == 0 || f != 0 && !math.IsInf(float64(f), 0)
}

// valid is the camera as it is used: defaults filled in and orbit values held
// to their limits. It also replaces what Validate refuses with a default, so
// that the methods here that cannot return an error never compute with it,
// and so that Validate accepts whatever it returns.
func (c Camera) valid() Camera {
	c = c.corrected()
	// Values that each pass can still combine into a projection or a view
	// that does not come out usable; then the lens or the placement as a
	// whole falls back to its default, which always does.
	if !usable(c.projection(1)) {
		c.FOV, c.AspectRatio, c.Near, c.Far, c.Xmag, c.Ymag = 0, 0, 0, 0, 0, 0
	}
	if !c.placed() {
		c.Pose, c.Target = nt3dmath.Mat4{}, nt3dmath.Vec3{}
	}
	return c
}

// placed reports whether a camera already corrected is somewhere its view
// matrix can express, facing some way: far enough out, the orbit distance
// vanishes into the target's float32 coordinates, the eye lands on the
// target, and no direction is left to look in. Origin is left out: it is a
// float64 offset that Scene.Frame takes off the camera and the nodes alike
// before either becomes float32, so a large one is no fault of the camera's,
// and is how to place a camera that far out.
func (c Camera) placed() bool {
	c.Origin = [3]float64{}
	v := c.view()
	return usable(v) && math.Abs(float64(v.Determinant3())-1) < .01 // A rotation, not a collapse.
}

// corrected applies valid's corrections field by field.
func (c Camera) corrected() Camera {
	if !nt3dmath.IsFinite(c.Elevation) {
		c.Elevation = 25
	}
	if !nt3dmath.IsFinite(c.Azimuth) {
		c.Azimuth = 40
	}
	c.Elevation = math.Max(-89, math.Min(89, c.Elevation))
	c.Azimuth = math.Mod(c.Azimuth, 360)
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
	if !nt3dmath.IsFinite(c.FOV) || c.FOV < 0 || c.FOV >= 180 || !inFloat32(c.FOV) {
		c.FOV = 0
	}
	if !nt3dmath.IsFinite(c.AspectRatio) || c.AspectRatio < 0 || !inFloat32(c.AspectRatio) {
		c.AspectRatio = 0
	}
	if !nt3dmath.IsFinite(c.Near) || !nt3dmath.IsFinite(c.Far) || c.Near < 0 || c.Far < 0 || !inFloat32(c.Near) || !inFloat32(c.Far) ||
		c.Far > 0 && float32(c.Far) <= float32(c.Near) { // As the projection sees them.
		c.Near, c.Far = 0, 0
	}
	if !nt3dmath.IsFinite(c.Xmag) || !nt3dmath.IsFinite(c.Ymag) || c.Xmag <= 0 || c.Ymag <= 0 || !inFloat32(c.Xmag) || !inFloat32(c.Ymag) {
		c.Xmag, c.Ymag = 0, 0
	}
	if !nt3dmath.IsFinite(c.Origin[0]) || !nt3dmath.IsFinite(c.Origin[1]) || !nt3dmath.IsFinite(c.Origin[2]) {
		c.Origin = [3]float64{}
	}
	return c
}

func (c Camera) relativeTo(origin [3]float64) Camera {
	c = c.valid()
	if !nt3dmath.IsFinite(origin[0]) || !nt3dmath.IsFinite(origin[1]) || !nt3dmath.IsFinite(origin[2]) {
		return c
	}
	c.Origin = [3]float64{
		c.Origin[0] - origin[0],
		c.Origin[1] - origin[1],
		c.Origin[2] - origin[2],
	}
	return c
}

func (c Camera) halfFOV() float64 {
	if c.FOV > 0 {
		return c.FOV * math.Pi / 360
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
	return c.valid().rawBasis()
}

// rawBasis is basis for a camera already corrected.
func (c Camera) rawBasis() (eye, right, up, forward nt3dmath.Vec3) {
	if c.Pose != (nt3dmath.Mat4{}) {
		eye = c.Pose.Point(nt3dmath.Vec3{})
		forward = c.Pose.Direction(nt3dmath.Vec3{Z: -1}).Normalize()
		right = forward.Cross(c.Pose.Direction(nt3dmath.Vec3{Y: 1})).Normalize()
		up = right.Cross(forward)
	} else {
		a, b := c.Elevation*math.Pi/180, c.Azimuth*math.Pi/180
		eye = c.Target.Add(nt3dmath.Vec3{X: float32(c.Distance * math.Cos(a) * math.Cos(b)), Y: float32(c.Distance * math.Cos(a) * math.Sin(b)), Z: float32(c.Distance * math.Sin(a))})
		forward = c.Target.Sub(eye).Normalize()
		right = forward.Cross(nt3dmath.Vec3{Z: 1}).Normalize()
		up = right.Cross(forward)
	}
	if c.Origin != ([3]float64{}) {
		eye = eye.Add(nt3dmath.Vec3{X: float32(c.Origin[0]), Y: float32(c.Origin[1]), Z: float32(c.Origin[2])})
	}
	return
}

// Matrix returns the world-to-clip view-projection matrix for the given
// viewport aspect ratio (width over height), using [0, 1] clip depth.
func (c Camera) Matrix(aspect float32) nt3dmath.Mat4 {
	c = c.valid()
	return c.projection(aspect).Mul(c.view())
}

// view is the world-to-eye matrix of a camera already corrected.
func (c Camera) view() nt3dmath.Mat4 {
	e, r, u, f := c.rawBasis()
	return nt3dmath.Mat4{r.X, u.X, -f.X, 0, r.Y, u.Y, -f.Y, 0, r.Z, u.Z, -f.Z, 0, -r.Dot(e), -u.Dot(e), f.Dot(e), 1}
}

// projection is the eye-to-clip matrix of a camera already corrected, for a
// viewport of the given aspect.
func (c Camera) projection(aspect float32) nt3dmath.Mat4 {
	if aspect <= 0 {
		aspect = 1
	}
	if c.AspectRatio > 0 {
		aspect = float32(c.AspectRatio)
	}
	near, far := float32(c.Distance*nearPlane), float32(c.Distance*farPlane)
	if c.Near > 0 {
		near = float32(c.Near)
	}
	if c.Far > 0 {
		far = float32(c.Far)
		if c.Projection == Orthographic {
			near = float32(c.Near)
		}
		// A far-only override can be closer than the automatic near plane.
		if c.Near == 0 && near >= far {
			near = far * float32(nearPlane/farPlane)
		}
	}
	// A near-only override can be at or beyond the automatic far plane, which
	// would leave no depth: keep the automatic depth beyond it. (A perspective
	// camera with only Near has no far plane at all.)
	if c.Far == 0 && near >= far {
		far += near
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
	return p
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

// Valid returns the camera as it is used: zeros replaced by their defaults
// and orbit values held to their limits. It does not report a field that
// cannot mean anything, and replaces it with a default; call Validate for
// that.
func (c Camera) Valid() Camera { return c.valid() }

// Orbit returns the camera turned about its target: azimuth is added to
// Azimuth and elevation to Elevation, both in degrees.
func (c Camera) Orbit(azimuth, elevation float64) Camera {
	c.Pose = nt3dmath.Mat4{}
	c.Azimuth += azimuth
	c.Elevation += elevation
	return c.valid()
}

// Zoom multiplies distance by factor; invalid factors leave the camera unchanged.
func (c Camera) Zoom(factor float64) Camera {
	if factor > 0 && nt3dmath.IsFinite(factor) {
		if c.Pose != (nt3dmath.Mat4{}) {
			local := c
			local.Origin = [3]float64{}
			e := c.Target.Add(local.Eye().Sub(c.Target).Scale(float32(factor)))
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
	c.Origin = [3]float64{}
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
	c.Origin = [3]float64{}
	c.Pose = nt3dmath.Mat4{}
	c.Target, c.Distance = target, distance
	c.Elevation = math.Asin(float64(d.Z)/distance) * 180 / math.Pi
	c.Azimuth = math.Atan2(float64(d.Y), float64(d.X)) * 180 / math.Pi
	return c.valid()
}

// LookDirection places the camera at eye facing forward while preserving
// Distance, and thus automatic clipping and orbit scale. Forward need not be
// normalized. Like LookAt, elevation is limited to 89 degrees. A zero or
// non-finite direction or eye leaves the sanitized camera unchanged.
func (c Camera) LookDirection(eye, forward nt3dmath.Vec3) Camera {
	c = c.valid()
	length := math.Sqrt(float64(forward.X)*float64(forward.X) + float64(forward.Y)*float64(forward.Y) + float64(forward.Z)*float64(forward.Z))
	if !(length > 0) || !nt3dmath.IsFinite(length) || !validFloat(eye.X, eye.Y, eye.Z) {
		return c
	}
	c.Origin = [3]float64{}
	c.Pose = nt3dmath.Mat4{}
	c.Elevation = -math.Asin(float64(forward.Z)/length) * 180 / math.Pi
	c.Azimuth = math.Atan2(-float64(forward.Y), -float64(forward.X)) * 180 / math.Pi
	c = c.valid()
	// Compute the target from the clamped orientation to keep the eye fixed.
	c.Target = nt3dmath.Vec3{}
	c.Target = eye.Sub(c.Eye())
	return c
}

// Eye returns the camera's position in world space.
func (c Camera) Eye() nt3dmath.Vec3 {
	eye, _, _, _ := c.basis()
	return eye
}

// WorldEye returns the camera's eye position in float64 world coordinates,
// combining Origin with the local Pose or orbit offset without float32 loss.
func (c Camera) WorldEye() [3]float64 {
	c = c.valid()
	local := c
	local.Origin = [3]float64{}
	eye, _, _, _ := local.basis()
	return [3]float64{
		c.Origin[0] + float64(eye.X),
		c.Origin[1] + float64(eye.Y),
		c.Origin[2] + float64(eye.Z),
	}
}

// Forward returns the camera's unit viewing direction in world space.
func (c Camera) Forward() nt3dmath.Vec3 {
	_, _, _, forward := c.basis()
	return forward
}

// Basis returns the camera's orthonormal right, up, and forward unit vectors in world space.
func (c Camera) Basis() (right, up, forward nt3dmath.Vec3) {
	_, right, up, forward = c.basis()
	return
}
