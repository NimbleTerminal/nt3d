// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"encoding/binary"
	"errors"
	"image"
	"math/bits"

	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

const (
	basisHuffMaxSyms     = 1 << 14
	basisHuffMaxCodeSize = 16
	basisHuffFastBits    = 10

	etc1sColor5Pal0PrevHi = 9
	etc1sColor5Pal1PrevHi = 21

	etc1sEndpointPredRepeatLast = 256
	etc1sEndpointPredMinRepeat  = 3
	etc1sEndpointPredVLCBits    = 4
	etc1sCREndpointPredIndex    = 2
	etc1sNoEndpointPredIndex    = 3

	etc1sSelectorHistoryRLEThresh = 3
	etc1sSelectorHistoryRLEBits   = 6
	etc1sSelectorHistoryRLETotal  = 1 << etc1sSelectorHistoryRLEBits
)

var (
	basisCodelengthOrder = [21]int{17, 18, 19, 20, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15, 16}

	// BasisLZ logical selector 0..3 maps to ETC1 modifiers [-large, -small, +small, +large].
	etc1sModifiers = [8][4]int{
		{-8, -2, 2, 8},
		{-17, -5, 5, 17},
		{-29, -9, 9, 29},
		{-42, -13, 13, 42},
		{-60, -18, 18, 60},
		{-80, -24, 24, 80},
		{-106, -33, 33, 106},
		{-183, -47, 47, 183},
	}
)

type etc1sEndpoint struct {
	r8, g8, b8 uint8
	inten      uint8
}

type etc1sSelector [4]uint8 // Row y in 0..3; 2-bit selector for x at (row >> (x*2)) & 3.

type basisBitReader struct {
	buf    []byte
	byteOf int
	bitBuf uint64
	nBits  int
}

func newBasisBitReader(data []byte) basisBitReader {
	r := basisBitReader{buf: data}
	r.refill()
	return r
}

func (r *basisBitReader) refill() {
	for r.nBits <= 56 && r.byteOf < len(r.buf) {
		r.bitBuf |= uint64(r.buf[r.byteOf]) << r.nBits
		r.byteOf++
		r.nBits += 8
	}
}

func (r *basisBitReader) getBits(n int) (uint32, bool) {
	if n == 0 {
		return 0, true
	}
	if r.nBits < n {
		r.refill()
		if r.nBits < n {
			// Basis bitstreams allow zero-padded reads slightly past the final byte
			// during fast Huffman lookahead, but explicit fixed-width fields must exist.
			return 0, false
		}
	}
	v := uint32(r.bitBuf & ((uint64(1) << n) - 1))
	r.bitBuf >>= n
	r.nBits -= n
	return v, true
}

func (r *basisBitReader) decodeVLC(chunkBits int) (int, bool) {
	chunkSize := uint32(1) << chunkBits
	mask := chunkSize - 1
	var total uint64
	var ofs int
	for {
		if ofs > 30 {
			return 0, false
		}
		s, ok := r.getBits(chunkBits + 1)
		if !ok {
			return 0, false
		}
		total |= uint64(s&mask) << ofs
		ofs += chunkBits
		if s&chunkSize == 0 {
			break
		}
	}
	if total > 1<<24 {
		return 0, false
	}
	return int(total), true
}

type basisHuffTable struct {
	fast     [1 << basisHuffFastBits]uint32 // len<<16 | sym (0 if slow path or invalid)
	maxCode  [basisHuffMaxCodeSize + 2]int
	valPtr   [basisHuffMaxCodeSize + 1]int
	syms     []uint16
	codeSize []uint8
}

func buildBasisHuffman(codeSizes []uint8) (basisHuffTable, error) {
	var t basisHuffTable
	var numCodes [basisHuffMaxCodeSize + 1]int
	totalUsed := 0
	for _, sz := range codeSizes {
		if sz == 0 {
			continue
		}
		if sz > basisHuffMaxCodeSize {
			return t, errors.New("invalid BasisLZ Huffman code length")
		}
		numCodes[sz]++
		totalUsed++
	}
	if totalUsed == 0 {
		return t, errors.New("empty BasisLZ Huffman table")
	}

	var nextCode [basisHuffMaxCodeSize + 1]int
	code := 0
	for bitsLen := 1; bitsLen <= basisHuffMaxCodeSize; bitsLen++ {
		code = (code + numCodes[bitsLen-1]) << 1
		nextCode[bitsLen] = code
	}
	// Verify Kraft inequality doesn't overflow 16-bit prefix space.
	if code+numCodes[basisHuffMaxCodeSize] > (1 << basisHuffMaxCodeSize) {
		return t, errors.New("oversubscribed BasisLZ Huffman table")
	}

	t.syms = make([]uint16, totalUsed)
	t.codeSize = codeSizes
	offset := 0
	for bitsLen := 1; bitsLen <= basisHuffMaxCodeSize; bitsLen++ {
		if numCodes[bitsLen] == 0 {
			t.maxCode[bitsLen] = -1
			continue
		}
		t.valPtr[bitsLen] = offset - nextCode[bitsLen]
		t.maxCode[bitsLen] = nextCode[bitsLen] + numCodes[bitsLen] - 1
		offset += numCodes[bitsLen]
	}
	t.maxCode[basisHuffMaxCodeSize+1] = 0xFFFFF

	curCode := nextCode
	var symPos [basisHuffMaxCodeSize + 1]int
	posAccum := 0
	for bitsLen := 1; bitsLen <= basisHuffMaxCodeSize; bitsLen++ {
		symPos[bitsLen] = posAccum
		posAccum += numCodes[bitsLen]
	}

	for sym, sz := range codeSizes {
		if sz == 0 {
			continue
		}
		msbCode := curCode[sz]
		curCode[sz]++
		idx := symPos[sz]
		symPos[sz]++
		t.syms[idx] = uint16(sym)

		if sz <= basisHuffFastBits {
			rev := int(bits.Reverse16(uint16(msbCode)) >> (16 - sz))
			entry := (uint32(sz) << 16) | uint32(sym)
			step := 1 << sz
			for k := rev; k < len(t.fast); k += step {
				t.fast[k] = entry
			}
		}
	}
	return t, nil
}

func readBasisHuffmanTable(r *basisBitReader) (basisHuffTable, error) {
	totalSymsU, ok := r.getBits(14)
	if !ok || totalSymsU > basisHuffMaxSyms {
		return basisHuffTable{}, errors.New("invalid BasisLZ Huffman symbol count")
	}
	if totalSymsU == 0 {
		return basisHuffTable{}, nil
	}
	totalSyms := int(totalSymsU)
	numCLCodes, ok := r.getBits(5)
	if !ok || numCLCodes == 0 || int(numCLCodes) > len(basisCodelengthOrder) {
		return basisHuffTable{}, errors.New("invalid BasisLZ Huffman codelength count")
	}
	clSizes := make([]uint8, len(basisCodelengthOrder))
	for i := range int(numCLCodes) {
		sz, ok := r.getBits(3)
		if !ok {
			return basisHuffTable{}, errors.New("truncated BasisLZ Huffman codelengths")
		}
		clSizes[basisCodelengthOrder[i]] = uint8(sz)
	}
	clTable, err := buildBasisHuffman(clSizes)
	if err != nil {
		return basisHuffTable{}, err
	}

	codeSizes := make([]uint8, totalSyms)
	var prevSize uint8
	for i := 0; i < totalSyms; {
		sym, ok := clTable.decode(r)
		if !ok {
			return basisHuffTable{}, errors.New("corrupt BasisLZ Huffman codelength stream")
		}
		switch {
		case sym <= 16:
			codeSizes[i] = uint8(sym)
			if sym != 0 {
				prevSize = uint8(sym)
			}
			i++
		case sym == 17:
			run, ok := r.getBits(3)
			if !ok {
				return basisHuffTable{}, errors.New("truncated BasisLZ Huffman zero run")
			}
			n := int(run) + 3
			if i+n > totalSyms {
				return basisHuffTable{}, errors.New("BasisLZ Huffman zero run overflow")
			}
			i += n
		case sym == 18:
			run, ok := r.getBits(7)
			if !ok {
				return basisHuffTable{}, errors.New("truncated BasisLZ Huffman long zero run")
			}
			n := int(run) + 11
			if i+n > totalSyms {
				return basisHuffTable{}, errors.New("BasisLZ Huffman long zero run overflow")
			}
			i += n
		case sym == 19:
			if prevSize == 0 {
				return basisHuffTable{}, errors.New("BasisLZ Huffman repeat with zero previous size")
			}
			run, ok := r.getBits(2)
			if !ok {
				return basisHuffTable{}, errors.New("truncated BasisLZ Huffman repeat")
			}
			n := int(run) + 3
			if i+n > totalSyms {
				return basisHuffTable{}, errors.New("BasisLZ Huffman repeat overflow")
			}
			for range n {
				codeSizes[i] = prevSize
				i++
			}
		case sym == 20:
			if prevSize == 0 {
				return basisHuffTable{}, errors.New("BasisLZ Huffman long repeat with zero previous size")
			}
			run, ok := r.getBits(7)
			if !ok {
				return basisHuffTable{}, errors.New("truncated BasisLZ Huffman long repeat")
			}
			n := int(run) + 7
			if i+n > totalSyms {
				return basisHuffTable{}, errors.New("BasisLZ Huffman long repeat overflow")
			}
			for range n {
				codeSizes[i] = prevSize
				i++
			}
		default:
			return basisHuffTable{}, errors.New("invalid BasisLZ codelength symbol")
		}
	}
	return buildBasisHuffman(codeSizes)
}

func (t *basisHuffTable) decode(r *basisBitReader) (int, bool) {
	if len(t.syms) == 0 {
		return 0, false
	}
	if r.nBits < basisHuffMaxCodeSize {
		r.refill()
	}
	if entry := t.fast[r.bitBuf&((1<<basisHuffFastBits)-1)]; entry != 0 {
		sz := int(entry >> 16)
		if sz > r.nBits {
			return 0, false
		}
		r.bitBuf >>= sz
		r.nBits -= sz
		return int(entry & 0xFFFF), true
	}
	// Slow path for code lengths 11..16 (canonical MSB accumulator).
	code := 0
	for sz := 1; sz <= basisHuffMaxCodeSize; sz++ {
		if sz > r.nBits {
			return 0, false
		}
		bit := int((r.bitBuf >> (sz - 1)) & 1)
		code = (code << 1) | bit
		if code <= t.maxCode[sz] {
			idx := t.valPtr[sz] + code
			if idx < 0 || idx >= len(t.syms) {
				return 0, false
			}
			r.bitBuf >>= sz
			r.nBits -= sz
			return int(t.syms[idx]), true
		}
	}
	return 0, false
}

type basisApproxMTF struct {
	values []int
	rover  int
}

func newBasisApproxMTF(size int) basisApproxMTF {
	return basisApproxMTF{
		values: make([]int, size),
		rover:  size / 2,
	}
}

func (m *basisApproxMTF) add(v int) {
	if len(m.values) == 0 {
		return
	}
	m.values[m.rover] = v
	m.rover++
	if m.rover == len(m.values) {
		m.rover = len(m.values) / 2
	}
}

func (m *basisApproxMTF) use(idx int) {
	if idx > 0 {
		x := m.values[idx/2]
		y := m.values[idx]
		m.values[idx/2] = y
		m.values[idx] = x
	}
}

type etc1sSliceTables struct {
	endpointPred     basisHuffTable
	deltaEndpoint    basisHuffTable
	selector         basisHuffTable
	selectorRLE      basisHuffTable
	selectorHistSize int
}

func decodeKTX2ETC1S(level0, sgd []byte, imageCount uint64, width, height int, dfd ktx2DFD, budget *nt3dmesh.ByteBudget) (*image.NRGBA, bool, error) {
	if len(sgd) < 20 || imageCount == 0 || imageCount > 1<<20 {
		return nil, false, errors.New("invalid KTX2 BasisLZ global header")
	}
	endpointCount := int(binary.LittleEndian.Uint16(sgd[0:2]))
	selectorCount := int(binary.LittleEndian.Uint16(sgd[2:4]))
	endpointsLen := uint64(binary.LittleEndian.Uint32(sgd[4:8]))
	selectorsLen := uint64(binary.LittleEndian.Uint32(sgd[8:12]))
	tablesLen := uint64(binary.LittleEndian.Uint32(sgd[12:16]))
	extendedLen := uint64(binary.LittleEndian.Uint32(sgd[16:20]))

	if endpointCount == 0 || selectorCount == 0 {
		return nil, false, errors.New("empty KTX2 BasisLZ codebook")
	}
	descsBytes := imageCount * 20
	headerAndDescs := uint64(20) + descsBytes
	if headerAndDescs > uint64(len(sgd)) ||
		endpointsLen > uint64(len(sgd))-headerAndDescs ||
		selectorsLen > uint64(len(sgd))-headerAndDescs-endpointsLen ||
		tablesLen > uint64(len(sgd))-headerAndDescs-endpointsLen-selectorsLen ||
		extendedLen > uint64(len(sgd))-headerAndDescs-endpointsLen-selectorsLen-tablesLen {
		return nil, false, errors.New("KTX2 BasisLZ global sections out of bounds")
	}

	bx := (width + 3) / 4
	workCost := int64(endpointCount)*4 + int64(selectorCount)*4 + int64(bx)*16
	if err := budget.Take(workCost, "KTX2 ETC1S codebooks"); err != nil {
		return nil, false, err
	}

	// Level 0, layer 0, face 0 is imageDescs[0].
	desc0 := sgd[20:40]
	imageFlags := binary.LittleEndian.Uint32(desc0[0:4])
	rgbOff := uint64(binary.LittleEndian.Uint32(desc0[4:8]))
	rgbLen := uint64(binary.LittleEndian.Uint32(desc0[8:12]))
	alphaOff := uint64(binary.LittleEndian.Uint32(desc0[12:16]))
	alphaLen := uint64(binary.LittleEndian.Uint32(desc0[16:20]))

	if imageFlags&1 != 0 {
		return nil, false, errors.New("KTX2 ETC1S video P-frames are not supported")
	}
	if rgbLen == 0 || rgbOff > uint64(len(level0)) || rgbLen > uint64(len(level0))-rgbOff {
		return nil, false, errors.New("KTX2 ETC1S RGB slice out of bounds")
	}
	hasAlphaSlice := alphaLen > 0 || len(dfd.channels) >= 2
	if alphaLen > 0 && (alphaOff > uint64(len(level0)) || alphaLen > uint64(len(level0))-alphaOff) {
		return nil, false, errors.New("KTX2 ETC1S alpha slice out of bounds")
	}
	if hasAlphaSlice && alphaLen == 0 {
		return nil, false, errors.New("missing KTX2 ETC1S second slice")
	}

	epStart := headerAndDescs
	selStart := epStart + endpointsLen
	tabStart := selStart + selectorsLen

	endpoints, err := decodeETC1SEndpoints(sgd[epStart:epStart+endpointsLen], endpointCount)
	if err != nil {
		return nil, false, err
	}
	selectors, err := decodeETC1SSelectors(sgd[selStart:selStart+selectorsLen], selectorCount)
	if err != nil {
		return nil, false, err
	}
	tables, err := decodeETC1STables(sgd[tabStart : tabStart+tablesLen])
	if err != nil {
		return nil, false, err
	}

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	if err := decodeETC1SSlice(level0[rgbOff:rgbOff+rgbLen], endpoints, selectors, tables, width, height, img, 0); err != nil {
		return nil, false, err
	}

	twoCh := false
	if alphaLen > 0 {
		secondChan := uint8(khrDFChannelETC1SAAA)
		if len(dfd.channels) >= 2 {
			secondChan = dfd.channels[1]
		}
		mode := 1 // Copy G to A
		if secondChan == khrDFChannelETC1SGGG {
			mode = 2 // Two-channel RRR+GGG: copy slice1 G into G, clear B=0, A=255
			twoCh = true
		}
		if err := decodeETC1SSlice(level0[alphaOff:alphaOff+alphaLen], endpoints, selectors, tables, width, height, img, mode); err != nil {
			return nil, false, err
		}
	}

	if dfd.flags&khrDFFlagAlphaPremultiplied != 0 && !twoCh {
		unpremultiplyNRGBA(img.Pix)
	}
	return img, twoCh, nil
}

func decodeETC1SEndpoints(data []byte, count int) ([]etc1sEndpoint, error) {
	r := newBasisBitReader(data)
	colorModel0, err := readBasisHuffmanTable(&r)
	if err != nil {
		return nil, err
	}
	colorModel1, err := readBasisHuffmanTable(&r)
	if err != nil {
		return nil, err
	}
	colorModel2, err := readBasisHuffmanTable(&r)
	if err != nil {
		return nil, err
	}
	intenModel, err := readBasisHuffmanTable(&r)
	if err != nil {
		return nil, err
	}
	grayBit, ok := r.getBits(1)
	if !ok {
		return nil, errors.New("truncated BasisLZ endpoint header")
	}
	grayscale := grayBit != 0

	out := make([]etc1sEndpoint, count)
	prevColor5 := [3]int{16, 16, 16}
	prevInten := 0
	nCh := 3
	if grayscale {
		nCh = 1
	}
	for i := range count {
		intenDelta, ok := intenModel.decode(&r)
		if !ok {
			return nil, errors.New("corrupt BasisLZ endpoint intensity")
		}
		prevInten = (prevInten + intenDelta) & 7

		for c := range nCh {
			var delta int
			switch {
			case prevColor5[c] <= etc1sColor5Pal0PrevHi:
				delta, ok = colorModel0.decode(&r)
			case prevColor5[c] <= etc1sColor5Pal1PrevHi:
				delta, ok = colorModel1.decode(&r)
			default:
				delta, ok = colorModel2.decode(&r)
			}
			if !ok {
				return nil, errors.New("corrupt BasisLZ endpoint color")
			}
			prevColor5[c] = (prevColor5[c] + delta) & 31
		}
		if grayscale {
			prevColor5[1] = prevColor5[0]
			prevColor5[2] = prevColor5[0]
		}
		r5, g5, b5 := uint8(prevColor5[0]), uint8(prevColor5[1]), uint8(prevColor5[2])
		out[i] = etc1sEndpoint{
			r8:    (r5 << 3) | (r5 >> 2),
			g8:    (g5 << 3) | (g5 >> 2),
			b8:    (b5 << 3) | (b5 >> 2),
			inten: uint8(prevInten),
		}
	}
	return out, nil
}

func decodeETC1SSelectors(data []byte, count int) ([]etc1sSelector, error) {
	r := newBasisBitReader(data)
	globalFlag, ok1 := r.getBits(1)
	hybridFlag, ok2 := r.getBits(1)
	rawFlag, ok3 := r.getBits(1)
	if !ok1 || !ok2 || !ok3 {
		return nil, errors.New("truncated BasisLZ selector header")
	}
	if globalFlag != 0 || hybridFlag != 0 {
		return nil, errors.New("unsupported BasisLZ global/hybrid selector codebook")
	}

	out := make([]etc1sSelector, count)
	if rawFlag != 0 {
		for i := range count {
			for j := range 4 {
				b, ok := r.getBits(8)
				if !ok {
					return nil, errors.New("truncated BasisLZ raw selector codebook")
				}
				out[i][j] = uint8(b)
			}
		}
		return out, nil
	}

	deltaModel, err := readBasisHuffmanTable(&r)
	if err != nil {
		return nil, err
	}
	var prevBytes [4]uint8
	for i := range count {
		if i == 0 {
			for j := range 4 {
				b, ok := r.getBits(8)
				if !ok {
					return nil, errors.New("truncated BasisLZ first selector")
				}
				prevBytes[j] = uint8(b)
				out[i][j] = uint8(b)
			}
			continue
		}
		for j := range 4 {
			d, ok := deltaModel.decode(&r)
			if !ok || d > 255 {
				return nil, errors.New("corrupt BasisLZ selector delta")
			}
			cur := uint8(d) ^ prevBytes[j]
			prevBytes[j] = cur
			out[i][j] = cur
		}
	}
	return out, nil
}

func decodeETC1STables(data []byte) (etc1sSliceTables, error) {
	var t etc1sSliceTables
	r := newBasisBitReader(data)
	var err error
	if t.endpointPred, err = readBasisHuffmanTable(&r); err != nil {
		return t, err
	}
	if t.deltaEndpoint, err = readBasisHuffmanTable(&r); err != nil {
		return t, err
	}
	if t.selector, err = readBasisHuffmanTable(&r); err != nil {
		return t, err
	}
	if t.selectorRLE, err = readBasisHuffmanTable(&r); err != nil {
		return t, err
	}
	histSize, ok := r.getBits(13)
	if !ok || histSize > 4096 {
		return t, errors.New("invalid BasisLZ selector history buffer size")
	}
	t.selectorHistSize = int(histSize)
	return t, nil
}

type etc1sBlockPred struct {
	endpointIndex int
	predBits      uint8
}

// decodeETC1SSlice decodes one ETC1S slice into img:
//   - sliceMode 0: write RGB + A=255
//   - sliceMode 1: write decoded green into A
//   - sliceMode 2: write decoded green into G, set B=0, A=255 (RRR+GGG 2-channel)
func decodeETC1SSlice(slice []byte, endpoints []etc1sEndpoint, selectors []etc1sSelector, tables etc1sSliceTables, width, height int, img *image.NRGBA, sliceMode int) error {
	r := newBasisBitReader(slice)
	bx, by := (width+3)/4, (height+3)/4
	var blockPreds [2][]etc1sBlockPred
	blockPreds[0] = make([]etc1sBlockPred, bx)
	blockPreds[1] = make([]etc1sBlockPred, bx)

	epCount := len(endpoints)
	selCount := len(selectors)
	histSize := tables.selectorHistSize
	rleSym := selCount + histSize
	mtf := newBasisApproxMTF(histSize)

	var (
		curPredBits         uint32
		prevEndpointPredSym int
		endpointPredRepeat  int
		prevEndpointIndex   int
		curSelectorRLECount int
	)

	for blockY := range by {
		curRow := blockY & 1
		for blockX := range bx {
			if blockX&1 == 0 {
				if blockY&1 == 0 {
					if endpointPredRepeat > 0 {
						endpointPredRepeat--
						curPredBits = uint32(prevEndpointPredSym)
					} else {
						sym, ok := tables.endpointPred.decode(&r)
						if !ok {
							return errors.New("corrupt ETC1S endpoint predictor")
						}
						if sym == etc1sEndpointPredRepeatLast {
							v, ok := r.decodeVLC(etc1sEndpointPredVLCBits)
							if !ok {
								return errors.New("corrupt ETC1S predictor RLE count")
							}
							endpointPredRepeat = v + etc1sEndpointPredMinRepeat - 1
							curPredBits = uint32(prevEndpointPredSym)
						} else {
							if sym > 255 {
								return errors.New("invalid ETC1S predictor symbol")
							}
							curPredBits = uint32(sym)
							prevEndpointPredSym = sym
						}
					}
					if blockY+1 < by {
						blockPreds[curRow^1][blockX].predBits = uint8(curPredBits >> 4)
					}
				} else {
					curPredBits = uint32(blockPreds[curRow][blockX].predBits)
				}
			}

			pred := curPredBits & 3
			curPredBits >>= 2

			var epIdx int
			switch pred {
			case 0:
				if blockX == 0 {
					return errors.New("invalid ETC1S left predictor at x=0")
				}
				epIdx = prevEndpointIndex
			case 1:
				if blockY == 0 {
					return errors.New("invalid ETC1S upper predictor at y=0")
				}
				epIdx = blockPreds[curRow^1][blockX].endpointIndex
			case etc1sCREndpointPredIndex:
				if blockX == 0 || blockY == 0 {
					return errors.New("invalid ETC1S diagonal predictor at border")
				}
				epIdx = blockPreds[curRow^1][blockX-1].endpointIndex
			default: // etc1sNoEndpointPredIndex (3)
				delta, ok := tables.deltaEndpoint.decode(&r)
				if !ok {
					return errors.New("corrupt ETC1S endpoint delta")
				}
				epIdx = (prevEndpointIndex + delta) % epCount
			}
			prevEndpointIndex = epIdx
			blockPreds[curRow][blockX].endpointIndex = epIdx

			var selectorSym int
			if curSelectorRLECount > 0 {
				curSelectorRLECount--
				selectorSym = selCount
			} else {
				var ok bool
				selectorSym, ok = tables.selector.decode(&r)
				if !ok {
					return errors.New("corrupt ETC1S selector symbol")
				}
				if histSize > 0 && selectorSym == rleSym {
					runSym, ok := tables.selectorRLE.decode(&r)
					if !ok {
						return errors.New("corrupt ETC1S selector RLE symbol")
					}
					if runSym == etc1sSelectorHistoryRLETotal-1 {
						v, ok := r.decodeVLC(7)
						if !ok {
							return errors.New("corrupt ETC1S selector VLC run")
						}
						curSelectorRLECount = v + etc1sSelectorHistoryRLEThresh
					} else {
						curSelectorRLECount = runSym + etc1sSelectorHistoryRLEThresh
					}
					selectorSym = selCount
					curSelectorRLECount--
				}
			}

			var selIdx int
			if selectorSym < selCount {
				selIdx = selectorSym
				if histSize > 0 {
					mtf.add(selIdx)
				}
			} else {
				hIdx := selectorSym - selCount
				if hIdx < 0 || hIdx >= histSize {
					return errors.New("ETC1S selector history index out of range")
				}
				selIdx = mtf.values[hIdx]
				if selIdx < 0 || selIdx >= selCount {
					return errors.New("corrupt ETC1S selector history value")
				}
				mtf.use(hIdx)
			}

			ep := endpoints[epIdx]
			sel := selectors[selIdx]
			mods := etc1sModifiers[ep.inten]
			for py := range 4 {
				yy := blockY*4 + py
				if yy >= height {
					break
				}
				rowByte := sel[py]
				for px := range 4 {
					xx := blockX*4 + px
					if xx >= width {
						break
					}
					m := mods[(rowByte>>(px*2))&3]
					dstOff := yy*img.Stride + xx*4
					switch sliceMode {
					case 0:
						img.Pix[dstOff] = clampByte(int(ep.r8) + m)
						img.Pix[dstOff+1] = clampByte(int(ep.g8) + m)
						img.Pix[dstOff+2] = clampByte(int(ep.b8) + m)
						img.Pix[dstOff+3] = 255
					case 1:
						img.Pix[dstOff+3] = clampByte(int(ep.g8) + m)
					case 2:
						img.Pix[dstOff+1] = clampByte(int(ep.g8) + m)
						img.Pix[dstOff+2] = 0
						img.Pix[dstOff+3] = 255
					}
				}
			}
		}
	}
	return nil
}

func clampByte(v int) uint8 {
	return uint8(max(0, min(255, v)))
}
