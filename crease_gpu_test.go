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
	for i := range f.Geometry {
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
	check(t, s.SetOutline(Root, OutlineStyle{Width: 2, Color: green, Crease: 30}))
	lined(true, green)
	lined(false, outlineInk)
	check(t, s.SetOutline(Root, OutlineStyle{Width: 2, Color: green}))
	lined(false, green)
	check(t, s.SetOutline(Root, OutlineStyle{Width: 2, Color: green, Crease: 30}))
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
		check(t, s.SetOutline(id, OutlineStyle{Width: 2, Color: outlineInk, Crease: crease}))
		return id
	}
	sharp := place("sharp", -.7, 30)
	place("blunt", .7, 50)
	f := creaseFrame(t, s)
	gpu, sw := creaseGPUOnce(t, f), softwareRender(f)
	for i := range f.Geometry {
		// The hinge's own edge, and a border, which every angle lines.
		fold := f.Instances[i].World.Point(nt3dmath.Vec3{Y: .5})
		border := f.Instances[i].World.Point(nt3dmath.Vec3{X: -1, Y: .5})
		want := f.Instances[i].Node == sharp
		for name, img := range map[string]image.Image{"GPU": gpu, "software": sw} {
			if inked(f, img, fold) != want || !inked(f, img, border) {
				t.Fatalf("%s node %d: fold line %v (want %v), border line %v", name, f.Instances[i].Node, inked(f, img, fold), want, inked(f, img, border))
			}
		}
	}
}

func TestGPUCreaseOffIsUnchanged(t *testing.T) {
	f := outlineFrame(t)
	plain := creaseGPUOnce(t, f)
	f.Geometry[0].Outline.Crease = 30
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
	check(t, s.SetOutline(Root, creaseStyle))
	f := creaseFrame(t, s)
	r := &gpuRenderer{}
	defer r.Close()
	gpu := creaseGPU(t, r, f)
	if len(r.draws) != 1 || r.draws[0].count != 3 {
		t.Fatalf("%d draws; want one of three instances", len(r.draws))
	}
	for i := range f.Geometry {
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
		check(t, s.SetOutline(Root, OutlineStyle{Width: 2, Color: outlineInk, Crease: float32(20 + i)}))
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
