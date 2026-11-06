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
