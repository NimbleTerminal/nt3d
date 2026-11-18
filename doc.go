// Copyright (c) 2026 Neomantra Corp.

// Package nt3d is a small 3D engine: a scene graph, cameras, geometry,
// materials, picking, and three renderers that draw the same frames with
// WebGPU, in software, or as terminal wireframe.
//
// # The pipeline
//
// Three types carry everything, in one direction:
//
//	Scene  ->  Frame  ->  Renderer  ->  image
//
// A [Scene] is a tree of named nodes. A node has a transform and may show
// geometry, a light, or a camera. The scene copies what it is given, so
// changing your data afterwards never changes the scene behind your back.
//
// A [Frame] is a snapshot of a scene through a [Camera] at a size in pixels:
// one [Draw] for each node with geometry, in the order they are drawn, and
// the scene's [Look] (background, lighting, fog, environment,
// post-processing). [Scene.Frame] makes one for interactive drawing, and
// [Scene.StillFrame] one for a picture that depends on the scene and camera
// alone. A frame never changes after it is made, so it can be rendered on
// another goroutine while the scene is edited, and it is the caller's to
// adjust first: hide a draw, move it, add decorations.
//
// A [Renderer] turns a frame into an image. [NewGPURenderer] and
// [NewSoftwareRenderer] make the two that return images, [RenderWireframe]
// returns text, and [Snapshot] tries WebGPU and falls back to software. A
// [GPUSession] renders into a window or texture a host already owns.
//
// # Where things are
//
//   - Building things to look at: [BuildSphere], [BuildCuboid],
//     [BuildHeightfield] and the other Build functions, or a file read by
//     package nt3dmesh. All produce a [Geometry], which is plain data.
//   - How a surface looks: [Material] and [Texture]; lights are [Light]
//     values in a [Lighting] or on nodes ([Scene.SetNodeLight]).
//   - What is under the pointer: [Scene.Pick] returns a [Hit].
//   - Telling nodes apart: [Scene.SetNodeData] attaches your own value to a
//     node, and [Scene.SetNodeDrawOrder] says when it is drawn.
//   - Many copies of one mesh: [SharedGeometry]. Detail by distance:
//     [Scene.SetLOD]. Whole models with animation: [SceneAsset].
//
// # What nt3d is not
//
// It draws what it is given and nothing more: no axes, legends, palettes, or
// plot ranges, which belong to whatever is built on top. It imports no
// terminal UI; package nt3dwidget (nt3d/widget) presents a scene in Bubble
// Tea. Package nt3dmath (nt3d/math) holds the float32 vectors, matrices, and
// bounds everything here is written in.
//
// The guide's Architecture page describes the packages, the files of this
// one by topic, and the rules that hold the three renderers together.
package nt3d
