// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"fmt"
	"image/color"
	"io/fs"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
	nt3dwidget "nimbleterminal.dev/nt3d/widget"
)

var demoNames = []string{
	"terrain",
	"procedural",
	"dynamic",
	"shapes",
	"models",
	"large",
	"fur",
	"mipmaps",
	"forest",
	"lights",
	"particles",
	"gaussians",
	"animation",
	"animate",
	"characters",
	"cloud",
	"aabb",
	"water",
}

// Names returns the ordered list of all 18 interactive demo scene names.
func Names() []string {
	out := make([]string, len(demoNames))
	copy(out, demoNames)
	return out
}

// Runner drives one of the interactive demo scenes inside an external host
// window (such as GoGPU) without starting a Bubble Tea terminal program.
type Runner struct {
	name    string
	samples fs.FS
	d       *demo
	async   chan paletteDoneMsg
}

// NewRunner constructs and initializes the named demo scene.
func NewRunner(name string, samples fs.FS, files []string, up nt3dmesh.Up, yDown bool) (*Runner, error) {
	proceduralDefault := false
	var kind sceneMode
	switch name {
	case "terrain":
		kind = sceneTerrain
	case "procedural":
		proceduralDefault = true
		kind = sceneTerrain
	case "dynamic":
		kind = sceneDynamic
	case "shapes", "meshes":
		kind = sceneMeshes
	case "models":
		kind = sceneModels
	case "large":
		kind = sceneLarge
	case "fur":
		kind = sceneFur
	case "mipmaps":
		kind = sceneMipmaps
	case "forest":
		kind = sceneForest
	case "lights":
		kind = sceneLights
	case "particles":
		kind = sceneParticles
	case "gaussians":
		kind = sceneGaussians
	case "animation":
		kind = sceneAnimation
	case "animate":
		kind = sceneAnimate
	case "characters":
		kind = sceneCharacters
	case "cloud":
		kind = sceneCloud
	case "aabb":
		kind = sceneAABB
	case "water":
		kind = sceneWater
	default:
		return nil, fmt.Errorf("unknown demo %q (valid: %s)", name, strings.Join(demoNames, ", "))
	}

	SampleFiles = samples
	d := &demo{
		scene:     nt3d.NewScene(),
		zones:     zone.New(),
		animate:   kind != sceneAABB,
		lastPhase: time.Now(),
	}
	var err error
	defer func() {
		if err != nil {
			if d.view != nil {
				_ = d.view.Close()
			}
			d.zones.Close()
		}
	}()

	d.group, err = d.scene.Add(nt3d.Root, "island")
	if err != nil {
		return nil, err
	}

	switch kind {
	case sceneCharacters:
		err = d.setupCharacters()
	case sceneAnimate:
		err = d.setupAnimate()
	case sceneAnimation:
		err = d.setupAnimation()
	case sceneDynamic:
		err = d.setupDynamic()
	case sceneMeshes:
		err = d.setupMeshes(files, up)
	case sceneModels:
		err = d.setupModels()
	case sceneLarge:
		err = d.setupLarge()
	case sceneFur:
		err = d.setupFur()
	case sceneMipmaps:
		err = d.setupMipmaps()
	case sceneForest:
		err = d.setupForest()
	case sceneLights:
		err = d.setupLights()
	case sceneGaussians:
		err = d.setupGaussians()
	case sceneParticles:
		err = d.setupParticles()
	case sceneCloud:
		err = d.setupCloud()
	case sceneWater:
		err = d.setupWater()
	case sceneAABB:
		err = d.setupAABB()
	default:
		d.procedural = proceduralDefault
		if err = d.loadTextures(); err == nil {
			if err = d.terrain(); err == nil {
				var marker nt3d.NodeID
				marker, err = d.scene.SetGeometry("marker", nt3d.Geometry{Boxes: []nt3d.Box{{
					Min:   nt3dmath.Vec3{X: .65, Y: .25, Z: .2},
					Size:  nt3dmath.Vec3{X: .12, Y: .12, Z: .35},
					Color: color.RGBA{200, 72, 48, 255},
				}}})
				if err == nil {
					err = d.scene.Reparent(marker, d.group)
				}
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if kind == sceneMeshes && yDown {
		if err = d.scene.SetTransform(d.group, nt3dmath.Scaling(nt3dmath.Vec3{X: 1, Y: -1, Z: -1})); err != nil {
			return nil, err
		}
	}

	camera := nt3d.DefaultCamera()
	camera.Distance = 4.3
	switch kind {
	case sceneDynamic:
		camera.Distance = 5.5
	case sceneMipmaps:
		camera.Alpha, camera.Distance, camera.Projection = 8, 2.6, nt3d.Perspective
	case sceneForest:
		camera.Alpha, camera.Distance, camera.Projection = 24, 9, nt3d.Perspective
	case sceneLights:
		camera.Alpha, camera.Distance, camera.Projection = 30, 5.5, nt3d.Perspective
	case sceneParticles:
		camera.Alpha, camera.Distance, camera.Projection = 14, 4.6, nt3d.Perspective
		camera.Target = nt3dmath.Vec3{Z: .9}
	case sceneCloud:
		camera.Alpha, camera.Distance, camera.Projection = 32, 9, nt3d.Perspective
	case sceneWater:
		camera.Alpha, camera.Beta, camera.Distance, camera.Projection = 27, 35, 11, nt3d.Perspective
	case sceneAABB:
		camera.Alpha, camera.Distance, camera.Projection = 30, 15, nt3d.Perspective
	}
	camera.AutoRotate = proceduralDefault

	d.view = nt3dwidget.New(
		d.scene,
		96,
		32,
		nt3dwidget.WithCamera(camera),
		nt3dwidget.WithRenderMode(nt3d.Software),
		nt3dwidget.WithZoneManager(d.zones),
		nt3dwidget.WithFrameDecorator(d.decorate),
	)

	switch kind {
	case sceneMeshes:
		d.fitMeshes()
	case sceneLarge, sceneAnimation, sceneGaussians:
		d.view.Fit()
		if kind == sceneAnimation {
			cam := d.view.Camera()
			cam.Distance *= 1.4
			d.view.SetCamera(cam)
		}
	case sceneFur:
		cam := d.view.Camera()
		cam.Beta = 30
		d.view.SetCamera(cam)
		d.view.Fit()
		d.view.SetSupersample(2)
	case sceneAnimate, sceneCharacters:
		d.frameAnimatedSample()
	case sceneModels:
		d.frameSample()
	}
	d.scene.WarmPickIndices()

	return &Runner{
		name:    name,
		samples: samples,
		d:       d,
		async:   make(chan paletteDoneMsg, 4),
	}, nil
}

// Close releases the runner's widget and zone resources and cancels any
// in-flight background palette build.
func (r *Runner) Close() error {
	if r == nil || r.d == nil {
		return nil
	}
	if r.d.meshes != nil && r.d.meshes.palette.building && r.d.meshes.palette.cancel != nil {
		r.d.meshes.palette.cancel()
	}
	var err error
	if r.d.view != nil {
		err = r.d.view.Close()
	}
	if r.d.zones != nil {
		r.d.zones.Close()
	}
	r.d = nil
	return err
}

// Name returns the demo's identifier (e.g. "water", "fur", "characters").
func (r *Runner) Name() string {
	if r == nil {
		return ""
	}
	return r.name
}

// Scene returns the demo's 3D scene graph.
func (r *Runner) Scene() *nt3d.Scene {
	if r == nil || r.d == nil {
		return nil
	}
	return r.d.scene
}

// Camera returns the demo's current camera state.
func (r *Runner) Camera() nt3d.Camera {
	if r == nil || r.d == nil || r.d.view == nil {
		return nt3d.DefaultCamera()
	}
	return r.d.view.Camera()
}

// SetCamera updates the demo's camera state.
func (r *Runner) SetCamera(c nt3d.Camera) {
	if r == nil || r.d == nil || r.d.view == nil {
		return
	}
	r.d.view.SetCamera(c)
}

// DetachSampleCamera switches back from an authored glTF camera node to the
// interactive orbit camera when the user orbits, pans, or zooms.
func (r *Runner) DetachSampleCamera() {
	if r == nil || r.d == nil || r.d.animatedSamples == nil {
		return
	}
	if r.d.animatedSamples.camera > 0 {
		r.d.animatedSamples.camera = 0
		r.d.view.SetCamera(r.d.animatedSamples.orbit)
	}
}

// SetViewportSize updates the internal widget's aspect ratio so camera fits
// match the host window dimensions.
func (r *Runner) SetViewportSize(w, h int) {
	if r == nil || r.d == nil || r.d.view == nil || w <= 0 || h <= 0 {
		return
	}
	// Model.Fit uses width / (2 * height) when no terminal cell raster is active.
	cellW := max(1, min(400, w/10))
	cellH := max(1, min(200, h/20))
	r.d.view.SetSize(cellW, cellH)
}

// Fit frames the active scene for the given viewport aspect ratio.
func (r *Runner) Fit(aspect float64) {
	if r == nil || r.d == nil || r.d.view == nil {
		return
	}
	r.DetachSampleCamera()
	if r.d.meshes != nil && r.d.meshes.capture {
		r.d.fitMeshes()
		return
	}
	r.d.view.SetCamera(r.d.view.Camera().Fit(r.d.scene.Bounds(), aspect))
}

// Supersample returns the active demo's requested GPU supersampling factor.
func (r *Runner) Supersample() int {
	if r == nil || r.d == nil || r.d.view == nil {
		return 1
	}
	return r.d.view.Supersample()
}

// SetSupersample sets the active demo's GPU supersampling factor (1..4).
func (r *Runner) SetSupersample(n int) {
	if r == nil || r.d == nil || r.d.view == nil {
		return
	}
	r.d.view.SetSupersample(n)
}

// Animating reports whether the demo's simulation/animation loop is running.
func (r *Runner) Animating() bool {
	if r == nil || r.d == nil {
		return false
	}
	return r.d.animate
}

// HandlesOwnPick reports whether the demo manages its own pick highlighting
// in the scene graph (true for the AABB tree demo).
func (r *Runner) HandlesOwnPick() bool {
	return r != nil && r.d != nil && r.d.aabb != nil
}

// Step advances the demo's simulation, animations, procedural textures, and
// camera auto-rotation by dt seconds.
func (r *Runner) Step(dt float64) {
	if r == nil || r.d == nil || dt <= 0 {
		return
	}
	SampleFiles = r.samples
	for {
		select {
		case done := <-r.async:
			r.d.Update(done)
		default:
			goto steppedAsync
		}
	}
steppedAsync:
	cam := r.d.view.Camera()
	if cam.AutoRotate {
		r.d.view.SetCamera(cam.Step(dt, cam.ResumeAfter))
	}
	now := time.Now()
	r.d.lastPhase = now.Add(-time.Duration(dt * float64(time.Second)))
	r.d.Update(phaseMsg(now))
}

// SendKey routes a key press (such as "a", "w", "s", "space", "enter", "[", "]")
// to the demo's update loop.
func (r *Runner) SendKey(keyStr string) {
	if r == nil || r.d == nil || keyStr == "" {
		return
	}
	SampleFiles = r.samples
	var msg tea.KeyPressMsg
	switch keyStr {
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case " ", "space":
		msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "left":
		msg = tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		msg = tea.KeyPressMsg{Code: tea.KeyRight}
	case "up":
		msg = tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		msg = tea.KeyPressMsg{Code: tea.KeyDown}
	default:
		ru, size := utf8.DecodeRuneInString(keyStr)
		if ru == utf8.RuneError || size != len(keyStr) {
			return
		}
		msg = tea.KeyPressMsg{Code: ru, Text: keyStr}
	}
	if r.d.animatedSamples != nil && r.d.animatedSamples.camera > 0 && detachesSampleCamera(msg) {
		r.d.animatedSamples.camera = 0
		r.d.view.SetCamera(r.d.animatedSamples.orbit)
	}
	_, cmd := r.d.Update(msg)
	r.d.scene.WarmPickIndices()
	if keyStr == "w" && r.d.meshes != nil && r.d.meshes.palette.building && cmd != nil {
		async := r.async
		go func() {
			if done := runAsyncPalette(cmd); done != nil {
				async <- *done
			}
		}()
	}
}

func detachesSampleCamera(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case "c", "r", "o", "+", "-", "_", "=", "left", "right", "up", "down", "h", "j", "l":
		return true
	}
	return false
}

func runAsyncPalette(cmd tea.Cmd) *paletteDoneMsg {
	if cmd == nil {
		return nil
	}
	switch m := cmd().(type) {
	case paletteDoneMsg:
		return &m
	case tea.BatchMsg:
		if len(m) > 0 {
			return runAsyncPalette(m[0])
		}
	}
	return nil
}

// Pick performs a 3D pick at pixel coordinates (x, y) on frame f.
func (r *Runner) Pick(camera nt3d.Camera, f nt3d.Frame, x, y, tol float32) *nt3d.Hit {
	return r.PickGPU(nil, camera, f, x, y, tol)
}

// PickGPU performs a 3D pick at pixel coordinates (x, y) on frame f, using
// session's GPU depth buffer when available to accelerate picking on
// unindexed or deforming meshes.
func (r *Runner) PickGPU(session *nt3d.GPUSession, camera nt3d.Camera, f nt3d.Frame, x, y, tol float32) *nt3d.Hit {
	if r == nil || r.d == nil {
		return nil
	}
	if r.d.aabb != nil {
		ray := camera.RayFromPixel(x, y, f.Width, f.Height)
		if err := r.d.pickAABB(ray); err != nil {
			r.d.status = err.Error()
		}
		return nil
	}
	var hit *nt3d.Hit
	if session != nil {
		hit = session.Pick(r.d.scene, camera, f, x, y, tol)
	} else {
		hit = r.d.scene.Pick(camera, f, x, y, tol)
	}
	r.d.status = "no hit"
	if hit != nil {
		r.d.status = fmt.Sprintf("%s #%d at %.2f %.2f %.2f", hit.Name, hit.Index, hit.Position.X, hit.Position.Y, hit.Position.Z)
	}
	return hit
}

// Frame builds and decorates a renderable Frame for the demo at (w, h).
func (r *Runner) Frame(camera nt3d.Camera, w, h int) (nt3d.Frame, error) {
	if r == nil || r.d == nil {
		return nt3d.Frame{}, fmt.Errorf("nil demo runner")
	}
	f, err := r.d.scene.Frame(camera, w, h)
	if err != nil {
		return f, err
	}
	f.Supersample = r.d.view.Supersample()
	r.d.decorate(&f)
	return f, nil
}

// HeaderAndStatus returns the demo's live controls header line and status
// footer line, stripped of terminal-specific backend/quit text.
func (r *Runner) HeaderAndStatus() (header, status string) {
	if r == nil || r.d == nil {
		return "", ""
	}
	const sentinel = "\x00VIEW\x00"
	d := r.d
	var raw string
	switch {
	case d.fur != nil:
		raw = d.furView(sentinel)
	case d.mipmaps != nil:
		raw = d.mipmapsView(sentinel)
	case d.forest != nil:
		raw = d.forestView(sentinel)
	case d.lights != nil:
		raw = d.lightsView(sentinel)
	case d.particles != nil:
		raw = d.particlesView(sentinel)
	case d.cloud != nil:
		raw = d.cloudView(sentinel)
	case d.water != nil:
		raw = d.waterView(sentinel)
	case d.aabb != nil:
		raw = d.aabbView(sentinel)
	case d.large != nil:
		raw = d.largeView(sentinel)
	case d.models != nil:
		raw = d.modelsView(sentinel)
	case d.meshes != nil:
		raw = d.meshesView(sentinel)
	case d.animatedSamples != nil:
		raw = d.animateView(sentinel)
	case d.animation != nil:
		raw = "nt3d glTF animation · a pause\n" + sentinel + "\nTwo joints, a morph target, and generic TRS tracks · drag orbit · wheel zoom"
	case d.dynamic != nil:
		state := "paused"
		if d.animate {
			state = "running"
		}
		mesh := "held"
		if d.dynamic.deform {
			mesh = "deforming"
		}
		raw = "nt3d dynamic scene · a pause · m deform · b add · x remove · v visibility\n" + sentinel +
			fmt.Sprintf("\n%s · %s · mesh %s · %d orbiters · %s", d.rendering(), state, mesh, len(d.dynamic.bodies), d.status)
	default:
		title := "nt3d terrain"
		st := d.status
		if d.procedural {
			title = "nt3d procedural plasma"
			if d.animate {
				st += " · animating"
			} else {
				st += " · paused"
			}
		}
		raw = title + " · a animate · p procedural · t texture · f flat · l lighting · n parent rotation · v visibility\n" + sentinel + "\n" + d.rendering() + " · " + st
	}

	parts := strings.SplitN(raw, "\n"+sentinel+"\n", 2)
	if len(parts) == 2 {
		header, status = parts[0], parts[1]
	} else {
		header = raw
	}
	header = strings.TrimSuffix(header, " · 1/2/3 backend · q quit")
	header = strings.TrimSuffix(header, " · q quit")
	prefix := d.rendering() + " · "
	status = strings.TrimPrefix(status, prefix)
	status = strings.ReplaceAll(status, " · drag orbit · shift pan · wheel zoom · r rotate · o projection · g glyph", "")
	status = strings.ReplaceAll(status, " · drag orbit · wheel zoom · c fit · r rotate", "")
	status = strings.ReplaceAll(status, " · drag orbit · wheel zoom · r rotate", "")
	status = strings.ReplaceAll(status, " · drag orbit · wheel zoom", "")
	return asciiClean(header), asciiClean(status)
}

var asciiReplacer = strings.NewReplacer(
	" · ", " | ",
	"·", "|",
	"×", "x",
	"→", "->",
	"°", " deg",
	"²", "^2",
	"…", "...",
)

func asciiClean(s string) string {
	return asciiReplacer.Replace(s)
}
