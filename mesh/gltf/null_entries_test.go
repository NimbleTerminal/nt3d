// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"encoding/json"
	"strings"
	"testing"
)

// nullEntryTriangle is one triangle in an embedded buffer, as the parts of a
// glTF document a test replaces or adds to.
func nullEntryTriangle() map[string]any {
	return map[string]any{
		"asset":  map[string]any{"version": "2.0"},
		"scene":  0,
		"scenes": []any{map[string]any{"nodes": []any{0}}},
		"nodes":  []any{map[string]any{"mesh": 0}},
		"meshes": []any{map[string]any{"primitives": []any{map[string]any{"attributes": map[string]any{"POSITION": 0}}}}},
		"accessors": []any{map[string]any{
			"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3",
			"min": []any{0, 0, 0}, "max": []any{1, 1, 0},
		}},
		"bufferViews": []any{map[string]any{"buffer": 0, "byteLength": 36}},
		"buffers": []any{map[string]any{
			"byteLength": 36,
			"uri":        "data:application/octet-stream;base64,AAAAAAAAAAAAAAAAAACAPwAAAAAAAAAAAAAAAAAAgD8AAAAA",
		}},
	}
}

// A null where the schema wants an object is refused by name, wherever it is:
// every array of objects in a document decodes to pointers, and a null one
// would otherwise be dereferenced by whichever part of the decoder met it.
func TestGLTFRefusesNullEntries(t *testing.T) {
	animation := func(channels, samplers []any) []any {
		return []any{map[string]any{"channels": channels, "samplers": samplers}}
	}
	sampler := map[string]any{"input": 0, "output": 0}
	channel := map[string]any{"sampler": 0, "target": map[string]any{"node": 0, "path": "translation"}}
	for name, tc := range map[string]struct {
		key   string
		value any
		want  string
	}{
		"accessors":          {"accessors", []any{nil}, "accessors[0]"},
		"animations":         {"animations", []any{nil}, "animations[0]"},
		"animation channels": {"animations", animation([]any{nil}, []any{sampler}), "animations[0].channels[0]"},
		"animation samplers": {"animations", animation([]any{channel}, []any{nil}), "animations[0].samplers[0]"},
		"buffers":            {"buffers", []any{nil}, "buffers[0]"},
		"bufferViews":        {"bufferViews", []any{nil}, "bufferViews[0]"},
		"cameras":            {"cameras", []any{nil}, "cameras[0]"},
		"images":             {"images", []any{nil}, "images[0]"},
		"materials":          {"materials", []any{nil}, "materials[0]"},
		"meshes":             {"meshes", []any{nil}, "meshes[0]"},
		"mesh primitives":    {"meshes", []any{map[string]any{"primitives": []any{nil}}}, "meshes[0].primitives[0]"},
		"nodes":              {"nodes", []any{nil}, "nodes[0]"},
		"samplers":           {"samplers", []any{nil}, "samplers[0]"},
		"scenes":             {"scenes", []any{nil}, "scenes[0]"},
		"skins":              {"skins", []any{nil}, "skins[0]"},
		"textures":           {"textures", []any{nil}, "textures[0]"},
		"later entry":        {"materials", []any{map[string]any{}, nil}, "materials[1]"},
	} {
		t.Run(name, func(t *testing.T) {
			doc := nullEntryTriangle()
			doc[tc.key] = tc.value
			data, err := json.Marshal(doc)
			check(t, err)
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Decode panicked: %v", r)
				}
			}()
			_, err = Decode(data, nil)
			if err == nil {
				t.Fatal("Decode accepted a null entry")
			}
			if !strings.Contains(err.Error(), tc.want+" is null") {
				t.Fatalf("error %q does not name %s", err, tc.want)
			}
		})
	}
}

// The triangle the null cases start from decodes, so each case fails for its
// null alone.
func TestGLTFNullEntryBaseDecodes(t *testing.T) {
	data, err := json.Marshal(nullEntryTriangle())
	check(t, err)
	m, err := Decode(data, nil)
	check(t, err)
	if len(m.Objects) != 1 || len(m.Objects[0].Geometry.Indices) != 3 {
		t.Fatalf("base document decoded to %d objects", len(m.Objects))
	}
}
