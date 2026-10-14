// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"context"
	_ "embed"
	"encoding/binary"
	"errors"
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
	texturedBatches []texturedBatch
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
	transformBytes  uint64
	pipelines       [7]*wgpu.RenderPipeline
	batches         [7]gpuBatch // points, triangles, boxes, lines, overlays, dynamic lines, arrows
	revision        uint64
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

func (g *gpuRenderer) upload(dev *wgpu.Device, f Frame) error {
	var data [7][]byte
	nodeIndex := float32(0)
	vertex := func(kind int, v Vertex, radius float32) {
		data[kind] = append(data[kind], floats(v.Position.X, v.Position.Y, v.Position.Z, radius, v.Normal.X, v.Normal.Y, v.Normal.Z, 0, float32(v.Color.R)/255, float32(v.Color.G)/255, float32(v.Color.B)/255, nodeIndex)...)
	}
	for index, geom := range f.Geometry {
		nodeIndex = float32(index + 1)
		for _, p := range geom.Points {
			vertex(0, Vertex{Position: p.Position, Color: p.Color}, p.Radius)
		}
		for _, i := range geom.Indices {
			if !textured(geom) {
				v := geom.Vertices[i]
				if geom.Material != nil && geom.Material.Unlit {
					v.Normal.X = 0
					v.Normal.Y = 0
					v.Normal.Z = 0
				}
				vertex(1, v, 1)
			}
		}
		for _, b := range geom.Boxes {
			vertex(2, Vertex{b.Min, b.Size, b.Color}, 1)
		}
		for _, v := range geom.Lines {
			vertex(3, v, lineWidth(geom.LineWidth))
		}
		for _, a := range geom.Arrows {
			data[6] = append(data[6], floats(a.Start.X, a.Start.Y, a.Start.Z, lineWidth(a.Width), a.End.X, a.End.Y, a.End.Z, a.HeadSize, float32(a.Color.R)/255, float32(a.Color.G)/255, float32(a.Color.B)/255, nodeIndex)...)
		}
	}
	for i, d := range data {
		if i == 4 || i == 5 {
			continue
		}
		old := &g.batches[i]
		if old.bind != nil {
			old.bind.Release()
		}
		if old.buffer != nil {
			old.buffer.Release()
		}
		*old = gpuBatch{}
		if len(d) == 0 {
			continue
		}
		var err error
		old.buffer, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: uint64(len(d)), Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst})
		if err != nil {
			return err
		}
		if err = dev.Queue().WriteBuffer(old.buffer, 0, d); err != nil {
			return err
		}
		old.bind, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: g.bindLayout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: g.uniform, Size: 112}, {Binding: 1, Buffer: old.buffer, Size: uint64(len(d))}, {Binding: 2, Buffer: g.transforms, Size: g.transformBytes}}})
		if err != nil {
			return err
		}
		old.count = uint32(len(d) / 48)
	}
	if err := g.uploadTexturedMeshes(dev, f); err != nil {
		return err
	}
	g.revision = f.Revision
	g.uploads++
	return nil
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
	if b.bind != nil {
		b.bind.Release()
	}
	if b.buffer != nil {
		b.buffer.Release()
	}
	*b = gpuBatch{}
	if len(data) == 0 {
		return nil
	}
	var err error
	b.buffer, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: uint64(len(data)), Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst})
	if err != nil {
		return err
	}
	if err = dev.Queue().WriteBuffer(b.buffer, 0, data); err != nil {
		return err
	}
	b.bind, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: g.bindLayout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: g.uniform, Size: 112}, {Binding: 1, Buffer: b.buffer, Size: uint64(len(data))}, {Binding: 2, Buffer: g.transforms, Size: g.transformBytes}}})
	if err != nil {
		return err
	}
	b.count = uint32(len(data) / 48)
	return nil
}

func (g *gpuRenderer) Render(f Frame) (image.Image, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := validateFrame(f); err != nil {
		return nil, err
	}
	if g.closed {
		return nil, errors.New("nt3d: renderer closed")
	}
	if g.initErr != nil {
		return nil, g.initErr
	}
	if g.r == nil {
		if err := g.setup(); err != nil {
			g.initErr = err
			return nil, err
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
		if err := g.uploadTransforms(dev, f); err != nil {
			return err
		}
		if g.uploads == 0 || g.revision != f.Revision {
			if err := g.upload(dev, f); err != nil {
				return err
			}
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
				for _, b := range g.texturedBatches {
					pass.SetPipeline(g.texturePipeline)
					pass.SetBindGroup(0, b.bind, nil)
					src := f.Geometry[b.geometry].Material.Texture
					if t := g.procedural[src]; t != nil {
						pass.SetBindGroup(1, t.target.BindGroup(), nil)
					} else {
						pass.SetBindGroup(1, g.textures[src].BindGroup(), nil)
					}
					pass.Draw(gputypes.DrawArgs{VertexCount: b.count, InstanceCount: 1})
				}
			}
			b := g.batches[i]
			if b.count == 0 {
				continue
			}
			pass.SetPipeline(g.pipelines[i])
			pass.SetBindGroup(0, b.bind, nil)
			v, n := b.count, uint32(1)
			if i == 0 {
				v, n = 6, b.count
			}
			if i == 2 {
				v, n = 36, b.count
			}
			if i == 3 || i == 5 {
				v, n = 6, b.count/2
			}
			if i == 6 {
				v, n = 9, b.count
			}
			pass.Draw(gputypes.DrawArgs{VertexCount: v, InstanceCount: n})
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
		for _, b := range g.texturedBatches {
			releaseBatch(b.gpuBatch)
		}
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

// uploadTransforms preserves geometry buffers on camera/transform/visibility edits.
func (g *gpuRenderer) uploadTransforms(dev *wgpu.Device, f Frame) error {
	size := uint64(len(f.Geometry)+1) * 128
	if g.transforms == nil || g.transformBytes != size {
		if g.transforms != nil {
			g.transforms.Release()
		}
		var err error
		g.transforms, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: size, Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst})
		if err != nil {
			return err
		}
		g.transformBytes = size
		g.uploads = 0
	}
	data := make([]byte, 0, size)
	put := func(m nt3dmath.Mat4, visible bool) {
		inv, _ := m.InverseAffine()
		normal := inv.Transpose()
		normal[15] = 0
		if visible {
			normal[15] = 1
		}
		data = append(data, floats(m[:]...)...)
		data = append(data, floats(normal[:]...)...)
	}
	put(nt3dmath.Identity(), true)
	for i := range f.Geometry {
		v := instance(f, i)
		put(v.World, v.Visible)
	}
	return dev.Queue().WriteBuffer(g.transforms, 0, data)
}
