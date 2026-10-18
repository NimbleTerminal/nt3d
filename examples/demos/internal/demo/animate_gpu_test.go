//go:build webgpu

package demo

import (
	"nimbleterminal.dev/nt3d"
	"testing"
)

func TestGPUKhronosSamples(t *testing.T) {
	d := newAnimateDemo(t)
	renderer := nt3d.NewGPURenderer()
	defer renderer.Close()
	for i, sample := range animatedSamples {
		t.Run(sample.title, func(t *testing.T) {
			if err := d.loadAnimatedSample(i, 0); err != nil {
				t.Fatal(err)
			}
			d.frameAnimatedSample()
			for _, fraction := range []float64{0, .37, .75} {
				if err := d.animation.Sample(d.animation.Clip.Duration() * fraction); err != nil {
					t.Fatal(err)
				}
				f, err := d.scene.Frame(d.view.Camera(), 160, 120)
				if err != nil {
					t.Fatal(err)
				}
				img, err := renderer.Render(f)
				if err != nil {
					t.Fatal(err)
				}
				assertAnimatedSampleVisible(t, img, f.Background)
			}
		})
	}
}
