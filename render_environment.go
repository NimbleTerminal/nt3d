// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"image"
)

func (g *gpuRenderer) prepareEnvironment(dev *wgpu.Device, f Frame) error {
	if g.reflection.texture == nil || g.reflection.source != f.Environment {
		if err := g.updateReflection(dev, f.Environment); err != nil {
			return err
		}
	}
	if g.environmentSource != f.Environment {
		if g.environmentTexture != nil {
			g.environmentTexture.release()
			g.environmentTexture = nil
		}
		g.environmentSource = nil
		if f.Environment != nil {
			t, err := g.uploadTextureLevels(dev, []*image.NRGBA{f.Environment.panorama}, true, gputypes.AddressModeRepeat, gputypes.AddressModeClampToEdge, 1)
			if err != nil {
				return err
			}
			g.environmentTexture = t
			g.environmentSource = f.Environment
		}
	}
	if f.Environment == nil || g.environmentPipeline != nil {
		return nil
	}
	uniform, err := dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}}}})
	if err != nil {
		return err
	}
	defer uniform.Release()
	layout, err := dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{uniform, g.shadowLayout, g.sampledLayout}})
	if err != nil {
		return err
	}
	defer layout.Release()
	bind, err := dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: uniform, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: g.uniform, Size: uniformSize}}})
	if err != nil {
		return err
	}
	pipeline, err := dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: "environment", Layout: layout, Vertex: wgpu.VertexState{Module: g.shader, EntryPoint: "vs_environment"}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone}, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: false, DepthCompare: gputypes.CompareFunctionAlways}, Multisample: gputypes.DefaultMultisampleState(), Fragment: &wgpu.FragmentState{Module: g.shader, EntryPoint: "fs_environment", Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}}})
	if err != nil {
		bind.Release()
		return err
	}
	g.environmentBind, g.environmentPipeline = bind, pipeline
	return nil
}
