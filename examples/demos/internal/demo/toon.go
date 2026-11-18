// Copyright (c) 2026 Neomantra Corp.

package demo

import (
	"fmt"
	"hash/fnv"
	"image/color"
	"io/fs"
	"math"
	"strings"

	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

// ToonMain shows Spot and the robot, giants in a city block, cel shaded with
// inked outlines and crease lines. SampleFiles supplies the models.
func ToonMain() { mainDemo(false, sceneToon) }

// toonScene is the toon demo's state: which look is on, and the nodes the
// outline applies to.
type toonScene struct {
	on, outlines, softShadows        bool
	bands                            int // Index into toonBands.
	buildings, street, actors, props nt3d.NodeID
	bannerJoint                      nt3d.NodeID
}

var (
	toonBands = []int{2, 3, 4}
	toonInk   = color.RGBA{24, 24, 36, 255}
	toonShade = color.RGBA{150, 160, 220, 255}
	toonSky   = color.RGBA{168, 204, 232, 255}
	// toonPalette paints the buildings, each by a hash of its name.
	toonPalette = []color.RGBA{{232, 196, 160, 255}, {196, 214, 224, 255}, {224, 170, 150, 255}, {186, 200, 170, 255}, {214, 206, 226, 255}}
	toonGround  = color.RGBA{92, 98, 110, 255}
	toonRoad    = color.RGBA{124, 130, 142, 255}
	// toonSun is one sun, low enough that the walls facing it reach the
	// brightest band while the ground and the other walls take the middle
	// one, casting the shadows.
	toonSun = nt3d.Lighting{
		Ambient: nt3d.Ambient{Sky: color.RGBA{255, 255, 255, 255}, Ground: color.RGBA{255, 255, 255, 255}, Intensity: .5},
		Lights:  []nt3d.Light{{Direction: nt3dmath.Vec3{X: .8, Y: .45, Z: .55}, Intensity: .75, Shadows: true}},
	}
)

const (
	toonActorHeight    = 14  // Meters: giants, to read against four-storey buildings.
	toonRoadWidth      = 7   // Meters.
	toonRoadLift       = .05 // Meters above the ground.
	toonShadowSoftness = .45 // Softened cast-shadow penumbra on city streets.
)

// toonCamera looks at the actors from the street's end, buildings behind.
func toonCamera(c *nt3d.Camera) {
	c.Elevation, c.Azimuth, c.Distance, c.Projection = 20, -35, 48, nt3d.Perspective
	c.Target = nt3dmath.Vec3{Z: toonActorHeight / 2}
}

// toonStyle is the look now; the actors alone get a rim highlight, which on
// flat ground and walls would draw an arc where the view grazes them.
func (d *demo) toonStyle(rim float32) nt3d.ToonStyle {
	if !d.toon.on {
		return nt3d.ToonStyle{}
	}
	shadowSoft := float32(0)
	if d.toon.softShadows {
		shadowSoft = toonShadowSoftness
	}
	return nt3d.ToonStyle{Bands: toonBands[d.toon.bands], ShadowSoftness: shadowSoft, Shade: toonShade, Rim: rim}
}

// applyToon gives the whole scene the toon style, and the buildings, actors,
// and street props their ink. The street takes none: crease lines would trace
// every road ribbon's border.
func (d *demo) applyToon() error {
	if err := d.scene.ApplyToon(nt3d.Root, d.toonStyle(0)); err != nil {
		return err
	}
	if err := d.scene.ApplyToon(d.toon.actors, d.toonStyle(.3)); err != nil {
		return err
	}
	ink := nt3d.OutlineStyle{}
	if d.toon.outlines {
		ink = nt3d.OutlineStyle{Width: 2, Color: toonInk, Crease: 30}
	}
	for _, id := range []nt3d.NodeID{d.toon.buildings, d.toon.actors, d.toon.props} {
		if err := d.scene.ApplyOutline(id, ink); err != nil {
			return err
		}
	}
	return nil
}

func (d *demo) toggleToon() error {
	d.toon.on = !d.toon.on
	return d.applyToon()
}

func (d *demo) cycleToonBands() error {
	d.toon.bands = (d.toon.bands + 1) % len(toonBands)
	return d.applyToon()
}

func (d *demo) toggleToonOutlines() error {
	d.toon.outlines = !d.toon.outlines
	return d.applyToon()
}

func (d *demo) toggleToonShadows() error {
	d.toon.softShadows = !d.toon.softShadows
	return d.applyToon()
}

func (d *demo) stepToon() error {
	if d.toon == nil || d.toon.bannerJoint == 0 {
		return nil
	}
	angle := (38 + 14*math.Sin(d.animationTime*2.2)) * math.Pi / 180
	return d.scene.SetTransform(d.toon.bannerJoint, nt3dmath.RotationZ(angle))
}

func paint(g *nt3d.Geometry, c color.RGBA) {
	for i := range g.Vertices {
		g.Vertices[i].Color = c
	}
}

// roadRibbons turns road centerlines, endpoint pairs, into one mesh of flat
// quads width wide at height z, each run on by half its width so bends close.
func roadRibbons(lines []nt3d.Vertex, width, z float32, c color.RGBA) nt3d.Geometry {
	g := nt3d.Geometry{
		Material: &nt3d.Material{Roughness: .65, Clearcoat: .75, ClearcoatRoughness: .14},
	}
	up := nt3dmath.Vec3{Z: 1}
	for i := 0; i+1 < len(lines); i += 2 {
		a, b := lines[i].Position, lines[i+1].Position
		along := nt3dmath.Vec3{X: b.X - a.X, Y: b.Y - a.Y}
		if l := float32(math.Hypot(float64(along.X), float64(along.Y))); l > 0 {
			along = along.Scale(width / 2 / l)
		} else {
			along = nt3dmath.Vec3{X: width / 2}
		}
		left := nt3dmath.Vec3{X: -along.Y, Y: along.X}
		a, b = a.Sub(along), b.Add(along)
		base := uint32(len(g.Vertices))
		for _, p := range []nt3dmath.Vec3{a.Sub(left), b.Sub(left), b.Add(left), a.Add(left)} {
			p.Z = z
			g.Vertices = append(g.Vertices, nt3d.Vertex{Position: p, Normal: up, Color: c})
		}
		g.Indices = append(g.Indices, base, base+1, base+2, base, base+2, base+3)
	}
	g.Bounds = nt3d.GeometryBounds(g)
	return g
}

// standing places a model whose bounds are b upright at p: scaled to the
// actors' height, centered over p, its lowest point there.
func standing(b nt3dmath.AABB, p nt3dmath.Vec3) nt3dmath.Mat4 {
	s := toonActorHeight / (b.Max.Z - b.Min.Z)
	center := nt3dmath.Vec3{X: (b.Min.X + b.Max.X) / 2, Y: (b.Min.Y + b.Max.Y) / 2, Z: b.Min.Z}
	return nt3dmath.Translation(p).Mul(nt3dmath.Scaling(nt3dmath.Vec3{X: s, Y: s, Z: s})).Mul(nt3dmath.Translation(center.Scale(-1)))
}

// roadSpots are two places to stand on the roads: the middle of the segment
// nearest the block's center, and of the nearest one clear of it.
func roadSpots(lines []nt3d.Vertex, clear float32) (first, second nt3dmath.Vec3) {
	mid := func(i int) nt3dmath.Vec3 { return lines[i].Position.Add(lines[i+1].Position).Scale(.5) }
	best := float32(math.Inf(1))
	for i := 0; i+1 < len(lines); i += 2 {
		if m := mid(i); m.X*m.X+m.Y*m.Y < best {
			best, first = m.X*m.X+m.Y*m.Y, m
		}
	}
	best, second = float32(math.Inf(1)), first.Add(nt3dmath.Vec3{X: clear})
	for i := 0; i+1 < len(lines); i += 2 {
		m := mid(i)
		away := m.Sub(first)
		if d2 := away.X*away.X + away.Y*away.Y; d2 >= clear*clear && d2 < best {
			best, second = d2, m
		}
	}
	return first, second
}

// buildToonProps places an inked street-level glass transit shelter (refractive
// enclosure + translucent canopy) topped by a skinned articulated transit
// pennant so see-through and deforming crease linework stand right beside the
// block's rigid buildings.
func buildToonProps(s *nt3d.Scene, here, there nt3dmath.Vec3) (props, bannerJoint nt3d.NodeID, err error) {
	props, err = s.Add(nt3d.Root, "props")
	if err != nil {
		return 0, 0, err
	}
	dir := there.Sub(here)
	dir.Z = 0
	yaw := math.Atan2(float64(dir.Y), float64(dir.X))
	normal := nt3dmath.Vec3{X: -float32(math.Sin(yaw)), Y: float32(math.Cos(yaw))}
	pos := here.Add(there).Scale(.5).Add(normal.Scale(4.6))
	pos.Z = toonRoadLift
	if err = s.SetTransform(props, nt3dmath.Translation(pos).Mul(nt3dmath.RotationZ(yaw))); err != nil {
		return 0, 0, err
	}

	enclosure, err := nt3d.BuildCuboid(nt3d.Cuboid{
		Size:     nt3dmath.Vec3{X: 5.2, Y: 2.6, Z: 4.6},
		Color:    color.RGBA{225, 244, 252, 255},
		Material: &nt3d.Material{PBR: true, Transmission: .82, Roughness: .08, IOR: 1.5},
	})
	if err != nil {
		return 0, 0, err
	}
	glassID, err := s.Add(props, "shelter glass")
	if err != nil {
		return 0, 0, err
	}
	if err = s.SetNodeGeometry(glassID, enclosure); err != nil {
		return 0, 0, err
	}
	if err = s.SetTransform(glassID, nt3dmath.Translation(nt3dmath.Vec3{Z: 2.3})); err != nil {
		return 0, 0, err
	}

	canopy, err := nt3d.BuildCuboid(nt3d.Cuboid{
		Size:     nt3dmath.Vec3{X: 6.2, Y: 3.4, Z: .4},
		Color:    color.RGBA{96, 184, 214, 175},
		Material: &nt3d.Material{Transparent: true},
	})
	if err != nil {
		return 0, 0, err
	}
	canopyID, err := s.Add(props, "shelter canopy")
	if err != nil {
		return 0, 0, err
	}
	if err = s.SetNodeGeometry(canopyID, canopy); err != nil {
		return 0, 0, err
	}
	if err = s.SetTransform(canopyID, nt3dmath.Translation(nt3dmath.Vec3{Z: 4.8})); err != nil {
		return 0, 0, err
	}

	// Two-panel articulated 3D pennant sharing x = 0 so its fold and borders
	// carry live skinned crease linework.
	coral := color.RGBA{236, 118, 92, 255}
	mastLeaf, err := nt3d.BuildCuboid(nt3d.Cuboid{Size: nt3dmath.Vec3{X: 1.2, Y: .22, Z: 1.8}, Color: coral})
	if err != nil {
		return 0, 0, err
	}
	wingLeaf, err := nt3d.BuildCuboid(nt3d.Cuboid{Size: nt3dmath.Vec3{X: 2.2, Y: .22, Z: 1.8}, Color: coral})
	if err != nil {
		return 0, 0, err
	}
	var banner nt3d.Geometry
	banner.Material = &nt3d.Material{
		Roughness:      .82,
		SheenColor:     color.RGBA{255, 210, 195, 255},
		SheenRoughness: .42,
	}
	var inf []nt3d.Influence
	for _, v := range mastLeaf.Vertices {
		v.Position.X -= .6
		banner.Vertices = append(banner.Vertices, v)
		inf = append(inf, nt3d.Influence{})
	}
	banner.Indices = append(banner.Indices, mastLeaf.Indices...)
	base := uint32(len(banner.Vertices))
	for _, v := range wingLeaf.Vertices {
		v.Position.X += 1.1
		banner.Vertices = append(banner.Vertices, v)
		inf = append(inf, nt3d.Influence{Weights: [4]float32{1}})
	}
	for _, idx := range wingLeaf.Indices {
		banner.Indices = append(banner.Indices, base+idx)
	}
	banner.Bounds = nt3d.GeometryBounds(banner)

	bannerID, err := s.Add(props, "shelter banner")
	if err != nil {
		return 0, 0, err
	}
	if err = s.SetNodeGeometry(bannerID, banner); err != nil {
		return 0, 0, err
	}
	if err = s.SetTransform(bannerID, nt3dmath.Translation(nt3dmath.Vec3{X: -1.6, Z: 5.9})); err != nil {
		return 0, 0, err
	}
	bannerJoint, err = s.Add(bannerID, "banner flap")
	if err != nil {
		return 0, 0, err
	}
	if err = s.SetDeformation(bannerID, nt3d.Deformation{
		Skeleton:   &nt3d.Skeleton{Joints: []nt3d.NodeID{bannerJoint}},
		Influences: inf,
	}); err != nil {
		return 0, 0, err
	}
	if err = s.SetTransform(bannerJoint, nt3dmath.RotationZ(38*math.Pi/180)); err != nil {
		return 0, 0, err
	}
	return props, bannerJoint, nil
}

func (d *demo) setupToon() error {
	d.toon = &toonScene{on: true, outlines: true, softShadows: true, bands: 1}
	// The scene is lit by the sun, under a day sky.
	look := d.scene.Look()
	look.Lighting, look.Background = toonSun, toonSky
	d.scene.SetLook(look)
	data, err := fs.ReadFile(SampleFiles, "flatiron.obj")
	if err != nil {
		return err
	}
	block, err := nt3dmesh.DecodeOBJ(data, &nt3dmesh.Options{Up: nt3dmesh.ZUp})
	if err != nil {
		return fmt.Errorf("flatiron.obj: %w", err)
	}
	// The file's ground is a decimeter under its origin: lift the city so
	// the ground is at zero.
	var ground *nt3d.Geometry
	var roads []nt3d.Vertex
	city, err := d.scene.Add(nt3d.Root, "city")
	if err != nil {
		return err
	}
	if d.toon.buildings, err = d.scene.Add(city, "buildings"); err != nil {
		return err
	}
	if d.toon.street, err = d.scene.Add(city, "street"); err != nil {
		return err
	}
	for i := range block.Objects {
		o := &block.Objects[i]
		switch {
		case strings.HasPrefix(o.Name, "building "):
			h := fnv.New32a()
			h.Write([]byte(o.Name))
			g := o.Geometry
			paint(&g, toonPalette[h.Sum32()%uint32(len(toonPalette))])
			id, err := d.scene.Add(d.toon.buildings, o.Name)
			if err != nil {
				return err
			}
			if err = d.scene.SetNodeGeometry(id, g); err != nil {
				return fmt.Errorf("%s: %w", o.Name, err)
			}
		case o.Name == "ground":
			ground = &o.Geometry
		case o.Name == "roads":
			roads = o.Geometry.Lines
		}
	}
	if ground == nil || len(ground.Vertices) == 0 || len(roads) < 2 {
		return fmt.Errorf("flatiron.obj: no ground or roads")
	}
	lift := -ground.Vertices[0].Position.Z
	if err = d.scene.SetTransform(city, nt3dmath.Translation(nt3dmath.Vec3{Z: lift})); err != nil {
		return err
	}
	paint(ground, toonGround)
	for name, g := range map[string]nt3d.Geometry{"ground": *ground, "roads": roadRibbons(roads, toonRoadWidth, toonRoadLift-lift, toonRoad)} {
		id, err := d.scene.Add(d.toon.street, name)
		if err != nil {
			return err
		}
		if err = d.scene.SetNodeGeometry(id, g); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}

	if d.toon.actors, err = d.scene.Add(nt3d.Root, "actors"); err != nil {
		return err
	}
	here, there := roadSpots(roads, 15)
	here.Z, there.Z = toonRoadLift, toonRoadLift
	cow, err := spot()
	if err != nil {
		return err
	}
	id, err := d.scene.Add(d.toon.actors, "spot")
	if err != nil {
		return err
	}
	if err = d.scene.SetNodeGeometry(id, cow); err != nil {
		return err
	}
	if err = d.scene.SetTransform(id, standing(nt3d.GeometryBounds(cow), here)); err != nil {
		return err
	}
	data, err = fs.ReadFile(SampleFiles, "robot.3mf")
	if err != nil {
		return err
	}
	robot, err := nt3dmesh.Decode(data, nt3dmesh.ThreeMF, &nt3dmesh.Options{Color: color.RGBA{100, 180, 230, 255}, Up: nt3dmesh.ZUp})
	if err != nil {
		return fmt.Errorf("robot.3mf: %w", err)
	}
	id, err = robot.AddTo(d.scene, d.toon.actors, "robot")
	if err != nil {
		return err
	}
	if err = d.scene.SetTransform(id, standing(robot.Bounds(), there)); err != nil {
		return err
	}
	if d.toon.props, d.toon.bannerJoint, err = buildToonProps(d.scene, here, there); err != nil {
		return err
	}
	return d.applyToon()
}

func (d *demo) toonView(content string) string {
	look, ink, shadows := "toon off", "outlines off", "crisp shadows"
	if d.toon.on {
		look = fmt.Sprintf("toon on · %d bands", toonBands[d.toon.bands])
	}
	if d.toon.outlines {
		ink = "outlines on"
	}
	if d.toon.softShadows {
		shadows = "soft shadows"
	}
	return fmt.Sprintf("nt3d toon · © OpenStreetMap contributors · Spot by Keenan Crane · t toon · n bands · b outlines · s shadows · a breeze · c fit · 1/2/3 backend · q quit\n%s\n%s · %s · %s · %s · %s",
		content, d.rendering(), look, ink, shadows, d.status)
}
