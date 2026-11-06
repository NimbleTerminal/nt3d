// Copyright (c) 2026 Neomantra Corp.
package nt3dsog

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

// tinyPNG is a 1×1 image holding one splat's bytes for an attribute.
func tinyPNG(f *testing.F, r, g, b, a uint8) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{r, g, b, a})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		f.Fatal(err)
	}
	return buf.Bytes()
}

// tinyImages are one splat's attribute images, named as the metas name them.
func tinyImages(f *testing.F) map[string][]byte {
	return map[string][]byte{
		"means_l.png": tinyPNG(f, 10, 20, 30, 255),
		"means_u.png": tinyPNG(f, 100, 110, 120, 255),
		"scales.png":  tinyPNG(f, 50, 60, 70, 255),
		"quats.png":   tinyPNG(f, 128, 128, 128, 252),
		"sh0.png":     tinyPNG(f, 200, 100, 50, 180),
	}
}

func fuzzCodebook(v float64) string {
	return "[" + strings.TrimSuffix(strings.Repeat(fmt.Sprint(v)+",", 256), ",") + "]"
}

// tinyMeta is a compact one-splat meta.json without higher-order SH, in the
// v1 (ranges) or v2 (codebooks) layout. Small seeds matter: the fuzzer
// minimizes each seed before fuzzing, and spends a whole short round on a
// large one.
func tinyMeta(version int) []byte {
	files := `"means":{"shape":[1,3],"mins":[-1,-1,-1],"maxs":[1,1,1],"files":["means_l.png","means_u.png"]},"quats":{"files":["quats.png"]}`
	if version == 2 {
		return []byte(`{"version":2,"count":1,` + files + `,"scales":{"codebook":` + fuzzCodebook(-2) + `,"files":["scales.png"]},"sh0":{"codebook":` + fuzzCodebook(.5) + `,"files":["sh0.png"]}}`)
	}
	return []byte(`{` + files + `,"scales":{"mins":[-3,-3,-3],"maxs":[-1,-1,-1],"files":["scales.png"]},"sh0":{"mins":[-1,-1,-1,-5],"maxs":[1,1,1,5],"files":["sh0.png"]}}`)
}

// tinyBundle zips a meta and the images, stored, so the fuzzer's byte
// mutations reach the entries rather than a deflate stream.
func tinyBundle(f *testing.F, meta []byte, images map[string][]byte) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	add := func(name string, data []byte) {
		e, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			f.Fatal(err)
		}
		if _, err := e.Write(data); err != nil {
			f.Fatal(err)
		}
	}
	add("meta.json", meta)
	for name, data := range images {
		add(name, data)
	}
	if err := w.Close(); err != nil {
		f.Fatal(err)
	}
	return buf.Bytes()
}

// FuzzDecode feeds SOG files to the decoder: zip bundles holding a meta.json
// and the images it names, or an unbundled meta.json whose images come
// through Options.Open. Every image name resolves to a 1×1 picture, so the
// fuzzer explores the metadata tables and the unpacking, not image codecs.
func FuzzDecode(f *testing.F) {
	images := tinyImages(f)
	o := fuzzOptions()
	o.Open = func(name string) ([]byte, error) {
		if data, ok := images[name]; ok {
			return data, nil
		}
		return images["sh0.png"], nil
	}
	// Few seeds, as the fuzzer re-minimizes each one every round: both metas
	// unbundled, and the smaller one bundled.
	for _, seed := range [][]byte{tinyMeta(1), tinyMeta(2), tinyBundle(f, tinyMeta(1), images)} {
		if _, err := Decode(seed, o); err != nil {
			f.Fatalf("seed does not decode: %v", err)
		}
		f.Add(seed)
	}
	f.Add([]byte(`{"version":1,"means":{"shape":[1e99,3],"mins":[null],"maxs":[1e999],"files":["a","b"]},"quats":{"files":["q"]}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := Decode(data, o)
		checkModel(t, m, err, o)
	})
}
