// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"math"
	"sync"
	"sync/atomic"
	"unsafe"
	"weak"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// creased reports whether a geometry draws crease lines: an outlined, opaque,
// rigid triangle mesh with a crease angle. Transparent, cutout, transmitting,
// and deformed meshes draw only their hull.
func creased(g *Geometry) bool {
	if g.Outline.Width <= 0 || g.Outline.Crease <= 0 || len(g.Indices) == 0 || g.posed != nil {
		return false
	}
	return !transparent(g) && !transmitting(g) && (g.Material == nil || !g.Material.Cutout)
}

// creaseEdges returns the edges of an indexed triangle mesh that get a crease
// line, as local-space endpoint pairs: boundary edges, with one face; edges
// whose two faces turn by more than degrees; and edges with three or more
// faces. It is nil when degrees is not positive or no edge qualifies.
//
// Edges join vertices by position, as outlineNormals does, so a mesh whose
// faces have their own vertices still shares its edges; a face's direction is
// its geometric normal, not its vertices'; triangles without area are
// skipped. Edges come in the order the indices first name them.
func creaseEdges(vertices []Vertex, indices []uint32, degrees float32) []nt3dmath.Vec3 {
	if !(degrees > 0) || len(indices) < 3 {
		return nil
	}
	type key [2]nt3dmath.Vec3
	type edge struct {
		normal nt3dmath.Vec3 // The first face's.
		faces  int
		sharp  bool
	}
	less := func(a, b nt3dmath.Vec3) bool {
		if a.X != b.X {
			return a.X < b.X
		}
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.Z < b.Z
	}
	limit := float32(math.Cos(float64(degrees) * math.Pi / 180))
	edges := make(map[key]*edge, len(indices))
	order := make([]key, 0, len(indices))
	for i := 0; i+2 < len(indices); i += 3 {
		p := [3]nt3dmath.Vec3{vertices[indices[i]].Position, vertices[indices[i+1]].Position, vertices[indices[i+2]].Position}
		n := p[1].Sub(p[0]).Cross(p[2].Sub(p[0]))
		l := float32(math.Sqrt(float64(n.Dot(n))))
		if !(l > 0) || !validFloat(l) {
			continue
		}
		n = n.Scale(1 / l)
		for c := range 3 {
			k := key{p[c], p[(c+1)%3]}
			if k[0] == k[1] {
				continue
			}
			if less(k[1], k[0]) {
				k[0], k[1] = k[1], k[0]
			}
			e := edges[k]
			if e == nil {
				edges[k] = &edge{normal: n, faces: 1}
				order = append(order, k)
				continue
			}
			e.faces++
			e.sharp = e.sharp || e.normal.Dot(n) < limit
		}
	}
	var out []nt3dmath.Vec3
	for _, k := range order {
		if e := edges[k]; e.faces != 2 || e.sharp {
			out = append(out, k[0], k[1])
		}
	}
	return out
}

// creaseKey names a mesh's arrays. Its addresses are only compared:
// creaseEntry's weak pointers tell a live array from another that later took
// its address.
type creaseKey struct {
	vertices, indices uintptr
	nv, ni            int
}

// creaseEntry is a mesh's crease edges at the last two angles asked of it:
// enough to flip between two looks, without keeping an angle slider's every
// step.
type creaseEntry struct {
	vertices weak.Pointer[Vertex]
	indices  weak.Pointer[uint32]
	angles   [2]creaseAngle
	next     int // The slot the next new angle replaces.
}

type creaseAngle struct {
	degrees float32 // Zero is an empty slot: no mesh is creased at zero.
	edges   []nt3dmath.Vec3
}

// creaseCache keeps each mesh's crease edges between frames, for the software
// renderer, which has no per-geometry state of its own. Meshes are immutable
// once drawn, so an entry holds while its arrays live; entries whose arrays
// were collected are swept as the cache grows.
var creaseCache = struct {
	sync.Mutex
	entries map[creaseKey]*creaseEntry
	sweepAt int
}{entries: map[creaseKey]*creaseEntry{}, sweepAt: 256}

// creaseExtractions counts creaseEdges runs made for the cache.
var creaseExtractions atomic.Int64

// cachedCreaseEdges is creaseEdges for a creased geometry, kept between frames.
func cachedCreaseEdges(g *Geometry) []nt3dmath.Vec3 {
	if len(g.Vertices) == 0 || len(g.Indices) == 0 {
		return nil
	}
	v, i := unsafe.SliceData(g.Vertices), unsafe.SliceData(g.Indices)
	key := creaseKey{uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(i)), len(g.Vertices), len(g.Indices)}
	creaseCache.Lock()
	defer creaseCache.Unlock()
	e := creaseCache.entries[key]
	if e != nil && (e.vertices.Value() != v || e.indices.Value() != i) {
		e = nil
	}
	if e == nil {
		if len(creaseCache.entries) >= creaseCache.sweepAt {
			for k, old := range creaseCache.entries {
				if old.vertices.Value() == nil || old.indices.Value() == nil {
					delete(creaseCache.entries, k)
				}
			}
			creaseCache.sweepAt = max(256, 2*len(creaseCache.entries))
		}
		e = &creaseEntry{vertices: weak.Make(v), indices: weak.Make(i)}
		creaseCache.entries[key] = e
	}
	for _, a := range e.angles {
		if a.degrees == g.Outline.Crease {
			return a.edges
		}
	}
	creaseExtractions.Add(1)
	edges := creaseEdges(g.Vertices, g.Indices, g.Outline.Crease)
	e.angles[e.next] = creaseAngle{g.Outline.Crease, edges}
	e.next = (e.next + 1) % len(e.angles)
	return edges
}
