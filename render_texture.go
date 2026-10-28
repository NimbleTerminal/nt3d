// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"image"
)

type gpuTexture struct {
	texture    *wgpu.Texture
	view       *wgpu.TextureView
	sampler    *wgpu.Sampler
	group      *wgpu.BindGroup
	mipViews   []*wgpu.TextureView
	mipGroups  []*wgpu.BindGroup
	mipScratch []*wgpu.Texture
	scratchMip []*wgpu.TextureView
	format     gputypes.TextureFormat
	width      int
	height     int
	levels     int
	written    bool
	mipsFilled bool
}

func (t *gpuTexture) BindGroup() *wgpu.BindGroup { return t.group }

func (t *gpuTexture) release() {
	if t == nil {
		return
	}
	for _, v := range t.scratchMip {
		if v != nil {
			v.Release()
		}
	}
	for _, tex := range t.mipScratch {
		if tex != nil {
			tex.Release()
		}
	}
	for _, g := range t.mipGroups {
		if g != nil {
			g.Release()
		}
	}
	for _, v := range t.mipViews {
		if v != nil {
			v.Release()
		}
	}
	if t.group != nil {
		t.group.Release()
	}
	if t.sampler != nil {
		t.sampler.Release()
	}
	if t.view != nil {
		t.view.Release()
	}
	if t.texture != nil {
		t.texture.Release()
	}
	*t = gpuTexture{}
}

func (g *gpuRenderer) uploadTextureLevels(dev *wgpu.Device, levels []*image.NRGBA, srgb bool, addrU, addrV gputypes.AddressMode, aniso int) (*gpuTexture, error) {
	w, h := levels[0].Bounds().Dx(), levels[0].Bounds().Dy()
	format := gputypes.TextureFormatRGBA8Unorm
	if srgb {
		format = gputypes.TextureFormatRGBA8UnormSrgb
	}
	anisoClamp := uint16(max(1, min(aniso, MaxAnisotropy)))
	mipFilter := gputypes.FilterModeNearest
	if len(levels) > 1 || anisoClamp > 1 {
		mipFilter = gputypes.FilterModeLinear
	}
	t := &gpuTexture{format: format, width: w, height: h, levels: len(levels)}
	var err error
	defer func() {
		if err != nil {
			t.release()
		}
	}()
	t.texture, err = dev.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "nt3d texture",
		Size:          wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
		MipLevelCount: uint32(len(levels)),
		SampleCount:   1,
		Dimension:     gputypes.TextureDimension2D,
		Format:        format,
		Usage:         gputypes.TextureUsageTextureBinding | gputypes.TextureUsageCopyDst,
	})
	if err != nil {
		return nil, err
	}
	for i, level := range levels {
		lw, lh := level.Bounds().Dx(), level.Bounds().Dy()
		pix := level.Pix
		if level.Stride != lw*4 || level.Rect.Min != (image.Point{}) {
			pix = make([]byte, lw*lh*4)
			for y := range lh {
				srcOff := level.PixOffset(level.Rect.Min.X, level.Rect.Min.Y+y)
				copy(pix[y*lw*4:(y+1)*lw*4], level.Pix[srcOff:srcOff+lw*4])
			}
		} else {
			pix = pix[:lw*lh*4]
		}
		if err = dev.Queue().WriteTexture(
			&wgpu.ImageCopyTexture{Texture: t.texture, MipLevel: uint32(i), Aspect: gputypes.TextureAspectAll},
			pix,
			&wgpu.ImageDataLayout{BytesPerRow: uint32(lw * 4), RowsPerImage: uint32(lh)},
			&wgpu.Extent3D{Width: uint32(lw), Height: uint32(lh), DepthOrArrayLayers: 1},
		); err != nil {
			return nil, err
		}
	}
	t.view, err = dev.CreateTextureView(t.texture, nil)
	if err != nil {
		return nil, err
	}
	t.sampler, err = dev.CreateSampler(&wgpu.SamplerDescriptor{
		Label:        "nt3d sampler",
		AddressModeU: addrU,
		AddressModeV: addrV,
		AddressModeW: gputypes.AddressModeClampToEdge,
		MagFilter:    gputypes.FilterModeLinear,
		MinFilter:    gputypes.FilterModeLinear,
		MipmapFilter: mipFilter,
		LodMinClamp:  0,
		LodMaxClamp:  float32(len(levels)),
		Anisotropy:   anisoClamp,
	})
	if err != nil {
		return nil, err
	}
	t.group, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "nt3d texture bind",
		Layout: g.sampledLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: t.view},
			{Binding: 1, Sampler: t.sampler},
		},
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

func releaseBatch(b gpuBatch) {
	if b.bind != nil {
		b.bind.Release()
	}
	if b.buffer != nil {
		b.buffer.Release()
	}
}

func (g *gpuRenderer) setupTextures(dev *wgpu.Device, sampledLayout *wgpu.BindGroupLayout) (err error) {
	g.textureLayout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{g.bindLayout, g.shadowLayout, sampledLayout, sampledLayout}})
	if err != nil {
		return err
	}
	pipeline := func(label, fragment string) (*wgpu.RenderPipeline, error) {
		return dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: label, Layout: g.textureLayout, Vertex: wgpu.VertexState{Module: g.shader, EntryPoint: "vs_texture"}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeBack}, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual}, Multisample: gputypes.DefaultMultisampleState(), Fragment: &wgpu.FragmentState{Module: g.shader, EntryPoint: fragment, Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}}})
	}
	g.texturePipeline, err = pipeline("textured mesh", "fs_texture")
	if err == nil {
		g.cutoutPipeline, err = pipeline("cutout mesh", "fs_cutout")
	}
	for i, entry := range []string{"vs_texture_back", "vs_texture"} {
		if err == nil {
			g.textureBlends[i], err = blendPipeline(dev, g.textureLayout, g.shader, entry, "fs_blend_texture", i)
		}
	}
	if err == nil {
		err = g.setupTexturedShadowPipelines(dev)
	}
	if err != nil {
		for _, p := range []**wgpu.RenderPipeline{&g.texturePipeline, &g.cutoutPipeline, &g.textureBlends[0], &g.textureBlends[1]} {
			if *p != nil {
				(*p).Release()
				*p = nil
			}
		}
		g.textureLayout.Release()
		g.textureLayout = nil
		return err
	}
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
	hasTextures := false
	for _, geom := range f.Geometry {
		var keys []textureKey
		if mapped(geom) && len(geom.Indices) > 0 {
			keys = append(keys, baseKey(geom.Material, f.anisotropy()), textureKey{src: normalMap(geom.Material), repeat: geom.Material.NormalRepeat, aniso: f.anisotropy()})
		}
		if geom.PointStyle.Texture != nil && len(geom.Points) > 0 {
			keys = append(keys, textureKey{src: geom.PointStyle.Texture, aniso: f.anisotropy()})
		}
		for _, key := range keys {
			hasTextures = true
			src := key.src
			if src.shader != "" {
				continue
			}
			used[key] = true
			t := g.textures[key]
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
				if t, err = g.uploadTextureLevels(dev, chain, !src.data, addr, addr, key.aniso); err != nil {
					return err
				}
				g.textures[key] = t
				g.textureUploads++
			}
		}
	}
	for key, t := range g.textures {
		if !used[key] {
			t.release()
			delete(g.textures, key)
		}
	}
	if hasTextures && (g.texturePipeline == nil || g.spriteLayout == nil) {
		if g.texturePipeline == nil {
			if err := g.setupTextures(dev, g.sampledLayout); err != nil {
				return err
			}
		}
		if g.spriteLayout == nil {
			return g.setupSprites(dev, g.sampledLayout)
		}
	}
	return nil
}

func (g *gpuRenderer) setupSprites(dev *wgpu.Device, sampled *wgpu.BindGroupLayout) (err error) {
	defer func() {
		if err != nil {
			for i, p := range g.spritePipelines {
				if p != nil {
					p.Release()
					g.spritePipelines[i] = nil
				}
			}
			if g.spriteLayout != nil {
				g.spriteLayout.Release()
				g.spriteLayout = nil
			}
		}
	}()
	g.spriteLayout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{g.pointBindLayout, g.shadowLayout, sampled}})
	if err != nil {
		return err
	}
	g.spritePipelines[0], err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: "opaque sprites", Layout: g.spriteLayout, Vertex: wgpu.VertexState{Module: g.shader, EntryPoint: "vs_sprite"}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone}, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual}, Multisample: gputypes.DefaultMultisampleState(), Fragment: &wgpu.FragmentState{Module: g.shader, EntryPoint: "fs_sprite", Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}}})
	if err != nil {
		return err
	}
	g.spritePipelines[1], err = pointPipeline(dev, g.spriteLayout, g.shader, "vs_sprite_sorted", "fs_sprite_alpha", false)
	if err != nil {
		return err
	}
	g.spritePipelines[2], err = pointPipeline(dev, g.spriteLayout, g.shader, "vs_sprite", "fs_sprite_add", true)
	return err
}

// drawSprite draws a geometry's sprites unsorted: opaque or additive. Its
// points bind with the empty run at offset 0 of the run table.
func (g *gpuRenderer) drawSprite(pass *wgpu.RenderPassEncoder, f Frame, d gpuDraw) {
	style := f.Geometry[d.index].PointStyle
	pass.SetPipeline(g.spritePipelines[style.Blend])
	pass.SetBindGroup(0, d.set.binds[0], noRunOffset)
	pass.SetBindGroup(2, g.textures[textureKey{src: style.Texture, aniso: f.anisotropy()}].BindGroup(), nil)
	pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: d.geom.batches[0].count})
}

// noRunOffset is the dynamic offset of unsorted points' stand-in run. It is
// shared and never written.
var noRunOffset = []uint32{0}
