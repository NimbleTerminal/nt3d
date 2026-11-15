// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image/color"
	"math"
	"strings"
	"time"

	nt3dcam "nimbleterminal.dev/nt3d/cam"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// CameraFromState converts a float64 evaluated CameraState into an nt3d.Camera.
//
// Pos is stored in Camera.Origin (with Pose holding only the orthonormal
// camera-to-world rotation) so Scene.Frame and Scene.Pick subtract the camera
// position in float64 before float32 matrix construction, avoiding precision
// jitter even at geographic (ENU/UTM) coordinates. Orbit fields (Target,
// Distance, Alpha, Beta) are also initialized along the viewing ray so
// interactive mouse Orbit/Pan handoff works seamlessly from the current pose.
func CameraFromState(state nt3dcam.CameraState) Camera {
	rot := state.Rot.Normalize()
	r, u, f := rot.Basis()
	back := f.Scale(-1)

	pose := nt3dmath.Mat4{
		float32(r.X), float32(r.Y), float32(r.Z), 0,
		float32(u.X), float32(u.Y), float32(u.Z), 0,
		float32(back.X), float32(back.Y), float32(back.Z), 0,
		0, 0, 0, 1,
	}

	dist := state.FocusDist
	if !(dist > 0) || !nt3dmath.IsFinite(dist) {
		dist = 3.4
	}
	dist = math.Max(minDistance, math.Min(maxDistance, dist))

	fov := state.FovY
	if !(fov > 0) || fov >= math.Pi || !nt3dmath.IsFinite(fov) {
		fov = nt3dcam.DefaultFovY
	}

	// Matching spherical angles so calling Orbit() later transitions smoothly
	// around Target = Origin + f * dist.
	alpha := math.Max(-89, math.Min(89, math.Asin(math.Max(-1, math.Min(1, -f.Z)))*180/math.Pi))
	beta := math.Atan2(-f.Y, -f.X) * 180 / math.Pi

	c := Camera{
		Origin:      [3]float64{state.Pos.X, state.Pos.Y, state.Pos.Z},
		Pose:        pose,
		YFOV:        fov,
		Near:        state.Near,
		Far:         state.Far,
		Distance:    dist,
		Alpha:       alpha,
		Beta:        beta,
		Target:      nt3dmath.Vec3{X: float32(f.X * dist), Y: float32(f.Y * dist), Z: float32(f.Z * dist)},
		Projection:  Perspective,
		ResumeAfter: 2,
	}
	return c.valid()
}

// StateFromCamera extracts a float64 nt3dcam.CameraState from an nt3d.Camera,
// preserving Camera.Origin precision for blending between interactive cameras
// and declarative camera paths.
func StateFromCamera(c Camera) nt3dcam.CameraState {
	c = c.valid()
	eye64 := c.WorldEye()
	r, u, f := c.Basis()
	fov := c.YFOV
	if fov <= 0 {
		fov = nt3dcam.DefaultFovY
	}
	return nt3dcam.CameraState{
		Pos: nt3dcam.Vec3d{X: eye64[0], Y: eye64[1], Z: eye64[2]},
		Rot: nt3dcam.QuatFromBasis(
			nt3dcam.Vec3d{X: float64(r.X), Y: float64(r.Y), Z: float64(r.Z)},
			nt3dcam.Vec3d{X: float64(u.X), Y: float64(u.Y), Z: float64(u.Z)},
			nt3dcam.Vec3d{X: float64(f.X), Y: float64(f.Y), Z: float64(f.Z)},
		),
		FovY:      fov,
		FocusDist: c.Distance,
		Near:      c.Near,
		Far:       c.Far,
	}
}

// SceneResolver returns an nt3dcam.Resolver that resolves "node:<name>" against
// scene s (using exact float64 NodeWorldPosition) and "path:<name>" against an
// optional map of named camera paths at timestamp t.
//
// Because Scene mutations are single-goroutine, do not mutate s concurrently
// while evaluating paths bound to this resolver.
func SceneResolver(s *Scene, paths map[string]*nt3dcam.Path) nt3dcam.Resolver {
	return &sceneResolver{scene: s, paths: paths}
}

type sceneResolver struct {
	scene *Scene
	paths map[string]*nt3dcam.Path
}

func (r *sceneResolver) ResolveTarget(ref string, t time.Duration) (nt3dcam.TargetInfo, bool) {
	return r.resolveDepth(ref, t, 0)
}

func (r *sceneResolver) resolveDepth(ref string, t time.Duration, depth int) (nt3dcam.TargetInfo, bool) {
	if depth > 8 {
		return nt3dcam.TargetInfo{}, false
	}
	if name, ok := strings.CutPrefix(ref, "node:"); ok && name != "" {
		if r.scene == nil {
			return nt3dcam.TargetInfo{}, false
		}
		id, found := r.scene.Find(name)
		if !found {
			return nt3dcam.TargetInfo{}, false
		}
		pos, ok := r.scene.NodeWorldPosition(id)
		if !ok {
			return nt3dcam.TargetInfo{}, false
		}
		return nt3dcam.TargetInfo{
			Pos: nt3dcam.Vec3d{X: pos[0], Y: pos[1], Z: pos[2]},
		}, true
	}
	if name, ok := strings.CutPrefix(ref, "path:"); ok && name != "" {
		if r.paths == nil {
			return nt3dcam.TargetInfo{}, false
		}
		p := r.paths[name]
		if p == nil {
			return nt3dcam.TargetInfo{}, false
		}
		sub := nt3dcam.ResolverFunc(func(nextRef string, nextT time.Duration) (nt3dcam.TargetInfo, bool) {
			return r.resolveDepth(nextRef, nextT, depth+1)
		})
		st := p.EvalWith(t, sub)
		return nt3dcam.TargetInfo{Pos: st.Pos}, true
	}
	return nt3dcam.TargetInfo{}, false
}

// CameraPathOverlayOptions customizes the 3D debug wireframe emitted by
// AppendCameraPathOverlay. Zero values select sensible defaults via
// DefaultCameraPathOverlayOptions.
type CameraPathOverlayOptions struct {
	CurveSamples     int              // Number of polyline samples along the path (default 128).
	LineWidth        float32          // Pixel width applied to f.DynamicLineWidth (default 3).
	ControlPointSize float64          // Half-extent of control-point 3D cross markers in meters (default 0.28).
	FrustumDepth     float64          // Length of the camera frustum pyramid in meters (default 1.2).
	LookRayLength    float64          // Length of the look-direction ray in meters (0 uses FocusDist or 2.5*FrustumDepth).
	Resolver         nt3dcam.Resolver // Optional per-call resolver for evaluating path at t.
	CurveColor       color.RGBA       // Polyline color (default cyan).
	ControlColor     color.RGBA       // Control-point & Bezier handle color (default amber).
	MarkerColor      color.RGBA       // Named timeline marker tick color (default magenta).
	FrustumColor     color.RGBA       // Active camera frustum & up-hat color (default coral/orange).
	LookRayColor     color.RGBA       // Active look-direction ray color (default green).
	HideControlPts   bool             // Omit control-point markers when true.
	HideFrustum      bool             // Omit the active camera frustum when true.
	HideLookRay      bool             // Omit the active look-direction ray when true.
}

// DefaultCameraPathOverlayOptions returns the default visual styling for
// AppendCameraPathOverlay.
func DefaultCameraPathOverlayOptions() CameraPathOverlayOptions {
	return CameraPathOverlayOptions{
		CurveSamples:     128,
		LineWidth:        3,
		ControlPointSize: 0.28,
		FrustumDepth:     1.2,
		CurveColor:       color.RGBA{R: 35, G: 160, B: 250, A: 255},
		ControlColor:     color.RGBA{R: 255, G: 180, B: 30, A: 255},
		MarkerColor:      color.RGBA{R: 235, G: 75, B: 175, A: 255},
		FrustumColor:     color.RGBA{R: 250, G: 95, B: 30, A: 255},
		LookRayColor:     color.RGBA{R: 35, G: 205, B: 90, A: 255},
	}
}

// AppendCameraPathOverlay appends depth-tested 3D debug lines to f.DynamicLines
// visualizing path's spatial curve, control points (and Bezier handles), named
// timeline markers, and the active camera frustum and look ray at timestamp t.
//
// All positions subtract f.Origin in float64 before float32 conversion so the
// overlay aligns jitter-free in both local and geographic-scale frames.
func AppendCameraPathOverlay(f *Frame, path *nt3dcam.Path, t time.Duration, opts ...CameraPathOverlayOptions) {
	if f == nil || path == nil {
		return
	}
	o := DefaultCameraPathOverlayOptions()
	if len(opts) > 0 {
		in := opts[0]
		if in.CurveSamples > 0 {
			o.CurveSamples = in.CurveSamples
		}
		if in.LineWidth > 0 {
			o.LineWidth = in.LineWidth
		}
		if in.ControlPointSize > 0 {
			o.ControlPointSize = in.ControlPointSize
		}
		if in.FrustumDepth > 0 {
			o.FrustumDepth = in.FrustumDepth
		}
		if in.LookRayLength > 0 {
			o.LookRayLength = in.LookRayLength
		}
		if in.Resolver != nil {
			o.Resolver = in.Resolver
		}
		if in.CurveColor != (color.RGBA{}) {
			o.CurveColor = in.CurveColor
		}
		if in.ControlColor != (color.RGBA{}) {
			o.ControlColor = in.ControlColor
		}
		if in.MarkerColor != (color.RGBA{}) {
			o.MarkerColor = in.MarkerColor
		}
		if in.FrustumColor != (color.RGBA{}) {
			o.FrustumColor = in.FrustumColor
		}
		if in.LookRayColor != (color.RGBA{}) {
			o.LookRayColor = in.LookRayColor
		}
		o.HideControlPts = in.HideControlPts
		o.HideFrustum = in.HideFrustum
		o.HideLookRay = in.HideLookRay
	}
	if o.LineWidth > 0 && validFloat(o.LineWidth) {
		clamped := min(float32(32), max(float32(1), o.LineWidth))
		if clamped > f.DynamicLineWidth {
			f.DynamicLineWidth = clamped
		}
	}

	toFrame := func(p nt3dcam.Vec3d) nt3dmath.Vec3 {
		return nt3dmath.Vec3{
			X: float32(p.X - f.Origin[0]),
			Y: float32(p.Y - f.Origin[1]),
			Z: float32(p.Z - f.Origin[2]),
		}
	}
	addSeg := func(a, b nt3dcam.Vec3d, col color.RGBA) {
		va, vb := toFrame(a), toFrame(b)
		if !finiteVec(va) || !finiteVec(vb) {
			return
		}
		f.DynamicLines = append(f.DynamicLines,
			Vertex{Position: va, Color: col},
			Vertex{Position: vb, Color: col},
		)
	}

	// 1. Sampled path polyline.
	samples := path.SampleCurve(max(2, o.CurveSamples))
	for i := 0; i+1 < len(samples); i++ {
		addSeg(samples[i], samples[i+1], o.CurveColor)
	}

	// 2. Control points & Bezier handle tangents.
	if !o.HideControlPts {
		r := o.ControlPointSize
		for _, cp := range path.ControlPoints() {
			addSeg(cp.Add(nt3dcam.Vec3d{X: -r}), cp.Add(nt3dcam.Vec3d{X: r}), o.ControlColor)
			addSeg(cp.Add(nt3dcam.Vec3d{Y: -r}), cp.Add(nt3dcam.Vec3d{Y: r}), o.ControlColor)
			addSeg(cp.Add(nt3dcam.Vec3d{Z: -r}), cp.Add(nt3dcam.Vec3d{Z: r}), o.ControlColor)
		}
		cfg := path.Config()
		if cfg.Position.Interp == nt3dcam.InterpBezier {
			dimCol := color.RGBA{R: o.ControlColor.R / 2, G: o.ControlColor.G / 2, B: o.ControlColor.B / 2, A: o.ControlColor.A}
			for _, k := range cfg.Position.Knots {
				if k.In != k.P {
					addSeg(k.P, k.In, dimCol)
				}
				if k.Out != k.P {
					addSeg(k.P, k.Out, dimCol)
				}
			}
		}
	}

	// 3. Named timeline markers.
	evalAt := func(ts time.Duration) nt3dcam.CameraState {
		if o.Resolver != nil {
			return path.EvalWith(ts, o.Resolver)
		}
		return path.Eval(ts)
	}
	tickSize := o.ControlPointSize * 0.85
	for _, m := range path.Markers() {
		ms := evalAt(m.T)
		up := ms.Up().Scale(tickSize)
		right := ms.Right().Scale(tickSize * 0.6)
		addSeg(ms.Pos.Sub(up), ms.Pos.Add(up), o.MarkerColor)
		addSeg(ms.Pos.Sub(right), ms.Pos.Add(right), o.MarkerColor)
	}

	// 4. Active camera state at t: look ray & 3D frustum pyramid + up-hat.
	st := evalAt(t)
	right, up, fwd := st.Right(), st.Up(), st.Forward()

	if !o.HideLookRay {
		rayLen := o.LookRayLength
		if !(rayLen > 0) {
			if st.FocusDist > 0 {
				rayLen = st.FocusDist
			} else {
				rayLen = o.FrustumDepth * 2.5
			}
		}
		tip := st.Pos.Add(fwd.Scale(rayLen))
		addSeg(st.Pos, tip, o.LookRayColor)
		// Small target crosshair at the end of the look ray.
		ch := o.ControlPointSize * 0.6
		addSeg(tip.Sub(right.Scale(ch)), tip.Add(right.Scale(ch)), o.LookRayColor)
		addSeg(tip.Sub(up.Scale(ch)), tip.Add(up.Scale(ch)), o.LookRayColor)
	}

	if !o.HideFrustum {
		aspect := 16.0 / 9.0
		if f.Width > 0 && f.Height > 0 {
			aspect = float64(f.Width) / float64(f.Height)
		}
		fov := st.FovY
		if !(fov > 0) || fov >= math.Pi {
			fov = nt3dcam.DefaultFovY
		}
		halfH := o.FrustumDepth * math.Tan(0.5*fov)
		halfW := halfH * aspect
		center := st.Pos.Add(fwd.Scale(o.FrustumDepth))
		rx := right.Scale(halfW)
		uy := up.Scale(halfH)

		tl := center.Sub(rx).Add(uy)
		tr := center.Add(rx).Add(uy)
		br := center.Add(rx).Sub(uy)
		bl := center.Sub(rx).Sub(uy)

		// Pyramid edges from eye to image plane.
		addSeg(st.Pos, tl, o.FrustumColor)
		addSeg(st.Pos, tr, o.FrustumColor)
		addSeg(st.Pos, br, o.FrustumColor)
		addSeg(st.Pos, bl, o.FrustumColor)
		// Image-plane frame.
		addSeg(tl, tr, o.FrustumColor)
		addSeg(tr, br, o.FrustumColor)
		addSeg(br, bl, o.FrustumColor)
		addSeg(bl, tl, o.FrustumColor)
		// Cinema up-hat triangle above the top edge so roll is unmistakably readable.
		hatApex := center.Add(up.Scale(halfH * 1.38))
		hatL := center.Sub(right.Scale(halfW * 0.45)).Add(uy)
		hatR := center.Add(right.Scale(halfW * 0.45)).Add(uy)
		addSeg(hatL, hatApex, o.FrustumColor)
		addSeg(hatR, hatApex, o.FrustumColor)
	}
}
