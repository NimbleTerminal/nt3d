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
	"nimbleterminal.dev/nt3d/sh"
	gpuimage "nimbleterminal.dev/ntgpuimage"
	"slices"
	"sync"
)

//go:embed scene.wgsl
var sceneShader string

// sceneShaderSource is the scene shader with the constants it shares with
// Go declared ahead of it: maxLights sizes its lights, and maxPointPixels
// clamps points in world units.
func sceneShaderSource() string {
	return fmt.Sprintf("const maxLights: u32 = %du;\nconst maxPointPixels: f32 = %d.0;\n", MaxLights, MaxPointPixels) + nt3dsh.WGSL() + sceneShader
}

// uniformSize is the scene uniform's bytes: matrix, viewport, sky ambient
// and light count, eye, forward, fog, fog color, ground ambient, and
// MaxLights lights of two vec4s each, shadow matrix and lookup params,
// nine diffuse environment coefficients, three sky camera basis vectors, and
// six light-view matrices (one used for a directional light).
const uniformSize = 160 + 16 + MaxLights*32 + 80 + 192 + 6*64 + 64

type gpuBatch struct {
	buffer *wgpu.Buffer
	bind   *wgpu.BindGroup // Frame-wide batches only; geometry binds per instance set.
	count  uint32
	size   uint64
}

type gpuRenderer struct {
	screen                 sceneSnapshot
	reflection             reflectionTexture
	environmentSource      *Environment
	environmentTexture     *gpuimage.Texture
	environmentPipeline    *wgpu.RenderPipeline
	environmentBind        *wgpu.BindGroup
	skinPipeline           *wgpu.ComputePipeline
	skinLayout             *wgpu.BindGroupLayout
	sort                   gpuSort   // The frame-wide blended point sort.
	plan                   blendPlan // The frame's blended work, as the sort and the draws see it.
	lod                    lodPlan   // The splats of plan's slots the sort keys: its points are their total.
	dropPipeline           *wgpu.RenderPipeline
	outlinePipelines       [5]*wgpu.RenderPipeline
	shadowLayout           *wgpu.BindGroupLayout
	noShadow, shadowTarget shadowTarget
	shadowActive           bool
	shadowPipelines        [4]*wgpu.RenderPipeline
	textureLayout          *wgpu.PipelineLayout
	texturePipeline        *wgpu.RenderPipeline
	cutoutPipeline         *wgpu.RenderPipeline
	// Transparent meshes' back faces, then front faces: untextured and textured.
	blendPipelines [2]*wgpu.RenderPipeline
	textureBlends  [2]*wgpu.RenderPipeline
	// Points bind their records and order in place of items: opaque points
	// through pipelines[0], blended ones through pointBlends, alpha then
	// additive, hard then soft then Gaussian. Alpha points read the sort's
	// order from their run of it (binding 6, at a dynamic offset). zeros
	// stands in for unsorted points' order and fills binding 1 (see setup);
	// zeroRun stands in for the run table, an empty run at offset 0.
	pointBindLayout *wgpu.BindGroupLayout
	pointLayout     *wgpu.PipelineLayout
	spriteLayout    *wgpu.PipelineLayout
	spritePipelines [3]*wgpu.RenderPipeline
	pointBlends     [2][3]*wgpu.RenderPipeline
	zeros, zeroRun  *wgpu.Buffer
	textures        map[textureKey]*gpuimage.Texture // Image textures, by addressing.
	procedural      map[proceduralKey]*proceduralTexture
	geometries      map[geometryKey]*gpuGeometry
	nodes           []*gpuGeometry // By frame geometry index.
	draws           []gpuDraw      // The frame's draws of geometry.
	blendDraw       map[int]int    // A transparent frame geometry's draw.
	sortedRuns      []sortedRun    // The frame's alpha point slots, resolved once, in plan order.
	runOffsets      []uint32       // Each slice's run offset for each slot, one dynamic offset apiece.
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
	target          sceneTarget
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
		g.shader, err = dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "nt3d", WGSL: sceneShaderSource()})
		if err != nil {
			return err
		}
		g.bindLayout, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}}, {Binding: 1, Visibility: wgpu.ShaderStageVertex, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage}}, {Binding: 2, Visibility: wgpu.ShaderStageVertex, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage}}}})
		if err != nil {
			return err
		}
		g.shadowLayout, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeDepth, ViewDimension: gputypes.TextureViewDimension2DArray}}, {Binding: 1, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeFloat, ViewDimension: gputypes.TextureViewDimension2D}}, {Binding: 2, Visibility: wgpu.ShaderStageFragment, Sampler: &gputypes.SamplerBindingLayout{Type: gputypes.SamplerBindingTypeFiltering}}, {Binding: 3, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeUnfilterableFloat, ViewDimension: gputypes.TextureViewDimension2D}}, {Binding: 4, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeDepth, ViewDimension: gputypes.TextureViewDimension2D}}}})
		if err != nil {
			return err
		}
		g.layout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{g.bindLayout, g.shadowLayout}})
		if err != nil {
			return err
		}
		// Metal binds a group's buffers to consecutive slots, while the
		// translated shader reads binding n from slot n, so the bindings run
		// 0 to 8 without a gap. The shader also reads array lengths, for its
		// bounds checks, from its first unused slot, 1 in the point entry
		// points; zeros there leave them unchecked, as an unbound slot
		// leaves the other entry points.
		storage := func(binding uint32) gputypes.BindGroupLayoutEntry {
			return gputypes.BindGroupLayoutEntry{Binding: binding, Visibility: wgpu.ShaderStageVertex, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage}}
		}
		g.pointBindLayout, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}}, storage(1), storage(2), storage(3), storage(4), storage(5), {Binding: 6, Visibility: wgpu.ShaderStageVertex, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform, HasDynamicOffset: true, MinBindingSize: 16}}, storage(7), storage(8)}})
		if err != nil {
			return err
		}
		g.pointLayout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{g.pointBindLayout, g.shadowLayout}})
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
			layout := g.layout
			if i == 0 {
				layout = g.pointLayout
			}
			g.pipelines[i], err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: entry, Layout: layout, Vertex: wgpu.VertexState{Module: g.shader, EntryPoint: entry}, Primitive: primitive, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual}, Multisample: gputypes.DefaultMultisampleState(), Fragment: &wgpu.FragmentState{Module: g.shader, EntryPoint: fragment, Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}}})
			if err != nil {
				return err
			}
		}
		for i, entry := range []string{"vs_mesh_back", "vs_mesh"} {
			if g.blendPipelines[i], err = blendPipeline(dev, g.layout, g.shader, entry, "fs_main", i); err != nil {
				return err
			}
		}
		for blend, vertex := range [2]string{"vs_point_sorted", "vs_point"} {
			for shape, fragment := range [2][3]string{{"fs_point_alpha", "fs_point_alpha_soft", "fs_gaussian"}, {"fs_point_add", "fs_point_add_soft", "fs_gaussian"}}[blend] {
				if blend == 1 && shape == int(GaussianPoint) {
					continue
				} // Gaussians require alpha blending.
				entry := vertex
				if shape == int(GaussianPoint) {
					entry = "vs_gaussian"
				}
				if g.pointBlends[blend][shape], err = pointPipeline(dev, g.pointLayout, g.shader, entry, fragment, blend == 1); err != nil {
					return err
				}
			}
		}
		if g.dropPipeline, err = pointPipeline(dev, g.layout, g.shader, "vs_drop", "fs_point_alpha_soft", false); err != nil {
			return err
		}
		if g.zeros, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: 48, Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst}); err != nil {
			return err
		}
		if g.zeroRun, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: sortRecord, Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst}); err != nil {
			return err
		}
		g.uniform, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: uniformSize, Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst})
		if err != nil {
			return err
		}
		if err = g.prepareSnapshot(dev, 1, 1); err != nil {
			return err
		}
		if err = g.updateReflection(dev, nil); err != nil {
			return err
		}
		g.noShadow, err = g.makeShadowTarget(dev, 1, 1)
		if err != nil {
			return err
		}
		return g.setupShadowPipelines(dev)
	})
}

func floats(values ...float32) []byte { return appendFloats(make([]byte, 0, len(values)*4), values...) }

// appendFloats appends values to dst, little-endian.
func appendFloats(dst []byte, values ...float32) []byte {
	for _, v := range values {
		dst = binary.LittleEndian.AppendUint32(dst, math.Float32bits(v))
	}
	return dst
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
	if err := g.prepareEnvironment(f); err != nil {
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
	// and the lighting is in that space: the sky's ambient color, carrying
	// the number of lights, here; the ground's and the lights after fog.
	rig := newRig(&f)
	environmentActive := float32(0)
	if rig.environmentActive {
		environmentActive = 1
	}
	ub := append(floats(f.Matrix[:]...), floats(float32(f.Width), float32(f.Height), linear, environmentActive, rig.sky[0], rig.sky[1], rig.sky[2], float32(len(rig.lights)),
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
	// Each light: its direction toward it or its position, and its kind;
	// its color times intensity, and its range. Unused lights are zero.
	var lights [MaxLights * 8]float32
	for i, l := range rig.lights {
		copy(lights[i*8:], []float32{l.v.X, l.v.Y, l.v.Z, float32(l.kind), l.color[0], l.color[1], l.color[2], l.rng})
	}
	environmentStrength := f.EnvironmentStrength
	if environmentStrength == 0 {
		environmentStrength = 1
	}
	ub = append(ub, floats(rig.ground[0], rig.ground[1], rig.ground[2], environmentStrength)...)
	ub = append(ub, floats(lights[:]...)...)
	drawn := f.drawn(1)
	snapshot := needsSceneSnapshot(f, drawn)
	plan, err := f.planBlend(drawn)
	if err != nil {
		return nil, err
	}
	g.plan = plan
	g.lod = f.planLOD(&g.plan)
	size := shadowMapSize
	if i := shadowCaster(f.Lighting); i >= 0 && f.Lighting.Lights[i].Kind == PointLight {
		size = pointShadowMapSize
	}
	views := fitShadows(&f, drawn, size)
	g.shadowActive = len(views) > 0
	var view shadowView
	params := [4]float32{-1, 0, 0, 0}
	if g.shadowActive {
		view = views[0]
		params = [4]float32{float32(view.light), 1 / float32(size), view.texel, float32(size)}
	}
	ub = append(ub, floats(view.matrix[:]...)...)
	ub = append(ub, floats(params[:]...)...)
	for _, c := range rig.environment {
		ub = append(ub, floats(c[0], c[1], c[2], 0)...)
	}
	right, up, forward := f.skyBasis()
	for _, v := range []nt3dmath.Vec3{right, up, forward} {
		ub = append(ub, floats(v.X, v.Y, v.Z, 0)...)
	}
	for i := 0; i < 6; i++ {
		var matrix nt3dmath.Mat4
		if i < len(views) {
			matrix = views[i].matrix
		}
		ub = append(ub, floats(matrix[:]...)...)
	}
	inverse, _ := inverseProjection(f.Matrix)
	ub = append(ub, floats(inverse[:]...)...)
	img, _, err := g.r.Render(context.Background(), func(frame *gpuimage.Frame) error {
		if err := g.drawProcedural(frame); err != nil {
			return err
		}
		dev := frame.Device
		if snapshot {
			if err := g.prepareSnapshot(dev, g.target.w, g.target.h); err != nil {
				return err
			}
		}
		if err := g.prepareOutlines(dev, f); err != nil {
			return err
		}
		if g.shadowActive && (g.shadowTarget.texture == nil || g.shadowTarget.size != size || len(g.shadowTarget.faces) != len(views)) {
			g.shadowTarget.release()
			var err error
			g.shadowTarget, err = g.makeShadowTarget(dev, size, len(views))
			if err != nil {
				return err
			}
		}
		if err := g.uploadIdentity(dev); err != nil {
			return err
		}
		// The sort's buffers grow before the point bind groups that hold
		// them are made, and it sorts once the points are on the device.
		if g.plan.points > 0 {
			if err := g.initSort(dev); err != nil {
				return err
			}
			if err := g.sort.ensure(dev, g.plan.points, len(g.plan.slots)); err != nil {
				return err
			}
		}
		if err := g.prepareGeometry(dev, f, drawn); err != nil {
			return err
		}
		if err := g.skinGeometry(dev, frame.Encoder, f); err != nil {
			return err
		}
		if err := g.encodeSort(dev, frame.Encoder, f, &g.plan, &g.lod); err != nil {
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
		return g.renderScene(frame, f, func(color, depth *wgpu.TextureView) error {
			return g.drawScene(f, snapshot, frame.Encoder, color, depth)
		})
	})
	if err != nil {
		for _, node := range g.geometries {
			if node.skin != nil {
				node.skin.revision = 0
			}
		}
	}
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
func (g *gpuRenderer) drawScene(f Frame, snapshot bool, encoder *wgpu.CommandEncoder, color, depth *wgpu.TextureView) error {
	{
		if g.shadowActive {
			if err := g.drawShadow(f, encoder); err != nil {
				return err
			}
		}

		pass, err := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: color, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore, ClearValue: background(f)}}, DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{View: depth, DepthLoadOp: gputypes.LoadOpClear, DepthStoreOp: gputypes.StoreOpStore, DepthClearValue: 1, StencilReadOnly: true}})
		if err != nil {
			return err
		}
		shadowBind := g.noShadow.bind
		if g.shadowActive {
			shadowBind = g.shadowTarget.bind
		}
		pass.SetBindGroup(1, shadowBind, nil)
		if f.Environment != nil {
			pass.SetPipeline(g.environmentPipeline)
			pass.SetBindGroup(0, g.environmentBind, nil)
			pass.SetBindGroup(2, g.environmentTexture.BindGroup(), nil)
			pass.Draw(gputypes.DrawArgs{VertexCount: 3, InstanceCount: 1})
		}
		// Draws hold the drawn instances: hidden and culled nodes keep their
		// buffers but are not drawn. Meshes draw each run of instances at once;
		// other primitives belong to unshared geometry, one instance each.
		// Transparent triangles wait for everything opaque.
		for _, i := range []int{5, 0, 1, 2, 3, 6, 4} {
			if i == 4 {
				for _, d := range g.draws {
					if !transparent(&f.Geometry[d.index]) {
						g.drawOutline(pass, f, d)
					}
				}
				for _, d := range g.draws {
					if d.geom.batches[8].count > 0 {
						pass.SetPipeline(g.dropPipeline)
						pass.SetBindGroup(0, d.set.binds[8], nil)
						pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: d.count})
					}
				}
				// Freeze opaque color/depth, then transmit from that immutable snapshot.
				if snapshot {
					if err := pass.End(); err != nil {
						return err
					}
					if err = g.captureScene(encoder); err != nil {
						return err
					}
					pass, err = g.resumeScene(encoder, color, depth, shadowBind)
					if err != nil {
						return err
					}
					transmitted := false
					for _, d := range g.draws {
						if !transmitting(&f.Geometry[d.index]) {
							continue
						}
						transmitted = true
						if b := d.geom.batches[1]; b.count > 0 {
							pass.SetPipeline(g.pipelines[1])
							pass.SetBindGroup(0, d.set.binds[1], nil)
							pass.Draw(gputypes.DrawArgs{VertexCount: b.count, InstanceCount: d.count})
						}
						if d.geom.batches[7].count > 0 {
							pass.SetPipeline(g.texturePipeline)
							g.drawTextured(pass, f, d)
						}
					}
					if transmitted {
						if err := pass.End(); err != nil {
							return err
						}
						// Particles see the transmissive surface depth as well as opaque geometry.
						if err = g.captureScene(encoder); err != nil {
							return err
						}
						pass, err = g.resumeScene(encoder, color, depth, shadowBind)
						if err != nil {
							return err
						}
					}
				}
				// Then blend them, far to near in depth slices: each slice's
				// transparent meshes and additive points, then its run of every
				// alpha geometry's sorted points, for the slices its bounds span.
				g.resolveSortedRuns(f)
				var last *wgpu.RenderPipeline
				for slice := range blendSlices {
					if len(g.plan.bySlice[slice]) > 0 {
						for _, index := range g.plan.bySlice[slice] {
							g.drawBlended(pass, f, g.draws[g.blendDraw[index]])
						}
						last = nil
					}
					for slot := range g.sortedRuns {
						if span := g.plan.slotSlices[slot]; slice >= span[0] && slice <= span[1] {
							last = g.drawSortedRun(pass, g.sortedRuns[slot], slice, slot, last)
						}
					}
				}
			}
			if i == 2 {
				for _, d := range g.draws {
					if transparent(&f.Geometry[d.index]) || transmitting(&f.Geometry[d.index]) || d.geom.batches[7].count == 0 {
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
				case b.count == 0 || i == 1 && (transparent(&f.Geometry[d.index]) || transmitting(&f.Geometry[d.index])) || i == 0 && blendedPoints(&f.Geometry[d.index]):
				case i == 0 && f.Geometry[d.index].PointStyle.Texture != nil:
					g.drawSprite(pass, f, d)
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

// drawBlended draws a transparent mesh, its outline then back faces then
// front faces, or additive points, unsorted.
func (g *gpuRenderer) drawBlended(pass *wgpu.RenderPassEncoder, f Frame, d gpuDraw) {
	geom := &f.Geometry[d.index]
	if transparent(geom) {
		g.drawOutline(pass, f, d)
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
	if blendedPoints(geom) && geom.PointStyle.Blend == AdditiveBlend && d.geom.batches[0].count > 0 {
		if geom.PointStyle.Texture != nil {
			g.drawSprite(pass, f, d)
			return
		}
		pass.SetPipeline(g.pointBlends[1][geom.PointStyle.Shape])
		pass.SetBindGroup(0, d.set.binds[0], noRunOffset)
		pass.Draw(gputypes.DrawArgs{VertexCount: 6, InstanceCount: d.geom.batches[0].count})
	}
}

// sortedRun is an alpha point geometry's draw resolved for the slice loop:
// its pipeline and, for sprites, texture bind group. A run with no pipeline
// has no points.
type sortedRun struct {
	d        gpuDraw
	pipeline *wgpu.RenderPipeline
	texture  *wgpu.BindGroup
}

// resolveSortedRuns resolves each of the plan's alpha slots once, and the
// dynamic offset of each of its runs. The offsets live in a table, one
// element per draw, since some backends hold the slice a draw is given
// until the pass is encoded.
func (g *gpuRenderer) resolveSortedRuns(f Frame) {
	g.sortedRuns = g.sortedRuns[:0]
	for _, index := range g.plan.slots {
		d := g.draws[g.blendDraw[index]]
		run := sortedRun{d: d}
		if d.geom.batches[0].count > 0 {
			style := f.Geometry[d.index].PointStyle
			if style.Texture != nil {
				run.pipeline = g.spritePipelines[style.Blend]
				run.texture = g.textures[textureKey{src: style.Texture, aniso: f.anisotropy()}].BindGroup()
			} else {
				run.pipeline = g.pointBlends[0][style.Shape]
			}
		}
		g.sortedRuns = append(g.sortedRuns, run)
	}
	g.runOffsets = g.runOffsets[:0]
	for slice := range blendSlices {
		for slot := range g.sortedRuns {
			g.runOffsets = append(g.runOffsets, g.sort.runOffset(slice, slot))
		}
	}
}

// drawSortedRun draws a slot's run of the sorted points in a slice, as many
// as the sort counted there. It sets the pipeline only when it is not last,
// the pipeline of the draw before, and returns the pipeline now set.
func (g *gpuRenderer) drawSortedRun(pass *wgpu.RenderPassEncoder, run sortedRun, slice, slot int, last *wgpu.RenderPipeline) *wgpu.RenderPipeline {
	if run.pipeline == nil {
		return last
	}
	if run.pipeline != last {
		pass.SetPipeline(run.pipeline)
	}
	i := slice*len(g.sortedRuns) + slot
	pass.SetBindGroup(0, run.d.set.binds[0], g.runOffsets[i:i+1:i+1])
	if run.texture != nil {
		pass.SetBindGroup(2, run.texture, nil)
	}
	pass.DrawIndirect(g.sort.args, g.sort.argsOffset(slice, slot))
	return run.pipeline
}

// blendPipeline draws a transparent mesh's back faces (side 0) or front faces
// (side 1), blending by alpha, depth tested without writing depth.
func blendPipeline(dev *wgpu.Device, layout *wgpu.PipelineLayout, shader *wgpu.ShaderModule, vertex, fragment string, side int) (*wgpu.RenderPipeline, error) {
	cull := gputypes.CullModeFront
	if side == 1 {
		cull = gputypes.CullModeBack
	}
	blend := gputypes.BlendStateAlpha()
	return dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: "transparent " + vertex, Layout: layout, Vertex: wgpu.VertexState{Module: shader, EntryPoint: vertex}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: cull}, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: false, DepthCompare: gputypes.CompareFunctionLessEqual}, Multisample: gputypes.DefaultMultisampleState(), Fragment: &wgpu.FragmentState{Module: shader, EntryPoint: fragment, Targets: []gputypes.ColorTargetState{{Format: sceneFormat, Blend: &blend, WriteMask: gputypes.ColorWriteMaskAll}}}})
}

// pointPipeline draws blended points, depth tested without writing depth:
// over what is behind by alpha, or added to it.
func pointPipeline(dev *wgpu.Device, layout *wgpu.PipelineLayout, shader *wgpu.ShaderModule, vertex, fragment string, additive bool) (*wgpu.RenderPipeline, error) {
	blend := gputypes.BlendStateAlpha()
	if additive {
		blend.Color = gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorSrcAlpha, DstFactor: gputypes.BlendFactorOne, Operation: gputypes.BlendOperationAdd}
		blend.Alpha = gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorZero, DstFactor: gputypes.BlendFactorOne, Operation: gputypes.BlendOperationAdd}
	}
	return dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: fragment, Layout: layout, Vertex: wgpu.VertexState{Module: shader, EntryPoint: vertex}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone}, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: false, DepthCompare: gputypes.CompareFunctionLessEqual}, Multisample: gputypes.DefaultMultisampleState(), Fragment: &wgpu.FragmentState{Module: shader, EntryPoint: fragment, Targets: []gputypes.ColorTargetState{{Format: sceneFormat, Blend: &blend, WriteMask: gputypes.ColorWriteMaskAll}}}})
}

func (g *gpuRenderer) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	if g.r == nil {
		return nil
	}
	if g.environmentTexture != nil {
		_ = g.environmentTexture.Close()
		g.environmentTexture = nil
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
		if g.environmentBind != nil {
			g.environmentBind.Release()
		}
		if g.environmentPipeline != nil {
			g.environmentPipeline.Release()
		}
		if g.skinPipeline != nil {
			g.skinPipeline.Release()
			g.skinPipeline = nil
		}
		if g.skinLayout != nil {
			g.skinLayout.Release()
			g.skinLayout = nil
		}
		g.sort.release()
		for key, node := range g.geometries {
			node.release()
			delete(g.geometries, key)
		}
		g.nodes = nil
		if g.dropPipeline != nil {
			g.dropPipeline.Release()
		}
		if g.cutoutPipeline != nil {
			g.cutoutPipeline.Release()
		}
		if g.texturePipeline != nil {
			g.texturePipeline.Release()
		}
		for _, p := range slices.Concat(g.blendPipelines[:], g.textureBlends[:], g.pointBlends[0][:], g.pointBlends[1][:], g.spritePipelines[:]) {
			if p != nil {
				p.Release()
			}
		}
		if g.spriteLayout != nil {
			g.spriteLayout.Release()
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
		for _, p := range g.outlinePipelines {
			if p != nil {
				p.Release()
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
		if g.zeros != nil {
			g.zeros.Release()
		}
		if g.zeroRun != nil {
			g.zeroRun.Release()
		}
		if g.pointLayout != nil {
			g.pointLayout.Release()
		}
		if g.pointBindLayout != nil {
			g.pointBindLayout.Release()
		}
		if g.layout != nil {
			g.layout.Release()
		}
		if g.bindLayout != nil {
			g.bindLayout.Release()
		}
		for _, p := range g.shadowPipelines {
			if p != nil {
				p.Release()
			}
		}
		g.noShadow.release()
		g.shadowTarget.release()
		g.reflection.release()
		g.screen.release()
		if g.shadowLayout != nil {
			g.shadowLayout.Release()
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
	g.transforms, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: instanceRecordSize, Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst})
	if err != nil {
		return err
	}
	if err = writeTransform(dev, g.transforms, Instance{World: nt3dmath.Identity(), Visible: true}, nil); err != nil {
		g.transforms.Release()
		g.transforms = nil
	}
	return err
}

// drawTextured draws a run's textured triangles with the pipeline set.
func (g *gpuRenderer) drawTextured(pass *wgpu.RenderPassEncoder, f Frame, d gpuDraw) {
	g.drawTexturedOffset(pass, f, d, 0)
}

// The high four vertex bits select a shadow face. Ordinary draws use zero.
func (g *gpuRenderer) drawTexturedOffset(pass *wgpu.RenderPassEncoder, f Frame, d gpuDraw, first uint32) {
	pass.SetBindGroup(0, d.set.binds[7], nil)
	m := f.Geometry[d.index].Material
	src := baseMap(m)
	if t := g.procedural[proceduralKey{src, m.Repeat}]; t != nil {
		pass.SetBindGroup(2, t.target.BindGroup(), nil)
	} else {
		pass.SetBindGroup(2, g.textures[baseKey(f.Geometry[d.index].Material, f.anisotropy())].BindGroup(), nil)
	}
	if t := g.procedural[proceduralKey{normalMap(m), m.NormalRepeat}]; t != nil {
		pass.SetBindGroup(3, t.target.BindGroup(), nil)
	} else {
		pass.SetBindGroup(3, g.textures[textureKey{src: normalMap(m), repeat: m.NormalRepeat, aniso: f.anisotropy()}].BindGroup(), nil)
	}
	pass.Draw(gputypes.DrawArgs{VertexCount: d.geom.batches[7].count, InstanceCount: d.count, FirstVertex: first})
}

func (g *gpuRenderer) drawBatch(pass *wgpu.RenderPassEncoder, kind int, b gpuBatch, bind *wgpu.BindGroup) {
	if b.count == 0 {
		return
	}
	pass.SetPipeline(g.pipelines[kind])
	if kind == 0 {
		pass.SetBindGroup(0, bind, noRunOffset) // Opaque points: the empty run.
	} else {
		pass.SetBindGroup(0, bind, nil)
	}
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
