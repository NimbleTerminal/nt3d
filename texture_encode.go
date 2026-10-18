// Copyright (c) 2026 Neomantra Corp.
package nt3d

import "math"

// opaqueEncodeBoundary[i] is the first float32 input whose *existing*
// encoded-frame conversion rounds above byte i. This accounts for rounding
// both the encoded float to float32 and its product with 255 before adding .5.
// It is one shared 1020-byte table, independent of texture count and size.
var opaqueEncodeBoundary = func() (out [255]float32) {
	for i := range out {
		lo, hi := uint32(0), math.Float32bits(1)
		for lo < hi {
			mid := lo + (hi-lo)/2
			v := float32(encodeSRGB(float64(math.Float32frombits(mid)))) * 255
			if v+.5 >= float32(i+1) {
				hi = mid
			} else {
				lo = mid + 1
			}
		}
		out[i] = math.Float32frombits(lo)
	}
	return
}()

// encodeOpaque8 is exact for a sampled linear channel going straight to an
// opaque, unlit, untinted encoded-frame pixel. The existing coarse LUT gives
// a cheap estimate; boundary corrections restore the original rounded byte.
// Never use this in place of intermediate float encoding for lighting/tints.
func encodeOpaque8(v float32) uint8 {
	c := encode8(v)
	for c > 0 && v < opaqueEncodeBoundary[c-1] {
		c--
	}
	for c < 255 && v >= opaqueEncodeBoundary[c] {
		c++
	}
	return c
}
