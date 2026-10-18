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

**WebGPU** draws on the GPU, through [ntgpuimage](https://github.com/NimbleTerminal/ntgpuimage): textured triangles, instanced points, boxes, and arrows, thick lines, and lighting. Meshes stay on the GPU between frames, so only what changed is uploaded.

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

**Points.** `Geometry.PointStyle` sets how a geometry's points are drawn. `Shape` is `HardPoint`, the default, or `SoftPoint`, whose alpha falls off smoothly to its edge. `Units` is `PixelUnits`, the default (a `Radius` of 1 to 32 pixels at any distance), or `WorldUnits` (any positive radius, smaller farther away, between half a pixel and `MaxPointPixels` on screen). `Blend` is `OpaqueBlend`, the default; `AlphaBlend`, over what's behind by the color's alpha, with the geometry's points sorted farthest first each frame; or `AdditiveBlend`, added to what's behind times alpha, so overlapping points glow. Blended points are hidden by nearer opaque surfaces but hide nothing themselves, and fog fades additive points out rather than tinting them. Points are never lit, and wireframe draws them as dots whatever their style. For animated particles, [`SetNodePoints` and `UpdateNodePoints`](../particles/) reuse GPU buffers and accept fixed bounds. `PointStyle.Texture` draws textured squares with grid-atlas selection through `Point.Frame`. The [particles demo](../demos/#particles) moves up to 100,000 points; `t` toggles atlas sprites.

**Linear light.** Frames from `Scene.Frame` light, blend, and average colors in linear light, decoding sRGB first and encoding the result, so light adds as it does physically. Surfaces turning away from the light dim gradually, and blended and smoothed edges keep their brightness instead of darkening. Hand-built frames work in encoded color unless they set `Frame.Linear`.

**Supersampling.** `Frame.Supersample` (1 to 4) renders at that multiple of the frame's size and averages each block down to one pixel, smoothing edges, thin strands, and cutouts. On the GPU this costs little; in software, a lot. Points, lines, and overlays keep their size.

**Fog.** `Frame.Fog` fades everything drawn in the scene toward a color (the background, by default), from `Near` to `Far` world units from the camera. Overlays and wireframe aren't fogged. The demos' `d` key scales it with the camera's distance.

**Clipping.** Geometry reaching behind the camera is cut at the near plane, on every renderer, so it stays drawn up close.

## Limits

- One geometry holds at most `MaxMeshTriangles` (932,067) triangles, so its records fit the 128 MiB storage binding WebGPU guarantees on every device. Larger meshes are split across nodes, as the [mesh readers](../meshes/) do. A frame has no total limit beyond the GPU's memory.
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

`nt3dmesh.DecodePLY` automatically recognizes uncompressed 3DGS PLY files;
`nt3dmesh.DecodeGaussianPLY` explicitly requires them. `ReadFile` also works.
The result is ordinary scene geometry, so orbit, pan, zoom, node transforms,
visibility, fog, and renderer switching work as usual:

```go
model, err := nt3dmesh.ReadFile("scene.ply", nil)
// Handle err, then install the cloud (standard 3DGS has one object).
id, err := scene.SetGeometry("splats", model.Objects[0].Geometry)
```

Try `task gaussians`, or the **Gaussian splatting** gallery demo. Its original,
CC0 576-splat ring is embedded from `examples/demos/internal/demo/models/gaussian-ring.ply`.
Drag to orbit, scroll to zoom, and press `1`, `2`, or `3` for GPU, software,
or wireframe. The same PLY can be opened with `task meshes -- path/to/scene.ply`.

### Supported PLY subset

ASCII, binary little-endian, and binary big-endian files are accepted. One
`vertex` element must contain scalar `x y z`, `scale_0..2`, `rot_0..3`,
`opacity`, and `f_dc_0..2` properties, in any order. Following the
[reference 3DGS format](https://github.com/graphdeco-inria/gaussian-splatting/blob/main/scene/gaussian_model.py),
scales are exponentiated, opacity uses a sigmoid, and the w,x,y,z quaternion
is normalized. DC color is `0.5 + 0.2820947918 * f_dc`, clamped and stored as
sRGB8; opacity is also quantized to eight bits. Normals and other scalar
properties are ignored. Higher SH coefficients produce a warning and are
ignored. Compressed PLY, `.splat`, exposure metadata, and view-dependent SH
are not supported. Invalid/nonfinite attributes, zero quaternions, missing
properties, and activated scales outside `[1e-20, 1e6]` are rejected.

### Rendering and limits

Gaussians extend `Point` with three local one-sigma `GaussianAxes`, using
`PointStyle{Shape: GaussianPoint, Units: WorldUnits, Blend: AlphaBlend}`.
Set a positive `Radius` for point validation; Gaussian rendering uses the axes.
Node and baked affine transforms rotate and scale the axes. GPU rendering
reuses the point quad, auxiliary sprite buffer, and stable CPU radix sorter;
it uploads sorted indices when the view or geometry changes. Sorting is by
center depth **within each geometry**. Merge overlapping clouds into one
geometry with `MergeGeometry` for interleaved sorting. Intersections with
transparent meshes retain the usual geometry-order limitations.

GPU and software project covariance with the perspective/orthographic
Jacobian and evaluate `exp(-r²/2)` on three-sigma ellipses. Both use a 0.3
pixel-squared variance floor, a 256 logical-pixel maximum support radius,
and a 0.99 peak opacity cap. They depth-test against opaque geometry without
writing depth. Splats are self-colored/unlit, receive fog, and use the normal
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
supersampling matter as much as count. The hard limit remains `MaxPoints`
(500,000) and Gaussian input obeys `Options.MaxBytes` (256 MiB by default).
GPU records use 72 bytes per splat including sort indices; CPU scene snapshots,
axes, sort scratch, and the input file require additional memory. Higher SH,
GPU sorting, compressed formats, and large-scene optimization are deferred.
