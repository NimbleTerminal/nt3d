// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

const oitRevealFormat = gputypes.TextureFormatR8Unorm

const oitCompositeShader = `
@group(0) @binding(0) var oitAccum: texture_2d<f32>;
@group(0) @binding(1) var oitReveal: texture_2d<f32>;

@vertex fn vs_oit_composite(@builtin(vertex_index) i: u32) -> @builtin(position) vec4<f32> {
	let p = vec2<f32>(f32((i << 1u) & 2u), f32(i & 2u));
	return vec4<f32>(p * 2.0 - 1.0, 0.0, 1.0);
}

@fragment fn fs_oit_composite(@builtin(position) pos: vec4<f32>) -> @location(0) vec4<f32> {
	let c = vec2<i32>(pos.xy);
	let reveal = textureLoad(oitReveal, c, 0).r;
	if reveal >= 0.99999 {
		discard;
	}
	let accum = textureLoad(oitAccum, c, 0);
	let avg = accum.rgb / max(accum.a, 1e-5);
	return vec4<f32>(avg, 1.0 - reveal);
}
`

type gpuOITShared struct {
	meshPipelines      [2]*wgpu.RenderPipeline
	texPipelines       [2]*wgpu.RenderPipeline
	outlinePipelines   [2][2]*wgpu.RenderPipeline
	creasePipelines    [2][2]*wgpu.RenderPipeline
	compositeShader    *wgpu.ShaderModule
	compositeGroup     *wgpu.BindGroupLayout
	compositeLayout    *wgpu.PipelineLayout
	compositePipelines [2]*wgpu.RenderPipeline
}

func (s *gpuOITShared) release() {
	for i := range 2 {
		if s.meshPipelines[i] != nil {
			s.meshPipelines[i].Release()
		}
		if s.texPipelines[i] != nil {
			s.texPipelines[i].Release()
		}
		if s.compositePipelines[i] != nil {
			s.compositePipelines[i].Release()
		}
		for k := range 2 {
			if s.outlinePipelines[i][k] != nil {
				s.outlinePipelines[i][k].Release()
			}
			if s.creasePipelines[i][k] != nil {
				s.creasePipelines[i][k].Release()
			}
		}
	}
	if s.compositeLayout != nil {
		s.compositeLayout.Release()
	}
	if s.compositeGroup != nil {
		s.compositeGroup.Release()
	}
	if s.compositeShader != nil {
		s.compositeShader.Release()
	}
	*s = gpuOITShared{}
}

type gpuOITTarget struct {
	w, h, samples  int
	accum          *wgpu.Texture
	accumView      *wgpu.TextureView
	reveal         *wgpu.Texture
	revealView     *wgpu.TextureView
	accumMSAA      *wgpu.Texture
	accumMSAAView  *wgpu.TextureView
	revealMSAA     *wgpu.Texture
	revealMSAAView *wgpu.TextureView
	compositeBind  *wgpu.BindGroup
}

func (t *gpuOITTarget) release() {
	if t.compositeBind != nil {
		t.compositeBind.Release()
	}
	for _, v := range []*wgpu.TextureView{t.accumMSAAView, t.revealMSAAView, t.accumView, t.revealView} {
		if v != nil {
			v.Release()
		}
	}
	for _, tex := range []*wgpu.Texture{t.accumMSAA, t.revealMSAA, t.accum, t.reveal} {
		if tex != nil {
			tex.Release()
		}
	}
	*t = gpuOITTarget{}
}

func oitFragmentTargets() []gputypes.ColorTargetState {
	accumBlend := gputypes.BlendState{
		Color: gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorOne, DstFactor: gputypes.BlendFactorOne, Operation: gputypes.BlendOperationAdd},
		Alpha: gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorOne, DstFactor: gputypes.BlendFactorOne, Operation: gputypes.BlendOperationAdd},
	}
	revealBlend := gputypes.BlendState{
		Color: gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorZero, DstFactor: gputypes.BlendFactorOneMinusSrcAlpha, Operation: gputypes.BlendOperationAdd},
		Alpha: gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorZero, DstFactor: gputypes.BlendFactorOneMinusSrcAlpha, Operation: gputypes.BlendOperationAdd},
	}
	return []gputypes.ColorTargetState{
		{Format: sceneFormat, Blend: &accumBlend, WriteMask: gputypes.ColorWriteMaskAll},
		{Format: oitRevealFormat, Blend: &revealBlend, WriteMask: gputypes.ColorWriteMaskAll},
	}
}

func createOITPipeline(dev *wgpu.Device, layout *wgpu.PipelineLayout, shader *wgpu.ShaderModule, vertex, fragment string, cull gputypes.CullMode, ms gputypes.MultisampleState) (*wgpu.RenderPipeline, error) {
	return dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:        "oit " + vertex + " " + fragment,
		Layout:       layout,
		Vertex:       wgpu.VertexState{Module: shader, EntryPoint: vertex},
		Primitive:    gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: cull},
		DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: false, DepthCompare: gputypes.CompareFunctionLessEqual},
		Multisample:  ms,
		Fragment:     &wgpu.FragmentState{Module: shader, EntryPoint: fragment, Targets: oitFragmentTargets()},
	})
}

func (g *gpuRenderer) prepareOIT(dev *wgpu.Device, f Frame, drawn []bool) error {
	if !hasOITWork(f, drawn) {
		return nil
	}
	idx := g.sampleIdx
	samples := g.target.samples
	ms := g.multisample()

	needMesh, needTex := false, false
	var needOutline [2]bool
	var needCrease [2]bool
	for i := range f.Geometry {
		geom := &f.Geometry[i]
		if !drawn[i] || !transparent(geom) {
			continue
		}
		if mapped(*geom) {
			if !hasShaderHook(*geom) {
				needTex = true
				if geom.Outline.Width != 0 && len(geom.Indices) > 0 {
					needOutline[1] = true
					if creased(geom) {
						needCrease[1] = true
					}
				}
			}
		} else {
			needMesh = true
			if geom.Outline.Width != 0 && len(geom.Indices) > 0 {
				needOutline[0] = true
				if creased(geom) {
					needCrease[0] = true
				}
			}
		}
	}
	if (needTex || needOutline[1] || needCrease[1]) && g.textureLayout == nil {
		if err := g.ensureTexturePipelines(dev); err != nil {
			return err
		}
	}

	if g.shared != nil {
		g.shared.mu.Lock()
		os := &g.shared.oit
		if os.compositeShader == nil {
			var err error
			if os.compositeShader, err = dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "nt3d oit composite", WGSL: oitCompositeShader}); err != nil {
				g.shared.mu.Unlock()
				return err
			}
			if os.compositeGroup, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
				Label: "nt3d oit composite group",
				Entries: []gputypes.BindGroupLayoutEntry{
					{Binding: 0, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeUnfilterableFloat, ViewDimension: gputypes.TextureViewDimension2D}},
					{Binding: 1, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeUnfilterableFloat, ViewDimension: gputypes.TextureViewDimension2D}},
				},
			}); err != nil {
				g.shared.mu.Unlock()
				return err
			}
			if os.compositeLayout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{os.compositeGroup}}); err != nil {
				g.shared.mu.Unlock()
				return err
			}
		}
		if os.compositePipelines[idx] == nil {
			blend := gputypes.BlendStateAlpha()
			p, err := dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
				Label:        "nt3d oit composite",
				Layout:       os.compositeLayout,
				Vertex:       wgpu.VertexState{Module: os.compositeShader, EntryPoint: "vs_oit_composite"},
				Primitive:    gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone},
				DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: false, DepthCompare: gputypes.CompareFunctionAlways},
				Multisample:  ms,
				Fragment:     &wgpu.FragmentState{Module: os.compositeShader, EntryPoint: "fs_oit_composite", Targets: []gputypes.ColorTargetState{{Format: sceneFormat, Blend: &blend, WriteMask: gputypes.ColorWriteMaskAll}}},
			})
			if err != nil {
				g.shared.mu.Unlock()
				return err
			}
			os.compositePipelines[idx] = p
		}
		if needMesh && os.meshPipelines[idx] == nil {
			p, err := createOITPipeline(dev, g.layout, g.shader, "vs_mesh", "fs_oit_main", gputypes.CullModeNone, ms)
			if err != nil {
				g.shared.mu.Unlock()
				return err
			}
			os.meshPipelines[idx] = p
		}
		if needTex && os.texPipelines[idx] == nil {
			p, err := createOITPipeline(dev, g.textureLayout, g.shader, "vs_texture", "fs_oit_texture", gputypes.CullModeNone, ms)
			if err != nil {
				g.shared.mu.Unlock()
				return err
			}
			os.texPipelines[idx] = p
		}
		if needOutline[0] && os.outlinePipelines[idx][0] == nil {
			p, err := createOITPipeline(dev, g.layout, g.shader, "vs_outline", "fs_oit_main", gputypes.CullModeFront, ms)
			if err != nil {
				g.shared.mu.Unlock()
				return err
			}
			os.outlinePipelines[idx][0] = p
		}
		if needOutline[1] && os.outlinePipelines[idx][1] == nil {
			p, err := createOITPipeline(dev, g.textureLayout, g.shader, "vs_outline_map", "fs_oit_outline_blend", gputypes.CullModeFront, ms)
			if err != nil {
				g.shared.mu.Unlock()
				return err
			}
			os.outlinePipelines[idx][1] = p
		}
		if needCrease[0] && os.creasePipelines[idx][0] == nil {
			p, err := createOITPipeline(dev, g.layout, g.shader, "vs_crease", "fs_oit_main", gputypes.CullModeNone, ms)
			if err != nil {
				g.shared.mu.Unlock()
				return err
			}
			os.creasePipelines[idx][0] = p
		}
		if needCrease[1] && os.creasePipelines[idx][1] == nil {
			p, err := createOITPipeline(dev, g.textureLayout, g.shader, "vs_crease", "fs_oit_outline_blend", gputypes.CullModeNone, ms)
			if err != nil {
				g.shared.mu.Unlock()
				return err
			}
			os.creasePipelines[idx][1] = p
		}
		compGroup := os.compositeGroup
		g.shared.mu.Unlock()

		return g.ensureOITTarget(dev, g.target.w, g.target.h, samples, compGroup)
	}
	return nil
}

func (g *gpuRenderer) ensureOITTarget(dev *wgpu.Device, w, h, samples int, compGroup *wgpu.BindGroupLayout) (err error) {
	if g.oit.accum != nil && g.oit.w == w && g.oit.h == h && g.oit.samples == samples {
		return nil
	}
	g.oit.release()
	var next gpuOITTarget
	defer func() {
		if err != nil {
			next.release()
		}
	}()
	next.w, next.h, next.samples = w, h, samples
	size := wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1}
	usage1x := wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageTextureBinding

	if next.accum, err = dev.CreateTexture(&wgpu.TextureDescriptor{Label: "oit accum", Size: size, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: sceneFormat, Usage: usage1x}); err != nil {
		return err
	}
	if next.accumView, err = dev.CreateTextureView(next.accum, nil); err != nil {
		return err
	}
	if next.reveal, err = dev.CreateTexture(&wgpu.TextureDescriptor{Label: "oit reveal", Size: size, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: oitRevealFormat, Usage: usage1x}); err != nil {
		return err
	}
	if next.revealView, err = dev.CreateTextureView(next.reveal, nil); err != nil {
		return err
	}
	if samples > 1 {
		if next.accumMSAA, err = dev.CreateTexture(&wgpu.TextureDescriptor{Label: "oit accum msaa", Size: size, MipLevelCount: 1, SampleCount: uint32(samples), Dimension: wgpu.TextureDimension2D, Format: sceneFormat, Usage: wgpu.TextureUsageRenderAttachment}); err != nil {
			return err
		}
		if next.accumMSAAView, err = dev.CreateTextureView(next.accumMSAA, nil); err != nil {
			return err
		}
		if next.revealMSAA, err = dev.CreateTexture(&wgpu.TextureDescriptor{Label: "oit reveal msaa", Size: size, MipLevelCount: 1, SampleCount: uint32(samples), Dimension: wgpu.TextureDimension2D, Format: oitRevealFormat, Usage: wgpu.TextureUsageRenderAttachment}); err != nil {
			return err
		}
		if next.revealMSAAView, err = dev.CreateTextureView(next.revealMSAA, nil); err != nil {
			return err
		}
	}
	if next.compositeBind, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "oit composite bind",
		Layout: compGroup,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: next.accumView},
			{Binding: 1, TextureView: next.revealView},
		},
	}); err != nil {
		return err
	}
	g.oit = next
	return nil
}

func (g *gpuRenderer) encodeOITPass(f Frame, encoder *wgpu.CommandEncoder, depth *wgpu.TextureView) error {
	accumAtt := wgpu.RenderPassColorAttachment{
		View:       g.oit.accumView,
		LoadOp:     gputypes.LoadOpClear,
		StoreOp:    gputypes.StoreOpStore,
		ClearValue: gputypes.Color{R: 0, G: 0, B: 0, A: 0},
	}
	revealAtt := wgpu.RenderPassColorAttachment{
		View:       g.oit.revealView,
		LoadOp:     gputypes.LoadOpClear,
		StoreOp:    gputypes.StoreOpStore,
		ClearValue: gputypes.Color{R: 1, G: 0, B: 0, A: 1},
	}
	if g.target.samples > 1 {
		accumAtt.View = g.oit.accumMSAAView
		accumAtt.ResolveTarget = g.oit.accumView
		revealAtt.View = g.oit.revealMSAAView
		revealAtt.ResolveTarget = g.oit.revealView
	}
	pass, err := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments:       []wgpu.RenderPassColorAttachment{accumAtt, revealAtt},
		DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{View: depth, DepthLoadOp: gputypes.LoadOpLoad, DepthStoreOp: gputypes.StoreOpStore, StencilReadOnly: true},
	})
	if err != nil {
		return err
	}

	idx := g.sampleIdx
	g.shared.mu.Lock()
	meshPipe := g.shared.oit.meshPipelines[idx]
	texPipe := g.shared.oit.texPipelines[idx]
	outlinePipes := g.shared.oit.outlinePipelines[idx]
	creasePipes := g.shared.oit.creasePipelines[idx]
	g.shared.mu.Unlock()

	for _, d := range g.draws {
		geom := &f.Geometry[d.index]
		if !transparent(geom) {
			continue
		}
		if geom.Outline.Width != 0 && len(geom.Indices) > 0 {
			if mapped(*geom) {
				if d.geom.batches[7].count > 0 {
					g.setHookOrScenePipeline(pass, geom, outlinePipes[1], hookPipeOITOutline)
					g.drawTextured(pass, f, d)
				}
			} else if d.geom.batches[1].count > 0 {
				g.setScenePipeline(pass, outlinePipes[0])
				pass.SetBindGroup(0, d.set.binds[1], nil)
				pass.Draw(gputypes.DrawArgs{VertexCount: d.geom.batches[1].count, InstanceCount: d.count})
			}
		}
		if mapped(*geom) {
			if d.geom.batches[7].count > 0 {
				g.setHookOrScenePipeline(pass, geom, texPipe, hookPipeOIT)
				g.drawTextured(pass, f, d)
			}
		} else if b := d.geom.batches[1]; b.count > 0 {
			g.setScenePipeline(pass, meshPipe)
			pass.SetBindGroup(0, d.set.binds[1], nil)
			pass.Draw(gputypes.DrawArgs{VertexCount: b.count, InstanceCount: d.count})
		}
		g.drawCreaseOIT(pass, f, d, creasePipes)
	}
	return pass.End()
}

func (g *gpuRenderer) drawOITComposite(pass *wgpu.RenderPassEncoder) {
	idx := g.sampleIdx
	g.shared.mu.Lock()
	compPipe := g.shared.oit.compositePipelines[idx]
	g.shared.mu.Unlock()
	pass.SetPipeline(compPipe)
	pass.SetBindGroup(0, g.oit.compositeBind, nil)
	pass.Draw(gputypes.DrawArgs{VertexCount: 3, InstanceCount: 1})
}
