// Copyright (c) 2026 Neomantra Corp.

// Package nt3dwater provides analytic Gerstner wave surfaces, bounded wake
// impulses, tiled water meshes, and Archimedes buoyancy/drag/planing force
// evaluation for rigid bodies.
//
// WaveSurface evaluates displaced surface positions, analytic normals,
// tangents, and material-point velocities from undisplaced rest coordinates
// (Sample) or inverted world XY coordinates (AtWorld). BuildWaveTiles splits
// a finite water grid into independently culled nt3d.Geometry tiles with
// matching shared borders, and BuoyantHull computes world-space water loads
// against any Surface implementation.
package nt3dwater
