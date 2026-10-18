# nt3d roadmap

What is open, roughly in priority order within each section. Finished items
move to Done with their commit; git history has the details.

## Next

- [ ] **gloss migration onto `nt3dmesh`** — in progress in gloss. nt3d
  supports it as needed: `Model.Keep`, `RenderSoftware`, and the 3MF
  `Model.Summary` so far; a helper
  turning `RenderSoftware` depth into view distance, if its outlines need one.

## Rendering

- [ ] **Particles**:
  - a cheap per-frame update of one geometry's points (no full
    validate, copy, and bounds pass per frame)
  - textured sprites and atlas frames, building on sprite points
- [ ] **Shared points, lines, and boxes**, only if something needs them
  (particles don't: they are many positions, not many copies): charts
  keep all their markers in one geometry, already one upload and draw,
  and sharing them means instancing shaders that index primitives by
  instance today.
- [ ] **Mipmap follow-ups**: procedural textures (needs `ntgpuimage`
  render-target mips); coverage-preserving alpha mips for cutouts (fur and
  leaves sample full size today).
- [ ] **Outline or toon edges** on the GPU and software renderers, for
  readability at terminal sizes.
- [ ] **Point-light shadows**.
- [ ] **A floor option for drop shadows**, instead of the bottom of each instance's world bounds.
- [ ] **Order-independent transparency**: only if scenes hit the sorting
  limits (intersecting glass, overlapping layers of one mesh).
- [ ] **Fur fins don't sway** with the wind; only slightly visible.

## Formats

- [ ] **glTF follow-ups**:
  - [x] Done: generic clips, playback/blending, morphs, CPU/GPU skinning, and glTF mapping
  - cameras
  - `KHR_texture_transform`
  - [x] Done: animated node hierarchy through reusable `SceneAsset` instances
  - [x] Done: generated CC0 glTF animation gallery demo
- [ ] **Exact 3MF painting**: painted faces are approximated per face today;
  exact rendering means subdividing them.

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

- [ ] **`.gitignore` root snapshots** (such as a stray `out.png`).
- [ ] **Report the shader translator bug upstream**: WGSL functions named
  `texture…` are miscompiled (an empty body, and the call turned into a
  texture constructor); nt3d avoids the name.

## Done

- [x] **World-anchored labels and billboards** — textured, depth-tested scene
  geometry with pixel/world sizes, fixed or camera-facing orientation, shared
  instances, picking, and terminal text fallback. Terrain gallery annotations.

Recent milestones, newest first:

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
