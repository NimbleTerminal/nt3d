// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"fmt"
	"image/color"
	"math"
	"nimbleterminal.dev/nt3d"
	ntmath "nimbleterminal.dev/nt3d/math"
)

var waterDebugColors = [...]color.RGBA{
	{80, 255, 120, 255},  // buoyancy / wet probe
	{40, 220, 255, 255},  // planing
	{255, 80, 100, 255},  // drag / dry probe
	{255, 220, 65, 255},  // jet
	{255, 255, 255, 255}, // velocity
}

func (d *demo) drawWaterDebug() error {
	w := d.water
	if w.debugMode == 0 || w.moored || !w.craft.ready || w.craft.time == 0 {
		return d.scene.SetVisible(w.debugNode, false)
	}
	t := w.craft.telemetry
	g := nt3d.Geometry{LineWidth: 2, PointStyle: nt3d.PointStyle{Units: nt3d.WorldUnits}}
	for _, p := range t.probes {
		c := waterDebugColors[0]
		if p.wet == 0 {
			c = waterDebugColors[2]
		}
		bottom, top := p.center.Add(vec(0, 0, -.115)), p.center.Add(vec(0, 0, .25))
		g.Lines = append(g.Lines, nt3d.Vertex{Position: bottom, Color: c}, nt3d.Vertex{Position: top, Color: c})
		// Crosses mark actual sampled water level; the elevated dot identifies
		// each probe even when its hull cell is hidden inside the visible hull.
		for _, axis := range []ntmath.Vec3{vec(.09, 0, 0), vec(0, .09, 0)} {
			g.Lines = append(g.Lines, nt3d.Vertex{Position: p.surface.Sub(axis), Color: waterDebugColors[1]}, nt3d.Vertex{Position: p.surface.Add(axis), Color: waterDebugColors[1]})
		}
		g.Points = append(g.Points, nt3d.Point{Position: top, Radius: .045, Color: c})
	}
	origin := t.origin.Add(vec(0, 0, .5))
	// All force arrows share 1 m / 500 N; velocity uses 1 m / 5 m/s.
	for i, v := range []ntmath.Vec3{t.water.Buoyancy, t.water.Planing, t.water.Drag, t.thrust} {
		if v.Dot(v) > 1e-8 {
			g.Arrows = append(g.Arrows, nt3d.Arrow{Start: origin, End: origin.Add(v.Scale(.002)), Width: 2, HeadSize: 8, Color: waterDebugColors[i]})
		}
	}
	if t.velocity.Dot(t.velocity) > 1e-8 {
		g.Arrows = append(g.Arrows, nt3d.Arrow{Start: origin, End: origin.Add(t.velocity.Scale(.2)), Width: 2, HeadSize: 8, Color: waterDebugColors[4]})
	}
	if err := d.scene.SetNodeGeometry(w.debugNode, g); err != nil {
		return err
	}
	return d.scene.SetVisible(w.debugNode, true)
}
func (d *demo) waterDebugText() string {
	c := &d.water.craft
	if c.time == 0 {
		return "No physics sample yet · a runs · . advances one paused step"
	}
	t := c.telemetry
	state := "running"
	if !d.animate {
		state = "paused"
	}
	if d.water.debugMode == 1 {
		magnitude := func(v ntmath.Vec3) float64 { return math.Sqrt(float64(v.Dot(v))) }
		return fmt.Sprintf("%s · %s · wet %.0f%%/%d · B %.0f L %.0f D %.0f T %.0f N · net %.2fg · peak %.2fg", state, c.handling().name, 100*t.water.WetFraction, t.water.WetPoints, magnitude(t.water.Buoyancy), magnitude(t.water.Planing), magnitude(t.water.Drag), magnitude(t.thrust), t.acceleration, t.peakAcceleration)
	}
	return fmt.Sprintf("%s · %s · %.1fm/s · power %.0f%% · steer %+.2f · trim %+.0f° · pitch %+.0f° bank %+.0f° slip %+.0f° · hit %.1fm/s", state, c.handling().name, c.speed, 100*c.power, c.rudder, c.trim*180/math.Pi, t.pitch, t.bank, t.slip, t.lastImpact)
}
