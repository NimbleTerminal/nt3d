---
title: "Architecture"
weight: 105
---

# Architecture

How nt3d is put together: the one pipeline everything follows, which package and file holds what, and the rules that keep three renderers drawing the same picture. Read this before changing the engine; to *use* it, start with [Getting started](../getting-started/).

## The pipeline

```
Scene  ──Frame(camera, w, h)──▶  Frame  ──Render──▶  image
 edit on one goroutine            immutable           WebGPU, software, or wireframe text
```

**`Scene`** is a tree of named nodes. A node has a transform and may show geometry, a light, or a camera. The scene copies everything it is given, checks it once, and remembers what the check established, so renderers need not check again.

**`Frame`** is a snapshot of a scene through a camera. It holds one `Draw` per node that has geometry, in draw order, and the scene's `Look`. A `Draw` is the handshake between scene and renderer:

| In a `Draw` | Meaning |
| --- | --- |
| `Geometry` | Plain data, in local coordinates. Every field is the application's. |
| `World`, `Hidden` | Where it is and whether it is drawn. A zero `World` is the identity. |
| `Node`, `Name`, `Data` | Which node it came from: what a pick reports. |
| `Revision`, `Shared` | Cache identity: when a renderer may keep the uploaded geometry, and which draws show the same one. |
| unexported state | What the scene knows about the geometry: that it was checked, which part changed last, its skin pose. A frame built by hand has none, and is checked and uploaded whole. |

**Renderers** read frames and nothing else. They never see the scene, which is why a frame can be rendered on another goroutine while the scene is edited, replayed later, or built by hand.

Two rules follow from this shape, and most design questions are answered by them:

1. **Say it on the scene, not on the frame.** Draw order (`SetNodeDrawOrder`), application data (`SetNodeData`), and how things are lit and finished (`SetLook`) belong to the scene. A frame is edited only for what is true of that one frame: hiding a draw for a capture, fog scaled to the camera.
2. **A frame depends on the scene and the camera.** The one exception is level-of-detail hysteresis, which `Scene.Frame` remembers between frames; `Scene.StillFrame` leaves it out.

## Packages

Dependencies point one way: everything builds on `math`, the engine builds on a few leaf packages, and the packages around it build on the engine.

| Package | Import | Role | Depends on |
| --- | --- | --- | --- |
| `nt3dmath` | `nt3d/math` | All the math: vectors, quaternions, matrices, bounds, and ray and view-volume tests, in float32 and in float64, with an oriented box for bounds too far out for float32 | nothing |
| `nt3dsh` | `nt3d/sh` | Spherical harmonics, in Go and WGSL | `math` |
| `nt3dclod` | `nt3d/clod` | Clustering many items for budgeted drawing | `math` |
| `nt3daabbtree` | `nt3d/aabbtree` | Dynamic bounding-volume tree | `math` |
| `nt3dcollide` | `nt3d/collide` | Ray casts and kinematic capsule movement against a static triangle mesh; no state between calls | `math` |
| `nt3dgeo` | `nt3d/geo` | Positions on the Earth: WGS84 latitude, longitude and height, Earth-centered meters, local east-north-up frames, and 3D Tiles bounding regions, in float64 | `math` |
| `nt3dcollidescene` | `nt3d/collide/scene` | Joins collision to the engine: a collision mesh from a `Geometry` | the engine, `collide` |
| `nt3dcam` | `nt3d/cam` and below | Declarative camera paths, in float64 | `math` |
| **`nt3d`** | `nt3d` | Scene, frame, the three renderers, picking | `math`, `sh`, `clod` |
| `nt3dcamscene` | `nt3d/cam/scene` | Joins camera paths to a scene: states to cameras, targets by node name, a path drawn into a frame | the engine, `cam` |
| `nt3dmesh` | `nt3d/mesh` | Reading and writing mesh files | the engine |
| `nt3dgltf`, `nt3dspz`, `nt3dsog` | `nt3d/mesh/…` | Formats that register themselves with `nt3dmesh` | `mesh` |
| `nt3dwater`, `nt3dfur` | `nt3d/water`, `nt3d/fur` | Geometry builders for particular effects | the engine |
| `nt3dwidget` | `nt3d/widget` | Bubble Tea widget, Kitty graphics | the engine, Bubble Tea |

Only `widget` imports the terminal UI stack. A test (`TestEngineLinksNoTerminalUI`) fails if the engine or anything built on it does. The demos are a separate module (`examples/demos`), so that what the browser build needs never becomes a requirement of the library.

## The engine's files, by topic

The root package is one flat package. Its files are named for what they hold:

| Topic | Files |
| --- | --- |
| **Scene** | `scene.go` (nodes, transforms, geometry setters), `draw_order.go`, `look.go`, `scene_light.go`, `scene_camera.go`, `scene_lod.go` (distance tiers), `scene_asset.go` and `scene_asset_lod.go` (reusable models), `animation.go`, `deformation.go` (skins and morphs), `mesh_update.go`, `point_update.go` (partial updates) |
| **Frame** | `frame.go` (`Frame`, `Draw`, `Scene.Frame`, `StillFrame`, validation), `stats.go`, `cull.go`, `overlay.go` |
| **Geometry** | `geometry.go` (the type and its checks), `primitives.go`, `shapes.go`, `heightfield.go`, `decal.go` and `surface_frame.go` (marks placed on a surface), `geometry_ops.go`, `tangents.go`, `crease.go` and `outline.go` (outline edges), `points.go`, `gaussian.go`, `gaussian_pack.go`, `gaussian_cloud.go`, `sh_palette.go` (Gaussian splats), `labels.go`, `limits.go` (size limits) |
| **Appearance** | `material.go` (the type and its one set of rules), `texture.go`, `texture_encode.go`, `hdr.go`, `normalmap.go`, `lightmap.go`, `flowmap.go`, `pbr.go`, `toon.go`, `lighting.go`, `environment.go`, `reflection_probe.go`, `reflection.go`, `post.go`, `shader_hooks.go`, `shader_names.go` (names hooks may not take) |
| **Camera** | `camera.go`, `clipping.go`, `near.go` |
| **Picking** | `pick.go`, `bvh.go`, `render_pick.go` (GPU depth) |
| **Renderers, shared** | `renderer.go` (the interface, `Snapshot`), `shading.go`, `shadow.go`, `indirect.go`, `blend.go`, `splat_budget.go`, `transmission.go`, `linear.go` |
| **Software** | `render_software.go` (pass order and primitive dispatch), `software_triangle.go` (corner preparation, clipping, and the pixel loop), `software_coverage.go` (projection and shared-edge ownership), `software_surface.go` (sampling, normals, material hooks, and cutout), `software_lighting.go` (lighting and emission), `software_target.go` (color, depth, and blending), `software.go`, `post_software.go`, `supersample.go`, `stroke.go` |
| **Wireframe** | `render_wireframe.go`, `wire_depth.go` |
| **WebGPU** | `render_gpu.go` (setup, the frame, the scene pass), `gpu_session.go` (host-owned targets), `render_slots.go` (primitive slots and draw order), `render_geometry.go` (uploads and caches), and one file per feature: `render_texture.go`, `render_shadow_gpu.go`, `render_outline.go`, `render_oit.go`, `render_post.go`, `render_sort.go`, `render_skin.go`, `render_hooks.go`, `render_environment.go`, `render_probe.go`, `render_reflection.go`, `render_procedural.go`, `render_transmission.go` |
| **Shaders** | `wgsl/scene_*.wgsl` (one module in ten files, joined in the order `sceneShaderParts` lists), `skin.wgsl`, `sort.wgsl` |

## How the three renderers stay in step

Shading is written twice, in Go for software and in WGSL for the GPU, and nothing in the language ties the two together. These do:

- **Reference pictures.** Every demo is drawn at a small size and compared with a picture recorded earlier, in software (`TestDemosMatchReference`, part of `task ci`) and on the GPU (`TestGPUDemosMatchReference`, part of `task test-gpu`). The tests below ask about a few pixels of small scenes, or compare one renderer with the other; these are what notice a whole picture change, or both renderers change together. A failure writes what was drawn and a map of where it differs; `-update-reference` records a picture that changed on purpose.
- **Parity tests.** Most features have a `_gpu_test.go` that renders the same frame both ways and compares pixels. They carry the `webgpu` build tag and need a real adapter: `task test-gpu`. `task ci` type-checks them without running them.
- **Layout tests.** `TestGPUBufferLayoutsMatchTheShader` parses the shader's struct declarations and fails if the byte counts Go writes for the scene uniform, instance, mesh, and point records differ from them. The uniform is also checked for length where it is written.
- **One module, one namespace.** WGSL has no namespaces. A shader hook's source is compiled into the scene shader's module, so hooks are refused if they declare a name it uses.
- **Browsers are stricter.** They compile WGSL with Tint, which rejects some shaders the native translator accepts. A shader change that passes `task test-gpu` still has to be loaded in a browser build before it is trusted.

Wireframe is a structural fallback, not the same picture: edges, points, and labels in terminal cells, hidden where a surface covers them.

## The GPU renderer's shape

- **Shared and per-view state.** Everything created once per device (the shader module, layouts, pipelines, uploaded geometry and textures) lives in `gpuShared`, behind its mutex, and is counted by the views that use it. Each `gpuRenderer` is one view: its own uniforms, instance buffers, targets, and a copy of the shared pipelines it reads without the lock.
- **Slots.** A primitive kind is a slot (`slotPoint`, `slotMesh`, `slotBox`, …), and geometry keeps a batch of records per slot. A pass draws geometry by geometry in the frame's order, each one's slots in `geometrySlots` order, never slot by slot: of two surfaces at one depth the later draw shows, as software has it.
- **Instancing.** Draws of one shared geometry are drawn together, where the first of them is. A later one joins only if no draw since overlaps its bounds, since that is the only case where drawing it early could change the picture.
- **Caches.** Uploaded geometry is keyed by `Draw.Shared`, else by `Draw.Node` and `Draw.Revision`, else by the frame's `Revision` and the draw's position. What a frame does not use is released when the next frame is prepared; hook programs are released after going unused for a while.

## Threads

- A `Scene` belongs to one goroutine: edit it, take frames from it, and pick against it there.
- A `Frame` may be rendered anywhere, at any time, as long as nobody changes its geometry.
- A renderer serializes its own `Render` and `Close` calls.
- Views of one GPU device (`GPUSession.NewView`) may render at the same time; they share uploads through `gpuShared`.

## Making a change

| To | Touch |
| --- | --- |
| Add a field the scene should carry | The scene setter, then the `Draw` or `Look` it reaches the frame through. Add an `Example` if it is something callers do. |
| Add a value to the scene uniform | Append it where `render_gpu.go` builds the uniform, add it to `Uniforms` in `wgsl/scene_bindings.wgsl` in the same position, and adjust `uniformSize`. The layout test says if they disagree. |
| Add a per-instance value | `transformRecord` in `render_geometry.go`, `Instance` in `wgsl/scene_bindings.wgsl`, and `instanceRecordSize`. |
| Add math | Package `math`, with a test there. No other package defines a vector, quaternion, matrix, or intersection test of its own; a float64 need uses `Vec3d`, `Quatd`, and `Mat4d`. |
| Add a material rule | `checkMaterial` in `material.go`, the only place a material's own rules are written. |
| Add shading | The software surface or lighting stage and the matching `wgsl/scene_*.wgsl`, with a `_gpu_test.go` that compares them. |
| Honor a glTF material extension | A read function and a row in `materialExtensions` (`mesh/gltf/material_extensions.go`); any other extension gets a row in `documentExtensions`. A test holds every extension name in the source to those tables. |
| Add a mesh format | A package under `mesh/` that registers a decoder with `nt3dmesh`, and a fuzz target with seeds under 1 KB. |
| Add a demo | `examples/demos/internal/demo`, plus its task in `Taskfile.yml` and its entry in the gallery. |
