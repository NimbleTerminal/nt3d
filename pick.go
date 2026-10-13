// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"math"
	"nimbleterminal.dev/nt3d/math"
)

type Primitive uint8

const (
	PointPrimitive Primitive = iota
	TrianglePrimitive
	BoxPrimitive
	LinePrimitive
	ArrowPrimitive
)

// Hit identifies a primitive in its original local geometry. Position is world
// space. Datum is the application ID for points, boxes, and arrows; for meshes
// and lines it is the original triangle or segment index.
type Hit struct {
	Node         NodeID
	Name         string
	Primitive    Primitive
	Index, Datum int
	Position     nt3dmath.Vec3
	Depth        float32
}

// Pick tests visible instances against a pixel coordinate. Meshes use actual
// ray/triangle intersections; points and strokes use screen-space tolerance.
// Frame must come from this scene and camera, without an altered Matrix.
func (s *Scene) Pick(camera Camera, f Frame, x, y, tolerance float32) *Hit {
	if x < 0 || y < 0 || x >= float32(f.Width) || y >= float32(f.Height) || !validFloat(x, y, tolerance) || tolerance < 0 {
		return nil
	}
	ray := camera.RayFromPixel(x, y, f.Width, f.Height)
	best := float32(math.Inf(1))
	var hit *Hit
	for i, g := range f.Geometry {
		inst := instance(f, i)
		if !inst.Visible {
			continue
		}
		inv, ok := inst.World.InverseAffine()
		if !ok {
			continue
		}
		localRay := nt3dmath.Ray{Origin: inv.Point(ray.Origin), Direction: inv.Direction(ray.Direction)}
		set := func(kind Primitive, index, datum int, p nt3dmath.Vec3) {
			world := inst.World.Point(p)
			_, _, depth, visible := f.Matrix.Project(world, f.Width, f.Height)
			if !visible || depth >= best {
				return
			}
			best = depth
			hit = &Hit{inst.Node, inst.Name, kind, index, datum, world, depth}
		}
		for j, p := range g.Points {
			px, py, _, ok := f.Matrix.Project(inst.World.Point(p.Position), f.Width, f.Height)
			r := max(tolerance, p.Radius)
			if ok && (px-x)*(px-x)+(py-y)*(py-y) <= r*r {
				set(PointPrimitive, j, p.Datum, p.Position)
			}
		}
		for j := 0; j+2 < len(g.Indices); j += 3 {
			a, b, c := g.Vertices[g.Indices[j]].Position, g.Vertices[g.Indices[j+1]].Position, g.Vertices[g.Indices[j+2]].Position
			if t, ok := rayTriangle(localRay, a, b, c); ok {
				set(TrianglePrimitive, j/3, j/3, localRay.Origin.Add(localRay.Direction.Scale(t)))
			}
		}
		for j, b := range g.Boxes {
			if t, ok := rayBox(localRay, b); ok {
				set(BoxPrimitive, j, b.Datum, localRay.Origin.Add(localRay.Direction.Scale(t)))
			}
		}
		stroke := func(kind Primitive, index, datum int, a, b nt3dmath.Vec3, width, head float32) {
			projected, ok := projectStroke(f, inst.World.Point(a), inst.World.Point(b))
			if !ok {
				return
			}
			rx, ry := x-projected.a.x, y-projected.a.y
			along := rx*projected.dx + ry*projected.dy
			side := abs(-rx*projected.dy + ry*projected.dx)
			limit := max(tolerance, lineWidth(width)*.5)
			hl, hw := projected.head(head, lineWidth(width))
			if hl > 0 && along > projected.length-hl && along <= projected.length {
				limit = max(limit, hw*(projected.length-along)/hl)
			}
			if along < -limit || along > projected.length+limit || side > limit {
				return
			}
			t := float32(0)
			if projected.length > 1e-5 {
				t = max(0, min(1, along/projected.length))
			}
			t = projected.worldT(t)
			set(kind, index, datum, a.Scale(1-t).Add(b.Scale(t)))
		}
		for j := 0; j+1 < len(g.Lines); j += 2 {
			stroke(LinePrimitive, j/2, j/2, g.Lines[j].Position, g.Lines[j+1].Position, g.LineWidth, 0)
		}
		for j, a := range g.Arrows {
			stroke(ArrowPrimitive, j, a.Datum, a.Start, a.End, a.Width, a.HeadSize)
		}
	}
	return hit
}

// rayTriangle follows the renderer's counterclockwise, front-face convention.
func rayTriangle(r nt3dmath.Ray, a, b, c nt3dmath.Vec3) (float32, bool) {
	edge1, edge2 := b.Sub(a), c.Sub(a)
	p := r.Direction.Cross(edge2)
	det := edge1.Dot(p)
	if det <= 1e-8 {
		return 0, false
	}
	v := r.Origin.Sub(a)
	u := v.Dot(p) / det
	if u < 0 || u > 1 {
		return 0, false
	}
	q := v.Cross(edge1)
	w := r.Direction.Dot(q) / det
	if w < 0 || u+w > 1 {
		return 0, false
	}
	t := edge2.Dot(q) / det
	return t, t >= 0
}
func rayBox(r nt3dmath.Ray, b Box) (float32, bool) {
	lo, hi := float32(0), float32(math.Inf(1))
	o, d, a, size := components(r.Origin), components(r.Direction), components(b.Min), components(b.Size)
	for i := range 3 {
		if d[i] == 0 {
			if o[i] < a[i] || o[i] > a[i]+size[i] {
				return 0, false
			}
			continue
		}
		x, y := (a[i]-o[i])/d[i], (a[i]+size[i]-o[i])/d[i]
		if x > y {
			x, y = y, x
		}
		lo = max(lo, x)
		hi = min(hi, y)
		if hi < lo {
			return 0, false
		}
	}
	return lo, true
}
