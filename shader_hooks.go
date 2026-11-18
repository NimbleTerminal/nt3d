// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"errors"
	"fmt"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// MaxShaderParameters is the maximum number of [4]float32 parameter vectors
// accepted by NewVertexDisplacementWithParameters and
// NewMaterialShaderWithParameters.
const MaxShaderParameters = 16

// VertexDisplaceInput carries the per-vertex inputs passed to a
// VertexDisplaceFunc fallback in software rendering, shadow maps, wireframe,
// and picking, matching WGSL's NT3DVertexInput.
type VertexDisplaceInput struct {
	Position nt3dmath.Vec3 // Local-space rest (or posed) vertex position.
	Normal   nt3dmath.Vec3 // Local-space rest (or posed) vertex normal.
	World    nt3dmath.Vec3 // World-space rest (or posed) vertex position.
	UV       UV            // Vertex UV, or zero when the mesh has no UVs.
	Time     float32       // Animation time in seconds.
	Params   [MaxShaderParameters][4]float32
}

// VertexDisplaceOutput carries the local-space displaced position and normal
// returned by a VertexDisplaceFunc, matching WGSL's NT3DVertexOutput.
type VertexDisplaceOutput struct {
	Position nt3dmath.Vec3
	Normal   nt3dmath.Vec3
}

// VertexDisplaceFunc evaluates vertex displacement on the CPU for software
// rendering, CPU shadow maps, wireframe, and ray picking.
type VertexDisplaceFunc func(VertexDisplaceInput) VertexDisplaceOutput

// VertexDisplacement animates or deforms indexed triangle vertices in the GPU
// vertex shader with a pure-Go fallback for software rendering, CPU shadow
// maps, wireframe, and ray picking. VertexDisplacement values are immutable and
// safe to share across geometries, scenes, and goroutines; call WithParameters
// to advance time or update uniform parameters without recompiling WGSL or
// re-uploading mesh buffers.
//
// In WGSL, nt3d supplies:
//
//	struct NT3DVertexInput {
//	    position: vec3<f32>,
//	    normal: vec3<f32>,
//	    world: vec3<f32>,
//	    uv: vec2<f32>,
//	    time: f32,
//	};
//	struct NT3DVertexOutput {
//	    position: vec3<f32>,
//	    normal: vec3<f32>,
//	};
//	fn nt3dDisplaceParam(i: u32) -> vec4<f32>; // also nt3dHooks.displace.params[i]
//
// and calls:
//
//	fn nt3dDisplace(in: NT3DVertexInput) -> NT3DVertexOutput
type VertexDisplacement struct {
	shader  string
	fn      VertexDisplaceFunc
	time    float32
	params  [MaxShaderParameters][4]float32
	maxDisp float32
}

// NewVertexDisplacement builds an immutable VertexDisplacement from WGSL source
// defining fn nt3dDisplace(in: NT3DVertexInput) -> NT3DVertexOutput, a
// conservative nonnegative local-space displacement bound maxDisplacement used
// to pad frustum-culling, shadow, and picking bounds, and a pure-Go fallback.
// WGSL compilation is deferred until a WebGPU renderer draws the geometry.
func NewVertexDisplacement(wgsl string, maxDisplacement float32, fallback VertexDisplaceFunc) (*VertexDisplacement, error) {
	return NewVertexDisplacementWithParameters(wgsl, maxDisplacement, 0, nil, fallback)
}

// NewVertexDisplacementWithParameters is NewVertexDisplacement with animation
// time (in seconds) and up to MaxShaderParameters vec4 uniform parameters.
func NewVertexDisplacementWithParameters(wgsl string, maxDisplacement, time float32, params [][4]float32, fallback VertexDisplaceFunc) (*VertexDisplacement, error) {
	if wgsl == "" {
		return nil, errors.New("nt3d: empty vertex displacement shader")
	}
	if fallback == nil {
		return nil, errors.New("nt3d: nil vertex displacement fallback")
	}
	if err := checkHookNames(wgsl, "nt3dDisplace"); err != nil {
		return nil, err
	}
	if !validFloat(maxDisplacement, time) || maxDisplacement < 0 {
		return nil, errors.New("nt3d: vertex displacement requires finite time and nonnegative maxDisplacement")
	}
	packed, err := packShaderParams(params)
	if err != nil {
		return nil, err
	}
	return &VertexDisplacement{
		shader:  wgsl,
		fn:      fallback,
		time:    time,
		params:  packed,
		maxDisp: maxDisplacement,
	}, nil
}

// WithParameters returns a copy of d with updated animation time and uniform
// parameters, sharing the WGSL shader source and Go fallback so GPU renderers
// reuse compiled pipelines and update only the uniform buffer.
func (d *VertexDisplacement) WithParameters(time float32, params [][4]float32) (*VertexDisplacement, error) {
	if !d.valid() {
		return nil, errors.New("nt3d: invalid vertex displacement")
	}
	if !validFloat(time) {
		return nil, errors.New("nt3d: non-finite shader time")
	}
	packed, err := packShaderParams(params)
	if err != nil {
		return nil, err
	}
	if d.time == time && d.params == packed {
		return d, nil
	}
	next := *d
	next.time = time
	next.params = packed
	return &next, nil
}

// WithMaxDisplacement returns a copy of d with an updated nonnegative local
// displacement bound while sharing its WGSL shader, Go fallback, time, and
// parameters.
func (d *VertexDisplacement) WithMaxDisplacement(maxDisplacement float32) (*VertexDisplacement, error) {
	if !d.valid() {
		return nil, errors.New("nt3d: invalid vertex displacement")
	}
	if !validFloat(maxDisplacement) || maxDisplacement < 0 {
		return nil, errors.New("nt3d: maxDisplacement must be finite and nonnegative")
	}
	if d.maxDisp == maxDisplacement {
		return d, nil
	}
	next := *d
	next.maxDisp = maxDisplacement
	return &next, nil
}

// Time returns d's animation time in seconds, or 0 when d is nil.
func (d *VertexDisplacement) Time() float32 {
	if d == nil {
		return 0
	}
	return d.time
}

// Parameters returns a copy of d's uniform parameter array, or zeros when d is nil.
func (d *VertexDisplacement) Parameters() [MaxShaderParameters][4]float32 {
	if d == nil {
		return [MaxShaderParameters][4]float32{}
	}
	return d.params
}

// MaxDisplacement returns d's local-space displacement bound, or 0 when d is nil.
func (d *VertexDisplacement) MaxDisplacement() float32 {
	if d == nil {
		return 0
	}
	return d.maxDisp
}

func (d *VertexDisplacement) valid() bool {
	return d != nil && d.shader != "" && d.fn != nil && validFloat(d.time, d.maxDisp) && d.maxDisp >= 0 && validShaderParamArray(&d.params)
}

// MaterialColorInput carries the per-fragment / per-pixel inputs passed to a
// MaterialColorFunc fallback in software rendering, matching WGSL's
// NT3DMaterialInput.
//
// Color is in linear light: RGB holds the surface's linear base color after
// vertex color/tint and Material.Texture sampling, and Color[3] holds the
// surface's base alpha (including vertex opacity when Material.Transparent is
// true). Returned RGB values remain in linear light (>= 0, unclamped above 1 so
// custom shaders can drive HDR Bloom and tone mapping) before scene lighting
// (unless Material.Unlit is true), emissive, and fog; returned alpha is clamped
// to 0..1 for cutout testing, alpha blending, and WBOIT.
type MaterialColorInput struct {
	World  nt3dmath.Vec3 // World-space fragment position.
	Normal nt3dmath.Vec3 // Unit world-space shading normal (zero if mesh normals are zero).
	View   nt3dmath.Vec3 // Unit world-space direction from World toward the camera.
	UV     UV            // Interpolated UV, or zero when the mesh has no UVs.
	Color  [4]float32    // Linear-light RGBA base color and alpha.
	Time   float32       // Animation time in seconds.
	Params [MaxShaderParameters][4]float32
}

// MaterialColorFunc evaluates custom per-fragment linear-light RGBA surface
// color on the CPU for software rendering.
type MaterialColorFunc func(MaterialColorInput) [4]float32

// MaterialShader customizes a Material's per-fragment base color and alpha in
// the GPU fragment shader with a pure-Go fallback for software rendering.
// MaterialShader values are immutable and safe to share across materials,
// scenes, and goroutines; call WithParameters to advance time or update uniform
// parameters without recompiling WGSL or re-uploading mesh buffers.
//
// In WGSL, nt3d supplies:
//
//	struct NT3DMaterialInput {
//	    world: vec3<f32>,
//	    normal: vec3<f32>,
//	    view: vec3<f32>,
//	    uv: vec2<f32>,
//	    color: vec4<f32>,
//	    time: f32,
//	};
//	fn nt3dMaterialParam(i: u32) -> vec4<f32>; // also nt3dHooks.material.params[i]
//
// and calls:
//
//	fn nt3dMaterial(in: NT3DMaterialInput) -> vec4<f32>
type MaterialShader struct {
	shader string
	fn     MaterialColorFunc
	time   float32
	params [MaxShaderParameters][4]float32
}

// NewMaterialShader builds an immutable MaterialShader from WGSL source
// defining fn nt3dMaterial(in: NT3DMaterialInput) -> vec4<f32> and a pure-Go
// fallback. WGSL compilation is deferred until a WebGPU renderer draws a mesh
// carrying the material.
func NewMaterialShader(wgsl string, fallback MaterialColorFunc) (*MaterialShader, error) {
	return NewMaterialShaderWithParameters(wgsl, 0, nil, fallback)
}

// NewMaterialShaderWithParameters is NewMaterialShader with animation time (in
// seconds) and up to MaxShaderParameters vec4 uniform parameters.
func NewMaterialShaderWithParameters(wgsl string, time float32, params [][4]float32, fallback MaterialColorFunc) (*MaterialShader, error) {
	if wgsl == "" {
		return nil, errors.New("nt3d: empty material shader")
	}
	if fallback == nil {
		return nil, errors.New("nt3d: nil material shader fallback")
	}
	if err := checkHookNames(wgsl, "nt3dMaterial"); err != nil {
		return nil, err
	}
	if !validFloat(time) {
		return nil, errors.New("nt3d: non-finite shader time")
	}
	packed, err := packShaderParams(params)
	if err != nil {
		return nil, err
	}
	return &MaterialShader{
		shader: wgsl,
		fn:     fallback,
		time:   time,
		params: packed,
	}, nil
}

// WithParameters returns a copy of s with updated animation time and uniform
// parameters, sharing the WGSL shader source and Go fallback so GPU renderers
// reuse compiled pipelines and update only the uniform buffer.
func (s *MaterialShader) WithParameters(time float32, params [][4]float32) (*MaterialShader, error) {
	if !s.valid() {
		return nil, errors.New("nt3d: invalid material shader")
	}
	if !validFloat(time) {
		return nil, errors.New("nt3d: non-finite shader time")
	}
	packed, err := packShaderParams(params)
	if err != nil {
		return nil, err
	}
	if s.time == time && s.params == packed {
		return s, nil
	}
	next := *s
	next.time = time
	next.params = packed
	return &next, nil
}

// Time returns s's animation time in seconds, or 0 when s is nil.
func (s *MaterialShader) Time() float32 {
	if s == nil {
		return 0
	}
	return s.time
}

// Parameters returns a copy of s's uniform parameter array, or zeros when s is nil.
func (s *MaterialShader) Parameters() [MaxShaderParameters][4]float32 {
	if s == nil {
		return [MaxShaderParameters][4]float32{}
	}
	return s.params
}

func (s *MaterialShader) valid() bool {
	return s != nil && s.shader != "" && s.fn != nil && validFloat(s.time) && validShaderParamArray(&s.params)
}

func packShaderParams(params [][4]float32) ([MaxShaderParameters][4]float32, error) {
	var out [MaxShaderParameters][4]float32
	if len(params) > MaxShaderParameters {
		return out, fmt.Errorf("nt3d: shader parameter count %d exceeds MaxShaderParameters (%d)", len(params), MaxShaderParameters)
	}
	for i, p := range params {
		if !validFloat(p[0], p[1], p[2], p[3]) {
			return out, errors.New("nt3d: non-finite shader parameter")
		}
		out[i] = p
	}
	return out, nil
}

func validShaderParamArray(params *[MaxShaderParameters][4]float32) bool {
	for i := range params {
		p := params[i]
		if !validFloat(p[0], p[1], p[2], p[3]) {
			return false
		}
	}
	return true
}

// displacedVertices returns g's vertices after evaluating g.Displacement in
// local space with world-space rest positions derived from world. When
// g.Displacement is nil or g has no vertices, g.Vertices is returned directly
// without allocation.
func displacedVertices(g *Geometry, world nt3dmath.Mat4) []Vertex {
	d := g.Displacement
	if d == nil || d.fn == nil || len(g.Vertices) == 0 {
		return g.Vertices
	}
	out := make([]Vertex, len(g.Vertices))
	in := VertexDisplaceInput{
		Time:   d.time,
		Params: d.params,
	}
	for i, v := range g.Vertices {
		in.Position = v.Position
		in.Normal = v.Normal
		in.World = world.Point(v.Position)
		if i < len(g.UV) {
			in.UV = g.UV[i]
		} else {
			in.UV = UV{}
		}
		res := d.fn(in)
		pos, norm := res.Position, res.Normal
		if !finiteVec(pos) {
			pos = v.Position
		}
		if !finiteVec(norm) {
			norm = v.Normal
		}
		out[i] = Vertex{Position: pos, Normal: norm, Color: v.Color}
	}
	return out
}

func evalMaterialColor(s *MaterialShader, world, normal, view nt3dmath.Vec3, uv UV, base [4]float32) [4]float32 {
	if s == nil || s.fn == nil {
		return base
	}
	out := s.fn(MaterialColorInput{
		World:  world,
		Normal: normal,
		View:   view,
		UV:     uv,
		Color:  base,
		Time:   s.time,
		Params: s.params,
	})
	for k := range 3 {
		if !validFloat(out[k]) {
			out[k] = base[k]
		}
		if out[k] < 0 {
			out[k] = 0
		}
	}
	if !validFloat(out[3]) {
		out[3] = base[3]
	}
	out[3] = min(1, max(0, out[3]))
	return out
}

func padGeometryBounds(rest nt3dmath.AABB, hasVertices bool, maxDisp float32) nt3dmath.AABB {
	if !rest.IsValid() || !hasVertices || maxDisp <= 0 {
		return rest
	}
	ext := nt3dmath.Vec3{X: maxDisp, Y: maxDisp, Z: maxDisp}
	var padded nt3dmath.AABB
	padded.Include(rest.Min.Sub(ext))
	padded.Include(rest.Max.Add(ext))
	return padded
}

func applyStoredDisplacement(g *sceneGeometry, d *VertexDisplacement) {
	oldPad := g.Displacement.MaxDisplacement()
	newPad := d.MaxDisplacement()
	g.Displacement = d
	if oldPad == newPad {
		return
	}
	if g.restBounds.IsValid() && len(g.Points)+len(g.Boxes)+len(g.Lines)+len(g.Arrows) == 0 {
		g.Bounds = padGeometryBounds(g.restBounds, len(g.Vertices) > 0, newPad)
		return
	}
	g.Bounds, g.restBounds = geometryBoundsPair(g.Geometry)
}

// SetDisplacement sets or clears the VertexDisplacement of every indexed
// triangle mesh in root's subtree (including LOD tiers and skeletal/morph
// deformations) while preserving uploaded GPU mesh buffers and SharedGeometry
// instancing. Pass nil to restore undisplaced rest geometry. Use Root to update
// the whole scene.
func (s *Scene) SetDisplacement(root NodeID, d *VertexDisplacement) error {
	if d != nil && !d.valid() {
		return errors.New("nt3d: invalid vertex displacement")
	}
	if s.nodes[root] == nil {
		return fmt.Errorf("nt3d: unknown displacement root %d", root)
	}

	anyChanged, anyTexChanged, deformSlotChanged := false, false, false
	pending := []NodeID{root}
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		n := s.nodes[id]
		pending = append(pending, n.Children...)

		if len(n.lod) > 0 {
			lodChanged, lodBoundsChanged := false, false
			for i := range n.lod {
				lg := n.lod[i].geometry
				if len(lg.Indices) == 0 || lg.Displacement == d {
					continue
				}
				oldMapped := mapped(lg.Geometry)
				oldPad := lg.Displacement.MaxDisplacement()
				g := *lg
				applyStoredDisplacement(&g, d)
				if mapped(g.Geometry) != oldMapped {
					anyTexChanged = true
				}
				if oldPad != d.MaxDisplacement() {
					lodBoundsChanged = true
				}
				n.lod[i].geometry = &g
				lodChanged = true
			}
			if lodChanged {
				n.lodDispPad = d.MaxDisplacement()
				if lodBoundsChanged {
					var lb nt3dmath.AABB
					for i := range n.lod {
						if b := n.lod[i].geometry.Bounds; b.IsValid() {
							lb.Include(b.Min)
							lb.Include(b.Max)
						} else if n.lod[i].geometry.Billboard != nil {
							lb.Include(nt3dmath.Vec3{})
						}
					}
					n.lodBounds = lb
				}
				n.geometry = n.lod[0].geometry
				anyChanged = true
			}
			continue
		}

		if n.geometry == nil || len(n.geometry.Indices) == 0 || n.geometry.Displacement == d {
			continue
		}
		old := n.geometry
		oldMapped := mapped(old.Geometry)
		if n.deformation != nil {
			def := n.deformation
			oldBase := def.base
			oldBase.Material = old.Material
			oldBase.Displacement = old.Displacement
			applyStoredDisplacement(&def.base, d)
			nextBase := def.base
			nextBase.Material = old.Material
			g := *old
			applyStoredDisplacement(&g, d)
			if mapped(oldBase.Geometry) != mapped(nextBase.Geometry) {
				def.weights = nil
				def.revision = nextRevision.Add(1)
				anyTexChanged = true
				deformSlotChanged = true
			} else if g.posed != nil {
				p := *g.posed
				p.source.Displacement = d
				g.posed = &p
			}
			n.geometry = &g
			anyChanged = true
			continue
		}

		g := *old
		applyStoredDisplacement(&g, d)
		if mapped(g.Geometry) != oldMapped {
			anyTexChanged = true
		}
		n.geometry = &g
		anyChanged = true
	}

	if !anyChanged {
		return nil
	}
	s.changed(deformSlotChanged, anyTexChanged || deformSlotChanged)
	if deformSlotChanged {
		return s.UpdateDeformations()
	}
	return nil
}

// SetMaterialShader sets or clears Material.Shader on every indexed triangle
// mesh in root's subtree (including LOD tiers and skeletal/morph deformations)
// while preserving existing Material properties, uploaded GPU mesh buffers, and
// SharedGeometry instancing. Meshes with a nil Material receive a default
// Material carrying shader when shader is non-nil. Pass nil to clear custom
// material shaders. Use Root to update the whole scene.
func (s *Scene) SetMaterialShader(root NodeID, shader *MaterialShader) error {
	if shader != nil && !shader.valid() {
		return errors.New("nt3d: invalid material shader")
	}
	if s.nodes[root] == nil {
		return fmt.Errorf("nt3d: unknown material shader root %d", root)
	}

	withShader := func(m *Material) (*Material, bool) {
		var cur *MaterialShader
		if m != nil {
			cur = m.Shader
		}
		if cur == shader {
			return m, false
		}
		if m == nil {
			return &Material{Shader: shader}, true
		}
		cp := cloneMaterial(m)
		cp.Shader = shader
		return cp, true
	}

	anyChanged, anyTexChanged, deformSlotChanged := false, false, false
	pending := []NodeID{root}
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		n := s.nodes[id]
		pending = append(pending, n.Children...)

		if len(n.lod) > 0 {
			lodChanged := false
			for i := range n.lod {
				lg := n.lod[i].geometry
				if len(lg.Indices) == 0 {
					continue
				}
				nextMat, ok := withShader(lg.Material)
				if !ok {
					continue
				}
				oldMapped := mapped(lg.Geometry)
				g := *lg
				g.Material = nextMat
				if mapped(g.Geometry) != oldMapped {
					anyTexChanged = true
				}
				n.lod[i].geometry = &g
				lodChanged = true
			}
			if lodChanged {
				n.geometry = n.lod[0].geometry
				anyChanged = true
			}
			continue
		}

		if n.geometry == nil || len(n.geometry.Indices) == 0 {
			continue
		}
		old := n.geometry
		nextMat, ok := withShader(old.Material)
		if !ok {
			continue
		}
		oldMapped := mapped(old.Geometry)
		if n.deformation != nil {
			def := n.deformation
			oldBase := def.base
			oldBase.Material = old.Material
			oldBase.Displacement = old.Displacement
			def.base.Material = cloneMaterial(nextMat)
			nextBase := def.base
			nextBase.Displacement = old.Displacement
			g := *old
			g.Material = nextMat
			if mapped(oldBase.Geometry) != mapped(nextBase.Geometry) {
				def.weights = nil
				def.revision = nextRevision.Add(1)
				anyTexChanged = true
				deformSlotChanged = true
			} else if g.posed != nil {
				p := *g.posed
				p.source.Material = nextMat
				g.posed = &p
			}
			n.geometry = &g
			anyChanged = true
			continue
		}

		g := *old
		g.Material = nextMat
		if mapped(g.Geometry) != oldMapped {
			anyTexChanged = true
		}
		n.geometry = &g
		anyChanged = true
	}

	if !anyChanged {
		return nil
	}
	s.changed(deformSlotChanged, anyTexChanged || deformSlotChanged)
	if deformSlotChanged {
		return s.UpdateDeformations()
	}
	return nil
}
