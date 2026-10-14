// Copyright (c) 2026 Neomantra Corp.
package terraindemo

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
	"time"
)

type demo struct {
	view              *nt3dwidget.Model
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
}

type sceneMode int

const (
	sceneTerrain sceneMode = iota
	sceneDynamic
	sceneMeshes
	sceneModels
	sceneLarge
	sceneFur
)

// terrainScene reports whether the terrain controls apply.
func (d *demo) terrainScene() bool {
	return d.dynamic == nil && d.meshes == nil && d.large == nil && d.fur == nil
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
	return d.scene.Reparent(id, d.group)
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
	switch v := msg.(type) {
	case phaseMsg:
		if d.closed {
			return d, nil
		}
		now := time.Time(v)
		elapsed := now.Sub(d.lastPhase).Seconds()
		d.lastPhase = now
		if !d.animate || (!d.procedural && d.dynamic == nil && d.fur == nil) {
			return d, phaseTick()
		}
		d.phase = math.Mod(d.phase+elapsed, 2*math.Pi)
		// Let an in-flight frame finish; the next update catches up to wall time.
		if d.view.Busy() {
			return d, phaseTick()
		}
		var err error
		switch {
		case d.dynamic != nil:
			err = d.stepDynamic()
		case d.fur != nil:
			err = d.stepFur()
		default:
			err = d.updatePlasma()
		}
		if err != nil {
			d.status = err.Error()
			d.animate = false
			return d, phaseTick()
		}
		return d, tea.Batch(d.view.Refresh(), phaseTick())
	case quitMsg:
		d.closed = true
		return d, tea.Sequence(d.view.Dispose(), tea.Quit)
	case tea.WindowSizeMsg:
		return d, d.view.SetSize(v.Width, max(1, v.Height-2))
	case nt3dwidget.RenderFailedMsg:
		d.status = "GPU failed · 2 renders in software"
		return d, nil
	case nt3dwidget.PickMsg:
		d.status = "no hit"
		if v.Hit != nil {
			d.status = fmt.Sprintf("%s #%d at %.2f %.2f %.2f", v.Hit.Name, v.Hit.Index, v.Hit.Position.X, v.Hit.Position.Y, v.Hit.Position.Z)
		}
		return d, nil
	case tea.KeyPressMsg:
		var err error
		switch v.String() {
		case "q", "ctrl+c":
			d.closed = true
			return d, tea.Sequence(d.view.Dispose(), tea.Quit)
		case "a":
			d.animate = !d.animate
			d.lastPhase = time.Now()
		case "m":
			if d.dynamic == nil {
				return d, nil
			}
			d.dynamic.deform = !d.dynamic.deform
		case "b", "x":
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
			if !d.terrainScene() {
				return d, nil
			}
			d.flat = !d.flat
			err = d.terrain()
		case "l":
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
			default:
				return d, nil
			}
		case "[", "]":
			if d.models == nil {
				return d, nil
			}
			step := 1
			if v.String() == "[" {
				step = -1
			}
			return d, d.stepSample(step)
		case "i":
			if d.large == nil {
				return d, nil
			}
			d.toggleIndexing()
		case "n":
			d.angle += math.Pi / 12
			err = d.scene.SetTransform(d.group, nt3dmath.RotationZ(d.angle))
		case "v":
			d.hidden = !d.hidden
			err = d.scene.SetVisible(d.group, !d.hidden)
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
	if d.dynamic != nil {
		state := "paused"
		if d.animate {
			state = "running"
		}
		mesh := "held"
		if d.dynamic.deform {
			mesh = "deforming"
		}
		v.Content = d.zones.Scan("nt3d dynamic scene · a pause · m deform · b add · x remove · v visibility · 1/2/3 backend · q quit\n" + v.Content + fmt.Sprintf("\n%s · %s · mesh %s · %d orbiters · drag orbit · wheel zoom · r camera · %s", d.view.RenderMode(), state, mesh, len(d.dynamic.bodies), status))
		v.AltScreen = true
		return v
	}
	v.Content = d.zones.Scan(title + " · a animate · p procedural · t texture · f flat · l lighting · n parent rotation · v visibility · 1/2/3 backend · q quit\n" + v.Content + "\n" + d.view.RenderMode().String() + " · drag orbit · shift pan · wheel zoom · r rotate · o projection · g glyph · " + status)
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
	animate := flag.Bool("animate", true, "animate the scene or procedural phase (a toggles pause)")
	mode := flag.String("render-mode", "gpu", "gpu, software, or wireframe")
	snapshot := flag.String("snapshot", "", "write a PNG without opening a terminal")
	duration := flag.Duration("duration", 0, "exit after a duration")
	medium := flag.String("medium", "auto", "auto, direct, or shm")
	up := flag.String("up", "z", "mesh files' up axis: z, or y for most OBJ files")
	flag.Parse()
	upAxis := nt3dmesh.ZUp
	switch *up {
	case "z":
	case "y":
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
	d := &demo{scene: nt3d.NewScene(), zones: zone.New(), duration: *duration, animate: *animate}
	defer d.zones.Close()
	var err error
	d.group, err = d.scene.Add(nt3d.Root, "island")
	if err != nil {
		return err
	}
	switch kind {
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
	camera := nt3d.DefaultCamera()
	camera.Distance = 4.3
	switch kind {
	case sceneDynamic:
		camera.Distance = 5.5
	}
	camera.AutoRotate = proceduralDefault
	d.view = nt3dwidget.New(d.scene, 80, 22, nt3dwidget.WithCamera(camera), nt3dwidget.WithRenderMode(renderMode), nt3dwidget.WithKittyMedium(transport), nt3dwidget.WithZoneManager(d.zones))
	defer d.view.Close()
	switch kind {
	case sceneMeshes, sceneLarge, sceneFur:
		d.view.Fit()
	case sceneModels:
		d.frameSample()
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
		return booba.Run(d, tea.WithoutSignalHandler())
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
