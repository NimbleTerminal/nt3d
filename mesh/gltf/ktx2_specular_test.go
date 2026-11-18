// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"image/color"
	"math"
	"math/bits"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"

	"nimbleterminal.dev/nt3d"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

type testBitWriter struct {
	buf   []byte
	cur   uint64
	nBits int
}

func (w *testBitWriter) put(v uint32, n int) {
	if n == 0 {
		return
	}
	w.cur |= (uint64(v) & ((uint64(1) << n) - 1)) << w.nBits
	w.nBits += n
	for w.nBits >= 8 {
		w.buf = append(w.buf, byte(w.cur))
		w.cur >>= 8
		w.nBits -= 8
	}
}

func (w *testBitWriter) putVLC(val, chunkBits int) {
	chunkSize := 1 << chunkBits
	mask := chunkSize - 1
	for {
		next := val >> chunkBits
		s := val & mask
		if next != 0 {
			s |= chunkSize
		}
		w.put(uint32(s), chunkBits+1)
		if next == 0 {
			break
		}
		val = next
	}
}

func (w *testBitWriter) bytes() []byte {
	out := append([]byte(nil), w.buf...)
	if w.nBits > 0 {
		out = append(out, byte(w.cur))
	}
	return out
}

type testBasisEncTable struct {
	codes [basisHuffMaxSyms]uint16
	sizes [basisHuffMaxSyms]uint8
}

func writeTestBasisHuffman(w *testBitWriter, codeSizes []uint8) testBasisEncTable {
	var enc testBasisEncTable
	if len(codeSizes) == 0 {
		w.put(0, 14)
		return enc
	}
	w.put(uint32(len(codeSizes)), 14)

	// Emit codelength symbols: use 0..16 directly, plus RLE codes 17/18 for zero runs and 19 for repeats.
	var clStream []struct {
		sym       int
		extraVal  uint32
		extraBits int
	}
	usedCL := map[int]bool{}
	for i := 0; i < len(codeSizes); {
		sz := codeSizes[i]
		if sz == 0 {
			j := i
			for j < len(codeSizes) && codeSizes[j] == 0 {
				j++
			}
			run := j - i
			for run >= 11 {
				take := min(138, run)
				clStream = append(clStream, struct {
					sym       int
					extraVal  uint32
					extraBits int
				}{18, uint32(take - 11), 7})
				usedCL[18] = true
				i += take
				run -= take
			}
			if run >= 3 {
				take := min(10, run)
				clStream = append(clStream, struct {
					sym       int
					extraVal  uint32
					extraBits int
				}{17, uint32(take - 3), 3})
				usedCL[17] = true
				i += take
				run -= take
			}
			for range run {
				clStream = append(clStream, struct {
					sym       int
					extraVal  uint32
					extraBits int
				}{0, 0, 0})
				usedCL[0] = true
				i++
			}
			continue
		}
		clStream = append(clStream, struct {
			sym       int
			extraVal  uint32
			extraBits int
		}{int(sz), 0, 0})
		usedCL[int(sz)] = true
		i++
		j := i
		for j < len(codeSizes) && codeSizes[j] == sz {
			j++
		}
		rep := j - i
		if rep >= 7 {
			take := min(134, rep)
			clStream = append(clStream, struct {
				sym       int
				extraVal  uint32
				extraBits int
			}{20, uint32(take - 7), 7})
			usedCL[20] = true
			i += take
			rep -= take
		}
		if rep >= 3 {
			take := min(6, rep)
			clStream = append(clStream, struct {
				sym       int
				extraVal  uint32
				extraBits int
			}{19, uint32(take - 3), 2})
			usedCL[19] = true
			i += take
			rep -= take
		}
	}

	// Assign uniform codelengths (5 bits, or 1 bit if only 1 symbol) to used CL symbols.
	clBitLen := uint8(5)
	if len(usedCL) == 1 {
		clBitLen = 1
	}
	var clCodeSizes [21]uint8
	for sym := range usedCL {
		clCodeSizes[sym] = clBitLen
	}
	numCLCodes := 1
	for idx, sym := range basisCodelengthOrder {
		if clCodeSizes[sym] > 0 && idx+1 > numCLCodes {
			numCLCodes = idx + 1
		}
	}
	w.put(uint32(numCLCodes), 5)
	for i := range numCLCodes {
		w.put(uint32(clCodeSizes[basisCodelengthOrder[i]]), 3)
	}
	clEnc := canonicalEnc(clCodeSizes[:])
	for _, item := range clStream {
		w.put(uint32(clEnc.codes[item.sym]), int(clEnc.sizes[item.sym]))
		if item.extraBits > 0 {
			w.put(item.extraVal, item.extraBits)
		}
	}
	return canonicalEnc(codeSizes)
}

func canonicalEnc(codeSizes []uint8) testBasisEncTable {
	var enc testBasisEncTable
	var numCodes [basisHuffMaxCodeSize + 1]int
	for _, sz := range codeSizes {
		if sz > 0 {
			numCodes[sz]++
		}
	}
	var nextCode [basisHuffMaxCodeSize + 1]int
	code := 0
	for l := 1; l <= basisHuffMaxCodeSize; l++ {
		code = (code + numCodes[l-1]) << 1
		nextCode[l] = code
	}
	for sym, sz := range codeSizes {
		if sz == 0 {
			continue
		}
		msb := nextCode[sz]
		nextCode[sz]++
		enc.codes[sym] = bits.Reverse16(uint16(msb)) >> (16 - sz)
		enc.sizes[sym] = sz
	}
	return enc
}

func uniformSizes(maxSym int, used ...int) []uint8 {
	out := make([]uint8, maxSym+1)
	if len(used) == 1 {
		out[used[0]] = 1
		return out
	}
	l := uint8(1)
	for (1 << l) < len(used) {
		l++
	}
	for _, s := range used {
		out[s] = l
	}
	return out
}

func (e *testBasisEncTable) emit(w *testBitWriter, sym int) {
	w.put(uint32(e.codes[sym]), int(e.sizes[sym]))
}

func makeBasicDFD(colorModel, transfer, flags uint8, channels ...uint8) []byte {
	blockSize := 24 + len(channels)*16
	totalSize := 4 + blockSize
	dfd := make([]byte, totalSize)
	binary.LittleEndian.PutUint32(dfd[0:4], uint32(totalSize))
	binary.LittleEndian.PutUint32(dfd[4:8], 0) // vendorId=0, descriptorType=0
	binary.LittleEndian.PutUint32(dfd[8:12], 2|(uint32(blockSize)<<16))
	dfd[12] = colorModel
	dfd[13] = 1 // BT709
	dfd[14] = transfer
	dfd[15] = flags
	for i, ch := range channels {
		off := 4 + 24 + i*16
		dfd[off+3] = ch
		dfd[off+12], dfd[off+13], dfd[off+14], dfd[off+15] = 255, 255, 255, 255
	}
	return dfd
}

func buildKTX2Container(vkFormat uint32, width, height, supercomp uint32, dfd, sgd, level0 []byte, uncompLen uint64) []byte {
	headerAndIdx := 80 + 24
	dfdOff := headerAndIdx
	// Align SGD and level data to 8 bytes per KTX2 convention.
	sgdOff := (dfdOff + len(dfd) + 7) &^ 7
	lvlOff := (sgdOff + len(sgd) + 7) &^ 7
	out := make([]byte, lvlOff+len(level0))

	copy(out[0:12], ktx2Magic[:])
	binary.LittleEndian.PutUint32(out[12:16], vkFormat)
	typeSize := uint32(1)
	binary.LittleEndian.PutUint32(out[16:20], typeSize)
	binary.LittleEndian.PutUint32(out[20:24], width)
	binary.LittleEndian.PutUint32(out[24:28], height)
	binary.LittleEndian.PutUint32(out[28:32], 0) // pixelDepth
	binary.LittleEndian.PutUint32(out[32:36], 0) // layerCount
	binary.LittleEndian.PutUint32(out[36:40], 1) // faceCount
	binary.LittleEndian.PutUint32(out[40:44], 1) // levelCount
	binary.LittleEndian.PutUint32(out[44:48], supercomp)
	binary.LittleEndian.PutUint32(out[48:52], uint32(dfdOff))
	binary.LittleEndian.PutUint32(out[52:56], uint32(len(dfd)))
	if len(sgd) > 0 {
		binary.LittleEndian.PutUint64(out[64:72], uint64(sgdOff))
		binary.LittleEndian.PutUint64(out[72:80], uint64(len(sgd)))
	}
	binary.LittleEndian.PutUint64(out[80:88], uint64(lvlOff))
	binary.LittleEndian.PutUint64(out[88:96], uint64(len(level0)))
	binary.LittleEndian.PutUint64(out[96:104], uncompLen)

	copy(out[dfdOff:], dfd)
	copy(out[sgdOff:], sgd)
	copy(out[lvlOff:], level0)
	return out
}

func buildSolidUASTCKTX2Test(c color.NRGBA) []byte {
	var bw testBitWriter
	bw.put(uint32(uastcModeCodes[8][0]), int(uastcModeCodes[8][1]))
	bw.put(uint32(c.R), 8)
	bw.put(uint32(c.G), 8)
	bw.put(uint32(c.B), 8)
	bw.put(uint32(c.A), 8)
	var blk [16]byte
	copy(blk[:], bw.bytes())
	dfd := makeBasicDFD(khrDFModelUASTC, khrDFTransferSRGB, 0, khrDFChannelUASTCRGBA)
	return buildKTX2Container(vkFormatUndefined, 4, 4, ktx2SupercompNone, dfd, nil, blk[:], 16)
}

func buildMinimalETC1SKTX2Test(r5, g5, b5 uint8) []byte {
	var epW testBitWriter
	dR := (int(r5) - 16) & 31
	dG := (int(g5) - 16) & 31
	dB := (int(b5) - 16) & 31
	used := []int{dR}
	if !containsInt(used, dG) {
		used = append(used, dG)
	}
	if !containsInt(used, dB) {
		used = append(used, dB)
	}
	m0 := writeTestBasisHuffman(&epW, uniformSizes(31, used...))
	m1 := writeTestBasisHuffman(&epW, uniformSizes(31, used...))
	m2 := writeTestBasisHuffman(&epW, uniformSizes(31, used...))
	mi := writeTestBasisHuffman(&epW, uniformSizes(7, 0))
	epW.put(0, 1) // RGB
	mi.emit(&epW, 0)
	prev := [3]int{16, 16, 16}
	for c, d := range [3]int{dR, dG, dB} {
		switch {
		case prev[c] <= 9:
			m0.emit(&epW, d)
		case prev[c] <= 21:
			m1.emit(&epW, d)
		default:
			m2.emit(&epW, d)
		}
		prev[c] = (prev[c] + d) & 31
	}
	epData := epW.bytes()

	var selW testBitWriter
	selW.put(0, 1) // global=0
	selW.put(0, 1) // hybrid=0
	selW.put(1, 1) // raw=1
	// Selector 0xAA = [2, 2, 2, 2] -> +2 modifier at inten=0.
	for range 4 {
		selW.put(0xAA, 8)
	}
	selData := selW.bytes()

	var tabW testBitWriter
	predEnc := writeTestBasisHuffman(&tabW, uniformSizes(255, 3))
	deltaEnc := writeTestBasisHuffman(&tabW, uniformSizes(0, 0))
	selEnc := writeTestBasisHuffman(&tabW, uniformSizes(0, 0))
	_ = writeTestBasisHuffman(&tabW, nil) // empty RLE table
	tabW.put(0, 13)                       // history size = 0
	tabData := tabW.bytes()

	var sliceW testBitWriter
	predEnc.emit(&sliceW, 3)  // block (0,0): pred=3 (no pred)
	deltaEnc.emit(&sliceW, 0) // epIdx = 0
	selEnc.emit(&sliceW, 0)   // selIdx = 0
	sliceBytes := sliceW.bytes()

	sgd := make([]byte, 20+20+len(epData)+len(selData)+len(tabData))
	binary.LittleEndian.PutUint16(sgd[0:2], 1)
	binary.LittleEndian.PutUint16(sgd[2:4], 1)
	binary.LittleEndian.PutUint32(sgd[4:8], uint32(len(epData)))
	binary.LittleEndian.PutUint32(sgd[8:12], uint32(len(selData)))
	binary.LittleEndian.PutUint32(sgd[12:16], uint32(len(tabData)))
	binary.LittleEndian.PutUint32(sgd[20+8:20+12], uint32(len(sliceBytes)))
	off := 40
	copy(sgd[off:], epData)
	off += len(epData)
	copy(sgd[off:], selData)
	off += len(selData)
	copy(sgd[off:], tabData)

	dfd := makeBasicDFD(khrDFModelETC1S, khrDFTransferSRGB, 0, khrDFChannelETC1SRGB)
	return buildKTX2Container(vkFormatUndefined, 4, 4, ktx2SupercompBasisLZ, dfd, sgd, sliceBytes, 0)
}

func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// packTestUASTCBlock packs a valid 128-bit UASTC block for any mode 0..18.
func packTestUASTCBlock(mode, pat, compSel int, tq, eb []uint32, w0, w1 [16]uint32) [16]byte {
	var bw testBitWriter
	bw.put(uint32(uastcModeCodes[mode][0]), int(uastcModeCodes[mode][1]))
	if mode == 8 {
		for i := range 4 {
			bw.put(eb[i], 8)
		}
		var out [16]byte
		copy(out[:], bw.bytes())
		return out
	}

	bw.put(0, uastcHasBC1Hint0[mode]+uastcHasBC1Hint1[mode]+8)
	if uastcHasETC1Bias[mode] != 0 {
		bw.put(0, 5)
	}
	if uastcHasAlpha[mode] != 0 {
		bw.put(0, 8)
	}

	subsets := uastcSubsets[mode]
	planes := uastcPlanes[mode]
	var part [16]int
	switch subsets {
	case 2:
		bw.put(uint32(pat), 5)
		seed := uastcBC7Partitions2[0]
		if mode == 7 {
			seed = bc73UASTC2Partitions[pat]
		} else {
			seed = uastcBC7Partitions2[pat]
		}
		for y := range 4 {
			for x := range 4 {
				part[y*4+x] = astcSelectPartition(seed, x, y, 0, 2, true)
			}
		}
	case 3:
		bw.put(uint32(pat), 4)
		seed := uastcBC7Partitions3[pat]
		for y := range 4 {
			for x := range 4 {
				part[y*4+x] = astcSelectPartition(seed, x, y, 0, 3, true)
			}
		}
	}

	if planes == 2 && mode != 17 {
		bw.put(uint32(compSel), 2)
	}

	epRange := uastcEndpointRange[mode]
	ebits, hasTrit, hasQuint := uastcRangeDesc(epRange)
	numEP := subsets * uastcComps[mode] * 2
	if hasTrit {
		for i := 0; i < numEP; {
			n := min(5, numEP-i)
			var packed uint32
			mul := uint32(1)
			for j := range n {
				packed += tq[i+j] * mul
				mul *= 3
			}
			bw.put(packed, uastcTritBits[n])
			i += n
		}
	} else if hasQuint {
		for i := 0; i < numEP; {
			n := min(3, numEP-i)
			var packed uint32
			mul := uint32(1)
			for j := range n {
				packed += tq[i+j] * mul
				mul *= 5
			}
			bw.put(packed, uastcQuintBits[n])
			i += n
		}
	}
	for i := range numEP {
		bw.put(eb[i], ebits)
	}

	wBits := uastcWeightBits[mode]
	if planes == 2 {
		for t := range 16 {
			nb := wBits
			if t == 0 {
				nb = wBits - 1
			}
			bw.put(w0[t], nb)
			bw.put(w1[t], nb)
		}
	} else {
		var seen [3]bool
		for t := range 16 {
			s := part[t]
			nb := wBits
			if !seen[s] {
				seen[s] = true
				nb = wBits - 1
			}
			bw.put(w0[t], nb)
		}
	}

	var out [16]byte
	raw := bw.bytes()
	if len(raw) > 16 {
		panic("UASTC test block exceeded 128 bits")
	}
	copy(out[:], raw)
	return out
}

func TestUASTCAll19ModesAndSupercompression(t *testing.T) {
	// Exercise every UASTC mode 0..18 and verify endpoint unquantization,
	// partition selection, dual-plane weights, and solid blocks.
	for mode := range totalUASTCModes {
		if mode == 8 {
			blk := packTestUASTCBlock(8, 0, 0, nil, []uint32{210, 120, 45, 180}, [16]uint32{}, [16]uint32{})
			px := unpackUASTCBlock(blk, true)
			for i, p := range px {
				if p != ([4]uint8{210, 120, 45, 180}) {
					t.Fatalf("mode 8 pixel %d = %v, want [210 120 45 180]", i, p)
				}
			}
			continue
		}

		subsets := uastcSubsets[mode]
		comps := uastcComps[mode]
		numEP := subsets * comps * 2
		epRange := uastcEndpointRange[mode]
		ebits, hasTrit, hasQuint := uastcRangeDesc(epRange)
		tq := make([]uint32, numEP)
		eb := make([]uint32, numEP)
		maxEB := uint32((1 << ebits) - 1)
		for i := range numEP {
			if i%2 == 1 {
				// High endpoint uses maximal quantized code (a=1, D=0, b..f=0 -> 255 in BISE, or maxEB in pure-bit).
				if hasTrit || hasQuint {
					tq[i] = 0
					eb[i] = 1
				} else {
					eb[i] = maxEB
				}
			}
		}

		wBits := uastcWeightBits[mode]
		maxW := uint32((1 << wBits) - 1)
		var w0, w1 [16]uint32
		// Keep anchor texel 0 at weight 0, set texel 15 to max weight (1.0 -> e1 = 255).
		w0[15] = maxW
		w1[15] = 0

		pat := 0
		if subsets == 2 {
			pat = 3
		} else if subsets == 3 {
			pat = 2
		}
		compSel := 1
		blk := packTestUASTCBlock(mode, pat, compSel, tq, eb, w0, w1)
		px := unpackUASTCBlock(blk, false)

		// Anchor texel 0 has weight 0 on all planes -> e0 = (0, 0, 0, 255 or 0).
		wantA0 := uint8(0)
		if comps == 3 {
			wantA0 = 255
		}
		if px[0] != ([4]uint8{0, 0, 0, wantA0}) {
			t.Fatalf("mode %d texel 0 = %v, want [0 0 0 %d]", mode, px[0], wantA0)
		}

		// Texel 15 has w0=64 (255), and for dual-plane modes w1=0 on channel compSel (or alpha for mode 17).
		want15 := [4]uint8{255, 255, 255, 255}
		if uastcPlanes[mode] == 2 {
			cs := compSel
			if mode == 17 {
				cs = 3
			}
			if comps == 3 && cs == 3 {
				want15[3] = 255
			} else {
				want15[cs] = 0
			}
		}
		if px[15] != want15 {
			t.Fatalf("mode %d texel 15 = %v, want %v", mode, px[15], want15)
		}
	}

	// Reserved mode 19 (0x45, 7 bits) gracefully decodes to transparent black without panicking.
	var reserved [16]byte
	reserved[0] = 0x45
	if got := unpackUASTCBlock(reserved, true); got[0] != ([4]uint8{0, 0, 0, 0}) {
		t.Fatalf("reserved UASTC mode 19 = %v, want zero", got[0])
	}

	// Verify Zstandard (scheme 2) and zlib (scheme 3) supercompressed UASTC containers,
	// including non-multiple-of-4 dimensions (5x3 -> 2x1 blocks).
	b0 := packTestUASTCBlock(8, 0, 0, nil, []uint32{220, 40, 90, 255}, [16]uint32{}, [16]uint32{})
	b1 := packTestUASTCBlock(8, 0, 0, nil, []uint32{30, 180, 240, 128}, [16]uint32{}, [16]uint32{})
	rawSlice := append(b0[:], b1[:]...)
	dfd := makeBasicDFD(khrDFModelUASTC, khrDFTransferSRGB, 0, khrDFChannelUASTCRGBA)

	enc, err := zstd.NewWriter(nil)
	check(t, err)
	zstdData := enc.EncodeAll(rawSlice, nil)
	_ = enc.Close()

	var zlibBuf bytes.Buffer
	zw := zlib.NewWriter(&zlibBuf)
	_, err = zw.Write(rawSlice)
	check(t, err)
	check(t, zw.Close())

	for _, sc := range []struct {
		name   string
		scheme uint32
		comp   []byte
	}{
		{"zstd", ktx2SupercompZstd, zstdData},
		{"zlib", ktx2SupercompZlib, zlibBuf.Bytes()},
	} {
		ktx := buildKTX2Container(vkFormatUndefined, 5, 3, sc.scheme, dfd, nil, sc.comp, uint64(len(rawSlice)))
		img, twoCh, err := decodeKTX2(ktx, nil)
		check(t, err)
		if twoCh || img.Bounds().Dx() != 5 || img.Bounds().Dy() != 3 {
			t.Fatalf("%s KTX2 bounds=%v twoCh=%v", sc.name, img.Bounds(), twoCh)
		}
		if got := img.NRGBAAt(0, 0); got != (color.NRGBA{220, 40, 90, 255}) {
			t.Fatalf("%s block 0 texel = %v", sc.name, got)
		}
		if got := img.NRGBAAt(4, 2); got != (color.NRGBA{30, 180, 240, 128}) {
			t.Fatalf("%s block 1 texel = %v", sc.name, got)
		}
	}
}

func TestBasisLZETC1SFullFeatures(t *testing.T) {
	// Construct an 8x8 (2x2 blocks, plus test 6x5 crop) dual-slice RGBA ETC1S KTX2 exercising:
	// - DPCM endpoints across all 3 prevColor5 ranges (<=9, 10..21, >=22)
	// - XOR-DPCM selector codebook (isRaw = 0)
	// - 2x2 predictor modes: pred=3 (delta), pred=0 (left), pred=1 (upper), pred=2 (diagonal),
	//   AND ENDPOINT_PRED_REPEAT_LAST_SYMBOL (256) VLC repeat!
	// - approx_move_to_front history buffer + RLE runs (both literal and VLC=63)!
	var epW testBitWriter
	// Three endpoints:
	// ep0: inten=0, color5=(4, 16, 28) -> exercises prev=16 (model1), deltas (-12=20, 0, +12)
	// ep1: inten=1, color5=(8, 20, 30) -> exercises prev=4 (model0: +4), prev=16 (model1: +4), prev=28 (model2: +2)
	// ep2: inten=0, color5=(31, 31, 31) -> exercises prev=8 (model0: +23), prev=20 (model1: +11), prev=30 (model2: +1)
	m0 := writeTestBasisHuffman(&epW, uniformSizes(31, 4, 23))
	m1 := writeTestBasisHuffman(&epW, uniformSizes(31, 0, 4, 11, 12, 20))
	m2 := writeTestBasisHuffman(&epW, uniformSizes(31, 1, 2))
	mi := writeTestBasisHuffman(&epW, uniformSizes(7, 0, 1, 7))
	epW.put(0, 1) // RGB

	// ep0
	mi.emit(&epW, 0) // inten = 0
	m1.emit(&epW, 20)
	m1.emit(&epW, 0)
	m1.emit(&epW, 12)
	// ep1
	mi.emit(&epW, 1) // inten = 1
	m0.emit(&epW, 4)
	m1.emit(&epW, 4)
	m2.emit(&epW, 2)
	// ep2 (used by alpha slice for 200+ alpha)
	mi.emit(&epW, 7) // inten = (1+7)&7 = 0
	m0.emit(&epW, 23)
	m1.emit(&epW, 11)
	m2.emit(&epW, 1)
	epData := epW.bytes()

	// Non-raw XOR-DPCM selector codebook with 2 selectors:
	// sel0: all s=2 (+small = +2 at inten 0, +5 at inten 1) -> byte 0xAA
	// sel1: all s=3 (+large = +8 at inten 0, +17 at inten 1) -> byte 0xFF (XOR delta = 0x55)
	var selW testBitWriter
	selW.put(0, 1) // global=0
	selW.put(0, 1) // hybrid=0
	selW.put(0, 1) // isRaw=0 (XOR DPCM!)
	selDeltaEnc := writeTestBasisHuffman(&selW, uniformSizes(255, 0x55))
	for range 4 {
		selW.put(0xAA, 8) // first selector raw
	}
	for range 4 {
		selDeltaEnc.emit(&selW, 0x55) // 0xAA ^ 0x55 = 0xFF
	}
	selData := selW.bytes()

	// Slice Huffman tables:
	// 16x8 image = 4x2 blocks (two 2x2 quads side by side!):
	// Quad 0 (bx=0..1, by=0..1): predBits = (3<<0) | (0<<2) | (1<<4) | (2<<6) = 0x93
	//   (0,0)=pred 3 (delta->ep0), (1,0)=pred 0 (left->ep0), (0,1)=pred 1 (up->ep0), (1,1)=pred 2 (diag->ep0)
	// Quad 1 (bx=2..3, by=0..1): predBits = 0xFF (all pred=3 -> delta +1 to ep1 on (2,0), then delta 0 on rest)
	// Alpha slice uses ENDPOINT_PRED_REPEAT_LAST_SYMBOL (256) and selector RLE!
	const histSize = 4
	const rleSym = 2 + histSize // selCount=2 + histSize=4 = 6
	var tabW testBitWriter
	predEnc := writeTestBasisHuffman(&tabW, uniformSizes(256, 0x03, 0x93, 0xFF, 256))
	deltaEnc := writeTestBasisHuffman(&tabW, uniformSizes(2, 0, 1, 2))
	selEnc := writeTestBasisHuffman(&tabW, uniformSizes(rleSym, 0, 1, 2, 3, 4, rleSym))
	rleEnc := writeTestBasisHuffman(&tabW, uniformSizes(63, 0, 63))
	tabW.put(histSize, 13)
	tabData := tabW.bytes()

	// RGB slice (4x2 = 8 blocks in raster order: y=0 x=0..3, then y=1 x=0..3):
	var rgbW testBitWriter
	// Row y=0:
	// x=0 (even x, even y): quad 0 predBits = 0x93 -> (0,0)=3, (1,0)=0, (0,1)=1, (1,1)=2
	predEnc.emit(&rgbW, 0x93)
	deltaEnc.emit(&rgbW, 0) // (0,0): prev=0 + 0 -> ep0
	selEnc.emit(&rgbW, 0)   // literal sel0 -> mtf[2]=0, rover=3
	// x=1: pred=0 (left -> ep0)
	selEnc.emit(&rgbW, 4) // history idx 4-2=2 -> sel0, mtf.use(2) swaps mtf[1]<->mtf[2]
	// x=2 (even x, even y): quad 1 predBits = 0xFF -> all 4 blocks pred=3
	predEnc.emit(&rgbW, 0xFF)
	deltaEnc.emit(&rgbW, 1) // (2,0): prev=0 + 1 -> ep1
	selEnc.emit(&rgbW, 1)   // literal sel1 -> mtf[3]=1, rover=2
	// x=3: pred=3
	deltaEnc.emit(&rgbW, 0) // (3,0): ep1
	selEnc.emit(&rgbW, 1)   // literal sel1 -> mtf[2]=1 (Wait: let's put sel1 in mtf[0] via MTF or literal!)

	// Row y=1:
	// x=0: pred=1 (upper -> ep0)
	selEnc.emit(&rgbW, 0)
	// x=1: pred=2 (diag -> ep0)
	selEnc.emit(&rgbW, 0)
	// x=2: pred=3 (prevEndpointIndex is 0 from (1,1), so delta=1 selects ep1)
	deltaEnc.emit(&rgbW, 1) // ep1
	selEnc.emit(&rgbW, 1)
	// x=3: pred=3
	deltaEnc.emit(&rgbW, 0) // ep1
	selEnc.emit(&rgbW, 1)
	rgbSlice := rgbW.bytes()

	// Alpha slice (4x2 = 8 blocks): exercises ENDPOINT_PRED_REPEAT_LAST_SYMBOL (256)
	// AND selector RLE with VLC (runSym == 63)!
	// First seed MTF[0] with sel1 (1): literal->mtf[2], use(2)->mtf[1], use(1)->mtf[0]!
	var alphaW testBitWriter
	// Quad 0: predBits = 0x03 -> (0,0)=pred 3, (1,0)=0, (0,1)=0? Wait: at (0,1) x=0 so pred cannot be 0!
	// Use 0xFF (all pred=3) and repeat it via 256 for Quad 1 ( wait: min repeat is 3 quads, which is fine even if slice ends!).
	predEnc.emit(&alphaW, 0xFF)
	// Block (0,0): ep2 (255), literal sel1 -> mtf[2]=1
	deltaEnc.emit(&alphaW, 2)
	selEnc.emit(&alphaW, 1)
	// Block (1,0): ep2, history idx 2 (sym 4) -> mtf.use(2) moves sel1 to mtf[1]
	deltaEnc.emit(&alphaW, 0)
	selEnc.emit(&alphaW, 4)
	// Block (2,0): Quad 1 uses ENDPOINT_PRED_REPEAT_LAST_SYMBOL (256) with VLC=0 (repeats 0xFF)!
	predEnc.emit(&alphaW, 256)
	alphaW.putVLC(0, etc1sEndpointPredVLCBits)
	// Block (2,0): ep2, history idx 1 (sym 3) -> mtf.use(1) moves sel1 to mtf[0]!
	deltaEnc.emit(&alphaW, 0)
	selEnc.emit(&alphaW, 3)
	// Block (3,0) + all 4 blocks of row y=1 (5 remaining blocks): emit selector RLE (rleSym=6, runSym=63, vlc=2 -> 2+3=5 blocks)!
	deltaEnc.emit(&alphaW, 0)
	selEnc.emit(&alphaW, rleSym)
	rleEnc.emit(&alphaW, 63)
	alphaW.putVLC(2, 7) // 5 blocks total (consumes (3,0) + 4 blocks on y=1)
	for range 4 {
		deltaEnc.emit(&alphaW, 0)
	}
	alphaSlice := alphaW.bytes()

	level0 := append(append([]byte(nil), rgbSlice...), alphaSlice...)
	sgd := make([]byte, 40+len(epData)+len(selData)+len(tabData))
	binary.LittleEndian.PutUint16(sgd[0:2], 3) // 3 endpoints
	binary.LittleEndian.PutUint16(sgd[2:4], 2) // 2 selectors
	binary.LittleEndian.PutUint32(sgd[4:8], uint32(len(epData)))
	binary.LittleEndian.PutUint32(sgd[8:12], uint32(len(selData)))
	binary.LittleEndian.PutUint32(sgd[12:16], uint32(len(tabData)))
	binary.LittleEndian.PutUint32(sgd[20+4:20+8], 0)
	binary.LittleEndian.PutUint32(sgd[20+8:20+12], uint32(len(rgbSlice)))
	binary.LittleEndian.PutUint32(sgd[20+12:20+16], uint32(len(rgbSlice)))
	binary.LittleEndian.PutUint32(sgd[20+16:20+20], uint32(len(alphaSlice)))
	off := 40
	copy(sgd[off:], epData)
	off += len(epData)
	copy(sgd[off:], selData)
	off += len(selData)
	copy(sgd[off:], tabData)

	dfd := makeBasicDFD(khrDFModelETC1S, khrDFTransferSRGB, 0, khrDFChannelETC1SRGB, khrDFChannelETC1SAAA)
	ktx := buildKTX2Container(vkFormatUndefined, 15, 7, ktx2SupercompBasisLZ, dfd, sgd, level0, 0)

	img, twoCh, err := decodeKTX2(ktx, nil)
	check(t, err)
	if twoCh || img.Bounds().Dx() != 15 || img.Bounds().Dy() != 7 {
		t.Fatalf("ETC1S bounds=%v twoCh=%v", img.Bounds(), twoCh)
	}
	// Left half (quad 0, ep0: color5=(4,16,28) -> 8-bit=(33,132,231), inten=0 +2 -> (35,134,233), alpha=255):
	if got := img.NRGBAAt(5, 5); got != (color.NRGBA{35, 134, 233, 255}) {
		t.Fatalf("ETC1S quad 0 texel = %v, want {35 134 233 255}", got)
	}
	// Right half (quad 1, ep1: color5=(8,20,30) -> 8-bit=(66,165,247), inten=1, sel1=+17 -> (83,182,255), alpha=255):
	if got := img.NRGBAAt(14, 6); got != (color.NRGBA{83, 182, 255, 255}) {
		t.Fatalf("ETC1S quad 1 texel = %v, want {83 182 255 255}", got)
	}
}

func TestGLTFKHRTextureBasisUAndTwoChannelNormals(t *testing.T) {
	p := upTriangle
	p.material = gltf.Index(0)
	p.uv = [][2]float32{{0, 0}, {1, 0}, {0, 1}}
	doc := gltfDoc(p)
	doc.ExtensionsUsed = []string{"KHR_texture_basisu", "KHR_materials_specular"}
	doc.ExtensionsRequired = []string{"KHR_texture_basisu", "KHR_materials_specular"}

	writeKTX2Img := func(name string, ktx []byte) int {
		bv := modeler.WriteBufferView(doc, gltf.TargetNone, ktx)
		idx := len(doc.Images)
		doc.Images = append(doc.Images, &gltf.Image{
			Name:       name,
			MimeType:   "image/ktx2",
			BufferView: gltf.Index(bv),
		})
		return idx
	}

	// 1. Base color: UASTC solid RGBA.
	baseIdx := writeKTX2Img("base", buildSolidUASTCKTX2Test(color.NRGBA{200, 95, 45, 220}))

	// 2. Normal map: two-channel UASTC RRRG (channel 5) with X in RGB (180 -> +0.408) and Y in A (128 -> ~0).
	// Loader must reconstruct positive tangent Z = sqrt(1 - nx^2 - ny^2) ≈ 0.913!
	var nrmBlk [16]byte
	nrmBlk = packTestUASTCBlock(8, 0, 0, nil, []uint32{180, 180, 180, 128}, [16]uint32{}, [16]uint32{})
	nrmDFD := makeBasicDFD(khrDFModelUASTC, khrDFTransferLinear, 0, khrDFChannelUASTCRRRG)
	nrmIdx := writeKTX2Img("normal-rrrg", buildKTX2Container(vkFormatUndefined, 4, 4, ktx2SupercompNone, nrmDFD, nil, nrmBlk[:], 16))

	// 3. ORM map: ETC1S BasisLZ.
	ormIdx := writeKTX2Img("orm-etc1s", buildMinimalETC1SKTX2Test(24, 12, 28))

	// 4. Emissive map: uncompressed VK_FORMAT_B8G8R8A8_SRGB (50).
	bgra := bytes.Repeat([]byte{30, 140, 250, 255}, 16)
	emDFD := makeBasicDFD(khrDFModelRGBSDA, khrDFTransferSRGB, 0, 2, 1, 0, 3)
	emIdx := writeKTX2Img("em-bgra", buildKTX2Container(vkFormatB8G8R8A8SR, 4, 4, ktx2SupercompNone, emDFD, nil, bgra, uint64(len(bgra))))

	ktxTex := func(src int) *gltf.Texture {
		return &gltf.Texture{
			Extensions: gltf.Extensions{"KHR_texture_basisu": rawJSON(t, map[string]any{"source": src})},
		}
	}
	doc.Textures = []*gltf.Texture{
		ktxTex(baseIdx),
		ktxTex(nrmIdx),
		ktxTex(ormIdx),
		ktxTex(emIdx),
	}
	doc.Materials = []*gltf.Material{{
		Name:      "3dtiles-pbr",
		AlphaMode: gltf.AlphaBlend,
		PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
			MetallicFactor:           gltf.Float(0.2),
			RoughnessFactor:          gltf.Float(0.6),
			BaseColorTexture:         &gltf.TextureInfo{Index: 0},
			MetallicRoughnessTexture: &gltf.TextureInfo{Index: 2},
		},
		NormalTexture:    &gltf.NormalTexture{Index: gltf.Index(1)},
		OcclusionTexture: &gltf.OcclusionTexture{Index: gltf.Index(2), Strength: gltf.Float(0.75)},
		EmissiveFactor:   [3]float64{1, 1, 1},
		EmissiveTexture:  &gltf.TextureInfo{Index: 3},
		Extensions: gltf.Extensions{
			"KHR_materials_specular": rawJSON(t, map[string]any{
				"specularFactor":      0.5,
				"specularColorFactor": []float64{2.0, 1.0, 0.0},
			}),
		},
	}}

	m, err := Decode(glb(t, doc), nil)
	check(t, err)
	if len(m.Warnings) != 0 {
		t.Fatalf("unexpected KHR_texture_basisu/KHR_materials_specular warnings: %v", m.Warnings)
	}
	mat := m.Objects[0].Geometry.Material
	if mat == nil || mat.Texture == nil || mat.Normal.Map == nil || mat.PBR.ORMMap == nil || mat.Emission.Map == nil {
		t.Fatalf("missing decoded KTX2 textures: %+v", mat)
	}
	if got := mat.Texture.NRGBAAt(0, 0); got != (color.NRGBA{200, 95, 45, 220}) {
		t.Fatalf("KTX2 UASTC base texel = %v, want {200 95 45 220}", got)
	}
	// Reconstructed Z for X=180 (+0.4078), Y=128 (+0.0039) -> Z ≈ 0.913 -> byte ≈ 244 (not collapsed to ~128!).
	nrmPx := mat.Normal.Map.At(0, 0).(color.RGBA)
	if nrmPx.R < 175 || nrmPx.B < 235 {
		t.Fatalf("expected two-channel RRRG normal map to reconstruct +Z, got %v", nrmPx)
	}
	if got := mat.Emission.Map.NRGBAAt(0, 0); got != (color.NRGBA{250, 140, 30, 255}) {
		t.Fatalf("KTX2 BGRA emissive texel = %v, want {250 140 30 255}", got)
	}
	if want := (nt3d.SpecularStyle{Color: color.RGBA{R: 255, G: srgb8(0.5), B: 0, A: 255}, Intensity: 2, Strength: .5}); mat.PBR.Specular != want {
		t.Fatalf("unexpected KHR_materials_specular decode: %+v, want %+v", mat.PBR.Specular, want)
	}

	// Priority & fallback chain: KHR_texture_basisu -> EXT_texture_webp -> Texture.Source.
	chainDoc := gltfDoc(p)
	chainDoc.ExtensionsUsed = []string{"KHR_texture_basisu", "EXT_texture_webp"}
	chainDoc.Buffers[0].URI = "geom.bin"
	chainDoc.Images = []*gltf.Image{
		{URI: "fallback.png"},
		{URI: "mid.webp"},
		{URI: "best.ktx2"},
	}
	chainDoc.Textures = []*gltf.Texture{{
		Source: gltf.Index(0),
		Extensions: gltf.Extensions{
			"EXT_texture_webp":   rawJSON(t, map[string]any{"source": 1}),
			"KHR_texture_basisu": rawJSON(t, map[string]any{"source": 2}),
		},
	}}
	chainDoc.Materials = []*gltf.Material{{
		PBRMetallicRoughness: &gltf.PBRMetallicRoughness{BaseColorTexture: &gltf.TextureInfo{Index: 0}},
	}}
	sideFiles := memFS{}
	var chainJSON bytes.Buffer
	enc := gltf.NewEncoderFS(&chainJSON, sideFiles)
	enc.AsBinary = false
	check(t, enc.Encode(chainDoc))

	geomBytes := sideFiles["geom.bin"].Bytes()
	pngBytes := pngTexture(t, color.NRGBA{20, 180, 40, 255})
	webpBytes := webpTexture(t, color.NRGBA{40, 90, 220, 255})
	ktxBytes := buildSolidUASTCKTX2Test(color.NRGBA{240, 70, 30, 255})

	mAll, err := Decode(chainJSON.Bytes(), &nt3dmesh.Options{Open: opener(map[string][]byte{
		"geom.bin":     geomBytes,
		"fallback.png": pngBytes,
		"mid.webp":     webpBytes,
		"best.ktx2":    ktxBytes,
	})})
	check(t, err)
	if got := mAll.Objects[0].Geometry.Material.Texture.NRGBAAt(0, 0); got != (color.NRGBA{240, 70, 30, 255}) {
		t.Fatalf("expected KHR_texture_basisu preferred, got %v", got)
	}

	mNoKTX, err := Decode(chainJSON.Bytes(), &nt3dmesh.Options{Open: opener(map[string][]byte{
		"geom.bin":     geomBytes,
		"fallback.png": pngBytes,
		"mid.webp":     webpBytes,
	})})
	check(t, err)
	if len(mNoKTX.Warnings) != 0 || mNoKTX.Objects[0].Geometry.Material.Texture.NRGBAAt(0, 0) != (color.NRGBA{40, 90, 220, 255}) {
		t.Fatalf("expected clean WebP fallback, warnings=%v", mNoKTX.Warnings)
	}

	mOnlyPNG, err := Decode(chainJSON.Bytes(), &nt3dmesh.Options{Open: opener(map[string][]byte{
		"geom.bin":     geomBytes,
		"fallback.png": pngBytes,
	})})
	check(t, err)
	if len(mOnlyPNG.Warnings) != 0 || mOnlyPNG.Objects[0].Geometry.Material.Texture.NRGBAAt(0, 0) != (color.NRGBA{20, 180, 40, 255}) {
		t.Fatalf("expected clean PNG fallback, warnings=%v", mOnlyPNG.Warnings)
	}
}

func TestGLTFKHRMaterialsSpecularVariantsAndValidation(t *testing.T) {
	doc := gltfDoc(upTriangle)
	doc.ExtensionsUsed = []string{"KHR_materials_specular", "KHR_materials_variants", "KHR_materials_unlit"}
	doc.ExtensionsRequired = []string{"KHR_materials_specular", "KHR_materials_variants"}
	doc.Extensions = gltf.Extensions{
		"KHR_materials_variants": rawJSON(t, map[string]any{
			"variants": []map[string]any{
				{"name": "Default"},
				{"name": "MatteChalk"},
				{"name": "GrazingRim"},
				{"name": "CopperCoat"},
			},
		}),
	}
	doc.Materials = []*gltf.Material{
		{
			Name: "default-spec",
			Extensions: gltf.Extensions{
				"KHR_materials_specular": rawJSON(t, map[string]any{
					"specularFactor":      1.0,
					"specularColorFactor": []float64{1, 1, 1},
				}),
			},
		},
		{
			Name: "matte-chalk",
			Extensions: gltf.Extensions{
				"KHR_materials_specular": rawJSON(t, map[string]any{"specularFactor": 0.0}),
			},
		},
		{
			Name: "grazing-rim",
			Extensions: gltf.Extensions{
				"KHR_materials_specular": rawJSON(t, map[string]any{
					"specularFactor":      0.8,
					"specularColorFactor": []float64{0, 0, 0},
				}),
			},
		},
		{
			Name: "copper-coat",
			Extensions: gltf.Extensions{
				"KHR_materials_specular": rawJSON(t, map[string]any{
					"specularFactor":      1.0,
					"specularColorFactor": []float64{3.0, 1.5, 0.5},
				}),
			},
		},
	}
	doc.Meshes[0].Primitives[0].Material = gltf.Index(0)
	doc.Meshes[0].Primitives[0].Extensions = gltf.Extensions{
		"KHR_materials_variants": rawJSON(t, map[string]any{
			"mappings": []map[string]any{
				{"material": 0, "variants": []int{0}},
				{"material": 1, "variants": []int{1}},
				{"material": 2, "variants": []int{2}},
				{"material": 3, "variants": []int{3}},
			},
		}),
	}
	doc.Nodes = []*gltf.Node{{Name: "a", Mesh: gltf.Index(0)}}
	doc.Scenes = []*gltf.Scene{{Nodes: []int{0}}}

	m, err := Decode(glb(t, doc), nil)
	check(t, err)
	if len(m.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", m.Warnings)
	}

	s := nt3d.NewScene()
	instA, err := m.Asset.Instantiate(s, nt3d.Root, "a")
	check(t, err)
	instB, err := m.Asset.Instantiate(s, nt3d.Root, "b")
	check(t, err)

	// Default [1,1,1], 1.0 leaves the Specular group zero.
	f0, err := s.Frame(nt3d.DefaultCamera(), 64, 64)
	check(t, err)
	if f0.Draws[0].Geometry.Material.PBR.Specular.Color != (color.RGBA{}) || f0.Draws[0].Geometry.Material.PBR.Specular.Intensity != 0 {
		t.Fatalf("expected a zero Specular group on default specular, got %+v", f0.Draws[0].Geometry.Material)
	}
	if len(f0.Draws) != 2 || f0.Draws[0].Shared == 0 || f0.Draws[0].Shared != f0.Draws[1].Shared {
		t.Fatalf("expected shared SharedGeometry ID across 2 instances, got %d draws", len(f0.Draws))
	}

	// Switch instance A to MatteChalk and instance B to CopperCoat: SharedGeometry stays shared!
	check(t, s.SetMaterialVariant(instA.Root, "MatteChalk"))
	check(t, s.SetMaterialVariant(instB.Root, "CopperCoat"))
	f1, err := s.Frame(nt3d.DefaultCamera(), 64, 64)
	check(t, err)
	if len(f1.Draws) != 2 || f1.Draws[0].Shared != f0.Draws[0].Shared || f1.Draws[1].Shared != f0.Draws[0].Shared {
		t.Fatalf("SetMaterialVariant with KHR_materials_specular broke SharedGeometry sharing: %d, %d", f1.Draws[0].Shared, f1.Draws[1].Shared)
	}

	check(t, s.SetMaterialVariant(instA.Root, "GrazingRim"))
	f2, err := s.Frame(nt3d.DefaultCamera(), 64, 64)
	check(t, err)
	if got, want := f2.Draws[0].Geometry.Material.PBR.Specular, (nt3d.SpecularStyle{Color: color.RGBA{A: 255}, Strength: .8}); got != want {
		t.Fatalf("GrazingRim specular = %+v, want %+v", got, want)
	}

	// Invalid KHR_materials_specular & KHR_texture_basisu payloads must be rejected.
	pMat := upTriangle
	pMat.material = gltf.Index(0)
	for _, bad := range []gltf.Extensions{
		{"KHR_materials_specular": map[string]any{"specularFactor": -0.1}},
		{"KHR_materials_specular": map[string]any{"specularFactor": 1.1}},
		{"KHR_materials_specular": map[string]any{"specularColorFactor": []float64{1, 1}}},
		{"KHR_materials_specular": map[string]any{"specularColorFactor": []float64{1, -0.2, 1}}},
		{"KHR_materials_specular": map[string]any{"specularTexture": map[string]any{"index": 9}}},
		{"KHR_materials_specular": map[string]any{"specularColorTexture": map[string]any{}}},
	} {
		badDoc := gltfDoc(pMat)
		badDoc.Materials = []*gltf.Material{{Extensions: bad}}
		if _, err := Decode(glb(t, badDoc), nil); err == nil {
			t.Fatalf("accepted invalid KHR_materials_specular %v", bad)
		}
	}

	for _, badTex := range []any{
		"bad",
		map[string]any{},
		map[string]any{"source": -1},
		map[string]any{"source": 5},
	} {
		badDoc := gltfDoc(upTriangle)
		badDoc.Images = []*gltf.Image{{URI: "a.ktx2"}}
		badDoc.Textures = []*gltf.Texture{{
			Extensions: gltf.Extensions{"KHR_texture_basisu": rawJSON(t, badTex)},
		}}
		if _, err := Decode(glb(t, badDoc), nil); err == nil {
			t.Fatalf("accepted invalid KHR_texture_basisu %v", badTex)
		}
	}

	// MaxBytes decompression bomb protection: tiny Zstd KTX2 claiming 2048x2048 RGBA (16 MiB)
	// must fail immediately with nt3dmesh.ErrTooLarge when MaxBytes is 64 KiB.
	bombDFD := makeBasicDFD(khrDFModelUASTC, khrDFTransferSRGB, 0, khrDFChannelUASTCRGBA)
	bombKTX := buildKTX2Container(vkFormatUndefined, 2048, 2048, ktx2SupercompZstd, bombDFD, nil, []byte{0x28, 0xb5, 0x2f, 0xfd}, 2048*2048)
	pBomb := upTriangle
	pBomb.material = gltf.Index(0)
	pBomb.uv = [][2]float32{{0, 0}, {1, 0}, {0, 1}}
	bombDoc := gltfDoc(pBomb)
	bv := modeler.WriteBufferView(bombDoc, gltf.TargetNone, bombKTX)
	bombDoc.Images = []*gltf.Image{{MimeType: "image/ktx2", BufferView: gltf.Index(bv)}}
	bombDoc.Textures = []*gltf.Texture{{
		Extensions: gltf.Extensions{"KHR_texture_basisu": rawJSON(t, map[string]any{"source": 0})},
	}}
	bombDoc.Materials = []*gltf.Material{{
		PBRMetallicRoughness: &gltf.PBRMetallicRoughness{BaseColorTexture: &gltf.TextureInfo{Index: 0}},
	}}
	if _, err := Decode(glb(t, bombDoc), &nt3dmesh.Options{MaxBytes: 64 << 10}); !errors.Is(err, nt3dmesh.ErrTooLarge) {
		t.Fatalf("expected ErrTooLarge on KTX2 allocation bomb, got %v", err)
	}

	// Premultiplied alpha uncompressed KTX2 un-premultiplies cleanly to straight NRGBA.
	premulDFD := makeBasicDFD(khrDFModelRGBSDA, khrDFTransferSRGB, khrDFFlagAlphaPremultiplied, 0, 1, 2, 3)
	premulKTX := buildKTX2Container(vkFormatR8G8B8A8SR, 1, 1, ktx2SupercompNone, premulDFD, nil, []byte{100, 50, 0, 128}, 4)
	premulImg, _, err := decodeKTX2(premulKTX, nil)
	check(t, err)
	gotPremul := premulImg.NRGBAAt(0, 0)
	if math.Abs(float64(gotPremul.R)-199) > 2 || math.Abs(float64(gotPremul.G)-100) > 2 || gotPremul.A != 128 {
		t.Fatalf("expected un-premultiplied ~{199 100 0 128}, got %v", gotPremul)
	}
}
