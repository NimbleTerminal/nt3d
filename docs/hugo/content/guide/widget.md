---
title: "The widget"
weight: 90
---

# The widget

`nt3dwidget` puts a scene in a [Bubble Tea](https://github.com/charmbracelet/bubbletea) program. It handles the camera and mouse, schedules renders so slow frames never pile up, falls back when the GPU can't help, and shows the result as Kitty graphics or colored glyphs through [ntpicture](https://github.com/NimbleTerminal/ntpicture).

```go
view := nt3dwidget.New(scene, 80, 24)
defer view.Close()
```

The widget draws only the 3D view. Headers, footers, and quitting belong to your program; [Getting started](../getting-started/#a-first-program) shows a complete one.

## Controls

| Input | Does |
| --- | --- |
| drag | orbit |
| shift-drag | pan |
| wheel, `+`, `-` | zoom |
| arrows, `h` `j` `k` `l` | orbit |
| `c` | fit the scene in view (`Fit()`) |
| `r` | turn automatic rotation on or off |
| `o` | switch between perspective and orthographic |
| `g` | switch between Kitty graphics and glyphs |

A click sends your program a `PickMsg` saying what was hit; see [Picking](../picking/).

## Working with it

- **Forward everything.** Pass terminal events, including size and cell-size changes, on to the widget, even while it shows wireframe, and run every command it returns.
- **After editing the scene**, call `Refresh()` and run its command, so the widget renders again. Setters (`SetCamera`, `SetLighting`, and the rest) work the same way. A command dropped by mistake doesn't stall the widget; it only delays the frame until the widget's next message.
- **Mouse zones.** When the widget is part of a larger view, share one zone manager with it (`WithZoneManager`) and have your program scan the complete view.
- **Status.** `FrameRate()` is how many frames the widget showed over the last second, and `RenderTime()` how long the last one took to render. `PictureStatus()` tells you about the image on screen.

## Options

- `WithRenderMode` chooses WebGPU, software, or wireframe; `SetRenderMode` changes it.
- `WithCamera`, `WithBackground`, `SetLighting`, and `SetFog` set up the look.
- `WithSupersample(n)` smooths GPU frames. Software frames aren't supersampled, since they're already trading resolution for speed.
- `WithPostProcess(fn)` runs `fn(*image.RGBA)` on every frame before it's shown: blur, vignettes, color grading, or dithering are ordinary Go. It runs on the render goroutine, one frame at a time, and may change pixels in place. The [fur demo](../demos/#fur) shows supersampling and post effects (`x`, `e`).
- `WithFrameDecorator(fn)` adds [decorations](../rendering/#decorations) to each frame.

## When rendering is slow or fails

Without a usable GPU, the widget renders in software. When software frames are slow, it lowers their resolution, and if that isn't enough, switches to wireframe.

When a working GPU fails on a frame, perhaps running out of memory on a huge model, the widget stops and shows the error rather than quietly switching to a much slower renderer. `Failed()` returns the error, and your program receives a `RenderFailedMsg`, so it can load less, show something else, or choose software. Rendering resumes when the scene changes or a mode is set. To switch to software for good instead, use `WithGPUFailure(nt3dwidget.FallBackOnGPUFailure)`.

## Leaving

There are two ways to quit:

- **Clean up.** Run `Dispose()`'s command before quitting, `tea.Sequence(view.Dispose(), tea.Quit)`, to remove the widget's images, then call `Close()` after the program returns to release the GPU.
- **Leave the picture.** For a viewer that quits leaving its last frame on screen, run `tea.Sequence(view.Keep(), tea.Quit)` instead, then `Close()`. `Keep` stops rotation and pending renders, so nothing newer appears. The image stays only on the main screen, since the alternate screen is cleared on exit.

Keep forwarding messages to a disposed widget while it's still in the event loop.

## Several widgets

Each widget opens its own terminal session. To show several on one terminal, create one session and pass it to each with `WithTerminalSession(session)`. Your program then owns the session, and closes it after disposing of the widgets.

## Terminals and browsers

Kitty graphics are sent directly, or through shared memory where the terminal and program share a machine; ntpicture chooses, and paces the stream so the terminal keeps up. Kitty graphics work over SSH. Glyphs use one pixel column and two pixel rows per character cell, and work in any terminal with color.

The same programs run in a browser through [go-booba](https://github.com/NimbleMarkets/go-booba), as the [demo gallery](../demos/#in-a-browser) does.
