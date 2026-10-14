// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import (
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
}
