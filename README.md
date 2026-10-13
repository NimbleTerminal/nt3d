# nt3d

A small 3D scene engine for terminal and browser applications. Source lives at
[github.com/NimbleTerminal/nt3d](https://github.com/NimbleTerminal/nt3d); the Go
module is `nimbleterminal.dev/nt3d` (Go 1.26.8).

Extracted from NimbleMarkets/ntcharts3d. MIT licensed, copyright Neomantra Corp.
The existing ntcharts3d repository is unchanged by this extraction.

| Import | Package | Purpose |
| --- | --- | --- |
| `nimbleterminal.dev/nt3d` | `nt3d` | Scene graph, camera, geometry, textures, picking, rendering |
| `nimbleterminal.dev/nt3d/math` | `nt3dmath` | Float32 vectors, matrices, rays and bounds |
| `nimbleterminal.dev/nt3d/widget` | `nt3dwidget` | Bubble Tea widget and Kitty/glyph presentation |

The library packages share one module. The terrain example has a separate module
so its go-booba runner and Bubble Tea browser fork do not become library requirements.

## Scene graph

```go
import (
    "image/color"
    "nimbleterminal.dev/nt3d"
    "nimbleterminal.dev/nt3d/math"
    "nimbleterminal.dev/nt3d/widget"
)

scene := nt3d.NewScene()
group, err := scene.Add(nt3d.Root, "objects")
if err != nil { panic(err) }
node, err := scene.SetGeometry("marker", nt3d.Geometry{
    Points: []nt3d.Point{{
        Position: nt3dmath.Vec3{Z: .5},
        Radius: 5, Color: color.RGBA{R: 240, A: 255},
    }},
})
if err != nil { panic(err) }
if err := scene.Reparent(node, group); err != nil { panic(err) }
if err := scene.SetTransform(group, nt3dmath.RotationZ(.4)); err != nil { panic(err) }
view := nt3dwidget.New(scene, 80, 24)
defer view.Close()
// Run view in Bubble Tea, or embed it and forward Update/View/Init.
```

Every node has a stable ID, a unique nonempty name, a parent, a local affine
transform, inherited visibility, and optional geometry. Root is a permanent
group. `Reparent` preserves local coordinates and rejects cycles. `Remove`
deletes a subtree; IDs are never reused. `Node` returns detached metadata.

`SetGeometry` creates or replaces a named node; `SetNodeGeometry` addresses an
existing node by ID. Setters copy slices and material headers. Textures own
immutable pixel snapshots. `SetTexture` changes a texture without rebuilding
geometry. There is no axes, legend, palette, category, or plot-range policy.

Transforms must be finite, invertible, affine, and orientation-preserving.
Reflections and zero scales are not supported in this initial graph. Local
geometry stays cached on the GPU: camera motion, node transforms, and visibility
update matrices rather than mesh buffers. Structural/geometry changes currently
rebuild combined mesh batches; per-node mesh upload caching is future work.

## Frames and rendering

```go
camera := nt3d.DefaultCamera()
frame, err := scene.Frame(camera, 960, 640)
if err != nil { panic(err) }
gpu := nt3d.NewGPURenderer()
defer gpu.Close()
img, mode, err := nt3d.Snapshot(frame, nt3d.WebGPU, gpu)
// img is independent of terminal size; mode reports WebGPU or software fallback.
```

`Scene.Frame` captures local geometry, world transforms, identities and visibility.
Frames are read-only once submitted. Mutate the scene and create a new frame to
update it; old frames remain valid. Use one goroutine for scene edits/frame
creation. Rendering may run asynchronously; GPU render/close calls are serialized.

- WebGPU uses ntgpuimage, indexed/textured triangles, point/box/arrow instances,
  thick lines, and directional lighting. The storage record layout is explicitly
  packed; Go struct memory is never assumed to match WGSL padding.
- Software shares the primitive and texture semantics. Interactive frames sample
  large geometry sets; `Snapshot` draws all primitives.
- `RenderWireframe` returns terminal text using ntcanvas. It has no image snapshot.
  Heightfields use a coarse, continuous row/column lattice whose density adapts
  to projected size in terminal cells. Borders and the original curve samples
  are retained; triangle diagonals are omitted. `Geometry.WireGrid` supplies this
  optional topology hint (`BuildHeightfield` sets it automatically). General
  meshes retain triangle edges; wireframe does not remove hidden lines.
- `DynamicLines`, `Overlays` (pixel rectangles), and `CellText` (wireframe labels)
  let higher layers supply decorations without importing their concepts.
- `Scene.Pick` returns node identity, primitive kind/index, application datum ID,
  and world position. Mesh picking intersects triangle faces. Hidden subtrees
  are excluded. Picking is linear in primitive count in this initial version.
- Camera offers projection, rays, orbit/pan/zoom, and explicit `Step(elapsed, idle)`
  for automatic rotation. Units are world units; no automatic normalization occurs.
  The default camera suits an origin-centered scene roughly two units across.
  `AABB.Normalization` can provide an explicit fitting transform.

Frame area is limited to 4096 × 2160 pixels. Textures must be opaque and no larger
than 8192 pixels per side. Color filtering/lighting retain the original renderer's
sRGB-encoded arithmetic. Transparency blending is not implemented. Software
triangles crossing the eye plane retain the original renderer's conservative
rejection behavior; this is not a full general-purpose clipping pipeline.

If constructing Frames manually, keep geometry immutable, supply one `Instance`
per geometry (or omit instances for identity transforms), and change `Revision`
when geometry changes and `TextureRevision` when textures change. Revision values
must not collide with other frames sent to that renderer. Scene creates these
revisions automatically across scene instances.

## Widget and transports

The widget owns scheduling, stale-frame rejection, GPU fallback, adaptive software
resolution, wireframe fallback, and ntpicture presentation. It renders only the
viewport; application headers, footers, and quit handling belong to the parent.
Call `Refresh()` and execute its command after editing the scene from a parent.
Forward terminal size/cell-size events. When embedded, share a zone manager and
have the parent scan the complete view.

Controls: drag to orbit, shift-drag to pan, wheel or +/- to zoom, arrows/hjkl to
orbit, `r` for automatic rotation, `o` for projection, `g` for glyph/Kitty output.
Clicks emit `nt3dwidget.PickMsg`. `WithFrameDecorator` adds generic decorations
on the update goroutine. `Dispose` returns terminal cleanup to run before exit;
`Close` releases GPU and shared-memory resources after exit.

Kitty encoding/direct/shared-memory transport comes from ntpicture. The example
runs natively and in browsers through go-booba; the browser-specific Bubble Tea
fork remains pinned in the example module.

## Development and terrain demo

ntcanvas and ntgpuimage are pinned to published upstream versions and downloaded
through Go modules. ntpicture still uses a pinned sibling checkout in CI while
its vanity version is prepared. For local development:

```sh
# Sibling layout: nt3d/, ntpicture/
task workspace
task ci
task test-gpu             # Requires a real WebGPU adapter; fails if unavailable.
task terrain -- -procedural  # GPU plasma texture on terrain; t changes phase.
task terrain -- -render-mode software
task terrain -- -render-mode gpu -snapshot /tmp/terrain.png
task build-web
task serve               # http://localhost:8768
```

`go.work` is local and ignored. See `go.work.example`. `task ci` runs Go 1.26.8
format checks, race tests, vet, native builds, and a standard Go js/wasm build.
The terrain demo supports texture replacement, flat/heightfield switching,
lighting, parent rotation, inherited visibility, picking, and all three backends.
Use `-medium direct` or `-medium shm` to select transport explicitly.

### Procedural texture demo

Run `task terrain -- -procedural`, or press `p` in the native/browser terrain
example to toggle procedural textures. `t` switches between two fixed shader
phases; `r` rotates the camera, `l` toggles lighting, and `1`/`2`/`3` select GPU,
software, and wireframe. This demo does not animate shader time.

`NewShaderTexture(source, fallback)` accepts WGSL defining
`fn nt3dTexture(uv: vec2<f32>) -> vec4<f32>`. UVs start at the top-left;
return opaque RGBA. The immutable fallback image determines the texture size
and supplies software-renderer pixels. See
[procedural.go](examples/terrain/procedural.go) for the plasma shader and its
matching CPU fallback.

Each GPU frame first generates the texture with ntgpuimage's `Frame.RenderTo`,
then samples it on the terrain mesh, using the same device and command encoder.
Only the final scene image is read back for presentation. Pipelines and targets
are cached per texture and renderer, and released when no longer referenced by
the rendered frame. Shader compilation is lazy; `Renderer.Render` reports errors,
while the widget can fall back to software. No time uniforms or custom bindings
are exposed yet.

```sh
task terrain -- -procedural -snapshot /tmp/plasma-gpu.png
task terrain -- -procedural -render-mode software -snapshot /tmp/plasma-software.png
```

The integrated path has been tested locally on Metal. Vulkan validation of
ntgpuimage was reported by Evan; this demo has not independently been run on
Vulkan. Browser/WASM compilation is checked by CI.

Before release, make the pinned dependencies downloadable, tidy both modules
with `GOWORK=off`, register the `nimbleterminal.dev/nt3d` vanity mapping, and publish
an nt3d version. No GitHub publication or infrastructure deployment is performed
by this local extraction. See [MIGRATION.md](MIGRATION.md) for the future chart split.
