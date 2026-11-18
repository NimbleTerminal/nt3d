//go:build webgpu

package nt3d

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// creaseGPU renders a scene's frame on r, as a copy the next render does not
// overwrite. Frames of one scene share r's caches; give each scene its own r.
func creaseGPU(t *testing.T, r *gpuRenderer, f Frame) *image.RGBA {
	t.Helper()
	img, err := r.Render(f)
	check(t, err)
	out := image.NewRGBA(img.Bounds())
	draw.Draw(out, img.Bounds(), img, image.Point{}, draw.Src)
	return out
}

func creaseGPUOnce(t *testing.T, f Frame) *image.RGBA {
	t.Helper()
	r := &gpuRenderer{}
	defer r.Close()
	return creaseGPU(t, r, f)
}

func TestGPUCreaseMatchesSoftware(t *testing.T) {
	_, f := creaseScene(t, creaseStyle, nil, nt3dmath.Identity())
	gpu, sw := creaseGPUOnce(t, f), softwareRender(f)
	for _, mid := range cornerEdges(f, 0, false) {
		if !inked(f, gpu, mid) {
			x, y := pixel(f, mid)
			t.Fatalf("GPU has no crease line at %v: %v", mid, at(gpu, x, y))
		}
	}
	for _, mid := range cornerEdges(f, 0, true) {
		if inked(f, gpu, mid) {
			t.Fatalf("GPU shows a far edge at %v", mid)
		}
	}
	off := 0
	for y := range f.Height {
		for x := range f.Width {
			if !closeRGB(at(gpu, x, y), at(sw, x, y), 6) {
				off++
			}
		}
	}
	if off > 600 {
		t.Fatalf("GPU and software differ at %d pixels", off)
	}
	_, f = creaseScene(t, OutlineStyle{Width: 2, Color: outlineInk}, nil, nt3dmath.Identity())
	gpu = creaseGPUOnce(t, f)
	for _, mid := range cornerEdges(f, 0, false) {
		if x, y := pixel(f, mid); at(gpu, x, y) != creaseGray {
			t.Fatalf("without a crease angle, GPU %v at %v", at(gpu, x, y), mid)
		}
	}
}

func TestGPUCreaseFollowsInstances(t *testing.T) {
	_, f := creaseScene(t, creaseStyle, nil,
		nt3dmath.Translation(nt3dmath.Vec3{X: -.8}),
		nt3dmath.Translation(nt3dmath.Vec3{X: .6}).Mul(nt3dmath.Scaling(nt3dmath.Vec3{X: 2, Y: 1, Z: 1})))
	gpu := creaseGPUOnce(t, f)
	for i := range f.Draws {
		for _, mid := range cornerEdges(f, i, false) {
			if !inked(f, gpu, mid) {
				t.Fatalf("instance %d has no GPU crease line at %v", i, mid)
			}
		}
	}
}

func TestGPUCreaseWorldUnits(t *testing.T) {
	ink := func(width float32) (gpu, sw int) {
		_, f := creaseScene(t, OutlineStyle{Width: width, Units: WorldUnits, Color: outlineInk, Crease: 30}, nil, nt3dmath.Identity())
		img := creaseGPUOnce(t, f)
		for _, mid := range cornerEdges(f, 0, false) {
			if !inked(f, img, mid) {
				t.Fatalf("no world-unit crease line at %v", mid)
			}
		}
		return coloredPixels(img, outlineInk), coloredPixels(softwareRender(f), outlineInk)
	}
	thin, thinSW := ink(.03)
	wide, wideSW := ink(.09)
	if wide < 2*thin {
		t.Fatalf("%d ink pixels at .09 world units, %d at .03", wide, thin)
	}
	for _, pair := range [][2]int{{thin, thinSW}, {wide, wideSW}} {
		if 10*pair[0] < 6*pair[1] || 10*pair[1] < 6*pair[0] {
			t.Fatalf("GPU inks %d pixels, software %d", pair[0], pair[1])
		}
	}
}

func TestGPUCreaseRestyle(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	s, f := creaseScene(t, creaseStyle, nil, nt3dmath.Identity())
	mids := cornerEdges(f, 0, false)
	lined := func(want bool, ink color.RGBA) {
		t.Helper()
		f := creaseFrame(t, s)
		img := creaseGPU(t, r, f)
		for _, mid := range mids {
			x, y := pixel(f, mid)
			found := false
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					found = found || at(img, x+dx, y+dy) == ink
				}
			}
			if found != want {
				t.Fatalf("crease line %v at %v, want %v: %v", found, mid, want, at(img, x, y))
			}
		}
	}
	lined(true, outlineInk)
	green := color.RGBA{20, 160, 60, 255}
	check(t, s.ApplyOutline(Root, OutlineStyle{Width: 2, Color: green, Crease: 30}))
	lined(true, green)
	lined(false, outlineInk)
	check(t, s.ApplyOutline(Root, OutlineStyle{Width: 2, Color: green}))
	lined(false, green)
	check(t, s.ApplyOutline(Root, OutlineStyle{Width: 2, Color: green, Crease: 30}))
	lined(true, green)
}

func TestGPUCreaseSharedAngles(t *testing.T) {
	g := hinge(40)
	for i := range g.Vertices {
		g.Vertices[i].Color = creaseGray
	}
	g.Bounds = GeometryBounds(g)
	shared, err := NewSharedGeometry(g)
	check(t, err)
	s := NewScene()
	place := func(name string, x float32, crease float32) NodeID {
		id, err := s.SetShared(name, shared)
		check(t, err)
		check(t, s.SetTransform(id, nt3dmath.Translation(nt3dmath.Vec3{X: x}).Mul(nt3dmath.Scaling(nt3dmath.Vec3{X: .4, Y: .4, Z: .4}))))
		check(t, s.ApplyOutline(id, OutlineStyle{Width: 2, Color: outlineInk, Crease: crease}))
		return id
	}
	sharp := place("sharp", -.7, 30)
	place("blunt", .7, 50)
	f := creaseFrame(t, s)
	gpu, sw := creaseGPUOnce(t, f), softwareRender(f)
	for i := range f.Draws {
		// The hinge's own edge, and a border, which every angle lines.
		fold := f.Draws[i].World.Point(nt3dmath.Vec3{Y: .5})
		border := f.Draws[i].World.Point(nt3dmath.Vec3{X: -1, Y: .5})
		want := f.Draws[i].Node == sharp
		for name, img := range map[string]image.Image{"GPU": gpu, "software": sw} {
			if inked(f, img, fold) != want || !inked(f, img, border) {
				t.Fatalf("%s node %d: fold line %v (want %v), border line %v", name, f.Draws[i].Node, inked(f, img, fold), want, inked(f, img, border))
			}
		}
	}
}

func TestGPUCreaseOffIsUnchanged(t *testing.T) {
	f := outlineFrame(t)
	plain := creaseGPUOnce(t, f)
	f.Draws[0].Geometry.Outline.Crease = 30
	f.Revision++
	if with := creaseGPUOnce(t, f); !bytes.Equal(plain.Pix, with.Pix) {
		t.Fatal("a crease angle changed a GPU mesh without crease edges")
	}
}

// One shared mesh on three nodes with one style is a single instanced draw:
// each instance, not only the first, gets its crease lines.
func TestGPUCreaseInstancedDraw(t *testing.T) {
	g, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: .5, Y: .5, Z: .5}, Color: creaseGray})
	check(t, err)
	shared, err := NewSharedGeometry(g)
	check(t, err)
	s := NewScene()
	for i, world := range []nt3dmath.Mat4{
		nt3dmath.Translation(nt3dmath.Vec3{X: -1}),
		nt3dmath.Translation(nt3dmath.Vec3{Y: .2}).Mul(nt3dmath.Scaling(nt3dmath.Vec3{X: 1, Y: 1.5, Z: 1})),
		nt3dmath.Translation(nt3dmath.Vec3{X: 1.1}).Mul(nt3dmath.Scaling(nt3dmath.Vec3{X: 1.6, Y: 1, Z: .7})),
	} {
		id, err := s.SetShared(string(rune('a'+i)), shared)
		check(t, err)
		check(t, s.SetTransform(id, world))
	}
	check(t, s.ApplyOutline(Root, creaseStyle))
	f := creaseFrame(t, s)
	r := &gpuRenderer{}
	defer r.Close()
	gpu := creaseGPU(t, r, f)
	if len(r.draws) != 1 || r.draws[0].count != 3 {
		t.Fatalf("%d draws; want one of three instances", len(r.draws))
	}
	for i := range f.Draws {
		for _, mid := range cornerEdges(f, i, false) {
			if !inked(f, gpu, mid) {
				t.Fatalf("instance %d has no crease line at %v", i, mid)
			}
		}
	}
}

// A mesh keeps few crease angles' buffers: an angle slider must not leak one
// buffer and bind group a step.
func TestGPUCreaseAnglesBounded(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	s, f := creaseScene(t, creaseStyle, nil, nt3dmath.Identity())
	mids := cornerEdges(f, 0, false)
	var img *image.RGBA
	for i := range 30 {
		check(t, s.ApplyOutline(Root, OutlineStyle{Width: 2, Color: outlineInk, Crease: float32(20 + i)}))
		f = creaseFrame(t, s)
		img = creaseGPU(t, r, f)
	}
	buffers, binds := 0, 0
	for _, node := range r.geometries {
		buffers += len(node.creases)
		for _, set := range node.sets {
			binds += len(set.creaseBinds)
		}
	}
	if buffers > maxCreaseAngles || binds > maxCreaseAngles {
		t.Fatalf("%d crease buffers and %d bind groups after 30 angles", buffers, binds)
	}
	for _, mid := range mids {
		if !inked(f, img, mid) {
			t.Fatalf("the latest angle draws no line at %v", mid)
		}
	}
}

func TestGPUCreaseSeeThroughMatchesSoftware(t *testing.T) {
	for name, f := range map[string]Frame{
		"texture cutout": creaseCutoutTextureScene(t),
		"shader cutout":  creaseCutoutShaderScene(t),
	} {
		gpu, sw := creaseGPUOnce(t, f), softwareRender(f)
		kept := nt3dmath.Vec3{X: -.3, Y: .6}
		discarded := nt3dmath.Vec3{X: .3, Y: .6}
		if !inked(f, gpu, kept) {
			t.Fatalf("%s: GPU opaque border at %v is not inked", name, kept)
		}
		if inked(f, gpu, discarded) {
			t.Fatalf("%s: GPU discarded border at %v drew ink", name, discarded)
		}
		if mad := hookImgMeanAbsDiff(gpu, sw); mad > 2.5 {
			t.Fatalf("%s: GPU vs software MAD = %.3f", name, mad)
		}
	}

	tf, glassIdx := creaseTransmissiveScene(t)
	tgpu, tsw := creaseGPUOnce(t, tf), softwareRender(tf)
	for _, mid := range cornerEdges(tf, glassIdx, false) {
		if !inked(tf, tgpu, mid) {
			t.Fatalf("GPU transmissive near edge at %v has no ink", mid)
		}
	}
	for _, mid := range cornerEdges(tf, glassIdx, true) {
		if inked(tf, tgpu, mid) {
			t.Fatalf("GPU transmissive far edge at %v leaked through front glass depth", mid)
		}
	}
	if mad := hookImgMeanAbsDiff(tgpu, tsw); mad > 4.5 {
		t.Fatalf("transmissive crease GPU vs software MAD = %.3f", mad)
	}

	for _, oit := range []bool{false, true} {
		for _, msaa := range []bool{false, true} {
			with := creaseTransparentScene(t, creaseStyle, 150)
			without := creaseTransparentScene(t, OutlineStyle{Width: 2, Color: outlineInk}, 150)
			with.OIT, without.OIT = oit, oit
			with.MSAA, without.MSAA = msaa, msaa
			gpuWith, gpuWithout := creaseGPUOnce(t, with), creaseGPUOnce(t, without)
			for _, mid := range cornerEdges(with, 0, false) {
				if !inkBlendedNear(with, gpuWith, gpuWithout, mid) {
					t.Fatalf("GPU transparent cube (OIT=%v, MSAA=%v) did not blend crease ink at %v", oit, msaa, mid)
				}
			}
			if !msaa {
				swWith := softwareRender(with)
				if mad := hookImgMeanAbsDiff(gpuWith, swWith); mad > 4.0 {
					t.Fatalf("transparent crease (OIT=%v) GPU vs software MAD = %.3f", oit, mad)
				}
			}
		}
		zero := creaseTransparentScene(t, creaseStyle, 0)
		zeroPlain := creaseTransparentScene(t, OutlineStyle{}, 0)
		zero.OIT, zeroPlain.OIT = oit, oit
		if !bytes.Equal(creaseGPUOnce(t, zero).Pix, creaseGPUOnce(t, zeroPlain).Pix) {
			t.Fatalf("GPU zero-alpha mesh (OIT=%v) drew visible crease ink", oit)
		}
	}
}

func TestGPUCreaseDeformingMatchesSoftware(t *testing.T) {
	skinR := &gpuRenderer{}
	defer skinR.Close()
	skinS, joint := creaseSkinnedScene(t)
	f0 := creaseFrame(t, skinS)
	gpu0 := creaseGPU(t, skinR, f0)
	if !inked(f0, gpu0, nt3dmath.Vec3{Y: .2}) {
		t.Fatal("GPU skinned hinge fold has no crease at rest")
	}
	var f1 Frame
	var gpu1 *image.RGBA
	for step := 1; step <= 11; step++ {
		check(t, skinS.SetTransform(joint, nt3dmath.Translation(nt3dmath.Vec3{X: float32(step) * .05})))
		f1 = creaseFrame(t, skinS)
		gpu1 = creaseGPU(t, skinR, f1)
	}
	if inked(f1, gpu1, nt3dmath.Vec3{Y: .2}) || !inked(f1, gpu1, nt3dmath.Vec3{X: .55, Y: .2}) {
		t.Fatal("GPU skinned hinge crease did not follow the joint transform")
	}
	for _, node := range skinR.geometries {
		if len(node.creases) > maxCreaseAngles {
			t.Fatalf("skinned mesh accumulated %d GPU crease buffers", len(node.creases))
		}
	}
	if mad := hookImgMeanAbsDiff(gpu1, softwareRender(f1)); mad > 2.5 {
		t.Fatalf("skinned crease GPU vs software MAD = %.3f", mad)
	}

	morphR := &gpuRenderer{}
	defer morphR.Close()
	morphS, morphID := creaseMorphScene(t)
	flatF := creaseFrame(t, morphS)
	flatGPU := creaseGPU(t, morphR, flatF)
	if inked(flatF, flatGPU, nt3dmath.Vec3{Y: .2}) || !inked(flatF, flatGPU, nt3dmath.Vec3{X: -.4, Y: .2}) {
		t.Fatal("GPU flat morph hinge should ink its border but not its 5-degree fold")
	}
	check(t, morphS.SetMorphWeights(morphID, []float32{1}))
	bentF := creaseFrame(t, morphS)
	bentGPU := creaseGPU(t, morphR, bentF)
	if !inked(bentF, bentGPU, nt3dmath.Vec3{Y: .2}) {
		t.Fatal("GPU bent morph hinge did not gain a fold crease line")
	}
	if mad := hookImgMeanAbsDiff(bentGPU, softwareRender(bentF)); mad > 2.5 {
		t.Fatalf("morph crease GPU vs software MAD = %.3f", mad)
	}

	// Single-instance vertex displacement re-evaluates crease topology per frame.
	dispR := &gpuRenderer{}
	defer dispR.Close()
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
	if inked(df0, creaseGPU(t, dispR, df0), nt3dmath.Vec3{Y: .2}) {
		t.Fatal("GPU unfolded displaced hinge drew a fold crease")
	}
	dg.Displacement = creaseFoldDisplacement(t, .6)
	check(t, dispS.SetNodeGeometry(dispID, dg))
	df1 := creaseFrame(t, dispS)
	dgpu1 := creaseGPU(t, dispR, df1)
	if !inked(df1, dgpu1, nt3dmath.Vec3{Y: .2}) {
		t.Fatal("GPU folded displaced hinge did not draw a fold crease")
	}
	if mad := hookImgMeanAbsDiff(dgpu1, softwareRender(df1)); mad > 2.5 {
		t.Fatalf("displaced fold GPU vs software MAD = %.3f", mad)
	}

	// Multi-instance SharedGeometry vertex displacement runs nt3d_displace per instance in vs_crease.
	instG, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: .5, Y: .5, Z: .5}, Color: creaseGray})
	check(t, err)
	instG.Displacement = creaseShiftDisplacement(t, .35)
	shared, err := NewSharedGeometry(instG)
	check(t, err)
	instS := NewScene()
	for i, x := range []float32{-1.0, .4} {
		id, err := instS.SetShared(string(rune('a'+i)), shared)
		check(t, err)
		check(t, instS.SetTransform(id, nt3dmath.Translation(nt3dmath.Vec3{X: x})))
	}
	check(t, instS.ApplyOutline(Root, creaseStyle))
	instF := creaseFrame(t, instS)
	instR := &gpuRenderer{}
	defer instR.Close()
	instGPU := creaseGPU(t, instR, instF)
	if len(instR.draws) != 1 || instR.draws[0].count != 2 {
		t.Fatalf("expected 1 instanced draw of 2 cubes, got %d draws", len(instR.draws))
	}
	instSW := softwareRender(instF)
	for i := range instF.Draws {
		for _, mid := range cornerEdges(instF, i, false) {
			shifted := mid.Add(nt3dmath.Vec3{X: .35})
			if !inked(instF, instGPU, shifted) {
				t.Fatalf("GPU instanced displaced cube %d missing crease at %v", i, shifted)
			}
		}
	}
	if mad := hookImgMeanAbsDiff(instGPU, instSW); mad > 3.0 {
		t.Fatalf("instanced displaced crease GPU vs software MAD = %.3f", mad)
	}
}
