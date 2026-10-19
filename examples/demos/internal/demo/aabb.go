// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"fmt"
	"image/color"
	"math"
	"math/rand/v2"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/aabbtree"
	"nimbleterminal.dev/nt3d/math"
)

// AABBMain shows a dynamic AABB tree over scattered boxes: click a box to
// see its ancestry as nested wireframes, and set the boxes drifting to
// watch the tree update.
func AABBMain() { mainDemo(false, sceneAABB) }

// aabbCounts are the box counts n cycles through.
var aabbCounts = []int{50, 150, 500}

// aabbShow is which tree nodes the hierarchy wireframes draw.
type aabbShow int

const (
	showPath aabbShow = iota // The picked box's ancestors.
	showAll                  // Every node.
	showNone
)

func (s aabbShow) String() string {
	switch s {
	case showAll:
		return "whole tree"
	case showNone:
		return "no tree"
	default:
		return "path to root"
	}
}

type aabbScene struct {
	tree      *nt3daabbtree.Tree[int] // Values index boxes.
	boxes     []aabbBox
	count     int // Index into aabbCounts.
	picked    nt3daabbtree.ID
	hasPick   bool
	show      aabbShow
	hierarchy nt3d.NodeID
	serial    int // Names boxes uniquely across adds.
	rng       *rand.Rand
}

// aabbBox is one scene box: its node, its leaf, and how it drifts.
type aabbBox struct {
	node         nt3d.NodeID
	id           nt3daabbtree.ID
	home, size   nt3dmath.Vec3
	color        color.RGBA
	phase, speed float64
	index        int // The tree value and Box.Datum; also the name.
}

func (b aabbBox) name() string { return fmt.Sprintf("box %d", b.index) }

var aabbPalette = []color.RGBA{{238, 133, 64, 255}, {160, 119, 224, 255}, {234, 196, 83, 255}, {84, 170, 196, 255}, {120, 190, 96, 255}, {220, 96, 120, 255}}

// aabbHalf is the half width of the floor; boxes stay within it.
const aabbHalf = 5.5

func (d *demo) setupAABB() error {
	d.aabb = &aabbScene{count: 1}
	d.animate = false // Boxes sit still until a sets them drifting.
	floor, err := nt3d.BuildHeightfield(nt3d.Heightfield{NX: 2, NY: 2, MinX: -aabbHalf, MaxX: aabbHalf, MinY: -aabbHalf, MaxY: aabbHalf, Heights: []float32{0, 0, 0, 0}, Color: color.RGBA{58, 62, 74, 255}})
	if err != nil {
		return err
	}
	id, err := d.scene.SetGeometry("floor", floor)
	if err != nil {
		return err
	}
	if err = d.scene.Reparent(id, d.group); err != nil {
		return err
	}
	if d.aabb.hierarchy, err = d.scene.Add(d.group, "hierarchy"); err != nil {
		return err
	}
	return d.plantAABB()
}

// plantAABB replaces the boxes with the current count, scattered the same
// way each time, rebuilds the tree, and picks the box nearest the center.
func (d *demo) plantAABB() error {
	a := d.aabb
	for _, b := range a.boxes {
		if err := d.scene.Remove(b.node); err != nil {
			return err
		}
	}
	a.boxes = a.boxes[:0]
	a.tree = nt3daabbtree.New[int]()
	a.hasPick = false
	a.serial = 0
	a.rng = rand.New(rand.NewPCG(5, 17))
	nearest, best := -1, float32(math.Inf(1))
	for range aabbCounts[a.count] {
		b, err := d.addAABBBox()
		if err != nil {
			return err
		}
		if r := b.home.X*b.home.X + b.home.Y*b.home.Y; r < best {
			nearest, best = len(a.boxes)-1, r
		}
	}
	if nearest >= 0 {
		return d.selectAABB(a.boxes[nearest].id)
	}
	return d.drawHierarchy()
}

// addAABBBox scatters one more box and indexes it. It returns the box.
func (d *demo) addAABBBox() (aabbBox, error) {
	a := d.aabb
	r := a.rng
	b := aabbBox{
		index: a.serial,
		home:  nt3dmath.Vec3{X: (r.Float32()*2 - 1) * (aabbHalf - 1.2), Y: (r.Float32()*2 - 1) * (aabbHalf - 1.2), Z: .8 + r.Float32()*2.4},
		size:  nt3dmath.Vec3{X: .2 + r.Float32()*.4, Y: .2 + r.Float32()*.4, Z: .2 + r.Float32()*.4},
		color: aabbPalette[a.serial%len(aabbPalette)],
		phase: r.Float64() * 2 * math.Pi,
		speed: .3 + r.Float64()*.5,
	}
	a.serial++
	var err error
	if b.node, err = d.scene.SetGeometry(b.name(), aabbGeometry(b, b.color)); err != nil {
		return b, err
	}
	if err = d.scene.Reparent(b.node, d.group); err != nil {
		return b, err
	}
	center := aabbCenter(b, d.animationTime)
	if err = d.scene.SetTransform(b.node, nt3dmath.Translation(center)); err != nil {
		return b, err
	}
	b.id = a.tree.Insert(aabbWorld(center, b.size), b.index)
	a.boxes = append(a.boxes, b)
	return b, d.drawHierarchy()
}

func aabbGeometry(b aabbBox, c color.RGBA) nt3d.Geometry {
	return nt3d.Geometry{Boxes: []nt3d.Box{{Min: b.size.Scale(-.5), Size: b.size, Color: c, Datum: b.index}}}
}

// aabbCenter is where a box is at time t: drifting about its home on three
// slow sines, a unit sideways and less up and down.
func aabbCenter(b aabbBox, t float64) nt3dmath.Vec3 {
	s := b.speed * t
	return b.home.Add(nt3dmath.Vec3{
		X: float32(math.Sin(b.phase + s)),
		Y: float32(math.Cos(b.phase + 1.3*s)),
		Z: float32(.4 * math.Sin(b.phase+.7*s)),
	})
}

// aabbWorld is the world box of a box of size at center.
func aabbWorld(center, size nt3dmath.Vec3) nt3dmath.AABB {
	var b nt3dmath.AABB
	b.Include(center.Sub(size.Scale(.5)))
	b.Include(center.Add(size.Scale(.5)))
	return b
}

// stepAABB drifts every box, moving its node and its leaf, and redraws.
func (d *demo) stepAABB() error {
	a := d.aabb
	for _, b := range a.boxes {
		center := aabbCenter(b, d.animationTime)
		if err := d.scene.SetTransform(b.node, nt3dmath.Translation(center)); err != nil {
			return err
		}
		a.tree.Update(b.id, aabbWorld(center, b.size))
	}
	return d.drawHierarchy()
}

// pickAABB casts the ray through the tree and picks the nearest box, or
// clears the pick if the ray meets none.
func (d *demo) pickAABB(ray nt3dmath.Ray) error {
	a := d.aabb
	best, found := float32(math.Inf(1)), nt3daabbtree.ID(0)
	hit := false
	a.tree.RayCast(ray, func(id nt3daabbtree.ID, _ int, t float32) bool {
		if t < best {
			best, found, hit = t, id, true
		}
		return true
	})
	if !hit {
		return d.clearAABBPick()
	}
	return d.selectAABB(found)
}

// selectAABB makes id the picked box, painting it white.
func (d *demo) selectAABB(id nt3daabbtree.ID) error {
	a := d.aabb
	if a.hasPick && a.picked == id {
		return nil
	}
	if err := d.clearAABBPick(); err != nil {
		return err
	}
	b := a.boxes[a.boxIndex(id)]
	if err := d.scene.SetNodeGeometry(b.node, aabbGeometry(b, color.RGBA{255, 255, 255, 255})); err != nil {
		return err
	}
	a.picked, a.hasPick = id, true
	return d.drawHierarchy()
}

// clearAABBPick restores the picked box's color and forgets it.
func (d *demo) clearAABBPick() error {
	a := d.aabb
	if !a.hasPick {
		return nil
	}
	b := a.boxes[a.boxIndex(a.picked)]
	a.hasPick = false
	if err := d.scene.SetNodeGeometry(b.node, aabbGeometry(b, b.color)); err != nil {
		return err
	}
	return d.drawHierarchy()
}

// boxIndex finds the box whose leaf is id.
func (a *aabbScene) boxIndex(id nt3daabbtree.ID) int {
	value := a.tree.Value(id)
	for i, b := range a.boxes {
		if b.index == value {
			return i
		}
	}
	panic("aabb demo: leaf without a box")
}

// removePickedAABB deletes the picked box from the scene and the tree.
func (d *demo) removePickedAABB() error {
	a := d.aabb
	if !a.hasPick {
		return nil
	}
	i := a.boxIndex(a.picked)
	b := a.boxes[i]
	a.hasPick = false
	a.tree.Remove(b.id)
	a.boxes = append(a.boxes[:i], a.boxes[i+1:]...)
	if err := d.scene.Remove(b.node); err != nil {
		return err
	}
	return d.drawHierarchy()
}

func (d *demo) cycleAABBCount() error {
	d.aabb.count = (d.aabb.count + 1) % len(aabbCounts)
	return d.plantAABB()
}

func (d *demo) cycleAABBShow() error {
	d.aabb.show = (d.aabb.show + 1) % 3
	return d.drawHierarchy()
}

// Hierarchy wireframes go from a cool, dim root to a warm, bright leaf.
var (
	aabbRootColor = color.RGBA{72, 108, 204, 255}
	aabbLeafColor = color.RGBA{255, 196, 64, 255}
)

// drawHierarchy rebuilds the hierarchy node's lines: the picked box's
// ancestors, every node, or nothing. Deeper nodes draw tighter, so nested
// edges do not coincide.
func (d *demo) drawHierarchy() error {
	a := d.aabb
	var lines []nt3d.Vertex
	height := a.tree.Height()
	add := func(n nt3daabbtree.Node) bool {
		t := float32(1)
		if height > 1 {
			t = float32(n.Depth) / float32(height-1)
		}
		c := color.RGBA{
			R: uint8(float32(aabbRootColor.R) + t*(float32(aabbLeafColor.R)-float32(aabbRootColor.R))),
			G: uint8(float32(aabbRootColor.G) + t*(float32(aabbLeafColor.G)-float32(aabbRootColor.G))),
			B: uint8(float32(aabbRootColor.B) + t*(float32(aabbLeafColor.B)-float32(aabbRootColor.B))),
			A: 255,
		}
		lines = append(lines, boxEdges(n.Bounds, .012*float32(height-1-n.Depth), c)...)
		return true
	}
	switch a.show {
	case showPath:
		if a.hasPick {
			a.tree.Ancestors(a.picked, add)
		}
	case showAll:
		a.tree.Walk(add)
	}
	return d.scene.SetNodeGeometry(a.hierarchy, nt3d.Geometry{Lines: lines, LineWidth: 2})
}

// boxEdges returns the twelve edges of b, grown by inflate on every side,
// as 24 line vertices.
func boxEdges(b nt3dmath.AABB, inflate float32, c color.RGBA) []nt3d.Vertex {
	lo := b.Min.Sub(nt3dmath.Vec3{X: inflate, Y: inflate, Z: inflate})
	hi := b.Max.Add(nt3dmath.Vec3{X: inflate, Y: inflate, Z: inflate})
	corner := func(i int) nt3d.Vertex {
		p := lo
		if i&1 != 0 {
			p.X = hi.X
		}
		if i&2 != 0 {
			p.Y = hi.Y
		}
		if i&4 != 0 {
			p.Z = hi.Z
		}
		return nt3d.Vertex{Position: p, Color: c}
	}
	out := make([]nt3d.Vertex, 0, 24)
	for i := range 8 {
		for _, bit := range []int{1, 2, 4} {
			if i&bit == 0 {
				out = append(out, corner(i), corner(i|bit))
			}
		}
	}
	return out
}

func (d *demo) aabbView(content string) string {
	a := d.aabb
	pick := "click a box"
	if a.hasPick {
		depth := 0
		a.tree.Ancestors(a.picked, func(n nt3daabbtree.Node) bool { depth = n.Depth; return true })
		pick = fmt.Sprintf("leaf at depth %d of %d", depth, a.tree.Height()-1)
	}
	drift := "still"
	if d.animate {
		drift = "drifting"
	}
	return fmt.Sprintf("nt3d AABB tree · click a box · h hierarchy · a drift · n count · b add · x remove · 1/2/3 backend · q quit\n%s\n%s · %s · %s · %d boxes, %d nodes · %s · drag orbit · wheel zoom · r rotate · %s",
		content, d.rendering(), a.show, pick, len(a.boxes), 2*len(a.boxes)-1, drift, d.status)
}
