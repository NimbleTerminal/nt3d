// Copyright (c) 2026 Neomantra Corp.
package nt3d

import "math"

// Colors are sRGB-encoded, as images and color.RGBA are. A Linear frame
// decodes them to linear light to light, interpolate, blend, and average
// them, and encodes the result: light then adds as it does physically, and
// blends and averages keep their brightness.

// srgbDecode is each 8-bit sRGB value's linear light, 0 to 1.
var srgbDecode = func() (t [256]float32) {
	for i := range t {
		t[i] = float32(decodeSRGB(float64(i) / 255))
	}
	return
}()

// srgbEncode is the 8-bit sRGB encoding of linear light at encodeSteps
// even steps from 0 to 1, fine enough to round as the exact formula does
// but for values within a hair of a rounding boundary.
const encodeSteps = 1 << 14

var srgbEncode = func() (t [encodeSteps + 1]uint8) {
	for i := range t {
		t[i] = uint8(encodeSRGB(float64(i)/encodeSteps)*255 + .5)
	}
	return
}()

func decodeSRGB(v float64) float64 {
	if v <= .04045 {
		return v / 12.92
	}
	return math.Pow((v+.055)/1.055, 2.4)
}

func encodeSRGB(v float64) float64 {
	if v <= .0031308 {
		return v * 12.92
	}
	return 1.055*math.Pow(v, 1/2.4) - .055
}

// encode8 is linear light, clamped to 0..1, as an 8-bit sRGB value.
func encode8(v float32) uint8 {
	return srgbEncode[int(max(0, min(1, v))*encodeSteps+.5)]
}

// ambientLight is a light's ambient level in the frame's color space. In
// linear light it is decoded as a color is, so a surface turned from the
// light keeps the brightness it has without linear light.
func ambientLight(f *Frame) float32 {
	if !f.Linear {
		return f.Light.Ambient
	}
	return float32(decodeSRGB(float64(max(0, min(1, f.Light.Ambient)))))
}
