// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/qmuntal/gltf"
)

// A glTF extension is honored in one of two places. A material extension is a
// row in materialExtensions with the function that reads it. Any other is a
// row here naming the file that honors it. Both declare the extension
// supported: a document that requires one not listed is refused, and one that
// merely uses it gets a warning. A test holds every extension name the
// package mentions to these two tables.

// documentExtensions are the supported extensions that are not read per
// material.
var documentExtensions = []string{
	"KHR_texture_transform",   // texture_transform.go: a texture's offset, rotation, and scale.
	"KHR_lights_punctual",     // lights.go: directional, point, and spot lights on nodes.
	"KHR_materials_variants",  // variants.go: alternative materials for a primitive.
	"KHR_mesh_quantization",   // quantization.go: integer vertex attributes.
	"KHR_texture_basisu",      // ktx2.go: KTX 2.0 textures, BasisLZ and UASTC.
	"EXT_meshopt_compression", // meshopt.go: compressed buffer views.
	"EXT_texture_webp",        // gltf.go, loadImage: WebP textures, when a WebP decoder is registered.
	"EXT_mesh_gpu_instancing", // instancing.go: many placements of one mesh.
}

// supportedExtensions are those whose meaning Decode honors.
var supportedExtensions = func() []string {
	names := make([]string, 0, len(materialExtensions)+len(documentExtensions))
	for _, e := range materialExtensions {
		names = append(names, e.name)
	}
	return append(names, documentExtensions...)
}()

// ignoredExtensions are passed over silently: they refine looks nt3d does not draw.
var ignoredExtensions []string

// extensions refuses a document that requires an extension Decode does not
// honor, and warns of those it uses that are passed over.
func (d *gltfDecoder) extensions() error {
	for _, e := range d.doc.ExtensionsRequired {
		if !slices.Contains(supportedExtensions, e) {
			return fmt.Errorf("nt3dmesh: glTF requires unsupported extension %s", e)
		}
	}
	for _, e := range d.doc.ExtensionsUsed {
		if !slices.Contains(supportedExtensions, e) && !slices.Contains(ignoredExtensions, e) && !slices.Contains(d.doc.ExtensionsRequired, e) {
			d.warn("extension %s is ignored", e)
		}
	}
	return nil
}

// decodeExtension reads the named extension's JSON into dst, and reports
// whether the object carried it.
func decodeExtension(extensions gltf.Extensions, name string, dst any) (bool, error) {
	raw, ok := extensions[name]
	if !ok || raw == nil {
		return false, nil
	}
	var data []byte
	switch v := raw.(type) {
	case json.RawMessage:
		data = v
	case []byte:
		data = v
	default:
		var err error
		if data, err = json.Marshal(raw); err != nil {
			return false, fmt.Errorf("nt3dmesh: %s: %w", name, err)
		}
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return false, fmt.Errorf("nt3dmesh: %s: %w", name, err)
	}
	return true, nil
}
