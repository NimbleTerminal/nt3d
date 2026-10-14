// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"context"
	_ "embed"
	"encoding/binary"
	"fmt"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	"image"
	"image/color"
	"math"
	"nimbleterminal.dev/nt3d/math"
	gpuimage "nimbleterminal.dev/ntgpuimage"
	"sync"
)

//go:embed scene.wgsl
var sceneShader string

type gpuBatch struct {
	buffer *wgpu.Buffer
	bind   *wgpu.BindGroup
	count  uint32
}

type gpuRenderer struct {
	textureLayout   *wgpu.PipelineLayout
	texturePipeline *wgpu.RenderPipeline
	textures        map[*Texture]*gpuimage.Texture
	procedural      map[*Texture]*proceduralTexture
	geometries      map[geometryKey]*gpuGeometry
	nodes           []*gpuGeometry
	textureUploads  uint64
	mu              sync.Mutex
	closed          bool
	initErr         error
	r               *gpuimage.Renderer
	shader          *wgpu.ShaderModule
	bindLayout      *wgpu.BindGroupLayout
	layout          *wgpu.PipelineLayout
	uniform         *wgpu.Buffer
	transforms      *wgpu.Buffer
	pipelines       [7]*wgpu.RenderPipeline
	batches         [7]gpuBatch // points, triangles, boxes, lines, overlays, dynamic lines, arrows
	uploads         uint64
}

func (g *gpuRenderer) setup() (err error) {
	g.r, err = gpuimage.New(context.Background(), gpuimage.Options{Width: 1, Height: 1, Depth: true, Label: "nt3d", PowerPreference: gputypes.PowerPreferenceHighPerformance})
	if err != nil {
		return err
	}
	if g.r.AdapterInfo().DeviceType == gputypes.DeviceTypeCPU {
		_ = g.r.Close()
		g.r = nil
		return gpuimage.ErrNoAdapter
	}
	return g.r.Do(func(dev *wgpu.Device) error {
		g.shader, err = dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "nt3d", WGSL: sceneShader})
		if err != nil {
			return err
		}
		g.bindLayout, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageVertex, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}}, {Binding: 1, Visibility: wgpu.ShaderStageVertex, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage}}, {Binding: 2, Visibility: wgpu.ShaderStageVertex, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage}}}})
		if err != nil {
			return err
		}
		g.layout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{g.bindLayout}})
		if err != nil {
			return err
		}
		for i, entry := range []string{"vs_point", "vs_mesh", "vs_box", "vs_line", "vs_ui", "vs_dynamic", "vs_arrow"} {
			primitive := gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeBack}
			if i == 0 {
				primitive.CullMode = gputypes.CullModeNone
			}
			if i == 3 {
				primitive.CullMode = gputypes.CullModeNone
			}
			if i >= 4 {
				primitive.CullMode = gputypes.CullModeNone
			}
			g.pipelines[i], err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: entry, Layout: g.layout, Vertex: wgpu.VertexState{Module: g.shader, EntryPoint: entry}, Primitive: primitive, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth24Plus, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual}, Multisample: gputypes.DefaultMultisampleState(), Fragment: &wgpu.FragmentState{Module: g.shader, EntryPoint: "fs_main", Targets: []gputypes.ColorTargetState{{Format: gputypes.TextureFormatRGBA8Unorm, WriteMask: gputypes.ColorWriteMaskAll}}}})
			if err != nil {
				return err
			}
		}
		g.uniform, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: 112, Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst})
		if err != nil {
			return err
		}
		return nil
	})
}

func floats(values ...float32) []byte {
	out := make([]byte, len(values)*4)
	for i, v := range values {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(v))
	}
	return out
}

func (g *gpuRenderer) uploadUI(dev *wgpu.Device, f Frame) error {
	var data []byte
	vertex := func(x, y int, c color.RGBA) {
		data = append(data, floats(float32(x), float32(y), 0, 1, 0, 0, 0, 0, float32(c.R)/255, float32(c.G)/255, float32(c.B)/255, float32(c.A)/255)...)
	}
	for _, r := range f.Overlays {
		if r.Width <= 0 || r.Height <= 0 {
			continue
		}
		x0, y0, x1, y1 := r.X, r.Y, r.X+r.Width, r.Y+r.Height
		vertex(x0, y0, r.Color)
		vertex(x1, y0, r.Color)
		vertex(x1, y1, r.Color)
		vertex(x0, y0, r.Color)
		vertex(x1, y1, r.Color)
		vertex(x0, y1, r.Color)
	}
	return g.uploadDynamic(dev, 4, data)
}

func (g *gpuRenderer) uploadLines(dev *wgpu.Device, f Frame) error {
	var data []byte
	for _, v := range f.DynamicLines {
		data = append(data, floats(v.Position.X, v.Position.Y, v.Position.Z, 1, 0, 0, 0, 0, float32(v.Color.R)/255, float32(v.Color.G)/255, float32(v.Color.B)/255, 0)...)
	}
	return g.uploadDynamic(dev, 5, data)
}

func (g *gpuRenderer) uploadDynamic(dev *wgpu.Device, index int, data []byte) error {
	b := &g.batches[index]
	releaseBatch(*b)
	*b = gpuBatch{}
	return g.uploadBatch(dev, b, data, g.transforms)
}

func (g *gpuRenderer) Render(f Frame) (image.Image, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := validateFrame(f); err != nil {
		return nil, err
	}
	if g.closed {
		return nil, fmt.Errorf("%w: renderer closed", ErrGPUUnavailable)
	}
	if g.initErr != nil {
		return nil, g.initErr
	}
	if g.r == nil {
		if err := g.setup(); err != nil {
			g.initErr = fmt.Errorf("%w: %w", ErrGPUUnavailable, err)
			return nil, g.initErr
		}
	}
	if w, h := g.r.Size(); w != f.Width || h != f.Height {
		if err := g.r.Resize(f.Width, f.Height); err != nil {
			return nil, err
		}
	}
	if err := g.prepareProcedural(f); err != nil {
		return nil, err
	}
	if err := g.prepareTextures(f); err != nil {
		return nil, err
	}
	ub := append(floats(f.Matrix[:]...), floats(float32(f.Width), float32(f.Height), 0, 0, f.Light.Direction.X, f.Light.Direction.Y, f.Light.Direction.Z, f.Light.Ambient, 0, 0, 0, 0)...)
	img, _, err := g.r.Render(context.Background(), func(frame *gpuimage.Frame) error {
		if err := g.drawProcedural(frame); err != nil {
			return err
		}
		dev := frame.Device
		if err := g.uploadIdentity(dev); err != nil {
			return err
		}
		if err := g.prepareGeometry(dev, f); err != nil {
			return err
		}
		if err := g.uploadLines(dev, f); err != nil {
			return err
		}
		if err := g.uploadUI(dev, f); err != nil {
			return err
		}
		if err := dev.Queue().WriteBuffer(g.uniform, 0, ub); err != nil {
			return err
		}
		pass, err := frame.Encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: frame.Color, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore, ClearValue: gputypes.Color{R: float64(f.Background.R) / 255, G: float64(f.Background.G) / 255, B: float64(f.Background.B) / 255, A: 1}}}, DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{View: frame.Depth, DepthLoadOp: gputypes.LoadOpClear, DepthStoreOp: gputypes.StoreOpStore, DepthClearValue: 1, StencilReadOnly: true}})
		if err != nil {
			return err
		}
		for _, i := range []int{5, 0, 1, 2, 3, 6, 4} {
			if i == 2 {
				for index, node := range g.nodes {
					b := node.batches[7]
					if b.count == 0 {
						continue
					}
					pass.SetPipeline(g.texturePipeline)
					pass.SetBindGroup(0, b.bind, nil)
					src := f.Geometry[index].Material.Texture
					if t := g.procedural[src]; t != nil {
						pass.SetBindGroup(1, t.target.BindGroup(), nil)
					} else {
						pass.SetBindGroup(1, g.textures[src].BindGroup(), nil)
					}
					pass.Draw(gputypes.DrawArgs{VertexCount: b.count, InstanceCount: 1})
				}
			}
			if i == 4 || i == 5 {
				g.drawBatch(pass, i, g.batches[i])
				continue
			}
			for _, node := range g.nodes {
				g.drawBatch(pass, i, node.batches[i])
			}
		}
		return pass.End()
	})
	return img, err
}

func (g *gpuRenderer) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	if g.r == nil {
		return nil
	}
	for src, t := range g.procedural {
		g.releaseProcedural(t)
		delete(g.procedural, src)
	}
	for src, t := range g.textures {
		_ = t.Close()
		delete(g.textures, src)
	}
	_ = g.r.Do(func(*wgpu.Device) error {
		for key, node := range g.geometries {
			node.release()
			delete(g.geometries, key)
		}
		g.nodes = nil
		if g.texturePipeline != nil {
			g.texturePipeline.Release()
		}
		if g.textureLayout != nil {
			g.textureLayout.Release()
		}
		for _, b := range g.batches {
			if b.bind != nil {
				b.bind.Release()
			}
			if b.buffer != nil {
				b.buffer.Release()
			}
		}
		for _, p := range g.pipelines {
			if p != nil {
				p.Release()
			}
		}
		if g.transforms != nil {
			g.transforms.Release()
		}
		if g.uniform != nil {
			g.uniform.Release()
		}
		if g.layout != nil {
			g.layout.Release()
		}
		if g.bindLayout != nil {
			g.bindLayout.Release()
		}
		if g.shader != nil {
			g.shader.Release()
		}
		return nil
	})
	err := g.r.Close()
	g.r = nil
	return err
}

// Dynamic world-space decorations always use an identity transform.
func (g *gpuRenderer) uploadIdentity(dev *wgpu.Device) error {
	if g.transforms != nil {
		return nil
	}
	var err error
	g.transforms, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: 128, Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst})
	if err != nil {
		return err
	}
	if err = writeTransform(dev, g.transforms, Instance{World: nt3dmath.Identity(), Visible: true}); err != nil {
		g.transforms.Release()
		g.transforms = nil
	}
	return err
}

func (g *gpuRenderer) drawBatch(pass *wgpu.RenderPassEncoder, kind int, b gpuBatch) {
	if b.count == 0 {
		return
	}
	pass.SetPipeline(g.pipelines[kind])
	pass.SetBindGroup(0, b.bind, nil)
	v, n := b.count, uint32(1)
	switch kind {
	case 0:
		v, n = 6, b.count
	case 2:
		v, n = 36, b.count
	case 3, 5:
		v, n = 6, b.count/2
	case 6:
		v, n = 9, b.count
	}
	pass.Draw(gputypes.DrawArgs{VertexCount: v, InstanceCount: n})
}
