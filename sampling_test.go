// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"image"
	"math"
	"math/rand/v2"
	"testing"
)

// Frozen afc39e3 sampler: an independent oracle for addressing and rounding.
func referenceSampleLevel(img *image.NRGBA, uv UV, repeat, data bool) [4]float32 {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	u, v := float64(uv.U), float64(uv.V)
	if !repeat {
		u, v = max(0, min(1, u)), max(0, min(1, v))
	}
	x := u*float64(w) - .5
	y := v*float64(h) - .5
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := float32(x-float64(x0)), float32(y-float64(y0))
	at := func(i, n int) int {
		if repeat {
			return ((i % n) + n) % n
		}
		return max(0, min(n-1, i))
	}
	var c [4]float32
	for k, p := range [4][2]int{{x0, y0}, {x0 + 1, y0}, {x0, y0 + 1}, {x0 + 1, y0 + 1}} {
		weight := [4]float32{(1 - fx) * (1 - fy), fx * (1 - fy), (1 - fx) * fy, fx * fy}[k]
		px := img.Pix[at(p[1], h)*img.Stride+at(p[0], w)*4:]
		for j := range 3 {
			v := srgbDecode[px[j]]
			if data {
				v = float32(px[j]) / 255
			}
			c[j] += weight * v
		}
		c[3] += weight * float32(px[3]) / 255
	}
	return c
}

func (t *Texture) referenceSampleLinear(uv UV, repeat bool, lod float32) [4]float32 {
	levels := t.levels()
	lod = max(0, min(lod, float32(len(levels)-1)))
	l0 := int(lod)
	c := referenceSampleLevel(levels[l0], uv, repeat, t.data)
	if f := lod - float32(l0); f > 0 && l0+1 < len(levels) {
		d := referenceSampleLevel(levels[l0+1], uv, repeat, t.data)
		for i := range c {
			c[i] += (d[i] - c[i]) * f
		}
	}
	return c
}

func (t *Texture) referenceSampleFootprint(uv, dx, dy UV, repeat bool, aniso int) [4]float32 {
	w, h := float64(t.width), float64(t.height)
	ax, ay := float64(dx.U-uv.U), float64(dx.V-uv.V)
	bx, by := float64(dy.U-uv.U), float64(dy.V-uv.V)
	lx, ly := math.Hypot(ax*w, ay*h), math.Hypot(bx*w, by*h)
	major, minor, mu, mv := lx, ly, ax, ay
	if ly > lx {
		major, minor, mu, mv = ly, lx, bx, by
	}
	n := 1
	if aniso > 1 && major > 1 {
		n = aniso
		if minor > 0 {
			n = min(aniso, int(math.Ceil(major/minor)))
		}
	}
	lod := float32(0)
	if step := major / float64(n); step > 1 {
		lod = float32(math.Log2(step))
	}
	if n == 1 {
		return t.referenceSampleLinear(uv, repeat, lod)
	}
	var sum [4]float32
	for i := range n {
		f := (float64(i)+.5)/float64(n) - .5
		c := t.referenceSampleLinear(UV{uv.U + float32(mu*f), uv.V + float32(mv*f)}, repeat, lod)
		for k := range sum {
			sum[k] += c[k]
		}
	}
	for k := range sum {
		sum[k] /= float32(n)
	}
	return sum
}

func samplingTexture(w, h int, data bool) *Texture {
	pixels := make([]byte, w*h*4)
	rng := rand.New(rand.NewPCG(17, 29))
	for i := range pixels {
		pixels[i] = byte(rng.Uint32())
	}
	return &Texture{width: w, height: h, pixels: pixels, data: data}
}

func TestSamplingMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(31, 47))
	for _, size := range [][2]int{{1, 1}, {1, 9}, {11, 1}, {3, 7}, {16, 32}, {63, 17}, {512, 512}} {
		for _, data := range []bool{false, true} {
			tex := samplingTexture(size[0], size[1], data)
			for _, repeat := range []bool{false, true} {
				for _, aniso := range []int{1, 4, 16} {
					for i := 0; i < 400; i++ {
						uv := UV{rng.Float32()*4 - 2, rng.Float32()*4 - 2}
						if i < 9 {
							uv = UV{float32(i%3) * .5, float32(i/3) * .5}
						}
						dx, dy := UV{uv.U + rng.Float32()*.2, uv.V + .0001}, UV{uv.U + .0003, uv.V + rng.Float32()*.05}
						got, want := tex.sampleFootprint(uv, dx, dy, repeat, aniso), tex.referenceSampleFootprint(uv, dx, dy, repeat, aniso)
						if got != want {
							t.Fatalf("%v data=%v repeat=%v aniso=%d uv=%v: %v != %v", size, data, repeat, aniso, uv, got, want)
						}
						for _, lod := range []float32{0, .001, .99, 1, 1.5, 2.99, 3, 20} {
							got, want = tex.sampleLinear(uv, repeat, lod, false), tex.referenceSampleLinear(uv, repeat, lod)
							if got != want {
								t.Fatalf("LOD %v: %v != %v", lod, got, want)
							}
						}
					}
				}
			}
		}
	}
}

var samplingSink [4]float32

func BenchmarkSampling(b *testing.B) {
	for _, data := range []bool{false, true} {
		for _, repeat := range []bool{false, true} {
			for _, aniso := range []int{1, 4, 16} {
				b.Run(fmt.Sprintf("data=%v/repeat=%v/aniso%d", data, repeat, aniso), func(b *testing.B) {
					tex := samplingTexture(512, 512, data)
					tex.levels()
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						uv := UV{float32(i%1024)/512 - .25, float32(i%73) / 100}
						samplingSink = tex.sampleFootprint(uv, UV{uv.U + .03, uv.V + .001}, UV{uv.U, uv.V + .001}, repeat, aniso)
					}
				})
			}
		}
	}
}

func TestSamplingStridedLevels(t *testing.T) {
	tex := samplingTexture(13, 9, false)
	parent := &image.NRGBA{Pix: tex.pixels, Stride: 13 * 4, Rect: image.Rect(0, 0, 13, 9)}
	sub := parent.SubImage(image.Rect(3, 2, 10, 8)).(*image.NRGBA)
	for _, repeat := range []bool{false, true} {
		for _, data := range []bool{false, true} {
			for i := -40; i <= 40; i++ {
				uv := UV{float32(i) / 17, float32(i) / 23}
				if got, want := sampleLevelData(sub, uv, repeat, data), referenceSampleLevel(sub, uv, repeat, data); got != want {
					t.Fatalf("strided: %v != %v", got, want)
				}
			}
		}
	}
}

func TestSamplingConcurrentReaders(t *testing.T) {
	tex := samplingTexture(63, 17, false)
	// Exercise lazy mip publication, followed by repeated immutable reads.
	done := make(chan bool, 8)
	for range 8 {
		go func() {
			ok := true
			for i := 0; i < 100; i++ {
				uv := UV{float32(i) / 97, .35}
				a := tex.sampleFootprint(uv, UV{uv.U + .2, .35}, UV{uv.U, .36}, true, 16)
				b := tex.referenceSampleFootprint(uv, UV{uv.U + .2, .35}, UV{uv.U, .36}, true, 16)
				ok = ok && a == b
			}
			done <- ok
		}()
	}
	for range 8 {
		if !<-done {
			t.Fatal("concurrent sampler differed")
		}
	}
}
