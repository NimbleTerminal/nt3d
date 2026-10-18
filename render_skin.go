// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	_ "embed"
	"encoding/binary"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

//go:embed skin.wgsl
var skinShader string

type gpuSkin struct {
	source, influences, palette *wgpu.Buffer
	bind                        *wgpu.BindGroup
	sourceRevision              uint64
	revision                    uint64
}

func (s *gpuSkin) release() {
	if s == nil {
		return
	}
	if s.bind != nil {
		s.bind.Release()
	}
	for _, b := range []*wgpu.Buffer{s.source, s.influences, s.palette} {
		if b != nil {
			b.Release()
		}
	}
}
func (g *gpuRenderer) initSkin(dev *wgpu.Device) error {
	if g.skinPipeline != nil {
		return nil
	}
	var entries []gputypes.BindGroupLayoutEntry
	for i := range 4 {
		typ := gputypes.BufferBindingTypeReadOnlyStorage
		if i == 3 {
			typ = gputypes.BufferBindingTypeStorage
		}
		entries = append(entries, gputypes.BindGroupLayoutEntry{Binding: uint32(i), Visibility: wgpu.ShaderStageCompute, Buffer: &gputypes.BufferBindingLayout{Type: typ}})
	}
	layout, err := dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: entries})
	if err != nil {
		return err
	}
	pipelineLayout, err := dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{layout}})
	if err != nil {
		layout.Release()
		return err
	}
	defer pipelineLayout.Release()
	shader, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "linear blend skin", WGSL: skinShader})
	if err != nil {
		layout.Release()
		return err
	}
	defer shader.Release()
	pipeline, err := dev.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{Layout: pipelineLayout, Module: shader, EntryPoint: "main"})
	if err != nil {
		layout.Release()
		return err
	}
	g.skinLayout, g.skinPipeline = layout, pipeline
	return nil
}

// skinGeometry writes posed triangle buffers before both shadow and color passes.
// Rest records and influences persist; joint-only updates upload just the palette.
func (g *gpuRenderer) skinGeometry(dev *wgpu.Device, encoder *wgpu.CommandEncoder, f Frame) error {
	for i, geom := range f.Geometry {
		p := geom.posed
		if p == nil || len(geom.Indices) == 0 {
			continue
		}
		node := g.nodes[i]
		if node.skin != nil && node.skin.revision == p.revision {
			continue
		}
		if err := g.initSkin(dev); err != nil {
			return err
		}
		slot := 1
		if mapped(geom) {
			slot = 7
		}
		if node.skin == nil {
			skin := &gpuSkin{}
			node.skin = skin
			sizes := []uint64{uint64(len(geom.Indices)) * 48, uint64(len(geom.Indices)) * 32, uint64(len(p.palette)) * 64}
			ptrs := []**wgpu.Buffer{&skin.source, &skin.influences, &skin.palette}
			for k, ptr := range ptrs {
				b, err := dev.CreateBuffer(&wgpu.BufferDescriptor{Size: sizes[k], Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst})
				if err != nil {
					skin.release()
					node.skin = nil
					return err
				}
				*ptr = b
			}
			data := make([]byte, 0, sizes[1])
			for _, index := range geom.Indices {
				in := p.influences[index]
				for _, j := range in.Joints {
					data = binary.LittleEndian.AppendUint32(data, j)
				}
				data = appendFloats(data, in.Weights[:]...)
			}
			if err := dev.Queue().WriteBuffer(skin.influences, 0, data); err != nil {
				skin.release()
				node.skin = nil
				return err
			}
			bind, err := dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: g.skinLayout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: skin.source, Size: sizes[0]}, {Binding: 1, Buffer: skin.influences, Size: sizes[1]}, {Binding: 2, Buffer: skin.palette, Size: sizes[2]}, {Binding: 3, Buffer: node.batches[slot].buffer, Size: sizes[0]}}})
			if err != nil {
				skin.release()
				node.skin = nil
				return err
			}
			skin.bind = bind
		}
		skin := node.skin
		if skin.sourceRevision != p.sourceRevision {
			source := p.source
			source.Material = geom.Material
			data := geometryData(source)[slot][:len(geom.Indices)*48]
			if err := dev.Queue().WriteBuffer(skin.source, 0, data); err != nil {
				return err
			}
			skin.sourceRevision = p.sourceRevision
		}
		data := make([]byte, 0, len(p.palette)*64)
		for _, m := range p.palette {
			data = appendFloats(data, m[:]...)
		}
		if err := dev.Queue().WriteBuffer(skin.palette, 0, data); err != nil {
			return err
		}
		if node.batches[8].buffer != nil {
			a, b := geom.Bounds.Min, geom.Bounds.Max
			data := appendFloats(nil, a.X, a.Y, a.Z, geom.DropShadow.Strength, b.X, b.Y, b.Z, geom.DropShadow.Radius, 0, 0, 0, 0)
			if err := dev.Queue().WriteBuffer(node.batches[8].buffer, 0, data); err != nil {
				return err
			}
		}
		pass, err := encoder.BeginComputePass(&wgpu.ComputePassDescriptor{Label: "skin mesh"})
		if err != nil {
			return err
		}
		pass.SetPipeline(g.skinPipeline)
		pass.SetBindGroup(0, skin.bind, nil)
		pass.Dispatch(uint32((len(geom.Indices)+63)/64), 1, 1)
		if err = pass.End(); err != nil {
			return err
		}
		skin.revision = p.revision
	}
	return nil
}
