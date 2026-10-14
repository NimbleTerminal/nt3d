// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"image"
	"image/color"
	"nimbleterminal.dev/nt3d/math"
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

type Light struct {
	Direction nt3dmath.Vec3
	Ambient   float32
}

func DefaultLight() Light { return Light{nt3dmath.Vec3{X: 1, Y: -1, Z: 2}.Normalize(), .3} }

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
	Instances         []Instance
	DynamicLines      []Vertex // World-space pairs, depth-tested with a small positive bias.
	Overlays          []OverlayRect
	CellText          []CellText
	Background        color.RGBA
	Light             Light
	Complete          bool // Disable interactive software sampling for full snapshots.
}

const MaxFramePixels = 4096 * 2160

// Frame creates a scene snapshot. The caller owns its header and decoration
// slices; its geometry and materials must not be mutated. Scene edits preserve it.
func (s *Scene) Frame(camera Camera, width, height int) (Frame, error) {
	f := Frame{Width: width, Height: height, Revision: s.revision, TextureRevision: s.textureRevision, Background: color.RGBA{235, 238, 242, 255}, Light: DefaultLight()}
	if width < 1 || height < 1 || width > MaxFramePixels || height > MaxFramePixels/width {
		return f, fmt.Errorf("nt3d: frame area must be 1..%d pixels", MaxFramePixels)
	}
	f.Matrix = camera.Matrix(float32(width) / float32(height))
	for _, id := range s.order {
		n := s.nodes[id]
		if n.geometry == nil {
			continue
		}
		f.Geometry = append(f.Geometry, *n.geometry)
		f.GeometryRevisions = append(f.GeometryRevisions, n.geometryRevision)
		f.Instances = append(f.Instances, Instance{id, n.Name, s.world(id), s.visible(id)})
	}
	return f, nil
}

// Renderer implementations serialize Render and Close. Call Close after use.
// Returned image pixels must remain stable across subsequent renders.
type Renderer interface {
	Render(Frame) (image.Image, error)
	Close() error
}

func NewGPURenderer() Renderer { return &gpuRenderer{} }

type softwareRenderer struct{}

func NewSoftwareRenderer() Renderer { return softwareRenderer{} }
func (softwareRenderer) Render(f Frame) (image.Image, error) {
	if err := validateFrame(f); err != nil {
		return nil, err
	}
	return softwareRender(f), nil
}
func (softwareRenderer) Close() error { return nil }

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
	if f.Width < 1 || f.Height < 1 || f.Width > MaxFramePixels || f.Height > MaxFramePixels/f.Width {
		return fmt.Errorf("nt3d: invalid frame dimensions")
	}
	if len(f.Instances) != 0 && len(f.Instances) != len(f.Geometry) {
		return fmt.Errorf("nt3d: instance count mismatch")
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
	if !validFloat(f.Matrix[:]...) || !finiteVec(f.Light.Direction) || !validFloat(f.Light.Ambient) {
		return fmt.Errorf("nt3d: non-finite frame")
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
	// render, and the widget falls back to software. Decorations and overlays
	// share frame-wide buffers and keep frame limits.
	if len(f.Geometry)+1 > (128<<20)/128 || len(f.DynamicLines) > (128<<20)/48 || len(f.Overlays) > (128<<20)/48/6 {
		return fmt.Errorf("nt3d: frame exceeds GPU binding limit")
	}
	for i, g := range f.Geometry {
		if err := g.Validate(); err != nil {
			return err
		}
		m := instance(f, i).World
		if _, ok := m.InverseAffine(); !ok || m.Determinant3() <= 0 {
			return fmt.Errorf("nt3d: invalid instance transform")
		}
	}
	return nil
}
