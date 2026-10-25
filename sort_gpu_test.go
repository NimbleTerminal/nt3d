// Copyright (c) 2026 Neomantra Corp.

//go:build webgpu

package nt3d

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/gogpu/wgpu"
	"nimbleterminal.dev/nt3d/math"
	gpuimage "nimbleterminal.dev/ntgpuimage"
)

// withSortDevice runs f on a device of its own, with a bare renderer whose
// sort it releases afterward.
func withSortDevice(t *testing.T, f func(dev *wgpu.Device, g *gpuRenderer) error) {
	t.Helper()
	r, err := gpuimage.New(context.Background(), gpuimage.Options{Width: 1, Height: 1, Label: "nt3d sort test"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	g := &gpuRenderer{}
	if err := r.Do(func(dev *wgpu.Device) error {
		defer g.sort.release()
		if err := g.initSort(dev); err != nil {
			return err
		}
		return f(dev, g)
	}); err != nil {
		t.Fatal(err)
	}
}

// readBuffers submits enc with copies of each buffer's first sizes bytes
// appended, waits, and returns their contents.
func readBuffers(dev *wgpu.Device, enc *wgpu.CommandEncoder, bufs []*wgpu.Buffer, sizes []uint64) ([][]byte, error) {
	var total uint64
	for _, s := range sizes {
		total += s
	}
	staging, err := dev.CreateBuffer(&wgpu.BufferDescriptor{Label: "nt3d sort staging", Size: total, Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst})
	if err != nil {
		return nil, err
	}
	defer staging.Release()
	var at uint64
	for i, b := range bufs {
		enc.CopyBufferToBuffer(b, 0, staging, at, sizes[i])
		at += sizes[i]
	}
	commands, err := enc.Finish()
	if err != nil {
		return nil, err
	}
	if _, err := dev.Queue().Submit(commands); err != nil {
		return nil, err
	}
	if err := staging.Map(context.Background(), wgpu.MapModeRead, 0, total); err != nil {
		return nil, err
	}
	defer staging.Unmap()
	mapped, err := staging.MappedRange(0, total)
	if err != nil {
		return nil, err
	}
	data := mapped.Bytes()
	out := make([][]byte, len(bufs))
	at = 0
	for i := range bufs {
		out[i] = slices.Clone(data[at : at+sizes[i]])
		at += sizes[i]
	}
	return out, nil
}

func words(data []byte) []uint32 {
	out := make([]uint32, len(data)/4)
	for i := range out {
		out[i] = binary.LittleEndian.Uint32(data[i*4:])
	}
	return out
}

func wordBytes(values []uint32) []byte {
	out := make([]byte, 0, len(values)*4)
	for _, v := range values {
		out = binary.LittleEndian.AppendUint32(out, v)
	}
	return out
}

func TestGPURadixSortMatchesCPU(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	// 100,000 keys over even slices and seven slots, of nine the buffers
	// hold, so odd slices and the last two slots are empty runs.
	var keys []uint32
	for i := range 100_000 {
		depth := rng.Uint32N(1024)
		if i%2 == 0 {
			depth = rng.Uint32N(8) * 129 // Few depths, so many keys tie.
		}
		keys = append(keys, rng.Uint32N(blendSlices/2)*2<<22|rng.Uint32N(7)<<10|depth)
	}
	withSortDevice(t, func(dev *wgpu.Device, g *gpuRenderer) error {
		return checkSort(t, dev, &g.sort, withSentinels(rng, keys, 1_000), 9)
	})
}

// The last slot's nearest key, 0x0FFFFBFF, differs from the sentinel in
// the sorted bits, so sentinels still sort after it.
func TestGPURadixSortLastSlotNearest(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	var keys []uint32
	for i := range 20_000 {
		k := uint32(0x0FFFFBFF)
		if i%2 == 0 {
			k = rng.Uint32N(blendSlices)<<22 | rng.Uint32N(maxBlendSlots-1)<<10 | rng.Uint32N(1024)
		}
		keys = append(keys, k)
	}
	if want := (&blendPlan{}).key(maxBlendSlots-1, 0); want != 0x0FFFFBFF {
		t.Fatalf("last slot's nearest key %#x, want 0x0FFFFBFF", want)
	}
	withSortDevice(t, func(dev *wgpu.Device, g *gpuRenderer) error {
		return checkSort(t, dev, &g.sort, withSentinels(rng, keys, 3_000), maxBlendSlots)
	})
}

// withSentinels inserts n sentinel keys at random places.
func withSentinels(rng *rand.Rand, keys []uint32, n int) []uint32 {
	keys = slices.Clone(keys)
	for range n {
		keys = slices.Insert(keys, rng.IntN(len(keys)+1), 0xFFFFFFFF)
	}
	return keys
}

// checkSort sorts keys, payload their indices, with buffers for slots, and
// checks the order and every run against the CPU's.
func checkSort(t *testing.T, dev *wgpu.Device, s *gpuSort, keys []uint32, slots int) error {
	total := len(keys)
	sentinels := 0
	payload := make([]uint32, total)
	for i, k := range keys {
		payload[i] = uint32(i)
		if k == 0xFFFFFFFF {
			sentinels++
		}
	}
	// The stable CPU order, and its runs.
	want := slices.Clone(payload)
	slices.SortStableFunc(want, func(a, b uint32) int {
		switch {
		case keys[a] < keys[b]:
			return -1
		case keys[a] > keys[b]:
			return 1
		}
		return 0
	})
	counts := make([]uint32, blendSlices*slots)
	for _, k := range keys {
		if k < 1<<28 {
			counts[int(k>>22)*slots+int(k>>10&4095)]++
		}
	}
	if err := s.ensure(dev, total, slots); err != nil {
		return err
	}
	if err := dev.Queue().WriteBuffer(s.keys[0], 0, wordBytes(keys)); err != nil {
		return err
	}
	if err := dev.Queue().WriteBuffer(s.payload[0], 0, wordBytes(payload)); err != nil {
		return err
	}
	enc, err := dev.CreateCommandEncoder(&wgpu.CommandEncoderDescriptor{Label: "nt3d sort test"})
	if err != nil {
		return err
	}
	if err := s.encodeSortPasses(dev, enc, total); err != nil {
		return err
	}
	if err := s.encodeRuns(dev, enc, total); err != nil {
		return err
	}
	runs := uint64(blendSlices * slots)
	start := time.Now()
	out, err := readBuffers(dev, enc, []*wgpu.Buffer{s.keys[s.final], s.payload[s.final], s.runs, s.args}, []uint64{uint64(total) * 4, uint64(total) * 4, runs * 256, runs * 16})
	if err != nil {
		return err
	}
	t.Logf("sorted %d keys and built %d runs in %v (submit to readback)", total, runs, time.Since(start))
	gotKeys, gotPayload := words(out[0]), words(out[1])
	seen := make([]bool, total)
	for i, p := range gotPayload {
		if p >= uint32(total) || seen[p] {
			return fmt.Errorf("payload %d is %d: not a permutation", i, p)
		}
		seen[p] = true
		if gotKeys[i] != keys[p] {
			return fmt.Errorf("key %d is %#x, its payload %d's key %#x", i, gotKeys[i], p, keys[p])
		}
		if i > 0 && gotKeys[i-1] > gotKeys[i] {
			return fmt.Errorf("keys %d, %d out of order: %#x > %#x", i-1, i, gotKeys[i-1], gotKeys[i])
		}
		if i > 0 && gotKeys[i-1] == gotKeys[i] && gotPayload[i-1] > p {
			return fmt.Errorf("equal keys at %d, %d lost index order: %d > %d", i-1, i, gotPayload[i-1], p)
		}
	}
	for i := total - sentinels; i < total; i++ {
		if gotKeys[i] != 0xFFFFFFFF {
			return fmt.Errorf("key %d is %#x, want a sentinel among the last %d", i, gotKeys[i], sentinels)
		}
	}
	if !slices.Equal(gotPayload, want) {
		return fmt.Errorf("payload differs from the stable CPU order")
	}
	at, empty := uint32(0), 0
	for slice := range blendSlices {
		for slot := range slots {
			r := slice*slots + slot
			rec := out[2][s.runOffset(slice, slot):]
			args := out[3][s.argsOffset(slice, slot):]
			gotStart, gotCount := binary.LittleEndian.Uint32(rec), binary.LittleEndian.Uint32(rec[4:])
			if gotStart != at || gotCount != counts[r] {
				return fmt.Errorf("run (%d, %d) is %d+%d, want %d+%d", slice, slot, gotStart, gotCount, at, counts[r])
			}
			if a := words(args[:16]); a[0] != 6 || a[1] != counts[r] || a[2] != 0 || a[3] != 0 {
				return fmt.Errorf("run (%d, %d) args %v, want [6 %d 0 0]", slice, slot, a, counts[r])
			}
			if counts[r] == 0 {
				empty++
			}
			for i := at; i < at+counts[r]; i++ {
				if k := gotKeys[i]; int(k>>22) != slice || int(k>>10&4095) != slot {
					return fmt.Errorf("run (%d, %d) holds key %#x at %d", slice, slot, k, i)
				}
			}
			at += counts[r]
		}
	}
	if at != uint32(total-sentinels) {
		return fmt.Errorf("runs cover %d keys, want %d", at, total-sentinels)
	}
	if empty == 0 {
		return fmt.Errorf("no empty runs checked")
	}
	return nil
}

// The key pass quantizes as blendPlan.key does, but for a last bit the
// GPU's arithmetic may round otherwise.
func TestGPUSortKeysMatchPlan(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	m := nt3dmath.Mat4{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, .5, .5, 0, 0, .25, 1.5}
	plan := blendPlan{near: .2, far: 1.1}
	sizes := []int{3000, 5000}
	withSortDevice(t, func(dev *wgpu.Device, g *gpuRenderer) error {
		s := &g.sort
		total := sizes[0] + sizes[1]
		if err := s.ensure(dev, total, len(sizes)); err != nil {
			return err
		}
		var slots []sortSlot
		var want []uint32
		var zs []float32
		for slot, size := range sizes {
			var data []byte
			world := nt3dmath.Identity()
			world[14] = float32(slot) * .1
			mw := m.Mul(world)
			for range size {
				p := nt3dmath.Vec3{X: rng.Float32()*2 - 1, Y: rng.Float32()*2 - 1, Z: rng.Float32()*8 - 4}
				data = appendFloats(data, p.X, p.Y, p.Z, 1)
				data = binary.LittleEndian.AppendUint32(data, 0xFFFFFFFF)
				_, _, z, w := mw.Transform(p)
				k := uint32(0xFFFFFFFF)
				if w > 0 && z >= 0 && z <= w {
					k = plan.key(slot, z)
				}
				want = append(want, k)
				zs = append(zs, z)
			}
			b, err := dev.CreateBuffer(&wgpu.BufferDescriptor{Label: "nt3d sort test points", Size: uint64(len(data)), Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst})
			if err != nil {
				return err
			}
			defer b.Release()
			if err := dev.Queue().WriteBuffer(b, 0, data); err != nil {
				return err
			}
			slots = append(slots, sortSlot{points: b, matrix: mw, count: size})
		}
		enc, err := dev.CreateCommandEncoder(&wgpu.CommandEncoderDescriptor{Label: "nt3d sort keys test"})
		if err != nil {
			return err
		}
		if err := s.encodeKeys(dev, enc, &plan, slots); err != nil {
			return err
		}
		out, err := readBuffers(dev, enc, []*wgpu.Buffer{s.keys[0], s.payload[0]}, []uint64{uint64(total) * 4, uint64(total) * 4})
		if err != nil {
			return err
		}
		gotKeys, gotPayload := words(out[0]), words(out[1])
		depth := func(k uint32) int { return int(65535 - (k>>22<<10 | k&1023)) }
		exact, base := 0, 0
		for slot, size := range sizes {
			for i := range size {
				got, w := gotKeys[base+i], want[base+i]
				if gotPayload[base+i] != uint32(i) {
					return fmt.Errorf("slot %d payload %d is %d", slot, i, gotPayload[base+i])
				}
				if got == w {
					exact++
					continue
				}
				if got == 0xFFFFFFFF || w == 0xFFFFFFFF || got>>10&4095 != w>>10&4095 || absInt(depth(got)-depth(w)) > 1 {
					return fmt.Errorf("slot %d point %d (z %g): key %#x, want %#x", slot, i, zs[base+i], got, w)
				}
			}
			base += size
		}
		if exact < total*99/100 {
			return fmt.Errorf("only %d of %d keys exact", exact, total)
		}
		return nil
	})
}

func TestGPUSortEnsureGrows(t *testing.T) {
	withSortDevice(t, func(dev *wgpu.Device, g *gpuRenderer) error {
		s := &g.sort
		if err := s.ensure(dev, 1000, 3); err != nil {
			return err
		}
		order, generation := s.payload[s.final], s.generation
		if err := s.ensure(dev, 500, 2); err != nil {
			return err
		}
		if s.payload[s.final] != order || s.generation != generation || s.capacity != 1000 || s.slots != 3 {
			return fmt.Errorf("ensure shrank or rebuilt: capacity %d, slots %d, generation %d", s.capacity, s.slots, s.generation)
		}
		if err := s.ensure(dev, 500, 5); err != nil {
			return err
		}
		if s.payload[s.final] != order || s.generation != generation || s.capacity != 1000 || s.slots != 5 {
			return fmt.Errorf("more slots rebuilt the order: capacity %d, slots %d, generation %d", s.capacity, s.slots, s.generation)
		}
		if err := s.ensure(dev, 4000, 1); err != nil {
			return err
		}
		if s.generation != generation+1 || s.capacity != 4000 || s.slots != 5 {
			return fmt.Errorf("growth gave capacity %d, slots %d, generation %d", s.capacity, s.slots, s.generation)
		}
		if err := s.ensure(dev, bindingLimit/4+1, 1); err == nil {
			return fmt.Errorf("ensure took more keys than a binding holds")
		}
		return nil
	})
}
