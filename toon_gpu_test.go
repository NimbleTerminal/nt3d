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

var toonRevision uint64 = 1 << 40

// toonGPU renders f on r under a cache key of its own, as a copy the next
// render does not overwrite.
func toonGPU(t *testing.T, r *gpuRenderer, f Frame) *image.RGBA {
	t.Helper()
	toonRevision++
	f.GeometryRevisions, f.Instances, f.Shared, f.Revision = nil, nil, nil, toonRevision
	img, err := r.Render(f)
	check(t, err)
	out := image.NewRGBA(img.Bounds())
	draw.Draw(out, img.Bounds(), img, image.Point{}, draw.Src)
	return out
}

// flat reports whether every pixel within reach of x, y has its color.
func flat(img image.Image, x, y, reach int) bool {
	c := at(img, x, y)
	for dy := -reach; dy <= reach; dy++ {
		for dx := -reach; dx <= reach; dx++ {
			if p := image.Pt(x+dx, y+dy); p.In(img.Bounds()) && at(img, p.X, p.Y) != c {
				return false
			}
		}
	}
	return true
}

func TestGPUToonMatchesSoftware(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	f := toonSphere(t, &Material{Specular: .4, Toon: ToonStyle{Bands: 3, Shade: color.RGBA{150, 160, 220, 255}, Rim: .3}}, 200)
	gpu, sw := toonGPU(t, r, f), softwareRender(f)
	compared, off := 0, 0
	for y := range f.Height {
		for x := range f.Width {
			if !flat(sw, x, y, 2) {
				continue
			}
			compared++
			if !closeRGB(at(gpu, x, y), at(sw, x, y), 6) {
				off++
			}
		}
	}
	if compared < f.Width*f.Height/2 || off > compared/100 {
		t.Fatalf("%d of %d flat pixels differ; center GPU %v, software %v", off, compared, at(gpu, 100, 100), at(sw, 100, 100))
	}
	plain := toonGPU(t, r, toonSphere(t, &Material{Toon: ToonStyle{Bands: 3}}, 200))
	if tones := flatTones(plain, 3, toonSky); len(tones) != 3 {
		t.Fatalf("GPU toon sphere has %d flat tones, want 3: %v", len(tones), tones)
	}
}

func TestGPUToonOffIsUnchanged(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	plain := toonGPU(t, r, toonSphere(t, &Material{Specular: .4}, 96))
	off := toonGPU(t, r, toonSphere(t, &Material{Specular: .4, Toon: ToonStyle{Softness: .5, Rim: .5, Shade: color.RGBA{0, 0, 255, 255}}}, 96))
	if !bytes.Equal(plain.Pix, off.Pix) {
		t.Fatal("a toon style without bands changed the GPU frame")
	}
}

func TestGPUToonComposes(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	two := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	two.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
	two.SetNRGBA(1, 0, color.NRGBA{0, 0, 255, 255})
	tex, err := NewTexture(two)
	check(t, err)
	painted := quad(.8, .3, white, &Material{Texture: tex, Toon: ToonStyle{Bands: 2}}, false)
	painted.UV = []UV{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	sheet := quad(.8, .3, white, &Material{Transparent: true, Toon: ToonStyle{Bands: 2}}, true)
	for name, g := range map[string]Geometry{"textured": painted, "back of a sheet": sheet} {
		f := blendScene(t, &shadowLit, g)
		gpu, sw := toonGPU(t, r, f), softwareRender(f)
		for _, x := range []float32{-.7, 0, .7} {
			px, py := pixel(f, nt3dmath.Vec3{X: x, Z: .3})
			if x == 0 && name == "textured" {
				continue // The texture's seam.
			}
			if !closeRGB(at(gpu, px, py), at(sw, px, py), 6) {
				t.Errorf("%s at x %v: GPU %v, software %v", name, x, at(gpu, px, py), at(sw, px, py))
			}
		}
	}
}

func TestGPUToonEdgeIsAntialiased(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	img := toonGPU(t, r, toonSphere(t, &Material{Toon: ToonStyle{Bands: 2}}, 400))
	tones := flatTones(img, 3, toonSky)
	if len(tones) != 2 {
		t.Fatalf("%d flat tones, want 2: %v", len(tones), tones)
	}
	// Pixels inside the sphere that are neither tone lie on the terminator.
	blended := 0
	for y := 1; y < 399; y++ {
		for x := 1; x < 399; x++ {
			c := at(img, x, y)
			if _, tone := tones[c]; tone || c == toonSky {
				continue
			}
			if at(img, x-1, y) != toonSky && at(img, x+1, y) != toonSky && at(img, x, y-1) != toonSky && at(img, x, y+1) != toonSky {
				blended++
			}
		}
	}
	if blended < 50 || blended > 3000 {
		t.Fatalf("%d blended pixels along the band edge", blended)
	}
}

func TestGPUToonLeavesPointsAndBoxes(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	scene := func(toon ToonStyle) (Frame, *image.RGBA) {
		g := quad(.5, .3, white, &Material{Toon: toon}, false)
		g.Points = []Point{{Position: nt3dmath.Vec3{X: -1.5, Z: .3}, Radius: 6, Color: color.RGBA{200, 0, 0, 255}}}
		g.Boxes = []Box{{Min: nt3dmath.Vec3{X: 1.2, Y: -.3, Z: 0}, Size: nt3dmath.Vec3{X: .6, Y: .6, Z: .3}, Color: color.RGBA{0, 200, 0, 255}}}
		f := blendScene(t, &shadowLit, g)
		return f, toonGPU(t, r, f)
	}
	f, plain := scene(ToonStyle{})
	_, toon := scene(ToonStyle{Bands: 2})
	for name, p := range map[string]nt3dmath.Vec3{"point": {X: -1.5, Z: .3}, "box": {X: 1.5, Z: .3}} {
		x, y := pixel(f, p)
		if at(plain, x, y) != at(toon, x, y) || at(plain, x, y).A == 0 {
			t.Errorf("%s: %v plain, %v beside a toon mesh", name, at(plain, x, y), at(toon, x, y))
		}
	}
	mx, my := pixel(f, nt3dmath.Vec3{Z: .3})
	if at(plain, mx, my) == at(toon, mx, my) {
		t.Fatal("the mesh itself is not toon shaded")
	}
}
