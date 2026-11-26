// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"errors"
	"fmt"
	"image/color"
	"math"

	"github.com/qmuntal/gltf"
	"nimbleterminal.dev/nt3d"

	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

type punctualLight struct {
	name  string
	light nt3d.Light
}

func (d *gltfDecoder) decodeLights() error {
	var rootExt struct {
		Lights []struct {
			Name      string    `json:"name"`
			Type      string    `json:"type"`
			Color     []float64 `json:"color"`
			Intensity *float64  `json:"intensity"`
			Range     *float64  `json:"range"`
			Spot      *struct {
				InnerConeAngle *float64 `json:"innerConeAngle"`
				OuterConeAngle *float64 `json:"outerConeAngle"`
			} `json:"spot"`
		} `json:"lights"`
	}
	ok, err := decodeExtension(d.doc.Extensions, "KHR_lights_punctual", &rootExt)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	d.lights = make([]punctualLight, 0, len(rootExt.Lights))
	for i, src := range rootExt.Lights {
		var out nt3d.Light
		switch src.Type {
		case "directional":
			out.Kind = nt3d.DirectionalLight
		case "point":
			out.Kind = nt3d.PointLight
		case "spot":
			out.Kind = nt3d.SpotLight
		default:
			return fmt.Errorf("nt3dmesh: KHR_lights_punctual light %d has unknown type %q", i, src.Type)
		}

		rgb := [3]float64{1, 1, 1}
		if src.Color != nil {
			if len(src.Color) != 3 {
				return errors.New("nt3dmesh: KHR_lights_punctual color needs 3 values")
			}
			for k, c := range src.Color {
				if !nt3dmesh.ValidCoordinate(float32(c)) || c < 0 || c > 1 {
					return errors.New("nt3dmesh: invalid KHR_lights_punctual color")
				}
				rgb[k] = c
			}
		}
		out.Color = color.RGBA{srgb8(rgb[0]), srgb8(rgb[1]), srgb8(rgb[2]), 255}

		intensity := 1.0
		if src.Intensity != nil {
			intensity = *src.Intensity
			if !nt3dmesh.ValidCoordinate(float32(intensity)) || intensity < 0 {
				return errors.New("nt3dmesh: invalid KHR_lights_punctual intensity")
			}
		}
		// A zero Intensity would mean the default, so a light of none is off.
		out.Intensity, out.Off = float32(intensity), intensity == 0

		if src.Range != nil {
			r := *src.Range
			if !nt3dmesh.ValidCoordinate(float32(r)) || r <= 0 {
				return errors.New("nt3dmesh: invalid KHR_lights_punctual range")
			}
			if out.Kind != nt3d.DirectionalLight {
				out.Range = float32(r)
			}
		}

		if out.Kind == nt3d.SpotLight {
			inner := 0.0
			outer := math.Pi / 4
			if src.Spot != nil {
				if src.Spot.InnerConeAngle != nil {
					inner = *src.Spot.InnerConeAngle
				}
				if src.Spot.OuterConeAngle != nil {
					outer = *src.Spot.OuterConeAngle
				}
			}
			if !nt3dmesh.ValidCoordinate(float32(inner)) || !nt3dmesh.ValidCoordinate(float32(outer)) || inner < 0 || outer <= 0 || outer >= math.Pi/2 || inner >= outer {
				return errors.New("nt3dmesh: invalid KHR_lights_punctual spot cone")
			}
			// The file holds radians.
			out.InnerAngle = float32(inner * 180 / math.Pi)
			out.OuterAngle = float32(outer * 180 / math.Pi)
		}

		d.lights = append(d.lights, punctualLight{name: src.Name, light: out})
	}
	return nil
}

func (d *gltfDecoder) nodeLight(ext gltf.Extensions) (*nt3d.Light, string, error) {
	var nodeExt struct {
		Light *int `json:"light"`
	}
	ok, err := decodeExtension(ext, "KHR_lights_punctual", &nodeExt)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return nil, "", nil
	}
	if nodeExt.Light == nil || *nodeExt.Light < 0 || *nodeExt.Light >= len(d.lights) {
		return nil, "", errors.New("nt3dmesh: invalid KHR_lights_punctual light index")
	}
	entry := d.lights[*nodeExt.Light]
	l := entry.light
	return &l, entry.name, nil
}
