// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"math"
	"slices"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	gpuimage "nimbleterminal.dev/ntgpuimage"
)

const depthPickShader = `
struct DepthQuery {
	center: vec2<f32>,
	pixel: vec2<i32>,
	size: vec2<i32>,
	factor: i32,
	radius: f32,
};

@group(0) @binding(0) var<uniform> q: DepthQuery;
@group(0) @binding(1) var depthTex: texture_depth_2d;
@group(0) @binding(2) var<storage, read_write> result: vec4<f32>;

@compute @workgroup_size(1)
fn main() {
	var bestDepth = 1.0;
	var bestX = q.center.x;
	var bestY = q.center.y;
	var found = 0.0;

	let x0 = clamp(q.pixel.x * q.factor, 0, q.size.x - 1);
	let y0 = clamp(q.pixel.y * q.factor, 0, q.size.y - 1);
	let x1 = clamp((q.pixel.x + 1) * q.factor - 1, 0, q.size.x - 1);
	let y1 = clamp((q.pixel.y + 1) * q.factor - 1, 0, q.size.y - 1);
	for (var sy = y0; sy <= y1; sy++) {
		for (var sx = x0; sx <= x1; sx++) {
			let d = textureLoad(depthTex, vec2<i32>(sx, sy), 0);
			if d >= 0.0 && d < bestDepth {
				bestDepth = d;
				bestX = f32(sx) + 0.5;
				bestY = f32(sy) + 0.5;
				found = 1.0;
			}
		}
	}

	if found < 0.5 && q.radius > 0.0 {
		let r = i32(ceil(q.radius));
		let cx = i32(floor(q.center.x));
		let cy = i32(floor(q.center.y));
		let minX = clamp(cx - r, 0, q.size.x - 1);
		let maxX = clamp(cx + r, 0, q.size.x - 1);
		let minY = clamp(cy - r, 0, q.size.y - 1);
		let maxY = clamp(cy + r, 0, q.size.y - 1);
		let r2 = q.radius * q.radius;
		for (var sy = minY; sy <= maxY; sy++) {
			for (var sx = minX; sx <= maxX; sx++) {
				let fx = f32(sx) + 0.5;
				let fy = f32(sy) + 0.5;
				let dx = fx - q.center.x;
				let dy = fy - q.center.y;
				if dx * dx + dy * dy <= r2 {
					let d = textureLoad(depthTex, vec2<i32>(sx, sy), 0);
					if d >= 0.0 && d < bestDepth {
						bestDepth = d;
						bestX = fx;
						bestY = fy;
						found = 1.0;
					}
				}
			}
		}
	}

	result = vec4<f32>(bestDepth, bestX, bestY, found);
}
`

type gpuDepthPicker struct {
	layout                 *wgpu.BindGroupLayout
	pipeline               *wgpu.ComputePipeline
	uniform, result, stage *wgpu.Buffer
	bind                   *wgpu.BindGroup
	view                   *wgpu.TextureView
}

func (p *gpuDepthPicker) release(shared bool) {
	if p.bind != nil {
		p.bind.Release()
	}
	for _, b := range []*wgpu.Buffer{p.uniform, p.result, p.stage} {
		if b != nil {
			b.Release()
		}
	}
	if !shared {
		if p.pipeline != nil {
			p.pipeline.Release()
		}
		if p.layout != nil {
			p.layout.Release()
		}
	}
	*p = gpuDepthPicker{}
}

func (g *gpuRenderer) initDepthPick(dev *wgpu.Device) error {
	p := &g.depthPick
	if p.pipeline == nil {
		if g.shared != nil {
			g.shared.mu.Lock()
			if g.shared.depthPickPipeline != nil {
				p.layout = g.shared.depthPickLayout
				p.pipeline = g.shared.depthPickPipeline
				g.shared.mu.Unlock()
			} else {
				layout, pipeline, err := createDepthPickPipeline(dev)
				if err != nil {
					g.shared.mu.Unlock()
					return err
				}
				g.shared.depthPickLayout = layout
				g.shared.depthPickPipeline = pipeline
				p.layout = layout
				p.pipeline = pipeline
				g.shared.mu.Unlock()
			}
		} else {
			layout, pipeline, err := createDepthPickPipeline(dev)
			if err != nil {
				return err
			}
			p.layout = layout
			p.pipeline = pipeline
		}
	}
	if p.uniform == nil {
		var err error
		p.uniform, err = dev.CreateBuffer(&wgpu.BufferDescriptor{
			Label: "nt3d depth pick query",
			Size:  32,
			Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
		})
		if err != nil {
			return err
		}
	}
	if p.result == nil {
		var err error
		p.result, err = dev.CreateBuffer(&wgpu.BufferDescriptor{
			Label: "nt3d depth pick result",
			Size:  16,
			Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc,
		})
		if err != nil {
			return err
		}
	}
	if p.stage == nil {
		var err error
		p.stage, err = dev.CreateBuffer(&wgpu.BufferDescriptor{
			Label: "nt3d depth pick staging",
			Size:  16,
			Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst,
		})
		if err != nil {
			return err
		}
	}
	if p.view != g.target.depthView {
		if p.bind != nil {
			p.bind.Release()
			p.bind = nil
			p.view = nil
		}
		bind, err := dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
			Layout: p.layout,
			Entries: []wgpu.BindGroupEntry{
				{Binding: 0, Buffer: p.uniform, Size: 32},
				{Binding: 1, TextureView: g.target.depthView},
				{Binding: 2, Buffer: p.result, Size: 16},
			},
		})
		if err != nil {
			return err
		}
		p.bind = bind
		p.view = g.target.depthView
	}
	return nil
}

func createDepthPickPipeline(dev *wgpu.Device) (*wgpu.BindGroupLayout, *wgpu.ComputePipeline, error) {
	layout, err := dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "nt3d depth pick layout",
		Entries: []gputypes.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageCompute,
				Buffer:     &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageCompute,
				Texture: &gputypes.TextureBindingLayout{
					SampleType:    gputypes.TextureSampleTypeDepth,
					ViewDimension: gputypes.TextureViewDimension2D,
				},
			},
			{
				Binding:    2,
				Visibility: wgpu.ShaderStageCompute,
				Buffer:     &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeStorage},
			},
		},
	})
	if err != nil {
		return nil, nil, err
	}
	pipelineLayout, err := dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: []*wgpu.BindGroupLayout{layout},
	})
	if err != nil {
		layout.Release()
		return nil, nil, err
	}
	defer pipelineLayout.Release()
	shader, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "nt3d depth pick",
		WGSL:  depthPickShader,
	})
	if err != nil {
		layout.Release()
		return nil, nil, err
	}
	defer shader.Release()
	pipeline, err := dev.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label:      "nt3d depth pick",
		Layout:     pipelineLayout,
		Module:     shader,
		EntryPoint: "main",
	})
	if err != nil {
		layout.Release()
		return nil, nil, err
	}
	return layout, pipeline, nil
}

// ReadDepth reads the hardware depth buffer from the session's most recently
// encoded or rendered Frame at pixel (x, y), searching within tolerance pixels
// when the target pixel is background, and unprojects the nearest sample into
// world space. It returns ok == false when no surface was written to the depth
// buffer within tolerance of (x, y). When Encode was used to record the frame,
// the caller's command encoder must be submitted before calling ReadDepth.
func (s *GPUSession) ReadDepth(f Frame, x, y, tolerance float32) (world nt3dmath.Vec3, depth float32, ok bool, err error) {
	s.g.mu.Lock()
	defer s.g.mu.Unlock()
	if s.g.closed || s.dev == nil {
		return nt3dmath.Vec3{}, 1, false, fmt.Errorf("%w: session closed", ErrGPUUnavailable)
	}
	if err := validateFrame(f); err != nil {
		return nt3dmath.Vec3{}, 1, false, err
	}
	if x < 0 || y < 0 || x >= float32(f.Width) || y >= float32(f.Height) || !validFloat(x, y, tolerance) || tolerance < 0 {
		return nt3dmath.Vec3{}, 1, false, nil
	}
	factor := supersampleFactor(f)
	if s.g.target.depthView == nil || s.g.target.w != f.Width*factor || s.g.target.h != f.Height*factor {
		return nt3dmath.Vec3{}, 1, false, fmt.Errorf("nt3d: GPU session depth buffer (%dx%d) does not match frame (%dx%d at %dx)",
			s.g.target.w, s.g.target.h, f.Width, f.Height, factor)
	}
	if err := s.g.initDepthPick(s.dev); err != nil {
		return nt3dmath.Vec3{}, 1, false, err
	}
	p := &s.g.depthPick
	var query [32]byte
	sf := float32(factor)
	binary.LittleEndian.PutUint32(query[0:], math.Float32bits(x*sf))
	binary.LittleEndian.PutUint32(query[4:], math.Float32bits(y*sf))
	binary.LittleEndian.PutUint32(query[8:], uint32(int32(x)))
	binary.LittleEndian.PutUint32(query[12:], uint32(int32(y)))
	binary.LittleEndian.PutUint32(query[16:], uint32(int32(s.g.target.w)))
	binary.LittleEndian.PutUint32(query[20:], uint32(int32(s.g.target.h)))
	binary.LittleEndian.PutUint32(query[24:], uint32(int32(factor)))
	binary.LittleEndian.PutUint32(query[28:], math.Float32bits(tolerance*sf))
	if err := s.dev.Queue().WriteBuffer(p.uniform, 0, query[:]); err != nil {
		return nt3dmath.Vec3{}, 1, false, err
	}

	enc, err := s.dev.CreateCommandEncoder(&wgpu.CommandEncoderDescriptor{Label: "nt3d depth pick"})
	if err != nil {
		return nt3dmath.Vec3{}, 1, false, err
	}
	pass, err := enc.BeginComputePass(&wgpu.ComputePassDescriptor{Label: "nt3d depth pick"})
	if err != nil {
		enc.DiscardEncoding()
		return nt3dmath.Vec3{}, 1, false, err
	}
	pass.SetPipeline(p.pipeline)
	pass.SetBindGroup(0, p.bind, nil)
	pass.Dispatch(1, 1, 1)
	if err := pass.End(); err != nil {
		enc.DiscardEncoding()
		return nt3dmath.Vec3{}, 1, false, err
	}
	enc.CopyBufferToBuffer(p.result, 0, p.stage, 0, 16)
	cmd, err := enc.Finish()
	if err != nil {
		return nt3dmath.Vec3{}, 1, false, err
	}
	if _, err := s.dev.Queue().Submit(cmd); err != nil {
		cmd.Release()
		return nt3dmath.Vec3{}, 1, false, err
	}
	s.dev.Poll(wgpu.PollWait)
	s.dev.FreeCommandBuffer(cmd)

	if err := p.stage.Map(context.Background(), wgpu.MapModeRead, 0, 16); err != nil {
		return nt3dmath.Vec3{}, 1, false, err
	}
	mapped, err := p.stage.MappedRange(0, 16)
	if err != nil {
		_ = p.stage.Unmap()
		return nt3dmath.Vec3{}, 1, false, err
	}
	raw := slices.Clone(mapped.Bytes())
	_ = p.stage.Unmap()

	bestDepth := math.Float32frombits(binary.LittleEndian.Uint32(raw[0:4]))
	bestX := math.Float32frombits(binary.LittleEndian.Uint32(raw[4:8]))
	bestY := math.Float32frombits(binary.LittleEndian.Uint32(raw[8:12]))
	found := math.Float32frombits(binary.LittleEndian.Uint32(raw[12:16]))
	if found < 0.5 || bestDepth < 0 || bestDepth >= 1 {
		return nt3dmath.Vec3{}, 1, false, nil
	}
	world, ok = f.Unproject(bestX/sf, bestY/sf, bestDepth)
	if !ok {
		return nt3dmath.Vec3{}, 1, false, nil
	}
	return world, bestDepth, true, nil
}

// Pick tests visible instances in f against pixel (x, y) using the session's
// GPU depth buffer to accelerate picking on unindexed, procedural, or skinned
// meshes while warming static mesh BVH indexes in the background. When all
// visible meshes already have warmed BVH indexes, Pick uses the CPU BVH
// directly without a GPU readback. When scene is nil, Pick still tests f's
// geometry and instances using the GPU depth buffer.
func (s *GPUSession) Pick(scene *Scene, camera Camera, f Frame, x, y, tolerance float32) *Hit {
	if x < 0 || y < 0 || x >= float32(f.Width) || y >= float32(f.Height) || !validFloat(x, y, tolerance) || tolerance < 0 {
		return nil
	}
	if scene != nil {
		scene.WarmPickIndices()
		if !scene.hasUnindexedLargeMesh(f) {
			return scene.Pick(camera, f, x, y, tolerance)
		}
	}
	if s != nil {
		if world, depth, ok, err := s.ReadDepth(f, x, y, tolerance); err == nil {
			return scene.pickWithGPUDepth(camera, f, x, y, tolerance, world, depth, ok)
		}
	}
	return scene.Pick(camera, f, x, y, tolerance)
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
