// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"nimbleterminal.dev/nt3d/math"
)

//go:embed sort.wgsl
var sortShader string

// The sort's uniform records, 256 bytes apart so each binds at a dynamic
// offset: the seven radix passes, the run pass, then one a slot.
const (
	sortRecord     = 256
	sortPasses     = 7 // Four bits each over the 28-bit keys.
	runParamRecord = sortPasses
	keyParamRecord = sortPasses + 1
	sortBlock      = 2048 // Keys a radix workgroup sorts.
	sortFold       = 65535
)

// gpuSort holds the frame-wide sort: key/payload ping-pong pairs, the
// per-pass block histograms, the run counts, the run table (dynamic-offset
// uniform records of 256 bytes: start, count, pad...) and indirect args.
type gpuSort struct {
	keys, payload      [2]*wgpu.Buffer // ping-pong; payload[final] is the sorted order bound to point bind groups
	hist, counts       *wgpu.Buffer    // counts: each run's first and end index, 0 ends where empty
	runs, args, params *wgpu.Buffer    // runs: 256 B per (slice, slot); args: 16 B per (slice, slot); params: sort uniform
	ranges             *wgpu.Buffer    // the frame's lodRanges, every clustered slot's back to back, 16 B each
	capacity, slots    int             // points and slots the buffers hold
	deviceLimit        int             // the device's storage binding, set at setup; 0 means unknown
	final              int             // which payload buffer holds the sorted order after seven passes (always the same parity: 1)
	generation         uint64          // bumps when payload[final] is reallocated, so point bind groups rebuild

	keyLayout, keyClusteredLayout, sortLayout, runLayout                                                       *wgpu.BindGroupLayout
	keyPipeline, keyClusteredPipeline, histPipeline, scanPipeline, scatterPipeline, countPipeline, runPipeline *wgpu.ComputePipeline

	// Bind groups over the buffers, dropped when one grows: the radix
	// passes' by parity, the run passes', and the key pass's a slot, with
	// the points buffer each was made for; a clustered slot's also with its
	// order buffer.
	passBinds [2]*wgpu.BindGroup
	runBind   *wgpu.BindGroup
	keyBinds  []*wgpu.BindGroup
	keyPoints []*wgpu.Buffer
	lodBinds  []*wgpu.BindGroup
	lodPoints []*wgpu.Buffer
	lodOrders []*wgpu.Buffer
}

// sortSlot is an alpha point geometry the key pass reads: its point
// records, the clip matrix they are drawn under, and how many it keys. A
// clustered slot keys only its selection: ranges of its order buffer (its
// Clusters.Order), count in all.
type sortSlot struct {
	points *wgpu.Buffer
	matrix nt3dmath.Mat4
	count  int
	order  *wgpu.Buffer
	ranges []lodRange // nil for a slot that keys every point
}

// initSort makes the sort's layouts and pipelines, once.
func (g *gpuRenderer) initSort(dev *wgpu.Device) error {
	s := &g.sort
	if s.keyPipeline != nil {
		return nil
	}
	if g.shared != nil {
		g.shared.mu.Lock()
		defer g.shared.mu.Unlock()
		if g.shared.sortKeyPipeline == nil {
			if err := s.initPipelines(dev); err != nil {
				return err
			}
			g.shared.sortKeyLayout = s.keyLayout
			g.shared.sortKeyClusteredLayout = s.keyClusteredLayout
			g.shared.sortLayout = s.sortLayout
			g.shared.sortRunLayout = s.runLayout
			g.shared.sortKeyPipeline = s.keyPipeline
			g.shared.sortKeyClustered = s.keyClusteredPipeline
			g.shared.sortHistPipeline = s.histPipeline
			g.shared.sortScanPipeline = s.scanPipeline
			g.shared.sortScatterPipeline = s.scatterPipeline
			g.shared.sortCountPipeline = s.countPipeline
			g.shared.sortRunPipeline = s.runPipeline
			return nil
		}
		s.keyLayout = g.shared.sortKeyLayout
		s.keyClusteredLayout = g.shared.sortKeyClusteredLayout
		s.sortLayout = g.shared.sortLayout
		s.runLayout = g.shared.sortRunLayout
		s.keyPipeline = g.shared.sortKeyPipeline
		s.keyClusteredPipeline = g.shared.sortKeyClustered
		s.histPipeline = g.shared.sortHistPipeline
		s.scanPipeline = g.shared.sortScanPipeline
		s.scatterPipeline = g.shared.sortScatterPipeline
		s.countPipeline = g.shared.sortCountPipeline
		s.runPipeline = g.shared.sortRunPipeline
		return nil
	}
	return s.initPipelines(dev)
}

func (s *gpuSort) initPipelines(dev *wgpu.Device) error {
	uniform := gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform, HasDynamicOffset: true}
	read := gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage}
	write := gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeStorage}
	layout := func(label string, bindings ...gputypes.BufferBindingLayout) (*wgpu.BindGroupLayout, error) {
		var entries []gputypes.BindGroupLayoutEntry
		for i := range bindings {
			entries = append(entries, gputypes.BindGroupLayoutEntry{Binding: uint32(i), Visibility: wgpu.ShaderStageCompute, Buffer: &bindings[i]})
		}
		return dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Label: label, Entries: entries})
	}
	var err error
	if s.keyLayout, err = layout("sort keys", uniform, read, write, write); err != nil {
		s.release()
		return err
	}
	if s.keyClusteredLayout, err = layout("sort keys clustered", uniform, read, write, write, read, read); err != nil {
		s.release()
		return err
	}
	if s.sortLayout, err = layout("sort pass", uniform, read, read, write, write, write); err != nil {
		s.release()
		return err
	}
	if s.runLayout, err = layout("sort runs", uniform, read, write, write, write); err != nil {
		s.release()
		return err
	}
	// Each section of the source is a module of its own after the shared
	// prelude, as their groups' bindings overlap.
	sections := strings.Split(sortShader, "\n// ==")
	modules := map[string]*wgpu.ShaderModule{}
	defer func() {
		for _, m := range modules {
			m.Release()
		}
	}()
	for _, section := range sections[1:] {
		name, _, _ := strings.Cut(strings.TrimSpace(section), ":")
		m, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "sort " + name, WGSL: sections[0] + "\n// ==" + section})
		if err != nil {
			s.release()
			return err
		}
		modules[name] = m
	}
	for _, p := range []struct {
		to     **wgpu.ComputePipeline
		layout *wgpu.BindGroupLayout
		module string
		entry  string
	}{
		{&s.keyPipeline, s.keyLayout, "keys", "keys"},
		{&s.keyClusteredPipeline, s.keyClusteredLayout, "clustered", "keysClustered"},
		{&s.histPipeline, s.sortLayout, "radix", "histogram"},
		{&s.scanPipeline, s.sortLayout, "radix", "scan"},
		{&s.scatterPipeline, s.sortLayout, "radix", "scatter"},
		{&s.countPipeline, s.runLayout, "runs", "count"},
		{&s.runPipeline, s.runLayout, "runs", "runs"},
	} {
		shader := modules[p.module]
		if shader == nil {
			s.release()
			return fmt.Errorf("nt3d: sort shader has no %s section", p.module)
		}
		pipelineLayout, err := dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{Label: "sort " + p.entry, BindGroupLayouts: []*wgpu.BindGroupLayout{p.layout}})
		if err != nil {
			s.release()
			return err
		}
		*p.to, err = dev.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{Label: "sort " + p.entry, Layout: pipelineLayout, Module: shader, EntryPoint: p.entry})
		pipelineLayout.Release()
		if err != nil {
			s.release()
			return err
		}
	}
	return nil
}

// limit is the bytes one sort buffer may take: the binding limit in force,
// or less when the device holds less.
func (s *gpuSort) limit() int {
	if s.deviceLimit > 0 {
		return min(BindingLimit(), s.deviceLimit)
	}
	return BindingLimit()
}

// growSort makes a buffer of at least size bytes, within limit, keeping the
// old one when it is large enough, and reports whether it changed.
func growSort(dev *wgpu.Device, b **wgpu.Buffer, size uint64, limit int, usage gputypes.BufferUsage, label string) (bool, error) {
	size = max(16, size)
	if *b != nil && (*b).Size() >= size {
		return false, nil
	}
	if size > uint64(limit) {
		return false, fmt.Errorf("nt3d: sort %s needs %d bytes, more than the GPU binding limit", label, size)
	}
	nb, err := dev.CreateBuffer(&wgpu.BufferDescriptor{Label: "sort " + label, Size: size, Usage: usage})
	if err != nil {
		return false, err
	}
	if *b != nil {
		(*b).Release()
	}
	*b = nb
	return true, nil
}

// ensure grows the buffers to hold points keys and slots' runs, never
// shrinking them, and drops the bind groups over any that changed.
func (s *gpuSort) ensure(dev *wgpu.Device, points, slots int) error {
	if points <= s.capacity && slots <= s.slots {
		return nil
	}
	limit := s.limit()
	if slots > maxBlendSlots || points*4 > limit {
		return fmt.Errorf("nt3d: frame exceeds GPU binding limit")
	}
	points, slots = max(points, s.capacity), max(slots, s.slots)
	s.final = sortPasses % 2
	storage := wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst | wgpu.BufferUsageCopySrc
	runs := uint64(blendSlices * slots)
	for _, b := range []struct {
		to    **wgpu.Buffer
		size  uint64
		usage gputypes.BufferUsage
		label string
	}{
		{&s.keys[0], uint64(points) * 4, storage, "keys"},
		{&s.keys[1], uint64(points) * 4, storage, "keys"},
		{&s.payload[0], uint64(points) * 4, storage, "payload"},
		{&s.payload[1], uint64(points) * 4, storage, "payload"},
		{&s.hist, uint64((points+sortBlock-1)/sortBlock) * 16 * 4, wgpu.BufferUsageStorage, "histograms"},
		{&s.counts, runs * 8, storage, "run bounds"},
		{&s.runs, runs * sortRecord, wgpu.BufferUsageUniform | wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc, "runs"},
		{&s.args, runs * 16, wgpu.BufferUsageIndirect | wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc, "args"},
		{&s.params, uint64(keyParamRecord+slots) * sortRecord, wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst, "params"},
	} {
		final := b.to == &s.payload[s.final]
		did, err := growSort(dev, b.to, b.size, limit, b.usage, b.label)
		if did {
			// At once, so a later failure leaves no group over a freed buffer.
			s.releaseBinds()
			if final {
				s.generation++
			}
		}
		if err != nil {
			return err
		}
	}
	s.capacity, s.slots = points, slots
	return nil
}

// runOffset is the byte offset of a (slice, slot) run record in runs.
func (s *gpuSort) runOffset(slice, slot int) uint32 {
	return uint32(sortRecord * (slice*s.slots + slot))
}

// argsOffset is the byte offset of a (slice, slot) run's draw arguments in args.
func (s *gpuSort) argsOffset(slice, slot int) uint64 {
	return uint64(16 * (slice*s.slots + slot))
}

// encodeSort records the frame's sort: keys for the alpha points lod selects
// of the plan's slots, the radix passes, and the run table, all on the
// device.
func (g *gpuRenderer) encodeSort(dev *wgpu.Device, encoder *wgpu.CommandEncoder, f Frame, plan *blendPlan, lod *lodPlan) error {
	if plan.points == 0 {
		return nil
	}
	if err := g.initSort(dev); err != nil {
		return err
	}
	s := &g.sort
	if err := s.ensure(dev, plan.points, len(plan.slots)); err != nil {
		return err
	}
	slots := make([]sortSlot, len(plan.slots))
	for slot, gi := range plan.slots {
		n := len(f.Geometry[gi].Points)
		var points *wgpu.Buffer
		if gi < len(g.nodes) && g.nodes[gi] != nil {
			points = g.nodes[gi].batches[0].buffer
		}
		if n > 0 && points == nil {
			return fmt.Errorf("nt3d: geometry %d's points are not on the device", gi)
		}
		slots[slot] = sortSlot{points: points, matrix: f.Matrix.Mul(instance(f, gi).World), count: n}
		if ranges := lod.slots[slot].ranges; ranges != nil {
			sl := &slots[slot]
			sl.count, sl.ranges = lod.slots[slot].selected, ranges
			sl.order = g.nodes[gi].order.buffer
			if sl.count > 0 && sl.order == nil {
				return fmt.Errorf("nt3d: geometry %d's cluster order is not on the device", gi)
			}
		}
	}
	if err := s.encodeKeys(dev, encoder, plan, slots); err != nil {
		return err
	}
	if err := s.encodeSortPasses(dev, encoder, plan.points); err != nil {
		return err
	}
	return s.encodeRuns(dev, encoder, plan.points)
}

// dispatchFolded dispatches groups workgroups, folding them over y past
// what a dimension allows.
func dispatchFolded(pass *wgpu.ComputePassEncoder, groups int) {
	if groups > 0 {
		pass.Dispatch(uint32(min(groups, sortFold)), uint32((groups+sortFold-1)/sortFold), 1)
	}
}

// encodeKeys writes each slot's keys and payloads into keys[0] and
// payload[0], the slots one after another. A payload is the point's index
// with its splat's scale byte above (packLOD): 255 for a slot that keys
// every point, its range's f8 for a clustered one.
func (s *gpuSort) encodeKeys(dev *wgpu.Device, encoder *wgpu.CommandEncoder, plan *blendPlan, slots []sortSlot) error {
	data := make([]byte, 0, len(slots)*sortRecord)
	var ranges []byte
	base := 0
	for slot, sl := range slots {
		rec := appendFloats(data, sl.matrix[:]...)
		rec = appendFloats(rec, plan.near, plan.far)
		for _, v := range []int{base, sl.count, slot, len(ranges) / 16, len(sl.ranges)} {
			rec = binary.LittleEndian.AppendUint32(rec, uint32(v))
		}
		data = append(rec, make([]byte, sortRecord-(len(rec)-len(data)))...)
		base += sl.count
		for _, r := range sl.ranges {
			for _, v := range []uint32{r.first, r.count, r.outBase, uint32(r.f8)} {
				ranges = binary.LittleEndian.AppendUint32(ranges, v)
			}
		}
	}
	if len(data) == 0 {
		return nil
	}
	if err := dev.Queue().WriteBuffer(s.params, keyParamRecord*sortRecord, data); err != nil {
		return err
	}
	if len(ranges) > 0 {
		grown, err := growSort(dev, &s.ranges, uint64(len(ranges)), s.limit(), wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst, "ranges")
		if grown {
			s.releaseLODBinds()
		}
		if err != nil {
			return err
		}
		if err := dev.Queue().WriteBuffer(s.ranges, 0, ranges); err != nil {
			return err
		}
	}
	s.keyBinds = append(s.keyBinds, make([]*wgpu.BindGroup, max(0, len(slots)-len(s.keyBinds)))...)
	s.keyPoints = append(s.keyPoints, make([]*wgpu.Buffer, max(0, len(slots)-len(s.keyPoints)))...)
	// A slot not clustered this frame lets its group go, and with it the
	// points and order buffers it holds.
	for k, b := range s.lodBinds {
		if b != nil && (k >= len(slots) || slots[k].ranges == nil) {
			b.Release()
			s.lodBinds[k], s.lodPoints[k], s.lodOrders[k] = nil, nil, nil
		}
	}
	s.lodBinds = append(s.lodBinds, make([]*wgpu.BindGroup, max(0, len(slots)-len(s.lodBinds)))...)
	s.lodPoints = append(s.lodPoints, make([]*wgpu.Buffer, max(0, len(slots)-len(s.lodPoints)))...)
	s.lodOrders = append(s.lodOrders, make([]*wgpu.Buffer, max(0, len(slots)-len(s.lodOrders)))...)
	for slot, sl := range slots {
		if sl.count == 0 || sl.ranges == nil || s.lodBinds[slot] != nil && s.lodPoints[slot] == sl.points && s.lodOrders[slot] == sl.order {
			continue
		}
		if s.lodBinds[slot] != nil {
			s.lodBinds[slot].Release()
			s.lodBinds[slot] = nil
		}
		bind, err := dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Label: "sort keys clustered", Layout: s.keyClusteredLayout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: s.params, Size: sortRecord}, {Binding: 1, Buffer: sl.points}, {Binding: 2, Buffer: s.keys[0]}, {Binding: 3, Buffer: s.payload[0]}, {Binding: 4, Buffer: sl.order}, {Binding: 5, Buffer: s.ranges}}})
		if err != nil {
			return err
		}
		s.lodBinds[slot], s.lodPoints[slot], s.lodOrders[slot] = bind, sl.points, sl.order
	}
	for slot, sl := range slots {
		if sl.count == 0 || sl.ranges != nil || s.keyBinds[slot] != nil && s.keyPoints[slot] == sl.points {
			continue
		}
		if s.keyBinds[slot] != nil {
			s.keyBinds[slot].Release()
			s.keyBinds[slot] = nil
		}
		bind, err := dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Label: "sort keys", Layout: s.keyLayout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: s.params, Size: sortRecord}, {Binding: 1, Buffer: sl.points}, {Binding: 2, Buffer: s.keys[0]}, {Binding: 3, Buffer: s.payload[0]}}})
		if err != nil {
			return err
		}
		s.keyBinds[slot], s.keyPoints[slot] = bind, sl.points
	}
	pass, err := encoder.BeginComputePass(&wgpu.ComputePassDescriptor{Label: "sort keys"})
	if err != nil {
		return err
	}
	// One workgroup a clustered slot's range, folded; 64 points a workgroup
	// otherwise.
	var pipeline *wgpu.ComputePipeline
	for slot, sl := range slots {
		if sl.count == 0 {
			continue
		}
		offset := []uint32{uint32((keyParamRecord + slot) * sortRecord)}
		want, bind, groups := s.keyPipeline, s.keyBinds[slot], (sl.count+63)/64
		if sl.ranges != nil {
			want, bind, groups = s.keyClusteredPipeline, s.lodBinds[slot], len(sl.ranges)
		}
		if pipeline != want {
			pipeline = want
			pass.SetPipeline(pipeline)
		}
		pass.SetBindGroup(0, bind, offset)
		dispatchFolded(pass, groups)
	}
	return pass.End()
}

// encodeSortPasses sorts the first n keys of keys[0], and payload[0] with
// them, into keys[final] and payload[final]: seven stable passes of four
// bits, ping-ponging between the pairs.
func (s *gpuSort) encodeSortPasses(dev *wgpu.Device, encoder *wgpu.CommandEncoder, n int) error {
	if n == 0 {
		return nil
	}
	blocks := (n + sortBlock - 1) / sortBlock
	data := make([]byte, sortPasses*sortRecord)
	for p := range sortPasses {
		for i, v := range []int{n, 4 * p, blocks, s.slots} {
			binary.LittleEndian.PutUint32(data[p*sortRecord+i*4:], uint32(v))
		}
	}
	if err := dev.Queue().WriteBuffer(s.params, 0, data); err != nil {
		return err
	}
	for i := range s.passBinds {
		if s.passBinds[i] != nil {
			continue
		}
		bind, err := dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Label: "sort pass", Layout: s.sortLayout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: s.params, Size: sortRecord}, {Binding: 1, Buffer: s.keys[i]}, {Binding: 2, Buffer: s.payload[i]}, {Binding: 3, Buffer: s.keys[1-i]}, {Binding: 4, Buffer: s.payload[1-i]}, {Binding: 5, Buffer: s.hist}}})
		if err != nil {
			return err
		}
		s.passBinds[i] = bind
	}
	pass, err := encoder.BeginComputePass(&wgpu.ComputePassDescriptor{Label: "sort passes"})
	if err != nil {
		return err
	}
	for p := range sortPasses {
		pass.SetBindGroup(0, s.passBinds[p%2], []uint32{uint32(p * sortRecord)})
		pass.SetPipeline(s.histPipeline)
		dispatchFolded(pass, blocks)
		pass.SetPipeline(s.scanPipeline)
		pass.Dispatch(1, 1, 1)
		pass.SetPipeline(s.scatterPipeline)
		dispatchFolded(pass, blocks)
	}
	return pass.End()
}

// encodeRuns finds where the sorted keys of each (slice, slot) begin and
// end, into counts, and writes the run table and draw arguments for every
// one, empty runs included.
func (s *gpuSort) encodeRuns(dev *wgpu.Device, encoder *wgpu.CommandEncoder, n int) error {
	data := make([]byte, 16)
	for i, v := range []int{n, 0, (n + sortBlock - 1) / sortBlock, s.slots} {
		binary.LittleEndian.PutUint32(data[i*4:], uint32(v))
	}
	if err := dev.Queue().WriteBuffer(s.params, runParamRecord*sortRecord, data); err != nil {
		return err
	}
	if s.runBind == nil {
		bind, err := dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Label: "sort runs", Layout: s.runLayout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: s.params, Size: sortRecord}, {Binding: 1, Buffer: s.keys[s.final]}, {Binding: 2, Buffer: s.counts}, {Binding: 3, Buffer: s.runs}, {Binding: 4, Buffer: s.args}}})
		if err != nil {
			return err
		}
		s.runBind = bind
	}
	encoder.ClearBuffer(s.counts, 0, s.counts.Size())
	pass, err := encoder.BeginComputePass(&wgpu.ComputePassDescriptor{Label: "sort runs"})
	if err != nil {
		return err
	}
	pass.SetBindGroup(0, s.runBind, []uint32{runParamRecord * sortRecord})
	pass.SetPipeline(s.countPipeline)
	dispatchFolded(pass, (n+255)/256)
	pass.SetPipeline(s.runPipeline)
	pass.Dispatch(1, 1, 1)
	return pass.End()
}

func (s *gpuSort) releaseBinds() {
	s.releaseLODBinds()
	for _, b := range append(s.keyBinds, s.passBinds[0], s.passBinds[1], s.runBind) {
		if b != nil {
			b.Release()
		}
	}
	s.passBinds, s.runBind, s.keyBinds, s.keyPoints = [2]*wgpu.BindGroup{}, nil, nil, nil
}

// releaseLODBinds drops the clustered slots' key bind groups, which hold
// the ranges buffer as well as the sort's.
func (s *gpuSort) releaseLODBinds() {
	for _, b := range s.lodBinds {
		if b != nil {
			b.Release()
		}
	}
	s.lodBinds, s.lodPoints, s.lodOrders = nil, nil, nil
}

// releaseBuffers frees the sort's per-view buffers and bind groups, leaving
// shared layouts and pipelines to gpuShared.
func (s *gpuSort) releaseBuffers() {
	s.releaseBinds()
	for _, b := range []*wgpu.Buffer{s.keys[0], s.keys[1], s.payload[0], s.payload[1], s.hist, s.counts, s.runs, s.args, s.params, s.ranges} {
		if b != nil {
			b.Release()
		}
	}
	*s = gpuSort{}
}

// release frees the sort's buffers, bind groups, layouts and pipelines.
func (s *gpuSort) release() {
	s.releaseBinds()
	for _, b := range []*wgpu.Buffer{s.keys[0], s.keys[1], s.payload[0], s.payload[1], s.hist, s.counts, s.runs, s.args, s.params, s.ranges} {
		if b != nil {
			b.Release()
		}
	}
	for _, p := range []*wgpu.ComputePipeline{s.keyPipeline, s.keyClusteredPipeline, s.histPipeline, s.scanPipeline, s.scatterPipeline, s.countPipeline, s.runPipeline} {
		if p != nil {
			p.Release()
		}
	}
	for _, l := range []*wgpu.BindGroupLayout{s.keyLayout, s.keyClusteredLayout, s.sortLayout, s.runLayout} {
		if l != nil {
			l.Release()
		}
	}
	*s = gpuSort{}
}
