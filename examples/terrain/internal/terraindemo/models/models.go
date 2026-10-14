// Copyright (c) 2026 Neomantra Corp.

// Package models holds the sample mesh files the models demo shows, made by
// the generate command from nt3d's shape builders.
package models

import "embed"

//go:generate go run ./generate

// Files holds knot.stl, star.obj with star.mtl and wood.png, and robot.3mf.
//
//go:embed knot.stl star.obj star.mtl wood.png robot.3mf
var Files embed.FS
