// Copyright (c) 2026 Neomantra Corp.

// Command gogpu demonstrates embedding NT3D inside a GoGPU window using
// nt3d.GPUSession and nt3d.GPUTarget without CPU readback. It serves as:
//   - A unified desktop application for all 21 interactive NT3D demos
//     (-demo <name>, or Tab / Shift+Tab / PageDown / PageUp to switch live).
//   - An interactive desktop 3D model and Gaussian splat viewer when given
//     -file or positional file paths (.glb, .gltf, .sog, .spz, .ply, .stl, .obj, .3mf).
//
// Controls:
//   - Drag & Drop files (.glb, .gltf, .sog, .spz, .ply, .stl, .obj, .3mf, .hdr, .png, .jpg) onto window
//   - O or Ctrl+O: open native file picker dialog for 3D models, splats, or panoramas (.hdr/.png/.jpg)
//   - Click top-left "[v] Scene: ..." bar (or press ` / F5): open interactive Demo Dropdown Menu
//   - Left-mouse drag: orbit camera (Alt + Left-drag: rotate sun/key light direction)
//   - Right-mouse drag (or Shift + Left-drag): pan camera
//   - Left-mouse click: pick object/primitive under cursor (3D annotation + outline + gizmo)
//   - Left-mouse drag on 3D Gizmo axis/ring/cube: translate, rotate, or scale selected node in 3D
//   - Click top-right 3D Camera Orientation Triad (+X/-X/+Y/-Y/+Z/-Z): snap camera to cardinal view
//   - W or F11: cycle 3D Transform Gizmo mode (Translate -> Rotate -> Scale -> Off)
//   - Backspace / Delete: reset selected node's transform to its original pose
//   - Mouse wheel: zoom camera
//   - E or F6: cycle Environment / IBL skybox preset (including HDR Studio Sun or dropped .hdr/.png)
//   - L or F7: cycle Lighting Rig preset (Key+Sky, 3-Point, Spotlights, RGB Lamps, Lantern, 8-Lamp, Pure IBL)
//   - K or F8: toggle real-time GPU Shadow Mapping (orthographic sun or perspective spotlight)
//   - N: toggle Weighted Blended Order-Independent Transparency (OIT, in viewer mode)
//   - M or 4: toggle 4x GPU MSAA (multisample anti-aliasing); S toggles 2x supersampling in viewer mode
//   - X or F12: cycle GPU Post-Processing preset (Off, Studio, Bloom+ACES, SSAO, Cinema Warm, Reinhard)
//   - P or F9: capture offscreen PNG screenshot via GPUSession.RenderToImage
//   - T or F10: capture 360-degree turntable animated GIF via GPUSession.RenderToImage
//   - Ctrl+S / Cmd+S (or Shift+P): save screenshot (.png) or turntable (.gif) via native Save dialog
//   - [ / ] and , / .: adjust sun/key light azimuth and elevation (in showcase/viewer mode)
//   - G: toggle studio shadow-catcher pedestal floor (in file viewer mode)
//   - Tab / PageDown: switch to next interactive demo in the 20-demo gallery
//   - Shift+Tab / PageUp: switch to previous interactive demo in the gallery
//   - ` (Grave) or F5: toggle interactive Demo Dropdown Menu (Up/Down + Enter or mouse click)
//   - F1 (or I): toggle 2D HUD overlay and 3D pick annotations
//   - V (or Shift+V): cycle glTF KHR_materials_variants colorways when loaded model defines variants
//   - D (or Shift+L): toggle multi-LOD auto collapse vs raw hierarchy (- / = / 0 adjust LOD bias)
//   - F2 (or V when no variants): toggle picture-in-picture sub-viewport (GPUTarget.Viewport)
//   - F3 (or B): toggle Load compositing mode over GoGPU background
//   - F4 (or 2): open a second GoGPU window sharing the same GPU device
//   - Escape: close dropdown menu, or quit application
//   - In demo mode: all per-demo keys (a, b, c, d, e, f, h, i, j, k, l, m, n, o, p, r, s, t, u, v, w, x, y, z, [, ], comma, period, space, enter, arrows)
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"log"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gogpu/gogpu"
	"github.com/gogpu/gpucontext"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/examples/demos/gallery"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
	_ "nimbleterminal.dev/nt3d/mesh/formats"
)

func main() {
	demoNames := gallery.Names()
	demoFlag := flag.String("demo", "", "interactive demo to run ("+strings.Join(demoNames, ", ")+", or 'list')")
	fileFlag := flag.String("file", "", "path to a 3D model or Gaussian splat file (.glb, .gltf, .sog, .spz, .ply, .stl, .obj, .3mf)")
	envFlag := flag.String("env", "", "path to an environment panorama (.hdr, .pic, .png, .jpg)")
	yUp := flag.Bool("y-up", false, "convert Y-up files to Z-up when loading")
	width := flag.Int("width", 960, "initial window width")
	height := flag.Int("height", 640, "initial window height")
	msaaFlag := flag.Bool("msaa", false, "enable 4x GPU multisample anti-aliasing (M or 4 toggles)")
	oitFlag := flag.Bool("oit", true, "enable Weighted Blended Order-Independent Transparency (N toggles in viewer)")
	supersampleFlag := flag.Int("supersample", 1, "GPU supersampling factor, 1..4 (S toggles in viewer)")
	postFlag := flag.String("post", "off", "GPU post-processing preset: off, studio, bloom, ssao, cinema, reinhard (X or F12 cycles)")
	maxFrames := flag.Int("frames", 0, "exit automatically after N rendered frames (0 = interactive)")
	screenshotFlag := flag.String("screenshot", "", "render an offscreen PNG screenshot to the given file path via GPUSession.RenderToImage")
	turntableFlag := flag.String("turntable", "", "render a 360-degree turntable (.gif or .png prefix) via GPUSession.RenderToImage")
	turntableFrames := flag.Int("turntable-frames", 36, "number of frames for 360-degree turntable capture")
	turntableFPS := flag.Int("turntable-fps", 24, "frames per second for turntable GIF capture")
	flag.Parse()

	if *demoFlag == "list" {
		fmt.Println("Available interactive demos:")
		for i, name := range demoNames {
			fmt.Printf("  %2d. %s\n", i+1, name)
		}
		os.Exit(0)
	}

	var files []string
	if *fileFlag != "" {
		files = append(files, *fileFlag)
	}
	files = append(files, flag.Args()...)

	upAxis := nt3dmesh.ZUp
	if *yUp {
		upAxis = nt3dmesh.YUp
	}

	camera := nt3d.DefaultCamera()
	camera.Alpha = 22
	camera.Beta = -38
	camera.Distance = 3.2

	title := "NT3D + GoGPU (19 Interactive Demos & 3D Viewer)"
	if *demoFlag != "" {
		title = "NT3D + GoGPU — Demo: " + *demoFlag
	} else if len(files) == 1 {
		title = "NT3D Viewer — " + filepath.Base(files[0])
	} else if len(files) > 1 {
		title = fmt.Sprintf("NT3D Viewer — %d files", len(files))
	}

	app := gogpu.NewApp(gogpu.DefaultConfig().
		WithTitle(title).
		WithSize(*width, *height).
		WithContinuousRender(true))

	var (
		viewer          *viewerState
		lighting        = newStudioLighting()
		hud             = newHUDOverlay()
		session         *nt3d.GPUSession
		pipSession      *nt3d.GPUSession
		hudSession      *nt3d.GPUSession
		menuSession     *nt3d.GPUSession
		secondSess      *nt3d.GPUSession
		lastFrame       nt3d.Frame
		hasFrame        bool
		leftDown        bool
		rightDown       bool
		shiftDown       bool
		altDown         bool
		uiConsumedMouse bool
		menuOpen        bool
		menuHover       int
		lastX           float64
		lastY           float64
		dragDist        float64
		animate         = true
		showHUD         = true
		pipView         = len(files) == 0 && *demoFlag == ""
		loadCompose     = false
		msaa            = *msaaFlag
		supersample     = max(1, min(nt3d.MaxSupersample, *supersampleFlag))
		angle           float64
		frameCount      int
		wantSecond      bool
		hasSecond       bool
	)
	lighting.oit = *oitFlag
	if err := lighting.setPostByName(*postFlag); err != nil {
		log.Fatal(err)
	}

	defer func() {
		viewer.close()
		if secondSess != nil {
			_ = secondSess.Close()
		}
		if menuSession != nil {
			_ = menuSession.Close()
		}
		if hudSession != nil {
			_ = hudSession.Close()
		}
		if pipSession != nil {
			_ = pipSession.Close()
		}
		if session != nil {
			_ = session.Close()
		}
	}()

	loadPanoramaPath := func(p string) bool {
		switch strings.ToLower(filepath.Ext(p)) {
		case ".hdr", ".pic":
			data, err := os.ReadFile(p)
			if err != nil {
				log.Printf("read HDR panorama %q: %v", p, err)
				return false
			}
			env, err := nt3d.NewHDREnvironment(data)
			if err != nil {
				log.Printf("NewHDREnvironment(%q): %v", p, err)
				return false
			}
			lighting.setCustomPanorama(filepath.Base(p), env, viewer)
			fmt.Printf("Loaded custom HDR environment panorama: %s\n", filepath.Base(p))
			return true
		case ".png", ".jpg", ".jpeg":
			f, err := os.Open(p)
			if err != nil {
				log.Printf("open panorama %q: %v", p, err)
				return false
			}
			img, _, err := image.Decode(f)
			_ = f.Close()
			if err != nil {
				log.Printf("decode panorama %q: %v", p, err)
				return false
			}
			env, err := nt3d.NewEnvironment(img)
			if err != nil {
				log.Printf("NewEnvironment(%q): %v", p, err)
				return false
			}
			lighting.setCustomPanorama(filepath.Base(p), env, viewer)
			fmt.Printf("Loaded custom environment panorama: %s (%dx%d)\n",
				filepath.Base(p), img.Bounds().Dx(), img.Bounds().Dy())
			return true
		default:
			return false
		}
	}
	if *envFlag != "" {
		loadPanoramaPath(*envFlag)
	}

	loadDroppedOrSelectedPaths := func(paths []string) {
		if len(paths) == 0 {
			return
		}
		var modelPaths []string
		for _, p := range paths {
			switch strings.ToLower(filepath.Ext(p)) {
			case ".hdr", ".pic", ".png", ".jpg", ".jpeg":
				loadPanoramaPath(p)
			default:
				modelPaths = append(modelPaths, p)
			}
		}
		if len(modelPaths) == 0 {
			return
		}
		nextViewer, err := loadModelScene(modelPaths, upAxis)
		if err != nil {
			log.Printf("loadModelScene(%v): %v", modelPaths, err)
			return
		}
		viewer.close()
		viewer = nextViewer
		files = modelPaths
		lighting.overrideInDemo = false
		lighting.applyEnvironment(viewer)
		w, h := *width, *height
		if hasFrame && lastFrame.Width > 0 && lastFrame.Height > 0 {
			w, h = lastFrame.Width, lastFrame.Height
		}
		camera = nt3d.DefaultCamera()
		camera.Alpha = 22
		camera.Beta = -38
		camera.Distance = 3.2
		if viewer.fitBounds.IsValid() && h > 0 {
			camera = camera.Fit(viewer.fitBounds, float64(w)/float64(h))
		}
		pipView = false
		menuOpen = false
		menuHover = 0
		if len(files) == 1 {
			app.SetTitle("NT3D Viewer — " + filepath.Base(files[0]))
		} else {
			app.SetTitle(fmt.Sprintf("NT3D Viewer — %d files", len(files)))
		}
		fmt.Printf("Loaded %d file(s): %s\n", len(files), viewer.summary)
	}

	openFileDialog := func() {
		paths, err := app.ShowOpenFileDialog(gogpu.FileDialogOptions{
			Title: "Open 3D Model, Gaussian Splat, or Panorama Image",
			Filters: []gogpu.FileTypeFilter{
				{
					Name:       "3D Models, Splats & Panoramas",
					Extensions: []string{"glb", "gltf", "sog", "spz", "ply", "stl", "obj", "3mf", "hdr", "pic", "png", "jpg", "jpeg"},
				},
				{
					Name:       "3D Models & Gaussian Splats",
					Extensions: []string{"glb", "gltf", "sog", "spz", "ply", "stl", "obj", "3mf"},
				},
				{
					Name:       "Environment Panoramas (.hdr, .png, .jpg)",
					Extensions: []string{"hdr", "pic", "png", "jpg", "jpeg"},
				},
			},
			Multiple: true,
		})
		if err != nil {
			log.Printf("ShowOpenFileDialog: %v", err)
			return
		}
		loadDroppedOrSelectedPaths(paths)
	}

	app.OnDragDrop(func(paths []string, _, _ float64) {
		loadDroppedOrSelectedPaths(paths)
	})

	currentCaptureDims := func() (int, int) {
		w, h := *width, *height
		if hasFrame && lastFrame.Width > 0 && lastFrame.Height > 0 {
			w, h = lastFrame.Width, lastFrame.Height
		}
		return max(1, w), max(1, h)
	}

	runScreenshot := func(path string) {
		if session == nil || viewer == nil {
			return
		}
		if path == "" {
			path = fmt.Sprintf("nt3d-screenshot-%s.png", time.Now().Format("20060102-150405"))
		} else if filepath.Ext(path) == "" {
			path += ".png"
		}
		w, h := currentCaptureDims()
		if err := captureScreenshot(session, viewer, camera, lighting, w, h, msaa, supersample, path); err != nil {
			log.Printf("captureScreenshot(%q): %v", path, err)
			return
		}
		hud.lastCapture = fmt.Sprintf("%s (%dx%d)", filepath.Base(path), w, h)
		fmt.Printf("Saved offscreen screenshot: %s (%dx%d)\n", path, w, h)
	}

	runTurntable := func(path string) {
		if session == nil || viewer == nil {
			return
		}
		if path == "" {
			path = fmt.Sprintf("nt3d-turntable-%s.gif", time.Now().Format("20060102-150405"))
		} else if filepath.Ext(path) == "" {
			path += ".gif"
		}
		w, h := currentCaptureDims()
		nFrames := max(1, *turntableFrames)
		fps := max(1, *turntableFPS)
		if err := captureTurntable(session, viewer, camera, lighting, w, h, msaa, supersample, nFrames, fps, path); err != nil {
			log.Printf("captureTurntable(%q): %v", path, err)
			return
		}
		hud.lastCapture = fmt.Sprintf("%s (%d frames, %dx%d)", filepath.Base(path), nFrames, w, h)
		fmt.Printf("Saved 360° turntable (%d frames @ %d FPS): %s (%dx%d)\n", nFrames, fps, path, w, h)
	}

	saveCaptureDialog := func() {
		if session == nil || viewer == nil {
			return
		}
		path, err := app.ShowSaveFileDialog(gogpu.FileDialogOptions{
			Title:           "Save Screenshot (.png) or 360° Turntable (.gif)",
			DefaultFilename: fmt.Sprintf("nt3d-capture-%s.png", time.Now().Format("20060102-150405")),
			Filters: []gogpu.FileTypeFilter{
				{
					Name:       "PNG Screenshot (*.png)",
					Extensions: []string{"png"},
				},
				{
					Name:       "360° Turntable Animated GIF (*.gif)",
					Extensions: []string{"gif"},
				},
			},
		})
		if err != nil {
			log.Printf("ShowSaveFileDialog: %v", err)
			return
		}
		if path == "" {
			return
		}
		if strings.EqualFold(filepath.Ext(path), ".gif") {
			runTurntable(path)
		} else {
			runScreenshot(path)
		}
	}

	selectMenuIndex := func(menuIdx int) {
		if menuIdx < 0 || menuIdx >= len(demoCatalog) {
			return
		}
		w, h := *width, *height
		if hasFrame && lastFrame.Width > 0 && lastFrame.Height > 0 {
			w, h = lastFrame.Width, lastFrame.Height
		}
		if menuIdx == 0 {
			var nextViewer *viewerState
			var err error
			if len(files) > 0 {
				nextViewer, err = loadModelScene(files, upAxis)
			} else {
				nextViewer, err = buildDemoScene()
			}
			if err != nil {
				log.Printf("select showcase: %v", err)
				return
			}
			viewer.close()
			viewer = nextViewer
			lighting.overrideInDemo = false
			lighting.applyEnvironment(viewer)
			camera = nt3d.DefaultCamera()
			camera.Alpha = 22
			camera.Beta = -38
			camera.Distance = 3.2
			if len(files) > 0 && viewer.fitBounds.IsValid() && h > 0 {
				camera = camera.Fit(viewer.fitBounds, float64(w)/float64(h))
			}
			menuHover = 0
			fmt.Println("Switched to scene [0]: showcase")
			return
		}
		demoIdx := menuIdx - 1
		name := demoNames[demoIdx]
		nextViewer, err := loadGalleryDemo(name, demoIdx, files, upAxis, *yUp)
		if err != nil {
			log.Printf("loadGalleryDemo(%q): %v", name, err)
			return
		}
		viewer.close()
		viewer = nextViewer
		lighting.overrideInDemo = false
		lighting.overridePostInDemo = false
		viewer.runner.SetViewportSize(w, h)
		camera = viewer.runner.Camera()
		menuHover = menuIdx
		fmt.Printf("Switched to demo [%d/%d]: %s\n", demoIdx+1, len(demoNames), name)
	}

	switchDemo := func(step int) {
		nextIdx := 0
		if viewer != nil && viewer.runner != nil {
			nextIdx = (viewer.demoIndex + step + len(demoNames)) % len(demoNames)
		} else if step < 0 {
			nextIdx = len(demoNames) - 1
		}
		selectMenuIndex(nextIdx + 1)
	}

	events := app.EventSource()
	events.OnMousePress(func(button gpucontext.MouseButton, x, y float64) {
		scale := app.ScaleFactor()
		if scale <= 0 {
			scale = 1
		}
		px, py := int(x*scale), int(y*scale)
		pt := image.Point{X: px, Y: py}

		if button == gpucontext.MouseButtonLeft {
			if pt.In(hud.headerRect) {
				menuOpen = !menuOpen
				if menuOpen {
					menuHover = currentMenuIndex(viewer)
				}
				uiConsumedMouse = true
				return
			}
			if menuOpen {
				if row := hud.menuRowAt(px, py); row >= 0 {
					uiConsumedMouse = true
					menuOpen = false
					selectMenuIndex(row)
					return
				}
				if pt.In(hud.menuRect) {
					uiConsumedMouse = true
					return
				}
				menuOpen = false
			}
		}

		switch button {
		case gpucontext.MouseButtonLeft:
			if viewer != nil && hasFrame && showHUD && !altDown && !shiftDown {
				if axis := hitTestOrientationTriad(&lastFrame, camera, float32(px), float32(py)); axis != 0 {
					if viewer.runner != nil {
						viewer.runner.DetachSampleCamera()
						camera = viewer.runner.Camera()
					}
					var viewName string
					camera, viewName = snapCameraToAxis(camera, axis)
					if viewer.runner != nil {
						viewer.runner.SetCamera(camera)
					}
					uiConsumedMouse = true
					fmt.Printf("Camera snapped to %s\n", viewName)
					return
				}
			}
			if viewer != nil && hasFrame && !altDown && !shiftDown &&
				viewer.beginGizmoDrag(lastFrame, camera, float32(px), float32(py)) {
				leftDown = true
				lastX, lastY = x, y
				dragDist = 0
				return
			}
			leftDown = true
			lastX, lastY = x, y
			dragDist = 0
		case gpucontext.MouseButtonRight:
			rightDown = true
			lastX, lastY = x, y
		}
	})
	events.OnMouseRelease(func(button gpucontext.MouseButton, x, y float64) {
		if uiConsumedMouse {
			uiConsumedMouse = false
			leftDown = false
			return
		}
		switch button {
		case gpucontext.MouseButtonLeft:
			if viewer != nil && viewer.gizmoDragging {
				viewer.endGizmoDrag()
				leftDown = false
				return
			}
			wasClick := leftDown && dragDist < 4.0 && !altDown
			leftDown = false
			if wasClick && viewer != nil && hasFrame {
				scale := app.ScaleFactor()
				if scale <= 0 {
					scale = 1
				}
				px, py := float32(x*scale), float32(y*scale)
				tol := float32(6 * scale)
				if viewer.runner != nil {
					hit := viewer.runner.PickGPU(session, camera, lastFrame, px, py, tol)
					if viewer.runner.HandlesOwnPick() {
						viewer.clearPick()
						return
					}
					if hit != nil {
						fmt.Printf("Picked %q (node=%d %s=%d pos=[%.3f, %.3f, %.3f] depth=%.4f)\n",
							hit.Name, hit.Node, primitiveName(hit.Primitive), hit.Index,
							hit.Position.X, hit.Position.Y, hit.Position.Z, hit.Depth)
						viewer.selectPick(hit, camera)
					} else {
						viewer.clearPick()
					}
					return
				}
				var hit *nt3d.Hit
				if session != nil {
					hit = session.Pick(viewer.scene, camera, lastFrame, px, py, tol)
				} else {
					hit = viewer.scene.Pick(camera, lastFrame, px, py, tol)
				}
				if hit != nil {
					fmt.Printf("Picked %q (node=%d %s=%d pos=[%.3f, %.3f, %.3f] depth=%.4f)\n",
						hit.Name, hit.Node, primitiveName(hit.Primitive), hit.Index,
						hit.Position.X, hit.Position.Y, hit.Position.Z, hit.Depth)
					viewer.selectPick(hit, camera)
				} else {
					viewer.clearPick()
				}
			}
		case gpucontext.MouseButtonRight:
			rightDown = false
		}
	})
	events.OnMouseMove(func(x, y float64) {
		scale := app.ScaleFactor()
		if scale <= 0 {
			scale = 1
		}
		if menuOpen {
			if row := hud.menuRowAt(int(x*scale), int(y*scale)); row >= 0 {
				menuHover = row
			}
		}
		if viewer != nil && viewer.gizmoDragging && hasFrame {
			viewer.updateGizmoDrag(lastFrame, camera, float32(x*scale), float32(y*scale), showHUD)
			return
		}
		if uiConsumedMouse || (!leftDown && !rightDown) {
			if viewer != nil && hasFrame && !menuOpen {
				fpx, fpy := float32(x*scale), float32(y*scale)
				if showHUD {
					viewer.orientHoverAxis = hitTestOrientationTriad(&lastFrame, camera, fpx, fpy)
				} else {
					viewer.orientHoverAxis = 0
				}
				viewer.gizmoHoverAxis = viewer.hitTestGizmo(lastFrame, camera, fpx, fpy)
			}
			return
		}
		dx, dy := x-lastX, y-lastY
		lastX, lastY = x, y
		dragDist += math.Hypot(dx, dy)
		if leftDown && altDown {
			lighting.rotateSun(dx*0.6, -dy*0.4, viewer)
			return
		}
		if viewer != nil && viewer.runner != nil {
			viewer.runner.DetachSampleCamera()
			camera = viewer.runner.Camera()
		}
		if rightDown || (leftDown && shiftDown) {
			scale := app.ScaleFactor()
			if scale <= 0 {
				scale = 1
			}
			w, h := *width, *height
			if hasFrame {
				w, h = lastFrame.Width, lastFrame.Height
			}
			camera = camera.Pan(float32(dx*scale), float32(dy*scale), w, h)
		} else if leftDown {
			camera = camera.Orbit(-dx*0.4, dy*0.4)
		}
		if viewer != nil && viewer.runner != nil {
			viewer.runner.SetCamera(camera)
		}
	})
	events.OnScroll(func(_, dy float64) {
		if viewer != nil && viewer.runner != nil {
			viewer.runner.DetachSampleCamera()
			camera = viewer.runner.Camera()
		}
		camera = camera.Zoom(math.Pow(0.92, dy))
		if viewer != nil && viewer.runner != nil {
			viewer.runner.SetCamera(camera)
		}
	})
	events.OnKeyPress(func(key gpucontext.Key, mods gpucontext.Modifiers) {
		shiftDown = mods&gpucontext.ModShift != 0
		altDown = mods&gpucontext.ModAlt != 0
		if (mods & (gpucontext.ModControl | gpucontext.ModSuper)) != 0 {
			switch key {
			case gpucontext.KeyO:
				openFileDialog()
				return
			case gpucontext.KeyS:
				saveCaptureDialog()
				return
			}
		}
		switch key {
		case gpucontext.KeyLeftShift, gpucontext.KeyRightShift:
			shiftDown = true
			return
		case gpucontext.KeyLeftAlt, gpucontext.KeyRightAlt:
			altDown = true
			return
		case gpucontext.KeyGrave, gpucontext.KeyF5:
			menuOpen = !menuOpen
			if menuOpen {
				menuHover = currentMenuIndex(viewer)
			}
			return
		case gpucontext.KeyEscape:
			if menuOpen {
				menuOpen = false
				return
			}
			app.Quit()
			return
		}

		if menuOpen {
			switch key {
			case gpucontext.KeyUp:
				menuHover = (menuHover - 1 + len(demoCatalog)) % len(demoCatalog)
				return
			case gpucontext.KeyDown:
				menuHover = (menuHover + 1) % len(demoCatalog)
				return
			case gpucontext.KeyEnter, gpucontext.KeySpace:
				menuOpen = false
				selectMenuIndex(menuHover)
				return
			}
		}

		switch key {
		case gpucontext.KeyTab:
			if shiftDown {
				switchDemo(-1)
			} else {
				switchDemo(1)
			}
			return
		case gpucontext.KeyPageDown:
			switchDemo(1)
			return
		case gpucontext.KeyPageUp:
			switchDemo(-1)
			return
		case gpucontext.KeyF1:
			showHUD = !showHUD
			fmt.Printf("HUD & 3D pick annotations: %v\n", showHUD)
			return
		case gpucontext.KeyF2:
			pipView = !pipView
			fmt.Printf("Sub-viewport inset: %v\n", pipView)
			return
		case gpucontext.KeyF3:
			loadCompose = !loadCompose
			fmt.Printf("Load composition mode: %v\n", loadCompose)
			return
		case gpucontext.KeyF4, gpucontext.Key2:
			if !hasSecond {
				wantSecond = true
			}
			return
		case gpucontext.Key4:
			msaa = !msaa
			fmt.Printf("4x MSAA: %v\n", msaa)
			return
		case gpucontext.KeyF6:
			lighting.cycleEnvironment(viewer)
			return
		case gpucontext.KeyF7:
			lighting.cycleRig()
			return
		case gpucontext.KeyF8:
			lighting.toggleShadows()
			return
		case gpucontext.KeyF9:
			runScreenshot("")
			return
		case gpucontext.KeyF10:
			runTurntable("")
			return
		case gpucontext.KeyF11:
			if viewer != nil {
				viewer.cycleGizmoMode()
			}
			return
		case gpucontext.KeyF12:
			lighting.cyclePost()
			return
		case gpucontext.KeyBackspace, gpucontext.KeyDelete:
			if viewer != nil {
				viewer.resetSelectedTransform(camera, showHUD)
			}
			return
		}

		if viewer != nil && viewer.runner != nil {
			name := viewer.runner.Name()
			if key == gpucontext.KeyI && name != "forest" && name != "large" {
				showHUD = !showHUD
				fmt.Printf("HUD & 3D pick annotations: %v\n", showHUD)
				return
			}
			demoKey := keyToDemoString(key, shiftDown)
			if demoKey != "" {
				switch demoKey {
				case "c", "[", "]", "b", "x", "n", "m", "s", "f", "l", "t", "i":
					viewer.clearPick()
				}
				viewer.runner.SendKey(demoKey)
				camera = viewer.runner.Camera()
			}
			return
		}

		switch key {
		case gpucontext.KeySpace:
			animate = !animate
		case gpucontext.KeyO:
			openFileDialog()
		case gpucontext.KeyW:
			if viewer != nil {
				viewer.cycleGizmoMode()
			}
		case gpucontext.KeyP:
			if shiftDown {
				saveCaptureDialog()
			} else {
				runScreenshot("")
			}
		case gpucontext.KeyT:
			runTurntable("")
		case gpucontext.KeyE:
			lighting.cycleEnvironment(viewer)
		case gpucontext.KeyL:
			if shiftDown && viewer != nil && viewer.hasLOD {
				viewer.toggleLODMode()
			} else {
				lighting.cycleRig()
			}
		case gpucontext.KeyD:
			if viewer != nil && viewer.hasLOD {
				viewer.toggleLODMode()
			}
		case gpucontext.KeyMinus:
			if viewer != nil && viewer.hasLOD {
				viewer.stepLODBias(0.9)
			}
		case gpucontext.KeyEqual:
			if viewer != nil && viewer.hasLOD {
				viewer.stepLODBias(1.1)
			}
		case gpucontext.Key0:
			if viewer != nil && viewer.hasLOD {
				viewer.stepLODBias(0)
			}
		case gpucontext.KeyK:
			lighting.toggleShadows()
		case gpucontext.KeyN:
			lighting.toggleOIT()
		case gpucontext.KeyG:
			if viewer != nil {
				viewer.toggleModelFloor()
			}
		case gpucontext.KeyLeftBracket:
			lighting.rotateSun(-10, 0, viewer)
		case gpucontext.KeyRightBracket:
			lighting.rotateSun(10, 0, viewer)
		case gpucontext.KeyComma:
			lighting.rotateSun(0, -6, viewer)
		case gpucontext.KeyPeriod:
			lighting.rotateSun(0, 6, viewer)
		case gpucontext.KeyF:
			if viewer != nil && viewer.fitBounds.IsValid() {
				aspect := float64(*width) / float64(*height)
				if hasFrame && lastFrame.Height > 0 {
					aspect = float64(lastFrame.Width) / float64(lastFrame.Height)
				}
				camera = camera.Fit(viewer.fitBounds, aspect)
			}
		case gpucontext.KeyC:
			if viewer != nil {
				viewer.cycleClip()
			}
		case gpucontext.KeyH:
			if viewer != nil {
				viewer.toggleSH()
			}
		case gpucontext.KeyU:
			if viewer != nil {
				viewer.adjustSplatBudget(0.5)
			}
		case gpucontext.KeyY:
			if viewer != nil {
				viewer.adjustSplatBudget(2.0)
			}
		case gpucontext.KeyZ:
			if viewer != nil {
				viewer.adjustSplatBudget(0)
			}
		case gpucontext.KeyI:
			showHUD = !showHUD
			fmt.Printf("HUD & 3D pick annotations: %v\n", showHUD)
		case gpucontext.KeyV:
			if viewer != nil && len(viewer.variants) > 0 {
				if shiftDown {
					viewer.stepVariant(-1)
				} else {
					viewer.stepVariant(1)
				}
			} else {
				pipView = !pipView
				fmt.Printf("Sub-viewport inset: %v\n", pipView)
			}
		case gpucontext.KeyB:
			loadCompose = !loadCompose
			fmt.Printf("Load composition mode: %v\n", loadCompose)
		case gpucontext.KeyM:
			msaa = !msaa
			fmt.Printf("4x MSAA: %v\n", msaa)
		case gpucontext.KeyX:
			lighting.cyclePost()
		case gpucontext.KeyS:
			if supersample == 1 {
				supersample = 2
			} else {
				supersample = 1
			}
			fmt.Printf("Supersample: %dx\n", supersample)
		}
	})
	events.OnKeyRelease(func(key gpucontext.Key, mods gpucontext.Modifiers) {
		shiftDown = mods&gpucontext.ModShift != 0
		altDown = mods&gpucontext.ModAlt != 0
		switch key {
		case gpucontext.KeyLeftShift, gpucontext.KeyRightShift:
			shiftDown = false
		case gpucontext.KeyLeftAlt, gpucontext.KeyRightAlt:
			altDown = false
		}
	})

	app.OnUpdate(func(dt float64) {
		hud.recordFrame(dt)
		if animate {
			lighting.step(dt)
		}
		if viewer == nil {
			return
		}
		if viewer.runner != nil {
			viewer.runner.Step(dt)
			camera = viewer.runner.Camera()
		} else if animate {
			if viewer.player != nil {
				_ = viewer.player.Step(dt)
			} else if viewer.rigID != nt3d.Root {
				angle += dt * 1.2
				bob := float32(0.15 * math.Sin(angle*2))
				_ = viewer.scene.SetTransform(viewer.rigID, nt3dmath.Translation(nt3dmath.Vec3{Z: bob}).Mul(nt3dmath.RotationZ(angle)))
			}
		}
		if wantSecond && !hasSecond && session != nil {
			wantSecond = false
			hasSecond = true
			var err error
			secondSess, err = session.NewView()
			if err != nil {
				log.Printf("session.NewView (window 2): %v", err)
				return
			}
			w2, err := app.NewWindow(gogpu.DefaultConfig().
				WithTitle("NT3D Top View (Shared GPU Device)").
				WithSize(480, 360))
			if err != nil {
				log.Printf("NewWindow: %v", err)
				return
			}
			w2.SetOnDraw(func(dc *gogpu.Context) {
				if viewer == nil {
					return
				}
				w, h := dc.SurfaceSize()
				if w == 0 || h == 0 {
					return
				}
				topCam := camera
				topCam.Alpha = 78
				topCam.Beta = camera.Beta + 90
				f, err := viewer.buildFrame(topCam, int(w), int(h), msaa, supersample)
				if err != nil {
					return
				}
				lighting.applyFrameLighting(&f, viewer)
				lighting.applyFramePost(&f, viewer.runner != nil)
				viewer.decorateFrame(&f, topCam, showHUD, true)
				if err := secondSess.Encode(f, dc.CommandEncoder(), nt3d.GPUTarget{
					Color:  dc.SurfaceView(),
					Format: dc.Format(),
					Width:  int(w),
					Height: int(h),
				}); err != nil {
					log.Printf("window 2 encode: %v", err)
					return
				}
				dc.MarkPreserveContent()
			})
		}
	})

	app.OnDraw(func(dc *gogpu.Context) {
		if session == nil {
			provider := app.GPUContextProvider()
			if provider == nil {
				return
			}
			var err error
			session, err = nt3d.NewGPUSessionFromProvider(provider)
			if err != nil {
				log.Fatalf("NewGPUSessionFromProvider: %v", err)
			}
			// Adopt the GPU device's binding limit before decoding any files so
			// large meshes and multi-million Gaussian splat clouds stay unsplit.
			_, _ = nt3d.AdoptBindingLimit(session)

			w, h := dc.SurfaceSize()
			if w == 0 || h == 0 {
				w, h = uint32(*width), uint32(*height)
			}

			switch {
			case *demoFlag != "":
				demoIdx := max(0, slices.Index(demoNames, *demoFlag))
				viewer, err = loadGalleryDemo(*demoFlag, demoIdx, files, upAxis, *yUp)
				if err != nil {
					log.Fatalf("loadGalleryDemo: %v", err)
				}
				viewer.runner.SetViewportSize(int(w), int(h))
				camera = viewer.runner.Camera()
			case len(files) > 0:
				viewer, err = loadModelScene(files, upAxis)
				if err != nil {
					log.Fatalf("loadModelScene: %v", err)
				}
				lighting.applyEnvironment(viewer)
				aspect := float64(w) / float64(h)
				if viewer.fitBounds.IsValid() {
					camera = camera.Fit(viewer.fitBounds, aspect)
				}
			default:
				viewer, err = buildDemoScene()
				if err != nil {
					log.Fatalf("buildDemoScene: %v", err)
				}
				lighting.applyEnvironment(viewer)
			}

			if *screenshotFlag != "" || *turntableFlag != "" {
				cw, ch := max(1, *width), max(1, *height)
				if *screenshotFlag != "" {
					if err := captureScreenshot(session, viewer, camera, lighting, cw, ch, msaa, supersample, *screenshotFlag); err != nil {
						log.Fatalf("captureScreenshot(%q): %v", *screenshotFlag, err)
					}
					hud.lastCapture = fmt.Sprintf("%s (%dx%d)", filepath.Base(*screenshotFlag), cw, ch)
					fmt.Printf("Saved offscreen screenshot: %s (%dx%d)\n", *screenshotFlag, cw, ch)
				}
				if *turntableFlag != "" {
					nFrames := max(1, *turntableFrames)
					fps := max(1, *turntableFPS)
					if err := captureTurntable(session, viewer, camera, lighting, cw, ch, msaa, supersample, nFrames, fps, *turntableFlag); err != nil {
						log.Fatalf("captureTurntable(%q): %v", *turntableFlag, err)
					}
					hud.lastCapture = fmt.Sprintf("%s (%d frames, %dx%d)", filepath.Base(*turntableFlag), nFrames, cw, ch)
					fmt.Printf("Saved 360° turntable (%d frames @ %d FPS): %s (%dx%d)\n", nFrames, fps, *turntableFlag, cw, ch)
				}
				if *maxFrames == 0 {
					app.Quit()
					return
				}
			}
		}

		w, h := dc.SurfaceSize()
		if w == 0 || h == 0 || viewer == nil {
			return
		}

		if viewer.runner != nil {
			viewer.runner.SetViewportSize(int(w), int(h))
		}
		viewer.updatePickAnnotation(camera, showHUD)

		f, err := viewer.buildFrame(camera, int(w), int(h), msaa, supersample)
		if err != nil {
			log.Printf("buildFrame: %v", err)
			return
		}
		lighting.applyFrameLighting(&f, viewer)
		lighting.applyFramePost(&f, viewer.runner != nil)
		if len(viewer.splatNodes) > 0 && !loadCompose && lighting.envIndex == 0 {
			f.Background = color.RGBA{18, 20, 26, 255}
		}
		if loadCompose {
			// Clear the host surface via GoGPU first, then composite NT3D with
			// a transparent background and no sky environment over it.
			dc.Clear(0.08, 0.14, 0.24, 1.0)
			f.Environment = nil
			f.Background = color.RGBA{0, 0, 0, 0}
		}
		viewer.decorateFrame(&f, camera, showHUD, false)
		lastFrame = f
		hasFrame = true
		viewer.lastStats = f.Stats()
		viewer.haveStats = true

		if err := session.Encode(f, dc.CommandEncoder(), nt3d.GPUTarget{
			Color:  dc.SurfaceView(),
			Format: dc.Format(),
			Width:  int(w),
			Height: int(h),
			Load:   loadCompose,
		}); err != nil {
			log.Printf("session.Encode: %v", err)
			return
		}

		if pipView && int(w) >= 320 && int(h) >= 240 {
			if pipSession == nil {
				var err error
				pipSession, err = session.NewView()
				if err != nil {
					log.Printf("session.NewView (pip): %v", err)
				}
			}
			if pipSession != nil {
				pipW, pipH := max(160, int(w)/4), max(120, int(h)/4)
				margin := 16
				pipRect := image.Rect(int(w)-pipW-margin, margin, int(w)-margin, margin+pipH)
				topCam := camera
				topCam.Alpha = 78
				topCam.Beta = camera.Beta + 90
				if pipFrame, err := viewer.buildFrame(topCam, pipW, pipH, msaa, 1); err == nil {
					lighting.applyFrameLighting(&pipFrame, viewer)
					lighting.applyFramePost(&pipFrame, viewer.runner != nil)
					if len(viewer.splatNodes) > 0 && lighting.envIndex == 0 {
						pipFrame.Background = color.RGBA{26, 30, 38, 255}
					}
					viewer.decorateFrame(&pipFrame, topCam, showHUD, true)
					appendBorderOverlays(&pipFrame, 2, color.RGBA{75, 140, 210, 255})
					if err := pipSession.Encode(pipFrame, dc.CommandEncoder(), nt3d.GPUTarget{
						Color:    dc.SurfaceView(),
						Format:   dc.Format(),
						Width:    int(w),
						Height:   int(h),
						Viewport: pipRect,
					}); err != nil {
						log.Printf("pipSession.Encode: %v", err)
					}
				}
			}
		}

		if hudSession == nil {
			var err error
			hudSession, err = session.NewView()
			if err != nil {
				log.Printf("session.NewView (hud): %v", err)
			}
		}
		if hudSession != nil {
			hudText := hud.formatText(viewer, camera, lighting, animate, pipView, loadCompose, showHUD, menuOpen, msaa, supersample)
			if err := hud.encode(hudSession, dc, hudText, menuOpen); err != nil {
				log.Printf("hud.encode: %v", err)
			}
		}
		if menuOpen {
			if menuSession == nil {
				var err error
				menuSession, err = session.NewView()
				if err != nil {
					log.Printf("session.NewView (menu): %v", err)
				}
			}
			if menuSession != nil {
				if err := hud.encodeMenu(menuSession, dc, currentMenuIndex(viewer), menuHover); err != nil {
					log.Printf("hud.encodeMenu: %v", err)
				}
			}
		}
		dc.MarkPreserveContent()

		frameCount++
		if *maxFrames > 0 && frameCount >= *maxFrames {
			app.Quit()
		}
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
