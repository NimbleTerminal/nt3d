---
title: "Rendering"
weight: 70
---

# Rendering

To draw a scene, take a `Frame` of it through a camera, then hand the frame to a renderer. The [widget](../widget/) does all of this for you; this page is for when you want to know what it's doing, or render without it.

```go
camera := nt3d.DefaultCamera()
frame, err := scene.Frame(camera, 960, 640)
if err != nil { panic(err) }
gpu := nt3d.NewGPURenderer()
defer gpu.Close()
img, mode, err := nt3d.Snapshot(frame, nt3d.WebGPU, gpu)
// mode reports whether WebGPU drew it or software stood in.
```

## Frames

A frame is a snapshot: the geometry, world transforms, and visibility of the scene at the moment you took it. It never changes afterwards. To update the picture, change the scene and take a new frame; old frames stay valid, and can even be rendered again. This is what lets rendering run on another goroutine while you keep editing.

Frames skip what the camera can't see. Every renderer, and picking, passes over a node whose bounds lie wholly outside the view. The picture is the same, but in a field of spheres seen from inside, software frames drew about 2.8× faster and wireframe 3.5×. The GPU keeps the skipped nodes' buffers for when they come back.

## Cameras

`DefaultCamera()` suits a scene centered on the origin and about two units across. For anything else:

- `Camera.Fit(bounds, aspect)` frames the bounds, keeping the view direction.
- `LookAt(eye, target)` places the camera.
- Orbit, pan, and zoom move it as the widget's mouse controls do, and `Step(elapsed, idle)` turns it automatically.
- `AABB.Normalization` instead moves the scene to suit the camera.

Units are world units and nothing is rescaled for you. The near and far planes scale with the camera's distance (1e-4 to 1e7), so millimeter and kilometer scenes draw alike.

## The three renderers

**WebGPU** draws on the GPU, through [ntgpuimage](https://github.com/NimbleTerminal/ntgpuimage) when `NewGPURenderer` owns the device and reads back an `image.Image`, or straight into a host window's swapchain or texture view through `GPUSession` and `GPUTarget` without copying pixels back to the CPU: textured triangles, instanced points, boxes, and arrows, thick lines, and lighting. Meshes stay on the GPU between frames, so only what changed is uploaded.

### Host-owned GPU targets (`GPUSession`)

When a desktop window or GPU framework such as [GoGPU](https://github.com/gogpu/gogpu) already owns the WebGPU device, swapchain, and frame command encoder, open a `GPUSession` on its `*wgpu.Device` (or `gpucontext.DeviceProvider`) and resolve each `Frame` directly into a `GPUTarget`:

```go
session, err := nt3d.NewGPUSessionFromProvider(app.GPUContextProvider())
if err != nil { panic(err) }
defer session.Close()
_, _ = nt3d.AdoptBindingLimit(session)

// Inside the host's draw callback:
frame, err := scene.Frame(camera, width, height)
if err != nil { return }
err = session.Encode(frame, dc.CommandEncoder(), nt3d.GPUTarget{
    Color:  dc.SurfaceView(),
    Format: dc.Format(), // RGBA8Unorm, RGBA8UnormSrgb, BGRA8Unorm, BGRA8UnormSrgb, or RGBA16Float
    Width:  width,
    Height: height,
})
dc.MarkPreserveContent()
```

- `session.Encode(frame, enc, target)` records compute, shadow, scene, and resolve passes onto `enc` and leaves `enc.Finish()` and queue submission to the host.
- `session.Render(frame, target)` creates an encoder, encodes `frame`, and submits to the device queue for standalone offscreen texture views.
- `target.Viewport` (`image.Rectangle`) resolves `frame` into a sub-rectangle of `target.Color` (such as a split-screen pane or picture-in-picture inset) while preserving pixels outside the rectangle; `frame.Width` and `frame.Height` match `Viewport.Dx()` and `Viewport.Dy()`.
- `target.Load` preserves existing pixels inside the resolved region and alpha-composites the 3D scene over them (for example, with `frame.Background = color.RGBA{0, 0, 0, 0}` and `frame.Environment = nil`).
- `session.Close()` releases only nt3d's pipelines, buffers, and textures, leaving the caller's `*wgpu.Device` open.

Run `task gogpu` (or `task gogpu -- -file scene.sog` for any `.glb`, `.gltf`, `.sog`, `.spz`, `.ply`, `.stl`, `.obj`, or `.3mf` file) in `examples/gogpu` for an interactive native window viewer with orbit/pan/zoom, click picking with outline highlight, animation clip cycling (`C`), Gaussian SH and splat budget controls (`H`, `U`/`Y`/`Z`), a picture-in-picture sub-viewport (`V`), `Load` compositing (`L`), supersampling (`S`), and a second window sharing the same GPU device (`2`).

**Software** draws the same picture on the CPU, with the same primitives, textures, and lighting. It's the fallback when there's no GPU, and it's slower: in interactive frames it thins out points, lines, and arrows, and the widget lowers its resolution, then switches to wireframe if frames are still too slow. `Snapshot` always draws everything. `RenderSoftware(frame)` also returns the depth buffer, for outlines or compositing done afterwards. Both its image and depth are independently owned.

Retain `NewSoftwareRenderer()` between image-only frames to reuse private depth storage; every returned color image is still independently owned. The renderer retains at most 4 MiB of depth scratch. Larger renders (including enlarged supersampled rasters) use temporary depth storage, and a render below one quarter of the retained capacity shrinks it. `Render` and `Close` serialize internally, so they may overlap. `Close` waits for the active render, releases scratch, is idempotent, and rejects later renders. Keep frame data immutable until `Render` returns. One-shot `Snapshot` and `RenderSoftware` calls do not retain scratch.

**Wireframe** draws terminal text with [ntcanvas](https://github.com/NimbleTerminal/ntcanvas), so it works in any terminal. Surfaces hide what's behind them. A coarse mesh draws its edges; a dense one draws its silhouettes, sharp creases, and contours instead, so a 1.5-million-triangle mesh is still a clean drawing, in about 50 ms. Heightfields draw as a lattice whose density adapts to their size on screen.

## How it looks

**Lighting.** `Frame.Lighting` (or the widget's `SetLighting`) holds an ambient light and up to eight lights (`MaxLights`), shaded per pixel on both the GPU and in software. The ambient light blends from `Ground`'s color on surfaces facing down to `Sky`'s facing up; equal colors give even light. Each `Light` is a `DirectionalLight` (from `Direction`, which it requires, like the sun) or a `PointLight` (from `Position`, fading to nothing at `Range`, or as 1/(1+d²) without one), with an sRGB `Color` (zero means white) and an `Intensity` (zero means 1). `DefaultLighting()` is a gray ambient and one white key light from above, front, and right. Unlit materials, points, lines, and overlays are never lit.

**Shadows.** Both WebGPU and software draw two kinds. `Geometry.DropShadow` lays a soft black disc centered under the geometry, on the horizontal plane at the bottom of its world bounds, without needing a light. `Strength` sets its center darkness from 0 to 1 (zero disables it); `Radius` is in world units, or zero for the bounds' larger horizontal half extent. `FixedFloor: true` places the disc at world-space `FloorZ` (including zero), keeping it on the ground when an instance moves vertically. It still follows the instance horizontally, and does not create a visible floor mesh. Shared geometry gets one disc per instance. Valid geometry bounds are required. Drop shadows darken surfaces beneath the disc, including unlit ones.

For shadows cast by a light, set `Light.Shadows`: the first light with the flag casts, whether directional or point. Directional maps fit the visible scene; point lights use six 90-degree views around `Position`, bounded by the visible scene and optionally `Range`. Both use a small 3×3 depth filter for soft edges. Opaque and textured meshes, cutouts with their gaps, and boxes cast; lit surfaces receive, including transparent ones. Transparent meshes, points, lines, arrows, and overlays don't cast, and unlit surfaces don't receive. Animated and skinned meshes use their current pose. Only one light casts mapped shadows at a time; other lights and ambient illumination remain unaffected.

```go
geometry.DropShadow = nt3d.DropShadow{Strength: 0.5, FixedFloor: true, FloorZ: 0}
// Radius follows the bounds; the disc stays on world Z=0.
lighting := nt3d.DefaultLighting()
lighting.Lights[0].Shadows = true
frame.Lighting = lighting
// Or select a point light:
frame.Lighting.Lights = []nt3d.Light{{
    Kind: nt3d.PointLight, Position: nt3dmath.Vec3{X: 2, Z: 3},
    Range: 10, Shadows: true,
}}
```

Both kinds follow the instances drawn by the camera: a culled object loses its shadow, even when a drop disc's `Radius` would reach into view. Wireframe ignores both. In the [lights and forest demos](../demos/), `s` cycles through no shadows, drop shadows, and shadow maps. In lights, use `l` to select a point-light preset and `b` to lift the shapes above the fixed drop-shadow floor.

Point maps use six 1024² depth faces on the GPU and for complete software frames (24 MiB depth storage); interactive software can use smaller faces. Directional maps remain 2048², with smaller interactive software maps. Point maps cost six geometry passes and can show small filtering seams at face boundaries. The near plane is 0.0001 of the fitted far distance; setting a useful `Range` helps precision in very large scenes. Wireframe does not render shadows.

**Points.** `Geometry.PointStyle` sets how a geometry's points are drawn. `Shape` is `HardPoint`, the default, or `SoftPoint`, whose alpha falls off smoothly to its edge. `Units` is `PixelUnits`, the default (a `Radius` of 1 to 32 pixels at any distance), or `WorldUnits` (any positive radius, smaller farther away, between half a pixel and `MaxPointPixels` on screen). `Blend` is `OpaqueBlend`, the default; `AlphaBlend`, over what's behind by the color's alpha, with the geometry's points sorted farthest first each frame, together with every other alpha point geometry's; or `AdditiveBlend`, added to what's behind times alpha, so overlapping points glow. Blended points are hidden by nearer opaque surfaces but hide nothing themselves, and fog fades additive points out rather than tinting them. Points are never lit, and wireframe draws them as dots whatever their style. For animated particles, [`SetNodePoints` and `UpdateNodePoints`](../particles/) reuse GPU buffers and accept fixed bounds. `PointStyle.Texture` draws textured squares with grid-atlas selection through `Point.Frame`. The [particles demo](../demos/#particles) moves up to 100,000 points; `t` toggles atlas sprites. For sets larger than the view needs, [clustering](../clustering/) selects which points to draw each frame; the [point cloud demo](../demos/#point-cloud) zooms through a million.

**Linear light.** Frames from `Scene.Frame` light, blend, and average colors in linear light, decoding sRGB first and encoding the result, so light adds as it does physically. Surfaces turning away from the light dim gradually, and blended and smoothed edges keep their brightness instead of darkening. Hand-built frames work in encoded color unless they set `Frame.Linear`.

**Supersampling.** `Frame.Supersample` (1 to 4) renders at that multiple of the frame's size and averages each block down to one pixel, smoothing edges, thin strands, and cutouts. On the GPU this costs little; in software, a lot. Points, lines, and overlays keep their size.

**Fog.** `Frame.Fog` fades everything drawn in the scene toward a color (the background, by default), from `Near` to `Far` world units from the camera. Overlays and wireframe aren't fogged. The demos' `d` key scales it with the camera's distance.

**Clipping.** Geometry reaching behind the camera is cut at the near plane, on every renderer, so it stays drawn up close.

## Limits

- One geometry holds at most `MaxMeshTriangles` (932,067) triangles, so its records fit the 128 MiB storage binding WebGPU guarantees on every device (`DefaultBindingLimit`). Larger meshes are split across nodes, as the [mesh readers](../meshes/) do. A frame has no total limit beyond the GPU's memory. A capable device holds more: `nt3d.SetBindingLimit(bytes)`, or `nt3d.AdoptBindingLimit(renderer)` with the GPU renderer or a `GPUSession`, raises the limit every geometry is measured against, up to `MaxBindingLimit` (1 GiB), and `MeshTriangleLimit()`, `MeshIndexLimit()`, and `PointLimit()` give the counts in force; a geometry never holds more than `MaxPointIndex` (16,777,216) points, whatever the limit. Raise it once at startup, before anything is stored or decoded. The GPU renderer refuses a geometry its own device cannot hold with a frame error naming both sizes, so a limit raised past the device fails cleanly. In the browser the device holds the baseline until ntgpuimage can request the adapter's limits.
- Frames are at most 4096 × 2160 pixels, and textures 8192 pixels a side.
- Errors wrapping `ErrGPUUnavailable` mean the device itself (no adapter, failed setup). Other errors concern the frame, such as running out of memory, and a smaller scene may still render.

## Decorations

`DynamicLines`, `Overlays` (pixel rectangles), and `CellText` (wireframe labels) let a layer built on nt3d, such as a chart, add decorations to a frame without nt3d knowing what they mean. The widget's `WithFrameDecorator` adds them to each frame.

## Building frames by hand

Most programs should use `Scene.Frame`. If you build frames yourself, keep the geometry immutable, give one `Instance` per geometry (or none, for identity transforms), and change `Revision` when geometry changes and `TextureRevision` when textures do.

For the GPU to cache node by node, also supply `GeometryRevisions`, one per geometry, and unique nonzero `Instance.Node` identities; change a node's revision when its geometry or material changes. Without them, any change re-uploads the whole frame. Revisions must not collide with other frames sent to the same renderer, which `Scene` takes care of across scenes. If you edit the geometry of a frame a scene made, update its revisions too, or clear `GeometryRevisions` and change `Revision`.

Hand-built frames don't cull unless you set `Frame.Cull`, and then each geometry's `Bounds` must contain it.

**Environment.** Add a [gradient sky or image panorama](../environment/) with `Scene.SetEnvironment`. GPU and software share diffuse image-based lighting; wireframe shows a terminal sky background.

## 3D Gaussian splatting

`nt3dmesh.DecodePLY` recognizes uncompressed 3DGS PLY files and SuperSplat's
compressed PLY by their headers; `nt3dmesh.DecodeGaussianPLY` and
`nt3dmesh.DecodeCompressedPLY` each require their own. Niantic's compressed
SPZ format, about ten times smaller, decodes through `nt3d/mesh/spz` (or the
`formats` package), which registers `.spz` with `ReadFile`; PlayCanvas's SOG
decodes through `nt3d/mesh/sog` (or `formats`) the same way. The result is
ordinary scene geometry, so orbit, pan, zoom, node transforms, visibility,
fog, and renderer switching work as usual:

```go
model, err := nt3dmesh.ReadFile("scene.sog", nil) // or scene.spz
// Handle err, then install the cloud: one object, or slabs of a big one.
for i, o := range model.Objects {
	id, err := scene.SetGeometry(fmt.Sprintf("splats %d", i), o.Geometry)
}
```

Try `task gaussians`, or the **Gaussian splatting** gallery demo. Its original,
CC0 576-splat ring is embedded from `examples/demos/internal/demo/models/gaussian-ring.ply`.
Drag to orbit, scroll to zoom, and press `1`, `2`, or `3` for GPU, software,
or wireframe. Press `h` to compare DC and full SH color. The same PLY can be opened with `task meshes -- path/to/scene.ply`.

### Supported PLY subset

ASCII, binary little-endian, and binary big-endian files are accepted. One
`vertex` element must contain scalar `x y z`, `scale_0..2`, `rot_0..3`,
`opacity`, and `f_dc_0..2` properties, in any order. Following the
[reference 3DGS format](https://github.com/graphdeco-inria/gaussian-splatting/blob/main/scene/gaussian_model.py),
scales are exponentiated, opacity uses a sigmoid, and the w,x,y,z quaternion
is normalized. SH degree 0 through 3 is supported: `f_dc_0..2` plus zero,
9, 24, or 45 contiguous `f_rest_*` properties. PLY's channel-major values
are converted to point-major RGB coefficients. The renderer evaluates them
in the local camera-to-splat direction and adds 0.5; negative colors are
clamped before the normal sRGB/linear conversion, and colors above 1 keep
their headroom while splats blend, in both renderers, clamping once per
pixel at the end. Orthographic cameras use
parallel rays. Coefficients remain float32; opacity is quantized to eight
bits. Wireframe uses clamped sRGB8 DC color. Normals and unrelated scalar
properties are ignored. Incomplete SH bands, nonfinite coefficients,
`.splat`, and exposure metadata are not supported. PlayCanvas's compressed
PLY is detected by its header and described [below](#sog-and-compressed-ply).
Invalid/nonfinite attributes, zero quaternions, missing
properties, and activated scales outside `[1e-20, 1e6]` are rejected.

### SPZ

`nt3dspz.Decode` reads SPZ versions 2 and 3 (one gzip stream) and 4 (zstd
streams), unpacking its 24-bit positions, byte opacities, colors, log scales,
and quaternions (three signed bytes, or the "smallest three" packing from
version 3) into the same points the PLY decoder makes, and its byte SH into
`GaussianSH` for degrees 0 to 3. SPZ stores right-up-back coordinates, so
the decoder turns them to Z-up itself and ignores `Options.Up`; the Y-down
PLY captures need `--up=-y` in the demos, an SPZ of the same scene needs
nothing. Degree-4 SH and version 1 are refused; the optional extensions
block (a named coordinate system, quantization notes) and the antialiased
flag are ignored. `Options.MaxBytes` bounds the decompressed size. Niantic's
samples, at 786k and 933k splats, open as one slab each.

### SOG and compressed PLY

SuperSplat writes PlayCanvas's compressed formats: SOG version 2 as a `.sog`
bundle (a zip of `meta.json` and lossless WebP images) or as a folder holding
an unbundled `meta.json` and its images, and a compressed PLY. Legacy SOG
version 1, which has no `version` field, decodes too. `nt3dsog.Decode` reads
a bundle, or a `meta.json` whose images come through `Options.Open`, which
`ReadFile` sets for you. `ReadFile` recognizes `.sog` by its extension and an
unbundled `meta.json` or a zip holding one by its content. `DecodePLY`
detects compressed PLY, and `DecodeCompressedPLY` requires it. Both store
the coordinates of the PLY they were made from, so `Options.Up` applies as
it does for PLY.

SOG's means are 16-bit values in a log domain; scales and DC color are
256-entry codebooks; quaternions use the "smallest three" packing; opacity is
sh0's alpha. Higher-order SH is a palette of up to 64k centroid sets, with a
16-bit label per splat, which the decoder keeps as a palette (see [SH
palettes](#sh-palettes)); that palette is lossy, so a SOG's higher-order SH is
approximate by construction. `Options.SHPalette` can expand it to per-splat
coefficients instead. Version 1 uses linear min/max ranges in place of the codebooks. Early
exports of either version omit the palette size and band count, which the
centroid image then supplies. A compressed PLY is chunks of 256 splats that bound their positions, scales, and
optionally colors; each splat packs an 11-10-11 bit position and scale, a
2-10-10-10 rotation, and 8-bit color and opacity, with optional 8-bit SH in an
`sh` element. All of it becomes the same points and `GaussianSH` as the PLY
decoder makes.

SOG's `model` tag (`antialiased`, `2dgs`) and the PLY `SplatRenderMode`
comment are ignored. `Options.MaxBytes` bounds a compressed PLY's input
bytes, and a SOG's files plus its decoded images (`width × height × 4` bytes
each). `.splat` and `.ksplat` remain unsupported.

### SH palettes

A splat's view-dependent color is up to 16 RGB spherical-harmonic coefficients, 48 bytes at degree 3 even when stored compactly, and most captures use far fewer distinct sets of the 15 bands above the first than they have splats. A palette shares them. `Geometry.GaussianSHPalette` (an `*SHPalette` holding a degree and `Count()` centroids of `Stride()` RGB coefficients each) and `Geometry.GaussianSHLabels` (a `uint16` centroid per splat) replace the per-splat bands: with a palette, `GaussianSH` holds just the first coefficient, the DC, one RGB per splat, and a splat's color is its DC plus its centroid's bands. A palette holds at most `MaxSHPaletteCentroids` (65,536) centroids. Both renderers draw it, `PointStyle.SHDCOnly` shows the DC alone, and `nt3d.ExpandSHPalette(&g)` turns a geometry back into per-splat coefficients, leaving the palette for other geometries to keep sharing.

`nt3dmesh.Options.SHPalette` chooses what the decoders hand back:

| Mode | Result |
| --- | --- |
| `SHPaletteKeep` (the default) | A SOG's palette stays a palette; PLY, compressed PLY, and SPZ give per-splat coefficients, and nothing is built. |
| `SHPaletteBuild` | Those three get a palette built at load, on the whole cloud before it is split, with `DefaultPaletteOptions()`. A SOG keeps its own. |
| `SHPaletteExpand` | Per-splat coefficients for every format, a SOG's included, as before palettes existed. |

Decoders apply the mode with `nt3dmesh.ApplySHPalette(&g, &options)`, once the cloud is complete and before `PointPieces` splits it, so every piece shares one palette; a decoder of your own calls it the same way. To build a palette yourself, call `BuildSHPalette(ctx, &g, nt3dmesh.DefaultPaletteOptions())`. It trains two-level k-means on a subsample of at most `DefaultSubsample` (262,144) splats, `DefaultCoarseCentroids` (256) coarse cells and then centroids within each, assigns every splat, and reports the centroids made and the RMS error per band coefficient. Set `Centroids`, `Iterations`, `Workers`, `MaxRMSError` (the report's `MetBudget` says whether it was met), or `Progress` in `PaletteOptions`; zero values mean the defaults. A cloud with no more distinct band sets than `Centroids` gets exactly those sets, with no error. On an Apple M5 Max, a million random degree-3 splats build 65,536 centroids in about 2.5 s with an RMS error of 0.227 against an input whose own RMS is 0.289; uniform random coefficients are the worst case, and real captures cluster far better.

`BuildSHPalette` never writes into the slices it is given, and it changes its geometry only on success, so it can run beside a scene that is drawing the original: copy the geometry, build on a goroutine, and swap the result in. To pay the build once, `nt3d-palette` writes the palette to a SOG bundle that later loads keep (see [Preprocessing splats](../meshes/#preprocessing-splats)).

```go
g2 := g // Shares g's slices; the build replaces g2's fields, not their contents.
go func() {
	report, err := nt3dmesh.BuildSHPalette(ctx, &g2, nt3dmesh.DefaultPaletteOptions())
	if err == nil {
		scene.SetNodeGeometry(id, g2) // From your update loop, not the goroutine, if it owns the scene.
	}
}()
```

A palette changes what the GPU holds. Compact SH goes from 48 bytes a degree-3 splat to 5 (3 for the DC and 2 for the label), plus a palette of 45 bytes a centroid, 2.9 MB at 65,536: a million splats' SH shrinks from 48 MB to 5 MB plus up to 2.9 MB. `Precise` goes from 192 bytes a splat to 14 (12 for the DC and 2 for the label), plus 180 bytes a centroid, 11.8 MB at 65,536. The palette is quantized as compact SH is, so a built palette is also approximate: the RMS error above is before that.

The scene operations know about palettes. `MergeGeometry` keeps a palette when its inputs share it, the same `*SHPalette` or a byte-identical one, and expands them otherwise. `Clip`, `TransformGeometry`, point updates, and `nt3dmesh.PointPieces` carry the labels, and pieces share the palette. A paletted splat has one stored coefficient, so `nt3d.MaxGaussians(1)` bounds a paletted cloud's pieces. Try it in `task meshes -- scene.spz`: `w` builds a palette for every piece in the background, with progress in the status line, then swaps it in; pressing `w` again swaps the originals back, and pressing it during the build cancels it.

### Budgeting splats

A capture can hold millions of splats, most too small to see at once. The decoders cluster each cloud at load (see [Gaussian clouds](../clustering/#gaussian-clouds)), and every frame draws a budgeted subset: clusters outside the view are dropped and the rest are thinned by their projected size, with the remaining splats enlarged to keep their coverage. Splats are shared out under `Frame.SplatBudget`, one million (`DefaultSplatBudget`) when zero; a negative budget draws every splat, and `Scene.SetSplatBudget` sets it for the scene's frames and redraws the widget. `Frame.SplatSelection` returns how many splats the frame draws and how many its clouds hold, exactly what the renderers draw.

```go
scene.SetSplatBudget(250_000)
f, err := scene.Frame(camera, width, height)
drawn, resident := f.SplatSelection()
```

The GPU keys only the selected prefixes, so the sort and the draw cost follow the budget, not the capture's size. Whole `Render` times with readback on an Apple M5 Max (`NT3D_GPU_BENCH=1`):

| Frame | ms |
| --- | --- |
| 1M splats, unbudgeted | 7.2 to 7.5 |
| 2.5M splats, unbudgeted | 16.5 |
| 2.5M splats, clustered, default budget (1,004,804 drawn) | 11.2 to 11.4 |

The budgeted frame costs more than the 1M one for two reasons. Scaling the thinned clusters' axes to keep their coverage costs about 1 ms (forcing the scale to 1 took the frame from 11.0 to 10.2 ms); the rest is gathering points through the cluster order out of the full buffers, which storing points in cluster order at upload would fix (see the roadmap). Frames also skip revalidating geometry a scene stores: `SetNodeGeometry` certifies points and clusters once, so `Render` does not walk every point each frame (a 2.5M-splat frame took 67 ms before and 17 now, and a 1M frame 28 ms before and 7 now). Clustering 2.5M points at load takes about 400 ms.

Try it in `task gaussians` or `task meshes -- scene.spz`: `u` halves the budget, `y` doubles it, `z` restores the default, and the status shows the splats drawn of those loaded.

### Rendering and limits

Gaussians extend `Point` with three local one-sigma `GaussianAxes`, using
`PointStyle{Shape: GaussianPoint, Units: WorldUnits, Blend: AlphaBlend}`.
Set a positive `Radius` for point validation; Gaussian rendering uses the axes.
Node and baked affine transforms rotate and scale the axes. GPU rendering
reuses the point quad and auxiliary sprite buffer. Every frame the GPU sorts
the centers of **every alpha-blended point geometry together**, with a radix
sort in compute shaders, and draws them far to near. Depth runs from the
near plane to the far end of the frame's blended geometries, and is
quantized by its square root, so finer near the viewer. Within a geometry the
order follows a 16-bit depth. Between geometries, and against transparent
meshes and additive points, it is accurate to one of 64 depth slices;
a transparent mesh or an additive geometry falls in the slice of its bounds'
center and draws before the alpha points of that slice. So overlapping clouds
and the slabs of a big cloud blend in order without merging, though
geometries whose points fall in the same slice draw one after another. The
software renderer orders points by the same slices and keys. A frame can hold
at most 4095 alpha point geometries; more are refused. Each costs an indirect
draw a frame for every slice its bounds span (all 64 unless the frame culls
by its bounds, as `Scene.Frame`'s do), so draw cost grows with the number of alpha
point geometries: keep emitters in a handful of geometries, not one per
particle.

GPU and software project covariance with the perspective/orthographic
Jacobian and evaluate `exp(-r²/2)` on three-sigma ellipses. Both use a 0.3
pixel-squared variance floor, a 256 logical-pixel maximum support radius,
and a 0.99 peak opacity cap. They depth-test against opaque geometry without
writing depth. Splats retain captured, view-dependent appearance; these SH coefficients are
not relightable materials. Splats are self-colored/unlit, receive fog, and use the normal
linear-color and supersampling paths. They do not cast lighting shadows.
Centers outside the near/far planes are discarded; large splats can therefore
pop when their centers cross a plane. This is a first-order projection and
center-depth ordering, not exact volumetric integration.

Software renders every Gaussian with the same sort and ellipse math, even in
interactive frames; it can be slow for large clouds or heavy overlap.
Wireframe draws the existing sampled point centers (up to about 2,000), without
ellipse shape or alpha. Start with **up to 10,000 splats on GPU** and **about
1,000 in software** for interactive use; these are conservative starting
budgets, not measured frame-rate guarantees. Coverage, resolution, and
supersampling matter as much as count. The hard limit is the storage binding,
per buffer of one geometry: the 128 MiB WebGPU guarantees unless
`SetBindingLimit` raises it (see [Limits](#limits)). The GPU stores splats
compactly by default: axes as half floats (24 bytes a splat) and each
spherical-harmonic channel as one byte at SPZ's quantization, the color byte
for the first coefficient and `(b-128)/128` for the bands, which clamp to
±1 as they do in SPZ. `PointStyle.Precise` keeps the float records the
software renderer draws, 48-byte axes and 12 bytes a coefficient, for splats
that must not be quantized. Compact axis components are half floats: magnitudes
from about 6e-5 to 65504 local units keep 2⁻¹¹ relative precision, a component
beyond 65504 clamps (which bends the axis), and one below about 3e-8 becomes
zero. The first coefficient saturates beyond about ±3.33, a base color outside
roughly −0.44 to 1.44. Scenes in unusual units, like millimetres with 65 m
background splats or kilometres with millimetre splats, or with extreme colors,
should set `Precise`. `nt3d.MaxGaussians(n)` gives the most compact
splats with `n` SH coefficients each at the limit in force (at the default,
2,796,202 at degree 3, 4,971,026 at degree 2, 5,592,405 at degree 0 or 1 or
without SH), and `nt3d.MaxPreciseGaussians(n)` the most with `Precise`
(699,050 at degree 3, 2,796,202 without SH). A larger cloud is [split into slabs](../meshes/#staying-safe-with-big-or-hostile-files).
The decoders split at the compact limit; to draw a decoded cloud `Precise`, set
`PointStyle.Precise` on each object's geometry and pass it through
`nt3dmesh.PointPieces` again, which re-splits at the precise limit.
Gaussian input obeys `Options.MaxBytes` (256 MiB by default).
A compact splat takes 44 GPU bytes (a 20-byte point record and 24 bytes of
axes), plus a 64-byte header per geometry, plus 3 bytes per RGB coefficient, 92 bytes at degree 3; a precise one
takes 68 plus 12 per coefficient, 260 at degree 3. A million degree-3 splats
take about 92 MB compact and 260 MB precise. With an [SH palette](#sh-palettes)
a splat holds 5 bytes of SH compact (49 in all, plus 45 bytes per centroid)
and 14 precise (82 in all, plus 180 per centroid), so a million paletted
splats take about 49 MB plus up to 2.9 MB compact. SH uses a separate buffer,
keeping each storage buffer within the binding limit even at the hard
count limit. The frame-wide sort adds 16 bytes per alpha point drawn in a
frame (two key and two index buffers), shared by all geometries and kept
between frames at the most a frame has needed, plus about 17 KiB a geometry
for its run table. On an Apple M5 Max through Metal, one geometry of a million
splats (640×360, camera inside a 10×10×2 slab) takes about 18 ms a frame, and
2.5 million splats about 43 ms, each a whole `Render` call: sort, draw, and
reading the image back to the CPU
(`NT3D_GPU_BENCH=1 go test -tags webgpu -run TestGPUSortMillion -v .`); these
depend on the GPU and on how much the splats overlap. CPU scene snapshots,
coefficients, and the input file require additional memory. Large-scene
optimization (level of detail and budgets) is deferred: every splat is still
sorted and drawn each frame.

`Geometry.GaussianSH` is an optional flat slice with 1, 4, 9, or 16 RGB
coefficients per point. Nil keeps ordinary point color behavior.
`PointStyle.SHDCOnly` evaluates only the first coefficient, making comparisons
possible without discarding data. Point updates preserve coefficients; use
`SetNodeGeometry` to replace them. `TransformGeometry` preserves the original
SH coordinate frame even under nonuniform scale. `MergeGeometry` requires
matching coefficient counts and SH coordinate frames. Coordinate conventions
are shared with `nt3dsh`; point color alpha remains the opacity.

## Spherical-harmonic lighting

The `nimbleterminal.dev/nt3d/sh` package (named `nt3dsh`) provides a shared
orthonormal real-SH basis through degree 3, projection, evaluation, rotation,
and diffuse convolution. `WGSL()` generates a shader evaluator from the same
polynomial definitions used by Go. RGB coefficients are color-space agnostic;
use **linear radiance** for lighting. No gamma conversion, clamping, or 3DGS
0.5 offset is implicit in this math package.

All image environments now use degree-two diffuse SH (nine RGB coefficients).
`NewSHEnvironment` also accepts coefficients directly, with an automatically
generated sky. Diffuse illumination retains HDR coefficients; the generated
sky is a clamped 128×64 sRGB image. `Environment.DiffuseSH` returns a copy of
the convolved coefficients for inspection or use elsewhere.

```go
radiance, err := nt3dsh.Project(2, func(d nt3dmath.Vec3) nt3dsh.RGB {
    warm := max(float32(0), d.X)
    return nt3dsh.RGB{0.1 + warm, 0.1 + 0.5*warm, 0.2}
})
// Handle err. Rotate the incoming lighting around world Z.
rotated, err := nt3dsh.Rotate(radiance, nt3dmath.RotationZ(math.Pi/4))
// Handle err; NewSHEnvironment applies diffuse convolution itself.
environment, err := nt3d.NewSHEnvironment(rotated)
// Handle err, then install on the scene.
scene.SetEnvironment(environment)
```

`nt3dsh.Diffuse` applies the clamped-cosine band factors divided by pi, so a
constant unit environment gives unit diffuse lighting. Evaluate the result
at a surface normal and multiply by albedo. It models broad diffuse lighting,
not sharp specular reflections, visibility, or shadows. Direct lights and
shadow maps remain available alongside it. SH can represent a probe's
lighting, but probe placement, baking, interpolation, and spatial occlusion
are not implemented.

Run `task lights` and press `h` to select the **SH studio** preset and rotate
it by 30° per press. The ordinary surfaces respond to the rotated environment.
Run `task meshes -- --up=-y /tmp/blackjacket_geofront.ply` for the downloaded
jacket, and press `h` to compare its captured full-SH appearance with DC-only.
For snapshots, `--sh-dc` selects the DC-only version. The two uses share the
basis math but evaluate different quantities: viewing direction for splats,
surface normal for diffuse illumination.
