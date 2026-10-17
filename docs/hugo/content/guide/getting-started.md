---
title: "Getting started"
weight: 10
---

# Getting started

## Install

nt3d is a Go module, `nimbleterminal.dev/nt3d`, and needs Go 1.26.8 or later.

## The packages

You import only what you use. The core package reads no files; `nt3dmesh` reads the common formats, and the others register themselves when imported, so a program that reads no glTF carries no glTF.

| Import | Package | What it is for |
| --- | --- | --- |
| `nimbleterminal.dev/nt3d` | `nt3d` | Scenes, cameras, geometry, textures, picking, and rendering |
| `nimbleterminal.dev/nt3d/math` | `nt3dmath` | Float32 vectors, matrices, rays, and bounds |
| `nimbleterminal.dev/nt3d/widget` | `nt3dwidget` | The Bubble Tea widget, with Kitty graphics or colored glyphs |
| `nimbleterminal.dev/nt3d/mesh` | `nt3dmesh` | Reading and writing STL, OBJ, 3MF, and PLY |
| `nimbleterminal.dev/nt3d/mesh/gltf` | `nt3dgltf` | Reading glTF 2.0 (`.gltf` and `.glb`) |
| `nimbleterminal.dev/nt3d/mesh/formats` | `formats` | Imported for its side effects: every optional mesh format, and WebP textures |

## A first program

This shows an orange ball in the terminal. Drag to orbit it, scroll to zoom, and press `q` to quit.

```go
package main

import (
	"image/color"
	"log"

	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/nt3d"
	nt3dwidget "nimbleterminal.dev/nt3d/widget"
)

type app struct{ view *nt3dwidget.Model }

func (a app) Init() tea.Cmd { return a.view.Init() }

func (a app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return a, tea.Sequence(a.view.Dispose(), tea.Quit)
		}
	case tea.WindowSizeMsg:
		return a, a.view.SetSize(msg.Width, msg.Height)
	}
	_, cmd := a.view.Update(msg)
	return a, cmd
}

func (a app) View() tea.View {
	v := a.view.View()
	v.AltScreen = true
	return v
}

func main() {
	scene := nt3d.NewScene()
	ball, err := nt3d.BuildSphere(nt3d.Sphere{
		Radius: .6,
		Color:  color.RGBA{R: 230, G: 120, B: 40, A: 255},
	})
	if err != nil {
		log.Fatal(err)
	}
	if _, err := scene.SetGeometry("ball", ball); err != nil {
		log.Fatal(err)
	}

	view := nt3dwidget.New(scene, 80, 24)
	defer view.Close()
	if _, err := tea.NewProgram(app{view}).Run(); err != nil {
		log.Fatal(err)
	}
}
```

A few things are worth noticing:

- **The widget draws only the 3D view.** Quitting, headers, and status lines are yours; here the `app` model adds them around it.
- **Clean up in two steps.** `Dispose` removes the widget's images while Bubble Tea is still running, and `Close` releases the GPU after it exits.
- **It works everywhere.** In a terminal with Kitty graphics (Kitty, Ghostty, WezTerm) you see a rendered image; elsewhere, colored glyphs. Without a GPU it renders in software. Press `g` to switch between Kitty and glyphs.

## Rendering without a terminal

The widget is optional. To render a picture directly, make a frame from the scene and a camera, and snapshot it:

```go
camera := nt3d.DefaultCamera()
frame, err := scene.Frame(camera, 960, 640)
if err != nil {
	log.Fatal(err)
}
gpu := nt3d.NewGPURenderer()
defer gpu.Close()
img, mode, err := nt3d.Snapshot(frame, nt3d.WebGPU, gpu)
// img is an image.Image; mode says whether WebGPU drew it or software did.
```

## Where next

- [Scenes](../scenes/) explains nodes, transforms, and sharing geometry.
- [Shapes and geometry](../shapes/) and [Mesh files](../meshes/) are the two ways to get something to look at.
- [The widget](../widget/) covers controls, picking, embedding, and what happens when the GPU fails.
- The [demos](../demos/) are complete programs, and the quickest way to see what nt3d can do.
