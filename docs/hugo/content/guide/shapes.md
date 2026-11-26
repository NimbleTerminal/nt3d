---
title: "Shapes and geometry"
weight: 30
---

# Shapes and geometry

A `Geometry` is what a node draws: triangles (with colors, normals, and texture coordinates), points, boxes, lines, and arrows, plus a material. You can fill one in by hand, but usually you build one or [read one from a file](../meshes/).

<img class="poster" src="../../posters/shapes.png" alt="A textured cube, golden torus, purple cone, orange cylinder, and green globe in a row">

## Building shapes

`BuildSphere`, `BuildCylinder`, `BuildTorus`, and `BuildCuboid` each take a small spec struct and return a lit, UV-mapped mesh centered on the origin, with Z up. Leave the tessellation counts and color at zero for sensible defaults.

```go
sphere, err := nt3d.BuildSphere(nt3d.Sphere{Radius: .5, Material: &nt3d.Material{Texture: tex}})
cone, err := nt3d.BuildCylinder(nt3d.Cylinder{BottomRadius: .4, Height: 1})
```

`BuildCylinder` makes cones too (a zero `TopRadius`) and frusta (two different radii). Faces wind counterclockwise seen from outside.

## Terrain

`BuildHeightfield` makes a surface from a grid of heights, from a slice or a `Sampler` function. It sets up normals, texture coordinates, and a hint (`Geometry.WireGrid`) that lets the wireframe renderer draw a tidy row-and-column lattice instead of every triangle. The [terrain demo](../demos/#terrain-explorer) is built on one.

## Changing geometry

```go
moved, err := nt3d.TransformGeometry(cone, nt3dmath.Translation(nt3dmath.Vec3{X: 1}))
both, err := nt3d.MergeGeometry(sphere, moved) // fails: different materials
smooth, err := nt3d.SmoothNormals(stlGeometry, 40)
```

- `TransformGeometry` bakes a transform into the vertices and normals. Unlike a node's transform it can mirror, and it reverses the winding so faces still point outward.
- `MergeGeometry` joins geometries that share a material.
- `FlatNormals` gives each face its own normal, for a faceted look.
- `SmoothNormals` averages the normals of faces meeting at a point, if they meet at less than the crease angle you give, weighting each by its corner's angle. It matches corners by position, so it smooths STL files, whose triangles share no vertices, too.

## Decals

`BuildDecal` projects a box onto a mesh and returns the part of the mesh inside it as a new, UV-mapped mesh to draw over the original: a sign on a wall, a mark on a floor.

```go
decal, err := nt3d.BuildDecal(wall, nt3d.Decal{
	Center:       nt3dmath.Vec3{Y: -0.5},  // on the surface
	Normal:       nt3dmath.Vec3{Y: -1},    // out of the surface
	Up:           nt3dmath.Vec3{Z: 1},     // the image's upward direction
	Width:        2, Height: 1.5, Depth: 0.4,
	NormalOffset: 0.01,
	Material:     &nt3d.Material{Texture: tex, Shading: nt3d.ShadeUnlit, Alpha: nt3d.AlphaTransparent},
})
```

- `Normal` and `Up` need not be unit length, and `Up` need not be perpendicular to `Normal`: it is made so. UV (0, 0) is the top left of the image seen from outside the surface.
- `Depth` is how far the box reaches through the surface. Keep it small, so the decal does not also land on what is behind.
- Only triangles that face along `Normal` are kept, so a decal does not wrap onto back faces or onto a floor perpendicular to a wall.
- `NormalOffset` lifts the decal off the surface along `Normal`, so the two do not fight for depth.
- A decal that lands on nothing is not an error. The geometry returned is empty, and a scene draws nothing for it.
- The target is in its own local coordinates, as `Center`, `Normal`, and `Up` are. For a node that has been moved, bake its transform in first with `TransformGeometry`, or place the decal's node with the same transform.

To mark a Gaussian splat cloud instead of a mesh, see [Patching a cloud](../rendering/#patching-a-cloud).

## Updating fixed-topology meshes

When an indexed triangle mesh animates on the CPU without changing its triangle indices, UVs, or material—such as [water tiles](../water/#larger-water-meshes), cloth, or terrain patches—update its vertices in place instead of calling `SetNodeGeometry`:

```go
err := scene.SetNodeVertices(node, vertices, bounds)
// Partial vertex-range replacement:
err = scene.UpdateNodeVertices(node, start, vertices[start:end], bounds)
// Or supply updated analytic tangents alongside vertices (for normal-mapped surfaces):
err = scene.SetNodeVerticesWithTangents(node, vertices, tangents, bounds)
```

These methods validate and clone only the updated vertex (and optional tangent) slices, reuse the existing `Indices`, `UV`, `NormalUV`, and `Material`, trust the caller-supplied `bounds` (verifying updated vertices lie inside it), and overwrite existing WebGPU vertex buffers via `WriteBuffer` without reallocating GPU buffers or bind groups. On a 64×64 normal-mapped mesh (`BenchmarkMeshUpdate64x64`), `SetNodeVerticesWithTangents` runs ~5× faster on the CPU than `SetNodeGeometry` with zero GPU buffer reallocations. Use `SetNodeGeometry` when topology, UVs, or morph/skin deformation changes, and `Scene.ApplyMaterial` to update material parameters independently.
