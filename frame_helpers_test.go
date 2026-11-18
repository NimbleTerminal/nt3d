package nt3d

import (
	"slices"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// The helpers below turn a frame a scene made into one built by hand, a part
// at a time. Each works on its own copy of the draws, so a frame copied from
// f keeps what it had.

// clearRevisions drops per-node caching: the draws are cached by the frame's
// Revision and their position.
func clearRevisions(f *Frame) {
	f.Draws = slices.Clone(f.Draws)
	for i := range f.Draws {
		f.Draws[i].Revision = 0
	}
}

// clearShared forgets which draws show one shared geometry.
func clearShared(f *Frame) {
	f.Draws = slices.Clone(f.Draws)
	for i := range f.Draws {
		f.Draws[i].Shared = 0
	}
}

// clearPlacements draws every geometry where it lies, visible, as no node's.
func clearPlacements(f *Frame) {
	f.Draws = slices.Clone(f.Draws)
	for i := range f.Draws {
		d := &f.Draws[i]
		*d = Draw{Geometry: d.Geometry, Revision: d.Revision, Shared: d.Shared}
	}
}

// drawsOf is a draw for each geometry, where it lies.
func drawsOf(geometry ...Geometry) []Draw {
	draws := make([]Draw, len(geometry))
	for i, g := range geometry {
		draws[i].Geometry = g
	}
	return draws
}

// placeAlone puts a frame's one draw at world, visible, as no node's.
func placeAlone(f *Frame, world nt3dmath.Mat4) {
	clearPlacements(f)
	f.Draws[0].World = world
}

// named is the node called name, which the test expects to exist.
func named(t testing.TB, s *Scene, name string) NodeID {
	t.Helper()
	id, ok := s.Find(name)
	if !ok {
		t.Fatalf("no node named %q", name)
	}
	return id
}
