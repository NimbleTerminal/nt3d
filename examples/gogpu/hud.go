// Copyright (c) 2026 Neomantra Corp.

package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/gogpu/gogpu"
	"github.com/gogpu/gpucontext"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/examples/demos/gallery"
)

type demoMenuItem struct {
	name string
	desc string
}

var demoCatalog = []demoMenuItem{
	{"showcase", "Reflective & glass showcase / file viewer"},
	{"terrain", "Heightfield island & textures"},
	{"procedural", "Animated GPU plasma shader"},
	{"dynamic", "Deforming mesh & orbiting bodies"},
	{"shapes", "Procedural 3D shape builders"},
	{"models", "Sample STL, OBJ & 3MF models"},
	{"large", "735k-triangle asteroid & BVH pick"},
	{"fur", "Shell + fin fur (Keenan Crane Spot)"},
	{"mipmaps", "Mipmap & anisotropic filtering"},
	{"forest", "Instanced wind-swept trees & shadows"},
	{"lights", "Multi-light presets, SH & shadows"},
	{"particles", "Soft & sprite fire/smoke emitter"},
	{"gaussians", "3D Gaussian splats & SH palette"},
	{"animation", "Skinned & morph-target ribbon"},
	{"animate", "Khronos glTF animation lab"},
	{"characters", "Quaternius Gobkit characters"},
	{"cloud", "Continuous LOD point cloud"},
	{"aabb", "Dynamic AABB tree & raycast hierarchy"},
	{"water", "Gerstner waves & force-driven craft"},
}

const (
	hudMargin      = 14
	hudPadding     = 8
	hudBorder      = 1
	hudInset       = hudPadding + hudBorder
	hudLineHeight  = 13
	menuHeaderRows = 1
)

type hudOverlay struct {
	scene       *nt3d.Scene
	menuScene   *nt3d.Scene
	text        string
	width       int
	height      int
	hudRect     image.Rectangle
	headerRect  image.Rectangle
	menuText    string
	menuWidth   int
	menuHeight  int
	menuRect    image.Rectangle
	accumDt     float64
	accumFrames int
	fps         float64
	frameMs     float64
	lastCapture string
}

func newHUDOverlay() *hudOverlay {
	return &hudOverlay{
		scene:     nt3d.NewScene(),
		menuScene: nt3d.NewScene(),
		fps:       60.0,
		frameMs:   16.7,
	}
}

func (h *hudOverlay) recordFrame(dt float64) {
	if dt <= 0 {
		return
	}
	h.accumDt += dt
	h.accumFrames++
	if h.accumDt >= 0.25 {
		h.fps = float64(h.accumFrames) / h.accumDt
		h.frameMs = (h.accumDt / float64(h.accumFrames)) * 1000.0
		h.accumDt = 0
		h.accumFrames = 0
	}
}

func keyToDemoString(key gpucontext.Key, shift bool) string {
	if key >= gpucontext.KeyA && key <= gpucontext.KeyZ {
		return string(rune('a' + (key - gpucontext.KeyA)))
	}
	switch key {
	case gpucontext.KeySpace:
		return " "
	case gpucontext.KeyEnter:
		return "enter"
	case gpucontext.KeyLeft:
		return "left"
	case gpucontext.KeyRight:
		return "right"
	case gpucontext.KeyUp:
		return "up"
	case gpucontext.KeyDown:
		return "down"
	case gpucontext.KeyLeftBracket:
		return "["
	case gpucontext.KeyRightBracket:
		return "]"
	case gpucontext.KeyComma:
		return ","
	case gpucontext.KeyPeriod:
		return "."
	case gpucontext.KeyMinus:
		if shift {
			return "_"
		}
		return "-"
	case gpucontext.KeyEqual:
		if shift {
			return "+"
		}
		return "="
	}
	return ""
}

func currentMenuIndex(v *viewerState) int {
	if v == nil || v.runner == nil {
		return 0
	}
	return v.demoIndex + 1
}

func (h *hudOverlay) formatText(v *viewerState, cam nt3d.Camera, lighting *studioLighting, animate, pipView, loadCompose, showHUD, menuOpen bool, supersample int) string {
	arrow := "[v]"
	hint := "click or ` for menu"
	if menuOpen {
		arrow = "[^]"
		hint = "select below or Esc"
	}

	if !showHUD {
		if v.runner != nil {
			names := gallery.Names()
			return fmt.Sprintf("%s Scene: %d/%d %s (%s)   %5.1f FPS",
				arrow, v.demoIndex+1, len(names), v.runner.Name(), hint, h.fps)
		}
		return fmt.Sprintf("%s Scene: 0/%d %s (%s)   %5.1f FPS",
			arrow, len(gallery.Names()), v.summary, hint, h.fps)
	}

	var b strings.Builder
	pipStr, loadStr, shadowStr := "off", "off", "off"
	if pipView {
		pipStr = "on"
	}
	if loadCompose {
		loadStr = "on"
	}
	if lighting != nil && lighting.shadows {
		shadowStr = "on"
	}

	if v.runner != nil {
		names := gallery.Names()
		header, status := v.runner.HeaderAndStatus()
		ss := max(supersample, v.runner.Supersample())
		fmt.Fprintf(&b, "%s Scene: %d/%d %s (%s)   %5.1f FPS (%4.1f ms)\n",
			arrow, v.demoIndex+1, len(names), v.runner.Name(), hint, h.fps, h.frameMs)
		if header != "" {
			fmt.Fprintf(&b, "%s\n", header)
		}
		if status != "" {
			fmt.Fprintf(&b, "%s\n", status)
		}
		if lighting != nil && lighting.overrideInDemo {
			fmt.Fprintf(&b, "Env: %s   Light: %s (sun az %.0f el %.0f, shadows: %s)\n",
				lighting.envName(), lighting.rigName(), lighting.sunAzimuth, lighting.sunElevation, shadowStr)
		}
		fmt.Fprintf(&b, "Cam: el %.0f deg az %.0f deg dist %.2f   SS: %dx   PiP: %s   Load: %s\n",
			cam.Alpha, cam.Beta, cam.Distance, ss, pipStr, loadStr)
		if v.selectedHit != nil {
			pos, _ := v.pickWorldPos()
			name := v.selectedHit.Name
			if name == "" {
				name = fmt.Sprintf("node-%d", v.selectedHit.Node)
			}
			fmt.Fprintf(&b, "Pick: %s (#%d, %s #%d) @ (%.2f, %.2f, %.2f)   Gizmo: %s [F11/Bksp]\n",
				name, v.selectedHit.Node, primitiveName(v.selectedHit.Primitive), v.selectedHit.Index,
				pos.X, pos.Y, pos.Z, v.gizmoModeName())
		}
		if h.lastCapture != "" {
			fmt.Fprintf(&b, "Saved: %s\n", h.lastCapture)
		}
		b.WriteString("[`/Click] Menu [Tab] Next [Ctrl+O] Open [F6] Env [F7] Light [F8] Shadow [F9] Shot [F10] Turntable")
		return b.String()
	}

	fmt.Fprintf(&b, "%s Scene: 0/%d showcase (%s)   %5.1f FPS (%4.1f ms)\n",
		arrow, len(gallery.Names()), hint, h.fps, h.frameMs)

	animState := "paused"
	if animate {
		animState = "playing"
	}
	if v.totalSplats > 0 {
		shMode := "off"
		if v.hasSH {
			if v.dcOnly {
				shMode = "DC-only"
			} else {
				shMode = "full"
			}
		}
		budget := v.splatBudget
		if budget <= 0 {
			budget = nt3d.DefaultSplatBudget
		}
		fmt.Fprintf(&b, "Splats: %s (%d pts, SH: %s, budget: %d)\n", v.summary, v.totalSplats, shMode, budget)
	} else if len(v.clips) > 0 {
		clip := v.clips[v.clipIndex]
		fmt.Fprintf(&b, "Model: %s (%d tris, clip %d/%d %q %s)\n",
			v.summary, v.totalTriangles, v.clipIndex+1, len(v.clips), clip.Name, animState)
	} else {
		fmt.Fprintf(&b, "Scene: %s (%d tris, anim: %s)\n", v.summary, v.totalTriangles, animState)
	}

	if lighting != nil {
		fmt.Fprintf(&b, "Env: %s   Light: %s (sun az %.0f el %.0f, shadows: %s)\n",
			lighting.envName(), lighting.rigName(), lighting.sunAzimuth, lighting.sunElevation, shadowStr)
	}
	fmt.Fprintf(&b, "Cam: el %.0f deg az %.0f deg dist %.2f   SS: %dx   PiP: %s   Load: %s\n",
		cam.Alpha, cam.Beta, cam.Distance, supersample, pipStr, loadStr)

	if v.selectedHit != nil {
		pos, _ := v.pickWorldPos()
		name := v.selectedHit.Name
		if name == "" {
			name = fmt.Sprintf("node-%d", v.selectedHit.Node)
		}
		fmt.Fprintf(&b, "Pick: %s (#%d, %s #%d) @ (%.2f, %.2f, %.2f)   Gizmo: %s [W/Bksp]\n",
			name, v.selectedHit.Node, primitiveName(v.selectedHit.Primitive), v.selectedHit.Index,
			pos.X, pos.Y, pos.Z, v.gizmoModeName())
	} else {
		b.WriteString("Pick: none (left-click object or splat)\n")
	}
	if h.lastCapture != "" {
		fmt.Fprintf(&b, "Saved: %s\n", h.lastCapture)
	}
	b.WriteString("[`/Click] Menu [O/Drop] Open [W] Gizmo [E] Env [L] Light [K] Shadow [G] Floor [P] Shot [T] Turntable")
	return b.String()
}

func (h *hudOverlay) encode(hudSession *nt3d.GPUSession, dc *gogpu.Context, text string, menuOpen bool) error {
	if text != h.text || h.width == 0 || h.height == 0 {
		geom, err := (nt3d.Label{
			Text:        text,
			Pixels:      true,
			Transparent: true,
			NoPick:      true,
			Color:       color.RGBA{230, 238, 250, 255},
			Background:  color.RGBA{14, 20, 32, 205},
			BorderColor: color.RGBA{75, 140, 210, 230},
			Shadow:      color.RGBA{0, 0, 0, 210},
			Padding:     hudPadding,
			Border:      hudBorder,
		}).Geometry()
		if err != nil {
			return err
		}
		geom.Material.FullSize = true
		if _, err := h.scene.SetGeometry("hud", geom); err != nil {
			return err
		}
		h.width = int(math.Round(float64(geom.Vertices[2].Position.X * 2)))
		h.height = int(math.Round(float64(geom.Vertices[2].Position.Y * 2)))
		h.text = text
	}
	sw, sh := dc.SurfaceSize()
	if h.width <= 0 || h.height <= 0 || hudMargin+h.width > int(sw) || hudMargin+h.height > int(sh) {
		h.hudRect = image.Rectangle{}
		h.headerRect = image.Rectangle{}
		return nil
	}
	h.hudRect = image.Rect(hudMargin, hudMargin, hudMargin+h.width, hudMargin+h.height)
	headerBottom := min(h.hudRect.Max.Y, hudMargin+hudInset+hudLineHeight+4)
	h.headerRect = image.Rect(hudMargin, hudMargin, hudMargin+h.width, headerBottom)

	hudFrame, err := h.scene.Frame(nt3d.DefaultCamera(), h.width, h.height)
	if err != nil {
		return err
	}
	hudFrame.Environment = nil
	hudFrame.Background = color.RGBA{0, 0, 0, 0}

	// Highlight the clickable top scene-selector bar inside the HUD card.
	barAccent := color.RGBA{75, 155, 235, 255}
	if menuOpen {
		barAccent = color.RGBA{255, 210, 65, 255}
	}
	hudFrame.Overlays = append(hudFrame.Overlays,
		nt3d.OverlayRect{X: 2, Y: hudInset - 2, Width: 3, Height: hudLineHeight + 2, Color: barAccent},
		nt3d.OverlayRect{X: 2, Y: hudInset + hudLineHeight + 1, Width: h.width - 4, Height: 1, Color: color.RGBA{55, 95, 145, 200}},
	)

	return hudSession.Encode(hudFrame, dc.CommandEncoder(), nt3d.GPUTarget{
		Color:    dc.SurfaceView(),
		Format:   dc.Format(),
		Width:    int(sw),
		Height:   int(sh),
		Viewport: h.hudRect,
		Load:     true,
	})
}

func (h *hudOverlay) formatMenuText(activeIdx, hoverIdx int) string {
	var b strings.Builder
	b.WriteString("SELECT INTERACTIVE SCENE (Click row, Up/Down + Enter, or Esc)\n")
	for i, item := range demoCatalog {
		cursor := "  "
		if i == hoverIdx {
			cursor = "> "
		}
		active := " "
		if i == activeIdx {
			active = "*"
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%s%s %2d. %-11s  %s", cursor, active, i, item.name, item.desc)
	}
	return b.String()
}

func (h *hudOverlay) menuRowAt(px, py int) int {
	if !(image.Point{X: px, Y: py}).In(h.menuRect) {
		return -1
	}
	relY := py - h.menuRect.Min.Y - hudInset - menuHeaderRows*hudLineHeight
	if relY < 0 {
		return -1
	}
	row := relY / hudLineHeight
	if row < 0 || row >= len(demoCatalog) {
		return -1
	}
	return row
}

func (h *hudOverlay) encodeMenu(menuSession *nt3d.GPUSession, dc *gogpu.Context, activeIdx, hoverIdx int) error {
	text := h.formatMenuText(activeIdx, hoverIdx)
	if text != h.menuText || h.menuWidth == 0 || h.menuHeight == 0 {
		geom, err := (nt3d.Label{
			Text:        text,
			Pixels:      true,
			Transparent: true,
			NoPick:      true,
			Color:       color.RGBA{235, 242, 255, 255},
			Background:  color.RGBA{12, 18, 30, 238},
			BorderColor: color.RGBA{255, 205, 65, 245},
			Shadow:      color.RGBA{0, 0, 0, 230},
			Padding:     hudPadding,
			Border:      hudBorder,
		}).Geometry()
		if err != nil {
			return err
		}
		geom.Material.FullSize = true
		if _, err := h.menuScene.SetGeometry("menu", geom); err != nil {
			return err
		}
		h.menuWidth = int(math.Round(float64(geom.Vertices[2].Position.X * 2)))
		h.menuHeight = int(math.Round(float64(geom.Vertices[2].Position.Y * 2)))
		h.menuText = text
	}

	sw, sh := dc.SurfaceSize()
	menuX := hudMargin
	menuY := h.hudRect.Max.Y + 4
	if menuY <= hudMargin {
		menuY = hudMargin
	}
	if menuY+h.menuHeight > int(sh)-hudMargin {
		menuY = max(4, int(sh)-h.menuHeight-4)
	}
	if h.menuWidth <= 0 || h.menuHeight <= 0 || menuX+h.menuWidth > int(sw) || menuY+h.menuHeight > int(sh) {
		h.menuRect = image.Rectangle{}
		return nil
	}
	h.menuRect = image.Rect(menuX, menuY, menuX+h.menuWidth, menuY+h.menuHeight)

	menuFrame, err := h.menuScene.Frame(nt3d.DefaultCamera(), h.menuWidth, h.menuHeight)
	if err != nil {
		return err
	}
	menuFrame.Environment = nil
	menuFrame.Background = color.RGBA{0, 0, 0, 0}

	// Separator line beneath the menu header row.
	sepY := hudInset + menuHeaderRows*hudLineHeight - 1
	menuFrame.Overlays = append(menuFrame.Overlays,
		nt3d.OverlayRect{X: 2, Y: sepY, Width: h.menuWidth - 4, Height: 1, Color: color.RGBA{75, 140, 210, 220}},
	)
	// Highlight frame around the hovered/selected item row.
	if hoverIdx >= 0 && hoverIdx < len(demoCatalog) {
		rowY := hudInset + (menuHeaderRows+hoverIdx)*hudLineHeight - 1
		gold := color.RGBA{255, 210, 65, 255}
		menuFrame.Overlays = append(menuFrame.Overlays,
			nt3d.OverlayRect{X: 3, Y: rowY, Width: 4, Height: hudLineHeight + 1, Color: gold},
			nt3d.OverlayRect{X: h.menuWidth - 7, Y: rowY, Width: 4, Height: hudLineHeight + 1, Color: gold},
			nt3d.OverlayRect{X: 3, Y: rowY, Width: h.menuWidth - 6, Height: 1, Color: color.RGBA{95, 165, 245, 200}},
			nt3d.OverlayRect{X: 3, Y: rowY + hudLineHeight, Width: h.menuWidth - 6, Height: 1, Color: color.RGBA{95, 165, 245, 200}},
		)
	}

	return menuSession.Encode(menuFrame, dc.CommandEncoder(), nt3d.GPUTarget{
		Color:    dc.SurfaceView(),
		Format:   dc.Format(),
		Width:    int(sw),
		Height:   int(sh),
		Viewport: h.menuRect,
		Load:     true,
	})
}
