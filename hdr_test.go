// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"bytes"
	"fmt"
	"image/color"
	"math"
	"strings"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
	nt3dsh "nimbleterminal.dev/nt3d/sh"
)

func floatToRGBE(r, g, b float64) [4]byte {
	v := max(r, max(g, b))
	if v < 1e-32 {
		return [4]byte{0, 0, 0, 0}
	}
	mant, exp := math.Frexp(v)
	scale := mant * 256.0 / v
	clamp8 := func(x float64) byte {
		c := int(x * scale)
		if c < 0 {
			return 0
		}
		if c > 255 {
			return 255
		}
		return byte(c)
	}
	return [4]byte{clamp8(r), clamp8(g), clamp8(b), byte(exp + 128)}
}

func encodeRLEChannel(dst []byte, ch []byte) []byte {
	for i := 0; i < len(ch); {
		run := 1
		for i+run < len(ch) && run < 127 && ch[i+run] == ch[i] {
			run++
		}
		if run >= 4 {
			dst = append(dst, byte(128+run), ch[i])
			i += run
			continue
		}
		lit := 1
		for i+lit < len(ch) && lit < 128 {
			r := 1
			for i+lit+r < len(ch) && r < 4 && ch[i+lit+r] == ch[i+lit] {
				r++
			}
			if r >= 4 {
				break
			}
			lit++
		}
		dst = append(dst, byte(lit))
		dst = append(dst, ch[i:i+lit]...)
		i += lit
	}
	return dst
}

func encodeRadianceHDR(w, h int, rle bool, exposure float64, resLine string, fn func(x, y int) [3]float64) []byte {
	var buf bytes.Buffer
	buf.WriteString("#?RADIANCE\n")
	buf.WriteString("# synthetic HDR panorama\n")
	buf.WriteString("FORMAT=32-bit_rle_rgbe\n")
	if exposure > 0 && exposure != 1 {
		fmt.Fprintf(&buf, "EXPOSURE=%g\n", exposure)
	}
	buf.WriteString("\n")
	if resLine == "" {
		resLine = fmt.Sprintf("-Y %d +X %d", h, w)
	}
	buf.WriteString(resLine + "\n")

	row := make([][4]byte, w)
	ch := make([]byte, w)
	for y := range h {
		for x := range w {
			c := fn(x, y)
			row[x] = floatToRGBE(c[0], c[1], c[2])
		}
		if rle && w >= 8 && w <= 0x7fff {
			buf.Write([]byte{2, 2, byte(w >> 8), byte(w)})
			for cIdx := range 4 {
				for x := range w {
					ch[x] = row[x][cIdx]
				}
				var enc []byte
				enc = encodeRLEChannel(enc, ch)
				buf.Write(enc)
			}
		} else {
			for x := range w {
				buf.Write(row[x][:])
			}
		}
	}
	return buf.Bytes()
}

func testSunHDREnvironment(t *testing.T) *Environment {
	t.Helper()
	const w, h = 64, 32
	data := encodeRadianceHDR(w, h, true, 1.0, "", func(x, y int) [3]float64 {
		// Sun centered at (+X) = (w/2, h/2) with unclamped HDR radiance 14.0.
		dx := float64(x) - float64(w)/2 + 0.5
		dy := float64(y) - float64(h)/2 + 0.5
		if dx*dx+dy*dy <= 6.5 {
			return [3]float64{14.0, 11.5, 8.0}
		}
		v := float64(y) / float64(h-1)
		return [3]float64{0.08 + 0.12*v, 0.16 * (1 - 0.5*v), 0.32 * (1 - 0.6*v)}
	})
	env, err := NewHDREnvironment(data)
	check(t, err)
	return env
}

func TestHDREnvironmentDecodeRLEUncompressedExposureAndFlip(t *testing.T) {
	// Adaptive RLE with runs + literals and unclamped radiance > 1.0.
	env := testSunHDREnvironment(t)
	sun := env.sample(nt3dmath.Vec3{X: 1})
	if sun[0] < 12.0 || sun[1] < 10.0 || sun[2] < 7.0 {
		t.Fatalf("expected unclamped HDR sun > 10, got %v", sun)
	}
	back := env.sample(nt3dmath.Vec3{X: -1})
	if back[0] > 0.5 || back[2] > 0.5 {
		t.Fatalf("expected dim sky opposite sun, got %v", back)
	}

	// DiffuseSH reflects bright +X sun energy.
	sh := env.DiffuseSH()
	plusX := nt3dsh.Evaluate(sh[:], nt3dmath.Vec3{X: 1})
	minusX := nt3dsh.Evaluate(sh[:], nt3dmath.Vec3{X: -1})
	if plusX[0] <= minusX[0] {
		t.Fatalf("expected +X irradiance (%v) > -X irradiance (%v)", plusX, minusX)
	}

	// Uncompressed small panorama (< 8 wide) + EXPOSURE=2.0 divides stored values by 2.
	uncomp := encodeRadianceHDR(6, 4, false, 2.0, "", func(x, y int) [3]float64 {
		return [3]float64{8.0, 4.0, 2.0}
	})
	envUncomp, err := NewHDREnvironment(uncomp)
	check(t, err)
	got := envUncomp.sample(nt3dmath.Vec3{X: 1})
	if math.Abs(float64(got[0]-4.0)) > 0.15 || math.Abs(float64(got[1]-2.0)) > 0.1 || math.Abs(float64(got[2]-1.0)) > 0.08 {
		t.Fatalf("EXPOSURE=2.0 scaling mismatch: got %v, want ~[4 2 1]", got)
	}

	// +Y / -X flipped orientation places row 0 at bottom (-Z) and reverses columns.
	flipped := encodeRadianceHDR(16, 8, true, 1.0, "+Y 8 -X 16", func(x, y int) [3]float64 {
		if y == 7 && x == 15 {
			return [3]float64{6.0, 0, 0}
		}
		return [3]float64{0, 0, 0.5}
	})
	envFlip, err := NewHDREnvironment(flipped)
	check(t, err)
	topCorner := envFlip.hdrBase.pix[:3]
	if topCorner[0] < 5.5 {
		t.Fatalf("expected flipped pixel at (0,0) to have R~6, got %v", topCorner)
	}
}

func TestHDREnvironmentSkyBloomSpecularIBLAndWireframe(t *testing.T) {
	env := testSunHDREnvironment(t)
	s := NewScene()
	s.SetEnvironment(env)

	smoothSpec := env.sampleSpecular(nt3dmath.Vec3{X: 1}, 0.0)
	roughSpec := env.sampleSpecular(nt3dmath.Vec3{X: 1}, 0.85)
	if smoothSpec[0] < 10.0 || smoothSpec[0] <= roughSpec[0]*2 {
		t.Fatalf("expected sharp HDR sun specular mip (%v) >> rough mip (%v)", smoothSpec, roughSpec)
	}

	// Beta = 180 places eye at (-Distance, 0, 0) looking straight along +X at the sun!
	cam := DefaultCamera()
	cam.Alpha, cam.Beta, cam.Distance = 0, 180, 3
	f, err := s.Frame(cam, 64, 48)
	check(t, err)
	f.Linear = true
	f.PostProcess = PostProcess{
		Bloom:    Bloom{Strength: 0.8, Threshold: 1.5, Radius: 0.8},
		Exposure: 0.6,
		ToneMap:  ACESToneMap,
	}

	withBloom := softwareRender(f)
	fNoBloom := f
	fNoBloom.PostProcess.Bloom = Bloom{}
	noBloom := softwareRender(fNoBloom)

	// Measure total red energy outside the central sun disk to prove HDR sky bloom spreads light.
	var sumBloom, sumPlain int
	for y := range 48 {
		for x := range 64 {
			dx, dy := x-32, y-24
			if dx*dx+dy*dy > 64 {
				sumBloom += int(at(withBloom, x, y).R)
				sumPlain += int(at(noBloom, x, y).R)
			}
		}
	}
	if sumBloom <= sumPlain+200 {
		t.Fatalf("expected HDR sky sun to bloom into surrounding sky: sumBloom=%d sumPlain=%d", sumBloom, sumPlain)
	}

	// Wireframe fallback clamps HDR sky cleanly to 8-bit ANSI background cells.
	wire, err := RenderWireframe(f, 20, 10)
	check(t, err)
	if !strings.Contains(wire, "48;") {
		t.Fatal("wireframe missing HDR sky background ANSI codes")
	}
}

func TestHDREnvironmentValidationErrors(t *testing.T) {
	bad := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"not-radiance", []byte("PNG\r\n\n-Y 4 +X 4\n")},
		{"bad-format", []byte("#?RADIANCE\nFORMAT=32-bit_rle_xyze\n\n-Y 4 +X 4\n")},
		{"bad-exposure", []byte("#?RADIANCE\nEXPOSURE=-1\n\n-Y 4 +X 4\n")},
		{"exposure-underflow", []byte("#?RADIANCE\nEXPOSURE=1e-200\nEXPOSURE=1e-200\n\n-Y 4 +X 4\n")},
		{"missing-blank", []byte("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n")},
		{"bad-res", []byte("#?RADIANCE\n\n+Z 4 +X 4\n")},
		{"oversize", []byte("#?RADIANCE\n\n-Y 9000 +X 16\n")},
		{"truncated-large-dim", []byte("#?RADIANCE\n\n-Y 8192 +X 8192\n\x02\x02\x20\x00")},
		{"truncated-scanline", []byte("#?RADIANCE\n\n-Y 2 +X 2\n\x00\x00")},
		{"rle-width-mismatch", []byte("#?RADIANCE\n\n-Y 1 +X 8\n\x02\x02\x00\x09")},
		{"rle-overrun", []byte("#?RADIANCE\n\n-Y 1 +X 8\n\x02\x02\x00\x08\xff\x80")},
	}
	for _, tc := range bad {
		if _, err := NewHDREnvironment(tc.data); err == nil {
			t.Errorf("%s: expected error from NewHDREnvironment", tc.name)
		}
	}

	env := testSunHDREnvironment(t)
	if got := env.pixel(nt3dmath.Vec3{X: 1}); got != (color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("expected HDR sun LDR fallback to clamp to white, got %v", got)
	}
}

func FuzzNewHDREnvironment(f *testing.F) {
	f.Add(encodeRadianceHDR(8, 4, true, 1.0, "", func(x, y int) [3]float64 {
		return [3]float64{float64(x) * 0.25, float64(y) * 0.5, 2.0}
	}))
	f.Add(encodeRadianceHDR(4, 3, false, 0.5, "+Y 3 -X 4", func(x, y int) [3]float64 {
		return [3]float64{float64(x + 1), float64(y), 0}
	}))
	f.Add([]byte("FORMAT=32-bit_rle_rgbe\n\n-Y 1 +X 1\n\x80\x40\x20\x81"))
	f.Add([]byte("#?RADIANCE\n\n-Y 1 +X 8\n\x02\x02\x00\x08\x88\x10\x88\x20\x88\x30\x88\x81"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			return
		}
		env, err := NewHDREnvironment(data)
		if err != nil {
			return
		}
		if env == nil {
			t.Fatal("nil Environment without error")
		}
		for _, dir := range []nt3dmath.Vec3{{X: 1}, {Y: -1}, {Z: 1}, {Z: -1}} {
			c := env.sample(dir)
			for i, v := range c {
				if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) || v < 0 {
					t.Fatalf("non-finite or negative HDR sample[%d]=%v for %v", i, v, dir)
				}
			}
			if p := env.pixel(dir); p.A != 255 {
				t.Fatalf("unexpected LDR sky alpha %d", p.A)
			}
		}
	})
}
