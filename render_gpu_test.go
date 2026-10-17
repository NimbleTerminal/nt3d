//go:build webgpu

package nt3d

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/gogpu/wgpu"
	"image"
	"image/color"
	"image/draw"
	"math"
	"nimbleterminal.dev/nt3d/math"
	"testing"
)

// Hardware tests are explicit: failure to initialize a GPU fails this test,
// rather than silently validating only the software fallback.
func TestGPUSceneRevisions(t *testing.T) {
	s := NewScene()
	red := testTexture(t, color.RGBA{255, 0, 0, 255})
	green := testTexture(t, color.RGBA{0, 255, 0, 255})
	mesh, err := BuildHeightfield(Heightfield{NX: 3, NY: 3, MinX: -.7, MaxX: .7, MinY: -.7, MaxY: .7, Heights: make([]float32, 9), Material: &Material{Texture: red, Unlit: true}})
	check(t, err)
	id, err := s.SetGeometry("terrain", mesh)
	check(t, err)
	group, err := s.Add(Root, "group")
	check(t, err)
	check(t, s.Reparent(id, group))
	r := &gpuRenderer{}
	defer r.Close()
	f := frame(t, s)
	img, err := r.Render(f)
	check(t, err)
	if coloredPixels(img, color.RGBA{255, 0, 0, 255}) < 100 {
		t.Fatal("missing textured terrain")
	}
	uploads, textures := r.uploads, r.textureUploads
	check(t, s.SetTransform(group, nt3dmath.Translation(nt3dmath.Vec3{X: .25}).Mul(nt3dmath.RotationZ(.5))))
	_, err = r.Render(frame(t, s))
	check(t, err)
	if r.uploads != uploads {
		t.Fatal("parent motion uploaded mesh")
	}
	check(t, s.SetTexture("terrain", green))
	img, err = r.Render(frame(t, s))
	check(t, err)
	if r.uploads != uploads || r.textureUploads != textures+1 || coloredPixels(img, color.RGBA{0, 255, 0, 255}) < 100 {
		t.Fatal("texture update")
	}
	check(t, s.SetVisible(group, false))
	img, err = r.Render(frame(t, s))
	check(t, err)
	if r.uploads != uploads || coloredPixels(img, f.Background) != f.Width*f.Height {
		t.Fatal("visibility")
	}
	// Replay an old frame: its texture and transform remain valid.
	img, err = r.Render(f)
	check(t, err)
	if coloredPixels(img, color.RGBA{255, 0, 0, 255}) < 100 {
		t.Fatal("old frame corrupted")
	}
	// Same renderer, unrelated scene: no revision collision.
	other := NewScene()
	_, err = other.SetGeometry("point", pointGeometry())
	check(t, err)
	_, err = r.Render(frame(t, other))
	check(t, err)
	if r.uploads == uploads {
		t.Fatal("scene switch reused mesh")
	}
}
func TestGPUAllPrimitivesAndOverlays(t *testing.T) {
	s := NewScene()
	white := color.RGBA{255, 255, 255, 255}
	g := pointGeometry()
	g.Points[0].Position = nt3dmath.Vec3{X: -.6, Y: -.6, Z: .2}
	g.Boxes = []Box{{Min: nt3dmath.Vec3{X: .2, Y: .2}, Size: nt3dmath.Vec3{X: .3, Y: .3, Z: .4}, Color: white}}
	g.Vertices = []Vertex{{Position: nt3dmath.Vec3{X: -.5, Y: -.5}, Normal: nt3dmath.Vec3{Z: 1}, Color: white}, {Position: nt3dmath.Vec3{X: .5, Y: -.5}, Normal: nt3dmath.Vec3{Z: 1}, Color: white}, {Position: nt3dmath.Vec3{Y: .5}, Normal: nt3dmath.Vec3{Z: 1}, Color: white}}
	g.Indices = []uint32{0, 1, 2}
	g.Lines = []Vertex{{Position: nt3dmath.Vec3{X: -.7, Z: .5}, Color: white}, {Position: nt3dmath.Vec3{X: .7, Z: .5}, Color: white}}
	g.LineWidth = 3
	g.Arrows = []Arrow{{Start: nt3dmath.Vec3{Y: -.7, Z: .6}, End: nt3dmath.Vec3{Y: .7, Z: .6}, Color: white, Width: 3, HeadSize: 10}}
	id, err := s.SetGeometry("all", g)
	check(t, err)
	check(t, s.SetTransform(id, nt3dmath.RotationZ(.3).Mul(nt3dmath.Scaling(nt3dmath.Vec3{X: 1.2, Y: .8, Z: .9}))))
	r := &gpuRenderer{}
	defer r.Close()
	f := frame(t, s)
	f.Overlays = []OverlayRect{{X: 2, Y: 2, Width: 8, Height: 8, Color: color.RGBA{1, 2, 3, 255}}}
	f.DynamicLines = g.Lines
	img, err := r.Render(f)
	check(t, err)
	if coloredPixels(img, f.Background) > f.Width*f.Height-100 {
		t.Fatal("empty frame")
	}
	if color.RGBAModel.Convert(img.At(3, 3)) != (color.RGBA{1, 2, 3, 255}) {
		t.Fatal("overlay")
	}
	for _, batch := range []int{0, 1, 2, 3, 4, 5, 6} {
		b := r.batches[batch]
		if batch != 4 && batch != 5 {
			b = r.nodes[0].batches[batch]
		}
		if b.count == 0 {
			t.Fatalf("missing GPU batch %d", batch)
		}
	}
}

// Deliberately different fallback pixels prove the GPU samples the shader
// output rather than quietly displaying an uploaded CPU image.
func TestGPUProceduralTexture(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	green := color.RGBA{0, 255, 0, 255}
	fallback := testTexture(t, red)
	tex, err := NewShaderTexture(`fn nt3dTexture(uv: vec2<f32>) -> vec4<f32> { return vec4<f32>(0,1,0,1); }`, fallback)
	check(t, err)
	mesh, err := BuildHeightfield(Heightfield{NX: 3, NY: 3, MinX: -.7, MaxX: .7, MinY: -.7, MaxY: .7, Heights: make([]float32, 9), Material: &Material{Texture: tex, Unlit: true}})
	check(t, err)
	s := NewScene()
	_, err = s.SetGeometry("terrain", mesh)
	check(t, err)
	f := frame(t, s)
	r := &gpuRenderer{}
	defer r.Close()
	for range 2 {
		img, err := r.Render(f)
		check(t, err)
		if coloredPixels(img, green) < 100 || r.textureUploads != 0 {
			t.Fatal("shader output missing or fallback uploaded")
		}
	}
	sw := NewSoftwareRenderer()
	defer sw.Close()
	img, err := sw.Render(f)
	check(t, err)
	if coloredPixels(img, red) < 100 {
		t.Fatal("software fallback missing")
	}
	check(t, s.SetTexture("terrain", fallback))
	img, err = r.Render(frame(t, s))
	check(t, err)
	if len(r.procedural) != 0 || coloredPixels(img, red) < 100 {
		t.Fatal("switch to image texture failed")
	}
	img, err = r.Render(f)
	check(t, err)
	if coloredPixels(img, green) < 100 {
		t.Fatal("shader snapshot replay failed")
	}
}

func TestGPUProceduralParameters(t *testing.T) {
	const source = `fn nt3dTexture(uv: vec2<f32>) -> vec4<f32> { return nt3dTextureParams; }`
	red := color.RGBA{255, 0, 0, 255}
	green := color.RGBA{0, 255, 0, 255}
	first, err := NewShaderTextureWithParameters(source, [4]float32{1, 0, 0, 1}, testTexture(t, red))
	check(t, err)
	second, err := NewShaderTextureWithParameters(source, [4]float32{0, 1, 0, 1}, testTexture(t, green))
	check(t, err)
	mesh, err := BuildHeightfield(Heightfield{NX: 3, NY: 3, MinX: -.7, MaxX: .7, MinY: -.7, MaxY: .7, Heights: make([]float32, 9), Material: &Material{Texture: first, Unlit: true}})
	check(t, err)
	s := NewScene()
	_, err = s.SetGeometry("terrain", mesh)
	check(t, err)
	old := frame(t, s)
	r := &gpuRenderer{}
	defer r.Close()
	img, err := r.Render(old)
	check(t, err)
	if coloredPixels(img, red) < 100 {
		t.Fatal("initial parameters missing")
	}
	resources := r.procedural[first]
	check(t, s.SetTexture("terrain", second))
	img, err = r.Render(frame(t, s))
	check(t, err)
	if coloredPixels(img, green) < 100 || r.procedural[second] != resources {
		t.Fatal("parameter update failed to reuse resources")
	}
	img, err = r.Render(old)
	check(t, err)
	if coloredPixels(img, red) < 100 {
		t.Fatal("old snapshot parameters changed")
	}
	// Simultaneous variants must never share a uniform or render target.
	mesh.Material.Texture = second
	_, err = s.SetGeometry("other", mesh)
	check(t, err)
	check(t, s.SetTexture("terrain", first))
	_, err = r.Render(frame(t, s))
	check(t, err)
	if len(r.procedural) != 2 || r.procedural[first] == r.procedural[second] {
		t.Fatal("simultaneous parameter variants aliased")
	}
}

func TestGPUPerNodeGeometryCache(t *testing.T) {
	s := NewScene()
	moving, err := s.SetGeometry("moving", pointGeometry())
	check(t, err)
	tex := testTexture(t, color.RGBA{10, 220, 80, 255})
	mesh, err := BuildHeightfield(Heightfield{NX: 3, NY: 3, MinX: -.7, MaxX: .7, MinY: -.7, MaxY: .7, Heights: make([]float32, 9), Material: &Material{Texture: tex, Unlit: true}})
	check(t, err)
	stable, err := s.SetGeometry("stable", mesh)
	check(t, err)
	r := &gpuRenderer{}
	defer r.Close()
	old := frame(t, s)
	original, err := r.Render(old)
	check(t, err)
	fixed := r.nodes[1]
	originalBuffer := fixed.batches[7].buffer
	uploads := r.uploads
	// Replacing one node, including its primitive type, leaves the other untouched.
	changed := Geometry{Boxes: []Box{{Min: nt3dmath.Vec3{X: .2, Y: .2}, Size: nt3dmath.Vec3{X: .3, Y: .3, Z: .5}, Color: color.RGBA{250, 30, 20, 255}}}}
	check(t, s.SetNodeGeometry(moving, changed))
	_, err = r.Render(frame(t, s))
	check(t, err)
	if r.uploads != uploads+1 || r.nodes[1] != fixed || fixed.batches[7].buffer != originalBuffer {
		t.Fatal("unrelated geometry reuploaded")
	}
	uploads = r.uploads
	group, err := s.Add(Root, "group")
	check(t, err)
	check(t, s.Reparent(stable, group))
	check(t, s.SetTransform(group, nt3dmath.Translation(nt3dmath.Vec3{X: .1})))
	check(t, s.SetVisible(moving, false))
	_, err = r.Render(frame(t, s))
	check(t, err)
	if r.uploads != uploads {
		t.Fatal("transform/reparent/visibility reuploaded geometry")
	}
	// Removal shifts the stable node's frame index, but not its GPU identity.
	check(t, s.Remove(moving))
	_, err = r.Render(frame(t, s))
	check(t, err)
	if r.uploads != uploads || len(r.geometries) != 1 || r.nodes[0] != fixed {
		t.Fatal("removal invalidated surviving node")
	}
	_, err = s.SetGeometry("new", pointGeometry())
	check(t, err)
	_, err = r.Render(frame(t, s))
	check(t, err)
	if r.uploads != uploads+1 || r.nodes[0] != fixed {
		t.Fatal("insertion invalidated surviving node")
	}
	// Old snapshots restore removed/replaced nodes, without corrupting transforms.
	replay, err := r.Render(old)
	check(t, err)
	for y := range old.Height {
		for x := range old.Width {
			if original.At(x, y) != replay.At(x, y) {
				t.Fatal("snapshot replay changed pixels")
			}
		}
	}
	if r.nodes[1] != fixed {
		t.Fatal("snapshot replay invalidated unchanged mesh")
	}
	// Material changes invalidate only their own node, including textured -> plain.
	mesh.Material = &Material{Unlit: true}
	check(t, s.SetNodeGeometry(stable, mesh))
	_, err = r.Render(frame(t, s))
	check(t, err)
	if r.nodes[0].batches[7].count != 0 || r.nodes[0].batches[1].count == 0 {
		t.Fatal("stale textured batch after material change")
	}
	s.Clear()
	_, err = r.Render(frame(t, s))
	check(t, err)
	if len(r.geometries) != 0 {
		t.Fatal("removed geometry retained")
	}
}

func TestGPUManualFrameGeometryCache(t *testing.T) {
	s := NewScene()
	_, err := s.SetGeometry("point", pointGeometry())
	check(t, err)
	f := frame(t, s)
	f.GeometryRevisions = nil
	f.Instances = nil
	r := &gpuRenderer{}
	defer r.Close()
	_, err = r.Render(f)
	check(t, err)
	before := r.uploads
	_, err = r.Render(f)
	check(t, err)
	if r.uploads != before {
		t.Fatal("unchanged manual frame reuploaded")
	}
	f.Revision++
	_, err = r.Render(f)
	check(t, err)
	if r.uploads != before+1 {
		t.Fatal("manual revision ignored")
	}
}

// Software cuts triangles at the near plane as the GPU does: the two agree
// on a floor reaching behind the camera.
func TestGPUNearPlaneMatchesSoftware(t *testing.T) {
	f, floor := nearScene(t)
	r := &gpuRenderer{}
	defer r.Close()
	gpu, err := r.Render(f)
	check(t, err)
	software := softwareRender(f)
	g, s := coloredPixels(gpu, floor), coloredPixels(software, floor)
	if g < f.Width*f.Height/3 || g-s > g/100 || s-g > g/100 {
		t.Fatalf("floor pixels: GPU %d, software %d", g, s)
	}
}

// Cutout texels are discarded on the GPU as in software.
func TestGPUCutoutMatchesSoftware(t *testing.T) {
	f, front, back := cutoutScene(t, true)
	r := &gpuRenderer{}
	defer r.Close()
	gpu, err := r.Render(f)
	check(t, err)
	software := softwareRender(f)
	for name, c := range map[string]color.RGBA{"front": front, "back": back} {
		g, s := count(gpu, c), count(software, c)
		if g < 2000 || g-s > g/20 || s-g > g/20 {
			t.Fatalf("%s pixels: GPU %d, software %d", name, g, s)
		}
	}
	f, _, _ = cutoutScene(t, false)
	opaque, err := r.Render(f)
	check(t, err)
	if n := count(opaque, back); n != 0 {
		t.Fatalf("%d back pixels through an opaque material", n)
	}
}

// Thresholds cut the same share of a ramp on the GPU as in software.
func TestGPUCutoutThreshold(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	for _, threshold := range []float32{.25, .75} {
		f, front := rampScene(t, threshold)
		img, err := r.Render(f)
		check(t, err)
		g, s := count(img, front), count(softwareRender(f), front)
		if g < 1000 || g-s > g/20 || s-g > g/20 {
			t.Fatalf("threshold %v: GPU %d, software %d", threshold, g, s)
		}
	}
}

// The GPU supersamples as software does: gray edges, points of their size.
func TestGPUSupersample(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	var edge, red [2]int
	for i, s := range []int{1, 3} {
		img, err := r.Render(edgeScene(t, s))
		check(t, err)
		if img.Bounds() != image.Rect(0, 0, 120, 120) {
			t.Fatal(img.Bounds())
		}
		edge[i], red[i] = shades(img)
	}
	if edge[0] > 5 || edge[1] < 60 || red[1] < red[0]*2/3 || red[1] > red[0]*3/2 {
		t.Fatalf("GPU edge %v, point %v", edge, red)
	}
	sw, _ := shades(softwareRender(edgeScene(t, 3)))
	if edge[1] < sw*2/3 || edge[1] > sw*3/2 {
		t.Fatalf("GPU gray edge %d, software %d", edge[1], sw)
	}
	// Back to one: the scene target shrinks to the frame.
	_, err := r.Render(edgeScene(t, 1))
	check(t, err)
	if r.target.w != 120 || r.target.h != 120 {
		t.Fatalf("scene target %d×%d, want the frame's 120×120", r.target.w, r.target.h)
	}
}

// The GPU tiles as software does.
func TestGPURepeat(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	for _, repeat := range []bool{true, false} {
		f := repeatScene(t, repeat)
		img, err := r.Render(f)
		check(t, err)
		if g, s := bands(img), bands(softwareRender(f)); g != s {
			t.Fatalf("repeat %v: GPU %d bands, software %d", repeat, g, s)
		}
	}
}

// Tint and edge-on cutouts on the GPU agree with software.
func TestGPUTintAndEdgeOn(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	red, white := color.RGBA{255, 0, 0, 255}, color.RGBA{255, 255, 255, 255}
	img, err := r.Render(materialScene(t, Material{Tint: true}, nt3dmath.Vec3{Z: 1}, red))
	check(t, err)
	if c := center(img); c.R < 120 || c.R > 136 || c.G > 5 {
		t.Fatalf("tinted gray %v", c)
	}
	for _, c := range []struct {
		normal nt3dmath.Vec3
		shown  bool
	}{{nt3dmath.Vec3{Z: 1}, false}, {nt3dmath.Vec3{X: 1}, true}} {
		f := materialScene(t, Material{Cutout: true, EdgeOn: true}, c.normal, white)
		img, err := r.Render(f)
		check(t, err)
		g, s := drawn(img, f.Background), drawn(softwareRender(f), f.Background)
		if (g > 0) != c.shown || g-s > g/20+5 || s-g > g/20+5 {
			t.Fatalf("normal %v: GPU %d, software %d pixels", c.normal, g, s)
		}
	}
}

// Culling leaves the GPU's picture unchanged too, including a point disc
// that overhangs the frame's edge from a center outside it.
func TestGPUCullMatchesUnculled(t *testing.T) {
	f := cullScene(t)
	off := f
	off.Cull = false
	r := &gpuRenderer{}
	defer r.Close()
	a, err := r.Render(f)
	check(t, err)
	culledPix := append([]byte(nil), a.(*image.NRGBA).Pix...)
	b, err := r.Render(off)
	check(t, err)
	if !bytes.Equal(culledPix, b.(*image.NRGBA).Pix) {
		t.Fatal("GPU pictures differ")
	}
	blue := 0
	for y := range f.Height {
		if c := color.RGBAModel.Convert(b.At(f.Width-1, y)).(color.RGBA); c.B > 150 && c.R < 60 {
			blue++
		}
	}
	if blue == 0 {
		t.Fatal("the overhanging point does not show")
	}
}

// Transparent geometry blends on the GPU as in software: one pane, two in
// either order, a sphere's two layers, a pane lit from behind, and a texture.
func TestGPUTransparentMatchesSoftware(t *testing.T) {
	red, green, white := color.RGBA{220, 0, 0, 128}, color.RGBA{0, 220, 0, 128}, color.RGBA{240, 240, 240, 128}
	ball, err := BuildSphere(Sphere{Radius: .6, Color: white, Material: glass})
	check(t, err)
	half := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	half.SetNRGBA(0, 0, color.NRGBA{200, 200, 0, 128})
	tex, err := NewTexture(half)
	check(t, err)
	textured := quad(1, .3, color.RGBA{255, 255, 255, 255}, &Material{Texture: tex, Transparent: true}, false)
	textured.UV = make([]UV, 4)
	l := oldLight(nt3dmath.Vec3{Z: 1}, .2, true)
	light := &l
	r := &gpuRenderer{}
	defer r.Close()
	for name, f := range map[string]Frame{
		"pane":       blendScene(t, nil, quad(1, .3, red, glass, false)),
		"far first":  blendScene(t, nil, quad(1, .3, red, glass, false), quad(1, .6, green, glass, false)),
		"near first": blendScene(t, nil, quad(1, .6, green, glass, false), quad(1, .3, red, glass, false)),
		"sphere":     blendScene(t, nil, ball),
		"back lit":   blendScene(t, light, quad(1, .3, white, glass, true)),
		"textured":   blendScene(t, nil, textured),
	} {
		img, err := r.Render(f)
		check(t, err)
		if gpu, software := at(img, 40, 40), at(softwareRender(f), 40, 40); !closeRGB(gpu, software, 3) {
			t.Errorf("%s: GPU %v, software %v", name, gpu, software)
		}
	}
}

// The GPU lights in linear light as software does, and as encoded without it.
func TestGPULinearLighting(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	gray := color.RGBA{200, 200, 200, 255}
	for _, linear := range []bool{true, false} {
		for _, cos := range []float64{1, .5, .2, 0} {
			l := oldLight(nt3dmath.Vec3{X: float32(math.Sqrt(1 - cos*cos)), Z: float32(cos)}, .3, linear)
			f := blendScene(t, &l, quad(1, .3, gray, nil, false))
			f.Linear = linear
			img, err := r.Render(f)
			check(t, err)
			if gpu, software := at(img, 40, 40), at(softwareRender(f), 40, 40); !closeRGB(gpu, software, 2) {
				t.Errorf("linear %v, cos %.1f: GPU %v, software %v", linear, cos, gpu, software)
			}
		}
	}
}

// Highlights and fog match software across whole pictures: a shiny sphere,
// a flat quad's per-pixel highlight, fogged surfaces, points, and lines,
// a shiny fogged glass sphere, and fog in an orthographic view.
func TestGPUSpecularAndFogMatchSoftware(t *testing.T) {
	ball, err := BuildSphere(Sphere{Radius: .6, Color: gray, Material: shiny})
	check(t, err)
	glassBall, err := BuildSphere(Sphere{Radius: .6, Color: color.RGBA{240, 240, 240, 128}, Material: &Material{Transparent: true, Specular: .8, Shininess: 64}})
	check(t, err)
	marks := Geometry{
		Points:    []Point{{Position: nt3dmath.Vec3{X: -.5, Z: .3}, Radius: 4, Color: color.RGBA{200, 0, 0, 255}}},
		Lines:     []Vertex{{Position: nt3dmath.Vec3{X: .2, Y: -.8, Z: -.5}, Color: color.RGBA{200, 0, 0, 255}}, {Position: nt3dmath.Vec3{X: .2, Y: .8, Z: .8}, Color: color.RGBA{0, 200, 0, 255}}},
		LineWidth: 3,
	}
	fogged := func(f Frame) Frame {
		f.Fog = Fog{Near: 3, Far: 5}
		return f
	}
	ortho := fogged(blendScene(t, overhead, ball))
	ortho.Perspective = false
	scenes := map[string]Frame{
		"shiny sphere":   blendScene(t, overhead, ball),
		"per-pixel quad": blendScene(t, overhead, quad(1.2, .3, gray, &Material{Specular: 1, Shininess: 1024}, false)),
		"fogged sphere":  fogged(blendScene(t, overhead, ball)),
		"fogged marks":   fogged(blendScene(t, nil, marks)),
		"fogged glass":   fogged(blendScene(t, overhead, glassBall)),
		"orthographic":   ortho,
	}
	r := &gpuRenderer{}
	defer r.Close()
	for name, f := range scenes {
		// Points' and strokes' edge pixels already differ between the
		// renderers; compare only where they agree without fog.
		clear := f
		clear.Fog = Fog{}
		img, err := r.Render(clear)
		check(t, err)
		gpuClear := image.NewRGBA(img.Bounds())
		draw.Draw(gpuClear, img.Bounds(), img, image.Point{}, draw.Src)
		swClear := softwareRender(clear)
		img, err = r.Render(f)
		check(t, err)
		sw := softwareRender(f)
		off := 0
		for y := range f.Height {
			for x := range f.Width {
				if name == "fogged marks" && !closeRGB(at(gpuClear, x, y), at(swClear, x, y), 6) {
					continue
				}
				if !closeRGB(at(img, x, y), at(sw, x, y), 6) {
					off++
				}
			}
		}
		if off > f.Width*f.Height/50 {
			t.Errorf("%s: %d of %d pixels differ; center GPU %v, software %v", name, off, f.Width*f.Height, at(img, 40, 40), at(sw, 40, 40))
		}
	}
}

// Nodes that share geometry upload it once and draw it in one instanced
// call, with the picture of separate copies.
func TestGPUSharedGeometry(t *testing.T) {
	shared, ids, _ := grid(t, 10, .25, true)
	copies, _, _ := grid(t, 10, .25, false)
	r := &gpuRenderer{}
	defer r.Close()
	f := frame(t, shared)
	visible := 0
	for _, d := range f.drawn(1) {
		if d {
			visible++
		}
	}
	img, err := r.Render(f)
	check(t, err)
	instanced := append([]byte(nil), img.(*image.NRGBA).Pix...)
	if r.uploads != 1 || len(r.draws) != 1 || r.draws[0].count != uint32(visible) || visible < 50 {
		t.Fatalf("%d uploads, %d draws of %d nodes in view", r.uploads, len(r.draws), visible)
	}
	separate := &gpuRenderer{}
	defer separate.Close()
	img, err = separate.Render(frame(t, copies))
	check(t, err)
	if separate.uploads != 100 || len(separate.draws) != visible || !bytes.Equal(instanced, img.(*image.NRGBA).Pix) {
		t.Fatalf("copies: %d uploads, %d draws; pictures equal %v", separate.uploads, len(separate.draws), bytes.Equal(instanced, img.(*image.NRGBA).Pix))
	}
	// Moving and hiding nodes rewrites transforms, uploading nothing.
	check(t, shared.SetTransform(ids[0], nt3dmath.Translation(nt3dmath.Vec3{Z: .5})))
	check(t, shared.SetVisible(ids[1], false))
	f = frame(t, shared)
	_, err = r.Render(f)
	check(t, err)
	if r.uploads != 1 || len(r.draws) != 1 || r.draws[0].count != uint32(visible-1) {
		t.Fatalf("after moving and hiding: %d uploads, %d draws of %d", r.uploads, len(r.draws), r.draws[0].count)
	}
	shared.Clear()
	_, err = r.Render(frame(t, shared))
	check(t, err)
	if len(r.geometries) != 0 || len(r.draws) != 0 {
		t.Fatal("cleared shared geometry retained")
	}
}

// Shared geometry splits its draws where nodes' textures differ, and draws
// transparent nodes one by one, in blending order, as copies would be.
func TestGPUSharedGeometrySplits(t *testing.T) {
	tex, other := testTexture(t, color.RGBA{10, 220, 80, 255}), testTexture(t, color.RGBA{200, 0, 0, 255})
	plane, err := BuildHeightfield(Heightfield{NX: 2, NY: 2, MinX: -.2, MaxX: .2, MinY: -.2, MaxY: .2, Heights: make([]float32, 4), Material: &Material{Texture: tex}})
	check(t, err)
	sg, err := NewSharedGeometry(plane)
	check(t, err)
	s := NewScene()
	for i := range 3 {
		id, err := s.SetShared(fmt.Sprint("plane ", i), sg)
		check(t, err)
		check(t, s.SetTransform(id, nt3dmath.Translation(nt3dmath.Vec3{X: float32(i-1) * .5})))
	}
	check(t, s.SetTexture("plane 2", other))
	r := &gpuRenderer{}
	defer r.Close()
	_, err = r.Render(frame(t, s))
	check(t, err)
	if r.uploads != 1 || len(r.draws) != 2 || r.draws[0].count+r.draws[1].count != 3 {
		t.Fatalf("%d uploads, %d draws", r.uploads, len(r.draws))
	}
	// Transparent shared spheres, one behind another, blend as copies do.
	glassBall, err := BuildSphere(Sphere{Radius: .3, Color: color.RGBA{240, 240, 240, 128}, Material: glass})
	check(t, err)
	shareGlass, err := NewSharedGeometry(glassBall)
	check(t, err)
	build := func(shared bool) Frame {
		s := NewScene()
		for i := range 3 {
			name := fmt.Sprint("glass ", i)
			var id NodeID
			if shared {
				id, err = s.SetShared(name, shareGlass)
			} else {
				id, err = s.SetGeometry(name, glassBall)
			}
			check(t, err)
			check(t, s.SetTransform(id, nt3dmath.Translation(nt3dmath.Vec3{X: float32(i) * .2, Z: float32(i) * .3})))
		}
		return frame(t, s)
	}
	a, err := r.Render(build(true))
	check(t, err)
	if len(r.draws) != 3 || r.uploads != 2 {
		t.Fatalf("transparent: %d draws, %d uploads", len(r.draws), r.uploads)
	}
	instanced := append([]byte(nil), a.(*image.NRGBA).Pix...)
	b, err := r.Render(build(false))
	check(t, err)
	if !bytes.Equal(instanced, b.(*image.NRGBA).Pix) {
		t.Fatal("transparent shared spheres blend unlike copies")
	}
}

// The GPU mipmaps as software does: the far checker evens out to the same
// gray, and the whole floor matches closely.
func TestGPUMipmaps(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	f := checkerFloor(t, false)
	img, err := r.Render(f)
	check(t, err)
	sw := softwareRender(f)
	gm, gd := spread(band(img, .29, .33))
	sm, sd := spread(band(sw, .29, .33))
	if gd > 25 || math.Abs(gm-sm) > 8 {
		t.Fatalf("far gray: GPU mean %.0f deviation %.1f, software mean %.0f deviation %.1f", gm, gd, sm, sd)
	}
	var diff float64
	for y := range f.Height {
		for x := range f.Width {
			a, b := at(img, x, y), at(sw, x, y)
			diff += math.Abs(float64(a.R) - float64(b.R))
		}
	}
	if mean := diff / float64(f.Width*f.Height); mean > 6 {
		t.Fatalf("GPU and software differ by %.1f levels on average", mean)
	}
}

// The GPU filters anisotropically too: a slanted floor keeps its checks
// further out than with mipmapping alone, as software does.
func TestGPUAnisotropy(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	f := checkerFloor(t, false)
	f.Anisotropy = 1
	img, err := r.Render(f)
	check(t, err)
	_, flat := spread(band(img, .33, .38))
	f.Anisotropy = 0
	img, err = r.Render(f)
	check(t, err)
	gm, detail := spread(band(img, .33, .38))
	sm, sd := spread(band(softwareRender(f), .33, .38))
	if flat > 5 || detail < 15 || math.Abs(gm-sm) > 10 {
		t.Fatalf("GPU spread: mipmapped %.1f, anisotropic %.1f (mean %.0f); software %.1f (mean %.0f)", flat, detail, gm, sd, sm)
	}
}

// Points are discs of their full radius on the GPU as in software, drawn
// where they hang into view.
func TestGPUPointsMatchSoftware(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	r := &gpuRenderer{}
	defer r.Close()
	for i, c := range []struct{ x, radius float32 }{{0, 4.9}, {0, 32}, {1 + 3./64, 8}} {
		f := flatFrame(128, 128, Geometry{Points: []Point{{Position: nt3dmath.Vec3{X: c.x, Z: .5}, Radius: c.radius, Color: red}}})
		f.Revision = uint64(i + 1) // Manual frames are cached by revision.
		gpu, err := r.Render(f)
		check(t, err)
		if g, s := coloredPixels(gpu, red), coloredPixels(softwareRender(f), red); g != s || g == 0 {
			t.Errorf("point at x=%v, radius %v: GPU %d pixels, software %d", c.x, c.radius, g, s)
		}
	}
}

// Gradient strokes mix in linear light on the GPU as in software, whole or
// clipped at the far plane.
func TestGPUStrokeGradientMatchesSoftware(t *testing.T) {
	red, black := color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 0, 255}
	r := &gpuRenderer{}
	defer r.Close()
	for i, far := range []float32{.5, 1.5} {
		f := flatFrame(64, 8, Geometry{Lines: []Vertex{{Position: nt3dmath.Vec3{X: -1, Z: .5}, Color: red}, {Position: nt3dmath.Vec3{X: 1, Z: far}, Color: black}}, LineWidth: 2})
		f.Revision = uint64(i + 1)
		f.Background = color.RGBA{0, 0, 255, 255}
		gpu, err := r.Render(f)
		check(t, err)
		software := softwareRender(f)
		for x := range 64 {
			g, s := at(gpu, x, 4), at(software, x, 4)
			if (g.B == 255) != (s.B == 255) {
				continue // One draws the clipped end a pixel further.
			}
			if !closeRGB(g, s, 2) {
				t.Fatalf("far end at z=%v, pixel %d: GPU %v, software %v", far, x, g, s)
			}
		}
	}
}

// A transparent quad's shared diagonal is blended once on the GPU, and so
// matches software.
func TestGPUSharedEdgeMatchesSoftware(t *testing.T) {
	f := flatFrame(8, 8, quad(1, .5, color.RGBA{255, 255, 255, 128}, glass, false))
	r := &gpuRenderer{}
	defer r.Close()
	gpu, err := r.Render(f)
	check(t, err)
	software := softwareRender(f)
	for y := range 8 {
		for x := range 8 {
			if g, s := at(gpu, x, y), at(software, x, y); !closeRGB(g, s, 1) {
				t.Fatalf("pixel (%d, %d): GPU %v, software %v", x, y, g, s)
			}
		}
	}
}

// An instance set that cannot be made is left empty, to be made again the
// next frame, not drawn from half made.
func TestGPUInstanceSetFailureRetries(t *testing.T) {
	s := NewScene()
	_, err := s.SetGeometry("point", pointGeometry())
	check(t, err)
	f := frame(t, s)
	r := &gpuRenderer{}
	defer r.Close()
	_, err = r.Render(f)
	check(t, err)
	var node *gpuGeometry
	for _, n := range r.geometries {
		node = n
	}
	// Failures are returned, not raised here: Fatal would leave the
	// device held.
	check(t, r.r.Do(func(dev *wgpu.Device) error {
		node.used = 0
		if _, err := r.instanceSet(dev, node, 1<<40); err == nil {
			return errors.New("a set past the device's limits was made")
		}
		if set := node.sets[0]; set.capacity != 0 || set.transform != nil || set.binds != [8]*wgpu.BindGroup{} {
			return fmt.Errorf("a failed set kept capacity %d", set.capacity)
		}
		node.used = 0
		set, err := r.instanceSet(dev, node, 1)
		if err != nil || set.capacity < 1 || set.transform == nil {
			return fmt.Errorf("set not remade: %v", err)
		}
		return nil
	}))
	_, err = r.Render(f)
	check(t, err)
}

// Lighting matches software across whole pictures, in linear light and
// encoded: a point light's hotspot, a full rig on a shiny sphere, all eight
// lights at once, glass lit from behind by a point light, no lighting at
// all, and a point light on a surface.
func TestGPULightingMatchesSoftware(t *testing.T) {
	ball, err := BuildSphere(Sphere{Radius: .6, Color: gray, Material: shiny})
	check(t, err)
	glassBall, err := BuildSphere(Sphere{Radius: .6, Color: color.RGBA{240, 240, 240, 128}, Material: &Material{Transparent: true, Specular: .8, Shininess: 64}})
	check(t, err)
	rig := Lighting{
		Ambient: Ambient{Sky: color.RGBA{90, 110, 160, 255}, Ground: color.RGBA{70, 50, 30, 255}, Intensity: .8},
		Lights: []Light{
			{Direction: nt3dmath.Vec3{X: 1, Y: -1, Z: 2}, Color: color.RGBA{255, 230, 200, 255}, Intensity: .7},
			{Kind: PointLight, Position: nt3dmath.Vec3{X: -.8, Y: .3, Z: .9}, Color: color.RGBA{80, 160, 255, 255}, Intensity: 2, Range: 3},
		},
	}
	var eight Lighting
	for i := range MaxLights {
		angle := float64(i) * math.Pi / 4
		eight.Lights = append(eight.Lights, Light{Kind: LightKind(i % 2), Direction: nt3dmath.Vec3{X: float32(math.Cos(angle)), Y: float32(math.Sin(angle)), Z: 1}, Position: nt3dmath.Vec3{X: float32(math.Cos(angle)), Y: float32(math.Sin(angle)), Z: .8}, Intensity: .2, Range: 2.5})
	}
	behind := Lighting{Ambient: Ambient{Sky: white, Ground: white, Intensity: .2}, Lights: []Light{{Kind: PointLight, Position: nt3dmath.Vec3{Z: -.3}, Color: color.RGBA{255, 120, 0, 255}}}}
	// A point light in the plane of a shiny quad, at its middle: it lights
	// nothing there (and its own point is no NaN), leaving the ambient.
	onSurface := Lighting{Ambient: Ambient{Sky: white, Ground: white, Intensity: .3}, Lights: []Light{{Kind: PointLight, Position: nt3dmath.Vec3{Z: .3}}}}
	scenes := map[string]Frame{
		"point hotspot":             pointQuad(t, 0, .1),
		"rig on a shiny sphere":     blendScene(t, &rig, ball),
		"eight lights":              blendScene(t, &eight, ball),
		"glass under a point light": blendScene(t, &behind, glassBall),
		"no lighting":               blendScene(t, &Lighting{}, quad(.3, .3, white, &Material{Unlit: true}, false), quad(1.2, .2, white, nil, false)),
		"light on the surface":      blendScene(t, &onSurface, quad(1.2, .3, white, shiny, false)),
	}
	r := &gpuRenderer{}
	defer r.Close()
	for name, f := range scenes {
		for _, linear := range []bool{true, false} {
			f.Linear = linear
			img, err := r.Render(f)
			check(t, err)
			sw := softwareRender(f)
			off := 0
			for y := range f.Height {
				for x := range f.Width {
					if !closeRGB(at(img, x, y), at(sw, x, y), 6) {
						off++
					}
				}
			}
			if off > f.Width*f.Height/50 {
				t.Errorf("%s, linear %v: %d of %d pixels differ; center GPU %v, software %v", name, linear, off, f.Width*f.Height, at(img, 40, 40), at(sw, 40, 40))
			}
		}
	}
}

// Styled points draw on the GPU as in software: each blend, shape, and
// unit, two overlapping points (the nearer given first) over the floor;
// and a pane and alpha points in one geometry.
func TestGPUPointStylesMatchSoftware(t *testing.T) {
	red, green := color.RGBA{255, 0, 0, 128}, color.RGBA{0, 255, 0, 160}
	r := &gpuRenderer{}
	defer r.Close()
	frames := map[string]Frame{}
	for _, blend := range []PointBlend{OpaqueBlend, AlphaBlend, AdditiveBlend} {
		for _, shape := range []PointShape{HardPoint, SoftPoint} {
			for _, units := range []PointUnits{PixelUnits, WorldUnits} {
				style := PointStyle{shape, units, blend}
				radius := float32(10)
				if units == WorldUnits {
					radius = .25
				}
				frames[fmt.Sprintf("%+v", style)] = blendScene(t, nil, Geometry{PointStyle: style, Points: []Point{
					{Position: nt3dmath.Vec3{X: .1, Z: .3}, Radius: radius, Color: green}, {Radius: radius, Color: red}}})
			}
		}
	}
	pane := quad(1, .3, color.RGBA{0, 0, 220, 128}, glass, false)
	pane.PointStyle, pane.Points = PointStyle{Shape: SoftPoint, Blend: AlphaBlend}, []Point{{Position: nt3dmath.Vec3{Z: .5}, Radius: 10, Color: red}}
	frames["pane and points"] = blendScene(t, nil, pane)
	// An opaque mesh with blended points stays opaque, hiding the points behind it.
	wall := quad(.5, .5, color.RGBA{0, 200, 0, 255}, nil, false)
	wall.PointStyle, wall.Points = PointStyle{Blend: AlphaBlend}, []Point{{Radius: 10, Color: color.RGBA{255, 0, 0, 200}}}
	frames["opaque mesh and points"] = blendScene(t, nil, wall)
	for name, f := range frames {
		gpu, err := r.Render(f)
		check(t, err)
		software := softwareRender(f)
		for _, x := range []int{40, 43, 46, 30, 10} {
			if g, s := at(gpu, x, 40), at(software, x, 40); !closeRGB(g, s, 4) {
				t.Errorf("%s at x=%d: GPU %v, software %v", name, x, g, s)
			}
		}
	}
}

// Fog fades additive points out on the GPU as in software, and turns alpha
// points toward its color; a world-unit point behind the camera draws
// nothing.
func TestGPUPointFogMatchesSoftware(t *testing.T) {
	white := color.RGBA{255, 255, 255, 255}
	r := &gpuRenderer{}
	defer r.Close()
	for _, blend := range []PointBlend{AlphaBlend, AdditiveBlend} {
		f := blendScene(t, nil, Geometry{PointStyle: PointStyle{Shape: SoftPoint, Units: WorldUnits, Blend: blend}, Points: []Point{
			{Radius: .3, Color: white}, {Position: nt3dmath.Vec3{Z: 9}, Radius: 1, Color: white}}})
		f.Fog = Fog{Near: 0, Far: 3.6, Color: color.RGBA{255, 0, 0, 255}}
		gpu, err := r.Render(f)
		check(t, err)
		software := softwareRender(f)
		for _, p := range [][2]int{{40, 40}, {42, 40}, {10, 10}, {70, 70}} {
			if g, s := at(gpu, p[0], p[1]), at(software, p[0], p[1]); !closeRGB(g, s, 4) {
				t.Errorf("blend %d at %v: GPU %v, software %v", blend, p, g, s)
			}
		}
	}
}
