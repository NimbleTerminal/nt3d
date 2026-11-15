---
title: "nt3d"
type: docs
bookToC: false
---

# nt3d

A small 3D scene engine for Go programs that draw in the terminal, the browser, or a native GPU window. Build a scene of meshes, points, lines, Gaussians, and textures, point a camera at it, and nt3d renders it on the GPU with WebGPU, in software when there is no GPU, or as wireframe text when there are no graphics at all.

<img class="poster" src="posters/fur.png" alt="Spot the cow covered in black-and-white fur">

[**Try the demos in your browser →**](https://nimbleterminal.github.io/nt3d/) Every demo in the gallery is the same Go program you can run in a terminal, compiled to WebAssembly.

## A first look

```go
scene := nt3d.NewScene()
ball, _ := nt3d.BuildSphere(nt3d.Sphere{Radius: .6, Color: orange})
scene.SetGeometry("ball", ball)

view := nt3dwidget.New(scene, 80, 24) // a Bubble Tea model you can drag to orbit
```

[Getting started](guide/getting-started/) turns that into a complete program.

## What it does

- **A scene graph** of named nodes with transforms, `float64` camera-relative world origins (`Scene.SetNodeOrigin`), visibility, instanced shared geometry, in-place vertex updates, and distance-tiered level of detail (`Scene.SetLOD`), so a forest of thousands of trees costs one mesh per tier.
- **Declarative camera paths**: pure-`float64`, zero-allocation camera trajectories (`nt3d/cam` and YAML/JSON `nt3d/cam/spec`) with arc-length splines, Bishop parallel transport, `C¹` SQUAD orientation, causal look crossfades, camera switching, and 3D frustum debug overlays.
- **Clustered level of detail & spatial indexing**: cluster million-point clouds, instances, and labels once (`nt3d/clod`) and cull/thin per frame on CPU or WebGPU compute, or query moving boxes with a generic dynamic AABB tree (`nt3d/aabbtree`).
- **Three renderers** that agree: WebGPU (including direct swapchain rendering via `GPUSession`), a software rasterizer that draws the same picture, and wireframe for any terminal.
- **Materials, lighting & effects**: Cook-Torrance GGX PBR with Clearcoat, Sheen, packed ORM maps, and HDR emissive glow alongside Blinn-Phong; Radiance `.hdr` and LDR image-based lighting; directional, point, and `SpotLight` shadow maps; mipmapped and normal-mapped textures; Weighted Blended Order-Independent Transparency (`Frame.OIT`) and scene refraction; 4× WebGPU MSAA and HDR post-processing (SSAO, bloom, tone mapping, vignette); cel/toon shading with silhouette and crease ink; `VertexDisplacement` and `MaterialShader` hooks; shell fur (`nt3d/fur`); and Gerstner water (`nt3d/water`).
- **Meshes, glTF animation & Gaussian splats**: STL, OBJ, 3MF, PLY, glTF/GLB (skins, morphs, cameras, PBR extensions, and `KHR_lights_punctual`), and 3DGS splats (`.ply`, `.spz`, `.sog`), decoded safely within memory and size limits.
- **A Bubble Tea widget** that handles camera controls, background-indexed picking, world-anchored labels, Kitty graphics, and falling back when the GPU or the terminal can't keep up.

## Where to look

- **[Guide](guide/)**: from a first program through scenes, materials, mesh files, and the widget.
- **[Demos](guide/demos/)**: what each gallery demo shows, and how to run it.
- **API reference**: every type and function is documented in the source; `go doc nimbleterminal.dev/nt3d` reads it.

## Source

nt3d is MIT-licensed, copyright Neomantra Corp. The source and issues are on [GitHub](https://github.com/NimbleTerminal/nt3d).
