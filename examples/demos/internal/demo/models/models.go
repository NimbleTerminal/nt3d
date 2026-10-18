// Copyright (c) 2026 Neomantra Corp.

// Package models holds the sample mesh files the demos show. Most are made by
// the generate command from nt3d's shape builders. Spot the cow
// (spot_triangulated.obj, spot_texture.png) is by Keenan Crane, who released
// it into the public domain; see spot_README.txt.
package models

import "embed"

//go:generate go run ./generate

// Files holds the models demo's samples: knot.stl, star.obj with star.mtl
// and wood.png, and robot.3mf.
//
//go:embed knot.stl star.obj star.mtl wood.png robot.3mf
var Files embed.FS

// Spot holds the fur demo's cow: spot_triangulated.obj, spot_texture.png,
// and its README. Kept apart from Files so each demo's program carries only
// what it shows.
//
//go:embed spot_triangulated.obj spot_texture.png spot_README.txt
var Spot embed.FS

// Ribbon is the generated animation demo's original CC0 sample.
//
//go:embed animated.gltf animated.LICENSE.txt
var Ribbon embed.FS
