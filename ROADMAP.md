# nt3d roadmap

What is open, roughly in priority order within each section. Finished items
move to Done with their commit; git history has the details.

## Next

Nothing queued; the sections below are the open work.

## Rendering

- [ ] **Clustered LOD follow-ups**: draw points straight from `nt3dclod.GPU`'s
  item and indirect buffers in the GPU renderer, so a selection never
  leaves the device; hierarchical levels that draw an interior node as one
  aggregate; per-frame rebuilds cheap enough for moving particles;
  hysteresis at range boundaries so clusters do not flicker between levels.
- [ ] **Gaussian splatting follow-ups**: large-scene performance (level of
  detail and budgets).

- [ ] **Shared points, lines, and boxes**, only if something needs them
  (particles don't: they are many positions, not many copies): charts
  keep all their markers in one geometry, already one upload and draw,
  and sharing them means instancing shaders that index primitives by
  instance today.
- [ ] **Outline follow-ups**: sharp-crease edges, world-unit widths, and seamless
  contours for meshes with split normals.
- [ ] **Order-independent transparency**: only if scenes hit the sorting
  limits (intersecting glass, overlapping layers of one mesh).
- [ ] **Fur fins don't sway** with the wind; only slightly visible.

## Formats

- [ ] **Exact 3MF painting**: painted faces are approximated per face today;
  exact rendering means subdividing them.

## Spatial index

- [ ] **AABB tree follow-ups**: `RayCast` slab-tests each leaf twice (once
  to order the children, again when popped); a test for negative-zero ray
  directions, which the slab test handles through `1/-0 = -Inf`.

## Robustness

- [ ] **Test a real GPU running out of memory**; failure reporting is only
  tested with a fake renderer.
- [ ] **Optionally request larger GPU limits** on capable adapters, for
  fewer mesh pieces.

## Web demos

- [ ] **The touch-control bar's wasm cost**: about 640 KB per demo, from
  `bubbles/help` and booba's help publishing.
- [ ] **The action list's 2048-byte limit**: the terrain scene has room for
  about three more buttons; less-used keys could move behind More.

## Housekeeping

- [ ] **Re-record the cloud poster** with `task tapes -- cloud`; `web/assets/cloud.png`
  is a `--snapshot` of the demo for now.
- [ ] **`.gitignore` root snapshots** (such as a stray `out.png`).
- [ ] **Report the shader translator bug upstream**: WGSL functions named
  `texture…` are miscompiled (an empty body, and the call turned into a
  texture constructor); nt3d avoids the name.

## Done

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
