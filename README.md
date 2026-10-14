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

## Fur

`FurCoat.Shells(base)` returns an opaque skin and one cutout layer per shell,
pushed out along the base's normals (which needs normals and UVs; smooth them
first, or shells crack apart along flat-shaded edges). Each strand has its own
place, length, and taper. `Fur.Spacing` sets about how far apart strands grow
in world units, from how much surface the mesh's UVs spread over the
texture; `Density` sets strands across the texture directly. A textured base
colors its fur from its texture and wears it as skin; otherwise color runs
from `Root` to `Tip`. Either way, fur is darker deeper in. Layer textures are
made once per density and color source, and take shells × (4 × density)²
× 4 bytes, so dense fur with many shells costs memory: Spot's demo coat,
12 shells at 225 strands a side, is about 36 MB.
`FurCoat.Sway(shell, v)` is the node transform that bends the fur as wind or
gravity would; a uniform sway moves a whole layer, so animating it uploads no
geometry. Shells sit at k/n of the length so the top one still meets the
longest strands. Seen edge-on, the layers show as dashes, the technique's
known limit; more shells soften it. Wireframe draws the coat's outer outline.

```go
coat, err := nt3d.NewFur(nt3d.Fur{Length: .05, Shells: 12, Spacing: .016})
shells, err := coat.Shells(spot) // Textured: the fur takes Spot's colors.
for k, g := range shells {
	id, _ := scene.SetGeometry(fmt.Sprintf("fur %d", k), g)
	scene.SetTransform(id, coat.Sway(k, wind)) // Each frame, to animate.
}
```

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
coordinates, normals, polygons (convex ones as fans, concave ones ear-clipped),
polylines, objects and groups, and
material libraries: `Kd` colors and `map_Kd` PNG/JPEG textures, with
transparent texels flattened over `Kd`. Each object or group becomes one
`Object` per material. Faces without normals get flat ones.

`Decode3MF` reads the core specification, the production extension's objects
in other model parts, base materials and color groups (a color at each corner),
textures (texture2d groups; each textured part becomes an object with UVs),
faces painted in Bambu Studio or PrusaSlicer (each face takes the filament
covering most of it), and
the filament colors and names that Bambu Studio, OrcaSlicer, and relatives keep
in `Metadata/`. Each build item is a `Model.Parts` entry with its triangle
count; `Options.Parts` chooses which to assemble, and each chosen part becomes
an `Object`, one per texture it uses, with its placement baked in (mirrored
placements are rewound).
`Model.Unit`, `Metadata`, and `Thumbnail` (the slicer's plate picture first)
are filled too. Packages are limited to 4096 entries and `Options.MaxBytes`
unpacked (256 MiB by default). Over the triangle or byte limit, `Decode3MF`
returns what it learned with an error wrapping `ErrTooManyTriangles` or
`ErrTooLarge`, so a caller can choose fewer parts or show the thumbnail.

Decoding is bounded: `Options.MaxTriangles` (a memory and time budget for the
whole model, `DefaultMaxTriangles` = 4M by default, which callers may raise),
coordinates within ±1e15, and textures at most 8192 pixels a side, checked
before decoding. An object larger than one geometry may hold
(`nt3d.MaxMeshTriangles`) becomes consecutive `Object` pieces sharing its
name, material, and part; `AddTo` gives each a node, while `Model.Geometry`,
which merges into one geometry, fails for them. Side files are read only through
`Options.Open`; `ReadFile` confines them to the model's directory, and
`Decode` without `Open` skips them. Missing side files become `Model.Warnings`.
`Object.Plain` marks vertices the file left uncolored, so a viewer can repaint
them. `EncodeSTL` (binary, with colors), `EncodeOBJ`, and `Encode3MF` (one object,
face colors as base materials) write triangles; OBJ writes lines too. Units are
the file's own and nothing is normalized. Files do not say which way is up:
`Options.Up = YUp` turns Y-up models, as many OBJ files are, to nt3d's Z up.

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
- Software shares the primitive and texture semantics. It draws every triangle
  and box, transforming each node's local geometry as it rasterizes rather than
  copying the scene. Interactive frames thin only screen-space marks (points,
  lines, arrows); `Snapshot` draws all primitives. Slow frames are handled by the
  widget, which lowers resolution and finally switches to wireframe.
- `RenderWireframe` returns terminal text using ntcanvas. It has no image snapshot.
  Heightfields use a coarse, continuous row/column lattice whose density adapts
  to projected size in terminal cells. Borders and the original curve samples
  are retained; triangle diagonals are omitted. `Geometry.WireGrid` supplies this
  optional topology hint (`BuildHeightfield` sets it automatically). Front faces
  of meshes and boxes are rasterized into a coarse depth buffer (2×4 samples a
  cell), which hides mesh edges, lattices, and box edges behind nearer surfaces;
  lines, arrows, and decorations are not hidden. A mesh whose triangles average
  at least two cells on screen draws its front faces' edges. A denser one draws
  its feature lines instead: silhouettes, creases sharper than 40°, and
  occluding contours, found in the depth and normal buffers. A 1.5M-triangle
  mesh takes about 50 ms.
  Lines use ntcanvas `RasterLine`: clipping to cell centers precedes
  nearest-cell endpoint rounding and connected rasterization.
- `DynamicLines`, `Overlays` (pixel rectangles), and `CellText` (wireframe labels)
  let higher layers supply decorations without importing their concepts.
- `Scene.Pick` returns node identity, primitive kind/index, application datum ID,
  and world position. Mesh picking intersects triangle faces. Hidden subtrees
  are excluded. Testing every triangle takes milliseconds even for a 1.5M
  triangle mesh, so a click is answered at once; the first pick of a mesh over
  256 triangles also starts indexing it (a bounding volume hierarchy) on another
  goroutine, and later picks, such as hover, take about a microsecond. The
  index lasts until the node's geometry changes. It takes about 28 bytes a
  triangle; `Scene.SetPickIndexing(false)` opts out where picks are rare.
  Points, boxes, and strokes are tested one by one.
- Camera offers projection, rays, orbit/pan/zoom, and explicit `Step(elapsed, idle)`
  for automatic rotation. Units are world units; no automatic normalization occurs.
  The default camera suits an origin-centered scene roughly two units across;
  `Camera.Fit(bounds, aspect)` frames any other, keeping the view direction,
  and `LookAt(eye, target)` places the camera. Near and far planes scale with
  `Distance` (1e-4 to 1e7), so millimeter and kilometer scenes draw alike.
  `AABB.Normalization` instead moves the scene to suit the camera.

Each geometry may hold at most `MaxMeshTriangles` (932,067) triangles: its
48-byte records must fit the 128 MiB storage binding that WebGPU guarantees on
every device. Many adapters allow larger bindings, but only when requested, so
nt3d keeps the portable limit and larger meshes are split across nodes. Frames
have no total: each geometry is bound separately, and the bound is device
memory, which WebGPU does not specify. Errors wrapping `ErrGPUUnavailable`
concern the device (no adapter, failed setup); others concern the frame, such
as running out of memory, and a smaller scene may still render.

Frame area is limited to 4096 × 2160 pixels. Textures may be no larger
than 8192 pixels per side. Color filtering/lighting retain the original renderer's
sRGB-encoded arithmetic. Transparency blending is not implemented; `Material.Cutout` instead skips
texels under its `Threshold` alpha (one half by default; on the GPU and in
software alike), cutting shapes out of opaque, depth-tested triangles. Other
materials ignore alpha. Software and
wireframe rendering cut triangles at the near plane, as the GPU does, so
geometry reaching behind the camera stays drawn up close.

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
software resolution, and wireframe fallback. Without a usable GPU
(`ErrGPUUnavailable`) it renders in software. When a working GPU fails on a
frame, by default (`ReportGPUFailure`) it stops drawing rather than switching to
a much slower software rendering: the view shows the error, `Failed` returns it,
and the parent receives `RenderFailedMsg`, so it can load less, show something
else, or choose `Software`. Rendering resumes when the scene changes or a mode
is set. `WithGPUFailure(FallBackOnGPUFailure)` falls back instead. ntpicture `Picture` owns image
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
orbit, `c` to fit the scene (`Fit()`), `r` for automatic rotation, `o` for
projection, `g` for glyph/Kitty output.
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
task shapes               # Shape builders and normal styles.
task models               # Sample STL, OBJ, and 3MF files; [ and ] change model.
task large                # A 735,000-triangle asteroid: picking and outlines.
task fur                  # Shell fur on Spot the cow, swaying in the wind.
task meshes -- model.stl  # Your own mesh files (--up y for Y-up OBJ).
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
The gallery links to the terrain explorer at `/terrain/`, procedural plasma
at `/procedural/`, a dynamic scene at `/dynamic/`, shape builders at
`/shapes/`, mesh files at `/models/`, a dense mesh at `/large/`, and fur at
`/fur/`, with
rendered previews, controls, and source links. Each demo
runs inside go-booba and supports GPU, software, and wireframe rendering.

The static site sources live in `web/`; the complete generated site is
`dist/web/`. It can be served under a subdirectory without changing asset paths.

### Mesh demos

The fur demo uses Spot the cow, by Keenan Crane, which he released into the
public domain ([model repository](https://www.cs.cmu.edu/~kmcrane/Projects/ModelRepository/);
see `spot_README.txt` beside it).

`task models` (or `/models/`) shows three sample files, embedded in the
program: a 3MF robot (components with a mirrored arm, a per-corner color
gradient, slicer-painted stripes with filament colors, a textured screen, and
a thumbnail nt3d rendered), a Y-up OBJ star (concave caps, a wood texture, an
MTL color), and a binary STL trefoil knot with face colors. They are made from
nt3d's shape builders by `task models-generate`
([generate](examples/terrain/internal/terraindemo/models/generate/main.go)),
so no third-party models are included. `[` and `]` change model, `s` cycles
normals, and `c` fits the view.

`task large` (or `/large/`) builds a 735,000-triangle asteroid. Software
draws every triangle, wireframe draws its silhouette and contours, and clicks
pick triangles through an index built in the background; `i` turns indexing
off. See [models.go](examples/terrain/internal/terraindemo/models.go).

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
which the widget reports or falls back from as `WithGPUFailure` chooses. `NewShaderTextureWithParameters(source, [4]float32{phase, 0, 0, 0}, fallback)`
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
