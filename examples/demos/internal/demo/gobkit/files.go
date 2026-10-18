// Copyright (c) 2026 Neomantra Corp.
// Package gobkit embeds CC0 characters only for cmd/characters.
package gobkit

import "embed"

// Files contains the original GLBs and their provenance.
//
//go:embed *.glb LICENSE.md SOURCES.md
var Files embed.FS
