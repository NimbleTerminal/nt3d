// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"fmt"
	"image/color"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// Material controls triangle appearance. Texture replaces vertex colors.
// Unlit preserves colors without applying the scene light. Cutout skips
// texels whose alpha is below Threshold (zero means one half), so a texture
// can cut shapes out of a triangle, as leaves, lace, and fur do; what remains
// is opaque and depth tested as usual, with no blending. Without Cutout,
// alpha is ignored. Varying Threshold over copies of one texture reveals
// different parts of it, as fur's layers do. A cutout samples mipmaps of
// its own, whose alpha keeps the share of texels above any threshold about
// what it is at full size (see Texture.cutoutLevels), so far off it stays
// as dense as near by instead of thinning away; it filters between levels
// but takes no anisotropic taps, which would average it thin again.
//
// Repeat tiles the texture: UVs outside [0, 1] wrap, and filtering blends
// across tile edges, so a texture made to tile shows no seams. Without it,
// UVs clamp to the texture's edge.
//
// FullSize samples the texture full size, without its mipmaps, as every
// cutout did before they had mipmaps of their own: far texels shimmer, so
// it is for comparing, or for a texture that must never blur. It applies
// to Texture only, not NormalMap, and shader textures ignore it.
//
// Tint multiplies the texture by the vertex colors, so one gray texture can
// take many colors. EdgeOn shows a cutout texture only where the surface
// turns edge-on to the viewer: its threshold rises toward 1 as the surface
// comes to face the camera, so it fades out face-on. Fur's fins use it to
// fill the silhouette; it needs the frame's camera (Frame.Eye), which
// Scene.Frame supplies, and wireframe rendering and picking leave it out.
//
// Transparent blends the triangles over what lies behind them, by the vertex
// colors' alpha times the texture's: glass, water, ghosts. Opaque surfaces
// are drawn first; then transparent geometries, farthest first by the
// centers of their bounds, each with its back faces (lit from behind) before
// its front faces. They are depth tested but hide nothing, so a convex shape
// shows its far side through its near one. Interleaved or intersecting
// transparent geometries, and the overlapping layers of one non-convex mesh,
// can blend out of order. Wireframe and picking treat them as opaque. It
// cannot be combined with Cutout or EdgeOn.
//
// Specular adds a white highlight where the surface mirrors the light toward
// the camera (Blinn-Phong): Specular is its strength, 0 (none) to 1, and
// Shininess its tightness, the exponent, 1 to 1024 (zero means 32: larger
// is smaller and sharper, as on polished plastic or glass). It is found at
// each pixel, from normals interpolated across the triangle, so highlights
// stay round on coarse meshes. It needs the frame's camera (Frame.Eye),
// which Scene.Frame supplies; unlit materials and wireframe have none.
type Material struct {
	// LightMap supplies baked linear irradiance using Geometry.LightMapUV.
	// It replaces ambient/environment diffuse; direct and specular light remain.
	LightMap *LightMap

	// FlowMap advects NormalMap using Geometry.UV to sample a vertex flow field.
	// FlowPhase is time in cycles (e.g. seconds / period). Two offset phases
	// crossfade to avoid a reset jump. FlowStrength scales UV displacement per
	// cycle; zero disables advection. FlowRepeat controls field sampling.
	FlowMap                 *FlowMap
	FlowPhase, FlowStrength float32
	FlowRepeat              bool

	// Transmission mixes the opaque scene behind this surface into its shading,
	// 0..1. Transmissive meshes draw after opaque geometry and write depth before
	// transparent meshes and particles. They cannot also be Transparent, Cutout,
	// EdgeOn, or Unlit. Only the opaque scene is sampled, not other transmitters.
	// Like transparent meshes, transmissive triangles do not cast shadow maps.
	Transmission float32
	// Refraction offsets the scene lookup along the shading normal projected into
	// screen space, in fractions of viewport height (0..1). Zero is undistorted.
	// Offscreen and foreground samples fall back to the undistorted lookup.
	Refraction float32
	// Absorption is the nonnegative RGB extinction coefficient per world unit.
	// Beer-Lambert attenuation mixes transmitted light toward the lit surface color.
	// It uses the view-ray distance to opaque geometry, capped at 1000 world units.
	Absorption nt3dmath.Vec3

	// Reflectivity is the environment reflection at normal incidence (F0), 0..1.
	// Zero disables reflections; .02 suits water and 1 is a mirror. Schlick
	// Fresnel increases reflection toward grazing angles. Requires an Environment
	// that lights the frame, or a reflection probe; ignored for Unlit materials.
	// Reflections sample the distant sky or a probe's panorama, not objects.
	Reflectivity float32
	// NormalMap is created with NewNormalMap and uses Geometry.UV. NormalStrength
	// scales tangent X/Y; set 1 for normal detail, 0 to disable. NormalRepeat
	// controls its sampler independently of the base-color texture.
	NormalMap      *Texture
	NormalStrength float32
	NormalRepeat   bool
	Texture        *Texture
	// BaseColor multiplies the surface's base sRGB color (and scales opacity on
	// Transparent materials by BaseColor.A / 255). The zero value leaves vertex
	// and texture colors unchanged; pass A: 255 for opaque colors (such as opaque
	// black color.RGBA{A: 255}) and non-zero RGB (such as color.RGBA{R: 255, G: 255,
	// B: 255, A: 0}) for zero-opacity Transparent materials. Passed per instance so
	// Scene.SetMaterial preserves uploaded GPU mesh buffers and SharedGeometry
	// instancing.
	BaseColor   color.RGBA
	Unlit       bool
	Cutout      bool
	Threshold   float32
	Repeat      bool
	FullSize    bool
	Tint        bool
	EdgeOn      bool
	Transparent bool
	Specular    float32
	Shininess   float32
	// PBR enables single-scatter Cook-Torrance GGX metallic/roughness shading
	// in place of Lambert/Blinn-Phong. Setting Metallic, Roughness, ORMMap,
	// Clearcoat, SheenColor, SpecularColor, or SpecularIntensity above zero also
	// enables PBR automatically; set PBR: true explicitly when drawing a
	// mirror-smooth dielectric (Metallic: 0, Roughness: 0).
	PBR bool
	// Metallic is the conductor weight, 0..1 (0 dielectric, 1 metal). Metals
	// tint F0 by the surface base color and suppress diffuse reflection.
	Metallic float32
	// Roughness is perceptual microfacet roughness, 0..1 (0 mirror-smooth,
	// 1 matte). When Frame.Environment is set, PBR surfaces sample its
	// prefiltered roughness mip chain automatically.
	Roughness float32
	// IOR is the dielectric index of refraction in [1, 3]; zero defaults to
	// 1.5 (F0 = 0.04).
	IOR float32
	// SpecularColor tints and scales dielectric Fresnel F0 (and scales grazing F90
	// by SpecularColor.A / 255) in sRGB color, matching glTF's
	// KHR_materials_specular. The zero value leaves dielectric specular at
	// [1, 1, 1] with factor 1; pass A: 255 for full factor (such as
	// color.RGBA{A: 255} for specularColorFactor [0, 0, 0] with grazing Fresnel
	// preserved) or non-zero RGB with A: 0 (such as color.RGBA{R: 255, G: 255,
	// B: 255, A: 0}) to suppress dielectric specular completely. Setting a
	// non-zero SpecularColor or SpecularIntensity > 0 on a lit material activates
	// PBR.
	SpecularColor color.RGBA
	// SpecularIntensity scales SpecularColor (>= 0); zero defaults to 1. Values
	// above 1 raise dielectric F0 (clamped per channel at 1 before metallic
	// mixing, per KHR_materials_specular).
	SpecularIntensity float32
	// Clearcoat adds a dielectric polyurethane/lacquer/water top layer (IOR 1.5,
	// F0 = 0.04) evaluated along the unperturbed geometric surface normal over
	// the base shading layer, 0..1 (0 disables). Evaluated in both PBR and
	// PBR + Toon; setting Clearcoat > 0 on a lit material activates PBR.
	Clearcoat float32
	// ClearcoatRoughness is perceptual roughness of the clearcoat layer, 0..1
	// (0 mirror-smooth coat, 1 rough coat).
	ClearcoatRoughness float32
	// SheenColor tints a retroreflective microfiber/cloth grazing lobe in sRGB
	// color (velvet, satin, felt); zero/black disables sheen and alpha is
	// ignored. Evaluated in both PBR and PBR + Toon; setting a nonzero RGB
	// SheenColor on a lit material activates PBR.
	SheenColor color.RGBA
	// SheenRoughness is perceptual roughness of the microfiber sheen lobe, 0..1
	// (low values concentrate sheen near the silhouette; high values broaden it
	// across the surface).
	SheenRoughness float32
	// ORMMap is a linear data texture created with NewORMMap: red is ambient
	// occlusion, green multiplies Roughness, and blue multiplies Metallic,
	// matching glTF 2.0's channel layout. Uses Geometry.UV.
	ORMMap    *Texture
	ORMRepeat bool
	// OcclusionStrength scales ORMMap's red-channel ambient occlusion on
	// indirect lighting, 0..1; zero defaults to 1 when ORMMap is set.
	OcclusionStrength float32
	// Emissive adds self-illumination in sRGB color after surface shading.
	// When EmissiveMap is set and Emissive is zero, white is used.
	Emissive color.RGBA
	// EmissiveIntensity scales emissive radiance (>= 0); zero defaults to 1
	// when Emissive or EmissiveMap is set. Values above 1 produce HDR glow
	// for PostProcess.Bloom in linear frames.
	EmissiveIntensity float32
	// EmissiveMap is an optional sRGB color texture multiplied by Emissive
	// and EmissiveIntensity, sampled at Geometry.UV.
	EmissiveMap    *Texture
	EmissiveRepeat bool
	// Toon shades in flat bands; see ToonStyle. The zero value is off.
	Toon ToonStyle
	// Shader optionally customizes per-fragment linear-light RGBA surface color
	// via a WGSL shader on WebGPU and a pure-Go fallback in software rendering.
	Shader *MaterialShader
}

// transparent reports whether a geometry's triangles are blended.
func transparent(g *Geometry) bool {
	return g.Material != nil && g.Material.Transparent && len(g.Indices) > 0
}

func transmitting(g *Geometry) bool {
	return g.Material != nil && g.Material.Transmission > 0 && len(g.Indices) > 0
}

// edgeOn reports whether a geometry is drawn only edge-on, so that wireframe
// and picking, which have no view of it, leave it out.
func edgeOn(g Geometry) bool { return g.Material != nil && g.Material.EdgeOn }

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
	if m.LightMap != nil && !m.LightMap.valid() {
		return fmt.Errorf("nt3d: invalid lightmap")
	}
	if m.Shader != nil && !m.Shader.valid() {
		return fmt.Errorf("nt3d: invalid material shader")
	}
	if m.Texture != nil && (!m.Texture.valid() || m.Texture.data || !hasUV) {
		return fmt.Errorf("nt3d: invalid texture or missing UVs")
	}
	if !validFloat(m.Transmission, m.Refraction, m.Absorption.X, m.Absorption.Y, m.Absorption.Z) || m.Transmission < 0 || m.Transmission > 1 || m.Refraction < 0 || m.Refraction > 1 || m.Absorption.X < 0 || m.Absorption.Y < 0 || m.Absorption.Z < 0 {
		return fmt.Errorf("nt3d: invalid transmission, refraction, or absorption")
	}
	if m.Transmission > 0 && (m.Transparent || m.Cutout || m.EdgeOn || m.Unlit) {
		return fmt.Errorf("nt3d: transmission requires a lit, non-blended surface")
	}
	if !validFloat(m.FlowPhase, m.FlowStrength) || m.FlowStrength < 0 {
		return fmt.Errorf("nt3d: flow phase must be finite and strength nonnegative")
	}
	if m.FlowMap != nil && (m.FlowMap.texture == nil || m.NormalMap == nil) {
		return fmt.Errorf("nt3d: flow requires a valid flow map and normal map")
	}
	if !validFloat(m.NormalStrength) {
		return fmt.Errorf("nt3d: non-finite normal strength")
	}
	if m.NormalMap != nil && (!m.NormalMap.valid() || !m.NormalMap.data || !hasUV) {
		return fmt.Errorf("nt3d: normal map requires NewNormalMap and vertex UVs")
	}
	if !validFloat(m.Metallic, m.Roughness, m.IOR, m.SpecularIntensity, m.Clearcoat, m.ClearcoatRoughness, m.SheenRoughness, m.OcclusionStrength, m.EmissiveIntensity) {
		return fmt.Errorf("nt3d: non-finite PBR or emissive parameter")
	}
	if m.SpecularIntensity < 0 {
		return fmt.Errorf("nt3d: specular intensity must be nonnegative")
	}
	if m.Metallic < 0 || m.Metallic > 1 || m.Roughness < 0 || m.Roughness > 1 {
		return fmt.Errorf("nt3d: metallic and roughness must be 0..1")
	}
	if m.Clearcoat < 0 || m.Clearcoat > 1 || m.ClearcoatRoughness < 0 || m.ClearcoatRoughness > 1 || m.SheenRoughness < 0 || m.SheenRoughness > 1 {
		return fmt.Errorf("nt3d: clearcoat and sheen parameters must be 0..1")
	}
	if m.IOR != 0 && (m.IOR < 1 || m.IOR > 3) {
		return fmt.Errorf("nt3d: IOR must be 0 or 1..3")
	}
	if m.OcclusionStrength < 0 || m.OcclusionStrength > 1 {
		return fmt.Errorf("nt3d: occlusion strength must be 0..1")
	}
	if m.ORMMap != nil && (!m.ORMMap.valid() || !m.ORMMap.data || !hasUV) {
		return fmt.Errorf("nt3d: ORM map requires NewORMMap and vertex UVs")
	}
	if m.EmissiveIntensity < 0 {
		return fmt.Errorf("nt3d: emissive intensity must be nonnegative")
	}
	if m.EmissiveMap != nil && (!m.EmissiveMap.valid() || m.EmissiveMap.data || !hasUV) {
		return fmt.Errorf("nt3d: emissive map requires a color texture and vertex UVs")
	}
	if !validFloat(m.Threshold) || m.Threshold < 0 || m.Threshold > 1 {
		return fmt.Errorf("nt3d: cutout threshold must be 0..1")
	}
	if !validFloat(m.Specular, m.Shininess) || m.Specular < 0 || m.Specular > 1 || m.Shininess != 0 && (m.Shininess < 1 || m.Shininess > 1024) {
		return fmt.Errorf("nt3d: specular must be 0..1 and shininess 1..1024")
	}
	if err := m.Toon.validate(); err != nil {
		return err
	}
	if !validFloat(m.Reflectivity) || m.Reflectivity < 0 || m.Reflectivity > 1 {
		return fmt.Errorf("nt3d: reflectivity must be 0..1")
	}
	if m.Transparent && (m.Cutout || m.EdgeOn) {
		return fmt.Errorf("nt3d: a transparent material cannot be a cutout")
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
	if m.NormalMap != nil && !hasUV {
		return fmt.Errorf("nt3d: normal map requires NewNormalMap and vertex UVs")
	}
	if m.ORMMap != nil && !hasUV {
		return fmt.Errorf("nt3d: ORM map requires NewORMMap and vertex UVs")
	}
	if m.EmissiveMap != nil && !hasUV {
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
		za.Cutout == zb.Cutout &&
		za.FullSize == zb.FullSize &&
		za.NormalMap == zb.NormalMap &&
		za.NormalRepeat == zb.NormalRepeat &&
		za.ORMMap == zb.ORMMap &&
		za.ORMRepeat == zb.ORMRepeat &&
		za.EmissiveMap == zb.EmissiveMap &&
		za.EmissiveRepeat == zb.EmissiveRepeat
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
	if om.NormalRepeat != nm.NormalRepeat || om.Repeat != nm.Repeat {
		return false
	}
	if (om.EdgeOn && om.Texture != nil) != (nm.EdgeOn && nm.Texture != nil) {
		return false
	}
	if (om.Tint || om.Texture == nil) != (nm.Tint || nm.Texture == nil) {
		return false
	}
	if !transparent(&next) && old.Material.threshold() != next.Material.threshold() {
		return false
	}
	oldStrength, newStrength := float32(0), float32(0)
	if om.NormalMap != nil {
		oldStrength = om.NormalStrength
	}
	if nm.NormalMap != nil {
		newStrength = nm.NormalStrength
	}
	if oldStrength != newStrength {
		return false
	}
	if extended(next) {
		if om.FlowMap != nm.FlowMap || (nm.FlowMap != nil && om.FlowRepeat != nm.FlowRepeat) {
			return false
		}
	}
	return true
}

// SetMaterial replaces the material of every indexed triangle mesh in root's
// subtree (including LOD tiers). Pass nil to clear materials back to default
// untextured surface shading.
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
func (s *Scene) SetMaterial(root NodeID, m *Material) error {
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
				if m != nil && m.NormalMap != nil && len(g.Tangents) == 0 {
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
			if m != nil && m.NormalMap != nil && len(nextBase.Tangents) == 0 {
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
		if m != nil && m.NormalMap != nil && len(g.Tangents) == 0 {
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
