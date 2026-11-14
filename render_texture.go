// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"image"
	gpuimage "nimbleterminal.dev/ntgpuimage"
)

type gpuTexture struct {
	tex    *gpuimage.Texture
	shared *gpuShared
	key    textureKey
	refs   int
}

func (t *gpuTexture) BindGroup() *wgpu.BindGroup             { return t.tex.BindGroup() }
func (t *gpuTexture) BindGroupLayout() *wgpu.BindGroupLayout { return t.tex.BindGroupLayout() }

func (t *gpuTexture) release() {
	if t == nil {
		return
	}
	if t.shared != nil {
		t.shared.mu.Lock()
		t.refs--
		if t.refs > 0 {
			t.shared.mu.Unlock()
			return
		}
		delete(t.shared.textures, t.key)
		t.shared.mu.Unlock()
	}
	if t.tex != nil {
		_ = t.tex.Close()
		t.tex = nil
	}
	*t = gpuTexture{}
}

func (g *gpuRenderer) uploadTextureLevels(levels []*image.NRGBA, srgb bool, addrU, addrV gputypes.AddressMode, aniso int) (*gpuTexture, error) {
	imgs := make([]image.Image, len(levels))
	for i, level := range levels {
		imgs[i] = level
	}
	tex, err := g.shared.texR.UploadTextureLevels(imgs, gpuimage.TextureOptions{
		SRGB:         srgb,
		AddressModeU: addrU,
		AddressModeV: addrV,
		Anisotropy:   uint16(max(1, min(aniso, MaxAnisotropy))),
	})
	if err != nil {
		return nil, err
	}
	return &gpuTexture{tex: tex}, nil
}

func releaseBatch(b gpuBatch) {
	if b.bind != nil {
		b.bind.Release()
	}
	if b.buffer != nil {
		b.buffer.Release()
	}
}

func (g *gpuRenderer) setupTextureLayouts(dev *wgpu.Device, sampledLayout *wgpu.BindGroupLayout) (err error) {
	if g.textureLayout == nil {
		g.textureLayout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{g.bindLayout, g.shadowLayout, sampledLayout, sampledLayout}})
		if err != nil {
			return err
		}
		if err = g.setupTexturedShadowPipelines(dev); err != nil {
			g.textureLayout.Release()
			g.textureLayout = nil
			return err
		}
	}
	if g.spriteLayout == nil {
		g.spriteLayout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{g.pointBindLayout, g.shadowLayout, sampledLayout}})
		if err != nil {
			return err
		}
	}
	return nil
}

func (g *gpuRenderer) setupTexturePipelines(dev *wgpu.Device) (err error) {
	ms := g.multisample()
	var texPipe, cutPipe *wgpu.RenderPipeline
	var texBlends [2]*wgpu.RenderPipeline
	var sprites [3]*wgpu.RenderPipeline
	defer func() {
		if err != nil {
			for _, p := range []*wgpu.RenderPipeline{texPipe, cutPipe, texBlends[0], texBlends[1], sprites[0], sprites[1], sprites[2]} {
				if p != nil {
					p.Release()
				}
			}
		}
	}()
	pipeline := func(label, fragment string) (*wgpu.RenderPipeline, error) {
		return dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: label, Layout: g.textureLayout, Vertex: wgpu.VertexState{Module: g.shader, EntryPoint: "vs_texture"}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeBack}, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual}, Multisample: ms, Fragment: &wgpu.FragmentState{Module: g.shader, EntryPoint: fragment, Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}}})
	}
	if texPipe, err = pipeline("textured mesh", "fs_texture"); err != nil {
		return err
	}
	if cutPipe, err = pipeline("cutout mesh", "fs_cutout"); err != nil {
		return err
	}
	for i, entry := range []string{"vs_texture_back", "vs_texture"} {
		if texBlends[i], err = blendPipelineMS(dev, g.textureLayout, g.shader, entry, "fs_blend_texture", i, ms); err != nil {
			return err
		}
	}
	if sprites[0], err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: "opaque sprites", Layout: g.spriteLayout, Vertex: wgpu.VertexState{Module: g.shader, EntryPoint: "vs_sprite"}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone}, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual}, Multisample: ms, Fragment: &wgpu.FragmentState{Module: g.shader, EntryPoint: "fs_sprite", Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}}}); err != nil {
		return err
	}
	if sprites[1], err = pointPipelineMS(dev, g.spriteLayout, g.shader, "vs_sprite_sorted", "fs_sprite_alpha", false, ms); err != nil {
		return err
	}
	if sprites[2], err = pointPipelineMS(dev, g.spriteLayout, g.shader, "vs_sprite", "fs_sprite_add", true, ms); err != nil {
		return err
	}
	g.texturePipeline = texPipe
	g.cutoutPipeline = cutPipe
	g.textureBlends = texBlends
	g.spritePipelines = sprites
	idx := g.sampleIdx
	g.msaaSets[idx].texturePipeline = texPipe
	g.msaaSets[idx].cutoutPipeline = cutPipe
	g.msaaSets[idx].textureBlends = texBlends
	g.msaaSets[idx].spritePipelines = sprites
	return nil
}

// textureKey is an uploaded texture: an image texture, by its sampler's
// addressing, repeating or clamped, and anisotropy, and by its chain: a
// cutout uploads the texture's coverage-preserving chain, with no
// anisotropy, whose taps would average its alpha thin again; a FullSize
// material uploads level 0 alone, with no anisotropy, so the sampler takes
// one tap of it whatever the gradients say.
type textureKey struct {
	src              *Texture
	repeat           bool
	aniso            int
	cutout, fullSize bool
}

// baseKey is the upload a material's base texture needs in a frame of the
// given anisotropy.
func baseKey(m *Material, aniso int) textureKey {
	key := textureKey{src: baseMap(m), repeat: m.Repeat, aniso: aniso}
	switch {
	case m.FullSize:
		key.fullSize, key.aniso = true, 1
	case m.Cutout:
		key.cutout, key.aniso = true, 1
	}
	return key
}

// prepareTextures uploads any new image textures and releases unused ones.
func (g *gpuRenderer) prepareTextures(dev *wgpu.Device, f Frame) error {
	if g.textures == nil {
		g.textures = make(map[textureKey]*gpuTexture)
	}
	used := make(map[textureKey]bool)
	var sampledLayout *wgpu.BindGroupLayout
	for _, geom := range f.Geometry {
		var keys []textureKey
		if mapped(geom) && len(geom.Indices) > 0 {
			keys = append(keys, baseKey(geom.Material, f.anisotropy()), textureKey{src: normalMap(geom.Material), repeat: geom.Material.NormalRepeat, aniso: f.anisotropy()})
		}
		if geom.PointStyle.Texture != nil && len(geom.Points) > 0 {
			keys = append(keys, textureKey{src: geom.PointStyle.Texture, aniso: f.anisotropy()})
		}
		for _, key := range keys {
			src := key.src
			if src.shader != "" {
				sampledLayout = g.procedural[proceduralKey{src, key.repeat}].target.BindGroupLayout()
				continue
			}
			used[key] = true
			t := g.textures[key]
			if t == nil && g.shared != nil {
				g.shared.mu.Lock()
				if cached := g.shared.textures[key]; cached != nil {
					cached.refs++
					t = cached
					g.textures[key] = t
				}
				g.shared.mu.Unlock()
			}
			if t == nil {
				// Color textures decode sRGB; normal maps keep linear vector data.
				// Both use the software renderer's mip chain and hardware repeat.
				chain := src.levels()
				if key.cutout {
					chain = src.cutoutLevels()
				}
				if key.fullSize {
					chain = chain[:1]
				}
				addr := gputypes.AddressModeClampToEdge
				if key.repeat {
					addr = gputypes.AddressModeRepeat
				}
				var err error
				if t, err = g.uploadTextureLevels(chain, !src.data, addr, addr, key.aniso); err != nil {
					return err
				}
				if g.shared != nil {
					g.shared.mu.Lock()
					t.shared = g.shared
					t.key = key
					t.refs = 1
					g.shared.textures[key] = t
					g.shared.mu.Unlock()
				}
				g.textures[key] = t
				g.textureUploads++
			}
			sampledLayout = t.BindGroupLayout()
		}
	}
	for key, t := range g.textures {
		if !used[key] {
			t.release()
			delete(g.textures, key)
		}
	}
	if sampledLayout != nil && (g.textureLayout == nil || g.spriteLayout == nil || g.texturePipeline == nil || g.spritePipelines[0] == nil) {
		idx := g.sampleIdx
		if g.shared != nil {
			g.shared.mu.Lock()
			defer g.shared.mu.Unlock()
			if g.shared.textureLayout == nil || g.shared.spriteLayout == nil {
				g.textureLayout = g.shared.textureLayout
				g.spriteLayout = g.shared.spriteLayout
				if err := g.setupTextureLayouts(dev, sampledLayout); err != nil {
					return err
				}
				g.shared.textureLayout = g.textureLayout
				g.shared.spriteLayout = g.spriteLayout
				g.shared.shadowPipelines[2] = g.shadowPipelines[2]
				g.shared.shadowPipelines[3] = g.shadowPipelines[3]
			} else {
				g.textureLayout = g.shared.textureLayout
				g.spriteLayout = g.shared.spriteLayout
				g.shadowPipelines[2] = g.shared.shadowPipelines[2]
				g.shadowPipelines[3] = g.shared.shadowPipelines[3]
			}
			if g.shared.msaaSets[idx].texturePipeline == nil {
				if err := g.setupTexturePipelines(dev); err != nil {
					return err
				}
				g.shared.msaaSets[idx].texturePipeline = g.texturePipeline
				g.shared.msaaSets[idx].cutoutPipeline = g.cutoutPipeline
				g.shared.msaaSets[idx].textureBlends = g.textureBlends
				g.shared.msaaSets[idx].spritePipelines = g.spritePipelines
			} else {
				ps := &g.shared.msaaSets[idx]
				g.texturePipeline = ps.texturePipeline
				g.cutoutPipeline = ps.cutoutPipeline
				g.textureBlends = ps.textureBlends
				g.spritePipelines = ps.spritePipelines
				g.msaaSets[idx].texturePipeline = ps.texturePipeline
				g.msaaSets[idx].cutoutPipeline = ps.cutoutPipeline
				g.msaaSets[idx].textureBlends = ps.textureBlends
				g.msaaSets[idx].spritePipelines = ps.spritePipelines
			}
			return nil
		}
		if err := g.setupTextureLayouts(dev, sampledLayout); err != nil {
			return err
		}
		if g.texturePipeline == nil || g.spritePipelines[0] == nil {
			return g.setupTexturePipelines(dev)
		}
	}
	return nil
}

// drawSprite draws a geometry's sprites unsorted: opaque or additive. Its
// points bind with the empty run at offset 0 of the run table.
func (g *gpuRenderer) drawSprite(pass *wgpu.RenderPassEncoder, f Frame, d gpuDraw) {
	style := f.Geometry[d.index].PointStyle
	g.setScenePipeline(pass, g.spritePipelines[style.Blend])
	pass.SetBindGroup(0, d.set.binds[0], noRunOffset)
	pass.SetBindGroup(2, g.textures[textureKey{src: style.Texture, aniso: f.anisotropy()}].BindGroup(), nil)
	pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: d.geom.batches[0].count})
}

// noRunOffset is the dynamic offset of unsorted points' stand-in run. It is
// shared and never written.
var noRunOffset = []uint32{0}
