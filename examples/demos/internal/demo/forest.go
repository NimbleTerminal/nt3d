// Copyright (c) 2026 Neomantra Corp.

package demo

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

// forestNear and forestMid are the camera-to-bounds distances where trees step
// down to coarser meshes; the coarsest tier uses MaxDistance 0 so trees stay
// visible at any zoom-out distance instead of clipping off the island.
const (
	forestNear     = 7
	forestMid      = 15
	forestMinBias  = 0.05
	forestMaxBias  = 20
	forestHystBand = 0.05
)

type forestLODMode uint8

const (
	forestLODOn forestLODMode = iota
	forestLODTint
	forestLODOff
)

func (m forestLODMode) String() string {
	switch m {
	case forestLODTint:
		return "LOD tint"
	case forestLODOff:
		return "LOD off"
	default:
		return "LOD on"
	}
}

type forestScene struct {
	shadows   shadowMode
	kinds     []forestKind
	trees     []forestTree
	count     int // Index into forestCounts.
	copies    bool
	lod       forestLODMode
	ground    nt3d.NodeID
	group     nt3d.NodeID
	have      bool
	drawn     int
	batches   int
	triangles int
	tiers     [3]int
}

type forestVariant struct {
	geometry   nt3d.Geometry
	shared     *nt3d.SharedGeometry
	lodCopies  []nt3d.LODLevel
	lodShared  []nt3d.LODLevel
	tintCopies []nt3d.LODLevel
	tintShared []nt3d.LODLevel
}

// forestKind is one kind of tree: its full mesh and LOD tiers, plain and drop-shadowed.
type forestKind struct {
	name        string
	plain, drop forestVariant
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
// When trunkSegments is zero, only the translated crown is returned.
func treeGeometry(crown nt3d.Geometry, crownHeight, trunkHeight float32, trunkSegments int, openTrunk bool) (nt3d.Geometry, error) {
	crown, err := nt3d.TransformGeometry(crown, nt3dmath.Translation(nt3dmath.Vec3{Z: crownHeight}))
	if err != nil || trunkSegments == 0 {
		return crown, err
	}
	trunk, err := nt3d.BuildCylinder(nt3d.Cylinder{
		BottomRadius: .035, TopRadius: .025, Height: trunkHeight,
		Segments: trunkSegments, Open: openTrunk, Color: color.RGBA{110, 74, 46, 255},
	})
	if err != nil {
		return nt3d.Geometry{}, err
	}
	if trunk, err = nt3d.TransformGeometry(trunk, nt3dmath.Translation(nt3dmath.Vec3{Z: trunkHeight / 2})); err != nil {
		return nt3d.Geometry{}, err
	}
	return nt3d.MergeGeometry(trunk, crown)
}

func tintGeometry(g nt3d.Geometry, c color.RGBA) nt3d.Geometry {
	out := g
	out.Vertices = make([]nt3d.Vertex, len(g.Vertices))
	copy(out.Vertices, g.Vertices)
	for i := range out.Vertices {
		out.Vertices[i].Color = c
	}
	return out
}

// forestTintColors color-code near (emerald), mid (amber), and far (coral) LOD tiers.
var forestTintColors = [3]color.RGBA{
	{46, 168, 76, 255},
	{230, 176, 48, 255},
	{214, 88, 72, 255},
}

func buildForestVariant(tiers [3]nt3d.Geometry, drop nt3d.DropShadow) (forestVariant, error) {
	distances := [3]float32{forestNear, forestMid, 0}
	var v forestVariant
	for i, g := range tiers {
		g.DropShadow = drop
		sg, err := nt3d.NewSharedGeometry(g)
		if err != nil {
			return forestVariant{}, err
		}
		if i == 0 {
			v.geometry, v.shared = g, sg
		}
		v.lodCopies = append(v.lodCopies, nt3d.LODLevel{MaxDistance: distances[i], Geometry: g})
		v.lodShared = append(v.lodShared, nt3d.LODLevel{MaxDistance: distances[i], Shared: sg})

		tg := tintGeometry(g, forestTintColors[i])
		tsg, err := nt3d.NewSharedGeometry(tg)
		if err != nil {
			return forestVariant{}, err
		}
		v.tintCopies = append(v.tintCopies, nt3d.LODLevel{MaxDistance: distances[i], Geometry: tg})
		v.tintShared = append(v.tintShared, nt3d.LODLevel{MaxDistance: distances[i], Shared: tsg})
	}
	return v, nil
}

func forestKinds() ([]forestKind, error) {
	pineColor := color.RGBA{38, 104, 58, 255}
	pineNear, err := nt3d.BuildCylinder(nt3d.Cylinder{BottomRadius: .17, Height: .5, Segments: 9, Color: pineColor})
	if err != nil {
		return nil, err
	}
	pineMid, err := nt3d.BuildCylinder(nt3d.Cylinder{BottomRadius: .17, Height: .5, Segments: 5, Open: true, Color: pineColor})
	if err != nil {
		return nil, err
	}
	pineFarCrown, err := nt3d.BuildCylinder(nt3d.Cylinder{BottomRadius: .17, Height: .7, Segments: 3, Open: true, Color: pineColor})
	if err != nil {
		return nil, err
	}

	roundColor := color.RGBA{86, 140, 52, 255}
	roundNear, err := nt3d.BuildSphere(nt3d.Sphere{Radius: .2, Segments: 10, Rings: 6, Color: roundColor})
	if err != nil {
		return nil, err
	}
	roundMid, err := nt3d.BuildSphere(nt3d.Sphere{Radius: .2, Segments: 5, Rings: 3, Color: roundColor})
	if err != nil {
		return nil, err
	}
	roundFar, err := nt3d.BuildSphere(nt3d.Sphere{Radius: .2, Segments: 4, Rings: 2, Color: roundColor})
	if err != nil {
		return nil, err
	}

	bushColor := color.RGBA{120, 150, 60, 255}
	bushNear, err := nt3d.BuildSphere(nt3d.Sphere{Radius: .12, Segments: 8, Rings: 5, Color: bushColor})
	if err != nil {
		return nil, err
	}
	bushMid, err := nt3d.BuildSphere(nt3d.Sphere{Radius: .12, Segments: 4, Rings: 3, Color: bushColor})
	if err != nil {
		return nil, err
	}
	bushFar, err := nt3d.BuildSphere(nt3d.Sphere{Radius: .12, Segments: 3, Rings: 2, Color: bushColor})
	if err != nil {
		return nil, err
	}

	type tierSpec struct {
		crown          nt3d.Geometry
		crownZ, trunkZ float32
		trunkSegs      int
		openTrunk      bool
	}
	specs := []struct {
		name  string
		tiers [3]tierSpec
	}{
		{"pine", [3]tierSpec{
			{pineNear, .45, .22, 8, false},
			{pineMid, .45, .22, 4, true},
			{pineFarCrown, .35, 0, 0, true},
		}},
		{"round", [3]tierSpec{
			{roundNear, .42, .28, 8, false},
			{roundMid, .42, .28, 4, true},
			{roundFar, .42, .28, 3, true},
		}},
		{"bush", [3]tierSpec{
			{bushNear, .1, .04, 8, false},
			{bushMid, .12, 0, 0, true},
			{bushFar, .12, 0, 0, true},
		}},
	}
	var kinds []forestKind
	for _, s := range specs {
		var tiers [3]nt3d.Geometry
		for i, ts := range s.tiers {
			tiers[i], err = treeGeometry(ts.crown, ts.crownZ, ts.trunkZ, ts.trunkSegs, ts.openTrunk)
			if err != nil {
				return nil, err
			}
		}
		plain, err := buildForestVariant(tiers, nt3d.DropShadow{})
		if err != nil {
			return nil, err
		}
		drop, err := buildForestVariant(tiers, nt3d.DropShadow{Strength: .4})
		if err != nil {
			return nil, err
		}
		kinds = append(kinds, forestKind{name: s.name, plain: plain, drop: drop})
	}
	return kinds, nil
}

func (d *demo) setupForest() error {
	kinds, err := forestKinds()
	if err != nil {
		return err
	}
	d.forest = &forestScene{kinds: kinds, count: 1, lod: forestLODOn}
	d.fog = true // Distance shows best in haze.
	const n = 48
	ground, err := nt3d.BuildHeightfield(nt3d.Heightfield{NX: n, NY: n, MinX: -forestSize, MaxX: forestSize, MinY: -forestSize, MaxY: forestSize,
		Sampler: groundHeight, Color: color.RGBA{96, 128, 62, 255}})
	if err != nil {
		return err
	}
	if d.forest.ground, err = d.scene.SetGeometry("ground", ground); err != nil {
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
	f.have = false
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

// dressTree gives a tree its kind's LOD tiers or full mesh, shared or copied.
func (d *demo) dressTree(t forestTree) error {
	v := d.forest.kinds[t.kind].plain
	if d.forest.shadows == shadowDrop {
		v = d.forest.kinds[t.kind].drop
	}
	switch d.forest.lod {
	case forestLODOn:
		if d.forest.copies {
			return d.scene.SetLOD(t.node, v.lodCopies)
		}
		return d.scene.SetLOD(t.node, v.lodShared)
	case forestLODTint:
		if d.forest.copies {
			return d.scene.SetLOD(t.node, v.tintCopies)
		}
		return d.scene.SetLOD(t.node, v.tintShared)
	default:
		if d.forest.copies {
			return d.scene.SetNodeGeometry(t.node, v.geometry)
		}
		return d.scene.SetNodeShared(t.node, v.shared)
	}
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

func (d *demo) cycleForestLOD() error {
	d.forest.lod = (d.forest.lod + 1) % 3
	d.forest.have = false
	for _, t := range d.forest.trees {
		if err := d.dressTree(t); err != nil {
			return err
		}
	}
	return nil
}

func (d *demo) stepForestLODBias(factor float64) error {
	next := float32(float64(d.scene.LODBias()) * factor)
	next = max(forestMinBias, min(forestMaxBias, next))
	return d.scene.SetLODBias(next)
}

func (d *demo) toggleForestLODHysteresis() error {
	if d.scene.LODHysteresis() > 0 {
		return d.scene.SetLODHysteresis(0)
	}
	return d.scene.SetLODHysteresis(forestHystBand)
}

// captureForest tallies the frustum-drawn trees, draw batches, per-tier LOD
// counts, and active triangles emitted in f via Frame.Stats.
func (d *demo) captureForest(f *nt3d.Frame) {
	fs := d.forest
	st := f.Stats()
	drawn, batches, triangles := st.DrawnInstances, st.Batches, st.Triangles
	for i, inst := range f.Instances {
		if inst.Node == fs.ground {
			gs := (nt3d.Frame{
				Width: f.Width, Height: f.Height, Matrix: f.Matrix, Cull: f.Cull,
				Geometry: f.Geometry[i : i+1], Instances: f.Instances[i : i+1],
			}).Stats()
			drawn -= gs.DrawnInstances
			batches -= gs.Batches
			triangles -= gs.Triangles
			break
		}
	}
	fs.tiers = [3]int{st.LODTierInstances[0], st.LODTierInstances[1], st.LODTierInstances[2]}
	fs.drawn, fs.batches, fs.triangles, fs.have = drawn, batches, triangles, true
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
	fs := d.forest
	mode := "instanced"
	if fs.copies {
		mode = "copies"
	}
	lodSummary := fs.lod.String()
	if fs.lod != forestLODOff {
		bandPct := int(math.Round(float64(d.scene.LODHysteresis()) * 100))
		lodSummary = fmt.Sprintf("%s · bias %.2fx · band %d%%", lodSummary, d.scene.LODBias(), bandPct)
	}
	fullTriangles := 0
	for _, t := range fs.trees {
		fullTriangles += len(fs.kinds[t.kind].plain.geometry.Indices) / 3
	}
	drawn, batches, activeTriangles := len(fs.trees), len(fs.kinds), fullTriangles
	if fs.copies {
		batches = len(fs.trees)
	}
	tierSummary := ""
	if fs.have {
		drawn, batches, activeTriangles = fs.drawn, fs.batches, fs.triangles
		if fs.lod != forestLODOff {
			tierSummary = fmt.Sprintf(" (tiers %s/%s/%s)", commas(fs.tiers[0]), commas(fs.tiers[1]), commas(fs.tiers[2]))
		}
	}
	return fmt.Sprintf("nt3d forest · l LOD · [/] bias · b band · i instancing · n trees · s shadows · a wind · d fog · 1/2/3 backend · q quit\n%s\n%s · %s · %s batches · %s / %s trees%s, %s / %s triangles · drag orbit · wheel zoom · r rotate · %s",
		content, d.rendering(), mode+" · "+lodSummary+" · "+fs.shadows.String(),
		commas(batches), commas(drawn), commas(len(fs.trees)), tierSummary, commas(activeTriangles), commas(fullTriangles), d.status)
}
