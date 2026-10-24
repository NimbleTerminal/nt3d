// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// Lazily create only the hull pipelines needed by this frame. All reuse the
// surface buffers, instance transforms, texture bindings, and skin compute pass.
func (g *gpuRenderer) prepareOutlines(dev *wgpu.Device, f Frame) error {
	for i := range f.Geometry {
		geom := &f.Geometry[i]
		if geom.Outline.Width == 0 || len(geom.Indices) == 0 {
			continue
		}
		kind := outlinePipelineKind(geom)
		if g.outlinePipelines[kind] != nil {
			continue
		}
		layout, vertex, fragment := g.layout, "vs_outline", "fs_main"
		if mapped(*geom) {
			layout, vertex = g.textureLayout, "vs_outline_map"
		}
		if kind == 3 {
			fragment = "fs_outline_cutout"
		}
		if kind == 4 {
			fragment = "fs_outline_blend"
		}
		var err error
		if transparent(geom) {
			g.outlinePipelines[kind], err = blendPipeline(dev, layout, g.shader, vertex, fragment, 0)
		} else {
			g.outlinePipelines[kind], err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
				Label: "outline hull", Layout: layout, Vertex: wgpu.VertexState{Module: g.shader, EntryPoint: vertex},
				Primitive:    gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeFront},
				DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual},
				Multisample:  gputypes.DefaultMultisampleState(),
				Fragment:     &wgpu.FragmentState{Module: g.shader, EntryPoint: fragment, Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}},
			})
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func outlinePipelineKind(g *Geometry) int {
	if mapped(*g) {
		if transparent(g) {
			return 4
		}
		if g.Material.Cutout {
			return 3
		}
		return 2
	}
	if transparent(g) {
		return 1
	}
	return 0
}

func (g *gpuRenderer) drawOutline(pass *wgpu.RenderPassEncoder, f Frame, d gpuDraw) {
	geom := &f.Geometry[d.index]
	if geom.Outline.Width == 0 || len(geom.Indices) == 0 {
		return
	}
	pass.SetPipeline(g.outlinePipelines[outlinePipelineKind(geom)])
	if mapped(*geom) {
		g.drawTextured(pass, f, d)
		return
	}
	pass.SetBindGroup(0, d.set.binds[1], nil)
	pass.Draw(gputypes.DrawArgs{VertexCount: d.geom.batches[1].count, InstanceCount: d.count})
}
