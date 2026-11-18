// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"context"
	"errors"
	"fmt"
	"image"

	"github.com/gogpu/gpucontext"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	gpuimage "nimbleterminal.dev/ntgpuimage"
)

// GPUTarget describes a caller-owned render target view that a GPUSession
// resolves a Frame into.
type GPUTarget struct {
	// Color is the destination texture view for the final resolve pass.
	Color *wgpu.TextureView
	// Format is the texture format of Color (RGBA8Unorm, RGBA8UnormSrgb,
	// BGRA8Unorm, BGRA8UnormSrgb, or RGBA16Float).
	Format gputypes.TextureFormat
	// Width and Height are the pixel dimensions of Color. When Viewport is
	// zero, they must match Frame.Width and Frame.Height.
	Width, Height int
	// Viewport is an optional sub-rectangle of Color, in pixel coordinates
	// within (0, 0)-(Width, Height), to resolve the Frame into. When zero,
	// the Frame resolves into the full (0, 0)-(Width, Height) bounds of
	// Color. When non-zero, Viewport must lie within (0, 0)-(Width, Height)
	// and its dimensions (Viewport.Dx(), Viewport.Dy()) must match
	// Frame.Width and Frame.Height.
	Viewport image.Rectangle
	// Load preserves existing pixels in the resolved region of Color and
	// alpha-composites the resolved scene over them when true; otherwise the
	// resolve pass overwrites the resolved region. Pixels outside a
	// sub-rectangle Viewport are always preserved.
	Load bool
}

// GPUSession renders Frames into caller-supplied targets on a host-owned
// WebGPU device, retaining uploaded geometry, textures, and pipelines across
// frames without owning a swapchain or reading pixels back to the CPU.
// Calls are serialized; Close releases NT3D's GPU resources while leaving the
// caller's Device open.
type GPUSession struct {
	dev *wgpu.Device
	g   gpuRenderer
}

// NewGPUSession creates a GPU rendering session on a caller-owned device.
func NewGPUSession(dev *wgpu.Device) (*GPUSession, error) {
	if dev == nil {
		return nil, fmt.Errorf("%w: nil device", ErrGPUUnavailable)
	}
	r, err := gpuimage.NewFromDevice(dev, gpuimage.Options{Label: "nt3d"})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrGPUUnavailable, err)
	}
	s := &GPUSession{dev: dev}
	s.g.r = r
	if err := s.g.setupDevice(dev); err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("%w: %w", ErrGPUUnavailable, err)
	}
	return s, nil
}

// NewGPUSessionFromProvider creates a GPU rendering session from a
// gpucontext.DeviceProvider (such as gogpu.App.GPUContextProvider()).
func NewGPUSessionFromProvider(provider gpucontext.DeviceProvider) (*GPUSession, error) {
	if provider == nil {
		return nil, fmt.Errorf("%w: nil device provider", ErrGPUUnavailable)
	}
	dev := provider.Device()
	if dev.IsNil() {
		return nil, fmt.Errorf("%w: nil device handle", ErrGPUUnavailable)
	}
	return NewGPUSession(wgpu.DeviceFromHandle(dev))
}

// NewView creates a secondary GPU rendering session on the same device that
// shares compiled pipelines, bind group layouts, shaders, and uploaded
// geometry, splat, and texture caches with s while maintaining its own
// per-view uniforms, instance transforms, depth sort buffers, shadow maps, and
// supersampling target. Either session may continue rendering after the other
// is closed; shared device resources are released when the last view closes.
func (s *GPUSession) NewView() (*GPUSession, error) {
	s.g.mu.Lock()
	if s.g.closed || s.dev == nil {
		s.g.mu.Unlock()
		return nil, fmt.Errorf("%w: session closed", ErrGPUUnavailable)
	}
	dev := s.dev
	r, err := gpuimage.NewFromDevice(dev, gpuimage.Options{Label: "nt3d"})
	if err != nil {
		s.g.mu.Unlock()
		return nil, fmt.Errorf("%w: %w", ErrGPUUnavailable, err)
	}
	v := &GPUSession{dev: dev}
	v.g.r = r
	err = v.g.setupView(dev, &s.g)
	s.g.mu.Unlock()
	if err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("%w: %w", ErrGPUUnavailable, err)
	}
	return v, nil
}

// BindingLimit returns the bytes one storage binding on the session's device
// holds, at most MaxBindingLimit; see SetBindingLimit.
func (s *GPUSession) BindingLimit() (int, error) {
	s.g.mu.Lock()
	defer s.g.mu.Unlock()
	if s.g.closed || s.dev == nil {
		return 0, fmt.Errorf("%w: session closed", ErrGPUUnavailable)
	}
	return min(s.g.deviceLimit, MaxBindingLimit), nil
}

// Encode uploads any changed scene resources and records the frame's compute,
// shadow, scene, and resolve passes into enc, resolving the final image into
// target. The caller finishes and submits enc.
func (s *GPUSession) Encode(f Frame, enc *wgpu.CommandEncoder, target GPUTarget) error {
	s.g.mu.Lock()
	defer s.g.mu.Unlock()
	if s.g.closed || s.dev == nil {
		return fmt.Errorf("%w: session closed", ErrGPUUnavailable)
	}
	if enc == nil {
		return errors.New("nt3d: nil GPU command encoder")
	}
	if err := validateFrame(f); err != nil {
		return err
	}
	if err := validateTarget(f, target); err != nil {
		return err
	}
	return s.g.encode(s.dev, enc, f, target)
}

// Render encodes f into target on a fresh command encoder and submits the
// command buffer to the session's device queue.
func (s *GPUSession) Render(f Frame, target GPUTarget) error {
	s.g.mu.Lock()
	defer s.g.mu.Unlock()
	if s.g.closed || s.dev == nil {
		return fmt.Errorf("%w: session closed", ErrGPUUnavailable)
	}
	if err := validateFrame(f); err != nil {
		return err
	}
	if err := validateTarget(f, target); err != nil {
		return err
	}
	enc, err := s.dev.CreateCommandEncoder(&wgpu.CommandEncoderDescriptor{Label: "nt3d session"})
	if err != nil {
		return err
	}
	if err := s.g.encode(s.dev, enc, f, target); err != nil {
		enc.DiscardEncoding()
		return err
	}
	cmd, err := enc.Finish()
	if err != nil {
		return err
	}
	if _, err := s.dev.Queue().Submit(cmd); err != nil {
		cmd.Release()
		return err
	}
	s.dev.Poll(wgpu.PollWait)
	s.dev.FreeCommandBuffer(cmd)
	return nil
}

// Close releases all GPU buffers, textures, bind groups, and pipelines
// retained by the session. Close is idempotent and does not close the caller's
// Device.
func (s *GPUSession) Close() error {
	s.g.mu.Lock()
	defer s.g.mu.Unlock()
	s.g.closed = true
	if s.dev == nil {
		return nil
	}
	s.g.releaseResources()
	var err error
	if s.g.r != nil {
		err = s.g.r.Close()
		s.g.r = nil
	}
	s.dev = nil
	return err
}

// RenderToImage encodes and renders f into an internal offscreen target on the
// session's device and reads the resolved RGBA8 pixels back into an
// *image.NRGBA, reusing all geometry, splat, texture, and pipeline caches held
// by the session.
func (s *GPUSession) RenderToImage(f Frame) (*image.NRGBA, error) {
	s.g.mu.Lock()
	defer s.g.mu.Unlock()
	if s.g.closed || s.dev == nil {
		return nil, fmt.Errorf("%w: session closed", ErrGPUUnavailable)
	}
	if err := validateFrame(f); err != nil {
		return nil, err
	}
	if w, h := s.g.r.Size(); w != f.Width || h != f.Height {
		if err := s.g.r.Resize(f.Width, f.Height); err != nil {
			return nil, err
		}
	}
	img, _, err := s.g.r.Render(context.Background(), func(frame *gpuimage.Frame) error {
		return s.g.encode(frame.Device, frame.Encoder, f, GPUTarget{
			Color:  frame.Color,
			Format: gputypes.TextureFormatRGBA8Unorm,
			Width:  f.Width,
			Height: f.Height,
		})
	})
	return img, err
}

// RenderToImageInto encodes and renders f into an internal offscreen target on
// the session's device and reads the resolved RGBA8 pixels back into dst,
// avoiding a per-frame image allocation during multi-frame captures.
func (s *GPUSession) RenderToImageInto(f Frame, dst *image.NRGBA) error {
	s.g.mu.Lock()
	defer s.g.mu.Unlock()
	if s.g.closed || s.dev == nil {
		return fmt.Errorf("%w: session closed", ErrGPUUnavailable)
	}
	if err := validateFrame(f); err != nil {
		return err
	}
	if dst == nil {
		return fmt.Errorf("nt3d: nil destination image")
	}
	if dst.Bounds().Dx() != f.Width || dst.Bounds().Dy() != f.Height {
		return fmt.Errorf("nt3d: destination image size %dx%d does not match frame size %dx%d",
			dst.Bounds().Dx(), dst.Bounds().Dy(), f.Width, f.Height)
	}
	if w, h := s.g.r.Size(); w != f.Width || h != f.Height {
		if err := s.g.r.Resize(f.Width, f.Height); err != nil {
			return err
		}
	}
	if _, err := s.g.r.RenderToTexture(context.Background(), func(frame *gpuimage.Frame) error {
		return s.g.encode(frame.Device, frame.Encoder, f, GPUTarget{
			Color:  frame.Color,
			Format: gputypes.TextureFormatRGBA8Unorm,
			Width:  f.Width,
			Height: f.Height,
		})
	}); err != nil {
		return err
	}
	_, err := s.g.r.ReadPixelsInto(context.Background(), dst)
	return err
}
