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

## Tangent frames

A normal map is baked against a tangent frame: the direction `T` in which
its U grows along the surface, and a handedness `W` (+1 or -1) giving the
bitangent as `W × (N × T)`, the glTF convention. Each vertex may carry one in
`Geometry.Tangents`, beside `UV` and `NormalUV`:

```go
g.Tangents = append(g.Tangents, nt3d.Tangent{Direction: nt3dmath.Vec3{X: 1}, W: 1})
```

Both renderers choose the frame the same way, in this order:

1. **Authored** `Geometry.Tangents`, set by you or by the glTF loader from a
   `TANGENT` attribute. Validation requires one per vertex, finite, with `W`
   exactly ±1; a zero direction perturbs nothing at that vertex.
2. **Generated at upload.** `SetGeometry`, `SetNodeGeometry`, and
   `NewSharedGeometry` fill `Tangents` with `GenerateTangents` when a
   geometry has a normal map, vertex UVs, no tangents of its own, and at
   most half the triangles the binding limit holds (`MaxMeshTriangles/2` at
   the default). A frame's `Geometry.Tangents` being
   non-empty tells you a scene geometry took this path or the first.
3. **Per-triangle basis**, derived from the triangle's (deformed) positions
   and normal-map UVs: frames built by hand without a scene, meshes past the
   limit, and anything else without tangents. Mirrored UVs keep their
   handedness; degenerate UV triangles fall back to the interpolated normal.

`GenerateTangents` is a self-contained port of the parts of MikkTSpace that
decide the frame, with that library's default parameters, so maps baked in
Blender or for glTF exporters look as their authors saw them. Corners are
welded by position, normal, and UV, so indexing does not matter; around each
vertex, faces joined through shared edges with the same UV handedness
average their U directions, weighted by their angle there, projected onto
the vertex normal. Each vertex takes the frame of its first corner, so a
vertex that faces of opposite handedness share (across a mirrored UV seam)
gets one side's frame: give each side its own vertices, as the glTF loader
does by splitting them. `GenerateCornerTangents` is the per-corner form for
loaders. Neither keeps MikkTSpace's magnitudes or a stored bitangent.

nt3d's own frames follow the UVs: on the per-triangle basis and in
`GenerateTangents` alike, the map's green points toward growing V, which is
down the image (its first row is V 0). A map whose green points up the
image, as glTF's and Blender's do, wants `W` negated, as the glTF loader
does for the frames it generates; a mesh with frames of mixed handedness
uses the sign interpolated at each pixel on both renderers.

The frame is interpolated across each triangle like the normal, projected
onto the interpolated normal at the pixel, and normalized. Tangents turn
with positions, not as normals do: instance transforms, `TransformGeometry`
(which also flips `W` under a reflection), skinning, and morph targets
(`MorphTarget.Tangents` are direction deltas) all apply to them, so the
frame follows the deformed surface with no per-frame regeneration. On the
GPU a normal-mapped mesh with tangents or independent normal UVs keeps an
extension record per index, which halves its per-geometry triangle limit
(`MaxMeshTriangles/2` at the default binding limit, `MeshTriangleLimit()/2`
in general); `Validate` rejects larger ones and the glTF loader splits them. A transparent mesh seen from behind keeps the map's U and V
directions and flips only the normal. Wireframe, picking, shadows (whose
bias uses the unperturbed normal), and outlines ignore tangents. Preserve
the mesh's authored vertex normals; normal maps add detail to them rather
than replacing them.

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

Authored `TANGENT` attributes are used as they are, with their `W`. Without
them, the loader generates frames per corner from the primitive's positions,
normals, and the normal map's coordinates (after any texture transform), with
`W` negated for glTF's image-up green so they agree with authored ones, and
splits a vertex whose corners disagree, as MikkTSpace would; morph targets'
`TANGENT` deltas are read too. Mirrored nodes and the back copies of
double-sided faces get their handedness flipped.

`Model.Warnings` reports cases that cannot be represented:

- Unreadable or mis-sized `TANGENT` data is passed over and frames are generated.
- Missing image or UV data falls back to vertex-normal shading; malformed UV accessors are rejected.
- Sampler addressing retains the loader's existing approximation: clamp only when
  both axes clamp; otherwise repeat, including mirrored-repeat samplers.

The [Khronos glTF specification](https://github.com/KhronosGroup/glTF/blob/main/specification/2.0/Specification.adoc#materials)
defines the linear-data RGB encoding, X/Y scale, and tangent conventions used
here. Keep loader warnings visible when evaluating imported assets.

## Try it

`task lights` starts with a generated, periodic relief normal map on its shapes.
Press **n** to compare it with smooth surfaces, **e** for highlights, **l** for
lighting/environment presets, and **1 / 2 / 3** for GPU, software, and wireframe.
The example generates its image in Go and downloads no assets.

For relief that must alter silhouettes, picking, or shadows, use actual geometry,
for example `BuildHeightfield`. Normal-map rendering only changes lighting.
