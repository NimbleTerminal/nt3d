// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import (
	"bytes"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// Formats beyond STL, OBJ, 3MF, PLY, and Stanford .conf register themselves
// when their packages are imported, as image formats do with the image
// package, so programs that do not use them do not carry them:
//
//	import _ "nimbleterminal.dev/nt3d/mesh/gltf"    // glTF only
//	import _ "nimbleterminal.dev/nt3d/mesh/formats" // every registered format

// registration is a format added by RegisterFormat.
type registration struct {
	format     Format
	extensions []string
	sniff      func([]byte) bool // Nil when the format has no content check.
	decode     func([]byte, *Options) (*Model, error)
}

var (
	registryMu sync.RWMutex
	registry   []registration
)

// RegisterFormat adds a format that Decode, ReadFile, and FormatOf know: by
// its name, its file extensions (with the dot, such as ".glb"), and, when it
// has one, the magic prefix its content begins with. Formats built in, and
// formats registered earlier, are chosen first. It is meant to be called
// from init functions.
func RegisterFormat(f Format, extensions []string, magic string, decode func(data []byte, o *Options) (*Model, error)) {
	var sniff func([]byte) bool
	if magic != "" {
		sniff = func(data []byte) bool { return bytes.HasPrefix(data, []byte(magic)) }
	}
	RegisterFormatSniff(f, extensions, sniff, decode)
}

// RegisterFormatSniff is RegisterFormat for a format whose content is
// recognized by a function rather than a fixed prefix, as a compressed
// container's is; sniff may be nil for a format known by extension alone.
func RegisterFormatSniff(f Format, extensions []string, sniff func(data []byte) bool, decode func(data []byte, o *Options) (*Model, error)) {
	registryMu.Lock()
	defer registryMu.Unlock()
	lower := make([]string, len(extensions))
	for i, e := range extensions {
		lower[i] = strings.ToLower(e)
	}
	registry = append(registry, registration{f, lower, sniff, decode})
}

// registeredExtension chooses a registered format by a file's extension.
func registeredExtension(name string) (Format, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	ext := strings.ToLower(filepath.Ext(name))
	for _, r := range registry {
		if ext != "" && slices.Contains(r.extensions, ext) {
			return r.format, true
		}
	}
	return "", false
}

// registeredMagic chooses a registered format by its content.
func registeredMagic(data []byte) (Format, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	for _, r := range registry {
		if r.sniff != nil && r.sniff(data) {
			return r.format, true
		}
	}
	return "", false
}

func registeredFormat(f Format) (registration, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	for _, r := range registry {
		if r.format == f {
			return r, true
		}
	}
	return registration{}, false
}
