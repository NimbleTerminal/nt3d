// ntcharts3d - Copyright (c) 2026 Neomantra Corp.
package ntcharts3d

import (
	"image"
	"testing"
)

// Fixed anisotropy (4), software, same deterministic map as presentation tests.
// Snapshot has no adaptive raster sizing or transport. Each large-small op
// includes one 2048x1024 snapshot followed by eight 640x320 interactive sizes.
func BenchmarkDepthSnapshots(b *testing.B) {
	for _, work := range []string{"camera", "texture", "resize", "large-small"} {
		b.Run(work, func(b *testing.B) {
			p, src := presentationFixture(b, "glyph", "camera", 80, 22)
			m := p.m
			if _, _, err := m.Snapshot(640, 320); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c := m.camera
				c.Beta = -90 + float64(i%72)*5
				m.camera = c
				sizes := []image.Point{{640, 320}}
				switch work {
				case "texture":
					tex, err := NewTexture(src)
					if err != nil {
						b.Fatal(err)
					}
					m.SetSeriesTexture("map", tex)
				case "resize":
					sizes = []image.Point{{640, 320}, {320, 640}, {1280, 640}, {320, 160}}
					sizes = sizes[i%len(sizes) : i%len(sizes)+1]
				case "large-small":
					sizes = []image.Point{{2048, 1024}, {640, 320}, {640, 320}, {640, 320}, {640, 320}, {640, 320}, {640, 320}, {640, 320}, {640, 320}}
				}
				for _, size := range sizes {
					img, mode, err := m.Snapshot(size.X, size.Y)
					if err != nil || mode != Software || img.Bounds().Size() != size || m.anisotropy != 4 {
						b.Fatal("invalid snapshot", err)
					}
				}
			}
		})
	}
}
