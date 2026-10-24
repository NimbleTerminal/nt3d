// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"encoding/binary"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"math"
	"nimbleterminal.dev/ntgpuimage"
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

type proceduralTexture struct {
	target     *ntgpuimage.Texture
	aniso      int // The target's sampler's.
	uniform    *wgpu.Buffer
	bindLayout *wgpu.BindGroupLayout
	bind       *wgpu.BindGroup
	shader     *wgpu.ShaderModule
	layout     *wgpu.PipelineLayout
	pipeline   *wgpu.RenderPipeline
}

// Called outside the render callback: NewTexture and Close use the executor.
func (g *gpuRenderer) prepareProcedural(f Frame) error {
	if g.procedural == nil {
		g.procedural = make(map[*Texture]*proceduralTexture)
	}
	used := make(map[*Texture]bool)
	for _, geom := range f.Geometry {
		if mapped(geom) && len(geom.Indices) > 0 {
			for _, src := range []*Texture{baseMap(geom.Material), normalMap(geom.Material)} {
				if src.shader != "" {
					used[src] = true
				}
			}
		}
	}
	// Recycle only snapshots absent from this frame. Multiple parameter variants
	// used by the same frame retain independent targets and uniforms.
	spare := make(map[*Texture]*proceduralTexture)
	for src, t := range g.procedural {
		if !used[src] || t.aniso != f.anisotropy() {
			spare[src] = t
			delete(g.procedural, src)
		}
	}
	defer func() {
		for _, t := range spare {
			g.releaseProcedural(t)
		}
	}()
	for src := range used {
		if g.procedural[src] != nil {
			continue
		}
		for old, t := range spare {
			if old.data == src.data && old.shader == src.shader && old.width == src.width && old.height == src.height && t.aniso == f.anisotropy() {
				g.procedural[src] = t
				delete(spare, old)
				break
			}
		}
		if g.procedural[src] != nil {
			continue
		}
		t := &proceduralTexture{aniso: f.anisotropy()}
		var err error
		options := ntgpuimage.TextureOptions{RenderTarget: true, SRGB: !src.data, Mipmaps: true, Anisotropy: uint16(t.aniso)}
		if src.data {
			options.AddressModeU = gputypes.AddressModeRepeat
			options.AddressModeV = gputypes.AddressModeRepeat
		}
		t.target, err = g.r.NewTexture(src.width, src.height, options)
		if err != nil {
			return err
		}
		err = g.r.Do(func(dev *wgpu.Device) (err error) {
			quad := proceduralQuad
			format := gputypes.TextureFormatRGBA8UnormSrgb
			if src.data {
				format = gputypes.TextureFormatRGBA8Unorm
				quad = strings.Replace(quad, "return vec4<f32>(nt3d_decode(clamp(c.rgb, vec3<f32>(0.0), vec3<f32>(1.0))), c.a);", "return vec4<f32>(clamp(c.rgb, vec3<f32>(0.0), vec3<f32>(1.0)), 1.0);", 1)
			}
			t.shader, err = dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "nt3d procedural texture", WGSL: src.shader + quad})
			if err != nil {
				return err
			}
			t.bindLayout, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageFragment, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}}}})
			if err != nil {
				return err
			}
			t.uniform, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: 16, Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst})
			if err != nil {
				return err
			}
			t.bind, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: t.bindLayout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: t.uniform, Size: 16}}})
			if err != nil {
				return err
			}
			t.layout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{t.bindLayout}})
			if err != nil {
				return err
			}
			t.pipeline, err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
				Layout:      t.layout,
				Vertex:      wgpu.VertexState{Module: t.shader, EntryPoint: "nt3d_texture_vertex"},
				Fragment:    &wgpu.FragmentState{Module: t.shader, EntryPoint: "nt3d_texture_fragment", Targets: []gputypes.ColorTargetState{{Format: format, WriteMask: gputypes.ColorWriteMaskAll}}},
				Primitive:   gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone},
				Multisample: gputypes.DefaultMultisampleState(),
			})
			return err
		})
		if err != nil {
			g.releaseProcedural(t)
			return err
		}
		g.procedural[src] = t
	}
	return nil
}

func (g *gpuRenderer) drawProcedural(f *ntgpuimage.Frame) error {
	for src, t := range g.procedural {
		var data [16]byte
		for i, v := range src.parameters {
			binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(v))
		}
		if err := f.Device.Queue().WriteBuffer(t.uniform, 0, data[:]); err != nil {
			return err
		}
		if err := f.RenderTo(t.target, func(target *ntgpuimage.Frame) error {
			pass, err := target.Encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: target.Color, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore}}})
			if err != nil {
				return err
			}
			pass.SetPipeline(t.pipeline)
			pass.SetBindGroup(0, t.bind, nil)
			pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: 1})
			return pass.End()
		}); err != nil {
			return err
		}
	}
	return nil
}

func (g *gpuRenderer) releaseProcedural(t *proceduralTexture) {
	_ = g.r.Do(func(*wgpu.Device) error {
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
		return nil
	})
	if t.target != nil {
		_ = t.target.Close()
	}
}
