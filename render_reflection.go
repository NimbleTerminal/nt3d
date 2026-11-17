// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

type reflectionTexture struct {
	source   *Environment
	texture  *wgpu.Texture
	view     *wgpu.TextureView
	sampler  *wgpu.Sampler
	borrowed bool
}

func (r *reflectionTexture) release() {
	if !r.borrowed {
		if r.view != nil {
			r.view.Release()
		}
		if r.texture != nil {
			r.texture.Release()
		}
		if r.sampler != nil {
			r.sampler.Release()
		}
	}
	*r = reflectionTexture{}
}

// Reflection shares group 1 with shadow depth, keeping normal-mapped materials
// within WebGPU's four bind groups. A one-pixel fallback keeps bindings valid
// even when no environment is selected. Both shadow targets are rebound when
// the panorama changes, including replay of older frames.
func (g *gpuRenderer) updateReflection(dev *wgpu.Device, e *Environment) (err error) {
	if g.reflection.texture != nil && g.reflection.source == e {
		return nil
	}
	r := reflectionTexture{source: e}
	defer func() {
		if err != nil {
			r.release()
		}
	}()
	if e != nil && g.environmentTexture != nil {
		r.texture = g.environmentTexture.texture
		r.view = g.environmentTexture.view
		r.sampler = g.environmentTexture.sampler
		r.borrowed = true
	} else if e != nil {
		r.texture, r.view, r.sampler, err = createEnvironmentTexture(dev, e, "environment reflection")
		if err != nil {
			return err
		}
	} else {
		size := wgpu.Extent3D{Width: 1, Height: 1, DepthOrArrayLayers: 1}
		r.texture, err = dev.CreateTexture(&wgpu.TextureDescriptor{Label: "environment reflection", Size: size, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: gputypes.TextureFormatRGBA8UnormSrgb, Usage: gputypes.TextureUsageTextureBinding | gputypes.TextureUsageCopyDst})
		if err != nil {
			return err
		}
		r.view, err = dev.CreateTextureView(r.texture, nil)
		if err != nil {
			return err
		}
		r.sampler, err = dev.CreateSampler(&wgpu.SamplerDescriptor{AddressModeU: gputypes.AddressModeRepeat, AddressModeV: gputypes.AddressModeClampToEdge, MagFilter: gputypes.FilterModeLinear, MinFilter: gputypes.FilterModeLinear, MipmapFilter: gputypes.FilterModeLinear, Anisotropy: 1})
		if err != nil {
			return err
		}
		if err = dev.Queue().WriteTexture(&wgpu.ImageCopyTexture{Texture: r.texture}, []byte{0, 0, 0, 255}, &wgpu.ImageDataLayout{BytesPerRow: 4, RowsPerImage: 1}, &size); err != nil {
			return err
		}
	}
	targets := []*shadowTarget{&g.noShadow, &g.shadowTarget}
	var binds [2]*wgpu.BindGroup
	for i, t := range targets {
		if t.view == nil {
			continue
		}
		binds[i], err = g.sceneLightingBind(dev, t.view, &r)
		if err != nil {
			for _, b := range binds {
				if b != nil {
					b.Release()
				}
			}
			return err
		}
	}
	for i, t := range targets {
		if binds[i] != nil {
			if t.bind != nil {
				t.bind.Release()
			}
			t.bind = binds[i]
		}
	}
	g.reflection.release()
	g.reflection = r
	return nil
}

func (g *gpuRenderer) sceneLightingBind(dev *wgpu.Device, depth *wgpu.TextureView, r *reflectionTexture) (*wgpu.BindGroup, error) {
	ao := g.lightingAO
	if ao == nil {
		ao = g.screen.colorView
	}
	entries := []wgpu.BindGroupEntry{{Binding: 0, TextureView: depth}, {Binding: 1, TextureView: r.view}, {Binding: 2, Sampler: r.sampler}, {Binding: 3, TextureView: g.screen.colorView}, {Binding: 4, TextureView: g.screen.depthView}, {Binding: 5, TextureView: ao}}
	for i, t := range g.probeTextures {
		view := r.view
		if t != nil {
			view = t.view
		}
		entries = append(entries, wgpu.BindGroupEntry{Binding: uint32(6 + i), TextureView: view})
	}
	return dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: g.shadowLayout, Entries: entries})
}

// bindLightingAO refreshes both shadow variants without owning the AO texture.
func (g *gpuRenderer) bindLightingAO(dev *wgpu.Device, ao *wgpu.TextureView, retainPrevious bool) error {
	if g.lightingAO == ao {
		return nil
	}
	old := g.lightingAO
	g.lightingAO = ao
	targets := []*shadowTarget{&g.noShadow, &g.shadowTarget}
	var binds [2]*wgpu.BindGroup
	for i, t := range targets {
		if t.view == nil {
			continue
		}
		b, err := g.sceneLightingBind(dev, t.view, &g.reflection)
		if err != nil {
			for _, b := range binds {
				if b != nil {
					b.Release()
				}
			}
			g.lightingAO = old
			return err
		}
		binds[i] = b
	}
	for i, t := range targets {
		if binds[i] != nil {
			if t.bind != nil {
				if retainPrevious {
					g.aoPrepassBinds = append(g.aoPrepassBinds, t.bind)
				} else {
					t.bind.Release()
				}
			}
			t.bind = binds[i]
		}
	}
	return nil
}

func (g *gpuRenderer) releaseAOPrepassBinds() {
	for _, b := range g.aoPrepassBinds {
		b.Release()
	}
	g.aoPrepassBinds = g.aoPrepassBinds[:0]
}
