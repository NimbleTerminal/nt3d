package nt3d

import (
	"image"
	"image/color"
	"nimbleterminal.dev/nt3d/math"
	"testing"
)

func spriteAtlas(t *testing.T) *Texture {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	colors := []color.NRGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 128}, {255, 255, 255, 0}}
	for y := range 8 {
		for x := range 8 {
			img.SetNRGBA(x, y, colors[(y/4)*2+x/4])
		}
	}
	tex, err := NewTexture(img)
	check(t, err)
	return tex
}

func TestSpriteAtlasAndCutout(t *testing.T) {
	style := PointStyle{Texture: spriteAtlas(t), Columns: 2, Rows: 2}
	for i, want := range []color.RGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}, {0, 0, 0, 255}} {
		p := Point{Position: nt3dmath.Vec3{Z: .5}, Radius: 12, Color: white, Frame: uint32(i)}
		f := flatFrame(64, 64, styled(style, p))
		check(t, validateFrame(f))
		img := softwareRender(f)
		for _, xy := range [][2]int{{32, 32}, {21, 21}, {43, 43}} {
			if got := at(img, xy[0], xy[1]); got != want {
				t.Errorf("frame %d at %v: %v want %v", i, xy, got, want)
			}
		}
	}
	style.Frame = 3
	if got := style.spriteSample(Point{Frame: 2}, .5, .5); got != ([4]float32{0, 1, 0, 1}) {
		t.Fatal("frame offset/wrap", got)
	}
	style.Columns = 3
	if validPointStyle(style) == nil {
		t.Fatal("uneven atlas accepted")
	}
}

func TestSpriteBlendingAndDepth(t *testing.T) {
	style := PointStyle{Texture: spriteAtlas(t), Columns: 2, Rows: 2, Frame: 2}
	p := Point{Position: nt3dmath.Vec3{Z: .5}, Radius: 12, Color: white}
	for _, blend := range []PointBlend{AlphaBlend, AdditiveBlend} {
		style.Blend = blend
		f := flatFrame(64, 64, styled(style, p))
		img := softwareRender(f)
		if c := at(img, 32, 32); c.B < 185 || c.B > 189 || c.R != 0 {
			t.Fatal("texture alpha missing", blend, c)
		}
		front := Point{Position: nt3dmath.Vec3{Z: .25}, Radius: 20, Color: color.RGBA{255, 0, 0, 255}}
		f = flatFrame(64, 64, styled(style, p), styled(PointStyle{}, front))
		img = softwareRender(f)
		if at(img, 32, 32) != front.Color {
			t.Fatal("sprite over nearer opaque point")
		}
	}
}

func TestSpritePickingAndUpdatedBounds(t *testing.T) {
	s := NewScene()
	camera := DefaultCamera()
	p := Point{Radius: 12, Color: white, Datum: 7}
	id, err := s.SetGeometry("sprite", styled(PointStyle{Texture: spriteAtlas(t), Columns: 2, Rows: 2}, p))
	check(t, err)
	f, err := s.Frame(camera, 64, 64)
	check(t, err)
	// A corner inside the square but outside the old disc is pickable.
	if hit := s.Pick(camera, f, 42, 42, 0); hit == nil || hit.Datum != 7 {
		t.Fatal("sprite corner not picked")
	}
	p.Frame = 3
	check(t, s.SetNodePoints(id, []Point{p}, pointBounds()))
	f, err = s.Frame(camera, 64, 64)
	check(t, err)
	if s.Pick(camera, f, 32, 32, 0) != nil {
		t.Fatal("transparent cell picked")
	}
	p.Frame = 1
	p.Position = nt3dmath.Vec3{X: .4}
	var bounds nt3dmath.AABB
	bounds.Include(p.Position)
	check(t, s.SetNodePoints(id, []Point{p}, bounds))
	f, err = s.Frame(camera, 64, 64)
	check(t, err)
	x, y, _, w := f.Matrix.Transform(p.Position)
	hit := s.Pick(camera, f, (x/w+1)*32, (1-y/w)*32, 0)
	if hit == nil || hit.Position != p.Position {
		t.Fatal("moved point not picked")
	}
	if f.Geometry[0].Bounds != bounds || !f.drawn(1)[0] {
		t.Fatal("updated bounds culled visible point")
	}
}
