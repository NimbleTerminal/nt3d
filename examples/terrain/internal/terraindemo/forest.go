// Copyright (c) 2026 Neomantra Corp.
package terraindemo

import (
	"fmt"
	"image/color"
	"math"
	"math/rand/v2"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
)

// ForestMain shows a forest of thousands of trees, three shared meshes drawn
// instanced, swaying in the wind.
func ForestMain() { mainDemo(true, sceneForest) }

// forestCounts are the tree counts n cycles through.
var forestCounts = []int{500, 2000, 8000}

type forestScene struct {
	kinds  []forestKind
	trees  []forestTree
	count  int  // Index into forestCounts.
	copies bool // Each tree its own geometry, rather than shared.
	group  nt3d.NodeID
}

// forestKind is one kind of tree: its mesh, shared or copied.
type forestKind struct {
	name     string
	geometry nt3d.Geometry
	shared   *nt3d.SharedGeometry
}

type forestTree struct {
	node       nt3d.NodeID
	kind       int
	base       nt3dmath.Mat4 // Placed, turned, and sized, before the wind.
	phase, amp float64
}

// forestSize is the ground's half width; heights follow a few rolling waves.
const forestSize = 10

func groundHeight(x, y float32) float32 {
	return .35*float32(math.Sin(float64(x)*.45)*math.Cos(float64(y)*.38)) + .15*float32(math.Sin(float64(x+y)*.9))
}

// treeGeometry merges a trunk and a crown into one mesh, standing on z = 0.
func treeGeometry(crown nt3d.Geometry, crownHeight float32, trunkHeight float32) (nt3d.Geometry, error) {
	trunk, err := nt3d.BuildCylinder(nt3d.Cylinder{BottomRadius: .035, TopRadius: .025, Height: trunkHeight, Segments: 8, Color: color.RGBA{110, 74, 46, 255}})
	if err != nil {
		return nt3d.Geometry{}, err
	}
	if trunk, err = nt3d.TransformGeometry(trunk, nt3dmath.Translation(nt3dmath.Vec3{Z: trunkHeight / 2})); err != nil {
		return nt3d.Geometry{}, err
	}
	if crown, err = nt3d.TransformGeometry(crown, nt3dmath.Translation(nt3dmath.Vec3{Z: crownHeight})); err != nil {
		return nt3d.Geometry{}, err
	}
	return nt3d.MergeGeometry(trunk, crown)
}

func forestKinds() ([]forestKind, error) {
	pine, err := nt3d.BuildCylinder(nt3d.Cylinder{BottomRadius: .17, Height: .5, Segments: 9, Color: color.RGBA{38, 104, 58, 255}})
	if err != nil {
		return nil, err
	}
	round, err := nt3d.BuildSphere(nt3d.Sphere{Radius: .2, Segments: 10, Rings: 6, Color: color.RGBA{86, 140, 52, 255}})
	if err != nil {
		return nil, err
	}
	bush, err := nt3d.BuildSphere(nt3d.Sphere{Radius: .12, Segments: 8, Rings: 5, Color: color.RGBA{120, 150, 60, 255}})
	if err != nil {
		return nil, err
	}
	var kinds []forestKind
	for _, k := range []struct {
		name           string
		crown          nt3d.Geometry
		crownZ, trunkZ float32 // The crown's center, and the trunk's height.
	}{{"pine", pine, .45, .22}, {"round", round, .42, .28}, {"bush", bush, .1, .04}} {
		g, err := treeGeometry(k.crown, k.crownZ, k.trunkZ)
		if err != nil {
			return nil, err
		}
		shared, err := nt3d.NewSharedGeometry(g)
		if err != nil {
			return nil, err
		}
		kinds = append(kinds, forestKind{k.name, g, shared})
	}
	return kinds, nil
}

func (d *demo) setupForest() error {
	kinds, err := forestKinds()
	if err != nil {
		return err
	}
	d.forest = &forestScene{kinds: kinds, count: 1}
	d.fog = true // Distance shows best in haze.
	const n = 48
	ground, err := nt3d.BuildHeightfield(nt3d.Heightfield{NX: n, NY: n, MinX: -forestSize, MaxX: forestSize, MinY: -forestSize, MaxY: forestSize,
		Sampler: groundHeight, Color: color.RGBA{96, 128, 62, 255}})
	if err != nil {
		return err
	}
	if _, err = d.scene.SetGeometry("ground", ground); err != nil {
		return err
	}
	if d.forest.group, err = d.scene.Add(nt3d.Root, "trees"); err != nil {
		return err
	}
	return d.plantForest()
}

// plantForest replaces the trees with the current count, scattered the same
// way each time: kinds, places, turns, and sizes from a fixed seed.
func (d *demo) plantForest() error {
	f := d.forest
	for _, t := range f.trees {
		if err := d.scene.Remove(t.node); err != nil {
			return err
		}
	}
	f.trees = f.trees[:0]
	rng := rand.New(rand.NewPCG(7, 11))
	for i := range forestCounts[f.count] {
		x, y := (rng.Float32()*2-1)*(forestSize-.3), (rng.Float32()*2-1)*(forestSize-.3)
		kind := rng.IntN(len(f.kinds))
		size := .7 + rng.Float32()*.8
		base := nt3dmath.Translation(nt3dmath.Vec3{X: x, Y: y, Z: groundHeight(x, y)}).
			Mul(nt3dmath.RotationZ(rng.Float64() * 2 * math.Pi)).
			Mul(nt3dmath.Scaling(nt3dmath.Vec3{X: size, Y: size, Z: size}))
		id, err := d.scene.Add(f.group, fmt.Sprintf("tree %d", i))
		if err != nil {
			return err
		}
		t := forestTree{node: id, kind: kind, base: base, phase: rng.Float64() * 2 * math.Pi, amp: .03 + rng.Float64()*.04}
		f.trees = append(f.trees, t)
		if err := d.dressTree(t); err != nil {
			return err
		}
		if err := d.scene.SetTransform(id, base); err != nil {
			return err
		}
	}
	return nil
}

// dressTree gives a tree its kind's shared mesh, or its own copy of it.
func (d *demo) dressTree(t forestTree) error {
	k := d.forest.kinds[t.kind]
	if d.forest.copies {
		return d.scene.SetNodeGeometry(t.node, k.geometry)
	}
	return d.scene.SetNodeShared(t.node, k.shared)
}

func (d *demo) toggleInstancing() error {
	d.forest.copies = !d.forest.copies
	for _, t := range d.forest.trees {
		if err := d.dressTree(t); err != nil {
			return err
		}
	}
	return nil
}

func (d *demo) cycleForest() error {
	d.forest.count = (d.forest.count + 1) % len(forestCounts)
	return d.plantForest()
}

// stepForest sways each tree from its base, rewriting every transform.
func (d *demo) stepForest() error {
	for _, t := range d.forest.trees {
		lean := t.amp * math.Sin(d.phase*2+t.phase)
		if err := d.scene.SetTransform(t.node, t.base.Mul(tiltX(lean))); err != nil {
			return err
		}
	}
	return nil
}

// tiltX turns about the X axis, through the tree's base.
func tiltX(radians float64) nt3dmath.Mat4 {
	m := nt3dmath.Identity()
	c, s := float32(math.Cos(radians)), float32(math.Sin(radians))
	m[5], m[6], m[9], m[10] = c, s, -s, c
	return m
}

func (d *demo) forestView(content string) string {
	mode := "instanced"
	if d.forest.copies {
		mode = "copies"
	}
	triangles := 0
	for _, t := range d.forest.trees {
		triangles += len(d.forest.kinds[t.kind].geometry.Indices) / 3
	}
	return fmt.Sprintf("nt3d forest · i instancing · n trees · a wind · d fog · 1/2/3 backend · q quit\n%s\n%s · %s · %d trees, %d triangles · drag orbit · wheel zoom · r rotate · %s",
		content, d.rendering(), mode, len(d.forest.trees), triangles, d.status)
}
