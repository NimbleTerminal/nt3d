// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
	"math"

	"github.com/klauspost/compress/zstd"

	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

var ktx2Magic = [12]byte{0xAB, 'K', 'T', 'X', ' ', '2', '0', 0xBB, '\r', '\n', 0x1A, '\n'}

const (
	ktx2SupercompNone    = 0
	ktx2SupercompBasisLZ = 1
	ktx2SupercompZstd    = 2
	ktx2SupercompZlib    = 3

	khrDFModelRGBSDA = 1
	khrDFModelETC1S  = 163
	khrDFModelUASTC  = 166

	khrDFTransferLinear = 1
	khrDFTransferSRGB   = 2

	khrDFFlagAlphaPremultiplied = 1

	khrDFChannelETC1SRGB = 0
	khrDFChannelETC1SGGG = 3
	khrDFChannelETC1SAAA = 15

	khrDFChannelUASTCRGB  = 0
	khrDFChannelUASTCRGBA = 3
	khrDFChannelUASTCRRR  = 4
	khrDFChannelUASTCRRRG = 5

	vkFormatUndefined   = 0
	vkFormatR8Unorm     = 9
	vkFormatR8SRGB      = 15
	vkFormatR8G8Unorm   = 16
	vkFormatR8G8SRGB    = 22
	vkFormatR8G8B8Unorm = 23
	vkFormatR8G8B8SRGB  = 29
	vkFormatR8G8B8A8Un  = 37
	vkFormatR8G8B8A8SR  = 43
	vkFormatB8G8R8A8Un  = 44
	vkFormatB8G8R8A8SR  = 50
)

type ktx2DFD struct {
	colorModel uint8
	transfer   uint8
	flags      uint8
	channels   []uint8
}

func isKTX2(data []byte) bool {
	return len(data) >= len(ktx2Magic) && bytes.Equal(data[:len(ktx2Magic)], ktx2Magic[:])
}

// decodeKTX2 decodes mip level 0 (layer 0, face 0) of a KTX 2.0 container into
// straight-alpha 8-bit NRGBA pixels. When twoChannelXY is true, the source
// stored two channels (such as UASTC RRRG, ETC1S RRR+GGG, or R8G8) so normal
// maps can reconstruct tangent Z before normalization.
func decodeKTX2(data []byte, left *int64) (*image.NRGBA, bool, error) {
	if len(data) < 80+24 || !isKTX2(data) {
		return nil, false, errors.New("invalid KTX2 header")
	}
	vkFormat := binary.LittleEndian.Uint32(data[12:16])
	pixelWidth := binary.LittleEndian.Uint32(data[20:24])
	pixelHeight := binary.LittleEndian.Uint32(data[24:28])
	pixelDepth := binary.LittleEndian.Uint32(data[28:32])
	layerCount := binary.LittleEndian.Uint32(data[32:36])
	faceCount := binary.LittleEndian.Uint32(data[36:40])
	levelCount := binary.LittleEndian.Uint32(data[40:44])
	supercomp := binary.LittleEndian.Uint32(data[44:48])
	dfdOffset := binary.LittleEndian.Uint32(data[48:52])
	dfdLength := binary.LittleEndian.Uint32(data[52:56])
	sgdOffset := binary.LittleEndian.Uint64(data[64:72])
	sgdLength := binary.LittleEndian.Uint64(data[72:80])

	if pixelWidth == 0 || pixelWidth > maxTextureSide || pixelHeight > maxTextureSide {
		return nil, false, fmt.Errorf("KTX2 dimensions %dx%d exceed limits", pixelWidth, pixelHeight)
	}
	if pixelDepth > 1 {
		return nil, false, errors.New("3D KTX2 textures are not supported")
	}
	if faceCount != 1 && faceCount != 6 {
		return nil, false, fmt.Errorf("invalid KTX2 faceCount %d", faceCount)
	}
	width := int(pixelWidth)
	height := int(pixelHeight)
	if height == 0 {
		height = 1
	}
	levels := max(uint32(1), levelCount)
	if levels > 32 || uint64(80)+uint64(levels)*24 > uint64(len(data)) {
		return nil, false, errors.New("invalid KTX2 level index")
	}
	layers := max(uint32(1), layerCount)
	if layers > 4096 {
		return nil, false, errors.New("excessive KTX2 layerCount")
	}

	lvl0Off := binary.LittleEndian.Uint64(data[80:88])
	lvl0Len := binary.LittleEndian.Uint64(data[88:96])
	lvl0Uncomp := binary.LittleEndian.Uint64(data[96:104])
	if lvl0Off > uint64(len(data)) || lvl0Len > uint64(len(data))-lvl0Off {
		return nil, false, errors.New("KTX2 level 0 out of bounds")
	}
	level0 := data[lvl0Off : lvl0Off+lvl0Len]

	if uint64(dfdOffset) > uint64(len(data)) || uint64(dfdLength) > uint64(len(data))-uint64(dfdOffset) {
		return nil, false, errors.New("KTX2 DFD out of bounds")
	}
	dfd, err := parseKTX2DFD(data[dfdOffset : dfdOffset+dfdLength])
	if err != nil {
		return nil, false, err
	}

	rgbaBytes := int64(width) * int64(height) * 4
	if left != nil {
		if rgbaBytes > *left {
			return nil, false, nt3dmesh.ErrTooLarge
		}
		*left -= rgbaBytes
	}

	if supercomp == ktx2SupercompBasisLZ {
		if vkFormat != vkFormatUndefined || dfd.colorModel != khrDFModelETC1S {
			return nil, false, errors.New("KTX2 BasisLZ requires VK_FORMAT_UNDEFINED and ETC1S DFD")
		}
		if sgdLength < 20 || sgdOffset > uint64(len(data)) || sgdLength > uint64(len(data))-sgdOffset {
			return nil, false, errors.New("KTX2 BasisLZ global data out of bounds")
		}
		sgd := data[sgdOffset : sgdOffset+sgdLength]
		imageCount := uint64(levels) * uint64(layers) * uint64(faceCount)
		return decodeKTX2ETC1S(level0, sgd, imageCount, width, height, dfd, left)
	}

	var raw []byte
	switch supercomp {
	case ktx2SupercompNone:
		if lvl0Uncomp != 0 && lvl0Uncomp != lvl0Len {
			return nil, false, errors.New("KTX2 uncompressed byte length mismatch")
		}
		raw = level0
	case ktx2SupercompZstd, ktx2SupercompZlib:
		maxAllowed := uint64(max(rgbaBytes*int64(layers)*int64(faceCount)*2, 64*1024))
		if lvl0Uncomp == 0 || lvl0Uncomp > maxAllowed {
			if left != nil && lvl0Uncomp > uint64(max(int64(0), *left)) {
				return nil, false, nt3dmesh.ErrTooLarge
			}
			return nil, false, errors.New("invalid KTX2 uncompressedByteLength")
		}
		if left != nil {
			if int64(lvl0Uncomp) > *left {
				return nil, false, nt3dmesh.ErrTooLarge
			}
			*left -= int64(lvl0Uncomp)
		}
		raw, err = decompressKTX2Level(level0, int(lvl0Uncomp), supercomp)
		if err != nil {
			return nil, false, err
		}
	default:
		return nil, false, fmt.Errorf("unsupported KTX2 supercompression scheme %d", supercomp)
	}

	var out *image.NRGBA
	var twoCh bool
	switch {
	case vkFormat == vkFormatUndefined && dfd.colorModel == khrDFModelUASTC:
		bx, by := (width+3)/4, (height+3)/4
		sliceBytes := bx * by * 16
		totalSlices := int(layers) * int(faceCount)
		if len(raw) < sliceBytes || len(raw) != sliceBytes*totalSlices {
			return nil, false, errors.New("invalid KTX2 UASTC level 0 byte length")
		}
		out, twoCh, err = decodeKTX2UASTC(raw[:sliceBytes], width, height, dfd)
		if err != nil {
			return nil, false, err
		}
	case vkFormat != vkFormatUndefined:
		out, twoCh, err = decodeKTX2Uncompressed(raw, width, height, int(layers)*int(faceCount), vkFormat)
		if err != nil {
			return nil, false, err
		}
	default:
		return nil, false, fmt.Errorf("unsupported KTX2 format %d (DFD model %d)", vkFormat, dfd.colorModel)
	}

	if dfd.flags&khrDFFlagAlphaPremultiplied != 0 && !twoCh {
		unpremultiplyNRGBA(out.Pix)
	}
	return out, twoCh, nil
}

func parseKTX2DFD(dfd []byte) (ktx2DFD, error) {
	if len(dfd) < 28 {
		return ktx2DFD{}, errors.New("truncated KTX2 DFD")
	}
	totalSize := binary.LittleEndian.Uint32(dfd[0:4])
	if totalSize > uint32(len(dfd)) || totalSize < 28 {
		return ktx2DFD{}, errors.New("invalid KTX2 DFD totalSize")
	}
	rem := dfd[4:totalSize]
	for len(rem) >= 8 {
		w0 := binary.LittleEndian.Uint32(rem[0:4])
		w1 := binary.LittleEndian.Uint32(rem[4:8])
		vendorID := w0 & 0x1FFFF
		descType := w0 >> 17
		version := w1 & 0xFFFF
		blockSize := int(w1 >> 16)
		if blockSize < 8 || blockSize > len(rem) {
			return ktx2DFD{}, errors.New("invalid KTX2 DFD descriptor block size")
		}
		blk := rem[:blockSize]
		rem = rem[blockSize:]
		if vendorID == 0 && descType == 0 && version == 2 {
			if blockSize < 24 || (blockSize-24)%16 != 0 {
				return ktx2DFD{}, errors.New("invalid KTX2 Basic DFD block")
			}
			nSamples := (blockSize - 24) / 16
			out := ktx2DFD{
				colorModel: blk[8],
				transfer:   blk[10],
				flags:      blk[11],
				channels:   make([]uint8, nSamples),
			}
			for i := range nSamples {
				sample := blk[24+i*16 : 24+(i+1)*16]
				out.channels[i] = sample[3] & 0x0F
			}
			return out, nil
		}
	}
	return ktx2DFD{}, errors.New("missing KTX2 Basic DFD block")
}

func decompressKTX2Level(comp []byte, uncompLen int, scheme uint32) ([]byte, error) {
	out := make([]byte, uncompLen)
	switch scheme {
	case ktx2SupercompZstd:
		dec, err := zstd.NewReader(bytes.NewReader(comp), zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(uint64(max(uncompLen, 1<<16))))
		if err != nil {
			return nil, err
		}
		defer dec.Close()
		if _, err := io.ReadFull(dec, out); err != nil {
			return nil, errors.New("truncated KTX2 Zstandard stream")
		}
		var extra [1]byte
		if n, _ := dec.Read(extra[:]); n != 0 {
			return nil, errors.New("trailing bytes in KTX2 Zstandard stream")
		}
	case ktx2SupercompZlib:
		zr, err := zlib.NewReader(bytes.NewReader(comp))
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		if _, err := io.ReadFull(zr, out); err != nil {
			return nil, errors.New("truncated KTX2 zlib stream")
		}
		var extra [1]byte
		if n, _ := zr.Read(extra[:]); n != 0 {
			return nil, errors.New("trailing bytes in KTX2 zlib stream")
		}
	}
	return out, nil
}

func decodeKTX2Uncompressed(raw []byte, width, height, slices int, vkFormat uint32) (*image.NRGBA, bool, error) {
	var bpp int
	var bgr, twoCh bool
	switch vkFormat {
	case vkFormatR8Unorm, vkFormatR8SRGB:
		bpp = 1
	case vkFormatR8G8Unorm, vkFormatR8G8SRGB:
		bpp = 2
		twoCh = true
	case vkFormatR8G8B8Unorm, vkFormatR8G8B8SRGB:
		bpp = 3
	case vkFormatR8G8B8A8Un, vkFormatR8G8B8A8SR:
		bpp = 4
	case vkFormatB8G8R8A8Un, vkFormatB8G8R8A8SR:
		bpp = 4
		bgr = true
	default:
		return nil, false, fmt.Errorf("unsupported uncompressed KTX2 VkFormat %d", vkFormat)
	}
	sliceBytes := width * height * bpp
	if len(raw) < sliceBytes || len(raw) != sliceBytes*slices {
		return nil, false, errors.New("invalid uncompressed KTX2 byte length")
	}
	src := raw[:sliceBytes]
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	dst := img.Pix
	pxCount := width * height
	switch bpp {
	case 1:
		for i := range pxCount {
			v := src[i]
			off := i * 4
			dst[off], dst[off+1], dst[off+2], dst[off+3] = v, v, v, 255
		}
	case 2:
		for i := range pxCount {
			off := i * 4
			dst[off], dst[off+1], dst[off+2], dst[off+3] = src[i*2], src[i*2+1], 0, 255
		}
	case 3:
		for i := range pxCount {
			off := i * 4
			dst[off], dst[off+1], dst[off+2], dst[off+3] = src[i*3], src[i*3+1], src[i*3+2], 255
		}
	case 4:
		if bgr {
			for i := range pxCount {
				off := i * 4
				dst[off], dst[off+1], dst[off+2], dst[off+3] = src[off+2], src[off+1], src[off], src[off+3]
			}
		} else {
			copy(dst, src)
		}
	}
	return img, twoCh, nil
}

func unpremultiplyNRGBA(pix []byte) {
	for i := 0; i+3 < len(pix); i += 4 {
		a := pix[i+3]
		if a == 0 {
			pix[i], pix[i+1], pix[i+2] = 0, 0, 0
		} else if a < 255 {
			pix[i] = uint8(min(255, (int(pix[i])*255+int(a)/2)/int(a)))
			pix[i+1] = uint8(min(255, (int(pix[i+1])*255+int(a)/2)/int(a)))
			pix[i+2] = uint8(min(255, (int(pix[i+2])*255+int(a)/2)/int(a)))
		}
	}
}

func reconstructNormalXYImage(src image.Image) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := nrgbaPixel(src, b, x, y)
			nx := float64(c.R)/255*2 - 1
			ny := float64(c.G)/255*2 - 1
			nz := math.Sqrt(max(0, 1-nx*nx-ny*ny))
			off := y*out.Stride + x*4
			out.Pix[off] = c.R
			out.Pix[off+1] = c.G
			out.Pix[off+2] = uint8((nz*.5+.5)*255 + .5)
			out.Pix[off+3] = 255
		}
	}
	return out
}
