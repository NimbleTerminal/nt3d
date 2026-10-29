# Changelog

## Unreleased

- `GPUSession` and `GPUTarget` (`NewGPUSession`, `NewGPUSessionFromProvider`, `Encode`, `Render`, `BindingLimit`, `Close`) render frames directly into a caller-supplied `*wgpu.TextureView` on a caller-owned `*wgpu.Device` and `*wgpu.CommandEncoder` without CPU readback, adapting sRGB and linear transfer curves across `RGBA8Unorm`, `RGBA8UnormSrgb`, `BGRA8Unorm`, `BGRA8UnormSrgb`, and `RGBA16Float` targets. `GPUTarget.Load` alpha-composites the resolved frame over existing target contents, `GPUTarget.Viewport` resolves into a sub-rectangle for split-screen panes or picture-in-picture insets, `AdoptBindingLimit` accepts any `BindingLimiter` (including `*GPUSession`), and `examples/gogpu` (`task gogpu`) demonstrates native GoGPU window and multi-window embedding.

## v0.7.0

- Update dependencies: `ntpicture` v0.1.2, `ntcanvas` v0.1.2, `ntgpuimage` v0.5.0, `go-booba` v0.10.5, Bubble Tea v2.1.1, and Ultraviolet at `a45b7c86333f`; synchronize the library and demo modules.

- `nt3d.SetBindingLimit` and `AdoptBindingLimit(renderer)` raise the per-geometry storage limit above WebGPU's 128 MiB baseline on capable devices, up to 1 GiB, with points still capped at 16,777,216 a geometry; `Validate`, `MaxGaussians`, the new `PointLimit`, `MeshTriangleLimit`, and `MeshIndexLimit`, and the decoders' piece splitting follow it, and the GPU renderer refuses a geometry past its device's own limit with a clear frame error. The demos adopt the device's limit before loading.

- The software renderer blends transparent fragments with headroom above white, as the GPU's 16-bit float target does, clamping once per pixel instead of at every blend, so splats whose spherical harmonics evaluate above 1 match the GPU renderer where they overlap.

- `nt3d-palette` (`cmd/nt3d-palette`, built to `bin/`) reads PLY, compressed PLY, SPZ, or SOG, builds an SH palette with `BuildSHPalette`, and writes a SOG v2 bundle through the new `nt3dsog.Encode`, so later loads keep the palette and skip the k-means; a SOG that already holds one passes through unchanged. The bundle's images are PNG, not WebP.

## v0.6.0

- Gaussian splats can share their spherical harmonics through a palette (`Geometry.GaussianSHPalette` and `GaussianSHLabels`), drawn by both renderers: `nt3dmesh.BuildSHPalette` (or `Options.SHPalette = SHPaletteBuild` for PLY, compressed PLY, and SPZ) builds one by two-level k-means, taking a degree-3 splat's SH from 48 bytes on the GPU to 5, plus a palette of at most 2.9 MB per geometry; `w` in the meshes demo builds one in the background. A decoded SOG now keeps its palette, so its `GaussianSH` holds DC only: code that reads a SOG's `GaussianSH` must call `nt3d.ExpandSHPalette` or decode with `Options.SHPalette = SHPaletteExpand`.

## v0.5.0

- Gaussian clouds cluster at load and both renderers draw a budgeted subset each frame (`Frame.SplatBudget`, default one million), culled and thinned by projected size with coverage scaling, so the sort and draw cost follow the budget rather than the capture's size.

- Frames no longer re-validate scene-stored geometry every frame: a store-time certificate covers points and clusters, so a 2.5M-splat frame dropped from 67 to 17 ms and a 1M frame from 28 to 7 ms on an M5 Max.

## v0.4.0

- Gaussian splats store compactly on the GPU by default: half-float axes and byte spherical harmonics at SPZ's precision, so a geometry holds 2,796,202 degree-3 splats instead of 699,050 and a million-splat cloud takes about 92 MB instead of 260; `PointStyle.Precise` keeps full-precision records. Niantic's SPZ samples now open as one slab each.

- Blended points sort on the GPU every frame, globally across geometries to within one of 64 depth slices, with transparent meshes interleaved; the software renderer orders the same way. Slabs of a big cloud and overlapping clouds blend in order.

- `nt3dmesh` decodes PlayCanvas SOG (`.sog` bundles, unbundled `meta.json`, legacy v1) through `nt3d/mesh/sog`, and SuperSplat compressed PLY through `DecodePLY`, into the same Gaussian geometry as PLY and SPZ.

- Water adds touring/sport handling, progressive throttle and steering, jet trim, rider lean, and predictive autopilot control. Debug pages show hull probe immersion, force/velocity arrows, handling telemetry, and landing measurements, with paused single-step inspection.

- `BuoyantHull` supplies world-space buoyancy, water-relative drag, and empirical planing forces/torques to external rigid-body engines. The water demo integrates these at 120 Hz with stern-jet propulsion, steering, and a `p` planing comparison.

- `WaveSurface.WithWake` adds bounded crest/trough impulses with analytic normals, tangents, and velocities shared by mesh and world queries.
- The water craft now deforms its wake and creates landing disturbances; `z` compares displacement independently of foam and spray.

- `NewFlowMap` and material flow phase/strength controls advect normal maps with matching GPU/software shading and continuous dual-phase blending.
- `BuildWaveTiles` creates bounded, independently culled water tiles with matching shared borders and allocation-free geometry updates.
- Water demonstrates terrain-driven shoreline foam, bank-aligned flow, and 10×10 / 30×30 water extents (`v` / `m` comparisons).

- Scene color/depth capture for `Material.Transmission`, normal-driven `Refraction`, and RGB Beer–Lambert `Absorption`, with GPU/software parity and foreground rejection.
- `PointStyle.SoftDistance` fades blended particles against opaque and transmissive surface depth. Water demonstrates refractive shallows and soft spray with `t` / `h` comparisons.

- Water wake ribbons and ballistic spray reuse transparent meshes and soft point particles, with bounded pools, wave-following foam, turn spray, landing bursts, and an `f` comparison control.
- Wireframe hides zero-alpha blended particles, matching dormant particle pools in the other renderers.

- Water driving prototype: throttle, steering, autopilot, crest launches and ballistic landings with fixed-step physics; animated ripple normals and sky reflections with comparison toggles.
- `Material.Reflectivity`: Schlick Fresnel environment reflections in GPU and software, respecting normal maps and environment intensity/background-only settings.
- `NewShaderNormalMap` / `NewShaderNormalMapWithParameters`: procedural linear-data normal targets with mipmaps and matching software fallbacks.
- Shared Gerstner wave surfaces: analytic normals and velocities, world-coordinate surface queries, and a `water` demo with a four-probe float on the same animated surface in every renderer.

## v0.3.1

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
