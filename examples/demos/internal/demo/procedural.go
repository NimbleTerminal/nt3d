// Copyright (c) 2026 Neomantra Corp.

package demo

import (
	"image"
	"image/color"
	"math"

	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
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

const proceduralWaveWGSL = `
fn nt3dDisplace(in: NT3DVertexInput) -> NT3DVertexOutput {
 let p0 = nt3dDisplaceParam(0u);
 let amp0 = p0.x;
 let amp1 = p0.y;
 let freq0 = p0.z;
 let freq1 = p0.w;
 let ph0 = in.position.x * freq0 + in.position.y * 3.2 + in.time * 2.4;
 let ph1 = in.position.x * 2.4 - in.position.y * freq1 - in.time * 1.8;
 let dz = amp0 * sin(ph0) + amp1 * cos(ph1);
 let dzdx = amp0 * freq0 * cos(ph0) - amp1 * 2.4 * sin(ph1);
 let dzdy = amp0 * 3.2 * cos(ph0) + amp1 * freq1 * sin(ph1);
 var n = in.normal;
 if dot(n, n) > 0.0 {
  n = normalize(n + vec3<f32>(-dzdx, -dzdy, 0.0));
 }
 return NT3DVertexOutput(in.position + vec3<f32>(0.0, 0.0, dz), n);
}
`

func proceduralWaveFallback(in nt3d.VertexDisplaceInput) nt3d.VertexDisplaceOutput {
	p0 := in.Params[0]
	amp0, amp1, freq0, freq1 := p0[0], p0[1], p0[2], p0[3]
	ph0 := float64(in.Position.X*freq0 + in.Position.Y*3.2 + in.Time*2.4)
	ph1 := float64(in.Position.X*2.4 - in.Position.Y*freq1 - in.Time*1.8)
	dz := amp0*float32(math.Sin(ph0)) + amp1*float32(math.Cos(ph1))
	dzdx := amp0*freq0*float32(math.Cos(ph0)) - amp1*2.4*float32(math.Sin(ph1))
	dzdy := amp0*3.2*float32(math.Cos(ph0)) + amp1*freq1*float32(math.Sin(ph1))
	n := in.Normal
	if n.Dot(n) > 0 {
		n = n.Add(nt3dmath.Vec3{X: -dzdx, Y: -dzdy}).Normalize()
	}
	return nt3d.VertexDisplaceOutput{
		Position: in.Position.Add(nt3dmath.Vec3{Z: dz}),
		Normal:   n,
	}
}

// proceduralWaveDisplacement builds or updates an immutable VertexDisplacement
// via WithParameters so WebGPU animates vertices in the vertex shader without
// re-uploading mesh buffers, while Software, Wireframe, shadows, and Pick use
// the matching Go fallback.
func proceduralWaveDisplacement(prev *nt3d.VertexDisplacement, t float32, style int) (*nt3d.VertexDisplacement, error) {
	params := [][4]float32{{0.12, 0.06, 5.2, 4.6}}
	if style == 1 {
		params[0] = [4]float32{0.15, 0.08, 6.4, 5.4}
	}
	const maxDisp = 0.25
	if prev == nil {
		return nt3d.NewVertexDisplacementWithParameters(proceduralWaveWGSL, maxDisp, t, params, proceduralWaveFallback)
	}
	return prev.WithParameters(t, params)
}

const proceduralMaterialWGSL = `
fn nt3dMaterial(in: NT3DMaterialInput) -> vec4<f32> {
 let p0 = nt3dMaterialParam(0u);
 let contour = 0.5 + 0.5 * sin(in.world.z * p0.x - in.time * p0.y);
 var ndv = 1.0;
 if dot(in.normal, in.normal) > 0.0 && dot(in.view, in.view) > 0.0 {
  ndv = clamp(dot(normalize(in.normal), normalize(in.view)), 0.0, 1.0);
 }
 let rim = (1.0 - ndv) * (1.0 - ndv);
 let crest = clamp((in.world.z + 0.15) * 1.4, 0.0, 1.0) * contour;
 let accent = vec3<f32>(p0.z, 0.92, p0.w);
 let rgb = mix(in.color.rgb * 0.88, accent, clamp(crest * 0.45 + rim * 0.35, 0.0, 1.0));
 return vec4<f32>(rgb, in.color.a);
}
`

func proceduralMaterialFallback(in nt3d.MaterialColorInput) [4]float32 {
	p0 := in.Params[0]
	contour := float32(0.5 + 0.5*math.Sin(float64(in.World.Z*p0[0]-in.Time*p0[1])))
	ndv := float32(1)
	if in.Normal.Dot(in.Normal) > 0 && in.View.Dot(in.View) > 0 {
		ndv = min(float32(1), max(float32(0), in.Normal.Normalize().Dot(in.View.Normalize())))
	}
	oneMinus := 1 - ndv
	rim := oneMinus * oneMinus
	crest := min(float32(1), max(float32(0), (in.World.Z+0.15)*1.4)) * contour
	w := min(float32(1), max(float32(0), crest*0.45+rim*0.35))
	accent := [3]float32{p0[2], 0.92, p0[3]}
	var out [4]float32
	for k := range 3 {
		lo := in.Color[k] * 0.88
		out[k] = lo + (accent[k]-lo)*w
	}
	out[3] = in.Color[3]
	return out
}

// proceduralMaterialShader builds or updates an immutable MaterialShader in
// linear light via WithParameters.
func proceduralMaterialShader(prev *nt3d.MaterialShader, t float32, style int) (*nt3d.MaterialShader, error) {
	params := [][4]float32{{14.0, 3.0, 0.35, 1.05}}
	if style == 1 {
		params[0] = [4]float32{18.0, 3.6, 1.05, 0.55}
	}
	if prev == nil {
		return nt3d.NewMaterialShaderWithParameters(proceduralMaterialWGSL, t, params, proceduralMaterialFallback)
	}
	return prev.WithParameters(t, params)
}
