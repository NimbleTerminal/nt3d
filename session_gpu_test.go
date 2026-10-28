// Copyright (c) 2026 Neomantra Corp.

//go:build webgpu

package nt3d

import (
	"context"
	"errors"
	"image"
	"image/color"
	"math"
	"slices"
	"testing"

	"github.com/gogpu/gpucontext"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"unsafe"
)

type testDeviceProvider struct {
	inst    *wgpu.Instance
	adapter *wgpu.Adapter
	dev     *wgpu.Device
	format  gputypes.TextureFormat
}

func (p *testDeviceProvider) Device() gpucontext.Device {
	return gpucontext.NewDevice(unsafe.Pointer(p.dev))
}

func (p *testDeviceProvider) Queue() gpucontext.Queue {
	if p.dev == nil {
		return gpucontext.Queue{}
	}
	return gpucontext.NewQueue(unsafe.Pointer(p.dev.Queue()))
}

func (p *testDeviceProvider) Adapter() gpucontext.Adapter {
	return gpucontext.NewAdapter(unsafe.Pointer(p.adapter))
}

func (p *testDeviceProvider) SurfaceFormat() gputypes.TextureFormat {
	return p.format
}

func (p *testDeviceProvider) AdapterInfo() gpucontext.AdapterInfo {
	return gpucontext.AdapterInfo{}
}

func (p *testDeviceProvider) Features() gputypes.Features {
	return 0
}

func (p *testDeviceProvider) DownlevelCapabilities() gputypes.DownlevelCapabilities {
	return gputypes.DownlevelCapabilities{}
}

func newTestHostDevice(t *testing.T) (*wgpu.Instance, *wgpu.Adapter, *wgpu.Device) {
	t.Helper()
	inst, err := wgpu.CreateInstance(nil)
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	adapter, err := inst.RequestAdapter(&wgpu.RequestAdapterOptions{
		PowerPreference: gputypes.PowerPreferenceHighPerformance,
	})
	if err != nil {
		inst.Release()
		t.Fatalf("RequestAdapter: %v", err)
	}
	dev, err := adapter.RequestDevice(nil)
	if err != nil {
		adapter.Release()
		inst.Release()
		t.Fatalf("RequestDevice: %v", err)
	}
	t.Cleanup(func() {
		dev.Release()
		adapter.Release()
		inst.Release()
	})
	return inst, adapter, dev
}

func createHostTarget(t *testing.T, dev *wgpu.Device, w, h int, format gputypes.TextureFormat) (*wgpu.Texture, *wgpu.TextureView) {
	t.Helper()
	tex, err := dev.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "host target",
		Size:          wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     gputypes.TextureDimension2D,
		Format:        format,
		Usage:         gputypes.TextureUsageRenderAttachment | gputypes.TextureUsageCopySrc | gputypes.TextureUsageTextureBinding,
	})
	if err != nil {
		t.Fatalf("CreateTexture(%v): %v", format, err)
	}
	view, err := dev.CreateTextureView(tex, nil)
	if err != nil {
		tex.Release()
		t.Fatalf("CreateTextureView(%v): %v", format, err)
	}
	t.Cleanup(func() {
		view.Release()
		tex.Release()
	})
	return tex, view
}

func readHostTarget(t *testing.T, dev *wgpu.Device, tex *wgpu.Texture, w, h int, format gputypes.TextureFormat, linear bool) *image.NRGBA {
	t.Helper()
	bpp := 4
	if format == gputypes.TextureFormatRGBA16Float {
		bpp = 8
	}
	bytesPerRow := (w*bpp + 255) &^ 255
	bufSize := uint64(bytesPerRow * h)
	staging, err := dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "host readback",
		Size:  bufSize,
		Usage: gputypes.BufferUsageCopyDst | gputypes.BufferUsageMapRead,
	})
	if err != nil {
		t.Fatalf("CreateBuffer: %v", err)
	}
	defer staging.Release()

	enc, err := dev.CreateCommandEncoder(&wgpu.CommandEncoderDescriptor{Label: "host readback"})
	if err != nil {
		t.Fatalf("CreateCommandEncoder: %v", err)
	}
	enc.TransitionTextures([]wgpu.TextureBarrier{{
		Texture: tex,
		Usage: wgpu.TextureUsageTransition{
			OldUsage: gputypes.TextureUsageRenderAttachment,
			NewUsage: gputypes.TextureUsageCopySrc,
		},
	}})
	enc.CopyTextureToBuffer(tex, staging, []wgpu.BufferTextureCopy{{
		TextureBase:  wgpu.ImageCopyTexture{Texture: tex, MipLevel: 0, Aspect: gputypes.TextureAspectAll},
		BufferLayout: wgpu.ImageDataLayout{Offset: 0, BytesPerRow: uint32(bytesPerRow), RowsPerImage: uint32(h)},
		Size:         wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
	}})
	cmd, err := enc.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if _, err := dev.Queue().Submit(cmd); err != nil {
		cmd.Release()
		t.Fatalf("Submit: %v", err)
	}
	dev.Poll(wgpu.PollWait)
	dev.FreeCommandBuffer(cmd)

	if err := staging.Map(context.Background(), wgpu.MapModeRead, 0, bufSize); err != nil {
		t.Fatalf("Map: %v", err)
	}
	mapped, err := staging.MappedRange(0, bufSize)
	if err != nil {
		staging.Unmap()
		t.Fatalf("MappedRange: %v", err)
	}
	raw := slices.Clone(mapped.Bytes())
	staging.Unmap()

	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	bgra := format == gputypes.TextureFormatBGRA8Unorm || format == gputypes.TextureFormatBGRA8UnormSrgb
	for y := range h {
		row := raw[y*bytesPerRow:]
		for x := range w {
			if format == gputypes.TextureFormatRGBA16Float {
				p := row[x*8:]
				rf := float16ToFloat32(uint16(p[0]) | uint16(p[1])<<8)
				gf := float16ToFloat32(uint16(p[2]) | uint16(p[3])<<8)
				bf := float16ToFloat32(uint16(p[4]) | uint16(p[5])<<8)
				af := float16ToFloat32(uint16(p[6]) | uint16(p[7])<<8)
				if linear {
					out.SetNRGBA(x, y, color.NRGBA{
						R: encode8(rf),
						G: encode8(gf),
						B: encode8(bf),
						A: uint8(math.Round(math.Min(1, math.Max(0, float64(af))) * 255)),
					})
				} else {
					out.SetNRGBA(x, y, color.NRGBA{
						R: uint8(math.Round(math.Min(1, math.Max(0, float64(rf))) * 255)),
						G: uint8(math.Round(math.Min(1, math.Max(0, float64(gf))) * 255)),
						B: uint8(math.Round(math.Min(1, math.Max(0, float64(bf))) * 255)),
						A: uint8(math.Round(math.Min(1, math.Max(0, float64(af))) * 255)),
					})
				}
				continue
			}
			p := row[x*4:]
			r, g, b, a := p[0], p[1], p[2], p[3]
			if bgra {
				r, b = b, r
			}
			out.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: a})
		}
	}
	return out
}

func float16ToFloat32(h uint16) float32 {
	sign := uint32(h>>15) & 1
	exp := uint32(h>>10) & 0x1f
	mant := uint32(h & 0x3ff)
	switch exp {
	case 0:
		if mant == 0 {
			return math.Float32frombits(sign << 31)
		}
		for (mant & 0x400) == 0 {
			mant <<= 1
			exp--
		}
		exp++
		mant &= 0x3ff
		return math.Float32frombits((sign << 31) | ((exp + (127 - 15)) << 23) | (mant << 13))
	case 0x1f:
		return math.Float32frombits((sign << 31) | 0x7f800000 | (mant << 13))
	default:
		return math.Float32frombits((sign << 31) | ((exp + (127 - 15)) << 23) | (mant << 13))
	}
}

func TestGPUSessionFormatsAndColorSpace(t *testing.T) {
	_, _, dev := newTestHostDevice(t)
	session, err := NewGPUSession(dev)
	check(t, err)
	defer session.Close()

	standalone := NewGPURenderer()
	defer standalone.Close()

	s := NewScene()
	tex := testTexture(t, color.RGBA{220, 90, 40, 255})
	sphere, err := BuildSphere(Sphere{Radius: .65, Color: color.RGBA{255, 255, 255, 255}, Material: &Material{Texture: tex, Specular: .4, Shininess: 32}})
	check(t, err)
	_, err = s.SetGeometry("sphere", sphere)
	check(t, err)

	formats := []gputypes.TextureFormat{
		gputypes.TextureFormatRGBA8Unorm,
		gputypes.TextureFormatRGBA8UnormSrgb,
		gputypes.TextureFormatBGRA8Unorm,
		gputypes.TextureFormatBGRA8UnormSrgb,
		gputypes.TextureFormatRGBA16Float,
	}

	for _, format := range formats {
		for _, linear := range []bool{true, false} {
			for _, ss := range []int{1, 2} {
				f, err := s.Frame(DefaultCamera(), 64, 64)
				check(t, err)
				f.Linear = linear
				f.Supersample = ss

				want, err := standalone.Render(f)
				check(t, err)

				texTarget, viewTarget := createHostTarget(t, dev, f.Width, f.Height, format)
				err = session.Render(f, GPUTarget{
					Color:  viewTarget,
					Format: format,
					Width:  f.Width,
					Height: f.Height,
				})
				check(t, err)

				got := readHostTarget(t, dev, texTarget, f.Width, f.Height, format, linear)
				off := 0
				for y := range f.Height {
					for x := range f.Width {
						if !closeRGB(at(got, x, y), at(want, x, y), 2) {
							off++
						}
					}
				}
				if off > 8 {
					t.Fatalf("format=%v linear=%v ss=%d: %d pixels differ from standalone renderer (center got=%v want=%v)",
						format, linear, ss, off, at(got, 32, 32), at(want, 32, 32))
				}
			}
		}
	}
}

func TestGPUSessionEncodeAndLoadComposition(t *testing.T) {
	_, _, dev := newTestHostDevice(t)
	session, err := NewGPUSession(dev)
	check(t, err)
	defer session.Close()

	s := NewScene()
	cube, err := BuildCuboid(Cuboid{
		Size:     nt3dmath.Vec3{X: .7, Y: .7, Z: .7},
		Color:    color.RGBA{240, 40, 40, 255},
		Material: &Material{Unlit: true},
	})
	check(t, err)
	_, err = s.SetGeometry("cube", cube)
	check(t, err)

	const w, h = 64, 64
	f, err := s.Frame(DefaultCamera(), w, h)
	check(t, err)
	f.Background = color.RGBA{0, 0, 0, 0}
	f.Supersample = 2

	texTarget, viewTarget := createHostTarget(t, dev, w, h, gputypes.TextureFormatRGBA8Unorm)

	enc, err := dev.CreateCommandEncoder(&wgpu.CommandEncoderDescriptor{Label: "host frame"})
	check(t, err)

	// Host pass 1: pre-fill target with a distinct host background color.
	hostBG := color.RGBA{20, 120, 220, 255}
	pass, err := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:       viewTarget,
			LoadOp:     gputypes.LoadOpClear,
			StoreOp:    gputypes.StoreOpStore,
			ClearValue: gputypes.Color{R: float64(hostBG.R) / 255, G: float64(hostBG.G) / 255, B: float64(hostBG.B) / 255, A: 1},
		}},
	})
	check(t, err)
	check(t, pass.End())

	// NT3D pass: encode 3D scene over the existing host target.
	check(t, session.Encode(f, enc, GPUTarget{
		Color:  viewTarget,
		Format: gputypes.TextureFormatRGBA8Unorm,
		Width:  w,
		Height: h,
		Load:   true,
	}))

	cmd, err := enc.Finish()
	check(t, err)
	_, err = dev.Queue().Submit(cmd)
	check(t, err)
	dev.Poll(wgpu.PollWait)
	dev.FreeCommandBuffer(cmd)

	got := readHostTarget(t, dev, texTarget, w, h, gputypes.TextureFormatRGBA8Unorm, false)

	// Corners must preserve the host's blue background.
	for _, corner := range [][2]int{{2, 2}, {w - 3, 2}, {2, h - 3}, {w - 3, h - 3}} {
		if !closeRGB(at(got, corner[0], corner[1]), hostBG, 2) {
			t.Fatalf("corner %v = %v, want host background %v", corner, at(got, corner[0], corner[1]), hostBG)
		}
	}
	// Center must show the red cube.
	if !closeRGB(at(got, w/2, h/2), color.RGBA{240, 40, 40, 255}, 3) {
		t.Fatalf("center = %v, want red cube", at(got, w/2, h/2))
	}
}

func TestGPUSessionSharedDeviceAndResize(t *testing.T) {
	_, _, dev := newTestHostDevice(t)
	s1, err := NewGPUSession(dev)
	check(t, err)
	defer s1.Close()

	s2, err := NewGPUSession(dev)
	check(t, err)
	defer s2.Close()

	scene := NewScene()
	ball, err := BuildSphere(Sphere{Radius: .6, Color: color.RGBA{40, 200, 90, 255}})
	check(t, err)
	_, err = scene.SetGeometry("ball", ball)
	check(t, err)

	// Render at 48x48, then 96x64 on s1: geometry is uploaded only once.
	for i, dim := range [][2]int{{48, 48}, {96, 64}} {
		f, err := scene.Frame(DefaultCamera(), dim[0], dim[1])
		check(t, err)
		_, view := createHostTarget(t, dev, dim[0], dim[1], gputypes.TextureFormatBGRA8UnormSrgb)
		check(t, s1.Render(f, GPUTarget{
			Color:  view,
			Format: gputypes.TextureFormatBGRA8UnormSrgb,
			Width:  dim[0],
			Height: dim[1],
		}))
		if s1.g.uploads != 1 {
			t.Fatalf("step %d (%dx%d): uploads = %d, want 1", i, dim[0], dim[1], s1.g.uploads)
		}
	}

	// Close s1; s2 on the same host device continues rendering cleanly.
	check(t, s1.Close())
	f2, err := scene.Frame(DefaultCamera(), 64, 64)
	check(t, err)
	tex2, view2 := createHostTarget(t, dev, 64, 64, gputypes.TextureFormatRGBA8Unorm)
	check(t, s2.Render(f2, GPUTarget{
		Color:  view2,
		Format: gputypes.TextureFormatRGBA8Unorm,
		Width:  64,
		Height: 64,
	}))
	got := readHostTarget(t, dev, tex2, 64, 64, gputypes.TextureFormatRGBA8Unorm, true)
	if drawn(got, f2.Background) < 100 {
		t.Fatal("s2 failed to render after s1 closed")
	}
}

func TestGPUSessionValidationAndLifecycle(t *testing.T) {
	if _, err := NewGPUSession(nil); !errors.Is(err, ErrGPUUnavailable) {
		t.Fatalf("NewGPUSession(nil) = %v, want ErrGPUUnavailable", err)
	}
	if _, err := NewGPUSessionFromProvider(nil); !errors.Is(err, ErrGPUUnavailable) {
		t.Fatalf("NewGPUSessionFromProvider(nil) = %v, want ErrGPUUnavailable", err)
	}
	if _, err := NewGPUSessionFromProvider(&testDeviceProvider{}); !errors.Is(err, ErrGPUUnavailable) {
		t.Fatalf("NewGPUSessionFromProvider(empty) = %v, want ErrGPUUnavailable", err)
	}

	inst, adapter, dev := newTestHostDevice(t)
	session, err := NewGPUSessionFromProvider(&testDeviceProvider{
		inst:    inst,
		adapter: adapter,
		dev:     dev,
		format:  gputypes.TextureFormatBGRA8Unorm,
	})
	check(t, err)

	limit, err := session.BindingLimit()
	check(t, err)
	if limit < DefaultBindingLimit || limit > MaxBindingLimit {
		t.Fatalf("BindingLimit() = %d, out of range [%d, %d]", limit, DefaultBindingLimit, MaxBindingLimit)
	}

	s := NewScene()
	_, err = s.SetGeometry("p", pointGeometry())
	check(t, err)
	f, err := s.Frame(DefaultCamera(), 32, 32)
	check(t, err)
	_, view := createHostTarget(t, dev, 32, 32, gputypes.TextureFormatRGBA8Unorm)

	// Nil encoder.
	if err := session.Encode(f, nil, GPUTarget{Color: view, Format: gputypes.TextureFormatRGBA8Unorm, Width: 32, Height: 32}); err == nil {
		t.Fatal("expected error for nil encoder")
	}
	// Nil color view.
	if err := session.Render(f, GPUTarget{Format: gputypes.TextureFormatRGBA8Unorm, Width: 32, Height: 32}); err == nil {
		t.Fatal("expected error for nil target Color")
	}
	// Dimension mismatch.
	if err := session.Render(f, GPUTarget{Color: view, Format: gputypes.TextureFormatRGBA8Unorm, Width: 64, Height: 32}); err == nil {
		t.Fatal("expected error for target dimension mismatch")
	}
	// Unsupported format.
	if err := session.Render(f, GPUTarget{Color: view, Format: gputypes.TextureFormatDepth32Float, Width: 32, Height: 32}); err == nil {
		t.Fatal("expected error for unsupported target format")
	}

	// Close is idempotent and leaves subsequent calls returning ErrGPUUnavailable.
	check(t, session.Close())
	check(t, session.Close())
	if _, err := session.BindingLimit(); !errors.Is(err, ErrGPUUnavailable) {
		t.Fatalf("BindingLimit after Close = %v, want ErrGPUUnavailable", err)
	}
	if err := session.Render(f, GPUTarget{Color: view, Format: gputypes.TextureFormatRGBA8Unorm, Width: 32, Height: 32}); !errors.Is(err, ErrGPUUnavailable) {
		t.Fatalf("Render after Close = %v, want ErrGPUUnavailable", err)
	}
}
