package main

import (
	"fmt"
	"image"
	"image/color"
	"testing"

	chart "github.com/NimbleMarkets/ntcharts3d"
)

// Use a deterministic opaque map-like texture: blocks, streets, and a river.
// This exercises filtering without network variability or a local tile cache.
func benchmarkMapImage(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			c := color.RGBA{R: 218, G: 226, B: 206, A: 255}
			switch {
			case (x+y/3)%size < size/12:
				c = color.RGBA{R: 120, G: 175, B: 210, A: 255}
			case x%32 < 3 || y%32 < 3:
				c = color.RGBA{R: 245, G: 240, B: 225, A: 255}
			default:
				c.R -= uint8((x/32+y/32)%5) * 8
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func benchmarkChart(b *testing.B, anisotropy int) (*chart.Model, *image.RGBA) {
	b.Helper()
	img := benchmarkMapImage(512) // 64 columns * 8 px, 32 map rows * 16 px.
	texture, err := chart.NewTexture(img)
	if err != nil {
		b.Fatal(err)
	}
	c := chart.New(80, 21, chart.WithRenderMode(chart.Software), chart.WithAnisotropy(anisotropy),
		chart.WithAxes(chart.Axes{X: chart.Axis{Hidden: true}, Y: chart.Axis{Hidden: true}, Z: chart.Axis{Hidden: true}}))
	b.Cleanup(func() { _ = c.Close() })
	c.SetColorLegendVisible(false)
	c.SetCamera(initialCamera())
	c.SetSurface("map", mapSurface(texture, img.Bounds()))
	if c.Err() != nil {
		b.Fatal(c.Err())
	}
	return c, img
}

func snapshotBenchmarkFrame(b *testing.B, c *chart.Model, width, height int) {
	b.Helper()
	img, mode, err := c.Snapshot(width, height)
	if err != nil {
		b.Fatal(err)
	}
	if mode != chart.Software || img.Bounds().Dx() != width || img.Bounds().Dy() != height {
		b.Fatal("unexpected renderer or frame size")
	}
}

// Snapshot uses the same software engine as interactive rendering but fixes
// output resolution, excludes presentation/encoding, and never adapts quality.
func BenchmarkMap3DCamera(b *testing.B) {
	for _, size := range []image.Point{{320, 160}, {640, 320}, {1280, 640}} {
		for _, anisotropy := range []int{1, 4} {
			b.Run(fmt.Sprintf("%dx%d/aniso%d", size.X, size.Y, anisotropy), func(b *testing.B) {
				c, _ := benchmarkChart(b, anisotropy)
				snapshotBenchmarkFrame(b, c, size.X, size.Y)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					camera := initialCamera()
					camera.Beta += float64(i%72) * 5
					c.SetCamera(camera)
					snapshotBenchmarkFrame(b, c, size.X, size.Y)
				}
			})
		}
	}
}

// Include immutable texture copying/mipmap construction, series replacement,
// and rendering. Tile fetching and map composition are intentionally excluded.
func BenchmarkMap3DTextureReplacement(b *testing.B) {
	c, img := benchmarkChart(b, 4)
	snapshotBenchmarkFrame(b, c, 640, 320)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		texture, err := chart.NewTexture(img)
		if err != nil {
			b.Fatal(err)
		}
		c.SetSeriesTexture("map", texture)
		camera := initialCamera()
		camera.Beta += float64(i%72) * 5
		c.SetCamera(camera)
		if c.Err() != nil {
			b.Fatal(c.Err())
		}
		snapshotBenchmarkFrame(b, c, 640, 320)
	}
}

func BenchmarkMap3DNewTexture(b *testing.B) {
	img := benchmarkMapImage(512)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		texture, err := chart.NewTexture(img)
		if err != nil || texture == nil {
			b.Fatalf("texture creation: %v", err)
		}
	}
}
