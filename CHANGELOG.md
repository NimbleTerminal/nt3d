# Changelog

## Unreleased

- Update dependencies: `ntpicture` v0.1.1 (no per-pixel allocations in Kitty NRGBA packing, legacy glyph cache kept across views), `ntcanvas` v0.1.1 (no grid reallocation on unchanged resizes), and `go-booba` v0.10.2 for the browser demos (OSC 52 clipboard from wasm, wheel events as SGR mouse buttons, Ctrl+V / Cmd+V paste).

## v0.3.0

- The widget's `PickMsg` carries the click's world-space `Ray`, for scenes that pick their own way (the AABB tree demo casts it through its tree).
- Add `nt3d/aabbtree`, a generic dynamic AABB tree (`Tree[T]`): insert, remove, and update boxed values, and query them by box overlap or ray, independent of the scene and renderers. [AABB tree demo](https://nimbleterminal.github.io/nt3d/aabb/).
- Tangent frames for normal maps: `Geometry.Tangents` carries authored frames (glTF `TANGENT`), scenes generate MikkTSpace-compatible ones at upload (`GenerateTangents`) for normal-mapped meshes without them, and both renderers interpolate, skin, and morph them (`MorphTarget.Tangents`); the per-triangle basis remains the fallback. glTF models without `TANGENT` now shade as they do with it (green up the image). Also fixes the GPU reading independent normal UVs as zeros.
- Read Niantic SPZ Gaussian splats (versions 2 to 4, zstd included) through `nt3d/mesh/spz` or `nt3d/mesh/formats`, about ten times smaller than PLY. Point limits now derive from the 128 MiB WebGPU binding (`MaxPoints`, `MaxGaussians`), and point clouds larger than one geometry split into slabs along their longest axis (`nt3dmesh.PointPieces`).
- Coverage-preserving mipmaps for cutouts: cutout textures get a mip chain whose alpha is rank-matched to full size, so distant fur and leaves stay dense instead of thinning or shimmering, on the GPU and in software. `Material.FullSize` keeps the old full-size sampling; `m` in the [fur demo](https://nimbleterminal.github.io/nt3d/fur/) compares.
- Mipmap shader textures: the GPU renderer regenerates their chain each frame from the picture the shader draws (`ntgpuimage` v0.4.0 render-target mips), and the software renderer mips the fallback image, so distant procedural floors stop shimmering.
- Add `nt3d/clod`, clustered LOD selection: cluster a large item set once, then cull and thin clusters by distance each frame on the CPU or in WebGPU compute. [Point cloud demo](https://nimbleterminal.github.io/nt3d/cloud/).
- Speed up software texture sampling while preserving interpolation results.
- Avoid expensive color conversion for eligible unlit textured pixels, with byte-identical output.

## v0.2.0

- Add reusable animation clips, playback and blending, morph targets, and CPU/GPU skinning, with glTF support. [Animation demo](https://nimbleterminal.github.io/nt3d/animation/).
- Add self-contained animation galleries with embedded CC0 [Khronos samples](https://nimbleterminal.github.io/nt3d/animate/) and [Gobkit characters](https://nimbleterminal.github.io/nt3d/characters/).
- Add gradient and image skies with diffuse image-based lighting. [Lights demo](https://nimbleterminal.github.io/nt3d/lights/).
- Add normal-map rendering in GPU and software renderers, including glTF materials. Toggle with `n` in the [lights demo](https://nimbleterminal.github.io/nt3d/lights/).
- Add directional shadow maps and soft drop shadows. [Lights](https://nimbleterminal.github.io/nt3d/lights/) and [forest](https://nimbleterminal.github.io/nt3d/forest/) demos.
- Add world-anchored labels and billboards. [Terrain demo](https://nimbleterminal.github.io/nt3d/terrain/).
- Avoid per-pixel allocations when copying textures.

## v0.1.0

- Initial release of the scene engine, WebGPU/software/wireframe renderers, mesh loaders, terminal widget, and web gallery.
