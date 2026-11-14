// Copyright (c) 2026 Neomantra Corp.

package demo

import (
	"fmt"
	"image/color"
	"math"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
)

// LightsMain shows a few shapes on a floor under presets of ambient,
// directional, and point lights, the point lights orbiting.
func LightsMain() { mainDemo(true, sceneLights) }

type lightScene struct {
	shRotation int
	lift       bool
	normal     bool
	shadows    shadowMode
	preset     int  // Index into lightPresets.
	post       int  // Index into lightPostPresets.
	ranged     bool // Point lights fade to nothing at their range, rather than with distance alone.
	material   int  // 0 PBR, 1 PBR ORM+glow, 2 shiny Blinn-Phong, 3 dull Lambert.
	t, phase   float64
	lamps      [nt3d.MaxLights]nt3d.NodeID // A small glowing sphere at each point light.
	lampColor  [nt3d.MaxLights]color.RGBA  // What each lamp's geometry is colored, to rebuild it only on change.
}

var lightMaterialNames = [...]string{"PBR", "PBR ORM+glow", "shiny", "dull"}

var lightPostPresets = []struct {
	name string
	post nt3d.PostProcess
}{
	{name: "post off"},
	{name: "studio", post: nt3d.PostProcess{
		SSAO:     nt3d.SSAO{Strength: .65, Radius: .35},
		Bloom:    nt3d.Bloom{Strength: .45, Threshold: .78, Radius: 1},
		ToneMap:  nt3d.ACESToneMap,
		Vignette: .35,
	}},
	{name: "bloom + ACES", post: nt3d.PostProcess{
		Bloom:   nt3d.Bloom{Strength: .7, Threshold: .68, Radius: 1},
		ToneMap: nt3d.ACESToneMap,
	}},
	{name: "SSAO", post: nt3d.PostProcess{
		SSAO: nt3d.SSAO{Strength: .85, Radius: .4},
	}},
	{name: "Reinhard + vignette", post: nt3d.PostProcess{
		Exposure: .35,
		ToneMap:  nt3d.ReinhardToneMap,
		Vignette: .55,
	}},
}

// lamp is a point or spot light circling the scene's middle.
type lamp struct {
	color                color.RGBA
	radius, height       float32
	speed, offset        float64 // Radians a second, and where it starts.
	intensity            float32
	rng                  float32
	spot                 bool
	innerCone, outerCone float32
	aim                  nt3dmath.Vec3
}

type lightPreset struct {
	environment func() *nt3d.Environment
	name        string
	background  color.RGBA // Zero means the demos' usual light gray.
	ambient     nt3d.Ambient
	sun         *nt3d.Light // A directional light, turning slowly about Z when spin is set.
	spin        bool
	lamps       []lamp
}

func gray(v uint8) color.RGBA { return color.RGBA{v, v, v, 255} }

// eightLamps is a ring of eight point lights of hues around the wheel.
func eightLamps() []lamp {
	var lamps []lamp
	for i := range nt3d.MaxLights {
		h := float64(i) / nt3d.MaxLights
		lamps = append(lamps, lamp{color: hue(h), radius: 1.9, height: .55, speed: .4, offset: h * 2 * math.Pi, intensity: 1.3, rng: 2.2})
	}
	return lamps
}

// hue is a fully saturated color at h, 0..1, around the color wheel.
func hue(h float64) color.RGBA {
	c := func(shift float64) uint8 {
		v := math.Abs(math.Mod(h*6+shift, 6)-3) - 1
		return uint8(255 * max(0, min(1, v)))
	}
	return color.RGBA{c(0), c(4), c(2), 255}
}

var lightPresets = []lightPreset{
	{name: "sky and key", environment: dayEnvironment, sun: &nt3d.Light{Direction: nt3dmath.Vec3{X: 1, Y: -1, Z: 2}, Intensity: .65}},
	{name: "HDR IBL", environment: hdrEnvironment},
	{name: "sky IBL", environment: dayEnvironment},
	{name: "image IBL", environment: imageEnvironment},
	{name: "SH studio", environment: func() *nt3d.Environment { return shEnvironments()[0] }},
	{name: "default", ambient: nt3d.DefaultLighting().Ambient, sun: &nt3d.DefaultLighting().Lights[0]},
	{name: "sun and sky", environment: dayEnvironment, background: color.RGBA{196, 214, 238, 255}, ambient: nt3d.Ambient{},
		sun: &nt3d.Light{Direction: nt3dmath.Vec3{X: 1, Y: -1, Z: 1.1}, Color: color.RGBA{255, 228, 186, 255}, Intensity: 1.2}, spin: true},
	{name: "lamps", background: gray(30), ambient: nt3d.Ambient{Sky: gray(255), Ground: gray(255), Intensity: .06}, lamps: []lamp{
		{color: color.RGBA{255, 60, 50, 255}, radius: 1.5, height: .5, speed: .9, offset: 0, intensity: 1.8, rng: 3.5},
		{color: color.RGBA{60, 255, 90, 255}, radius: 1.2, height: .8, speed: -.7, offset: 2.1, intensity: 1.8, rng: 3.5},
		{color: color.RGBA{70, 110, 255, 255}, radius: 1.8, height: 1.1, speed: .5, offset: 4.2, intensity: 1.8, rng: 3.5},
	}},
	{name: "spotlights", background: gray(20), ambient: nt3d.Ambient{Sky: color.RGBA{180, 195, 225, 255}, Ground: gray(60), Intensity: .05}, lamps: []lamp{
		{color: color.RGBA{255, 224, 180, 255}, radius: 1.85, height: 1.75, speed: .45, offset: 0, intensity: 3.6, rng: 5.5, spot: true, innerCone: .20, outerCone: .48, aim: nt3dmath.Vec3{Z: .2}},
		{color: color.RGBA{80, 185, 255, 255}, radius: 1.75, height: 1.55, speed: -.35, offset: 2.1, intensity: 2.8, rng: 5.0, spot: true, innerCone: .16, outerCone: .42, aim: nt3dmath.Vec3{X: .2, Y: .1, Z: .2}},
		{color: color.RGBA{255, 95, 165, 255}, radius: 1.65, height: 1.45, speed: .6, offset: 4.2, intensity: 2.6, rng: 5.0, spot: true, innerCone: .16, outerCone: .40, aim: nt3dmath.Vec3{X: -.2, Y: -.1, Z: .2}},
	}},
	{name: "night", background: color.RGBA{10, 12, 24, 255}, ambient: nt3d.Ambient{Sky: color.RGBA{24, 30, 56, 255}, Ground: gray(8), Intensity: 1}, lamps: []lamp{
		{color: color.RGBA{255, 172, 92, 255}, radius: 1.75, height: .3, speed: .6, intensity: 2.2, rng: 2.8},
	}},
	{name: "eight", background: gray(30), ambient: nt3d.Ambient{Sky: gray(255), Ground: gray(255), Intensity: .04}, lamps: eightLamps()},
}

// Shapes showcase conductors and dielectrics in PBR modes, or pale surfaces in classic modes.
var lightShapes = []struct {
	name                string
	classicCol, pbrCol  color.RGBA
	metallic, roughness float32
	ior                 float32
	emissive            bool
	build               func(color.RGBA, *nt3d.Material) (nt3d.Geometry, error)
	at                  nt3dmath.Vec3
}{
	{"sphere", gray(225), color.RGBA{242, 198, 92, 255}, .95, .18, 0, false, func(c color.RGBA, m *nt3d.Material) (nt3d.Geometry, error) {
		return nt3d.BuildSphere(nt3d.Sphere{Radius: .45, Color: c, Material: m})
	}, nt3dmath.Vec3{X: -.7, Y: .2, Z: .45}},
	{"torus", color.RGBA{232, 214, 196, 255}, color.RGBA{236, 152, 112, 255}, .9, .32, 0, true, func(c color.RGBA, m *nt3d.Material) (nt3d.Geometry, error) {
		return nt3d.BuildTorus(nt3d.Torus{MajorRadius: .38, MinorRadius: .14, Color: c, Material: m})
	}, nt3dmath.Vec3{X: .75, Y: .45, Z: .14}},
	{"column", color.RGBA{204, 218, 232, 255}, color.RGBA{204, 218, 232, 255}, 0, .15, 1.65, false, func(c color.RGBA, m *nt3d.Material) (nt3d.Geometry, error) {
		return nt3d.BuildCylinder(nt3d.Cylinder{BottomRadius: .22, TopRadius: .22, Height: .9, Color: c, Material: m})
	}, nt3dmath.Vec3{X: .3, Y: -.75, Z: .45}},
	{"cube", gray(210), color.RGBA{198, 204, 214, 255}, .8, .45, 0, true, func(c color.RGBA, m *nt3d.Material) (nt3d.Geometry, error) {
		return nt3d.BuildCuboid(nt3d.Cuboid{Size: nt3dmath.Vec3{X: .45, Y: .45, Z: .45}, Color: c, Material: m})
	}, nt3dmath.Vec3{X: -.55, Y: -.85, Z: .225}},
}

func (d *demo) setupLights() error {
	d.lights = &lightScene{ranged: true, normal: true}
	// Open on the three orbiting lamps.
	for i, p := range lightPresets {
		if p.name == "lamps" {
			d.lights.preset = i
		}
	}
	if err := d.buildLightShapes(); err != nil {
		return err
	}
	for i := range d.lights.lamps {
		id, err := d.scene.Add(nt3d.Root, fmt.Sprintf("lamp %d", i+1))
		if err != nil {
			return err
		}
		d.lights.lamps[i] = id
	}
	_, err := d.applyLights()
	return err
}

// buildLightShapes places the floor and shapes for the active material mode.
func (d *demo) buildLightShapes() error {
	// The floor is two triangles: a point light's bright spot on it is
	// found per pixel, where no vertex is.
	const s = 3
	up := nt3dmath.Vec3{Z: 1}
	var floorMat *nt3d.Material
	if d.lights.material <= 1 {
		floorMat = &nt3d.Material{PBR: true, Metallic: .08, Roughness: .42}
	}
	floor := nt3d.Geometry{Indices: []uint32{0, 1, 2, 0, 2, 3}, Material: floorMat}
	for _, p := range [][2]float32{{-s, -s}, {s, -s}, {s, s}, {-s, s}} {
		floor.Vertices = append(floor.Vertices, nt3d.Vertex{Position: nt3dmath.Vec3{X: p[0], Y: p[1]}, Normal: up, Color: gray(200)})
	}
	if _, err := d.scene.SetGeometry("floor", floor); err != nil {
		return err
	}

	for _, shape := range lightShapes {
		col := shape.classicCol
		var m *nt3d.Material
		switch d.lights.material {
		case 0: // PBR
			col = shape.pbrCol
			m = &nt3d.Material{PBR: true, Metallic: shape.metallic, Roughness: shape.roughness, IOR: shape.ior}
		case 1: // PBR ORM + HDR emissive inlay
			col = shape.pbrCol
			m = &nt3d.Material{
				PBR:               true,
				Metallic:          1,
				Roughness:         1,
				IOR:               shape.ior,
				ORMMap:            reliefORMMap(),
				ORMRepeat:         true,
				OcclusionStrength: .85,
			}
			if shape.emissive {
				m.Emissive = color.RGBA{255, 255, 255, 255}
				m.EmissiveIntensity = 3.2
				m.EmissiveMap = inlayEmissiveMap()
				m.EmissiveRepeat = true
			}
		case 2: // Classic Blinn-Phong
			m = &nt3d.Material{Specular: .5, Shininess: 48}
		default: // Classic matte Lambert
			m = &nt3d.Material{}
		}
		if d.lights.normal {
			m.NormalMap = reliefNormalMap()
			m.NormalStrength = .65
			m.NormalRepeat = true
		}
		g, err := shape.build(col, m)
		if err != nil {
			return err
		}
		if g, err = nt3d.TransformGeometry(g, nt3dmath.Translation(d.lightShapePosition(shape.at))); err != nil {
			return err
		}
		if d.lights.shadows == shadowDrop {
			g.DropShadow = nt3d.DropShadow{Strength: .5, FixedFloor: true, FloorZ: 0}
		}
		if _, err = d.scene.SetGeometry(shape.name, g); err != nil {
			return err
		}
	}
	return nil
}

// lights is the preset's lighting at time t, in seconds: the sun turned,
// if it turns, and each lamp along its orbit, fading at its range or not.
func (p lightPreset) lights(t float64, ranged bool) nt3d.Lighting {
	l := nt3d.Lighting{Ambient: p.ambient}
	if p.sun != nil {
		sun := *p.sun
		if p.spin {
			sun.Direction = nt3dmath.RotationZ(t * .25).Direction(sun.Direction)
		}
		l.Lights = append(l.Lights, sun)
	}
	for _, lp := range p.lamps {
		a := lp.offset + lp.speed*t
		pos := nt3dmath.Vec3{X: lp.radius * float32(math.Cos(a)), Y: lp.radius * float32(math.Sin(a)), Z: lp.height}
		light := nt3d.Light{Kind: nt3d.PointLight, Position: pos, Color: lp.color, Intensity: lp.intensity}
		if lp.spot {
			light.Kind = nt3d.SpotLight
			light.Direction = lp.aim.Sub(pos).Normalize()
			light.InnerCone = lp.innerCone
			light.OuterCone = lp.outerCone
		}
		if ranged {
			light.Range = lp.rng
		}
		l.Lights = append(l.Lights, light)
	}
	return l
}

// applyLights moves each point light's lamp to where the preset's lighting
// puts it at the scene's time, and returns that lighting. The view's frames
// take their lighting from decorateLights, so changing it schedules no
// render here: the caller's Refresh does.
func (d *demo) applyLights() (nt3d.Lighting, error) {
	ls := d.lights
	p := lightPresets[ls.preset]
	var environment *nt3d.Environment
	if p.environment != nil {
		environment = p.environment()
	}
	if p.name == "SH studio" {
		environment = shEnvironments()[ls.shRotation]
	}
	d.scene.SetEnvironment(environment)
	l := p.lights(ls.t, ls.ranged)
	points := l.Lights[len(l.Lights)-len(p.lamps):]
	for i := range ls.lamps {
		if i >= len(p.lamps) {
			if err := d.scene.SetVisible(ls.lamps[i], false); err != nil {
				return l, err
			}
			continue
		}
		lp := p.lamps[i]
		if ls.lampColor[i] != lp.color {
			// Lamps are unlit with HDR emissive boost so bloom presets halo them.
			g, err := nt3d.BuildSphere(nt3d.Sphere{
				Radius:   .05,
				Segments: 12,
				Rings:    8,
				Color:    lp.color,
				Material: &nt3d.Material{Unlit: true, Emissive: lp.color, EmissiveIntensity: 3.0},
			})
			if err != nil {
				return l, err
			}
			if err := d.scene.SetNodeGeometry(ls.lamps[i], g); err != nil {
				return l, err
			}
			ls.lampColor[i] = lp.color
		}
		if err := d.scene.SetTransform(ls.lamps[i], nt3dmath.Translation(points[i].Position)); err != nil {
			return l, err
		}
		if err := d.scene.SetVisible(ls.lamps[i], true); err != nil {
			return l, err
		}
	}
	return l, nil
}

// decorateLights lights a frame as the preset is now, against its
// background.
func (d *demo) decorateLights(f *nt3d.Frame) {
	p := lightPresets[d.lights.preset]
	f.Lighting = p.lights(d.lights.t, d.lights.ranged)
	if len(f.Lighting.Lights) > 0 && d.lights.shadows == shadowMap {
		f.Lighting.Lights[0].Shadows = true
	}
	if p.background.A != 0 {
		f.Background = p.background
	}
	if d.lights.post > 0 && d.lights.post < len(lightPostPresets) {
		f.PostProcess = lightPostPresets[d.lights.post].post
	}
}

// stepLights moves the lights on by the time since the last step.
func (d *demo) stepLights() error {
	// The demo's phase wraps at 2π; the lights keep their own clock, so
	// their speeds need not divide a turn.
	step := math.Mod(d.phase-d.lights.phase+2*math.Pi, 2*math.Pi)
	d.lights.phase = d.phase
	d.lights.t += step
	_, err := d.applyLights()
	return err
}

func (d *demo) cycleLightPreset() error {
	d.lights.preset = (d.lights.preset + 1) % len(lightPresets)
	_, err := d.applyLights()
	return err
}

func (d *demo) toggleFalloff() error {
	d.lights.ranged = !d.lights.ranged
	_, err := d.applyLights()
	return err
}

func (d *demo) toggleLightShine() error {
	d.lights.material = (d.lights.material + 1) % len(lightMaterialNames)
	return d.buildLightShapes()
}

func (d *demo) cycleLightPost() {
	d.lights.post = (d.lights.post + 1) % len(lightPostPresets)
}

func (d *demo) lightsView(content string) string {
	p := lightPresets[d.lights.preset]
	n := len(p.lamps)
	if p.sun != nil {
		n++
	}
	falloff := "fade at range"
	if !d.lights.ranged {
		falloff = "1/(1+d²)"
	}
	shine := lightMaterialNames[d.lights.material]
	aa := ""
	if d.view != nil && (d.view.MSAA() || d.view.Supersample() > 1) {
		aa = " · " + d.aaLabel()
	}
	return fmt.Sprintf("nt3d lights · l preset · h SH rotation · f falloff · e material · n normal map · b lift · s shadows · p post · x AA · a pause · d fog · 1/2/3 backend · q quit\n%s\n%s · %s · %d of %d lights · %s · %s · %s%s · drag orbit · wheel zoom · r rotate · %s",
		content, d.rendering(), p.name, n, nt3d.MaxLights, falloff, fmt.Sprintf("%s · normal map %t · lifted %t · %s", shine, d.lights.normal, d.lights.lift, d.lights.shadows.String()), lightPostPresets[d.lights.post].name, aa, d.status)
}

func (d *demo) toggleNormalMap() error {
	d.lights.normal = !d.lights.normal
	return d.buildLightShapes()
}

func (d *demo) lightShapePosition(p nt3dmath.Vec3) nt3dmath.Vec3 {
	if d.lights.lift {
		p.Z += .6
	}
	return p
}

func (d *demo) rotateSHLight() error {
	for i, p := range lightPresets {
		if p.name == "SH studio" {
			d.lights.preset = i
			break
		}
	}
	d.lights.shRotation = (d.lights.shRotation + 1) % 12
	_, err := d.applyLights()
	return err
}
