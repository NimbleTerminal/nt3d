// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"math"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// Crease pipeline slots follow the five hull kinds (0..4).
const (
	creasePipeline         = 5
	creaseCutoutPipeline   = 6
	creaseBlendPipeline    = 7
	creaseBlendMapPipeline = 8
)

func creasePipelineKind(g *Geometry) int {
	if transparent(g) {
		if mapped(*g) {
			return creaseBlendMapPipeline
		}
		return creaseBlendPipeline
	}
	if mapped(*g) && g.Material != nil && g.Material.cutout() {
		return creaseCutoutPipeline
	}
	return creasePipeline
}

func creaseNeedsMaterialBind(g *Geometry) bool {
	if hasShaderHook(*g) {
		return true
	}
	if !mapped(*g) {
		return false
	}
	return transparent(g) || (g.Material != nil && g.Material.cutout())
}

// Lazily create only the hull and crease pipelines needed by this frame. All
// reuse the surface buffers, instance transforms, texture bindings, and skin
// compute pass.
func (g *gpuRenderer) prepareOutlines(dev *wgpu.Device, f Frame) error {
	for i := range f.Draws {
		geom := &f.Draws[i].Geometry
		if geom.Outline.Width == 0 || len(geom.Indices) == 0 {
			continue
		}
		if (f.OIT && transparent(geom)) || hasShaderHook(*geom) {
			continue
		}
		kinds := []int{outlinePipelineKind(geom)}
		if creased(geom) {
			kinds = append(kinds, creasePipelineKind(geom))
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
	needsTexLayout := kind == 2 || kind == 3 || kind == 4 || kind == creaseCutoutPipeline || kind == creaseBlendMapPipeline
	if needsTexLayout && g.textureLayout == nil {
		if err := g.ensureTexturePipelines(dev); err != nil {
			return err
		}
	}
	idx := g.sampleIdx
	g.shared.mu.Lock()
	defer g.shared.mu.Unlock()
	if p := g.shared.msaaSets[idx].outlinePipelines[kind]; p != nil {
		g.outlinePipelines[kind] = p
		g.msaaSets[idx].outlinePipelines[kind] = p
		return nil
	}
	layout, vertex, fragment := g.layout, "vs_outline", "fs_main"
	cull := gputypes.CullModeFront
	blendSide := 0
	switch kind {
	case 2:
		layout, vertex = g.textureLayout, "vs_outline_map"
	case 3:
		layout, vertex, fragment = g.textureLayout, "vs_outline_map", "fs_outline_cutout"
	case 4:
		layout, vertex, fragment = g.textureLayout, "vs_outline_map", "fs_outline_blend"
	case creasePipeline:
		vertex, cull = "vs_crease", gputypes.CullModeNone
	case creaseCutoutPipeline:
		layout, vertex, fragment, cull = g.textureLayout, "vs_crease", "fs_outline_cutout", gputypes.CullModeNone
	case creaseBlendPipeline:
		vertex, cull, blendSide = "vs_crease", gputypes.CullModeNone, 2
	case creaseBlendMapPipeline:
		layout, vertex, fragment, cull, blendSide = g.textureLayout, "vs_crease", "fs_outline_blend", gputypes.CullModeNone, 2
	}
	isBlended := kind == 1 || kind == 4 || kind == creaseBlendPipeline || kind == creaseBlendMapPipeline
	ms := g.multisample()
	var err error
	if isBlended {
		g.outlinePipelines[kind], err = blendPipelineMS(dev, layout, g.shader, vertex, fragment, blendSide, ms)
	} else {
		label := "outline hull"
		if kind >= creasePipeline {
			label = "crease lines"
		}
		g.outlinePipelines[kind], err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
			Label: label, Layout: layout, Vertex: wgpu.VertexState{Module: g.shader, EntryPoint: vertex},
			Primitive:    gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: cull},
			DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual},
			Multisample:  ms,
			Fragment:     &wgpu.FragmentState{Module: g.shader, EntryPoint: fragment, Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}},
		})
	}
	if err != nil {
		return err
	}
	g.msaaSets[idx].outlinePipelines[kind] = g.outlinePipelines[kind]
	g.shared.msaaSets[idx].outlinePipelines[kind] = g.outlinePipelines[kind]
	return nil
}

// maxCreaseAngles is how many crease uploads a mesh keeps before those no draw
// has used lately are released.
const maxCreaseAngles = 4

func creaseKeyForDraw(f Frame, d gpuDraw) (creaseUploadKey, uint64, creaseDisplaceState) {
	geom := &f.Draws[d.index].Geometry
	var flags uint32
	if geom.Material != nil && geom.Material.Repeat {
		flags |= 1
	}
	displaceOnGPU := geom.Displacement != nil && (d.count > 1 || d.geom.used > 1)
	if displaceOnGPU {
		flags |= 2
	}
	var cutBits uint32
	if transparent(geom) {
		cutBits = 0xFFFFFFFF
	} else if geom.Material != nil && geom.Material.cutout() {
		cutBits = math.Float32bits(geom.Material.threshold())
	}
	var poseRev uint64
	if posed := f.Draws[d.index].state.posed; posed != nil {
		poseRev = posed.revision
	}
	var disp creaseDisplaceState
	if geom.Displacement != nil && !displaceOnGPU {
		disp = creaseDisplaceState{
			active: true,
			ptr:    geom.Displacement,
			world:  instance(f, d.index).World,
		}
	}
	return creaseUploadKey{
		angleBits: math.Float32bits(geom.Outline.Crease),
		cutBits:   cutBits,
		flags:     flags,
	}, poseRev, disp
}

// prepareCreases uploads the crease edges each draw of a creased mesh needs,
// updating posed or displaced meshes in place when capacity permits, and pairs
// them with the draw's transforms.
func (g *gpuRenderer) prepareCreases(dev *wgpu.Device, f Frame) error {
	g.creaseFrame++
	for _, d := range g.draws {
		geom := &f.Draws[d.index].Geometry
		if !creased(geom) {
			continue
		}
		key, poseRev, disp := creaseKeyForDraw(f, d)
		c := d.geom.creases[key]
		if c == nil || c.poseRevision != poseRev || c.displace != disp {
			verts := geom.Vertices
			if disp.active {
				verts = displacedVertices(geom, disp.world)
			}
			segments := creaseSegments(verts, geom.Indices, geom.UV, geom.Outline.Crease)
			isTrans := transparent(geom)
			cutVal := float32(1)
			if !isTrans && geom.Material != nil && geom.Material.cutout() {
				cutVal = geom.Material.threshold()
			}
			flagFloat := float32(key.flags)
			data := make([]byte, 0, len(segments)*48)
			for _, v := range segments {
				alpha := cutVal
				if isTrans {
					alpha = v.opacity
				}
				data = appendFloats(data, v.pos.X, v.pos.Y, v.pos.Z, v.uv.U, v.normal.X, v.normal.Y, v.normal.Z, v.uv.V, alpha, flagFloat, 0, 0)
			}
			if c == nil {
				if len(d.geom.creases) >= maxCreaseAngles {
					for k, old := range d.geom.creases {
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
						delete(d.geom.creases, k)
					}
				}
				c = &gpuCrease{poseRevision: poseRev, displace: disp}
				if err := g.uploadData(dev, &c.batch, data, 48); err != nil {
					return err
				}
				c.capacity = c.batch.size
				if d.geom.creases == nil {
					d.geom.creases = map[creaseUploadKey]*gpuCrease{}
				}
				d.geom.creases[key] = c
			} else {
				c.poseRevision, c.displace = poseRev, disp
				if len(data) == 0 {
					c.batch.count = 0
				} else if c.batch.buffer != nil && uint64(len(data)) <= c.capacity {
					if err := dev.Queue().WriteBuffer(c.batch.buffer, 0, data); err != nil {
						return err
					}
					c.batch.count = uint32(len(data) / 48)
					c.batch.size = uint64(len(data))
				} else {
					for _, set := range d.geom.sets {
						if bind := set.creaseBinds[c]; bind != nil {
							bind.Release()
							delete(set.creaseBinds, c)
						}
					}
					releaseBatch(c.batch)
					c.batch = gpuBatch{}
					if err := g.uploadData(dev, &c.batch, data, 48); err != nil {
						return err
					}
					c.capacity = c.batch.size
				}
			}
		}
		c.used = g.creaseFrame
		if c.batch.buffer == nil || d.set.creaseBinds[c] != nil {
			continue
		}
		entries := []wgpu.BindGroupEntry{{Binding: 0, Buffer: g.uniform, Size: uniformSize}, {Binding: 1, Buffer: c.batch.buffer, Size: c.capacity}, {Binding: 2, Buffer: d.set.transform, Size: uint64(d.set.capacity) * instanceRecordSize}}
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
// vertices a segment.
func (g *gpuRenderer) drawCrease(pass *wgpu.RenderPassEncoder, f Frame, d gpuDraw) {
	geom := &f.Draws[d.index].Geometry
	if !creased(geom) {
		return
	}
	key, _, _ := creaseKeyForDraw(f, d)
	c := d.geom.creases[key]
	if c == nil || c.batch.count < 2 || d.set.creaseBinds[c] == nil {
		return
	}
	kind := creasePipelineKind(geom)
	if creaseNeedsMaterialBind(geom) {
		g.setHookOrScenePipeline(pass, geom, g.outlinePipelines[kind], hookCreasePipeKind(kind))
		pass.SetBindGroup(0, d.set.creaseBinds[c], nil)
		mk := g.materialKey(geom.Material, f.anisotropy())
		pass.SetBindGroup(2, g.materialBinds[mk], nil)
		if bind := g.hookBindFor(geom); bind != nil {
			pass.SetBindGroup(3, bind, nil)
		}
	} else {
		g.setScenePipeline(pass, g.outlinePipelines[kind])
		pass.SetBindGroup(0, d.set.creaseBinds[c], nil)
	}
	pass.Draw(gputypes.DrawArgs{VertexCount: c.batch.count / 2 * 6, InstanceCount: d.count})
}

func (g *gpuRenderer) drawCreaseOIT(pass *wgpu.RenderPassEncoder, f Frame, d gpuDraw, creasePipes [2]*wgpu.RenderPipeline) {
	geom := &f.Draws[d.index].Geometry
	if !creased(geom) {
		return
	}
	key, _, _ := creaseKeyForDraw(f, d)
	c := d.geom.creases[key]
	if c == nil || c.batch.count < 2 || d.set.creaseBinds[c] == nil {
		return
	}
	if mapped(*geom) {
		g.setHookOrScenePipeline(pass, geom, creasePipes[1], hookPipeOITCrease)
		pass.SetBindGroup(0, d.set.creaseBinds[c], nil)
		mk := g.materialKey(geom.Material, f.anisotropy())
		pass.SetBindGroup(2, g.materialBinds[mk], nil)
		if bind := g.hookBindFor(geom); bind != nil {
			pass.SetBindGroup(3, bind, nil)
		}
	} else {
		g.setScenePipeline(pass, creasePipes[0])
		pass.SetBindGroup(0, d.set.creaseBinds[c], nil)
	}
	pass.Draw(gputypes.DrawArgs{VertexCount: c.batch.count / 2 * 6, InstanceCount: d.count})
}

func outlinePipelineKind(g *Geometry) int {
	if mapped(*g) {
		if transparent(g) {
			return 4
		}
		if g.Material != nil && g.Material.cutout() {
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
	geom := &f.Draws[d.index].Geometry
	if geom.Outline.Width == 0 || len(geom.Indices) == 0 {
		return
	}
	kind := outlinePipelineKind(geom)
	if mapped(*geom) {
		if d.geom.batches[slotTextured].count == 0 {
			return
		}
		g.setHookOrScenePipeline(pass, geom, g.outlinePipelines[kind], hookOutlinePipeKind(kind))
		g.drawTextured(pass, f, d)
		return
	}
	if d.geom.batches[slotMesh].count == 0 {
		return
	}
	g.setScenePipeline(pass, g.outlinePipelines[kind])
	pass.SetBindGroup(0, d.set.binds[slotMesh], nil)
	pass.Draw(gputypes.DrawArgs{VertexCount: d.geom.batches[slotMesh].count, InstanceCount: d.count})
}
