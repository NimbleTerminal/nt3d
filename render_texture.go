// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"image"
	"nimbleterminal.dev/ntgpuimage"
)

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
	pipeline := func(label, fragment string) (*wgpu.RenderPipeline, error) {
		return dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: label, Layout: g.textureLayout, Vertex: wgpu.VertexState{Module: g.shader, EntryPoint: "vs_texture"}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeBack}, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth24Plus, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual}, Multisample: gputypes.DefaultMultisampleState(), Fragment: &wgpu.FragmentState{Module: g.shader, EntryPoint: fragment, Targets: []gputypes.ColorTargetState{{Format: gputypes.TextureFormatRGBA8Unorm, WriteMask: gputypes.ColorWriteMaskAll}}}})
	}
	g.texturePipeline, err = pipeline("textured mesh", "fs_texture")
	if err == nil {
		g.cutoutPipeline, err = pipeline("cutout mesh", "fs_cutout")
	}
	for i, entry := range []string{"vs_texture_back", "vs_texture"} {
		if err == nil {
			g.textureBlends[i], err = blendPipeline(dev, g.textureLayout, g.shader, entry, "fs_blend_texture", i)
		}
	}
	if err != nil {
		for _, p := range []**wgpu.RenderPipeline{&g.texturePipeline, &g.cutoutPipeline, &g.textureBlends[0], &g.textureBlends[1]} {
			if *p != nil {
				(*p).Release()
				*p = nil
			}
		}
		g.textureLayout.Release()
		g.textureLayout = nil
		return err
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
