// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"os"
	"path/filepath"
	"testing"
)

type csplat struct {
	position [3]float64
	logScale [3]float64
	rotation [4]float64 // w x y z, unit
	dc       [3]float64
	opacity  float64   // 0..1, after the sigmoid
	rest     []float64 // channel-major, len 0 or 9
}

func vec(v [3]float64) nt3dmath.Vec3 {
	return nt3dmath.Vec3{X: float32(v[0]), Y: float32(v[1]), Z: float32(v[2])}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func length(v nt3dmath.Vec3) float64 {
	return math.Sqrt(float64(v.X)*float64(v.X) + float64(v.Y)*float64(v.Y) + float64(v.Z)*float64(v.Z))
}

func assertNear(t *testing.T, got, want nt3dmath.Vec3, tolerance float64) {
	t.Helper()
	if d := length(nt3dmath.Vec3{X: got.X - want.X, Y: got.Y - want.Y, Z: got.Z - want.Z}); d > tolerance {
		t.Fatalf("got %v, want %v: distance %g exceeds %g", got, want, d, tolerance)
	}
}

// assertAxis checks an axis's length to a relative tolerance and its direction to an angle.
func assertAxis(t *testing.T, got, want nt3dmath.Vec3, relative, angle float64) {
	t.Helper()
	lg, lw := length(got), length(want)
	if math.Abs(lg-lw) > relative*lw {
		t.Fatalf("axis %v, want %v: length %g vs %g", got, want, lg, lw)
	}
	dot := float64(got.X)*float64(want.X) + float64(got.Y)*float64(want.Y) + float64(got.Z)*float64(want.Z)
	if a := math.Acos(math.Max(-1, math.Min(1, dot/(lg*lw)))); a > angle {
		t.Fatalf("axis %v, want %v: angle %g exceeds %g", got, want, a, angle)
	}
}

func packUnorm(v float64, bits int) uint32 {
	top := float64(uint32(1)<<bits - 1)
	return uint32(math.Max(0, math.Min(top, math.Floor(v*top+.5))))
}

func normalize(v, lo, hi float64) float64 {
	if hi-lo < 1e-5 {
		return 0
	}
	return (v - lo) / (hi - lo)
}

// encodeCompressedPLY writes splat-transform's layout: 256-splat chunks with
// 18 (or, with colorBounds false, 12) float32 bounds, 4 uint32 per splat,
// and an sh element of uchar f_rest when rest is present.
func encodeCompressedPLY(t *testing.T, splats []csplat, colorBounds bool) []byte {
	t.Helper()
	const c0 = 0.28209479177387814
	chunks := (len(splats) + 255) / 256
	names := compressedChunkProperties
	if !colorBounds {
		names = names[:12]
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "ply\nformat binary_little_endian 1.0\nelement chunk %d\n", chunks)
	for _, n := range names {
		fmt.Fprintf(&b, "property float %s\n", n)
	}
	fmt.Fprintf(&b, "element vertex %d\n", len(splats))
	for _, n := range []string{"packed_position", "packed_rotation", "packed_scale", "packed_color"} {
		fmt.Fprintf(&b, "property uint %s\n", n)
	}
	rest := len(splats[0].rest)
	if rest > 0 {
		fmt.Fprintf(&b, "element sh %d\n", len(splats))
		for j := 0; j < rest; j++ {
			fmt.Fprintf(&b, "property uchar f_rest_%d\n", j)
		}
	}
	b.WriteString("end_header\n")
	put := func(v any) { binary.Write(&b, binary.LittleEndian, v) }
	color := func(s csplat, k int) float64 { return .5 + c0*s.dc[k] }
	type bounds struct{ lo, hi [3]float64 }
	var position, scale, shade = make([]bounds, chunks), make([]bounds, chunks), make([]bounds, chunks)
	for c := range chunks {
		for _, set := range []*bounds{&position[c], &scale[c], &shade[c]} {
			for k := range 3 {
				set.lo[k], set.hi[k] = math.Inf(1), math.Inf(-1)
			}
		}
		// The last chunk's bounds use the last splat repeated.
		for i := c * 256; i < (c+1)*256; i++ {
			s := splats[min(i, len(splats)-1)]
			for k := range 3 {
				for _, set := range []struct {
					b *bounds
					v float64
				}{{&position[c], s.position[k]}, {&scale[c], s.logScale[k]}, {&shade[c], color(s, k)}} {
					set.b.lo[k], set.b.hi[k] = math.Min(set.b.lo[k], set.v), math.Max(set.b.hi[k], set.v)
				}
			}
		}
		for _, set := range []bounds{position[c], scale[c]} {
			for _, side := range [][3]float64{set.lo, set.hi} {
				for _, v := range side {
					put(float32(v))
				}
			}
		}
		if colorBounds {
			for _, side := range [][3]float64{shade[c].lo, shade[c].hi} {
				for _, v := range side {
					put(float32(v))
				}
			}
		}
	}
	pack3 := func(s csplat, set bounds, get func(csplat, int) float64) uint32 {
		var n [3]float64
		for k := range 3 {
			n[k] = normalize(get(s, k), set.lo[k], set.hi[k])
		}
		return packUnorm(n[0], 11)<<21 | packUnorm(n[1], 10)<<11 | packUnorm(n[2], 11)
	}
	for i, s := range splats {
		c := i / 256
		put(pack3(s, position[c], func(s csplat, k int) float64 { return s.position[k] }))
		q := s.rotation
		largest := 0
		for k := 1; k < 4; k++ {
			if math.Abs(q[k]) > math.Abs(q[largest]) {
				largest = k
			}
		}
		if q[largest] < 0 {
			for k := range q {
				q[k] = -q[k]
			}
		}
		packed := uint32(largest) << 30
		shift := 20
		for k := range 4 {
			if k != largest {
				packed |= packUnorm(q[k]*math.Sqrt2/2+.5, 10) << shift
				shift -= 10
			}
		}
		put(packed)
		put(pack3(s, scale[c], func(s csplat, k int) float64 { return s.logScale[k] }))
		var rgb [3]uint32
		for k := range 3 {
			v := color(s, k)
			if colorBounds {
				v = normalize(v, shade[c].lo[k], shade[c].hi[k])
			}
			rgb[k] = packUnorm(v, 8)
		}
		put(rgb[0]<<24 | rgb[1]<<16 | rgb[2]<<8 | packUnorm(s.opacity, 8))
	}
	if rest > 0 {
		for _, s := range splats {
			for _, v := range s.rest {
				put(uint8(math.Max(0, math.Min(255, math.Trunc((v/8+.5)*256)))))
			}
		}
	}
	return b.Bytes()
}

func TestCompressedPLYKnownValues(t *testing.T) {
	splats := []csplat{
		{[3]float64{1, 2, 3}, [3]float64{-2, -1, 0}, [4]float64{.5, .5, .5, .5}, [3]float64{.2, -.1, .4}, .8, []float64{.1, .2, .3, -.1, -.2, -.3, .5, 0, -.5}},
		{[3]float64{-1, 0, 4}, [3]float64{-3, -3, -3}, [4]float64{0, 1, 0, 0}, [3]float64{0, 0, 0}, .1, make([]float64, 9)},
	}
	for _, colorBounds := range []bool{true, false} {
		m, err := DecodePLY(encodeCompressedPLY(t, splats, colorBounds), nil)
		if err != nil || m.Format != PLY || len(m.Objects) != 1 || len(m.Objects[0].Geometry.Points) != 2 || len(m.Objects[0].Geometry.GaussianSH) != 8 {
			t.Fatal(err, m)
		}
		g := m.Objects[0].Geometry
		if g.PointStyle != (nt3d.PointStyle{Shape: nt3d.GaussianPoint, Units: nt3d.WorldUnits, Blend: nt3d.AlphaBlend}) {
			t.Fatal(g.PointStyle)
		}
		for i, s := range splats {
			want, _ := GaussianPoint(vec(s.position), s.rotation, s.logScale, s.dc, s.opacity)
			p := g.Points[i]
			// 11 bits over a range of 5: within 0.005; alpha within 1; color within 2 (when bounds are present).
			assertNear(t, p.Position, want.Position, .005)
			if abs(int(p.Color.A)-int(want.Color.A)) > 1 {
				t.Fatal(i, p.Color, want.Color)
			}
			if colorBounds && (abs(int(p.Color.R)-int(want.Color.R)) > 2 || abs(int(p.Color.B)-int(want.Color.B)) > 2) {
				t.Fatal(i, p.Color, want.Color)
			}
			for k := range 3 { // 10-bit scales over a range of 3 log units: 0.3% length; rotation within 1°
				assertAxis(t, p.GaussianAxes[k], want.GaussianAxes[k], .01, math.Pi/180)
			}
			for j, c := range splats[i].rest { // SH byte step 8/255
				ch, k := j/3, j%3
				if got := g.GaussianSH[4*i+1+k][ch]; math.Abs(float64(got)-c) > .04 {
					t.Fatal(i, j, got, c)
				}
			}
		}
	}
}

func TestCompressedPLYDegreeZero(t *testing.T) {
	splats := []csplat{
		{[3]float64{1, 2, 3}, [3]float64{-2, -1, 0}, [4]float64{1, 0, 0, 0}, [3]float64{.2, -.1, .4}, .8, nil},
		{[3]float64{-1, 0, 4}, [3]float64{-3, -3, -3}, [4]float64{0, 0, 0, 1}, [3]float64{0, 0, 0}, .1, nil},
	}
	m, err := DecodePLY(encodeCompressedPLY(t, splats, true), nil)
	if err != nil || len(m.Objects) != 1 {
		t.Fatal(err, m)
	}
	if g := m.Objects[0].Geometry; len(g.GaussianSH) != len(g.Points) || len(g.Points) != 2 {
		t.Fatal(len(g.GaussianSH), len(g.Points))
	}
}

// Past one chunk, each splat decodes against its own chunk's bounds.
func TestCompressedPLYChunks(t *testing.T) {
	var splats []csplat
	for i := range 300 {
		f := float64(i)
		splats = append(splats, csplat{[3]float64{f, -f / 2, 100 + f}, [3]float64{-2, -2, -2}, [4]float64{1, 0, 0, 0}, [3]float64{0, 0, 0}, .5, nil})
	}
	m, err := DecodePLY(encodeCompressedPLY(t, splats, true), nil)
	if err != nil || len(m.Objects) != 1 || len(m.Objects[0].Geometry.Points) != 300 {
		t.Fatal(err, m)
	}
	for i, p := range m.Objects[0].Geometry.Points {
		assertNear(t, p.Position, vec(splats[i].position), .15)
	}
}

func TestCompressedPLYRejects(t *testing.T) {
	splats := []csplat{
		{[3]float64{1, 2, 3}, [3]float64{-2, -1, 0}, [4]float64{.5, .5, .5, .5}, [3]float64{.2, -.1, .4}, .8, make([]float64, 9)},
		{[3]float64{-1, 0, 4}, [3]float64{-3, -3, -3}, [4]float64{0, 1, 0, 0}, [3]float64{0, 0, 0}, .1, make([]float64, 9)},
	}
	good := encodeCompressedPLY(t, splats, true)
	if _, err := DecodePLY(good, nil); err != nil {
		t.Fatal(err)
	}
	replace := func(old, new string) []byte {
		if !bytes.Contains(good, []byte(old)) {
			t.Fatalf("header lacks %q", old)
		}
		return bytes.Replace(good, []byte(old), []byte(new), 1)
	}
	cases := map[string]struct {
		data []byte
		o    *Options
	}{
		"sh count":         {replace("element sh 2", "element sh 3"), nil},
		"chunk count":      {replace("element chunk 1", "element chunk 2"), nil},
		"truncated":        {good[:len(good)-4], nil},
		"sh properties":    {replace("property uchar f_rest_6\nproperty uchar f_rest_7\nproperty uchar f_rest_8\n", ""), nil},
		"chunk properties": {replace("element vertex", "property float extra\nelement vertex"), nil},
		"byte limit":       {good, &Options{MaxBytes: 16}},
	}
	for name, c := range cases {
		if m, err := DecodePLY(c.data, c.o); err == nil || m != nil {
			t.Errorf("%s: accepted: %v %v", name, m, err)
		}
	}
}

func decodeRing(t *testing.T, o *Options) *Model {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "examples", "demos", "internal", "demo", "models", "gaussian-ring.ply"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := DecodePLY(data, o)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// nearest returns, for each of got's points, the index of the closest of want's.
func nearest(got, want []nt3d.Point) []int {
	index := make([]int, len(got))
	for i, p := range got {
		best := math.Inf(1)
		for j, q := range want {
			d := p.Position.Sub(q.Position)
			if s := float64(d.Dot(d)); s < best {
				best, index[i] = s, j
			}
		}
	}
	return index
}

func TestCompressedFixtureMatchesPLY(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "gaussian-ring.compressed.ply"))
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range []*Options{nil, {Up: YUp}} {
		want := decodeRing(t, o)
		got, err := DecodePLY(data, o)
		if err != nil {
			t.Fatal(err)
		}
		if got.Format != PLY || len(got.Objects) != 1 || len(want.Objects) != 1 {
			t.Fatalf("%+v: format %v, %d objects", o, got.Format, len(got.Objects))
		}
		g, w := got.Objects[0].Geometry, want.Objects[0].Geometry
		if len(g.Points) != 576 || len(w.Points) != 576 || len(g.GaussianSH) != 4*576 || len(w.GaussianSH) != 4*576 {
			t.Fatalf("%+v: %d points, %d SH; PLY %d, %d", o, len(g.Points), len(g.GaussianSH), len(w.Points), len(w.GaussianSH))
		}
		if g.GaussianSHTransform != w.GaussianSHTransform || g.PointStyle != w.PointStyle {
			t.Fatalf("%+v: SH transform %v style %v; PLY %v %v", o, g.GaussianSHTransform, g.PointStyle, w.GaussianSHTransform, w.PointStyle)
		}
		// Positions are quantized to 11 bits over a chunk's bounds, at worst
		// the source's extent per axis over 1024.
		lo, hi := w.Points[0].Position, w.Points[0].Position
		for _, p := range w.Points {
			lo = nt3dmath.Vec3{X: min(lo.X, p.Position.X), Y: min(lo.Y, p.Position.Y), Z: min(lo.Z, p.Position.Z)}
			hi = nt3dmath.Vec3{X: max(hi.X, p.Position.X), Y: max(hi.Y, p.Position.Y), Z: max(hi.Z, p.Position.Z)}
		}
		tolerance := [3]float64{float64(hi.X-lo.X) / 1024, float64(hi.Y-lo.Y) / 1024, float64(hi.Z-lo.Z) / 1024}
		// The writer reorders the splats, so match each to the source by position.
		match := nearest(g.Points, w.Points)
		seen := make([]bool, len(w.Points))
		for i, p := range g.Points {
			j := match[i]
			if seen[j] {
				t.Fatalf("%+v: splats %d and another both match source %d", o, i, j)
			}
			seen[j] = true
			q := w.Points[j]
			d := p.Position.Sub(q.Position)
			for k, v := range [3]float32{d.X, d.Y, d.Z} {
				if math.Abs(float64(v)) > tolerance[k] {
					t.Fatalf("%+v: splat %d at %v, source %d at %v: axis %d off by %g, limit %g", o, i, p.Position, j, q.Position, k, v, tolerance[k])
				}
			}
			if abs(int(p.Color.A)-int(q.Color.A)) > 1 {
				t.Fatalf("%+v: splat %d alpha %d, source %d", o, i, p.Color.A, q.Color.A)
			}
			if abs(int(p.Color.R)-int(q.Color.R)) > 2 || abs(int(p.Color.G)-int(q.Color.G)) > 2 || abs(int(p.Color.B)-int(q.Color.B)) > 2 {
				t.Fatalf("%+v: splat %d color %v, source %d %v", o, i, p.Color, j, q.Color)
			}
			for k := range 3 {
				assertAxis(t, p.GaussianAxes[k], q.GaussianAxes[k], .04, math.Pi/180)
			}
			for k := range 4 {
				for ch := range 3 {
					if a, b := g.GaussianSH[4*i+k][ch], w.GaussianSH[4*j+k][ch]; math.Abs(float64(a-b)) > .04 {
						t.Fatalf("%+v: splat %d SH %d channel %d: %v, source %d %v", o, i, k, ch, a, j, b)
					}
				}
			}
		}
	}
}
