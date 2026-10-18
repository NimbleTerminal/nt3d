// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image"
	"image/color"
	"math"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// Environment is an immutable distant sky and diffuse image-based light.
// Construct one with NewEnvironment or GradientEnvironment and share it between
// scenes. It does not contribute geometry, bounds, picking, or shadow casters.
type Environment struct {
	texture  *Texture
	panorama *image.NRGBA
	// First-order spherical harmonics after cosine convolution, divided by pi:
	// constant plus X, Y, Z. A constant white image produces unit diffuse light.
	diffuse [4][3]float32
}

// NewEnvironment snapshots an sRGB equirectangular image (normally 2:1).
// +Z is up (top row); the image center faces +X, and U increases toward +Y.
// Alpha is ignored. Dimensions have the same limits as NewTexture.
// Lighting uses a low-frequency diffuse approximation, without specular IBL.
func NewEnvironment(src image.Image) (*Environment, error) {
	t, err := NewTexture(src)
	if err != nil {
		return nil, err
	}
	for i := 3; i < len(t.pixels); i += 4 {
		t.pixels[i] = 255
	}
	e := &Environment{texture: t, panorama: &image.NRGBA{Pix: t.pixels, Stride: t.width * 4, Rect: image.Rect(0, 0, t.width, t.height)}}
	// Fixed equal-area quadrature bounds setup cost independently of image size.
	const nx, ny = 64, 32
	for y := 0; y < ny; y++ {
		z := 1 - 2*(float64(y)+.5)/ny
		r := math.Sqrt(1 - z*z)
		for x := 0; x < nx; x++ {
			phi := ((float64(x)+.5)/nx - .5) * 2 * math.Pi
			d := nt3dmath.Vec3{X: float32(r * math.Cos(phi)), Y: float32(r * math.Sin(phi)), Z: float32(z)}
			c := e.sample(d)
			weights := [4]float32{1, 2 * d.X, 2 * d.Y, 2 * d.Z}
			for j, w := range weights {
				for k := range 3 {
					e.diffuse[j][k] += c[k] * w / (nx * ny)
				}
			}
		}
	}
	return e, nil
}

// GradientEnvironment creates a cheap sky, blending zenith to horizon to
// ground in linear light. Colors are opaque sRGB; their alpha is ignored.
func GradientEnvironment(zenith, horizon, ground color.RGBA) *Environment {
	img := image.NewNRGBA(image.Rect(0, 0, 128, 64))
	for y := 0; y < 64; y++ {
		z := math.Cos((float64(y) + .5) / 64 * math.Pi)
		end := zenith
		if z < 0 {
			end = ground
		}
		k := float32(math.Abs(z))
		c := color.NRGBA{R: encode8(srgbDecode[horizon.R]*(1-k) + srgbDecode[end.R]*k), G: encode8(srgbDecode[horizon.G]*(1-k) + srgbDecode[end.G]*k), B: encode8(srgbDecode[horizon.B]*(1-k) + srgbDecode[end.B]*k), A: 255}
		for x := 0; x < 128; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	e, _ := NewEnvironment(img)
	return e
}

// SetEnvironment selects a shared sky and diffuse light; nil restores the
// solid background. Existing frames retain their environment.
func (s *Scene) SetEnvironment(e *Environment) {
	if s.environment != e {
		s.environment = e
		s.changed(false, false)
	}
}

func (e *Environment) sample(d nt3dmath.Vec3) [4]float32 {
	u := float32(math.Atan2(float64(d.Y), float64(d.X))/(2*math.Pi) + .5)
	v := float32(math.Acos(float64(max(-1, min(1, d.Z)))) / math.Pi)
	half := .5 / float32(e.texture.height)
	return sampleLevel(e.panorama, UV{u, max(half, min(1-half, v))}, true)
}

// The sky uses the camera's orientation with a fixed 45 degree vertical field
// of view, including orthographic views. Translation and zoom leave it fixed.
func (f Frame) skyBasis() (right, up, forward nt3dmath.Vec3) {
	forward = f.Forward.Normalize()
	if forward == (nt3dmath.Vec3{}) {
		forward = nt3dmath.Vec3{Y: 1}
	}
	right = forward.Cross(nt3dmath.Vec3{Z: 1}).Normalize()
	if right == (nt3dmath.Vec3{}) {
		right = nt3dmath.Vec3{X: 1}
	}
	up = right.Cross(forward)
	return right.Scale(float32(math.Tan(halfFOV)) * float32(f.Width) / float32(f.Height)), up.Scale(float32(math.Tan(halfFOV))), forward
}

func (f Frame) skyPixel(x, y int) color.RGBA {
	r, u, v := f.skyBasis()
	d := v.Add(r.Scale(2*(float32(x)+.5)/float32(f.Width) - 1)).Add(u.Scale(1 - 2*(float32(y)+.5)/float32(f.Height))).Normalize()
	return f.Environment.pixel(d)
}

func (e *Environment) pixel(d nt3dmath.Vec3) color.RGBA {
	c := e.sample(d)
	return color.RGBA{encode8(c[0]), encode8(c[1]), encode8(c[2]), 255}
}
