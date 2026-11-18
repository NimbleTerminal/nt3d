// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"errors"
	"fmt"
	"image"
	"sync"
)

// RenderMode selects the rendering backend.
type RenderMode uint8

// Supported render modes.
const (
	WebGPU RenderMode = iota
	Software
	Wireframe
)

// String returns "WebGPU", "software", or "wireframe".
func (m RenderMode) String() string {
	switch m {
	case WebGPU:
		return "WebGPU"
	case Software:
		return "software"
	default:
		return "wireframe"
	}
}

// ErrGPUUnavailable is wrapped by GPU renderer errors that concern the
// device rather than the frame: no adapter, a failed setup, or a closed
// renderer. Other GPU errors concern the frame, such as running out of memory
// for its geometry; a smaller scene may still render.
var ErrGPUUnavailable = errors.New("nt3d: GPU unavailable")

// Renderer implementations serialize Render and Close. Call Close after use.
// Returned image pixels must remain stable across subsequent renders.
type Renderer interface {
	Render(Frame) (image.Image, error)
	Close() error
}

// NewGPURenderer returns a WebGPU renderer that lazily initializes an offscreen
// device on first Render. Retain it across frames and call Close after use.
func NewGPURenderer() Renderer { return &gpuRenderer{} }

// BindingLimiter is a renderer or session that knows how many bytes one
// storage binding on its device holds; the GPU renderer and GPUSession do.
type BindingLimiter interface {
	// BindingLimit sets the device up if it has to and returns the bytes one
	// storage binding holds, at most MaxBindingLimit.
	BindingLimit() (int, error)
}

// AdoptBindingLimit raises the binding limit (SetBindingLimit) to what r's
// device holds, when r is a BindingLimiter, and returns the limit in force.
// Any other value leaves the limit as it is. When the device cannot be
// set up, the limit is untouched and the error returned. Call it once at
// startup, before geometries are stored or files decoded.
func AdoptBindingLimit(r any) (int, error) {
	l, ok := r.(BindingLimiter)
	if !ok {
		return BindingLimit(), nil
	}
	bytes, err := l.BindingLimit()
	if err != nil {
		return BindingLimit(), err
	}
	return SetBindingLimit(bytes), nil
}

// Retain at most 4 MiB of private depth scratch. Larger snapshots use temporary
// storage, leaving the interactive buffer intact. Shrinking below one quarter
// of its capacity replaces the buffer with an exactly sized allocation.
const maxSoftwareDepthPixels = 1 << 20

type softwareRenderer struct {
	mu     sync.Mutex
	depth  []float32
	closed bool
}

// NewSoftwareRenderer returns a renderer with bounded private depth scratch.
// Render and Close may overlap: calls are serialized, Close waits for an active
// render, releases scratch, and rejects later renders. Close is idempotent.
// Each returned image is independently owned. Frames must remain immutable
// throughout Render. Retain the renderer between frames and Close it after use.
func NewSoftwareRenderer() Renderer { return &softwareRenderer{} }

// Render rasterizes f in software using bounded reusable depth scratch.
func (r *softwareRenderer) Render(f Frame) (image.Image, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("nt3d: software renderer closed")
	}
	if err := validateFrame(f); err != nil {
		return nil, err
	}
	s := supersampleFactor(f)
	n := f.Width * f.Height * s * s
	var depth []float32
	if n > maxSoftwareDepthPixels {
		depth = make([]float32, n)
	} else {
		if cap(r.depth) < n || n < cap(r.depth)/4 {
			r.depth = make([]float32, n)
		}
		r.depth = r.depth[:n]
		depth = r.depth
	}
	return softwareRenderScratch(f, depth), nil
}

// Close releases the renderer's depth scratch and rejects future Render calls.
func (r *softwareRenderer) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	r.depth = nil
	return nil
}

// Snapshot renders all primitives. WebGPU failure falls back to software.
// Supply a reusable GPU renderer to retain uploads; nil selects software fallback.
// Wireframe has no image and returns an error; use RenderWireframe instead.
func Snapshot(f Frame, mode RenderMode, gpu Renderer) (image.Image, RenderMode, error) {
	if err := validateFrame(f); err != nil {
		return nil, mode, err
	}
	f.Complete = true
	if mode == Wireframe || mode > Wireframe {
		return nil, mode, fmt.Errorf("nt3d: mode has no snapshot image")
	}
	if mode == WebGPU && gpu != nil {
		if img, err := gpu.Render(f); err == nil {
			return img, WebGPU, nil
		}
	}
	return softwareRender(f), Software, nil
}

// RenderWireframe renders f as an ANSI terminal wireframe of cols by rows character cells.
func RenderWireframe(f Frame, cols, rows int) (string, error) {
	if err := validateFrame(f); err != nil {
		return "", err
	}
	if cols < 1 || rows < 1 || cols > MaxFramePixels || rows > MaxFramePixels/cols {
		return "", fmt.Errorf("nt3d: invalid wireframe dimensions")
	}
	return wireRender(f, cols, rows), nil
}
