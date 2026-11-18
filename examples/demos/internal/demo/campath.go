// Copyright (c) 2026 Neomantra Corp.

package demo

import (
	"fmt"
	"image/color"
	"math"
	"time"

	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/nt3d"
	nt3dcam "nimbleterminal.dev/nt3d/cam"
	nt3dcamscene "nimbleterminal.dev/nt3d/cam/scene"
	nt3dcamspec "nimbleterminal.dev/nt3d/cam/spec"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// CamPathMain showcases declarative YAML camera paths (flythrough, closed orbit
// with focal-length pull, and Nebula-style camera switching) with 3D frustum
// and spline debug overlays.
func CamPathMain() { mainDemo(false, sceneCamPath) }

const flythroughSpecYAML = `
camera_path:
  version: 1
  name: canyon-flythrough
  frame: scene
  duration: 14s
  loop: loop
  position:
    interp: catmull-rom-centripetal
    points:
      - [-13.8, -8.8, 3.8]
      - [-9.0, -5.5, 3.0]
      - [-3.5, -2.2, 2.3]
      - [2.5, 2.4, 2.4]
      - [8.0, 5.2, 3.1]
      - [11.5, -1.5, 4.4]
      - [4.5, -8.5, 4.8]
      - [-6.5, -11.2, 4.8]
  timing:
    - {to: 0.28, over: 4s, ease: ease-in-out}
    - {to: 0.76, over: 6s, ease: linear}
    - {to: 1.00, over: 4s, ease: ease-in-out}
  look:
    - {mode: target, target: "node:beacon", from: 0s}
    - {mode: tangent, from: 3.2s, blend: 1.8s, blend_ease: ease-in-out}
    - {mode: target, target: "node:sculpture", from: 9.2s, blend: 1.8s, blend_ease: smoothstep}
    - {mode: target, target: "node:beacon", from: 12.2s, blend: 1.5s}
  roll:
    - {t: 0s, v: 0}
    - {t: 4.8s, v: -12, ease: ease-in-out}
    - {t: 7.6s, v: 14, ease: ease-in-out}
    - {t: 10.5s, v: 0, ease: ease-out}
    - {t: 14s, v: 0}
  lens:
    fov:
      - {t: 0s, v: 56}
      - {t: 6s, v: 48, ease: ease-in-out}
      - {t: 10.5s, v: 36, ease: smoothstep}
      - {t: 14s, v: 56, ease: ease-in-out}
    focus_dist: [{t: 0s, v: 9.0}, {t: 10.5s, v: 7.5, ease: smoothstep}, {t: 14s, v: 9.0}]
    aperture: [{t: 0s, v: 2.8}]
    near: 0.1
    far: 400
  markers:
    - {name: launch, t: 0s}
    - {name: west-arch, t: 3.8s}
    - {name: east-arch, t: 7.2s}
    - {name: sculpture-lock, t: 9.8s}
`

const orbitSpecYAML = `
camera_path:
  version: 1
  name: beacon-orbit
  frame: scene
  duration: 10s
  loop: loop
  position:
    interp: catmull-rom-centripetal
    closed: true
    points:
      - [0.0, -7.2, 3.0]
      - [7.2, 0.0, 4.4]
      - [0.0, 7.2, 3.2]
      - [-7.2, 0.0, 2.2]
  look:
    - {mode: target, target: "node:beacon", from: 0s}
  roll:
    - {t: 0s, v: 0}
    - {t: 2.5s, v: 4, ease: smoothstep}
    - {t: 7.5s, v: -4, ease: smoothstep}
    - {t: 10s, v: 0, ease: smoothstep}
  lens:
    focal_length:
      - {t: 0s, v: 24}
      - {t: 5s, v: 50, ease: ease-in-out}
      - {t: 10s, v: 24, ease: ease-in-out}
    sensor_height: 24
    focus_dist: [{t: 0s, v: 7.4}]
    aperture: [{t: 0s, v: 2.0}]
    near: 0.1
    far: 400
  markers:
    - {name: wide-24mm, t: 0s}
    - {name: tele-50mm, t: 5s}
`

type camPathPreset int

const (
	camPresetFlythrough camPathPreset = iota
	camPresetOrbit
	camPresetSwitcher
)

var camPresetNames = [...]string{"flythrough", "orbit", "switcher"}

type camPathScene struct {
	flythrough  *nt3dcam.Path
	orbit       *nt3dcam.Path
	resolver    nt3dcam.Resolver
	preset      camPathPreset
	riding      bool
	overlay     bool
	orbitCam    nt3d.Camera
	playhead    time.Duration
	beaconRing  nt3d.NodeID
	sculptureID nt3d.NodeID
}

func camPathDirectorCamera(c *nt3d.Camera) {
	c.Elevation, c.Azimuth, c.Distance, c.Projection = 28, -52, 26, nt3d.Perspective
	c.Target = nt3dmath.Vec3{Z: 2.2}
}

func (d *demo) setupCamPath() error {
	cp := &camPathScene{
		riding:  true,
		overlay: true,
	}
	camPathDirectorCamera(&cp.orbitCam)

	// Terraced plaza floor & water basin.
	waterMat := &nt3d.Material{Roughness: 0.14, Metallic: 0.05, Clearcoat: 0.9, ClearcoatRoughness: 0.08}
	waterDisk, err := nt3d.BuildCylinder(nt3d.Cylinder{
		BottomRadius: 20,
		TopRadius:    20,
		Height:       0.2,
		Segments:     64,
		Color:        color.RGBA{R: 34, G: 92, B: 112, A: 255},
		Material:     waterMat,
	})
	if err != nil {
		return err
	}
	waterID, err := d.scene.SetGeometry("basin", waterDisk)
	if err != nil {
		return err
	}
	if err := d.scene.Reparent(waterID, d.group); err != nil {
		return err
	}
	if err := d.scene.SetTransform(waterID, nt3dmath.Translation(nt3dmath.Vec3{Z: -0.12})); err != nil {
		return err
	}

	stoneMat := &nt3d.Material{Roughness: 0.78}
	plaza, err := nt3d.BuildCylinder(nt3d.Cylinder{
		BottomRadius: 14.5,
		TopRadius:    14.0,
		Height:       0.36,
		Segments:     48,
		Color:        color.RGBA{R: 214, G: 204, B: 188, A: 255},
		Material:     stoneMat,
	})
	if err != nil {
		return err
	}
	plazaID, err := d.scene.SetGeometry("plaza", plaza)
	if err != nil {
		return err
	}
	if err := d.scene.Reparent(plazaID, d.group); err != nil {
		return err
	}
	if err := d.scene.SetTransform(plazaID, nt3dmath.Translation(nt3dmath.Vec3{Z: 0.16})); err != nil {
		return err
	}

	// Central "beacon" spire + animated golden astrolabe ring (resolved as "node:beacon").
	beaconID, err := d.scene.Add(d.group, "beacon")
	if err != nil {
		return err
	}
	if err := d.scene.SetTransform(beaconID, nt3dmath.Translation(nt3dmath.Vec3{Z: 2.8})); err != nil {
		return err
	}
	spirePillar, err := nt3d.BuildCylinder(nt3d.Cylinder{
		BottomRadius: 0.75,
		TopRadius:    0.32,
		Height:       2.6,
		Segments:     24,
		Color:        color.RGBA{R: 72, G: 78, B: 92, A: 255},
		Material:     &nt3d.Material{Roughness: 0.4, Metallic: 0.3},
	})
	if err != nil {
		return err
	}
	pillarID, err := d.scene.SetGeometry("beacon-pillar", spirePillar)
	if err != nil {
		return err
	}
	if err := d.scene.Reparent(pillarID, d.group); err != nil {
		return err
	}
	if err := d.scene.SetTransform(pillarID, nt3dmath.Translation(nt3dmath.Vec3{Z: 1.5})); err != nil {
		return err
	}

	goldGlow := &nt3d.Material{
		Roughness:         0.22,
		Metallic:          0.85,
		Emissive:          color.RGBA{R: 255, G: 185, B: 65, A: 255},
		EmissiveIntensity: 0.45,
	}
	orb, err := nt3d.BuildSphere(nt3d.Sphere{
		Radius:   0.48,
		Segments: 28,
		Rings:    16,
		Color:    color.RGBA{R: 255, G: 205, B: 85, A: 255},
		Material: goldGlow,
	})
	if err != nil {
		return err
	}
	if err := d.scene.SetNodeGeometry(beaconID, orb); err != nil {
		return err
	}

	ringMesh, err := nt3d.BuildTorus(nt3d.Torus{
		MajorRadius: 1.05,
		MinorRadius: 0.09,
		Segments:    40,
		Sides:       14,
		Color:       color.RGBA{R: 245, G: 190, B: 75, A: 255},
		Material:    goldGlow,
	})
	if err != nil {
		return err
	}
	cp.beaconRing, err = d.scene.SetGeometry("beacon-ring", ringMesh)
	if err != nil {
		return err
	}
	if err := d.scene.Reparent(cp.beaconRing, beaconID); err != nil {
		return err
	}

	// Off-axis clearcoated cobalt sculpture ("node:sculpture") for target handoff.
	coatMat := &nt3d.Material{
		Roughness:          0.28,
		Metallic:           0.2,
		Clearcoat:          1.0,
		ClearcoatRoughness: 0.06,
	}
	knotTorus, err := nt3d.BuildTorus(nt3d.Torus{
		MajorRadius: 0.85,
		MinorRadius: 0.26,
		Segments:    40,
		Sides:       18,
		Color:       color.RGBA{R: 52, G: 122, B: 226, A: 255},
		Material:    coatMat,
	})
	if err != nil {
		return err
	}
	cp.sculptureID, err = d.scene.SetGeometry("sculpture", knotTorus)
	if err != nil {
		return err
	}
	if err := d.scene.Reparent(cp.sculptureID, d.group); err != nil {
		return err
	}
	if err := d.scene.SetTransform(cp.sculptureID, nt3dmath.Translation(nt3dmath.Vec3{X: 7.5, Y: -4.2, Z: 1.7}).Mul(nt3dmath.RotationX(0.9))); err != nil {
		return err
	}
	pedestal, err := nt3d.BuildCylinder(nt3d.Cylinder{
		BottomRadius: 0.9,
		TopRadius:    0.75,
		Height:       0.7,
		Segments:     24,
		Color:        color.RGBA{R: 190, G: 182, B: 170, A: 255},
		Material:     stoneMat,
	})
	if err != nil {
		return err
	}
	pedID, err := d.scene.SetGeometry("sculpture-pedestal", pedestal)
	if err != nil {
		return err
	}
	if err := d.scene.Reparent(pedID, d.group); err != nil {
		return err
	}
	if err := d.scene.SetTransform(pedID, nt3dmath.Translation(nt3dmath.Vec3{X: 7.5, Y: -4.2, Z: 0.55})); err != nil {
		return err
	}

	// Three terracotta archways framing the S-curve flythrough corridor.
	arches := []struct {
		name       string
		x, y, yaw  float64
		span, tall float32
		col        color.RGBA
	}{
		{name: "arch-west", x: -6.2, y: -3.8, yaw: 0.58, span: 4.4, tall: 4.5, col: color.RGBA{R: 214, G: 122, B: 92, A: 255}},
		{name: "arch-mid", x: -0.5, y: 0.1, yaw: 0.66, span: 4.6, tall: 4.7, col: color.RGBA{R: 224, G: 148, B: 98, A: 255}},
		{name: "arch-east", x: 5.4, y: 3.9, yaw: 0.52, span: 4.4, tall: 4.5, col: color.RGBA{R: 196, G: 112, B: 96, A: 255}},
	}
	archMat := &nt3d.Material{Roughness: 0.65}
	for _, a := range arches {
		rootID, err := d.scene.Add(d.group, a.name)
		if err != nil {
			return err
		}
		pose := nt3dmath.Translation(nt3dmath.Vec3{X: float32(a.x), Y: float32(a.y), Z: 0.34}).Mul(nt3dmath.RotationZ(a.yaw))
		if err := d.scene.SetTransform(rootID, pose); err != nil {
			return err
		}
		colGeom, err := nt3d.BuildCuboid(nt3d.Cuboid{
			Size:     nt3dmath.Vec3{X: 0.55, Y: 0.55, Z: a.tall},
			Color:    a.col,
			Material: archMat,
		})
		if err != nil {
			return err
		}
		for side, sx := range []float32{-a.span * 0.5, a.span * 0.5} {
			cid, err := d.scene.SetGeometry(fmt.Sprintf("%s-col-%d", a.name, side), colGeom)
			if err != nil {
				return err
			}
			if err := d.scene.Reparent(cid, rootID); err != nil {
				return err
			}
			if err := d.scene.SetTransform(cid, nt3dmath.Translation(nt3dmath.Vec3{Y: sx, Z: a.tall * 0.5})); err != nil {
				return err
			}
		}
		lintelGeom, err := nt3d.BuildCuboid(nt3d.Cuboid{
			Size:     nt3dmath.Vec3{X: 0.72, Y: a.span + 1.0, Z: 0.58},
			Color:    a.col,
			Material: archMat,
		})
		if err != nil {
			return err
		}
		lid, err := d.scene.SetGeometry(a.name+"-lintel", lintelGeom)
		if err != nil {
			return err
		}
		if err := d.scene.Reparent(lid, rootID); err != nil {
			return err
		}
		if err := d.scene.SetTransform(lid, nt3dmath.Translation(nt3dmath.Vec3{Z: a.tall + 0.25})); err != nil {
			return err
		}
	}

	paths := make(map[string]*nt3dcam.Path, 2)
	cp.resolver = nt3dcamscene.SceneResolver(d.scene, paths)

	cp.flythrough, err = nt3dcamspec.Compile([]byte(flythroughSpecYAML), nt3dcam.WithResolver(cp.resolver))
	if err != nil {
		return err
	}
	cp.orbit, err = nt3dcamspec.Compile([]byte(orbitSpecYAML), nt3dcam.WithResolver(cp.resolver))
	if err != nil {
		return err
	}
	paths["flythrough"] = cp.flythrough
	paths["orbit"] = cp.orbit

	d.camPath = cp
	return d.stepCamPathNodes(0)
}

func (d *demo) evalCamPathState(t time.Duration) nt3dcam.CameraState {
	cp := d.camPath
	switch cp.preset {
	case camPresetOrbit:
		return cp.orbit.EvalWith(t, cp.resolver)
	case camPresetSwitcher:
		// 14s switcher cycle: flythrough [0..4.5s] -> 1.5s smooth crossfade to orbit [6..10.5s] -> 1.5s crossfade back!
		sec := math.Mod(t.Seconds(), 14.0)
		if sec < 0 {
			sec += 14.0
		}
		sel := 0.0
		switch {
		case sec < 4.5:
			sel = 0
		case sec < 6.0:
			sel = (sec - 4.5) / 1.5
		case sec < 10.5:
			sel = 1
		case sec < 12.0:
			sel = 1.0 - (sec-10.5)/1.5
		default:
			sel = 0
		}
		states := []nt3dcam.CameraState{
			cp.flythrough.EvalWith(t, cp.resolver),
			cp.orbit.EvalWith(t, cp.resolver),
		}
		return nt3dcam.Switch(states, sel, nt3dcam.EaseSpec{Kind: nt3dcam.EaseSmoothstep})
	default:
		return cp.flythrough.EvalWith(t, cp.resolver)
	}
}

func (d *demo) activeCamPath() *nt3dcam.Path {
	if d.camPath.preset == camPresetOrbit {
		return d.camPath.orbit
	}
	return d.camPath.flythrough
}

func (d *demo) stepCamPathNodes(sec float64) error {
	cp := d.camPath
	if cp == nil {
		return nil
	}
	if cp.beaconRing != 0 {
		rot := nt3dmath.RotationZ(sec * 0.9).Mul(nt3dmath.RotationX(0.65 + 0.25*math.Sin(sec*0.7)))
		if err := d.scene.SetTransform(cp.beaconRing, rot); err != nil {
			return err
		}
	}
	if cp.sculptureID != 0 {
		pose := nt3dmath.Translation(nt3dmath.Vec3{X: 7.5, Y: -4.2, Z: 1.7 + float32(0.12*math.Sin(sec*1.3))}).
			Mul(nt3dmath.RotationZ(sec * 0.55)).
			Mul(nt3dmath.RotationX(0.9))
		if err := d.scene.SetTransform(cp.sculptureID, pose); err != nil {
			return err
		}
	}
	return nil
}

func (d *demo) stepCamPath(elapsed float64) error {
	cp := d.camPath
	if cp == nil {
		return nil
	}
	cp.playhead += time.Duration(elapsed * float64(time.Second))
	return d.stepCamPathNodes(cp.playhead.Seconds())
}

func (d *demo) scrubCamPath(delta time.Duration) tea.Cmd {
	cp := d.camPath
	if cp == nil {
		return nil
	}
	dur := d.activeCamPath().Duration()
	cp.playhead += delta
	if cp.playhead < 0 {
		cp.playhead += dur
	}
	if err := d.stepCamPathNodes(cp.playhead.Seconds()); err != nil {
		d.status = err.Error()
	}
	return tea.Batch(d.followCamPath(), d.view.Refresh())
}

func (d *demo) cycleCamPathPreset(step int) tea.Cmd {
	cp := d.camPath
	if cp == nil {
		return nil
	}
	n := len(camPresetNames)
	cp.preset = camPathPreset((int(cp.preset) + step%n + n) % n)
	return tea.Batch(d.followCamPath(), d.view.Refresh())
}

func (d *demo) toggleCamPathView() tea.Cmd {
	cp := d.camPath
	if cp == nil || d.view == nil {
		return nil
	}
	if !cp.riding {
		cp.orbitCam = d.view.Camera()
		cp.riding = true
		return tea.Batch(d.followCamPath(), d.view.Refresh())
	}
	cp.riding = false
	return tea.Batch(d.view.SetCamera(cp.orbitCam), d.view.Refresh())
}

func (d *demo) detachCamPath() {
	cp := d.camPath
	if cp == nil || !cp.riding || d.view == nil {
		return
	}
	cp.riding = false
	d.view.SetCamera(cp.orbitCam)
}

func (d *demo) followCamPath() tea.Cmd {
	cp := d.camPath
	if cp == nil || !cp.riding || d.view == nil {
		return nil
	}
	st := d.evalCamPathState(cp.playhead)
	return d.view.SetCamera(nt3dcamscene.CameraFromState(st))
}

func (d *demo) decorateCamPath(f *nt3d.Frame) {
	f.Background = color.RGBA{R: 152, G: 192, B: 222, A: 255}
	f.Lighting = nt3d.Lighting{
		Ambient: nt3d.Ambient{
			Sky:       color.RGBA{R: 220, G: 235, B: 255, A: 255},
			Ground:    color.RGBA{R: 150, G: 140, B: 125, A: 255},
			Intensity: 0.55,
		},
		Lights: []nt3d.Light{
			{
				Kind:      nt3d.DirectionalLight,
				Direction: nt3dmath.Vec3{X: 0.62, Y: -0.48, Z: 0.62},
				Color:     color.RGBA{R: 255, G: 246, B: 228, A: 255},
				Intensity: 0.9,
				Shadows:   true,
			},
		},
	}

	cp := d.camPath
	if cp == nil || !cp.overlay {
		return
	}

	baseOpts := nt3dcamscene.CameraPathOverlayOptions{
		Resolver:   cp.resolver,
		LineWidth:  3.5,
		CurveColor: color.RGBA{R: 15, G: 115, B: 235, A: 255},
	}
	if !cp.riding {
		// In Director Orbit view, draw both paths (companion + full active frustum & look ray).
		if cp.preset == camPresetSwitcher {
			nt3dcamscene.AppendCameraPathOverlay(f, cp.flythrough, cp.playhead, baseOpts)
			orbitOpts := baseOpts
			orbitOpts.CurveColor = color.RGBA{R: 20, G: 185, B: 135, A: 255}
			orbitOpts.FrustumColor = color.RGBA{R: 245, G: 185, B: 30, A: 255}
			nt3dcamscene.AppendCameraPathOverlay(f, cp.orbit, cp.playhead, orbitOpts)
			return
		}
		nt3dcamscene.AppendCameraPathOverlay(f, d.activeCamPath(), cp.playhead, baseOpts)
		return
	}

	// While riding one path, draw the companion camera rig's curve & live frustum in the world
	// so the active view is unobstructed while still showing live 3D camera path visualization!
	companion := cp.orbit
	if cp.preset == camPresetOrbit {
		companion = cp.flythrough
	}
	nt3dcamscene.AppendCameraPathOverlay(f, companion, cp.playhead, baseOpts)
}

func (d *demo) activeMarkerName() string {
	p := d.activeCamPath()
	markers := p.Markers()
	if len(markers) == 0 || p.Duration() <= 0 {
		return "-"
	}
	tWrap := d.camPath.playhead % p.Duration()
	if tWrap < 0 {
		tWrap += p.Duration()
	}
	cur := markers[0].Name
	for _, m := range markers {
		if m.T <= tWrap {
			cur = m.Name
		}
	}
	return cur
}

func (d *demo) camPathView(content string) string {
	cp := d.camPath
	play := "playing"
	if !d.animate {
		play = "paused"
	}
	camMode := "director orbit"
	if cp.riding {
		camMode = "riding path"
	}
	ov := "overlay off"
	if cp.overlay {
		ov = "overlay on"
	}
	st := d.evalCamPathState(cp.playhead)
	fovDeg := st.FovY * 180 / math.Pi
	dur := d.activeCamPath().Duration()
	tWrap := time.Duration(0)
	if dur > 0 {
		tWrap = cp.playhead % dur
		if tWrap < 0 {
			tWrap += dur
		}
	}
	header := "nt3d camera paths · [/] preset · k path/orbit cam · ,/. scrub · b overlay · a pause · enter restart · 1/2/3 backend · q quit"
	footer := fmt.Sprintf("%s · %s · %s · %s · t=%.1fs/%.0fs · fov %.0f° · cue %s · %s",
		d.rendering(), camPresetNames[cp.preset], camMode, ov,
		tWrap.Seconds(), dur.Seconds(), fovDeg, d.activeMarkerName(), play)
	return header + "\n" + content + "\n" + footer
}
