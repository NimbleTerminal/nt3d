// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"nimbleterminal.dev/ntgpuimage"
)

// Explicit UVs avoid naga's Metal indexed-array arithmetic issue.
const proceduralQuad = `
struct NT3DTextureOut { @builtin(position) pos: vec4<f32>, @location(0) uv: vec2<f32> };
@vertex fn nt3d_texture_vertex(@builtin(vertex_index) i: u32) -> NT3DTextureOut {
 let pos = array<vec2<f32>, 6>(vec2<f32>(-1,1), vec2<f32>(1,1), vec2<f32>(1,-1), vec2<f32>(-1,1), vec2<f32>(1,-1), vec2<f32>(-1,-1));
 let uv = array<vec2<f32>, 6>(vec2<f32>(0,0), vec2<f32>(1,0), vec2<f32>(1,1), vec2<f32>(0,0), vec2<f32>(1,1), vec2<f32>(0,1));
 var out: NT3DTextureOut;
 out.pos = vec4<f32>(pos[i], 0, 1);
 out.uv = uv[i];
 return out;
}
@fragment fn nt3d_texture_fragment(in: NT3DTextureOut) -> @location(0) vec4<f32> {
 return nt3dTexture(in.uv);
}
`

type proceduralTexture struct {
	target   *ntgpuimage.Texture
	shader   *wgpu.ShaderModule
	layout   *wgpu.PipelineLayout
	pipeline *wgpu.RenderPipeline
}

// Called outside the render callback: NewTexture and Close use the executor.
func (g *gpuRenderer) prepareProcedural(f Frame) error {
	if g.procedural == nil {
		g.procedural = make(map[*Texture]*proceduralTexture)
	}
	used := make(map[*Texture]bool)
	for _, geom := range f.Geometry {
		if textured(geom) && len(geom.Indices) > 0 && geom.Material.Texture.shader != "" {
			used[geom.Material.Texture] = true
		}
	}
	for src, t := range g.procedural {
		if !used[src] {
			g.releaseProcedural(t)
			delete(g.procedural, src)
		}
	}
	for src := range used {
		if g.procedural[src] != nil {
			continue
		}
		t := &proceduralTexture{}
		var err error
		t.target, err = g.r.NewTexture(src.width, src.height, ntgpuimage.TextureOptions{RenderTarget: true})
		if err != nil {
			return err
		}
		err = g.r.Do(func(dev *wgpu.Device) (err error) {
			t.shader, err = dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "nt3d procedural texture", WGSL: src.shader + proceduralQuad})
			if err != nil {
				return err
			}
			t.layout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{})
			if err != nil {
				return err
			}
			t.pipeline, err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
				Layout:      t.layout,
				Vertex:      wgpu.VertexState{Module: t.shader, EntryPoint: "nt3d_texture_vertex"},
				Fragment:    &wgpu.FragmentState{Module: t.shader, EntryPoint: "nt3d_texture_fragment", Targets: []gputypes.ColorTargetState{{Format: gputypes.TextureFormatRGBA8Unorm, WriteMask: gputypes.ColorWriteMaskAll}}},
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
	for _, t := range g.procedural {
		if err := f.RenderTo(t.target, func(target *ntgpuimage.Frame) error {
			pass, err := target.Encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: target.Color, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore}}})
			if err != nil {
				return err
			}
			pass.SetPipeline(t.pipeline)
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
		if t.shader != nil {
			t.shader.Release()
		}
		return nil
	})
	if t.target != nil {
		_ = t.target.Close()
	}
}
