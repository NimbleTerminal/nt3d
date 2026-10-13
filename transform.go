// Copyright (c) 2026 Neomantra Corp.
package nt3d

// worldGeometry prepares CPU rendering inputs. GPU rendering keeps the same
// geometry in local coordinates and uploads only the instance transforms.
func worldGeometry(f Frame) []Geometry {
	out := make([]Geometry, 0, len(f.Geometry))
	for i, g := range f.Geometry {
		inst := instance(f, i)
		if !inst.Visible {
			continue
		}
		inv, _ := inst.World.InverseAffine()
		normal := inv.Transpose()
		g = cloneGeometry(g)
		for j := range g.Points {
			g.Points[j].Position = inst.World.Point(g.Points[j].Position)
		}
		for j := range g.Vertices {
			g.Vertices[j].Position = inst.World.Point(g.Vertices[j].Position)
			g.Vertices[j].Normal = normal.Direction(g.Vertices[j].Normal).Normalize()
		}
		for j := range g.Lines {
			g.Lines[j].Position = inst.World.Point(g.Lines[j].Position)
		}
		for j := range g.Arrows {
			g.Arrows[j].Start = inst.World.Point(g.Arrows[j].Start)
			g.Arrows[j].End = inst.World.Point(g.Arrows[j].End)
		}
		// Rotated boxes cannot be represented as axis-aligned box instances on CPU.
		// Use a separate untextured mesh so a mesh material cannot affect boxes.
		boxes := Geometry{}
		for _, b := range g.Boxes {
			for _, v := range boxVertices(b) {
				v.Position = inst.World.Point(v.Position)
				v.Normal = normal.Direction(v.Normal).Normalize()
				boxes.Indices = append(boxes.Indices, uint32(len(boxes.Vertices)))
				boxes.Vertices = append(boxes.Vertices, v)
			}
		}
		g.Boxes = nil
		out = append(out, g)
		if len(boxes.Vertices) > 0 {
			out = append(out, boxes)
		}
	}
	return out
}
