# nt3d roadmap

What is open, roughly in priority order within each section. Finished items
move to Done with their commit; git history has the details.

## Next

- [ ] **gloss migration onto `nt3dmesh`** — in progress in gloss. nt3d
  supports it as needed (`Model.Keep` was the first request).
- [ ] **Release**:
  - push and release `ntgpuimage` (nt3d uses its unpushed sRGB, mipmap, and
    sampler commits through `go.work`; CI checks it out at `933755e`)
  - publish `ntpicture`
  - update nt3d's pinned version of it (`go get` fails until then, because
    the pin is an unpublished revision; dependencies are added to `go.mod`
    and `go.sum` by hand)
  - tidy both modules
  - register the `nimbleterminal.dev/nt3d` import path
  - tag a version

## Rendering

- [ ] **More lights**: point lights and a fill light; today there is one
  directional light plus ambient.
- [ ] **Instancing follow-ups**: a demo of shared geometry (a forest, a
  marker field); shared points, lines, and boxes, if charts need them.
- [ ] **Mipmap follow-ups**: procedural textures (needs `ntgpuimage`
  render-target mips); coverage-preserving alpha mips for cutouts (fur and
  leaves sample full size today); anisotropic filtering for grazing floors.
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

- mipmaps and linear-light texture filtering, both renderers; mipmaps demo
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
