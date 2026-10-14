//go:build webgpu

package nt3d

import (
	"image"
	"image/color"
	"nimbleterminal.dev/nt3d/math"
	"testing"
)

func coloredPixels(img image.Image, want color.RGBA) int {
	n := 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			if c == want {
				n++
			}
		}
	}
	return n
}

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
	// Back to one: the enlarged target is released.
	_, err := r.Render(edgeScene(t, 1))
	check(t, err)
	if r.super.target != nil {
		t.Fatal("kept the enlarged target")
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
