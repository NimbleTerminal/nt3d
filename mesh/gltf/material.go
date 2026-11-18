// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"errors"
	"image/color"

	"github.com/qmuntal/gltf"
	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// gltfLook is a glTF material as read: what nt3d needs of it to make an
// nt3d.Material, before any one primitive's vertex data is known.
type gltfLook struct {
	mapping, normalMapping, ormMapping, emissiveMapping textureMapping
	err                                                 error
	normal                                              *nt3d.Texture
	normalStrength                                      float32
	normalRepeat                                        bool
	orm                                                 *nt3d.Texture
	ormRepeat                                           bool
	occlusionStrength                                   float32
	emissive                                            color.RGBA
	emissiveIntensity                                   float32
	emissiveMap                                         *nt3d.Texture
	emissiveRepeat                                      bool
	name                                                string
	factor                                              [4]float64 // Linear base color and alpha.
	texture                                             *nt3d.Texture
	repeat                                              bool
	alpha                                               gltf.AlphaMode
	cutoff                                              float64
	double                                              bool
	unlit                                               bool
	pbr                                                 bool
	metallic, roughness, ior                            float32
	clearcoat, clearcoatRoughness                       float32
	sheenColor                                          color.RGBA
	sheenRoughness                                      float32
	specularColor                                       color.RGBA
	specularIntensity                                   float32
	transmission, refraction                            float32
	absorption                                          nt3dmath.Vec3
	plain                                               bool // No material: Options.Color paints it.
}

// materialBuild reads one glTF material into a gltfLook. The core material is
// read first (base), then each extension the document may use, in the order
// materialExtensions lists them, and last what an extension can scale
// (emissive).
type materialBuild struct {
	d *gltfDecoder
	m *gltf.Material
	l *gltfLook
	// What one extension reads and another, or the core, uses.
	ior              float64 // KHR_materials_ior, for KHR_materials_transmission.
	emissiveStrength float64 // KHR_materials_emissive_strength, for emissive.
}

// fail records the first thing wrong with the material.
func (mb *materialBuild) fail(err error) {
	if err != nil && mb.l.err == nil {
		mb.l.err = err
	}
}

func (d *gltfDecoder) look(index *int) gltfLook {
	l := gltfLook{factor: [4]float64{1, 1, 1, 1}, cutoff: .5, plain: index == nil}
	if index == nil || *index < 0 || *index >= len(d.doc.Materials) {
		if index != nil {
			d.warn("material %d does not exist", *index)
			l.plain = true
		}
		return l
	}
	mb := &materialBuild{d: d, m: d.doc.Materials[*index], l: &l, ior: 1.5, emissiveStrength: 1}
	mb.base()
	for _, e := range materialExtensions {
		if e.read != nil {
			e.read(mb)
		}
	}
	mb.emissive()
	mb.fail(d.budgetErr)
	return l
}

// base reads the core material: its name and alpha mode, base color and
// texture, metallic-roughness and occlusion, and normal map.
func (mb *materialBuild) base() {
	l, m, d := mb.l, mb.m, mb.d
	l.name, l.alpha, l.cutoff, l.double = m.Name, m.AlphaMode, m.AlphaCutoffOrDefault(), m.DoubleSided
	_, l.unlit = m.Extensions["KHR_materials_unlit"]
	if !l.unlit {
		l.pbr = true
		l.metallic, l.roughness = 1, 1
	}
	mrIdx, occIdx := -1, -1
	haveORMMap := false
	if p := m.PBRMetallicRoughness; p != nil {
		l.factor = p.BaseColorFactorOrDefault()
		if !l.unlit {
			mf, rf := p.MetallicFactorOrDefault(), p.RoughnessFactorOrDefault()
			if !validCoordinate(float32(mf), float32(rf)) {
				mb.fail(errors.New("nt3dmesh: non-finite glTF PBR factor"))
			}
			l.metallic = float32(max(0, min(1, mf)))
			l.roughness = float32(max(0, min(1, rf)))
			if mr := p.MetallicRoughnessTexture; mr != nil {
				mrIdx = mr.Index
				mMap, err := readMapping(mr.TexCoord, mr.Extensions)
				mb.fail(err)
				l.ormMapping, haveORMMap = mMap, true
			}
		}
		if t := p.BaseColorTexture; t != nil {
			l.texture, l.repeat = d.texture(t.Index)
			mMap, err := readMapping(t.TexCoord, t.Extensions)
			mb.fail(err)
			l.mapping = mMap
		}
	}
	if !l.unlit {
		if occ := m.OcclusionTexture; occ != nil && occ.Index != nil {
			s := occ.StrengthOrDefault()
			if !validCoordinate(float32(s)) {
				mb.fail(errors.New("nt3dmesh: non-finite glTF occlusion strength"))
			} else if s > 0 {
				occIdx = *occ.Index
				l.occlusionStrength = float32(min(1, s))
				oMap, err := readMapping(occ.TexCoord, occ.Extensions)
				mb.fail(err)
				if !haveORMMap {
					l.ormMapping, haveORMMap = oMap, true
				} else if oMap != l.ormMapping {
					d.warn("occlusion and metallic-roughness textures use distinct UV transforms")
				}
			}
		}
		if mrIdx >= 0 || occIdx >= 0 {
			l.orm, l.ormRepeat = d.ormTexture(mrIdx, occIdx)
		}
	}
	if n := m.NormalTexture; n != nil && n.Index != nil && !l.unlit {
		l.normal, l.normalRepeat = d.textureKind(*n.Index, true)
		l.normalStrength = float32(n.ScaleOrDefault())
		nMap, err := readMapping(n.TexCoord, n.Extensions)
		mb.fail(err)
		l.normalMapping = nMap
	}

	if !validCoordinate(float32(m.EmissiveFactor[0]), float32(m.EmissiveFactor[1]), float32(m.EmissiveFactor[2])) {
		mb.fail(errors.New("nt3dmesh: non-finite glTF emissive factor"))
	}
}

// emissive reads the emissive color, strength, and texture.
func (mb *materialBuild) emissive() {
	l, m, d := mb.l, mb.m, mb.d
	emStrength := mb.emissiveStrength
	ef := [3]float64{max(0, m.EmissiveFactor[0]), max(0, m.EmissiveFactor[1]), max(0, m.EmissiveFactor[2])}
	if maxCh := max(ef[0], max(ef[1], ef[2])); maxCh > 0 && emStrength > 0 {
		l.emissive = color.RGBA{srgb8(ef[0] / maxCh), srgb8(ef[1] / maxCh), srgb8(ef[2] / maxCh), 255}
		l.emissiveIntensity = float32(maxCh * emStrength)
		if m.EmissiveTexture != nil {
			l.emissiveMap, l.emissiveRepeat = d.texture(m.EmissiveTexture.Index)
			eMap, err := readMapping(m.EmissiveTexture.TexCoord, m.EmissiveTexture.Extensions)
			mb.fail(err)
			l.emissiveMapping = eMap
		}
	}
}
