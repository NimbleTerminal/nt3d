---
title: Normal maps
weight: 42
---

Normal maps add surface detail by changing the shading normal between vertices.
GPU and software use them for diffuse light, specular highlights, and environment
diffuse light. They leave silhouettes, bounds, picking, wireframe, and shadow-caster
geometry unchanged. Shadow bias still uses the unperturbed surface normal.

## Materials

Decode a normal image with Go's image packages, then create a linear-data texture:

```go
normal, err := nt3d.NewNormalMap(img)
if err != nil {
    return err
}
mat := &nt3d.Material{
    NormalMap:      normal,
    NormalStrength: 1,
    NormalRepeat:  true,
    Specular:      0.4,
    Shininess:     48,
}
```

Assign the material to triangle geometry with one `Geometry.UV` per vertex.
`NormalStrength` scales tangent-space X and Y. **Set it to 1 for the authored
strength; zero disables the normal perturbation.** Negative strengths reverse the
X/Y slopes. `NormalRepeat` controls wrapping independently of the base-color
texture's `Repeat`. Both slots use the same `Geometry.UV` coordinates.

`Material.Texture` remains the base-color slot. Normal maps work with or without
it; without it, the mesh keeps its vertex colors. `Unlit` materials ignore normal
maps. Cutouts and transparent materials use their usual alpha and depth behavior.

`NewNormalMap` snapshots the image, ignores alpha, and accepts the same dimensions
as `NewTexture` (at most 8192 pixels a side). RGB encodes tangent X/Y/Z from -1 to 1
as values from 0 to 1. A conventional neutral image is approximately `(128,128,255)`;
8-bit rounding can leave a tiny tilt. Strength zero restores the exact interpolated
surface normal.

Unlike color textures, normal maps use **linear data**, with no sRGB decode.
Both renderers average the encoded vectors for mipmaps, filter them, apply strength,
and normalize the result. The GPU uses hardware sampling and software uses the
matching mip chain and anisotropic sampling. The material validator rejects a
`NewTexture` color texture in the normal slot and a normal map in the color slot.

To replace an existing map while retaining GPU geometry buffers:

```go
if err := scene.SetNormalMap("mesh", replacementNormalMap); err != nil {
    return err
}
```

Frames already submitted retain their old map. To add/remove a normal-map slot or
change strength or wrapping, update the geometry/material through `SetGeometry`.
For an animated mesh, reinstall its deformation definition if replacing geometry.

## Tangent basis and animation

This implementation derives each triangle's tangent directions from its current
positions and UVs, then orthonormalizes against the interpolated vertex normal.
Mirrored UVs preserve their handedness. Degenerate UV triangles fall back to the
interpolated normal. The basis follows transformed and deformed positions, so the
same path works for procedural meshes, shared instances, morph targets, and
CPU/GPU skinning. It adds no tangent buffers or per-frame tangent uploads.

This is a **per-triangle basis**, not MikkTSpace or an authored tangent basis.
Maps baked for smooth authored tangents can show seams or different shading,
especially on coarse curved meshes. Explicit tangent attributes and a compatible
tangent generator remain follow-ups. Preserve the mesh's authored vertex normals;
normal maps add detail to them rather than replacing them.

## glTF mapping

The loader maps `normalTexture` and its `scale` (default 1) into `NormalMap` and
`NormalStrength`. The image is cached separately from base color even if both
slots reference the same image, so their color-space interpretations stay correct.
Each map can select any readable `TEXCOORD_n`, including a `KHR_texture_transform`
UV-set override. Offset, rotation and scale are baked into its coordinates.
`Geometry.NormalUV` supplies independent normal-map UVs; nil uses `Geometry.UV`.
Both renderers derive the triangle basis from the normal-map coordinates, including
when clipping or skinning the mesh. Mesh operations preserve both UV arrays.
Independent UVs use an additional GPU record per index, halving the per-geometry
triangle limit; the glTF loader splits these meshes automatically.

`Model.Warnings` reports cases that cannot be represented:

- Authored `TANGENT` attributes are replaced with the generated triangle basis.
- Missing image or UV data falls back to vertex-normal shading; malformed UV accessors are rejected.
- Sampler addressing retains the loader's existing approximation: clamp only when
  both axes clamp; otherwise repeat, including mirrored-repeat samplers.

The [Khronos glTF specification](https://github.com/KhronosGroup/glTF/blob/main/specification/2.0/Specification.adoc#materials)
defines the linear-data RGB encoding and X/Y scale used here. Its authored-tangent
and MikkTSpace conventions are more precise than this first implementation's
triangle basis. Keep loader warnings visible when evaluating imported assets.

## Try it

`task lights` starts with a generated, periodic relief normal map on its shapes.
Press **n** to compare it with smooth surfaces, **e** for highlights, **l** for
lighting/environment presets, and **1 / 2 / 3** for GPU, software, and wireframe.
The example generates its image in Go and downloads no assets.

For relief that must alter silhouettes, picking, or shadows, use actual geometry,
for example `BuildHeightfield`. Normal-map rendering only changes lighting.
