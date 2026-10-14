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
| `nimbleterminal.dev/nt3d/mesh` | `nt3dmesh` | STL, OBJ, and 3MF reading and writing |

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
update matrices rather than mesh buffers. Each node has an independent geometry
revision, GPU primitive buffers, and transform buffer. Replacing geometry uploads
only that node; adding/removing nodes preserves
surviving buffers. Texture replacement preserves mesh buffers too. Removed or
superseded buffers are released; replaying an old frame reuploads only missing
geometry. This trades combined draw batches for draws per node and primitive type.

## Shapes and geometry operations

`BuildSphere`, `BuildCylinder` (also cones and frusta), `BuildTorus`, and
`BuildCuboid` return lit, UV-mapped meshes centered on the origin with Z up,
following the `BuildHeightfield` pattern: a spec struct whose zero tessellation
counts and color take defaults. Faces wind counterclockwise from outside.

```go
sphere, err := nt3d.BuildSphere(nt3d.Sphere{Radius: .5, Material: &nt3d.Material{Texture: tex}})
cone, err := nt3d.BuildCylinder(nt3d.Cylinder{BottomRadius: .4, Height: 1})
moved, err := nt3d.TransformGeometry(cone, nt3dmath.Translation(nt3dmath.Vec3{X: 1}))
both, err := nt3d.MergeGeometry(sphere, moved) // Fails: different materials.
smooth, err := nt3d.SmoothNormals(stlGeometry, 40)
```

`TransformGeometry` bakes a transform into vertices and normals. Unlike node
transforms it accepts reflections, reversing winding to keep faces outward.
`MergeGeometry` concatenates geometry that shares a material. `FlatNormals`
gives each face its own normal. `SmoothNormals` averages, weighted by corner
angle, the faces meeting at equal positions within a crease angle, so unwelded
STL meshes smooth too.

## Mesh files

```go
model, err := nt3dmesh.ReadFile("part.obj", &nt3dmesh.Options{Color: grey})
group, err := model.AddTo(scene, nt3d.Root, "part")   // One child node per object.
fit, _, _ := model.Bounds().Normalization()
err = scene.SetTransform(group, fit)
```

`DecodeSTL` reads binary and ASCII STL, including several ASCII solids and
VisCAM/SolidView or Magics face colors. Binary is recognized by its exact
record length, as headers may begin with "solid". Facet normals are recomputed.
`DecodeOBJ` reads positions with the r g b vertex-color extension, texture
coordinates, normals, polygons (as fans), polylines, objects and groups, and
material libraries: `Kd` colors and `map_Kd` PNG/JPEG textures, with
transparent texels flattened over `Kd`. Each object or group becomes one
`Object` per material. Faces without normals get flat ones.

`Decode3MF` reads the core specification, the production extension's objects
in other model parts, base materials and color groups (one color a face), and
the filament colors and names that Bambu Studio, OrcaSlicer, and relatives keep
in `Metadata/`. Each build item is a `Model.Parts` entry with its triangle
count; `Options.Parts` chooses which to assemble, and each chosen part becomes
an `Object` with its placement baked in (mirrored placements are rewound).
`Model.Unit`, `Metadata`, and `Thumbnail` (the slicer's plate picture first)
are filled too. Packages are limited to 4096 entries and `Options.MaxBytes`
unpacked (256 MiB by default). Over the triangle or byte limit, `Decode3MF`
returns what it learned with an error wrapping `ErrTooManyTriangles` or
`ErrTooLarge`, so a caller can choose fewer parts or show the thumbnail.

Decoding is bounded: `Options.MaxTriangles` (default and ceiling
`nt3d.MaxMeshTriangles`), coordinates within ±1e15, and textures at most 8192
pixels a side, checked before decoding. Side files are read only through
`Options.Open`; `ReadFile` confines them to the model's directory, and
`Decode` without `Open` skips them. Missing side files become `Model.Warnings`.
`Object.Plain` marks vertices the file left uncolored, so a viewer can repaint
them. `EncodeSTL` (binary, with colors), `EncodeOBJ`, and `Encode3MF` (one object,
face colors as base materials) write triangles; OBJ writes lines too. Units are the file's own; nothing is normalized or reoriented.

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
  packed; Go struct memory is never assumed to match WGSL padding. Image uploads,
  texture views/samplers, sample bindings, and GPU texture lifetimes are owned by
  ntgpuimage; nt3d retains immutable CPU snapshots for software fallback.
- Software shares the primitive and texture semantics. Interactive frames sample
  large geometry sets; `Snapshot` draws all primitives.
- `RenderWireframe` returns terminal text using ntcanvas. It has no image snapshot.
  Heightfields use a coarse, continuous row/column lattice whose density adapts
  to projected size in terminal cells. Borders and the original curve samples
  are retained; triangle diagonals are omitted. `Geometry.WireGrid` supplies this
  optional topology hint (`BuildHeightfield` sets it automatically). General
  meshes retain triangle edges; wireframe does not remove hidden lines.
  Lines use ntcanvas `RasterLine`: clipping to cell centers precedes
  nearest-cell endpoint rounding and connected rasterization.
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
when geometry changes and `TextureRevision` when textures change.
For per-node caching, additionally supply `GeometryRevisions`, one revision per
geometry, and unique nonzero `Instance.Node` identities. Change a node's revision
for geometry or material changes (except texture replacement). Without this slice,
the renderer retains frame-wide revision invalidation. Revision values
must not collide with other frames sent to that renderer. Scene creates these
revisions automatically across scene instances.
If manually editing the geometry entries of a scene-produced frame, update its
per-node revisions too, or clear `GeometryRevisions` and change `Revision`.

## Widget and transports

The widget owns render scheduling, stale-frame rejection, GPU fallback, adaptive
software resolution, and wireframe fallback. ntpicture `Picture` owns image
encoding, transmission pacing, stable streaming placements, and cleanup. Renderer
snapshots are submitted with `SetImageImmutable`, avoiding a second pixel copy.
Frame dimensions come from `Picture.RasterGeometry().Pixels`; camera projection
uses its separate `DisplayAspect`. Glyphs render directly at one pixel column
and two pixel rows per cell. Adaptive software rendering sets Kitty resolution
through `SetResolution`, so newly rendered frames match encoded dimensions
instead of being enlarged before transmission. Resolution changes retain the
previous front until ready and may replace its placement ID. The existing
20/60 ms pacing thresholds keep resolution changes outside a stable timing band.
The widget caps cell layout so the fixed glyph raster fits `MaxFramePixels`, and
reduces Kitty resolution when needed to respect the same framebuffer budget. It renders only the
viewport; application headers, footers, and quit handling belong to the parent.
Call `Refresh()` and execute its command after editing the scene from a parent.
Execute every returned command, including resize and mode changes.
Forward terminal size/cell-size events. When embedded, share a zone manager and
have the parent scan the complete view.

Controls: drag to orbit, shift-drag to pan, wheel or +/- to zoom, arrows/hjkl to
orbit, `r` for automatic rotation, `o` for projection, `g` for glyph/Kitty output.
`PictureStatus()` exposes presentation generation, busy/error state, and selected
format/medium. Completion means ordered output/grid completion, not terminal
display acknowledgement.

Each widget creates an explicit terminal session by default. For multiple widgets
on one terminal, pass the same `WithTerminalSession(session)` to all of them; the
host owns and closes that shared session after disposing its widgets. Forward
terminal events to the widgets, including while wireframe is visible.

Clicks emit `nt3dwidget.PickMsg`. `WithFrameDecorator` adds generic decorations
on the update goroutine. `Dispose` returns terminal cleanup to run before exit;
`Close` releases GPU resources after exit. `Dispose` also closes a widget-owned
session; caller-owned sessions remain open. Continue forwarding late messages
while a disposed widget is still in the event loop.

Kitty encoding/direct/shared-memory transport comes from ntpicture. The example
runs natively and in browsers through go-booba; the browser-specific Bubble Tea
fork remains pinned in the example module.

## Development and demos

ntcanvas and ntgpuimage are pinned to published upstream versions and downloaded
through Go modules. ntpicture still uses a pinned sibling checkout in CI while
its upstream publication is prepared. The pinned commit must be pushed before
CI can fetch that checkout. For local development:

```sh
# Sibling layout: nt3d/, ntpicture/
task workspace
task ci
task test-gpu             # Requires a real WebGPU adapter; fails if unavailable.
task procedural           # Starts plasma textures and camera rotation.
task dynamic              # Moving nodes, deforming mesh, and add/remove controls.
task meshes               # Shape builders; or: task meshes -- model.stl part.obj
task terrain -- --render-mode software
task terrain -- --render-mode gpu --snapshot /tmp/terrain.png
task build-web
task serve               # http://localhost:8768
```

`go.work` is local and ignored. See `go.work.example`. `task ci` runs Go 1.26.8
format checks, race tests, vet, native builds, and the complete js/wasm gallery build.
The terrain demo supports texture replacement, flat/heightfield switching,
lighting, parent rotation, inherited visibility, picking, and all three backends.
Use `--medium direct` or `--medium shm` to select transport explicitly.

### Browser gallery

Run `task build-web` and `task serve`, then open http://localhost:8768/.
The gallery links to the terrain explorer at `/terrain/` and procedural plasma
at `/procedural/`, plus a dynamic scene at `/dynamic/`, with rendered previews,
controls, and source links. Each demo
runs inside go-booba and supports GPU, software, and wireframe rendering.

The static site sources live in `web/`; the complete generated site is
`dist/web/`. It can be served under a subdirectory without changing asset paths.

### Dynamic scene demo

Run `task dynamic` or open http://localhost:8768/dynamic/ after building the gallery.
A static textured floor surrounds a deforming wave mesh and independently orbiting
boxes. The wave changes vertex positions and topology; each box moves through
`SetTransform` without replacing geometry. `m` holds the wave mesh so you can
observe transform-only animation, `b` adds an object, `x` removes the oldest,
`a` pauses/resumes, and `v` toggles inherited visibility. `1`/`2`/`3` select GPU,
software, and wireframe. The camera remains interactive. Use `--animate=false`
to start paused or `--snapshot /tmp/dynamic.png` for a still image.

See [dynamic.go](examples/terrain/internal/terraindemo/dynamic.go). GPU tests assert
that changing one node uploads only that node, surviving buffer identities stay
stable across insertion/removal, and old snapshots replay with identical pixels.

### Procedural texture demo

The standalone entry point is `examples/terrain/cmd/procedural`; it shares scene
and transport code with terrain. Both use pflag (`--render-mode`, `--snapshot`,
`--duration`, `--medium`, `--procedural`, `--animate`, and `--help`). After `task build-web`
and `task serve`, open http://localhost:8768/procedural/ for the browser demo.

Run `task procedural`, or press `p` in the native/browser terrain
example to toggle procedural textures. Phase animates at up to 30 updates per
second; `a` pauses/resumes it and `t` shifts phase. `r` independently rotates the
camera, `l` toggles lighting, and `1`/`2`/`3` select GPU, software, and wireframe.
Use `--animate=false` to start paused. Snapshots capture the initial phase.
The demo skips updates while the widget is busy and advances by elapsed time.

`NewShaderTexture(source, fallback)` accepts WGSL defining
`fn nt3dTexture(uv: vec2<f32>) -> vec4<f32>`. UVs start at the top-left;
return opaque RGBA. The immutable fallback image determines the texture size
and supplies software-renderer pixels. See
[procedural.go](examples/terrain/internal/terraindemo/procedural.go) for the plasma shader and its
matching CPU fallback.

Each GPU frame first generates the texture with ntgpuimage's `Frame.RenderTo`,
then samples it on the terrain mesh, using the same device and command encoder.
Only the final scene image is read back for presentation. Pipelines and targets
are cached per texture and renderer, and released when no longer referenced by
the rendered frame. Shader compilation is lazy; `Renderer.Render` reports errors,
while the widget can fall back to software. `NewShaderTextureWithParameters(source, [4]float32{phase, 0, 0, 0}, fallback)`
exposes `nt3dTextureParams`, a `vec4<f32>` uniform at group 0, binding 0.
Create a new immutable texture and call `Scene.SetTexture` for each update;
provide fallback pixels for the same phase. The renderer reuses the pipeline
and target when source and dimensions match, updating only the uniform on the
GPU. Old frames remain replayable. `NewShaderTexture` uses zero parameters.
Custom bindings are not exposed.

```sh
task terrain -- --procedural --snapshot /tmp/plasma-gpu.png
task terrain -- --procedural --render-mode software --snapshot /tmp/plasma-software.png
```

The integrated path has been tested locally on Metal. Vulkan validation of
ntgpuimage was reported by Evan; this demo has not independently been run on
Vulkan. Browser/WASM compilation is checked by CI.

Before release, make the pinned dependencies downloadable, tidy both modules
with `GOWORK=off`, register the `nimbleterminal.dev/nt3d` vanity mapping, and publish
an nt3d version. No GitHub publication or infrastructure deployment is performed
by this local extraction. See [MIGRATION.md](MIGRATION.md) for the future chart split.
