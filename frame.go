// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"fmt"
	"image/color"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// Draw is one thing a frame draws: a geometry, where it is, and the node it
// came from. A Draw with only its Geometry set draws that geometry where it
// lies, so a frame built by hand needs nothing else.
type Draw struct {
	// Geometry is in local coordinates. Treat a frame's geometry and
	// materials as read-only.
	Geometry Geometry
	// World places Geometry in the frame's space. The zero matrix is the
	// identity.
	World nt3dmath.Mat4
	// Hidden draws nothing and is never picked. Scene.Frame keeps a hidden
	// node's draw, so that showing it again in a frame is one assignment.
	Hidden bool
	// Node and Name are the scene node the draw came from, and Data its
	// Scene.SetNodeData value: what a pick of the draw reports.
	Node NodeID
	Name string
	Data any
	// Revision, when nonzero, caches the draw's uploaded geometry under its
	// Node from frame to frame: Node must then be nonzero and belong to no
	// other such draw, and Revision must change whenever the geometry or its
	// material (except Texture) changes. Scene.Frame supplies it. Zero caches
	// by the frame's Revision and the draw's position in Draws instead.
	Revision uint64
	// Shared, when nonzero, marks geometry several draws show: draws with
	// the same value hold the same geometry, which renderers may upload once
	// and draw together. Scene.Frame supplies it for nodes that show one
	// SharedGeometry.
	Shared uint64
	// state is what the scene that made the draw knows about Geometry; see
	// geometryState. To draw other geometry in a draw's place, replace the
	// whole Draw rather than its Geometry.
	state geometryState
}

// world is where the draw's geometry lies: World, or the identity it stands for.
func (d *Draw) world() nt3dmath.Mat4 {
	if d.World == (nt3dmath.Mat4{}) {
		return nt3dmath.Identity()
	}
	return d.World
}

// placement is a draw without its geometry, as renderers and picking read it.
type placement struct {
	Node    NodeID
	Name    string
	Data    any
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

// Frame is read-only after submission: one Draw for each thing it shows, in
// the order they are drawn, and how they are viewed, lit, and finished.
// Scene.Frame orders the draws by Scene.SetNodeDrawOrder, and fills
// Background, Lighting, Fog, the Environment fields, and PostProcess from the
// scene's Look; change them on the Scene rather than on each frame.
// Revision identifies geometry across scenes; transforms and overlays can change
// without it. TextureRevision changes when texture bindings need refreshing.
type Frame struct {
	Width, Height             int
	Matrix                    nt3dmath.Mat4
	Revision, TextureRevision uint64
	// Draws is what the frame shows. The caller owns the slice, and may hide,
	// show, move, or drop its entries before the frame is rendered.
	Draws            []Draw
	DynamicLines     []Vertex // World-space pairs, depth-tested with a small positive bias.
	DynamicLineWidth float32  // Render pixels for DynamicLines; zero defaults to 1.
	Overlays         []OverlayRect
	CellText         []CellText
	// ReflectionProbes supplies local specular light, in priority order.
	// Positions and bounds are frame-relative; Scene.Frame snapshots and converts them.
	ReflectionProbes []ReflectionProbe
	// Environment supplies a distant sky, diffuse light, and material reflections.
	// Nil preserves the solid Background. EnvironmentStrength scales the light;
	// zero means 1. EnvironmentBackgroundOnly disables its diffuse light and
	// reflections, leaving it a backdrop. Reflection probes are independent of
	// it: one holding the sky lets surfaces inside its box reflect it.
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
	// MSAA multisamples the GPU scene pass at 4 samples per pixel, smoothing
	// polygon, line, and point edges without shading interior fragments four
	// times as Supersample does. Software and wireframe rendering ignore it.
	// It may be combined with Supersample.
	MSAA bool
	// OIT enables McGuire & Bavoil Weighted Blended Order-Independent
	// Transparency for Material.Transparent meshes on WebGPU (including 4x MSAA)
	// and software, compositing intersecting or overlapping transparent layers
	// in a single unsorted pass without view-dependent popping. Wireframe
	// ignores it.
	OIT bool
	// PostProcess configures SSAO within indirect lighting, followed by HDR
	// bloom, exposure, tone mapping, and vignette before overlays.
	PostProcess PostProcess
	// Eye and Forward are the camera's position and viewing direction, in
	// frame space (relative to Origin), and Perspective whether its view
	// converges at Eye. Origin is the float64 world offset subtracted by
	// Scene.Frame when Camera.Origin is nonzero.
	Eye, Forward nt3dmath.Vec3
	Origin       [3]float64
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

// MaxFramePixels is the maximum width*height pixel area of a rendered Frame.
const MaxFramePixels = 4096 * 2160

// Frame creates a scene snapshot for interactive drawing. The caller owns its
// header and decoration slices; its geometry and materials must not be
// mutated. Scene edits preserve it.
//
// Frame is one of a run of frames: with Scene.SetLODHysteresis in force, a
// node holds the level of detail it drew last until the camera has moved far
// enough, which Frame remembers in the scene. For a picture that depends on
// the scene and camera alone, use StillFrame.
func (s *Scene) Frame(camera Camera, width, height int) (Frame, error) {
	return s.frame(camera, width, height, false)
}

// StillFrame creates a scene snapshot for a picture of its own: a screenshot,
// a thumbnail, a golden image, a render for a model to look at. It is Frame
// except that it reads and writes nothing left by earlier frames. Each level
// of detail is chosen by distance alone, without hysteresis, so the same scene
// and camera always give the same frame, and the interactive frames after it
// draw as if it had never been made. Complete is set, for full sampling.
func (s *Scene) StillFrame(camera Camera, width, height int) (Frame, error) {
	f, err := s.frame(camera, width, height, true)
	f.Complete = true
	return f, err
}

func (s *Scene) frame(camera Camera, width, height int, still bool) (Frame, error) {
	if err := camera.Validate(); err != nil {
		return Frame{}, err
	}
	if err := s.UpdateDeformations(); err != nil {
		return Frame{}, err
	}
	validCam := camera.valid()
	frameOrigin := validCam.Origin
	relCam := validCam
	relCam.Origin = [3]float64{}

	look := s.look
	f := Frame{
		Width: width, Height: height, Revision: s.revision, TextureRevision: s.textureRevision,
		Background: look.Background, Lighting: Lighting{Ambient: look.Lighting.Ambient}, Fog: look.Fog,
		Environment: look.Environment, EnvironmentStrength: look.EnvironmentStrength, EnvironmentBackgroundOnly: look.EnvironmentBackgroundOnly,
		PostProcess: look.PostProcess,
		Origin:      frameOrigin, Cull: true, Linear: true, SplatBudget: s.splatBudget,
	}
	f.ReflectionProbes = s.reflectionProbesRelative(frameOrigin)
	// The scene's light nodes are its lights; without any, the look's are.
	if lights := s.sceneLightsRelative(Root, frameOrigin); len(lights) > 0 {
		f.Lighting.Lights = lights
	} else if len(look.Lighting.Lights) > 0 {
		f.Lighting.Lights = lightsRelative(look.Lighting.Lights, frameOrigin)
	}
	if width < 1 || height < 1 || width > MaxFramePixels || height > MaxFramePixels/width {
		return f, fmt.Errorf("nt3d: frame area must be 1..%d pixels", MaxFramePixels)
	}
	f.Matrix = relCam.Matrix(float32(width) / float32(height))
	f.Eye, _, _, f.Forward = relCam.basis()
	f.Perspective = relCam.Projection == Perspective
	lodBias, lodBand, lodEpoch := s.lodBiasValue(), s.lodHysteresis, s.lodEpochValue()
	// The nodes that draw are counted first, so the frame's draws are
	// allocated once at their final size instead of growing node by node.
	drawn := make([]*sceneNode, 0, len(s.order))
	for _, id := range s.sequence() {
		if n := s.nodes[id]; n.geometry != nil {
			drawn = append(drawn, n)
		}
	}
	f.Draws = make([]Draw, len(drawn))
	for i, n := range drawn {
		id := n.ID
		geom, rev, sg := n.geometry, n.geometryRevision, n.shared
		world, visible := s.worldRelative(id, frameOrigin), s.visible(id)
		if len(n.lod) > 0 {
			lodWorld := world
			if n.lodRefBounds.IsValid() && n.lodRefNode != 0 && s.nodes[n.lodRefNode] != nil {
				lodWorld = s.worldRelative(n.lodRefNode, frameOrigin)
			}
			lvl, inRange := n.selectLOD(lodWorld, f.Eye, lodBias, lodBand, lodEpoch, visible, still)
			geom, rev, sg = lvl.geometry, lvl.geometryRevision, lvl.shared
			visible = visible && inRange
		}
		if b := geom.Billboard; b != nil {
			var inDepth bool
			world, inDepth = billboardWorld(f, relCam, world, b)
			visible = visible && inDepth
		}
		d := &f.Draws[i]
		d.Geometry, d.state, d.Revision = geom.Geometry, geom.geometryState, rev
		if sg != nil {
			d.Shared = sg.id
		}
		d.Node, d.Name, d.Data, d.World, d.Hidden = id, n.Name, n.Data, world, !visible
	}
	return f, nil
}

func instance(f Frame, i int) placement {
	d := &f.Draws[i]
	return placement{Node: d.Node, Name: d.Name, Data: d.Data, World: d.world(), Visible: !d.Hidden}
}
func validateFrame(f Frame) error {
	if f.Supersample < 0 || f.Supersample > MaxSupersample {
		return fmt.Errorf("nt3d: supersample must be 0..%d", MaxSupersample)
	}
	if f.Width < 1 || f.Height < 1 || f.Width > MaxFramePixels || f.Height > MaxFramePixels/f.Width {
		return fmt.Errorf("nt3d: invalid frame dimensions")
	}
	if f.Anisotropy < 0 || f.Anisotropy > MaxAnisotropy {
		return fmt.Errorf("nt3d: anisotropy must be 0..%d", MaxAnisotropy)
	}
	if !validFog(f.Fog) {
		return fmt.Errorf("nt3d: non-finite fog")
	}
	if err := f.PostProcess.validate(); err != nil {
		return err
	}
	var seen map[NodeID]bool
	for i := range f.Draws {
		d := &f.Draws[i]
		if d.Revision == 0 {
			continue
		}
		if seen == nil {
			seen = make(map[NodeID]bool, len(f.Draws))
		}
		if d.Node == Root || seen[d.Node] {
			return fmt.Errorf("nt3d: per-node caching requires unique nonzero node IDs")
		}
		seen[d.Node] = true
	}
	if !validFloat(f.Matrix[:]...) || !nt3dmath.IsFinite(f.Origin[0]) || !nt3dmath.IsFinite(f.Origin[1]) || !nt3dmath.IsFinite(f.Origin[2]) {
		return fmt.Errorf("nt3d: non-finite frame")
	}
	if !validFloat(f.EnvironmentStrength) || f.EnvironmentStrength < 0 || (f.Environment != nil && (!f.Environment.valid() || !finiteVec(f.Forward))) {
		return fmt.Errorf("nt3d: invalid environment")
	}
	if err := validReflectionProbes(f.ReflectionProbes); err != nil {
		return err
	}
	if !validLighting(f.Lighting) {
		return fmt.Errorf("nt3d: lighting needs at most %d lights of known kinds, finite, with nonnegative intensity and range, directional and spot lights a direction, and spot lights valid cone angles", MaxLights)
	}
	if len(f.DynamicLines)%2 != 0 {
		return fmt.Errorf("nt3d: incomplete dynamic line")
	}
	if !validWidth(f.DynamicLineWidth) {
		return fmt.Errorf("nt3d: invalid dynamic line width")
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
	if len(f.Draws)+1 > (128<<20)/128 || len(f.DynamicLines) > (128<<20)/48 || len(f.Overlays) > (128<<20)/48/6 {
		return fmt.Errorf("nt3d: frame exceeds GPU binding limit")
	}
	for i := range f.Draws {
		g, st := f.Draws[i].Geometry, &f.Draws[i].state
		if err := validateFrameGeometry(g, st); err != nil {
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
