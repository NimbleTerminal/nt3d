// Copyright (c) 2026 Neomantra Corp.

// Command gogpu demonstrates embedding NT3D inside a GoGPU window using
// nt3d.GPUSession and nt3d.GPUTarget without CPU readback. It serves as:
//   - A unified desktop application for all 18 interactive NT3D demos
//     (-demo <name>, or Tab / Shift+Tab / PageDown / PageUp to switch live).
//   - An interactive desktop 3D model and Gaussian splat viewer when given
//     -file or positional file paths (.glb, .gltf, .sog, .spz, .ply, .stl, .obj, .3mf).
//
// Controls:
//   - Drag & Drop files (.glb, .gltf, .sog, .spz, .ply, .stl, .obj, .3mf, .png, .jpg) onto window
//   - O or Ctrl+O: open native file picker dialog for 3D models, splats, or panoramas
//   - Click top-left "[v] Scene: ..." bar (or press ` / F5): open interactive Demo Dropdown Menu
//   - Left-mouse drag: orbit camera (Alt + Left-drag: rotate sun/key light direction)
//   - Right-mouse drag (or Shift + Left-drag): pan camera
//   - Left-mouse click: pick object/primitive under cursor (3D annotation + outline + gizmo)
//   - Left-mouse drag on 3D Gizmo axis/ring/cube: translate, rotate, or scale selected node in 3D
//   - Click top-right 3D Camera Orientation Triad (+X/-X/+Y/-Y/+Z/-Z): snap camera to cardinal view
//   - W or F11: cycle 3D Transform Gizmo mode (Translate -> Rotate -> Scale -> Off)
//   - Backspace / Delete: reset selected node's transform to its original pose
//   - Mouse wheel: zoom camera
//   - E or F6: cycle Environment / IBL skybox preset (or dropped panorama)
//   - L or F7: cycle Lighting Rig preset (Key+Sky, 3-Point, RGB Lamps, Lantern, 8-Lamp, Pure IBL)
//   - K or F8: toggle real-time GPU Shadow Mapping
//   - P or F9: capture offscreen PNG screenshot via GPUSession.RenderToImage
//   - T or F10: capture 360-degree turntable animated GIF via GPUSession.RenderToImage
//   - Ctrl+S / Cmd+S (or Shift+P): save screenshot (.png) or turntable (.gif) via native Save dialog
//   - [ / ] and , / .: adjust sun/key light azimuth and elevation (in showcase/viewer mode)
//   - G: toggle studio shadow-catcher pedestal floor (in file viewer mode)
//   - Tab / PageDown: switch to next interactive demo in the 18-demo gallery
//   - Shift+Tab / PageUp: switch to previous interactive demo in the gallery
//   - ` (Grave) or F5: toggle interactive Demo Dropdown Menu (Up/Down + Enter or mouse click)
//   - F1 (or I): toggle 2D HUD overlay and 3D pick annotations
//   - F2 (or V): toggle picture-in-picture sub-viewport (GPUTarget.Viewport)
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
	"image/draw"
	"image/gif"
	_ "image/jpeg"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gogpu/gogpu"
	"github.com/gogpu/gpucontext"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/examples/demos/gallery"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
	_ "nimbleterminal.dev/nt3d/mesh/formats"
	nt3dsh "nimbleterminal.dev/nt3d/sh"
)

type gizmoMode int

const (
	gizmoTranslate gizmoMode = iota
	gizmoRotate
	gizmoScale
	gizmoOff
)

type viewerState struct {
	scene             *nt3d.Scene
	runner            *gallery.Runner
	demoIndex         int
	summary           string
	rigID             nt3d.NodeID
	floorNode         nt3d.NodeID
	hasFloor          bool
	fitBounds         nt3dmath.AABB
	modelRoot         nt3d.NodeID
	clips             []nt3d.AnimationClip
	clipIndex         int
	player            *nt3d.Animation
	splatNodes        []nt3d.NodeID
	splatSources      []nt3d.Geometry
	totalTriangles    int
	totalSplats       int
	hasSH             bool
	dcOnly            bool
	splatBudget       int
	selectedNode      nt3d.NodeID
	selectedHit       *nt3d.Hit
	selectedLocal     nt3dmath.Vec3
	selectedInitLocal nt3dmath.Mat4
	hasInitLocal      bool
	pickLabelID       nt3d.NodeID
	pickLabelText     string
	gizmoMode         gizmoMode
	gizmoHoverAxis    int
	gizmoActiveAxis   int
	gizmoDragging     bool
	gizmoStartX       float32
	gizmoStartY       float32
	gizmoStartLocal   nt3dmath.Mat4
	gizmoStartWorld   nt3dmath.Mat4
	gizmoStartPivot   nt3dmath.Vec3
	orientHoverAxis   int // ±1 (±X), ±2 (±Y), ±3 (±Z) on top-right orientation triad
}

func (v *viewerState) close() {
	if v == nil {
		return
	}
	v.clearPick()
	if v.runner != nil {
		_ = v.runner.Close()
		v.runner = nil
	}
}

func loadGalleryDemo(name string, demoIdx int, files []string, up nt3dmesh.Up, yDown bool) (*viewerState, error) {
	r, err := gallery.NewRunner(name, files, up, yDown)
	if err != nil {
		return nil, err
	}
	return &viewerState{
		scene:     r.Scene(),
		runner:    r,
		demoIndex: demoIdx,
		summary:   "demo: " + name,
	}, nil
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

	totalTris := (len(floorGeom.Indices) + len(centerSphere.Indices) + len(torus.Indices) + 4*len(satSphere.Indices)) / 3
	s.WarmPickIndices()
	state := &viewerState{
		scene:          s,
		summary:        "procedural demo (7 meshes)",
		rigID:          rigID,
		fitBounds:      s.Bounds(),
		totalTriangles: totalTris,
	}
	state.selectPick(&nt3d.Hit{
		Node:      centerID,
		Name:      "center-sphere",
		Primitive: nt3d.TrianglePrimitive,
		Index:     0,
		Position:  nt3dmath.Vec3{Z: 0.42},
		Depth:     0.45,
	}, nt3d.DefaultCamera())
	return state, nil
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

	state.totalTriangles = totalTriangles
	state.totalSplats = totalSplats
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = filepath.Base(f)
	}
	state.summary = strings.Join(names, ", ")

	if totalSplats > 0 {
		s.SetEnvironment(nil)
		log.Printf("Loaded %d file(s): %d Gaussian splats (hasSH=%v)", len(files), totalSplats, state.hasSH)
	} else {
		s.WarmPickIndices()
		log.Printf("Loaded %d file(s): %d triangles, %d animation clip(s)", len(files), totalTriangles, len(state.clips))
	}
	return state, nil
}

func (v *viewerState) toggleModelFloor() {
	if v == nil || v.runner != nil || v.rigID != nt3d.Root || len(v.splatNodes) > 0 {
		return
	}
	if v.floorNode == nt3d.Root {
		size := float32(3.2)
		if v.fitBounds.IsValid() {
			ext := v.fitBounds.Max.Sub(v.fitBounds.Min)
			size = max(3.2, max(ext.X, ext.Y)*1.4)
		}
		minZ := float32(-1.02)
		if b := v.scene.Bounds(); b.IsValid() {
			minZ = b.Min.Z - 0.02
		}
		floorGeom, err := nt3d.BuildHeightfield(nt3d.Heightfield{
			NX: 2, NY: 2,
			MinX: -size, MaxX: size,
			MinY: -size, MaxY: size,
			Heights: []float32{minZ, minZ, minZ, minZ},
			Color:   color.RGBA{115, 122, 134, 255},
			Material: &nt3d.Material{
				Specular:     0.2,
				Shininess:    24,
				Reflectivity: 0.08,
			},
		})
		if err != nil {
			return
		}
		id, err := v.scene.SetGeometry("studio-floor", floorGeom)
		if err != nil {
			return
		}
		v.floorNode = id
		v.hasFloor = true
		fmt.Println("Studio shadow-catcher floor: on")
		return
	}
	v.hasFloor = !v.hasFloor
	_ = v.scene.SetVisible(v.floorNode, v.hasFloor)
	fmt.Printf("Studio shadow-catcher floor: %v\n", v.hasFloor)
}

var (
	envStudioHorizon = sync.OnceValue(func() *nt3d.Environment {
		return nt3d.GradientEnvironment(
			color.RGBA{35, 55, 95, 255},
			color.RGBA{120, 145, 185, 255},
			color.RGBA{25, 30, 40, 255},
		)
	})
	envClearDay = sync.OnceValue(func() *nt3d.Environment {
		return nt3d.GradientEnvironment(
			color.RGBA{72, 130, 220, 255},
			color.RGBA{218, 231, 245, 255},
			color.RGBA{115, 100, 77, 255},
		)
	})
	envGoldenSunset = sync.OnceValue(func() *nt3d.Environment {
		return nt3d.GradientEnvironment(
			color.RGBA{42, 35, 88, 255},
			color.RGBA{245, 135, 68, 255},
			color.RGBA{48, 32, 28, 255},
		)
	})
	envSoftboxPanorama = sync.OnceValue(func() *nt3d.Environment {
		img := image.NewNRGBA(image.Rect(0, 0, 192, 96))
		for y := range 96 {
			for x := range 192 {
				phi := ((float64(x)+0.5)/192 - 0.5) * 2 * math.Pi
				z := math.Cos((float64(y) + 0.5) / 96 * math.Pi)
				warm := math.Max(0, math.Cos(phi)) * math.Sqrt(max(0, 1-z*z))
				cool := math.Max(0, -math.Cos(phi)) * math.Sqrt(max(0, 1-z*z))
				img.SetNRGBA(x, y, color.NRGBA{
					R: uint8(min(255, 48+190*warm+30*cool)),
					G: uint8(min(255, 68+135*warm+85*cool)),
					B: uint8(min(255, 105+60*warm+135*cool+40*max(0, z))),
					A: 255,
				})
			}
		}
		e, _ := nt3d.NewEnvironment(img)
		return e
	})
	envSHBaseRadiance = sync.OnceValue(func() []nt3dsh.RGB {
		radiance, _ := nt3dsh.Project(2, func(d nt3dmath.Vec3) nt3dsh.RGB {
			warm := max(float32(0), d.X) * max(float32(0), d.X)
			cool := max(float32(0), -d.Y) * max(float32(0), -d.Y)
			return nt3dsh.RGB{0.12 + 1.5*warm, 0.12 + 0.65*warm + 0.3*cool, 0.16 + 0.15*warm + 1.5*cool}
		})
		return radiance
	})
)

type studioLighting struct {
	envIndex       int
	rigIndex       int
	sunAzimuth     float64 // Degrees around Z.
	sunElevation   float64 // Degrees above XY plane.
	shadows        bool
	lampPhase      float64
	customEnv      *nt3d.Environment
	customEnvName  string
	overrideInDemo bool
}

var envPresetNames = []string{
	"Studio Horizon",
	"Clear Day",
	"Golden Sunset",
	"Softbox Panorama",
	"Analytic SH",
	"Solid Dark",
}

var rigPresetNames = []string{
	"Key + Sky Fill",
	"3-Point Studio",
	"Orbiting RGB",
	"Night Lantern",
	"8-Lamp Ring",
	"Pure IBL",
}

func newStudioLighting() *studioLighting {
	return &studioLighting{
		envIndex:     0,
		rigIndex:     0,
		sunAzimuth:   -45,
		sunElevation: 48,
	}
}

func (l *studioLighting) envCount() int {
	if l.customEnv != nil {
		return len(envPresetNames) + 1
	}
	return len(envPresetNames)
}

func (l *studioLighting) envName() string {
	if l.customEnv != nil && l.envIndex == len(envPresetNames) {
		return "Panorama: " + l.customEnvName
	}
	if l.envIndex >= 0 && l.envIndex < len(envPresetNames) {
		return envPresetNames[l.envIndex]
	}
	return envPresetNames[0]
}

func (l *studioLighting) rigName() string {
	if l.rigIndex >= 0 && l.rigIndex < len(rigPresetNames) {
		return rigPresetNames[l.rigIndex]
	}
	return rigPresetNames[0]
}

func (l *studioLighting) environment() *nt3d.Environment {
	if l.customEnv != nil && l.envIndex == len(envPresetNames) {
		return l.customEnv
	}
	switch l.envIndex {
	case 0:
		return envStudioHorizon()
	case 1:
		return envClearDay()
	case 2:
		return envGoldenSunset()
	case 3:
		return envSoftboxPanorama()
	case 4:
		rad := l.sunAzimuth * math.Pi / 180
		if rotated, err := nt3dsh.Rotate(envSHBaseRadiance(), nt3dmath.RotationZ(rad)); err == nil {
			if env, err := nt3d.NewSHEnvironment(rotated); err == nil {
				return env
			}
		}
		return envStudioHorizon()
	case 5:
		return nil
	default:
		return envStudioHorizon()
	}
}

func (l *studioLighting) applyEnvironment(v *viewerState) {
	if v == nil || v.scene == nil {
		return
	}
	if len(v.splatNodes) > 0 && l.envIndex == 0 {
		v.scene.SetEnvironment(nil)
		return
	}
	v.scene.SetEnvironment(l.environment())
}

func (l *studioLighting) setCustomPanorama(name string, env *nt3d.Environment, v *viewerState) {
	l.customEnv = env
	l.customEnvName = name
	l.envIndex = len(envPresetNames)
	l.overrideInDemo = true
	l.applyEnvironment(v)
}

func (l *studioLighting) cycleEnvironment(v *viewerState) {
	l.envIndex = (l.envIndex + 1) % l.envCount()
	l.overrideInDemo = true
	l.applyEnvironment(v)
	fmt.Printf("Environment preset: %s\n", l.envName())
}

func (l *studioLighting) cycleRig() {
	l.rigIndex = (l.rigIndex + 1) % len(rigPresetNames)
	l.overrideInDemo = true
	fmt.Printf("Lighting rig: %s\n", l.rigName())
}

func (l *studioLighting) toggleShadows() {
	l.shadows = !l.shadows
	l.overrideInDemo = true
	fmt.Printf("GPU Shadow Mapping: %v\n", l.shadows)
}

func (l *studioLighting) rotateSun(dAzimuth, dElevation float64, v *viewerState) {
	l.sunAzimuth = math.Mod(l.sunAzimuth+dAzimuth, 360)
	if l.sunAzimuth > 180 {
		l.sunAzimuth -= 360
	} else if l.sunAzimuth < -180 {
		l.sunAzimuth += 360
	}
	l.sunElevation = max(5, min(88, l.sunElevation+dElevation))
	l.overrideInDemo = true
	if l.envIndex == 4 {
		l.applyEnvironment(v)
	}
}

func (l *studioLighting) step(dt float64) {
	if dt > 0 {
		l.lampPhase = math.Mod(l.lampPhase+dt, 200*math.Pi)
	}
}

func sunDirection(azimuthDeg, elevationDeg float64) nt3dmath.Vec3 {
	az := azimuthDeg * math.Pi / 180
	el := elevationDeg * math.Pi / 180
	cosEl := math.Cos(el)
	return (nt3dmath.Vec3{
		X: float32(cosEl * math.Cos(az)),
		Y: float32(cosEl * math.Sin(az)),
		Z: float32(math.Sin(el)),
	}).Normalize()
}

func hueWheel(h float64) color.RGBA {
	ch := func(shift float64) uint8 {
		v := math.Abs(math.Mod(h*6+shift, 6)-3) - 1
		return uint8(255 * max(0, min(1, v)))
	}
	return color.RGBA{ch(0), ch(4), ch(2), 255}
}

func (l *studioLighting) applyFrameLighting(f *nt3d.Frame, isDemo bool) {
	if isDemo && !l.overrideInDemo {
		return
	}
	if l.envIndex == 5 {
		f.Environment = nil
		f.Background = color.RGBA{20, 24, 32, 255}
	} else if isDemo {
		f.Environment = l.environment()
	}
	sunDir := sunDirection(l.sunAzimuth, l.sunElevation)
	switch l.rigIndex {
	case 0: // Key + Sky Fill
		f.Lighting = nt3d.Lighting{
			Ambient: nt3d.Ambient{
				Sky:       color.RGBA{95, 105, 120, 255},
				Ground:    color.RGBA{55, 50, 45, 255},
				Intensity: 1,
			},
			Lights: []nt3d.Light{{
				Kind:      nt3d.DirectionalLight,
				Direction: sunDir,
				Color:     color.RGBA{255, 250, 242, 255},
				Intensity: 0.9,
				Shadows:   l.shadows,
			}},
		}
	case 1: // 3-Point Studio
		fillDir := sunDirection(l.sunAzimuth+115, 20)
		rimDir := sunDirection(l.sunAzimuth+180, 35)
		f.Lighting = nt3d.Lighting{
			Ambient: nt3d.Ambient{
				Sky:       color.RGBA{60, 68, 82, 255},
				Ground:    color.RGBA{34, 34, 40, 255},
				Intensity: 0.65,
			},
			Lights: []nt3d.Light{
				{
					Kind:      nt3d.DirectionalLight,
					Direction: sunDir,
					Color:     color.RGBA{255, 242, 224, 255},
					Intensity: 1.0,
					Shadows:   l.shadows,
				},
				{
					Kind:      nt3d.DirectionalLight,
					Direction: fillDir,
					Color:     color.RGBA{150, 188, 255, 255},
					Intensity: 0.48,
				},
				{
					Kind:      nt3d.DirectionalLight,
					Direction: rimDir,
					Color:     color.RGBA{255, 255, 255, 255},
					Intensity: 0.62,
				},
			},
		}
	case 2: // Orbiting RGB
		f.Lighting = nt3d.Lighting{
			Ambient: nt3d.Ambient{
				Sky:       color.RGBA{45, 48, 58, 255},
				Ground:    color.RGBA{22, 22, 28, 255},
				Intensity: 0.35,
			},
		}
		if l.shadows {
			f.Lighting.Lights = append(f.Lighting.Lights, nt3d.Light{
				Kind:      nt3d.DirectionalLight,
				Direction: sunDir,
				Color:     color.RGBA{200, 210, 230, 255},
				Intensity: 0.35,
				Shadows:   true,
			})
		}
		rgbColors := [3]color.RGBA{{255, 65, 55, 255}, {65, 255, 95, 255}, {75, 120, 255, 255}}
		speeds := [3]float64{0.9, -0.7, 0.55}
		heights := [3]float32{0.45, 0.75, 1.0}
		for i := range 3 {
			a := l.lampPhase*speeds[i] + float64(i)*2*math.Pi/3
			f.Lighting.Lights = append(f.Lighting.Lights, nt3d.Light{
				Kind:      nt3d.PointLight,
				Position:  nt3dmath.Vec3{X: 1.55 * float32(math.Cos(a)), Y: 1.55 * float32(math.Sin(a)), Z: heights[i]},
				Color:     rgbColors[i],
				Intensity: 1.85,
				Range:     3.8,
			})
		}
	case 3: // Night Lantern
		a := l.lampPhase * 0.65
		f.Lighting = nt3d.Lighting{
			Ambient: nt3d.Ambient{
				Sky:       color.RGBA{24, 30, 56, 255},
				Ground:    color.RGBA{10, 10, 14, 255},
				Intensity: 0.85,
			},
		}
		if l.shadows {
			f.Lighting.Lights = append(f.Lighting.Lights, nt3d.Light{
				Kind:      nt3d.DirectionalLight,
				Direction: sunDir,
				Color:     color.RGBA{120, 140, 200, 255},
				Intensity: 0.3,
				Shadows:   true,
			})
		}
		f.Lighting.Lights = append(f.Lighting.Lights, nt3d.Light{
			Kind:      nt3d.PointLight,
			Position:  nt3dmath.Vec3{X: 1.5 * float32(math.Cos(a)), Y: 1.5 * float32(math.Sin(a)), Z: 0.42},
			Color:     color.RGBA{255, 175, 95, 255},
			Intensity: 2.25,
			Range:     3.5,
		})
	case 4: // 8-Lamp Ring
		f.Lighting = nt3d.Lighting{
			Ambient: nt3d.Ambient{
				Sky:       color.RGBA{38, 38, 46, 255},
				Ground:    color.RGBA{18, 18, 22, 255},
				Intensity: 0.22,
			},
		}
		count := nt3d.MaxLights
		if l.shadows {
			f.Lighting.Lights = append(f.Lighting.Lights, nt3d.Light{
				Kind:      nt3d.DirectionalLight,
				Direction: sunDir,
				Color:     color.RGBA{220, 225, 235, 255},
				Intensity: 0.35,
				Shadows:   true,
			})
			count = nt3d.MaxLights - 1
		}
		for i := range count {
			h := float64(i) / float64(count)
			a := l.lampPhase*0.45 + h*2*math.Pi
			f.Lighting.Lights = append(f.Lighting.Lights, nt3d.Light{
				Kind:      nt3d.PointLight,
				Position:  nt3dmath.Vec3{X: 1.8 * float32(math.Cos(a)), Y: 1.8 * float32(math.Sin(a)), Z: 0.55},
				Color:     hueWheel(h),
				Intensity: 1.35,
				Range:     2.6,
			})
		}
	case 5: // Pure IBL
		f.Lighting = nt3d.Lighting{}
	}
}

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
		_ = v.scene.SetOutline(v.selectedNode, nt3d.OutlineStyle{})
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
	_ = v.scene.SetOutline(hit.Node, nt3d.OutlineStyle{
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
		for i := range f.Instances {
			if f.Instances[i].Node == v.pickLabelID {
				f.Instances[i].Visible = false
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

func (v *viewerState) buildFrame(camera nt3d.Camera, w, h, supersample int) (nt3d.Frame, error) {
	if v.runner != nil {
		f, err := v.runner.Frame(camera, w, h)
		if err != nil {
			return f, err
		}
		if supersample > f.Supersample {
			f.Supersample = supersample
		}
		return f, nil
	}
	f, err := v.scene.Frame(camera, w, h)
	if err != nil {
		return f, err
	}
	f.Supersample = supersample
	return f, nil
}

func savePNG(path string, img image.Image) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

var turntablePalette = sync.OnceValue(func() color.Palette {
	pal := make(color.Palette, 0, 256)
	// 6x6x6 perceptual RGB cube (216 colors).
	steps := [6]uint8{0, 51, 102, 153, 204, 255}
	for _, r := range steps {
		for _, g := range steps {
			for _, b := range steps {
				pal = append(pal, color.RGBA{R: r, G: g, B: b, A: 255})
			}
		}
	}
	// 24-step neutral grayscale ramp + 16-step studio sky/slate ramp (40 colors).
	for i := range 24 {
		v := uint8((i*255 + 11) / 23)
		pal = append(pal, color.RGBA{R: v, G: v, B: v, A: 255})
	}
	for i := range 16 {
		t := float64(i) / 15.0
		pal = append(pal, color.RGBA{
			R: uint8(18 + t*110),
			G: uint8(24 + t*125),
			B: uint8(36 + t*160),
			A: 255,
		})
	}
	return pal
})

func saveTurntableGIF(path string, frames []*image.NRGBA, fps int) error {
	if len(frames) == 0 {
		return fmt.Errorf("saveTurntableGIF: no frames to encode")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if fps <= 0 {
		fps = 24
	}
	delay := max(1, (100+fps/2)/fps)
	pal := turntablePalette()
	paletted := make([]*image.Paletted, len(frames))
	delays := make([]int, len(frames))

	var wg sync.WaitGroup
	wg.Add(len(frames))
	for i, src := range frames {
		delays[i] = delay
		go func(idx int, img *image.NRGBA) {
			defer wg.Done()
			dst := image.NewPaletted(img.Bounds(), pal)
			draw.FloydSteinberg.Draw(dst, img.Bounds(), img, img.Bounds().Min)
			paletted[idx] = dst
		}(i, src)
	}
	wg.Wait()

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := gif.EncodeAll(f, &gif.GIF{
		Image: paletted,
		Delay: delays,
	}); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func buildCaptureFrame(v *viewerState, cam nt3d.Camera, lighting *studioLighting, w, h, supersample int) (nt3d.Frame, error) {
	if v.runner != nil {
		v.runner.SetViewportSize(w, h)
	}
	f, err := v.buildFrame(cam, w, h, max(1, supersample))
	if err != nil {
		return nt3d.Frame{}, err
	}
	if lighting != nil {
		lighting.applyFrameLighting(&f, v.runner != nil)
	}
	if len(v.splatNodes) > 0 && (lighting == nil || lighting.envIndex == 0) {
		f.Background = color.RGBA{18, 20, 26, 255}
	}
	v.decorateFrame(&f, cam, false, false)
	return f, nil
}

func captureSingleFrame(session *nt3d.GPUSession, v *viewerState, cam nt3d.Camera, lighting *studioLighting, w, h, supersample int) (*image.NRGBA, error) {
	f, err := buildCaptureFrame(v, cam, lighting, w, h, supersample)
	if err != nil {
		return nil, err
	}
	return session.RenderToImage(f)
}

func captureSingleFrameInto(session *nt3d.GPUSession, v *viewerState, cam nt3d.Camera, lighting *studioLighting, w, h, supersample int, dst *image.NRGBA) error {
	f, err := buildCaptureFrame(v, cam, lighting, w, h, supersample)
	if err != nil {
		return err
	}
	return session.RenderToImageInto(f, dst)
}

func captureScreenshot(session *nt3d.GPUSession, v *viewerState, cam nt3d.Camera, lighting *studioLighting, w, h, supersample int, path string) error {
	img, err := captureSingleFrame(session, v, cam, lighting, w, h, max(2, supersample))
	if err != nil {
		return err
	}
	return savePNG(path, img)
}

func captureTurntable(session *nt3d.GPUSession, v *viewerState, baseCam nt3d.Camera, lighting *studioLighting, w, h, supersample, numFrames, fps int, path string) error {
	numFrames = max(1, numFrames)
	fps = max(1, fps)
	dt := 1.0 / float64(fps)
	if v.runner != nil {
		v.runner.DetachSampleCamera()
		defer v.runner.SetCamera(baseCam)
	}

	ext := filepath.Ext(path)
	if strings.EqualFold(ext, ".png") {
		buf := image.NewNRGBA(image.Rect(0, 0, w, h))
		stem := strings.TrimSuffix(path, ext)
		for i := range numFrames {
			if i > 0 {
				if lighting != nil {
					lighting.step(dt)
				}
				if v.runner != nil {
					v.runner.Step(dt)
				} else if v.player != nil {
					_ = v.player.Step(dt)
				}
			}
			cam := baseCam.Orbit(float64(i)*360.0/float64(numFrames), 0)
			if v.runner != nil {
				v.runner.SetCamera(cam)
			}
			if err := captureSingleFrameInto(session, v, cam, lighting, w, h, max(1, supersample), buf); err != nil {
				return err
			}
			framePath := path
			if numFrames > 1 {
				framePath = fmt.Sprintf("%s-%03d%s", stem, i, ext)
			}
			if err := savePNG(framePath, buf); err != nil {
				return err
			}
		}
		return nil
	}

	frames := make([]*image.NRGBA, 0, numFrames)
	for i := range numFrames {
		if i > 0 {
			if lighting != nil {
				lighting.step(dt)
			}
			if v.runner != nil {
				v.runner.Step(dt)
			} else if v.player != nil {
				_ = v.player.Step(dt)
			}
		}
		cam := baseCam.Orbit(float64(i)*360.0/float64(numFrames), 0)
		if v.runner != nil {
			v.runner.SetCamera(cam)
		}
		img, err := captureSingleFrame(session, v, cam, lighting, w, h, max(1, supersample))
		if err != nil {
			return err
		}
		frames = append(frames, img)
	}
	return saveTurntableGIF(path, frames, fps)
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
	demoNames := gallery.Names()
	demoFlag := flag.String("demo", "", "interactive demo to run ("+strings.Join(demoNames, ", ")+", or 'list')")
	fileFlag := flag.String("file", "", "path to a 3D model or Gaussian splat file (.glb, .gltf, .sog, .spz, .ply, .stl, .obj, .3mf)")
	yUp := flag.Bool("y-up", false, "convert Y-up files to Z-up when loading")
	width := flag.Int("width", 960, "initial window width")
	height := flag.Int("height", 640, "initial window height")
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

	title := "NT3D + GoGPU (18 Interactive Demos & 3D Viewer)"
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
		supersample     = 1
		angle           float64
		frameCount      int
		wantSecond      bool
		hasSecond       bool
	)

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

	loadDroppedOrSelectedPaths := func(paths []string) {
		if len(paths) == 0 {
			return
		}
		var modelPaths []string
		for _, p := range paths {
			switch strings.ToLower(filepath.Ext(p)) {
			case ".png", ".jpg", ".jpeg":
				f, err := os.Open(p)
				if err != nil {
					log.Printf("open panorama %q: %v", p, err)
					continue
				}
				img, _, err := image.Decode(f)
				_ = f.Close()
				if err != nil {
					log.Printf("decode panorama %q: %v", p, err)
					continue
				}
				env, err := nt3d.NewEnvironment(img)
				if err != nil {
					log.Printf("NewEnvironment(%q): %v", p, err)
					continue
				}
				lighting.setCustomPanorama(filepath.Base(p), env, viewer)
				fmt.Printf("Loaded custom environment panorama: %s (%dx%d)\n",
					filepath.Base(p), img.Bounds().Dx(), img.Bounds().Dy())
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
					Extensions: []string{"glb", "gltf", "sog", "spz", "ply", "stl", "obj", "3mf", "png", "jpg", "jpeg"},
				},
				{
					Name:       "3D Models & Gaussian Splats",
					Extensions: []string{"glb", "gltf", "sog", "spz", "ply", "stl", "obj", "3mf"},
				},
				{
					Name:       "Environment Panoramas",
					Extensions: []string{"png", "jpg", "jpeg"},
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
		if err := captureScreenshot(session, viewer, camera, lighting, w, h, supersample, path); err != nil {
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
		if err := captureTurntable(session, viewer, camera, lighting, w, h, supersample, nFrames, fps, path); err != nil {
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
			lighting.cycleRig()
		case gpucontext.KeyK:
			lighting.toggleShadows()
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
			pipView = !pipView
			fmt.Printf("Sub-viewport inset: %v\n", pipView)
		case gpucontext.KeyB:
			loadCompose = !loadCompose
			fmt.Printf("Load composition mode: %v\n", loadCompose)
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
				f, err := viewer.buildFrame(topCam, int(w), int(h), supersample)
				if err != nil {
					return
				}
				lighting.applyFrameLighting(&f, viewer.runner != nil)
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
					if err := captureScreenshot(session, viewer, camera, lighting, cw, ch, supersample, *screenshotFlag); err != nil {
						log.Fatalf("captureScreenshot(%q): %v", *screenshotFlag, err)
					}
					hud.lastCapture = fmt.Sprintf("%s (%dx%d)", filepath.Base(*screenshotFlag), cw, ch)
					fmt.Printf("Saved offscreen screenshot: %s (%dx%d)\n", *screenshotFlag, cw, ch)
				}
				if *turntableFlag != "" {
					nFrames := max(1, *turntableFrames)
					fps := max(1, *turntableFPS)
					if err := captureTurntable(session, viewer, camera, lighting, cw, ch, supersample, nFrames, fps, *turntableFlag); err != nil {
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

		f, err := viewer.buildFrame(camera, int(w), int(h), supersample)
		if err != nil {
			log.Printf("buildFrame: %v", err)
			return
		}
		lighting.applyFrameLighting(&f, viewer.runner != nil)
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
				if pipFrame, err := viewer.buildFrame(topCam, pipW, pipH, 1); err == nil {
					lighting.applyFrameLighting(&pipFrame, viewer.runner != nil)
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
			hudText := hud.formatText(viewer, camera, lighting, animate, pipView, loadCompose, showHUD, menuOpen, supersample)
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
