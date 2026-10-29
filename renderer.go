// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"nimbleterminal.dev/nt3d/math"
	"sync"
)

type RenderMode uint8

const (
	WebGPU RenderMode = iota
	Software
	Wireframe
)

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

// Instance associates local geometry with a world transform and picking identity.
type Instance struct {
	Node    NodeID
	Name    string
	World   nt3dmath.Mat4
	Visible bool
}

// OverlayRect is an opaque screen-space rectangle in render pixels.
type OverlayRect struct {
	X, Y, Width, Height int
	Color               color.RGBA
}

// CellText is a wireframe-only label in terminal cells.
type CellText struct {
	X, Y  int
	Text  string
	Color color.RGBA
}

// Frame is read-only after submission. Geometry is in local coordinates, with
// one Instance per Geometry entry. Empty Instances means visible identity instances.
// Revision identifies geometry across scenes; transforms and overlays can change
// without it. TextureRevision changes when texture bindings need refreshing.
type Frame struct {
	Width, Height             int
	Matrix                    nt3dmath.Mat4
	Revision, TextureRevision uint64
	Geometry                  []Geometry
	// GeometryRevisions optionally enables per-node GPU caching. Each entry must
	// have a nonzero, unique Instance.Node and a revision that changes whenever
	// its geometry or material (except Texture) changes. Scene.Frame supplies it.
	// Omit this slice to use the frame-wide Revision and geometry order.
	GeometryRevisions []uint64
	// Shared optionally marks geometry several instances show: entries with
	// the same nonzero value hold the same geometry, which renderers may
	// upload once and draw together. Scene.Frame supplies it for nodes that
	// show one SharedGeometry.
	Shared       []uint64
	Instances    []Instance
	DynamicLines []Vertex // World-space pairs, depth-tested with a small positive bias.
	Overlays     []OverlayRect
	CellText     []CellText
	// Environment supplies a distant sky, diffuse light, and material reflections.
	// Nil preserves the solid Background. EnvironmentStrength scales the light;
	// zero means 1. EnvironmentBackgroundOnly disables diffuse light and reflections.
	Environment               *Environment
	EnvironmentStrength       float32
	EnvironmentBackgroundOnly bool
	Background                color.RGBA
	Lighting                  Lighting
	Complete                  bool // Disable interactive software sampling for full snapshots.
	// Supersample renders at this multiple of Width and Height, 1..4, and
	// averages each block back to one pixel, smoothing edges, thin strands,
	// and cutouts. Zero means 1. Pixel sizes (points, line widths, overlays)
	// keep their size. The factor is lowered until the enlarged frame fits
	// 8192 pixels a side. Costs grow with its square, on the GPU and far more
	// in software.
	Supersample int
	// Eye and Forward are the camera's position and viewing direction, in
	// world space, and Perspective whether its view converges at Eye. They
	// serve view-dependent materials (Material.EdgeOn); Scene.Frame sets them.
	Eye, Forward nt3dmath.Vec3
	Perspective  bool
	// Linear lights, interpolates, blends, and averages colors in linear
	// light, decoding sRGB colors and textures and encoding the result, as
	// light adds physically: lit surfaces turn from the light more gradually,
	// and blends and supersampled edges keep their brightness. Lighting's
	// colors are decoded too.
	// Scene.Frame sets it; without it, colors are mixed as encoded.
	Linear bool
	// Fog fades what is drawn toward a color with distance from the camera.
	Fog Fog
	// Anisotropy is the most samples a texture takes across a surface seen
	// at a slant, where a pixel covers a long, thin run of texels: along the
	// run, at the mip level its width calls for, so a floor keeps detail
	// toward the horizon that mipmapping alone blurs. 1 is mipmapping alone;
	// zero means MaxAnisotropy.
	Anisotropy int
	// Cull skips geometry wholly outside the view, which costs nothing to
	// leave out. It trusts each Geometry's Bounds to contain it, as
	// Scene.Frame's do, and Scene.Frame sets it; geometry without valid
	// Bounds is always drawn. Changing Matrix afterward is fine.
	Cull bool
	// SplatBudget is the most splats a frame draws across every clustered
	// Gaussian geometry: each cluster draws a prefix of its progressive
	// order, longer the more screen it covers, until the budget is shared
	// out. Zero means DefaultSplatBudget; negative draws everything. Points
	// that are not clustered count against the budget first. A cluster in
	// view always draws at least one splat, so a frame can run over by up
	// to one splat per visible cluster. Scene.Frame sets it.
	SplatBudget int
}

// DefaultSplatBudget is the splat budget a frame gets when it names none.
const DefaultSplatBudget = 1_000_000

// MaxAnisotropy is the most Frame.Anisotropy allows, and its default.
const MaxAnisotropy = 16

// anisotropy is the frame's anisotropy, its default filled in.
func (f Frame) anisotropy() int {
	if f.Anisotropy == 0 {
		return MaxAnisotropy
	}
	return f.Anisotropy
}

// viewAt is the direction from a world point toward the viewer.
func (f Frame) viewAt(p nt3dmath.Vec3) nt3dmath.Vec3 {
	if f.Perspective {
		return f.Eye.Sub(p).Normalize()
	}
	return f.Forward.Scale(-1)
}

const MaxFramePixels = 4096 * 2160

// Frame creates a scene snapshot. The caller owns its header and decoration
// slices; its geometry and materials must not be mutated. Scene edits preserve it.
func (s *Scene) Frame(camera Camera, width, height int) (Frame, error) {
	if err := s.UpdateDeformations(); err != nil {
		return Frame{}, err
	}
	f := Frame{Width: width, Height: height, Revision: s.revision, TextureRevision: s.textureRevision, Environment: s.environment, Background: color.RGBA{235, 238, 242, 255}, Lighting: DefaultLighting(), Cull: true, Linear: true, SplatBudget: s.splatBudget}
	if width < 1 || height < 1 || width > MaxFramePixels || height > MaxFramePixels/width {
		return f, fmt.Errorf("nt3d: frame area must be 1..%d pixels", MaxFramePixels)
	}
	f.Matrix = camera.Matrix(float32(width) / float32(height))
	f.Eye, _, _, f.Forward = camera.valid().basis()
	f.Perspective = camera.valid().Projection == Perspective
	for _, id := range s.order {
		n := s.nodes[id]
		if n.geometry == nil {
			continue
		}
		f.Geometry = append(f.Geometry, *n.geometry)
		f.GeometryRevisions = append(f.GeometryRevisions, n.geometryRevision)
		var shared uint64
		if n.shared != nil {
			shared = n.shared.id
		}
		f.Shared = append(f.Shared, shared)
		world, visible := s.world(id), s.visible(id)
		if b := n.geometry.Billboard; b != nil {
			var inDepth bool
			world, inDepth = billboardWorld(f, camera, world, b)
			visible = visible && inDepth
		}
		f.Instances = append(f.Instances, Instance{id, n.Name, world, visible})
	}
	return f, nil
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
func RenderWireframe(f Frame, cols, rows int) (string, error) {
	if err := validateFrame(f); err != nil {
		return "", err
	}
	if cols < 1 || rows < 1 || cols > MaxFramePixels || rows > MaxFramePixels/cols {
		return "", fmt.Errorf("nt3d: invalid wireframe dimensions")
	}
	return wireRender(f, cols, rows), nil
}
func instance(f Frame, i int) Instance {
	if len(f.Instances) == 0 {
		return Instance{World: nt3dmath.Identity(), Visible: true}
	}
	return f.Instances[i]
}
func validateFrame(f Frame) error {
	if f.Supersample < 0 || f.Supersample > MaxSupersample {
		return fmt.Errorf("nt3d: supersample must be 0..%d", MaxSupersample)
	}
	if f.Width < 1 || f.Height < 1 || f.Width > MaxFramePixels || f.Height > MaxFramePixels/f.Width {
		return fmt.Errorf("nt3d: invalid frame dimensions")
	}
	if len(f.Instances) != 0 && len(f.Instances) != len(f.Geometry) {
		return fmt.Errorf("nt3d: instance count mismatch")
	}
	if len(f.Shared) != 0 && len(f.Shared) != len(f.Geometry) {
		return fmt.Errorf("nt3d: shared geometry count mismatch")
	}
	if f.Anisotropy < 0 || f.Anisotropy > MaxAnisotropy {
		return fmt.Errorf("nt3d: anisotropy must be 0..%d", MaxAnisotropy)
	}
	if !validFog(f.Fog) {
		return fmt.Errorf("nt3d: non-finite fog")
	}
	if len(f.GeometryRevisions) != 0 {
		if len(f.GeometryRevisions) != len(f.Geometry) || len(f.Instances) != len(f.Geometry) {
			return fmt.Errorf("nt3d: geometry revision count mismatch")
		}
		seen := make(map[NodeID]bool, len(f.Instances))
		for _, v := range f.Instances {
			if v.Node == Root || seen[v.Node] {
				return fmt.Errorf("nt3d: per-node caching requires unique nonzero node IDs")
			}
			seen[v.Node] = true
		}
	}
	if !validFloat(f.Matrix[:]...) {
		return fmt.Errorf("nt3d: non-finite frame")
	}
	if !validFloat(f.EnvironmentStrength) || f.EnvironmentStrength < 0 || (f.Environment != nil && (f.Environment.panorama == nil || f.Environment.texture == nil || !f.Environment.texture.valid() || !finiteVec(f.Forward))) {
		return fmt.Errorf("nt3d: invalid environment")
	}
	if !validLighting(f.Lighting) {
		return fmt.Errorf("nt3d: lighting needs at most %d lights of known kinds, finite, with nonnegative intensity and range, and directional lights a direction", MaxLights)
	}
	if len(f.DynamicLines)%2 != 0 {
		return fmt.Errorf("nt3d: incomplete dynamic line")
	}
	for _, v := range f.DynamicLines {
		if !finiteVec(v.Position) {
			return fmt.Errorf("nt3d: invalid dynamic line")
		}
	}
	// Each geometry has buffers and bindings of its own, so the 128 MiB binding
	// WebGPU guarantees limits each geometry (see Validate), not the frame. The
	// frame's total is bounded by device memory; a GPU that runs out fails the
	// render, without ErrGPUUnavailable. Decorations and overlays
	// share frame-wide buffers and keep frame limits.
	if len(f.Geometry)+1 > (128<<20)/128 || len(f.DynamicLines) > (128<<20)/48 || len(f.Overlays) > (128<<20)/48/6 {
		return fmt.Errorf("nt3d: frame exceeds GPU binding limit")
	}
	for i, g := range f.Geometry {
		if err := validateFrameGeometry(g); err != nil {
			return err
		}
		m := instance(f, i).World
		if _, ok := m.InverseAffine(); !ok || m.Determinant3() <= 0 {
			return fmt.Errorf("nt3d: invalid instance transform")
		}
	}
	// Alpha points order across geometries, one slot each.
	_, err := f.planBlend(f.drawn(1))
	return err
}
