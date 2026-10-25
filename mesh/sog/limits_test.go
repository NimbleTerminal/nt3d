// Copyright (c) 2026 Neomantra Corp.

package nt3dsog

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
	"nimbleterminal.dev/nt3d/sh"
)

// rebundle rewrites a bundle entry by entry, deflated, in order. edit is
// handed each entry's name and content and returns the content to write, or
// false to drop the entry.
func rebundle(t *testing.T, bundle []byte, edit func(name string, content []byte) ([]byte, bool)) []byte {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, f := range z.File {
		content := readEntry(t, bundle, f.Name)
		content, keep := edit(f.Name, content)
		if !keep {
			continue
		}
		fw, err := w.Create(f.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// editMeta rewrites a bundle with its meta.json changed by edit.
func editMeta(t *testing.T, bundle []byte, edit func(m map[string]any)) []byte {
	t.Helper()
	return rebundle(t, bundle, func(name string, content []byte) ([]byte, bool) {
		if name != "meta.json" {
			return content, true
		}
		var m map[string]any
		if err := json.Unmarshal(content, &m); err != nil {
			t.Fatal(err)
		}
		edit(m)
		out, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return out, true
	})
}

// replaceEntry rewrites a bundle with the named entry's content replaced.
func replaceEntry(t *testing.T, bundle []byte, name string, content []byte) []byte {
	t.Helper()
	return rebundle(t, bundle, func(n string, old []byte) ([]byte, bool) {
		if n == name {
			return content, true
		}
		return old, true
	})
}

func ring(t *testing.T) []byte { return read(t, filepath.Join("testdata", "gaussian-ring.sog")) }

func decodeBundle(t *testing.T, bundle []byte) nt3d.Geometry {
	t.Helper()
	m, err := Decode(bundle, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Objects) != 1 {
		t.Fatalf("%d objects", len(m.Objects))
	}
	return m.Objects[0].Geometry
}

func TestDecodeTolerates(t *testing.T) {
	bundle := ring(t)
	full := decodeBundle(t, bundle)

	t.Run("lower count", func(t *testing.T) {
		g := decodeBundle(t, editMeta(t, bundle, func(m map[string]any) { m["count"] = 575 }))
		if len(g.Points) != 575 || len(g.GaussianSH) != 4*575 {
			t.Fatalf("%d points, %d SH", len(g.Points), len(g.GaussianSH))
		}
		if !reflect.DeepEqual(g.Points, full.Points[:575]) {
			t.Fatal("points differ from the full decode's first 575")
		}
	})

	t.Run("no shN", func(t *testing.T) {
		g := decodeBundle(t, editMeta(t, bundle, func(m map[string]any) {
			if _, ok := m["shN"]; !ok {
				t.Fatal("fixture has no shN")
			}
			delete(m, "shN")
		}))
		if len(g.Points) != 576 || len(g.GaussianSH) != len(g.Points) {
			t.Fatalf("%d points, %d SH", len(g.Points), len(g.GaussianSH))
		}
		for i, sh := range g.GaussianSH {
			if sh != full.GaussianSH[4*i] {
				t.Fatalf("splat %d DC %v, full decode %v", i, sh, full.GaussianSH[4*i])
			}
		}
	})

	t.Run("labels past shN.count", func(t *testing.T) {
		g := decodeBundle(t, editMeta(t, bundle, func(m map[string]any) { m["shN"].(map[string]any)["count"] = 64 }))
		if len(g.Points) != 576 || len(g.GaussianSH) != 4*576 {
			t.Fatalf("%d points, %d SH", len(g.Points), len(g.GaussianSH))
		}
		zero, kept := 0, 0
		for i := range g.Points {
			got, want := g.GaussianSH[4*i:4*i+4], full.GaussianSH[4*i:4*i+4]
			if got[0] != want[0] {
				t.Fatalf("splat %d DC %v, full decode %v", i, got[0], want[0])
			}
			switch {
			case got[1] == (nt3dsh.RGB{}) && got[2] == (nt3dsh.RGB{}) && got[3] == (nt3dsh.RGB{}):
				zero++
			case reflect.DeepEqual(got, want):
				kept++
			default:
				t.Fatalf("splat %d SH %v is neither zero nor the full decode's %v", i, got, want)
			}
		}
		if zero == 0 || kept == 0 {
			t.Fatalf("%d splats zeroed, %d kept", zero, kept)
		}
	})

	t.Run("deflated entries", func(t *testing.T) {
		src, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range src.File {
			if f.Method != zip.Store {
				t.Fatalf("fixture entry %s is method %d, not stored", f.Name, f.Method)
			}
		}
		deflated := rebundle(t, bundle, func(_ string, c []byte) ([]byte, bool) { return c, true })
		z, err := zip.NewReader(bytes.NewReader(deflated), int64(len(deflated)))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range z.File {
			if f.Method != zip.Deflate {
				t.Fatalf("rebundled entry %s is method %d, not deflated", f.Name, f.Method)
			}
		}
		if g := decodeBundle(t, deflated); !reflect.DeepEqual(g, full) {
			t.Fatal("deflated bundle decodes differently")
		}
	})

	t.Run("null first scales codebook entry", func(t *testing.T) {
		g := decodeBundle(t, editMeta(t, bundle, func(m map[string]any) { m["scales"].(map[string]any)["codebook"].([]any)[0] = nil }))
		if len(g.Points) != 576 {
			t.Fatalf("%d points", len(g.Points))
		}
	})

	// A tag below 252 in the quaternion image's alpha means no rotation.
	// sh0.webp's alpha is an opacity byte of 208; means_l.webp's 255 would be
	// a valid tag.
	t.Run("invalid quaternion tag", func(t *testing.T) {
		g := decodeBundle(t, replaceEntry(t, bundle, "quats.webp", readEntry(t, bundle, "sh0.webp")))
		if len(g.Points) != 576 {
			t.Fatalf("%d points", len(g.Points))
		}
		scales := [3]float64{length(full.Points[0].GaussianAxes[0]), length(full.Points[0].GaussianAxes[1]), length(full.Points[0].GaussianAxes[2])}
		if scales[0] == scales[1] || scales[1] == scales[2] || scales[0] == scales[2] {
			t.Fatalf("scales %v are not distinct", scales)
		}
		rotated := false
		for i, p := range g.Points {
			want, err := nt3dmesh.GaussianPoint(p.Position, [4]float64{1, 0, 0, 0}, [3]float64{math.Log(scales[0]), math.Log(scales[1]), math.Log(scales[2])}, [3]float64{}, 1)
			if err != nil {
				t.Fatal(err)
			}
			for k := range 3 {
				assertAxis(t, i, k, p.GaussianAxes[k], want.GaussianAxes[k], 1e-5, .001*math.Pi/180)
				if length(full.Points[i].GaussianAxes[k].Sub(want.GaussianAxes[k])) > 1e-3 {
					rotated = true
				}
			}
		}
		if !rotated {
			t.Fatal("the fixture's own rotations are the identity, so this case shows nothing")
		}
	})
}

func TestDecodeRejects(t *testing.T) {
	bundle := ring(t)
	swapped := func(m map[string]any) {
		m["means"].(map[string]any)["files"] = []any{"shN_centroids.webp", "means_u.webp"}
	}
	for _, c := range []struct {
		name   string
		bundle []byte
		o      *nt3dmesh.Options
		want   string // a substring of the error
	}{
		{"version 3", editMeta(t, bundle, func(m map[string]any) { m["version"] = 3 }), nil, "version 3"},
		{"count over the images", editMeta(t, bundle, func(m map[string]any) { m["count"] = 577 }), nil, "do not fit"},
		{"means over centroids", editMeta(t, bundle, swapped), nil, "not 192"},
		{"bands 2", editMeta(t, bundle, func(m map[string]any) { m["shN"].(map[string]any)["bands"] = 2 }), nil, "bands"},
		{"sh0.webp missing", rebundle(t, bundle, func(n string, c []byte) ([]byte, bool) { return c, n != "sh0.webp" }), nil, "sh0.webp"},
		{"meta.json missing", rebundle(t, bundle, func(n string, c []byte) ([]byte, bool) { return c, n != "meta.json" }), nil, "meta.json"},
		{"version only", zipOf(t, map[string]string{"meta.json": `{"version":2}`}, "meta.json"), nil, "SOG"},
		{"MaxBytes below meta.json", bundle, &nt3dmesh.Options{MaxBytes: 1 << 12}, "meta.json"},
		// meta.json fits (15086 bytes) but the images do not.
		{"MaxBytes below the images", bundle, &nt3dmesh.Options{MaxBytes: 16000}, ".webp"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, err := Decode(c.bundle, c.o)
			if err == nil || m != nil {
				t.Fatalf("decoded: %v, %v", m, err)
			}
			if !strings.HasPrefix(err.Error(), "nt3dmesh: SOG ") && !errors.Is(err, nt3dmesh.ErrTooLarge) {
				t.Fatalf("error %q does not start with the package prefix", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not mention %q", err, c.want)
			}
			if c.o != nil && !errors.Is(err, nt3dmesh.ErrTooLarge) {
				t.Fatalf("error %q does not wrap ErrTooLarge", err)
			}
		})
	}

	if sniff(rebundle(t, bundle, func(n string, c []byte) ([]byte, bool) { return c, n != "meta.json" })) {
		t.Fatal("a zip without meta.json sniffed as SOG")
	}
}

func pix(img *image.NRGBA, x int, r, g, b, a uint8) {
	copy(img.Pix[img.PixOffset(x, 0):], []byte{r, g, b, a})
}

// codebook is 256 distinct values: scale*(i-offset).
func codebook(scale float64, offset int) table {
	cb := make([]float64, 256)
	for i := range cb {
		cb[i] = scale * float64(i-offset)
	}
	return table{codebook: cb}
}

// TestUnpackSynthetic decodes every quaternion tag, and a palette label at or past
// shN.count, as the fixture cannot: its rotations all use tags 252 and 255
// and its labels all fall within the palette.
func TestUnpackSynthetic(t *testing.T) {
	const count = 4
	m := &meta{version: 2, count: count}
	m.scales = codebook(.01, 128)
	m.sh0 = codebook(.01, 128)
	shn := &shN{count: 2, bands: 1, values: codebook(.005, 128)}
	m.shN = shn
	im := &images{width: count, height: 1}
	for _, p := range []**image.NRGBA{&im.meansL, &im.meansU, &im.scales, &im.quats, &im.sh0, &im.labels} {
		*p = image.NewNRGBA(image.Rect(0, 0, count, 1))
	}
	im.centroids = image.NewNRGBA(image.Rect(0, 0, 64*3, 1))
	// Two palette entries: three coefficients each, bytes distinct throughout.
	for label := range 2 {
		for k := range 3 {
			pix(im.centroids, label*3+k, uint8(100+30*label+10*k), uint8(101+30*label+10*k), uint8(102+30*label+10*k), 255)
		}
	}

	// Splats 0 and 1 use tags 253 and 254 and 2 and 3 use tags 10 and 252.
	// R, G, B are distinct, so any order of the three kept components shows.
	tags := [count]uint8{253, 254, 10, 252}
	rgb := [count][3]uint8{{200, 100, 150}, {90, 220, 40}, {200, 100, 150}, {30, 160, 240}}
	labels := [count][2]uint8{{1, 0}, {2, 0}, {0, 0}, {0, 1}} // valid, ==count, valid, 256
	for i := range count {
		pix(im.meansL, i, 0, 0, 0, 255)
		pix(im.meansU, i, 0, 0, 0, 255)
		pix(im.scales, i, uint8(130+i), uint8(140+i), uint8(150+i), 255)
		pix(im.quats, i, rgb[i][0], rgb[i][1], rgb[i][2], tags[i])
		pix(im.sh0, i, uint8(120+i), uint8(150+i), uint8(180+i), uint8(60*i+10))
		pix(im.labels, i, labels[i][0], labels[i][1], 0, 255)
	}

	g, err := unpack(m, im)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Points) != count || len(g.GaussianSH) != 4*count {
		t.Fatalf("%d points, %d SH", len(g.Points), len(g.GaussianSH))
	}

	// The expected quaternion, w, x, y, z: the kept components in order with
	// the dropped one, recomputed from the bytes, put back at the tag's index.
	component := func(b uint8) float64 { return (float64(b)/255 - .5) * math.Sqrt2 }
	for i := range count {
		a, b, c := component(rgb[i][0]), component(rgb[i][1]), component(rgb[i][2])
		d := math.Sqrt(1 - a*a - b*b - c*c)
		var q [4]float64
		switch tags[i] {
		case 252:
			q = [4]float64{d, a, b, c}
		case 253:
			q = [4]float64{a, d, b, c}
		case 254:
			q = [4]float64{a, b, d, c}
		default:
			q = [4]float64{1, 0, 0, 0}
		}
		logScales := [3]float64{m.scales.value(0, uint8(130+i)), m.scales.value(0, uint8(140+i)), m.scales.value(0, uint8(150+i))}
		dc := [3]float64{m.sh0.value(0, uint8(120+i)), m.sh0.value(0, uint8(150+i)), m.sh0.value(0, uint8(180+i))}
		want, err := nt3dmesh.GaussianPoint(nt3dmath.Vec3{}, q, logScales, dc, float64(60*i+10)/255)
		if err != nil {
			t.Fatal(err)
		}
		if g.Points[i] != want {
			t.Fatalf("splat %d with tag %d: %+v, want %+v", i, tags[i], g.Points[i], want)
		}
	}
	// Tag 10 is the identity orientation: each axis along its own direction.
	for k, p := range g.Points[2].GaussianAxes {
		var along float32
		switch k {
		case 0:
			along = p.X
		case 1:
			along = p.Y
		default:
			along = p.Z
		}
		if along <= 0 || float64(along) != length(p) {
			t.Fatalf("tag 10 axis %d is %v, not along its own direction", k, p)
		}
	}

	// A label in the palette gives its centroid's coefficients, channel-major;
	// one at or past shN.count (here 2 and 256) gives none.
	for i := range count {
		want := []float64{0, 0, 0, 0, 0, 0, 0, 0, 0}
		if label := int(labels[i][0]) | int(labels[i][1])<<8; label < shn.count {
			for k := range 3 {
				for ch := range 3 {
					want[ch*3+k] = shn.values.value(0, uint8(100+30*label+10*k+ch))
				}
			}
		}
		dc := [3]float64{m.sh0.value(0, uint8(120+i)), m.sh0.value(0, uint8(150+i)), m.sh0.value(0, uint8(180+i))}
		sh, err := nt3dmesh.GaussianSH(dc, want)
		if err != nil {
			t.Fatal(err)
		}
		if got := g.GaussianSH[4*i : 4*i+4]; !reflect.DeepEqual(got, sh) {
			t.Fatalf("splat %d label %d SH %v, want %v", i, labels[i], got, sh)
		}
	}
	valid, past := g.GaussianSH[0:4], g.GaussianSH[4:8]
	if valid[1] == (nt3dsh.RGB{}) || past[1] != (nt3dsh.RGB{}) || past[2] != (nt3dsh.RGB{}) || past[3] != (nt3dsh.RGB{}) {
		t.Fatalf("valid label SH %v, label past count SH %v", valid, past)
	}
}
