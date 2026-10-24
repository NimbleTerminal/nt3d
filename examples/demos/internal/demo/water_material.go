// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"image"
	"image/color"
	"math"
	"nimbleterminal.dev/nt3d"
	"sync"
)

// Integer UV frequencies tile exactly. Derivatives describe a height pattern
// on a 2.5-world-unit tile; the small waves only affect shading, not buoyancy.
const waterRippleShader = `
fn nt3dTexture(uv:vec2<f32>)->vec4<f32> {
 let t=nt3dTextureParams.x;
 let a=cos(6.28318530718*(4.0*uv.x+uv.y)-2.1*t)*0.03015928947;
 let b=cos(6.28318530718*(-2.0*uv.x+5.0*uv.y)+1.7*t)*0.02010619298;
 let c=cos(6.28318530718*(9.0*uv.x-3.0*uv.y)-3.2*t)*0.01005309649;
 let n=normalize(vec3<f32>(-(4.0*a-2.0*b+9.0*c),-(a+5.0*b-3.0*c),1.0));
 return vec4<f32>(n*0.5+vec3<f32>(0.5),1.0);
}`

func rippleVector(u, v, t float64) (x, y, z float64) {
	a := math.Cos(2*math.Pi*(4*u+v)-2.1*t) * (.012 * 2 * math.Pi / 2.5)
	b := math.Cos(2*math.Pi*(-2*u+5*v)+1.7*t) * (.008 * 2 * math.Pi / 2.5)
	c := math.Cos(2*math.Pi*(9*u-3*v)-3.2*t) * (.004 * 2 * math.Pi / 2.5)
	x, y, z = -(4*a - 2*b + 9*c), -(a + 5*b - 3*c), 1
	l := math.Sqrt(x*x + y*y + 1)
	return x / l, y / l, 1 / l
}

func waterRipples(time float64) (*nt3d.Texture, error) {
	const size = 128
	// Both implementations use the exact float32 uniform time.
	phase := float32(time)
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	encode := func(v float64) uint8 { return uint8(math.Round((v*.5 + .5) * 255)) }
	for y := range size {
		for x := range size {
			nx, ny, nz := rippleVector((float64(x)+.5)/size, (float64(y)+.5)/size, float64(phase))
			img.SetNRGBA(x, y, color.NRGBA{encode(nx), encode(ny), encode(nz), 255})
		}
	}
	return nt3d.NewShaderNormalMapWithParameters(waterRippleShader, [4]float32{phase}, img)
}

var waterEnvironment = sync.OnceValue(func() *nt3d.Environment {
	const w, h = 512, 256
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			longitude := (float64(x)+.5)/w*2*math.Pi - math.Pi
			latitude := (float64(y) + .5) / h * math.Pi
			z := math.Cos(latitude)
			dx, dy := math.Sin(latitude)*math.Cos(longitude), math.Sin(latitude)*math.Sin(longitude)
			horizon := math.Exp(-math.Abs(z) * 5)
			cloud := math.Max(0, math.Min(1, (math.Sin(10*dx+2*dy)*math.Sin(14*dy-3*z)+.5*math.Sin(21*dx+9*z)-.1)*1.8))
			cloud *= math.Max(0, math.Min(1, (z-.08)*5))
			r, g, b := 70+100*horizon, 135+65*horizon, 210+25*horizon
			if z < 0 {
				r, g, b = 45+55*horizon, 75+65*horizon, 95+70*horizon
				cloud = 0
			}
			r = r*(1-cloud) + 240*cloud
			g = g*(1-cloud) + 246*cloud
			b = b*(1-cloud) + 250*cloud
			img.SetNRGBA(x, y, color.NRGBA{uint8(r), uint8(g), uint8(b), 255})
		}
	}
	e, err := nt3d.NewEnvironment(img)
	if err != nil {
		panic(err)
	}
	return e
})
