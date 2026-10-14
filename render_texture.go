// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"image"
	"nimbleterminal.dev/ntgpuimage"
)

type texturedBatch struct {
	gpuBatch
	geometry int
}

func releaseBatch(b gpuBatch) {
	if b.bind != nil {
		b.bind.Release()
	}
	if b.buffer != nil {
		b.buffer.Release()
	}
}

func (g *gpuRenderer) setupTextures(dev *wgpu.Device, sampledLayout *wgpu.BindGroupLayout) (err error) {
	g.textureLayout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{g.bindLayout, sampledLayout}})
	if err != nil {
		return err
	}
	g.texturePipeline, err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: "textured mesh", Layout: g.textureLayout, Vertex: wgpu.VertexState{Module: g.shader, EntryPoint: "vs_texture"}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeBack}, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth24Plus, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual}, Multisample: gputypes.DefaultMultisampleState(), Fragment: &wgpu.FragmentState{Module: g.shader, EntryPoint: "fs_texture", Targets: []gputypes.ColorTargetState{{Format: gputypes.TextureFormatRGBA8Unorm, WriteMask: gputypes.ColorWriteMaskAll}}}})
	if err != nil {
		g.textureLayout.Release()
		g.textureLayout = nil
		return err
	}
	return nil
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

// prepareTextures runs outside GPU callbacks: ntgpuimage owns allocation,
// upload, sampling resources and cleanup on its executor thread.
func (g *gpuRenderer) prepareTextures(f Frame) error {
	if g.textures == nil {
		g.textures = make(map[*Texture]*ntgpuimage.Texture)
	}
	used := make(map[*Texture]bool)
	var sampledLayout *wgpu.BindGroupLayout
	for _, geom := range f.Geometry {
		if !textured(geom) || len(geom.Indices) == 0 {
			continue
		}
		src := geom.Material.Texture
		if src.shader != "" {
			sampledLayout = g.procedural[src].target.BindGroupLayout()
			continue
		}
		used[src] = true
		t := g.textures[src]
		if t == nil {
			// The immutable snapshot is already packed opaque RGBA. Expose a view
			// to ntgpuimage so its packer can use the standard image fast path.
			pixels := &image.NRGBA{Pix: src.pixels, Stride: src.width * 4, Rect: src.Bounds()}
			var err error
			t, err = g.r.UploadTexture(pixels, ntgpuimage.TextureOptions{})
			if err != nil {
				return err
			}
			g.textures[src] = t
			g.textureUploads++
		}
		sampledLayout = t.BindGroupLayout()
	}
	for src, t := range g.textures {
		if !used[src] {
			if err := t.Close(); err != nil {
				return err
			}
			delete(g.textures, src)
		}
	}
	if sampledLayout != nil && g.texturePipeline == nil {
		return g.r.Do(func(dev *wgpu.Device) error { return g.setupTextures(dev, sampledLayout) })
	}
	return nil
}
