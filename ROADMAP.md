# nt3d roadmap

What is open, roughly in priority order within each section. Finished items
move to Done with their commit; git history has the details.

## Next

- [ ] **Release the API changes, then move the applications onto them.**
  Everything under Unreleased in `CHANGELOG.md` is breaking: `Frame.Draws`,
  plain `Geometry`, degrees, node addressing, camera validation, and the
  grouped `Material`. newlock.city and nebula build against a tagged release,
  so they follow the tag.

## Rendering

- [ ] **Linux Vulkan GPU validation**: on dwarfspark (Linux ARM64, NVIDIA GB10,
  driver 580.178.04), wgpu v0.34.5/naga v0.20.4 still fail image comparisons
  for environments, textured/normal-mapped geometry, sprites, and Gaussian
  splats after fixing pipeline-before-bind-group ordering. Particle sorting
  and clustered LOD comparisons also fail; the water GPU demo reports
  `vkQueueSubmit` error -4 (device lost). Reproduce with NVIDIA selected via
  `VK_DRIVER_FILES=/usr/share/vulkan/icd.d/nvidia_icd.json` and run the root,
  `clod`, and demo GPU suites separately (`task test-gpu` stops at the first
  failing package). Ordinary Linux CI and the Metal GPU suites pass.

- [ ] **Clustered LOD follow-ups**: draw points straight from `nt3dclod.GPU`'s
  item and indirect buffers; hierarchical levels that draw an interior node
  as one aggregate (a preprocessing tool's job); per-frame rebuilds cheap
  enough for moving particles.
- [ ] **Gaussian splatting follow-ups**: streaming and partial loading. Measured and
  dropped (2026-10-26): storing points in cluster order so budgeted gathers
  are contiguous saved only 0.5 ms of the budgeted 2.5M frame's 12 ms
  (against 7 for 1M unbudgeted); the gap is the draw pass gathering by
  sorted index from a 2.5× larger buffer, which only compacting the
  selection into a dense per-frame buffer would address.

- [ ] **Shared points, lines, and boxes**, only if something needs them
  (particles don't: they are many positions, not many copies): charts
  keep all their markers in one geometry, already one upload and draw,
  and sharing them means instancing shaders that index primitives by
  instance today.
- [ ] **Fur fins don't sway** with the wind; only slightly visible.

## Formats

- [ ] **Stream 3D Tiles into a scene.** `nt3d/geo`, `nt3d/tiles3d` and
  `nt3d/tiles3d/scene` read, place and stream a tileset. What remains:
  1. Follow the SPZ compression draft (KhronosGroup/glTF pull 2531) to
     ratification. `nt3d/mesh/gltf` reads it under its three names and
     takes the payload's values as glTF's coordinates, unflipped, as
     CesiumJS does; the draft's text says left-up-front, which negates X
     and Z. `spzSplats` is the one place to change.
  2. Blended splats from several tiles: `blendPlan` orders slot by slot
     within a depth slice and spreads 64 slices from the nearest to the
     farthest blended draw, so overlapping tiles seam and one distant tile
     coarsens the slices near the camera. Measured by
     `TestBlendOrderAcrossOverlappingSplatTiles`: two overlapping tiles 20
     to 50 m off are blended up to 0.9 depth units out of order, and up to
     12 with one blended draw 10 km behind. Finer slices were tried and
     dropped (2026-11-27): 1024 ratio-spaced slices bring those to 0.15
     and 0.34, but each slice a cloud spans is a draw at about 7 µs, and
     a frame of two tiles went from 2 ms to 14.6. The fix is to give every
     Gaussian cloud one shared slot, so they sort splat by splat, and to
     resolve each selected splat once a frame in a compute pass (clip
     position, screen covariance, final color with SH evaluated, fog
     distance) into one buffer the splat shader draws from: at most 64
     draws for all splats. It costs about 48 bytes a selected splat a
     frame and uses all 8 storage bindings of the compute stage.
  3. Tile metadata inside glTF (`EXT_structural_metadata`,
     `EXT_mesh_features`).
- [ ] **Exact 3MF painting**: painted faces are approximated per face today;
  exact rendering means subdividing them.
- [ ] **Double-sided glTF materials without copying faces**: `nt3d/mesh/gltf`
  adds every triangle again facing the other way for `doubleSided` materials.
  Blender exports most materials double-sided by default, so a whole imported
  kit doubles in triangles, memory and vertex work, and the doubled faces count
  against the loader's face budget. A per-material cull-none pipeline (GPU) and
  no-cull rasterisation (software) would halve that. `Model.Triangles()` should
  probably report authored as well as drawn triangles.

## Assets and licensing

- [ ] **License-aware assets**: per-asset provenance and usage policy (source,
  license, "may be shown to AI", redistributable), carried from asset import
  through the scene. Captures for an AI or other restricted consumers can
  then substitute stand-ins ("downgrade" to proxies such as bounds boxes,
  placeholder meshes or a neutral material) or omit the asset. Editors and
  exporters can refuse to export restricted assets, and web builds can pack
  rather than serve raw files. Prompt:
  `docs/prompts/2026-11-16-license-aware-assets.md`.

## Robustness

- [ ] **Check glTF decoding against files from other encoders.** The KTX2
  and meshopt tests read fixtures nt3d's own test code wrote, so a shared
  misreading of the formats would pass. Fixtures from `gltfpack` or
  `toktx` would settle it.
- [ ] **A strength faded to zero comes back at full.** A zero `Intensity` or
  `Strength` means 1, so code that animates one down to exactly zero gets
  the default on the last step. `Light.Off`, `Ambient.Off`,
  `ReflectionProbe.Off`, `PBR.NoOcclusion`, and `PBR.Specular.Off` say none,
  but the caller has to set them; `Material.BaseColor` has the same shape
  for transparent black.
- [ ] **Test a real GPU running out of memory**; failure reporting is only
  tested with a fake renderer.
## Web demos

- [ ] **Request the adapter's storage limits in ntgpuimage** (an option on
  `ntgpuimage.Options`, and a `Limits()` accessor), so a browser build can
  raise `nt3d.SetBindingLimit` past the 128 MiB a default device gets; the
  native backend already hands out the adapter's full limits.
- [ ] **The touch-control bar's wasm cost**: about 640 KB per demo, from
  `bubbles/help` and booba's help publishing.
- [ ] **The action list's 2048-byte limit**: the terrain scene has room for
  about three more buttons; less-used keys could move behind More.

## Housekeeping

- [ ] **Per-view copies of the GPU pipelines are kept in step by hand.**
  Each `gpuRenderer` copies the shared pipelines so it can read them without
  the lock; a field added to one and not the other is caught by nothing.
- [ ] **Older per-node scene methods lack the `SetNode` prefix**
  (`SetTransform`, `SetVisible`, `SetLOD`, `SetDeformation`,
  `SetMorphWeights`). They take a `NodeID` like the rest; only the name
  differs from the convention.
- [ ] **Re-record the cloud poster** with `task tapes -- cloud`; `web/assets/cloud.png`
  is a `--snapshot` of the demo for now.
- [ ] **Report the dropped write mask upstream**: `gogpu/wgpu`'s browser
  layer sets a color target's `writeMask` only when it is above zero, so a
  mask of none becomes the browser's default of all. nt3d's cutout shadow
  shader writes its scratch target to avoid needing one.
- [ ] **Report the shader translator bug upstream**: WGSL functions named
  `texture…` are miscompiled (an empty body, and the call turned into a
  texture constructor); nt3d avoids the name.

## Done

- **Split software triangle rendering.** Mesh faces and drop shadows share
  the top-left coverage rule. Surface evaluation handles texturing, normals,
  material hooks, and cutout for both the color pass and the AO depth prepass.
  Hand-built frames retain their encoded-color default through `Frame.Linear`.
- **`Material` grouped by feature, with its lighting model explicit**:
  `Shading` and `Alpha` say what kind of surface it is, and `Phong`, `PBR`,
  `Normal`, `Emission`, `Transmission`, and `Toon` are groups that are off
  while zero. Nothing switches the model by being set, and a group for
  another model is an error.
- **Wrong input is an error, not a correction**: `Camera.Validate` and
  `Material.Validate`, returned by the scene as `Geometry.Validate` is.
  `FuzzCamera` holds that `Camera.Valid` always satisfies `Validate` and that
  an accepted camera draws.
- **One way to address a node**: scene methods take a `NodeID`, a method that
  reaches a subtree is `ApplyX`, and only `Find` and the create-or-replace
  shortcuts take a name. A test holds it.
- **Degrees for every angle on a camera or light**, under names that say what
  they measure, and flags that say "none" where zero means the default.
- **No terminal UI stack in the engine**: the wireframe renderer uses
  `nimbleterminal.dev/ntcanvas/grid` (ntcanvas v0.2.0), so the root package and
  `water`, `fur`, `mesh`, `clod`, `cam`, `sh`, `aabbtree`, and `math` no longer
  link Bubble Tea, Lip Gloss, or bubblezone; only `widget` does. A test fails
  if they do again.
- **The frame contract**: nodes declare their draw order
  (`Scene.SetNodeDrawOrder`) and carry application data (`SetNodeData`), a
  still is taken without touching level-of-detail memory (`StillFrame`), and
  the scene holds its look (`SetLook`). `Frame` is one slice of `Draw`s in
  place of four index-aligned slices, and `Geometry` is plain data with no
  unexported fields: what a scene knows about a geometry rides beside it in
  the `Draw`.
- **Pure-Go `KHR_texture_basisu` (KTX 2.0) and `KHR_materials_specular` +
  `Material.PBR.Specular.Color` / `PBR.Specular.Intensity`**: `nt3d/mesh/gltf` decodes
  KTX 2.0 BasisLZ/ETC1S, 19-mode UASTC, and uncompressed `VkFormat` textures
  (with None, Zstandard, and zlib supercompression, `Options.MaxBytes`
  enforcement, two-channel normal-map `Z` reconstruction, and fallback to
  `EXT_texture_webp` / `Texture.Source`) in pure Go with zero CGo, completing
  OGC 3D Tiles 1.1 `.glb` payload parity across native and WASM builds.
  `KHR_materials_specular` moves from `ignoredExtensions` into
  `supportedExtensions` (accepted in `extensionsRequired`), mapping to
  per-instance `Material.PBR.Specular.Color` and `Material.PBR.Specular.Intensity` across
  WebGPU and software PBR renderers without growing `instanceRecordSize`.

- **Interactive `KHR_materials_variants` and multi-LOD `.glb` inspection in
  `task meshes` and `task gogpu`**: `(*SceneAsset).LODTiers`, `Bounds`,
  `Triangles`, and `(*nt3dmesh.Model).LODTiers` inspect arbitrary assets for
  compilable `*_LOD0..N` subtrees; `task meshes -- model.glb` preserves PBR
  `SceneAsset` hierarchies, auto-collapses multi-LOD packs (`l` toggles auto vs
  raw, `[`/`]` step `SetLODBias`, `v` cycles colorways), and `task gogpu` adds
  matching `V`/`Shift+V` colorway and `D`/`-`/`=`/`0` LOD controls with live
  per-tier `LODTierInstances` HUD telemetry.

- **Multi-LOD `.glb` and multi-file `Scene.SetLOD` helpers**: `AssetNode.LOD`,
  `AssetNode.LODBounds`, and `AssetNode.LODVariantMaterials` extend `SceneAsset`
  so `Instantiate` binds shared `LODLevel` chains with synchronized multi-part
  prop bounds and `Scene.SetMaterialVariant` colorway switching across tiers.
  `(*SceneAsset).WithLOD` / `nt3dmesh.Model.LODAsset` / `Model.AddLODTo` collapse
  sibling `*_LOD0..N` subtrees in single or kitbash `.glb` packs, and
  `CombineAssetLOD` / `nt3dmesh.CombineLOD` / `AddLODModels` merge separate
  per-tier files while preserving `SharedGeometry` GPU instancing and BVH pick
  indexes.

- **glTF `KHR_materials_variants`, `Material.BaseColor`, and per-instance
  `Scene.SetMaterialVariant`**: `Material.BaseColor` packs per-instance sRGB
  albedo tint and transparent opacity into existing GPU instance uniforms so
  solid/tinted colorways switch without re-encoding mesh vertices or detaching
  `SharedGeometry`. `SceneAsset.Variants`, `AssetNode.VariantMaterials`,
  `nt3dmesh.Model.Variants`, and `Scene.MaterialVariants` / `MaterialVariant` /
  `SetMaterialVariant` (with `""` reset and automatic `SharedGeometry`
  reattachment) expose named colorways across independent instances, and
  `nt3d/mesh/gltf` decodes `KHR_materials_variants` (accepted in
  `extensionsRequired`) while pre-populating UVs and tangents needed by textured
  or normal-mapped variants.

- **glTF `KHR_mesh_quantization` and pure-Go `EXT_meshopt_compression`**:
  `nt3d/mesh/gltf` supports `KHR_mesh_quantization` (`BYTE` / `UNSIGNED_BYTE` /
  `SHORT` / `UNSIGNED_SHORT` `POSITION` and `TEXCOORD_n`, normalized `BYTE` /
  `SHORT` `NORMAL` and `TANGENT`, and quantized morph `POSITION` / `NORMAL` /
  `TANGENT` deltas) and a zero-CGo, WASM-safe `EXT_meshopt_compression`
  byte-stream decoder (`ATTRIBUTES`, `TRIANGLES`, and `INDICES` modes with
  `OCTAHEDRAL`, `QUATERNION`, and `EXPONENTIAL` post-filters, dummy `fallback`
  buffers, and normalized integer `rotation` / `weights` animation tracks),
  bounded by `Options.MaxBytes` and accepted in `extensionsRequired`.

- **glTF `EXT_texture_webp`, `EXT_mesh_gpu_instancing`, and `SceneAsset`
  `SharedGeometry` deduplication**: `AssetNode.Shared` (`*SharedGeometry`) and
  `SharedGeometry.Geometry` let `SceneAsset.Instantiate` bind shared triangle
  meshes via `Scene.SetNodeShared`, and `nt3d/mesh/gltf` caches undeformed
  static triangle primitives in `Model.Asset` (plus reflected handles for static
  mirrored nodes) so multi-node kitbash `.glb` packs and repeated `Model.AddTo`
  placements across geographic scenes share one GPU upload, instanced draw
  batch, and BVH pick index. `nt3d/mesh/gltf` also registers WebP decoding
  directly and supports `EXT_texture_webp` (with fallback to `Texture.Source`
  when an external WebP side file is absent) and `EXT_mesh_gpu_instancing`
  (`TRANSLATION`, `FLOAT` / normalized `BYTE` / normalized `SHORT` `ROTATION`,
  and `SCALE` accessors) in `extensionsRequired`.

- **`Scene.SetLOD` bias, hysteresis, and per-tier `FrameStats`**:
  `Scene.SetLODBias` / `LODBias` scales every `LODLevel.MaxDistance` (including
  finite final hide-beyond cutoffs) in `O(1)` via FMA-free `float64`→`float32`
  threshold scaling without touching node geometries or GPU/BVH caches, while
  bumping a scene bias epoch so budget steps take effect immediately.
  `Scene.SetLODHysteresis` / `LODHysteresis` (`[0, 0.5]`) remembers each visible
  LOD node's selected state across `Scene.Frame` calls, requiring `> D*(1+band)`
  to step coarser (or hide) and `< D*(1-band)` to step finer (or re-show),
  resetting on first frames, `SetLOD`, `SetVisible`, and bias changes.
  `FrameStats.LODTierInstances` (`[8]int`, folding `7+` into index `7`) reports
  drawn LOD instances per tier, showcased in `forest` (`[` / `]` bias `×0.9` /
  `×1.1`, `b` 5% hysteresis band).

- **Declarative `float64` camera paths (`nt3d/cam`, `nt3d/cam/spec`) and
  camera-relative world origins**: zero-dependency pure-`float64` packages
  (`nt3d/cam`, `nt3d/cam/ease`, `nt3d/cam/curve`, `nt3d/cam/frames`, and
  `nt3d/cam/spec` with JSON Schema `ntcamera.schema.json`) precompile YAML 1.2
  and JSON `camera_path` v1 rigs into immutable `*nt3dcam.Path` trajectories
  that evaluate `CameraState` in ~0.2 µs with zero allocations (`Eval`,
  `EvalWith`, `EvalOverrides`, `Blend`, `Switch`, `FitRect`), supporting
  centripetal Catmull-Rom, cubic Bézier, clamped/periodic B-splines,
  Gauss-Legendre arc-length parameterization, `C¹` `Squad` and `Slerp`, Hanson &
  Ma Bishop parallel-transport tangent steering, causal `look` crossfades,
  banking `roll`, keyframed `fov` / `focal_mm` / `ortho_scale` / DoF hints, and
  timeline markers. `Camera.Origin` / `WorldEye`, `Frame.Origin`,
  `Scene.SetNodeOrigin` / `NodeOrigin` / `NodeWorldPosition`, and
  `Hit.WorldPosition` subtract `Camera.Origin` in `float64` before `float32`
  render, shadow, widget, and picking transforms for jitter-free geographic
  coordinates, paired with `CameraFromState`, `StateFromCamera`,
  `SceneResolver`, `Frame.DynamicLineWidth`, `AppendCameraPathOverlay`, and the
  `campath` demo.

- **Clearcoat & Sheen on `Material` + glTF `KHR_materials_clearcoat` and
  `KHR_materials_sheen`**: `Material.PBR.Clearcoat.Amount` and
  `Material.PBR.Clearcoat.Roughness` (`0..1`) add an energy-conserving dielectric
  top layer (`PBR.IOR = 1.5`, `F0 = 0.04`) evaluated along the unperturbed
  geometric surface normal with independent prefiltered GGX environment IBL,
  direct GGX specular, and quantized cel lacquer highlights in `PBR` + `Toon`,
  keeping base normal maps (car-paint flakes, wood grain, rain-wet cobblestones)
  underneath a smooth glossy coat. `Material.PBR.Sheen.Color` and
  `Material.PBR.Sheen.Roughness` (`0..1`) add an Estevez & Kulla Charlie microfiber
  grazing/retroreflective lobe with Neubelt visibility, directional-albedo base
  energy scaling, ambient cloth sheen, and quantized cel cloth bands across
  both WebGPU and software renderers while preserving `Scene.ApplyMaterial` GPU
  mesh buffers and `SharedGeometry` instancing. `nt3d/mesh/gltf` promotes
  `KHR_materials_clearcoat` and `KHR_materials_sheen` out of
  `ignoredExtensions` into `supportedExtensions`. Showcased in `lights` (`e`
  `coat+sheen` preset) and `toon` (rain-slick coated street ribbons and
  cloth-sheen articulated pennant).

- **Softened toon cast shadows (`ToonStyle.ShadowSoftness`) and crease lines on
  deforming & see-through meshes**: `ToonStyle.ShadowSoftness` (`0..1`) pairs a
  continuous separable tent PCF shadow filter (`1.5..4.0` texels, `sinc²`
  frequency response across directional, spot, and point shadow maps) with a
  decoupled cast-shadow band transition (`toonShadowFactor`), eliminating
  stair-stepped shadow-texel teeth on large city streets while keeping curved
  characters' `N·L` cel bands crisp in sunlight and avoiding mid-band contour
  rings. `OutlineStyle.Crease` now renders architectural/mechanical crease
  linework on deforming (skinned, morphed, in-place vertex-updated, and single-
  or instanced-`VertexDisplacement`) and see-through (`AlphaCutout` textures and
  `Material.Shader`, post-depth `Transmission`, sorted `AlphaTransparent`, and
  Weighted Blended `Frame.OIT` at 1× and 4× MSAA) meshes across both WebGPU and
  software renderers, reusing GPU crease buffers via `WriteBuffer`. Showcased in
  `toon` (`s` soft street shadows, refractive/translucent inked transit shelter,
  and `a` breeze-animated skinned pennant), `shapes` (`b` + `t`), `animate`, and
  `characters`.

- **Data-driven shader hooks (`VertexDisplacement` and `MaterialShader`)**:
  `Geometry.Displacement` (`NewVertexDisplacement`,
  `NewVertexDisplacementWithParameters`, `WithParameters`, `WithMaxDisplacement`,
  `Scene.ApplyDisplacement`) and `Material.Shader` (`NewMaterialShader`,
  `NewMaterialShaderWithParameters`, `WithParameters`, `Scene.ApplyMaterialShader`)
  pair immutable WGSL (`fn nt3dDisplace` / `fn nt3dMaterial` plus `Time` and up
  to `MaxShaderParameters = 16` `vec4` uniforms) with pure-Go fallbacks
  (`VertexDisplaceFunc` / `MaterialColorFunc`). WebGPU compiles lazily cached
  per-hook shader programs shared across `GPUSession.NewView`, recycles uniform
  buffers via `WriteBuffer` without mesh re-uploads, preserves `SharedGeometry`
  instancing, pads culling/shadow/picking bounds from rest bounds by
  `MaxDisplacement` without drifting `DropShadow`, and keeps software rendering,
  directional/point/spot shadow maps (including procedural `AlphaCutout` shadows),
  silhouette outlines, wireframe, and `Scene.Pick` / `GPUSession.Pick` in parity.
  `MaterialShader` evaluates in linear light with unclamped HDR output (`> 1.0`
  feeding `Bloom` and tone mapping) and procedural alpha feeding `AlphaCutout`,
  alpha blending, and `Frame.OIT`. Showcased in `procedural` / `terrain` (`w`
  toggles live wave displacement + crest material shader hooks).

- **In-place fixed-topology mesh updates, `Material.EnvironmentReflect`, and
  subtree `Scene.ApplyMaterial`**: `Scene.SetNodeVertices`,
  `Scene.UpdateNodeVertices`, `Scene.SetNodeVerticesWithTangents`, and
  `(*nt3dwater.WaveTile).Apply` update animated mesh vertices and tangents
  without re-validating or cloning indices/UVs/materials, skip `O(V + I)` frame
  re-validation via mesh certificates and smooth-grid `outlineNormals` map
  allocations, and overwrite existing WebGPU vertex buffers via `WriteBuffer`
  with reusable scratch buffers (~5.2× faster CPU scene updates and zero GPU
  buffer reallocations). `Material.EnvironmentReflect` opts classic
  `Phong.Reflectivity` and `PBR` surfaces into environment reflections while
  `Frame.EnvironmentBackgroundOnly` keeps diffuse environment SH lighting off
  for the rest of the scene. `Scene.ApplyMaterial` atomically validates and
  replaces (or clears) materials across indexed meshes in a subtree—including
  `SharedGeometry`, `LOD` tiers, and deformed meshes—preserving GPU mesh
  buffers and instanced `SharedGeometry` when vertex-baked fields are unchanged.

- **Radiance `.hdr` panoramas, `SpotLight` + `KHR_lights_punctual`, and Weighted
  Blended Order-Independent Transparency (`Frame.OIT`)**: `NewHDREnvironment`
  decodes Radiance RGBE (`.hdr` / `.pic`) panoramas (adaptive RLE and
  uncompressed, `EXPOSURE`, and `-Y/+Y`/`+X/-X` orientations) with zero
  dependencies, feeding unclamped `RGBA16Float` radiance into the 6-level GGX
  specular IBL mip chain, degree-2 SH diffuse sky, HDR sky background pass,
  `Bloom`, and `ACESToneMap` on both WebGPU and software (with 8-bit wireframe
  fallback). `SpotLight` (`InnerCone`, `OuterCone`) adds smooth quadratic cone
  attenuation and single-face 2048² perspective shadow maps alongside
  `SetNodeLight` / `NodeLight` / `Lights` / `SceneLights` and glTF
  `KHR_lights_punctual` decoding (`directional`, `point`, and `spot` lights on
  scene nodes). `Frame.OIT` (`WithOIT` / `SetOIT` on `nt3dwidget`) implements
  McGuire & Bavoil Weighted Blended OIT across `RGBA16Float` accumulation +
  `R8Unorm` revealage targets on WebGPU (compatible with both 1× and 4× MSAA and
  shared across `GPUSession.NewView`) and matching float accumulators in
  software, eliminating mesh-order popping on intersecting glass and multi-layer
  shells. Showcased in `lights` (`HDR IBL` and `spotlights` presets), `shapes` /
  `meshes` (`t` cycles `opaque` → `glass (OIT)` → `glass (sorted)`), and
  `examples/gogpu` (`-env`, `-oit`, `HDR Studio Sun`, `Spotlights`, `N` OIT).
- **Cook-Torrance GGX PBR, ORM maps, roughness-filtered IBL, HDR emissive
  materials, and glTF material extensions**: unified `Material` gains `PBR`,
  `PBR.Metallic`, `PBR.Roughness`, `PBR.IOR`, packed linear `PBR.ORMMap` (`NewORMMap`,
  `NewShaderORMMap`, `Scene.SetNodeORMMap`, `PBR.OcclusionStrength`), and HDR
  `Emission.Color` / `Emission.Intensity` / `Emission.Map` (`Scene.SetNodeEmissiveMap`) on
  both WebGPU (consolidated 8-binding material group + procedural GPU mip
  downsampling + 6-level GGX-prefiltered environment specular mip chain) and
  software, composing with `Toon` and `Transmission`. `mesh/gltf` decodes real
  PBR factors, packed/merged ORM textures, emissive maps, and
  `KHR_materials_emissive_strength`, `KHR_materials_ior`,
  `KHR_materials_transmission`, and `KHR_materials_volume`.
- **4× WebGPU MSAA and GPU post-processing pipeline**: `Frame.MSAA` enables 4×
  hardware multisampling across every WebGPU scene pass, transmission snapshot,
  and GPU depth pick (`GPUSession.NewView` shares both 1× and 4× pipeline sets,
  while software and wireframe gracefully ignore `MSAA`). `Frame.PostProcess`
  adds screen-space ambient occlusion (`SSAO` with bilateral depth blur), a
  five-level `RGBA16Float` HDR `Bloom` pyramid, `Exposure`, `ReinhardToneMap` /
  `ACESToneMap`, and `Vignette` on GPU (`Renderer` and `GPUSession`, including
  `Viewport`, `Load`, and crisp post-resolve `Overlays`), with tiered CPU
  software fallbacks (exact HDR exposure/tone mapping/vignette, quarter-res
  two-level bloom, and `Complete`-only SSAO) and wireframe no-op. Exposed in
  `nt3dwidget` (`WithMSAA` / `SetMSAA`, `WithEffects` / `SetEffects`), the
  demos (`--msaa`, `--effect`, `x` AA and `e`/`p` post presets in `fur`,
  `mipmaps`, and `lights`), and `examples/gogpu` (`-msaa`, `-post`, `4`/`M`,
  `F12`/`X`).
- **Toon shading, crease lines, and the toon demo** (`2d82e04`…): `Material.Toon`
  / `ToonStyle` and `Scene.ApplyToon`, banded diffuse and cast shadows with a
  shade tint, flat specular, and rim; `OutlineStyle.Crease` for sharp-edge
  ink on opaque rigid meshes; both in the GPU and software renderers. The
  `toon` demo shows them on Spot, the robot, and a baked Flatiron block.
- **GPUSession, GoGPU desktop viewer, world-unit & split-normal outlines, budgeted plain point clouds, and single-pass AABB raycast** (`ba2f565`…`adac237`):
  - **Host-supplied GPU rendering (`GPUSession` / `GPUTarget`)**: `NewGPUSession`,
    `NewGPUSessionFromProvider`, `Encode`, and `Render` resolve frames directly into a
    caller-owned `*wgpu.TextureView` (`RGBA8Unorm`, `RGBA8UnormSrgb`, `BGRA8Unorm`,
    `BGRA8UnormSrgb`, or `RGBA16Float`) on a caller-owned `*wgpu.Device` and
    `*wgpu.CommandEncoder` without CPU readback, with sub-rectangle resolve
    (`GPUTarget.Viewport`) and coverage-preserving alpha composition (`GPUTarget.Load`).
  - **Shared GPU resources (`NewView` & `ntgpuimage` `v0.6.0`)**: `(*GPUSession).NewView()`
    shares compiled pipelines, bind group layouts, shaders, and ref-counted geometry,
    Gaussian splat, procedural/image texture (`ntgpuimage.NewFromDevice`), and
    environment/reflection panorama (`Texture.View()` / `Texture.Sampler()`) caches
    across viewports and windows while isolating per-view uniforms, transforms, sort
    buffers, and targets. `Pick` / `ReadDepth` read the session's hardware depth buffer
    to accelerate picking while warming static BVH indexes in the background, and
    `RenderToImage` / `RenderToImageInto` capture offscreen `*image.NRGBA` frames on the
    session's device reusing its caches (`ba2f565`…`1452f77`).
  - **Desktop GoGPU application (`examples/gogpu`)**: `task gogpu` runs all 18 gallery
    demos (`-demo <name>`, `[` / `]`, interactive HUD dropdown menu `M`, and demo
    shortcuts) plus a 3D model and Gaussian splat viewer (`-file <path>` and window
    drag-and-drop) with GPU-depth picking, 3D callout annotations, a 3D transform
    gizmo (`G`: `Translate` -> `Rotate` -> `Scale` -> `Off` with per-axis and uniform
    handles), an interactive top-right 3D Camera Orientation Triad (`+X`/`-X`/`+Y`/`-Y`/`+Z`/`-Z`),
    lighting/environment/shadow controls (`1`–`5`, `E`, `K`), PNG screenshot (`P`) and
    animated GIF turntable capture (`T`), picture-in-picture sub-viewport (`V`), `Load`
    compositing (`L`), supersampling (`S`), and multi-window GPU sharing (`2`).
  - **Outlines, budgeted point clouds, and AABB raycast** (`adac237`):
    `OutlineStyle.Units` (`WorldUnits`) scales outlines with distance, and vertices
    sharing a position with split normals expand along their 18-bit position-averaged
    contour normal on both GPU and software. Clustered plain alpha-blended point clouds
    (`HardPoint`, `SoftPoint`, sprites) participate in `Frame.SplatBudget`, and
    `nt3daabbtree.Tree.RayCast` slab-tests each leaf once.
- **`v0.7.0`** (`0293cb9`…`9e2c4b6`): `SetBindingLimit` / `AdoptBindingLimit` up to
  1 GiB, `nt3d-palette` and `nt3dsog.Encode` for SOG v2 SH-palette bundles, and
  unclamped software transparent blending headroom matching the GPU's `RGBA16Float`
  target.

- **`v0.6.0`** (`f44ada1`…`e2664cf`): spherical-harmonic palettes below.
- **SH palettes**: a Gaussian geometry keeps its spherical-harmonic bands
  above DC as a palette of up to 65,536 centroids shared by pointer
  (`Geometry.GaussianSHPalette`) with a 16-bit label a splat
  (`GaussianSHLabels`), DC staying per splat, so a million degree-3 splats
  take about 49 MB plus at most 2.9 MB of palette instead of 92. Both
  renderers resolve the palette, inside the two existing SH buffers.
  `nt3dmesh.BuildSHPalette` builds one by deterministic two-level k-means
  (a million splats in about 2.5 s on an M5 Max), at load through
  `Options.SHPalette` (`Keep` by default, which keeps a SOG's own palette;
  `Build`; `Expand` for the old per-splat shape) or in the background with
  a swap, as the meshes demo's `w` shows; `ExpandSHPalette` inverts
  (`f44ada1`…`e2664cf`).
- **`v0.5.0`** (`2bfe699`…`6ddfaf3`): budgeted splat level of detail below.
- **Budgeted splat LOD**: every splat decoder clusters its clouds at load
  (`Geometry.Clusters`; `nt3dmesh.Options.NoClusters` opts out), and both
  renderers draw a per-frame subset under `Frame.SplatBudget` (default one
  million): each visible cluster gets a prefix of its spread order sized by
  its projected area, survivors scale by 1/√f to keep coverage, the GPU keys
  and sorts only the selection, and `Frame.SplatSelection()` reports drawn
  and resident counts. Shares are continuous, so the clustered-LOD items on
  the renderer consuming selections directly and on flicker between levels
  are closed. A store-time validation certificate stops `Render`
  re-validating stored geometry each frame (a 1M-splat frame 28→7 ms)
  (`2bfe699`…`6ddfaf3`).
- **`v0.4.0`** (`8e1e04d`…`72c0fcc`): water, from shared Gerstner wave
  surfaces with wakes, buoyancy, drag and planing hull forces, flow maps,
  transmission, refraction and absorption, environment reflections, and soft
  particles to the water demo's driven craft; then the three splat projects
  below: SOG and compressed PLY decoding, global GPU splat sorting, and
  compact GPU splat storage.
- **Compact GPU splat storage**: Gaussian splats upload as 24-byte half-float
  axes and one byte per SH channel at SPZ's quantization (bands clamp to ±1),
  decoded in the shader, so a geometry holds 2,796,202 degree-3 splats instead
  of 699,050 and Niantic's SPZ samples open as one slab each;
  `PointStyle.Precise` keeps the float records and `MaxPreciseGaussians`
  their limit (`75fbc7c`…`8d11e63`).
- **Global GPU splat sorting**: blended points sort on the GPU every frame
  across every alpha point geometry, to within one of 64 depth slices, with
  transparent meshes and additive points interleaved by slice; the software
  renderer orders by the same plan, so overlapping clouds and the slabs of a
  big cloud blend in order (`8f49a76`…`f5bbd25`).
- **SOG and compressed PLY decoding**: PlayCanvas SOG versions 1 and 2
  (`.sog` bundles and unbundled `meta.json`) in `nt3dmesh/sog`, and
  SuperSplat compressed PLY in `DecodePLY`, into the same Gaussian geometry
  as PLY and SPZ. A shared `GaussianSH` helper assembles point-major
  coefficients for every splat decoder, and registered sniffs may claim a zip
  before it defaults to 3MF (`3336623`…`9f66b22`).
- **gloss on `nt3dmesh`**: gloss decodes STL, OBJ, PLY, and 3MF through
  `nt3dmesh` (gloss `aa49fd3`, `ef31abc`, `e21ce3f`) and draws its export
  outlines from `RenderSoftware`'s normalized depth with relative thresholds,
  so no view-distance helper was needed. nt3d's side was `Model.Keep`,
  `RenderSoftware`, and the 3MF `Model.Summary`.
- **`v0.3.1`**: adopted `ntpicture` `v0.1.1`, `ntcanvas` `v0.1.1`, and
  `go-booba` `v0.10.2` for the web demos (wheel events as SGR mouse buttons,
  OSC 52 clipboard from wasm) (`0350219`, `5a0a3b8`).
- **`v0.3.0`** (`18023bb`): the AABB tree, tangent frames, SPZ splats, cutout
  mips, mipmapped shader textures, and clustered LOD below; the widget's
  `PickMsg` carries the click's world ray (`be53a91`).
- **Dynamic AABB tree (`nt3daabbtree`)**: a generic `Tree[T]` spatial index
  with insert, remove, update, inclusive overlap and near-first ray queries,
  Box2D-style balancing, and the spatial index guide (`d419650`…`2784ca9`);
  `Ancestors`/`Walk` and the AABB tree demo of clicked-box ancestry and
  drifting updates (`6609285`…`99621f5`).
- **Tangent frames for normal maps**: authored `Geometry.Tangents` (glTF
  `TANGENT`), MikkTSpace-compatible generation at scene upload and in the
  glTF loader (per corner, splitting vertices), skinned and morphed frames
  on both renderers, the per-triangle basis as fallback (`76c4cb0`…`6c500b5`).
- **SPZ Gaussian splats**: Niantic's compressed format, versions 2 to 4, in
  `nt3dmesh/spz`; point limits from the WebGPU binding (`MaxPoints`,
  `MaxGaussians`) and big clouds split into slabs (`552bee4`…`b7febd6`).
- **Coverage-preserving alpha mips for cutouts**: a chain per cutout texture
  whose alpha is rank-matched to full size, sampled trilinearly by both
  renderers; `Material.FullSize` for comparing, `m` in the fur demo
  (`5f112e3`…`29e8691`).
- **Clustered LOD selection (`nt3dclod`)**: median-split clusters with a
  progressive item order, CPU and WebGPU compute selection (frustum culling,
  distance levels, indirect draw arguments) that agree, the point cloud demo
  of a million points, and the clustering guide (`cdc18b0`…`7ec5cbd`).

- **Shared spherical harmonics**: `nt3dsh` provides degree 0–3 real SH,
  projection, rotation, diffuse convolution, and a shared Go/WGSL basis.
  Gaussian PLY retains full view-dependent SH; image and analytic environments
  use nine-coefficient diffuse lighting. Model-viewer DC/full-SH controls and
  a rotatable SH studio lighting preset exercise both uses.

- **3D Gaussian splatting (first version)**: standard uncompressed 3DGS PLY
  with DC color, anisotropic GPU ellipses using the point pipeline and CPU radix
  sorting, matching software ellipses, wireframe centers, and an embedded gallery
  sample. See the rendering guide for count budgets and ordering limitations.

- **glTF hierarchy, cameras and texture transforms**: static and animated files
  instantiate reusable `SceneAsset` trees; node cameras expose authored perspective
  and orthographic lenses, roll, and animated poses. `KHR_texture_transform` and UV
  set overrides feed independent color/normal UVs on GPU and software. Flat rest-pose
  objects remain available. Generic animation, skins and gallery demos shipped earlier.

- **Particle updates and atlas sprites**: `SetNodePoints` / `UpdateNodePoints`
  preserve snapshots, skip unrelated validation/copies and bounds scans, and
  reuse GPU buffers with range uploads. Image sprites and per-point atlas frames
  support opaque cutouts, alpha and additive blending on GPU and software;
  wireframe uses dots. The particles demo uses the fast path and `t` toggles sprites.


- [x] **Silhouette outlines** — configurable pixel-width back-face hulls on GPU
  and software, with depth, alpha, animation, and supersampling support; `b` in
  the shapes and characters galleries.

- [x] **Normal-map rendering** — linear-data textures, matching GPU/software
  diffuse/specular/environment shading, morph/skin support through generated
  triangle bases, glTF material mapping, and the lights gallery's `n` toggle.

- [x] **Environment sky and diffuse IBL** — gradient or equirectangular image,
  shared GPU/software lighting, terminal sky fallback, and lighting gallery presets.

- [x] **World-anchored labels and billboards** — textured, depth-tested scene
  geometry with pixel/world sizes, fixed or camera-facing orientation, shared
  instances, picking, and terminal text fallback. Terrain gallery annotations.

Recent milestones, newest first:

- [x] **Point-light shadows and fixed drop-shadow floors**: six-face point maps
  on GPU/software; `DropShadow.FixedFloor` / `FloorZ` keep discs on a world plane.
  Lights demo: `s` shadows, `l` presets, `b` lift shapes.

- shadows: drop discs and directional shadow maps, both renderers; `s` in
  lights and forest (API: `b19983c`, `bf7b9ad`)

- `v0.1.0`: the first release, at `nimbleterminal.dev/nt3d`
- `749834b`…`d5a711f` sprite points: soft, sized in world units, alpha and additive blending, both renderers, 20-byte GPU records; the particles demo
- adopted `ntpicture` `v0.1.0` (stale-frame reporting, `KeepOnMainScreen`); no more sibling checkout
- lighting rig: hemisphere ambient, up to eight directional and
  point lights, per-pixel diffuse, both renderers; the lights demo
- adopted `ntgpuimage` `v0.3.0` (sRGB, mipmaps, samplers, anisotropy); no more sibling checkout
- `e1f3f0a`, `c30c9b4` anisotropic texture filtering (`Frame.Anisotropy`), both renderers; in the mipmaps demo
- `1d860e7` 3MF `Model.Summary`: bounds, area, and mesh counts past the triangle limit (for gloss)
- `6e4685a`, `cf8172c` forest demo of shared geometry; transforms check only
  the subtree they move (updating every node was quadratic: 3.3 s for 5,000)
- `83f3da2`, `cd7488b` mipmaps and linear-light texture filtering, both
  renderers; mipmaps demo (`83b4925` builds against the sibling `ntgpuimage`)
- `4fe7974` `RenderSoftware`: software pictures with their depth buffer (for gloss's outlines)
- `3a1ccc4` shared geometry and GPU instancing (`NewSharedGeometry`, `SetShared`)
- `b79bfc1` widget `Keep()`: leave the last frame on screen at exit (for gloss)
- `4cd35b4`, `2c577bc` mesh format registry; glTF in `mesh/gltf`, `mesh/formats` bundle
- `c82960e`, `768c46c` specular highlights and fog
- `bf94678` linear lighting, blending, and supersampling
- `60162d9`, `972f2a4` transparency blending; glass in the shapes demo
- `73bf4d8` picking through cutout gaps
- `e243240` per-node frustum culling
- `b84c180` frame rate and render time in the demos
- `1c75f12` touch controls (booba Terminal Actions) in the web demos
- PLY range grids and Stanford `.conf`; leaner wasm demos; fur fins and fur
  without UVs; texture repeat; supersampling and post-processing; PLY reading
