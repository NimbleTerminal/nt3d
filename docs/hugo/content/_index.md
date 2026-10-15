---
title: "nt3d"
type: docs
bookToC: false
---

# nt3d

A small 3D scene engine for Go programs that draw in the terminal or the browser. Build a scene of meshes, points, lines, and textures, point a camera at it, and nt3d renders it on the GPU with WebGPU, in software when there is no GPU, or as wireframe text when there are no graphics at all.

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

- **A scene graph** of named nodes with transforms, visibility, and shared geometry, so a forest of a thousand trees costs one mesh.
- **Three renderers** that agree: WebGPU, a software rasterizer that draws the same picture, and wireframe for any terminal.
- **Materials**: textures with mipmaps, transparency, cutouts, highlights, fog, shader-generated textures, and shell fur.
- **Mesh files**: STL, OBJ, 3MF, PLY, and glTF, read safely within memory and size limits.
- **A Bubble Tea widget** that handles the camera, picking, Kitty graphics, and falling back when the GPU or the terminal can't keep up.

## Where to look

- **[Guide](guide/)**: from a first program through scenes, materials, mesh files, and the widget.
- **[Demos](guide/demos/)**: what each gallery demo shows, and how to run it.
- **API reference**: every type and function is documented in the source; `go doc nimbleterminal.dev/nt3d` reads it.

## Source

nt3d is MIT-licensed, copyright Neomantra Corp. The source and issues are on [GitHub](https://github.com/NimbleTerminal/nt3d).
