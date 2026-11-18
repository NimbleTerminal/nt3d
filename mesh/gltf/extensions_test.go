// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// An extension the decoder reads must be declared, or a document that
// requires it is refused although its meaning is honored; one declared and
// never read is a promise not kept. Every extension name in the package's
// source is held to the tables that declare support.
func TestEveryExtensionReadIsDeclared(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	name := regexp.MustCompile(`"((?:KHR|EXT)_[a-z0-9_]+)"`)
	mentioned := map[string][]string{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range name.FindAllStringSubmatch(string(src), -1) {
			if !slices.Contains(mentioned[m[1]], file) {
				mentioned[m[1]] = append(mentioned[m[1]], file)
			}
		}
	}
	for ext, where := range mentioned {
		if !slices.Contains(supportedExtensions, ext) && !slices.Contains(ignoredExtensions, ext) {
			t.Errorf("%s is read in %v but declared in neither materialExtensions nor documentExtensions", ext, where)
		}
	}
	// Honored without being named: the extension only permits integer vertex
	// attributes, which quantization.go reads wherever it meets them.
	unnamed := []string{"KHR_mesh_quantization"}
	tables := []string{"extensions.go", "material_extensions.go"}
	for _, ext := range supportedExtensions {
		read := slices.Contains(unnamed, ext) || slices.ContainsFunc(mentioned[ext], func(file string) bool { return !slices.Contains(tables, file) })
		row := slices.IndexFunc(materialExtensions, func(e materialExtension) bool { return e.name == ext })
		if !read && (row < 0 || materialExtensions[row].read == nil) {
			t.Errorf("%s is declared supported but nothing reads it", ext)
		}
	}
	seen := map[string]bool{}
	for _, ext := range supportedExtensions {
		if seen[ext] {
			t.Errorf("%s is declared twice", ext)
		}
		seen[ext] = true
	}
}
