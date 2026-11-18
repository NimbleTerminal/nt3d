// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"errors"
	"fmt"
	"image/color"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// ShadingModel is how a material answers light.
type ShadingModel uint8

const (
	// ShadePhong lights a surface by Lambert diffuse, with the highlight and
	// reflection its Phong group describes. It is the zero value.
	ShadePhong ShadingModel = iota
	// ShadePBR lights it by Cook-Torrance GGX metallic/roughness shading, as
	// its PBR group describes: a zero group is a mirror-smooth dielectric.
	ShadePBR
	// ShadeUnlit shows its colors as they are, with no light applied.
	ShadeUnlit
)

// AlphaMode is what a material does with the alpha of its texture and
// vertex colors.
type AlphaMode uint8

const (
	// AlphaOpaque ignores alpha. It is the zero value.
	AlphaOpaque AlphaMode = iota
	// AlphaCutout skips texels whose alpha is below Threshold, so a texture
	// can cut shapes out of a triangle, as leaves, lace, and fur do; what
	// remains is opaque and depth tested as usual, with no blending.
	AlphaCutout
	// AlphaEdgeOn is a cutout shown only where the surface turns edge-on to
	// the viewer: its threshold rises toward 1 as the surface comes to face
	// the camera, so it fades out face-on. Fur's fins use it to fill the
	// silhouette; it needs the frame's camera (Frame.Eye), which Scene.Frame
	// supplies, and wireframe rendering and picking leave it out.
	AlphaEdgeOn
	// AlphaTransparent blends the triangles over what lies behind them, by
	// the vertex colors' alpha times the texture's: glass, water, ghosts.
	// Opaque surfaces are drawn first; then transparent geometries, farthest
	// first by the centers of their bounds, each with its back faces (lit
	// from behind) before its front faces. They are depth tested but hide
	// nothing, so a convex shape shows its far side through its near one.
	// Interleaved or intersecting transparent geometries, and the overlapping
	// layers of one non-convex mesh, can blend out of order. Wireframe and
	// picking treat them as opaque.
	AlphaTransparent
)

// Material is how triangles look. The zero value is an opaque, untextured
// surface lit by ShadePhong.
//
// Two fields choose what kind of surface it is, and nothing else does:
// Shading is the lighting model, and Alpha is what alpha does. The rest are
// groups, one per feature, each off while it is zero: Phong and PBR for the
// two lighting models, Normal for surface detail, Emission for glow,
// Transmission for seeing through, and Toon for banded shading. A group's
// type is its field's name with Style after it. A group that belongs to a
// lighting model the material does not use is refused, not ignored.
//
// A strength or intensity left zero means 1 wherever its group is in use;
// none is said by leaving the group out, or by the flag the group documents.
type Material struct {
	// Shading is the lighting model: ShadePhong (the zero value), ShadePBR,
	// or ShadeUnlit.
	Shading ShadingModel

	// Texture replaces vertex colors, and uses Geometry.UV. Repeat tiles it:
	// UVs outside [0, 1] wrap, and filtering blends across tile edges, so a
	// texture made to tile shows no seams; without it, UVs clamp to the
	// texture's edge. FullSize samples it full size, without its mipmaps:
	// far texels shimmer, so it is for comparing, or for a texture that must
	// never blur; shader textures ignore it. Tint multiplies the texture by
	// the vertex colors, so one gray texture can take many colors.
	Texture  *Texture
	Repeat   bool
	FullSize bool
	Tint     bool
	// BaseColor multiplies the surface's base sRGB color, and on an
	// AlphaTransparent material scales opacity by BaseColor.A / 255. The
	// zero value leaves vertex and texture colors unchanged, so opaque black
	// is color.RGBA{A: 255}. It is passed per instance, so Scene.ApplyMaterial
	// preserves uploaded GPU mesh buffers and SharedGeometry instancing.
	BaseColor color.RGBA

	// Alpha is what the surface does with alpha: AlphaOpaque (the zero
	// value), AlphaCutout, AlphaEdgeOn, or AlphaTransparent. Threshold is a
	// cutout's alpha threshold, 0..1; zero means one half. Varying it over
	// copies of one texture reveals different parts of it, as fur's layers
	// do. A cutout samples mipmaps of its own, whose alpha keeps the share of
	// texels above any threshold about what it is at full size (see
	// Texture.cutoutLevels), so far off it stays as dense as near by instead
	// of thinning away; it filters between levels but takes no anisotropic
	// taps, which would average it thin again.
	Alpha     AlphaMode
	Threshold float32

	// Phong is the highlight and reflection of a ShadePhong material.
	Phong PhongStyle
	// PBR is the surface of a ShadePBR material.
	PBR PBRStyle
	// Normal perturbs the shading normal from a map.
	Normal NormalStyle
	// Emission adds light of the surface's own.
	Emission EmissionStyle
	// Transmission shows the opaque scene behind the surface through it.
	Transmission TransmissionStyle
	// Toon shades in flat bands; see ToonStyle. The zero value is off.
	Toon ToonStyle

	// LightMap supplies baked linear irradiance using Geometry.LightMapUV.
	// It replaces ambient/environment diffuse; direct and specular light remain.
	LightMap *LightMap
	// Shader optionally customizes per-fragment linear-light RGBA surface color
	// via a WGSL shader on WebGPU and a pure-Go fallback in software rendering.
	Shader *MaterialShader
}

// PhongStyle is the highlight and reflection of a ShadePhong material. The
// zero value has neither.
type PhongStyle struct {
	// Specular adds a white highlight where the surface mirrors the light
	// toward the camera (Blinn-Phong): its strength, 0 (none) to 1. Shininess
	// is its tightness, the exponent, 1 to 1024; zero means 32, and larger is
	// smaller and sharper, as on polished plastic or glass. It is found at
	// each pixel, from normals interpolated across the triangle, so
	// highlights stay round on coarse meshes. It needs the frame's camera
	// (Frame.Eye), which Scene.Frame supplies; wireframe has none.
	Specular  float32
	Shininess float32
	// Reflectivity is the environment reflection at normal incidence (F0),
	// 0..1. Zero is none; .02 suits water and 1 is a mirror. Schlick Fresnel
	// increases reflection toward grazing angles. It needs an Environment
	// that lights the frame, or a reflection probe, and samples the distant
	// sky or a probe's panorama, not objects.
	Reflectivity float32
}

// PBRStyle is the surface of a ShadePBR material: single-scatter
// Cook-Torrance GGX metallic/roughness shading. The zero value is a
// mirror-smooth dielectric.
type PBRStyle struct {
	// Metallic is the conductor weight, 0..1 (0 dielectric, 1 metal). Metals
	// tint F0 by the surface base color and suppress diffuse reflection.
	Metallic float32
	// Roughness is perceptual microfacet roughness, 0..1 (0 mirror-smooth,
	// 1 matte). When Frame.Environment is set, PBR surfaces sample its
	// prefiltered roughness mip chain automatically.
	Roughness float32
	// IOR is the dielectric index of refraction, 1..3; zero means 1.5
	// (F0 = 0.04).
	IOR float32
	// ORMMap is a linear data texture created with NewORMMap: red is ambient
	// occlusion, green multiplies Roughness, and blue multiplies Metallic,
	// matching glTF 2.0's channel layout. It uses Geometry.UV, and ORMRepeat
	// tiles it. OcclusionStrength scales its occlusion of indirect light,
	// 0..1; zero means 1, so NoOcclusion is how to ignore the red channel.
	ORMMap            *Texture
	ORMRepeat         bool
	OcclusionStrength float32
	NoOcclusion       bool
	// Specular adjusts the dielectric highlight.
	Specular SpecularStyle
	// Clearcoat adds a clear layer over the surface.
	Clearcoat ClearcoatStyle
	// Sheen adds a cloth-like glow toward the silhouette.
	Sheen SheenStyle
}

// SpecularStyle adjusts a PBR material's dielectric highlight, matching
// glTF's KHR_materials_specular. The zero value leaves it as IOR gives it.
type SpecularStyle struct {
	// Color tints the highlight at normal incidence (F0), in sRGB; zero means
	// white, and alpha is ignored. Intensity scales it (>= 0; zero means 1):
	// values above 1 raise F0, which is clamped per channel at 1 before
	// metallic mixing.
	Color     color.RGBA
	Intensity float32
	// Strength scales the whole highlight, grazing angles included, 0..1;
	// zero means 1, so Off is how to ask for none.
	Strength float32
	Off      bool
}

// ClearcoatStyle is a dielectric lacquer, polyurethane, or water layer (IOR
// 1.5, F0 = 0.04) over a PBR surface, evaluated along the unperturbed
// geometric normal. The zero value is none.
type ClearcoatStyle struct {
	// Amount is how much of the layer there is, 0..1; zero is none.
	Amount float32
	// Roughness is the layer's perceptual roughness, 0..1 (0 mirror-smooth,
	// 1 rough).
	Roughness float32
}

// SheenStyle is a retroreflective microfiber lobe on a PBR surface: velvet,
// satin, felt. The zero value is none.
type SheenStyle struct {
	// Color tints the lobe, in sRGB; black is none, and alpha is ignored.
	Color color.RGBA
	// Roughness is its perceptual roughness, 0..1: low values concentrate
	// sheen near the silhouette, and high values broaden it across the
	// surface.
	Roughness float32
}

// NormalStyle perturbs a surface's shading normal from a map. The zero value
// leaves the normal as the mesh has it.
type NormalStyle struct {
	// Map is created with NewNormalMap and uses Geometry.UV. Strength scales
	// its tangent X and Y; zero means 1, and to have none, leave Map out.
	// Repeat tiles it, independently of the base-color texture. FullSize
	// does not apply to it.
	Map      *Texture
	Strength float32
	Repeat   bool
	// Flow advects the map along a flow field, as moving water does.
	Flow FlowStyle
}

// FlowStyle advects a normal map using Geometry.UV to sample a flow field.
// The zero value is none; it needs a normal map.
type FlowStyle struct {
	// Map is the flow field. Phase is time in cycles (e.g. seconds / period):
	// two offset phases crossfade to avoid a reset jump. Strength scales UV
	// displacement per cycle; zero is none. Repeat tiles the field.
	Map             *FlowMap
	Phase, Strength float32
	Repeat          bool
}

// EmissionStyle is light of a surface's own, added after shading and before
// fog. The zero value is none.
type EmissionStyle struct {
	// Color is the light's sRGB color. With a Map and no Color, it is white.
	Color color.RGBA
	// Intensity scales it (>= 0); zero means 1. Values above 1 give HDR glow
	// for PostProcess.Bloom in linear frames. To have none, leave Color and
	// Map out.
	Intensity float32
	// Map is an optional sRGB color texture multiplied by Color and
	// Intensity, sampled at Geometry.UV. Repeat tiles it.
	Map    *Texture
	Repeat bool
}

// TransmissionStyle shows the opaque scene behind a surface through it. The
// zero value is none. Transmissive meshes draw after opaque geometry and
// write depth before transparent meshes and particles; like transparent
// meshes, they do not cast shadow maps. It needs an AlphaOpaque surface that
// is not ShadeUnlit, and samples the opaque scene only, not other
// transmitters.
type TransmissionStyle struct {
	// Amount mixes the scene behind into the surface's shading, 0..1; zero
	// is none.
	Amount float32
	// Refraction offsets the scene lookup along the shading normal projected
	// into screen space, in fractions of viewport height, 0..1; zero is
	// undistorted. Offscreen and foreground samples fall back to the
	// undistorted lookup.
	Refraction float32
	// Absorption is the nonnegative RGB extinction coefficient per world
	// unit: Beer-Lambert attenuation mixes transmitted light toward the lit
	// surface color, by the view-ray distance to opaque geometry, capped at
	// 1000 world units.
	Absorption nt3dmath.Vec3
}

// unlit, cutout, edgeOn, and blended say what kind of surface a material is.
// A nil material is a lit, opaque one.
func (m *Material) unlit() bool { return m != nil && m.Shading == ShadeUnlit }
func (m *Material) cutout() bool {
	return m != nil && (m.Alpha == AlphaCutout || m.Alpha == AlphaEdgeOn)
}
func (m *Material) edgeOn() bool  { return m != nil && m.Alpha == AlphaEdgeOn }
func (m *Material) blended() bool { return m != nil && m.Alpha == AlphaTransparent }

// normalStrength is how strongly the normal map applies: zero without a map,
// and 1 when its Strength is left zero.
func (m *Material) normalStrength() float32 {
	if m == nil || m.Normal.Map == nil {
		return 0
	}
	if m.Normal.Strength == 0 {
		return 1
	}
	return m.Normal.Strength
}

// Validate reports a material that cannot mean what it says: a value out of
// range, a group that belongs to a lighting model the material does not use,
// or a combination that cannot be drawn. A scene returns its error when it is
// given a geometry with the material.
func (m Material) Validate() error { return checkMaterial(&m, true) }

// transparent reports whether a geometry's triangles are blended.
func transparent(g *Geometry) bool {
	return g.Material != nil && g.Material.blended() && len(g.Indices) > 0
}

func transmitting(g *Geometry) bool {
	return g.Material != nil && g.Material.Transmission.Amount > 0 && len(g.Indices) > 0
}

// edgeOn reports whether a geometry is drawn only edge-on, so that wireframe
// and picking, which have no view of it, leave it out.
func edgeOn(g Geometry) bool { return g.Material != nil && g.Material.edgeOn() }

// edgeOnCut is a cutout threshold raised as the surface faces the viewer:
// unchanged edge-on, past 1 (cutting everything) once |normal · view| passes
// one half, smoothly between. The GPU's vs_texture computes the same.
func edgeOnCut(threshold float32, normal, view nt3dmath.Vec3) float32 {
	if normal.Dot(normal) == 0 || view.Dot(view) == 0 {
		return threshold
	}
	facing := float64(abs(normal.Normalize().Dot(view.Normalize())))
	t := min(1, max(0, (facing-.15)/.35))
	fade := float32(t * t * (3 - 2*t))
	return threshold + (1.01-threshold)*fade
}

// threshold is the alpha below which a cutout material skips texels.
func (m *Material) threshold() float32 {
	if m == nil || m.Threshold == 0 {
		return .5
	}
	return m.Threshold
}

func cloneMaterial(m *Material) *Material {
	if m == nil {
		return nil
	}
	c := *m
	return &c
}

// validateMaterial checks intrinsic Material invariants independent of a
// specific mesh's vertex/UV slices.
func validateMaterial(m *Material) error { return checkMaterial(m, true) }

// checkMaterial is the one place a Material's own rules are written.
// hasUV says whether the mesh it is on has a UV for every vertex, which its
// texture and maps need; validateMaterial, with no mesh to ask, assumes so.
func checkMaterial(m *Material, hasUV bool) error {
	if m == nil {
		return nil
	}
	if m.Shading > ShadeUnlit {
		return fmt.Errorf("nt3d: unknown material Shading %d", m.Shading)
	}
	if m.Alpha > AlphaTransparent {
		return fmt.Errorf("nt3d: unknown material Alpha %d", m.Alpha)
	}
	// A group that belongs to another lighting model is a mistake, not a
	// switch: it would otherwise be ignored without a word.
	if m.Shading != ShadePBR && m.PBR != (PBRStyle{}) {
		return errors.New("nt3d: material sets its PBR group but its Shading is not ShadePBR")
	}
	if m.Shading != ShadePhong && m.Phong != (PhongStyle{}) {
		return errors.New("nt3d: material sets its Phong group but its Shading is not ShadePhong")
	}
	if !validFloat(m.PBR.Specular.Strength) || m.PBR.Specular.Strength < 0 || m.PBR.Specular.Strength > 1 {
		return errors.New("nt3d: PBR specular strength must be 0..1")
	}
	if m.LightMap != nil && !m.LightMap.valid() {
		return fmt.Errorf("nt3d: invalid lightmap")
	}
	if m.Shader != nil && !m.Shader.valid() {
		return fmt.Errorf("nt3d: invalid material shader")
	}
	if m.Texture != nil && (!m.Texture.valid() || m.Texture.data || !hasUV) {
		return fmt.Errorf("nt3d: invalid texture or missing UVs")
	}
	if !validFloat(m.Transmission.Amount, m.Transmission.Refraction, m.Transmission.Absorption.X, m.Transmission.Absorption.Y, m.Transmission.Absorption.Z) || m.Transmission.Amount < 0 || m.Transmission.Amount > 1 || m.Transmission.Refraction < 0 || m.Transmission.Refraction > 1 || m.Transmission.Absorption.X < 0 || m.Transmission.Absorption.Y < 0 || m.Transmission.Absorption.Z < 0 {
		return fmt.Errorf("nt3d: invalid transmission, refraction, or absorption")
	}
	if m.Transmission.Amount > 0 && (m.Alpha != AlphaOpaque || m.unlit()) {
		return fmt.Errorf("nt3d: transmission requires a lit, non-blended surface")
	}
	if !validFloat(m.Normal.Flow.Phase, m.Normal.Flow.Strength) || m.Normal.Flow.Strength < 0 {
		return fmt.Errorf("nt3d: flow phase must be finite and strength nonnegative")
	}
	if m.Normal.Flow.Map != nil && (m.Normal.Flow.Map.texture == nil || m.Normal.Map == nil) {
		return fmt.Errorf("nt3d: flow requires a valid flow map and normal map")
	}
	if !validFloat(m.Normal.Strength) {
		return fmt.Errorf("nt3d: non-finite normal strength")
	}
	if m.Normal.Map != nil && (!m.Normal.Map.valid() || !m.Normal.Map.data || !hasUV) {
		return fmt.Errorf("nt3d: normal map requires NewNormalMap and vertex UVs")
	}
	if !validFloat(m.PBR.Metallic, m.PBR.Roughness, m.PBR.IOR, m.PBR.Specular.Intensity, m.PBR.Clearcoat.Amount, m.PBR.Clearcoat.Roughness, m.PBR.Sheen.Roughness, m.PBR.OcclusionStrength, m.Emission.Intensity) {
		return fmt.Errorf("nt3d: non-finite PBR or emissive parameter")
	}
	if m.PBR.Specular.Intensity < 0 {
		return fmt.Errorf("nt3d: specular intensity must be nonnegative")
	}
	if m.PBR.Metallic < 0 || m.PBR.Metallic > 1 || m.PBR.Roughness < 0 || m.PBR.Roughness > 1 {
		return fmt.Errorf("nt3d: metallic and roughness must be 0..1")
	}
	if m.PBR.Clearcoat.Amount < 0 || m.PBR.Clearcoat.Amount > 1 || m.PBR.Clearcoat.Roughness < 0 || m.PBR.Clearcoat.Roughness > 1 || m.PBR.Sheen.Roughness < 0 || m.PBR.Sheen.Roughness > 1 {
		return fmt.Errorf("nt3d: clearcoat and sheen parameters must be 0..1")
	}
	if m.PBR.IOR != 0 && (m.PBR.IOR < 1 || m.PBR.IOR > 3) {
		return fmt.Errorf("nt3d: IOR must be 0 or 1..3")
	}
	if m.PBR.OcclusionStrength < 0 || m.PBR.OcclusionStrength > 1 {
		return fmt.Errorf("nt3d: occlusion strength must be 0..1")
	}
	if m.PBR.ORMMap != nil && (!m.PBR.ORMMap.valid() || !m.PBR.ORMMap.data || !hasUV) {
		return fmt.Errorf("nt3d: ORM map requires NewORMMap and vertex UVs")
	}
	if m.Emission.Intensity < 0 {
		return fmt.Errorf("nt3d: emissive intensity must be nonnegative")
	}
	if m.Emission.Map != nil && (!m.Emission.Map.valid() || m.Emission.Map.data || !hasUV) {
		return fmt.Errorf("nt3d: emissive map requires a color texture and vertex UVs")
	}
	if !validFloat(m.Threshold) || m.Threshold < 0 || m.Threshold > 1 {
		return fmt.Errorf("nt3d: cutout threshold must be 0..1")
	}
	if !validFloat(m.Phong.Specular, m.Phong.Shininess) || m.Phong.Specular < 0 || m.Phong.Specular > 1 || m.Phong.Shininess != 0 && (m.Phong.Shininess < 1 || m.Phong.Shininess > 1024) {
		return fmt.Errorf("nt3d: specular must be 0..1 and shininess 1..1024")
	}
	if err := m.Toon.validate(); err != nil {
		return err
	}
	if !validFloat(m.Phong.Reflectivity) || m.Phong.Reflectivity < 0 || m.Phong.Reflectivity > 1 {
		return fmt.Errorf("nt3d: reflectivity must be 0..1")
	}
	return nil
}

func validateMaterialForMesh(g Geometry, m *Material) error {
	if m == nil || len(g.Indices) == 0 {
		return nil
	}
	hasUV := len(g.UV) == len(g.Vertices)
	if m.Texture != nil && !hasUV {
		return fmt.Errorf("nt3d: invalid texture or missing UVs")
	}
	if m.Normal.Map != nil && !hasUV {
		return fmt.Errorf("nt3d: normal map requires NewNormalMap and vertex UVs")
	}
	if m.PBR.ORMMap != nil && !hasUV {
		return fmt.Errorf("nt3d: ORM map requires NewORMMap and vertex UVs")
	}
	if m.Emission.Map != nil && !hasUV {
		return fmt.Errorf("nt3d: emissive map requires a color texture and vertex UVs")
	}
	g.Material = m
	if err := validateLightMap(g); err != nil {
		return err
	}
	if extended(g) && len(g.Indices) > MeshIndexLimit()/2 {
		return fmt.Errorf("nt3d: mesh with extended UV or tangent data exceeds GPU binding limit")
	}
	return nil
}

func materialsEqual(a, b *Material) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func textureBindingsEqual(a, b *Material) bool {
	if a == nil && b == nil {
		return true
	}
	var za, zb Material
	if a != nil {
		za = *a
	}
	if b != nil {
		zb = *b
	}
	return za.LightMap == zb.LightMap && za.Texture == zb.Texture &&
		za.Repeat == zb.Repeat &&
		za.cutout() == zb.cutout() &&
		za.FullSize == zb.FullSize &&
		za.Normal.Map == zb.Normal.Map &&
		za.Normal.Repeat == zb.Normal.Repeat &&
		za.PBR.ORMMap == zb.PBR.ORMMap &&
		za.PBR.ORMRepeat == zb.PBR.ORMRepeat &&
		za.Emission.Map == zb.Emission.Map &&
		za.Emission.Repeat == zb.Emission.Repeat
}

// vertexMaterialEqual reports whether old and next encode identical GPU mesh
// vertex/extension records, allowing instance-uniform and texture-binding
// changes to reuse existing uploaded mesh buffers and SharedGeometry batches.
func vertexMaterialEqual(old, next Geometry) bool {
	if mapped(old) != mapped(next) || transparent(&old) != transparent(&next) {
		return false
	}
	if !mapped(next) {
		return true
	}
	if extended(old) != extended(next) || (len(old.Tangents) > 0) != (len(next.Tangents) > 0) {
		return false
	}
	om, nm := materialOrZero(old.Material), materialOrZero(next.Material)
	if om.Normal.Repeat != nm.Normal.Repeat || om.Repeat != nm.Repeat {
		return false
	}
	if (om.edgeOn() && om.Texture != nil) != (nm.edgeOn() && nm.Texture != nil) {
		return false
	}
	if (om.Tint || om.Texture == nil) != (nm.Tint || nm.Texture == nil) {
		return false
	}
	if !transparent(&next) && old.Material.threshold() != next.Material.threshold() {
		return false
	}
	if om.normalStrength() != nm.normalStrength() {
		return false
	}
	if extended(next) {
		if om.Normal.Flow.Map != nm.Normal.Flow.Map || (nm.Normal.Flow.Map != nil && om.Normal.Flow.Repeat != nm.Normal.Flow.Repeat) {
			return false
		}
	}
	return true
}

// ApplyMaterial replaces the material of every indexed triangle mesh in root's
// subtree (including LOD tiers), root included, as the subtree is when it is
// called: like every Apply method it remembers nothing, so a mesh added
// afterwards keeps its own material. Pass nil to clear materials back to
// default untextured surface shading.
//
// Input materials are copied and validated atomically across the subtree before
// mutating any node: applying a textured, normal-mapped, ORM-mapped, or
// emissive-mapped material to a mesh without vertex UVs returns an error and
// leaves the scene unchanged.
//
// Changes confined to shader/instance uniforms or texture bindings preserve
// uploaded GPU mesh buffers, fixed-topology vertex updates, SharedGeometry
// instancing, and skeletal/morph deformations; changes to vertex-encoded
// properties (such as Transparent, Cutout/Threshold, Repeat, Tint, FlowMap, or
// adding/removing maps altogether) automatically re-encode affected mesh nodes
// and detach modified SharedGeometry instances while preserving previous Frame
// snapshots. Use Root to update the whole scene.
func (s *Scene) ApplyMaterial(root NodeID, m *Material) error {
	if err := validateMaterial(m); err != nil {
		return err
	}
	if s.nodes[root] == nil {
		return fmt.Errorf("nt3d: unknown material root %d", root)
	}

	// Pre-validate every affected mesh in the subtree so incompatible meshes
	// fail atomically without partial mutation.
	var visit []NodeID
	pending := []NodeID{root}
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		n := s.nodes[id]
		pending = append(pending, n.Children...)
		if len(n.lod) > 0 {
			hasMesh := false
			for i := range n.lod {
				lg := n.lod[i].geometry
				if len(lg.Indices) == 0 {
					continue
				}
				hasMesh = true
				if err := validateMaterialForMesh(lg.Geometry, m); err != nil {
					return err
				}
			}
			if hasMesh {
				visit = append(visit, id)
			}
			continue
		}
		if n.geometry == nil || len(n.geometry.Indices) == 0 {
			continue
		}
		checkGeom := *n.geometry
		if n.deformation != nil {
			checkGeom = n.deformation.base
		}
		if err := validateMaterialForMesh(checkGeom.Geometry, m); err != nil {
			return err
		}
		visit = append(visit, id)
	}

	anyChanged, anyVertexChanged, anyTexChanged := false, false, false
	for _, id := range visit {
		n := s.nodes[id]
		if len(n.lod) > 0 {
			lodRebuilt := false
			for i := range n.lod {
				lg := n.lod[i].geometry
				if len(lg.Indices) == 0 || materialsEqual(lg.Material, m) {
					continue
				}
				if !textureBindingsEqual(lg.Material, m) {
					anyTexChanged = true
				}
				g := *lg
				g.Material = cloneMaterial(m)
				if m != nil && m.Normal.Map != nil && len(g.Tangents) == 0 {
					generateTangentsOnUpload(&g.Geometry)
				}
				if !vertexMaterialEqual(lg.Geometry, g.Geometry) {
					g.meshUpdate = nil
					g.meshCert = newMeshCertificate(&g.Geometry, lg.splitOutlines(&lg.Geometry))
					n.lod[i].shared = nil
					n.lod[i].geometryRevision = nextRevision.Add(1)
					n.lod[i].pickIndex = nil
					lodRebuilt = true
					anyVertexChanged = true
				}
				n.lod[i].geometry = &g
				anyChanged = true
			}
			if lodRebuilt {
				retainKeys := make([]geometryKey, len(n.lod))
				for i := range n.lod {
					if n.lod[i].shared != nil {
						retainKeys[i] = geometryKey{shared: n.lod[i].shared.id}
					} else {
						retainKeys[i] = geometryKey{node: id, revision: n.lod[i].geometryRevision}
					}
				}
				for i := range n.lod {
					g := *n.lod[i].geometry
					g.retainKeys = retainKeys
					n.lod[i].geometry = &g
				}
			}
			n.geometry, n.shared, n.geometryRevision = n.lod[0].geometry, n.lod[0].shared, n.lod[0].geometryRevision
			continue
		}

		old := n.geometry
		if materialsEqual(old.Material, m) {
			continue
		}
		if !textureBindingsEqual(old.Material, m) {
			anyTexChanged = true
		}

		if n.deformation != nil {
			d := n.deformation
			nextBase := d.base
			nextBase.Material = cloneMaterial(m)
			if m != nil && m.Normal.Map != nil && len(nextBase.Tangents) == 0 {
				generateTangentsOnUpload(&nextBase.Geometry)
			}
			oldBase := d.base
			oldBase.Material = old.Material
			d.base = nextBase
			if !vertexMaterialEqual(oldBase.Geometry, nextBase.Geometry) {
				d.weights = nil
				d.revision = nextRevision.Add(1)
				g := *old
				g.Material = cloneMaterial(m)
				n.geometry = &g
				anyVertexChanged = true
			} else {
				g := *old
				g.Material = cloneMaterial(m)
				if g.posed != nil {
					p := *g.posed
					p.source.Material = g.Material
					g.posed = &p
				}
				n.geometry = &g
			}
			anyChanged = true
			continue
		}

		g := *old
		g.Material = cloneMaterial(m)
		if m != nil && m.Normal.Map != nil && len(g.Tangents) == 0 {
			generateTangentsOnUpload(&g.Geometry)
		}
		if !vertexMaterialEqual(old.Geometry, g.Geometry) {
			g.meshUpdate = nil
			g.meshCert = newMeshCertificate(&g.Geometry, old.splitOutlines(&old.Geometry))
			n.shared = nil
			n.geometryRevision = nextRevision.Add(1)
			n.pickIndex = nil
			anyVertexChanged = true
		}
		n.geometry = &g
		anyChanged = true
	}

	if !anyChanged {
		return nil
	}
	s.changed(anyVertexChanged, anyTexChanged || anyVertexChanged)
	if anyVertexChanged {
		if err := s.UpdateDeformations(); err != nil {
			return err
		}
	}
	return nil
}
