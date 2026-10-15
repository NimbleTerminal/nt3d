// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"image/color"
	"math"

	"nimbleterminal.dev/nt3d/math"
)

// Shape builders return lit, UV-mapped triangle meshes centered on the local
// origin with Z up. Faces wind counterclockwise when seen from outside, as the
// renderers' back-face culling expects. Zero Color defaults to opaque white,
// and zero tessellation counts use the documented defaults. Place shapes with
// node transforms, or bake a transform with TransformGeometry.

// Sphere is a UV sphere. Segments run around Z (default 32, 3..1024) and Rings
// from the +Z pole to the -Z pole (default 16, 2..1024).
type Sphere struct {
	Radius          float32
	Segments, Rings int
	Color           color.RGBA
	Material        *Material
}

// Cylinder spans Z from -Height/2 to +Height/2. A zero TopRadius makes a cone;
// unequal radii make a frustum. Open omits both caps. Segments default to 32
// (3..1024).
type Cylinder struct {
	BottomRadius, TopRadius float32
	Height                  float32
	Segments                int
	Open                    bool
	Color                   color.RGBA
	Material                *Material
}

// Torus surrounds Z. Segments run around Z (default 48) and Sides around the
// tube (default 16); both are 3..1024.
type Torus struct {
	MajorRadius, MinorRadius float32
	Segments, Sides          int
	Color                    color.RGBA
	Material                 *Material
}

// Cuboid is a box mesh with its own UVs on each face. Unlike Box instances, it
// can be textured, picked per triangle, and transformed freely.
type Cuboid struct {
	Size     nt3dmath.Vec3
	Color    color.RGBA
	Material *Material
}

const maxShapeDivisions = 1024

func shapeDivisions(n, fallback, least int) (int, error) {
	if n == 0 {
		return fallback, nil
	}
	if n < least || n > maxShapeDivisions {
		return 0, fmt.Errorf("nt3d: shape divisions must be %d..%d", least, maxShapeDivisions)
	}
	return n, nil
}
func shapeColor(c color.RGBA) color.RGBA {
	if c == (color.RGBA{}) {
		return color.RGBA{255, 255, 255, 255}
	}
	return c
}
func positive(v ...float32) bool {
	for _, x := range v {
		if !validFloat(x) || x <= 0 || x > 1e15 {
			return false
		}
	}
	return true
}

// lattice is a (rows+1) × (cols+1) grid of vertices whose first column repeats
// as the last, so texture coordinates can wrap. UV V runs down the rows.
type lattice struct {
	g          Geometry
	rows, cols int
	base       uint32
}

func newLattice(g Geometry, rows, cols int, at func(i, j int, theta, t float64) (p, n nt3dmath.Vec3), c color.RGBA) *lattice {
	l := &lattice{g: g, rows: rows, cols: cols, base: uint32(len(g.Vertices))}
	for i := 0; i <= rows; i++ {
		for j := 0; j <= cols; j++ {
			p, n := at(i, j, 2*math.Pi*float64(j%cols)/float64(cols), float64(i)/float64(rows))
			l.g.Vertices = append(l.g.Vertices, Vertex{p, n, c})
			l.g.UV = append(l.g.UV, UV{float32(j) / float32(cols), float32(i) / float32(rows)})
		}
	}
	return l
}
func (l *lattice) at(i, j int) uint32 { return l.base + uint32(i*(l.cols+1)+j) }

// quad adds the cell below and after (i, j). When the rows advance against the
// surface's outward orientation, flip swaps the winding.
func (l *lattice) quad(i, j int, flip, skipFirst, skipSecond bool) {
	a, b, c, d := l.at(i, j), l.at(i, j+1), l.at(i+1, j), l.at(i+1, j+1)
	if flip {
		b, c = c, b
	}
	if !skipFirst {
		l.g.Indices = append(l.g.Indices, a, c, b)
	}
	if !skipSecond {
		l.g.Indices = append(l.g.Indices, b, c, d)
	}
}

func finishShape(g Geometry, m *Material) (Geometry, error) {
	g.Material = cloneMaterial(m)
	g.Bounds = GeometryBounds(g)
	return g, g.Validate()
}

// BuildSphere builds a UV sphere with smooth normals.
func BuildSphere(s Sphere) (Geometry, error) {
	if !positive(s.Radius) {
		return Geometry{}, fmt.Errorf("nt3d: sphere radius must be positive")
	}
	segments, err := shapeDivisions(s.Segments, 32, 3)
	if err != nil {
		return Geometry{}, err
	}
	rings, err := shapeDivisions(s.Rings, 16, 2)
	if err != nil {
		return Geometry{}, err
	}
	l := newLattice(Geometry{}, rings, segments, func(i, _ int, theta, t float64) (p, n nt3dmath.Vec3) {
		sin, cos := math.Sincos(math.Pi * t)
		if i == rings {
			sin = 0 // Every vertex of a pole is the same point.
		}
		n = nt3dmath.Vec3{X: float32(sin * math.Cos(theta)), Y: float32(sin * math.Sin(theta)), Z: float32(cos)}
		return n.Scale(s.Radius), n
	}, shapeColor(s.Color))
	for i := range rings {
		for j := range segments {
			// Each pole row has one real triangle per cell.
			l.quad(i, j, false, i == 0, i == rings-1)
		}
	}
	return finishShape(l.g, s.Material)
}

// BuildCylinder builds a cylinder, cone, or frustum with smooth sides and flat caps.
func BuildCylinder(c Cylinder) (Geometry, error) {
	if !positive(c.Height) || !validFloat(c.BottomRadius, c.TopRadius) || c.BottomRadius < 0 || c.TopRadius < 0 || max(c.BottomRadius, c.TopRadius) <= 0 || max(c.BottomRadius, c.TopRadius) > 1e15 {
		return Geometry{}, fmt.Errorf("nt3d: cylinder needs a positive height and nonnegative radii, one positive")
	}
	segments, err := shapeDivisions(c.Segments, 32, 3)
	if err != nil {
		return Geometry{}, err
	}
	shade := shapeColor(c.Color)
	half := c.Height / 2
	slope := float64(c.BottomRadius-c.TopRadius) / float64(c.Height)
	l := newLattice(Geometry{}, 1, segments, func(i, _ int, theta, _ float64) (p, n nt3dmath.Vec3) {
		r, z := c.TopRadius, half
		if i == 1 {
			r, z = c.BottomRadius, -half
		}
		cos, sin := math.Cos(theta), math.Sin(theta)
		return nt3dmath.Vec3{X: r * float32(cos), Y: r * float32(sin), Z: z}, nt3dmath.Vec3{X: float32(cos), Y: float32(sin), Z: float32(slope)}.Normalize()
	}, shade)
	for j := range segments {
		l.quad(0, j, false, c.TopRadius == 0, c.BottomRadius == 0)
	}
	g := l.g
	addCap := func(r, z float32, up bool) {
		if c.Open || r == 0 {
			return
		}
		n := nt3dmath.Vec3{Z: 1}
		if !up {
			n.Z = -1
		}
		center := uint32(len(g.Vertices))
		g.Vertices = append(g.Vertices, Vertex{nt3dmath.Vec3{Z: z}, n, shade})
		g.UV = append(g.UV, UV{.5, .5})
		for j := range segments {
			theta := 2 * math.Pi * float64(j) / float64(segments)
			cos, sin := float32(math.Cos(theta)), float32(math.Sin(theta))
			g.Vertices = append(g.Vertices, Vertex{nt3dmath.Vec3{X: r * cos, Y: r * sin, Z: z}, n, shade})
			// Seen from outside, from over the side at -Y, a cap's
			// texture has its top toward +Y: the bottom's runs the
			// other way in X.
			g.UV = append(g.UV, UV{.5 + .5*cos*n.Z, .5 - .5*sin})
		}
		for j := range uint32(segments) {
			a, b := center+1+j, center+1+(j+1)%uint32(segments)
			if !up {
				a, b = b, a
			}
			g.Indices = append(g.Indices, center, a, b)
		}
	}
	addCap(c.TopRadius, half, true)
	addCap(c.BottomRadius, -half, false)
	return finishShape(g, c.Material)
}

// BuildTorus builds a ring torus with smooth normals.
func BuildTorus(t Torus) (Geometry, error) {
	if !positive(t.MajorRadius, t.MinorRadius) || t.MinorRadius >= t.MajorRadius {
		return Geometry{}, fmt.Errorf("nt3d: torus radii must be positive, with the minor radius smaller")
	}
	segments, err := shapeDivisions(t.Segments, 48, 3)
	if err != nil {
		return Geometry{}, err
	}
	sides, err := shapeDivisions(t.Sides, 16, 3)
	if err != nil {
		return Geometry{}, err
	}
	l := newLattice(Geometry{}, sides, segments, func(i, _ int, theta, _ float64) (p, n nt3dmath.Vec3) {
		// Rows run down the outside, as on a sphere, so the texture is not
		// mirrored there or inside.
		phi := -2 * math.Pi * float64(i%sides) / float64(sides)
		ring := float64(t.MajorRadius) + float64(t.MinorRadius)*math.Cos(phi)
		n = nt3dmath.Vec3{X: float32(math.Cos(phi) * math.Cos(theta)), Y: float32(math.Cos(phi) * math.Sin(theta)), Z: float32(math.Sin(phi))}
		return nt3dmath.Vec3{X: float32(ring * math.Cos(theta)), Y: float32(ring * math.Sin(theta)), Z: t.MinorRadius * n.Z}, n
	}, shapeColor(t.Color))
	for i := range sides {
		for j := range segments {
			l.quad(i, j, false, false, false)
		}
	}
	return finishShape(l.g, t.Material)
}

// BuildCuboid builds a box mesh of the given size, with flat faces.
func BuildCuboid(c Cuboid) (Geometry, error) {
	if !positive(c.Size.X, c.Size.Y, c.Size.Z) {
		return Geometry{}, fmt.Errorf("nt3d: cuboid size must be positive")
	}
	box := boxVertices(Box{Min: c.Size.Scale(-.5), Size: c.Size, Color: shapeColor(c.Color)})
	g := Geometry{}
	// Each face's texture is upright seen from outside: its top toward +Z
	// on the sides, and toward +Y on the top and bottom, as seen coming over
	// the box from -Y. Its right is then up × outward.
	extent := func(axis nt3dmath.Vec3) float32 { return abs(axis.Dot(c.Size)) }
	// boxVertices emits each face as corners 0,1,2,0,2,3.
	for f := 0; f < len(box); f += 6 {
		base := uint32(len(g.Vertices))
		n := box[f].Normal
		up := nt3dmath.Vec3{Z: 1}
		if abs(n.Z) > .5 {
			up = nt3dmath.Vec3{Y: 1}
		}
		right := up.Cross(n)
		for _, i := range []int{0, 1, 2, 5} {
			p := box[f+i].Position
			g.Vertices = append(g.Vertices, box[f+i])
			g.UV = append(g.UV, UV{.5 + p.Dot(right)/extent(right), .5 - p.Dot(up)/extent(up)})
		}
		g.Indices = append(g.Indices, base, base+1, base+2, base, base+2, base+3)
	}
	return finishShape(g, c.Material)
}
