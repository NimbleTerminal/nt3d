# Changelog

## Unreleased

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
