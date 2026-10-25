// Copyright (c) 2026 Neomantra Corp.

// Package nt3dsog reads PlayCanvas's SOG, the compressed 3D Gaussian splat
// bundle, as nt3d Gaussian geometry: the same points, axes, colors, and
// spherical harmonics the PLY decoder makes, in a fraction of the space. A
// SOG file is a zip of a meta.json and the lossless WebP images it names.
// Importing it registers the format with nt3dmesh, as nt3dmesh/formats does:
//
//	import _ "nimbleterminal.dev/nt3d/mesh/sog"
//
// Both versions decode: version 2 (codebooks of 256 values per attribute,
// named by a "version" field) and the original version 1 (linear ranges per
// channel, no "version" field). SOG files store the coordinates of the PLY
// they were made from, so Options.Up applies as it does for PLY. The "model"
// tag some files carry is ignored. A cloud larger than one geometry holds is
// split as nt3dmesh.PointPieces splits it.
package nt3dsog
