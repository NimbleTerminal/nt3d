// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

type gpuEnvironmentTexture struct {
	texture *wgpu.Texture
	view    *wgpu.TextureView
	sampler *wgpu.Sampler
	shared  *gpuShared
	key     *Environment
	refs    int
}

func (t *gpuEnvironmentTexture) release() {
	if t == nil {
		return
	}
	if t.shared != nil {
		t.shared.mu.Lock()
		t.refs--
		if t.refs > 0 {
			t.shared.mu.Unlock()
			return
		}
		delete(t.shared.environments, t.key)
		t.shared.mu.Unlock()
	}
	if t.view != nil {
		t.view.Release()
	}
	if t.texture != nil {
		t.texture.Release()
	}
	if t.sampler != nil {
		t.sampler.Release()
	}
	*t = gpuEnvironmentTexture{}
}

func (g *gpuRenderer) acquireEnvironmentTexture(dev *wgpu.Device, e *Environment) (*gpuEnvironmentTexture, error) {
	if g.shared != nil {
		g.shared.mu.Lock()
		if t := g.shared.environments[e]; t != nil {
			t.refs++
			g.shared.mu.Unlock()
			return t, nil
		}
		g.shared.mu.Unlock()
	}
	levels := e.specularLevels()
	w, h := e.texture.width, e.texture.height
	size := wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1}
	tex, err := dev.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "environment panorama",
		Size:          size,
		MipLevelCount: uint32(len(levels)),
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        gputypes.TextureFormatRGBA8UnormSrgb,
		Usage:         gputypes.TextureUsageTextureBinding | gputypes.TextureUsageCopyDst,
	})
	if err != nil {
		return nil, err
	}
	view, err := dev.CreateTextureView(tex, nil)
	if err != nil {
		tex.Release()
		return nil, err
	}
	sampler, err := dev.CreateSampler(&wgpu.SamplerDescriptor{
		AddressModeU: gputypes.AddressModeRepeat,
		AddressModeV: gputypes.AddressModeClampToEdge,
		MagFilter:    gputypes.FilterModeLinear,
		MinFilter:    gputypes.FilterModeLinear,
		MipmapFilter: gputypes.FilterModeLinear,
		Anisotropy:   1,
	})
	if err != nil {
		view.Release()
		tex.Release()
		return nil, err
	}
	for m, level := range levels {
		lw, lh := level.Rect.Dx(), level.Rect.Dy()
		lsize := wgpu.Extent3D{Width: uint32(lw), Height: uint32(lh), DepthOrArrayLayers: 1}
		if err := dev.Queue().WriteTexture(
			&wgpu.ImageCopyTexture{Texture: tex, MipLevel: uint32(m)},
			level.Pix,
			&wgpu.ImageDataLayout{BytesPerRow: uint32(lw * 4), RowsPerImage: uint32(lh)},
			&lsize,
		); err != nil {
			sampler.Release()
			view.Release()
			tex.Release()
			return nil, err
		}
	}
	t := &gpuEnvironmentTexture{texture: tex, view: view, sampler: sampler}
	if g.shared != nil {
		g.shared.mu.Lock()
		t.shared = g.shared
		t.key = e
		t.refs = 1
		g.shared.environments[e] = t
		g.shared.mu.Unlock()
	}
	return t, nil
}

func (g *gpuRenderer) prepareEnvironment(dev *wgpu.Device, f Frame) error {
	if g.environmentSource != f.Environment {
		var next *gpuEnvironmentTexture
		if f.Environment != nil {
			var err error
			next, err = g.acquireEnvironmentTexture(dev, f.Environment)
			if err != nil {
				return err
			}
		}
		old := g.environmentTexture
		g.environmentTexture = next
		g.environmentSource = f.Environment
		if err := g.updateReflection(dev, f.Environment); err != nil {
			if old != nil {
				old.release()
			}
			return err
		}
		if old != nil {
			old.release()
		}
	} else if g.reflection.texture == nil || g.reflection.source != f.Environment {
		if err := g.updateReflection(dev, f.Environment); err != nil {
			return err
		}
	}
	if f.Environment == nil || (g.environmentPipeline != nil && g.environmentBind != nil) {
		return nil
	}
	idx := g.sampleIdx
	ms := g.multisample()
	if g.shared != nil {
		g.shared.mu.Lock()
		if g.shared.environmentUniform == nil {
			uniform, err := dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}}}})
			if err != nil {
				g.shared.mu.Unlock()
				return err
			}
			g.shared.environmentUniform = uniform
		}
		if g.shared.msaaSets[idx].environmentPipeline == nil {
			layout, err := dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{g.shared.environmentUniform, g.shadowLayout}})
			if err != nil {
				g.shared.mu.Unlock()
				return err
			}
			pipeline, err := dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: "environment", Layout: layout, Vertex: wgpu.VertexState{Module: g.shader, EntryPoint: "vs_environment"}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone}, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: false, DepthCompare: gputypes.CompareFunctionAlways}, Multisample: ms, Fragment: &wgpu.FragmentState{Module: g.shader, EntryPoint: "fs_environment", Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}}})
			layout.Release()
			if err != nil {
				g.shared.mu.Unlock()
				return err
			}
			g.shared.msaaSets[idx].environmentPipeline = pipeline
		}
		uniformLayout := g.shared.environmentUniform
		g.environmentUniform = uniformLayout
		g.environmentPipeline = g.shared.msaaSets[idx].environmentPipeline
		g.msaaSets[idx].environmentPipeline = g.environmentPipeline
		g.shared.mu.Unlock()
		if g.environmentBind == nil {
			bind, err := dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: uniformLayout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: g.uniform, Size: uniformSize}}})
			if err != nil {
				return err
			}
			g.environmentBind = bind
		}
		return nil
	}
	if g.environmentUniform == nil {
		uniform, err := dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}}}})
		if err != nil {
			return err
		}
		g.environmentUniform = uniform
	}
	if g.environmentPipeline == nil {
		layout, err := dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{g.environmentUniform, g.shadowLayout}})
		if err != nil {
			return err
		}
		pipeline, err := dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: "environment", Layout: layout, Vertex: wgpu.VertexState{Module: g.shader, EntryPoint: "vs_environment"}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone}, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: false, DepthCompare: gputypes.CompareFunctionAlways}, Multisample: ms, Fragment: &wgpu.FragmentState{Module: g.shader, EntryPoint: "fs_environment", Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}}})
		layout.Release()
		if err != nil {
			return err
		}
		g.environmentPipeline = pipeline
		g.msaaSets[idx].environmentPipeline = pipeline
	}
	if g.environmentBind == nil {
		bind, err := dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: g.environmentUniform, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: g.uniform, Size: uniformSize}}})
		if err != nil {
			return err
		}
		g.environmentBind = bind
	}
	return nil
}
