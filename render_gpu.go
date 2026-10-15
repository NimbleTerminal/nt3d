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

// uniformSize is the scene uniform's bytes: matrix, viewport, light, eye,
// forward, fog, fog color.
const uniformSize = 160

type gpuBatch struct {
	buffer *wgpu.Buffer
	bind   *wgpu.BindGroup // Frame-wide batches only; geometry binds per instance set.
	count  uint32
	size   uint64
}

type gpuRenderer struct {
	textureLayout   *wgpu.PipelineLayout
	texturePipeline *wgpu.RenderPipeline
	cutoutPipeline  *wgpu.RenderPipeline
	// Transparent meshes' back faces, then front faces: untextured and textured.
	blendPipelines [2]*wgpu.RenderPipeline
	textureBlends  [2]*wgpu.RenderPipeline
	textures       map[textureKey]*gpuimage.Texture // Image textures, by addressing.
	procedural     map[*Texture]*proceduralTexture
	geometries     map[geometryKey]*gpuGeometry
	nodes          []*gpuGeometry // By frame geometry index.
	draws          []gpuDraw      // The frame's draws of geometry.
	blendDraw      map[int]int    // A transparent frame geometry's draw.
	textureUploads uint64
	mu             sync.Mutex
	closed         bool
	initErr        error
	r              *gpuimage.Renderer
	shader         *wgpu.ShaderModule
	bindLayout     *wgpu.BindGroupLayout
	layout         *wgpu.PipelineLayout
	uniform        *wgpu.Buffer
	transforms     *wgpu.Buffer
	pipelines      [7]*wgpu.RenderPipeline
	batches        [7]gpuBatch // points, triangles, boxes, lines, overlays, dynamic lines, arrows
	uploads        uint64
	target         sceneTarget
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
		g.bindLayout, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}}, {Binding: 1, Visibility: wgpu.ShaderStageVertex, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage}}, {Binding: 2, Visibility: wgpu.ShaderStageVertex, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage}}}})
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
			fragment := "fs_main"
			if i == 4 {
				fragment = "fs_overlay" // Overlays: no highlight or fog.
			}
			g.pipelines[i], err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: entry, Layout: g.layout, Vertex: wgpu.VertexState{Module: g.shader, EntryPoint: entry}, Primitive: primitive, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth24Plus, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual}, Multisample: gputypes.DefaultMultisampleState(), Fragment: &wgpu.FragmentState{Module: g.shader, EntryPoint: fragment, Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}}})
			if err != nil {
				return err
			}
		}
		for i, entry := range []string{"vs_mesh_back", "vs_mesh"} {
			if g.blendPipelines[i], err = blendPipeline(dev, g.layout, g.shader, entry, "fs_main", i); err != nil {
				return err
			}
		}
		g.uniform, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: uniformSize, Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst})
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
	if err := g.prepareTarget(f); err != nil {
		return nil, err
	}
	perspective, linear := float32(0), float32(0)
	if f.Perspective {
		perspective = 1
	}
	if f.Linear {
		linear = 1
	}
	// The viewport's third component says whether to work in linear light,
	// and the ambient level is in that space.
	ub := append(floats(f.Matrix[:]...), floats(float32(f.Width), float32(f.Height), linear, 0, f.Light.Direction.X, f.Light.Direction.Y, f.Light.Direction.Z, ambientLight(&f),
		f.Eye.X, f.Eye.Y, f.Eye.Z, perspective, f.Forward.X, f.Forward.Y, f.Forward.Z, 0)...)
	// Fog's near and far distances (equal when there is none) and color, in
	// the frame's color space.
	var fog [8]float32
	if f.Fog.on() {
		c := f.Fog.color(&f)
		fog = [8]float32{f.Fog.Near, f.Fog.Far, 0, 0, float32(c.R) / 255, float32(c.G) / 255, float32(c.B) / 255, 1}
		if f.Linear {
			for i := 4; i < 7; i++ {
				fog[i] = float32(decodeSRGB(float64(fog[i])))
			}
		}
	}
	ub = append(ub, floats(fog[:]...)...)
	drawn := f.drawn(1)
	img, _, err := g.r.Render(context.Background(), func(frame *gpuimage.Frame) error {
		if err := g.drawProcedural(frame); err != nil {
			return err
		}
		dev := frame.Device
		if err := g.uploadIdentity(dev); err != nil {
			return err
		}
		if err := g.prepareGeometry(dev, f, drawn); err != nil {
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
		// The scene draws into the scene target, which a resolve pass
		// averages down and encodes. Pixel sizes stay the frame's, through
		// the uniform viewport, so points and lines keep their size when
		// supersampled.
		return g.renderScene(frame, f, func(color, depth *wgpu.TextureView) error { return g.drawScene(f, drawn, frame.Encoder, color, depth) })
	})
	return img, err
}

// background is the frame's background in the scene target's color space.
func background(f Frame) gputypes.Color {
	c := [3]float64{float64(f.Background.R) / 255, float64(f.Background.G) / 255, float64(f.Background.B) / 255}
	if f.Linear {
		for i := range c {
			c[i] = decodeSRGB(c[i])
		}
	}
	return gputypes.Color{R: c[0], G: c[1], B: c[2], A: 1}
}

// drawScene records the scene's pass into a color and depth target.
func (g *gpuRenderer) drawScene(f Frame, drawn []bool, encoder *wgpu.CommandEncoder, color, depth *wgpu.TextureView) error {
	{
		pass, err := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: color, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore, ClearValue: background(f)}}, DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{View: depth, DepthLoadOp: gputypes.LoadOpClear, DepthStoreOp: gputypes.StoreOpStore, DepthClearValue: 1, StencilReadOnly: true}})
		if err != nil {
			return err
		}
		// Draws hold the drawn instances: hidden and culled nodes keep their
		// buffers but are not drawn. Meshes draw each run of instances at once;
		// other primitives belong to unshared geometry, one instance each.
		// Transparent triangles wait for everything opaque.
		for _, i := range []int{5, 0, 1, 2, 3, 6, 4} {
			if i == 4 {
				// Then blend them, farthest first, back faces before front.
				for _, index := range f.blendOrder(drawn) {
					d := g.draws[g.blendDraw[index]]
					for side := range 2 {
						if b := d.geom.batches[1]; b.count > 0 {
							pass.SetPipeline(g.blendPipelines[side])
							pass.SetBindGroup(0, d.set.binds[1], nil)
							pass.Draw(gputypes.DrawArgs{VertexCount: b.count, InstanceCount: d.count})
						}
						if d.geom.batches[7].count > 0 {
							pass.SetPipeline(g.textureBlends[side])
							g.drawTextured(pass, f, d)
						}
					}
				}
			}
			if i == 2 {
				for _, d := range g.draws {
					if d.blended || d.geom.batches[7].count == 0 {
						continue
					}
					if f.Geometry[d.index].Material.Cutout {
						pass.SetPipeline(g.cutoutPipeline)
					} else {
						pass.SetPipeline(g.texturePipeline)
					}
					g.drawTextured(pass, f, d)
				}
			}
			if i == 4 || i == 5 {
				g.drawBatch(pass, i, g.batches[i], g.batches[i].bind)
				continue
			}
			for _, d := range g.draws {
				b := d.geom.batches[i]
				switch {
				case b.count == 0 || i == 1 && d.blended:
				case i == 1:
					pass.SetPipeline(g.pipelines[1])
					pass.SetBindGroup(0, d.set.binds[1], nil)
					pass.Draw(gputypes.DrawArgs{VertexCount: b.count, InstanceCount: d.count})
				default:
					g.drawBatch(pass, i, b, d.set.binds[i])
				}
			}
		}
		return pass.End()
	}
}

// blendPipeline draws a transparent mesh's back faces (side 0) or front faces
// (side 1), blending by alpha, depth tested without writing depth.
func blendPipeline(dev *wgpu.Device, layout *wgpu.PipelineLayout, shader *wgpu.ShaderModule, vertex, fragment string, side int) (*wgpu.RenderPipeline, error) {
	cull := gputypes.CullModeFront
	if side == 1 {
		cull = gputypes.CullModeBack
	}
	blend := gputypes.BlendStateAlpha()
	return dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: "transparent " + vertex, Layout: layout, Vertex: wgpu.VertexState{Module: shader, EntryPoint: vertex}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: cull}, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth24Plus, DepthWriteEnabled: false, DepthCompare: gputypes.CompareFunctionLessEqual}, Multisample: gputypes.DefaultMultisampleState(), Fragment: &wgpu.FragmentState{Module: shader, EntryPoint: fragment, Targets: []gputypes.ColorTargetState{{Format: sceneFormat, Blend: &blend, WriteMask: gputypes.ColorWriteMaskAll}}}})
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
		if g.cutoutPipeline != nil {
			g.cutoutPipeline.Release()
		}
		if g.texturePipeline != nil {
			g.texturePipeline.Release()
		}
		for _, p := range append(g.blendPipelines[:], g.textureBlends[:]...) {
			if p != nil {
				p.Release()
			}
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
	g.releaseTarget()
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
	if err = writeTransform(dev, g.transforms, Instance{World: nt3dmath.Identity(), Visible: true}, nil); err != nil {
		g.transforms.Release()
		g.transforms = nil
	}
	return err
}

// drawTextured draws a geometry's textured triangles with the pipeline set.
// drawTextured draws a run's textured triangles with the pipeline set.
func (g *gpuRenderer) drawTextured(pass *wgpu.RenderPassEncoder, f Frame, d gpuDraw) {
	pass.SetBindGroup(0, d.set.binds[7], nil)
	src := f.Geometry[d.index].Material.Texture
	if t := g.procedural[src]; t != nil {
		pass.SetBindGroup(1, t.target.BindGroup(), nil)
	} else {
		pass.SetBindGroup(1, g.textures[textureKey{src, f.Geometry[d.index].Material.Repeat, f.anisotropy()}].BindGroup(), nil)
	}
	pass.Draw(gputypes.DrawArgs{VertexCount: d.geom.batches[7].count, InstanceCount: d.count})
}

func (g *gpuRenderer) drawBatch(pass *wgpu.RenderPassEncoder, kind int, b gpuBatch, bind *wgpu.BindGroup) {
	if b.count == 0 {
		return
	}
	pass.SetPipeline(g.pipelines[kind])
	pass.SetBindGroup(0, bind, nil)
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
