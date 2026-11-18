// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"errors"
	"image/color"
	"math"

	"github.com/qmuntal/gltf"
	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

// A materialExtension is one glTF material extension: its name, and how it
// changes the material being read. To honor another, write its read function
// here and add a row; the row also declares it supported.
type materialExtension struct {
	name string
	read func(*materialBuild) // Nil when the extension is read with the core material.
}

// materialExtensions are read in this order, which matters where one uses
// what another read: transmission uses the index of refraction, and volume
// the transmission.
var materialExtensions = []materialExtension{
	{"KHR_materials_unlit", nil}, // Its presence alone decides whether the material is lit: see base.
	{"KHR_materials_emissive_strength", (*materialBuild).emissiveStrengthExtension},
	{"KHR_materials_ior", (*materialBuild).iorExtension},
	{"KHR_materials_transmission", (*materialBuild).transmissionExtension},
	{"KHR_materials_volume", (*materialBuild).volumeExtension},
	{"KHR_materials_clearcoat", (*materialBuild).clearcoatExtension},
	{"KHR_materials_sheen", (*materialBuild).sheenExtension},
	{"KHR_materials_specular", (*materialBuild).specularExtension},
}

// KHR_materials_emissive_strength scales the emissive color past 1.
func (mb *materialBuild) emissiveStrengthExtension() {
	m := mb.m
	var emExt struct {
		EmissiveStrength *float64 `json:"emissiveStrength"`
	}
	if ok, err := decodeExtension(m.Extensions, "KHR_materials_emissive_strength", &emExt); err != nil {
		mb.fail(err)
	} else if ok && emExt.EmissiveStrength != nil {
		s := *emExt.EmissiveStrength
		if !validCoordinate(float32(s)) || s < 0 {
			mb.fail(errors.New("nt3dmesh: invalid KHR_materials_emissive_strength"))
		} else {
			mb.emissiveStrength = s
		}
	}
}

// KHR_materials_ior sets the index of refraction.
func (mb *materialBuild) iorExtension() {
	l, m := mb.l, mb.m
	var iorExt struct {
		IOR *float64 `json:"ior"`
	}
	if ok, err := decodeExtension(m.Extensions, "KHR_materials_ior", &iorExt); err != nil {
		mb.fail(err)
	} else if ok && iorExt.IOR != nil {
		v := *iorExt.IOR
		if !validCoordinate(float32(v)) || v < 0 {
			mb.fail(errors.New("nt3dmesh: invalid KHR_materials_ior"))
		} else if !l.unlit {
			if v == 0 {
				mb.ior = 1
			} else {
				mb.ior = max(1, min(3, v))
			}
			l.ior = float32(mb.ior)
		}
	}
}

// KHR_materials_transmission lets light through an opaque, lit surface.
func (mb *materialBuild) transmissionExtension() {
	l, m := mb.l, mb.m
	var transExt struct {
		TransmissionFactor *float64 `json:"transmissionFactor"`
	}
	if ok, err := decodeExtension(m.Extensions, "KHR_materials_transmission", &transExt); err != nil {
		mb.fail(err)
	} else if ok && transExt.TransmissionFactor != nil {
		tf := *transExt.TransmissionFactor
		if !validCoordinate(float32(tf)) || tf < 0 || tf > 1 {
			mb.fail(errors.New("nt3dmesh: invalid KHR_materials_transmission"))
		} else if !l.unlit && l.alpha == gltf.AlphaOpaque && tf > 0 {
			l.transmission = float32(tf)
			l.refraction = float32(min(.25, max(0, (mb.ior-1)*.06)))
		}
	}
}

// KHR_materials_volume gives a transmitting surface thickness and absorption.
func (mb *materialBuild) volumeExtension() {
	l, m := mb.l, mb.m
	var volExt struct {
		ThicknessFactor     *float64  `json:"thicknessFactor"`
		AttenuationDistance *float64  `json:"attenuationDistance"`
		AttenuationColor    []float64 `json:"attenuationColor"`
	}
	if ok, err := decodeExtension(m.Extensions, "KHR_materials_volume", &volExt); err != nil {
		mb.fail(err)
	} else if ok {
		thick := 0.0
		if volExt.ThicknessFactor != nil {
			thick = *volExt.ThicknessFactor
			if !validCoordinate(float32(thick)) || thick < 0 {
				mb.fail(errors.New("nt3dmesh: invalid KHR_materials_volume thickness"))
			}
		} else if volExt.AttenuationDistance != nil || volExt.AttenuationColor != nil {
			thick = 1
		}
		if volExt.AttenuationColor != nil && len(volExt.AttenuationColor) != 3 {
			mb.fail(errors.New("nt3dmesh: KHR_materials_volume attenuationColor needs 3 values"))
		}
		if volExt.AttenuationDistance != nil {
			ad := *volExt.AttenuationDistance
			if !validCoordinate(float32(ad)) || ad <= 0 {
				mb.fail(errors.New("nt3dmesh: invalid KHR_materials_volume attenuationDistance"))
			}
		}
		if l.err == nil && !l.unlit && l.transmission > 0 {
			if volExt.ThicknessFactor != nil && thick == 0 {
				l.refraction = 0
			} else if thick > 0 {
				ac := [3]float64{1, 1, 1}
				if len(volExt.AttenuationColor) == 3 {
					for k := range 3 {
						if !validCoordinate(float32(volExt.AttenuationColor[k])) || volExt.AttenuationColor[k] < 0 || volExt.AttenuationColor[k] > 1 {
							mb.fail(errors.New("nt3dmesh: invalid KHR_materials_volume attenuationColor"))
						}
						ac[k] = volExt.AttenuationColor[k]
					}
				}
				dist := math.Inf(1)
				if volExt.AttenuationDistance != nil {
					dist = *volExt.AttenuationDistance
				}
				if l.err == nil && !math.IsInf(dist, 1) {
					d := max(1e-4, dist)
					l.absorption = nt3dmath.Vec3{
						X: float32(min(nt3dmesh.MaxCoordinate, -math.Log(max(1e-4, min(1, ac[0])))/d)),
						Y: float32(min(nt3dmesh.MaxCoordinate, -math.Log(max(1e-4, min(1, ac[1])))/d)),
						Z: float32(min(nt3dmesh.MaxCoordinate, -math.Log(max(1e-4, min(1, ac[2])))/d)),
					}
				}
			}
		}
	}
}

// KHR_materials_clearcoat adds a clear top coat.
func (mb *materialBuild) clearcoatExtension() {
	l, m := mb.l, mb.m
	var ccExt struct {
		ClearcoatFactor          *float64 `json:"clearcoatFactor"`
		ClearcoatRoughnessFactor *float64 `json:"clearcoatRoughnessFactor"`
	}
	if ok, err := decodeExtension(m.Extensions, "KHR_materials_clearcoat", &ccExt); err != nil {
		mb.fail(err)
	} else if ok {
		cf, crf := 0.0, 0.0
		if ccExt.ClearcoatFactor != nil {
			cf = *ccExt.ClearcoatFactor
			if !validCoordinate(float32(cf)) || cf < 0 || cf > 1 {
				mb.fail(errors.New("nt3dmesh: invalid KHR_materials_clearcoat"))
			}
		}
		if ccExt.ClearcoatRoughnessFactor != nil {
			crf = *ccExt.ClearcoatRoughnessFactor
			if !validCoordinate(float32(crf)) || crf < 0 || crf > 1 {
				mb.fail(errors.New("nt3dmesh: invalid KHR_materials_clearcoat"))
			}
		}
		if l.err == nil && !l.unlit {
			l.clearcoat = float32(cf)
			l.clearcoatRoughness = float32(crf)
		}
	}
}

// KHR_materials_sheen adds the grazing sheen of cloth.
func (mb *materialBuild) sheenExtension() {
	l, m := mb.l, mb.m
	var sheenExt struct {
		SheenColorFactor     []float64 `json:"sheenColorFactor"`
		SheenRoughnessFactor *float64  `json:"sheenRoughnessFactor"`
	}
	if ok, err := decodeExtension(m.Extensions, "KHR_materials_sheen", &sheenExt); err != nil {
		mb.fail(err)
	} else if ok {
		if sheenExt.SheenColorFactor != nil && len(sheenExt.SheenColorFactor) != 3 {
			mb.fail(errors.New("nt3dmesh: KHR_materials_sheen sheenColorFactor needs 3 values"))
		}
		var sc [3]float64
		if len(sheenExt.SheenColorFactor) == 3 {
			for k := range 3 {
				v := sheenExt.SheenColorFactor[k]
				if !validCoordinate(float32(v)) || v < 0 || v > 1 {
					mb.fail(errors.New("nt3dmesh: invalid KHR_materials_sheen"))
				}
				sc[k] = v
			}
		}
		srf := 0.0
		if sheenExt.SheenRoughnessFactor != nil {
			srf = *sheenExt.SheenRoughnessFactor
			if !validCoordinate(float32(srf)) || srf < 0 || srf > 1 {
				mb.fail(errors.New("nt3dmesh: invalid KHR_materials_sheen"))
			}
		}
		if l.err == nil && !l.unlit {
			r, g, b := srgb8(sc[0]), srgb8(sc[1]), srgb8(sc[2])
			if r > 0 || g > 0 || b > 0 {
				l.sheenColor = color.RGBA{r, g, b, 255}
			}
			l.sheenRoughness = float32(srf)
		}
	}
}

// KHR_materials_specular scales and tints the dielectric specular reflection.
func (mb *materialBuild) specularExtension() {
	l, m, d := mb.l, mb.m, mb.d
	type specTexRef struct {
		Index      *int            `json:"index"`
		TexCoord   int             `json:"texCoord"`
		Extensions gltf.Extensions `json:"extensions"`
	}
	var specExt struct {
		SpecularFactor       *float64    `json:"specularFactor"`
		SpecularTexture      *specTexRef `json:"specularTexture"`
		SpecularColorFactor  []float64   `json:"specularColorFactor"`
		SpecularColorTexture *specTexRef `json:"specularColorTexture"`
	}
	if ok, err := decodeExtension(m.Extensions, "KHR_materials_specular", &specExt); err != nil {
		mb.fail(err)
	} else if ok {
		sf := 1.0
		if specExt.SpecularFactor != nil {
			sf = *specExt.SpecularFactor
			if !validCoordinate(float32(sf)) || sf < 0 || sf > 1 {
				mb.fail(errors.New("nt3dmesh: invalid KHR_materials_specular"))
			}
		}
		if specExt.SpecularColorFactor != nil && len(specExt.SpecularColorFactor) != 3 {
			mb.fail(errors.New("nt3dmesh: KHR_materials_specular specularColorFactor needs 3 values"))
		}
		scf := [3]float64{1, 1, 1}
		if len(specExt.SpecularColorFactor) == 3 {
			for k := range 3 {
				v := specExt.SpecularColorFactor[k]
				if !validCoordinate(float32(v)) || v < 0 {
					mb.fail(errors.New("nt3dmesh: invalid KHR_materials_specular"))
				}
				scf[k] = v
			}
		}
		for _, ref := range []*specTexRef{specExt.SpecularTexture, specExt.SpecularColorTexture} {
			if ref == nil {
				continue
			}
			if ref.Index == nil || *ref.Index < 0 || *ref.Index >= len(d.doc.Textures) || d.doc.Textures[*ref.Index] == nil {
				mb.fail(errors.New("nt3dmesh: invalid KHR_materials_specular texture"))
				continue
			}
			_, err := readMapping(ref.TexCoord, ref.Extensions)
			mb.fail(err)
		}
		if l.err == nil && !l.unlit {
			// The factor scales the whole highlight, the color only its
			// strength head on. A zero strength would mean the default, so
			// none is Off.
			var style nt3d.SpecularStyle
			switch {
			case sf == 0:
				style.Off = true
			case sf < 1:
				style.Strength = float32(sf)
			}
			maxCh := max(scf[0], max(scf[1], scf[2]))
			switch {
			case maxCh == 0:
				style.Color = color.RGBA{A: 255}
			case maxCh > 1:
				style.Color = color.RGBA{R: srgb8(scf[0] / maxCh), G: srgb8(scf[1] / maxCh), B: srgb8(scf[2] / maxCh), A: 255}
				style.Intensity = float32(maxCh)
			default:
				if c := (color.RGBA{R: srgb8(scf[0]), G: srgb8(scf[1]), B: srgb8(scf[2]), A: 255}); c != (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
					style.Color = c
				}
			}
			l.specular = style
		}
	}
}
