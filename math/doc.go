// nt3d - Copyright (c) 2026 Neomantra Corp.

// Package nt3dmath is nt3d's math, all of it: the vectors, quaternions,
// matrices, bounds, and intersection tests that the engine, the mesh
// decoders, and the camera packages share. It imports nothing but the
// standard library.
//
// There are two families, alike in shape:
//
//   - float32, matching WebGPU's f32 and buffer layouts: [Vec3], [Quat],
//     [Mat4], [AABB], and [Ray]. Geometry, transforms sent to a renderer, and
//     picking use these.
//   - float64, for where float32 does not reach: [Vec3d], [Quatd], and
//     [Mat4d]. Node transforms carrying geographic origins, matrices read
//     from model files, and camera paths compose in these and round once.
//
// Convert between them with D on the float32 types and Vec3, Quat, and Mat4
// on the float64 ones.
//
// Matrices are column-major and multiply as a.Mul(b), applying b first.
// Transforms: [Translation], [Scaling], [RotationX] and its siblings, [TRS]
// and [TRSd] from a translation, rotation, and scale, [Mat4.InverseAffine]
// for affine transforms and [Mat4.Inverse] for any, and [Mat4.Rotation] to
// take the rotation back out.
//
// Tests against space: [RayProbe.Enter] and [AABB.Hits] for a ray and a box,
// as a spatial index asks while it walks, and [Mat4.Outside] for a box and a
// view volume, which is the culling test of every renderer.
package nt3dmath
