// Copyright (c) 2026 Neomantra Corp.

// Package gltfsamples embeds the small Khronos animation samples and original CC0 scenes for cmd/animate.
// Generated Assembly and Spotlights data are CC0; see their README.md files.
// Khronos model data is CC0; the upstream metadata is CC-BY-4.0. See each LICENSE.md
// and SOURCES.md for attribution and the pinned upstream revision.
package gltfsamples

import "embed"

// Files preserves each sample's directory structure so relative buffer URIs
// resolve exactly as they do when the model is loaded from disk.
//
//go:generate go run ./generate
//go:embed AnimatedMorphCube SimpleSkin AnimatedTriangle Assembly Spotlights SOURCES.md
var Files embed.FS
