// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

type gpuTexture struct {
	texture *wgpu.Texture
	view    *wgpu.TextureView
	bind    *wgpu.BindGroup
}

func (t *gpuTexture) release() {
	if t.bind != nil {
		t.bind.Release()
	}
	if t.view != nil {
		t.view.Release()
	}
	if t.texture != nil {
		t.texture.Release()
	}
}

type texturedBatch struct {
	gpuBatch
	geometry int
	texture  *Texture
}

func releaseBatch(b gpuBatch) {
	if b.bind != nil {
		b.bind.Release()
	}
	if b.buffer != nil {
		b.buffer.Release()
	}
}

func (g *gpuRenderer) setupTextures(dev *wgpu.Device) (err error) {
	g.textureBindLayout, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []gputypes.BindGroupLayoutEntry{
		{Binding: 0, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeFloat, ViewDimension: gputypes.TextureViewDimension2D}},
		{Binding: 1, Visibility: wgpu.ShaderStageFragment, Sampler: &gputypes.SamplerBindingLayout{Type: gputypes.SamplerBindingTypeFiltering}},
	}})
	if err != nil {
		return err
	}
	g.textureLayout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{g.bindLayout, g.textureBindLayout}})
	if err != nil {
		return err
	}
	g.texturePipeline, err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: "textured mesh", Layout: g.textureLayout, Vertex: wgpu.VertexState{Module: g.shader, EntryPoint: "vs_texture"}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeBack}, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth24Plus, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual}, Multisample: gputypes.DefaultMultisampleState(), Fragment: &wgpu.FragmentState{Module: g.shader, EntryPoint: "fs_texture", Targets: []gputypes.ColorTargetState{{Format: gputypes.TextureFormatRGBA8Unorm, WriteMask: gputypes.ColorWriteMaskAll}}}})
	if err != nil {
		return err
	}
	g.textureSampler, err = dev.CreateSampler(&wgpu.SamplerDescriptor{AddressModeU: gputypes.AddressModeClampToEdge, AddressModeV: gputypes.AddressModeClampToEdge, AddressModeW: gputypes.AddressModeClampToEdge, MagFilter: gputypes.FilterModeLinear, MinFilter: gputypes.FilterModeLinear, MipmapFilter: gputypes.FilterModeNearest, LodMaxClamp: 0, Anisotropy: 1})
	g.textures = make(map[*Texture]*gpuTexture)
	return err
}
func (g *gpuRenderer) uploadTexturedMeshes(dev *wgpu.Device, f Frame) error {
	for _, b := range g.texturedBatches {
		releaseBatch(b.gpuBatch)
	}
	g.texturedBatches = nil
	for i, geom := range f.Geometry {
		if !textured(geom) || len(geom.Indices) == 0 {
			continue
		}
		var data []byte
		for _, index := range geom.Indices {
			v, uv := geom.Vertices[index], geom.UV[index]
			nx, ny, nz := v.Normal.X, v.Normal.Y, v.Normal.Z
			if geom.Material.Unlit {
				nx, ny, nz = 0, 0, 0
			}
			data = append(data, floats(v.Position.X, v.Position.Y, v.Position.Z, uv.U, nx, ny, nz, uv.V, 1, 1, 1, float32(i+1))...)
		}
		b := texturedBatch{geometry: i}
		// Keep partially created resources reachable for Close on errors.
		g.texturedBatches = append(g.texturedBatches, b)
		target := &g.texturedBatches[len(g.texturedBatches)-1]
		var err error
		target.buffer, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: uint64(len(data)), Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst})
		if err != nil {
			return err
		}
		if err = dev.Queue().WriteBuffer(target.buffer, 0, data); err != nil {
			return err
		}
		target.bind, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: g.bindLayout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: g.uniform, Size: 112}, {Binding: 1, Buffer: target.buffer, Size: uint64(len(data))}, {Binding: 2, Buffer: g.transforms, Size: g.transformBytes}}})
		if err != nil {
			return err
		}
		target.count = uint32(len(geom.Indices))
	}
	return nil
}
func (g *gpuRenderer) syncTextures(dev *wgpu.Device, f Frame) error {
	used := make(map[*Texture]bool)
	for i := range g.texturedBatches {
		b := &g.texturedBatches[i]
		src := f.Geometry[b.geometry].Material.Texture
		b.texture = src
		if src.shader != "" {
			continue
		}
		used[src] = true
		if g.textures[src] != nil {
			continue
		}
		t := &gpuTexture{}
		var err error
		size := wgpu.Extent3D{Width: uint32(src.width), Height: uint32(src.height), DepthOrArrayLayers: 1}
		t.texture, err = dev.CreateTexture(&wgpu.TextureDescriptor{Size: size, MipLevelCount: 1, SampleCount: 1, Dimension: gputypes.TextureDimension2D, Format: gputypes.TextureFormatRGBA8Unorm, Usage: gputypes.TextureUsageTextureBinding | gputypes.TextureUsageCopyDst})
		if err != nil {
			return err
		}
		if err = dev.Queue().WriteTexture(&wgpu.ImageCopyTexture{Texture: t.texture}, src.pixels, &wgpu.ImageDataLayout{BytesPerRow: uint32(src.width * 4), RowsPerImage: uint32(src.height)}, &size); err != nil {
			t.release()
			return err
		}
		t.view, err = dev.CreateTextureView(t.texture, nil)
		if err != nil {
			t.release()
			return err
		}
		t.bind, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: g.textureBindLayout, Entries: []wgpu.BindGroupEntry{{Binding: 0, TextureView: t.view}, {Binding: 1, Sampler: g.textureSampler}}})
		if err != nil {
			t.release()
			return err
		}
		g.textures[src] = t
		g.textureUploads++
	}
	for src, t := range g.textures {
		if !used[src] {
			t.release()
			delete(g.textures, src)
		}
	}
	g.textureRevision = f.TextureRevision
	return nil
}
