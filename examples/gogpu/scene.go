// Copyright (c) 2026 Neomantra Corp.

package main

import (
	"fmt"
	"image/color"
	"log"
	"math"
	"path/filepath"
	"slices"
	"strings"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/examples/demos/gallery"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
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
			PBR:       true,
			Metallic:  0.1,
			Roughness: 0.42,
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

	// Brushed copper-gold conductor center sphere.
	centerSphere, err := nt3d.BuildSphere(nt3d.Sphere{
		Radius: 0.42,
		Color:  color.RGBA{242, 165, 75, 255},
		Material: &nt3d.Material{
			PBR:       true,
			Metallic:  0.94,
			Roughness: 0.18,
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

	// Anodized cyan metallic torus ring with subtle HDR emissive glow.
	torus, err := nt3d.BuildTorus(nt3d.Torus{
		MajorRadius: 0.72,
		MinorRadius: 0.10,
		Color:       color.RGBA{75, 195, 235, 255},
		Material: &nt3d.Material{
			PBR:               true,
			Metallic:          0.85,
			Roughness:         0.26,
			Emissive:          color.RGBA{30, 150, 225, 255},
			EmissiveIntensity: 1.6,
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

	// High-IOR crystal glass satellites.
	satSphere, err := nt3d.BuildSphere(nt3d.Sphere{
		Radius: 0.18,
		Color:  color.RGBA{255, 95, 145, 165},
		Material: &nt3d.Material{
			PBR:         true,
			Transparent: true,
			Roughness:   0.12,
			IOR:         1.8,
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

func (v *viewerState) buildFrame(camera nt3d.Camera, w, h int, msaa bool, supersample int) (nt3d.Frame, error) {
	if v.runner != nil {
		f, err := v.runner.Frame(camera, w, h)
		if err != nil {
			return f, err
		}
		if msaa {
			f.MSAA = true
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
	f.MSAA = msaa
	f.Supersample = supersample
	return f, nil
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
