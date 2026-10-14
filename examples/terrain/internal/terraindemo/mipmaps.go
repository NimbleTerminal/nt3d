// Copyright (c) 2026 Neomantra Corp.
package terraindemo

import (
	"fmt"
	"image"
	"image/color"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
)

// MipmapsMain shows textures tiled to the horizon, with mipmaps or without.
func MipmapsMain() { mainDemo(true, sceneMipmaps) }

type mipScene struct {
	textures []*nt3d.Texture // One per mipPatterns entry.
	pattern  int
	full     bool // Sampled full size, as without mipmaps.
}

// mipPatterns are textures that shimmer when sampled full size far off.
var mipPatterns = []struct {
	name  string
	image func() image.Image
}{
	{"checker", checkerImage},
	{"bricks", brickImage},
	{"fine grid", gridImage},
}

func checkerImage() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			c := color.NRGBA{30, 34, 48, 255}
			if (x/8+y/8)%2 == 0 {
				c = color.NRGBA{236, 232, 220, 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// brickImage is a running bond: rows of bricks, each row offset by half.
func brickImage() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	for y := range 32 {
		for x := range 64 {
			shift := 0
			if y/16%2 == 1 {
				shift = 16
			}
			mortar := y%16 < 2 || (x+shift)%32 < 2
			c := color.NRGBA{uint8(150 + (x*7+y*13)%40), 62, 44, 255}
			if mortar {
				c = color.NRGBA{205, 200, 190, 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// gridImage is one-texel lines on a light ground: the worst for shimmer.
func gridImage() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			c := color.NRGBA{232, 236, 240, 255}
			if x == 0 || y == 0 {
				c = color.NRGBA{20, 70, 140, 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func (d *demo) setupMipmaps() error {
	d.mipmaps = &mipScene{}
	for _, p := range mipPatterns {
		t, err := nt3d.NewTexture(p.image())
		if err != nil {
			return err
		}
		d.mipmaps.textures = append(d.mipmaps.textures, t)
	}
	ball, err := nt3d.BuildSphere(nt3d.Sphere{Radius: .35, Color: color.RGBA{214, 120, 52, 255}, Material: &nt3d.Material{Specular: .6, Shininess: 64}})
	if err != nil {
		return err
	}
	ball, err = nt3d.TransformGeometry(ball, nt3dmath.Translation(nt3dmath.Vec3{Z: .35}))
	if err != nil {
		return err
	}
	if _, err = d.scene.SetGeometry("ball", ball); err != nil {
		return err
	}
	return d.mipFloor()
}

// mipFloor lays the floor: 80 units across, its texture tiled 160 times.
func (d *demo) mipFloor() error {
	m := &nt3d.Material{Texture: d.mipmaps.textures[d.mipmaps.pattern], Repeat: true}
	if d.mipmaps.full {
		// A cutout that cuts nothing samples full size, as every texture
		// did before mipmaps.
		m.Cutout, m.Threshold = true, .01
	}
	floor, err := nt3d.BuildHeightfield(nt3d.Heightfield{NX: 2, NY: 2, MinX: -40, MaxX: 40, MinY: -40, MaxY: 40, Heights: make([]float32, 4), Material: m,
		UV: []nt3d.UV{{U: 0, V: 0}, {U: 160, V: 0}, {U: 0, V: 160}, {U: 160, V: 160}}})
	if err != nil {
		return err
	}
	_, err = d.scene.SetGeometry("floor", floor)
	return err
}

func (d *demo) toggleMipmaps() error {
	d.mipmaps.full = !d.mipmaps.full
	return d.mipFloor()
}

func (d *demo) cyclePattern() error {
	d.mipmaps.pattern = (d.mipmaps.pattern + 1) % len(mipPatterns)
	return d.mipFloor()
}

func (d *demo) mipmapsView(content string) string {
	mode := "mipmaps"
	if d.mipmaps.full {
		mode = "full size"
	}
	return fmt.Sprintf("nt3d mipmaps · m mipmaps · t texture · x supersample · d fog · 1/2/3 backend · q quit\n%s\n%s · %s · %s · supersample %d× · drag orbit · wheel zoom · r rotate · %s",
		content, d.rendering(), mode, mipPatterns[d.mipmaps.pattern].name, d.view.Supersample(), d.status)
}
