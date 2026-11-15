// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"bytes"
	"testing"

	"github.com/qmuntal/gltf"
)

// FuzzDecode feeds glTF JSON and GLB containers to the decoder. Buffers,
// images, and other side files the document names resolve to the second
// input, so accessor bounds are tested against arbitrary buffer contents.
func FuzzDecode(f *testing.F) {
	// Seeds stay small: the fuzzer minimizes each one before fuzzing, and a
	// fixture the size of testdata/animated.gltf would eat a whole short round.
	for _, doc := range []*gltf.Document{
		gltfDoc(upTriangle),
		gltfDoc(prim{positions: [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {1, 1, 0}}, indices: []uint16{0, 1, 2, 2, 1, 3}, normals: [][3]float32{{0, 0, 1}, {0, 0, 1}, {0, 0, 1}, {0, 0, 1}}, uv: [][2]float32{{0, 0}, {1, 0}, {0, 1}, {1, 1}}}),
	} {
		var buf bytes.Buffer
		if err := gltf.NewEncoder(&buf).Encode(doc); err != nil {
			f.Fatal(err)
		}
		f.Add(buf.Bytes(), []byte{})
	}
	f.Add([]byte(`{"asset":{"version":"2.0"},"buffers":[{"uri":"side.bin","byteLength":36}],"bufferViews":[{"buffer":0,"byteLength":36}],"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}],"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}],"nodes":[{"mesh":0}],"scenes":[{"nodes":[0]}],"scene":0}`), bytes.Repeat([]byte{0, 0, 128, 63}, 9))
	f.Add([]byte(`{"asset":{"version":"2.0"},"extensionsUsed":["EXT_mesh_gpu_instancing"],"buffers":[{"uri":"side.bin","byteLength":60}],"bufferViews":[{"buffer":0,"byteLength":36},{"buffer":0,"byteOffset":36,"byteLength":24}],"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"},{"bufferView":1,"componentType":5126,"count":2,"type":"VEC3"}],"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}],"nodes":[{"mesh":0,"extensions":{"EXT_mesh_gpu_instancing":{"attributes":{"TRANSLATION":1}}}}],"scenes":[{"nodes":[0]}],"scene":0}`), bytes.Repeat([]byte{0, 0, 128, 63}, 15))
	f.Add([]byte(`{"asset":{"version":"2.0"},"extensionsUsed":["EXT_texture_webp"],"buffers":[{"uri":"side.bin","byteLength":36}],"bufferViews":[{"buffer":0,"byteLength":36}],"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}],"images":[{"uri":"tex.webp","mimeType":"image/webp"}],"textures":[{"extensions":{"EXT_texture_webp":{"source":0}}}],"materials":[{"pbrMetallicRoughness":{"baseColorTexture":{"index":0}}}],"meshes":[{"primitives":[{"attributes":{"POSITION":0},"material":0}]}],"nodes":[{"mesh":0}],"scenes":[{"nodes":[0]}],"scene":0}`), bytes.Repeat([]byte{0, 0, 128, 63}, 9))
	f.Add([]byte(`{"asset":{"version":"2.0"},"extensionsUsed":["KHR_materials_variants"],"extensionsRequired":["KHR_materials_variants"],"extensions":{"KHR_materials_variants":{"variants":[{"name":"Taxi"},{"name":"Police"}]}},"buffers":[{"uri":"side.bin","byteLength":36}],"bufferViews":[{"buffer":0,"byteLength":36}],"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}],"materials":[{"pbrMetallicRoughness":{"baseColorFactor":[1,1,1,1]}},{"pbrMetallicRoughness":{"baseColorFactor":[1,0.8,0,1]}},{"alphaMode":"BLEND","pbrMetallicRoughness":{"baseColorFactor":[0,0.3,1,0.5]}}],"meshes":[{"primitives":[{"attributes":{"POSITION":0},"material":0,"extensions":{"KHR_materials_variants":{"mappings":[{"material":1,"variants":[0]},{"material":2,"variants":[1]}]}}}]}],"nodes":[{"mesh":0}],"scenes":[{"nodes":[0]}],"scene":0}`), bytes.Repeat([]byte{0, 0, 128, 63}, 9))
	// KHR_mesh_quantization seed: SHORT positions + BYTE octahedral-style normals + USHORT UVs.
	f.Add([]byte(`{"asset":{"version":"2.0"},"extensionsUsed":["KHR_mesh_quantization"],"extensionsRequired":["KHR_mesh_quantization"],"buffers":[{"uri":"side.bin","byteLength":48}],"bufferViews":[{"buffer":0,"byteLength":24,"byteStride":8},{"buffer":0,"byteOffset":24,"byteLength":12,"byteStride":4},{"buffer":0,"byteOffset":36,"byteLength":12,"byteStride":4}],"accessors":[{"bufferView":0,"componentType":5122,"count":3,"type":"VEC3"},{"bufferView":1,"componentType":5120,"normalized":true,"count":3,"type":"VEC3"},{"bufferView":2,"componentType":5123,"normalized":true,"count":3,"type":"VEC2"}],"meshes":[{"primitives":[{"attributes":{"POSITION":0,"NORMAL":1,"TEXCOORD_0":2}}]}],"nodes":[{"mesh":0}],"scenes":[{"nodes":[0]}],"scene":0}`), bytes.Repeat([]byte{1, 0, 2, 0, 3, 0, 127, 0}, 6))
	// EXT_meshopt_compression seeds: ATTRIBUTES + OCTAHEDRAL + TRIANGLES + fallback buffer resolved from fuzzed side.bin.
	meshoptPos := encodeMeshoptAttributesTest([]byte{
		0, 0, 0, 0, 0, 0, 0, 0,
		100, 0, 0, 0, 0, 0, 0, 0,
		0, 100, 0, 0, 0, 0, 0, 0,
	}, 3, 8)
	meshoptNrm := encodeMeshoptAttributesTest([]byte{
		0, 0, 127, 0,
		0, 0, 127, 0,
		0, 0, 127, 0,
	}, 3, 4)
	meshoptTri := append([]byte{0xe1, 0x20, 0, 0, 1, 2}, make([]byte, 16)...)
	meshoptSide := slicesConcat(meshoptPos, meshoptNrm, meshoptTri)
	f.Add([]byte(`{"asset":{"version":"2.0"},"extensionsUsed":["KHR_mesh_quantization","EXT_meshopt_compression"],"extensionsRequired":["KHR_mesh_quantization","EXT_meshopt_compression"],"buffers":[{"uri":"side.bin","byteLength":112},{"byteLength":42,"extensions":{"EXT_meshopt_compression":{"fallback":true}}}],"bufferViews":[{"buffer":1,"byteLength":24,"byteStride":8,"extensions":{"EXT_meshopt_compression":{"buffer":0,"byteOffset":0,"byteLength":35,"byteStride":8,"count":3,"mode":"ATTRIBUTES"}}},{"buffer":1,"byteLength":12,"byteStride":4,"extensions":{"EXT_meshopt_compression":{"buffer":0,"byteOffset":35,"byteLength":34,"byteStride":4,"count":3,"mode":"ATTRIBUTES","filter":"OCTAHEDRAL"}}},{"buffer":1,"byteLength":6,"extensions":{"EXT_meshopt_compression":{"buffer":0,"byteOffset":69,"byteLength":22,"byteStride":2,"count":3,"mode":"TRIANGLES"}}}],"accessors":[{"bufferView":0,"componentType":5122,"count":3,"type":"VEC3"},{"bufferView":1,"componentType":5120,"normalized":true,"count":3,"type":"VEC3"},{"bufferView":2,"componentType":5123,"count":3,"type":"SCALAR"}],"meshes":[{"primitives":[{"attributes":{"POSITION":0,"NORMAL":1},"indices":2}]}],"nodes":[{"mesh":0}],"scenes":[{"nodes":[0]}],"scene":0}`), append(meshoptSide, make([]byte, 32)...))
	f.Add([]byte(`{"asset":{"version":"2.0"},"nodes":[{"children":[0]}],"scenes":[{"nodes":[0]}],"scene":0}`), []byte{})
	f.Add([]byte("glTF\x02\x00\x00\x00\x14\x00\x00\x00"), []byte{})
	f.Fuzz(func(t *testing.T, data, side []byte) {
		o := fuzzOptions()
		o.Open = func(string) ([]byte, error) { return side, nil }
		m, err := Decode(data, o)
		checkModel(t, m, err, o)
		if err == nil && m.Asset == nil {
			t.Fatal("glTF without an asset")
		}
	})
}
