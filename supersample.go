// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"encoding/binary"
	"image"
	"math"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	gpuimage "nimbleterminal.dev/ntgpuimage"
)

// MaxSupersample is the largest Frame.Supersample.
const MaxSupersample = 4

// maxTextureSide is the largest texture side every WebGPU device supports.
const maxTextureSide = 8192

// supersampleFactor is the factor a frame renders at: Supersample, lowered
// until the enlarged frame fits the texture size every device supports.
func supersampleFactor(f Frame) int {
	s := max(1, f.Supersample)
	for s > 1 && (f.Width*s > maxTextureSide || f.Height*s > maxTextureSide) {
		s--
	}
	return s
}

// The GPU renderer draws a frame's scene into its own target, in 16-bit
// floats, at the frame's supersampling factor, so blending works in the
// shaders' color space: linear light when the frame asks for it. A resolve
// pass then averages each s × s block into one pixel of the frame, a box
// filter exact for integer factors, encoding linear light as sRGB.
const resolveShader = `
struct Resolve { factor: vec4<f32> };
@group(0) @binding(0) var<uniform> r: Resolve;
@group(1) @binding(0) var src: texture_2d<f32>;
@vertex fn vs_resolve(@builtin(vertex_index) i: u32) -> @builtin(position) vec4<f32> {
 let p = vec2<f32>(f32((i << 1u) & 2u), f32(i & 2u));
 return vec4<f32>(p * 2.0 - 1.0, 0.0, 1.0);
}
fn encode(c: vec3<f32>) -> vec3<f32> {
 let v = clamp(c, vec3<f32>(0.0), vec3<f32>(1.0));
 return select(1.055 * pow(v, vec3<f32>(1.0 / 2.4)) - 0.055, v * 12.92, v <= vec3<f32>(0.0031308));
}
@fragment fn fs_resolve(@builtin(position) p: vec4<f32>) -> @location(0) vec4<f32> {
 let s = i32(r.factor.x);
 let base = vec2<i32>(floor(p.xy)) * s;
 var sum = vec4<f32>(0.0);
 for (var y = 0; y < s; y++) {
  for (var x = 0; x < s; x++) {
   sum += textureLoad(src, base + vec2<i32>(x, y), 0);
  }
 }
 var rgb = sum.rgb / f32(s * s);
 if r.factor.y > 0.5 { rgb = encode(rgb); }
 return vec4<f32>(rgb, 1.0);
}
`

// sceneFormat is the scene target's format: floats, which blend in whatever
// color space the shaders write.
const sceneFormat = gputypes.TextureFormatRGBA16Float

// sceneTarget is the GPU renderer's scene target and resolve pass.
type sceneTarget struct {
	w, h                 int
	color, depth         *wgpu.Texture
	colorView, depthView *wgpu.TextureView
	sampled              *wgpu.BindGroup // colorView, for the resolve.
	pipeline             *wgpu.RenderPipeline
	layout               *wgpu.PipelineLayout
	group, sampledGroup  *wgpu.BindGroupLayout
	uniform              *wgpu.Buffer
	bind                 *wgpu.BindGroup
	shader               *wgpu.ShaderModule
}

// releaseTextures frees the target's textures, on the GPU's executor.
func (t *sceneTarget) releaseTextures() {
	if t.sampled != nil {
		t.sampled.Release()
	}
	for _, v := range []*wgpu.TextureView{t.colorView, t.depthView} {
		if v != nil {
			v.Release()
		}
	}
	for _, x := range []*wgpu.Texture{t.color, t.depth} {
		if x != nil {
			x.Release()
		}
	}
	t.w, t.h, t.color, t.depth, t.colorView, t.depthView, t.sampled = 0, 0, nil, nil, nil, nil, nil
}

// prepareTarget sizes the scene target for this frame, creating the resolve
// pass the first time, outside GPU callbacks as ntgpuimage requires.
func (g *gpuRenderer) prepareTarget(f Frame) error {
	s := supersampleFactor(f)
	w, h := f.Width*s, f.Height*s
	t := &g.target
	if t.pipeline != nil && t.w == w && t.h == h {
		return nil
	}
	return g.r.Do(func(dev *wgpu.Device) (err error) {
		if t.pipeline == nil {
			if err = t.setup(dev); err != nil {
				return err
			}
		}
		t.releaseTextures()
		texture := func(label string, format gputypes.TextureFormat, usage gputypes.TextureUsage) (*wgpu.Texture, *wgpu.TextureView, error) {
			x, err := dev.CreateTexture(&wgpu.TextureDescriptor{Label: label, Size: wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: format, Usage: usage})
			if err != nil {
				return nil, nil, err
			}
			v, err := dev.CreateTextureView(x, nil)
			if err != nil {
				x.Release()
				return nil, nil, err
			}
			return x, v, nil
		}
		if t.color, t.colorView, err = texture("nt3d scene", sceneFormat, wgpu.TextureUsageRenderAttachment|wgpu.TextureUsageTextureBinding); err != nil {
			return err
		}
		if t.depth, t.depthView, err = texture("nt3d scene depth", gputypes.TextureFormatDepth32Float, wgpu.TextureUsageRenderAttachment|wgpu.TextureUsageTextureBinding); err != nil {
			t.releaseTextures()
			return err
		}
		if t.sampled, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: t.sampledGroup, Entries: []wgpu.BindGroupEntry{{Binding: 0, TextureView: t.colorView}}}); err != nil {
			t.releaseTextures()
			return err
		}
		t.w, t.h = w, h
		return nil
	})
}

// setup creates the resolve pass.
func (t *sceneTarget) setup(dev *wgpu.Device) (err error) {
	if t.shader, err = dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "nt3d resolve", WGSL: resolveShader}); err != nil {
		return err
	}
	if t.group, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageFragment, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}}}}); err != nil {
		return err
	}
	if t.sampledGroup, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeUnfilterableFloat, ViewDimension: gputypes.TextureViewDimension2D}}}}); err != nil {
		return err
	}
	if t.uniform, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: 16, Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst}); err != nil {
		return err
	}
	if t.bind, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: t.group, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: t.uniform, Size: 16}}}); err != nil {
		return err
	}
	if t.layout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{t.group, t.sampledGroup}}); err != nil {
		return err
	}
	t.pipeline, err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: "nt3d resolve", Layout: t.layout,
		Vertex:      wgpu.VertexState{Module: t.shader, EntryPoint: "vs_resolve"},
		Primitive:   gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone},
		Multisample: gputypes.DefaultMultisampleState(),
		Fragment:    &wgpu.FragmentState{Module: t.shader, EntryPoint: "fs_resolve", Targets: []gputypes.ColorTargetState{{Format: gputypes.TextureFormatRGBA8Unorm, WriteMask: gputypes.ColorWriteMaskAll}}}})
	return err
}

// renderScene draws the scene into the scene target, then resolves it into
// the frame.
func (g *gpuRenderer) renderScene(frame *gpuimage.Frame, f Frame, draw func(color, depth *wgpu.TextureView) error) error {
	t := &g.target
	if err := draw(t.colorView, t.depthView); err != nil {
		return err
	}
	linear := float32(0)
	if f.Linear {
		linear = 1
	}
	var factor [16]byte
	binary.LittleEndian.PutUint32(factor[:], math.Float32bits(float32(supersampleFactor(f))))
	binary.LittleEndian.PutUint32(factor[4:], math.Float32bits(linear))
	if err := frame.Device.Queue().WriteBuffer(t.uniform, 0, factor[:]); err != nil {
		return err
	}
	pass, err := frame.Encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: frame.Color, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore}}})
	if err != nil {
		return err
	}
	pass.SetPipeline(t.pipeline)
	pass.SetBindGroup(0, t.bind, nil)
	pass.SetBindGroup(1, t.sampled, nil)
	pass.Draw(gputypes.DrawArgs{VertexCount: 3, InstanceCount: 1})
	return pass.End()
}

// releaseTarget frees the scene target and resolve pass, on the GPU's executor.
func (g *gpuRenderer) releaseTarget() {
	t := g.target
	g.target = sceneTarget{}
	_ = g.r.Do(func(*wgpu.Device) error {
		t.releaseTextures()
		if t.pipeline != nil {
			t.pipeline.Release()
		}
		if t.layout != nil {
			t.layout.Release()
		}
		if t.bind != nil {
			t.bind.Release()
		}
		if t.uniform != nil {
			t.uniform.Release()
		}
		for _, l := range []*wgpu.BindGroupLayout{t.group, t.sampledGroup} {
			if l != nil {
				l.Release()
			}
		}
		if t.shader != nil {
			t.shader.Release()
		}
		return nil
	})
}

// shrink averages each s × s block of a software rendering into one pixel,
// in linear light if linear.
func shrink(big *image.RGBA, s int, linear bool) *image.RGBA {
	if linear {
		return shrinkLinear(big, s)
	}
	w, h := big.Rect.Dx()/s, big.Rect.Dy()/s
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	n := uint32(s * s)
	for y := range h {
		for x := range w {
			var sum [3]uint32
			for j := range s {
				row := big.Pix[(y*s+j)*big.Stride:]
				for i := range s {
					p := row[(x*s+i)*4:]
					sum[0], sum[1], sum[2] = sum[0]+uint32(p[0]), sum[1]+uint32(p[1]), sum[2]+uint32(p[2])
				}
			}
			o := out.Pix[y*out.Stride+x*4:]
			o[0], o[1], o[2], o[3] = uint8((sum[0]+n/2)/n), uint8((sum[1]+n/2)/n), uint8((sum[2]+n/2)/n), 255
		}
	}
	return out
}

// shrinkLinear averages each s × s block in linear light.
func shrinkLinear(big *image.RGBA, s int) *image.RGBA {
	w, h := big.Rect.Dx()/s, big.Rect.Dy()/s
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	n := float32(s * s)
	for y := range h {
		for x := range w {
			var sum [3]float32
			for j := range s {
				row := big.Pix[(y*s+j)*big.Stride:]
				for i := range s {
					p := row[(x*s+i)*4:]
					sum[0], sum[1], sum[2] = sum[0]+srgbDecode[p[0]], sum[1]+srgbDecode[p[1]], sum[2]+srgbDecode[p[2]]
				}
			}
			o := out.Pix[y*out.Stride+x*4:]
			o[0], o[1], o[2], o[3] = encode8(sum[0]/n), encode8(sum[1]/n), encode8(sum[2]/n), 255
		}
	}
	return out
}
