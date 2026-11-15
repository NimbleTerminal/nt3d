// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"fmt"
	"strings"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"

	nt3dsh "nimbleterminal.dev/nt3d/sh"
)

const defaultHookPreamble = `
struct NT3DVertexOutput {
 position: vec3<f32>,
 normal: vec3<f32>,
};
const nt3dHasDisplace: bool = false;
const nt3dHasMaterial: bool = false;
fn nt3d_displace(pos: vec3<f32>, norm: vec3<f32>, world: vec3<f32>, uv: vec2<f32>) -> NT3DVertexOutput {
 return NT3DVertexOutput(pos, norm);
}
fn nt3d_material(world: vec3<f32>, normal: vec3<f32>, view: vec3<f32>, uv: vec2<f32>, color: vec4<f32>) -> vec4<f32> {
 return color;
}
`

const hookCommonHeader = `
struct NT3DHookBlock {
 time: vec4<f32>,
 params: array<vec4<f32>, 16>,
};
struct NT3DHookUniforms {
 displace: NT3DHookBlock,
 material: NT3DHookBlock,
};
@group(3) @binding(0) var<uniform> nt3dHooks: NT3DHookUniforms;
struct NT3DVertexInput {
 position: vec3<f32>,
 normal: vec3<f32>,
 world: vec3<f32>,
 uv: vec2<f32>,
 time: f32,
};
struct NT3DVertexOutput {
 position: vec3<f32>,
 normal: vec3<f32>,
};
struct NT3DMaterialInput {
 world: vec3<f32>,
 normal: vec3<f32>,
 view: vec3<f32>,
 uv: vec2<f32>,
 color: vec4<f32>,
 time: f32,
};
fn nt3dDisplaceParam(i: u32) -> vec4<f32> {
 switch i {
  case 0u: { return nt3dHooks.displace.params[0]; }
  case 1u: { return nt3dHooks.displace.params[1]; }
  case 2u: { return nt3dHooks.displace.params[2]; }
  case 3u: { return nt3dHooks.displace.params[3]; }
  case 4u: { return nt3dHooks.displace.params[4]; }
  case 5u: { return nt3dHooks.displace.params[5]; }
  case 6u: { return nt3dHooks.displace.params[6]; }
  case 7u: { return nt3dHooks.displace.params[7]; }
  case 8u: { return nt3dHooks.displace.params[8]; }
  case 9u: { return nt3dHooks.displace.params[9]; }
  case 10u: { return nt3dHooks.displace.params[10]; }
  case 11u: { return nt3dHooks.displace.params[11]; }
  case 12u: { return nt3dHooks.displace.params[12]; }
  case 13u: { return nt3dHooks.displace.params[13]; }
  case 14u: { return nt3dHooks.displace.params[14]; }
  default: { return nt3dHooks.displace.params[15]; }
 }
}
fn nt3dMaterialParam(i: u32) -> vec4<f32> {
 switch i {
  case 0u: { return nt3dHooks.material.params[0]; }
  case 1u: { return nt3dHooks.material.params[1]; }
  case 2u: { return nt3dHooks.material.params[2]; }
  case 3u: { return nt3dHooks.material.params[3]; }
  case 4u: { return nt3dHooks.material.params[4]; }
  case 5u: { return nt3dHooks.material.params[5]; }
  case 6u: { return nt3dHooks.material.params[6]; }
  case 7u: { return nt3dHooks.material.params[7]; }
  case 8u: { return nt3dHooks.material.params[8]; }
  case 9u: { return nt3dHooks.material.params[9]; }
  case 10u: { return nt3dHooks.material.params[10]; }
  case 11u: { return nt3dHooks.material.params[11]; }
  case 12u: { return nt3dHooks.material.params[12]; }
  case 13u: { return nt3dHooks.material.params[13]; }
  case 14u: { return nt3dHooks.material.params[14]; }
  default: { return nt3dHooks.material.params[15]; }
 }
}
`

// hookUniformSize is two NT3DHookBlocks (time vec4 + 16 param vec4s each).
const hookUniformSize = 2 * (1 + MaxShaderParameters) * 16

type hookProgramKey struct {
	displace string
	material string
}

type hookPipelineKind int

const (
	hookPipeOpaque hookPipelineKind = iota
	hookPipeCutout
	hookPipeBlendBack
	hookPipeBlendFront
	hookPipeOutlineOpaque
	hookPipeOutlineCutout
	hookPipeOutlineBlend
	hookPipeOIT
	hookPipeOITOutline
)

func hookOutlinePipeKind(outlineKind int) hookPipelineKind {
	switch outlineKind {
	case 3:
		return hookPipeOutlineCutout
	case 4:
		return hookPipeOutlineBlend
	default:
		return hookPipeOutlineOpaque
	}
}

type gpuHookSampleSet struct {
	opaque     *wgpu.RenderPipeline
	cutout     *wgpu.RenderPipeline
	blends     [2]*wgpu.RenderPipeline
	outlines   [3]*wgpu.RenderPipeline
	oit        *wgpu.RenderPipeline
	oitOutline *wgpu.RenderPipeline
}

func (s *gpuHookSampleSet) release() {
	for _, p := range []*wgpu.RenderPipeline{
		s.opaque, s.cutout, s.blends[0], s.blends[1],
		s.outlines[0], s.outlines[1], s.outlines[2],
		s.oit, s.oitOutline,
	} {
		if p != nil {
			p.Release()
		}
	}
	*s = gpuHookSampleSet{}
}

type gpuHookProgram struct {
	shader  *wgpu.ShaderModule
	samples [2]gpuHookSampleSet
	shadows [2]*wgpu.RenderPipeline
}

func (p *gpuHookProgram) release() {
	if p == nil {
		return
	}
	for i := range p.samples {
		p.samples[i].release()
	}
	for i, sp := range p.shadows {
		if sp != nil {
			sp.Release()
			p.shadows[i] = nil
		}
	}
	if p.shader != nil {
		p.shader.Release()
		p.shader = nil
	}
}

type hookUniformKey struct {
	displaceTime   float32
	displaceParams [MaxShaderParameters][4]float32
	materialTime   float32
	materialParams [MaxShaderParameters][4]float32
}

type gpuHookBind struct {
	buffer *wgpu.Buffer
	bind   *wgpu.BindGroup
}

func (b *gpuHookBind) release() {
	if b == nil {
		return
	}
	if b.bind != nil {
		b.bind.Release()
		b.bind = nil
	}
	if b.buffer != nil {
		b.buffer.Release()
		b.buffer = nil
	}
}

func hookProgramKeyFor(geom *Geometry) (hookProgramKey, bool) {
	if geom == nil || !hasShaderHook(*geom) {
		return hookProgramKey{}, false
	}
	var key hookProgramKey
	if geom.Displacement != nil {
		key.displace = geom.Displacement.shader
	}
	if geom.Material != nil && geom.Material.Shader != nil {
		key.material = geom.Material.Shader.shader
	}
	return key, true
}

func hookUniformKeyFor(geom *Geometry) (hookUniformKey, bool) {
	if geom == nil || !hasShaderHook(*geom) {
		return hookUniformKey{}, false
	}
	var key hookUniformKey
	if geom.Displacement != nil {
		key.displaceTime = geom.Displacement.time
		key.displaceParams = geom.Displacement.params
	}
	if geom.Material != nil && geom.Material.Shader != nil {
		key.materialTime = geom.Material.Shader.time
		key.materialParams = geom.Material.Shader.params
	}
	return key, true
}

func encodeHookUniforms(dst []byte, k hookUniformKey) []byte {
	dst = appendFloats(dst[:0], k.displaceTime, 0, 0, 0)
	for _, p := range k.displaceParams {
		dst = appendFloats(dst, p[0], p[1], p[2], p[3])
	}
	dst = appendFloats(dst, k.materialTime, 0, 0, 0)
	for _, p := range k.materialParams {
		dst = appendFloats(dst, p[0], p[1], p[2], p[3])
	}
	return dst
}

func meshOnlySceneShader() string {
	const beginMarker = "// BEGIN_POINT_ONLY"
	const endMarker = "// END_POINT_ONLY"
	s := sceneShader
	var b strings.Builder
	b.Grow(len(s))
	for {
		i := strings.Index(s, beginMarker)
		if i < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])
		rest := s[i+len(beginMarker):]
		j := strings.Index(rest, endMarker)
		if j < 0 {
			break
		}
		s = rest[j+len(endMarker):]
	}
	return b.String()
}

func hookShaderSource(displaceSrc, materialSrc string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "const maxLights: u32 = %du;\nconst maxPointPixels: f32 = %d.0;\n", MaxLights, MaxPointPixels)
	b.WriteString(nt3dsh.WGSL())
	b.WriteString(hookCommonHeader)
	if displaceSrc != "" {
		b.WriteString("const nt3dHasDisplace: bool = true;\n")
		b.WriteString(displaceSrc)
		b.WriteString(`
fn nt3d_displace(pos: vec3<f32>, norm: vec3<f32>, world: vec3<f32>, uv: vec2<f32>) -> NT3DVertexOutput {
 return nt3dDisplace(NT3DVertexInput(pos, norm, world, uv, nt3dHooks.displace.time.x));
}
`)
	} else {
		b.WriteString(`
const nt3dHasDisplace: bool = false;
fn nt3d_displace(pos: vec3<f32>, norm: vec3<f32>, world: vec3<f32>, uv: vec2<f32>) -> NT3DVertexOutput {
 let _unused = nt3dHooks.displace.time.x;
 return NT3DVertexOutput(pos, norm);
}
`)
	}
	if materialSrc != "" {
		b.WriteString("const nt3dHasMaterial: bool = true;\n")
		b.WriteString(materialSrc)
		b.WriteString(`
fn nt3d_material(world: vec3<f32>, normal: vec3<f32>, view: vec3<f32>, uv: vec2<f32>, color: vec4<f32>) -> vec4<f32> {
 let out = nt3dMaterial(NT3DMaterialInput(world, normal, view, uv, color, nt3dHooks.material.time.x));
 return vec4<f32>(max(out.rgb, vec3<f32>(0.0)), clamp(out.a, 0.0, 1.0));
}
`)
	} else {
		b.WriteString(`
const nt3dHasMaterial: bool = false;
fn nt3d_material(world: vec3<f32>, normal: vec3<f32>, view: vec3<f32>, uv: vec2<f32>, color: vec4<f32>) -> vec4<f32> {
 let _unused = nt3dHooks.material.time.x;
 return color;
}
`)
	}
	b.WriteString(meshOnlySceneShader())
	return b.String()
}

func (g *gpuRenderer) ensureHookLayouts(dev *wgpu.Device) error {
	if g.hookLayout != nil && g.hookBindLayout != nil {
		return nil
	}
	if err := g.ensureTexturePipelines(dev); err != nil {
		return err
	}
	if g.shared != nil {
		g.shared.mu.Lock()
		defer g.shared.mu.Unlock()
		if g.shared.hookBindLayout == nil {
			bl, err := dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
				Label: "nt3d shader hooks",
				Entries: []gputypes.BindGroupLayoutEntry{{
					Binding:    0,
					Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment,
					Buffer: &gputypes.BufferBindingLayout{
						Type:           gputypes.BufferBindingTypeUniform,
						MinBindingSize: hookUniformSize,
					},
				}},
			})
			if err != nil {
				return err
			}
			g.shared.hookBindLayout = bl
		}
		if g.shared.hookLayout == nil {
			pl, err := dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
				Label:            "nt3d hook pipeline",
				BindGroupLayouts: []*wgpu.BindGroupLayout{g.bindLayout, g.shadowLayout, g.materialBindLayout, g.shared.hookBindLayout},
			})
			if err != nil {
				return err
			}
			g.shared.hookLayout = pl
		}
		g.hookBindLayout = g.shared.hookBindLayout
		g.hookLayout = g.shared.hookLayout
		return nil
	}
	var err error
	if g.hookBindLayout == nil {
		g.hookBindLayout, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
			Label: "nt3d shader hooks",
			Entries: []gputypes.BindGroupLayoutEntry{{
				Binding:    0,
				Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment,
				Buffer: &gputypes.BufferBindingLayout{
					Type:           gputypes.BufferBindingTypeUniform,
					MinBindingSize: hookUniformSize,
				},
			}},
		})
		if err != nil {
			return err
		}
	}
	if g.hookLayout == nil {
		g.hookLayout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
			Label:            "nt3d hook pipeline",
			BindGroupLayouts: []*wgpu.BindGroupLayout{g.bindLayout, g.shadowLayout, g.materialBindLayout, g.hookBindLayout},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (g *gpuRenderer) ensureHookProgram(dev *wgpu.Device, key hookProgramKey) (*gpuHookProgram, error) {
	if g.shared != nil {
		g.shared.mu.Lock()
		defer g.shared.mu.Unlock()
		if g.shared.hookPrograms == nil {
			g.shared.hookPrograms = make(map[hookProgramKey]*gpuHookProgram)
		}
		if prog := g.shared.hookPrograms[key]; prog != nil {
			return prog, nil
		}
		mod, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
			Label: "nt3d shader hook",
			WGSL:  hookShaderSource(key.displace, key.material),
		})
		if err != nil {
			return nil, err
		}
		prog := &gpuHookProgram{shader: mod}
		g.shared.hookPrograms[key] = prog
		return prog, nil
	}
	if g.hookPrograms == nil {
		g.hookPrograms = make(map[hookProgramKey]*gpuHookProgram)
	}
	if prog := g.hookPrograms[key]; prog != nil {
		return prog, nil
	}
	mod, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "nt3d shader hook",
		WGSL:  hookShaderSource(key.displace, key.material),
	})
	if err != nil {
		return nil, err
	}
	prog := &gpuHookProgram{shader: mod}
	g.hookPrograms[key] = prog
	return prog, nil
}

func (g *gpuRenderer) ensureHookPipeline(dev *wgpu.Device, prog *gpuHookProgram, kind hookPipelineKind) error {
	if g.shared != nil {
		g.shared.mu.Lock()
		defer g.shared.mu.Unlock()
	}
	idx := g.sampleIdx
	ms := g.multisample()
	s := &prog.samples[idx]
	var err error
	switch kind {
	case hookPipeOpaque:
		if s.opaque == nil {
			s.opaque, err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
				Label:        "hook textured mesh",
				Layout:       g.hookLayout,
				Vertex:       wgpu.VertexState{Module: prog.shader, EntryPoint: "vs_texture"},
				Primitive:    gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeBack},
				DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual},
				Multisample:  ms,
				Fragment:     &wgpu.FragmentState{Module: prog.shader, EntryPoint: "fs_texture", Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}},
			})
		}
	case hookPipeCutout:
		if s.cutout == nil {
			s.cutout, err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
				Label:        "hook cutout mesh",
				Layout:       g.hookLayout,
				Vertex:       wgpu.VertexState{Module: prog.shader, EntryPoint: "vs_texture"},
				Primitive:    gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeBack},
				DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual},
				Multisample:  ms,
				Fragment:     &wgpu.FragmentState{Module: prog.shader, EntryPoint: "fs_cutout", Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}},
			})
		}
	case hookPipeBlendBack:
		if s.blends[0] == nil {
			s.blends[0], err = blendPipelineMS(dev, g.hookLayout, prog.shader, "vs_texture_back", "fs_blend_texture", 0, ms)
		}
	case hookPipeBlendFront:
		if s.blends[1] == nil {
			s.blends[1], err = blendPipelineMS(dev, g.hookLayout, prog.shader, "vs_texture", "fs_blend_texture", 1, ms)
		}
	case hookPipeOutlineOpaque:
		if s.outlines[0] == nil {
			s.outlines[0], err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
				Label:        "hook outline hull",
				Layout:       g.hookLayout,
				Vertex:       wgpu.VertexState{Module: prog.shader, EntryPoint: "vs_outline_map"},
				Primitive:    gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeFront},
				DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual},
				Multisample:  ms,
				Fragment:     &wgpu.FragmentState{Module: prog.shader, EntryPoint: "fs_main", Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}},
			})
		}
	case hookPipeOutlineCutout:
		if s.outlines[1] == nil {
			s.outlines[1], err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
				Label:        "hook outline cutout",
				Layout:       g.hookLayout,
				Vertex:       wgpu.VertexState{Module: prog.shader, EntryPoint: "vs_outline_map"},
				Primitive:    gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeFront},
				DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual},
				Multisample:  ms,
				Fragment:     &wgpu.FragmentState{Module: prog.shader, EntryPoint: "fs_outline_cutout", Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}},
			})
		}
	case hookPipeOutlineBlend:
		if s.outlines[2] == nil {
			s.outlines[2], err = blendPipelineMS(dev, g.hookLayout, prog.shader, "vs_outline_map", "fs_outline_blend", 0, ms)
		}
	case hookPipeOIT:
		if s.oit == nil {
			s.oit, err = createOITPipeline(dev, g.hookLayout, prog.shader, "vs_texture", "fs_oit_texture", gputypes.CullModeNone, ms)
		}
	case hookPipeOITOutline:
		if s.oitOutline == nil {
			s.oitOutline, err = createOITPipeline(dev, g.hookLayout, prog.shader, "vs_outline_map", "fs_oit_outline_blend", gputypes.CullModeFront, ms)
		}
	}
	return err
}

func (g *gpuRenderer) ensureHookShadowPipeline(dev *wgpu.Device, prog *gpuHookProgram, cutout bool) error {
	if g.shared != nil {
		g.shared.mu.Lock()
		defer g.shared.mu.Unlock()
	}
	slot, fragment := 0, ""
	if cutout {
		slot, fragment = 1, "fs_shadow_cutout"
	}
	if prog.shadows[slot] != nil {
		return nil
	}
	d := &wgpu.RenderPipelineDescriptor{
		Label:        "hook vs_shadow_texture" + fragment,
		Layout:       g.hookLayout,
		Vertex:       wgpu.VertexState{Module: prog.shader, EntryPoint: "vs_shadow_texture"},
		Primitive:    gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeBack},
		DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual},
		Multisample:  gputypes.DefaultMultisampleState(),
	}
	if fragment != "" {
		d.Fragment = &wgpu.FragmentState{
			Module:     prog.shader,
			EntryPoint: fragment,
			Targets:    []gputypes.ColorTargetState{{Format: gputypes.TextureFormatR8Unorm, WriteMask: 0}},
		}
	}
	var err error
	prog.shadows[slot], err = dev.CreateRenderPipeline(d)
	return err
}

func (g *gpuRenderer) prepareHooks(dev *wgpu.Device, f Frame, drawn []bool) error {
	usedUniforms := make(map[hookUniformKey]bool)
	needHooks := false
	for i := range f.Geometry {
		geom := &f.Geometry[i]
		if !drawn[i] || len(geom.Indices) == 0 || !hasShaderHook(*geom) {
			continue
		}
		needHooks = true
		uk, _ := hookUniformKeyFor(geom)
		usedUniforms[uk] = true
	}

	var spare []*gpuHookBind
	for uk, hb := range g.hookBinds {
		if !usedUniforms[uk] {
			spare = append(spare, hb)
			delete(g.hookBinds, uk)
		}
	}
	defer func() {
		for _, hb := range spare {
			hb.release()
		}
	}()

	if !needHooks {
		return nil
	}
	if err := g.ensureHookLayouts(dev); err != nil {
		return err
	}
	if g.hookBinds == nil {
		g.hookBinds = make(map[hookUniformKey]*gpuHookBind)
	}

	for i := range f.Geometry {
		geom := &f.Geometry[i]
		if !drawn[i] || len(geom.Indices) == 0 {
			continue
		}
		pk, ok := hookProgramKeyFor(geom)
		if !ok {
			continue
		}
		prog, err := g.ensureHookProgram(dev, pk)
		if err != nil {
			return err
		}
		if transparent(geom) {
			if f.OIT {
				if err := g.ensureHookPipeline(dev, prog, hookPipeOIT); err != nil {
					return err
				}
				if geom.Outline.Width != 0 {
					if err := g.ensureHookPipeline(dev, prog, hookPipeOITOutline); err != nil {
						return err
					}
				}
			} else {
				if err := g.ensureHookPipeline(dev, prog, hookPipeBlendBack); err != nil {
					return err
				}
				if err := g.ensureHookPipeline(dev, prog, hookPipeBlendFront); err != nil {
					return err
				}
				if geom.Outline.Width != 0 {
					if err := g.ensureHookPipeline(dev, prog, hookPipeOutlineBlend); err != nil {
						return err
					}
				}
			}
		} else {
			cutout := geom.Material != nil && geom.Material.Cutout
			if cutout {
				if err := g.ensureHookPipeline(dev, prog, hookPipeCutout); err != nil {
					return err
				}
			} else {
				if err := g.ensureHookPipeline(dev, prog, hookPipeOpaque); err != nil {
					return err
				}
			}
			if g.shadowActive && !transmitting(geom) {
				if err := g.ensureHookShadowPipeline(dev, prog, cutout); err != nil {
					return err
				}
			}
			if geom.Outline.Width != 0 {
				if err := g.ensureHookPipeline(dev, prog, hookOutlinePipeKind(outlinePipelineKind(geom))); err != nil {
					return err
				}
			}
		}
	}

	var buf [hookUniformSize]byte
	raw := buf[:0]
	for uk := range usedUniforms {
		if g.hookBinds[uk] != nil {
			continue
		}
		raw = encodeHookUniforms(raw, uk)
		if n := len(spare); n > 0 {
			hb := spare[n-1]
			spare = spare[:n-1]
			if err := dev.Queue().WriteBuffer(hb.buffer, 0, raw); err != nil {
				hb.release()
				return err
			}
			g.hookBinds[uk] = hb
			continue
		}
		ubuf, err := dev.CreateBuffer(&wgpu.BufferDescriptor{
			Label: "nt3d hook uniform",
			Size:  hookUniformSize,
			Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
		})
		if err != nil {
			return err
		}
		if err := dev.Queue().WriteBuffer(ubuf, 0, raw); err != nil {
			ubuf.Release()
			return err
		}
		bind, err := dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
			Label:   "nt3d hook bind",
			Layout:  g.hookBindLayout,
			Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: ubuf, Size: hookUniformSize}},
		})
		if err != nil {
			ubuf.Release()
			return err
		}
		g.hookBinds[uk] = &gpuHookBind{buffer: ubuf, bind: bind}
	}
	return nil
}

func (g *gpuRenderer) lookupHookProgram(key hookProgramKey) *gpuHookProgram {
	if g.shared != nil {
		g.shared.mu.Lock()
		prog := g.shared.hookPrograms[key]
		g.shared.mu.Unlock()
		return prog
	}
	return g.hookPrograms[key]
}

func (g *gpuRenderer) hookPipelineFor(geom *Geometry, kind hookPipelineKind) *wgpu.RenderPipeline {
	pk, ok := hookProgramKeyFor(geom)
	if !ok {
		return nil
	}
	prog := g.lookupHookProgram(pk)
	if prog == nil {
		return nil
	}
	if g.shared != nil {
		g.shared.mu.Lock()
		defer g.shared.mu.Unlock()
	}
	s := &prog.samples[g.sampleIdx]
	switch kind {
	case hookPipeOpaque:
		return s.opaque
	case hookPipeCutout:
		return s.cutout
	case hookPipeBlendBack:
		return s.blends[0]
	case hookPipeBlendFront:
		return s.blends[1]
	case hookPipeOutlineOpaque:
		return s.outlines[0]
	case hookPipeOutlineCutout:
		return s.outlines[1]
	case hookPipeOutlineBlend:
		return s.outlines[2]
	case hookPipeOIT:
		return s.oit
	case hookPipeOITOutline:
		return s.oitOutline
	default:
		return nil
	}
}

func (g *gpuRenderer) setHookOrScenePipeline(pass *wgpu.RenderPassEncoder, geom *Geometry, fallback *wgpu.RenderPipeline, kind hookPipelineKind) {
	if hp := g.hookPipelineFor(geom, kind); hp != nil {
		g.setScenePipeline(pass, hp)
		return
	}
	g.setScenePipeline(pass, fallback)
}

func (g *gpuRenderer) hookOrShadowPipeline(geom *Geometry, fallback *wgpu.RenderPipeline, slot int) *wgpu.RenderPipeline {
	pk, ok := hookProgramKeyFor(geom)
	if !ok {
		return fallback
	}
	prog := g.lookupHookProgram(pk)
	if prog == nil {
		return fallback
	}
	if g.shared != nil {
		g.shared.mu.Lock()
		defer g.shared.mu.Unlock()
	}
	if prog.shadows[slot] != nil {
		return prog.shadows[slot]
	}
	return fallback
}

func (g *gpuRenderer) hookBindFor(geom *Geometry) *wgpu.BindGroup {
	uk, ok := hookUniformKeyFor(geom)
	if !ok {
		return nil
	}
	if hb := g.hookBinds[uk]; hb != nil {
		return hb.bind
	}
	return nil
}
