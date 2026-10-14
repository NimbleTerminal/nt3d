// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"math"

	"nimbleterminal.dev/nt3d/math"
)

// Wireframe depth is sampled at 2×4 points per cell, which are about square.
const (
	wireSubX, wireSubY = 2, 4
	// wireDense is the mean on-screen area, in cells, below which a mesh's
	// triangles are too small for their edges to read; its feature lines are
	// drawn instead.
	wireDense = 2
	// wireCrease is the cosine of the angle between neighboring samples'
	// normals beyond which they lie on either side of an edge.
	wireCrease = .766 // 40°
	// wireJump is the relative depth step between neighboring samples of one
	// mesh that marks an occluding contour.
	wireJump = .05
	// wireSlack is the relative depth by which a line may lie behind the
	// surface in its cell and still be seen, as lines on that surface do.
	wireSlack = .03
)

// wireDepth is the nearest front face at each sample: its depth along the
// view, world normal, and geometry. Depth is the view distance for
// perspective projections and the depth coordinate otherwise; both grow
// away from the viewer.
type wireDepth struct {
	cols, rows, w, h int
	projective       bool
	depth            []float32
	normal           []nt3dmath.Vec3
	owner            []int32
	far              []float32 // Per cell: the farthest sample's depth; +Inf where any is empty.
	area             []float32 // Per geometry: front faces' total area, in cells.
	faces            []int     // Per geometry: front faces drawn.
}

func newWireDepth(f Frame, cols, rows int) *wireDepth {
	w, h := cols*wireSubX, rows*wireSubY
	d := &wireDepth{cols: cols, rows: rows, w: w, h: h, projective: f.Matrix[3] != 0 || f.Matrix[7] != 0 || f.Matrix[11] != 0,
		depth: make([]float32, w*h), normal: make([]nt3dmath.Vec3, w*h), owner: make([]int32, w*h), far: make([]float32, cols*rows),
		area: make([]float32, len(f.Geometry)), faces: make([]int, len(f.Geometry))}
	inf := float32(math.Inf(1))
	for i := range d.depth {
		d.depth[i], d.owner[i] = inf, -1
	}
	for gi, g := range f.Geometry {
		inst := instance(f, gi)
		if !inst.Visible || edgeOn(g) {
			continue // Edge-on geometry depends on a view wireframe does not draw.
		}
		m := f.Matrix.Mul(inst.World)
		inverse, _ := inst.World.InverseAffine()
		normals := inverse.Transpose()
		for i := 0; i+2 < len(g.Indices); i += 3 {
			d.triangle(gi, &m, &normals, g.Vertices[g.Indices[i]].Position, g.Vertices[g.Indices[i+1]].Position, g.Vertices[g.Indices[i+2]].Position)
		}
		for _, b := range g.Boxes {
			v := boxVertices(b)
			for j := 0; j < len(v); j += 3 {
				d.triangle(gi, &m, &normals, v[j].Position, v[j+1].Position, v[j+2].Position)
			}
		}
	}
	for cy := range rows {
		for cx := range cols {
			far := float32(0)
			for sy := cy * wireSubY; sy < (cy+1)*wireSubY; sy++ {
				for sx := cx * wireSubX; sx < (cx+1)*wireSubX; sx++ {
					far = max(far, d.depth[sy*w+sx])
				}
			}
			d.far[cy*cols+cx] = far
		}
	}
	return d
}

// value is a corner's depth quantity that is affine across the screen.
func (d *wireDepth) value(z, w float32) float32 {
	if d.projective {
		return 1 / w
	}
	return z / w
}
func (d *wireDepth) linear(value float32) float32 {
	if d.projective {
		return 1 / value
	}
	return value
}

// triangle records a local-space triangle's front face, if it has one, cut
// at the near plane as the renderers cut it.
func (d *wireDepth) triangle(gi int, m, normals *nt3dmath.Mat4, a, b, c nt3dmath.Vec3) {
	var corners [3]clipCorner
	for k, p := range [3]nt3dmath.Vec3{a, b, c} {
		corners[k].x, corners[k].y, corners[k].z, corners[k].w = m.Transform(p)
	}
	n := lazyNormal{a: a, b: b, c: c, normals: normals}
	if corners[0].z >= 0 && corners[1].z >= 0 && corners[2].z >= 0 {
		if d.fill(gi, &corners[0], &corners[1], &corners[2], &n) {
			d.faces[gi]++
		}
		return
	}
	var storage [4]clipCorner
	polygon := clipNear(corners, &storage)
	front := false
	for k := 1; k+1 < len(polygon); k++ {
		if d.fill(gi, &polygon[0], &polygon[k], &polygon[k+1], &n) {
			front = true
		}
	}
	if front {
		d.faces[gi]++
	}
}

// lazyNormal is a face's world normal, found when a sample first needs it.
type lazyNormal struct {
	a, b, c nt3dmath.Vec3
	normals *nt3dmath.Mat4
	n       nt3dmath.Vec3
	done    bool
}

func (l *lazyNormal) get() nt3dmath.Vec3 {
	if !l.done {
		l.n, l.done = l.normals.Direction(l.b.Sub(l.a).Cross(l.c.Sub(l.a))).Normalize(), true
	}
	return l.n
}

// fill rasterizes a clip-space triangle past the near plane, reporting
// whether it faces the viewer.
func (d *wireDepth) fill(gi int, a, b, c *clipCorner, normal *lazyNormal) bool {
	if a.w <= 0 || b.w <= 0 || c.w <= 0 {
		return false
	}
	var sx, sy, q [3]float32
	for k, p := range [3]*clipCorner{a, b, c} {
		sx[k], sy[k], q[k] = (p.x/p.w+1)*float32(d.w)*.5, (1-p.y/p.w)*float32(d.h)*.5, d.value(p.z, p.w)
	}
	area := (sx[1]-sx[0])*(sy[2]-sy[0]) - (sy[1]-sy[0])*(sx[2]-sx[0])
	if area >= -1e-9 {
		return false // Back-facing or degenerate: counter-clockwise faces turn clockwise with Y down.
	}
	d.area[gi] += -area / 2 / (wireSubX * wireSubY)
	loX, hiX := max(0, int(math.Ceil(float64(min(sx[0], sx[1], sx[2])-.5)))), min(d.w-1, int(math.Floor(float64(max(sx[0], sx[1], sx[2])-.5))))
	loY, hiY := max(0, int(math.Ceil(float64(min(sy[0], sy[1], sy[2])-.5)))), min(d.h-1, int(math.Floor(float64(max(sy[0], sy[1], sy[2])-.5))))
	for y := loY; y <= hiY; y++ {
		for x := loX; x <= hiX; x++ {
			px, py := float32(x)+.5, float32(y)+.5
			u := ((sx[1]-px)*(sy[2]-py) - (sy[1]-py)*(sx[2]-px)) / area
			v := ((sx[2]-px)*(sy[0]-py) - (sy[2]-py)*(sx[0]-px)) / area
			t := 1 - u - v
			if min(u, v, t) < 0 {
				continue
			}
			i := y*d.w + x
			if depth := d.linear(u*q[0] + v*q[1] + t*q[2]); depth < d.depth[i] {
				d.depth[i], d.normal[i], d.owner[i] = depth, normal.get(), int32(gi)
			}
		}
	}
	return true
}

// dense reports whether a geometry's front faces are too small for their edges.
func (d *wireDepth) dense(gi int) bool {
	return d.faces[gi] > 0 && d.area[gi] < wireDense*float32(d.faces[gi])
}

// hidden reports whether a line point at a cell lies behind the surface there.
func (d *wireDepth) hidden(cx, cy int, depth float32) bool {
	if cx < 0 || cy < 0 || cx >= d.cols || cy >= d.rows {
		return false
	}
	far := d.far[cy*d.cols+cx]
	return depth > far*(1+wireSlack)+1e-6
}

// features visits the cells holding dense meshes' silhouettes, creases, and
// occluding contours: samples whose neighbor is empty, farther and another
// geometry's, at a sharp angle, or well behind.
func (d *wireDepth) features(visit func(cx, cy int)) {
	marked := make([]bool, d.cols*d.rows)
	for y := range d.h {
		for x := range d.w {
			i := y*d.w + x
			g := d.owner[i]
			if g < 0 || !d.dense(int(g)) || marked[(y/wireSubY)*d.cols+x/wireSubX] {
				continue
			}
			edge := false
			for _, step := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				nx, ny := x+step[0], y+step[1]
				if nx < 0 || ny < 0 || nx >= d.w || ny >= d.h {
					continue
				}
				j := ny*d.w + nx
				switch {
				case d.owner[j] < 0:
					edge = true
				case d.owner[j] != g:
					edge = d.depth[j] > d.depth[i]
				case d.depth[j] >= d.depth[i]:
					edge = d.normal[i].Dot(d.normal[j]) < wireCrease || d.depth[j] > d.depth[i]*(1+wireJump)
				}
				if edge {
					break
				}
			}
			if edge {
				marked[(y/wireSubY)*d.cols+x/wireSubX] = true
			}
		}
	}
	for i, m := range marked {
		if m {
			visit(i%d.cols, i/d.cols)
		}
	}
}
