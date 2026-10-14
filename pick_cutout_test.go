// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"image"
	"image/color"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// Picks pass through a cutout's gaps to what the renderer shows behind
// them, whether triangles are tested one by one or through the index.
func TestPickThroughCutout(t *testing.T) {
	// Vertical stripes, solid and clear, repeated four times across.
	stripes := image.NewNRGBA(image.Rect(0, 0, 8, 1))
	for x := range 8 {
		a := uint8(255)
		if x%2 == 1 {
			a = 0
		}
		stripes.SetNRGBA(x, 0, color.NRGBA{128, 128, 128, a})
	}
	tex, err := NewTexture(stripes)
	check(t, err)
	red := color.RGBA{220, 30, 30, 255}
	for _, n := range []int{2, 17} { // 2 triangles, then 512: indexed.
		t.Run(fmt.Sprint(n, "x", n), func(t *testing.T) {
			s := NewScene()
			front, err := BuildHeightfield(Heightfield{NX: n, NY: n, MinX: -1.5, MaxX: 1.5, MinY: -1.5, MaxY: 1.5, Heights: make([]float32, n*n), Material: &Material{Texture: tex, Cutout: true, Repeat: true}})
			check(t, err)
			for i := range front.UV {
				front.UV[i].U *= 4
			}
			back, err := BuildHeightfield(Heightfield{NX: 2, NY: 2, MinX: -3, MaxX: 3, MinY: -3, MaxY: 3, Heights: make([]float32, 4), Color: red})
			check(t, err)
			frontID, err := s.SetGeometry("front", front)
			check(t, err)
			check(t, s.SetTransform(frontID, nt3dmath.Translation(nt3dmath.Vec3{Z: .3})))
			backID, err := s.SetGeometry("back", back)
			check(t, err)
			camera := DefaultCamera()
			camera.Projection, camera.Alpha = Perspective, 89
			f, err := s.Frame(camera, 120, 120)
			check(t, err)
			f.Light = Light{Direction: nt3dmath.Vec3{Z: 1}, Ambient: 1}
			if n > 2 {
				s.Pick(camera, f, 60, 60, 1) // Starts the index; wait for it.
				<-s.nodes[frontID].pickIndex.done
				if s.pickIndex(f, 0) == nil {
					t.Fatal("the front was not indexed")
				}
			}
			img := softwareRender(f)
			agree, through, cases := 0, 0, 0
			for y := 20; y < 100; y += 3 {
				for x := 20; x < 100; x++ {
					hit := s.Pick(camera, f, float32(x)+.5, float32(y)+.5, 1)
					if hit == nil {
						t.Fatalf("pick at %d,%d hit nothing", x, y)
					}
					shown := frontID
					if color.RGBAModel.Convert(img.At(x, y)).(color.RGBA) == red {
						shown = backID
					}
					cases++
					if hit.Node == shown {
						agree++
					}
					if hit.Node == backID {
						through++
					}
				}
			}
			if through < cases/3 || through > cases*2/3 {
				t.Fatalf("%d of %d picks went through the gaps", through, cases)
			}
			// Texels at the stripes' edges blend; the picture and the pick
			// sample them a hair apart.
			if agree < cases*97/100 {
				t.Fatalf("picks agree with the picture at %d of %d pixels", agree, cases)
			}
		})
	}
}
