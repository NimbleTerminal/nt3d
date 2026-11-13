// Copyright (c) 2026 Neomantra Corp.

// Package nt3dfur builds shell-fur and edge-on silhouette-fin geometry for
// nt3d triangle meshes.
//
// Coat.Shells returns an opaque base skin followed by cutout layers pushed out
// along averaged vertex normals, sharing one coverage-preserving strand height
// texture per strand density. Coat.Sway returns per-shell node translations for
// wind or gravity animation without re-uploading geometry, and Coat.Fins
// generates edge-on strips along mesh edges to fill silhouettes.
package nt3dfur
