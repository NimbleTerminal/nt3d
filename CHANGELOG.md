# Changelog

## Unreleased

- Add `Scene.SetNodeVertices`, `Scene.UpdateNodeVertices`,
  `Scene.SetNodeVerticesWithTangents`, and `(*nt3dwater.WaveTile).Apply` for
  in-place fixed-topology mesh updates that preserve indices, UVs, and
  materials, skip redundant per-frame `O(V + I)` validation and smooth-grid
  outline-normal maps, and overwrite existing WebGPU vertex buffers via
  `WriteBuffer` without buffer reallocations. Add `Material.EnvironmentReflect`
  so reflective (`Reflectivity > 0`) and `PBR` materials can sample
  `Frame.Environment` reflections when `Frame.EnvironmentBackgroundOnly` keeps
  diffuse environment SH lighting off for the rest of the scene, and add
  `Scene.SetMaterial` to replace or clear materials atomically across indexed
  meshes in a subtree (including `SharedGeometry`, `LOD` tiers, and deformed
  meshes) while preserving GPU mesh buffers when vertex-baked fields are
  unchanged. Wire all three into the `water` demo.
- Add `NewHDREnvironment` (zero-dependency Radiance RGBE `.hdr` / `.pic`
  panorama decoder feeding unclamped `RGBA16Float` radiance into GGX specular
  IBL, diffuse SH, HDR skies, and `Bloom`), `SpotLight` (`InnerCone`,
  `OuterCone`, and single-face perspective shadow maps on GPU and software),
  scene-graph punctual light nodes (`Scene.SetNodeLight`, `Lights`, `NodeLight`,
  `SceneLights`) with glTF `KHR_lights_punctual` decoding in `nt3d/mesh/gltf`,
  and McGuire & Bavoil Weighted Blended Order-Independent Transparency
  (`Frame.OIT` and `nt3dwidget` `WithOIT` / `SetOIT` / `OIT`, compatible with
  1× and 4× WebGPU MSAA and software float accumulation). Wire `HDR IBL` and
  `spotlights` into `lights`, an intersecting-glass OIT showcase (`m` and `t`)
  into `shapes`, an animated `KHR_lights_punctual` **Spotlight stage** scene
  into `animate`, `RotationX` / `RotationY` into `nt3d/math`, and `.hdr`
  panorama loading, `Spotlights`, and intersecting crystal-glass `OIT` into
  `examples/gogpu`.
- **Breaking:** move Gerstner wave surfaces, wake impulses, tiled water meshes,
  and Archimedes hull buoyancy, drag, and planing evaluation out of the root
  `nt3d` package into `nimbleterminal.dev/nt3d/water` (`package nt3dwater`),
  renaming `WaterSurface`, `WaterBodyState`, and `WaterForces` to `Surface`,
  `BodyState`, and `Forces`.
- **Breaking:** move shell-fur and silhouette-fin builders out of the root
  `nt3d` package into `nimbleterminal.dev/nt3d/fur` (`package nt3dfur`) as
  `Config`, `Coat`, and `New`. Expose `Texture.NRGBAAt` and `Texture.Sample`
  for straight-alpha texel reads and bilinear UV sampling.
- Add Cook-Torrance GGX metallic/roughness PBR, packed linear ORM maps,
  roughness-filtered specular IBL, and HDR emissive materials to `Material` on
  both WebGPU and software renderers (`PBR`, `Metallic`, `Roughness`, `IOR`,
  `ORMMap`, `ORMRepeat`, `OcclusionStrength`, `Emissive`, `EmissiveIntensity`,
  `EmissiveMap`, `EmissiveRepeat`, `NewORMMap`, `NewShaderORMMap`,
  `NewShaderORMMapWithParameters`, `Scene.SetORMMap`, and
  `Scene.SetEmissiveMap`). Update `nt3d/mesh/gltf` to decode real PBR
  metallic/roughness, merged occlusion/metallic-roughness ORM maps, emissive
  textures, and `KHR_materials_emissive_strength`, `KHR_materials_ior`,
  `KHR_materials_transmission`, and `KHR_materials_volume`, and showcase PBR
  and emissive materials in `lights` (`e` material cycle) and `examples/gogpu`.
- Add `Frame.MSAA` (4× WebGPU multisample anti-aliasing across scene passes,
  transmission captures, and `GPUSession.Pick` depth reads, shared across
  `GPUSession.NewView`) and `Frame.PostProcess` (`SSAO`, HDR `Bloom`,
  `Exposure`, `ReinhardToneMap` / `ACESToneMap`, and `Vignette`) on GPU with
  tiered CPU software fallbacks and `nt3dwidget` `WithMSAA` / `SetMSAA` and
  `WithEffects` / `SetEffects`. Wire AA and post-processing controls into the
  demos (`--msaa`, `--effect`, `fur`, `mipmaps`, `lights`) and `examples/gogpu`
  (`-msaa`, `-post`, `4`/`M`, `F12`/`X`).
- Add `Scene.SetLOD` and `LODLevel` for multi-tier distance-based level of
  detail with shared GPU buffers and BVH pick indexes across instances,
  per-tier outline overrides, and optional far-distance cutoffs.
- Add `Frame.Stats` and `FrameStats` to report total, visible, and
  frustum-drawn instances, draw batches, triangles, lines, and splat-budgeted
  points. Update the `forest` demo to showcase three-tier tree LODs (`l` to
  cycle `LOD on`, `LOD tint`, and `LOD off`) and live `Frame.Stats` counters.
- Add `Camera.Forward` and `Camera.Basis` to return the camera's world-space
  viewing direction and orthonormal right/up/forward basis alongside
  `Camera.Eye`.
- Document remaining exported symbols across all library packages, and separate
  file copyright headers from `package` clauses so `go doc` and `pkg.go.dev`
  render clean package overviews.
- Split the `examples/gogpu` desktop viewer into focused source files,
  consolidate benchmark artifacts under `docs/performance/`, add
  `CODE_OF_CONDUCT.md`, and refresh `README.md`.

----

## v0.9.0 (2026-11-12)

This release adds toon shading and crease outlines to the GPU and software
renderers, with a new demo showing both. It also improves camera depth
precision, adds controls for embedding nt3d in applications with their own
camera and keyboard handling, and fixes a Vulkan binding-order crash.

### Toon shading and outlines

- Add `Material.Toon` and `ToonStyle{Bands, Softness, Shade, Rim}` for banded
  lighting, tinted ambient shading, flat specular highlights, and rim lighting.
  `Scene.SetToon` applies the style to indexed meshes throughout a subtree.
  Toon band edges use antialiasing to reduce jagged transitions on flat faces.
- Add `OutlineStyle.Crease`, an angle in degrees, to draw sharp edges, open
  borders, and edges shared by more than two faces on opaque rigid meshes.
  Crease lines use the outline's color and width, and their geometry caches
  stay bounded as styles change.
- Add the `toon` demo to `task toon`, the browser gallery, and the GoGPU
  viewer. Spot and the robot stand in a Manhattan block baked from
  OpenStreetMap data (`flatiron.obj`, © OpenStreetMap contributors, ODbL).
  The fur demo also gains a `t` toggle for toon bands and inked fur tips.

### Cameras and embedding

- Fix perspective cameras with only `Far` set: `Near == 0` now retains an
  automatic positive near plane instead of collapsing depth values. If Far
  falls inside the automatic near plane, scale Near to Far at the default
  4,000:1 ratio. Preserve infinite perspective far planes (`Near > 0,
  Far == 0`) and explicit zero near planes for orthographic cameras.
- **Clipping behavior change:** increase the automatic near plane from
  `Distance * 0.01 / 3.4` to `Distance * 0.05 / 3.4`. This improves perspective
  depth precision and reduces z-fighting, but clips geometry very close to
  the eye sooner. Set an explicit positive `Near` when closer viewing is
  needed; set `Far` too when a finite perspective far plane is desired.
- Add `Camera.LookDirection(eye, forward)` to position first-person and chase
  cameras while preserving Distance and the automatic clipping scale.
  `LookAt` continues to set Distance from the eye-to-target distance.
- Add `nt3dwidget.WithDefaultKeyBindings(false)` so host applications can own
  keyboard controls while retaining the widget's mouse orbit, zoom, and
  picking. Existing keyboard bindings remain enabled by default.

### Reliability and dependencies

- Fix Vulkan render and compute binding order: select pipelines before
  binding resources, restore scene lighting across pipeline layout changes,
  and defer resumed-pass bindings until a pipeline is selected. This removes
  the `SetBindGroup` nil-pointer crash and prevents dropped particle-sort and
  clustered-LOD compute bindings.
- Add native Go fuzz targets for mesh decoders, polygon triangulation,
  AABB tree operations, spherical-harmonic rotation, and line clipping.
  Decoder tests use bounded inputs and include format round trips where
  supported. Run all targets briefly with `task fuzz-smoke`.
- Update dependencies across the library and both example modules:
  ntpicture v0.2.0, booba v0.13.0, gpucontext v0.32.0, Lip Gloss v2.1.0,
  Bubblezone v2.0.1, compress v1.20.4, pflag v1.1.0, terminfo v1.3.1,
  and golang.org/x/image v0.48.0, x/sync v0.25.0, and x/sys v0.50.0.
- Retain wgpu v0.34.5, naga v0.20.4, and gogpu v0.54.0 for Metal
  compatibility. Tested wgpu v0.34.14 with naga v0.21.10 fails five Metal
  environment/reflection tests; gogpu v0.54.1 requires a newer GPU stack.

### Validation and known limitations

- Full CI passes on macOS and Linux ARM64, including race tests, vet, native
  builds, and browser builds. All Metal hardware GPU suites pass.
- **Linux Vulkan GPU validation is not yet passing.** Testing on an NVIDIA
  GB10 with driver 580.178.04 confirms the binding-order crash is fixed, but
  rendering, particle-sort, and clustered-LOD comparisons still fail, and
  the water GPU demo reports device loss. See [ROADMAP.md](ROADMAP.md) for
  reproduction details and follow-up work.
- Toon cast-shadow boundaries can remain stair-stepped in large scenes.
  Crease lines currently support opaque rigid meshes; skinned, morphed,
  transparent, cutout, and transmissive meshes retain hull outlines only.

----

## v0.8.1 (2026-11-04)

- Update gputypes to v0.8.2 and goffi to v0.6.8 across all three Go modules.
  Retain wgpu v0.34.5 and naga v0.20.4: wgpu v0.34.6/v0.34.7 fail Metal
  hardware tests with their matching naga versions.

- Update ntcanvas to v0.1.4 and ntpicture to v0.1.5 across the library and
  example modules; ntgpuimage remains at v0.6.0.
- Migrate the browser demos and asset tool from `github.com/NimbleMarkets/go-booba`
  v0.10.7 to `nimbleterminal.dev/booba` v0.12.1. Regenerate browser assets with
  `task build-web` to pick up the bundled ntwebterm v0.4.2 runtime.

## v0.8.0 (2026-10-30)

- Add `GPUSession` and `GPUTarget` for rendering into host-owned WebGPU devices,
  command encoders, and texture views without CPU readback. Support sRGB and
  linear RGBA/BGRA targets, RGBA16Float, alpha composition with `GPUTarget.Load`,
  and sub-viewports through `GPUTarget.Viewport`.
- Add `GPUSession.NewView` to share pipelines, geometry, splat, texture, and
  environment/reflection caches across views while keeping per-view state
  independent. Use ntgpuimage's borrowed-device support and accept any
  `BindingLimiter` in `AdoptBindingLimit`.
- Add GPU-depth picking, `ReadDepth`, `RenderToImage`, and `RenderToImageInto`,
  plus asynchronous BVH warming for CPU picking.
- Expand the GoGPU desktop example into an 18-demo gallery and model/splat
  viewer with file dialogs and drag-and-drop, HUD controls, pick annotations,
  translate/rotate/scale gizmos, a camera orientation triad, lighting and shadow
  presets, and PNG/GIF turntable capture. Pin published GoGPU v0.54.0 so no
  sibling GoGPU checkout is required.
- Add world-unit outlines with `OutlineStyle.Units` (`WorldUnits`) and
  position-averaged contour normals for seamless expansion of split-normal
  meshes on both GPU and software renderers.
- Include clustered alpha-blended point clouds in `Frame.SplatBudget` continuous
  LOD selection, scaling retained point radii by 1/√f. Carry leaf entry distances
  through `nt3daabbtree.Tree.RayCast` so each leaf is slab-tested once.
- Add `--kitty-frame-edits=auto|on|off`, active update-mode status, and an FPS
  readout for the animation demo. Add paired headless benchmarks for animation
  and water, with per-frame protocol bytes, uploaded pixel area, preparation/
  encoding time, and scene render time; include the recorded comparison results.
- Update ntcanvas to v0.1.3, ntgpuimage to v0.6.0, ntpicture to v0.1.4, go-booba
  to v0.10.7, terminfo to v1.3.0, and the browser-compatible Bubble Tea fork to
  `536722b0bdb6`. ntpicture brings unknown-terminal capability probing,
  synchronized image uploads to prevent replacement flicker, and cleanup of
  unanswered shared-memory probes.

----

## v0.7.0

- Update dependencies: `ntpicture` v0.1.2, `ntcanvas` v0.1.2, `ntgpuimage` v0.5.0, `go-booba` v0.10.5, Bubble Tea v2.1.1, and Ultraviolet at `a45b7c86333f`; synchronize the library and demo modules.

- `nt3d.SetBindingLimit` and `AdoptBindingLimit(renderer)` raise the per-geometry storage limit above WebGPU's 128 MiB baseline on capable devices, up to 1 GiB, with points still capped at 16,777,216 a geometry; `Validate`, `MaxGaussians`, the new `PointLimit`, `MeshTriangleLimit`, and `MeshIndexLimit`, and the decoders' piece splitting follow it, and the GPU renderer refuses a geometry past its device's own limit with a clear frame error. The demos adopt the device's limit before loading.

- The software renderer blends transparent fragments with headroom above white, as the GPU's 16-bit float target does, clamping once per pixel instead of at every blend, so splats whose spherical harmonics evaluate above 1 match the GPU renderer where they overlap.

- `nt3d-palette` (`cmd/nt3d-palette`, built to `bin/`) reads PLY, compressed PLY, SPZ, or SOG, builds an SH palette with `BuildSHPalette`, and writes a SOG v2 bundle through the new `nt3dsog.Encode`, so later loads keep the palette and skip the k-means; a SOG that already holds one passes through unchanged. The bundle's images are PNG, not WebP.

----

## v0.6.0

- Gaussian splats can share their spherical harmonics through a palette (`Geometry.GaussianSHPalette` and `GaussianSHLabels`), drawn by both renderers: `nt3dmesh.BuildSHPalette` (or `Options.SHPalette = SHPaletteBuild` for PLY, compressed PLY, and SPZ) builds one by two-level k-means, taking a degree-3 splat's SH from 48 bytes on the GPU to 5, plus a palette of at most 2.9 MB per geometry; `w` in the meshes demo builds one in the background. A decoded SOG now keeps its palette, so its `GaussianSH` holds DC only: code that reads a SOG's `GaussianSH` must call `nt3d.ExpandSHPalette` or decode with `Options.SHPalette = SHPaletteExpand`.

----

## v0.5.0

- Gaussian clouds cluster at load and both renderers draw a budgeted subset each frame (`Frame.SplatBudget`, default one million), culled and thinned by projected size with coverage scaling, so the sort and draw cost follow the budget rather than the capture's size.

- Frames no longer re-validate scene-stored geometry every frame: a store-time certificate covers points and clusters, so a 2.5M-splat frame dropped from 67 to 17 ms and a 1M frame from 28 to 7 ms on an M5 Max.

----

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

----

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

----

## v0.2.0

- Add reusable animation clips, playback and blending, morph targets, and CPU/GPU skinning, with glTF support. [Animation demo](https://nimbleterminal.github.io/nt3d/animation/).
- Add self-contained animation galleries with embedded CC0 [Khronos samples](https://nimbleterminal.github.io/nt3d/animate/) and [Gobkit characters](https://nimbleterminal.github.io/nt3d/characters/).
- Add gradient and image skies with diffuse image-based lighting. [Lights demo](https://nimbleterminal.github.io/nt3d/lights/).
- Add normal-map rendering in GPU and software renderers, including glTF materials. Toggle with `n` in the [lights demo](https://nimbleterminal.github.io/nt3d/lights/).
- Add directional shadow maps and soft drop shadows. [Lights](https://nimbleterminal.github.io/nt3d/lights/) and [forest](https://nimbleterminal.github.io/nt3d/forest/) demos.
- Add world-anchored labels and billboards. [Terrain demo](https://nimbleterminal.github.io/nt3d/terrain/).
- Avoid per-pixel allocations when copying textures.

----

## v0.1.0

- Initial release of the scene engine, WebGPU/software/wireframe renderers, mesh loaders, terminal widget, and web gallery.
