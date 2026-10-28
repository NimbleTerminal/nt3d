// Copyright (c) 2026 Neomantra Corp.

// Command gogpu demonstrates embedding NT3D inside a GoGPU window using
// nt3d.GPUSession and nt3d.GPUTarget without CPU readback.
//
// Controls:
//   - Left-mouse drag: orbit camera
//   - Mouse wheel: zoom camera
//   - Space: toggle scene animation
//   - L: toggle Load compositing mode over GoGPU background
//   - S: cycle supersampling (1x / 2x)
//   - 2: open a second GoGPU window sharing the same GPU device
//   - Escape: quit
package main

import (
	"flag"
	"fmt"
	"image/color"
	"log"
	"math"

	"github.com/gogpu/gogpu"
	"github.com/gogpu/gpucontext"
	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func buildDemoScene() (*nt3d.Scene, nt3d.NodeID, error) {
	s := nt3d.NewScene()
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
	floor, err := nt3d.BuildHeightfield(nt3d.Heightfield{
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
		return nil, 0, err
	}
	if _, err := s.SetGeometry("floor", floor); err != nil {
		return nil, 0, err
	}

	// Center reflective sphere.
	coreSphere, err := nt3d.BuildSphere(nt3d.Sphere{
		Radius:   0.48,
		Segments: 48,
		Rings:    24,
		Color:    color.RGBA{235, 110, 55, 255},
		Material: &nt3d.Material{
			Specular:     0.85,
			Shininess:    96,
			Reflectivity: 0.35,
		},
	})
	if err != nil {
		return nil, 0, err
	}
	if _, err := s.SetGeometry("core", coreSphere); err != nil {
		return nil, 0, err
	}

	// Orbiting group with satellite torus/cuboid.
	rig, err := s.Add(nt3d.Root, "rig")
	if err != nil {
		return nil, 0, err
	}
	sat, err := nt3d.BuildCuboid(nt3d.Cuboid{
		Size:  nt3dmath.Vec3{X: 0.28, Y: 0.28, Z: 0.28},
		Color: color.RGBA{70, 210, 165, 255},
		Material: &nt3d.Material{
			Specular:     0.6,
			Shininess:    48,
			Reflectivity: 0.2,
		},
	})
	if err != nil {
		return nil, 0, err
	}
	satID, err := s.SetGeometry("satellite", sat)
	if err != nil {
		return nil, 0, err
	}
	if err := s.Reparent(satID, rig); err != nil {
		return nil, 0, err
	}
	if err := s.SetTransform(satID, nt3dmath.Translation(nt3dmath.Vec3{X: 0.95, Z: 0.15})); err != nil {
		return nil, 0, err
	}

	return s, rig, nil
}

func main() {
	maxFrames := flag.Int("frames", 0, "exit automatically after rendering N frames (0 runs until closed)")
	flag.Parse()

	scene, rigID, err := buildDemoScene()
	if err != nil {
		log.Fatal(err)
	}

	camera := nt3d.DefaultCamera()
	camera.Projection = nt3d.Perspective
	camera.Distance = 3.2
	camera.Alpha = 24
	camera.Beta = 35

	app := gogpu.NewApp(gogpu.DefaultConfig().
		WithTitle("NT3D + GoGPU Host-Rendered 3D Demo").
		WithSize(960, 640).
		WithContinuousRender(true))

	var (
		session     *nt3d.GPUSession
		secondSess  *nt3d.GPUSession
		dragging    bool
		lastX       float64
		lastY       float64
		animate     = true
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
		if session != nil {
			_ = session.Close()
		}
	}()

	events := app.EventSource()
	events.OnMousePress(func(button gpucontext.MouseButton, x, y float64) {
		if button == gpucontext.MouseButtonLeft {
			dragging = true
			lastX, lastY = x, y
		}
	})
	events.OnMouseRelease(func(button gpucontext.MouseButton, _, _ float64) {
		if button == gpucontext.MouseButtonLeft {
			dragging = false
		}
	})
	events.OnMouseMove(func(x, y float64) {
		if !dragging {
			return
		}
		dx, dy := x-lastX, y-lastY
		lastX, lastY = x, y
		camera.Beta -= dx * 0.4
		camera.Alpha = math.Max(-85, math.Min(85, camera.Alpha+dy*0.4))
	})
	events.OnScroll(func(_, dy float64) {
		camera.Distance = math.Max(1.2, math.Min(12, camera.Distance*(1-dy*0.08)))
	})
	events.OnKeyPress(func(key gpucontext.Key, _ gpucontext.Modifiers) {
		switch key {
		case gpucontext.KeyEscape:
			app.Quit()
		case gpucontext.KeySpace:
			animate = !animate
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

	app.OnUpdate(func(dt float64) {
		if animate {
			angle += dt * 1.2
			bob := float32(0.15 * math.Sin(angle*2))
			_ = scene.SetTransform(rigID, nt3dmath.Translation(nt3dmath.Vec3{Z: bob}).Mul(nt3dmath.RotationZ(angle)))
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
				w, h := dc.SurfaceSize()
				if w == 0 || h == 0 {
					return
				}
				topCam := camera
				topCam.Alpha = 78
				topCam.Beta = camera.Beta + 90
				f, err := scene.Frame(topCam, int(w), int(h))
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
			if limit, err := session.BindingLimit(); err == nil {
				nt3d.SetBindingLimit(limit)
			}
		}

		w, h := dc.SurfaceSize()
		if w == 0 || h == 0 {
			return
		}

		f, err := scene.Frame(camera, int(w), int(h))
		if err != nil {
			log.Printf("scene.Frame: %v", err)
			return
		}
		f.Supersample = supersample
		if loadCompose {
			// Clear the host surface via GoGPU first, then composite NT3D with
			// a transparent background and no sky environment over it.
			dc.Clear(0.08, 0.14, 0.24, 1.0)
			f.Environment = nil
			f.Background = color.RGBA{0, 0, 0, 0}
		}

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
