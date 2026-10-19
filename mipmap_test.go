// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image"
	"image/color"
	"math"
	"testing"
)

// checkerFloor is a floor tiled 400 times with a black and white checker,
// seen from low over it, so its far half packs many texels into each pixel.
// Full is sampled full size, as without mipmaps: a cutout that cuts nothing.
func checkerFloor(t *testing.T, full bool) Frame {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			v := uint8(0)
			if (x/4+y/4)%2 == 0 {
				v = 255
			}
			img.SetNRGBA(x, y, color.NRGBA{v, v, v, 255})
		}
	}
	tex, err := NewTexture(img)
	check(t, err)
	m := &Material{Texture: tex, Repeat: true, Unlit: true}
	if full {
		m.Cutout, m.Threshold = true, .01
	}
	floor, err := BuildHeightfield(Heightfield{NX: 2, NY: 2, MinX: -20, MaxX: 20, MinY: -20, MaxY: 20, Heights: make([]float32, 4), Material: m,
		UV: []UV{{0, 0}, {200, 0}, {0, 200}, {200, 200}}})
	check(t, err)
	s := NewScene()
	_, err = s.SetGeometry("floor", floor)
	check(t, err)
	camera := DefaultCamera()
	camera.Projection, camera.Alpha, camera.Distance = Perspective, 12, 2
	f, err := s.Frame(camera, 160, 120)
	check(t, err)
	return f
}

// band is the gray levels of the frame's rows from top to bottom (fractions
// of its height), where the floor is far.
func band(img image.Image, top, bottom float64) []float64 {
	b := img.Bounds()
	var out []float64
	for y := int(top * float64(b.Dy())); y < int(bottom*float64(b.Dy())); y++ {
		for x := range b.Dx() {
			out = append(out, float64(color.GrayModel.Convert(img.At(x, y)).(color.Gray).Y))
		}
	}
	return out
}

func spread(v []float64) (mean, deviation float64) {
	for _, x := range v {
		mean += x
	}
	mean /= float64(len(v))
	for _, x := range v {
		deviation += (x - mean) * (x - mean)
	}
	return mean, math.Sqrt(deviation / float64(len(v)))
}

// Far off, a mipmapped checker averages to an even gray, where sampled full
// size it breaks into noise.
func TestMipmapsAverageDistantTextures(t *testing.T) {
	mipped, full := softwareRender(checkerFloor(t, false)), softwareRender(checkerFloor(t, true))
	_, near := spread(band(mipped, .9, 1))
	// Just below the horizon, at about 27% of the height.
	mean, far := spread(band(mipped, .29, .33))
	_, noisy := spread(band(full, .29, .33))
	if near < 60 {
		t.Fatalf("the near checker is not crisp: deviation %.1f", near)
	}
	// Half light is 188 encoded.
	if far > 25 || noisy < 2*far || math.Abs(mean-188) > 15 {
		t.Fatalf("far gray: mipmapped mean %.0f deviation %.1f, full size deviation %.1f", mean, far, noisy)
	}
}

func TestTextureLevels(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 5, 3))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	tex, err := NewTexture(img)
	check(t, err)
	levels := tex.levels()
	if len(levels) != 3 || levels[2].Rect.Dx() != 1 || levels[2].Rect.Dy() != 1 {
		t.Fatalf("%d levels", len(levels))
	}
	if c := tex.sampleLinear(UV{.5, .5}, false, 1.5); math.Abs(float64(c[0]-1)) > 1e-6 || c[3] != 1 {
		t.Fatalf("white between levels: %v", c)
	}
	shader, err := NewShaderTexture("fn nt3dTexture(uv: vec2<f32>) -> vec4<f32> { return vec4<f32>(1.0); }", img)
	check(t, err)
	if len(shader.levels()) != 3 {
		t.Fatal("shader textures mip their fallback, as the GPU mips the picture it draws")
	}
	// The mip level is log2 of how many texels a pixel step crosses.
	if lod := textureLOD(tex, UV{0, 0}, UV{4. / 5, 0}, UV{0, 0}); math.Abs(float64(lod-2)) > 1e-5 {
		t.Fatalf("lod %v, want 2", lod)
	}
}

// Anisotropic filtering keeps a slanted floor's checks further toward the
// horizon than mipmapping alone, which blurs them to gray, and still evens
// out the farthest texels rather than shimmering.
func TestAnisotropy(t *testing.T) {
	f := checkerFloor(t, false)
	f.Anisotropy = 1
	mipped := softwareRender(f)
	f.Anisotropy = 0 // MaxAnisotropy.
	aniso := softwareRender(f)
	_, flat := spread(band(mipped, .33, .38))
	_, detail := spread(band(aniso, .33, .38))
	_, horizon := spread(band(aniso, .28, .30))
	if flat > 5 || detail < 20 || horizon > 15 {
		t.Fatalf("checks' spread: mipmapped %.1f, anisotropic %.1f; anisotropic at the horizon %.1f", flat, detail, horizon)
	}
	f.Anisotropy = MaxAnisotropy + 1
	if _, err := NewSoftwareRenderer().Render(f); err == nil {
		t.Fatal("anisotropy 17 accepted")
	}
}
