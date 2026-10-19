// Copyright (c) 2026 Neomantra Corp.
package nt3dclod

import (
	"context"
	_ "embed"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

//go:embed select.wgsl
var selectShader string

// Layouts shared with select.wgsl.
const (
	uniformSize   = 176 // Matrix, eye, pads, eight ranges, eight fractions, counts.
	clusterRecord = 32  // min.xyz, first, max.xyz, count.
	pickRecord    = 16  // level, count, offset, visible.
	indirectSize  = 16  // vertexCount, instanceCount, firstVertex, firstInstance.
	maxBinding    = 128 << 20
	maxDispatch   = 65535
)

// GPU selects on the device what Set.Select selects on the CPU: three
// compute passes write the picks, the compact item indices, and indirect
// draw arguments into storage buffers that a draw path may bind. Upload
// the set, Encode the passes into each frame's command encoder, and draw
// from Items and Indirect; or Read the result back. One GPU serves one
// set at a time, and one device.
type GPU struct {
	layout                 *wgpu.BindGroupLayout
	pick, scan, scatter    *wgpu.ComputePipeline
	uniform                *wgpu.Buffer
	clusters, order, picks *wgpu.Buffer
	items, indirect        *wgpu.Buffer
	bind                   *wgpu.BindGroup
	set                    *Set
	revision               uint64
	clusterCount           int
	itemCount              int
}

// NewGPU creates the selection pipelines on dev.
func NewGPU(dev *wgpu.Device) (*GPU, error) {
	g := &GPU{}
	var entries []gputypes.BindGroupLayoutEntry
	for i, typ := range []gputypes.BufferBindingType{gputypes.BufferBindingTypeUniform, gputypes.BufferBindingTypeReadOnlyStorage, gputypes.BufferBindingTypeReadOnlyStorage, gputypes.BufferBindingTypeStorage, gputypes.BufferBindingTypeStorage, gputypes.BufferBindingTypeStorage} {
		entries = append(entries, gputypes.BindGroupLayoutEntry{Binding: uint32(i), Visibility: wgpu.ShaderStageCompute, Buffer: &gputypes.BufferBindingLayout{Type: typ}})
	}
	var err error
	if g.layout, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Label: "clod", Entries: entries}); err != nil {
		return nil, err
	}
	pipelineLayout, err := dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{g.layout}})
	if err != nil {
		g.Release()
		return nil, err
	}
	defer pipelineLayout.Release()
	shader, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "clod select", WGSL: selectShader})
	if err != nil {
		g.Release()
		return nil, err
	}
	defer shader.Release()
	for _, p := range []struct {
		entry string
		to    **wgpu.ComputePipeline
	}{{"pick", &g.pick}, {"scan", &g.scan}, {"scatter", &g.scatter}} {
		if *p.to, err = dev.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{Label: "clod " + p.entry, Layout: pipelineLayout, Module: shader, EntryPoint: p.entry}); err != nil {
			g.Release()
			return nil, err
		}
	}
	if g.uniform, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Label: "clod params", Size: uniformSize, Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst}); err != nil {
		g.Release()
		return nil, err
	}
	return g, nil
}

// Release frees the GPU's pipelines and buffers.
func (g *GPU) Release() {
	for _, b := range []*wgpu.Buffer{g.uniform, g.clusters, g.order, g.picks, g.items, g.indirect} {
		if b != nil {
			b.Release()
		}
	}
	for _, p := range []*wgpu.ComputePipeline{g.pick, g.scan, g.scatter} {
		if p != nil {
			p.Release()
		}
	}
	if g.bind != nil {
		g.bind.Release()
	}
	if g.layout != nil {
		g.layout.Release()
	}
	*g = GPU{}
}

// Items is the selected item indices, u32 each, as many as Indirect's
// instance count, once the encoded passes have run.
func (g *GPU) Items() *wgpu.Buffer { return g.items }

// Indirect holds DrawIndirect arguments: the vertex count Encode was
// given, the selected item count as the instance count, and zeros.
func (g *GPU) Indirect() *wgpu.Buffer { return g.indirect }

// Picks holds four u32 a cluster: level, listed count, offset into Items,
// and whether the cluster is in view.
func (g *GPU) Picks() *wgpu.Buffer { return g.picks }

// grow makes a buffer of at least size bytes, keeping the old one when it
// is large enough, and reports whether it changed.
func grow(dev *wgpu.Device, b **wgpu.Buffer, size uint64, usage gputypes.BufferUsage, label string) (bool, error) {
	size = max(16, size)
	if *b != nil && (*b).Size() >= size {
		return false, nil
	}
	if size > maxBinding {
		return false, fmt.Errorf("nt3dclod: %s needs %d bytes, more than a binding's %d", label, size, maxBinding)
	}
	nb, err := dev.CreateBuffer(&wgpu.BufferDescriptor{Label: "clod " + label, Size: size, Usage: usage})
	if err != nil {
		return false, err
	}
	if *b != nil {
		(*b).Release()
	}
	*b = nb
	return true, nil
}

// Upload sends the set's clusters and order to the device when they
// changed since the last upload, growing its buffers as needed.
func (g *GPU) Upload(dev *wgpu.Device, s *Set) error {
	if g.set == s && g.revision == s.revision {
		return nil
	}
	c, n := len(s.Clusters), len(s.Order)
	changed := false
	for _, b := range []struct {
		to    **wgpu.Buffer
		size  uint64
		usage gputypes.BufferUsage
		label string
	}{
		{&g.clusters, uint64(c) * clusterRecord, wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst, "clusters"},
		{&g.order, uint64(n) * 4, wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst, "order"},
		{&g.picks, uint64(c) * pickRecord, wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc, "picks"},
		{&g.items, uint64(n) * 4, wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc, "items"},
		{&g.indirect, indirectSize, wgpu.BufferUsageIndirect | wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc | wgpu.BufferUsageCopyDst, "indirect"},
	} {
		did, err := grow(dev, b.to, b.size, b.usage, b.label)
		if err != nil {
			return err
		}
		changed = changed || did
	}
	if changed || g.bind == nil {
		if g.bind != nil {
			g.bind.Release()
			g.bind = nil
		}
		var entries []wgpu.BindGroupEntry
		for i, b := range []*wgpu.Buffer{g.uniform, g.clusters, g.order, g.picks, g.items, g.indirect} {
			entries = append(entries, wgpu.BindGroupEntry{Binding: uint32(i), Buffer: b})
		}
		bind, err := dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: g.layout, Entries: entries})
		if err != nil {
			return err
		}
		g.bind = bind
	}
	data := make([]byte, 0, max(c*clusterRecord, n*4))
	for _, cl := range s.Clusters {
		data = appendFloats(data, cl.Bounds.Min.X, cl.Bounds.Min.Y, cl.Bounds.Min.Z)
		data = binary.LittleEndian.AppendUint32(data, cl.First)
		data = appendFloats(data, cl.Bounds.Max.X, cl.Bounds.Max.Y, cl.Bounds.Max.Z)
		data = binary.LittleEndian.AppendUint32(data, cl.Count)
	}
	if len(data) > 0 {
		if err := dev.Queue().WriteBuffer(g.clusters, 0, data); err != nil {
			return err
		}
	}
	data = data[:0]
	for _, i := range s.Order {
		data = binary.LittleEndian.AppendUint32(data, i)
	}
	if len(data) > 0 {
		if err := dev.Queue().WriteBuffer(g.order, 0, data); err != nil {
			return err
		}
	}
	g.set, g.revision, g.clusterCount, g.itemCount = s, s.revision, c, n
	return nil
}

func appendFloats(b []byte, f ...float32) []byte {
	for _, x := range f {
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(x))
	}
	return b
}

// Encode validates the view and levels, writes them to the device, and
// encodes the selection passes into enc, after Upload. Items that the
// draw path draws as vertexCount vertices each get that count in the
// indirect arguments. Commands submitted earlier in the same queue that
// read Items or Indirect see the previous selection.
func (g *GPU) Encode(dev *wgpu.Device, enc *wgpu.CommandEncoder, v View, levels []Level, vertexCount uint32) error {
	if g.set == nil {
		return fmt.Errorf("nt3dclod: Encode before Upload")
	}
	if err := validView(v); err != nil {
		return err
	}
	if err := validLevels(levels); err != nil {
		return err
	}
	padX, padY := v.pads()
	data := make([]byte, 0, uniformSize)
	data = appendFloats(data, v.Matrix[:]...)
	data = appendFloats(data, v.Eye.X, v.Eye.Y, v.Eye.Z, 0, padX, padY, v.orthoDistance(), 0)
	var ranges, fractions [MaxLevels]float32
	for i, l := range levels {
		ranges[i], fractions[i] = l.Range, l.Fraction
	}
	data = appendFloats(data, ranges[:]...)
	data = appendFloats(data, fractions[:]...)
	perspective := uint32(0)
	if v.Perspective {
		perspective = 1
	}
	for _, x := range []uint32{uint32(g.clusterCount), uint32(len(levels)), perspective, vertexCount} {
		data = binary.LittleEndian.AppendUint32(data, x)
	}
	if err := dev.Queue().WriteBuffer(g.uniform, 0, data); err != nil {
		return err
	}
	if g.clusterCount == 0 {
		return dev.Queue().WriteBuffer(g.indirect, 0, binary.LittleEndian.AppendUint32(make([]byte, 0, indirectSize), vertexCount)[:indirectSize])
	}
	pass, err := enc.BeginComputePass(&wgpu.ComputePassDescriptor{Label: "clod select"})
	if err != nil {
		return err
	}
	pass.SetBindGroup(0, g.bind, nil)
	pass.SetPipeline(g.pick)
	pass.Dispatch(uint32((g.clusterCount+63)/64), 1, 1)
	pass.SetPipeline(g.scan)
	pass.Dispatch(1, 1, 1)
	pass.SetPipeline(g.scatter)
	pass.Dispatch(uint32(min(g.clusterCount, maxDispatch)), uint32((g.clusterCount+maxDispatch-1)/maxDispatch), 1)
	return pass.End()
}

// Read runs the selection and returns it as Set.Select would, for tests
// and consumers that draw on the CPU. It submits its own commands and
// waits for them.
func (g *GPU) Read(ctx context.Context, dev *wgpu.Device, v View, levels []Level) (*Selection, error) {
	enc, err := dev.CreateCommandEncoder(&wgpu.CommandEncoderDescriptor{Label: "clod read"})
	if err != nil {
		return nil, err
	}
	if err := g.Encode(dev, enc, v, levels, 6); err != nil {
		return nil, err
	}
	picksSize, itemsSize := uint64(g.clusterCount)*pickRecord, uint64(g.itemCount)*4
	size := picksSize + itemsSize + indirectSize
	staging, err := dev.CreateBuffer(&wgpu.BufferDescriptor{Label: "clod staging", Size: size, Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst})
	if err != nil {
		return nil, err
	}
	defer staging.Release()
	if picksSize > 0 {
		enc.CopyBufferToBuffer(g.picks, 0, staging, 0, picksSize)
	}
	if itemsSize > 0 {
		enc.CopyBufferToBuffer(g.items, 0, staging, picksSize, itemsSize)
	}
	enc.CopyBufferToBuffer(g.indirect, 0, staging, picksSize+itemsSize, indirectSize)
	commands, err := enc.Finish()
	if err != nil {
		return nil, err
	}
	if _, err := dev.Queue().Submit(commands); err != nil {
		return nil, err
	}
	if err := staging.Map(ctx, wgpu.MapModeRead, 0, size); err != nil {
		return nil, err
	}
	defer staging.Unmap()
	mapped, err := staging.MappedRange(0, size)
	if err != nil {
		return nil, err
	}
	data := mapped.Bytes()
	out := &Selection{}
	for c := range g.clusterCount {
		rec := data[c*pickRecord:]
		level, count, offset, visible := binary.LittleEndian.Uint32(rec), binary.LittleEndian.Uint32(rec[4:]), binary.LittleEndian.Uint32(rec[8:]), binary.LittleEndian.Uint32(rec[12:])
		if visible == 0 {
			continue
		}
		if uint64(offset)+uint64(count) > uint64(g.itemCount) || offset != uint32(len(out.Items)) {
			return nil, fmt.Errorf("nt3dclod: cluster %d listed at %d+%d, expected %d", c, offset, count, len(out.Items))
		}
		out.Picks = append(out.Picks, Pick{uint32(c), level, offset, count})
		items := data[picksSize+uint64(offset)*4:]
		for i := range count {
			out.Items = append(out.Items, binary.LittleEndian.Uint32(items[i*4:]))
		}
	}
	indirect := data[picksSize+itemsSize:]
	if vertices, total := binary.LittleEndian.Uint32(indirect), binary.LittleEndian.Uint32(indirect[4:]); vertices != 6 || total != uint32(len(out.Items)) {
		return nil, fmt.Errorf("nt3dclod: indirect arguments %d %d, expected 6 %d", vertices, total, len(out.Items))
	}
	return out, nil
}
