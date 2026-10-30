// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	booba "github.com/NimbleMarkets/go-booba"
	zone "github.com/lrstanley/bubblezone/v2"
	flag "github.com/spf13/pflag"
	"image"
	"image/color"
	"image/png"
	"math"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
	"nimbleterminal.dev/nt3d/widget"
	"nimbleterminal.dev/ntpicture"
	"os"
	"runtime"
	"slices"
	"time"
)

type demo struct {
	animatedSamples   *animateScene
	animation         *nt3d.Animation
	animationTime     float64
	view              *nt3dwidget.Model
	kittySession      *ntpicture.TerminalSession
	fog               bool // Fog that keeps its look as the camera zooms.
	scene             *nt3d.Scene
	group             nt3d.NodeID
	zones             *zone.Manager
	textures          [2]*nt3d.Texture
	style             int
	procedural        bool
	flat, lit, hidden bool
	angle             float64
	status            string
	duration          time.Duration
	animate           bool
	phase             float64
	lastPhase         time.Time
	closed            bool
	dynamic           *dynamicScene
	meshes            *meshScene
	models            *modelScene
	large             *largeScene
	fur               *furScene
	mipmaps           *mipScene
	forest            *forestScene
	lights            *lightScene
	particles         *particleScene
	cloud             *cloudScene
	aabb              *aabbScene
	water             *waterScene
	splats            splatStats
}

type shadowMode int

const (
	shadowNone shadowMode = iota
	shadowDrop
	shadowMap
)

func (m shadowMode) String() string {
	switch m {
	case shadowDrop:
		return "drop shadows"
	case shadowMap:
		return "shadow map"
	default:
		return "no shadows"
	}
}

func (d *demo) cycleShadows() error {
	if d.lights != nil {
		d.lights.shadows = (d.lights.shadows + 1) % 3
		return d.buildLightShapes()
	}
	if d.forest != nil {
		d.forest.shadows = (d.forest.shadows + 1) % 3
		for _, tree := range d.forest.trees {
			if err := d.dressTree(tree); err != nil {
				return err
			}
		}
	}
	return nil
}

type sceneMode int

const (
	sceneTerrain sceneMode = iota
	sceneDynamic
	sceneMeshes
	sceneModels
	sceneLarge
	sceneFur
	sceneMipmaps
	sceneForest
	sceneLights
	sceneParticles
	sceneGaussians
	sceneAnimation
	sceneAnimate
	sceneCharacters
	sceneCloud
	sceneAABB
	sceneWater
)

// terrainScene reports whether the terrain controls apply.
func (d *demo) terrainScene() bool {
	return d.animation == nil && d.dynamic == nil && d.meshes == nil && d.large == nil && d.fur == nil && d.mipmaps == nil && d.forest == nil && d.lights == nil && d.particles == nil && d.cloud == nil && d.aabb == nil && d.water == nil
}

type quitMsg struct{}
type phaseMsg time.Time

func phaseTick() tea.Cmd {
	return tea.Tick(time.Second/30, func(t time.Time) tea.Msg { return phaseMsg(t) })
}

func (d *demo) updatePlasma() error {
	tex, err := plasmaTexture(d.phase + float64(d.style)*2)
	if err != nil {
		return err
	}
	d.textures[d.style] = tex
	return d.scene.SetTexture("terrain", tex)
}

func (d *demo) terrain() error {
	g, err := nt3d.BuildHeightfield(nt3d.Heightfield{NX: 72, NY: 56, MinX: -1.5, MaxX: 1.5, MinY: -1, MaxY: 1, Sampler: func(x, y float32) float32 {
		if d.flat {
			return 0
		}
		return float32(.6*math.Exp(-float64(x*x+y*y)*2) + .13*math.Sin(float64(x)*7)*math.Cos(float64(y)*5))
	}, Material: &nt3d.Material{Texture: d.textures[d.style], Unlit: !d.lit}})
	if err != nil {
		return err
	}
	id, err := d.scene.SetGeometry("terrain", g)
	if err != nil {
		return err
	}
	if err := d.scene.Reparent(id, d.group); err != nil {
		return err
	}
	return d.terrainLabels(id)
}
func (d *demo) Init() tea.Cmd {
	d.lastPhase = time.Now()
	cmd := tea.Batch(d.view.Init(), phaseTick())
	if d.duration > 0 {
		return tea.Batch(cmd, tea.Tick(d.duration, func(time.Time) tea.Msg { return quitMsg{} }))
	}
	return cmd
}
func (d *demo) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if state := d.animatedSamples; state != nil && state.camera > 0 {
		detach := false
		switch v := msg.(type) {
		case tea.MouseClickMsg, tea.MouseWheelMsg:
			detach = true
		case tea.KeyPressMsg:
			switch v.String() {
			case "c", "r", "o", "+", "-", "_", "=", "left", "right", "up", "down", "h", "j", "l":
				detach = true
			}
		}
		if detach {
			state.camera = 0
			// Replay the gesture after restoring the editable orbit view.
			return d, tea.Sequence(d.view.SetCamera(state.orbit), func() tea.Msg { return msg })
		}
	}
	switch v := msg.(type) {
	case phaseMsg:
		if d.closed {
			return d, nil
		}
		now := time.Time(v)
		elapsed := now.Sub(d.lastPhase).Seconds()
		d.lastPhase = now
		if d.cloud != nil {
			// The cloud reselects for the view the last frame saw, paused or not.
			changed, err := d.stepCloud()
			if err != nil {
				d.status = err.Error()
			}
			if changed {
				return d, tea.Batch(d.view.Refresh(), phaseTick())
			}
			return d, phaseTick()
		}
		if !d.animate || (!d.procedural && d.animation == nil && d.dynamic == nil && d.fur == nil && d.forest == nil && d.lights == nil && d.particles == nil && d.aabb == nil && d.water == nil) {
			return d, phaseTick()
		}
		d.animationTime += elapsed
		d.phase = math.Mod(d.phase+elapsed, 2*math.Pi)
		// Let an in-flight frame finish; the next update catches up to wall time.
		if d.view.Busy() {
			return d, phaseTick()
		}
		var err error
		switch {
		case d.animation != nil:
			err = d.animation.Sample(d.animationTime)
		case d.dynamic != nil:
			err = d.stepDynamic()
		case d.fur != nil:
			err = d.stepFur()
		case d.forest != nil:
			err = d.stepForest()
		case d.lights != nil:
			err = d.stepLights()
		case d.particles != nil:
			err = d.stepParticles()
		case d.water != nil:
			err = d.stepWater()
		case d.aabb != nil:
			err = d.stepAABB()
		default:
			err = d.updatePlasma()
		}
		if err != nil {
			d.status = err.Error()
			d.animate = false
			return d, phaseTick()
		}
		return d, tea.Batch(d.followSampleCamera(), d.view.Refresh(), phaseTick())
	case paletteTickMsg:
		if d.meshes != nil && d.meshes.palette.building {
			return d, paletteTick()
		}
		return d, nil
	case paletteDoneMsg:
		if d.meshes == nil {
			return d, nil
		}
		if err := d.finishPalette(v); err != nil {
			d.status = err.Error()
		}
		return d, d.view.Refresh()
	case quitMsg:
		d.closed = true
		return d, tea.Sequence(d.view.Dispose(), tea.Quit)
	case tea.WindowSizeMsg:
		return d, d.view.SetSize(v.Width, max(1, v.Height-2))
	case nt3dwidget.RenderFailedMsg:
		d.status = "GPU failed · 2 renders in software"
		return d, nil
	case nt3dwidget.PickMsg:
		if d.aabb != nil {
			// The tree picks along the click's ray; nothing met clears.
			if err := d.pickAABB(v.Ray); err != nil {
				d.status = err.Error()
			}
			return d, d.view.Refresh()
		}
		d.status = "no hit"
		if v.Hit != nil {
			d.status = fmt.Sprintf("%s #%d at %.2f %.2f %.2f", v.Hit.Name, v.Hit.Index, v.Hit.Position.X, v.Hit.Position.Y, v.Hit.Position.Z)
		}
		return d, nil
	case tea.KeyPressMsg:
		if d.water != nil {
			if handled, err := d.waterKey(v.String()); handled {
				if err != nil {
					d.status = err.Error()
				}
				return d, d.view.Refresh()
			}
		}
		var err error
		switch v.String() {
		case "q", "ctrl+c":
			d.closed = true
			if d.meshes != nil && d.meshes.palette.building {
				d.meshes.palette.cancel()
			}
			return d, tea.Sequence(d.view.Dispose(), tea.Quit)
		case "enter":
			if d.animatedSamples != nil {
				return d, d.restartAnimation()
			}
		case "a":
			d.animate = !d.animate
			d.lastPhase = time.Now()
		case "u", "y", "z":
			if !d.showsSplats() {
				return d, nil
			}
			d.adjustSplatBudget(v.String())
		case "w":
			if !d.showsSplats() || !d.meshes.hasSH {
				return d, nil
			}
			return d, d.togglePalette()
		case "h":
			if d.aabb != nil {
				err = d.cycleAABBShow()
				break
			}
			if d.meshes != nil && d.meshes.hasSH {
				d.meshes.dcOnly = !d.meshes.dcOnly
				err = d.rebuildMeshes()
				break
			}
			if d.lights != nil {
				err = d.rotateSHLight()
				break
			}
			return d, nil
		case "m":
			if d.mipmaps != nil {
				err = d.cycleFiltering()
				break
			}
			if d.fur != nil {
				d.fur.fullSize = !d.fur.fullSize
				err = d.growFur()
				break
			}
			if d.dynamic == nil {
				return d, nil
			}
			d.dynamic.deform = !d.dynamic.deform
		case "e":
			if d.lights != nil {
				err = d.toggleLightShine()
				break
			}
			if d.meshes != nil && d.models == nil {
				err = d.toggleShine()
				break
			}
			if d.fur == nil {
				return d, nil
			}
			return d, d.cycleEffect()
		case "d":
			d.fog = !d.fog
			return d, d.view.Refresh()
		case "b", "x":
			if d.aabb != nil {
				if v.String() == "b" {
					_, err = d.addAABBBox()
				} else {
					err = d.removePickedAABB()
				}
				break
			}
			if d.lights != nil && v.String() == "b" {
				d.lights.lift = !d.lights.lift
				err = d.buildLightShapes()
				break
			}
			if v.String() == "b" && d.animatedSamples != nil {
				err = d.toggleAnimatedOutlines()
				break
			}
			if v.String() == "b" && d.meshes != nil {
				err = d.toggleOutlines()
				break
			}
			if v.String() == "b" && d.particles != nil {
				err = d.cycleParticleBlend()
				break
			}
			if v.String() == "x" && (d.fur != nil || d.mipmaps != nil) {
				return d, d.cycleSupersample()
			}
			if d.dynamic == nil {
				return d, nil
			}
			if v.String() == "b" {
				err = d.addBody()
			} else {
				err = d.removeBody()
			}
			if err == nil {
				err = d.stepDynamic()
			}
		case "p":
			if !d.terrainScene() {
				return d, nil
			}
			d.procedural = !d.procedural
			err = d.loadTextures()
			if err == nil {
				err = d.scene.SetTexture("terrain", d.textures[d.style])
			}
		case "t":
			if d.particles != nil {
				err = d.toggleParticleSprites()
				break
			}
			if d.mipmaps != nil {
				err = d.cyclePattern()
				break
			}
			if d.meshes != nil && d.models == nil {
				err = d.toggleGlass()
				break
			}
			if !d.terrainScene() {
				return d, nil
			}
			d.style = 1 - d.style
			if d.procedural {
				err = d.updatePlasma()
			} else {
				err = d.scene.SetTexture("terrain", d.textures[d.style])
			}
		case "f":
			if d.lights != nil {
				err = d.toggleFalloff()
				break
			}
			if d.fur != nil {
				d.fur.noFins = !d.fur.noFins
				err = d.growFur()
				break
			}
			if !d.terrainScene() {
				return d, nil
			}
			d.flat = !d.flat
			err = d.terrain()
		case "l":
			if d.lights != nil {
				err = d.cycleLightPreset()
				break
			}
			if d.cloud != nil {
				err = d.toggleCloudLOD()
				break
			}
			if d.fur != nil {
				d.fur.length = (d.fur.length + 1) % len(furLengths)
				err = d.growFur()
				break
			}
			if !d.terrainScene() {
				return d, nil
			}
			d.lit = !d.lit
			err = d.terrain()
		case "s":
			switch {
			case d.fur != nil:
				d.fur.shells = (d.fur.shells + 1) % len(furShells)
				err = d.growFur()
			case d.meshes != nil:
				err = d.cycleNormals()
			case d.particles != nil:
				err = d.toggleParticleShape()
			case d.lights != nil || d.forest != nil:
				err = d.cycleShadows()
			default:
				return d, nil
			}
		case "[", "]":
			if d.animatedSamples != nil {
				step := 1
				if v.String() == "[" {
					step = -1
				}
				return d, d.stepAnimatedSample(step)
			}
			if d.models == nil {
				return d, nil
			}
			step := 1
			if v.String() == "[" {
				step = -1
			}
			return d, d.stepSample(step)
		case ",", ".":
			if d.animatedSamples == nil {
				return d, nil
			}
			step := 1
			if v.String() == "," {
				step = -1
			}
			return d, d.stepAnimationClip(step)
		case "i":
			if d.forest != nil {
				err = d.toggleInstancing()
				break
			}
			if d.large == nil {
				return d, nil
			}
			d.toggleIndexing()
		case "n":
			if d.aabb != nil {
				err = d.cycleAABBCount()
				break
			}
			if d.lights != nil {
				err = d.toggleNormalMap()
				break
			}
			if d.cloud != nil {
				err = d.cycleCloudCount()
				break
			}
			if d.particles != nil {
				err = d.cycleParticleCount()
				break
			}
			if d.forest != nil {
				err = d.cycleForest()
				break
			}
			d.angle += math.Pi / 12
			err = d.scene.SetTransform(d.group, nt3dmath.RotationZ(d.angle))
		case "k":
			if d.animatedSamples != nil && len(d.animatedSamples.cameras) > 0 {
				return d, d.cycleSampleCamera()
			}
			_, cmd := d.view.Update(msg)
			return d, cmd
		case "v":
			d.hidden = !d.hidden
			err = d.scene.SetVisible(d.group, !d.hidden)
		case "c":
			if d.meshes != nil && d.meshes.capture {
				return d, d.fitMeshes()
			}
			_, cmd := d.view.Update(msg)
			return d, cmd
		case "-", "_", "=", "+":
			if d.cloud == nil {
				_, cmd := d.view.Update(msg)
				return d, cmd
			}
			d.zoomCloud(v.String() == "=" || v.String() == "+")
		case "1":
			return d, d.view.SetRenderMode(nt3d.WebGPU)
		case "2":
			return d, d.view.SetRenderMode(nt3d.Software)
		case "3":
			return d, d.view.SetRenderMode(nt3d.Wireframe)
		default:
			_, cmd := d.view.Update(msg)
			return d, cmd
		}
		if err != nil {
			d.status = err.Error()
		}
		return d, d.view.Refresh()
	}
	_, cmd := d.view.Update(msg)
	return d, cmd
}

// rendering is the backend and how fast it draws: frames a second, and the
// last frame's render time. The numbers keep their width, so what follows
// them on the status line stays put as they change.
func (d *demo) rendering() string {
	ms := min(999.9, float64(d.view.RenderTime().Microseconds())/1000)
	s := fmt.Sprintf("%s · %3.0f fps · %5.1f ms", d.view.RenderMode(), min(999, d.view.FrameRate()), ms)
	if d.kittySession != nil && d.view.PictureMode() == ntpicture.PictureKitty {
		switch d.kittySession.Snapshot().FrameEdits {
		case ntpicture.KittyCapabilitySupported:
			s += " · frame edits"
		case ntpicture.KittyCapabilityUnsupported:
			s += " · full uploads"
		default:
			s += " · probing frame edits"
		}
	}
	if d.fog {
		s += " · fog"
	}
	return s
}

// decorate lights frames in the lights demo, and fogs them, when fog is on,
// from just behind the camera's target to well past it, so the far side of
// the scene fades whatever the zoom.
func (d *demo) decorate(f *nt3d.Frame) {
	if d.forest != nil && d.forest.shadows == shadowMap && len(f.Lighting.Lights) > 0 {
		f.Lighting.Lights = slices.Clone(f.Lighting.Lights)
		f.Lighting.Lights[0].Shadows = true
	}
	if d.lights != nil {
		d.decorateLights(f)
	}
	if d.particles != nil {
		f.Background = color.RGBA{16, 14, 20, 255} // Night, so the fire glows.
	}
	if d.cloud != nil {
		d.captureCloudView(f)
	}
	if d.mipmaps != nil && d.mipmaps.mode == mipOnly {
		f.Anisotropy = 1
	}
	if d.showsSplats() {
		d.splats.drawn, d.splats.resident = f.SplatSelection()
	}
	if d.fog {
		r := float32(d.view.Camera().Distance)
		f.Fog = nt3d.Fog{Near: r * .9, Far: r * 1.8}
	}
}

func (d *demo) View() tea.View {
	v := d.view.View()
	status := d.status
	if d.view.Err() != nil {
		status = d.view.Err().Error()
	}
	title := "nt3d terrain"
	if d.procedural {
		title = "nt3d procedural plasma"
		if d.animate {
			status += " · animating"
		} else {
			status += " · paused"
		}
	}
	if d.fur != nil {
		v.Content = d.zones.Scan(d.furView(v.Content))
		v.AltScreen = true
		return v
	}
	if d.mipmaps != nil {
		v.Content = d.zones.Scan(d.mipmapsView(v.Content))
		v.AltScreen = true
		return v
	}
	if d.forest != nil {
		v.Content = d.zones.Scan(d.forestView(v.Content))
		v.AltScreen = true
		return v
	}
	if d.lights != nil {
		v.Content = d.zones.Scan(d.lightsView(v.Content))
		v.AltScreen = true
		return v
	}
	if d.particles != nil {
		v.Content = d.zones.Scan(d.particlesView(v.Content))
		v.AltScreen = true
		return v
	}
	if d.cloud != nil {
		v.Content = d.zones.Scan(d.cloudView(v.Content))
		v.AltScreen = true
		return v
	}
	if d.water != nil {
		v.Content = d.zones.Scan(d.waterView(v.Content))
		v.AltScreen = true
		return v
	}
	if d.aabb != nil {
		v.Content = d.zones.Scan(d.aabbView(v.Content))
		v.AltScreen = true
		return v
	}
	if d.large != nil {
		v.Content = d.zones.Scan(d.largeView(v.Content))
		v.AltScreen = true
		return v
	}
	if d.models != nil {
		v.Content = d.zones.Scan(d.modelsView(v.Content))
		v.AltScreen = true
		return v
	}
	if d.meshes != nil {
		v.Content = d.zones.Scan(d.meshesView(v.Content))
		v.AltScreen = true
		return v
	}
	if d.animatedSamples != nil {
		v.Content = d.zones.Scan(d.animateView(v.Content))
		return v
	}
	if d.animation != nil {
		state := "paused"
		if d.animate {
			state = "playing"
		}
		v.Content = d.zones.Scan("nt3d glTF animation · a pause · 1/2/3 backend · q quit\n" + v.Content + "\n" + d.rendering() + " · " + state + " · drag orbit · wheel zoom")
		return v
	}
	if d.dynamic != nil {
		state := "paused"
		if d.animate {
			state = "running"
		}
		mesh := "held"
		if d.dynamic.deform {
			mesh = "deforming"
		}
		v.Content = d.zones.Scan("nt3d dynamic scene · a pause · m deform · b add · x remove · v visibility · 1/2/3 backend · q quit\n" + v.Content + fmt.Sprintf("\n%s · %s · mesh %s · %d orbiters · drag orbit · wheel zoom · r camera · %s", d.rendering(), state, mesh, len(d.dynamic.bodies), status))
		v.AltScreen = true
		return v
	}
	v.Content = d.zones.Scan(title + " · a animate · p procedural · t texture · f flat · l lighting · n parent rotation · v visibility · 1/2/3 backend · q quit\n" + v.Content + "\n" + d.rendering() + " · drag orbit · shift pan · wheel zoom · r rotate · o projection · g glyph · " + status)
	v.AltScreen = true
	return v
}
func mapImage(topo bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 384, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 384; x++ {
			c := color.RGBA{208, 224, 175, 255}
			if x%36 < 3 || y%36 < 3 {
				c = color.RGBA{250, 243, 216, 255}
			}
			if topo {
				z := math.Sin(float64(x)*.025) * math.Cos(float64(y)*.03)
				c = color.RGBA{uint8(155 + 30*z), uint8(194 + 30*z), uint8(125 + 25*z), 255}
				if math.Mod(z+1, .12) < .016 {
					c = color.RGBA{85, 123, 70, 255}
				}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}
func run(proceduralDefault bool, kind sceneMode) error {
	procedural := flag.Bool("procedural", proceduralDefault, "generate plasma textures with a GPU shader (CPU image fallback)")
	animate := flag.Bool("animate", kind != sceneAABB, "animate the scene or procedural phase (a toggles pause); the AABB demo starts still")
	mode := flag.String("render-mode", "gpu", "gpu, software, or wireframe")
	shDC := flag.Bool("sh-dc", false, "show only DC Gaussian color (h toggles full SH)")
	snapshot := flag.String("snapshot", "", "write a PNG without opening a terminal")
	benchmark := flag.String("kitty-benchmark", "", "write a paired frame-edit benchmark JSON without opening a terminal (water or animation)")
	benchmarkFrames := flag.Int("benchmark-frames", 120, "measured frames per Kitty benchmark, after 10 warmup frames")
	benchmarkScale := flag.Float64("benchmark-camera-scale", 1, "camera distance multiplier for Kitty benchmark")
	duration := flag.Duration("duration", 0, "exit after a duration")
	medium := flag.String("medium", "auto", "auto, direct, or shm")
	frameEdits := flag.String("kitty-frame-edits", "auto", "auto, on, or off; on forces Kitty partial-frame updates (requires terminal support)")
	up := flag.String("up", "z", "mesh files' up axis: z, y, or -y for Y-down captures")
	supersample := flag.Int("supersample", 0, "GPU supersampling, 1..4 (x cycles it in the fur demo)")
	effectName := flag.String("effect", "none", "fur demo post effect: none, soft focus, or vignette")
	flag.Parse()
	frameCapability, err := parseKittyFrameEdits(*frameEdits)
	if err != nil {
		return err
	}
	upAxis := nt3dmesh.ZUp
	switch *up {
	case "z":
	case "y", "-y":
		upAxis = nt3dmesh.YUp
	default:
		return fmt.Errorf("invalid up axis")
	}
	renderMode := nt3d.WebGPU
	switch *mode {
	case "gpu":
	case "software":
		renderMode = nt3d.Software
	case "wireframe":
		renderMode = nt3d.Wireframe
	default:
		return fmt.Errorf("invalid render mode")
	}
	transport := ntpicture.KittyMediumDirect
	switch *medium {
	case "auto":
		if runtime.GOOS == "js" {
			transport = ntpicture.KittyMediumSharedMemory
		}
	case "direct":
	case "shm":
		transport = ntpicture.KittyMediumSharedMemory
	default:
		return fmt.Errorf("invalid medium")
	}
	// On the GPU, take the device's binding limit before anything loads, so a
	// big capture needs fewer pieces where the device allows it. The widget
	// owns the renderer it is given and falls back as it would with its own.
	// In the browser the device holds the 128 MiB baseline whatever is asked,
	// so nothing is adopted there.
	var renderer nt3d.Renderer
	if renderMode == nt3d.WebGPU && runtime.GOOS != "js" {
		r := nt3d.NewGPURenderer()
		if _, err := nt3d.AdoptBindingLimit(r); err != nil {
			_ = r.Close()
		} else {
			renderer = r
		}
	}
	d := &demo{scene: nt3d.NewScene(), zones: zone.New(), duration: *duration, animate: *animate}
	defer d.zones.Close()
	d.group, err = d.scene.Add(nt3d.Root, "island")
	if err != nil {
		return err
	}
	switch kind {
	case sceneCharacters:
		if err = d.setupCharacters(); err != nil {
			return err
		}
	case sceneAnimate:
		if err = d.setupAnimate(); err != nil {
			return err
		}
	case sceneAnimation:
		if err = d.setupAnimation(); err != nil {
			return err
		}
	case sceneDynamic:
		if err = d.setupDynamic(); err != nil {
			return err
		}
	case sceneMeshes:
		if err = d.setupMeshes(flag.Args(), upAxis); err != nil {
			return err
		}
	case sceneModels:
		if err = d.setupModels(); err != nil {
			return err
		}
	case sceneLarge:
		if err = d.setupLarge(); err != nil {
			return err
		}
	case sceneFur:
		if err = d.setupFur(); err != nil {
			return err
		}
	case sceneMipmaps:
		if err = d.setupMipmaps(); err != nil {
			return err
		}
	case sceneForest:
		if err = d.setupForest(); err != nil {
			return err
		}
	case sceneLights:
		if err = d.setupLights(); err != nil {
			return err
		}
	case sceneGaussians:
		if err = d.setupGaussians(); err != nil {
			return err
		}
	case sceneParticles:
		if err = d.setupParticles(); err != nil {
			return err
		}
	case sceneCloud:
		if err = d.setupCloud(); err != nil {
			return err
		}
	case sceneWater:
		if err = d.setupWater(); err != nil {
			return err
		}
	case sceneAABB:
		if err = d.setupAABB(); err != nil {
			return err
		}
	default:
		d.procedural = *procedural
		if err = d.loadTextures(); err != nil {
			return err
		}

		if err = d.terrain(); err != nil {
			return err
		}
		// A child marker makes parent transformations and inherited visibility visible.
		marker, err := d.scene.SetGeometry("marker", nt3d.Geometry{Boxes: []nt3d.Box{{Min: nt3dmath.Vec3{X: .65, Y: .25, Z: .2}, Size: nt3dmath.Vec3{X: .12, Y: .12, Z: .35}, Color: color.RGBA{200, 72, 48, 255}}}})
		if err != nil {
			return err
		}
		if err = d.scene.Reparent(marker, d.group); err != nil {
			return err
		}
	}
	if d.meshes != nil && *shDC {
		d.meshes.dcOnly = true
		if err = d.rebuildMeshes(); err != nil {
			return err
		}
	}
	if kind == sceneMeshes && *up == "-y" {
		// Rotate the Y-up result by 180 degrees about X for Y-down captures.
		if err = d.scene.SetTransform(d.group, nt3dmath.Scaling(nt3dmath.Vec3{X: 1, Y: -1, Z: -1})); err != nil {
			return err
		}
	}
	camera := nt3d.DefaultCamera()
	camera.Distance = 4.3
	switch kind {
	case sceneDynamic:
		camera.Distance = 5.5
	case sceneMipmaps:
		// Low over the floor, so it runs to the horizon.
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
	d.kittySession = ntpicture.NewTerminalSession(ntpicture.TerminalOptions{FrameEdits: frameCapability})
	defer d.kittySession.Close()
	options := []nt3dwidget.Option{nt3dwidget.WithTerminalSession(d.kittySession), nt3dwidget.WithCamera(camera), nt3dwidget.WithRenderMode(renderMode), nt3dwidget.WithKittyMedium(transport), nt3dwidget.WithZoneManager(d.zones), nt3dwidget.WithFrameDecorator(d.decorate)}
	if renderer != nil {
		options = append(options, nt3dwidget.WithRenderer(renderer))
	}
	d.view = nt3dwidget.New(d.scene, 80, 22, options...)
	defer d.view.Close()
	switch kind {
	case sceneMeshes:
		d.fitMeshes()
	case sceneLarge, sceneAnimation, sceneGaussians:
		d.view.Fit()
		if kind == sceneAnimation {
			camera := d.view.Camera()
			camera.Distance *= 1.4
			d.view.SetCamera(camera)
		}
	case sceneFur:
		camera := d.view.Camera()
		camera.Beta = 30
		d.view.SetCamera(camera)
		d.view.Fit()
		d.view.SetSupersample(2) // Fur's thin strands are what supersampling smooths best.
		for i, e := range effects {
			if e.name == *effectName {
				d.fur.effect = i
				d.view.SetPostProcess(e.apply)
			}
		}
	case sceneAnimate, sceneCharacters:
		d.frameAnimatedSample()
	case sceneModels:
		d.frameSample()
	}
	if *supersample != 0 {
		d.view.SetSupersample(*supersample)
	}
	if *benchmark != "" {
		return d.benchmarkKitty(*benchmark, *benchmarkFrames, *benchmarkScale)
	}
	if *snapshot != "" {
		img, used, err := d.view.Snapshot(960, 640)
		if err != nil {
			return err
		}
		file, err := os.Create(*snapshot)
		if err != nil {
			return err
		}
		err = png.Encode(file, img)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		fmt.Printf("rendered %s with %s\n", *snapshot, used)
		return closeErr
	}
	if runtime.GOOS == "js" {
		// In the browser, the page shows the scene's keys as touch controls.
		model, err := d.withActions()
		if err != nil {
			return err
		}
		return booba.Run(model, tea.WithoutSignalHandler())
	}
	return booba.Run(d)
}

// Main runs the terrain scene, optionally starting with procedural textures.
func Main(proceduralDefault bool) { mainDemo(proceduralDefault, sceneTerrain) }

func mainDemo(proceduralDefault bool, mode sceneMode) {
	if err := run(proceduralDefault, mode); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	holdRuntime()
}

func (d *demo) loadTextures() error {
	for i := range d.textures {
		var err error
		if d.procedural {
			d.textures[i], err = plasmaTexture(d.phase + float64(i)*2)
		} else {
			d.textures[i], err = nt3d.NewTexture(mapImage(i == 1))
		}
		if err != nil {
			return err
		}
	}
	if d.procedural {
		d.status = "procedural plasma · a pause · t shifts phase"
	} else {
		d.status = "image texture"
	}
	return nil
}
