// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"encoding/binary"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"math"
	"strings"
)

// Explicit UVs avoid naga's Metal indexed-array arithmetic issue.
const proceduralQuad = `
@group(0) @binding(0) var<uniform> nt3dTextureParams: vec4<f32>;
struct NT3DTextureOut { @builtin(position) pos: vec4<f32>, @location(0) uv: vec2<f32> };
@vertex fn nt3d_texture_vertex(@builtin(vertex_index) i: u32) -> NT3DTextureOut {
 let pos = array<vec2<f32>, 6>(vec2<f32>(-1,1), vec2<f32>(1,1), vec2<f32>(1,-1), vec2<f32>(-1,1), vec2<f32>(1,-1), vec2<f32>(-1,-1));
 let uv = array<vec2<f32>, 6>(vec2<f32>(0,0), vec2<f32>(1,0), vec2<f32>(1,1), vec2<f32>(0,0), vec2<f32>(1,1), vec2<f32>(0,1));
 var out: NT3DTextureOut;
 out.pos = vec4<f32>(pos[i], 0, 1);
 out.uv = uv[i];
 return out;
}
// The target is sRGB, so it stores the encoded color the shader returns:
// decoded here, encoded again as it is written.
fn nt3d_decode(c: vec3<f32>) -> vec3<f32> {
 return select(pow((c + 0.055) / 1.055, vec3<f32>(2.4)), c / 12.92, c <= vec3<f32>(0.04045));
}
@fragment fn nt3d_texture_fragment(in: NT3DTextureOut) -> @location(0) vec4<f32> {
 let c = nt3dTexture(in.uv);
 return vec4<f32>(nt3d_decode(clamp(c.rgb, vec3<f32>(0.0), vec3<f32>(1.0))), c.a);
}
`

const proceduralMipShader = `
@group(0) @binding(0) var tex: texture_2d<f32>;
@group(0) @binding(1) var samp: sampler;
struct Out { @builtin(position) pos: vec4<f32>, @location(0) uv: vec2<f32> };
@vertex fn vs_main(@builtin(vertex_index) i: u32) -> Out {
	let p = array<vec2<f32>, 6>(
		vec2<f32>(-1.0, 1.0), vec2<f32>(1.0, 1.0), vec2<f32>(1.0, -1.0),
		vec2<f32>(-1.0, 1.0), vec2<f32>(1.0, -1.0), vec2<f32>(-1.0, -1.0));
	let uv = array<vec2<f32>, 6>(
		vec2<f32>(0.0, 0.0), vec2<f32>(1.0, 0.0), vec2<f32>(1.0, 1.0),
		vec2<f32>(0.0, 0.0), vec2<f32>(1.0, 1.0), vec2<f32>(0.0, 1.0));
	var o: Out;
	o.pos = vec4<f32>(p[i], 0.0, 1.0);
	o.uv = uv[i];
	return o;
}
@fragment fn fs_main(o: Out) -> @location(0) vec4<f32> {
	return textureSampleLevel(tex, samp, o.uv, 0.0);
}`

const generalTextureLayout = gputypes.TextureUsageStorageBinding | gputypes.TextureUsageTextureBinding

func transitionTexture(enc *wgpu.CommandEncoder, tex *wgpu.Texture, from, to gputypes.TextureUsage) {
	if from == to {
		return
	}
	enc.TransitionTextures([]wgpu.TextureBarrier{{
		Texture: tex,
		Range:   wgpu.TextureRange{Aspect: gputypes.TextureAspectAll, BaseMipLevel: 0, MipLevelCount: 1, ArrayLayerCount: 1},
		Usage:   wgpu.TextureUsageTransition{OldUsage: from, NewUsage: to},
	}})
}

func transitionTextureLevel(enc *wgpu.CommandEncoder, tex *wgpu.Texture, level int, from, to gputypes.TextureUsage) {
	enc.TransitionTextures([]wgpu.TextureBarrier{{
		Texture: tex,
		Range:   wgpu.TextureRange{Aspect: gputypes.TextureAspectAll, BaseMipLevel: uint32(level), MipLevelCount: 1, ArrayLayerCount: 1},
		Usage:   wgpu.TextureUsageTransition{OldUsage: from, NewUsage: to},
	}})
}

// Procedural targets own their sampler, so addressing is part of their identity.
type proceduralKey struct {
	src    *Texture
	repeat bool
}

type proceduralTexture struct {
	target     *gpuTexture
	aniso      int // The target's sampler's.
	uniform    *wgpu.Buffer
	bindLayout *wgpu.BindGroupLayout
	bind       *wgpu.BindGroup
	shader     *wgpu.ShaderModule
	layout     *wgpu.PipelineLayout
	pipeline   *wgpu.RenderPipeline
}

func (g *gpuRenderer) ensureMipGen(dev *wgpu.Device, format gputypes.TextureFormat) (err error) {
	if g.mipGenShader == nil {
		g.mipGenShader, err = dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
			Label: "nt3d mipgen shader",
			WGSL:  proceduralMipShader,
		})
		if err != nil {
			return err
		}
	}
	if g.mipGenLayout == nil {
		g.mipGenLayout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
			Label:            "nt3d mipgen layout",
			BindGroupLayouts: []*wgpu.BindGroupLayout{g.sampledLayout},
		})
		if err != nil {
			return err
		}
	}
	if g.mipGenSampler == nil {
		g.mipGenSampler, err = dev.CreateSampler(&wgpu.SamplerDescriptor{
			Label:        "nt3d mipgen sampler",
			AddressModeU: gputypes.AddressModeClampToEdge,
			AddressModeV: gputypes.AddressModeClampToEdge,
			AddressModeW: gputypes.AddressModeClampToEdge,
			MagFilter:    gputypes.FilterModeLinear,
			MinFilter:    gputypes.FilterModeLinear,
			MipmapFilter: gputypes.FilterModeNearest,
			LodMinClamp:  0,
			LodMaxClamp:  1,
			Anisotropy:   1,
		})
		if err != nil {
			return err
		}
	}
	if g.mipSink == nil {
		g.mipSink, err = dev.CreateBuffer(&wgpu.BufferDescriptor{
			Label: "nt3d mip sink",
			Size:  256,
			Usage: wgpu.BufferUsageCopyDst,
		})
		if err != nil {
			return err
		}
	}
	idx := 0
	if format == gputypes.TextureFormatRGBA8UnormSrgb {
		idx = 1
	}
	if g.mipGenPipelines[idx] == nil {
		g.mipGenPipelines[idx], err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
			Label:       "nt3d mipgen pipeline",
			Layout:      g.mipGenLayout,
			Vertex:      wgpu.VertexState{Module: g.mipGenShader, EntryPoint: "vs_main"},
			Primitive:   gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone},
			Multisample: gputypes.DefaultMultisampleState(),
			Fragment: &wgpu.FragmentState{
				Module:     g.mipGenShader,
				EntryPoint: "fs_main",
				Targets:    []gputypes.ColorTargetState{{Format: format, WriteMask: gputypes.ColorWriteMaskAll}},
			},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (g *gpuRenderer) newProceduralTarget(dev *wgpu.Device, w, h int, srgb, repeat bool, aniso int) (*gpuTexture, error) {
	levels := 1
	for mw, mh := w, h; mw > 1 || mh > 1; mw, mh = max(1, mw/2), max(1, mh/2) {
		levels++
	}
	format := gputypes.TextureFormatRGBA8Unorm
	if srgb {
		format = gputypes.TextureFormatRGBA8UnormSrgb
	}
	if levels > 1 {
		if err := g.ensureMipGen(dev, format); err != nil {
			return nil, err
		}
	}
	anisoClamp := uint16(max(1, min(aniso, MaxAnisotropy)))
	mipFilter := gputypes.FilterModeNearest
	if levels > 1 || anisoClamp > 1 {
		mipFilter = gputypes.FilterModeLinear
	}
	addr := gputypes.AddressModeClampToEdge
	if repeat {
		addr = gputypes.AddressModeRepeat
	}
	t := &gpuTexture{format: format, width: w, height: h, levels: levels}
	var err error
	defer func() {
		if err != nil {
			t.release()
		}
	}()
	t.texture, err = dev.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "nt3d procedural target",
		Size:          wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
		MipLevelCount: uint32(levels),
		SampleCount:   1,
		Dimension:     gputypes.TextureDimension2D,
		Format:        format,
		Usage:         gputypes.TextureUsageTextureBinding | gputypes.TextureUsageRenderAttachment | gputypes.TextureUsageCopySrc | gputypes.TextureUsageCopyDst,
	})
	if err != nil {
		return nil, err
	}
	t.view, err = dev.CreateTextureView(t.texture, nil)
	if err != nil {
		return nil, err
	}
	t.sampler, err = dev.CreateSampler(&wgpu.SamplerDescriptor{
		Label:        "nt3d procedural sampler",
		AddressModeU: addr,
		AddressModeV: addr,
		AddressModeW: gputypes.AddressModeClampToEdge,
		MagFilter:    gputypes.FilterModeLinear,
		MinFilter:    gputypes.FilterModeLinear,
		MipmapFilter: mipFilter,
		LodMinClamp:  0,
		LodMaxClamp:  float32(levels - 1),
		Anisotropy:   anisoClamp,
	})
	if err != nil {
		return nil, err
	}
	t.group, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "nt3d procedural bind",
		Layout: g.sampledLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: t.view},
			{Binding: 1, Sampler: t.sampler},
		},
	})
	if err != nil {
		return nil, err
	}
	t.mipViews = make([]*wgpu.TextureView, levels)
	for level := range levels {
		t.mipViews[level], err = dev.CreateTextureView(t.texture, &wgpu.TextureViewDescriptor{
			Label:           "nt3d procedural mip view",
			BaseMipLevel:    uint32(level),
			MipLevelCount:   1,
			ArrayLayerCount: 1,
		})
		if err != nil {
			return nil, err
		}
	}
	if levels > 1 {
		t.mipGroups = make([]*wgpu.BindGroup, levels-1)
		t.mipScratch = make([]*wgpu.Texture, levels)
		t.scratchMip = make([]*wgpu.TextureView, levels)
		mw, mh := w, h
		for level := 0; level < levels; level++ {
			if level < levels-1 {
				t.mipGroups[level], err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
					Label:  "nt3d procedural mip bind",
					Layout: g.sampledLayout,
					Entries: []wgpu.BindGroupEntry{
						{Binding: 0, TextureView: t.mipViews[level]},
						{Binding: 1, Sampler: g.mipGenSampler},
					},
				})
				if err != nil {
					return nil, err
				}
			}
			if level > 0 {
				mw, mh = max(1, mw/2), max(1, mh/2)
				t.mipScratch[level], err = dev.CreateTexture(&wgpu.TextureDescriptor{
					Label:         "nt3d procedural mip scratch",
					Size:          wgpu.Extent3D{Width: uint32(mw), Height: uint32(mh), DepthOrArrayLayers: 1},
					MipLevelCount: 1,
					SampleCount:   1,
					Dimension:     gputypes.TextureDimension2D,
					Format:        format,
					Usage:         gputypes.TextureUsageRenderAttachment | gputypes.TextureUsageCopySrc,
				})
				if err != nil {
					return nil, err
				}
				t.scratchMip[level], err = dev.CreateTextureView(t.mipScratch[level], nil)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	return t, nil
}

func (g *gpuRenderer) prepareProcedural(dev *wgpu.Device, f Frame) error {
	if g.procedural == nil {
		g.procedural = make(map[proceduralKey]*proceduralTexture)
	}
	used := make(map[proceduralKey]bool)
	for _, geom := range f.Geometry {
		if mapped(geom) && len(geom.Indices) > 0 {
			m := geom.Material
			for _, key := range []proceduralKey{{baseMap(m), m.Repeat}, {normalMap(m), m.NormalRepeat}} {
				if key.src.shader != "" {
					used[key] = true
				}
			}
		}
	}
	// Recycle only snapshots absent from this frame. Multiple parameter variants
	// used by the same frame retain independent targets and uniforms.
	spare := make(map[proceduralKey]*proceduralTexture)
	for key, t := range g.procedural {
		if !used[key] || t.aniso != f.anisotropy() {
			spare[key] = t
			delete(g.procedural, key)
		}
	}
	defer func() {
		for _, t := range spare {
			g.releaseProcedural(t)
		}
	}()
	for key := range used {
		src := key.src
		if g.procedural[key] != nil {
			continue
		}
		for old, t := range spare {
			if old.repeat == key.repeat && old.src.data == src.data && old.src.shader == src.shader && old.src.width == src.width && old.src.height == src.height && t.aniso == f.anisotropy() {
				g.procedural[key] = t
				delete(spare, old)
				break
			}
		}
		if g.procedural[key] != nil {
			continue
		}
		t := &proceduralTexture{aniso: f.anisotropy()}
		var err error
		t.target, err = g.newProceduralTarget(dev, src.width, src.height, !src.data, key.repeat, t.aniso)
		if err != nil {
			return err
		}
		quad := proceduralQuad
		format := gputypes.TextureFormatRGBA8UnormSrgb
		if src.data {
			format = gputypes.TextureFormatRGBA8Unorm
			quad = strings.Replace(quad, "return vec4<f32>(nt3d_decode(clamp(c.rgb, vec3<f32>(0.0), vec3<f32>(1.0))), c.a);", "return vec4<f32>(clamp(c.rgb, vec3<f32>(0.0), vec3<f32>(1.0)), 1.0);", 1)
		}
		t.shader, err = dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "nt3d procedural texture", WGSL: src.shader + quad})
		if err == nil {
			t.bindLayout, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageFragment, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}}}})
		}
		if err == nil {
			t.uniform, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: 16, Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst})
		}
		if err == nil {
			t.bind, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: t.bindLayout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: t.uniform, Size: 16}}})
		}
		if err == nil {
			t.layout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{t.bindLayout}})
		}
		if err == nil {
			t.pipeline, err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
				Layout:      t.layout,
				Vertex:      wgpu.VertexState{Module: t.shader, EntryPoint: "nt3d_texture_vertex"},
				Fragment:    &wgpu.FragmentState{Module: t.shader, EntryPoint: "nt3d_texture_fragment", Targets: []gputypes.ColorTargetState{{Format: format, WriteMask: gputypes.ColorWriteMaskAll}}},
				Primitive:   gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone},
				Multisample: gputypes.DefaultMultisampleState(),
			})
		}
		if err != nil {
			g.releaseProcedural(t)
			return err
		}
		g.procedural[key] = t
	}
	return nil
}

func (g *gpuRenderer) drawProcedural(dev *wgpu.Device, enc *wgpu.CommandEncoder) error {
	for key, t := range g.procedural {
		src := key.src
		var data [16]byte
		for i, v := range src.parameters {
			binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(v))
		}
		if err := dev.Queue().WriteBuffer(t.uniform, 0, data[:]); err != nil {
			return err
		}
		if t.target.written {
			transitionTexture(enc, t.target.texture, gputypes.TextureUsageTextureBinding, generalTextureLayout)
		}
		pass, err := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{
			ColorAttachments: []wgpu.RenderPassColorAttachment{{
				View:    t.target.mipViews[0],
				LoadOp:  gputypes.LoadOpClear,
				StoreOp: gputypes.StoreOpStore,
			}},
		})
		if err != nil {
			return err
		}
		pass.SetPipeline(t.pipeline)
		pass.SetBindGroup(0, t.bind, nil)
		pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: 1})
		if err := pass.End(); err != nil {
			return err
		}
		transitionTexture(enc, t.target.texture, generalTextureLayout, gputypes.TextureUsageTextureBinding)
		if err := g.generateMipmaps(enc, t.target); err != nil {
			return err
		}
		t.target.written = true
		if t.target.levels > 1 {
			t.target.mipsFilled = true
		}
	}
	return nil
}

func (g *gpuRenderer) generateMipmaps(enc *wgpu.CommandEncoder, t *gpuTexture) error {
	if t.levels <= 1 {
		return nil
	}
	idx := 0
	if t.format == gputypes.TextureFormatRGBA8UnormSrgb {
		idx = 1
	}
	pipeline := g.mipGenPipelines[idx]
	var previous gputypes.TextureUsage
	if t.mipsFilled {
		previous = gputypes.TextureUsageTextureBinding
	}
	mw, mh := t.width, t.height
	for level := 1; level < t.levels; level++ {
		mw, mh = max(1, mw/2), max(1, mh/2)
		pass, err := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{
			Label: "nt3d mipgen pass",
			ColorAttachments: []wgpu.RenderPassColorAttachment{{
				View:    t.scratchMip[level],
				LoadOp:  gputypes.LoadOpClear,
				StoreOp: gputypes.StoreOpStore,
			}},
		})
		if err != nil {
			return err
		}
		pass.SetPipeline(pipeline)
		pass.SetBindGroup(0, t.mipGroups[level-1], nil)
		pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: 1})
		if err := pass.End(); err != nil {
			return err
		}
		transitionTexture(enc, t.mipScratch[level], gputypes.TextureUsageRenderAttachment, gputypes.TextureUsageCopySrc)
		transitionTextureLevel(enc, t.texture, level, previous, gputypes.TextureUsageCopyDst)
		enc.CopyTextureToTexture(t.mipScratch[level], t.texture, []wgpu.TextureCopy{{
			Source:      wgpu.ImageCopyTexture{Texture: t.mipScratch[level]},
			Destination: wgpu.ImageCopyTexture{Texture: t.texture, MipLevel: uint32(level)},
			Size:        wgpu.Extent3D{Width: uint32(mw), Height: uint32(mh), DepthOrArrayLayers: 1},
		}})
		transitionTextureLevel(enc, t.texture, level, gputypes.TextureUsageCopyDst, gputypes.TextureUsageTextureBinding)
	}
	last := t.levels - 1
	transitionTextureLevel(enc, t.texture, last, gputypes.TextureUsageTextureBinding, gputypes.TextureUsageCopySrc)
	enc.CopyTextureToBuffer(t.texture, g.mipSink, []wgpu.BufferTextureCopy{{
		BufferLayout: wgpu.ImageDataLayout{BytesPerRow: 256, RowsPerImage: 1},
		TextureBase:  wgpu.ImageCopyTexture{Texture: t.texture, MipLevel: uint32(last)},
		Size:         wgpu.Extent3D{Width: 1, Height: 1, DepthOrArrayLayers: 1},
	}})
	transitionTextureLevel(enc, t.texture, last, gputypes.TextureUsageCopySrc, gputypes.TextureUsageTextureBinding)
	return nil
}

func (g *gpuRenderer) releaseProcedural(t *proceduralTexture) {
	if t.pipeline != nil {
		t.pipeline.Release()
	}
	if t.layout != nil {
		t.layout.Release()
	}
	if t.bind != nil {
		t.bind.Release()
	}
	if t.uniform != nil {
		t.uniform.Release()
	}
	if t.bindLayout != nil {
		t.bindLayout.Release()
	}
	if t.shader != nil {
		t.shader.Release()
	}
	if t.target != nil {
		t.target.release()
		t.target = nil
	}
}
