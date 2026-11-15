// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/qmuntal/gltf"

	"nimbleterminal.dev/nt3d/mesh"
)

const (
	glbMagic     = 0x46546C67 // "glTF"
	glbChunkJSON = 0x4E4F534A // "JSON"
	glbChunkBIN  = 0x004E4942 // "BIN\x00"
)

type meshoptBufferExt struct {
	Fallback bool `json:"fallback"`
}

type meshoptBufferViewExt struct {
	Buffer     *int   `json:"buffer"`
	ByteOffset int    `json:"byteOffset"`
	ByteLength *int   `json:"byteLength"`
	ByteStride *int   `json:"byteStride"`
	Count      *int   `json:"count"`
	Mode       string `json:"mode"`
	Filter     string `json:"filter"`
}

// decodeContainer parses a .gltf JSON or .glb binary container and loads raw
// buffers while allowing EXT_meshopt_compression dummy fallback buffers (which
// intentionally omit buffer.uri per the Khronos specification).
func (d *gltfDecoder) decodeContainer(data []byte) error {
	var isBinary bool
	var rest []byte
	if len(data) >= 4 && binary.LittleEndian.Uint32(data[0:4]) == glbMagic {
		if len(data) < 20 {
			return errors.New("nt3dmesh: glTF: truncated GLB header")
		}
		version := binary.LittleEndian.Uint32(data[4:8])
		totalLen := binary.LittleEndian.Uint32(data[8:12])
		jsonLen := binary.LittleEndian.Uint32(data[12:16])
		jsonType := binary.LittleEndian.Uint32(data[16:20])
		if version != 2 || jsonType != glbChunkJSON || int64(jsonLen)+20 > int64(totalLen) {
			return errors.New("nt3dmesh: glTF: invalid GLB JSON header")
		}
		if int64(jsonLen)+20 > int64(len(data)) {
			return fmt.Errorf("nt3dmesh: glTF: %w", io.ErrUnexpectedEOF)
		}
		if err := json.NewDecoder(bytes.NewReader(data[20 : 20+int(jsonLen)])).Decode(d.doc); err != nil {
			return fmt.Errorf("nt3dmesh: glTF: %w", err)
		}
		end := len(data)
		if int64(totalLen) < int64(end) {
			end = int(totalLen)
		}
		rest = data[20+int(jsonLen) : end]
		isBinary = true
	} else {
		if err := json.NewDecoder(bytes.NewReader(data)).Decode(d.doc); err != nil {
			return fmt.Errorf("nt3dmesh: glTF: %w", err)
		}
	}

	externalStart := 0
	if isBinary && len(d.doc.Buffers) > 0 {
		b0 := d.doc.Buffers[0]
		if b0 == nil {
			return errors.New("nt3dmesh: nil glTF buffer 0")
		}
		var ext0 meshoptBufferExt
		ok0, err := decodeExtension(b0.Extensions, "EXT_meshopt_compression", &ext0)
		if err != nil {
			return err
		}
		if b0.URI == "" && !(ok0 && ext0.Fallback && len(rest) == 0) {
			externalStart = 1
			if ok0 && ext0.Fallback {
				return errors.New("nt3dmesh: EXT_meshopt_compression fallback buffer 0 cannot own GLB BIN chunk")
			}
			if b0.ByteLength <= 0 {
				return errors.New("nt3dmesh: glTF: invalid buffer.byteLength")
			}
			if len(rest) < 8 {
				return fmt.Errorf("nt3dmesh: glTF: %w", io.ErrUnexpectedEOF)
			}
			binLen := binary.LittleEndian.Uint32(rest[0:4])
			binType := binary.LittleEndian.Uint32(rest[4:8])
			if int64(b0.ByteLength) > math.MaxUint32 || binType != glbChunkBIN ||
				int64(binLen) < int64(b0.ByteLength) || int64(binLen) > int64(b0.ByteLength)+3 {
				return errors.New("nt3dmesh: glTF: invalid GLB BIN header")
			}
			if int64(len(rest))-8 < int64(b0.ByteLength) {
				return fmt.Errorf("nt3dmesh: glTF: %w", io.ErrUnexpectedEOF)
			}
			b0.Data = rest[8 : 8+b0.ByteLength : 8+b0.ByteLength]
		}
	}

	for i := externalStart; i < len(d.doc.Buffers); i++ {
		b := d.doc.Buffers[i]
		if b == nil {
			return fmt.Errorf("nt3dmesh: nil glTF buffer %d", i)
		}
		if b.ByteLength <= 0 {
			return errors.New("nt3dmesh: glTF: invalid buffer.byteLength")
		}
		var ext meshoptBufferExt
		ok, err := decodeExtension(b.Extensions, "EXT_meshopt_compression", &ext)
		if err != nil {
			return err
		}
		if ok && ext.Fallback {
			if b.URI != "" {
				return fmt.Errorf("nt3dmesh: EXT_meshopt_compression fallback buffer %d must not define a URI", i)
			}
			continue
		}
		if b.URI == "" {
			return errors.New("nt3dmesh: glTF: buffer without URI")
		}
		if b.IsEmbeddedResource() {
			raw, err := decodeDataURIBuffer(b.URI)
			if err != nil {
				return fmt.Errorf("nt3dmesh: glTF: %w", err)
			}
			if len(raw) > b.ByteLength {
				raw = raw[:b.ByteLength:b.ByteLength]
			}
			b.Data = raw
			continue
		}
		uri, err := sanitizeBufferURI(b.URI)
		if err != nil {
			return fmt.Errorf("nt3dmesh: glTF: %w", err)
		}
		raw, err := d.files.ReadFile(uri)
		if err != nil {
			if errors.Is(err, nt3dmesh.ErrTooLarge) {
				return err
			}
			return fmt.Errorf("nt3dmesh: glTF: %w", err)
		}
		if len(raw) > b.ByteLength {
			raw = raw[:b.ByteLength:b.ByteLength]
		}
		b.Data = raw
	}
	return nil
}

func decodeDataURIBuffer(uri string) ([]byte, error) {
	for _, prefix := range [...]string{
		"data:application/octet-stream;base64",
		"data:application/gltf-buffer;base64",
	} {
		if strings.HasPrefix(uri, prefix) {
			pos := len(prefix) + 1
			if len(uri) < pos || uri[len(prefix)] != ',' {
				return nil, errors.New("invalid base64 buffer URI")
			}
			raw, err := base64.StdEncoding.DecodeString(uri[pos:])
			if err != nil || len(raw) == 0 {
				if err != nil {
					return nil, err
				}
				return nil, errors.New("empty base64 buffer URI")
			}
			return raw, nil
		}
	}
	return nil, errors.New("unsupported embedded buffer URI")
}

func sanitizeBufferURI(raw string) (string, error) {
	uri := raw
	s := strings.ReplaceAll(raw, `\`, "/")
	s = strings.ReplaceAll(s, "/./", "/")
	s = strings.TrimPrefix(s, "./")
	if u, err := url.Parse(s); err == nil {
		if u.Scheme == "" {
			if unescaped, uerr := url.PathUnescape(s); uerr == nil {
				uri = unescaped
			}
		} else {
			uri = u.String()
		}
	}
	if u, err := url.Parse(uri); err == nil && u.Scheme != "" {
		return uri, nil
	}
	if !filepath.IsLocal(uri) {
		return "", fmt.Errorf("invalid buffer.uri %q", raw)
	}
	return uri, nil
}

// decompressBuffers validates raw glTF buffers, honoring EXT_meshopt_compression
// fallback buffers and decompressing compressed bufferViews within Options.MaxBytes.
func (d *gltfDecoder) decompressBuffers() error {
	origBuffers := len(d.doc.Buffers)
	fallback := make([]bool, origBuffers)
	for i, b := range d.doc.Buffers {
		if b == nil {
			return fmt.Errorf("nt3dmesh: nil glTF buffer %d", i)
		}
		var ext meshoptBufferExt
		ok, err := decodeExtension(b.Extensions, "EXT_meshopt_compression", &ext)
		if err != nil {
			return err
		}
		if ok && ext.Fallback {
			if b.URI != "" {
				return fmt.Errorf("nt3dmesh: EXT_meshopt_compression fallback buffer %d must not define a URI", i)
			}
			fallback[i] = true
			continue
		}
		if len(b.Data) < b.ByteLength {
			return fmt.Errorf("nt3dmesh: glTF buffer %d is missing or short", i)
		}
	}

	for i, bv := range d.doc.BufferViews {
		if bv == nil {
			return fmt.Errorf("nt3dmesh: nil glTF bufferView %d", i)
		}
		var ext meshoptBufferViewExt
		ok, err := decodeExtension(bv.Extensions, "EXT_meshopt_compression", &ext)
		if err != nil {
			return err
		}
		if !ok {
			if bv.Buffer >= 0 && bv.Buffer < origBuffers && fallback[bv.Buffer] {
				return fmt.Errorf("nt3dmesh: glTF buffer %d is missing or short", bv.Buffer)
			}
			continue
		}
		if ext.Buffer == nil || ext.ByteLength == nil || ext.ByteStride == nil || ext.Count == nil {
			return fmt.Errorf("nt3dmesh: EXT_meshopt_compression bufferView %d misses required properties", i)
		}
		bufIdx := *ext.Buffer
		if bufIdx < 0 || bufIdx >= origBuffers || fallback[bufIdx] {
			return fmt.Errorf("nt3dmesh: EXT_meshopt_compression bufferView %d names invalid source buffer %d", i, bufIdx)
		}
		compLen := *ext.ByteLength
		stride := *ext.ByteStride
		count := *ext.Count
		if ext.ByteOffset < 0 || compLen <= 0 || stride <= 0 || count <= 0 {
			return fmt.Errorf("nt3dmesh: EXT_meshopt_compression bufferView %d has invalid range or count", i)
		}
		srcBuf := d.doc.Buffers[bufIdx]
		if int64(ext.ByteOffset)+int64(compLen) > int64(srcBuf.ByteLength) || int64(ext.ByteOffset)+int64(compLen) > int64(len(srcBuf.Data)) {
			return fmt.Errorf("nt3dmesh: EXT_meshopt_compression bufferView %d exceeds source buffer %d", i, bufIdx)
		}
		unpacked := int64(count) * int64(stride)
		if unpacked <= 0 || int64(bv.ByteLength) != unpacked {
			return fmt.Errorf("nt3dmesh: EXT_meshopt_compression bufferView %d byteLength %d does not match count*byteStride (%d)", i, bv.ByteLength, unpacked)
		}
		if bv.ByteStride != 0 && bv.ByteStride != stride {
			return fmt.Errorf("nt3dmesh: EXT_meshopt_compression bufferView %d byteStride %d does not match %d", i, bv.ByteStride, stride)
		}
		if *d.files.left -= unpacked; *d.files.left < 0 {
			return fmt.Errorf("%w: EXT_meshopt_compression bufferView", nt3dmesh.ErrTooLarge)
		}
		dst := make([]byte, int(unpacked))
		src := srcBuf.Data[ext.ByteOffset : ext.ByteOffset+compLen]
		if err := decodeMeshoptBufferView(dst, src, count, stride, ext.Mode, ext.Filter); err != nil {
			return fmt.Errorf("nt3dmesh: EXT_meshopt_compression bufferView %d: %w", i, err)
		}
		d.doc.Buffers = append(d.doc.Buffers, &gltf.Buffer{
			ByteLength: len(dst),
			Data:       dst,
		})
		bv.Buffer = len(d.doc.Buffers) - 1
		bv.ByteOffset = 0
		bv.ByteLength = len(dst)
	}
	return nil
}

func decodeMeshoptBufferView(dst, src []byte, count, stride int, mode, filter string) error {
	if filter == "" {
		filter = "NONE"
	}
	switch mode {
	case "ATTRIBUTES":
		if stride < 4 || stride > 256 || stride%4 != 0 {
			return fmt.Errorf("invalid ATTRIBUTES byteStride %d", stride)
		}
		switch filter {
		case "NONE":
		case "OCTAHEDRAL":
			if stride != 4 && stride != 8 {
				return fmt.Errorf("OCTAHEDRAL filter requires byteStride 4 or 8, got %d", stride)
			}
		case "QUATERNION":
			if stride != 8 {
				return fmt.Errorf("QUATERNION filter requires byteStride 8, got %d", stride)
			}
		case "EXPONENTIAL":
		default:
			return fmt.Errorf("unsupported filter %q", filter)
		}
		if err := decodeMeshoptAttributes(dst, src, count, stride); err != nil {
			return err
		}
		switch filter {
		case "OCTAHEDRAL":
			return applyMeshoptFilterOct(dst, count, stride)
		case "QUATERNION":
			return applyMeshoptFilterQuat(dst, count)
		case "EXPONENTIAL":
			applyMeshoptFilterExp(dst)
		}
		return nil
	case "TRIANGLES":
		if filter != "NONE" {
			return fmt.Errorf("TRIANGLES mode does not allow filter %q", filter)
		}
		if stride != 2 && stride != 4 {
			return fmt.Errorf("invalid TRIANGLES byteStride %d", stride)
		}
		if count%3 != 0 {
			return fmt.Errorf("TRIANGLES count %d is not a multiple of 3", count)
		}
		return decodeMeshoptTriangles(dst, src, count, stride)
	case "INDICES":
		if filter != "NONE" {
			return fmt.Errorf("INDICES mode does not allow filter %q", filter)
		}
		if stride != 2 && stride != 4 {
			return fmt.Errorf("invalid INDICES byteStride %d", stride)
		}
		return decodeMeshoptIndices(dst, src, count, stride)
	default:
		return fmt.Errorf("unsupported mode %q", mode)
	}
}

func decodeMeshoptAttributes(dst, src []byte, count, stride int) error {
	tailSize := stride
	tailSizePadded := max(tailSize, 32)
	if len(src) < 1+tailSizePadded {
		return errors.New("truncated ATTRIBUTES stream")
	}
	if src[0] != 0xa0 {
		return fmt.Errorf("unsupported ATTRIBUTES header 0x%02x", src[0])
	}
	var lastVertex [256]byte
	copy(lastVertex[:stride], src[len(src)-tailSize:])

	maxBlock := min((8192/stride)&^15, 256)
	pos := 1
	dataEnd := len(src) - tailSizePadded
	var blockBuf [256]byte

	for offset := 0; offset < count; {
		blockCount := min(count-offset, maxBlock)
		groups := (blockCount + 15) / 16
		headerBytes := (groups + 3) / 4

		for k := range stride {
			if pos+headerBytes > dataEnd {
				return errors.New("truncated ATTRIBUTES group header")
			}
			hdr := src[pos : pos+headerBytes]
			pos += headerBytes

			for g := range groups {
				mode := (hdr[g>>2] >> ((g & 3) * 2)) & 3
				base := g * 16
				switch mode {
				case 0:
					clear(blockBuf[base : base+16])
				case 1:
					if pos+4 > dataEnd {
						return errors.New("truncated ATTRIBUTES 2-bit group")
					}
					packed := src[pos : pos+4]
					dataVar := pos + 4
					for bi := range 4 {
						b := packed[bi]
						for j := range 4 {
							enc := b >> 6
							b <<= 2
							if enc == 3 {
								if dataVar >= dataEnd {
									return errors.New("truncated ATTRIBUTES 2-bit sentinel")
								}
								enc = src[dataVar]
								dataVar++
							}
							blockBuf[base+bi*4+j] = enc
						}
					}
					pos = dataVar
				case 2:
					if pos+8 > dataEnd {
						return errors.New("truncated ATTRIBUTES 4-bit group")
					}
					packed := src[pos : pos+8]
					dataVar := pos + 8
					for bi := range 8 {
						b := packed[bi]
						for j := range 2 {
							enc := b >> 4
							b <<= 4
							if enc == 15 {
								if dataVar >= dataEnd {
									return errors.New("truncated ATTRIBUTES 4-bit sentinel")
								}
								enc = src[dataVar]
								dataVar++
							}
							blockBuf[base+bi*2+j] = enc
						}
					}
					pos = dataVar
				case 3:
					if pos+16 > dataEnd {
						return errors.New("truncated ATTRIBUTES literal group")
					}
					copy(blockBuf[base:base+16], src[pos:pos+16])
					pos += 16
				}
			}

			prev := lastVertex[k]
			for i := range blockCount {
				v := blockBuf[i]
				d := byte(int8(v>>1) ^ -int8(v&1))
				prev += d
				dst[(offset+i)*stride+k] = prev
			}
			lastVertex[k] = prev
		}
		offset += blockCount
	}
	if pos != dataEnd {
		return errors.New("corrupt ATTRIBUTES stream length")
	}
	return nil
}

func decodeMeshoptTriangles(dst, src []byte, count, stride int) error {
	triCount := count / 3
	if len(src) < 1+triCount+16 {
		return errors.New("truncated TRIANGLES stream")
	}
	if (src[0] & 0xf0) != 0xe0 {
		return fmt.Errorf("unsupported TRIANGLES header 0x%02x", src[0])
	}
	version := src[0] & 0x0f
	if version > 1 {
		return fmt.Errorf("unsupported TRIANGLES version %d", version)
	}
	fecmax := byte(15)
	if version >= 1 {
		fecmax = 13
	}

	codes := src[1 : 1+triCount]
	data := src[1+triCount : len(src)-16]
	codeaux := src[len(src)-16:]
	dataPos := 0

	var edgefifo [16][2]uint32
	var vertexfifo [16]uint32
	edgeOffset := 0
	vertexOffset := 0
	var next, last uint32

	pushEdge := func(a, b uint32) {
		edgefifo[edgeOffset] = [2]uint32{a, b}
		edgeOffset = (edgeOffset + 1) & 15
	}
	pushVertex := func(v uint32) {
		vertexfifo[vertexOffset] = v
		vertexOffset = (vertexOffset + 1) & 15
	}
	decodeIndex := func() (uint32, bool) {
		v, ok := readMeshoptVByte(data, &dataPos)
		if !ok {
			return 0, false
		}
		d := (v >> 1) ^ uint32(-int32(v&1))
		last += d
		return last, true
	}

	for i, code := range codes {
		var a, b, c uint32
		if code < 0xf0 {
			e := edgefifo[(edgeOffset-1-int(code>>4))&15]
			a, b = e[0], e[1]
			y := code & 0x0f
			switch {
			case y == 0:
				c = next
				next++
				pushEdge(c, b)
				pushEdge(a, c)
				pushVertex(c)
			case y < fecmax:
				c = vertexfifo[(vertexOffset-int(y))&15]
				pushEdge(c, b)
				pushEdge(a, c)
			case y == 13:
				last--
				c = last
				pushEdge(c, b)
				pushEdge(a, c)
				pushVertex(c)
			case y == 14:
				last++
				c = last
				pushEdge(c, b)
				pushEdge(a, c)
				pushVertex(c)
			default:
				var ok bool
				if c, ok = decodeIndex(); !ok {
					return errors.New("truncated TRIANGLES index varint")
				}
				pushEdge(c, b)
				pushEdge(a, c)
				pushVertex(c)
			}
		} else if code < 0xfe {
			aux := codeaux[code&0x0f]
			z, w := aux>>4, aux&0x0f
			a = next
			next++
			if z == 0 {
				b = next
				next++
			} else {
				b = vertexfifo[(vertexOffset-int(z))&15]
			}
			if w == 0 {
				c = next
				next++
			} else {
				c = vertexfifo[(vertexOffset-int(w))&15]
			}
			pushEdge(b, a)
			pushEdge(c, b)
			pushEdge(a, c)
			pushVertex(a)
			if z == 0 {
				pushVertex(b)
			}
			if w == 0 {
				pushVertex(c)
			}
		} else {
			if dataPos >= len(data) {
				return errors.New("truncated TRIANGLES extra control byte")
			}
			aux := data[dataPos]
			dataPos++
			if aux == 0 {
				next = 0
			}
			z, w := aux>>4, aux&0x0f
			if code == 0xfe {
				a = next
				next++
			} else {
				var ok bool
				if a, ok = decodeIndex(); !ok {
					return errors.New("truncated TRIANGLES explicit a")
				}
			}
			switch {
			case z == 0:
				b = next
				next++
			case z < 15:
				b = vertexfifo[(vertexOffset-int(z))&15]
			default:
				var ok bool
				if b, ok = decodeIndex(); !ok {
					return errors.New("truncated TRIANGLES explicit b")
				}
			}
			switch {
			case w == 0:
				c = next
				next++
			case w < 15:
				c = vertexfifo[(vertexOffset-int(w))&15]
			default:
				var ok bool
				if c, ok = decodeIndex(); !ok {
					return errors.New("truncated TRIANGLES explicit c")
				}
			}
			pushEdge(b, a)
			pushEdge(c, b)
			pushEdge(a, c)
			pushVertex(a)
			if z == 0 || z == 15 {
				pushVertex(b)
			}
			if w == 0 || w == 15 {
				pushVertex(c)
			}
		}

		if stride == 2 {
			off := i * 6
			binary.LittleEndian.PutUint16(dst[off+0:], uint16(a))
			binary.LittleEndian.PutUint16(dst[off+2:], uint16(b))
			binary.LittleEndian.PutUint16(dst[off+4:], uint16(c))
		} else {
			off := i * 12
			binary.LittleEndian.PutUint32(dst[off+0:], a)
			binary.LittleEndian.PutUint32(dst[off+4:], b)
			binary.LittleEndian.PutUint32(dst[off+8:], c)
		}
	}
	if dataPos != len(data) {
		return errors.New("corrupt TRIANGLES stream length")
	}
	return nil
}

func decodeMeshoptIndices(dst, src []byte, count, stride int) error {
	if len(src) < 1+4 {
		return errors.New("truncated INDICES stream")
	}
	if (src[0] & 0xf0) != 0xd0 {
		return fmt.Errorf("unsupported INDICES header 0x%02x", src[0])
	}
	version := src[0] & 0x0f
	if version > 1 {
		return fmt.Errorf("unsupported INDICES version %d", version)
	}
	data := src[1 : len(src)-4]
	dataPos := 0
	var last [2]uint32

	for i := range count {
		v, ok := readMeshoptVByte(data, &dataPos)
		if !ok {
			return errors.New("truncated INDICES varint")
		}
		b := v & 1
		v >>= 1
		d := (v >> 1) ^ uint32(-int32(v&1))
		idx := last[b] + d
		last[b] = idx
		if stride == 2 {
			binary.LittleEndian.PutUint16(dst[i*2:], uint16(idx))
		} else {
			binary.LittleEndian.PutUint32(dst[i*4:], idx)
		}
	}
	if dataPos != len(data) {
		return errors.New("corrupt INDICES stream length")
	}
	return nil
}

func readMeshoptVByte(data []byte, pos *int) (uint32, bool) {
	if *pos >= len(data) {
		return 0, false
	}
	lead := data[*pos]
	*pos++
	if lead < 128 {
		return uint32(lead), true
	}
	res := uint32(lead & 127)
	shift := uint(7)
	for range 4 {
		if *pos >= len(data) {
			return 0, false
		}
		g := data[*pos]
		*pos++
		res |= uint32(g&127) << shift
		shift += 7
		if g < 128 {
			return res, true
		}
	}
	return res, true
}

func applyMeshoptFilterOct(dst []byte, count, stride int) error {
	if stride == 4 {
		for i := range count {
			off := i * 4
			in0 := int8(dst[off+0])
			in1 := int8(dst[off+1])
			in2 := int8(dst[off+2])
			if in2 <= 0 {
				return errors.New("invalid OCTAHEDRAL scale")
			}
			x, y, z := decodeOctUnit(float32(in0), float32(in1), float32(in2))
			dst[off+0] = byte(roundSnorm8(x))
			dst[off+1] = byte(roundSnorm8(y))
			dst[off+2] = byte(roundSnorm8(z))
		}
		return nil
	}
	for i := range count {
		off := i * 8
		in0 := int16(binary.LittleEndian.Uint16(dst[off+0:]))
		in1 := int16(binary.LittleEndian.Uint16(dst[off+2:]))
		in2 := int16(binary.LittleEndian.Uint16(dst[off+4:]))
		if in2 <= 0 {
			return errors.New("invalid OCTAHEDRAL scale")
		}
		x, y, z := decodeOctUnit(float32(in0), float32(in1), float32(in2))
		binary.LittleEndian.PutUint16(dst[off+0:], uint16(roundSnorm16(x)))
		binary.LittleEndian.PutUint16(dst[off+2:], uint16(roundSnorm16(y)))
		binary.LittleEndian.PutUint16(dst[off+4:], uint16(roundSnorm16(z)))
	}
	return nil
}

func decodeOctUnit(in0, in1, one float32) (float32, float32, float32) {
	x := in0 / one
	y := in1 / one
	z := 1 - float32(math.Abs(float64(x))) - float32(math.Abs(float64(y)))
	t := min(z, 0)
	if x >= 0 {
		x += t
	} else {
		x -= t
	}
	if y >= 0 {
		y += t
	} else {
		y -= t
	}
	l := float32(math.Sqrt(float64(x*x + y*y + z*z)))
	if l > 0 {
		inv := 1 / l
		x *= inv
		y *= inv
		z *= inv
	}
	return x, y, z
}

func applyMeshoptFilterQuat(dst []byte, count int) error {
	const invSqrt2 = float32(1.0 / math.Sqrt2)
	for i := range count {
		off := i * 8
		in0 := int16(binary.LittleEndian.Uint16(dst[off+0:]))
		in1 := int16(binary.LittleEndian.Uint16(dst[off+2:]))
		in2 := int16(binary.LittleEndian.Uint16(dst[off+4:]))
		in3 := int16(binary.LittleEndian.Uint16(dst[off+6:]))
		oneInt := in3 | 3
		if oneInt <= 0 {
			return errors.New("invalid QUATERNION scale")
		}
		sf := invSqrt2 / float32(oneInt)
		x := float32(in0) * sf
		y := float32(in1) * sf
		z := float32(in2) * sf
		ww := max(float32(0), 1-x*x-y*y-z*z)
		w := float32(math.Sqrt(float64(ww)))

		mc := int(in3 & 3)
		var out [4]int16
		out[(mc+1)&3] = roundSnorm16(x)
		out[(mc+2)&3] = roundSnorm16(y)
		out[(mc+3)&3] = roundSnorm16(z)
		out[(mc+0)&3] = roundSnorm16(w)
		binary.LittleEndian.PutUint16(dst[off+0:], uint16(out[0]))
		binary.LittleEndian.PutUint16(dst[off+2:], uint16(out[1]))
		binary.LittleEndian.PutUint16(dst[off+4:], uint16(out[2]))
		binary.LittleEndian.PutUint16(dst[off+6:], uint16(out[3]))
	}
	return nil
}

func applyMeshoptFilterExp(dst []byte) {
	for off := 0; off+4 <= len(dst); off += 4 {
		v := int32(binary.LittleEndian.Uint32(dst[off:]))
		e := int(v >> 24)
		m := (v << 8) >> 8
		f := float32(math.Ldexp(float64(m), e))
		binary.LittleEndian.PutUint32(dst[off:], math.Float32bits(f))
	}
}

func roundSnorm8(v float32) int8 {
	r := math.Round(float64(v) * 127)
	return int8(max(-127, min(127, r)))
}

func roundSnorm16(v float32) int16 {
	r := math.Round(float64(v) * 32767)
	return int16(max(-32767, min(32767, r)))
}
