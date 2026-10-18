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
	g.textureLayout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{g.bindLayout, g.shadowLayout, sampledLayout, sampledLayout}})
	if err != nil {
		return err
	}
	pipeline := func(label, fragment string) (*wgpu.RenderPipeline, error) {
		return dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: label, Layout: g.textureLayout, Vertex: wgpu.VertexState{Module: g.shader, EntryPoint: "vs_texture"}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeBack}, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth24Plus, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual}, Multisample: gputypes.DefaultMultisampleState(), Fragment: &wgpu.FragmentState{Module: g.shader, EntryPoint: fragment, Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}}})
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
	if err == nil {
		err = g.setupTexturedShadowPipelines(dev)
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

// textureKey is an uploaded texture: an image texture, by its sampler's
// addressing, repeating or clamped, and anisotropy.
type textureKey struct {
	src    *Texture
	repeat bool
	aniso  int
}

// prepareTextures runs outside GPU callbacks: ntgpuimage owns allocation,
// upload, sampling resources and cleanup on its executor thread.
func (g *gpuRenderer) prepareTextures(f Frame) error {
	if g.textures == nil {
		g.textures = make(map[textureKey]*ntgpuimage.Texture)
	}
	used := make(map[textureKey]bool)
	var sampledLayout *wgpu.BindGroupLayout
	for _, geom := range f.Geometry {
		if !mapped(geom) || len(geom.Indices) == 0 {
			continue
		}
		for _, binding := range []struct {
			src    *Texture
			repeat bool
		}{{baseMap(geom.Material), geom.Material.Repeat}, {normalMap(geom.Material), geom.Material.NormalRepeat}} {
			src := binding.src
			if src.shader != "" {
				sampledLayout = g.procedural[src].target.BindGroupLayout()
				continue
			}
			key := textureKey{src, binding.repeat, f.anisotropy()}
			used[key] = true
			t := g.textures[key]
			if t == nil {
				// Color textures decode sRGB; normal maps keep linear vector data.
				// Both use the software renderer's mip chain and hardware repeat.
				var levels []image.Image
				for _, level := range src.levels() {
					levels = append(levels, level)
				}
				options := ntgpuimage.TextureOptions{SRGB: !src.data, Anisotropy: uint16(key.aniso)}
				if key.repeat {
					options.AddressModeU, options.AddressModeV = gputypes.AddressModeRepeat, gputypes.AddressModeRepeat
				}
				var err error
				if t, err = g.r.UploadTextureLevels(levels, options); err != nil {
					return err
				}
				g.textures[key] = t
				g.textureUploads++
			}
			sampledLayout = t.BindGroupLayout()
		}
	}
	for key, t := range g.textures {
		if !used[key] {
			if err := t.Close(); err != nil {
				return err
			}
			delete(g.textures, key)
		}
	}
	if sampledLayout != nil && g.texturePipeline == nil {
		return g.r.Do(func(dev *wgpu.Device) error { return g.setupTextures(dev, sampledLayout) })
	}
	return nil
}
