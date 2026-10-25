// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import (
	"archive/zip"
	"bytes"
	"errors"
	"testing"
)

// Registered formats are found by extension and content after the built-in
// ones, and decode through Decode.
func TestRegisterFormat(t *testing.T) {
	defer func(r []registration) { registry = r }(registry)
	called := false
	RegisterFormat("toy", []string{".TOY", ".ply"}, "TOY!", func(data []byte, o *Options) (*Model, error) {
		called = true
		return nil, errors.New("toy decoder")
	})
	for _, c := range []struct {
		name, data string
		want       Format
	}{
		{"a.toy", "", "toy"},
		{"a.bin", "TOY! 1", "toy"},
		{"a.ply", "", PLY}, // Built in first.
		{"a.bin", "ply\n", PLY},
		{"a.bin", "nothing", ""},
	} {
		if f, _ := FormatOf(c.name, []byte(c.data)); f != c.want {
			t.Errorf("%s %q: %q, want %q", c.name, c.data, f, c.want)
		}
	}
	if _, err := Decode(nil, "toy", nil); err == nil || !called {
		t.Fatalf("toy decode: %v, called %v", err, called)
	}
	if _, err := Decode(nil, "none", nil); err == nil {
		t.Fatal("unknown format decoded")
	}
	// A format may recognize its content by a function, as a compressed
	// container needs, rather than a fixed prefix.
	RegisterFormatSniff("zipped", []string{".zp"}, func(data []byte) bool { return bytes.HasPrefix(data, []byte{0x1f, 0x8b}) && len(data) > 4 }, func([]byte, *Options) (*Model, error) { return nil, errors.New("zipped") })
	if f, _ := FormatOf("a.bin", []byte{0x1f, 0x8b, 8, 0, 0, 0}); f != "zipped" {
		t.Fatalf("gzip content: %q", f)
	}
	if f, _ := FormatOf("a.bin", []byte{0x1f, 0x8b}); f != "" {
		t.Fatalf("too short for the sniff: %q", f)
	}
	if f, _ := FormatOf("b.ZP", nil); f != "zipped" {
		t.Fatalf("extension: %q", f)
	}
	// A registered sniff may claim a zip; otherwise a zip is 3MF.
	zipWith := func(name string) []byte {
		var b bytes.Buffer
		w := zip.NewWriter(&b)
		f, _ := w.Create(name)
		f.Write([]byte("{}"))
		w.Close()
		return b.Bytes()
	}
	RegisterFormatSniff("bundle", []string{".bundle"}, func(data []byte) bool {
		r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return false
		}
		for _, f := range r.File {
			if f.Name == "meta.json" {
				return true
			}
		}
		return false
	}, func([]byte, *Options) (*Model, error) { return nil, errors.New("bundle") })
	if f, _ := FormatOf("a.bin", zipWith("meta.json")); f != "bundle" {
		t.Fatalf("zip with meta.json: %q", f)
	}
	if f, _ := FormatOf("a.bin", zipWith("3D/3dmodel.model")); f != ThreeMF {
		t.Fatalf("other zip: %q", f)
	}
	if f, _ := FormatOf("a.3mf", zipWith("meta.json")); f != ThreeMF {
		t.Fatalf("extension first: %q", f)
	}
}
