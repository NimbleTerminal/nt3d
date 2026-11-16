// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"encoding/binary"
	"image"
)

const totalUASTCModes = 19

type uastcModeEntry struct {
	mode uint8
	len  uint8
	ok   bool
}

var (
	uastcModeLUT [128]uastcModeEntry

	uastcModeCodes = [totalUASTCModes][2]uint8{
		{0x01, 4}, // 0
		{0x35, 6}, // 1
		{0x1D, 5}, // 2
		{0x03, 5}, // 3
		{0x13, 5}, // 4
		{0x0B, 5}, // 5
		{0x1B, 5}, // 6
		{0x07, 5}, // 7
		{0x17, 5}, // 8 (solid RGBA)
		{0x0F, 5}, // 9
		{0x02, 3}, // 10
		{0x00, 2}, // 11
		{0x06, 3}, // 12
		{0x1F, 5}, // 13
		{0x0D, 5}, // 14
		{0x05, 7}, // 15
		{0x15, 6}, // 16
		{0x25, 6}, // 17
		{0x09, 4}, // 18
	}

	uastcWeightBits    = [totalUASTCModes]int{4, 2, 3, 2, 2, 3, 2, 2, 0, 2, 4, 2, 3, 1, 2, 4, 2, 2, 5}
	uastcEndpointRange = [totalUASTCModes]int{19, 20, 8, 7, 12, 20, 18, 12, 0, 8, 13, 13, 19, 20, 20, 20, 20, 20, 11}
	uastcSubsets       = [totalUASTCModes]int{1, 1, 2, 3, 2, 1, 1, 2, 0, 2, 1, 1, 1, 1, 1, 1, 2, 1, 1}
	uastcPlanes        = [totalUASTCModes]int{1, 1, 1, 1, 1, 1, 2, 1, 0, 1, 1, 2, 1, 2, 1, 1, 1, 2, 1}
	uastcComps         = [totalUASTCModes]int{3, 3, 3, 3, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 2, 2, 2, 3}
	uastcHasETC1Bias   = [totalUASTCModes]int{1, 1, 1, 1, 1, 1, 1, 1, 0, 1, 0, 0, 0, 1, 1, 1, 1, 1, 1}
	uastcHasBC1Hint0   = [totalUASTCModes]int{1, 1, 1, 1, 1, 1, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	uastcHasBC1Hint1   = [totalUASTCModes]int{1, 1, 1, 1, 1, 1, 1, 1, 0, 1, 0, 0, 0, 1, 1, 1, 1, 1, 1}
	uastcHasAlpha      = [totalUASTCModes]int{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0}

	// ASTC partition seeds for 2-subset UASTC modes (2, 4, 9, 16).
	uastcBC7Partitions2 = [30]int{
		28, 20, 16, 29, 91, 9, 107, 72, 149, 204,
		56, 88, 11, 14, 21, 30, 54, 60, 93, 102,
		115, 117, 125, 131, 133, 135, 150, 153, 154, 163,
	}

	// ASTC partition seeds for 2-subset UASTC mode 7.
	bc73UASTC2Partitions = [19]int{
		36, 48, 61, 137, 161, 183, 226, 281, 302, 307,
		479, 492, 532, 535, 547, 551, 567, 874, 902,
	}

	// ASTC partition seeds for 3-subset UASTC mode 3.
	uastcBC7Partitions3 = [11]int{
		260, 74, 32, 156, 183, 15, 745, 0, 335, 902, 254,
	}

	astcWeights1 = [2]uint32{0, 64}
	astcWeights2 = [4]uint32{0, 21, 43, 64}
	astcWeights3 = [8]uint32{0, 9, 18, 27, 37, 46, 55, 64}
	astcWeights4 = [16]uint32{0, 4, 8, 12, 17, 21, 25, 29, 35, 39, 43, 47, 52, 56, 60, 64}
	astcWeights5 = [32]uint32{
		0, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20, 22, 24, 26, 28, 30,
		34, 36, 38, 40, 42, 44, 46, 48, 50, 52, 54, 56, 58, 60, 62, 64,
	}

	uastcTritBits  = [6]int{0, 2, 4, 5, 7, 8}
	uastcTritMax   = [6]uint32{1, 3, 9, 27, 81, 243}
	uastcQuintBits = [4]int{0, 3, 5, 7}
	uastcQuintMax  = [4]uint32{1, 5, 25, 125}
)

func init() {
	for mode, c := range uastcModeCodes {
		code, length := c[0], c[1]
		step := 1 << length
		for v := int(code); v < 128; v += step {
			uastcModeLUT[v] = uastcModeEntry{mode: uint8(mode), len: length, ok: true}
		}
	}
}

func decodeKTX2UASTC(slice []byte, width, height int, dfd ktx2DFD) (*image.NRGBA, bool, error) {
	srgb := dfd.transfer == khrDFTransferSRGB
	channel := uint8(khrDFChannelUASTCRGBA)
	if len(dfd.channels) > 0 {
		channel = dfd.channels[0]
	}
	twoCh := channel == khrDFChannelUASTCRRRG
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	bx, by := (width+3)/4, (height+3)/4
	var blk [16]byte
	for yBlock := range by {
		for xBlock := range bx {
			off := (yBlock*bx + xBlock) * 16
			copy(blk[:], slice[off:off+16])
			pixels := unpackUASTCBlock(blk, srgb)
			for py := range 4 {
				yy := yBlock*4 + py
				if yy >= height {
					break
				}
				for px := range 4 {
					xx := xBlock*4 + px
					if xx >= width {
						break
					}
					c := pixels[py*4+px]
					switch channel {
					case khrDFChannelUASTCRGB:
						c[3] = 255
					case khrDFChannelUASTCRRR:
						c[1], c[2], c[3] = c[0], c[0], 255
					case khrDFChannelUASTCRRRG:
						c[1], c[2], c[3] = c[3], 0, 255
					}
					dstOff := yy*img.Stride + xx*4
					img.Pix[dstOff] = c[0]
					img.Pix[dstOff+1] = c[1]
					img.Pix[dstOff+2] = c[2]
					img.Pix[dstOff+3] = c[3]
				}
			}
		}
	}
	return img, twoCh, nil
}

func uastcReadBits(lo, hi uint64, pos, n int) uint32 {
	if n == 0 {
		return 0
	}
	mask := uint64((uint64(1) << n) - 1)
	if pos+n <= 64 {
		return uint32((lo >> pos) & mask)
	}
	if pos >= 64 {
		return uint32((hi >> (pos - 64)) & mask)
	}
	lowBits := 64 - pos
	v := (lo >> pos) | (hi << lowBits)
	return uint32(v & mask)
}

func unpackUASTCBlock(blk [16]byte, srgb bool) [16][4]uint8 {
	lo := binary.LittleEndian.Uint64(blk[0:8])
	hi := binary.LittleEndian.Uint64(blk[8:16])

	entry := uastcModeLUT[lo&0x7F]
	if !entry.ok {
		return [16][4]uint8{}
	}
	mode := int(entry.mode)
	pos := int(entry.len)

	if mode == 8 {
		c := [4]uint8{
			uint8(uastcReadBits(lo, hi, 5, 8)),
			uint8(uastcReadBits(lo, hi, 13, 8)),
			uint8(uastcReadBits(lo, hi, 21, 8)),
			uint8(uastcReadBits(lo, hi, 29, 8)),
		}
		var out [16][4]uint8
		for i := range out {
			out[i] = c
		}
		return out
	}

	// Skip BC1, ETC1, and ETC2 transcoding acceleration hints.
	pos += uastcHasBC1Hint0[mode] + uastcHasBC1Hint1[mode] + 8
	if uastcHasETC1Bias[mode] != 0 {
		pos += 5
	}
	if uastcHasAlpha[mode] != 0 {
		pos += 8
	}

	subsets := uastcSubsets[mode]
	planes := uastcPlanes[mode]
	var part [16]int
	switch subsets {
	case 2:
		pat := int(uastcReadBits(lo, hi, pos, 5))
		pos += 5
		var seed int
		if mode == 7 {
			if pat >= len(bc73UASTC2Partitions) {
				return [16][4]uint8{}
			}
			seed = bc73UASTC2Partitions[pat]
		} else {
			if pat >= len(uastcBC7Partitions2) {
				return [16][4]uint8{}
			}
			seed = uastcBC7Partitions2[pat]
		}
		for y := range 4 {
			for x := range 4 {
				part[y*4+x] = astcSelectPartition(seed, x, y, 0, 2, true)
			}
		}
	case 3:
		pat := int(uastcReadBits(lo, hi, pos, 4))
		pos += 4
		if pat >= len(uastcBC7Partitions3) {
			return [16][4]uint8{}
		}
		seed := uastcBC7Partitions3[pat]
		for y := range 4 {
			for x := range 4 {
				part[y*4+x] = astcSelectPartition(seed, x, y, 0, 3, true)
			}
		}
	}

	compSel := 0
	if planes == 2 {
		if mode == 17 {
			compSel = 3
		} else {
			compSel = int(uastcReadBits(lo, hi, pos, 2))
			pos += 2
		}
	}

	comps := uastcComps[mode]
	numEndpoints := subsets * comps * 2
	epRange := uastcEndpointRange[mode]
	ebits, hasTrit, hasQuint := uastcRangeDesc(epRange)

	var tq [18]uint32
	if hasTrit {
		for i := 0; i < numEndpoints; {
			n := min(5, numEndpoints-i)
			nb := uastcTritBits[n]
			v := uastcReadBits(lo, hi, pos, nb)
			pos += nb
			if v >= uastcTritMax[n] {
				return [16][4]uint8{}
			}
			for j := range n {
				tq[i+j] = v % 3
				v /= 3
			}
			i += n
		}
	} else if hasQuint {
		for i := 0; i < numEndpoints; {
			n := min(3, numEndpoints-i)
			nb := uastcQuintBits[n]
			v := uastcReadBits(lo, hi, pos, nb)
			pos += nb
			if v >= uastcQuintMax[n] {
				return [16][4]uint8{}
			}
			for j := range n {
				tq[i+j] = v % 5
				v /= 5
			}
			i += n
		}
	}

	var ep [18]uint8
	for i := range numEndpoints {
		eb := uastcReadBits(lo, hi, pos, ebits)
		pos += ebits
		ep[i] = unquantASTCEndpoint(epRange, tq[i], eb)
	}

	var e0, e1 [3][4]uint8
	for s := range subsets {
		base := s * comps * 2
		switch comps {
		case 3:
			e0[s] = [4]uint8{ep[base+0], ep[base+2], ep[base+4], 255}
			e1[s] = [4]uint8{ep[base+1], ep[base+3], ep[base+5], 255}
		case 4:
			e0[s] = [4]uint8{ep[base+0], ep[base+2], ep[base+4], ep[base+6]}
			e1[s] = [4]uint8{ep[base+1], ep[base+3], ep[base+5], ep[base+7]}
		case 2:
			l0, l1, a0, a1 := ep[base+0], ep[base+1], ep[base+2], ep[base+3]
			e0[s] = [4]uint8{l0, l0, l0, a0}
			e1[s] = [4]uint8{l1, l1, l1, a1}
		}
	}

	wBits := uastcWeightBits[mode]
	var w0, w1 [16]uint32
	if planes == 2 {
		for t := range 16 {
			nb := wBits
			if t == 0 {
				nb = wBits - 1
			}
			q0 := uastcReadBits(lo, hi, pos, nb)
			pos += nb
			q1 := uastcReadBits(lo, hi, pos, nb)
			pos += nb
			w0[t] = unquantASTCWeight(wBits, q0)
			w1[t] = unquantASTCWeight(wBits, q1)
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
			q := uastcReadBits(lo, hi, pos, nb)
			pos += nb
			w0[t] = unquantASTCWeight(wBits, q)
		}
	}

	var out [16][4]uint8
	for t := range 16 {
		s := part[t]
		for c := range 4 {
			w := w0[t]
			if planes == 2 && c == compSel {
				w = w1[t]
			}
			out[t][c] = astcInterpolate(e0[s][c], e1[s][c], w, srgb && c < 3)
		}
		if comps == 3 {
			out[t][3] = 255
		}
	}
	return out
}

func uastcRangeDesc(epRange int) (ebits int, hasTrit, hasQuint bool) {
	switch epRange {
	case 7:
		return 2, true, false
	case 8:
		return 4, false, false
	case 11:
		return 5, false, false
	case 12:
		return 3, false, true
	case 13:
		return 4, true, false
	case 18:
		return 5, false, true
	case 19:
		return 6, true, false
	default: // 20
		return 8, false, false
	}
}

func unquantASTCEndpoint(epRange int, d, bits uint32) uint8 {
	switch epRange {
	case 8: // 4-bit
		return uint8((bits << 4) | bits)
	case 11: // 5-bit
		return uint8((bits << 3) | (bits >> 2))
	case 20: // 8-bit
		return uint8(bits)
	}

	var A uint32
	if bits&1 != 0 {
		A = 0x1FF
	}
	var B, C uint32
	switch epRange {
	case 7: // 12 levels: 1 trit + 2 bits (ba), B = b000b0bb0, C = 93
		b := (bits >> 1) & 1
		B = (b << 8) | (b << 4) | (b << 2) | (b << 1)
		C = 93
	case 12: // 40 levels: 1 quint + 3 bits (cba), B = cb0000cbc, C = 26
		b := (bits >> 1) & 1
		c := (bits >> 2) & 1
		B = (c << 8) | (b << 7) | (c << 2) | (b << 1) | c
		C = 26
	case 13: // 48 levels: 1 trit + 4 bits (dcba), B = dcb000dcb, C = 22
		dcb := (bits >> 1) & 7
		B = (dcb << 6) | dcb
		C = 22
	case 18: // 160 levels: 1 quint + 5 bits (edcba), B = edcb0000e, C = 6
		edcb := (bits >> 1) & 15
		e := (bits >> 4) & 1
		B = (edcb << 5) | e
		C = 6
	case 19: // 192 levels: 1 trit + 6 bits (fedcba), B = fedcb000f, C = 5
		fedcb := (bits >> 1) & 31
		f := (bits >> 5) & 1
		B = (fedcb << 4) | f
		C = 5
	}
	T := d*C + B
	T ^= A
	return uint8((A & 0x80) | (T >> 2))
}

func unquantASTCWeight(bits int, q uint32) uint32 {
	switch bits {
	case 1:
		return astcWeights1[q&1]
	case 2:
		return astcWeights2[q&3]
	case 3:
		return astcWeights3[q&7]
	case 4:
		return astcWeights4[q&15]
	default:
		return astcWeights5[q&31]
	}
}

func astcInterpolate(l8, h8 uint8, w uint32, srgb bool) uint8 {
	var l, h uint32
	if srgb {
		l = (uint32(l8) << 8) | 0x80
		h = (uint32(h8) << 8) | 0x80
	} else {
		l = (uint32(l8) << 8) | uint32(l8)
		h = (uint32(h8) << 8) | uint32(h8)
	}
	k := (l*(64-w) + h*w + 32) >> 6
	return uint8(k >> 8)
}

func astcHash52(p uint32) uint32 {
	p ^= p >> 15
	p -= p << 17
	p += p << 7
	p += p << 4
	p ^= p >> 5
	p += p << 16
	p ^= p >> 7
	p ^= p >> 3
	p ^= p << 6
	p ^= p >> 17
	return p
}

func astcSelectPartition(seed, x, y, z, partitionCount int, smallBlock bool) int {
	if smallBlock {
		x <<= 1
		y <<= 1
		z <<= 1
	}
	seed += (partitionCount - 1) * 1024
	rnum := astcHash52(uint32(seed))

	seed1 := rnum & 0xF
	seed2 := (rnum >> 4) & 0xF
	seed3 := (rnum >> 8) & 0xF
	seed4 := (rnum >> 12) & 0xF
	seed5 := (rnum >> 16) & 0xF
	seed6 := (rnum >> 20) & 0xF
	seed7 := (rnum >> 24) & 0xF
	seed8 := (rnum >> 28) & 0xF
	seed9 := (rnum >> 18) & 0xF
	seed10 := (rnum >> 22) & 0xF
	seed11 := (rnum >> 26) & 0xF
	seed12 := ((rnum >> 30) | (rnum << 2)) & 0xF

	seed1 *= seed1
	seed2 *= seed2
	seed3 *= seed3
	seed4 *= seed4
	seed5 *= seed5
	seed6 *= seed6
	seed7 *= seed7
	seed8 *= seed8
	seed9 *= seed9
	seed10 *= seed10
	seed11 *= seed11
	seed12 *= seed12

	var sh1, sh2, sh3 uint32
	if seed&1 != 0 {
		if seed&2 != 0 {
			sh1 = 4
		} else {
			sh1 = 5
		}
		if partitionCount == 3 {
			sh2 = 6
		} else {
			sh2 = 5
		}
	} else {
		if partitionCount == 3 {
			sh1 = 6
		} else {
			sh1 = 5
		}
		if seed&2 != 0 {
			sh2 = 4
		} else {
			sh2 = 5
		}
	}
	if seed&0x10 != 0 {
		sh3 = sh1
	} else {
		sh3 = sh2
	}

	seed1 >>= sh1
	seed2 >>= sh2
	seed3 >>= sh1
	seed4 >>= sh2
	seed5 >>= sh1
	seed6 >>= sh2
	seed7 >>= sh1
	seed8 >>= sh2
	seed9 >>= sh3
	seed10 >>= sh3
	seed11 >>= sh3
	seed12 >>= sh3

	a := (int(seed1)*x + int(seed2)*y + int(seed11)*z + int(rnum>>14)) & 0x3F
	b := (int(seed3)*x + int(seed4)*y + int(seed12)*z + int(rnum>>10)) & 0x3F
	c := (int(seed5)*x + int(seed6)*y + int(seed9)*z + int(rnum>>6)) & 0x3F
	d := (int(seed7)*x + int(seed8)*y + int(seed10)*z + int(rnum>>2)) & 0x3F

	if partitionCount < 4 {
		d = 0
	}
	if partitionCount < 3 {
		c = 0
	}
	if a >= b && a >= c && a >= d {
		return 0
	}
	if b >= c && b >= d {
		return 1
	}
	if c >= d {
		return 2
	}
	return 3
}
