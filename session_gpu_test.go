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
	enc.TransitionTextures([]wgpu.TextureBarrier{{
		Texture: tex,
		Usage: wgpu.TextureUsageTransition{
			OldUsage: gputypes.TextureUsageCopySrc,
			NewUsage: gputypes.TextureUsageRenderAttachment,
		},
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

func TestGPUSessionSubViewportAndSplitScreen(t *testing.T) {
	_, _, dev := newTestHostDevice(t)
	session, err := NewGPUSession(dev)
	check(t, err)
	defer session.Close()

	ref := NewGPURenderer()
	defer ref.Close()

	scene := NewScene()
	cube, err := BuildCuboid(Cuboid{
		Size:  nt3dmath.Vec3{X: 1.1, Y: 0.9, Z: 0.8},
		Color: color.RGBA{230, 80, 50, 255},
	})
	check(t, err)
	_, err = scene.SetGeometry("cube", cube)
	check(t, err)

	ball, err := BuildSphere(Sphere{
		Radius: 0.45,
		Color:  color.RGBA{50, 180, 240, 255},
	})
	check(t, err)
	ballID, err := scene.SetGeometry("ball", ball)
	check(t, err)
	check(t, scene.SetTransform(ballID, nt3dmath.Translation(nt3dmath.Vec3{X: 0.85, Y: -0.35, Z: 0.25})))

	const (
		fullW, fullH = 128, 96
		paneW, paneH = 64, 48
	)
	format := gputypes.TextureFormatBGRA8UnormSrgb
	tex, view := createHostTarget(t, dev, fullW, fullH, format)

	// Clear the full target first so we verify each sub-viewport preserves
	// previous quadrants while overwriting its own rectangle.
	enc, err := dev.CreateCommandEncoder(nil)
	check(t, err)
	pass, err := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:       view,
			LoadOp:     gputypes.LoadOpClear,
			StoreOp:    gputypes.StoreOpStore,
			ClearValue: gputypes.Color{R: 0, G: 0, B: 0, A: 1},
		}},
	})
	check(t, err)
	check(t, pass.End())
	cmd, err := enc.Finish()
	check(t, err)
	_, err = dev.Queue().Submit(cmd)
	check(t, err)
	dev.Poll(wgpu.PollWait)
	dev.FreeCommandBuffer(cmd)

	type paneSpec struct {
		name        string
		rect        image.Rectangle
		alpha, beta float64
		bg          color.RGBA
		supersample int
	}
	panes := []paneSpec{
		{name: "top-left", rect: image.Rect(0, 0, paneW, paneH), alpha: 20, beta: -35, bg: color.RGBA{24, 30, 48, 255}, supersample: 1},
		{name: "top-right", rect: image.Rect(paneW, 0, fullW, paneH), alpha: 75, beta: 0, bg: color.RGBA{44, 24, 36, 255}, supersample: 2},
		{name: "bottom-left", rect: image.Rect(0, paneH, paneW, fullH), alpha: 5, beta: 90, bg: color.RGBA{20, 44, 32, 255}, supersample: 1},
		{name: "bottom-right", rect: image.Rect(paneW, paneH, fullW, fullH), alpha: -25, beta: 140, bg: color.RGBA{46, 38, 20, 255}, supersample: 2},
	}

	wants := make([]image.Image, len(panes))
	for i, p := range panes {
		cam := DefaultCamera()
		cam.Alpha = p.alpha
		cam.Beta = p.beta
		f, err := scene.Frame(cam, p.rect.Dx(), p.rect.Dy())
		check(t, err)
		f.Background = p.bg
		f.Supersample = p.supersample

		want, err := ref.Render(f)
		check(t, err)
		wants[i] = want

		check(t, session.Render(f, GPUTarget{
			Color:    view,
			Format:   format,
			Width:    fullW,
			Height:   fullH,
			Viewport: p.rect,
		}))
	}

	gotFull := readHostTarget(t, dev, tex, fullW, fullH, format, true)
	for i, p := range panes {
		off := 0
		for y := range paneH {
			for x := range paneW {
				if !closeRGB(at(gotFull, p.rect.Min.X+x, p.rect.Min.Y+y), at(wants[i], x, y), 2) {
					off++
				}
			}
		}
		if off > 0 {
			t.Fatalf("pane %s (%v): %d pixels differ by >2", p.name, p.rect, off)
		}
	}

	// Now composite a transparent picture-in-picture overlay into a centered
	// sub-viewport with Load: true and verify outside pixels and uncovered
	// inside pixels remain intact while the overlay's center is drawn.
	pipRect := image.Rect(40, 24, 88, 72)
	overlayScene := NewScene()
	marker, err := BuildSphere(Sphere{
		Radius:   0.65,
		Color:    color.RGBA{250, 230, 40, 255},
		Material: &Material{Unlit: true},
	})
	check(t, err)
	_, err = overlayScene.SetGeometry("marker", marker)
	check(t, err)
	pipFrame, err := overlayScene.Frame(DefaultCamera(), pipRect.Dx(), pipRect.Dy())
	check(t, err)
	pipFrame.Background = color.RGBA{0, 0, 0, 0}
	pipFrame.Supersample = 2

	check(t, session.Render(pipFrame, GPUTarget{
		Color:    view,
		Format:   format,
		Width:    fullW,
		Height:   fullH,
		Viewport: pipRect,
		Load:     true,
	}))

	gotOverlay := readHostTarget(t, dev, tex, fullW, fullH, format, true)
	// Outside pipRect must be completely unchanged.
	if at(gotOverlay, 10, 10) != at(gotFull, 10, 10) {
		t.Fatalf("outside pixel (10,10) changed from %v to %v", at(gotFull, 10, 10), at(gotOverlay, 10, 10))
	}
	// Corner inside pipRect (41, 25) is uncovered by the sphere, so Load: true
	// preserves the underlying split-screen pixel.
	if !closeRGB(at(gotOverlay, 41, 25), at(gotFull, 41, 25), 2) {
		t.Fatalf("uncovered pip corner (41,25) = %v, want underlying %v", at(gotOverlay, 41, 25), at(gotFull, 41, 25))
	}
	// Center of pipRect (64, 48) must show the yellow unlit marker sphere.
	center := at(gotOverlay, 64, 48)
	if !closeRGB(center, color.RGBA{250, 230, 40, 255}, 3) {
		t.Fatalf("pip center (64,48) = %v, want yellow marker", center)
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
	defer setBindingLimit(DefaultBindingLimit)
	adopted, err := AdoptBindingLimit(session)
	check(t, err)
	if adopted != limit || BindingLimit() != limit {
		t.Fatalf("AdoptBindingLimit(session) = %d (BindingLimit() = %d), want %d", adopted, BindingLimit(), limit)
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
	// Out-of-bounds viewport.
	if err := session.Render(f, GPUTarget{Color: view, Format: gputypes.TextureFormatRGBA8Unorm, Width: 64, Height: 64, Viewport: image.Rect(40, 0, 72, 32)}); err == nil {
		t.Fatal("expected error for out-of-bounds target viewport")
	}
	// Negative viewport origin.
	if err := session.Render(f, GPUTarget{Color: view, Format: gputypes.TextureFormatRGBA8Unorm, Width: 64, Height: 64, Viewport: image.Rect(-4, 0, 28, 32)}); err == nil {
		t.Fatal("expected error for negative target viewport origin")
	}
	// Viewport dimension mismatch against Frame.
	if err := session.Render(f, GPUTarget{Color: view, Format: gputypes.TextureFormatRGBA8Unorm, Width: 64, Height: 64, Viewport: image.Rect(0, 0, 16, 16)}); err == nil {
		t.Fatal("expected error for viewport dimension mismatch")
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
	if _, err := session.NewView(); !errors.Is(err, ErrGPUUnavailable) {
		t.Fatalf("NewView after Close = %v, want ErrGPUUnavailable", err)
	}
	if err := session.Render(f, GPUTarget{Color: view, Format: gputypes.TextureFormatRGBA8Unorm, Width: 32, Height: 32}); !errors.Is(err, ErrGPUUnavailable) {
		t.Fatalf("Render after Close = %v, want ErrGPUUnavailable", err)
	}
}

func TestGPUSessionNewViewSharesResources(t *testing.T) {
	_, _, dev := newTestHostDevice(t)
	primary, err := NewGPUSession(dev)
	check(t, err)
	defer primary.Close()

	secondary, err := primary.NewView()
	check(t, err)
	defer secondary.Close()

	ref := NewGPURenderer()
	defer ref.Close()

	scene := NewScene()
	scene.SetEnvironment(testEnvironment(t))

	tex := testTexture(t, color.RGBA{220, 100, 50, 255})
	sphere, err := BuildSphere(Sphere{
		Radius: 0.55,
		Color:  color.RGBA{255, 255, 255, 255},
		Material: &Material{
			Texture:      tex,
			Specular:     0.5,
			Shininess:    32,
			Reflectivity: 0.25,
		},
	})
	check(t, err)
	sphere.Outline = OutlineStyle{Width: 2, Color: color.RGBA{255, 220, 60, 255}}
	_, err = scene.SetGeometry("sphere", sphere)
	check(t, err)

	// Add a Gaussian splat cloud to verify per-view depth sorting over shared
	// splat buffers.
	splats := styled(PointStyle{Shape: GaussianPoint, Units: WorldUnits, Blend: AlphaBlend},
		Point{Position: nt3dmath.Vec3{X: -0.45, Y: 0.35, Z: 0.2}, Radius: 1, Color: color.RGBA{80, 220, 255, 190}, GaussianAxes: [3]nt3dmath.Vec3{{X: 0.16}, {Y: 0.16}, {Z: 0.12}}},
		Point{Position: nt3dmath.Vec3{X: 0.45, Y: -0.35, Z: -0.15}, Radius: 1, Color: color.RGBA{255, 90, 190, 190}, GaussianAxes: [3]nt3dmath.Vec3{{X: 0.16}, {Y: 0.16}, {Z: 0.12}}},
	)
	_, err = scene.SetGeometry("splats", splats)
	check(t, err)

	const (
		fullW, fullH = 128, 64
		paneW, paneH = 64, 64
	)
	format := gputypes.TextureFormatBGRA8UnormSrgb
	targetTex, targetView := createHostTarget(t, dev, fullW, fullH, format)

	camFront := DefaultCamera()
	camFront.Alpha = 18
	camFront.Beta = -30
	fFront, err := scene.Frame(camFront, paneW, paneH)
	check(t, err)

	camTop := DefaultCamera()
	camTop.Alpha = 78
	camTop.Beta = 60
	fTop, err := scene.Frame(camTop, paneW, paneH)
	check(t, err)

	// Encode both views on the same CommandEncoder into left/right split-screen panes.
	enc, err := dev.CreateCommandEncoder(&wgpu.CommandEncoderDescriptor{Label: "multi-view frame"})
	check(t, err)
	leftRect := image.Rect(0, 0, paneW, paneH)
	rightRect := image.Rect(paneW, 0, fullW, fullH)
	check(t, primary.Encode(fFront, enc, GPUTarget{
		Color:    targetView,
		Format:   format,
		Width:    fullW,
		Height:   fullH,
		Viewport: leftRect,
	}))
	check(t, secondary.Encode(fTop, enc, GPUTarget{
		Color:    targetView,
		Format:   format,
		Width:    fullW,
		Height:   fullH,
		Viewport: rightRect,
	}))
	cmd, err := enc.Finish()
	check(t, err)
	_, err = dev.Queue().Submit(cmd)
	check(t, err)
	dev.Poll(wgpu.PollWait)
	dev.FreeCommandBuffer(cmd)

	// Primary uploaded geometry and textures once; secondary reused them with 0 uploads.
	if primary.g.uploads != 2 || primary.g.textureUploads == 0 {
		t.Fatalf("primary uploads = (%d geom, %d tex), want (2, >0)", primary.g.uploads, primary.g.textureUploads)
	}
	if secondary.g.uploads != 0 || secondary.g.textureUploads != 0 {
		t.Fatalf("secondary uploads = (%d geom, %d tex), want (0, 0)", secondary.g.uploads, secondary.g.textureUploads)
	}
	if secondary.g.environmentTexture != primary.g.environmentTexture {
		t.Fatal("expected secondary view to share primary environment texture")
	}
	if primary.g.reflection.texture != primary.g.environmentTexture.texture || secondary.g.reflection.texture != primary.g.environmentTexture.texture {
		t.Fatal("expected primary and secondary reflections to share environment panorama texture")
	}
	if secondary.g.nodes[0].batches[7].buffer != primary.g.nodes[0].batches[7].buffer {
		t.Fatal("expected secondary view to share primary mesh vertex buffer")
	}
	if secondary.g.nodes[1].batches[0].buffer != primary.g.nodes[1].batches[0].buffer {
		t.Fatal("expected secondary view to share primary splat point buffer")
	}
	if secondary.g.uniform == primary.g.uniform || secondary.g.target.color == primary.g.target.color {
		t.Fatal("expected secondary view to own distinct uniform and sceneTarget buffers")
	}

	gotSplit := readHostTarget(t, dev, targetTex, fullW, fullH, format, true)
	wantFront, err := ref.Render(fFront)
	check(t, err)
	wantTop, err := ref.Render(fTop)
	check(t, err)

	for paneIdx, want := range []image.Image{wantFront, wantTop} {
		ox := paneIdx * paneW
		off := 0
		for y := range paneH {
			for x := range paneW {
				if !closeRGB(at(gotSplit, ox+x, y), at(want, x, y), 2) {
					off++
				}
			}
		}
		if off > 4 {
			t.Fatalf("pane %d: %d pixels differ from reference renderer", paneIdx, off)
		}
	}

	// Closing the primary session must leave shared resources intact for the
	// secondary view and any view derived from it.
	check(t, primary.Close())

	tertiary, err := secondary.NewView()
	check(t, err)
	defer tertiary.Close()

	soloTex, soloView := createHostTarget(t, dev, paneW, paneH, format)
	check(t, tertiary.Render(fTop, GPUTarget{
		Color:  soloView,
		Format: format,
		Width:  paneW,
		Height: paneH,
	}))
	if tertiary.g.uploads != 0 || tertiary.g.textureUploads != 0 {
		t.Fatalf("tertiary uploads after primary close = (%d geom, %d tex), want (0, 0)", tertiary.g.uploads, tertiary.g.textureUploads)
	}
	gotTertiary := readHostTarget(t, dev, soloTex, paneW, paneH, format, true)
	off := 0
	for y := range paneH {
		for x := range paneW {
			if !closeRGB(at(gotTertiary, x, y), at(wantTop, x, y), 2) {
				off++
			}
		}
	}
	if off > 4 {
		t.Fatalf("tertiary after primary close: %d pixels differ from reference renderer", off)
	}
}

func TestGPUSessionReadDepthAndPick(t *testing.T) {
	_, _, dev := newTestHostDevice(t)
	sess, err := NewGPUSession(dev)
	check(t, err)
	defer sess.Close()

	s := NewScene()
	// Disable CPU BVH pick indexing so GPUSession.Pick exercises the GPU
	// depth-buffer unproject and spatial AABB triangle filter path.
	s.SetPickIndexing(false)

	sphere, err := BuildSphere(Sphere{Radius: 0.7, Segments: 64, Rings: 32, Color: color.RGBA{220, 90, 60, 255}})
	check(t, err)
	sphereID, err := s.SetGeometry("sphere", sphere)
	check(t, err)

	cube, err := BuildCuboid(Cuboid{
		Size:  nt3dmath.Vec3{X: 0.6, Y: 0.6, Z: 0.6},
		Color: color.RGBA{70, 160, 240, 255},
	})
	check(t, err)
	cubeID, err := s.SetGeometry("cube", cube)
	check(t, err)
	check(t, s.SetTransform(cubeID, nt3dmath.Translation(nt3dmath.Vec3{X: 1.4, Y: 0, Z: 0})))

	cam := DefaultCamera()
	cam.Alpha = 20
	cam.Beta = -35
	cam.Distance = 3.6

	const w, h = 160, 120
	format := gputypes.TextureFormatRGBA8Unorm
	_, targetView := createHostTarget(t, dev, w, h, format)

	for _, ss := range []int{1, 2} {
		f, err := s.Frame(cam, w, h)
		check(t, err)
		f.Supersample = ss

		check(t, sess.Render(f, GPUTarget{
			Color:  targetView,
			Format: format,
			Width:  w,
			Height: h,
		}))

		// 1. Background pixel near corner should report ok == false.
		if _, _, ok, err := sess.ReadDepth(f, 2, 2, 0); err != nil || ok {
			t.Fatalf("ss=%d: ReadDepth on background = (ok=%v, err=%v), want (false, nil)", ss, ok, err)
		}

		// 2. Center of sphere should report a valid depth and world position matching CPU Pick.
		sx, sy, _, ok := f.Matrix.Project(nt3dmath.Vec3{}, w, h)
		if !ok {
			t.Fatal("sphere center not in view")
		}
		world, depth, ok, err := sess.ReadDepth(f, sx, sy, 2)
		check(t, err)
		if !ok || depth <= 0 || depth >= 1 {
			t.Fatalf("ss=%d: ReadDepth on sphere = (world=%v, depth=%v, ok=%v)", ss, world, depth, ok)
		}

		wantHit := s.Pick(cam, f, sx, sy, 2)
		if wantHit == nil || wantHit.Node != sphereID {
			t.Fatalf("ss=%d: CPU Pick missed sphere: %+v", ss, wantHit)
		}
		if d := world.Sub(wantHit.Position); d.Dot(d) > 2e-3 {
			t.Fatalf("ss=%d: ReadDepth world %v differs from CPU Pick %v", ss, world, wantHit.Position)
		}

		// 3. GPUSession.Pick on unindexed high-poly sphere matches CPU Pick.
		gotHit := sess.Pick(s, cam, f, sx, sy, 2)
		if gotHit == nil || gotHit.Node != sphereID || gotHit.Index != wantHit.Index {
			t.Fatalf("ss=%d: GPUSession.Pick got %+v, want %+v", ss, gotHit, wantHit)
		}

		// 4. GPUSession.Pick with nil scene still resolves the frame's geometry.
		nilSceneHit := sess.Pick(nil, cam, f, sx, sy, 2)
		if nilSceneHit == nil || nilSceneHit.Node != sphereID || nilSceneHit.Index != wantHit.Index {
			t.Fatalf("ss=%d: GPUSession.Pick(nil) got %+v, want %+v", ss, nilSceneHit, wantHit)
		}

		// 5. GPUSession.Pick on the cube hits the cube.
		cx, cy, _, ok := f.Matrix.Project(nt3dmath.Vec3{X: 1.4}, w, h)
		if !ok {
			t.Fatal("cube center not in view")
		}
		cubeHit := sess.Pick(s, cam, f, cx, cy, 2)
		if cubeHit == nil || cubeHit.Node != cubeID {
			t.Fatalf("ss=%d: GPUSession.Pick on cube got %+v, want node %d", ss, cubeHit, cubeID)
		}
	}
}

func TestGPUSessionRenderToImage(t *testing.T) {
	_, _, dev := newTestHostDevice(t)
	sess, err := NewGPUSession(dev)
	check(t, err)
	defer sess.Close()

	ref := NewGPURenderer()
	defer ref.Close()

	s := NewScene()
	s.SetEnvironment(testEnvironment(t))
	sphere, err := BuildSphere(Sphere{
		Radius: 0.65,
		Color:  color.RGBA{235, 110, 55, 255},
		Material: &Material{
			Specular:     0.6,
			Shininess:    48,
			Reflectivity: 0.3,
		},
	})
	check(t, err)
	_, err = s.SetGeometry("sphere", sphere)
	check(t, err)

	cam := DefaultCamera()
	cam.Alpha = 20
	cam.Beta = -35

	reusable := image.NewNRGBA(image.Rect(0, 0, 96, 72))
	if err := sess.RenderToImageInto(Frame{Width: 96, Height: 72}, nil); err == nil {
		t.Fatal("expected error for nil destination image in RenderToImageInto")
	}
	if err := sess.RenderToImageInto(Frame{Width: 96, Height: 72}, image.NewNRGBA(image.Rect(0, 0, 32, 32))); err == nil {
		t.Fatal("expected error for mismatched destination image size in RenderToImageInto")
	}

	for step := range 3 {
		cam.Beta = float64(-35 + step*45)
		f, err := s.Frame(cam, 96, 72)
		check(t, err)
		f.Supersample = 2

		got, err := sess.RenderToImage(f)
		check(t, err)
		check(t, sess.RenderToImageInto(f, reusable))
		want, err := ref.Render(f)
		check(t, err)

		if got.Bounds() != want.Bounds() {
			t.Fatalf("step %d: bounds %v != %v", step, got.Bounds(), want.Bounds())
		}
		off := 0
		for y := range 72 {
			for x := range 96 {
				if !closeRGB(at(got, x, y), at(want, x, y), 2) {
					off++
				}
				if at(reusable, x, y) != at(got, x, y) {
					t.Fatalf("step %d: RenderToImageInto pixel (%d,%d) = %v, want %v",
						step, x, y, at(reusable, x, y), at(got, x, y))
				}
			}
		}
		if off > 4 {
			t.Fatalf("step %d: %d pixels differ from reference renderer", step, off)
		}
	}
	if sess.g.uploads != 1 {
		t.Fatalf("uploads across 3 turntable frames = %d, want 1", sess.g.uploads)
	}
}
