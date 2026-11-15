// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/qmuntal/gltf"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

func zigzag8(v int8) byte {
	return byte((uint8(v) << 1) ^ uint8(v>>7))
}

func zigzag32(v int32) uint32 {
	return (uint32(v) << 1) ^ uint32(v>>31)
}

func appendMeshoptVByte(dst []byte, v uint32) []byte {
	for v >= 128 {
		dst = append(dst, byte(v&127)|128)
		v >>= 7
	}
	return append(dst, byte(v))
}

// encodeMeshoptAttributesTest encodes raw (count*stride bytes) into a valid
// EXT_meshopt_compression Mode 0 (0xa0) stream, selecting the optimal 0/2/4/8-bit
// group mode per 16-element group just as meshoptimizer's vertexcodec v0 does.
func encodeMeshoptAttributesTest(raw []byte, count, stride int) []byte {
	var out []byte
	out = append(out, 0xa0)

	var lastVertex [256]byte
	copy(lastVertex[:stride], raw[:stride])

	maxBlock := min((8192/stride)&^15, 256)
	var deltas [256]byte

	for offset := 0; offset < count; {
		blockCount := min(count-offset, maxBlock)
		groups := (blockCount + 15) / 16
		headerBytes := (groups + 3) / 4

		for k := range stride {
			clear(deltas[:])
			prev := lastVertex[k]
			for i := range blockCount {
				cur := raw[(offset+i)*stride+k]
				deltas[i] = zigzag8(int8(cur - prev))
				prev = cur
			}
			lastVertex[k] = prev

			hdrStart := len(out)
			out = append(out, make([]byte, headerBytes)...)

			for g := range groups {
				grp := deltas[g*16 : g*16+16]
				allZero := true
				cost1 := 4
				cost2 := 8
				for _, v := range grp {
					if v != 0 {
						allZero = false
					}
					if v >= 3 {
						cost1++
					}
					if v >= 15 {
						cost2++
					}
				}
				var mode byte
				switch {
				case allZero:
					mode = 0
				case cost1 <= cost2 && cost1 <= 16:
					mode = 1
				case cost2 <= 16:
					mode = 2
				default:
					mode = 3
				}
				out[hdrStart+(g>>2)] |= mode << ((g & 3) * 2)

				switch mode {
				case 0:
				case 1:
					var packed [4]byte
					var extra []byte
					for bi := range 4 {
						var b byte
						for j := range 4 {
							v := grp[bi*4+j]
							code := v
							if v >= 3 {
								code = 3
								extra = append(extra, v)
							}
							b = (b << 2) | code
						}
						packed[bi] = b
					}
					out = append(out, packed[:]...)
					out = append(out, extra...)
				case 2:
					var packed [8]byte
					var extra []byte
					for bi := range 8 {
						var b byte
						for j := range 2 {
							v := grp[bi*2+j]
							code := v
							if v >= 15 {
								code = 15
								extra = append(extra, v)
							}
							b = (b << 4) | code
						}
						packed[bi] = b
					}
					out = append(out, packed[:]...)
					out = append(out, extra...)
				case 3:
					out = append(out, grp...)
				}
			}
		}
		offset += blockCount
	}

	tailSize := stride
	tailSizePadded := max(tailSize, 32)
	if pad := tailSizePadded - tailSize; pad > 0 {
		out = append(out, make([]byte, pad)...)
	}
	out = append(out, raw[:stride]...)
	return out
}

// encodeMeshoptIndicesTest encodes indices into a Mode 2 (INDICES) stream.
func encodeMeshoptIndicesTest(indices []uint32, stride int, version byte) []byte {
	out := []byte{0xd0 | version}
	var last [2]uint32
	for i, idx := range indices {
		b := uint32(i & 1)
		d0 := zigzag32(int32(idx - last[0]))
		d1 := zigzag32(int32(idx - last[1]))
		if d0 <= d1 {
			b = 0
		} else {
			b = 1
		}
		d := zigzag32(int32(idx - last[b]))
		last[b] = idx
		out = appendMeshoptVByte(out, (d<<1)|b)
	}
	out = append(out, 0, 0, 0, 0)
	return out
}

func encodeOctSnorm8(nx, ny, nz float64, bits int, w int8) [4]byte {
	l1 := math.Abs(nx) + math.Abs(ny) + math.Abs(nz)
	u := nx / l1
	v := ny / l1
	if nz < 0 {
		ou := (1 - math.Abs(v))
		if u < 0 {
			ou = -ou
		}
		ov := (1 - math.Abs(u))
		if v < 0 {
			ov = -ov
		}
		u, v = ou, ov
	}
	one := float64((int(1) << (bits - 1)) - 1)
	return [4]byte{
		byte(int8(math.Round(u * one))),
		byte(int8(math.Round(v * one))),
		byte(int8(one)),
		byte(w),
	}
}

func encodeOctSnorm16(nx, ny, nz float64, bits int, w int16) [8]byte {
	l1 := math.Abs(nx) + math.Abs(ny) + math.Abs(nz)
	u := nx / l1
	v := ny / l1
	if nz < 0 {
		ou := (1 - math.Abs(v))
		if u < 0 {
			ou = -ou
		}
		ov := (1 - math.Abs(u))
		if v < 0 {
			ov = -ov
		}
		u, v = ou, ov
	}
	one := float64((int(1) << (bits - 1)) - 1)
	var b [8]byte
	binary.LittleEndian.PutUint16(b[0:], uint16(int16(math.Round(u*one))))
	binary.LittleEndian.PutUint16(b[2:], uint16(int16(math.Round(v*one))))
	binary.LittleEndian.PutUint16(b[4:], uint16(int16(one)))
	binary.LittleEndian.PutUint16(b[6:], uint16(w))
	return b
}

func encodeQuatSnorm16(q [4]float64, bits int) [8]byte {
	mc := 0
	for i := 1; i < 4; i++ {
		if math.Abs(q[i]) > math.Abs(q[mc]) {
			mc = i
		}
	}
	if q[mc] < 0 {
		for i := range 4 {
			q[i] = -q[i]
		}
	}
	one := float64((int(1) << (bits - 1)) - 1)
	sf := math.Sqrt2 * one
	var b [8]byte
	binary.LittleEndian.PutUint16(b[0:], uint16(int16(math.Round(q[(mc+1)&3]*sf))))
	binary.LittleEndian.PutUint16(b[2:], uint16(int16(math.Round(q[(mc+2)&3]*sf))))
	binary.LittleEndian.PutUint16(b[4:], uint16(int16(math.Round(q[(mc+3)&3]*sf))))
	c3 := (int16(one) &^ 3) | int16(mc)
	binary.LittleEndian.PutUint16(b[6:], uint16(c3))
	return b
}

func encodeExpFloat32(f float32, exp int) [4]byte {
	m := int32(math.Round(math.Ldexp(float64(f), -exp)))
	u := (uint32(uint8(int8(exp))) << 24) | (uint32(m) & 0x00ffffff)
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], u)
	return b
}

func TestMeshoptCodecModesAndFilters(t *testing.T) {
	t.Run("AttributesMultiBlockAndAllGroupModes", func(t *testing.T) {
		for _, stride := range []int{4, 12, 32, 48} {
			const count = 300 // Spans >1 block (maxBlock <= 256).
			raw := make([]byte, count*stride)
			for i := range count {
				for k := range stride {
					switch k % 4 {
					case 0:
						raw[i*stride+k] = 42 // Mode 0: all zero deltas.
					case 1:
						raw[i*stride+k] = byte(i / 3) // Mode 1: tiny 2-bit deltas + occasional sentinel.
					case 2:
						raw[i*stride+k] = byte(i * 3) // Mode 2: 4-bit deltas.
					case 3:
						raw[i*stride+k] = byte(i*73 + k*19) // Mode 3: high-entropy literal deltas.
					}
				}
			}
			enc := encodeMeshoptAttributesTest(raw, count, stride)
			got := make([]byte, len(raw))
			check(t, decodeMeshoptBufferView(got, enc, count, stride, "ATTRIBUTES", "NONE"))
			if !bytes.Equal(got, raw) {
				t.Fatalf("stride %d round-trip mismatch", stride)
			}
		}
	})

	t.Run("TrianglesAllOpcodesV1AndV0", func(t *testing.T) {
		// Construct a hand-crafted v1 (0xe1) stream exercising every opcode:
		//   tri 0: 0xf0 (codeaux[0]=0x00 -> a=0, b=1, c=2; edges (1,0),(2,1),(0,2); vfifo 0,1,2)
		//   tri 1: 0x10 (edgefifo[1]=(2,1), Y=0 -> c=next++=3 -> (2,1,3); edges (3,1),(2,3); vfifo ..,3)
		//   tri 2: 0x04 (edgefifo[0]=(2,3), Y=4 -> vfifo[offset-4]=0 -> (2,3,0))
		//   tri 3: 0x0f (edgefifo[0]=(2,0), Y=15 -> decodeIndex(+10 -> last=10) -> (2,0,10))
		//   tri 4: 0x0d (edgefifo[0]=(2,10), Y=13 -> last-- = 9 -> (2,10,9))
		//   tri 5: 0x0e (edgefifo[0]=(2,9), Y=14 -> last++ = 10 -> (2,9,10))
		//   tri 6: 0xf1 (codeaux[1]=0x12 -> a=next++=4, b=vfifo[offset-1]=10, c=vfifo[offset-2]=9 -> (4,10,9))
		//   tri 7: 0xfe with aux=0x00 -> resets next=0! a=0, b=1, c=2 -> (0,1,2)
		//   tri 8: 0xff with aux=0x1f -> a=decodeIndex(+200 -> last=210), b=vfifo[offset-1]=2, c=decodeIndex(-5 -> last=205) -> (210,2,205)
		codes := []byte{0xf0, 0x10, 0x04, 0x0f, 0x0d, 0x0e, 0xf1, 0xfe, 0xff}
		var data []byte
		data = appendMeshoptVByte(data, zigzag32(10))  // tri 3: last = 0 + 10 = 10
		data = append(data, 0x00)                      // tri 7: 0xfe aux=0x00 (reset next=0, Z=0, W=0)
		data = append(data, 0x1f)                      // tri 8: 0xff aux=0x1f (Z=1, W=15)
		data = appendMeshoptVByte(data, zigzag32(200)) // tri 8 a: last = 10 + 200 = 210
		data = appendMeshoptVByte(data, zigzag32(-5))  // tri 8 c: last = 210 - 5 = 205
		var codeaux [16]byte
		codeaux[0] = 0x00
		codeaux[1] = 0x12

		var stream []byte
		stream = append(stream, 0xe1)
		stream = append(stream, codes...)
		stream = append(stream, data...)
		stream = append(stream, codeaux[:]...)

		want := []uint32{
			0, 1, 2,
			2, 1, 3,
			2, 3, 0,
			2, 0, 10,
			2, 10, 9,
			2, 9, 10,
			4, 10, 9,
			0, 1, 2,
			210, 2, 205,
		}

		for _, stride := range []int{2, 4} {
			dst := make([]byte, len(want)*stride)
			check(t, decodeMeshoptBufferView(dst, stream, len(want), stride, "TRIANGLES", ""))
			for i, w := range want {
				var got uint32
				if stride == 2 {
					got = uint32(binary.LittleEndian.Uint16(dst[i*2:]))
				} else {
					got = binary.LittleEndian.Uint32(dst[i*4:])
				}
				if got != w {
					t.Fatalf("stride %d index %d: got %d, want %d", stride, i, got, w)
				}
			}
		}

		// Also verify v0 (0xe0) where fecmax == 15 (so Y=13 reads vertexfifo instead of last-1).
		v0Codes := []byte{0xf0, 0x10}
		var v0Stream []byte
		v0Stream = append(v0Stream, 0xe0)
		v0Stream = append(v0Stream, v0Codes...)
		v0Stream = append(v0Stream, codeaux[:]...)
		v0Dst := make([]byte, 6*2)
		check(t, decodeMeshoptBufferView(v0Dst, v0Stream, 6, 2, "TRIANGLES", "NONE"))
		for i, w := range []uint16{0, 1, 2, 2, 1, 3} {
			if got := binary.LittleEndian.Uint16(v0Dst[i*2:]); got != w {
				t.Fatalf("v0 index %d: got %d, want %d", i, got, w)
			}
		}
	})

	t.Run("IndicesV0AndV1", func(t *testing.T) {
		want := []uint32{0, 1000, 1, 1002, 2, 999, 70000, 70001}
		for _, ver := range []byte{0, 1} {
			enc := encodeMeshoptIndicesTest(want, 4, ver)
			dst := make([]byte, len(want)*4)
			check(t, decodeMeshoptBufferView(dst, enc, len(want), 4, "INDICES", ""))
			for i, w := range want {
				if got := binary.LittleEndian.Uint32(dst[i*4:]); got != w {
					t.Fatalf("ver %d index %d: got %d, want %d", ver, i, got, w)
				}
			}
		}
	})

	t.Run("OctahedralFilter8And16", func(t *testing.T) {
		dirs := [][3]float64{
			{0, 0, 1},
			{0, 0, -1}, // Negative hemisphere fold corner!
			{1, 0, 0},
			{0, -1, 0},
			{1 / math.Sqrt(3), -1 / math.Sqrt(3), -1 / math.Sqrt(3)},
		}
		var raw8, raw16 []byte
		for i, d := range dirs {
			w8 := int8(127)
			w16 := int16(32767)
			if i%2 == 1 {
				w8 = -127
				w16 = -32767
			}
			b8 := encodeOctSnorm8(d[0], d[1], d[2], 8, w8)
			raw8 = append(raw8, b8[:]...)
			b16 := encodeOctSnorm16(d[0], d[1], d[2], 12, w16)
			raw16 = append(raw16, b16[:]...)
		}

		dst8 := make([]byte, len(raw8))
		check(t, decodeMeshoptBufferView(dst8, encodeMeshoptAttributesTest(raw8, len(dirs), 4), len(dirs), 4, "ATTRIBUTES", "OCTAHEDRAL"))
		dst16 := make([]byte, len(raw16))
		check(t, decodeMeshoptBufferView(dst16, encodeMeshoptAttributesTest(raw16, len(dirs), 8), len(dirs), 8, "ATTRIBUTES", "OCTAHEDRAL"))

		for i, d := range dirs {
			gx8 := float64(gltf.DenormalizeByte(int8(dst8[i*4+0])))
			gy8 := float64(gltf.DenormalizeByte(int8(dst8[i*4+1])))
			gz8 := float64(gltf.DenormalizeByte(int8(dst8[i*4+2])))
			if dot := gx8*d[0] + gy8*d[1] + gz8*d[2]; dot < 0.99 {
				t.Fatalf("oct8 %d dot=%v got=(%v,%v,%v) want=%v", i, dot, gx8, gy8, gz8, d)
			}
			wantW8 := int8(127)
			if i%2 == 1 {
				wantW8 = -127
			}
			if int8(dst8[i*4+3]) != wantW8 {
				t.Fatalf("oct8 %d passthrough W changed: got %d, want %d", i, int8(dst8[i*4+3]), wantW8)
			}

			gx16 := float64(gltf.DenormalizeShort(int16(binary.LittleEndian.Uint16(dst16[i*8+0:]))))
			gy16 := float64(gltf.DenormalizeShort(int16(binary.LittleEndian.Uint16(dst16[i*8+2:]))))
			gz16 := float64(gltf.DenormalizeShort(int16(binary.LittleEndian.Uint16(dst16[i*8+4:]))))
			if dot := gx16*d[0] + gy16*d[1] + gz16*d[2]; dot < 0.9999 {
				t.Fatalf("oct16 %d dot=%v got=(%v,%v,%v) want=%v", i, dot, gx16, gy16, gz16, d)
			}
		}
	})

	t.Run("QuaternionFilterAllMaxComponents", func(t *testing.T) {
		s := 1 / math.Sqrt2
		quats := [][4]float64{
			{1, 0, 0, 0},  // maxcomp 0
			{0, -1, 0, 0}, // maxcomp 1 (negative double-cover)
			{0, 0, 1, 0},  // maxcomp 2
			{0, 0, 0, 1},  // maxcomp 3
			{0, s, 0, s},  // 90 deg Y
			{-0.5, 0.5, -0.5, 0.5},
		}
		var raw []byte
		for _, q := range quats {
			b := encodeQuatSnorm16(q, 12)
			raw = append(raw, b[:]...)
		}
		dst := make([]byte, len(raw))
		check(t, decodeMeshoptBufferView(dst, encodeMeshoptAttributesTest(raw, len(quats), 8), len(quats), 8, "ATTRIBUTES", "QUATERNION"))
		for i, q := range quats {
			var dot float64
			for c := range 4 {
				v := float64(gltf.DenormalizeShort(int16(binary.LittleEndian.Uint16(dst[i*8+c*2:]))))
				dot += v * q[c]
			}
			if math.Abs(dot) < 0.9995 {
				t.Fatalf("quat %d |dot|=%v", i, math.Abs(dot))
			}
		}
	})

	t.Run("ExponentialFilter", func(t *testing.T) {
		vecs := [][3]float32{
			{0, 1.5, -2.25},
			{128.125, -0.0625, 42.5},
		}
		var raw []byte
		for _, v := range vecs {
			for c := range 3 {
				b := encodeExpFloat32(v[c], -6)
				raw = append(raw, b[:]...)
			}
		}
		dst := make([]byte, len(raw))
		check(t, decodeMeshoptBufferView(dst, encodeMeshoptAttributesTest(raw, len(vecs), 12), len(vecs), 12, "ATTRIBUTES", "EXPONENTIAL"))
		for i, v := range vecs {
			for c := range 3 {
				got := math.Float32frombits(binary.LittleEndian.Uint32(dst[i*12+c*4:]))
				if got != v[c] {
					t.Fatalf("exp [%d][%d]: got %v, want %v", i, c, got, v[c])
				}
			}
		}
	})
}

func TestKHRMeshQuantizationAttributesAndMorphs(t *testing.T) {
	// Test all 8 POSITION quantized componentType/normalized modes + quantized NORMAL, TANGENT, TEXCOORD, and morph targets.
	type posCase struct {
		name string
		comp gltf.ComponentType
		norm bool
		raw  []byte
		want [3]float32 // Expected glTF Y-up position of vertex 1 before Z-up rotation.
	}
	putI16 := func(a, b, c int16) []byte {
		var out [8]byte
		binary.LittleEndian.PutUint16(out[0:], uint16(a))
		binary.LittleEndian.PutUint16(out[2:], uint16(b))
		binary.LittleEndian.PutUint16(out[4:], uint16(c))
		return out[:]
	}
	putU16 := func(a, b, c uint16) []byte {
		var out [8]byte
		binary.LittleEndian.PutUint16(out[0:], a)
		binary.LittleEndian.PutUint16(out[2:], b)
		binary.LittleEndian.PutUint16(out[4:], c)
		return out[:]
	}

	i8b := func(v int8) byte { return byte(v) }
	i16u := func(v int16) uint16 { return uint16(v) }

	cases := []posCase{
		{
			name: "ByteUnnorm",
			comp: gltf.ComponentByte,
			norm: false,
			raw:  []byte{0, 0, 0, 0, 10, 20, i8b(-5), 0, 0, 10, 0, 0},
			want: [3]float32{10, 20, -5},
		},
		{
			name: "ByteNorm",
			comp: gltf.ComponentByte,
			norm: true,
			raw:  []byte{0, 0, 0, 0, 127, 0, i8b(-127), 0, 0, 127, 0, 0},
			want: [3]float32{1, 0, -1},
		},
		{
			name: "UbyteUnnorm",
			comp: gltf.ComponentUbyte,
			norm: false,
			raw:  []byte{0, 0, 0, 0, 4, 8, 2, 0, 0, 4, 0, 0},
			want: [3]float32{4, 8, 2},
		},
		{
			name: "UbyteNorm",
			comp: gltf.ComponentUbyte,
			norm: true,
			raw:  []byte{0, 0, 0, 0, 255, 0, 0, 0, 0, 255, 0, 0},
			want: [3]float32{1, 0, 0},
		},
		{
			name: "ShortUnnorm",
			comp: gltf.ComponentShort,
			norm: false,
			raw:  slicesConcat(putI16(0, 0, 0), putI16(300, -100, 50), putI16(0, 300, 0)),
			want: [3]float32{300, -100, 50},
		},
		{
			name: "ShortNorm",
			comp: gltf.ComponentShort,
			norm: true,
			raw:  slicesConcat(putI16(0, 0, 0), putI16(32767, 0, -32767), putI16(0, 32767, 0)),
			want: [3]float32{1, 0, -1},
		},
		{
			name: "UshortUnnorm",
			comp: gltf.ComponentUshort,
			norm: false,
			raw:  slicesConcat(putU16(0, 0, 0), putU16(1000, 500, 250), putU16(0, 1000, 0)),
			want: [3]float32{1000, 500, 250},
		},
		{
			name: "UshortNorm",
			comp: gltf.ComponentUshort,
			norm: true,
			raw:  slicesConcat(putU16(0, 0, 0), putU16(65535, 0, 0), putU16(0, 65535, 0)),
			want: [3]float32{1, 0, 0},
		},
	}

	texBytes := webpTexture(t, color.NRGBA{R: 200, G: 100, B: 50, A: 255})
	normBytes := webpTexture(t, color.NRGBA{R: 128, G: 128, B: 255, A: 255})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stride := len(tc.raw) / 3
			// Normals: normalized BYTE pointing +Z in glTF (0, 0, 127, 0).
			normBuf := []byte{0, 0, 127, 0, 0, 0, 127, 0, 0, 0, 127, 0}
			// Tangents: normalized SHORT (+X, W=-1): (32767, 0, 0, -32767).
			var tanBuf []byte
			for range 3 {
				var b [8]byte
				binary.LittleEndian.PutUint16(b[0:], i16u(32767))
				binary.LittleEndian.PutUint16(b[6:], i16u(-32767))
				tanBuf = append(tanBuf, b[:]...)
			}
			// UVs: normalized SHORT (0,0), (32767, 0), (0, 32767).
			uvBuf := make([]byte, 3*4)
			binary.LittleEndian.PutUint16(uvBuf[4:], 32767)
			binary.LittleEndian.PutUint16(uvBuf[10:], 32767)

			// Morph delta POSITION (SHORT unnormalized: +2 along X on all 3 verts) and NORMAL (BYTE normalized).
			morphPos := slicesConcat(putI16(2, 0, 0), putI16(2, 0, 0), putI16(2, 0, 0))
			morphNorm := []byte{0, 127, 0, 0, 0, 127, 0, 0, 0, 127, 0, 0}

			var bin bytes.Buffer
			addView := func(b []byte, byteStride int) int {
				for bin.Len()%4 != 0 {
					bin.WriteByte(0)
				}
				off := bin.Len()
				bin.Write(b)
				return off
			}
			posOff := addView(tc.raw, stride)
			normOff := addView(normBuf, 4)
			tanOff := addView(tanBuf, 8)
			uvOff := addView(uvBuf, 4)
			mpOff := addView(morphPos, 8)
			mnOff := addView(morphNorm, 4)
			texOff := addView(texBytes, 0)
			nmOff := addView(normBytes, 0)

			doc := &gltf.Document{
				Asset:              gltf.Asset{Version: "2.0"},
				ExtensionsUsed:     []string{"KHR_mesh_quantization", "EXT_texture_webp"},
				ExtensionsRequired: []string{"KHR_mesh_quantization", "EXT_texture_webp"},
				Buffers:            []*gltf.Buffer{{ByteLength: bin.Len(), Data: bin.Bytes()}},
				BufferViews: []*gltf.BufferView{
					{Buffer: 0, ByteOffset: posOff, ByteLength: len(tc.raw), ByteStride: stride},
					{Buffer: 0, ByteOffset: normOff, ByteLength: len(normBuf), ByteStride: 4},
					{Buffer: 0, ByteOffset: tanOff, ByteLength: len(tanBuf), ByteStride: 8},
					{Buffer: 0, ByteOffset: uvOff, ByteLength: len(uvBuf), ByteStride: 4},
					{Buffer: 0, ByteOffset: mpOff, ByteLength: len(morphPos), ByteStride: 8},
					{Buffer: 0, ByteOffset: mnOff, ByteLength: len(morphNorm), ByteStride: 4},
					{Buffer: 0, ByteOffset: texOff, ByteLength: len(texBytes)},
					{Buffer: 0, ByteOffset: nmOff, ByteLength: len(normBytes)},
				},
				Accessors: []*gltf.Accessor{
					{BufferView: gltf.Index(0), ComponentType: tc.comp, Normalized: tc.norm, Count: 3, Type: gltf.AccessorVec3},
					{BufferView: gltf.Index(1), ComponentType: gltf.ComponentByte, Normalized: true, Count: 3, Type: gltf.AccessorVec3},
					{BufferView: gltf.Index(2), ComponentType: gltf.ComponentShort, Normalized: true, Count: 3, Type: gltf.AccessorVec4},
					{BufferView: gltf.Index(3), ComponentType: gltf.ComponentShort, Normalized: true, Count: 3, Type: gltf.AccessorVec2},
					{BufferView: gltf.Index(4), ComponentType: gltf.ComponentShort, Normalized: false, Count: 3, Type: gltf.AccessorVec3},
					{BufferView: gltf.Index(5), ComponentType: gltf.ComponentByte, Normalized: true, Count: 3, Type: gltf.AccessorVec3},
				},
				Images: []*gltf.Image{
					{BufferView: gltf.Index(6), MimeType: "image/webp"},
					{BufferView: gltf.Index(7), MimeType: "image/webp"},
				},
				Textures: []*gltf.Texture{
					{Extensions: gltf.Extensions{"EXT_texture_webp": json.RawMessage(`{"source":0}`)}},
					{Extensions: gltf.Extensions{"EXT_texture_webp": json.RawMessage(`{"source":1}`)}},
				},
				Materials: []*gltf.Material{{
					PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
						BaseColorTexture: &gltf.TextureInfo{Index: 0},
					},
					NormalTexture: &gltf.NormalTexture{Index: gltf.Index(1)},
				}},
				Meshes: []*gltf.Mesh{{
					Name:    "qmesh",
					Weights: []float64{0.5},
					Primitives: []*gltf.Primitive{{
						Attributes: gltf.PrimitiveAttributes{
							gltf.POSITION:   0,
							gltf.NORMAL:     1,
							gltf.TANGENT:    2,
							gltf.TEXCOORD_0: 3,
						},
						Material: gltf.Index(0),
						Mode:     gltf.PrimitiveTriangles,
						Targets: []gltf.PrimitiveAttributes{{
							gltf.POSITION: 4,
							gltf.NORMAL:   5,
						}},
					}},
				}},
				Nodes:  []*gltf.Node{{Name: "qnode", Mesh: gltf.Index(0)}},
				Scenes: []*gltf.Scene{{Nodes: []int{0}}},
				Scene:  gltf.Index(0),
			}
			var glb bytes.Buffer
			enc := gltf.NewEncoder(&glb)
			enc.AsBinary = true
			check(t, enc.Encode(doc))

			m, err := Decode(glb.Bytes(), nil)
			check(t, err)
			if len(m.Warnings) != 0 {
				t.Fatalf("unexpected warnings: %v", m.Warnings)
			}
			g := m.Objects[0].Geometry
			// glTF (x, y, z) rotates Y-up -> Z-up as (x, -z, y).
			wantV1 := nt3dmath.Vec3{X: tc.want[0], Y: -tc.want[2], Z: tc.want[1]}
			gotV1 := g.Vertices[1].Position
			diff := gotV1.Sub(wantV1)
			if diff.Dot(diff) > 1e-6 {
				t.Fatalf("vertex 1 position: got %v, want %v", gotV1, wantV1)
			}
			if g.Tangents[1].W != -1 {
				t.Fatalf("tangent W: got %v, want -1", g.Tangents[1].W)
			}
			if math.Abs(float64(g.UV[1].U-1)) > 1e-4 || math.Abs(float64(g.UV[2].V-1)) > 1e-4 {
				t.Fatalf("unexpected quantized UVs: %v", g.UV)
			}
			// Verify morph target delta in Asset (+2 * 0.5 = +1 on X when instantiated!).
			sc := nt3d.NewScene()
			_, err = m.Asset.Instantiate(sc, nt3d.Root, "q")
			check(t, err)
		})
	}
}

func TestEXTMeshoptCompressionEndToEndGLB(t *testing.T) {
	// Full gltfpack-style .glb combining KHR_mesh_quantization + EXT_meshopt_compression +
	// EXT_texture_webp + EXT_mesh_gpu_instancing + quantized rotation animation!
	//
	// Build a 4-vertex quad (2 triangles) with SHORT quantized positions + node scale/offset,
	// OCTAHEDRAL BYTE normals & tangents, USHORT UVs, TRIANGLES-compressed indices,
	// INDICES-compressed line segment, EXPONENTIAL-compressed instance translations,
	// and QUATERNION-compressed animation rotations.
	putPos := func(x, y, z int16) [8]byte {
		var b [8]byte
		binary.LittleEndian.PutUint16(b[0:], uint16(x))
		binary.LittleEndian.PutUint16(b[2:], uint16(y))
		binary.LittleEndian.PutUint16(b[4:], uint16(z))
		return b
	}
	var posRaw, normRaw, tanRaw, uvRaw []byte
	quadPts := [][3]int16{{0, 0, 0}, {1000, 0, 0}, {0, 1000, 0}, {1000, 1000, 0}}
	quadUVs := [][2]uint16{{0, 0}, {65535, 0}, {0, 65535}, {65535, 65535}}
	for i, p := range quadPts {
		pb := putPos(p[0], p[1], p[2])
		posRaw = append(posRaw, pb[:]...)
		nb := encodeOctSnorm8(0, 0, 1, 8, 0)
		normRaw = append(normRaw, nb[:]...)
		tb := encodeOctSnorm8(1, 0, 0, 8, 127)
		tanRaw = append(tanRaw, tb[:]...)
		var ub [4]byte
		binary.LittleEndian.PutUint16(ub[0:], quadUVs[i][0])
		binary.LittleEndian.PutUint16(ub[2:], quadUVs[i][1])
		uvRaw = append(uvRaw, ub[:]...)
	}

	// 2 triangles: (0,1,2) via 0xf0 (codeaux[0]=0x00) and (2,1,3) via 0x10!
	var triComp []byte
	triComp = append(triComp, 0xe1, 0xf0, 0x10)
	triComp = append(triComp, make([]byte, 16)...)

	// Line primitive indices (0, 3) compressed with Mode 2 INDICES!
	lineComp := encodeMeshoptIndicesTest([]uint32{0, 3}, 2, 1)

	// Instance translations: 2 instances at (0,0,0) and (2,0,0), EXPONENTIAL-filtered!
	var instTransRaw []byte
	for _, tr := range [][3]float32{{0, 0, 0}, {2, 0, 0}} {
		for c := range 3 {
			b := encodeExpFloat32(tr[c], -4)
			instTransRaw = append(instTransRaw, b[:]...)
		}
	}

	// Animation times (0.0, 1.0) EXPONENTIAL-filtered + rotations QUATERNION-filtered!
	var animTimeRaw, animRotRaw []byte
	for _, tm := range []float32{0, 1} {
		b := encodeExpFloat32(tm, -4)
		animTimeRaw = append(animTimeRaw, b[:]...)
	}
	for _, q := range [][4]float64{{0, 0, 0, 1}, {0, 1 / math.Sqrt2, 0, 1 / math.Sqrt2}} {
		b := encodeQuatSnorm16(q, 12)
		animRotRaw = append(animRotRaw, b[:]...)
	}

	posComp := encodeMeshoptAttributesTest(posRaw, 4, 8)
	normComp := encodeMeshoptAttributesTest(normRaw, 4, 4)
	tanComp := encodeMeshoptAttributesTest(tanRaw, 4, 4)
	uvComp := encodeMeshoptAttributesTest(uvRaw, 4, 4)
	instTransComp := encodeMeshoptAttributesTest(instTransRaw, 2, 12)
	animTimeComp := encodeMeshoptAttributesTest(animTimeRaw, 2, 4)
	animRotComp := encodeMeshoptAttributesTest(animRotRaw, 2, 8)

	texBytes := webpTexture(t, color.NRGBA{R: 80, G: 180, B: 240, A: 255})
	normMapBytes := webpTexture(t, color.NRGBA{R: 128, G: 128, B: 255, A: 255})

	var bin bytes.Buffer
	var views []*gltf.BufferView
	fallbackOffset := 0

	addCompressedView := func(comp []byte, count, stride int, mode, filter string, setViewStride bool) int {
		for bin.Len()%4 != 0 {
			bin.WriteByte(0)
		}
		off := bin.Len()
		bin.Write(comp)
		unpacked := count * stride
		extObj := map[string]any{
			"buffer":     0,
			"byteOffset": off,
			"byteLength": len(comp),
			"byteStride": stride,
			"count":      count,
			"mode":       mode,
		}
		if filter != "" {
			extObj["filter"] = filter
		}
		extJSON, err := json.Marshal(extObj)
		check(t, err)
		bv := &gltf.BufferView{
			Buffer:     1, // Fallback buffer!
			ByteOffset: fallbackOffset,
			ByteLength: unpacked,
			Extensions: gltf.Extensions{"EXT_meshopt_compression": json.RawMessage(extJSON)},
		}
		if setViewStride {
			bv.ByteStride = stride
		}
		fallbackOffset += (unpacked + 3) &^ 3
		idx := len(views)
		views = append(views, bv)
		return idx
	}
	addRawView := func(b []byte) int {
		for bin.Len()%4 != 0 {
			bin.WriteByte(0)
		}
		off := bin.Len()
		bin.Write(b)
		idx := len(views)
		views = append(views, &gltf.BufferView{Buffer: 0, ByteOffset: off, ByteLength: len(b)})
		return idx
	}

	posView := addCompressedView(posComp, 4, 8, "ATTRIBUTES", "NONE", true)
	normView := addCompressedView(normComp, 4, 4, "ATTRIBUTES", "OCTAHEDRAL", true)
	tanView := addCompressedView(tanComp, 4, 4, "ATTRIBUTES", "OCTAHEDRAL", true)
	uvView := addCompressedView(uvComp, 4, 4, "ATTRIBUTES", "NONE", true)
	triView := addCompressedView(triComp, 6, 2, "TRIANGLES", "", false)
	lineView := addCompressedView(lineComp, 2, 2, "INDICES", "", false)
	instView := addCompressedView(instTransComp, 2, 12, "ATTRIBUTES", "EXPONENTIAL", false)
	timeView := addCompressedView(animTimeComp, 2, 4, "ATTRIBUTES", "EXPONENTIAL", false)
	rotView := addCompressedView(animRotComp, 2, 8, "ATTRIBUTES", "QUATERNION", false)
	texView := addRawView(texBytes)
	nmView := addRawView(normMapBytes)

	exts := []string{
		"KHR_mesh_quantization",
		"EXT_meshopt_compression",
		"EXT_texture_webp",
		"EXT_mesh_gpu_instancing",
	}
	doc := &gltf.Document{
		Asset:              gltf.Asset{Version: "2.0"},
		ExtensionsUsed:     exts,
		ExtensionsRequired: exts,
		Buffers: []*gltf.Buffer{
			{ByteLength: bin.Len(), Data: bin.Bytes()},
			{ByteLength: fallbackOffset, Extensions: gltf.Extensions{"EXT_meshopt_compression": json.RawMessage(`{"fallback":true}`)}},
		},
		BufferViews: views,
		Accessors: []*gltf.Accessor{
			{BufferView: gltf.Index(posView), ComponentType: gltf.ComponentShort, Normalized: false, Count: 4, Type: gltf.AccessorVec3},
			{BufferView: gltf.Index(normView), ComponentType: gltf.ComponentByte, Normalized: true, Count: 4, Type: gltf.AccessorVec3},
			{BufferView: gltf.Index(tanView), ComponentType: gltf.ComponentByte, Normalized: true, Count: 4, Type: gltf.AccessorVec4},
			{BufferView: gltf.Index(uvView), ComponentType: gltf.ComponentUshort, Normalized: true, Count: 4, Type: gltf.AccessorVec2},
			{BufferView: gltf.Index(triView), ComponentType: gltf.ComponentUshort, Count: 6, Type: gltf.AccessorScalar},
			{BufferView: gltf.Index(lineView), ComponentType: gltf.ComponentUshort, Count: 2, Type: gltf.AccessorScalar},
			{BufferView: gltf.Index(instView), ComponentType: gltf.ComponentFloat, Count: 2, Type: gltf.AccessorVec3},
			{BufferView: gltf.Index(timeView), ComponentType: gltf.ComponentFloat, Count: 2, Type: gltf.AccessorScalar},
			{BufferView: gltf.Index(rotView), ComponentType: gltf.ComponentShort, Normalized: true, Count: 2, Type: gltf.AccessorVec4},
		},
		Images: []*gltf.Image{
			{BufferView: gltf.Index(texView), MimeType: "image/webp"},
			{BufferView: gltf.Index(nmView), MimeType: "image/webp"},
		},
		Textures: []*gltf.Texture{
			{Extensions: gltf.Extensions{"EXT_texture_webp": json.RawMessage(`{"source":0}`)}},
			{Extensions: gltf.Extensions{"EXT_texture_webp": json.RawMessage(`{"source":1}`)}},
		},
		Materials: []*gltf.Material{{
			PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
				BaseColorTexture: &gltf.TextureInfo{Index: 0},
			},
			NormalTexture: &gltf.NormalTexture{Index: gltf.Index(1)},
		}},
		Meshes: []*gltf.Mesh{{
			Name: "tile",
			Primitives: []*gltf.Primitive{
				{
					Attributes: gltf.PrimitiveAttributes{
						gltf.POSITION:   0,
						gltf.NORMAL:     1,
						gltf.TANGENT:    2,
						gltf.TEXCOORD_0: 3,
					},
					Indices:  gltf.Index(4),
					Material: gltf.Index(0),
					Mode:     gltf.PrimitiveTriangles,
				},
				{
					Attributes: gltf.PrimitiveAttributes{gltf.POSITION: 0},
					Indices:    gltf.Index(5),
					Mode:       gltf.PrimitiveLines,
				},
			},
		}},
		Nodes: []*gltf.Node{{
			Name:  "prop_root",
			Scale: [3]float64{0.001, 0.001, 0.001}, // Dequantizes 0..1000 -> 0..1m!
			Mesh:  gltf.Index(0),
			Extensions: gltf.Extensions{
				"EXT_mesh_gpu_instancing": json.RawMessage(`{"attributes":{"TRANSLATION":6}}`),
			},
		}},
		Animations: []*gltf.Animation{{
			Name: "spin",
			Samplers: []*gltf.AnimationSampler{{
				Input:         7,
				Output:        8,
				Interpolation: gltf.InterpolationLinear,
			}},
			Channels: []*gltf.AnimationChannel{{
				Sampler: 0,
				Target:  gltf.AnimationChannelTarget{Node: gltf.Index(0), Path: gltf.TRSRotation},
			}},
		}},
		Scenes: []*gltf.Scene{{Nodes: []int{0}}},
		Scene:  gltf.Index(0),
	}

	var glb bytes.Buffer
	enc := gltf.NewEncoder(&glb)
	enc.AsBinary = true
	check(t, enc.Encode(doc))

	m, err := Decode(glb.Bytes(), nil)
	check(t, err)
	if len(m.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", m.Warnings)
	}
	// 2 instances * 2 primitives (1 triangle quad + 1 line segment) = 4 objects!
	if len(m.Objects) != 4 {
		t.Fatalf("expected 4 objects, got %d", len(m.Objects))
	}
	// Verify SharedGeometry reuse on the two triangle instances in Model.Asset.
	var sharedPtrs []*nt3d.SharedGeometry
	for _, an := range m.Asset.Nodes {
		if an.Shared != nil {
			sharedPtrs = append(sharedPtrs, an.Shared)
		}
	}
	if len(sharedPtrs) != 2 || sharedPtrs[0] != sharedPtrs[1] {
		t.Fatalf("expected 2 asset nodes sharing one *SharedGeometry, got %v", sharedPtrs)
	}
	// Verify dequantized bounds: instance 0 spans X in [0, 1], instance 1 translated by 2*0.001 = 0.002 -> X up to 1.002!
	bounds := m.Bounds()
	if math.Abs(float64(bounds.Max.X-1.002)) > 1e-3 || math.Abs(float64(bounds.Max.Z-1.0)) > 1e-3 {
		t.Fatalf("unexpected bounds: %+v", bounds)
	}
	// Verify Model.Asset vs flattened Model.Objects software-render pixel parity before stepping animation!
	sFlat := nt3d.NewScene()
	for i, o := range m.Objects {
		_, err := sFlat.SetGeometry(fmt.Sprintf("flat-%d", i), o.Geometry)
		check(t, err)
	}
	sc := nt3d.NewScene()
	inst, err := m.Asset.Instantiate(sc, nt3d.Root, "city")
	check(t, err)
	cam := nt3d.DefaultCamera()
	cam.Distance = 4
	fAsset, err := sc.Frame(cam, 96, 72)
	check(t, err)
	fFlat, err := sFlat.Frame(cam, 96, 72)
	check(t, err)
	imgAsset, err := nt3d.NewSoftwareRenderer().Render(fAsset)
	check(t, err)
	imgFlat, err := nt3d.NewSoftwareRenderer().Render(fFlat)
	check(t, err)
	if !bytes.Equal(imgAsset.(*image.RGBA).Pix, imgFlat.(*image.RGBA).Pix) {
		t.Fatal("meshopt+quantized SceneAsset render differs from flattened Model.Objects render")
	}

	// Verify quantized quaternion animation clip runs cleanly!
	if len(inst.Clips) != 1 {
		t.Fatalf("expected 1 clip, got %d", len(inst.Clips))
	}
	anim, err := nt3d.NewAnimation(sc, inst.Clips[0])
	check(t, err)
	anim.Step(0.5)
}

func TestMeshoptExternalJSONQuantizedUVTransformAndFallbacks(t *testing.T) {
	// 1. External .gltf JSON + compressed side.bin + fallback buffer +
	//    unnormalized USHORT TEXCOORD_0 with KHR_texture_transform +
	//    normalized USHORT TRSWeights morph animation clip!
	posRaw := make([]byte, 3*8) // 3 SHORT VEC3 padded to 8 bytes
	for i, p := range [][3]int16{{0, 0, 0}, {100, 0, 0}, {0, 100, 0}} {
		binary.LittleEndian.PutUint16(posRaw[i*8+0:], uint16(p[0]))
		binary.LittleEndian.PutUint16(posRaw[i*8+2:], uint16(p[1]))
		binary.LittleEndian.PutUint16(posRaw[i*8+4:], uint16(p[2]))
	}
	uvRaw := make([]byte, 3*4) // 3 unnormalized USHORT VEC2 in [0..4000]
	for i, uv := range [][2]uint16{{0, 0}, {4000, 0}, {0, 4000}} {
		binary.LittleEndian.PutUint16(uvRaw[i*4+0:], uv[0])
		binary.LittleEndian.PutUint16(uvRaw[i*4+2:], uv[1])
	}
	morphRaw := make([]byte, 3*8) // 3 SHORT VEC3 morph delta
	for i := range 3 {
		binary.LittleEndian.PutUint16(morphRaw[i*8+4:], uint16(int16(50)))
	}
	timesRaw := make([]byte, 8)
	binary.LittleEndian.PutUint32(timesRaw[0:], math.Float32bits(0))
	binary.LittleEndian.PutUint32(timesRaw[4:], math.Float32bits(1))
	weightsRaw := make([]byte, 4) // 2 normalized USHORT SCALAR morph weights: 0 -> 65535 (0.0 -> 1.0)
	binary.LittleEndian.PutUint16(weightsRaw[0:], 0)
	binary.LittleEndian.PutUint16(weightsRaw[2:], 65535)

	posComp := encodeMeshoptAttributesTest(posRaw, 3, 8)
	uvComp := encodeMeshoptAttributesTest(uvRaw, 3, 4)
	morphComp := encodeMeshoptAttributesTest(morphRaw, 3, 8)
	weightsComp := encodeMeshoptAttributesTest(weightsRaw, 1, 4) // 1x4-byte stride holding 2 uint16 scalars

	var side bytes.Buffer
	addChunk := func(b []byte) (int, int) {
		for side.Len()%4 != 0 {
			side.WriteByte(0)
		}
		off := side.Len()
		side.Write(b)
		return off, len(b)
	}
	posOff, posLen := addChunk(posComp)
	uvOff, uvLen := addChunk(uvComp)
	morphOff, morphLen := addChunk(morphComp)
	timesOff, timesLen := addChunk(timesRaw)
	wOff, wLen := addChunk(weightsComp)

	ext := func(off, clen, stride, count int) gltf.Extensions {
		raw, _ := json.Marshal(map[string]any{
			"buffer":     0,
			"byteOffset": off,
			"byteLength": clen,
			"byteStride": stride,
			"count":      count,
			"mode":       "ATTRIBUTES",
		})
		return gltf.Extensions{"EXT_meshopt_compression": json.RawMessage(raw)}
	}

	texBytes := pngTexture(t, color.NRGBA{40, 180, 220, 255})
	doc := &gltf.Document{
		Asset: gltf.Asset{Version: "2.0"},
		ExtensionsUsed: []string{
			"KHR_mesh_quantization",
			"EXT_meshopt_compression",
			"KHR_texture_transform",
		},
		ExtensionsRequired: []string{
			"KHR_mesh_quantization",
			"EXT_meshopt_compression",
		},
		Buffers: []*gltf.Buffer{
			{URI: "compressed.bin", ByteLength: side.Len()},
			{ByteLength: 256, Extensions: gltf.Extensions{"EXT_meshopt_compression": json.RawMessage(`{"fallback":true}`)}},
		},
		BufferViews: []*gltf.BufferView{
			{Buffer: 1, ByteLength: len(posRaw), ByteStride: 8, Extensions: ext(posOff, posLen, 8, 3)},
			{Buffer: 1, ByteLength: len(uvRaw), ByteStride: 4, Extensions: ext(uvOff, uvLen, 4, 3)},
			{Buffer: 1, ByteLength: len(morphRaw), ByteStride: 8, Extensions: ext(morphOff, morphLen, 8, 3)},
			{Buffer: 0, ByteOffset: timesOff, ByteLength: timesLen},
			{Buffer: 1, ByteLength: len(weightsRaw), Extensions: ext(wOff, wLen, 4, 1)},
		},
		Accessors: []*gltf.Accessor{
			{BufferView: gltf.Index(0), ComponentType: gltf.ComponentShort, Count: 3, Type: gltf.AccessorVec3},
			{BufferView: gltf.Index(1), ComponentType: gltf.ComponentUshort, Count: 3, Type: gltf.AccessorVec2},
			{BufferView: gltf.Index(2), ComponentType: gltf.ComponentShort, Count: 3, Type: gltf.AccessorVec3},
			{BufferView: gltf.Index(3), ComponentType: gltf.ComponentFloat, Count: 2, Type: gltf.AccessorScalar},
			{BufferView: gltf.Index(4), ComponentType: gltf.ComponentUshort, Normalized: true, Count: 2, Type: gltf.AccessorScalar},
		},
		Images:   []*gltf.Image{{URI: "albedo.png"}},
		Textures: []*gltf.Texture{{Source: gltf.Index(0)}},
		Materials: []*gltf.Material{{
			PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
				BaseColorTexture: &gltf.TextureInfo{
					Index: 0,
					Extensions: gltf.Extensions{
						"KHR_texture_transform": json.RawMessage(`{"offset":[0.25,0.5],"scale":[0.00025,0.00025]}`),
					},
				},
			},
		}},
		Meshes: []*gltf.Mesh{{
			Weights: []float64{0},
			Primitives: []*gltf.Primitive{{
				Attributes: gltf.PrimitiveAttributes{gltf.POSITION: 0, gltf.TEXCOORD_0: 1},
				Targets:    []gltf.PrimitiveAttributes{{gltf.POSITION: 2}},
				Material:   gltf.Index(0),
			}},
		}},
		Nodes: []*gltf.Node{{Scale: [3]float64{0.01, 0.01, 0.01}, Mesh: gltf.Index(0)}},
		Animations: []*gltf.Animation{{
			Samplers: []*gltf.AnimationSampler{{Input: 3, Output: 4, Interpolation: gltf.InterpolationLinear}},
			Channels: []*gltf.AnimationChannel{{Sampler: 0, Target: gltf.AnimationChannelTarget{Node: gltf.Index(0), Path: gltf.TRSWeights}}},
		}},
		Scenes: []*gltf.Scene{{Nodes: []int{0}}},
		Scene:  gltf.Index(0),
	}

	jsonBytes, err := json.Marshal(doc)
	check(t, err)
	files := map[string][]byte{
		"compressed.bin": side.Bytes(),
		"albedo.png":     texBytes,
	}
	m, err := Decode(jsonBytes, &nt3dmesh.Options{Open: opener(files)})
	check(t, err)
	if len(m.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", m.Warnings)
	}
	// UV vertex 1 had unnormalized USHORT (4000, 0) * scale (0.00025, 0.00025) + offset (0.25, 0.5) = (1.25, 0.5)!
	uv1 := m.Objects[0].Geometry.UV[1]
	if math.Abs(float64(uv1.U-1.25)) > 1e-4 || math.Abs(float64(uv1.V-0.5)) > 1e-4 {
		t.Fatalf("expected transformed quantized UV (1.25, 0.5), got %v", uv1)
	}
	// Step morph weight animation to t=1.0 (weight 65535 -> 1.0): glTF +Z (+50*0.01) maps to nt3d Y = -0.5!
	sc := nt3d.NewScene()
	inst, err := m.Asset.Instantiate(sc, nt3d.Root, "morph")
	check(t, err)
	anim, err := nt3d.NewAnimation(sc, inst.Clips[0])
	check(t, err)
	anim.Step(1.0)
	if math.Abs(float64(sc.Bounds().Min.Y+0.5)) > 1e-3 {
		t.Fatalf("expected morphed Y bound -0.5 at t=1.0, got %+v", sc.Bounds())
	}

	// 2. Invalid quantized NORMAL (unnormalized SHORT) and TANGENT (UBYTE) emit warnings and fall back cleanly!
	badNorm := make([]byte, 3*8)
	badTan := make([]byte, 3*4)
	rawWarn := slicesConcat(posRaw, uvRaw, badNorm, badTan)
	warnDoc := &gltf.Document{
		Asset:          gltf.Asset{Version: "2.0"},
		ExtensionsUsed: []string{"KHR_mesh_quantization"},
		Buffers:        []*gltf.Buffer{{ByteLength: len(rawWarn), Data: rawWarn}},
		BufferViews: []*gltf.BufferView{
			{Buffer: 0, ByteOffset: 0, ByteLength: len(posRaw), ByteStride: 8},
			{Buffer: 0, ByteOffset: len(posRaw), ByteLength: len(uvRaw), ByteStride: 4},
			{Buffer: 0, ByteOffset: len(posRaw) + len(uvRaw), ByteLength: len(badNorm), ByteStride: 8},
			{Buffer: 0, ByteOffset: len(posRaw) + len(uvRaw) + len(badNorm), ByteLength: len(badTan), ByteStride: 4},
		},
		Accessors: []*gltf.Accessor{
			{BufferView: gltf.Index(0), ComponentType: gltf.ComponentShort, Count: 3, Type: gltf.AccessorVec3},
			{BufferView: gltf.Index(1), ComponentType: gltf.ComponentUshort, Normalized: true, Count: 3, Type: gltf.AccessorVec2},
			{BufferView: gltf.Index(2), ComponentType: gltf.ComponentShort, Normalized: false, Count: 3, Type: gltf.AccessorVec3},
			{BufferView: gltf.Index(3), ComponentType: gltf.ComponentUbyte, Normalized: true, Count: 3, Type: gltf.AccessorVec4},
		},
		Images:   []*gltf.Image{{URI: "nrm.png"}},
		Textures: []*gltf.Texture{{Source: gltf.Index(0)}},
		Materials: []*gltf.Material{{
			NormalTexture: &gltf.NormalTexture{Index: gltf.Index(0)},
		}},
		Meshes: []*gltf.Mesh{{
			Primitives: []*gltf.Primitive{{
				Attributes: gltf.PrimitiveAttributes{
					gltf.POSITION:   0,
					gltf.TEXCOORD_0: 1,
					gltf.NORMAL:     2,
					gltf.TANGENT:    3,
				},
				Material: gltf.Index(0),
			}},
		}},
		Nodes:  []*gltf.Node{{Mesh: gltf.Index(0)}},
		Scenes: []*gltf.Scene{{Nodes: []int{0}}},
		Scene:  gltf.Index(0),
	}
	var warnGLB bytes.Buffer
	enc := gltf.NewEncoder(&warnGLB)
	enc.AsBinary = true
	check(t, enc.Encode(warnDoc))
	mWarn, err := Decode(warnGLB.Bytes(), &nt3dmesh.Options{Open: opener(map[string][]byte{"nrm.png": texBytes})})
	check(t, err)
	joined := strings.Join(mWarn.Warnings, "; ")
	if !strings.Contains(joined, "unreadable normals are computed") || !strings.Contains(joined, "unreadable tangents are generated") {
		t.Fatalf("expected normal and tangent fallback warnings, got %v", mWarn.Warnings)
	}
}

func TestQuantizationAndMeshoptErrors(t *testing.T) {
	t.Run("MeshoptMaxBytesLimit", func(t *testing.T) {
		raw := make([]byte, 64*12) // 768 bytes unpacked
		comp := encodeMeshoptAttributesTest(raw, 64, 12)
		extJSON, _ := json.Marshal(map[string]any{
			"buffer":     0,
			"byteOffset": 0,
			"byteLength": len(comp),
			"byteStride": 12,
			"count":      64,
			"mode":       "ATTRIBUTES",
		})
		doc := &gltf.Document{
			Asset:              gltf.Asset{Version: "2.0"},
			ExtensionsUsed:     []string{"EXT_meshopt_compression"},
			ExtensionsRequired: []string{"EXT_meshopt_compression"},
			Buffers: []*gltf.Buffer{
				{ByteLength: len(comp), Data: comp},
				{ByteLength: len(raw), Extensions: gltf.Extensions{"EXT_meshopt_compression": json.RawMessage(`{"fallback":true}`)}},
			},
			BufferViews: []*gltf.BufferView{{
				Buffer:     1,
				ByteLength: len(raw),
				ByteStride: 12,
				Extensions: gltf.Extensions{"EXT_meshopt_compression": json.RawMessage(extJSON)},
			}},
			Accessors: []*gltf.Accessor{{BufferView: gltf.Index(0), ComponentType: gltf.ComponentFloat, Count: 64, Type: gltf.AccessorVec3}},
			Meshes:    []*gltf.Mesh{{Primitives: []*gltf.Primitive{{Attributes: gltf.PrimitiveAttributes{gltf.POSITION: 0}}}}},
			Nodes:     []*gltf.Node{{Mesh: gltf.Index(0)}},
			Scenes:    []*gltf.Scene{{Nodes: []int{0}}},
			Scene:     gltf.Index(0),
		}
		var glb bytes.Buffer
		enc := gltf.NewEncoder(&glb)
		enc.AsBinary = true
		check(t, enc.Encode(doc))

		// Limit covers the .glb file itself, but NOT the 768-byte unpacked meshopt bufferView!
		_, err := Decode(glb.Bytes(), &nt3dmesh.Options{MaxBytes: int64(glb.Len() + 64)})
		if !errors.Is(err, nt3dmesh.ErrTooLarge) {
			t.Fatalf("expected ErrTooLarge, got %v", err)
		}
	})

	t.Run("UncompressedViewOnFallbackBufferRejected", func(t *testing.T) {
		js := `{"asset":{"version":"2.0"},"extensionsUsed":["EXT_meshopt_compression"],"buffers":[{"byteLength":36,"extensions":{"EXT_meshopt_compression":{"fallback":true}}}],"bufferViews":[{"buffer":0,"byteLength":36}],"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}],"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}],"nodes":[{"mesh":0}],"scenes":[{"nodes":[0]}],"scene":0}`
		if _, err := Decode([]byte(js), nil); err == nil {
			t.Fatal("expected error for uncompressed bufferView targeting empty fallback buffer")
		}
	})

	t.Run("FallbackBufferWithURIRejected", func(t *testing.T) {
		js := `{"asset":{"version":"2.0"},"extensionsUsed":["EXT_meshopt_compression"],"buffers":[{"uri":"side.bin","byteLength":36,"extensions":{"EXT_meshopt_compression":{"fallback":true}}}],"bufferViews":[{"buffer":0,"byteLength":36}],"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}],"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}],"nodes":[{"mesh":0}],"scenes":[{"nodes":[0]}],"scene":0}`
		_, err := Decode([]byte(js), &nt3dmesh.Options{Open: func(string) ([]byte, error) { return make([]byte, 36), nil }})
		if err == nil {
			t.Fatal("expected error for fallback buffer with URI")
		}
	})

	t.Run("InvalidFilterAndCorruptStreamsRejected", func(t *testing.T) {
		dst := make([]byte, 24)
		if err := decodeMeshoptBufferView(dst, []byte{0xa1}, 2, 12, "ATTRIBUTES", "NONE"); err == nil {
			t.Fatal("expected truncated/unsupported header error")
		}
		if err := decodeMeshoptBufferView(dst[:16], make([]byte, 40), 2, 8, "ATTRIBUTES", "UNKNOWN"); err == nil {
			t.Fatal("expected unsupported filter error")
		}
		if err := decodeMeshoptBufferView(dst[:24], make([]byte, 40), 2, 12, "ATTRIBUTES", "OCTAHEDRAL"); err == nil {
			t.Fatal("expected invalid OCTAHEDRAL stride error")
		}
		if err := decodeMeshoptBufferView(dst[:16], make([]byte, 40), 4, 4, "ATTRIBUTES", "QUATERNION"); err == nil {
			t.Fatal("expected invalid QUATERNION stride error")
		}
		if err := decodeMeshoptBufferView(dst[:6], make([]byte, 30), 3, 2, "TRIANGLES", "OCTAHEDRAL"); err == nil {
			t.Fatal("expected TRIANGLES filter error")
		}
		// Zero scale in OCTAHEDRAL must fail cleanly instead of producing NaN.
		zeroOct := encodeMeshoptAttributesTest([]byte{10, 10, 0, 0}, 1, 4)
		if err := decodeMeshoptBufferView(make([]byte, 4), zeroOct, 1, 4, "ATTRIBUTES", "OCTAHEDRAL"); err == nil {
			t.Fatal("expected zero OCTAHEDRAL scale error")
		}
		// Negative scale in QUATERNION must fail cleanly instead of producing NaN.
		negQuat := make([]byte, 8)
		negScale := int16(-8)
		binary.LittleEndian.PutUint16(negQuat[6:], uint16(negScale))
		negQuatComp := encodeMeshoptAttributesTest(negQuat, 1, 8)
		if err := decodeMeshoptBufferView(make([]byte, 8), negQuatComp, 1, 8, "ATTRIBUTES", "QUATERNION"); err == nil {
			t.Fatal("expected negative QUATERNION scale error")
		}
	})

	t.Run("UnnormalizedMorphNormalRejected", func(t *testing.T) {
		pos := make([]byte, 36)
		morphNorm := make([]byte, 24) // 3 * 8-byte SHORT unnormalized
		raw := append(pos, morphNorm...)
		doc := &gltf.Document{
			Asset:          gltf.Asset{Version: "2.0"},
			ExtensionsUsed: []string{"KHR_mesh_quantization"},
			Buffers:        []*gltf.Buffer{{ByteLength: len(raw), Data: raw}},
			BufferViews: []*gltf.BufferView{
				{Buffer: 0, ByteLength: 36},
				{Buffer: 0, ByteOffset: 36, ByteLength: 24, ByteStride: 8},
			},
			Accessors: []*gltf.Accessor{
				{BufferView: gltf.Index(0), ComponentType: gltf.ComponentFloat, Count: 3, Type: gltf.AccessorVec3},
				{BufferView: gltf.Index(1), ComponentType: gltf.ComponentShort, Normalized: false, Count: 3, Type: gltf.AccessorVec3},
			},
			Meshes: []*gltf.Mesh{{
				Weights: []float64{1},
				Primitives: []*gltf.Primitive{{
					Attributes: gltf.PrimitiveAttributes{gltf.POSITION: 0},
					Targets:    []gltf.PrimitiveAttributes{{gltf.NORMAL: 1}},
				}},
			}},
			Nodes:  []*gltf.Node{{Mesh: gltf.Index(0)}},
			Scenes: []*gltf.Scene{{Nodes: []int{0}}},
			Scene:  gltf.Index(0),
		}
		var glb bytes.Buffer
		enc := gltf.NewEncoder(&glb)
		enc.AsBinary = true
		check(t, enc.Encode(doc))
		if _, err := Decode(glb.Bytes(), nil); err == nil {
			t.Fatal("expected error for unnormalized integer morph NORMAL")
		}
	})
}

func slicesConcat(slices ...[]byte) []byte {
	var out []byte
	for _, s := range slices {
		out = append(out, s...)
	}
	return out
}
