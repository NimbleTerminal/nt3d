// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"image"
	"image/color"
	"math"
	"sync"

	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

const (
	waterWakeCapacity  = 128
	waterSprayCapacity = 384
	waterWakeLife      = 3.6
)

// Stations stay where the stern passed. Their sideways spread grows with age;
// rendering projects them onto the current waves rather than dragging a decal
// behind the craft. Runs prevent a ribbon from bridging an airborne interval.
type wakeStation struct {
	x, y, dx, dy, born, distance, speed float64
	run                                 uint64
}
type sprayDrop struct {
	origin, velocity nt3dmath.Vec3
	born, life       float64
	radius           float32
	live             bool
}
type waterEffects struct {
	stations                [waterWakeCapacity]wakeStation
	first, count            int
	drops                   [waterSprayCapacity]sprayDrop
	nextDrop, serial, ticks int
	run                     uint64
	contact                 bool
	distance                float64
	mode                    int // all, wake only, spray only, hidden; simulation always continues.
	left, right, spray      nt3d.NodeID
	points                  [waterSprayCapacity]nt3d.Point
	live                    int
}

func (e *waterEffects) modeName() string {
	return [...]string{"wake+spray", "wake", "spray", "effects off"}[e.mode]
}

func (e *waterEffects) addStation(s wakeStation) {
	if e.count == len(e.stations) {
		e.first = (e.first + 1) % len(e.stations)
		e.count--
	}
	e.stations[(e.first+e.count)%len(e.stations)] = s
	e.count++
}

func (e *waterEffects) spawn(position, velocity nt3dmath.Vec3, time, life float64, radius float32) {
	e.drops[e.nextDrop] = sprayDrop{origin: position, velocity: velocity, born: time, life: life, radius: radius, live: true}
	e.nextDrop = (e.nextDrop + 1) % len(e.drops)
}

func dropPosition(d sprayDrop, time float64) nt3dmath.Vec3 {
	age := time - d.born
	return d.origin.Add(d.velocity.Scale(float32(age))).Add(vec(0, 0, -.5*9.81*age*age))
}

// Called once per physics step, including while the effects are hidden. No
// renderer cadence, wall clock, or global random generator influences emission.
func (e *waterEffects) step(surface *nt3d.WaveSurface, c *waterCraft, dt, impact, turn float64) error {
	e.ticks++
	for e.count > 0 && c.time-e.stations[e.first].born >= waterWakeLife {
		e.first = (e.first + 1) % len(e.stations)
		e.count--
	}
	for i := range e.drops {
		d := &e.drops[i]
		if !d.live {
			continue
		}
		age := c.time - d.born
		p := dropPosition(*d, c.time)
		if age >= d.life || math.Abs(float64(p.X)) > 4.7 || math.Abs(float64(p.Y)) > 4.7 {
			d.live = false
			continue
		}
		if age > .06 && float64(d.velocity.Z)-9.81*age < 0 {
			water, err := surface.AtWorld(float64(p.X), float64(p.Y), c.time)
			if err != nil {
				return err
			}
			if p.Z <= water.Position.Z+.015 {
				d.live = false
			}
		}
	}
	touching := !c.airborne && c.speed > .4
	if touching && !e.contact {
		e.run++
	}
	dx, dy := math.Cos(c.heading), math.Sin(c.heading)
	if touching {
		e.distance += c.speed * dt
		if !e.contact || e.ticks%4 == 0 {
			s := wakeStation{x: c.x - .68*dx, y: c.y - .68*dy, dx: dx, dy: dy, born: c.time, distance: e.distance, speed: c.speed, run: e.run}
			if e.count > 0 {
				prev := e.stations[(e.first+e.count-1)%len(e.stations)]
				// A boundary bounce or a discontinuous heading must not join folded quads.
				if prev.dx*dx+prev.dy*dy < .6 || math.Hypot(s.x-prev.x, s.y-prev.y) > .7 {
					e.run++
					s.run = e.run
				}
			}
			e.addStation(s)
			// Thin side jets grow with speed and turn rate; the outside of a turn
			// throws more spray. All positions use the actual water under the hull.
			for _, side := range []float64{-1, 1} {
				count := 1 + int(math.Min(2, math.Abs(turn)*c.speed*.2))
				outward := .5 + .12*c.speed
				if side*turn < 0 {
					outward += math.Min(1.5, math.Abs(turn)*c.speed*.2)
				}
				for range count {
					x, y := c.x-.35*dx-side*.29*dy, c.y-.35*dy+side*.29*dx
					q, err := surface.AtWorld(x, y, c.time)
					if err != nil {
						return err
					}
					e.serial++
					r := particleRandom(e.serial, 10)
					velocity := vec(dx*c.speed*.25-side*dy*outward, dy*c.speed*.25+side*dx*outward, .8+.12*c.speed+.6*r)
					e.spawn(q.Position.Add(vec(0, 0, .05)), velocity, c.time, .55+.35*r, float32(.025+.025*r))
				}
			}
		}
	}
	e.contact = touching
	if impact > .8 {
		count := min(72, 16+int(impact*10))
		for range count {
			e.serial++
			a := particleRandom(e.serial, 1) * 2 * math.Pi
			r := particleRandom(e.serial, 2)
			x, y := c.x+math.Cos(a)*.35, c.y+math.Sin(a)*.25
			q, err := surface.AtWorld(x, y, c.time)
			if err != nil {
				return err
			}
			spread := .7 + math.Min(3, impact*.6)*r
			velocity := vec(math.Cos(a)*spread+dx*c.speed*.18, math.Sin(a)*spread+dy*c.speed*.18, 1+math.Min(3, impact*.5)*(.4+.6*r))
			e.spawn(q.Position.Add(vec(0, 0, .06)), velocity, c.time, .6+.4*r, float32(.035+.035*r))
		}
	}
	return nil
}

var wakeFoam = sync.OnceValue(func() *nt3d.Texture {
	const width, height = 64, 32
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			u, v := (float64(x)+.5)/width, (float64(y)+.5)/height
			edge := math.Pow(math.Sin(math.Pi*v), 1.3)
			streak := .45 + .3*math.Sin(2*math.Pi*(3*u+v)) + .25*math.Cos(2*math.Pi*(7*u-2*v))
			img.SetNRGBA(x, y, color.NRGBA{225, 246, 250, uint8(255 * edge * math.Max(.05, streak))})
		}
	}
	t, err := nt3d.NewTexture(img)
	if err != nil {
		panic(err)
	}
	return t
})

func (e *waterEffects) ribbon(surface *nt3d.WaveSurface, time, side float64) (nt3d.Geometry, error) {
	g := nt3d.Geometry{Material: &nt3d.Material{Texture: wakeFoam(), Repeat: true, Tint: true, Transparent: true, Unlit: true}}
	previous := -1
	var last wakeStation
	for i := 0; i < e.count; i++ {
		s := e.stations[(e.first+i)%len(e.stations)]
		age := time - s.born
		if age < 0 || age >= waterWakeLife {
			previous = -1
			continue
		}
		spread := .28 + age*(.23+.025*s.speed)
		width := .14 + .12*age
		var row [3]nt3d.Vertex
		visible := true
		for j := range row {
			offset := side*spread + (float64(j)-1)*width
			x, y := s.x-s.dy*offset, s.y+s.dx*offset
			boundary := math.Max(math.Abs(x), math.Abs(y))
			if boundary >= 4.65 {
				visible = false
				break
			}
			q, err := surface.AtWorld(x, y, time)
			if err != nil {
				return g, err
			}
			fade := math.Pow(1-age/waterWakeLife, 1.5) * math.Min(1, age/.10) * math.Min(1, s.speed/3) * math.Min(1, (4.65-boundary)/.3)
			alpha := uint8(210 * fade)
			if j != 1 {
				alpha = 0
			}
			row[j] = nt3d.Vertex{Position: q.Position.Add(vec(0, 0, .025)), Normal: q.Normal, Color: color.RGBA{255, 255, 255, alpha}}
		}
		if !visible {
			previous = -1
			continue
		}
		base := len(g.Vertices)
		g.Vertices = append(g.Vertices, row[:]...)
		for j := range row {
			g.UV = append(g.UV, nt3d.UV{U: float32(s.distance * 1.5), V: float32(j) * .5})
		}
		if previous >= 0 && s.run == last.run {
			for j := 0; j < 2; j++ {
				a, b, c, d := uint32(previous+j), uint32(base+j), uint32(base+j+1), uint32(previous+j+1)
				// Consecutive stations run forward; offsets increase toward the left.
				g.Indices = append(g.Indices, a, b, c, a, c, d)
			}
		}
		previous = base
		last = s
	}
	g.Bounds = nt3d.GeometryBounds(g)
	return g, nil
}

func (e *waterEffects) pointSnapshot(time float64) nt3dmath.AABB {
	var bounds nt3dmath.AABB
	e.live = 0
	for i, d := range e.drops {
		p := nt3d.Point{Radius: .01} // Dormant slots have zero alpha, even in wireframe.
		age := time - d.born
		if d.live && age >= 0 && age < d.life {
			fade := math.Min(1, age/.035) * math.Pow(1-age/d.life, 1.2)
			p = nt3d.Point{Position: dropPosition(d, time), Radius: d.radius * float32(1+.8*age/d.life), Color: color.RGBA{224, 245, 255, uint8(220 * fade)}}
			e.live++
		}
		e.points[i] = p
		bounds.Include(p.Position)
	}
	return bounds
}

func (d *demo) setupWaterEffects() error {
	e := &d.water.effects
	for _, node := range []struct {
		name string
		id   *nt3d.NodeID
	}{{"wake-left", &e.left}, {"wake-right", &e.right}, {"water-spray", &e.spray}} {
		id, err := d.scene.SetGeometry(node.name, nt3d.Geometry{})
		if err != nil {
			return err
		}
		*node.id = id
		if err = d.scene.Reparent(id, d.group); err != nil {
			return err
		}
	}
	return d.updateWaterSprayStyle()
}

func (d *demo) updateWaterSprayStyle() error {
	e := &d.water.effects
	bounds := e.pointSnapshot(d.water.craft.time)
	soft := float32(0)
	if d.water.softSpray {
		soft = .16
	}
	return d.scene.SetNodeGeometry(e.spray, nt3d.Geometry{Points: e.points[:], Bounds: bounds, PointStyle: nt3d.PointStyle{Units: nt3d.WorldUnits, Shape: nt3d.SoftPoint, Blend: nt3d.AlphaBlend, SoftDistance: soft}})
}

func (d *demo) drawWaterEffects() error {
	w := d.water
	e := &w.effects
	// Physics and emissions share the fixed-step clock; the wave-following ribbons
	// are placed at display time so they don't sink into the current water mesh.
	for i, side := range []float64{-1, 1} {
		g, err := e.ribbon(w.surface, d.animationTime, side)
		if err != nil {
			return err
		}
		id := []nt3d.NodeID{e.left, e.right}[i]
		if err = d.scene.SetNodeGeometry(id, g); err != nil {
			return err
		}
		if err = d.scene.SetVisible(id, e.mode == 0 || e.mode == 1); err != nil {
			return err
		}
	}
	bounds := e.pointSnapshot(w.craft.time)
	if err := d.scene.SetNodePoints(e.spray, e.points[:], bounds); err != nil {
		return err
	}
	return d.scene.SetVisible(e.spray, e.mode == 0 || e.mode == 2)
}
