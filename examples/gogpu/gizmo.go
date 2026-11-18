// Copyright (c) 2026 Neomantra Corp.

package main

import (
	"fmt"
	"image/color"
	"math"

	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

type gizmoMode int

const (
	gizmoTranslate gizmoMode = iota
	gizmoRotate
	gizmoScale
	gizmoOff
)

func primitiveName(p nt3d.Primitive) string {
	switch p {
	case nt3d.PointPrimitive:
		return "splat/pt"
	case nt3d.TrianglePrimitive:
		return "tri"
	case nt3d.BoxPrimitive:
		return "box"
	case nt3d.LinePrimitive:
		return "line"
	case nt3d.ArrowPrimitive:
		return "arrow"
	default:
		return "prim"
	}
}

func (v *viewerState) clearPick() {
	if v.selectedNode != nt3d.Root {
		_ = v.scene.ApplyOutline(v.selectedNode, nt3d.OutlineStyle{})
		v.selectedNode = nt3d.Root
	}
	v.selectedHit = nil
	v.hasInitLocal = false
	v.gizmoHoverAxis = 0
	v.gizmoActiveAxis = 0
	v.gizmoDragging = false
	if v.pickLabelID != nt3d.Root {
		_ = v.scene.SetVisible(v.pickLabelID, false)
	}
}

func (v *viewerState) selectPick(hit *nt3d.Hit, camera nt3d.Camera) {
	v.clearPick()
	if hit == nil {
		return
	}
	v.selectedHit = hit
	v.selectedNode = hit.Node
	v.selectedLocal = hit.Position
	if node, ok := v.scene.Node(hit.Node); ok {
		v.selectedInitLocal = node.Local
		v.hasInitLocal = true
		if inv, ok := node.World.InverseAffine(); ok {
			v.selectedLocal = inv.Point(hit.Position)
		}
	}
	_ = v.scene.ApplyOutline(hit.Node, nt3d.OutlineStyle{
		Width: 3,
		Color: color.RGBA{255, 210, 60, 255},
	})
	v.updatePickAnnotation(camera, true)
}

func (v *viewerState) gizmoModeName() string {
	switch v.gizmoMode {
	case gizmoTranslate:
		return "translate"
	case gizmoRotate:
		return "rotate"
	case gizmoScale:
		return "scale"
	default:
		return "off"
	}
}

func (v *viewerState) cycleGizmoMode() {
	if v == nil {
		return
	}
	v.gizmoDragging = false
	v.gizmoActiveAxis = 0
	v.gizmoHoverAxis = 0
	switch v.gizmoMode {
	case gizmoTranslate:
		v.gizmoMode = gizmoRotate
	case gizmoRotate:
		v.gizmoMode = gizmoScale
	case gizmoScale:
		v.gizmoMode = gizmoOff
	default:
		v.gizmoMode = gizmoTranslate
	}
	fmt.Printf("3D Transform Gizmo: %s\n", v.gizmoModeName())
}

func (v *viewerState) resetSelectedTransform(camera nt3d.Camera, showHUD bool) {
	if v == nil || v.selectedNode == nt3d.Root || !v.hasInitLocal {
		return
	}
	v.gizmoDragging = false
	v.gizmoActiveAxis = 0
	_ = v.scene.SetTransform(v.selectedNode, v.selectedInitLocal)
	v.updatePickAnnotation(camera, showHUD)
	fmt.Printf("Reset transform on node #%d\n", v.selectedNode)
}

func gizmoAxisVec(axis int) nt3dmath.Vec3 {
	switch axis {
	case 1:
		return nt3dmath.Vec3{X: 1}
	case 2:
		return nt3dmath.Vec3{Y: 1}
	case 3:
		return nt3dmath.Vec3{Z: 1}
	default:
		return nt3dmath.Vec3{}
	}
}

func gizmoPlaneBasis(axis int) (nt3dmath.Vec3, nt3dmath.Vec3) {
	switch axis {
	case 1: // YZ plane around X axis
		return nt3dmath.Vec3{Y: 1}, nt3dmath.Vec3{Z: 1}
	case 2: // ZX plane around Y axis
		return nt3dmath.Vec3{Z: 1}, nt3dmath.Vec3{X: 1}
	default: // XY plane around Z axis
		return nt3dmath.Vec3{X: 1}, nt3dmath.Vec3{Y: 1}
	}
}

func gizmoAxisRotation(axis int, radians float64) nt3dmath.Mat4 {
	c := float32(math.Cos(radians))
	s := float32(math.Sin(radians))
	m := nt3dmath.Identity()
	switch axis {
	case 1: // Rotate around X
		m[5], m[9] = c, -s
		m[6], m[10] = s, c
	case 2: // Rotate around Y
		m[0], m[8] = c, s
		m[2], m[10] = -s, c
	case 3: // Rotate around Z
		m[0], m[4] = c, -s
		m[1], m[5] = s, c
	}
	return m
}

func gizmoAxisScale(axis int, s float32) nt3dmath.Mat4 {
	switch axis {
	case 1:
		return nt3dmath.Scaling(nt3dmath.Vec3{X: s, Y: 1, Z: 1})
	case 2:
		return nt3dmath.Scaling(nt3dmath.Vec3{X: 1, Y: s, Z: 1})
	case 3:
		return nt3dmath.Scaling(nt3dmath.Vec3{X: 1, Y: 1, Z: s})
	default:
		return nt3dmath.Scaling(nt3dmath.Vec3{X: s, Y: s, Z: s})
	}
}

func (v *viewerState) gizmoRadius(camera nt3d.Camera) float32 {
	return float32(max(0.10, camera.Distance*0.17))
}

func pointToSegmentDist(px, py, x0, y0, x1, y1 float32) float32 {
	dx, dy := x1-x0, y1-y0
	l2 := dx*dx + dy*dy
	if l2 <= 1e-6 {
		return float32(math.Hypot(float64(px-x0), float64(py-y0)))
	}
	t := ((px-x0)*dx + (py-y0)*dy) / l2
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	cx, cy := x0+t*dx, y0+t*dy
	return float32(math.Hypot(float64(px-cx), float64(py-cy)))
}

func (v *viewerState) hitTestGizmo(f nt3d.Frame, camera nt3d.Camera, px, py float32) int {
	if v == nil || v.gizmoMode == gizmoOff || v.selectedNode == nt3d.Root || v.selectedHit == nil || f.Width <= 0 || f.Height <= 0 {
		return 0
	}
	origin, ok := v.pickWorldPos()
	if !ok {
		return 0
	}
	radius := v.gizmoRadius(camera)

	if v.gizmoMode == gizmoTranslate || v.gizmoMode == gizmoScale {
		ox, oy, _, okO := f.Matrix.Project(origin, f.Width, f.Height)
		if !okO {
			return 0
		}
		if v.gizmoMode == gizmoScale && float32(math.Hypot(float64(px-ox), float64(py-oy))) < 11.0 {
			return 4 // Center handle: uniform X/Y/Z scale
		}
		bestAxis := 0
		bestTipDist := float32(13.0)
		for axis := 1; axis <= 3; axis++ {
			tip := origin.Add(gizmoAxisVec(axis).Scale(radius))
			tx, ty, _, okT := f.Matrix.Project(tip, f.Width, f.Height)
			if !okT {
				continue
			}
			d := float32(math.Hypot(float64(px-tx), float64(py-ty)))
			if d < bestTipDist {
				bestTipDist = d
				bestAxis = axis
			}
		}
		if bestAxis != 0 {
			return bestAxis
		}
		bestSegDist := float32(9.0)
		for axis := 1; axis <= 3; axis++ {
			tip := origin.Add(gizmoAxisVec(axis).Scale(radius))
			tx, ty, _, okT := f.Matrix.Project(tip, f.Width, f.Height)
			if !okT {
				continue
			}
			d := pointToSegmentDist(px, py, ox, oy, tx, ty)
			if d < bestSegDist {
				bestSegDist = d
				bestAxis = axis
			}
		}
		return bestAxis
	}

	// gizmoRotate: test distance to each projected 24-segment rotation circle.
	const ringSegs = 24
	ringRadius := radius * 0.85
	bestAxis := 0
	bestDist := float32(10.0)
	for axis := 1; axis <= 3; axis++ {
		u, w := gizmoPlaneBasis(axis)
		var prevX, prevY float32
		var prevOK bool
		for i := 0; i <= ringSegs; i++ {
			ang := float64(i) * 2 * math.Pi / ringSegs
			cs, sn := float32(math.Cos(ang))*ringRadius, float32(math.Sin(ang))*ringRadius
			pt := origin.Add(u.Scale(cs)).Add(w.Scale(sn))
			sx, sy, _, okP := f.Matrix.Project(pt, f.Width, f.Height)
			if okP && prevOK {
				d := pointToSegmentDist(px, py, prevX, prevY, sx, sy)
				if d < bestDist {
					bestDist = d
					bestAxis = axis
				}
			}
			prevX, prevY, prevOK = sx, sy, okP
		}
	}
	return bestAxis
}

func (v *viewerState) beginGizmoDrag(f nt3d.Frame, camera nt3d.Camera, px, py float32) bool {
	axis := v.hitTestGizmo(f, camera, px, py)
	if axis == 0 {
		return false
	}
	node, ok := v.scene.Node(v.selectedNode)
	if !ok {
		return false
	}
	pivot, ok := v.pickWorldPos()
	if !ok {
		return false
	}
	v.gizmoDragging = true
	v.gizmoActiveAxis = axis
	v.gizmoHoverAxis = axis
	v.gizmoStartX = px
	v.gizmoStartY = py
	v.gizmoStartLocal = node.Local
	v.gizmoStartWorld = node.World
	v.gizmoStartPivot = pivot
	return true
}

func (v *viewerState) updateGizmoDrag(f nt3d.Frame, camera nt3d.Camera, px, py float32, showHUD bool) {
	if v == nil || !v.gizmoDragging || v.gizmoActiveAxis == 0 || v.selectedNode == nt3d.Root {
		return
	}
	node, ok := v.scene.Node(v.selectedNode)
	if !ok {
		return
	}
	parentWorld := nt3dmath.Identity()
	if node.Parent != nt3d.Root {
		if pNode, ok := v.scene.Node(node.Parent); ok {
			parentWorld = pNode.World
		}
	}
	parentInv, ok := parentWorld.InverseAffine()
	if !ok {
		parentInv = nt3dmath.Identity()
	}

	axisVec := gizmoAxisVec(v.gizmoActiveAxis)
	radius := v.gizmoRadius(camera)

	if v.gizmoMode == gizmoTranslate {
		x0, y0, _, ok0 := f.Matrix.Project(v.gizmoStartPivot, f.Width, f.Height)
		x1, y1, _, ok1 := f.Matrix.Project(v.gizmoStartPivot.Add(axisVec.Scale(radius)), f.Width, f.Height)
		if !ok0 || !ok1 {
			return
		}
		dxS, dyS := x1-x0, y1-y0
		len2 := dxS*dxS + dyS*dyS
		if len2 <= 1e-3 {
			return
		}
		t := ((px-v.gizmoStartX)*dxS + (py-v.gizmoStartY)*dyS) / len2
		deltaWorld := axisVec.Scale(t * radius)
		newLocal := parentInv.Mul(nt3dmath.Translation(deltaWorld)).Mul(v.gizmoStartWorld)
		_ = v.scene.SetTransform(v.selectedNode, newLocal)
		v.updatePickAnnotation(camera, showHUD)
		return
	}

	if v.gizmoMode == gizmoScale {
		var factor float32 = 1.0
		if v.gizmoActiveAxis == 4 {
			factor = float32(math.Exp(float64((px-v.gizmoStartX)-(py-v.gizmoStartY)) * 0.008))
		} else {
			x0, y0, _, ok0 := f.Matrix.Project(v.gizmoStartPivot, f.Width, f.Height)
			x1, y1, _, ok1 := f.Matrix.Project(v.gizmoStartPivot.Add(axisVec.Scale(radius)), f.Width, f.Height)
			if !ok0 || !ok1 {
				return
			}
			dxS, dyS := x1-x0, y1-y0
			len2 := dxS*dxS + dyS*dyS
			if len2 <= 1e-3 {
				return
			}
			t := ((px-v.gizmoStartX)*dxS + (py-v.gizmoStartY)*dyS) / len2
			factor = 1.0 + t
		}
		factor = min(20.0, max(0.05, factor))
		scaleMat := gizmoAxisScale(v.gizmoActiveAxis, factor)
		worldDelta := nt3dmath.Translation(v.gizmoStartPivot).
			Mul(scaleMat).
			Mul(nt3dmath.Translation(v.gizmoStartPivot.Scale(-1)))
		newLocal := parentInv.Mul(worldDelta).Mul(v.gizmoStartWorld)
		_ = v.scene.SetTransform(v.selectedNode, newLocal)
		v.updatePickAnnotation(camera, showHUD)
		return
	}

	// gizmoRotate: compute rotation angle around v.gizmoStartPivot.
	cx, cy, _, okC := f.Matrix.Project(v.gizmoStartPivot, f.Width, f.Height)
	var angleRad float64
	v0x, v0y := float64(v.gizmoStartX-cx), float64(v.gizmoStartY-cy)
	v1x, v1y := float64(px-cx), float64(py-cy)
	if okC && math.Hypot(v0x, v0y) > 6 && math.Hypot(v1x, v1y) > 6 {
		angleRad = math.Atan2(v1y, v1x) - math.Atan2(v0y, v0x)
		// Screen Y points down; flip sign when camera looks against the axis normal.
		if f.Forward.Dot(axisVec) < 0 {
			angleRad = -angleRad
		}
	} else {
		angleRad = float64((px-v.gizmoStartX)-(py-v.gizmoStartY)) * 0.015
	}
	rot := gizmoAxisRotation(v.gizmoActiveAxis, angleRad)
	worldDelta := nt3dmath.Translation(v.gizmoStartPivot).
		Mul(rot).
		Mul(nt3dmath.Translation(v.gizmoStartPivot.Scale(-1)))
	newLocal := parentInv.Mul(worldDelta).Mul(v.gizmoStartWorld)
	_ = v.scene.SetTransform(v.selectedNode, newLocal)
	v.updatePickAnnotation(camera, showHUD)
}

func (v *viewerState) endGizmoDrag() {
	if v == nil {
		return
	}
	v.gizmoDragging = false
	v.gizmoActiveAxis = 0
}

func (v *viewerState) pickWorldPos() (nt3dmath.Vec3, bool) {
	if v.selectedHit == nil {
		return nt3dmath.Vec3{}, false
	}
	if node, ok := v.scene.Node(v.selectedNode); ok {
		return node.World.Point(v.selectedLocal), true
	}
	return v.selectedHit.Position, true
}

func (v *viewerState) updatePickAnnotation(camera nt3d.Camera, show bool) (worldPos, labelAnchor nt3dmath.Vec3, ok bool) {
	worldPos, ok = v.pickWorldPos()
	if !ok {
		return nt3dmath.Vec3{}, nt3dmath.Vec3{}, false
	}
	if !show {
		if v.pickLabelID != nt3d.Root {
			_ = v.scene.SetVisible(v.pickLabelID, false)
		}
		return worldPos, worldPos, true
	}
	arm := float32(max(0.02, camera.Distance*0.035))
	labelAnchor = worldPos.Add(nt3dmath.Vec3{Z: arm * 3.0})

	name := v.selectedHit.Name
	if name == "" {
		name = fmt.Sprintf("node-%d", v.selectedHit.Node)
	}
	text := fmt.Sprintf("%s (#%d)\n%s #%d  depth %.3f\nxyz (%.2f, %.2f, %.2f)",
		name, v.selectedHit.Node,
		primitiveName(v.selectedHit.Primitive), v.selectedHit.Index, v.selectedHit.Depth,
		worldPos.X, worldPos.Y, worldPos.Z)
	if text != v.pickLabelText || v.pickLabelID == nt3d.Root {
		geom, err := (nt3d.Label{
			Text:        text,
			Pixels:      true,
			Transparent: true,
			NoPick:      true,
			Color:       color.RGBA{255, 248, 225, 255},
			Background:  color.RGBA{18, 24, 36, 215},
			BorderColor: color.RGBA{255, 205, 65, 240},
			Shadow:      color.RGBA{0, 0, 0, 220},
			Padding:     6,
			Border:      1,
		}).Geometry()
		if err == nil {
			geom.Material.FullSize = true
			if id, err := v.scene.SetGeometry("nt3d-pick-callout", geom); err == nil {
				v.pickLabelID = id
				v.pickLabelText = text
			}
		}
	}
	if v.pickLabelID != nt3d.Root {
		_ = v.scene.SetTransform(v.pickLabelID, nt3dmath.Translation(labelAnchor))
		_ = v.scene.SetVisible(v.pickLabelID, true)
	}
	return worldPos, labelAnchor, true
}

func (v *viewerState) decorateFrame(f *nt3d.Frame, camera nt3d.Camera, showAnnotations, isInset bool) {
	if (!showAnnotations || isInset) && v.pickLabelID != nt3d.Root {
		for i := range f.Draws {
			if f.Draws[i].Node == v.pickLabelID {
				f.Draws[i].Hidden = true
			}
		}
	}
	if showAnnotations && !isInset {
		v.drawOrientationTriad(f, camera)
	}
	if !showAnnotations || v.selectedHit == nil {
		return
	}
	worldPos, ok := v.pickWorldPos()
	if !ok {
		return
	}
	arm := float32(max(0.02, camera.Distance*0.035))
	labelAnchor := worldPos.Add(nt3dmath.Vec3{Z: arm * 3.0})

	red := color.RGBA{255, 95, 95, 255}
	green := color.RGBA{95, 235, 135, 255}
	blue := color.RGBA{95, 175, 255, 255}
	gold := color.RGBA{255, 215, 70, 255}

	// 3D XYZ axis crosshair at worldPos.
	f.DynamicLines = append(f.DynamicLines,
		nt3d.Vertex{Position: worldPos.Sub(nt3dmath.Vec3{X: arm}), Color: red},
		nt3d.Vertex{Position: worldPos.Add(nt3dmath.Vec3{X: arm}), Color: red},
		nt3d.Vertex{Position: worldPos.Sub(nt3dmath.Vec3{Y: arm}), Color: green},
		nt3d.Vertex{Position: worldPos.Add(nt3dmath.Vec3{Y: arm}), Color: green},
		nt3d.Vertex{Position: worldPos.Sub(nt3dmath.Vec3{Z: arm}), Color: blue},
		nt3d.Vertex{Position: worldPos.Add(nt3dmath.Vec3{Z: arm}), Color: blue},
	)
	// 3D diamond marker + vertical leader line to the billboard callout anchor.
	d := arm * 0.65
	pX0 := worldPos.Sub(nt3dmath.Vec3{X: d})
	pX1 := worldPos.Add(nt3dmath.Vec3{X: d})
	pY0 := worldPos.Sub(nt3dmath.Vec3{Y: d})
	pY1 := worldPos.Add(nt3dmath.Vec3{Y: d})
	f.DynamicLines = append(f.DynamicLines,
		nt3d.Vertex{Position: pX0, Color: gold}, nt3d.Vertex{Position: pY0, Color: gold},
		nt3d.Vertex{Position: pY0, Color: gold}, nt3d.Vertex{Position: pX1, Color: gold},
		nt3d.Vertex{Position: pX1, Color: gold}, nt3d.Vertex{Position: pY1, Color: gold},
		nt3d.Vertex{Position: pY1, Color: gold}, nt3d.Vertex{Position: pX0, Color: gold},
		nt3d.Vertex{Position: worldPos, Color: gold}, nt3d.Vertex{Position: labelAnchor, Color: gold},
	)

	// Interactive 3D Transform Gizmo (Translate arrows or Rotate rings) at the selected node's pivot.
	if v.selectedHit.Node != nt3d.Root && v.gizmoMode != gizmoOff {
		radius := v.gizmoRadius(camera)
		hiAxis := v.gizmoHoverAxis
		if v.gizmoDragging {
			hiAxis = v.gizmoActiveAxis
		}
		hiColor := color.RGBA{255, 235, 80, 255}
		axisColors := [3]color.RGBA{red, green, blue}

		switch v.gizmoMode {
		case gizmoTranslate:
			for axis := 1; axis <= 3; axis++ {
				col := axisColors[axis-1]
				if hiAxis == axis {
					col = hiColor
				}
				dir := gizmoAxisVec(axis)
				u, w := gizmoPlaneBasis(axis)
				tip := worldPos.Add(dir.Scale(radius))
				base := worldPos.Add(dir.Scale(radius * 0.78))
				flare := radius * 0.09

				// Main shaft (and parallel highlight shaft when hovered/active).
				f.DynamicLines = append(f.DynamicLines,
					nt3d.Vertex{Position: worldPos, Color: col},
					nt3d.Vertex{Position: tip, Color: col},
				)
				if hiAxis == axis {
					off := u.Scale(radius * 0.018)
					f.DynamicLines = append(f.DynamicLines,
						nt3d.Vertex{Position: worldPos.Add(off), Color: col},
						nt3d.Vertex{Position: tip.Add(off), Color: col},
					)
				}

				// 4-rib 3D pyramidal arrowhead at tip.
				c0 := base.Add(u.Scale(flare))
				c1 := base.Add(w.Scale(flare))
				c2 := base.Sub(u.Scale(flare))
				c3 := base.Sub(w.Scale(flare))
				f.DynamicLines = append(f.DynamicLines,
					nt3d.Vertex{Position: tip, Color: col}, nt3d.Vertex{Position: c0, Color: col},
					nt3d.Vertex{Position: tip, Color: col}, nt3d.Vertex{Position: c1, Color: col},
					nt3d.Vertex{Position: tip, Color: col}, nt3d.Vertex{Position: c2, Color: col},
					nt3d.Vertex{Position: tip, Color: col}, nt3d.Vertex{Position: c3, Color: col},
					nt3d.Vertex{Position: c0, Color: col}, nt3d.Vertex{Position: c1, Color: col},
					nt3d.Vertex{Position: c1, Color: col}, nt3d.Vertex{Position: c2, Color: col},
					nt3d.Vertex{Position: c2, Color: col}, nt3d.Vertex{Position: c3, Color: col},
					nt3d.Vertex{Position: c3, Color: col}, nt3d.Vertex{Position: c0, Color: col},
				)

				// 2D handle box at arrow tip.
				if !isInset {
					if tx, ty, _, ok := f.Matrix.Project(tip, f.Width, f.Height); ok {
						hx, hy := int(math.Round(float64(tx))), int(math.Round(float64(ty)))
						sz := 5
						if hiAxis == axis {
							sz = 7
						}
						f.Overlays = append(f.Overlays,
							nt3d.OverlayRect{X: hx - sz/2, Y: hy - sz/2, Width: sz, Height: sz, Color: col},
						)
					}
				}
			}

		case gizmoRotate:
			ringR := radius * 0.85
			const segs = 24
			for axis := 1; axis <= 3; axis++ {
				col := axisColors[axis-1]
				if hiAxis == axis {
					col = hiColor
				}
				u, w := gizmoPlaneBasis(axis)
				var prev, prev2 nt3dmath.Vec3
				for i := 0; i <= segs; i++ {
					ang := 2 * math.Pi * float64(i) / float64(segs)
					ca, sa := float32(math.Cos(ang)), float32(math.Sin(ang))
					pt := worldPos.Add(u.Scale(ringR * ca)).Add(w.Scale(ringR * sa))
					pt2 := worldPos.Add(u.Scale(ringR * 1.03 * ca)).Add(w.Scale(ringR * 1.03 * sa))
					if i > 0 {
						f.DynamicLines = append(f.DynamicLines,
							nt3d.Vertex{Position: prev, Color: col},
							nt3d.Vertex{Position: pt, Color: col},
						)
						if hiAxis == axis {
							f.DynamicLines = append(f.DynamicLines,
								nt3d.Vertex{Position: prev2, Color: col},
								nt3d.Vertex{Position: pt2, Color: col},
							)
						}
					}
					prev, prev2 = pt, pt2
				}
				if !isInset {
					handlePt := worldPos.Add(u.Scale(ringR))
					if hx, hy, _, ok := f.Matrix.Project(handlePt, f.Width, f.Height); ok {
						ix, iy := int(math.Round(float64(hx))), int(math.Round(float64(hy)))
						sz := 5
						if hiAxis == axis {
							sz = 7
						}
						f.Overlays = append(f.Overlays,
							nt3d.OverlayRect{X: ix - sz/2, Y: iy - sz/2, Width: sz, Height: sz, Color: col},
						)
					}
				}
			}

		case gizmoScale:
			boxHalf := radius * 0.065
			for axis := 1; axis <= 3; axis++ {
				col := axisColors[axis-1]
				if hiAxis == axis || hiAxis == 4 {
					col = hiColor
				}
				dir := gizmoAxisVec(axis)
				u, w := gizmoPlaneBasis(axis)
				tip := worldPos.Add(dir.Scale(radius))

				f.DynamicLines = append(f.DynamicLines,
					nt3d.Vertex{Position: worldPos, Color: col},
					nt3d.Vertex{Position: tip, Color: col},
				)
				if hiAxis == axis || hiAxis == 4 {
					off := u.Scale(radius * 0.018)
					f.DynamicLines = append(f.DynamicLines,
						nt3d.Vertex{Position: worldPos.Add(off), Color: col},
						nt3d.Vertex{Position: tip.Add(off), Color: col},
					)
				}

				// 3D wireframe cube at the scale tip.
				var c [8]nt3dmath.Vec3
				for i := range 8 {
					sx, sy, sz := float32(-1), float32(-1), float32(-1)
					if i&1 != 0 {
						sx = 1
					}
					if i&2 != 0 {
						sy = 1
					}
					if i&4 != 0 {
						sz = 1
					}
					c[i] = tip.Add(dir.Scale(sx * boxHalf)).Add(u.Scale(sy * boxHalf)).Add(w.Scale(sz * boxHalf))
				}
				edges := [12][2]int{
					{0, 1}, {2, 3}, {4, 5}, {6, 7},
					{0, 2}, {1, 3}, {4, 6}, {5, 7},
					{0, 4}, {1, 5}, {2, 6}, {3, 7},
				}
				for _, e := range edges {
					f.DynamicLines = append(f.DynamicLines,
						nt3d.Vertex{Position: c[e[0]], Color: col},
						nt3d.Vertex{Position: c[e[1]], Color: col},
					)
				}
				if !isInset {
					if tx, ty, _, ok := f.Matrix.Project(tip, f.Width, f.Height); ok {
						hx, hy := int(math.Round(float64(tx))), int(math.Round(float64(ty)))
						sz := 6
						if hiAxis == axis || hiAxis == 4 {
							sz = 8
						}
						f.Overlays = append(f.Overlays,
							nt3d.OverlayRect{X: hx - sz/2, Y: hy - sz/2, Width: sz, Height: sz, Color: col},
						)
					}
				}
			}
			// Center uniform-scale handle box.
			if !isInset {
				if cx, cy, _, ok := f.Matrix.Project(worldPos, f.Width, f.Height); ok {
					ix, iy := int(math.Round(float64(cx))), int(math.Round(float64(cy)))
					col := gold
					sz := 7
					if hiAxis == 4 {
						col = hiColor
						sz = 9
					}
					f.Overlays = append(f.Overlays,
						nt3d.OverlayRect{X: ix - sz/2, Y: iy - sz/2, Width: sz, Height: sz, Color: col},
					)
				}
			}
		}
	}

	// 2D projected screen-space corner-bracket reticle around the picked 3D point.
	if !isInset {
		if px, py, _, ok := f.Matrix.Project(worldPos, f.Width, f.Height); ok {
			cx, cy := int(math.Round(float64(px))), int(math.Round(float64(py)))
			const r, lenB, thick = 10, 5, 2
			f.Overlays = append(f.Overlays,
				// Top-left bracket
				nt3d.OverlayRect{X: cx - r, Y: cy - r, Width: lenB, Height: thick, Color: gold},
				nt3d.OverlayRect{X: cx - r, Y: cy - r, Width: thick, Height: lenB, Color: gold},
				// Top-right bracket
				nt3d.OverlayRect{X: cx + r - lenB + 1, Y: cy - r, Width: lenB, Height: thick, Color: gold},
				nt3d.OverlayRect{X: cx + r - thick + 1, Y: cy - r, Width: thick, Height: lenB, Color: gold},
				// Bottom-left bracket
				nt3d.OverlayRect{X: cx - r, Y: cy + r - thick + 1, Width: lenB, Height: thick, Color: gold},
				nt3d.OverlayRect{X: cx - r, Y: cy + r - lenB + 1, Width: thick, Height: lenB, Color: gold},
				// Bottom-right bracket
				nt3d.OverlayRect{X: cx + r - lenB + 1, Y: cy + r - thick + 1, Width: lenB, Height: thick, Color: gold},
				nt3d.OverlayRect{X: cx + r - thick + 1, Y: cy + r - lenB + 1, Width: thick, Height: lenB, Color: gold},
			)
		}
	}
}
