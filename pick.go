// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"math"
	"nimbleterminal.dev/nt3d/math"
	"sync/atomic"
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
// The first pick of a large mesh starts indexing its triangles in the
// background; later picks use the index until the geometry changes. Until
// then, and for frames made elsewhere or older than the scene's geometry,
// triangles are tested one by one. Call Pick on the goroutine that edits the
// scene.
func (s *Scene) Pick(camera Camera, f Frame, x, y, tolerance float32) *Hit {
	if x < 0 || y < 0 || x >= float32(f.Width) || y >= float32(f.Height) || !validFloat(x, y, tolerance) || tolerance < 0 {
		return nil
	}
	ray := camera.RayFromPixel(x, y, f.Width, f.Height)
	best := float32(math.Inf(1))
	var hit *Hit
	drawn := f.drawn(1)
	for i, g := range f.Geometry {
		inst := instance(f, i)
		if !drawn[i] || edgeOn(g) || g.Billboard != nil && (g.Billboard.NoPick || len(g.Vertices) > 0 && g.Vertices[0].Color.A == 0) {
			continue // Edge-on geometry shows only at silhouettes; the surface beneath is picked.
		}
		inv, ok := inst.World.InverseAffine()
		if !ok {
			continue
		}
		localRay := nt3dmath.Ray{Origin: inv.Point(ray.Origin), Direction: inv.Direction(ray.Direction)}
		// set keeps a hit at local point p if it is in front of the best,
		// as renderers keep what they draw: depth biased as the primitive is,
		// and a tie going to the later draw. The pixel is in view, so only
		// depth clips: a point's center may lie off screen.
		set := func(kind Primitive, index, datum int, p nt3dmath.Vec3, bias float32) {
			world := inst.World.Point(p)
			_, _, z, w := f.Matrix.Transform(world)
			if w <= 0 || z < 0 || z > w || z/w+bias > best {
				return
			}
			best = z/w + bias
			hit = &Hit{inst.Node, inst.Name, kind, index, datum, world, z / w}
		}
		for j, p := range g.Points {
			cx, cy, cz, cw := f.Matrix.Transform(inst.World.Point(p.Position))
			if cw <= 0 || cz < 0 || cz > cw {
				continue
			}
			px, py := (cx/cw+1)*float32(f.Width)*.5, (1-cy/cw)*float32(f.Height)*.5
			if r := max(tolerance, f.pointRadius(g.PointStyle, p.Radius, cw, 1)); (px-x)*(px-x)+(py-y)*(py-y) <= r*r {
				set(PointPrimitive, j, p.Datum, p.Position, 0)
			}
		}
		if index := s.pickIndex(f, i); index != nil {
			if tri, t, ok := index.nearest(&g, localRay, func(tri int, u, w float32) bool { return cutAt(&g, tri, u, w) }); ok {
				set(TrianglePrimitive, tri, tri, localRay.Origin.Add(localRay.Direction.Scale(t)), 0)
			}
		} else {
			for j := 0; j+2 < len(g.Indices); j += 3 {
				a, b, c := g.Vertices[g.Indices[j]].Position, g.Vertices[g.Indices[j+1]].Position, g.Vertices[g.Indices[j+2]].Position
				if t, u, w, ok := rayTriangle(localRay, a, b, c); ok && !cutAt(&g, j/3, u, w) {
					set(TrianglePrimitive, j/3, j/3, localRay.Origin.Add(localRay.Direction.Scale(t)), 0)
				}
			}
		}
		for j, b := range g.Boxes {
			if t, ok := rayBox(localRay, b); ok {
				set(BoxPrimitive, j, b.Datum, localRay.Origin.Add(localRay.Direction.Scale(t)), 0)
			}
		}
		// Strokes end square at their ends, as they are drawn.
		stroke := func(kind Primitive, index, datum int, a, b nt3dmath.Vec3, width, head, bias float32) {
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
			if projected.length < 1e-5 {
				if abs(rx) > limit || abs(ry) > limit {
					return
				}
			} else if along < 0 || along > projected.length || side > limit {
				return
			}
			t := float32(0)
			if projected.length > 1e-5 {
				t = max(0, min(1, along/projected.length))
			}
			t = projected.worldT(t)
			set(kind, index, datum, a.Scale(1-t).Add(b.Scale(t)), bias)
		}
		for j := 0; j+1 < len(g.Lines); j += 2 {
			stroke(LinePrimitive, j/2, j/2, g.Lines[j].Position, g.Lines[j+1].Position, g.LineWidth, 0, lineBias)
		}
		for j, a := range g.Arrows {
			stroke(ArrowPrimitive, j, a.Datum, a.Start, a.End, a.Width, a.HeadSize, 0)
		}
	}
	return hit
}

// rayTriangle follows the renderer's counterclockwise, front-face convention.
// It returns the ray parameter of the hit and its barycentric weights toward
// b and c.
func rayTriangle(r nt3dmath.Ray, a, b, c nt3dmath.Vec3) (t, u, w float32, ok bool) {
	edge1, edge2 := b.Sub(a), c.Sub(a)
	p := r.Direction.Cross(edge2)
	det := edge1.Dot(p)
	if det <= 1e-8 {
		return 0, 0, 0, false
	}
	v := r.Origin.Sub(a)
	u = v.Dot(p) / det
	if u < 0 || u > 1 {
		return 0, 0, 0, false
	}
	q := v.Cross(edge1)
	w = r.Direction.Dot(q) / det
	if w < 0 || u+w > 1 {
		return 0, 0, 0, false
	}
	t = edge2.Dot(q) / det
	return t, u, w, t >= 0
}

// cutAt reports whether a cutout material cuts out the point of triangle tri
// with barycentric weights u and w toward its second and third corners: its
// texture's alpha falls below the threshold, so renderers draw nothing there.
func cutAt(g *Geometry, tri int, u, w float32) bool {
	m := g.Material
	if m == nil || !m.Cutout || m.Texture == nil {
		return false
	}
	var uv UV
	if len(g.UV) > 0 {
		i := 3 * tri
		a, b, c := g.UV[g.Indices[i]], g.UV[g.Indices[i+1]], g.UV[g.Indices[i+2]]
		s := 1 - u - w
		uv = UV{s*a.U + u*b.U + w*c.U, s*a.V + u*b.V + w*c.V}
	}
	// Sampled and compared as the renderers sample a cutout: filtered at
	// full size, against the threshold itself, so a click goes through
	// where the picture does.
	return m.Texture.sampleLinear(uv, m.Repeat, 0)[3] < m.threshold()
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

// SetPickIndexing chooses whether Pick indexes large meshes. It does by
// default, which costs a background build of about a third of a second and
// 28 bytes a triangle (40 MB for 1.5M triangles), repaid when a mesh is
// picked often, as on hover. Turn it off where picks are rare; they then test every
// triangle, which takes milliseconds. Turning it off drops indexes already
// built, shared geometry's included; a build under way finishes and is
// discarded.
func (s *Scene) SetPickIndexing(on bool) {
	s.noPickIndex = !on
	if !on {
		for _, n := range s.nodes {
			n.pickIndex = nil
			if n.shared != nil {
				n.shared.pick = nil
			}
		}
	}
}

// SetNodePickIndexing chooses whether Pick indexes one node's mesh, as
// SetPickIndexing does for the whole scene; a node is indexed only when both
// allow it. The choice belongs to the node and outlasts its geometry, so a
// large mesh that is never picked, such as terrain or a backdrop, need not
// pay for an index while the models around it are indexed. Turning it off
// drops the node's own index, but not shared geometry's, which other nodes
// may still use.
func (s *Scene) SetNodePickIndexing(id NodeID, on bool) error {
	n, ok := s.nodes[id]
	if !ok {
		return fmt.Errorf("nt3d: unknown node %d", id)
	}
	n.noPickIndex = !on
	if !on {
		n.pickIndex = nil
	}
	return nil
}

// bvhSlot receives the triangle index of one geometry, built on another
// goroutine. Geometry is immutable once set, so the build only reads it; a
// slot belongs to one geometry and is dropped with it.
type bvhSlot struct {
	index atomic.Pointer[meshBVH]
	done  chan struct{}
}

// pickIndex returns the triangle index for a frame's geometry once it is
// built, or nil. The first pick of a large mesh starts the build and is
// answered triangle by triangle meanwhile: that takes milliseconds, and the
// build far longer, which a click should not wait for. Small meshes and
// frames that do not show the node's current geometry are never indexed.
func (s *Scene) pickIndex(f Frame, i int) *meshBVH {
	g := &f.Geometry[i]
	if s.noPickIndex || len(g.Indices)/3 < bvhLinear || len(f.GeometryRevisions) != len(f.Geometry) {
		return nil
	}
	n := s.nodes[instance(f, i).Node]
	if n == nil || n.noPickIndex || n.geometry == nil || n.geometryRevision != f.GeometryRevisions[i] {
		return nil
	}
	// Shared geometry is indexed once, for every node that shows it.
	slot := &n.pickIndex
	if n.shared != nil {
		slot = &n.shared.pick
	}
	if *slot == nil {
		built, geometry := &bvhSlot{done: make(chan struct{})}, n.geometry
		*slot = built
		go func() {
			built.index.Store(buildBVH(geometry))
			close(built.done)
		}()
	}
	return (*slot).index.Load()
}
