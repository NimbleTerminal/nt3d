# nt3d roadmap

What is open, roughly in priority order within each section. Finished items
move to Done with their commit; git history has the details.

## Next

- [ ] **gloss migration onto `nt3dmesh`** — in progress in gloss. nt3d
  supports it as needed: `Model.Keep`, `RenderSoftware`, and the 3MF
  `Model.Summary` so far; a helper
  turning `RenderSoftware` depth into view distance, if its outlines need one.
- [ ] **Release**:
  - move nt3d's ntpicture pin from its main-branch pseudo-version to
    `v0.1.0` once that is tagged, in both modules
  - register the `nimbleterminal.dev/nt3d` import path
  - tag a version

## Rendering

- [ ] **Shared points, lines, and boxes**, only if something needs them:
  charts keep all their markers in one geometry, already one upload and
  draw, and sharing them means instancing shaders that index primitives by
  instance today.
- [ ] **Mipmap follow-ups**: procedural textures (needs `ntgpuimage`
  render-target mips); coverage-preserving alpha mips for cutouts (fur and
  leaves sample full size today).
- [ ] **World-anchored labels and billboards** on every renderer; `CellText`
  is wireframe-only.
- [ ] **Outline or toon edges** on the GPU and software renderers, for
  readability at terminal sizes.
- [ ] **Shadows**: drop shadows first; shadow maps later.
- [ ] **Order-independent transparency**: only if scenes hit the sorting
  limits (intersecting glass, overlapping layers of one mesh).
- [ ] **Fur fins don't sway** with the wind; only slightly visible.

## Formats

- [ ] **glTF follow-ups**:
  - animation and skins
  - cameras
  - `KHR_texture_transform`
  - keeping the node hierarchy instead of flattening it
  - a glTF sample in the models demo (license permitting)
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

Recent milestones, newest first:

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
