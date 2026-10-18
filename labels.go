// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
	"nimbleterminal.dev/nt3d/math"
)

// Billboard describes a quad centered on a node's origin. By default its
// local XY plane faces the camera. Fixed preserves the node's orientation.
// Pixels interprets local coordinates as render pixels, independent of zoom;
// otherwise they are world units. Camera-facing quads inherit translation,
// but not parent rotation or scale. Fixed quads inherit the entire transform.
// Text is the optional wireframe representation. Empty Text draws quad edges.
// NoPick lets clicks pass through. Use NewBillboard to construct a quad.
type Billboard struct {
	Fixed, Pixels, NoPick bool
	Text                  string
	Color                 color.RGBA
}

// NewBillboard makes a textured quad of the given positive width and height.
// Cutout texels write depth; transparent quads blend and do not write depth.
// The texture is immutable and can be shared, as can the returned geometry
// through NewSharedGeometry. Opacity is 0..1 (zero is fully invisible).
func NewBillboard(texture *Texture, width, height, opacity float32, transparent bool, style Billboard) (Geometry, error) {
	if !texture.valid() || !validFloat(width, height, opacity) || width <= 0 || height <= 0 || opacity < 0 || opacity > 1 {
		return Geometry{}, fmt.Errorf("nt3d: invalid billboard texture, size, or opacity")
	}
	if opacity < 1 {
		transparent = true
	}
	ink := color.RGBA{255, 255, 255, uint8(opacity*255 + .5)}
	g := Geometry{Billboard: &style, Material: &Material{Texture: texture, Unlit: true, Cutout: !transparent, Transparent: transparent},
		Vertices: []Vertex{
			{Position: nt3dmath.Vec3{X: -width / 2, Y: -height / 2}, Color: ink},
			{Position: nt3dmath.Vec3{X: width / 2, Y: -height / 2}, Color: ink},
			{Position: nt3dmath.Vec3{X: width / 2, Y: height / 2}, Color: ink},
			{Position: nt3dmath.Vec3{X: -width / 2, Y: height / 2}, Color: ink},
		}, Indices: []uint32{0, 1, 2, 0, 2, 3}, UV: []UV{{0, 1}, {1, 1}, {1, 0}, {0, 0}}}
	g.Bounds = GeometryBounds(g)
	return g, g.Validate()
}

// Label is text baked into an immutable texture when Geometry or SetLabel is
// called. Height is the total quad height (zero uses the texture's pixel height).
// Pixels selects render-pixel sizes; otherwise Height is in world units.
// Face defaults to basicfont.Face7x13; supply a face for other sizes or scripts.
// Padding and Border are texture pixels. Color defaults to white. Background,
// BorderColor, and Shadow use alpha; zero leaves them out. Shadow is offset by
// one texture pixel. Transparent enables soft alpha blending instead of the
// default depth-writing cutout. Opacity defaults to one; Hide makes it zero.
type Label struct {
	Text                                     string
	Face                                     font.Face
	Height                                   float32
	Pixels, Fixed, NoPick, Transparent, Hide bool
	Color, Background, BorderColor, Shadow   color.RGBA
	Padding, Border                          int
	Opacity                                  float32
	Icon                                     image.Image // Optional icon, drawn at native size to the left of text.
}

func (l Label) Geometry() (Geometry, error) {
	if !validFloat(l.Height, l.Opacity) || l.Height < 0 || l.Opacity < 0 || l.Opacity > 1 || l.Padding < 0 || l.Border < 0 || l.Padding > 4096 || l.Border > 4096 || len(l.Text) > 65536 {
		return Geometry{}, fmt.Errorf("nt3d: invalid label size, padding, or opacity")
	}
	face := l.Face
	if face == nil {
		face = basicfont.Face7x13
	}
	lines := strings.Split(l.Text, "\n")
	metrics := face.Metrics()
	lineHeight := max(1, metrics.Height.Ceil())
	tw := 0
	for _, line := range lines {
		tw = max(tw, font.MeasureString(face, line).Ceil())
	}
	iw, ih := 0, 0
	if l.Icon != nil {
		iw, ih = l.Icon.Bounds().Dx(), l.Icon.Bounds().Dy()
		if iw < 0 || ih < 0 || iw > 8192 || ih > 8192 {
			return Geometry{}, fmt.Errorf("nt3d: invalid label icon")
		}
	}
	gap := 0
	if iw > 0 && l.Text != "" {
		gap = 2
	}
	inset := l.Padding + l.Border
	w, h := max(1, tw+iw+gap+2*inset+1), max(1, max(len(lines)*lineHeight, ih)+2*inset+1)
	if w > 8192 || h > 8192 || w > MaxFramePixels/h {
		return Geometry{}, fmt.Errorf("nt3d: label texture too large")
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(l.BorderColor), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(l.Border, l.Border, w-l.Border, h-l.Border), image.NewUniform(l.Background), image.Point{}, draw.Src)
	if l.Icon != nil {
		draw.Draw(img, image.Rect(inset, inset, inset+iw, inset+ih), l.Icon, l.Icon.Bounds().Min, draw.Over)
	}
	ink := l.Color
	if ink == (color.RGBA{}) {
		ink = color.RGBA{255, 255, 255, 255}
	}
	for i, line := range lines {
		p := fixed.P(inset+iw+gap, inset+metrics.Ascent.Ceil()+i*lineHeight)
		if l.Shadow.A > 0 {
			d := font.Drawer{Dst: img, Src: image.NewUniform(l.Shadow), Face: face, Dot: p.Add(fixed.P(1, 1))}
			d.DrawString(line)
		}
		d := font.Drawer{Dst: img, Src: image.NewUniform(ink), Face: face, Dot: p}
		d.DrawString(line)
	}
	tex, err := NewTexture(img)
	if err != nil {
		return Geometry{}, err
	}
	height := l.Height
	if height == 0 {
		if l.Pixels {
			height = float32(h)
		} else {
			height = 1
		}
	}
	opacity := l.Opacity
	if opacity == 0 {
		opacity = 1
	}
	if l.Hide {
		opacity = 0
	}
	return NewBillboard(tex, height*float32(w)/float32(h), height, opacity, l.Transparent, Billboard{Fixed: l.Fixed, Pixels: l.Pixels, NoPick: l.NoPick, Text: l.Text, Color: ink})
}

// SetLabel replaces a node's label or creates it under Root. Replacing the
// label preserves its transform and children. Reparent attaches it to a mesh.
// For many identical labels, build Geometry once and use NewSharedGeometry.
func (s *Scene) SetLabel(name string, label Label) (NodeID, error) {
	g, err := label.Geometry()
	if err != nil {
		return 0, err
	}
	return s.SetGeometry(name, g)
}

// billboardWorld resolves orientation and pixel scale once, before all renderers
// and picking consume the same ordinary textured geometry and instance matrix.
func billboardWorld(f Frame, camera Camera, world nt3dmath.Mat4, b *Billboard) (nt3dmath.Mat4, bool) {
	anchor := world.Point(nt3dmath.Vec3{})
	_, _, z, w := f.Matrix.Transform(anchor)
	if w <= 0 || z < 0 || z > w {
		return world, false
	}
	if !b.Fixed {
		_, r, u, forward := camera.basis()
		n := forward.Scale(-1)
		world = nt3dmath.Mat4{r.X, r.Y, r.Z, 0, u.X, u.Y, u.Z, 0, n.X, n.Y, n.Z, 0, anchor.X, anchor.Y, anchor.Z, 1}
	}
	if b.Pixels {
		// Camera vertical projection scale, independent of the view rotation.
		_, _, up, _ := camera.basis()
		scale := abs(f.Matrix[1]*up.X + f.Matrix[5]*up.Y + f.Matrix[9]*up.Z)
		units := 2 * w / (float32(f.Height) * scale)
		for i := 0; i < 12; i++ {
			if i%4 != 3 {
				world[i] *= units
			}
		}
	}
	return world, true
}
