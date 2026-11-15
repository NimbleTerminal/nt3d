// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"context"
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/gogpu/gpucontext"
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
	return fmt.Sprintf("const maxLights: u32 = %du;\nconst maxPointPixels: f32 = %d.0;\n", MaxLights, MaxPointPixels) + nt3dsh.WGSL() + defaultHookPreamble + sceneShader
}

// uniformSize is the scene uniform's bytes: matrix, viewport, sky ambient
// and light count, eye, forward, fog, fog color, ground ambient, and
// MaxLights lights of three vec4s each, shadow matrix and lookup params,
// nine diffuse environment coefficients, three sky camera basis vectors, and
// six light-view matrices (one used for directional and spot lights).
const uniformSize = 160 + 16 + MaxLights*48 + 80 + 192 + 6*64 + 64

type gpuBatch struct {
	buffer *wgpu.Buffer
	bind   *wgpu.BindGroup // Frame-wide batches only; geometry binds per instance set.
	count  uint32
	size   uint64
}

type scenePipelineSet struct {
	pipelines           [7]*wgpu.RenderPipeline
	blendPipelines      [2]*wgpu.RenderPipeline
	pointBlends         [2][3]*wgpu.RenderPipeline
	dropPipeline        *wgpu.RenderPipeline
	outlinePipelines    [9]*wgpu.RenderPipeline
	texturePipeline     *wgpu.RenderPipeline
	cutoutPipeline      *wgpu.RenderPipeline
	textureBlends       [2]*wgpu.RenderPipeline
	spritePipelines     [3]*wgpu.RenderPipeline
	environmentPipeline *wgpu.RenderPipeline
}

func (ps *scenePipelineSet) release() {
	if ps.environmentPipeline != nil {
		ps.environmentPipeline.Release()
	}
	if ps.dropPipeline != nil {
		ps.dropPipeline.Release()
	}
	if ps.cutoutPipeline != nil {
		ps.cutoutPipeline.Release()
	}
	if ps.texturePipeline != nil {
		ps.texturePipeline.Release()
	}
	for _, p := range slices.Concat(
		ps.pipelines[:],
		ps.blendPipelines[:],
		ps.pointBlends[0][:],
		ps.pointBlends[1][:],
		ps.outlinePipelines[:],
		ps.textureBlends[:],
		ps.spritePipelines[:],
	) {
		if p != nil {
			p.Release()
		}
	}
	*ps = scenePipelineSet{}
}

func multisampleState(samples int) gputypes.MultisampleState {
	if samples > 1 {
		return gputypes.MultisampleState{Count: uint32(samples), Mask: 0xFFFFFFFF}
	}
	return gputypes.DefaultMultisampleState()
}

type gpuShared struct {
	mu                     sync.Mutex
	refs                   int
	shader                 *wgpu.ShaderModule
	bindLayout             *wgpu.BindGroupLayout
	shadowLayout           *wgpu.BindGroupLayout
	layout                 *wgpu.PipelineLayout
	pointBindLayout        *wgpu.BindGroupLayout
	pointLayout            *wgpu.PipelineLayout
	msaaSets               [2]scenePipelineSet
	zeros, zeroRun         *wgpu.Buffer
	shadowPipelines        [4]*wgpu.RenderPipeline
	materialBindLayout     *wgpu.BindGroupLayout
	spriteBindLayout       *wgpu.BindGroupLayout
	textureLayout          *wgpu.PipelineLayout
	spriteLayout           *wgpu.PipelineLayout
	hookBindLayout         *wgpu.BindGroupLayout
	hookLayout             *wgpu.PipelineLayout
	hookPrograms           map[hookProgramKey]*gpuHookProgram
	procDownShader         *wgpu.ShaderModule
	procDownGroup          *wgpu.BindGroupLayout
	procDownLayout         *wgpu.PipelineLayout
	procDownPipes          [2]*wgpu.RenderPipeline
	environmentUniform     *wgpu.BindGroupLayout
	skinLayout             *wgpu.BindGroupLayout
	skinPipeline           *wgpu.ComputePipeline
	sortKeyLayout          *wgpu.BindGroupLayout
	sortKeyClusteredLayout *wgpu.BindGroupLayout
	sortLayout             *wgpu.BindGroupLayout
	sortRunLayout          *wgpu.BindGroupLayout
	sortKeyPipeline        *wgpu.ComputePipeline
	sortKeyClustered       *wgpu.ComputePipeline
	sortHistPipeline       *wgpu.ComputePipeline
	sortScanPipeline       *wgpu.ComputePipeline
	sortScatterPipeline    *wgpu.ComputePipeline
	sortCountPipeline      *wgpu.ComputePipeline
	sortRunPipeline        *wgpu.ComputePipeline
	resolveShader          *wgpu.ShaderModule
	resolveGroup           *wgpu.BindGroupLayout
	resolveSampledGroup    *wgpu.BindGroupLayout
	resolveLayout          *wgpu.PipelineLayout
	resolvePipelines       map[resolveKey]*wgpu.RenderPipeline
	depthPickLayouts       [2]*wgpu.BindGroupLayout
	depthPickPipelines     [2]*wgpu.ComputePipeline
	post                   gpuPostShared
	oit                    gpuOITShared
	geometries             map[geometryKey]*sharedGeometry
	textures               map[textureKey]*gpuTexture
	environments           map[*Environment]*gpuEnvironmentTexture
}

func (s *gpuShared) syncFrom(g *gpuRenderer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shader = g.shader
	s.bindLayout = g.bindLayout
	s.shadowLayout = g.shadowLayout
	s.layout = g.layout
	s.pointBindLayout = g.pointBindLayout
	s.pointLayout = g.pointLayout
	s.msaaSets = g.msaaSets
	s.zeros = g.zeros
	s.zeroRun = g.zeroRun
	s.shadowPipelines = g.shadowPipelines
}

func (s *gpuShared) copyTo(g *gpuRenderer) {
	g.shader = s.shader
	g.bindLayout = s.bindLayout
	g.shadowLayout = s.shadowLayout
	g.layout = s.layout
	g.pointBindLayout = s.pointBindLayout
	g.pointLayout = s.pointLayout
	g.msaaSets = s.msaaSets
	g.sampleIdx = 0
	g.applyPipelineSet(g.msaaSets[0])
	g.zeros = s.zeros
	g.zeroRun = s.zeroRun
	g.shadowPipelines = s.shadowPipelines
	g.materialBindLayout = s.materialBindLayout
	g.spriteBindLayout = s.spriteBindLayout
	g.textureLayout = s.textureLayout
	g.spriteLayout = s.spriteLayout
	g.hookBindLayout = s.hookBindLayout
	g.hookLayout = s.hookLayout
	g.procDownGroup = s.procDownGroup
	g.procDownPipes = s.procDownPipes
	g.environmentUniform = s.environmentUniform
	g.skinLayout = s.skinLayout
	g.skinPipeline = s.skinPipeline
	g.sort.keyLayout = s.sortKeyLayout
	g.sort.keyClusteredLayout = s.sortKeyClusteredLayout
	g.sort.sortLayout = s.sortLayout
	g.sort.runLayout = s.sortRunLayout
	g.sort.keyPipeline = s.sortKeyPipeline
	g.sort.keyClusteredPipeline = s.sortKeyClustered
	g.sort.histPipeline = s.sortHistPipeline
	g.sort.scanPipeline = s.sortScanPipeline
	g.sort.scatterPipeline = s.sortScatterPipeline
	g.sort.countPipeline = s.sortCountPipeline
	g.sort.runPipeline = s.sortRunPipeline
}

func (s *gpuShared) release() {
	s.mu.Lock()
	s.refs--
	if s.refs > 0 {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.post.release()
	s.oit.release()
	for i := range s.msaaSets {
		s.msaaSets[i].release()
	}
	if s.environmentUniform != nil {
		s.environmentUniform.Release()
		s.environmentUniform = nil
	}
	if s.skinPipeline != nil {
		s.skinPipeline.Release()
		s.skinPipeline = nil
	}
	if s.skinLayout != nil {
		s.skinLayout.Release()
		s.skinLayout = nil
	}
	for _, p := range []*wgpu.ComputePipeline{s.sortKeyPipeline, s.sortKeyClustered, s.sortHistPipeline, s.sortScanPipeline, s.sortScatterPipeline, s.sortCountPipeline, s.sortRunPipeline} {
		if p != nil {
			p.Release()
		}
	}
	for _, l := range []*wgpu.BindGroupLayout{s.sortKeyLayout, s.sortKeyClusteredLayout, s.sortLayout, s.sortRunLayout} {
		if l != nil {
			l.Release()
		}
	}
	for pk, prog := range s.hookPrograms {
		prog.release()
		delete(s.hookPrograms, pk)
	}
	if s.hookLayout != nil {
		s.hookLayout.Release()
		s.hookLayout = nil
	}
	if s.hookBindLayout != nil {
		s.hookBindLayout.Release()
		s.hookBindLayout = nil
	}
	if s.spriteLayout != nil {
		s.spriteLayout.Release()
		s.spriteLayout = nil
	}
	if s.textureLayout != nil {
		s.textureLayout.Release()
		s.textureLayout = nil
	}
	if s.spriteBindLayout != nil {
		s.spriteBindLayout.Release()
		s.spriteBindLayout = nil
	}
	if s.materialBindLayout != nil {
		s.materialBindLayout.Release()
		s.materialBindLayout = nil
	}
	for i, p := range s.procDownPipes {
		if p != nil {
			p.Release()
			s.procDownPipes[i] = nil
		}
	}
	if s.procDownLayout != nil {
		s.procDownLayout.Release()
		s.procDownLayout = nil
	}
	if s.procDownGroup != nil {
		s.procDownGroup.Release()
		s.procDownGroup = nil
	}
	if s.procDownShader != nil {
		s.procDownShader.Release()
		s.procDownShader = nil
	}
	if s.zeros != nil {
		s.zeros.Release()
		s.zeros = nil
	}
	if s.zeroRun != nil {
		s.zeroRun.Release()
		s.zeroRun = nil
	}
	if s.pointLayout != nil {
		s.pointLayout.Release()
		s.pointLayout = nil
	}
	if s.pointBindLayout != nil {
		s.pointBindLayout.Release()
		s.pointBindLayout = nil
	}
	if s.layout != nil {
		s.layout.Release()
		s.layout = nil
	}
	if s.bindLayout != nil {
		s.bindLayout.Release()
		s.bindLayout = nil
	}
	for i, p := range s.shadowPipelines {
		if p != nil {
			p.Release()
			s.shadowPipelines[i] = nil
		}
	}
	if s.shadowLayout != nil {
		s.shadowLayout.Release()
		s.shadowLayout = nil
	}
	if s.shader != nil {
		s.shader.Release()
		s.shader = nil
	}
	for key, p := range s.resolvePipelines {
		if p != nil {
			p.Release()
		}
		delete(s.resolvePipelines, key)
	}
	if s.resolveLayout != nil {
		s.resolveLayout.Release()
		s.resolveLayout = nil
	}
	for _, l := range []*wgpu.BindGroupLayout{s.resolveGroup, s.resolveSampledGroup} {
		if l != nil {
			l.Release()
		}
	}
	if s.resolveShader != nil {
		s.resolveShader.Release()
		s.resolveShader = nil
	}
	for i := range s.depthPickPipelines {
		if s.depthPickPipelines[i] != nil {
			s.depthPickPipelines[i].Release()
			s.depthPickPipelines[i] = nil
		}
		if s.depthPickLayouts[i] != nil {
			s.depthPickLayouts[i].Release()
			s.depthPickLayouts[i] = nil
		}
	}
}

type gpuRenderer struct {
	shared                 *gpuShared
	msaaSets               [2]scenePipelineSet
	sampleIdx              int
	screen                 sceneSnapshot
	reflection             reflectionTexture
	environmentSource      *Environment
	environmentTexture     *gpuEnvironmentTexture
	environmentUniform     *wgpu.BindGroupLayout
	environmentPipeline    *wgpu.RenderPipeline
	environmentBind        *wgpu.BindGroup
	skinPipeline           *wgpu.ComputePipeline
	skinLayout             *wgpu.BindGroupLayout
	sort                   gpuSort   // The frame-wide blended point sort.
	plan                   blendPlan // The frame's blended work, as the sort and the draws see it.
	lod                    lodPlan   // The splats of plan's slots the sort keys: its points are their total.
	dropPipeline           *wgpu.RenderPipeline
	outlinePipelines       [9]*wgpu.RenderPipeline
	creaseFrame            uint64 // Counts frames, to tell which crease edges are in use.
	shadowLayout           *wgpu.BindGroupLayout
	noShadow, shadowTarget shadowTarget
	shadowActive           bool
	shadowPipelines        [4]*wgpu.RenderPipeline
	materialBindLayout     *wgpu.BindGroupLayout
	spriteBindLayout       *wgpu.BindGroupLayout
	textureLayout          *wgpu.PipelineLayout
	hookBindLayout         *wgpu.BindGroupLayout
	hookLayout             *wgpu.PipelineLayout
	hookPrograms           map[hookProgramKey]*gpuHookProgram
	hookBinds              map[hookUniformKey]*gpuHookBind
	texturePipeline        *wgpu.RenderPipeline
	cutoutPipeline         *wgpu.RenderPipeline
	procDownShader         *wgpu.ShaderModule
	procDownGroup          *wgpu.BindGroupLayout
	procDownLayout         *wgpu.PipelineLayout
	procDownPipes          [2]*wgpu.RenderPipeline
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
	textures        map[textureKey]*gpuTexture // Image textures, by addressing.
	procedural      map[proceduralKey]*proceduralTexture
	materialBinds   map[materialBindKey]*wgpu.BindGroup
	geometries      map[geometryKey]*gpuGeometry
	nodes           []*gpuGeometry // By frame geometry index.
	draws           []gpuDraw      // The frame's draws of geometry.
	blendDraw       map[int]int    // A transparent frame geometry's draw.
	sortedRuns      []sortedRun    // The frame's alpha point slots, resolved once, in plan order.
	runOffsets      []uint32       // Each slice's run offset for each slot, one dynamic offset apiece.
	meshScratch     []byte         // Reusable encoding scratch for in-place mesh updates.
	flowScratch     []UV           // Reusable vertex flow scratch for in-place mesh updates.
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
	post            gpuPost
	oit             gpuOITTarget
	depthPick       gpuDepthPicker
	// deviceLimit is the most bytes one storage binding on the device holds,
	// the smaller of its binding and buffer limits, read at setup; a geometry
	// past it is refused at upload. Zero before setup. Tests lower it.
	deviceLimit int
}

// deviceBinding is the bytes one storage binding may hold on a device with
// these limits, within what an int holds.
func deviceBinding(l gputypes.Limits) int {
	bytes := min(l.MaxStorageBufferBindingSize, l.MaxBufferSize)
	if bytes > uint64(math.MaxInt) {
		return math.MaxInt
	}
	return int(bytes)
}

// checkUpload refuses a buffer the device's binding cannot hold, as a frame
// error, before the device would fail to create or bind it.
func (g *gpuRenderer) checkUpload(size int) error {
	if g.deviceLimit > 0 && size > g.deviceLimit {
		return fmt.Errorf("nt3d: geometry needs %d bytes in one buffer, more than the device's %d-byte binding limit", size, g.deviceLimit)
	}
	return nil
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
	if err = g.r.Do(func(dev *wgpu.Device) error {
		return g.setupDevice(dev)
	}); err != nil {
		_ = g.r.Close()
		g.r = nil
		return err
	}
	return nil
}

func (g *gpuRenderer) setupDevice(dev *wgpu.Device) (err error) {
	g.shared = &gpuShared{
		refs:         1,
		geometries:   make(map[geometryKey]*sharedGeometry),
		textures:     make(map[textureKey]*gpuTexture),
		environments: make(map[*Environment]*gpuEnvironmentTexture),
	}
	defer func() {
		g.shared.syncFrom(g)
		if err != nil {
			g.releaseResources()
		}
	}()
	g.deviceLimit = deviceBinding(dev.Limits())
	g.sort.deviceLimit = g.deviceLimit
	g.shader, err = dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "nt3d", WGSL: sceneShaderSource()})
	if err != nil {
		return err
	}
	g.bindLayout, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}}, {Binding: 1, Visibility: wgpu.ShaderStageVertex, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage}}, {Binding: 2, Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage}}}})
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
	// The fragment stage reads binding 2, the instance records, for a
	// mesh's toon style: every entry point that lights a fragment names it.
	storage := func(binding uint32) gputypes.BindGroupLayoutEntry {
		visibility := wgpu.ShaderStageVertex
		if binding == 2 {
			visibility |= wgpu.ShaderStageFragment
		}
		return gputypes.BindGroupLayoutEntry{Binding: binding, Visibility: visibility, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeReadOnlyStorage}}
	}
	g.pointBindLayout, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}}, storage(1), storage(2), storage(3), storage(4), storage(5), {Binding: 6, Visibility: wgpu.ShaderStageVertex, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform, HasDynamicOffset: true, MinBindingSize: 16}}, storage(7), storage(8)}})
	if err != nil {
		return err
	}
	g.pointLayout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{g.pointBindLayout, g.shadowLayout}})
	if err != nil {
		return err
	}
	g.msaaSets[0], err = g.createCorePipelineSet(dev, 1)
	if err != nil {
		return err
	}
	g.sampleIdx = 0
	g.applyPipelineSet(g.msaaSets[0])
	if g.zeros, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: 4 * shHeaderFloats, Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst}); err != nil {
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
}

func (g *gpuRenderer) setupView(dev *wgpu.Device, parent *gpuRenderer) (err error) {
	parent.shared.mu.Lock()
	parent.shared.refs++
	g.shared = parent.shared
	g.shared.copyTo(g)
	parent.shared.mu.Unlock()
	defer func() {
		if err != nil {
			g.releaseResources()
		}
	}()
	g.deviceLimit = parent.deviceLimit
	g.sort.deviceLimit = g.deviceLimit
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
	return g.clearNoShadow(dev)
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

// BindingLimit sets the device up if it has to and returns the bytes one
// storage binding on it holds, at most MaxBindingLimit; see AdoptBindingLimit.
func (g *gpuRenderer) BindingLimit() (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return 0, fmt.Errorf("%w: renderer closed", ErrGPUUnavailable)
	}
	if g.initErr != nil {
		return 0, g.initErr
	}
	if g.r == nil {
		if err := g.setup(); err != nil {
			g.initErr = fmt.Errorf("%w: %w", ErrGPUUnavailable, err)
			return 0, g.initErr
		}
	}
	return min(g.deviceLimit, MaxBindingLimit), nil
}

// Render draws f on the WebGPU backend and reads back the resulting image.
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
	img, _, err := g.r.Render(context.Background(), func(frame *gpuimage.Frame) error {
		return g.encode(frame.Device, frame.Encoder, f, GPUTarget{
			Color:  frame.Color,
			Format: gputypes.TextureFormatRGBA8Unorm,
			Width:  f.Width,
			Height: f.Height,
		})
	})
	return img, err
}

func (g *gpuRenderer) encode(dev *wgpu.Device, encoder *wgpu.CommandEncoder, f Frame, target GPUTarget) (err error) {
	defer func() {
		if err != nil {
			for _, node := range g.geometries {
				if node.skin != nil {
					node.skin.revision = 0
				}
			}
		}
	}()
	if err = g.activateSampleCount(dev, f.MSAA); err != nil {
		return err
	}
	if err = g.prepareProcedural(dev, f); err != nil {
		return err
	}
	if err = g.prepareTextures(dev, f); err != nil {
		return err
	}
	if err = g.prepareEnvironment(dev, f); err != nil {
		return err
	}
	if err = g.prepareTarget(dev, f); err != nil {
		return err
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
	} else if f.Environment != nil {
		environmentActive = 0.25
	}
	maxEnvMip := float32(0)
	if f.Environment != nil {
		maxEnvMip = float32(max(0, f.Environment.specMipCount()-1))
	}
	ub := append(floats(f.Matrix[:]...), floats(float32(f.Width), float32(f.Height), linear, environmentActive, rig.sky[0], rig.sky[1], rig.sky[2], float32(len(rig.lights)),
		f.Eye.X, f.Eye.Y, f.Eye.Z, perspective, f.Forward.X, f.Forward.Y, f.Forward.Z, maxEnvMip)...)
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
	// Each light: its direction toward it or its position, and its kind
	// encoding (0 directional, 1 point, -invSpan spot); its color times
	// intensity and its range; and for spotlights, beam direction and cosOuter.
	var lights [MaxLights * 12]float32
	for i, l := range rig.lights {
		kind := float32(l.kind)
		if l.kind == SpotLight {
			kind = -l.invSpan
		}
		copy(lights[i*12:], []float32{
			l.v.X, l.v.Y, l.v.Z, kind,
			l.color[0], l.color[1], l.color[2], l.rng,
			l.spotDir.X, l.spotDir.Y, l.spotDir.Z, l.cosOuter,
		})
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
		return err
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
	if err = g.drawProcedural(dev, encoder); err != nil {
		return err
	}
	if snapshot {
		if err = g.prepareSnapshot(dev, g.target.w, g.target.h); err != nil {
			return err
		}
	}
	if err = g.prepareOutlines(dev, f); err != nil {
		return err
	}
	oitActive := hasOITWork(f, drawn)
	if err = g.prepareOIT(dev, f, drawn); err != nil {
		return err
	}
	if g.shadowActive && (g.shadowTarget.texture == nil || g.shadowTarget.size != size || len(g.shadowTarget.faces) != len(views)) {
		g.shadowTarget.release()
		g.shadowTarget, err = g.makeShadowTarget(dev, size, len(views))
		if err != nil {
			return err
		}
	}
	if err = g.uploadIdentity(dev); err != nil {
		return err
	}
	// The sort's buffers grow before the point bind groups that hold
	// them are made, and it sorts once the points are on the device.
	if g.plan.points > 0 {
		if err = g.initSort(dev); err != nil {
			return err
		}
		if err = g.sort.ensure(dev, g.plan.points, len(g.plan.slots)); err != nil {
			return err
		}
	}
	if err = g.prepareGeometry(dev, f, drawn); err != nil {
		return err
	}
	if err = g.prepareHooks(dev, f, drawn); err != nil {
		return err
	}
	if err = g.prepareCreases(dev, f); err != nil {
		return err
	}
	if err = g.skinGeometry(dev, encoder, f); err != nil {
		return err
	}
	if err = g.encodeSort(dev, encoder, f, &g.plan, &g.lod); err != nil {
		return err
	}
	if err = g.uploadLines(dev, f); err != nil {
		return err
	}
	if err = g.uploadUI(dev, f); err != nil {
		return err
	}
	if err = dev.Queue().WriteBuffer(g.uniform, 0, ub); err != nil {
		return err
	}
	// The scene draws into the scene target, which a resolve pass
	// averages down and encodes. Pixel sizes stay the frame's, through
	// the uniform viewport, so points and lines keep their size when
	// supersampled.
	return g.renderScene(dev, encoder, f, target, func(color, depth *wgpu.TextureView) error {
		return g.drawScene(f, snapshot, oitActive, target.Load, encoder, color, depth)
	})
}

// background is the frame's background in the scene target's color space.
func background(f Frame, load bool) gputypes.Color {
	c := [3]float64{float64(f.Background.R) / 255, float64(f.Background.G) / 255, float64(f.Background.B) / 255}
	if f.Linear {
		for i := range c {
			c[i] = decodeSRGB(c[i])
		}
	}
	if load {
		a := float64(f.Background.A) / 255
		return gputypes.Color{R: c[0] * a, G: c[1] * a, B: c[2] * a, A: a}
	}
	return gputypes.Color{R: c[0], G: c[1], B: c[2], A: 1}
}

// setScenePipeline binds lighting after selecting the pipeline. Vulkan needs
// the current pipeline layout to bind descriptor sets; changing group 0's
// layout can also invalidate group 1, so restore lighting on every switch.
func (g *gpuRenderer) setScenePipeline(pass *wgpu.RenderPassEncoder, pipeline *wgpu.RenderPipeline) {
	pass.SetPipeline(pipeline)
	lighting := g.noShadow.bind
	if g.shadowActive {
		lighting = g.shadowTarget.bind
	}
	pass.SetBindGroup(1, lighting, nil)
}

// drawScene records the scene's pass into a color and depth target.
func (g *gpuRenderer) drawScene(f Frame, snapshot, oitActive, load bool, encoder *wgpu.CommandEncoder, color, depth *wgpu.TextureView) error {
	{
		if g.shadowActive {
			if err := g.drawShadow(f, encoder); err != nil {
				return err
			}
		}

		colorAtt := wgpu.RenderPassColorAttachment{View: color, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore, ClearValue: background(f, load)}
		if g.target.samples > 1 {
			colorAtt.View = g.target.msaaColorView
			colorAtt.ResolveTarget = color
		}
		pass, err := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{colorAtt}, DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{View: depth, DepthLoadOp: gputypes.LoadOpClear, DepthStoreOp: gputypes.StoreOpStore, DepthClearValue: 1, StencilReadOnly: true}})
		if err != nil {
			return err
		}
		if f.Environment != nil {
			g.setScenePipeline(pass, g.environmentPipeline)
			pass.SetBindGroup(0, g.environmentBind, nil)
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
					if !transparent(&f.Geometry[d.index]) && !transmitting(&f.Geometry[d.index]) {
						g.drawCrease(pass, f, d)
					}
				}
				for _, d := range g.draws {
					if d.geom.batches[8].count > 0 {
						g.setScenePipeline(pass, g.dropPipeline)
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
					pass, err = g.resumeScene(encoder, color, depth)
					if err != nil {
						return err
					}
					transmitted := false
					for _, d := range g.draws {
						geom := &f.Geometry[d.index]
						if !transmitting(geom) {
							continue
						}
						transmitted = true
						if mapped(*geom) {
							if d.geom.batches[7].count > 0 {
								g.setHookOrScenePipeline(pass, geom, g.texturePipeline, hookPipeOpaque)
								g.drawTextured(pass, f, d)
							}
						} else if b := d.geom.batches[1]; b.count > 0 {
							g.setScenePipeline(pass, g.pipelines[1])
							pass.SetBindGroup(0, d.set.binds[1], nil)
							pass.Draw(gputypes.DrawArgs{VertexCount: b.count, InstanceCount: d.count})
						}
					}
					if transmitted {
						for _, d := range g.draws {
							if transmitting(&f.Geometry[d.index]) {
								g.drawCrease(pass, f, d)
							}
						}
						if err := pass.End(); err != nil {
							return err
						}
						// Particles see the transmissive surface depth as well as opaque geometry.
						if err = g.captureScene(encoder); err != nil {
							return err
						}
						pass, err = g.resumeScene(encoder, color, depth)
						if err != nil {
							return err
						}
					}
				}
				if oitActive {
					if err := pass.End(); err != nil {
						return err
					}
					if err = g.encodeOITPass(f, encoder, depth); err != nil {
						return err
					}
					pass, err = g.resumeScene(encoder, color, depth)
					if err != nil {
						return err
					}
					g.drawOITComposite(pass)
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
					geom := &f.Geometry[d.index]
					if !mapped(*geom) || transparent(geom) || transmitting(geom) || d.geom.batches[7].count == 0 {
						continue
					}
					if geom.Material != nil && geom.Material.Cutout {
						g.setHookOrScenePipeline(pass, geom, g.cutoutPipeline, hookPipeCutout)
					} else {
						g.setHookOrScenePipeline(pass, geom, g.texturePipeline, hookPipeOpaque)
					}
					g.drawTextured(pass, f, d)
				}
			}
			if i == 4 || i == 5 {
				if i == 4 && f.PostProcess.on() {
					continue
				}
				g.drawBatch(pass, i, g.batches[i], g.batches[i].bind)
				continue
			}
			for _, d := range g.draws {
				b := d.geom.batches[i]
				switch {
				case b.count == 0 || i == 1 && (mapped(f.Geometry[d.index]) || transparent(&f.Geometry[d.index]) || transmitting(&f.Geometry[d.index])) || i == 0 && blendedPoints(&f.Geometry[d.index]):
				case i == 0 && f.Geometry[d.index].PointStyle.Texture != nil:
					g.drawSprite(pass, f, d)
				case i == 1:
					g.setScenePipeline(pass, g.pipelines[1])
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
			if mapped(*geom) {
				if d.geom.batches[7].count > 0 {
					kind := hookPipeBlendBack
					if side == 1 {
						kind = hookPipeBlendFront
					}
					g.setHookOrScenePipeline(pass, geom, g.textureBlends[side], kind)
					g.drawTextured(pass, f, d)
				}
			} else if b := d.geom.batches[1]; b.count > 0 {
				g.setScenePipeline(pass, g.blendPipelines[side])
				pass.SetBindGroup(0, d.set.binds[1], nil)
				pass.Draw(gputypes.DrawArgs{VertexCount: b.count, InstanceCount: d.count})
			}
		}
		if !f.OIT {
			g.drawCrease(pass, f, d)
		}
	}
	if blendedPoints(geom) && geom.PointStyle.Blend == AdditiveBlend && d.geom.batches[0].count > 0 {
		if geom.PointStyle.Texture != nil {
			g.drawSprite(pass, f, d)
			return
		}
		g.setScenePipeline(pass, g.pointBlends[1][geom.PointStyle.Shape])
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
				run.texture = g.textures[textureKey{src: style.Texture, aniso: f.anisotropy()}].spriteBind
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
		g.setScenePipeline(pass, run.pipeline)
	}
	i := slice*len(g.sortedRuns) + slot
	pass.SetBindGroup(0, run.d.set.binds[0], g.runOffsets[i:i+1:i+1])
	if run.texture != nil {
		pass.SetBindGroup(2, run.texture, nil)
	}
	pass.DrawIndirect(g.sort.args, g.sort.argsOffset(slice, slot))
	return run.pipeline
}

func (g *gpuRenderer) multisample() gputypes.MultisampleState {
	if g.sampleIdx > 0 {
		return multisampleState(4)
	}
	return gputypes.DefaultMultisampleState()
}

func (g *gpuRenderer) currentPipelineSet() scenePipelineSet {
	return scenePipelineSet{
		pipelines:           g.pipelines,
		blendPipelines:      g.blendPipelines,
		pointBlends:         g.pointBlends,
		dropPipeline:        g.dropPipeline,
		outlinePipelines:    g.outlinePipelines,
		texturePipeline:     g.texturePipeline,
		cutoutPipeline:      g.cutoutPipeline,
		textureBlends:       g.textureBlends,
		spritePipelines:     g.spritePipelines,
		environmentPipeline: g.environmentPipeline,
	}
}

func (g *gpuRenderer) applyPipelineSet(ps scenePipelineSet) {
	g.pipelines = ps.pipelines
	g.blendPipelines = ps.blendPipelines
	g.pointBlends = ps.pointBlends
	g.dropPipeline = ps.dropPipeline
	g.outlinePipelines = ps.outlinePipelines
	g.texturePipeline = ps.texturePipeline
	g.cutoutPipeline = ps.cutoutPipeline
	g.textureBlends = ps.textureBlends
	g.spritePipelines = ps.spritePipelines
	g.environmentPipeline = ps.environmentPipeline
}

func (g *gpuRenderer) createCorePipelineSet(dev *wgpu.Device, samples int) (ps scenePipelineSet, err error) {
	defer func() {
		if err != nil {
			ps.release()
		}
	}()
	ms := multisampleState(samples)
	for i, entry := range []string{"vs_point", "vs_mesh", "vs_box", "vs_line", "vs_ui", "vs_dynamic", "vs_arrow"} {
		primitive := gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeBack}
		if i == 0 || i == 3 || i >= 4 {
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
		ps.pipelines[i], err = dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: entry, Layout: layout, Vertex: wgpu.VertexState{Module: g.shader, EntryPoint: entry}, Primitive: primitive, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: true, DepthCompare: gputypes.CompareFunctionLessEqual}, Multisample: ms, Fragment: &wgpu.FragmentState{Module: g.shader, EntryPoint: fragment, Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}}})
		if err != nil {
			return ps, err
		}
	}
	for i, entry := range []string{"vs_mesh_back", "vs_mesh"} {
		if ps.blendPipelines[i], err = blendPipelineMS(dev, g.layout, g.shader, entry, "fs_main", i, ms); err != nil {
			return ps, err
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
			if ps.pointBlends[blend][shape], err = pointPipelineMS(dev, g.pointLayout, g.shader, entry, fragment, blend == 1, ms); err != nil {
				return ps, err
			}
		}
	}
	if ps.dropPipeline, err = pointPipelineMS(dev, g.layout, g.shader, "vs_drop", "fs_point_alpha_soft", false, ms); err != nil {
		return ps, err
	}
	return ps, nil
}

func (g *gpuRenderer) activateSampleCount(dev *wgpu.Device, msaa bool) error {
	g.msaaSets[g.sampleIdx] = g.currentPipelineSet()
	idx, samples := 0, 1
	if msaa {
		idx, samples = 1, 4
	}
	if g.shared != nil {
		g.shared.mu.Lock()
		if g.shared.msaaSets[idx].pipelines[0] == nil {
			ps, err := g.createCorePipelineSet(dev, samples)
			if err != nil {
				g.shared.mu.Unlock()
				return err
			}
			g.shared.msaaSets[idx] = ps
		}
		g.msaaSets[idx] = g.shared.msaaSets[idx]
		g.shared.mu.Unlock()
	} else if g.msaaSets[idx].pipelines[0] == nil {
		ps, err := g.createCorePipelineSet(dev, samples)
		if err != nil {
			return err
		}
		g.msaaSets[idx] = ps
	}
	g.sampleIdx = idx
	g.applyPipelineSet(g.msaaSets[idx])
	return nil
}

// blendPipelineMS draws a transparent mesh's back faces (side 0) or front faces
// (side 1), blending by alpha, depth tested without writing depth.
func blendPipelineMS(dev *wgpu.Device, layout *wgpu.PipelineLayout, shader *wgpu.ShaderModule, vertex, fragment string, side int, ms gputypes.MultisampleState) (*wgpu.RenderPipeline, error) {
	cull := gputypes.CullModeFront
	switch side {
	case 1:
		cull = gputypes.CullModeBack
	case 2:
		cull = gputypes.CullModeNone
	}
	blend := gputypes.BlendStateAlpha()
	return dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: "transparent " + vertex, Layout: layout, Vertex: wgpu.VertexState{Module: shader, EntryPoint: vertex}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: cull}, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: false, DepthCompare: gputypes.CompareFunctionLessEqual}, Multisample: ms, Fragment: &wgpu.FragmentState{Module: shader, EntryPoint: fragment, Targets: []gputypes.ColorTargetState{{Format: sceneFormat, Blend: &blend, WriteMask: gputypes.ColorWriteMaskAll}}}})
}

// pointPipelineMS draws blended points, depth tested without writing depth:
// over what is behind by alpha, or added to it.
func pointPipelineMS(dev *wgpu.Device, layout *wgpu.PipelineLayout, shader *wgpu.ShaderModule, vertex, fragment string, additive bool, ms gputypes.MultisampleState) (*wgpu.RenderPipeline, error) {
	blend := gputypes.BlendStateAlpha()
	if additive {
		blend.Color = gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorSrcAlpha, DstFactor: gputypes.BlendFactorOne, Operation: gputypes.BlendOperationAdd}
		blend.Alpha = gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorZero, DstFactor: gputypes.BlendFactorOne, Operation: gputypes.BlendOperationAdd}
	}
	return dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: fragment, Layout: layout, Vertex: wgpu.VertexState{Module: shader, EntryPoint: vertex}, Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone}, DepthStencil: &wgpu.DepthStencilState{Format: gputypes.TextureFormatDepth32Float, DepthWriteEnabled: false, DepthCompare: gputypes.CompareFunctionLessEqual}, Multisample: ms, Fragment: &wgpu.FragmentState{Module: shader, EntryPoint: fragment, Targets: []gputypes.ColorTargetState{{Format: sceneFormat, Blend: &blend, WriteMask: gputypes.ColorWriteMaskAll}}}})
}

func (g *gpuRenderer) releaseResources() {
	g.reflection.release()
	if g.environmentTexture != nil {
		g.environmentTexture.release()
		g.environmentTexture = nil
	}
	g.environmentSource = nil
	for uk, hb := range g.hookBinds {
		hb.release()
		delete(g.hookBinds, uk)
	}
	for mk, b := range g.materialBinds {
		b.Release()
		delete(g.materialBinds, mk)
	}
	for src, t := range g.procedural {
		g.releaseProcedural(t)
		delete(g.procedural, src)
	}
	for src, t := range g.textures {
		t.release()
		delete(g.textures, src)
	}
	if g.environmentBind != nil {
		g.environmentBind.Release()
		g.environmentBind = nil
	}
	for key, node := range g.geometries {
		node.release(g.shared)
		delete(g.geometries, key)
	}
	g.nodes = nil
	for i, b := range g.batches {
		if b.bind != nil {
			b.bind.Release()
		}
		if b.buffer != nil {
			b.buffer.Release()
		}
		g.batches[i] = gpuBatch{}
	}
	if g.transforms != nil {
		g.transforms.Release()
		g.transforms = nil
	}
	if g.uniform != nil {
		g.uniform.Release()
		g.uniform = nil
	}
	g.noShadow.release()
	g.shadowTarget.release()
	g.screen.release()
	g.oit.release()
	g.post.release(g.shared != nil)
	g.depthPick.release(g.shared != nil)
	if g.shared != nil {
		g.sort.releaseBuffers()
		g.target.releaseView()
		g.msaaSets = [2]scenePipelineSet{}
		g.sampleIdx = 0
		g.applyPipelineSet(scenePipelineSet{})
		g.environmentUniform = nil
		g.skinPipeline = nil
		g.skinLayout = nil
		g.spriteLayout = nil
		g.textureLayout = nil
		g.hookLayout = nil
		g.hookBindLayout = nil
		g.hookPrograms = nil
		g.spriteBindLayout = nil
		g.materialBindLayout = nil
		g.procDownPipes = [2]*wgpu.RenderPipeline{}
		g.procDownLayout = nil
		g.procDownGroup = nil
		g.procDownShader = nil
		g.zeros = nil
		g.zeroRun = nil
		g.pointLayout = nil
		g.pointBindLayout = nil
		g.layout = nil
		g.bindLayout = nil
		g.shadowPipelines = [4]*wgpu.RenderPipeline{}
		g.shadowLayout = nil
		g.shader = nil
		g.shared.release()
		g.shared = nil
		return
	}
	g.msaaSets[g.sampleIdx] = g.currentPipelineSet()
	for i := range g.msaaSets {
		g.msaaSets[i].release()
	}
	g.sampleIdx = 0
	g.applyPipelineSet(scenePipelineSet{})
	if g.environmentUniform != nil {
		g.environmentUniform.Release()
		g.environmentUniform = nil
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
	for pk, prog := range g.hookPrograms {
		prog.release()
		delete(g.hookPrograms, pk)
	}
	if g.hookLayout != nil {
		g.hookLayout.Release()
		g.hookLayout = nil
	}
	if g.hookBindLayout != nil {
		g.hookBindLayout.Release()
		g.hookBindLayout = nil
	}
	if g.spriteLayout != nil {
		g.spriteLayout.Release()
		g.spriteLayout = nil
	}
	if g.textureLayout != nil {
		g.textureLayout.Release()
		g.textureLayout = nil
	}
	if g.spriteBindLayout != nil {
		g.spriteBindLayout.Release()
		g.spriteBindLayout = nil
	}
	if g.materialBindLayout != nil {
		g.materialBindLayout.Release()
		g.materialBindLayout = nil
	}
	for i, p := range g.procDownPipes {
		if p != nil {
			p.Release()
			g.procDownPipes[i] = nil
		}
	}
	if g.procDownLayout != nil {
		g.procDownLayout.Release()
		g.procDownLayout = nil
	}
	if g.procDownGroup != nil {
		g.procDownGroup.Release()
		g.procDownGroup = nil
	}
	if g.procDownShader != nil {
		g.procDownShader.Release()
		g.procDownShader = nil
	}
	if g.zeros != nil {
		g.zeros.Release()
		g.zeros = nil
	}
	if g.zeroRun != nil {
		g.zeroRun.Release()
		g.zeroRun = nil
	}
	if g.pointLayout != nil {
		g.pointLayout.Release()
		g.pointLayout = nil
	}
	if g.pointBindLayout != nil {
		g.pointBindLayout.Release()
		g.pointBindLayout = nil
	}
	if g.layout != nil {
		g.layout.Release()
		g.layout = nil
	}
	if g.bindLayout != nil {
		g.bindLayout.Release()
		g.bindLayout = nil
	}
	for i, p := range g.shadowPipelines {
		if p != nil {
			p.Release()
			g.shadowPipelines[i] = nil
		}
	}
	if g.shadowLayout != nil {
		g.shadowLayout.Release()
		g.shadowLayout = nil
	}
	if g.shader != nil {
		g.shader.Release()
		g.shader = nil
	}
	g.target.release()
}

// Close releases all WebGPU pipelines, buffers, and textures owned by g.
func (g *gpuRenderer) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	if g.r == nil {
		return nil
	}
	_ = g.r.Do(func(*wgpu.Device) error {
		g.releaseResources()
		return nil
	})
	err := g.r.Close()
	g.r = nil
	return err
}

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
	geom := &f.Geometry[d.index]
	pass.SetBindGroup(0, d.set.binds[7], nil)
	mk := g.materialKey(geom.Material, f.anisotropy())
	pass.SetBindGroup(2, g.materialBinds[mk], nil)
	if bind := g.hookBindFor(geom); bind != nil {
		pass.SetBindGroup(3, bind, nil)
	}
	pass.Draw(gputypes.DrawArgs{VertexCount: d.geom.batches[7].count, InstanceCount: d.count, FirstVertex: first})
}

func (g *gpuRenderer) drawBatch(pass *wgpu.RenderPassEncoder, kind int, b gpuBatch, bind *wgpu.BindGroup) {
	if b.count == 0 {
		return
	}
	g.setScenePipeline(pass, g.pipelines[kind])
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
