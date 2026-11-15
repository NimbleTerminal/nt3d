// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/qmuntal/gltf"

	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

type variantEntry struct {
	variant  int
	material int
}

// decodeVariants parses and validates document-level and primitive-level
// KHR_materials_variants extensions across the glTF document.
func (d *gltfDecoder) decodeVariants() error {
	var rootExt struct {
		Variants []struct {
			Name string `json:"name"`
		} `json:"variants"`
	}
	ok, err := decodeExtension(d.doc.Extensions, "KHR_materials_variants", &rootExt)
	if err != nil {
		return err
	}
	if ok {
		if len(rootExt.Variants) == 0 {
			return errors.New("nt3dmesh: KHR_materials_variants variants cannot be empty")
		}
		if len(rootExt.Variants) > maxVisits {
			return nt3dmesh.ErrTooLarge
		}
		seen := make(map[string]bool, len(rootExt.Variants))
		d.variantNames = make([]string, len(rootExt.Variants))
		for i, v := range rootExt.Variants {
			if v.Name == "" {
				return errors.New("nt3dmesh: KHR_materials_variants variant name cannot be empty")
			}
			if seen[v.Name] {
				return fmt.Errorf("nt3dmesh: duplicate KHR_materials_variants name %q", v.Name)
			}
			seen[v.Name] = true
			d.variantNames[i] = v.Name
		}
	}
	for _, mesh := range d.doc.Meshes {
		if mesh == nil {
			continue
		}
		for _, p := range mesh.Primitives {
			if p == nil {
				continue
			}
			entries, err := d.primitiveVariants(p)
			if err != nil {
				return err
			}
			for _, e := range entries {
				if l := d.look(&e.material); l.err != nil {
					return l.err
				}
			}
		}
	}
	return nil
}

func (d *gltfDecoder) primitiveVariants(p *gltf.Primitive) ([]variantEntry, error) {
	var primExt struct {
		Mappings []struct {
			Material *int  `json:"material"`
			Variants []int `json:"variants"`
		} `json:"mappings"`
	}
	ok, err := decodeExtension(p.Extensions, "KHR_materials_variants", &primExt)
	if err != nil || !ok {
		return nil, err
	}
	if len(d.variantNames) == 0 {
		return nil, errors.New("nt3dmesh: primitive KHR_materials_variants requires document KHR_materials_variants")
	}
	if len(primExt.Mappings) == 0 {
		return nil, errors.New("nt3dmesh: KHR_materials_variants mappings cannot be empty")
	}
	seen := make(map[int]bool, len(primExt.Mappings))
	var entries []variantEntry
	for _, m := range primExt.Mappings {
		if m.Material == nil || *m.Material < 0 || *m.Material >= len(d.doc.Materials) || d.doc.Materials[*m.Material] == nil {
			return nil, errors.New("nt3dmesh: KHR_materials_variants names invalid material")
		}
		if len(m.Variants) == 0 {
			return nil, errors.New("nt3dmesh: KHR_materials_variants mapping has no variants")
		}
		for _, vi := range m.Variants {
			if vi < 0 || vi >= len(d.variantNames) {
				return nil, fmt.Errorf("nt3dmesh: KHR_materials_variants variant %d out of range", vi)
			}
			if seen[vi] {
				return nil, fmt.Errorf("nt3dmesh: duplicate KHR_materials_variants variant %d on primitive", vi)
			}
			seen[vi] = true
			entries = append(entries, variantEntry{variant: vi, material: *m.Material})
		}
	}
	slices.SortFunc(entries, func(a, b variantEntry) int { return cmp.Compare(a.variant, b.variant) })
	return entries, nil
}
