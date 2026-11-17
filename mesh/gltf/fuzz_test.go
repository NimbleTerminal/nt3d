// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"image"
	"image/color"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/qmuntal/gltf"

	"nimbleterminal.dev/nt3d"
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
	// A null where an object belongs: each array of objects decodes to pointers.
	f.Add([]byte(`{"asset":{"version":"2.0"},"buffers":[{"uri":"side.bin","byteLength":36}],"bufferViews":[{"buffer":0,"byteLength":36}],"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}],"materials":[null],"cameras":[null],"meshes":[{"primitives":[{"attributes":{"POSITION":0},"material":0},null]}],"nodes":[{"mesh":0,"camera":0}],"scenes":[{"nodes":[0]},null],"scene":0}`), bytes.Repeat([]byte{0, 0, 128, 63}, 9))
	// KHR_texture_basisu + KHR_materials_specular seeds (both UASTC and BasisLZ/ETC1S side KTX2 payloads).
	ktxUASTCSeed := append(append([]byte(nil), buildSolidUASTCKTX2Test(color.NRGBA{200, 100, 50, 255})...), bytes.Repeat([]byte{0, 0, 128, 63}, 16)...)
	ktxETC1SSeed := append(append([]byte(nil), buildMinimalETC1SKTX2Test(20, 12, 8)...), bytes.Repeat([]byte{0, 0, 128, 63}, 16)...)
	ktxJSON := []byte(`{"asset":{"version":"2.0"},"extensionsUsed":["KHR_texture_basisu","KHR_materials_specular"],"extensionsRequired":["KHR_texture_basisu","KHR_materials_specular"],"buffers":[{"uri":"side.bin","byteLength":256}],"bufferViews":[{"buffer":0,"byteOffset":200,"byteLength":36},{"buffer":0,"byteOffset":236,"byteLength":12}],"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"},{"bufferView":1,"componentType":5123,"normalized":true,"count":3,"type":"VEC2"}],"images":[{"uri":"tex.ktx2","mimeType":"image/ktx2"}],"textures":[{"extensions":{"KHR_texture_basisu":{"source":0}}}],"materials":[{"pbrMetallicRoughness":{"baseColorTexture":{"index":0}},"extensions":{"KHR_materials_specular":{"specularFactor":0.5,"specularColorFactor":[2,1,0.5]}}}],"meshes":[{"primitives":[{"attributes":{"POSITION":0,"TEXCOORD_0":1},"material":0}]}],"nodes":[{"mesh":0}],"scenes":[{"nodes":[0]}],"scene":0}`)
	f.Add(ktxJSON, ktxUASTCSeed)
	f.Add(ktxJSON, ktxETC1SSeed)
	// Multi-LOD + KHR_materials_variants + KHR_materials_specular seed.
	f.Add([]byte(`{"asset":{"version":"2.0"},"extensionsUsed":["KHR_materials_variants","KHR_materials_specular"],"extensionsRequired":["KHR_materials_variants","KHR_materials_specular"],"extensions":{"KHR_materials_variants":{"variants":[{"name":"Taxi"},{"name":"Matte"}]}},"buffers":[{"uri":"side.bin","byteLength":36}],"bufferViews":[{"buffer":0,"byteLength":36}],"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}],"materials":[{"pbrMetallicRoughness":{"baseColorFactor":[1,1,1,1]}},{"pbrMetallicRoughness":{"baseColorFactor":[1,0.8,0,1]},"extensions":{"KHR_materials_specular":{"specularFactor":1,"specularColorFactor":[2,1.5,0.5]}}},{"extensions":{"KHR_materials_specular":{"specularFactor":0}}}],"meshes":[{"primitives":[{"attributes":{"POSITION":0},"material":0,"extensions":{"KHR_materials_variants":{"mappings":[{"material":1,"variants":[0]},{"material":2,"variants":[1]}]}}}]}],"nodes":[{"name":"Prop_LOD0","mesh":0},{"name":"Prop_LOD1","mesh":0,"translation":[2,0,0]}],"scenes":[{"nodes":[0,1]}],"scene":0}`), []byte{0, 0, 0, 0, 0, 0, 128, 63, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 128, 63, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
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

// FuzzDecodeKTX2 directly fuzzes the pure-Go KTX 2.0 decoder (container
// headers, DFD parsing, Zstandard/zlib supercompression, all 19 UASTC 4x4
// modes, BasisLZ/ETC1S codebooks & Huffman/VLC/RLE/MTF slices, uncompressed
// VkFormats, and two-channel normal Z reconstruction) within a 1 MiB budget.
func FuzzDecodeKTX2(f *testing.F) {
	f.Add(buildSolidUASTCKTX2Test(color.NRGBA{210, 105, 45, 255}), true)
	f.Add(buildMinimalETC1SKTX2Test(24, 12, 6), false)

	// Partitioned, BISE trit/quint, dual-plane, LA, and RRRG UASTC seeds.
	for _, mode := range []int{0, 2, 3, 6, 9, 11, 15, 17, 18} {
		subsets := uastcSubsets[mode]
		comps := uastcComps[mode]
		numEP := subsets * comps * 2
		tq := make([]uint32, numEP)
		eb := make([]uint32, numEP)
		for i := 1; i < numEP; i += 2 {
			eb[i] = 1
		}
		var w0, w1 [16]uint32
		w0[15] = 1
		blk := packTestUASTCBlock(mode, 0, 1, tq, eb, w0, w1)
		ch := uint8(khrDFChannelUASTCRGBA)
		if mode == 6 {
			ch = khrDFChannelUASTCRGB
		} else if mode == 15 {
			ch = khrDFChannelUASTCRRR
		} else if mode == 17 {
			ch = khrDFChannelUASTCRRRG
		}
		dfd := makeBasicDFD(khrDFModelUASTC, khrDFTransferSRGB, 0, ch)
		f.Add(buildKTX2Container(vkFormatUndefined, 4, 4, ktx2SupercompNone, dfd, nil, blk[:], 16), mode == 17)
	}

	// Zstandard and zlib supercompressed UASTC seeds.
	solidBlk := packTestUASTCBlock(8, 0, 0, nil, []uint32{120, 180, 240, 200}, [16]uint32{}, [16]uint32{})
	uastcDFD := makeBasicDFD(khrDFModelUASTC, khrDFTransferSRGB, 0, khrDFChannelUASTCRGBA)
	if zw, err := zstd.NewWriter(nil); err == nil {
		zstdComp := zw.EncodeAll(solidBlk[:], nil)
		_ = zw.Close()
		f.Add(buildKTX2Container(vkFormatUndefined, 4, 4, ktx2SupercompZstd, uastcDFD, nil, zstdComp, 16), false)
	}
	var zlibBuf bytes.Buffer
	wZlib := zlib.NewWriter(&zlibBuf)
	_, _ = wZlib.Write(solidBlk[:])
	_ = wZlib.Close()
	f.Add(buildKTX2Container(vkFormatUndefined, 4, 4, ktx2SupercompZlib, uastcDFD, nil, zlibBuf.Bytes(), 16), false)

	// Uncompressed premultiplied RGBA8 and R8G8 seeds.
	premulDFD := makeBasicDFD(khrDFModelRGBSDA, khrDFTransferSRGB, khrDFFlagAlphaPremultiplied, 0, 1, 2, 3)
	f.Add(buildKTX2Container(vkFormatR8G8B8A8SR, 2, 2, ktx2SupercompNone, premulDFD, nil, bytes.Repeat([]byte{64, 32, 16, 128}, 4), 16), false)
	rgDFD := makeBasicDFD(khrDFModelRGBSDA, khrDFTransferLinear, 0, 0, 1)
	f.Add(buildKTX2Container(vkFormatR8G8Unorm, 2, 2, ktx2SupercompNone, rgDFD, nil, bytes.Repeat([]byte{160, 128}, 4), 8), true)
	f.Add(ktx2Magic[:], false)

	f.Fuzz(func(t *testing.T, data []byte, srgb bool) {
		// Directly exercise the 128-bit UASTC block unpacker on arbitrary 16-byte inputs too.
		if len(data) >= 16 {
			var blk [16]byte
			copy(blk[:], data[:16])
			_ = unpackUASTCBlock(blk, srgb)
		}

		const maxFuzzBytes = 1 << 20
		left := int64(maxFuzzBytes)
		img, twoCh, err := decodeKTX2(data, &left)
		if err != nil {
			return
		}
		if left < 0 {
			t.Fatalf("decodeKTX2 exceeded byte budget: left=%d", left)
		}
		if img == nil {
			t.Fatal("decodeKTX2 returned nil image without error")
		}
		w, h := img.Bounds().Dx(), img.Bounds().Dy()
		if w <= 0 || w > maxTextureSide || h <= 0 || h > maxTextureSide {
			t.Fatalf("invalid decoded KTX2 dimensions %dx%d", w, h)
		}
		if len(img.Pix) != w*h*4 || img.Stride != w*4 {
			t.Fatalf("invalid NRGBA buffer len=%d stride=%d for %dx%d", len(img.Pix), img.Stride, w, h)
		}
		if _, err := nt3d.NewTexture(img); err != nil {
			t.Fatalf("NewTexture failed on decoded KTX2: %v", err)
		}
		if _, err := nt3d.NewORMMap(img); err != nil {
			t.Fatalf("NewORMMap failed on decoded KTX2: %v", err)
		}
		nrmSrc := image.Image(img)
		if twoCh {
			nrmSrc = reconstructNormalXYImage(img)
		}
		if _, err := nt3d.NewNormalMap(nrmSrc); err != nil {
			t.Fatalf("NewNormalMap failed on decoded KTX2: %v", err)
		}
	})
}

// FuzzDecodeMeshopt directly fuzzes the pure-Go EXT_meshopt_compression byte
// stream decoders (ATTRIBUTES, TRIANGLES, INDICES) and post-filters
// (OCTAHEDRAL, QUATERNION, EXPONENTIAL) on arbitrary compressed payloads.
func FuzzDecodeMeshopt(f *testing.F) {
	attr8 := encodeMeshoptAttributesTest([]byte{
		0, 0, 0, 0, 0, 0, 0, 0,
		100, 0, 0, 0, 0, 0, 0, 0,
		0, 100, 0, 0, 0, 0, 0, 0,
	}, 3, 8)
	attr4 := encodeMeshoptAttributesTest([]byte{
		0, 0, 127, 0,
		64, 64, 64, 0,
		192, 64, 64, 0,
	}, 3, 4)
	tri2 := append([]byte{0xe1, 0x20, 0, 0, 1, 2}, make([]byte, 16)...)
	idx2 := append([]byte{0xd1, 0x00, 0x00, 0x00}, make([]byte, 16)...)

	f.Add(attr8, uint16(3), uint8(8), uint8(0), uint8(0))  // ATTRIBUTES + NONE
	f.Add(attr4, uint16(3), uint8(4), uint8(0), uint8(1))  // ATTRIBUTES + OCTAHEDRAL (4B)
	f.Add(attr8, uint16(3), uint8(8), uint8(0), uint8(1))  // ATTRIBUTES + OCTAHEDRAL (8B)
	f.Add(attr8, uint16(3), uint8(8), uint8(0), uint8(2))  // ATTRIBUTES + QUATERNION (8B)
	f.Add(attr8, uint16(2), uint8(12), uint8(0), uint8(3)) // ATTRIBUTES + EXPONENTIAL
	f.Add(tri2, uint16(3), uint8(2), uint8(1), uint8(0))   // TRIANGLES (2B)
	f.Add(idx2, uint16(3), uint8(2), uint8(2), uint8(0))   // INDICES (2B)

	modes := [...]string{"ATTRIBUTES", "TRIANGLES", "INDICES", "INVALID"}
	filters := [...]string{"NONE", "OCTAHEDRAL", "QUATERNION", "EXPONENTIAL", "INVALID"}

	f.Fuzz(func(t *testing.T, src []byte, countU16 uint16, strideU8, modeIdx, filterIdx uint8) {
		count := int(countU16 % 512)
		stride := int(strideU8)
		if count <= 0 || stride <= 0 || count*stride > 64<<10 {
			return
		}
		mode := modes[int(modeIdx)%len(modes)]
		filter := filters[int(filterIdx)%len(filters)]
		dst := make([]byte, count*stride)
		err := decodeMeshoptBufferView(dst, src, count, stride, mode, filter)
		if err != nil {
			return
		}
		if mode == "ATTRIBUTES" && filter == "QUATERNION" && stride == 8 {
			for i := range count {
				off := i * 8
				qx := int16(binary.LittleEndian.Uint16(dst[off : off+2]))
				qy := int16(binary.LittleEndian.Uint16(dst[off+2 : off+4]))
				qz := int16(binary.LittleEndian.Uint16(dst[off+4 : off+6]))
				qw := int16(binary.LittleEndian.Uint16(dst[off+6 : off+8]))
				if qx == -32768 || qy == -32768 || qz == -32768 || qw == -32768 {
					t.Fatalf("QUATERNION filter emitted -32768 at element %d", i)
				}
				if qx == 0 && qy == 0 && qz == 0 && qw == 0 {
					t.Fatalf("QUATERNION filter emitted all-zero quaternion at element %d", i)
				}
			}
		}
	})
}
