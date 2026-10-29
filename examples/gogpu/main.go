// Copyright (c) 2026 Neomantra Corp.

// Command gogpu demonstrates embedding NT3D inside a GoGPU window using
// nt3d.GPUSession and nt3d.GPUTarget without CPU readback, and serves as an
// interactive desktop 3D model and Gaussian splat viewer when given -file or
// positional file paths (.glb, .gltf, .sog, .spz, .ply, .stl, .obj, .3mf).
//
// Controls:
//   - Left-mouse drag: orbit camera
//   - Right-mouse drag (or Shift + Left-drag): pan camera
//   - Left-mouse click: pick object/primitive under cursor (highlights mesh)
//   - Mouse wheel: zoom camera
//   - F: fit camera to scene bounds
//   - Space: toggle animation
//   - C: cycle animation clip (for animated glTF/GLB models)
//   - H: toggle full Spherical Harmonics vs DC-only (for Gaussian splats)
//   - U / Y / Z: halve / double / reset Gaussian splat budget
//   - V: toggle picture-in-picture sub-viewport (GPUTarget.Viewport)
//   - L: toggle Load compositing mode over GoGPU background
//   - S: cycle supersampling (1x / 2x)
//   - 2: open a second GoGPU window sharing the same GPU device
//   - Escape: quit
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"log"
	"math"
	"path/filepath"
	"slices"

	"github.com/gogpu/gogpu"
	"github.com/gogpu/gpucontext"
	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
	_ "nimbleterminal.dev/nt3d/mesh/formats"
)

type viewerState struct {
	scene        *nt3d.Scene
	rigID        nt3d.NodeID
	fitBounds    nt3dmath.AABB
	modelRoot    nt3d.NodeID
	clips        []nt3d.AnimationClip
	clipIndex    int
	player       *nt3d.Animation
	splatNodes   []nt3d.NodeID
	splatSources []nt3d.Geometry
	hasSH        bool
	dcOnly       bool
	splatBudget  int
	selectedNode nt3d.NodeID
}

func buildDemoScene() (*viewerState, error) {
	s := nt3d.NewScene()
	s.SetPickIndexing(true)
	s.SetEnvironment(nt3d.GradientEnvironment(
		color.RGBA{35, 55, 95, 255},
		color.RGBA{120, 145, 185, 255},
		color.RGBA{25, 30, 40, 255},
	))

	// Floor heightfield.
	const grid = 24
	heights := make([]float32, grid*grid)
	for y := range grid {
		for x := range grid {
			fx := float64(x)/float64(grid-1)*2 - 1
			fy := float64(y)/float64(grid-1)*2 - 1
			heights[y*grid+x] = float32(0.08*math.Cos(fx*math.Pi*1.5)*math.Cos(fy*math.Pi*1.5) - 0.55)
		}
	}
	floorGeom, err := nt3d.BuildHeightfield(nt3d.Heightfield{
		NX: grid, NY: grid,
		MinX: -1.6, MaxX: 1.6,
		MinY: -1.6, MaxY: 1.6,
		Heights: heights,
		Color:   color.RGBA{90, 110, 130, 255},
		Material: &nt3d.Material{
			Specular:     0.25,
			Shininess:    24,
			Reflectivity: 0.15,
		},
	})
	if err != nil {
		return nil, err
	}
	if _, err := s.SetGeometry("floor", floorGeom); err != nil {
		return nil, err
	}

	rigID, err := s.Add(nt3d.Root, "rig")
	if err != nil {
		return nil, err
	}

	// Reflective center sphere.
	centerSphere, err := nt3d.BuildSphere(nt3d.Sphere{
		Radius: 0.42,
		Color:  color.RGBA{240, 135, 55, 255},
		Material: &nt3d.Material{
			Specular:     0.7,
			Shininess:    48,
			Reflectivity: 0.35,
		},
	})
	if err != nil {
		return nil, err
	}
	centerID, err := s.SetGeometry("center-sphere", centerSphere)
	if err != nil {
		return nil, err
	}
	if err := s.Reparent(centerID, rigID); err != nil {
		return nil, err
	}

	// Orbiting torus ring.
	torus, err := nt3d.BuildTorus(nt3d.Torus{
		MajorRadius: 0.72,
		MinorRadius: 0.10,
		Color:       color.RGBA{75, 195, 235, 255},
		Material: &nt3d.Material{
			Specular:  0.6,
			Shininess: 32,
		},
	})
	if err != nil {
		return nil, err
	}
	torusID, err := s.SetGeometry("torus", torus)
	if err != nil {
		return nil, err
	}
	if err := s.Reparent(torusID, rigID); err != nil {
		return nil, err
	}

	// Translucent glass satellites.
	satSphere, err := nt3d.BuildSphere(nt3d.Sphere{
		Radius: 0.18,
		Color:  color.RGBA{255, 95, 145, 165},
		Material: &nt3d.Material{
			Transparent: true,
			Specular:    0.8,
			Shininess:   64,
		},
	})
	if err != nil {
		return nil, err
	}
	for i := range 4 {
		angle := float64(i) * math.Pi / 2
		id, err := s.SetGeometry(fmt.Sprintf("satellite-%d", i), satSphere)
		if err != nil {
			return nil, err
		}
		if err := s.Reparent(id, rigID); err != nil {
			return nil, err
		}
		pos := nt3dmath.Vec3{
			X: float32(1.05 * math.Cos(angle)),
			Y: float32(1.05 * math.Sin(angle)),
			Z: 0.18,
		}
		if err := s.SetTransform(id, nt3dmath.Translation(pos)); err != nil {
			return nil, err
		}
	}

	return &viewerState{
		scene:     s,
		rigID:     rigID,
		fitBounds: s.Bounds(),
	}, nil
}

// splatFraming computes a tight bounding box around the dense subject of a 3D
// Gaussian splat capture so distant background floaters do not push Camera.Fit
// far away from the subject.
func splatFraming(m *nt3dmesh.Model) (nt3dmath.AABB, bool) {
	var xs, ys, zs []float32
	for _, o := range m.Objects {
		g := o.Geometry
		if g.PointStyle.Shape != nt3d.GaussianPoint || len(g.Vertices) > 0 || len(g.Lines) > 0 || len(g.Boxes) > 0 {
			return nt3dmath.AABB{}, false
		}
		for _, p := range g.Points {
			xs = append(xs, p.Position.X)
			ys = append(ys, p.Position.Y)
			zs = append(zs, p.Position.Z)
		}
	}
	if len(xs) == 0 {
		return nt3dmath.AABB{}, false
	}
	median := func(v []float32) float32 {
		sorted := slices.Clone(v)
		slices.Sort(sorted)
		return sorted[len(sorted)/2]
	}
	center := nt3dmath.Vec3{X: median(xs), Y: median(ys), Z: median(zs)}
	distances := make([]float32, len(xs))
	for i := range xs {
		d := nt3dmath.Vec3{X: xs[i], Y: ys[i], Z: zs[i]}.Sub(center)
		distances[i] = float32(math.Sqrt(float64(d.Dot(d))))
	}
	half := 0.75 * median(distances)
	if half <= 0 {
		return nt3dmath.AABB{}, false
	}
	var box nt3dmath.AABB
	box.Include(center.Sub(nt3dmath.Vec3{X: half, Y: half, Z: half}))
	box.Include(center.Add(nt3dmath.Vec3{X: half, Y: half, Z: half}))
	return box, true
}

func loadModelScene(files []string, up nt3dmesh.Up) (*viewerState, error) {
	s := nt3d.NewScene()
	s.SetPickIndexing(true)
	s.SetEnvironment(nt3d.GradientEnvironment(
		color.RGBA{35, 55, 95, 255},
		color.RGBA{120, 145, 185, 255},
		color.RGBA{25, 30, 40, 255},
	))

	state := &viewerState{scene: s}
	spacing := float32(2.2)
	var totalTriangles, totalSplats int

	for i, name := range files {
		m, err := nt3dmesh.ReadFile(name, &nt3dmesh.Options{
			Color: color.RGBA{100, 180, 230, 255},
			Up:    up,
		})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		for _, w := range m.Warnings {
			log.Printf("%s: %s", filepath.Base(name), w)
		}
		group, err := m.AddTo(s, nt3d.Root, filepath.Base(name))
		if err != nil {
			return nil, err
		}
		box := m.Bounds()
		if framing, ok := splatFraming(m); ok {
			box = framing
		}
		fit, _, _ := box.Normalization()
		offset := nt3dmath.Translation(nt3dmath.Vec3{X: spacing * (float32(i) - float32(len(files)-1)/2)})
		if err := s.SetTransform(group, offset.Mul(fit)); err != nil {
			return nil, err
		}

		if node, ok := s.Node(group); ok {
			for j, id := range node.Children {
				if j < len(m.Objects) {
					g := m.Objects[j].Geometry
					if g.PointStyle.Shape == nt3d.GaussianPoint {
						state.splatNodes = append(state.splatNodes, id)
						state.splatSources = append(state.splatSources, g)
						state.hasSH = state.hasSH || len(g.GaussianSH) > 0
						totalSplats += len(g.Points)
					}
				}
			}
		}
		if clips := s.AnimationClips(group); len(clips) > 0 && state.player == nil {
			state.modelRoot = group
			state.clips = clips
			player, err := nt3d.NewAnimation(s, clips[0])
			if err == nil {
				player.Mode = nt3d.Loop
				_ = player.Sample(0)
				state.player = player
			}
		}
		totalTriangles += m.Triangles()
	}

	// Normalized models sit in a ~2-unit box around the origin.
	var fitBox nt3dmath.AABB
	halfWidth := float32(1.0)
	if len(files) > 1 {
		halfWidth = spacing*float32(len(files)-1)/2 + 1.0
	}
	fitBox.Include(nt3dmath.Vec3{X: -halfWidth, Y: -1, Z: -1})
	fitBox.Include(nt3dmath.Vec3{X: halfWidth, Y: 1, Z: 1})
	state.fitBounds = fitBox

	if totalSplats > 0 {
		s.SetEnvironment(nil)
		log.Printf("Loaded %d file(s): %d Gaussian splats (hasSH=%v)", len(files), totalSplats, state.hasSH)
	} else {
		log.Printf("Loaded %d file(s): %d triangles, %d animation clip(s)", len(files), totalTriangles, len(state.clips))
	}
	return state, nil
}

func (v *viewerState) toggleSH() {
	if !v.hasSH {
		return
	}
	v.dcOnly = !v.dcOnly
	for i, id := range v.splatNodes {
		g := v.splatSources[i]
		g.PointStyle.SHDCOnly = v.dcOnly
		_ = v.scene.SetNodeGeometry(id, g)
	}
	fmt.Printf("Gaussian SH DC-only: %v\n", v.dcOnly)
}

func (v *viewerState) adjustSplatBudget(factor float64) {
	if len(v.splatNodes) == 0 {
		return
	}
	if factor == 0 {
		v.splatBudget = 0
		v.scene.SetSplatBudget(0)
		fmt.Printf("Splat budget reset to default (%d)\n", nt3d.DefaultSplatBudget)
		return
	}
	b := v.splatBudget
	if b <= 0 {
		b = nt3d.DefaultSplatBudget
	}
	b = max(1000, min(1<<30, int(float64(b)*factor)))
	v.splatBudget = b
	v.scene.SetSplatBudget(b)
	fmt.Printf("Splat budget: %d\n", b)
}

func (v *viewerState) cycleClip() {
	if len(v.clips) == 0 {
		return
	}
	v.clipIndex = (v.clipIndex + 1) % len(v.clips)
	clip := v.clips[v.clipIndex]
	player, err := nt3d.NewAnimation(v.scene, clip)
	if err != nil {
		log.Printf("NewAnimation: %v", err)
		return
	}
	player.Mode = nt3d.Loop
	_ = player.Sample(0)
	v.player = player
	fmt.Printf("Animation clip [%d/%d]: %q (%.2fs)\n", v.clipIndex+1, len(v.clips), clip.Name, clip.Duration())
}

func main() {
	fileFlag := flag.String("file", "", "path to a 3D model or Gaussian splat file (.glb, .gltf, .sog, .spz, .ply, .stl, .obj, .3mf)")
	yUp := flag.Bool("y-up", false, "convert Y-up files to Z-up when loading")
	width := flag.Int("width", 960, "initial window width")
	height := flag.Int("height", 640, "initial window height")
	maxFrames := flag.Int("frames", 0, "exit automatically after N rendered frames (0 = interactive)")
	flag.Parse()

	var files []string
	if *fileFlag != "" {
		files = append(files, *fileFlag)
	}
	files = append(files, flag.Args()...)

	camera := nt3d.DefaultCamera()
	camera.Alpha = 22
	camera.Beta = -38
	camera.Distance = 3.2

	title := "NT3D + GoGPU (Host-Supplied GPU Session)"
	if len(files) == 1 {
		title = "NT3D Viewer — " + filepath.Base(files[0])
	} else if len(files) > 1 {
		title = fmt.Sprintf("NT3D Viewer — %d files", len(files))
	}

	app := gogpu.NewApp(gogpu.DefaultConfig().
		WithTitle(title).
		WithSize(*width, *height).
		WithContinuousRender(true))

	var (
		viewer      *viewerState
		session     *nt3d.GPUSession
		pipSession  *nt3d.GPUSession
		secondSess  *nt3d.GPUSession
		lastFrame   nt3d.Frame
		hasFrame    bool
		leftDown    bool
		rightDown   bool
		shiftDown   bool
		lastX       float64
		lastY       float64
		dragDist    float64
		animate     = true
		pipView     = len(files) == 0
		loadCompose = false
		supersample = 1
		angle       float64
		frameCount  int
		wantSecond  bool
		hasSecond   bool
	)

	defer func() {
		if secondSess != nil {
			_ = secondSess.Close()
		}
		if pipSession != nil {
			_ = pipSession.Close()
		}
		if session != nil {
			_ = session.Close()
		}
	}()

	events := app.EventSource()
	events.OnMousePress(func(button gpucontext.MouseButton, x, y float64) {
		switch button {
		case gpucontext.MouseButtonLeft:
			leftDown = true
			lastX, lastY = x, y
			dragDist = 0
		case gpucontext.MouseButtonRight:
			rightDown = true
			lastX, lastY = x, y
		}
	})
	events.OnMouseRelease(func(button gpucontext.MouseButton, x, y float64) {
		switch button {
		case gpucontext.MouseButtonLeft:
			wasClick := leftDown && dragDist < 4.0
			leftDown = false
			if wasClick && viewer != nil && hasFrame {
				if viewer.selectedNode != nt3d.Root {
					_ = viewer.scene.SetOutline(viewer.selectedNode, nt3d.OutlineStyle{})
					viewer.selectedNode = nt3d.Root
				}
				if hit := viewer.scene.Pick(camera, lastFrame, float32(x), float32(y), 6); hit != nil {
					fmt.Printf("Picked %q (node=%d index=%d pos=[%.3f, %.3f, %.3f] depth=%.4f)\n",
						hit.Name, hit.Node, hit.Index, hit.Position.X, hit.Position.Y, hit.Position.Z, hit.Depth)
					viewer.selectedNode = hit.Node
					_ = viewer.scene.SetOutline(hit.Node, nt3d.OutlineStyle{
						Width: 3,
						Color: color.RGBA{255, 210, 60, 255},
					})
				}
			}
		case gpucontext.MouseButtonRight:
			rightDown = false
		}
	})
	events.OnMouseMove(func(x, y float64) {
		if !leftDown && !rightDown {
			return
		}
		dx, dy := x-lastX, y-lastY
		lastX, lastY = x, y
		dragDist += math.Hypot(dx, dy)
		if rightDown || (leftDown && shiftDown) {
			w, h := *width, *height
			if hasFrame {
				w, h = lastFrame.Width, lastFrame.Height
			}
			camera = camera.Pan(float32(dx), float32(dy), w, h)
		} else if leftDown {
			camera = camera.Orbit(-dx*0.4, dy*0.4)
		}
	})
	events.OnScroll(func(_, dy float64) {
		camera = camera.Zoom(math.Pow(0.92, dy))
	})
	events.OnKeyPress(func(key gpucontext.Key, mods gpucontext.Modifiers) {
		shiftDown = mods&gpucontext.ModShift != 0
		switch key {
		case gpucontext.KeyLeftShift, gpucontext.KeyRightShift:
			shiftDown = true
		case gpucontext.KeyEscape:
			app.Quit()
		case gpucontext.KeySpace:
			animate = !animate
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
		case gpucontext.KeyV:
			pipView = !pipView
			fmt.Printf("Sub-viewport inset: %v\n", pipView)
		case gpucontext.KeyL:
			loadCompose = !loadCompose
			fmt.Printf("Load composition mode: %v\n", loadCompose)
		case gpucontext.KeyS:
			if supersample == 1 {
				supersample = 2
			} else {
				supersample = 1
			}
			fmt.Printf("Supersample: %dx\n", supersample)
		case gpucontext.Key2:
			if !hasSecond {
				wantSecond = true
			}
		}
	})
	events.OnKeyRelease(func(key gpucontext.Key, mods gpucontext.Modifiers) {
		shiftDown = mods&gpucontext.ModShift != 0
		if key == gpucontext.KeyLeftShift || key == gpucontext.KeyRightShift {
			shiftDown = false
		}
	})

	app.OnUpdate(func(dt float64) {
		if viewer == nil {
			return
		}
		if animate {
			if viewer.player != nil {
				_ = viewer.player.Step(dt)
			} else if viewer.rigID != nt3d.Root {
				angle += dt * 1.2
				bob := float32(0.15 * math.Sin(angle*2))
				_ = viewer.scene.SetTransform(viewer.rigID, nt3dmath.Translation(nt3dmath.Vec3{Z: bob}).Mul(nt3dmath.RotationZ(angle)))
			}
		}
		if wantSecond && !hasSecond && app.GPUContextProvider() != nil {
			wantSecond = false
			hasSecond = true
			var err error
			secondSess, err = nt3d.NewGPUSessionFromProvider(app.GPUContextProvider())
			if err != nil {
				log.Printf("NewGPUSessionFromProvider (window 2): %v", err)
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
				f, err := viewer.scene.Frame(topCam, int(w), int(h))
				if err != nil {
					return
				}
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

			if len(files) > 0 {
				up := nt3dmesh.ZUp
				if *yUp {
					up = nt3dmesh.YUp
				}
				viewer, err = loadModelScene(files, up)
				if err != nil {
					log.Fatalf("loadModelScene: %v", err)
				}
				w, h := dc.SurfaceSize()
				aspect := float64(*width) / float64(*height)
				if w > 0 && h > 0 {
					aspect = float64(w) / float64(h)
				}
				if viewer.fitBounds.IsValid() {
					camera = camera.Fit(viewer.fitBounds, aspect)
				}
			} else {
				viewer, err = buildDemoScene()
				if err != nil {
					log.Fatalf("buildDemoScene: %v", err)
				}
			}
		}

		w, h := dc.SurfaceSize()
		if w == 0 || h == 0 || viewer == nil {
			return
		}

		f, err := viewer.scene.Frame(camera, int(w), int(h))
		if err != nil {
			log.Printf("scene.Frame: %v", err)
			return
		}
		f.Supersample = supersample
		if len(viewer.splatNodes) > 0 && !loadCompose {
			f.Background = color.RGBA{18, 20, 26, 255}
		}
		if loadCompose {
			// Clear the host surface via GoGPU first, then composite NT3D with
			// a transparent background and no sky environment over it.
			dc.Clear(0.08, 0.14, 0.24, 1.0)
			f.Environment = nil
			f.Background = color.RGBA{0, 0, 0, 0}
		}
		lastFrame = f
		hasFrame = true

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
				pipSession, err = nt3d.NewGPUSessionFromProvider(app.GPUContextProvider())
				if err != nil {
					log.Printf("NewGPUSessionFromProvider (pip): %v", err)
				}
			}
			if pipSession != nil {
				pipW, pipH := max(160, int(w)/4), max(120, int(h)/4)
				margin := 16
				pipRect := image.Rect(int(w)-pipW-margin, margin, int(w)-margin, margin+pipH)
				topCam := camera
				topCam.Alpha = 78
				topCam.Beta = camera.Beta + 90
				if pipFrame, err := viewer.scene.Frame(topCam, pipW, pipH); err == nil {
					if len(viewer.splatNodes) > 0 {
						pipFrame.Background = color.RGBA{26, 30, 38, 255}
					}
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
