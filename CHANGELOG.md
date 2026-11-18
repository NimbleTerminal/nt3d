# Changelog

## Unreleased

- `nt3dmesh.Options.AssetOnly` decodes a glTF into `Model.Asset` alone,
  leaving `Objects` empty. A glTF decode otherwise reads every primitive
  twice, placed for the flat view and again in its own space for the asset;
  a caller that only instantiates needs the second. `MaxTriangles` then
  bounds what the asset holds, counting a shared mesh once.
- Textures convert without allocating per pixel. `NewTexture`, and the glTF
  occlusion-roughness-metallic packer, read JPEG and WebP images (`YCbCr`,
  `NYCbCrA`), premultiplied `RGBA`, and `Gray` through their own accessors
  instead of boxing each pixel's color; the pixels are identical. A 5 MB
  glTF with WebP textures decodes in 211 ms instead of 300, with 38 thousand
  allocations instead of 13.9 million, and in 181 ms and 126 MB with
  `AssetOnly`, from 239 MB.
- `nt3dgltf` internals: each material extension is a function and a row in
  one table, which also declares it supported, in place of a 330-line
  function; a test holds every extension name the package reads to the
  tables. The scene-asset builder has its own file.

- **Breaking: `Material.EnvironmentReflect` is removed.** It let one material
  reflect the environment when `EnvironmentBackgroundOnly` made it a backdrop.
  A reflection probe holding the same panorama does that for every reflective
  surface inside its box, and is independent of the setting:
  `scene.SetReflectionProbes([]nt3d.ReflectionProbe{{Environment: sky,
  Position: p, Bounds: box}})`. Where the environment lights the frame the
  field did nothing and can simply be deleted.

- **Breaking: the camera-path bridge has its own package.** `CameraFromState`,
  `StateFromCamera`, `SceneResolver`, `AppendCameraPathOverlay`, and
  `CameraPathOverlayOptions` move from package `nt3d` to
  `nimbleterminal.dev/nt3d/cam/scene` (package `nt3dcamscene`), under the same
  names. The engine no longer imports `nt3d/cam`, so a program that does not
  use camera paths does not build them.

- `MaxVectors` is renamed `MaxArrows`, which is what it limits.
- Orientation for people and agents changing or using the engine: the package
  documentation describes the Scene, Frame, Renderer pipeline and where things
  are; the guide gains an Architecture page mapping packages and files and
  the rules that keep the renderers in step; `AGENTS.md` states the
  repository's invariants and workflow; and the root package and widget have
  compiled examples for the pipeline, draw order, node data, the look, editing
  a frame, and building one by hand.

- Shader hooks: a hook whose WGSL declares a name the scene shader uses is
  refused by `NewVertexDisplacement` and `NewMaterialShader` with an error
  naming it. Hooks are compiled into the scene shader's module and share its
  names, so such a hook never compiled on a GPU; it now fails when it is
  made, in software too, instead of on the first GPU frame with the shader
  translator's message. A displacement and a material shader on one mesh that
  both declare a name fail that frame the same way.
- GPU hook programs no frame has drawn for 600 frames are released, and
  compiled again if their source returns. They were kept until the renderer
  closed, so an application that generates shader sources grew without bound.
- GPU renderer internals, with no change in what is drawn: the unreachable
  single-renderer code path beside the shared device state is removed (some
  300 lines); primitive slots, their entry points, and the draw order are
  named rather than numbered; the scene shader is ten files by topic under
  `wgsl/`, joined into the same module text; and tests hold the byte counts Go
  writes for the scene uniform, instance, mesh, and point records to the
  structs the shader declares.

- The engine no longer links the terminal UI stack. `RenderWireframe` was the
  root package's only use of ntcanvas, whose single package brought Bubble
  Tea, Lip Gloss, and bubblezone to every importer, `water` and `fur`
  included. It now uses `nimbleterminal.dev/ntcanvas/grid` (ntcanvas v0.2.0),
  which imports none of them, and wireframe output is unchanged. Only
  `nt3dwidget` imports Bubble Tea, so a program that does not use the widget
  no longer builds it.

- **Breaking: a frame is one list of draws.** `Frame.Geometry`, `Instances`,
  `GeometryRevisions`, and `Shared`, four slices kept in step by index, are
  one slice, `Frame.Draws`, and `Instance` is gone. A `Draw` holds a node's
  `Geometry`, `World`, `Node`, `Name`, `Data`, `Revision`, and `Shared`, and
  is `Hidden` rather than not `Visible`, so that `Draw{Geometry: g}` alone
  draws `g` where it lies: a zero `World` is the identity. To migrate:

  | Before | After |
  | --- | --- |
  | `f.Geometry[i]` | `f.Draws[i].Geometry` |
  | `f.Instances[i].World`, `.Node`, `.Name` | `f.Draws[i].World`, `.Node`, `.Name` |
  | `f.Instances[i].Visible = false` | `f.Draws[i].Hidden = true` |
  | `f.GeometryRevisions[i]`, `f.Shared[i]` | `f.Draws[i].Revision`, `f.Draws[i].Shared` |
  | `Frame{Geometry: []Geometry{g}}` | `Frame{Draws: []Draw{{Geometry: g}}}` |
  | reordering all four slices | `Scene.SetNodeDrawOrder`, or reorder `Draws` |

  Per-node GPU caching is now per draw: a draw with a nonzero `Revision`
  needs a `Node` that is nonzero and its alone, and a frame may mix cached and
  uncached draws. `Scene.Frame` makes three allocations however many nodes
  it draws.

- **Breaking in behaviour only: `Geometry` is plain data.** Its eight
  unexported fields (certificates, partial-update ranges, skin pose, rest
  bounds, level-of-detail tier) moved beside it into the `Draw`, so every
  field of a `Geometry` is the application's, and a geometry copied out of a
  frame carries nothing with it. A frame edited by assigning to a scene-made
  draw's `Geometry` should replace the whole `Draw` instead.

- Four additions close gaps in what a scene could say about its frames, which
  applications had been working around by editing frames after they were
  made. Nothing existing changes.

  - **Draw order.** `Scene.SetNodeDrawOrder(id, DrawOrder{Layer, Key})` places
    a node, and the nodes under it, among those a frame draws: by layer, then
    key, then the order they were added. Blended geometry can be put after
    what shows through it, and a scene that loads in the background can draw
    the same way every time, without reordering `Frame.Geometry`,
    `Instances`, `GeometryRevisions`, and `Shared` by hand. The sorted order
    is kept between frames.
  - **Node data.** `Scene.SetNodeData(id, v)` attaches an application's own
    value to a node. It comes back from `NodeData`, `Node.Data`,
    `Instance.Data`, and `Hit.Data`, so nothing has to be encoded into node
    names. `SceneAsset.Instantiate` now documents how it names its nodes.
  - **Stills.** `Scene.StillFrame` takes a frame that depends on the scene and
    camera alone: levels of detail are chosen by distance, and the hysteresis
    memory `Scene.Frame` keeps is neither read nor written. A capture no
    longer needs `SetLODHysteresis(0)` around it. `nt3dwidget`'s `Snapshot`
    uses it.
  - **Look.** `Scene.SetLook(Look{...})` holds the background, lighting, fog,
    environment settings, and post-processing, and every frame starts out
    with them, so a scene is lit where it is built instead of on each frame.
    `SetEnvironment` sets the look's `Environment`. Light positions are world
    space and follow `Camera.Origin`. `nt3dwidget` draws the scene's look
    unless it was given a setting of its own (`WithBackground`, `SetFog`,
    `SetLighting`, `WithEffects`, and the rest).

- Fix two decoder panics on malformed files. A glTF/GLB document with `null`
  in any array of objects (materials, scenes, cameras, mesh primitives,
  animation channels and samplers, and the rest) is refused by name, such as
  `materials[0] is null`, including a null nothing refers to, which was
  accepted before. A PLY range grid whose `num_cols` × `num_rows` overflows
  is skipped like any other size that does not match the element.

- `Scene.Frame` allocates its slices once a call instead of twice a node:
  10,000 nodes cost 6 allocations and 8.9 MB, from 20,078 and 43.7 MB, in a
  little over half the time. Frames are unchanged.

- `nt3dwidget`: a frame that software or wireframe mode was asked for and
  refused no longer closes the GPU renderer held for WebGPU mode. Only a GPU
  fallback lets it go, as documented.

- `nt3dwater.WaveTile.RestBounds` returns a valid `AABB`; it returned one
  that `IsValid`, `Transformed`, and culling treated as empty.

- `task ci` type-checks the `webgpu`-tagged GPU tests in every module (`go vet
  -tags webgpu`), which no CI step compiled before. Running them still takes
  `task test-gpu` and a GPU.

- Add `task room`: an interactive cutaway-room demo with independent local
  probe and lightmap toggles, a moving polished sphere, bundled synthetic HDR
  fixtures, and terminal, native-gallery, and web entry points.

- Consume externally baked linear HDR irradiance through `NewLightMap` /
  `NewHDRLightMap`, `Material.LightMap`, and independent `Geometry.LightMapUV`.
  GPU and software replace indirect diffuse, preserve direct/specular/emissive
  lighting, and share half-float mip filtering. UVs survive cloning, clipping,
  merging, and normal rebuilding; material replacements preserve snapshots.

- Add authored local `ReflectionProbe` panoramas with capture positions,
  influence boxes, and box-projected PBR, clearcoat, and classic reflections
  on GPU and software. `Scene.SetReflectionProbes` snapshots up to four
  ordered probes; uncovered surfaces retain environment lighting. LDR/HDR
  textures share the existing environment cache and roughness filtering.

- Extract internal Go/WGSL indirect-light sampling from material response,
  with premultiplied radiance and coverage composition for future local and
  screen-space sources. Preserve hemisphere, SH, GGX, clearcoat, and toon
  lighting, with source occlusion that leaves coverage unchanged.

- Apply SSAO within indirect lighting on WebGPU and complete software frames,
  preserving direct lights, emission, and unlit surfaces. Combine screen-space
  and material AO by minimum visibility. SSAO now adds an opaque geometry
  pass before shading; disabled SSAO retains the single scene pass.

## v0.11.1 (2026-11-16)

- Add `Scene.SetLODReferenceBounds`, which gives a `Scene.SetLOD` node shared
  reference bounds (in the node's local space) to measure its distance from
  instead of its own tier bounds, so the separate parts of one chunk switch
  tiers together. It resets the node's LOD hysteresis; `SceneAsset` already did
  this internally for multi-part props.

## v0.11.0 (2026-11-16)

This release completes pure-Go, WASM-safe glTF 2.0 and OGC 3D Tiles 1.1 asset
loading, instanced multi-colorway switching, and distance-tiered multi-LOD
compilation across `nt3d` and `nt3d/mesh`: KTX 2.0 (`KHR_texture_basisu`
BasisLZ/ETC1S and 19-mode UASTC), `KHR_mesh_quantization`, and
`EXT_meshopt_compression` decode without CGo; `EXT_texture_webp`,
`EXT_mesh_gpu_instancing`, `KHR_materials_variants`, and
`KHR_materials_specular` work across WebGPU and software renderers while
sharing `*SharedGeometry` GPU buffers; `(*SceneAsset).WithLOD`,
`CombineAssetLOD`, `Scene.SetLODBias`, and `Scene.SetLODHysteresis` scale
thousands of multi-tier props in `O(1)` per frame; and `task meshes` and
`task gogpu` inspect colorways and LOD tiers interactively out of the box.

### Pure-Go glTF and OGC 3D Tiles 1.1 codecs

- Add pure-Go, WASM-safe `KHR_texture_basisu` (KTX 2.0) decoding to
  `nt3d/mesh/gltf` (accepted in `extensionsRequired`, with zero new module
  dependencies):
  - Decodes `KHR_DF_MODEL_ETC1S` (`BasisLZ` global DPCM endpoint and raw/XOR-DPCM
    selector codebooks, canonical LSB-first Huffman + VLC + RLE +
    `approx_move_to_front` selector history slices, `2×2` spatial endpoint
    predictors, single-slice `RGB`/`RRR`, and dual-slice `RGBA` / `RRR+GGG`),
    `KHR_DF_MODEL_UASTC` across all 19 4×4 LDR block modes (`0..18`, including
    BISE trit/quint endpoints, 1/2/3-subset ASTC partitions, and dual-plane
    weights), and uncompressed 8-bit `VkFormat` payloads with `None`, Zstandard,
    or zlib supercompression bounded by `Options.MaxBytes`.
  - Automatically reconstructs unit tangent `+Z` on two-channel (`RRRG`,
    `RRR+GGG`, `R8G8`) normal maps before `NewNormalMap`, and resolves texture
    sources in priority order `KHR_texture_basisu` → `EXT_texture_webp` →
    `Texture.Source` with clean fallback when external side files are absent.
- Add pure-Go, WASM-safe `KHR_mesh_quantization` and `EXT_meshopt_compression`
  support to `nt3d/mesh/gltf` (accepted in `extensionsRequired`):
  - `KHR_mesh_quantization` decodes quantized `BYTE`, `UNSIGNED_BYTE`, `SHORT`,
    and `UNSIGNED_SHORT` `POSITION` and `TEXCOORD_n` accessors, normalized `BYTE` /
    `SHORT` `NORMAL` and `TANGENT` frames, and quantized morph target deltas
    (`POSITION`, `NORMAL`, `TANGENT`).
  - `EXT_meshopt_compression` decompresses `ATTRIBUTES`, `TRIANGLES`, and
    `INDICES` `bufferView` streams with `OCTAHEDRAL`, `QUATERNION`, and
    `EXPONENTIAL` post-filters and dummy `fallback` buffers within
    `Options.MaxBytes`, plus normalized integer `rotation` and `weights`
    animation sampler outputs.
- Add `EXT_texture_webp` and `EXT_mesh_gpu_instancing` support to
  `nt3d/mesh/gltf` (accepted in `extensionsRequired`), expanding per-instance
  `TRANSLATION`, `ROTATION` (`FLOAT` and normalized `BYTE` / `SHORT`
  quaternions), and `SCALE` accessors in both `Model.Objects` and `Model.Asset`
  within triangle, visit, and byte budgets.

### Material colorways, dielectric specular, and shared asset meshes

- Add glTF `KHR_materials_variants` support (`nt3d/mesh/gltf`, accepted in
  `extensionsRequired`), per-instance `Material.BaseColor`, and
  format-independent colorway switching (`SceneAsset.Variants`,
  `AssetNode.VariantMaterials`, `AnimatedInstance.Variants`,
  `nt3dmesh.Model.Variants`, `Scene.MaterialVariants`, `Scene.MaterialVariant`,
  and `Scene.SetMaterialVariant`) so multi-colorway props and vehicles switch
  materials per instantiated subtree while preserving `SharedGeometry` GPU
  vertex buffers and BVH pick indexes.
- Add `KHR_materials_specular` support (`nt3d/mesh/gltf`, accepted in
  `extensionsRequired`, leaving `ignoredExtensions` empty) and per-instance
  `Material.SpecularColor` and `Material.SpecularIntensity` across smooth PBR
  and `PBR` + `Toon` on both WebGPU and software renderers, packing
  `sqrt`-quantized dielectric `F0` RGB and grazing `F90` into existing GPU
  instance uniform slots without increasing `instanceRecordSize`.
- Add `AssetNode.Shared` (`*SharedGeometry`) and `SharedGeometry.Geometry` so
  `SceneAsset.Instantiate` attaches shared triangle meshes via
  `Scene.SetNodeShared`. Update `nt3d/mesh/gltf` to deduplicate undeformed
  static triangle primitives in `Model.Asset` into cached `*SharedGeometry`
  handles (including cached reflected handles for static mirrored nodes),
  sharing one GPU upload, instanced draw batch, and BVH pick index across
  repeated mesh nodes in a `.glb` and repeated `Model.AddTo` /
  `SceneAsset.Instantiate` placements in a scene.

### Multi-LOD compilation, distance scaling, and viewer inspection

- Add multi-LOD asset compilation and `Scene.SetLOD` template support:
  `AssetNode.LOD`, `AssetNode.LODBounds`, and `AssetNode.LODVariantMaterials`
  let `SceneAsset.Instantiate` bind shared `LODLevel` chains directly with
  synchronized multi-part AABB switching and per-tier `Scene.SetMaterialVariant`
  colorway updates; `(*SceneAsset).WithLOD`, `CombineAssetLOD`,
  `(*SceneAsset).LODTiers`, `(*SceneAsset).Bounds`, `(*SceneAsset).Triangles`,
  `nt3dmesh.Model.LODAsset`, `Model.AddLODTo`, `Model.LODTiers`,
  `nt3dmesh.CombineLOD`, and `nt3dmesh.AddLODModels` compile sibling `*_LOD0..N`
  `.glb` subtrees (including kitbash packs, nested containers, and
  showcase-offset previews) or separate per-tier files into instanced
  `SharedGeometry` LOD assets.
- Add `Scene.SetLODBias` / `LODBias` for `O(1)` scene-wide distance scaling
  across `Scene.SetLOD` tiers (including finite hide-beyond cutoffs) without
  rebuilding node geometries or invalidating GPU/BVH caches,
  `Scene.SetLODHysteresis` / `LODHysteresis` (`[0, 0.5]`) for deterministic
  per-node deadband anti-popping across WebGPU and software renderers, and
  `FrameStats.LODTierInstances` (`[8]int`, folding tiers `>= 7` into index `7`).
- Add interactive `KHR_materials_variants` and multi-LOD `.glb` inspection to
  `task meshes` (`v` colorway cycle, `l` `LOD auto` vs raw hierarchy toggle,
  `[`/`]` `SetLODBias` steps) and `task gogpu` (`V`/`Shift+V` colorways,
  `D`/`-`/`=`/`0` LOD controls, and live `LODTierInstances` HUD telemetry), and
  extend `forest` (`[`/`]` bias steps, `b` 5% hysteresis band).

### Fuzzing, dependencies, and validation

- Add `FuzzDecodeKTX2` and `FuzzDecodeMeshopt` in `nt3d/mesh/gltf` and
  `FuzzSceneAssetLOD` in root `nt3d` (registered in `Taskfile.yml`
  `FUZZ_TARGETS`), and expand `mesh/gltf:FuzzDecode`'s oracle to instantiate
  `SceneAsset`, cycle material variants, compile multi-LOD assets, and validate
  `Scene.Frame` outputs.
- Bump the nominal `nimbleterminal.dev/nt3d` requirement in `examples/demos` and
  `examples/gogpu` to `v0.10.0` while adding zero new external module
  dependencies (retaining `wgpu v0.34.5`, `naga v0.20.4`, `gputypes v0.8.2`, and
  `gogpu v0.54.0` for Apple Metal compatibility after verifying `wgpu v0.34.15`
  with `naga v0.22.4` still fails Metal environment pipeline creation).
- Full CI (`task ci`), fuzz smoke (`task fuzz-smoke` across all 17 targets),
  site/guide builds (`task build-site`), and Apple Metal hardware GPU suites
  (`task test-gpu`) pass across all library packages and 20 gallery demos. Linux
  Vulkan GPU validation remains open per [ROADMAP.md](ROADMAP.md).

----

## v0.10.0 (2026-11-15)

This release upgrades `nt3d`'s shading, lighting, camera, and transparency
pipeline on both WebGPU and software: Cook-Torrance GGX PBR with Clearcoat and
Sheen lobes, Radiance `.hdr` environments and roughness-filtered specular IBL,
spotlights and glTF punctual lights, Weighted Blended Order-Independent
Transparency, 4× WebGPU MSAA, HDR post-processing, data-driven vertex/material
shader hooks, in-place dynamic mesh updates, distance-tiered LOD, declarative
`float64` camera paths (`nt3d/cam` and `nt3d/cam/spec`) with jitter-free
camera-relative world origins, and softened toon street shadows with crease ink
on deforming and see-through meshes. It also moves `water` and `fur` into
dedicated subpackages.

### Breaking changes

- **Move water into `nt3d/water`:** Gerstner wave surfaces, wake impulses, tiled
  water meshes, and Archimedes hull buoyancy, drag, and planing evaluation move
  out of the root `nt3d` package into `nimbleterminal.dev/nt3d/water`
  (`package nt3dwater`), renaming `WaterSurface`, `WaterBodyState`, and
  `WaterForces` to `Surface`, `BodyState`, and `Forces`.
- **Move shell fur into `nt3d/fur`:** shell-fur and silhouette-fin builders move
  out of the root `nt3d` package into `nimbleterminal.dev/nt3d/fur`
  (`package nt3dfur`) as `Config`, `Coat`, and `New`. Expose `Texture.NRGBAAt`
  and `Texture.Sample` for straight-alpha texel reads and bilinear UV sampling.

### PBR, Clearcoat, Sheen, and HDR image-based lighting

- Add Cook-Torrance GGX metallic/roughness PBR, packed linear ORM maps,
  roughness-filtered specular IBL, and HDR emissive materials to `Material` on
  both WebGPU and software renderers (`PBR`, `Metallic`, `Roughness`, `IOR`,
  `ORMMap`, `ORMRepeat`, `OcclusionStrength`, `Emissive`, `EmissiveIntensity`,
  `EmissiveMap`, `EmissiveRepeat`, `NewORMMap`, `NewShaderORMMap`,
  `NewShaderORMMapWithParameters`, `Scene.SetORMMap`, and
  `Scene.SetEmissiveMap`).
- Add `Material.Clearcoat`, `Material.ClearcoatRoughness`,
  `Material.SheenColor`, and `Material.SheenRoughness` to both smooth PBR and
  `PBR` + `Toon` across WebGPU and software renderers. Clearcoat adds an
  energy-conserving dielectric top coat (`IOR = 1.5`, `F0 = 0.04`) along the
  unperturbed geometric surface normal with independent prefiltered GGX
  environment IBL, direct GGX specular, and quantized cel lacquer highlights
  over normal-mapped or rough bases; Sheen adds an Estevez & Kulla Charlie
  microfiber grazing/retroreflective lobe with Neubelt visibility,
  directional-albedo base attenuation, ambient cloth sheen, and quantized cel
  cloth bands.
- Update `nt3d/mesh/gltf` to decode glTF 2.0 PBR metallic/roughness, merged
  occlusion/metallic-roughness ORM maps, emissive textures, and
  `KHR_materials_clearcoat`, `KHR_materials_sheen`,
  `KHR_materials_emissive_strength`, `KHR_materials_ior`,
  `KHR_materials_transmission`, and `KHR_materials_volume` (accepted in
  `extensionsRequired`).
- Add `NewHDREnvironment`, a zero-dependency Radiance RGBE (`.hdr` / `.pic`)
  panorama decoder feeding unclamped `RGBA16Float` radiance into GGX specular
  IBL, diffuse SH, HDR skies, and `Bloom`, plus `Material.EnvironmentReflect`
  so reflective (`Reflectivity > 0`) and `PBR` materials can sample
  `Frame.Environment` reflections when `Frame.EnvironmentBackgroundOnly` keeps
  diffuse environment SH lighting off for the rest of the scene.

### Lighting, OIT, MSAA, and post-processing

- Add `SpotLight` (`InnerCone`, `OuterCone`, and single-face perspective shadow
  maps on GPU and software), scene-graph punctual light nodes
  (`Scene.SetNodeLight`, `Lights`, `NodeLight`, `SceneLights`), and glTF
  `KHR_lights_punctual` decoding in `nt3d/mesh/gltf`. Add `RotationX` and
  `RotationY` to `nt3d/math`.
- Add McGuire & Bavoil Weighted Blended Order-Independent Transparency
  (`Frame.OIT` and `nt3dwidget` `WithOIT` / `SetOIT` / `OIT`), compatible with
  1× and 4× WebGPU MSAA and software float accumulation.
- Add `Frame.MSAA` (4× WebGPU multisample anti-aliasing across scene passes,
  transmission captures, and `GPUSession.Pick` depth reads, shared across
  `GPUSession.NewView`) and `Frame.PostProcess` (`SSAO`, HDR `Bloom`,
  `Exposure`, `ReinhardToneMap` / `ACESToneMap`, and `Vignette`) on GPU with
  tiered CPU software fallbacks and `nt3dwidget` `WithMSAA` / `SetMSAA` and
  `WithEffects` / `SetEffects`.
- Showcase PBR, ORM/emissive, `coat+sheen`, `HDR IBL`, `spotlights`, MSAA, and
  post-processing in `lights`, intersecting-glass OIT (`m` and `t`) in `shapes`,
  an animated `KHR_lights_punctual` **Spotlight stage** scene in `animate`, and
  matching controls in `fur`, `mipmaps`, and `examples/gogpu`.

### Shader hooks, dynamic meshes, and level of detail

- Add data-driven shader hooks with pure-Go fallbacks for vertex displacement
  and per-fragment material color: `VertexDisplacement` (`NewVertexDisplacement`,
  `NewVertexDisplacementWithParameters`, `WithParameters`, `WithMaxDisplacement`,
  `Geometry.Displacement`, `Scene.SetDisplacement`) and `MaterialShader`
  (`NewMaterialShader`, `NewMaterialShaderWithParameters`, `WithParameters`,
  `Material.Shader`, `Scene.SetMaterialShader`, `MaxShaderParameters = 16`).
  WebGPU compiles lazily cached hook pipelines shared across
  `GPUSession.NewView` and updates `[16][4]float32` uniform blocks via
  `WriteBuffer` without mesh buffer re-uploads while preserving `SharedGeometry`
  instancing; software rendering, directional/point/spot shadow maps (including
  procedural `Cutout` shadows), silhouette outlines, wireframe, and `Scene.Pick`
  / `GPUSession.Pick` evaluate the Go fallbacks in parity. Wire live wave
  displacement and crest material shader hooks (`w`) into `procedural` and
  `terrain`.
- Add `Scene.SetNodeVertices`, `Scene.UpdateNodeVertices`,
  `Scene.SetNodeVerticesWithTangents`, and `(*nt3dwater.WaveTile).Apply` for
  in-place fixed-topology mesh updates that preserve indices, UVs, and
  materials, skip redundant per-frame `O(V + I)` validation and smooth-grid
  outline-normal maps, and overwrite existing WebGPU vertex buffers via
  `WriteBuffer` without buffer reallocations. Add `Scene.SetMaterial` to replace
  or clear materials atomically across indexed meshes in a subtree while
  preserving GPU mesh buffers when vertex-baked fields are unchanged. Wire these
  into `water`.
- Add `Scene.SetLOD` and `LODLevel` for multi-tier distance-based level of
  detail with shared GPU buffers and BVH pick indexes across instances,
  per-tier outline overrides, and optional far-distance cutoffs, plus
  `Frame.Stats` / `FrameStats` (`forest` `l` toggle) and `Camera.Forward` /
  `Camera.Basis`.

### Stylized toon shading and crease lines

- Add `ToonStyle.ShadowSoftness` (`0..1`) with continuous separable tent PCF
  (`sinc²` frequency response across `1.5..4.0` shadow-map texels on
  directional, spot, and point shadow maps) and decoupled cast-shadow band
  transitions so large toon-lit scenes smooth shadow-texel staircases across
  streets without blurring `N·L` cel bands on curved characters or creating
  mid-band contour rings.
- Extend `OutlineStyle.Crease` to deforming (skinned, morphed, in-place updated,
  and single- or instanced-`VertexDisplacement`) and see-through (`Cutout`,
  `Transmission`, sorted `Transparent`, and Weighted Blended `Frame.OIT`) meshes
  across both WebGPU and software renderers, with in-place `WriteBuffer` crease
  reuse on GPU.
- Expand `toon` with coated rain-slick asphalt, soft street shadows (`s`), a
  refractive/translucent inked transit shelter, and a breeze-animated skinned
  cloth-sheen pennant (`a`), plus crease linework on transparent/OIT glass in
  `shapes` and deforming rigs in `animate` and `characters`.

### Declarative camera paths and float64 world origins

- Add zero-dependency pure-`float64` camera path packages (`nt3d/cam`,
  `nt3d/cam/ease`, `nt3d/cam/curve`, `nt3d/cam/frames`, and `nt3d/cam/spec`
  with JSON Schema `ntcamera.schema.json`): precompiled immutable `Path` values
  evaluate `CameraState` (`Pos`, `Rot`, `FovY`, `OrthoScale`, `FocusDist`,
  `Aperture`, `Near`, `Far`) in ~0.2 µs with zero heap allocations at any
  `time.Duration` (`Eval` / `EvalWith` / `EvalOverrides`, `Blend`, `Switch`,
  `FitRect`), supporting centripetal Catmull-Rom, cubic Bézier, clamped/periodic
  B-splines, Gauss-Legendre arc-length parameterization, `C¹` `Squad` and `Slerp`
  orientation, Hanson & Ma Bishop parallel-transport tangent steering, causal
  look crossfades (`target`, `tangent`, `orient`, `path`), banking `roll`,
  keyframed `fov` / `focal_mm` / `ortho_scale` / depth-of-field hints, timeline
  markers, and YAML 1.2 + JSON parsing and round-trip marshaling.
- Add `Camera.Origin`, `Camera.WorldEye`, `Frame.Origin`, `Scene.SetNodeOrigin`,
  `Scene.NodeOrigin`, `Scene.NodeWorldPosition`, and `Hit.WorldPosition` so
  scenes, `nt3dwidget` viewports, and cameras placed at geographic or
  astronomical `float64` coordinates subtract `Camera.Origin` in `float64`
  before constructing `float32` render, shadow, lighting, and picking
  transforms, eliminating `float32` vertex and depth jitter far from `(0, 0, 0)`
  with zero overhead when origins are unset.
- Add `CameraFromState`, `StateFromCamera`, `SceneResolver` (resolving
  `"node:<name>"` world positions and `"path:<name>"` trajectories),
  `Frame.DynamicLineWidth`, and `AppendCameraPathOverlay`
  (`CameraPathOverlayOptions` / `DefaultCameraPathOverlayOptions`, including
  configurable pixel `LineWidth`) for 3D spline polylines, control points,
  Bézier tangents, marker ticks, target rays, and live camera frustum pyramids.
- Add the `campath` demo (`task campath`, browser gallery #20, and
  `examples/gogpu`) showcasing a banked canyon flythrough, a closed orbit with
  focal-length pull, causal camera switching, bidirectional scrubbing, and an
  interactive director camera with live 3D frustum and spline overlays.

### Reliability, dependencies, and documentation

- Add `FuzzNewHDREnvironment` for Radiance `.hdr` decoding with pre-allocation
  scanline bounds checks and `FuzzParse` for `nt3d/cam/spec` YAML/JSON round-trip
  validation, and include `examples/gogpu` in `task ci` builds, race tests, and
  `go vet`.
- Document remaining exported symbols across all library packages, and separate
  file copyright headers from `package` clauses so `go doc` and `pkg.go.dev`
  render clean package overviews.
- Split the `examples/gogpu` desktop viewer into focused source files,
  consolidate benchmark artifacts under `docs/performance/`, add
  `CODE_OF_CONDUCT.md`, and refresh `README.md` and the Hugo guide.
- Update Lip Gloss to v2.1.1 across the library and both example modules
  (`ntpicture v0.2.0`, `ntgpuimage v0.6.0`, `ntcanvas v0.1.4`, and
  `booba v0.13.0` remain at their latest releases).
- Retain `wgpu v0.34.5`, `naga v0.20.4`, `gputypes v0.8.2`, and `gogpu v0.54.0`
  for Metal compatibility: tested `wgpu v0.34.15` with `naga v0.21.12` and
  `v0.22.1` still fails Metal environment/reflection pipeline creation, and
  `gogpu v0.54.1` requires that newer GPU stack.

### Validation and known limitations

- Full CI (`task ci`), fuzz smoke (`task fuzz-smoke`), Hugo documentation builds
  (`task build-site`), and all Apple Metal hardware GPU suites (`task test-gpu`)
  pass across all library packages and 20 gallery demos.
- **Linux Vulkan GPU validation remains open** per [ROADMAP.md](ROADMAP.md)
  (NVIDIA GB10 rendering, particle-sort, clustered-LOD, and `water` device-loss
  follow-up). `v0.9.0`'s toon cast-shadow stair-stepping and rigid-only crease
  line limitations are resolved in `v0.10.0` via `ToonStyle.ShadowSoftness` and
  deforming/see-through crease support.

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
