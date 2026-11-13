// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"math"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// creasePipeline is the crease lines' slot among the outline pipelines, after
// the five hull kinds.
const creasePipeline = 5

// Lazily create only the hull pipelines needed by this frame, and the crease
// line pipeline. All reuse the surface buffers, instance transforms, texture
// bindings, and skin compute pass.
func (g *gpuRenderer) prepareOutlines(dev *wgpu.Device, f Frame) error {
	for i := range f.Geometry {
		geom := &f.Geometry[i]
		if geom.Outline.Width == 0 || len(geom.Indices) == 0 {
			continue
		}
		kinds := []int{outlinePipelineKind(geom)}
		if creased(geom) {
			kinds = append(kinds, creasePipeline)
		}
		for _, kind := range kinds {
			if err := g.prepareOutline(dev, geom, kind); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *gpuRenderer) prepareOutline(dev *wgpu.Device, geom *Geometry, kind int) error {
	if g.outlinePipelines[kind] != nil {
		return nil
	}
	if g.shared != nil {
		g.shared.mu.Lock()
		defer g.shared.mu.Unlock()
		if g.shared.outlinePipelines[kind] != nil {
			g.outlinePipelines[kind] = g.shared.outlinePipelines[kind]
			return nil
		}
	}
	layout, vertex, fragment := g.layout, "vs_outline", "fs_main"
	cull := gputypes.CullModeFront
	if kind == creasePipeline {
		vertex, cull = "vs_crease", gputypes.CullModeNone
	} else if mapped(*geom) {
		layout, vertex = g.textureLayout, "vs_outline_map"
	}
	if kind == 3 {
		fragment = "fs_outline_cutout"
	}
	if kind == 4 {
		fragment = "fs_outline_blend"
	}
	var err error
	if kind != creasePipeline && transparent(geom) {
		g.outlinePipelines[kind], err = blendPipeline(dev, layout, g.shader, vertex, fragment, 0)
	} else {
		label := "outline hull"
		if kind == creasePipeline {
			label = "crease lines"
		}
		g.outlinePipelines[kind], err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
			Label: label, Layout: layout, Vertex: wgpu.VertexState{Module: g.shader, EntryPoint: vertex},
			Primitive:    gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: cull},
			DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual},
			Multisample:  gputypes.DefaultMultisampleState(),
			Fragment:     &wgpu.FragmentState{Module: g.shader, EntryPoint: fragment, Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}},
		})
	}
	if err != nil {
		return err
	}
	if g.shared != nil {
		g.shared.outlinePipelines[kind] = g.outlinePipelines[kind]
	}
	return nil
}

// maxCreaseAngles is how many crease angles' edges an uploaded mesh keeps
// before those no draw has used lately are released.
const maxCreaseAngles = 4

// prepareCreases uploads the crease edges each draw of a creased mesh needs,
// at its own crease angle, and pairs them with the draw's transforms. A mesh
// keeps the edges of the angles its instances use; past maxCreaseAngles,
// those no draw has used in this frame or the last are released.
func (g *gpuRenderer) prepareCreases(dev *wgpu.Device, f Frame) error {
	g.creaseFrame++
	for _, d := range g.draws {
		geom := &f.Geometry[d.index]
		if !creased(geom) {
			continue
		}
		bits := math.Float32bits(geom.Outline.Crease)
		c := d.geom.creases[bits]
		if c == nil {
			edges := creaseEdges(geom.Vertices, geom.Indices, geom.Outline.Crease)
			data := make([]byte, 0, len(edges)*48)
			for _, p := range edges {
				data = appendFloats(data, p.X, p.Y, p.Z, 0, 0, 0, 0, 0, 0, 0, 0, 0)
			}
			if len(d.geom.creases) >= maxCreaseAngles {
				for angle, old := range d.geom.creases {
					if old.used+1 >= g.creaseFrame {
						continue
					}
					for _, set := range d.geom.sets {
						if bind := set.creaseBinds[old]; bind != nil {
							bind.Release()
							delete(set.creaseBinds, old)
						}
					}
					releaseBatch(old.batch)
					delete(d.geom.creases, angle)
				}
			}
			c = &gpuCrease{}
			if err := g.uploadData(dev, &c.batch, data, 48); err != nil {
				return err
			}
			if d.geom.creases == nil {
				d.geom.creases = map[uint32]*gpuCrease{}
			}
			d.geom.creases[bits] = c
		}
		c.used = g.creaseFrame
		if c.batch.buffer == nil || d.set.creaseBinds[c] != nil {
			continue
		}
		entries := []wgpu.BindGroupEntry{{Binding: 0, Buffer: g.uniform, Size: uniformSize}, {Binding: 1, Buffer: c.batch.buffer, Size: c.batch.size}, {Binding: 2, Buffer: d.set.transform, Size: uint64(d.set.capacity) * instanceRecordSize}}
		bind, err := dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: g.bindLayout, Entries: entries})
		if err != nil {
			return err
		}
		if d.set.creaseBinds == nil {
			d.set.creaseBinds = map[*gpuCrease]*wgpu.BindGroup{}
		}
		d.set.creaseBinds[c] = bind
	}
	return nil
}

// drawCrease draws a creased mesh's crease lines for a draw's instances, six
// vertices a segment, after the opaque hulls.
func (g *gpuRenderer) drawCrease(pass *wgpu.RenderPassEncoder, f Frame, d gpuDraw) {
	geom := &f.Geometry[d.index]
	if !creased(geom) {
		return
	}
	c := d.geom.creases[math.Float32bits(geom.Outline.Crease)]
	if c == nil || d.set.creaseBinds[c] == nil {
		return
	}
	g.setScenePipeline(pass, g.outlinePipelines[creasePipeline])
	pass.SetBindGroup(0, d.set.creaseBinds[c], nil)
	pass.Draw(gputypes.DrawArgs{VertexCount: c.batch.count / 2 * 6, InstanceCount: d.count})
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
	g.setScenePipeline(pass, g.outlinePipelines[outlinePipelineKind(geom)])
	if mapped(*geom) {
		g.drawTextured(pass, f, d)
		return
	}
	pass.SetBindGroup(0, d.set.binds[1], nil)
	pass.Draw(gputypes.DrawArgs{VertexCount: d.geom.batches[1].count, InstanceCount: d.count})
}
