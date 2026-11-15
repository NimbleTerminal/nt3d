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

// creased reports whether a geometry draws crease lines: an outlined indexed
// triangle mesh with a positive crease angle.
func creased(g *Geometry) bool {
	return g.Outline.Width > 0 && g.Outline.Crease > 0 && len(g.Indices) >= 3
}

// creaseVertex is one endpoint of an extracted crease segment, carrying its
// local position, vertex normal, UV, and vertex alpha opacity (0..1).
type creaseVertex struct {
	pos, normal nt3dmath.Vec3
	uv          UV
	opacity     float32
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
	segments := creaseSegments(vertices, indices, nil, degrees)
	if len(segments) == 0 {
		return nil
	}
	out := make([]nt3dmath.Vec3, len(segments))
	for i, s := range segments {
		out[i] = s.pos
	}
	return out
}

func creaseSegments(vertices []Vertex, indices []uint32, uvs []UV, degrees float32) []creaseVertex {
	if !(degrees > 0) || len(indices) < 3 {
		return nil
	}
	type key [2]nt3dmath.Vec3
	type edge struct {
		normal nt3dmath.Vec3 // The first face's.
		a, b   creaseVertex
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
	endpoint := func(idx uint32) creaseVertex {
		v := vertices[idx]
		var uv UV
		if int(idx) < len(uvs) {
			uv = uvs[idx]
		}
		return creaseVertex{
			pos:     v.Position,
			normal:  v.Normal,
			uv:      uv,
			opacity: float32(v.Color.A) / 255,
		}
	}
	limit := float32(math.Cos(float64(degrees) * math.Pi / 180))
	edges := make(map[key]*edge, len(indices))
	order := make([]key, 0, len(indices))
	for i := 0; i+2 < len(indices); i += 3 {
		idx := [3]uint32{indices[i], indices[i+1], indices[i+2]}
		p := [3]nt3dmath.Vec3{vertices[idx[0]].Position, vertices[idx[1]].Position, vertices[idx[2]].Position}
		n := p[1].Sub(p[0]).Cross(p[2].Sub(p[0]))
		l := float32(math.Sqrt(float64(n.Dot(n))))
		if !(l > 0) || !validFloat(l) {
			continue
		}
		n = n.Scale(1 / l)
		for c := range 3 {
			i0, i1 := idx[c], idx[(c+1)%3]
			k := key{p[c], p[(c+1)%3]}
			if k[0] == k[1] {
				continue
			}
			if less(k[1], k[0]) {
				k[0], k[1] = k[1], k[0]
				i0, i1 = i1, i0
			}
			e := edges[k]
			if e == nil {
				edges[k] = &edge{normal: n, a: endpoint(i0), b: endpoint(i1), faces: 1}
				order = append(order, k)
				continue
			}
			e.faces++
			e.sharp = e.sharp || e.normal.Dot(n) < limit
		}
	}
	var out []creaseVertex
	for _, k := range order {
		if e := edges[k]; e.faces != 2 || e.sharp {
			out = append(out, e.a, e.b)
		}
	}
	return out
}

// creaseKey names a mesh's arrays. Its addresses are only compared:
// creaseEntry's weak pointers tell a live array from another that later took
// its address.
type creaseKey struct {
	vertices, indices, uvs uintptr
	nv, ni, nuv            int
}

// creaseEntry is a mesh's crease edges at the last two angles asked of it:
// enough to flip between two looks, without keeping an angle slider's every
// step.
type creaseEntry struct {
	vertices weak.Pointer[Vertex]
	indices  weak.Pointer[uint32]
	uvs      weak.Pointer[UV]
	hasUV    bool
	angles   [2]creaseAngle
	next     int // The slot the next new angle replaces.
}

type creaseAngle struct {
	degrees  float32 // Zero is an empty slot: no mesh is creased at zero.
	segments []creaseVertex
}

// creaseCache keeps each mesh's crease edges between frames, for the software
// renderer, which has no per-geometry state of its own. Entries whose arrays
// were collected are swept as the cache grows.
var creaseCache = struct {
	sync.Mutex
	entries map[creaseKey]*creaseEntry
	sweepAt int
}{entries: map[creaseKey]*creaseEntry{}, sweepAt: 256}

// creaseExtractions counts creaseSegments runs made for the cache.
var creaseExtractions atomic.Int64

// cachedCreaseSegments is creaseSegments for a creased geometry, kept between
// frames when its vertex slice is stable. Dynamically displaced vertex slices
// bypass the cache so per-frame allocations do not accumulate.
func cachedCreaseSegments(g *Geometry, verts []Vertex) []creaseVertex {
	if len(verts) == 0 || len(g.Indices) == 0 {
		return nil
	}
	if g.Displacement != nil {
		return creaseSegments(verts, g.Indices, g.UV, g.Outline.Crease)
	}
	v, i := unsafe.SliceData(verts), unsafe.SliceData(g.Indices)
	var u *UV
	if len(g.UV) > 0 {
		u = unsafe.SliceData(g.UV)
	}
	key := creaseKey{
		vertices: uintptr(unsafe.Pointer(v)),
		indices:  uintptr(unsafe.Pointer(i)),
		uvs:      uintptr(unsafe.Pointer(u)),
		nv:       len(verts),
		ni:       len(g.Indices),
		nuv:      len(g.UV),
	}
	creaseCache.Lock()
	defer creaseCache.Unlock()
	e := creaseCache.entries[key]
	if e != nil && (e.vertices.Value() != v || e.indices.Value() != i || (e.hasUV && e.uvs.Value() != u)) {
		e = nil
	}
	if e == nil {
		if len(creaseCache.entries) >= creaseCache.sweepAt {
			for k, old := range creaseCache.entries {
				if old.vertices.Value() == nil || old.indices.Value() == nil || (old.hasUV && old.uvs.Value() == nil) {
					delete(creaseCache.entries, k)
				}
			}
			creaseCache.sweepAt = max(256, 2*len(creaseCache.entries))
		}
		e = &creaseEntry{vertices: weak.Make(v), indices: weak.Make(i), hasUV: u != nil}
		if u != nil {
			e.uvs = weak.Make(u)
		}
		creaseCache.entries[key] = e
	}
	for _, a := range e.angles {
		if a.degrees == g.Outline.Crease {
			return a.segments
		}
	}
	creaseExtractions.Add(1)
	segments := creaseSegments(verts, g.Indices, g.UV, g.Outline.Crease)
	e.angles[e.next] = creaseAngle{g.Outline.Crease, segments}
	e.next = (e.next + 1) % len(e.angles)
	return segments
}
