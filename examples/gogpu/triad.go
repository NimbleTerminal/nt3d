// Copyright (c) 2026 Neomantra Corp.

package main

import (
	"image/color"
	"math"
	"slices"

	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

type triadEndpoint struct {
	axis   int // ±1 (±X), ±2 (±Y), ±3 (±Z)
	sx, sy float32
	depth  float32
	color  color.RGBA
}

func orientationTriadEndpoints(f *nt3d.Frame, camera nt3d.Camera) (cx, cy float32, endpoints [6]triadEndpoint, ok bool) {
	if f.Width < 240 || f.Height < 180 {
		return 0, 0, endpoints, false
	}
	cx, cy = float32(f.Width-46), 46
	const arm = 28.0
	rotCam := nt3d.Camera{
		Alpha:    camera.Alpha,
		Beta:     camera.Beta,
		Distance: 4.0,
		Near:     0.1,
		Far:      10.0,
	}
	m := rotCam.Matrix(1)
	ox, oy, _, _ := m.Transform(nt3dmath.Vec3{})
	dirs := [6]struct {
		axis int
		vec  nt3dmath.Vec3
		col  color.RGBA
	}{
		{+1, nt3dmath.Vec3{X: 1}, color.RGBA{255, 85, 85, 255}},
		{-1, nt3dmath.Vec3{X: -1}, color.RGBA{145, 55, 55, 220}},
		{+2, nt3dmath.Vec3{Y: 1}, color.RGBA{85, 230, 125, 255}},
		{-2, nt3dmath.Vec3{Y: -1}, color.RGBA{50, 135, 75, 220}},
		{+3, nt3dmath.Vec3{Z: 1}, color.RGBA{85, 170, 255, 255}},
		{-3, nt3dmath.Vec3{Z: -1}, color.RGBA{50, 95, 150, 220}},
	}
	for i, d := range dirs {
		tx, ty, tz, _ := m.Transform(d.vec)
		dx, dy := tx-ox, -(ty - oy)
		endpoints[i] = triadEndpoint{
			axis:  d.axis,
			sx:    cx + dx*arm,
			sy:    cy + dy*arm,
			depth: tz,
			color: d.col,
		}
	}
	return cx, cy, endpoints, true
}

func (v *viewerState) drawOrientationTriad(f *nt3d.Frame, camera nt3d.Camera) {
	cx, cy, eps, ok := orientationTriadEndpoints(f, camera)
	if !ok {
		return
	}
	// Sort back-to-front (largest depth first) so front-facing axes draw on top.
	slices.SortFunc(eps[:], func(a, b triadEndpoint) int {
		if a.depth > b.depth {
			return -1
		}
		if a.depth < b.depth {
			return 1
		}
		return 0
	})
	hiColor := color.RGBA{255, 235, 80, 255}
	icx, icy := int(math.Round(float64(cx))), int(math.Round(float64(cy)))
	for _, ep := range eps {
		col := ep.color
		if v != nil && v.orientHoverAxis == ep.axis {
			col = hiColor
		}
		// Draw positive axis shafts from center to endpoint using small overlay steps.
		if ep.axis > 0 {
			const steps = 10
			for s := 1; s < steps; s++ {
				t := float32(s) / float32(steps)
				px := int(math.Round(float64(cx + (ep.sx-cx)*t)))
				py := int(math.Round(float64(cy + (ep.sy-cy)*t)))
				f.Overlays = append(f.Overlays,
					nt3d.OverlayRect{X: px - 1, Y: py - 1, Width: 2, Height: 2, Color: col},
				)
			}
		}
		sz := 6
		if ep.axis > 0 {
			sz = 8
		}
		if v != nil && v.orientHoverAxis == ep.axis {
			sz += 2
		}
		hx, hy := int(math.Round(float64(ep.sx))), int(math.Round(float64(ep.sy)))
		f.Overlays = append(f.Overlays,
			nt3d.OverlayRect{X: hx - sz/2, Y: hy - sz/2, Width: sz, Height: sz, Color: col},
		)
	}
	// Center hub dot.
	f.Overlays = append(f.Overlays,
		nt3d.OverlayRect{X: icx - 2, Y: icy - 2, Width: 4, Height: 4, Color: color.RGBA{220, 230, 245, 255}},
	)
}

func hitTestOrientationTriad(f *nt3d.Frame, camera nt3d.Camera, px, py float32) int {
	_, _, eps, ok := orientationTriadEndpoints(f, camera)
	if !ok {
		return 0
	}
	bestAxis := 0
	bestDist := float32(10.0)
	for _, ep := range eps {
		d := float32(math.Hypot(float64(px-ep.sx), float64(py-ep.sy)))
		if d < bestDist {
			bestDist = d
			bestAxis = ep.axis
		}
	}
	return bestAxis
}

func snapCameraToAxis(cam nt3d.Camera, axis int) (nt3d.Camera, string) {
	cam.Pose = nt3dmath.Mat4{}
	switch axis {
	case +1:
		cam.Alpha, cam.Beta = 0, 0
		return cam, "+X (Right)"
	case -1:
		cam.Alpha, cam.Beta = 0, 180
		return cam, "-X (Left)"
	case +2:
		cam.Alpha, cam.Beta = 0, 90
		return cam, "+Y (Back)"
	case -2:
		cam.Alpha, cam.Beta = 0, -90
		return cam, "-Y (Front)"
	case +3:
		cam.Alpha, cam.Beta = 89, -90
		return cam, "+Z (Top)"
	case -3:
		cam.Alpha, cam.Beta = -89, -90
		return cam, "-Z (Bottom)"
	default:
		return cam, ""
	}
}

func appendBorderOverlays(f *nt3d.Frame, thickness int, c color.RGBA) {
	if f.Width <= 2*thickness || f.Height <= 2*thickness {
		return
	}
	f.Overlays = append(f.Overlays,
		nt3d.OverlayRect{X: 0, Y: 0, Width: f.Width, Height: thickness, Color: c},
		nt3d.OverlayRect{X: 0, Y: f.Height - thickness, Width: f.Width, Height: thickness, Color: c},
		nt3d.OverlayRect{X: 0, Y: thickness, Width: thickness, Height: f.Height - 2*thickness, Color: c},
		nt3d.OverlayRect{X: f.Width - thickness, Y: thickness, Width: thickness, Height: f.Height - 2*thickness, Color: c},
	)
}
