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

// The resolve pass averages each s × s block of the supersampled target into
// one pixel of the frame: a box filter, exact for integer factors.
const resolveShader = `
struct Resolve { factor: vec4<f32> };
@group(0) @binding(0) var<uniform> r: Resolve;
@group(1) @binding(0) var src: texture_2d<f32>;
@group(1) @binding(1) var srcSampler: sampler;
@vertex fn vs_resolve(@builtin(vertex_index) i: u32) -> @builtin(position) vec4<f32> {
 let p = vec2<f32>(f32((i << 1u) & 2u), f32(i & 2u));
 return vec4<f32>(p * 2.0 - 1.0, 0.0, 1.0);
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
 return vec4<f32>(sum.rgb / f32(s * s), 1.0);
}
`

// supersampler is the GPU renderer's enlarged target and resolve pass.
type supersampler struct {
	target   *gpuimage.Texture
	pipeline *wgpu.RenderPipeline
	layout   *wgpu.PipelineLayout
	group    *wgpu.BindGroupLayout
	uniform  *wgpu.Buffer
	bind     *wgpu.BindGroup
	shader   *wgpu.ShaderModule
}

// prepareSupersample sizes the enlarged target for this frame, outside GPU
// callbacks as ntgpuimage requires, or releases it when the frame needs none.
func (g *gpuRenderer) prepareSupersample(f Frame) error {
	s := supersampleFactor(f)
	if t := g.super.target; t != nil {
		if w, h := t.Size(); s == 1 || w != f.Width*s || h != f.Height*s {
			if err := t.Close(); err != nil {
				return err
			}
			g.super.target = nil
		}
	}
	if s == 1 || g.super.target != nil {
		return nil
	}
	var err error
	g.super.target, err = g.r.NewTexture(f.Width*s, f.Height*s, gpuimage.TextureOptions{RenderTarget: true, Depth: true})
	if err != nil {
		return err
	}
	if g.super.pipeline != nil {
		return nil
	}
	sampled := g.super.target.BindGroupLayout()
	return g.r.Do(func(dev *wgpu.Device) (err error) {
		p := &g.super
		if p.shader, err = dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "nt3d resolve", WGSL: resolveShader}); err != nil {
			return err
		}
		if p.group, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageFragment, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}}}}); err != nil {
			return err
		}
		if p.uniform, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: 16, Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst}); err != nil {
			return err
		}
		if p.bind, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: p.group, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: p.uniform, Size: 16}}}); err != nil {
			return err
		}
		if p.layout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{p.group, sampled}}); err != nil {
			return err
		}
		p.pipeline, err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: "nt3d resolve", Layout: p.layout,
			Vertex:      wgpu.VertexState{Module: p.shader, EntryPoint: "vs_resolve"},
			Primitive:   gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone},
			Multisample: gputypes.DefaultMultisampleState(),
			Fragment:    &wgpu.FragmentState{Module: p.shader, EntryPoint: "fs_resolve", Targets: []gputypes.ColorTargetState{{Format: gputypes.TextureFormatRGBA8Unorm, WriteMask: gputypes.ColorWriteMaskAll}}}})
		return err
	})
}

// renderSupersampled draws the scene into the enlarged target, then resolves
// it into the frame.
func (g *gpuRenderer) renderSupersampled(frame *gpuimage.Frame, s int, draw func(color, depth *wgpu.TextureView) error) error {
	if err := frame.RenderTo(g.super.target, func(big *gpuimage.Frame) error { return draw(big.Color, big.Depth) }); err != nil {
		return err
	}
	var factor [16]byte
	binary.LittleEndian.PutUint32(factor[:], math.Float32bits(float32(s)))
	if err := frame.Device.Queue().WriteBuffer(g.super.uniform, 0, factor[:]); err != nil {
		return err
	}
	pass, err := frame.Encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: frame.Color, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore}}})
	if err != nil {
		return err
	}
	pass.SetPipeline(g.super.pipeline)
	pass.SetBindGroup(0, g.super.bind, nil)
	pass.SetBindGroup(1, g.super.target.BindGroup(), nil)
	pass.Draw(gputypes.DrawArgs{VertexCount: 3, InstanceCount: 1})
	return pass.End()
}

// releaseSupersample frees the target and, on the GPU's executor, the
// resolve pass's objects.
func (g *gpuRenderer) releaseSupersample() {
	p := g.super
	g.super = supersampler{}
	if p.target != nil {
		_ = p.target.Close()
	}
	_ = g.r.Do(func(*wgpu.Device) error {
		if p.pipeline != nil {
			p.pipeline.Release()
			p.layout.Release()
			p.bind.Release()
			p.uniform.Release()
			p.group.Release()
			p.shader.Release()
		}
		return nil
	})
}

// shrink averages each s × s block of a software rendering into one pixel.
func shrink(big *image.RGBA, s int) *image.RGBA {
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
