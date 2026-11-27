// Copyright (c) 2026 Neomantra Corp.

package nt3dtiles3d

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Scheme is how an implicit tree divides a tile.
type Scheme uint8

const (
	// Quadtree divides a tile in four, across its first two axes (or its
	// longitude and latitude), and leaves its height whole.
	Quadtree Scheme = iota
	// Octree divides a tile in eight, across all three.
	Octree
)

// Coord names a tile of an implicit tree: its level, the root being 0, and
// its place along each axis at that level, 0 to 2^Level - 1. Z is 0 in a
// quadtree.
type Coord struct {
	Level   int
	X, Y, Z int
}

// maxImplicitLevels keeps a coordinate within an int32 and a level's tile
// count within an int64.
const maxImplicitLevels = 30

// Implicit describes an implicit tree: tiles that are not written out, but
// follow from dividing the root, with subtree files that say which exist.
type Implicit struct {
	Scheme Scheme
	// SubtreeLevels is how many levels one subtree file covers.
	SubtreeLevels int
	// AvailableLevels is how many levels the tree has at most.
	AvailableLevels int
	// Subtrees is the template of a subtree file's URI.
	Subtrees string

	// The root's own bounds, error, and content templates, which every tile
	// of the tree is derived from.
	bounds   Volume
	error    float64
	contents []string
}

type rawImplicit struct {
	Scheme          string `json:"subdivisionScheme"`
	SubtreeLevels   int    `json:"subtreeLevels"`
	AvailableLevels int    `json:"availableLevels"`
	MaximumLevel    *int   `json:"maximumLevel"` // the 1.0 extension's name: the last level, not the count.
	Subtrees        struct {
		URI string `json:"uri"`
	} `json:"subtrees"`
}

func (r *rawImplicit) implicit(root *Tile) (*Implicit, error) {
	out := &Implicit{SubtreeLevels: r.SubtreeLevels, AvailableLevels: r.AvailableLevels, Subtrees: r.Subtrees.URI, bounds: root.Bounds, error: root.GeometricError}
	switch r.Scheme {
	case "QUADTREE":
		out.Scheme = Quadtree
	case "OCTREE":
		out.Scheme = Octree
	default:
		return nil, fmt.Errorf("nt3dtiles3d: unknown subdivision scheme %q", printable(r.Scheme, 16))
	}
	if out.AvailableLevels == 0 && r.MaximumLevel != nil {
		out.AvailableLevels = *r.MaximumLevel + 1
	}
	if out.SubtreeLevels < 1 || out.AvailableLevels < 1 || out.AvailableLevels > maxImplicitLevels {
		return nil, fmt.Errorf("nt3dtiles3d: implicit tiling needs 1 or more subtree levels and 1 to %d available levels", maxImplicitLevels)
	}
	// A subtree's availability is a bit for each of its tiles: keep that
	// within what one file may hold.
	if out.SubtreeLevels > maxSubtreeLevels(out.Scheme) {
		return nil, fmt.Errorf("%w: %d subtree levels", ErrTooLarge, out.SubtreeLevels)
	}
	if out.Subtrees == "" || len(out.Subtrees) > maxURILength {
		return nil, errors.New("nt3dtiles3d: implicit tiling needs a subtree uri template")
	}
	if root.Bounds.Kind == VolumeSphere {
		return nil, errors.New("nt3dtiles3d: an implicit root cannot be bounded by a sphere")
	}
	for _, c := range root.Contents {
		out.contents = append(out.contents, c.URI)
	}
	return out, nil
}

// maxSubtreeLevels keeps a subtree's tile count under about 20 million.
func maxSubtreeLevels(s Scheme) int {
	if s == Octree {
		return 8
	}
	return 12
}

// branching is how many children a tile has: 4 or 8.
func (s Scheme) branching() int {
	if s == Octree {
		return 8
	}
	return 4
}

// Valid reports whether c names a tile of the tree: a level the tree has, and
// places within that level.
func (i *Implicit) Valid(c Coord) bool {
	if c.Level < 0 || c.Level >= i.AvailableLevels {
		return false
	}
	side := 1 << c.Level
	if c.X < 0 || c.X >= side || c.Y < 0 || c.Y >= side || c.Z < 0 {
		return false
	}
	if i.Scheme == Quadtree {
		return c.Z == 0
	}
	return c.Z < side
}

// Children returns the coordinates of c's children, whether or not they
// exist: four in a quadtree and eight in an octree.
func (i *Implicit) Children(c Coord) []Coord {
	out := make([]Coord, 0, 8)
	for k := range i.Scheme.branching() {
		out = append(out, Coord{Level: c.Level + 1, X: 2*c.X + k&1, Y: 2*c.Y + k>>1&1, Z: 2*c.Z + k>>2&1})
	}
	return out
}

// SubtreeRoot returns the coordinate of the root of the subtree that holds
// c: the nearest ancestor, or c itself, at a level that is a multiple of
// SubtreeLevels.
func (i *Implicit) SubtreeRoot(c Coord) Coord {
	up := c.Level % i.SubtreeLevels
	return Coord{Level: c.Level - up, X: c.X >> up, Y: c.Y >> up, Z: c.Z >> up}
}

// SubtreeURI returns the URI of the subtree file whose root is c, relative to
// the tileset.
func (i *Implicit) SubtreeURI(c Coord) string { return expand(i.Subtrees, c) }

// ContentURIs returns the URIs of c's contents, one for each content the
// implicit root has, relative to the tileset. Whether each exists is for the
// subtree to say.
func (i *Implicit) ContentURIs(c Coord) []string {
	out := make([]string, len(i.contents))
	for k, template := range i.contents {
		out[k] = expand(template, c)
	}
	return out
}

func expand(template string, c Coord) string {
	return strings.NewReplacer("{level}", strconv.Itoa(c.Level), "{x}", strconv.Itoa(c.X), "{y}", strconv.Itoa(c.Y), "{z}", strconv.Itoa(c.Z)).Replace(template)
}

// GeometricError returns the geometric error of a tile at c's level: the
// root's, halved for each level down.
func (i *Implicit) GeometricError(c Coord) float64 {
	return math.Ldexp(i.error, -c.Level)
}

// Bounds returns the bounding volume of the tile c: the root's, divided in
// two along each divided axis for each level. A box divides along its first
// two axes, and its third in an octree; a region divides its longitude and
// latitude, and its height in an octree. The volume is in the implicit
// root's space, as the root's own is.
func (i *Implicit) Bounds(c Coord) Volume {
	v := i.bounds
	n := math.Ldexp(1, c.Level)
	// Where the tile's middle lies along an axis, from -1 to 1 of the root.
	middle := func(k int) float64 { return (2*float64(k)+1)/n - 1 }
	place := [3]float64{middle(c.X), middle(c.Y), 0}
	shrink := [3]float64{1 / n, 1 / n, 1}
	if i.Scheme == Octree {
		place[2], shrink[2] = middle(c.Z), 1/n
	}
	switch v.Kind {
	case VolumeBox:
		for axis := range 3 {
			v.Box.Center = v.Box.Center.Add(i.bounds.Box.Axes[axis].Scale(place[axis]))
			v.Box.Axes[axis] = i.bounds.Box.Axes[axis].Scale(shrink[axis])
		}
	case VolumeRegion:
		r := i.bounds.Region
		span := r.East - r.West
		if span < 0 {
			span += 360
		}
		v.Region.West = wrap(r.West + span*float64(c.X)/n)
		v.Region.East = wrap(r.West + span*float64(c.X+1)/n)
		v.Region.South = r.South + (r.North-r.South)*float64(c.Y)/n
		v.Region.North = r.South + (r.North-r.South)*float64(c.Y+1)/n
		if i.Scheme == Octree {
			v.Region.MinHeight = r.MinHeight + (r.MaxHeight-r.MinHeight)*float64(c.Z)/n
			v.Region.MaxHeight = r.MinHeight + (r.MaxHeight-r.MinHeight)*float64(c.Z+1)/n
		}
	}
	return v
}

func wrap(lon float64) float64 {
	if lon > 180 {
		return lon - 360
	}
	return lon
}

// morton interleaves a tile's places within its level into its index there:
// X in the lowest bit of each group, then Y, then Z in an octree.
func (s Scheme) morton(x, y, z, level int) int64 {
	var out int64
	for bit := range level {
		if s == Octree {
			out |= int64(x>>bit&1)<<(3*bit) | int64(y>>bit&1)<<(3*bit+1) | int64(z>>bit&1)<<(3*bit+2)
		} else {
			out |= int64(x>>bit&1)<<(2*bit) | int64(y>>bit&1)<<(2*bit+1)
		}
	}
	return out
}

// levelCount is how many tiles a level has, and levelOffset how many all the
// levels above it have together.
func (s Scheme) levelCount(level int) int64 {
	if s == Octree {
		return 1 << (3 * level)
	}
	return 1 << (2 * level)
}

func (s Scheme) levelOffset(level int) int64 {
	return (s.levelCount(level) - 1) / int64(s.branching()-1)
}
