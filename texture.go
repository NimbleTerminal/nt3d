// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"reflect"
	"strings"
	"sync"

	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/ntgpuimage"
)

// UV is a normalized image coordinate; (0,0) is the top-left corner.
type UV struct{ U, V float32 }

// Material controls triangle appearance. Texture replaces vertex colors.
// Unlit preserves colors without applying the scene light. Cutout skips
// texels whose alpha is below Threshold (zero means one half), so a texture
// can cut shapes out of a triangle, as leaves, lace, and fur do; what remains
// is opaque and depth tested as usual, with no blending. Without Cutout,
// alpha is ignored. Varying Threshold over copies of one texture reveals
// different parts of it, as fur's layers do.
//
// Repeat tiles the texture: UVs outside [0, 1] wrap, and filtering blends
// across tile edges, so a texture made to tile shows no seams. Without it,
// UVs clamp to the texture's edge.
//
// Tint multiplies the texture by the vertex colors, so one gray texture can
// take many colors. EdgeOn shows a cutout texture only where the surface
// turns edge-on to the viewer: its threshold rises toward 1 as the surface
// comes to face the camera, so it fades out face-on. Fur's fins use it to
// fill the silhouette; it needs the frame's camera (Frame.Eye), which
// Scene.Frame supplies, and wireframe rendering and picking leave it out.
//
// Transparent blends the triangles over what lies behind them, by the vertex
// colors' alpha times the texture's: glass, water, ghosts. Opaque surfaces
// are drawn first; then transparent geometries, farthest first by the
// centers of their bounds, each with its back faces (lit from behind) before
// its front faces. They are depth tested but hide nothing, so a convex shape
// shows its far side through its near one. Interleaved or intersecting
// transparent geometries, and the overlapping layers of one non-convex mesh,
// can blend out of order. Wireframe and picking treat them as opaque. It
// cannot be combined with Cutout or EdgeOn.
//
// Specular adds a white highlight where the surface mirrors the light toward
// the camera (Blinn-Phong): Specular is its strength, 0 (none) to 1, and
// Shininess its tightness, the exponent, 1 to 1024 (zero means 32: larger
// is smaller and sharper, as on polished plastic or glass). It is found at
// each pixel, from normals interpolated across the triangle, so highlights
// stay round on coarse meshes. It needs the frame's camera (Frame.Eye),
// which Scene.Frame supplies; unlit materials and wireframe have none.
type Material struct {
	// NormalMap is created with NewNormalMap and uses Geometry.UV. NormalStrength
	// scales tangent X/Y; set 1 for normal detail, 0 to disable. NormalRepeat
	// controls its sampler independently of the base-color texture.
	NormalMap      *Texture
	NormalStrength float32
	NormalRepeat   bool
	Texture        *Texture
	Unlit          bool
	Cutout         bool
	Threshold      float32
	Repeat         bool
	Tint           bool
	EdgeOn         bool
	Transparent    bool
	Specular       float32
	Shininess      float32
}

// transparent reports whether a geometry's triangles are blended.
func transparent(g *Geometry) bool {
	return g.Material != nil && g.Material.Transparent && len(g.Indices) > 0
}

// edgeOn reports whether a geometry is drawn only edge-on, so that wireframe
// and picking, which have no view of it, leave it out.
func edgeOn(g Geometry) bool { return g.Material != nil && g.Material.EdgeOn }

// edgeOnCut is a cutout threshold raised as the surface faces the viewer:
// unchanged edge-on, past 1 (cutting everything) once |normal · view| passes
// one half, smoothly between. The GPU's vs_texture computes the same.
func edgeOnCut(threshold float32, normal, view nt3dmath.Vec3) float32 {
	if normal.Dot(normal) == 0 || view.Dot(view) == 0 {
		return threshold
	}
	facing := float64(abs(normal.Normalize().Dot(view.Normalize())))
	t := min(1, max(0, (facing-.15)/.35))
	fade := float32(t * t * (3 - 2*t))
	return threshold + (1.01-threshold)*fade
}

// threshold is the alpha below which a cutout material skips texels.
func (m *Material) threshold() float32 {
	if m == nil || m.Threshold == 0 {
		return .5
	}
	return m.Threshold
}

// Texture owns an immutable snapshot of image pixels, with straight
// (non-premultiplied) alpha that only Cutout materials use. The zero value is invalid.
// GPU resources are owned by each renderer, not by Texture.
type Texture struct {
	data          bool // Linear vector data, not sRGB color.
	width, height int
	pixels        []byte
	shader        string
	parameters    [4]float32
	mipOnce       sync.Once
	mips          []*image.NRGBA // Level 0 and its mipmaps, built when first sampled.
}

// levels is the texture's mip chain: level 0, then each half the size down
// to 1×1, averaged in linear light by ntgpuimage.MipChain, the chain the GPU
// uploads too, so both renderers sample the same levels. A shader texture
// mips its fallback image here, as the GPU mips the picture it draws.
func (t *Texture) levels() []*image.NRGBA {
	t.mipOnce.Do(func() {
		base := &image.NRGBA{Pix: t.pixels, Stride: t.width * 4, Rect: image.Rect(0, 0, t.width, t.height)}
		t.mips = []*image.NRGBA{base}
		chain, err := ntgpuimage.MipChain(base, ntgpuimage.MipOptions{SRGB: !t.data, AlphaWeighted: !t.data})
		if err != nil {
			return
		}
		t.mips = make([]*image.NRGBA, len(chain))
		for i, level := range chain {
			t.mips[i] = level.(*image.NRGBA)
		}
	})
	return t.mips
}

// sampleLinear samples linear-light color and alpha, 0 to 1, at uv: filtered
// bilinearly within a level, as sampleWrap is, but in linear light, and
// blended between the two levels nearest lod (zero is full size), as the
// GPU's trilinear filtering does.
func (t *Texture) sampleLinear(uv UV, repeat bool, lod float32) [4]float32 {
	levels := t.levels()
	lod = max(0, min(lod, float32(len(levels)-1)))
	l0 := int(lod)
	c := sampleLevelData(levels[l0], uv, repeat, t.data)
	if f := lod - float32(l0); f > 0 && l0+1 < len(levels) {
		d := sampleLevelData(levels[l0+1], uv, repeat, t.data)
		for i := range c {
			c[i] += (d[i] - c[i]) * f
		}
	}
	return c
}

// sampleFootprint samples the texels a pixel covers: those between uv and
// dx, dy, where the coordinates fall a pixel right and down. A pixel seen
// square-on takes one trilinear sample, as textureLOD chooses; one seen at a
// slant covers a long, thin run of texels, and takes up to aniso samples
// spread along it, at the mip level for its width, as GPUs filter
// anisotropically.
func (t *Texture) sampleFootprint(uv, dx, dy UV, repeat bool, aniso int) [4]float32 {
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
		return t.sampleLinear(uv, repeat, lod)
	}
	var sum [4]float32
	for i := range n {
		f := (float64(i)+.5)/float64(n) - .5
		c := t.sampleLinear(UV{uv.U + float32(mu*f), uv.V + float32(mv*f)}, repeat, lod)
		for k := range sum {
			sum[k] += c[k]
		}
	}
	for k := range sum {
		sum[k] /= float32(n)
	}
	return sum
}

// sampleLevel filters one level bilinearly in linear light.
func sampleLevel(img *image.NRGBA, uv UV, repeat bool) [4]float32 {
	return sampleLevelData(img, uv, repeat, false)
}
func sampleLevelData(img *image.NRGBA, uv UV, repeat, data bool) [4]float32 {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	u, v := float64(uv.U), float64(uv.V)
	if !repeat {
		u, v = max(0, min(1, u)), max(0, min(1, v))
	}
	x := u*float64(w) - .5
	y := v*float64(h) - .5
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := float32(x-float64(x0)), float32(y-float64(y0))
	x1, y1 := x0+1, y0+1
	if repeat {
		x0, x1 = wrapTexel(x0, w), wrapTexel(x1, w)
		y0, y1 = wrapTexel(y0, h), wrapTexel(y1, h)
	} else {
		x0, x1 = max(0, min(w-1, x0)), max(0, min(w-1, x1))
		y0, y1 = max(0, min(h-1, y0)), max(0, min(h-1, y1))
	}
	x0, x1, y0, y1 = x0*4, x1*4, y0*img.Stride, y1*img.Stride
	weights := [4]float32{(1 - fx) * (1 - fy), fx * (1 - fy), (1 - fx) * fy, fx * fy}
	decode := &srgbDecode
	if data {
		decode = &dataDecode
	}
	var c [4]float32
	// Explicit taps preserve top-left, top-right, bottom-left, bottom-right
	// accumulation while eliminating both fixed-size inner loops.
	{
		px := img.Pix[y0+x0 : y0+x0+4]
		weight := weights[0]
		c[0] += weight * decode[px[0]]
		c[1] += weight * decode[px[1]]
		c[2] += weight * decode[px[2]]
		c[3] += weight * float32(px[3]) / 255
	}
	{
		px := img.Pix[y0+x1 : y0+x1+4]
		weight := weights[1]
		c[0] += weight * decode[px[0]]
		c[1] += weight * decode[px[1]]
		c[2] += weight * decode[px[2]]
		c[3] += weight * float32(px[3]) / 255
	}
	{
		px := img.Pix[y1+x0 : y1+x0+4]
		weight := weights[2]
		c[0] += weight * decode[px[0]]
		c[1] += weight * decode[px[1]]
		c[2] += weight * decode[px[2]]
		c[3] += weight * float32(px[3]) / 255
	}
	{
		px := img.Pix[y1+x1 : y1+x1+4]
		weight := weights[3]
		c[0] += weight * decode[px[0]]
		c[1] += weight * decode[px[1]]
		c[2] += weight * decode[px[2]]
		c[3] += weight * float32(px[3]) / 255
	}

	return c
}

// dataDecode shares the exact float32 division used by linear-data sampling.
// Like srgbDecode, this is one process-wide table, not per-texture storage.
var dataDecode = func() (t [256]float32) {
	for i := range t {
		t[i] = float32(i) / 255
	}
	return
}()

func wrapTexel(i, n int) int {
	i %= n
	if i < 0 {
		i += n
	}
	return i
}

// NewTexture snapshots src, including images with nonzero bounds, of at most
// 8192 pixels per side. Channels are sRGB-encoded, as images are; textures
// are filtered in linear light, and mipmapped, so surfaces seen far off or
// at a slant average their texels rather than shimmer. Alpha is kept for
// Cutout and Transparent materials; others draw every texel opaque. Cutouts
// sample full size, as averaged alpha would thin them in the distance.
func NewTexture(src image.Image) (*Texture, error) {
	if src == nil || (reflect.ValueOf(src).Kind() == reflect.Ptr && reflect.ValueOf(src).IsNil()) {
		return nil, fmt.Errorf("nt3d: nil texture image")
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 || w > 8192 || h > 8192 {
		return nil, fmt.Errorf("nt3d: texture dimensions must be 1..8192")
	}
	t := &Texture{width: w, height: h, pixels: make([]byte, w*h*4)}
	// NRGBA is already straight-alpha. Opaque RGBA has identical channel
	// representation. Copy only visible rows to preserve subimage bounds and
	// stride, and keep the texture independent of subsequent source mutations.
	var pixels []byte
	var stride, offset int
	switch img := src.(type) {
	case *image.NRGBA:
		pixels, stride, offset = img.Pix, img.Stride, img.PixOffset(b.Min.X, b.Min.Y)
	case *image.RGBA:
		if img.Opaque() {
			pixels, stride, offset = img.Pix, img.Stride, img.PixOffset(b.Min.X, b.Min.Y)
		}
	}
	if pixels != nil {
		for y := 0; y < h; y++ {
			copy(t.pixels[y*w*4:(y+1)*w*4], pixels[offset+y*stride:offset+y*stride+w*4])
		}
		return t, nil
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.NRGBAModel.Convert(src.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			i := (y*w + x) * 4
			copy(t.pixels[i:i+4], []byte{c.R, c.G, c.B, c.A})
		}
	}
	return t, nil
}
func (t *Texture) valid() bool {
	return t != nil && t.width > 0 && t.height > 0 && len(t.pixels) == t.width*t.height*4
}
func cloneMaterial(m *Material) *Material {
	if m == nil {
		return nil
	}
	c := *m
	return &c
}
func textured(g Geometry) bool { return g.Material != nil && g.Material.Texture != nil }

// sample uses the same texel-center convention as a bilinear clamp-to-edge
// sampler, filtering straight alpha as the GPU does.
func (t *Texture) sample(uv UV) color.NRGBA { return t.sampleWrap(uv, false) }

// sampleWrap samples clamped, or repeating, as the GPU's sampleMap does:
// wrapping the four texels it blends, so tiles meet without a seam.
func (t *Texture) sampleWrap(uv UV, repeat bool) color.NRGBA {
	u, v := float64(uv.U), float64(uv.V)
	if !repeat {
		u, v = max(0, min(1, u)), max(0, min(1, v))
	}
	x := u*float64(t.width) - .5
	y := v*float64(t.height) - .5
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(x0), y-float64(y0)
	at := func(i, n int) int {
		if repeat {
			return ((i % n) + n) % n
		}
		return max(0, min(n-1, i))
	}
	pixel := func(x, y, c int) float64 {
		return float64(t.pixels[(at(y, t.height)*t.width+at(x, t.width))*4+c])
	}
	var c [4]uint8
	for i := range c {
		c[i] = uint8((pixel(x0, y0, i)*(1-fx)+pixel(x0+1, y0, i)*fx)*(1-fy) + (pixel(x0, y0+1, i)*(1-fx)+pixel(x0+1, y0+1, i)*fx)*fy + .5)
	}
	return color.NRGBA{c[0], c[1], c[2], c[3]}
}

// Bounds returns the snapshot dimensions with a zero origin.
func (t *Texture) Bounds() image.Rectangle {
	if t == nil {
		return image.Rectangle{}
	}
	return image.Rect(0, 0, t.width, t.height)
}

// ColorModel implements image.Image without exposing mutable storage.
func (t *Texture) ColorModel() color.Model { return color.RGBAModel }

// At implements image.Image. Pixels outside Bounds are transparent black.
func (t *Texture) At(x, y int) color.Color {
	if t == nil || x < 0 || y < 0 || x >= t.width || y >= t.height {
		return color.RGBA{}
	}
	i := (y*t.width + x) * 4
	return color.RGBAModel.Convert(color.NRGBA{t.pixels[i], t.pixels[i+1], t.pixels[i+2], t.pixels[i+3]})
}

// NewShaderTexture creates an immutable procedural texture. source must define
// fn nt3dTexture(uv: vec2<f32>) -> vec4<f32>, returning straight RGBA in [0,1];
// alpha matters only to Cutout materials.
// UV (0,0) is the top-left. nt3dTextureParams is a zero-valued vec4<f32> uniform. WGSL is compiled lazily
// by the GPU renderer; compilation errors are returned by Render.
// fallback is required: it sets the target dimensions and supplies pixels for
// software rendering and image.Image access. GPU rendering never uploads these
// pixels: it generates and samples the texture on the same device.
func NewShaderTexture(source string, fallback image.Image) (*Texture, error) {
	return NewShaderTextureWithParameters(source, [4]float32{}, fallback)
}

// NewShaderTextureWithParameters snapshots a procedural texture and its parameters.
// WGSL can read nt3dTextureParams (vec4<f32>, group 0 binding 0). Parameters must
// be finite. Supply fallback pixels evaluated with the same parameters for software
// rendering. Create a new texture and use Scene.SetTexture to advance animation;
// previous textures and frames remain immutable. Unchanged source and dimensions
// allow the renderer to reuse GPU resources across successive snapshots.
func NewShaderTextureWithParameters(source string, parameters [4]float32, fallback image.Image) (*Texture, error) {
	for _, v := range parameters {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return nil, fmt.Errorf("nt3d: non-finite texture parameter")
		}
	}
	if strings.TrimSpace(source) == "" {
		return nil, fmt.Errorf("nt3d: empty texture shader")
	}
	t, err := NewTexture(fallback)
	if err != nil {
		return nil, err
	}
	t.shader = source
	t.parameters = parameters
	return t, nil
}
