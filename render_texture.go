// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

type gpuTexture struct {
	tex        *wgpu.Texture
	view       *wgpu.TextureView
	sampler    *wgpu.Sampler
	spriteBind *wgpu.BindGroup
	shared     *gpuShared
	key        textureKey
	refs       int
}

func (t *gpuTexture) ensureSpriteBind(dev *wgpu.Device, layout *wgpu.BindGroupLayout) error {
	if t.shared != nil {
		t.shared.mu.Lock()
		defer t.shared.mu.Unlock()
	}
	if t.spriteBind != nil {
		return nil
	}
	bind, err := dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: layout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: t.view},
			{Binding: 1, Sampler: t.sampler},
		},
	})
	if err != nil {
		return err
	}
	t.spriteBind = bind
	return nil
}

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
	if t.spriteBind != nil {
		t.spriteBind.Release()
	}
	if t.view != nil {
		t.view.Release()
	}
	if t.sampler != nil {
		t.sampler.Release()
	}
	if t.tex != nil {
		t.tex.Release()
	}
	*t = gpuTexture{}
}

type materialBindKey struct {
	baseView, normalView, ormView, emissiveView, lightMapView *wgpu.TextureView
	baseSamp, normalSamp, ormSamp, emissiveSamp, lightMapSamp *wgpu.Sampler
}

func (k materialBindKey) usesView(v *wgpu.TextureView) bool {
	return k.baseView == v || k.normalView == v || k.ormView == v || k.emissiveView == v || k.lightMapView == v
}

func (g *gpuRenderer) slotBinding(key textureKey) (*wgpu.TextureView, *wgpu.Sampler) {
	if key.src.shader != "" {
		p := g.procedural[proceduralKey{key.src, key.repeat}]
		return p.view, p.sampler
	}
	t := g.textures[key]
	return t.view, t.sampler
}

func (g *gpuRenderer) materialKey(m *Material, aniso int) materialBindKey {
	var normRep, ormRep, emRep bool
	if m != nil {
		normRep, ormRep, emRep = m.NormalRepeat, m.ORMRepeat, m.EmissiveRepeat
	}
	bv, bs := g.slotBinding(baseKey(m, aniso))
	nv, ns := g.slotBinding(textureKey{src: normalMap(m), repeat: normRep, aniso: aniso})
	ov, os := g.slotBinding(textureKey{src: ormMap(m), repeat: ormRep, aniso: aniso})
	ev, es := g.slotBinding(textureKey{src: emissiveMap(m), repeat: emRep, aniso: aniso})
	lv, ls := g.slotBinding(textureKey{src: lightMapTexture(m), aniso: aniso})
	return materialBindKey{
		lightMapView: lv, lightMapSamp: ls,
		baseView: bv, normalView: nv, ormView: ov, emissiveView: ev,
		baseSamp: bs, normalSamp: ns, ormSamp: os, emissiveSamp: es,
	}
}

func uploadTextureLevels(dev *wgpu.Device, levels []*image.NRGBA, srgb bool, addrU, addrV gputypes.AddressMode, aniso int) (*gpuTexture, error) {
	w, h := levels[0].Rect.Dx(), levels[0].Rect.Dy()
	format := gputypes.TextureFormatRGBA8Unorm
	if srgb {
		format = gputypes.TextureFormatRGBA8UnormSrgb
	}
	tex, err := dev.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "nt3d texture",
		Size:          wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
		MipLevelCount: uint32(len(levels)),
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        format,
		Usage:         gputypes.TextureUsageTextureBinding | gputypes.TextureUsageCopyDst,
	})
	if err != nil {
		return nil, err
	}
	for m, level := range levels {
		lw, lh := level.Rect.Dx(), level.Rect.Dy()
		stride := level.Stride
		lsize := wgpu.Extent3D{Width: uint32(lw), Height: uint32(lh), DepthOrArrayLayers: 1}
		data := level.Pix[:(lh-1)*stride+lw*4]
		if err := dev.Queue().WriteTexture(
			&wgpu.ImageCopyTexture{Texture: tex, MipLevel: uint32(m)},
			data,
			&wgpu.ImageDataLayout{BytesPerRow: uint32(stride), RowsPerImage: uint32(lh)},
			&lsize,
		); err != nil {
			tex.Release()
			return nil, err
		}
	}
	view, err := dev.CreateTextureView(tex, nil)
	if err != nil {
		tex.Release()
		return nil, err
	}
	sampler, err := dev.CreateSampler(&wgpu.SamplerDescriptor{
		AddressModeU: addrU,
		AddressModeV: addrV,
		MagFilter:    gputypes.FilterModeLinear,
		MinFilter:    gputypes.FilterModeLinear,
		MipmapFilter: gputypes.FilterModeLinear,
		Anisotropy:   uint16(max(1, min(aniso, MaxAnisotropy))),
	})
	if err != nil {
		view.Release()
		tex.Release()
		return nil, err
	}
	return &gpuTexture{tex: tex, view: view, sampler: sampler}, nil
}

func releaseBatch(b gpuBatch) {
	if b.bind != nil {
		b.bind.Release()
	}
	if b.buffer != nil {
		b.buffer.Release()
	}
}

func sampledEntries(pairs int) []gputypes.BindGroupLayoutEntry {
	entries := make([]gputypes.BindGroupLayoutEntry, 0, pairs*2)
	for i := range pairs {
		entries = append(entries,
			gputypes.BindGroupLayoutEntry{
				Binding:    uint32(i * 2),
				Visibility: wgpu.ShaderStageFragment,
				Texture:    &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeFloat, ViewDimension: gputypes.TextureViewDimension2D},
			},
			gputypes.BindGroupLayoutEntry{
				Binding:    uint32(i*2 + 1),
				Visibility: wgpu.ShaderStageFragment,
				Sampler:    &gputypes.SamplerBindingLayout{Type: gputypes.SamplerBindingTypeFiltering},
			},
		)
	}
	return entries
}

func (g *gpuRenderer) setupTextureLayouts(dev *wgpu.Device) (err error) {
	if g.materialBindLayout == nil {
		if g.materialBindLayout, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Label: "nt3d material", Entries: sampledEntries(5)}); err != nil {
			return err
		}
	}
	if g.spriteBindLayout == nil {
		if g.spriteBindLayout, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Label: "nt3d sprite", Entries: sampledEntries(1)}); err != nil {
			return err
		}
	}
	if g.textureLayout == nil {
		g.textureLayout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{g.bindLayout, g.shadowLayout, g.materialBindLayout}})
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
		g.spriteLayout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{g.pointBindLayout, g.shadowLayout, g.spriteBindLayout}})
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
	if m == nil {
		return textureKey{src: whiteMap, aniso: aniso}
	}
	key := textureKey{src: baseMap(m), repeat: m.Repeat, aniso: aniso}
	switch {
	case m.FullSize:
		key.fullSize, key.aniso = true, 1
	case m.Cutout:
		key.cutout, key.aniso = true, 1
	}
	return key
}

func (g *gpuRenderer) ensureTexturePipelines(dev *wgpu.Device) error {
	idx := g.sampleIdx
	if g.shared != nil {
		g.shared.mu.Lock()
		defer g.shared.mu.Unlock()
		if g.shared.textureLayout == nil || g.shared.spriteLayout == nil {
			g.materialBindLayout = g.shared.materialBindLayout
			g.spriteBindLayout = g.shared.spriteBindLayout
			g.textureLayout = g.shared.textureLayout
			g.spriteLayout = g.shared.spriteLayout
			if err := g.setupTextureLayouts(dev); err != nil {
				return err
			}
			g.shared.materialBindLayout = g.materialBindLayout
			g.shared.spriteBindLayout = g.spriteBindLayout
			g.shared.textureLayout = g.textureLayout
			g.shared.spriteLayout = g.spriteLayout
			g.shared.shadowPipelines[2] = g.shadowPipelines[2]
			g.shared.shadowPipelines[3] = g.shadowPipelines[3]
		} else {
			g.materialBindLayout = g.shared.materialBindLayout
			g.spriteBindLayout = g.shared.spriteBindLayout
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
	if err := g.setupTextureLayouts(dev); err != nil {
		return err
	}
	if g.texturePipeline == nil || g.spritePipelines[0] == nil {
		return g.setupTexturePipelines(dev)
	}
	return nil
}

// prepareTextures uploads any new image textures, caches material bind groups,
// and releases unused ones.
func (g *gpuRenderer) prepareTextures(dev *wgpu.Device, f Frame) error {
	if g.textures == nil {
		g.textures = make(map[textureKey]*gpuTexture)
	}
	if g.materialBinds == nil {
		g.materialBinds = make(map[materialBindKey]*wgpu.BindGroup)
	}
	used := make(map[textureKey]bool)
	var spriteKeys []textureKey
	needTextured := false
	aniso := f.anisotropy()
	for _, geom := range f.Geometry {
		var keys []textureKey
		if mapped(geom) && len(geom.Indices) > 0 {
			m := materialOrZero(geom.Material)
			needTextured = true
			keys = append(keys,
				baseKey(geom.Material, aniso),
				textureKey{src: normalMap(&m), repeat: m.NormalRepeat, aniso: aniso},
				textureKey{src: ormMap(&m), repeat: m.ORMRepeat, aniso: aniso},
				textureKey{src: emissiveMap(&m), repeat: m.EmissiveRepeat, aniso: aniso},
				textureKey{src: lightMapTexture(&m), aniso: aniso},
			)
		}
		if geom.PointStyle.Texture != nil && len(geom.Points) > 0 {
			needTextured = true
			sk := textureKey{src: geom.PointStyle.Texture, aniso: aniso}
			keys = append(keys, sk)
			spriteKeys = append(spriteKeys, sk)
		}
		for _, key := range keys {
			src := key.src
			if src.shader != "" {
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
				var err error
				if len(src.hdr) > 0 {
					var tex *wgpu.Texture
					var view *wgpu.TextureView
					var sampler *wgpu.Sampler
					tex, view, sampler, err = uploadHDRLevels(dev, src.hdr, "nt3d lightmap", gputypes.AddressModeClampToEdge, key.aniso)
					if err != nil {
						return err
					}
					t = &gpuTexture{tex: tex, view: view, sampler: sampler}
				} else {
					// Color textures decode sRGB; normal and ORM maps keep linear data.
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
					if t, err = uploadTextureLevels(dev, chain, !src.data, addr, addr, key.aniso); err != nil {
						return err
					}
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
		}
	}
	if needTextured && (g.materialBindLayout == nil || g.spriteBindLayout == nil || g.textureLayout == nil || g.spriteLayout == nil || g.texturePipeline == nil || g.spritePipelines[0] == nil) {
		if err := g.ensureTexturePipelines(dev); err != nil {
			return err
		}
	}
	for _, sk := range spriteKeys {
		if err := g.textures[sk].ensureSpriteBind(dev, g.spriteBindLayout); err != nil {
			return err
		}
	}
	usedBinds := make(map[materialBindKey]bool)
	for _, geom := range f.Geometry {
		if mapped(geom) && len(geom.Indices) > 0 {
			mk := g.materialKey(geom.Material, aniso)
			usedBinds[mk] = true
			if g.materialBinds[mk] == nil {
				bind, err := dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
					Layout: g.materialBindLayout,
					Entries: []wgpu.BindGroupEntry{
						{Binding: 0, TextureView: mk.baseView},
						{Binding: 1, Sampler: mk.baseSamp},
						{Binding: 2, TextureView: mk.normalView},
						{Binding: 3, Sampler: mk.normalSamp},
						{Binding: 4, TextureView: mk.ormView},
						{Binding: 5, Sampler: mk.ormSamp},
						{Binding: 6, TextureView: mk.emissiveView},
						{Binding: 7, Sampler: mk.emissiveSamp},
						{Binding: 8, TextureView: mk.lightMapView},
						{Binding: 9, Sampler: mk.lightMapSamp},
					},
				})
				if err != nil {
					return err
				}
				g.materialBinds[mk] = bind
			}
		}
	}
	for mk, bind := range g.materialBinds {
		if !usedBinds[mk] {
			bind.Release()
			delete(g.materialBinds, mk)
		}
	}
	for key, t := range g.textures {
		if !used[key] {
			t.release()
			delete(g.textures, key)
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
	pass.SetBindGroup(2, g.textures[textureKey{src: style.Texture, aniso: f.anisotropy()}].spriteBind, nil)
	pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: d.geom.batches[0].count})
}

// noRunOffset is the dynamic offset of unsorted points' stand-in run. It is
// shared and never written.
var noRunOffset = []uint32{0}
