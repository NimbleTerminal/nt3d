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
	clearRevisions(&f)
	clearPlacements(&f)
	clearShared(&f)
	f.Revision = toonRevision
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
	off := toonGPU(t, r, toonSphere(t, &Material{Specular: .4, Toon: ToonStyle{Softness: .5, ShadowSoftness: .7, Rim: .5, Shade: color.RGBA{0, 0, 255, 255}}}, 96))
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

// A flat face has no normal derivative: under a point light its band edge
// must still blend, by how the light's direction changes across a pixel.
func TestGPUToonFlatFloorEdgeIsAntialiased(t *testing.T) {
	r := &gpuRenderer{}
	defer r.Close()
	camera := DefaultCamera()
	camera.Projection, camera.Elevation = Perspective, 89
	s := NewScene()
	_, err := s.SetGeometry("floor", quad(3, -1, white, &Material{Toon: ToonStyle{Bands: 2}}, false))
	check(t, err)
	f, err := s.Frame(camera, 400, 400)
	check(t, err)
	// The band edge is the circle 60 degrees off the light's foot.
	f.Lighting = Lighting{Lights: []Light{{Kind: PointLight, Position: nt3dmath.Vec3{}, Range: 40}}}
	img := toonGPU(t, r, f)
	blended, rows := 0, 0
	for y := 150; y < 250; y += 5 {
		// The sharpest step right of the center, and whether a pixel beside
		// it lies well between the tones on its two sides. An edge blended
		// across one pixel shows such a pixel in most rows (not all: the edge
		// can fall between two pixels); a hard step shows none.
		best, edge := 0, 0
		for x := 205; x < 394; x++ {
			if d := absInt(int(at(img, x+1, y).R) - int(at(img, x, y).R)); d > best {
				best, edge = d, x
			}
		}
		if best < 20 {
			continue
		}
		rows++
		lo, hi := int(at(img, edge-2, y).R), int(at(img, edge+3, y).R)
		if lo > hi {
			lo, hi = hi, lo
		}
		for x := edge - 1; x <= edge+2; x++ {
			if v := int(at(img, x, y).R); 10*v > 10*lo+(hi-lo) && 10*v < 10*hi-(hi-lo) {
				blended++
				break
			}
		}
	}
	if rows < 10 || blended*5 < rows*2 {
		t.Fatalf("only %d of %d rows blend across the band edge", blended, rows)
	}
}

func TestGPUToonShadowSoftnessMatchesSoftware(t *testing.T) {
	for _, pbr := range []bool{false, true} {
		crispF := toonStreetShadowFrame(t, pbr, 0, DirectionalLight)
		softF := toonStreetShadowFrame(t, pbr, .75, DirectionalLight)
		crispGPU := creaseGPUOnce(t, crispF)
		softGPU := creaseGPUOnce(t, softF)
		softSW := softwareRender(softF)
		cs, cl, cBetween, _, cMaxStep, cRev := shadowProfileStats(crispF, crispGPU)
		ss, sl, sBetween, sDistinct, sMaxStep, sRev := shadowProfileStats(softF, softGPU)
		if !closeRGB(cs, ss, 4) || !closeRGB(cl, sl, 4) || int(cl.R) < int(cs.R)+40 {
			t.Fatalf("GPU pbr=%v endpoints changed or flat: crisp %v->%v, soft %v->%v", pbr, cs, cl, ss, sl)
		}
		if cRev != 0 || sRev != 0 {
			t.Fatalf("GPU pbr=%v non-monotonic shadow reversals: crisp=%d soft=%d", pbr, cRev, sRev)
		}
		if sBetween < cBetween+6 || sDistinct < 8 || sMaxStep < 4 || sMaxStep >= cMaxStep-20 {
			t.Fatalf("GPU pbr=%v soft shadow did not widen penumbra: crisp(between=%d, maxStep=%d) soft(between=%d, distinct=%d, maxStep=%d)",
				pbr, cBetween, cMaxStep, sBetween, sDistinct, sMaxStep)
		}
		if mad := hookImgMeanAbsDiff(softGPU, softSW); mad > 3.5 {
			t.Fatalf("GPU pbr=%v soft toon shadow MAD vs software = %.3f", pbr, mad)
		}
	}

	for _, kind := range []LightKind{SpotLight, PointLight} {
		crispF := toonStreetShadowFrame(t, false, 0, kind)
		softF := toonStreetShadowFrame(t, false, .75, kind)
		_, _, _, _, cMaxStep, _ := shadowProfileStats(crispF, creaseGPUOnce(t, crispF))
		softGPU := creaseGPUOnce(t, softF)
		cs, cl, _, sDistinct, sMaxStep, sRev := shadowProfileStats(softF, softGPU)
		if int(cl.R) < int(cs.R)+30 || sRev != 0 || sDistinct < 8 || sMaxStep >= cMaxStep-20 {
			t.Fatalf("GPU kind=%v shadow softness failed: shadow=%v lit=%v crispMaxStep=%d softMaxStep=%d distinct=%d rev=%d",
				kind, cs, cl, cMaxStep, sMaxStep, sDistinct, sRev)
		}
		if mad := hookImgMeanAbsDiff(softGPU, softwareRender(softF)); mad > 3.5 {
			t.Fatalf("GPU kind=%v soft toon shadow MAD vs software = %.3f", kind, mad)
		}
	}

	r := &gpuRenderer{}
	defer r.Close()
	sunShadows := toonSun
	sunShadows.Lights = []Light{{Direction: toonSun.Lights[0].Direction, Intensity: toonSun.Lights[0].Intensity, Shadows: true}}
	crispSphere := toonSphere(t, &Material{Toon: ToonStyle{Bands: 3, ShadowSoftness: 0}}, 200)
	softSphere := toonSphere(t, &Material{Toon: ToonStyle{Bands: 3, ShadowSoftness: 1}}, 200)
	crispSphere.Lighting, softSphere.Lighting = sunShadows, sunShadows
	crispSphereGPU := toonGPU(t, r, crispSphere)
	softSphereGPU := toonGPU(t, r, softSphere)
	if tones := flatTones(softSphereGPU, 3, toonSky); len(tones) != 3 {
		t.Fatalf("GPU sunlit sphere with ShadowSoftness=1 has %d flat tones, want 3: %v", len(tones), tones)
	}
	if !bytes.Equal(crispSphereGPU.Pix, softSphereGPU.Pix) {
		t.Fatal("ShadowSoftness changed a GPU sunlit curved sphere's cel bands")
	}
}
