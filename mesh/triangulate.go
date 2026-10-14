// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import "nimbleterminal.dev/nt3d/math"

// maxEarClip is the most corners a polygon is ear-clipped with. Ear clipping
// takes time cubic in the corners at worst; larger polygons, which are rare,
// are split as fans.
const maxEarClip = 256

// triangulate splits a polygon, given by its corners' positions in order,
// into triangles wound as it is, returned as corner indices. Convex polygons
// become fans. Others are ear-clipped in the plane that best fits them, which
// splits concave polygons correctly where a fan would cover what lies outside
// them. Polygons that are degenerate, self-intersecting, or too large fall
// back to fans.
func triangulate(points []nt3dmath.Vec3) [][3]int {
	n := len(points)
	fan := func(corners []int) (out [][3]int) {
		for k := 1; k+1 < len(corners); k++ {
			out = append(out, [3]int{corners[0], corners[k], corners[k+1]})
		}
		return out
	}
	all := make([]int, n)
	for i := range all {
		all[i] = i
	}
	if n <= 3 {
		return fan(all)
	}
	// Newell's normal: the polygon's area-weighted plane, robust to a few
	// corners out of it.
	var normal [3]float64
	for i := range n {
		p, q := points[i], points[(i+1)%n]
		normal[0] += float64(p.Y-q.Y) * float64(p.Z+q.Z)
		normal[1] += float64(p.Z-q.Z) * float64(p.X+q.X)
		normal[2] += float64(p.X-q.X) * float64(p.Y+q.Y)
	}
	// Drop the axis the normal leans along most; the next two, in cyclic order,
	// wind the polygon counter-clockwise when that component is positive.
	axis := 0
	for k := 1; k < 3; k++ {
		if abs64(normal[k]) > abs64(normal[axis]) {
			axis = k
		}
	}
	if normal[axis] == 0 || n > maxEarClip {
		return fan(all)
	}
	sign := 1.0
	if normal[axis] < 0 {
		sign = -1
	}
	flat := make([][2]float64, n)
	for i, p := range points {
		c := [3]float64{float64(p.X), float64(p.Y), float64(p.Z)}
		flat[i] = [2]float64{c[(axis+1)%3], c[(axis+2)%3]}
	}
	// turn is positive where a, b, c turn the polygon's way.
	turn := func(a, b, c int) float64 {
		p, q, r := flat[a], flat[b], flat[c]
		return sign * ((q[0]-p[0])*(r[1]-p[1]) - (q[1]-p[1])*(r[0]-p[0]))
	}
	convex := true
	for i := range n {
		if turn(i, (i+1)%n, (i+2)%n) < 0 {
			convex = false
			break
		}
	}
	if convex {
		return fan(all)
	}
	inside := func(p, a, b, c int) bool {
		if flat[p] == flat[a] || flat[p] == flat[b] || flat[p] == flat[c] {
			return false
		}
		return turn(a, b, p) >= 0 && turn(b, c, p) >= 0 && turn(c, a, p) >= 0
	}
	var out [][3]int
	left := all
	for len(left) > 3 {
		clipped := false
		for i := range left {
			a, b, c := left[(i+len(left)-1)%len(left)], left[i], left[(i+1)%len(left)]
			if turn(a, b, c) <= 0 {
				continue // Reflex or straight: not an ear.
			}
			ear := true
			for _, p := range left {
				if p != a && p != b && p != c && inside(p, a, b, c) {
					ear = false
					break
				}
			}
			if ear {
				out = append(out, [3]int{a, b, c})
				left = append(left[:i:i], left[i+1:]...)
				clipped = true
				break
			}
		}
		if clipped {
			continue
		}
		// No ear: drop a corner on a straight run, which adds no area,
		// else the polygon crosses itself; fan what is left.
		straight := -1
		for i := range left {
			if turn(left[(i+len(left)-1)%len(left)], left[i], left[(i+1)%len(left)]) == 0 {
				straight = i
				break
			}
		}
		if straight < 0 {
			return append(out, fan(left)...)
		}
		left = append(left[:straight:straight], left[straight+1:]...)
	}
	return append(out, [3]int{left[0], left[1], left[2]})
}

func abs64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
