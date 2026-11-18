// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

type sceneSnapshot struct {
	source                *wgpu.TextureView
	depthSource           *wgpu.TextureView
	w, h, samples         int
	color, depth          *wgpu.Texture
	colorView, depthView  *wgpu.TextureView
	captureBind           *wgpu.BindGroup
	capturePipeline       *wgpu.RenderPipeline
	captureLayout         *wgpu.BindGroupLayout
	capturePipelineLayout *wgpu.PipelineLayout
	captureShader         *wgpu.ShaderModule
}

func (s *sceneSnapshot) release() {
	if s.captureBind != nil {
		s.captureBind.Release()
	}
	if s.capturePipeline != nil {
		s.capturePipeline.Release()
	}
	if s.capturePipelineLayout != nil {
		s.capturePipelineLayout.Release()
	}
	if s.captureLayout != nil {
		s.captureLayout.Release()
	}
	if s.captureShader != nil {
		s.captureShader.Release()
	}
	for _, v := range []*wgpu.TextureView{s.colorView, s.depthView} {
		if v != nil {
			v.Release()
		}
	}
	for _, t := range []*wgpu.Texture{s.color, s.depth} {
		if t != nil {
			t.Release()
		}
	}
	*s = sceneSnapshot{}
}
func (g *gpuRenderer) prepareSnapshot(dev *wgpu.Device, w, h int) (err error) {
	samples := max(1, g.target.samples)
	if g.screen.w == w && g.screen.h == h && g.screen.source == g.target.colorView && g.screen.depthSource == g.target.depthView && g.screen.samples == samples {
		return nil
	}
	s := sceneSnapshot{w: w, h: h, samples: samples, source: g.target.colorView, depthSource: g.target.depthView}
	defer func() {
		if err != nil {
			s.release()
		}
	}()
	for i, format := range []gputypes.TextureFormat{sceneFormat, gputypes.TextureFormatDepth32Float} {
		t, e := dev.CreateTexture(&wgpu.TextureDescriptor{Label: "scene snapshot", Size: wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: format, Usage: wgpu.TextureUsageTextureBinding | wgpu.TextureUsageRenderAttachment})
		if e != nil {
			return e
		}
		if i == 0 {
			s.color = t
		} else {
			s.depth = t
		}
		v, e := dev.CreateTextureView(t, nil)
		if e != nil {
			return e
		}
		if i == 0 {
			s.colorView = v
		} else {
			s.depthView = v
		}
	}
	if g.target.colorView != nil {
		if err = s.setupCapture(dev, g.target.colorView, g.target.depthView, samples > 1); err != nil {
			return err
		}
	}
	old := g.screen
	g.screen = s
	targets := []*shadowTarget{&g.noShadow, &g.shadowTarget}
	var binds [2]*wgpu.BindGroup
	for i, t := range targets {
		if t.view == nil {
			continue
		}
		binds[i], err = g.sceneLightingBind(dev, t.view, &g.reflection)
		if err != nil {
			g.screen = old
			for _, b := range binds {
				if b != nil {
					b.Release()
				}
			}
			return err
		}
	}
	for i, t := range targets {
		if binds[i] != nil {
			if t.bind != nil {
				t.bind.Release()
			}
			t.bind = binds[i]
		}
	}
	old.release()
	return nil
}

// A color attachment keeps depth capture compatible with native backends that
// require color targets when a fragment shader writes depth. The second capture
// refreshes both textures; only depth is consumed by the remaining particles.
func (g *gpuRenderer) captureScene(e *wgpu.CommandEncoder) error {
	s := &g.screen
	p, err := e.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: s.colorView, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore}}, DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{View: s.depthView, DepthLoadOp: gputypes.LoadOpClear, DepthStoreOp: gputypes.StoreOpStore, DepthClearValue: 1, StencilReadOnly: true}})
	if err != nil {
		return err
	}
	p.SetPipeline(s.capturePipeline)
	p.SetBindGroup(0, s.captureBind, nil)
	p.Draw(gputypes.DrawArgs{VertexCount: 3, InstanceCount: 1})
	return p.End()
}
func (g *gpuRenderer) resumeScene(e *wgpu.CommandEncoder, color, depth *wgpu.TextureView) (*wgpu.RenderPassEncoder, error) {
	att := wgpu.RenderPassColorAttachment{View: color, LoadOp: gputypes.LoadOpLoad, StoreOp: gputypes.StoreOpStore}
	if g.target.samples > 1 {
		att.View = g.target.msaaColorView
		att.ResolveTarget = color
	}
	return e.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{att}, DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{View: depth, DepthLoadOp: gputypes.LoadOpLoad, DepthStoreOp: gputypes.StoreOpStore, StencilReadOnly: true}})
}

const captureShader = `
@group(0) @binding(0) var color: texture_2d<f32>;
@group(0) @binding(1) var depth: texture_depth_2d;
@vertex fn vs(@builtin(vertex_index) i:u32)->@builtin(position) vec4<f32> {
 let x=f32((i<<1u)&2u);let y=f32(i&2u);return vec4<f32>(x*2.0-1.0,y*2.0-1.0,0.0,1.0);
}
struct Sample { @location(0) color:vec4<f32>, @builtin(frag_depth) depth:f32 };
@fragment fn both(@builtin(position) p:vec4<f32>)->Sample {
 return Sample(textureLoad(color,vec2<i32>(p.xy),0),textureLoad(depth,vec2<i32>(p.xy),0));
}
`

const captureShaderMSAA = `
@group(0) @binding(0) var color: texture_2d<f32>;
@group(0) @binding(1) var depth: texture_depth_multisampled_2d;
@vertex fn vs(@builtin(vertex_index) i:u32)->@builtin(position) vec4<f32> {
 let x=f32((i<<1u)&2u);let y=f32(i&2u);return vec4<f32>(x*2.0-1.0,y*2.0-1.0,0.0,1.0);
}
struct Sample { @location(0) color:vec4<f32>, @builtin(frag_depth) depth:f32 };
@fragment fn both(@builtin(position) p:vec4<f32>)->Sample {
 let c=vec2<i32>(p.xy);
 let d0=textureLoad(depth,c,0);
 let d1=textureLoad(depth,c,1);
 let d2=textureLoad(depth,c,2);
 let d3=textureLoad(depth,c,3);
 return Sample(textureLoad(color,c,0),min(min(d0,d1),min(d2,d3)));
}
`

func (s *sceneSnapshot) setupCapture(dev *wgpu.Device, color, depth *wgpu.TextureView, msaa bool) (err error) {
	wgsl := captureShader
	if msaa {
		wgsl = captureShaderMSAA
	}
	s.captureShader, err = dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "scene capture", WGSL: wgsl})
	if err != nil {
		return err
	}
	s.captureLayout, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []gputypes.BindGroupLayoutEntry{
		{Binding: 0, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeUnfilterableFloat, ViewDimension: gputypes.TextureViewDimension2D}},
		{Binding: 1, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeDepth, ViewDimension: gputypes.TextureViewDimension2D, Multisampled: msaa}},
	}})
	if err != nil {
		return err
	}
	s.captureBind, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: s.captureLayout, Entries: []wgpu.BindGroupEntry{{Binding: 0, TextureView: color}, {Binding: 1, TextureView: depth}}})
	if err != nil {
		return err
	}
	s.capturePipelineLayout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{s.captureLayout}})
	if err != nil {
		return err
	}

	s.capturePipeline, err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: "scene capture", Layout: s.capturePipelineLayout, Vertex: wgpu.VertexState{Module: s.captureShader, EntryPoint: "vs"}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone}, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionAlways}, Multisample: gputypes.DefaultMultisampleState(), Fragment: &wgpu.FragmentState{Module: s.captureShader, EntryPoint: "both", Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}}})
	if err != nil {
		return err
	}
	return nil
}
