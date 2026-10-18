// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

type shadowTarget struct {
	size        int
	faces       []*wgpu.TextureView
	texture     *wgpu.Texture
	view        *wgpu.TextureView
	bind        *wgpu.BindGroup
	scratch     *wgpu.Texture
	scratchView *wgpu.TextureView
}

func (t *shadowTarget) release() {
	for _, v := range t.faces {
		v.Release()
	}
	if t.scratchView != nil {
		t.scratchView.Release()
	}
	if t.scratch != nil {
		t.scratch.Release()
	}
	if t.bind != nil {
		t.bind.Release()
	}
	if t.view != nil {
		t.view.Release()
	}
	if t.texture != nil {
		t.texture.Release()
	}
	*t = shadowTarget{}
}
func (g *gpuRenderer) makeShadowTarget(dev *wgpu.Device, size, layers int) (t shadowTarget, err error) {
	defer func() {
		if err != nil {
			t.release()
		}
	}()
	t.size = size
	t.texture, err = dev.CreateTexture(&wgpu.TextureDescriptor{Label: "shadow depth", Size: wgpu.Extent3D{Width: uint32(size), Height: uint32(size), DepthOrArrayLayers: uint32(layers)}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: gputypes.TextureFormatDepth32Float, Usage: gputypes.TextureUsageRenderAttachment | gputypes.TextureUsageTextureBinding})
	if err != nil {
		return
	}
	t.view, err = dev.CreateTextureView(t.texture, &wgpu.TextureViewDescriptor{Dimension: gputypes.TextureViewDimension2DArray, ArrayLayerCount: uint32(layers), MipLevelCount: 1})
	if err != nil {
		return
	}
	if size > 1 {
		t.scratch, err = dev.CreateTexture(&wgpu.TextureDescriptor{Label: "shadow cutout scratch", Size: wgpu.Extent3D{Width: uint32(size), Height: uint32(size), DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: gputypes.TextureFormatR8Unorm, Usage: gputypes.TextureUsageRenderAttachment})
		if err != nil {
			return
		}
		t.scratchView, err = dev.CreateTextureView(t.scratch, nil)
		if err != nil {
			return
		}
	}
	for i := 0; i < layers; i++ {
		v, e := dev.CreateTextureView(t.texture, &wgpu.TextureViewDescriptor{Dimension: gputypes.TextureViewDimension2D, BaseArrayLayer: uint32(i), ArrayLayerCount: 1, MipLevelCount: 1})
		if e != nil {
			err = e
			return
		}
		t.faces = append(t.faces, v)
	}
	t.bind, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: g.shadowLayout, Entries: []wgpu.BindGroupEntry{{Binding: 0, TextureView: t.view}}})

	return
}
func shadowPass(encoder *wgpu.CommandEncoder, view *wgpu.TextureView) (*wgpu.RenderPassEncoder, error) {
	return encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{View: view, DepthLoadOp: gputypes.LoadOpClear, DepthStoreOp: gputypes.StoreOpStore, DepthClearValue: 1, StencilReadOnly: true}})
}

func (g *gpuRenderer) shadowPipeline(dev *wgpu.Device, layout *wgpu.PipelineLayout, vertex, fragment string) (*wgpu.RenderPipeline, error) {
	d := &wgpu.RenderPipelineDescriptor{Label: vertex + fragment, Layout: layout, Vertex: wgpu.VertexState{Module: g.shader, EntryPoint: vertex}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeBack}, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual}, Multisample: gputypes.DefaultMultisampleState()}
	if fragment != "" {
		d.Fragment = &wgpu.FragmentState{Module: g.shader, EntryPoint: fragment, Targets: []gputypes.ColorTargetState{{Format: gputypes.TextureFormatR8Unorm, WriteMask: 0}}}
	}
	return dev.CreateRenderPipeline(d)
}
func (g *gpuRenderer) setupShadowPipelines(dev *wgpu.Device) (err error) {
	for i, vertex := range []string{"vs_shadow_mesh", "vs_shadow_box"} {
		g.shadowPipelines[i], err = g.shadowPipeline(dev, g.layout, vertex, "")
		if err != nil {
			return
		}
	}
	// Initialize the fallback once; queue submission precedes all scene renders.
	enc, err := dev.CreateCommandEncoder(&wgpu.CommandEncoderDescriptor{Label: "clear no shadow"})
	if err != nil {
		return err
	}
	pass, err := shadowPass(enc, g.noShadow.faces[0])
	if err != nil {
		enc.DiscardEncoding()
		return err
	}
	if err = pass.End(); err != nil {
		enc.DiscardEncoding()
		return err
	}
	cmd, err := enc.Finish()
	if err != nil {
		return err
	}
	_, err = dev.Queue().Submit(cmd)
	if err != nil {
		cmd.Release()
		return err
	}
	dev.Poll(wgpu.PollWait)
	dev.FreeCommandBuffer(cmd)
	return nil
}
func (g *gpuRenderer) setupTexturedShadowPipelines(dev *wgpu.Device) (err error) {
	defer func() {
		if err != nil {
			for i := 2; i < 4; i++ {
				if g.shadowPipelines[i] != nil {
					g.shadowPipelines[i].Release()
					g.shadowPipelines[i] = nil
				}
			}
		}
	}()
	for i, fragment := range []string{"", "fs_shadow_cutout"} {
		p, e := g.shadowPipeline(dev, g.textureLayout, "vs_shadow_texture", fragment)
		if e != nil {
			return e
		}
		g.shadowPipelines[i+2] = p
	}
	return nil
}

// drawShadow records opaque front faces and alpha-tested cutouts from the light.
// Opaque pipelines have no color targets. Cutouts need a separate pass with
// R8 scratch to satisfy wgpu fragment validation and Vulkan pass compatibility.
func (g *gpuRenderer) drawShadow(f Frame, encoder *wgpu.CommandEncoder) error {
	for i := range g.shadowTarget.faces {
		if err := g.drawShadowFace(f, encoder, i); err != nil {
			return err
		}
	}
	return nil
}
func (g *gpuRenderer) drawShadowFace(f Frame, encoder *wgpu.CommandEncoder, face int) error {
	pass, err := shadowPass(encoder, g.shadowTarget.faces[face])
	if err != nil {
		return err
	}
	// Never bind the map being written; this group is required by the layouts.
	hasCutout := false
	for _, d := range g.draws {
		geom := &f.Geometry[d.index]
		if !transparent(geom) {
			if b := d.geom.batches[1]; b.count > 0 {
				pass.SetPipeline(g.shadowPipelines[0])
				pass.SetBindGroup(1, g.noShadow.bind, nil)
				pass.SetBindGroup(0, d.set.binds[1], nil)
				pass.Draw(gputypes.DrawArgs{VertexCount: b.count, InstanceCount: d.count, FirstVertex: uint32(face) << 28})
			}
			if d.geom.batches[7].count > 0 {
				if geom.Material.Cutout {
					hasCutout = true
				} else {
					pass.SetPipeline(g.shadowPipelines[2])
					pass.SetBindGroup(1, g.noShadow.bind, nil)
					g.drawTexturedOffset(pass, f, d, uint32(face)<<28)
				}
			}
		}
		if b := d.geom.batches[2]; b.count > 0 {
			pass.SetPipeline(g.shadowPipelines[1])
			pass.SetBindGroup(1, g.noShadow.bind, nil)
			pass.SetBindGroup(0, d.set.binds[2], nil)
			pass.Draw(gputypes.DrawArgs{VertexCount: 36, InstanceCount: b.count, FirstVertex: uint32(face) << 28})
		}
	}
	if err := pass.End(); err != nil {
		return err
	}
	if !hasCutout {
		return nil
	}
	// Preserve opaque depth and only use the pipeline with one color target.
	pass, err = encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: g.shadowTarget.scratchView, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpDiscard}}, DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{View: g.shadowTarget.faces[face], DepthLoadOp: gputypes.LoadOpLoad, DepthStoreOp: gputypes.StoreOpStore, StencilReadOnly: true}})
	if err != nil {
		return err
	}
	pass.SetPipeline(g.shadowPipelines[3])
	pass.SetBindGroup(1, g.noShadow.bind, nil)
	for _, d := range g.draws {
		geom := &f.Geometry[d.index]
		if !transparent(geom) && d.geom.batches[7].count > 0 && geom.Material.Cutout {
			g.drawTexturedOffset(pass, f, d, uint32(face)<<28)
		}
	}
	return pass.End()
}
