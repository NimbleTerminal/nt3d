// Copyright (c) 2026 Neomantra Corp.

// Package nt3dcollide resolves Z-up kinematic capsule movement and ray queries
// against static triangle meshes.
//
// Coordinates are Z-up in float64 (nt3dmath.Vec3d), with products rounded
// explicitly as float64(a*b)+c so simulation results agree across platforms.
package nt3dcollide

import (
	"math"
	"slices"

	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

const (
	leafTriangles = 6
	eps           = 1e-9
)

// Capsule is a Z-aligned capsule whose base (lowest point) sits at a position.
// Height is the full vertical span from base to crown (>= 2*Radius).
type Capsule struct {
	Radius float64
	Height float64
}

// Segment returns the medial line segment [a, b] for a capsule with base pos.
func (c Capsule) Segment(pos nt3dmath.Vec3d) (a, b nt3dmath.Vec3d) {
	r := math.Max(1e-4, c.Radius)
	h := math.Max(float64(2*r), c.Height)
	a = nt3dmath.Vec3d{X: pos.X, Y: pos.Y, Z: pos.Z + r}
	b = nt3dmath.Vec3d{X: pos.X, Y: pos.Y, Z: pos.Z + h - r}
	return a, b
}

// Bounds returns the world-space AABB enclosing the capsule at base pos,
// expanded by pad (>= 0) on each axis.
func (c Capsule) Bounds(pos nt3dmath.Vec3d, pad float64) (lo, hi nt3dmath.Vec3d) {
	r := math.Max(1e-4, c.Radius)
	h := math.Max(float64(2*r), c.Height)
	p := math.Max(0, pad)
	rp := r + p
	lo = nt3dmath.Vec3d{X: pos.X - rp, Y: pos.Y - rp, Z: pos.Z - p}
	hi = nt3dmath.Vec3d{X: pos.X + rp, Y: pos.Y + rp, Z: pos.Z + h + p}
	return lo, hi
}

// Triangle is one world-space collision face and its unit normal.
type Triangle struct {
	A, B, C nt3dmath.Vec3d
	Normal  nt3dmath.Vec3d
	lo, hi  nt3dmath.Vec3d
}

type bvhNode struct {
	lo, hi       nt3dmath.Vec3d
	right        int32
	start, count int32
}

// Mesh is an immutable static triangle BVH for ray casts and capsule movement.
type Mesh struct {
	tris  []Triangle
	nodes []bvhNode
	lo    nt3dmath.Vec3d
	hi    nt3dmath.Vec3d
}

// NewMesh builds a collision mesh from g transformed by world (identity when
// world is the zero matrix). Degenerate triangles are skipped.
func NewMesh(g nt3d.Geometry, world nt3dmath.Mat4d) *Mesh {
	return NewMeshFromGeometries([]nt3d.Geometry{g}, []nt3dmath.Mat4d{world})
}

// NewMeshFromGeometries combines one or more geometries (with optional per-mesh
// world matrices; nil or zero entries mean identity) into a single BVH.
func NewMeshFromGeometries(gs []nt3d.Geometry, worlds []nt3dmath.Mat4d) *Mesh {
	var tris []Triangle
	id := nt3dmath.IdentityD()
	for gi, g := range gs {
		m := id
		if gi < len(worlds) && worlds[gi] != (nt3dmath.Mat4d{}) {
			m = worlds[gi]
		}
		for i := 0; i+2 < len(g.Indices); i += 3 {
			i0, i1, i2 := int(g.Indices[i]), int(g.Indices[i+1]), int(g.Indices[i+2])
			if i0 >= len(g.Vertices) || i1 >= len(g.Vertices) || i2 >= len(g.Vertices) {
				continue
			}
			a := transformPoint(m, g.Vertices[i0].Position.D())
			b := transformPoint(m, g.Vertices[i1].Position.D())
			c := transformPoint(m, g.Vertices[i2].Position.D())
			if t, ok := makeTriangle(a, b, c); ok {
				tris = append(tris, t)
			}
		}
	}
	return NewMeshFromTriangles(tris)
}

// NewMeshFromTriangles builds a BVH over world-space triangles.
func NewMeshFromTriangles(in []Triangle) *Mesh {
	tris := make([]Triangle, 0, len(in))
	for _, t := range in {
		if valid, ok := makeTriangle(t.A, t.B, t.C); ok {
			tris = append(tris, valid)
		}
	}
	m := &Mesh{tris: tris}
	if len(tris) == 0 {
		return m
	}
	m.nodes = make([]bvhNode, 0, 2*len(tris)/leafTriangles+2)
	centroids := make([]nt3dmath.Vec3d, len(tris))
	for i, t := range tris {
		centroids[i] = nt3dmath.Vec3d{
			X: (t.A.X + t.B.X + t.C.X) / 3,
			Y: (t.A.Y + t.B.Y + t.C.Y) / 3,
			Z: (t.A.Z + t.B.Z + t.C.Z) / 3,
		}
	}
	m.build(centroids, 0, len(tris))
	m.lo, m.hi = m.nodes[0].lo, m.nodes[0].hi
	return m
}

// TriangleCount returns the number of non-degenerate triangles in m.
func (m *Mesh) TriangleCount() int {
	if m == nil {
		return 0
	}
	return len(m.tris)
}

// Bounds returns the world-space AABB of m, and false when m is empty.
func (m *Mesh) Bounds() (lo, hi nt3dmath.Vec3d, ok bool) {
	if m == nil || len(m.tris) == 0 {
		return nt3dmath.Vec3d{}, nt3dmath.Vec3d{}, false
	}
	return m.lo, m.hi, true
}

func (m *Mesh) build(centroids []nt3dmath.Vec3d, lo, hi int) int32 {
	bLo, bHi := m.tris[lo].lo, m.tris[lo].hi
	cLo, cHi := centroids[lo], centroids[lo]
	for i := lo + 1; i < hi; i++ {
		bLo = vecMin(bLo, m.tris[i].lo)
		bHi = vecMax(bHi, m.tris[i].hi)
		cLo = vecMin(cLo, centroids[i])
		cHi = vecMax(cHi, centroids[i])
	}
	idx := int32(len(m.nodes))
	m.nodes = append(m.nodes, bvhNode{lo: bLo, hi: bHi, start: int32(lo), count: int32(hi - lo)})
	ext := [3]float64{cHi.X - cLo.X, cHi.Y - cLo.Y, cHi.Z - cLo.Z}
	axis := 0
	if ext[1] > ext[axis] {
		axis = 1
	}
	if ext[2] > ext[axis] {
		axis = 2
	}
	if hi-lo <= leafTriangles || ext[axis] <= 1e-9 {
		return idx
	}
	coord := func(v nt3dmath.Vec3d) float64 {
		switch axis {
		case 1:
			return v.Y
		case 2:
			return v.Z
		default:
			return v.X
		}
	}
	split := coord(cLo) + float64(0.5*ext[axis])
	mid := lo
	for i := lo; i < hi; i++ {
		if coord(centroids[i]) < split {
			m.tris[i], m.tris[mid] = m.tris[mid], m.tris[i]
			centroids[i], centroids[mid] = centroids[mid], centroids[i]
			mid++
		}
	}
	if mid == lo || mid == hi {
		type pair struct {
			t Triangle
			c nt3dmath.Vec3d
		}
		ps := make([]pair, hi-lo)
		for i := range ps {
			ps[i] = pair{m.tris[lo+i], centroids[lo+i]}
		}
		slices.SortFunc(ps, func(a, b pair) int {
			ca, cb := coord(a.c), coord(b.c)
			switch {
			case ca < cb:
				return -1
			case ca > cb:
				return 1
			default:
				return 0
			}
		})
		for i, p := range ps {
			m.tris[lo+i], centroids[lo+i] = p.t, p.c
		}
		mid = (lo + hi) / 2
	}
	m.nodes[idx].count = 0
	m.build(centroids, lo, mid)
	right := m.build(centroids, mid, hi)
	m.nodes[idx].right = right
	return idx
}

func (m *Mesh) queryBox(lo, hi nt3dmath.Vec3d, visit func(Triangle)) {
	if m == nil || len(m.nodes) == 0 {
		return
	}
	stack := make([]int32, 1, 64)
	for len(stack) > 0 {
		idx := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := m.nodes[idx]
		if !boxesOverlap(lo, hi, n.lo, n.hi) {
			continue
		}
		if n.count > 0 {
			for _, t := range m.tris[n.start : n.start+n.count] {
				if boxesOverlap(lo, hi, t.lo, t.hi) {
					visit(t)
				}
			}
			continue
		}
		stack = append(stack, n.right, idx+1)
	}
}

// RayHit describes the nearest triangle intersection along a ray.
type RayHit struct {
	Position nt3dmath.Vec3d
	Normal   nt3dmath.Vec3d
	Distance float64
}

// RayCast finds the closest hit along origin + t*dir (both sides of triangles
// are tested so thin walls and open floors never leak) within [0, maxDist].
func (m *Mesh) RayCast(origin, dir nt3dmath.Vec3d, maxDist float64) (RayHit, bool) {
	if m == nil || len(m.nodes) == 0 || !(maxDist > 0) || !finiteVec(origin) || !finiteVec(dir) {
		return RayHit{}, false
	}
	lenDir := vecLen(dir)
	if !(lenDir > 1e-12) {
		return RayHit{}, false
	}
	d := vecScale(dir, 1/lenDir)
	best := maxDist
	var hit RayHit
	found := false

	stack := make([]int32, 1, 64)
	for len(stack) > 0 {
		idx := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := m.nodes[idx]
		if _, ok := rayEnterBox(origin, d, n.lo, n.hi, best); !ok {
			continue
		}
		if n.count > 0 {
			for _, tri := range m.tris[n.start : n.start+n.count] {
				t, ok := rayTriangleTwoSided(origin, d, tri, best)
				if !ok {
					continue
				}
				best = t
				norm := tri.Normal
				if dot(norm, d) > 0 {
					norm = vecScale(norm, -1)
				}
				hit = RayHit{
					Position: vecAdd(origin, vecScale(d, t)),
					Normal:   norm,
					Distance: t,
				}
				found = true
			}
			continue
		}
		left, right := idx+1, n.right
		lt, lok := rayEnterBox(origin, d, m.nodes[left].lo, m.nodes[left].hi, best)
		rt, rok := rayEnterBox(origin, d, m.nodes[right].lo, m.nodes[right].hi, best)
		switch {
		case lok && rok && lt <= rt:
			stack = append(stack, right, left)
		case lok && rok:
			stack = append(stack, left, right)
		case lok:
			stack = append(stack, left)
		case rok:
			stack = append(stack, right)
		}
	}
	return hit, found
}

func makeTriangle(a, b, c nt3dmath.Vec3d) (Triangle, bool) {
	if !finiteVec(a) || !finiteVec(b) || !finiteVec(c) {
		return Triangle{}, false
	}
	ab := vecSub(b, a)
	ac := vecSub(c, a)
	n := cross(ab, ac)
	l := vecLen(n)
	if !(l > 1e-12) {
		return Triangle{}, false
	}
	return Triangle{
		A:      a,
		B:      b,
		C:      c,
		Normal: vecScale(n, 1/l),
		lo:     vecMin(a, vecMin(b, c)),
		hi:     vecMax(a, vecMax(b, c)),
	}, true
}

func transformPoint(m nt3dmath.Mat4d, p nt3dmath.Vec3d) nt3dmath.Vec3d {
	x := float64(m[0]*p.X) + float64(m[4]*p.Y) + float64(m[8]*p.Z) + m[12]
	y := float64(m[1]*p.X) + float64(m[5]*p.Y) + float64(m[9]*p.Z) + m[13]
	z := float64(m[2]*p.X) + float64(m[6]*p.Y) + float64(m[10]*p.Z) + m[14]
	return nt3dmath.Vec3d{X: x, Y: y, Z: z}
}

func finiteVec(v nt3dmath.Vec3d) bool {
	return nt3dmath.IsFinite(v.X) && nt3dmath.IsFinite(v.Y) && nt3dmath.IsFinite(v.Z)
}

func vecAdd(a, b nt3dmath.Vec3d) nt3dmath.Vec3d {
	return nt3dmath.Vec3d{X: a.X + b.X, Y: a.Y + b.Y, Z: a.Z + b.Z}
}

func vecSub(a, b nt3dmath.Vec3d) nt3dmath.Vec3d {
	return nt3dmath.Vec3d{X: a.X - b.X, Y: a.Y - b.Y, Z: a.Z - b.Z}
}

func vecScale(v nt3dmath.Vec3d, s float64) nt3dmath.Vec3d {
	return nt3dmath.Vec3d{X: float64(v.X * s), Y: float64(v.Y * s), Z: float64(v.Z * s)}
}

func dot(a, b nt3dmath.Vec3d) float64 {
	return float64(a.X*b.X) + float64(a.Y*b.Y) + float64(a.Z*b.Z)
}

func cross(a, b nt3dmath.Vec3d) nt3dmath.Vec3d {
	return nt3dmath.Vec3d{
		X: float64(a.Y*b.Z) - float64(a.Z*b.Y),
		Y: float64(a.Z*b.X) - float64(a.X*b.Z),
		Z: float64(a.X*b.Y) - float64(a.Y*b.X),
	}
}

func vecLenSq(v nt3dmath.Vec3d) float64 { return dot(v, v) }
func vecLen(v nt3dmath.Vec3d) float64   { return math.Sqrt(vecLenSq(v)) }

func vecMin(a, b nt3dmath.Vec3d) nt3dmath.Vec3d {
	return nt3dmath.Vec3d{X: math.Min(a.X, b.X), Y: math.Min(a.Y, b.Y), Z: math.Min(a.Z, b.Z)}
}

func vecMax(a, b nt3dmath.Vec3d) nt3dmath.Vec3d {
	return nt3dmath.Vec3d{X: math.Max(a.X, b.X), Y: math.Max(a.Y, b.Y), Z: math.Max(a.Z, b.Z)}
}

func boxesOverlap(aLo, aHi, bLo, bHi nt3dmath.Vec3d) bool {
	return aLo.X <= bHi.X && aHi.X >= bLo.X &&
		aLo.Y <= bHi.Y && aHi.Y >= bLo.Y &&
		aLo.Z <= bHi.Z && aHi.Z >= bLo.Z
}

func rayEnterBox(origin, dir, lo, hi nt3dmath.Vec3d, maxT float64) (float64, bool) {
	tMin, tMax := 0.0, maxT
	o := [3]float64{origin.X, origin.Y, origin.Z}
	d := [3]float64{dir.X, dir.Y, dir.Z}
	bLo := [3]float64{lo.X, lo.Y, lo.Z}
	bHi := [3]float64{hi.X, hi.Y, hi.Z}
	for k := range 3 {
		if math.Abs(d[k]) <= 1e-15 {
			if o[k] < bLo[k] || o[k] > bHi[k] {
				return 0, false
			}
			continue
		}
		inv := 1 / d[k]
		t0 := float64((bLo[k] - o[k]) * inv)
		t1 := float64((bHi[k] - o[k]) * inv)
		if t0 > t1 {
			t0, t1 = t1, t0
		}
		tMin = math.Max(tMin, t0)
		tMax = math.Min(tMax, t1)
		if tMax < tMin {
			return 0, false
		}
	}
	return tMin, true
}

func rayTriangleTwoSided(origin, dir nt3dmath.Vec3d, tri Triangle, maxT float64) (float64, bool) {
	e1 := vecSub(tri.B, tri.A)
	e2 := vecSub(tri.C, tri.A)
	p := cross(dir, e2)
	det := dot(e1, p)
	if math.Abs(det) <= 1e-12 {
		return 0, false
	}
	invDet := 1 / det
	s := vecSub(origin, tri.A)
	u := float64(dot(s, p) * invDet)
	if u < -1e-9 || u > 1+1e-9 {
		return 0, false
	}
	q := cross(s, e1)
	v := float64(dot(dir, q) * invDet)
	if v < -1e-9 || u+v > 1+1e-9 {
		return 0, false
	}
	t := float64(dot(e2, q) * invDet)
	if t < 0 || t > maxT {
		return 0, false
	}
	return t, true
}
