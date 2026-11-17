// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"encoding/binary"
	"math"
	"strings"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
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

const procDownShader = `
@group(0) @binding(0) var srcTex: texture_2d<f32>;
@vertex fn vs_mip_down(@builtin(vertex_index) i: u32) -> @builtin(position) vec4<f32> {
 let p = vec2<f32>(f32((i << 1u) & 2u), f32(i & 2u));
 return vec4<f32>(p * 2.0 - 1.0, 0.0, 1.0);
}
@fragment fn fs_mip_down(@builtin(position) pos: vec4<f32>) -> @location(0) vec4<f32> {
 let hi = vec2<i32>(textureDimensions(srcTex)) - vec2<i32>(1);
 let base = vec2<i32>(floor(pos.xy)) * 2;
 let s0 = textureLoad(srcTex, clamp(base, vec2<i32>(0), hi), 0);
 let s1 = textureLoad(srcTex, clamp(base + vec2<i32>(1, 0), vec2<i32>(0), hi), 0);
 let s2 = textureLoad(srcTex, clamp(base + vec2<i32>(0, 1), vec2<i32>(0), hi), 0);
 let s3 = textureLoad(srcTex, clamp(base + vec2<i32>(1, 1), vec2<i32>(0), hi), 0);
 let aSum = s0.a + s1.a + s2.a + s3.a;
 var rgb = (s0.rgb + s1.rgb + s2.rgb + s3.rgb) * 0.25;
 if aSum > 1e-6 {
  rgb = (s0.rgb * s0.a + s1.rgb * s1.a + s2.rgb * s2.a + s3.rgb * s3.a) / aSum;
 }
 return vec4<f32>(rgb, aSum * 0.25);
}
`

// Procedural targets own their sampler, so addressing is part of their identity.
type proceduralKey struct {
	src    *Texture
	repeat bool
}

type proceduralMip struct {
	w, h     int
	tex      *wgpu.Texture
	view     *wgpu.TextureView
	downBind *wgpu.BindGroup
}

type proceduralTexture struct {
	sampled      *wgpu.Texture
	view         *wgpu.TextureView
	sampler      *wgpu.Sampler
	sampledReady bool
	mips         []proceduralMip
	aniso        int
	data         bool
	uniform      *wgpu.Buffer
	bindLayout   *wgpu.BindGroupLayout
	bind         *wgpu.BindGroup
	shader       *wgpu.ShaderModule
	layout       *wgpu.PipelineLayout
	pipeline     *wgpu.RenderPipeline
}

func (g *gpuRenderer) ensureProcDownPipeline(dev *wgpu.Device, data bool) (*wgpu.BindGroupLayout, *wgpu.RenderPipeline, error) {
	idx := 0
	format := gputypes.TextureFormatRGBA8UnormSrgb
	if data {
		idx = 1
		format = gputypes.TextureFormatRGBA8Unorm
	}
	if g.shared != nil {
		g.shared.mu.Lock()
		defer g.shared.mu.Unlock()
		if g.shared.procDownPipes[idx] != nil {
			g.procDownGroup = g.shared.procDownGroup
			g.procDownPipes[idx] = g.shared.procDownPipes[idx]
			return g.procDownGroup, g.procDownPipes[idx], nil
		}
		var err error
		if g.shared.procDownShader == nil {
			g.shared.procDownShader, err = dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "nt3d procedural mip", WGSL: procDownShader})
			if err != nil {
				return nil, nil, err
			}
		}
		if g.shared.procDownGroup == nil {
			g.shared.procDownGroup, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
				Entries: []gputypes.BindGroupLayoutEntry{{
					Binding:    0,
					Visibility: wgpu.ShaderStageFragment,
					Texture:    &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeUnfilterableFloat, ViewDimension: gputypes.TextureViewDimension2D},
				}},
			})
			if err != nil {
				return nil, nil, err
			}
		}
		if g.shared.procDownLayout == nil {
			g.shared.procDownLayout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{g.shared.procDownGroup}})
			if err != nil {
				return nil, nil, err
			}
		}
		pipe, err := dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
			Label:       "nt3d procedural mip down",
			Layout:      g.shared.procDownLayout,
			Vertex:      wgpu.VertexState{Module: g.shared.procDownShader, EntryPoint: "vs_mip_down"},
			Fragment:    &wgpu.FragmentState{Module: g.shared.procDownShader, EntryPoint: "fs_mip_down", Targets: []gputypes.ColorTargetState{{Format: format, WriteMask: gputypes.ColorWriteMaskAll}}},
			Primitive:   gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone},
			Multisample: gputypes.DefaultMultisampleState(),
		})
		if err != nil {
			return nil, nil, err
		}
		g.shared.procDownPipes[idx] = pipe
		g.procDownGroup = g.shared.procDownGroup
		g.procDownPipes[idx] = pipe
		return g.procDownGroup, pipe, nil
	}
	if g.procDownPipes[idx] != nil {
		return g.procDownGroup, g.procDownPipes[idx], nil
	}
	var err error
	if g.procDownShader == nil {
		if g.procDownShader, err = dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "nt3d procedural mip", WGSL: procDownShader}); err != nil {
			return nil, nil, err
		}
	}
	if g.procDownGroup == nil {
		g.procDownGroup, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
			Entries: []gputypes.BindGroupLayoutEntry{{
				Binding:    0,
				Visibility: wgpu.ShaderStageFragment,
				Texture:    &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeUnfilterableFloat, ViewDimension: gputypes.TextureViewDimension2D},
			}},
		})
		if err != nil {
			return nil, nil, err
		}
	}
	if g.procDownLayout == nil {
		if g.procDownLayout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{g.procDownGroup}}); err != nil {
			return nil, nil, err
		}
	}
	pipe, err := dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:       "nt3d procedural mip down",
		Layout:      g.procDownLayout,
		Vertex:      wgpu.VertexState{Module: g.procDownShader, EntryPoint: "vs_mip_down"},
		Fragment:    &wgpu.FragmentState{Module: g.procDownShader, EntryPoint: "fs_mip_down", Targets: []gputypes.ColorTargetState{{Format: format, WriteMask: gputypes.ColorWriteMaskAll}}},
		Primitive:   gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone},
		Multisample: gputypes.DefaultMultisampleState(),
	})
	if err != nil {
		return nil, nil, err
	}
	g.procDownPipes[idx] = pipe
	return g.procDownGroup, pipe, nil
}

func mipDims(w, h int) [][2]int {
	var dims [][2]int
	for mw, mh := max(1, w), max(1, h); ; {
		dims = append(dims, [2]int{mw, mh})
		if mw == 1 && mh == 1 {
			return dims
		}
		mw = max(1, mw>>1)
		mh = max(1, mh>>1)
	}
}

func (g *gpuRenderer) prepareProcedural(dev *wgpu.Device, f Frame) error {
	if g.procedural == nil {
		g.procedural = make(map[proceduralKey]*proceduralTexture)
	}
	used := make(map[proceduralKey]bool)
	for di := range f.Draws {
		geom := f.Draws[di].Geometry
		if mapped(geom) && len(geom.Indices) > 0 {
			m := materialOrZero(geom.Material)
			for _, key := range []proceduralKey{
				{baseMap(&m), m.Repeat},
				{normalMap(&m), m.NormalRepeat},
				{ormMap(&m), m.ORMRepeat},
				{emissiveMap(&m), m.EmissiveRepeat},
			} {
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
		t := &proceduralTexture{aniso: f.anisotropy(), data: src.data}
		addr := gputypes.AddressModeClampToEdge
		if key.repeat {
			addr = gputypes.AddressModeRepeat
		}
		quad := proceduralQuad
		format := gputypes.TextureFormatRGBA8UnormSrgb
		if src.data {
			format = gputypes.TextureFormatRGBA8Unorm
			quad = strings.Replace(quad, "return vec4<f32>(nt3d_decode(clamp(c.rgb, vec3<f32>(0.0), vec3<f32>(1.0))), c.a);", "return vec4<f32>(clamp(c.rgb, vec3<f32>(0.0), vec3<f32>(1.0)), 1.0);", 1)
		}
		dims := mipDims(src.width, src.height)
		var downGroup *wgpu.BindGroupLayout
		var err error
		if len(dims) > 1 {
			if downGroup, _, err = g.ensureProcDownPipeline(dev, src.data); err != nil {
				return err
			}
		}
		t.sampled, err = dev.CreateTexture(&wgpu.TextureDescriptor{
			Label:         "nt3d procedural sampled",
			Size:          wgpu.Extent3D{Width: uint32(src.width), Height: uint32(src.height), DepthOrArrayLayers: 1},
			MipLevelCount: uint32(len(dims)),
			SampleCount:   1,
			Dimension:     wgpu.TextureDimension2D,
			Format:        format,
			Usage:         gputypes.TextureUsageCopyDst | gputypes.TextureUsageTextureBinding,
		})
		if err == nil {
			t.view, err = dev.CreateTextureView(t.sampled, nil)
		}
		if err == nil {
			t.sampler, err = dev.CreateSampler(&wgpu.SamplerDescriptor{
				AddressModeU: addr,
				AddressModeV: addr,
				MagFilter:    gputypes.FilterModeLinear,
				MinFilter:    gputypes.FilterModeLinear,
				MipmapFilter: gputypes.FilterModeLinear,
				Anisotropy:   uint16(max(1, min(t.aniso, MaxAnisotropy))),
			})
		}
		if err == nil {
			t.mips = make([]proceduralMip, len(dims))
			for m, d := range dims {
				mip := proceduralMip{w: d[0], h: d[1]}
				mip.tex, err = dev.CreateTexture(&wgpu.TextureDescriptor{
					Label:         "nt3d procedural mip",
					Size:          wgpu.Extent3D{Width: uint32(d[0]), Height: uint32(d[1]), DepthOrArrayLayers: 1},
					MipLevelCount: 1,
					SampleCount:   1,
					Dimension:     wgpu.TextureDimension2D,
					Format:        format,
					Usage:         gputypes.TextureUsageRenderAttachment | gputypes.TextureUsageTextureBinding | gputypes.TextureUsageCopySrc,
				})
				if err != nil {
					break
				}
				mip.view, err = dev.CreateTextureView(mip.tex, nil)
				if err != nil {
					mip.tex.Release()
					break
				}
				if m > 0 {
					mip.downBind, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
						Layout:  downGroup,
						Entries: []wgpu.BindGroupEntry{{Binding: 0, TextureView: t.mips[m-1].view}},
					})
					if err != nil {
						mip.view.Release()
						mip.tex.Release()
						break
					}
				}
				t.mips[m] = mip
			}
		}
		if err == nil {
			t.shader, err = dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "nt3d procedural texture", WGSL: src.shader + quad})
		}
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
		pass, err := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{
			ColorAttachments: []wgpu.RenderPassColorAttachment{{
				View:    t.mips[0].view,
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
		var downPipe *wgpu.RenderPipeline
		if len(t.mips) > 1 {
			if _, downPipe, err = g.ensureProcDownPipeline(dev, t.data); err != nil {
				return err
			}
		}
		for m := 1; m < len(t.mips); m++ {
			enc.TransitionTextures([]wgpu.TextureBarrier{{
				Texture: t.mips[m-1].tex,
				Usage: wgpu.TextureUsageTransition{
					OldUsage: gputypes.TextureUsageRenderAttachment,
					NewUsage: gputypes.TextureUsageTextureBinding,
				},
			}})
			mpass, err := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{
				ColorAttachments: []wgpu.RenderPassColorAttachment{{
					View:    t.mips[m].view,
					LoadOp:  gputypes.LoadOpClear,
					StoreOp: gputypes.StoreOpStore,
				}},
			})
			if err != nil {
				return err
			}
			mpass.SetPipeline(downPipe)
			mpass.SetBindGroup(0, t.mips[m].downBind, nil)
			mpass.Draw(gputypes.DrawArgs{VertexCount: 3, InstanceCount: 1})
			if err := mpass.End(); err != nil {
				return err
			}
		}
		preCopy := make([]wgpu.TextureBarrier, 0, len(t.mips)+1)
		for m := range t.mips {
			oldUsage := gputypes.TextureUsageTextureBinding
			if m == len(t.mips)-1 {
				oldUsage = gputypes.TextureUsageRenderAttachment
			}
			preCopy = append(preCopy, wgpu.TextureBarrier{
				Texture: t.mips[m].tex,
				Usage:   wgpu.TextureUsageTransition{OldUsage: oldUsage, NewUsage: gputypes.TextureUsageCopySrc},
			})
		}
		sampledOld := gputypes.TextureUsageNone
		if t.sampledReady {
			sampledOld = gputypes.TextureUsageTextureBinding
		}
		preCopy = append(preCopy, wgpu.TextureBarrier{
			Texture: t.sampled,
			Usage:   wgpu.TextureUsageTransition{OldUsage: sampledOld, NewUsage: gputypes.TextureUsageCopyDst},
		})
		enc.TransitionTextures(preCopy)
		for m, mip := range t.mips {
			enc.CopyTextureToTexture(mip.tex, t.sampled, []wgpu.TextureCopy{{
				Source:      wgpu.ImageCopyTexture{Texture: mip.tex, MipLevel: 0, Aspect: gputypes.TextureAspectAll},
				Destination: wgpu.ImageCopyTexture{Texture: t.sampled, MipLevel: uint32(m), Aspect: gputypes.TextureAspectAll},
				Size:        wgpu.Extent3D{Width: uint32(mip.w), Height: uint32(mip.h), DepthOrArrayLayers: 1},
			}})
		}
		postCopy := make([]wgpu.TextureBarrier, 0, len(t.mips)+1)
		for _, mip := range t.mips {
			postCopy = append(postCopy, wgpu.TextureBarrier{
				Texture: mip.tex,
				Usage:   wgpu.TextureUsageTransition{OldUsage: gputypes.TextureUsageCopySrc, NewUsage: gputypes.TextureUsageRenderAttachment},
			})
		}
		postCopy = append(postCopy, wgpu.TextureBarrier{
			Texture: t.sampled,
			Usage:   wgpu.TextureUsageTransition{OldUsage: gputypes.TextureUsageCopyDst, NewUsage: gputypes.TextureUsageTextureBinding},
		})
		enc.TransitionTextures(postCopy)
		t.sampledReady = true
	}
	return nil
}

func (g *gpuRenderer) releaseProcedural(t *proceduralTexture) {
	if t == nil {
		return
	}
	if t.view != nil {
		for mk, b := range g.materialBinds {
			if mk.usesView(t.view) {
				b.Release()
				delete(g.materialBinds, mk)
			}
		}
	}
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
	for _, mip := range t.mips {
		if mip.downBind != nil {
			mip.downBind.Release()
		}
		if mip.view != nil {
			mip.view.Release()
		}
		if mip.tex != nil {
			mip.tex.Release()
		}
	}
	t.mips = nil
	if t.sampler != nil {
		t.sampler.Release()
	}
	if t.view != nil {
		t.view.Release()
	}
	if t.sampled != nil {
		t.sampled.Release()
	}
	*t = proceduralTexture{}
}
