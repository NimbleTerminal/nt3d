// Copyright (c) 2026 Neomantra Corp.
package terraindemo

import (
	"image"
	"image/color"
	"math"

	"nimbleterminal.dev/nt3d"
)

// The fallback evaluates the same pattern once on the CPU. Only software
// rendering uses those pixels; WebGPU generates the texture with this shader.
func plasmaTexture(phase float64) (*nt3d.Texture, error) {
	const size = 256
	const src = `
fn nt3dTexture(uv: vec2<f32>) -> vec4<f32> {
 let phase = nt3dTextureParams.x;
 let p = uv * 2.0 - vec2<f32>(1.0);
 let wave = sin(p.x * 9.0 + phase) + sin(p.y * 7.0 - phase) + sin(length(p) * 13.0);
 let rgb = vec3<f32>(0.5) + 0.5 * cos(vec3<f32>(wave) + vec3<f32>(0.0, 2.1, 4.2));
 return vec4<f32>(rgb, 1.0);
}`
	fallback := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			px, py := 2*(float64(x)+.5)/size-1, 2*(float64(y)+.5)/size-1
			wave := math.Sin(px*9+phase) + math.Sin(py*7-phase) + math.Sin(math.Hypot(px, py)*13)
			channel := func(offset float64) uint8 { return uint8((.5+.5*math.Cos(wave+offset))*255 + .5) }
			fallback.SetRGBA(x, y, color.RGBA{channel(0), channel(2.1), channel(4.2), 255})
		}
	}
	return nt3d.NewShaderTextureWithParameters(src, [4]float32{float32(phase)}, fallback)
}
