// Copyright (c) 2026 Neomantra Corp.

// Package models holds the sample mesh files the demos show. Most are made by
// the generate command from nt3d's shape builders. Spot the cow
// (spot_triangulated.obj, spot_texture.png) is by Keenan Crane, who released
// it into the public domain; see spot_README.txt.
package models

import "embed"

//go:generate go run ./generate

// Files holds knot.stl, star.obj with star.mtl and wood.png, robot.3mf, and
// spot_triangulated.obj with spot_texture.png.
//
//go:embed knot.stl star.obj star.mtl wood.png robot.3mf spot_triangulated.obj spot_texture.png spot_README.txt
var Files embed.FS
