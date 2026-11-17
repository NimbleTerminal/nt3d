// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func creaseCube(t *testing.T) Geometry {
	t.Helper()
	g, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 1, Y: 1, Z: 1}, Color: white})
	check(t, err)
	return g
}

// hinge is two unit quads sharing the Y axis edge, the second turned up by
// degrees from the first's plane.
func hinge(degrees float64) Geometry {
	r := degrees * math.Pi / 180
	c, s := float32(math.Cos(r)), float32(math.Sin(r))
	p := []nt3dmath.Vec3{{X: -1}, {}, {Y: 1}, {X: -1, Y: 1}, {X: c, Z: s}, {X: c, Y: 1, Z: s}}
	g := Geometry{Indices: []uint32{0, 1, 2, 0, 2, 3, 1, 4, 5, 1, 5, 2}}
	for _, v := range p {
		g.Vertices = append(g.Vertices, Vertex{Position: v, Normal: up, Color: white})
	}
	return g
}

func hasEdge(edges []nt3dmath.Vec3, a, b nt3dmath.Vec3) int {
	n := 0
	for i := 0; i+1 < len(edges); i += 2 {
		if edges[i] == a && edges[i+1] == b || edges[i] == b && edges[i+1] == a {
			n++
		}
	}
	return n
}

func TestCreaseEdgesCube(t *testing.T) {
	g := creaseCube(t)
	edges := creaseEdges(g.Vertices, g.Indices, 30)
	if len(edges) != 24 {
		t.Fatalf("%d endpoints, want 24", len(edges))
	}
	for i := 0; i < len(edges); i += 2 {
		d := edges[i+1].Sub(edges[i])
		axes := 0
		for _, v := range []float32{d.X, d.Y, d.Z} {
			if v != 0 {
				axes++
				if abs(abs(v)-1) > 1e-6 {
					t.Fatalf("edge %v is not a side long", d)
				}
			}
		}
		if axes != 1 {
			t.Fatalf("edge %v is not along an axis", d)
		}
	}
}

func TestCreaseEdgesSphere(t *testing.T) {
	g, err := BuildSphere(Sphere{Radius: 1, Segments: 40, Rings: 24, Color: white})
	check(t, err)
	if edges := creaseEdges(g.Vertices, g.Indices, 30); edges != nil {
		t.Fatalf("smooth sphere has %d crease endpoints", len(edges))
	}
	if creaseEdges(g.Vertices, g.Indices, 1) == nil {
		t.Fatal("a one degree threshold found no edges on a faceted sphere")
	}
}

func TestCreaseEdgesOpenQuad(t *testing.T) {
	g := quad(1, 0, white, nil, false)
	edges := creaseEdges(g.Vertices, g.Indices, 30)
	if len(edges) != 8 {
		t.Fatalf("%d endpoints, want 8", len(edges))
	}
	if hasEdge(edges, g.Vertices[0].Position, g.Vertices[2].Position) != 0 {
		t.Fatal("the diagonal got a line")
	}
}

func TestCreaseEdgesSplitNormals(t *testing.T) {
	g := creaseCube(t)
	if len(g.Vertices) != 24 {
		t.Fatalf("cuboid has %d vertices; the test wants per-face normals", len(g.Vertices))
	}
	// The same cube welded to eight vertices.
	welded := Geometry{}
	index := map[nt3dmath.Vec3]uint32{}
	for _, i := range g.Indices {
		p := g.Vertices[i].Position
		if _, ok := index[p]; !ok {
			index[p] = uint32(len(welded.Vertices))
			welded.Vertices = append(welded.Vertices, Vertex{Position: p, Normal: up, Color: white})
		}
		welded.Indices = append(welded.Indices, index[p])
	}
	split, joined := creaseEdges(g.Vertices, g.Indices, 30), creaseEdges(welded.Vertices, welded.Indices, 30)
	if len(welded.Vertices) != 8 || len(split) != 24 || len(joined) != 24 {
		t.Fatalf("%d welded vertices, %d and %d endpoints", len(welded.Vertices), len(split), len(joined))
	}
	for i := 0; i < len(split); i += 2 {
		if hasEdge(joined, split[i], split[i+1]) != 1 {
			t.Fatalf("edge %v %v is not in the welded cube's", split[i], split[i+1])
		}
	}
}

func TestCreaseEdgesThreshold(t *testing.T) {
	g := hinge(40)
	a, b := g.Vertices[1].Position, g.Vertices[2].Position
	if hasEdge(creaseEdges(g.Vertices, g.Indices, 30), a, b) != 1 {
		t.Fatal("a 40 degree hinge has no line at 30")
	}
	if hasEdge(creaseEdges(g.Vertices, g.Indices, 50), a, b) != 0 {
		t.Fatal("a 40 degree hinge has a line at 50")
	}
}

func TestCreaseEdgesDegenerate(t *testing.T) {
	// Three fins on one edge, and a triangle of no area.
	g := Geometry{Indices: []uint32{0, 1, 2, 0, 1, 3, 0, 1, 4, 2, 2, 3, 0, 5, 1}}
	for _, p := range []nt3dmath.Vec3{{}, {Y: 1}, {X: 1}, {X: -1}, {Z: 1}, {Y: .5}} {
		g.Vertices = append(g.Vertices, Vertex{Position: p, Normal: up, Color: white})
	}
	edges := creaseEdges(g.Vertices, g.Indices, 30)
	for _, p := range edges {
		if !validFloat(p.X, p.Y, p.Z) {
			t.Fatalf("endpoint %v", p)
		}
	}
	if n := hasEdge(edges, g.Vertices[0].Position, g.Vertices[1].Position); n != 1 {
		t.Fatalf("the three-face edge appears %d times", n)
	}
}

func TestCreaseDisabled(t *testing.T) {
	g := creaseCube(t)
	if creaseEdges(g.Vertices, g.Indices, 0) != nil || creaseEdges(g.Vertices, nil, 30) != nil {
		t.Fatal("edges without a threshold or without triangles")
	}
}

func TestCreaseValidation(t *testing.T) {
	s := NewScene()
	_, err := s.SetGeometry("cube", creaseCube(t))
	check(t, err)
	for _, c := range []float32{-1, 181, float32(math.NaN())} {
		style := OutlineStyle{Width: 2, Crease: c}
		if style.validate() == nil || s.SetOutline(Root, style) == nil {
			t.Fatalf("accepted crease %v", c)
		}
	}
	for _, c := range []float32{0, 180} {
		check(t, s.SetOutline(Root, OutlineStyle{Width: 2, Crease: c}))
	}
}

func TestCreasedEligibility(t *testing.T) {
	style := OutlineStyle{Width: 2, Crease: 30}
	g := creaseCube(t)
	g.Outline = style
	if !creased(&g) {
		t.Fatal("an opaque rigid outlined mesh draws no crease lines")
	}
	for name, change := range map[string]func(*Geometry){
		"no width":     func(g *Geometry) { g.Outline.Width = 0 },
		"no crease":    func(g *Geometry) { g.Outline.Crease = 0 },
		"no triangles": func(g *Geometry) { g.Indices = nil },
	} {
		other := g
		change(&other)
		if creased(&other) {
			t.Errorf("%s: draws crease lines", name)
		}
	}
	for name, change := range map[string]func(*Geometry){
		"transparent":  func(g *Geometry) { g.Material = &Material{Transparent: true} },
		"cutout":       func(g *Geometry) { g.Material = &Material{Cutout: true} },
		"transmitting": func(g *Geometry) { g.Material = &Material{Transmission: .5} },
	} {
		other := g
		change(&other)
		if !creased(&other) {
			t.Errorf("%s: skipped crease lines", name)
		}
	}
	s, _, _ := animationSkinScene(t)
	check(t, s.SetOutline(Root, style))
	if f := frame(t, s); f.Draws[0].Geometry.Outline != style || !creased(&f.Draws[0].Geometry) {
		t.Fatal("a deformed mesh skipped crease lines")
	}
}

var creaseGray = color.RGBA{200, 200, 200, 255}

// creaseStyle is the ink the crease tests look for.
var creaseStyle = OutlineStyle{Width: 2, Color: outlineInk, Crease: 30}

// creaseScene is evenly lit gray half-unit cubes, one per transform, each
// with style.
func creaseScene(t *testing.T, style OutlineStyle, m *Material, transforms ...nt3dmath.Mat4) (*Scene, Frame) {
	t.Helper()
	g, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: .5, Y: .5, Z: .5}, Color: creaseGray, Material: m})
	check(t, err)
	g.Outline = style
	s := NewScene()
	for i, world := range transforms {
		id, err := s.SetGeometry(string(rune('a'+i)), g)
		check(t, err)
		check(t, s.SetTransform(id, world))
	}
	return s, creaseFrame(t, s)
}

func creaseFrame(t *testing.T, s *Scene) Frame {
	t.Helper()
	f, err := s.Frame(DefaultCamera(), 200, 200)
	check(t, err)
	f.Lighting, f.Background = even, toonSky
	return f
}

// cornerEdges are the midpoints, in world space, of the three edges that
// meet at the corner of f's instance i nearest the viewer (or farthest).
func cornerEdges(f Frame, i int, farthest bool) (mids []nt3dmath.Vec3) {
	world := f.Draws[i].World
	var corner nt3dmath.Vec3
	best := float32(math.Inf(1))
	for _, sx := range []float32{-.25, .25} {
		for _, sy := range []float32{-.25, .25} {
			for _, sz := range []float32{-.25, .25} {
				c := nt3dmath.Vec3{X: sx, Y: sy, Z: sz}
				_, _, z, _ := f.Matrix.Project(world.Point(c), f.Width, f.Height)
				if farthest {
					z = -z
				}
				if z < best {
					best, corner = z, c
				}
			}
		}
	}
	for axis := range 3 {
		m := corner
		*[]*float32{&m.X, &m.Y, &m.Z}[axis] = 0
		mids = append(mids, world.Point(m))
	}
	return mids
}

// inked reports whether a pixel within a pixel of world point p is ink.
func inked(f Frame, img image.Image, p nt3dmath.Vec3) bool {
	x, y := pixel(f, p)
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if at(img, x+dx, y+dy) == outlineInk {
				return true
			}
		}
	}
	return false
}

func TestCreaseLinesRendered(t *testing.T) {
	_, f := creaseScene(t, creaseStyle, nil, nt3dmath.Identity())
	img := softwareRender(f)
	for _, mid := range cornerEdges(f, 0, false) {
		if !inked(f, img, mid) {
			x, y := pixel(f, mid)
			t.Fatalf("no crease line at %v: %v", mid, at(img, x, y))
		}
	}
	_, f = creaseScene(t, OutlineStyle{Width: 2, Color: outlineInk}, nil, nt3dmath.Identity())
	img = softwareRender(f)
	for _, mid := range cornerEdges(f, 0, false) {
		if x, y := pixel(f, mid); at(img, x, y) != creaseGray {
			t.Fatalf("without a crease angle, %v at %v", at(img, x, y), mid)
		}
	}
}

func TestCreaseZeroIsUnchanged(t *testing.T) {
	// A smooth sphere has no crease edges: asking for them changes nothing.
	f := outlineFrame(t)
	plain := softwareRender(f).(*image.RGBA)
	f.Draws[0].Geometry.Outline.Crease = 30
	if !creased(&f.Draws[0].Geometry) {
		t.Fatal("the sphere is not eligible")
	}
	if with := softwareRender(f).(*image.RGBA); !bytes.Equal(plain.Pix, with.Pix) {
		t.Fatal("a crease angle changed a mesh without crease edges")
	}
}

func TestCreaseLinesHidden(t *testing.T) {
	_, f := creaseScene(t, creaseStyle, nil, nt3dmath.Identity())
	img := softwareRender(f)
	for _, mid := range cornerEdges(f, 0, true) {
		if inked(f, img, mid) {
			t.Fatalf("a far edge shows through at %v", mid)
		}
	}
}

func TestCreaseLinesFollowInstances(t *testing.T) {
	_, f := creaseScene(t, creaseStyle, nil,
		nt3dmath.Translation(nt3dmath.Vec3{X: -.8}),
		nt3dmath.Translation(nt3dmath.Vec3{X: .6}).Mul(nt3dmath.Scaling(nt3dmath.Vec3{X: 2, Y: 1, Z: 1})))
	img := softwareRender(f)
	for i := range f.Draws {
		for _, mid := range cornerEdges(f, i, false) {
			if !inked(f, img, mid) {
				t.Fatalf("instance %d has no crease line at %v", i, mid)
			}
		}
	}
}

func creaseCutoutTextureScene(t *testing.T) Frame {
	t.Helper()
	half := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 4 {
			half.SetNRGBA(x, y, color.NRGBA{creaseGray.R, creaseGray.G, creaseGray.B, 255})
		}
	}
	tex, err := NewTexture(half)
	check(t, err)
	g := quad(.6, 0, white, &Material{Texture: tex, Cutout: true}, false)
	g.UV = []UV{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	g.Outline = creaseStyle
	s := NewScene()
	_, err = s.SetGeometry("cutout", g)
	check(t, err)
	return creaseFrame(t, s)
}

func creaseCutoutShaderScene(t *testing.T) Frame {
	t.Helper()
	const wgsl = `
fn nt3dMaterial(in: NT3DMaterialInput) -> vec4<f32> {
	let a = select(in.color.a, 0.0, in.world.x > 0.0);
	return vec4<f32>(in.color.rgb, a);
}
`
	sh, err := NewMaterialShader(wgsl, func(in MaterialColorInput) [4]float32 {
		c := in.Color
		if in.World.X > 0 {
			c[3] = 0
		}
		return c
	})
	check(t, err)
	g := quad(.6, 0, creaseGray, &Material{Cutout: true, Shader: sh}, false)
	g.Outline = creaseStyle
	s := NewScene()
	_, err = s.SetGeometry("cutout", g)
	check(t, err)
	return creaseFrame(t, s)
}

func creaseTransmissiveScene(t *testing.T) (Frame, int) {
	t.Helper()
	s := NewScene()
	_, err := s.SetGeometry("floor", quad(2, -.6, color.RGBA{220, 140, 80, 255}, nil, false))
	check(t, err)
	cube, err := BuildCuboid(Cuboid{
		Size:     nt3dmath.Vec3{X: .5, Y: .5, Z: .5},
		Color:    white,
		Material: &Material{PBR: true, Transmission: .85, Roughness: .08, IOR: 1.5},
	})
	check(t, err)
	cube.Outline = creaseStyle
	cubeNode, err := s.SetGeometry("glass", cube)
	check(t, err)
	f := creaseFrame(t, s)
	for i, inst := range f.Draws {
		if inst.Node == cubeNode {
			return f, i
		}
	}
	t.Fatal("missing glass cube instance")
	return f, 0
}

func creaseTransparentScene(t *testing.T, style OutlineStyle, alpha uint8) Frame {
	t.Helper()
	g, err := BuildCuboid(Cuboid{
		Size:     nt3dmath.Vec3{X: .5, Y: .5, Z: .5},
		Color:    color.RGBA{200, 200, 200, alpha},
		Material: &Material{Transparent: true},
	})
	check(t, err)
	g.Outline = style
	s := NewScene()
	_, err = s.SetGeometry("glass", g)
	check(t, err)
	return creaseFrame(t, s)
}

func inkBlendedNear(f Frame, creasedImg, plainImg image.Image, p nt3dmath.Vec3) bool {
	iabs := func(v int) int {
		if v < 0 {
			return -v
		}
		return v
	}
	dist := func(a, b color.RGBA) int {
		return iabs(int(a.R)-int(b.R)) + iabs(int(a.G)-int(b.G)) + iabs(int(a.B)-int(b.B))
	}
	x, y := pixel(f, p)
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			c, pl := at(creasedImg, x+dx, y+dy), at(plainImg, x+dx, y+dy)
			if dist(c, outlineInk)+30 < dist(pl, outlineInk) {
				return true
			}
		}
	}
	return false
}

func TestCreaseSeeThroughMeshes(t *testing.T) {
	for name, f := range map[string]Frame{
		"texture cutout": creaseCutoutTextureScene(t),
		"shader cutout":  creaseCutoutShaderScene(t),
	} {
		img := softwareRender(f)
		kept := nt3dmath.Vec3{X: -.3, Y: .6}
		discarded := nt3dmath.Vec3{X: .3, Y: .6}
		if !inked(f, img, kept) {
			t.Fatalf("%s: opaque border at %v is not inked", name, kept)
		}
		if inked(f, img, discarded) {
			t.Fatalf("%s: discarded border at %v drew ink", name, discarded)
		}
	}

	tf, glassIdx := creaseTransmissiveScene(t)
	timg := softwareRender(tf)
	for _, mid := range cornerEdges(tf, glassIdx, false) {
		if !inked(tf, timg, mid) {
			t.Fatalf("transmissive near edge at %v has no ink", mid)
		}
	}
	for _, mid := range cornerEdges(tf, glassIdx, true) {
		if inked(tf, timg, mid) {
			t.Fatalf("transmissive far edge at %v leaked through front glass depth", mid)
		}
	}

	for _, oit := range []bool{false, true} {
		with := creaseTransparentScene(t, creaseStyle, 150)
		without := creaseTransparentScene(t, OutlineStyle{Width: 2, Color: outlineInk}, 150)
		with.OIT, without.OIT = oit, oit
		imgWith, imgWithout := softwareRender(with), softwareRender(without)
		for _, mid := range cornerEdges(with, 0, false) {
			if !inkBlendedNear(with, imgWith, imgWithout, mid) {
				t.Fatalf("transparent cube (OIT=%v) did not blend crease ink at %v", oit, mid)
			}
		}
		zero := creaseTransparentScene(t, creaseStyle, 0)
		zeroPlain := creaseTransparentScene(t, OutlineStyle{}, 0)
		zero.OIT, zeroPlain.OIT = oit, oit
		if !bytes.Equal(softwareRender(zero).(*image.RGBA).Pix, softwareRender(zeroPlain).(*image.RGBA).Pix) {
			t.Fatalf("zero-alpha mesh (OIT=%v) drew visible crease ink", oit)
		}
	}
}

func creaseSkinnedScene(t *testing.T) (*Scene, NodeID) {
	t.Helper()
	s := NewScene()
	joint, err := s.Add(Root, "joint")
	check(t, err)
	g := hinge(90)
	for i := range g.Vertices {
		g.Vertices[i].Position = g.Vertices[i].Position.Scale(.4)
		g.Vertices[i].Color = creaseGray
	}
	g.Outline = creaseStyle
	id, err := s.SetGeometry("hinge", g)
	check(t, err)
	inf := make([]Influence, len(g.Vertices))
	for i := range inf {
		inf[i] = Influence{Weights: [4]float32{1}}
	}
	check(t, s.SetDeformation(id, Deformation{
		Skeleton:   &Skeleton{Joints: []NodeID{joint}},
		Influences: inf,
	}))
	return s, joint
}

func creaseMorphScene(t *testing.T) (*Scene, NodeID) {
	t.Helper()
	s := NewScene()
	g := hinge(5)
	for i := range g.Vertices {
		g.Vertices[i].Position = g.Vertices[i].Position.Scale(.4)
		g.Vertices[i].Color = creaseGray
	}
	g.Outline = creaseStyle
	id, err := s.SetGeometry("morph", g)
	check(t, err)
	offsets := make([]nt3dmath.Vec3, len(g.Vertices))
	offsets[4] = nt3dmath.Vec3{Z: .6}
	offsets[5] = nt3dmath.Vec3{Z: .6}
	check(t, s.SetDeformation(id, Deformation{
		Morphs: []MorphTarget{{Positions: offsets}},
	}))
	return s, id
}

func creaseFoldDisplacement(t *testing.T, lift float32) *VertexDisplacement {
	t.Helper()
	const wgsl = `
fn nt3dDisplace(in: NT3DVertexInput) -> NT3DVertexOutput {
	let dz = nt3dDisplaceParam(0u).x * select(0.0, 1.0, in.position.x > 0.2);
	return NT3DVertexOutput(in.position + vec3<f32>(0.0, 0.0, dz), in.normal);
}
`
	d, err := NewVertexDisplacementWithParameters(wgsl, .8, 0, [][4]float32{{lift, 0, 0, 0}}, func(in VertexDisplaceInput) VertexDisplaceOutput {
		dz := float32(0)
		if in.Position.X > .2 {
			dz = in.Params[0][0]
		}
		return VertexDisplaceOutput{
			Position: in.Position.Add(nt3dmath.Vec3{Z: dz}),
			Normal:   in.Normal,
		}
	})
	check(t, err)
	return d
}

func creaseShiftDisplacement(t *testing.T, dx float32) *VertexDisplacement {
	t.Helper()
	const wgsl = `
fn nt3dDisplace(in: NT3DVertexInput) -> NT3DVertexOutput {
	let dx = nt3dDisplaceParam(0u).x;
	return NT3DVertexOutput(in.position + vec3<f32>(dx, 0.0, 0.0), in.normal);
}
`
	d, err := NewVertexDisplacementWithParameters(wgsl, abs(dx)+.2, 0, [][4]float32{{dx, 0, 0, 0}}, func(in VertexDisplaceInput) VertexDisplaceOutput {
		return VertexDisplaceOutput{
			Position: in.Position.Add(nt3dmath.Vec3{X: in.Params[0][0]}),
			Normal:   in.Normal,
		}
	})
	check(t, err)
	return d
}

func TestCreaseDeformingMeshes(t *testing.T) {
	skinS, joint := creaseSkinnedScene(t)
	f0 := creaseFrame(t, skinS)
	img0 := softwareRender(f0)
	if !inked(f0, img0, nt3dmath.Vec3{Y: .2}) {
		t.Fatal("skinned hinge fold has no crease at rest")
	}
	check(t, skinS.SetTransform(joint, nt3dmath.Translation(nt3dmath.Vec3{X: .55})))
	f1 := creaseFrame(t, skinS)
	img1 := softwareRender(f1)
	if inked(f1, img1, nt3dmath.Vec3{Y: .2}) || !inked(f1, img1, nt3dmath.Vec3{X: .55, Y: .2}) {
		t.Fatal("skinned hinge crease did not follow the joint transform")
	}

	morphS, morphID := creaseMorphScene(t)
	flatF := creaseFrame(t, morphS)
	flatImg := softwareRender(flatF)
	if inked(flatF, flatImg, nt3dmath.Vec3{Y: .2}) || !inked(flatF, flatImg, nt3dmath.Vec3{X: -.4, Y: .2}) {
		t.Fatal("flat morph hinge should ink its border but not its 5-degree fold")
	}
	check(t, morphS.SetMorphWeights(morphID, []float32{1}))
	bentF := creaseFrame(t, morphS)
	bentImg := softwareRender(bentF)
	if !inked(bentF, bentImg, nt3dmath.Vec3{Y: .2}) {
		t.Fatal("bent morph hinge did not gain a fold crease line")
	}

	dispS := NewScene()
	dg := hinge(5)
	for i := range dg.Vertices {
		dg.Vertices[i].Position = dg.Vertices[i].Position.Scale(.4)
		dg.Vertices[i].Color = creaseGray
	}
	dg.Outline = creaseStyle
	dg.Displacement = creaseFoldDisplacement(t, 0)
	dispID, err := dispS.SetGeometry("disp", dg)
	check(t, err)
	df0 := creaseFrame(t, dispS)
	if inked(df0, softwareRender(df0), nt3dmath.Vec3{Y: .2}) {
		t.Fatal("unfolded displaced hinge drew a fold crease")
	}
	creaseCache.Lock()
	beforeEntries := len(creaseCache.entries)
	creaseCache.Unlock()
	for step := 1; step <= 10; step++ {
		dg.Displacement = creaseFoldDisplacement(t, float32(step)*.06)
		check(t, dispS.SetNodeGeometry(dispID, dg))
		df := creaseFrame(t, dispS)
		img := softwareRender(df)
		if step == 10 && !inked(df, img, nt3dmath.Vec3{Y: .2}) {
			t.Fatal("folded displaced hinge did not draw a fold crease")
		}
	}
	creaseCache.Lock()
	grown := len(creaseCache.entries) - beforeEntries
	creaseCache.Unlock()
	if grown != 0 {
		t.Fatalf("per-frame vertex displacement grew creaseCache by %d entries", grown)
	}
}

func TestCreaseSharedAngles(t *testing.T) {
	g, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: .5, Y: .5, Z: .5}, Color: creaseGray})
	check(t, err)
	shared, err := NewSharedGeometry(g)
	check(t, err)
	s := NewScene()
	a, err := s.SetShared("a", shared)
	check(t, err)
	b, err := s.SetShared("b", shared)
	check(t, err)
	check(t, s.SetTransform(a, nt3dmath.Translation(nt3dmath.Vec3{X: -.6})))
	check(t, s.SetTransform(b, nt3dmath.Translation(nt3dmath.Vec3{X: .6})))
	check(t, s.SetOutline(a, creaseStyle))
	check(t, s.SetOutline(b, OutlineStyle{Width: 2, Color: outlineInk}))
	f := creaseFrame(t, s)
	img := softwareRender(f)
	for i := range f.Draws {
		want := f.Draws[i].Node == a
		for _, mid := range cornerEdges(f, i, false) {
			if inked(f, img, mid) != want {
				t.Fatalf("node %d: crease line %v at %v", f.Draws[i].Node, !want, mid)
			}
		}
	}
}

func TestCreaseCacheReuse(t *testing.T) {
	// The scene keeps a copy of the mesh for each node: two meshes.
	_, f := creaseScene(t, creaseStyle, nil, nt3dmath.Translation(nt3dmath.Vec3{X: -.6}), nt3dmath.Translation(nt3dmath.Vec3{X: .6}))
	before := creaseExtractions.Load()
	softwareRender(f)
	first := creaseExtractions.Load() - before
	softwareRender(f)
	if again := creaseExtractions.Load() - before - first; first < 1 || first > 2 || again != 0 {
		t.Fatalf("%d extractions in the first frame, %d in the second", first, again)
	}
}

// A mesh keeps the edges of its last two crease angles, not of every angle
// it ever had: an angle slider must not grow the cache.
func TestCreaseCacheBounded(t *testing.T) {
	_, f := creaseScene(t, creaseStyle, nil, nt3dmath.Identity())
	softwareRender(f)
	creaseCache.Lock()
	meshes := len(creaseCache.entries)
	creaseCache.Unlock()
	for i := range 50 {
		f.Draws[0].Geometry.Outline.Crease = float32(20 + i)
		softwareRender(f)
	}
	creaseCache.Lock()
	grown := len(creaseCache.entries) - meshes
	creaseCache.Unlock()
	if grown != 0 {
		t.Fatalf("50 crease angles on one mesh added %d cache entries", grown)
	}
	// Flipping between two angles extracts nothing more.
	before := creaseExtractions.Load()
	for _, c := range []float32{69, 68, 69, 68} {
		f.Draws[0].Geometry.Outline.Crease = c
		softwareRender(f)
	}
	if n := creaseExtractions.Load() - before; n != 0 {
		t.Fatalf("%d extractions flipping between the last two angles", n)
	}
}
