---
title: "Development"
weight: 110
---

# Development

## Setting up

nt3d's own dependencies, ntcanvas and ntgpuimage, are published and come through Go modules. ntpicture isn't published yet, so for now nt3d is developed beside a checkout of it, joined by a Go workspace:

```sh
git clone https://github.com/NimbleTerminal/nt3d
git clone https://github.com/NimbleTerminal/ntpicture
cd nt3d
task workspace   # copies go.work.example to go.work, which git ignores
task ci
```

CI checks out ntpicture at the revision `go.mod` pins, so that revision must be pushed before CI can fetch it.

The library is one module. The demos in `examples/terrain` are a second module, so that go-booba and the Bubble Tea fork the browser needs never become requirements of the library.

## Tasks

| Task | Does |
| --- | --- |
| `task ci` | format check, race tests, vet, native builds, and the complete WebAssembly gallery build |
| `task test-gpu` | the GPU tests, which need a real WebGPU adapter and fail without one |
| `task build-web`, `task serve` | build the browser gallery, and serve it at http://localhost:8768 |
| `task terrain`, `task fur`, … | run a [demo](../demos/) |
| `task models-generate` | regenerate the sample models the models demo embeds |
| `task tapes` | record the demos' GIFs and the gallery's posters |
| `task docs:hugo:serve` | serve this guide while editing it |

GPU tests check, among other things, that changing one node uploads only that node, that buffers survive other nodes coming and going, and that old frames replay to identical pixels. The GPU path is tested on Metal; ntgpuimage itself has been checked on Vulkan, but these demos haven't. CI compiles the WebAssembly build.

## Recording the demos

The gallery's posters are frames of the terminal demos, Kitty graphics and all, recorded by [ntrecord](https://github.com/NimbleTerminal/ntrecord) from the tapes in `tapes/`. `task tapes` records every demo, or `task tapes -- fur forest` just some. Each tape records a GIF to share and a few poster candidates into `dist/tapes/`, and `tapes/finish.sh` copies the first that shows the scene to `web/assets/`, and trims and compresses the GIF. Only the posters are committed. Recording needs ntrecord, ffmpeg, gifsicle, and a GPU.

## This guide

The guide is a [Hugo](https://gohugo.io/) site in `docs/hugo`, using the [hugo-book](https://github.com/alex-shpak/hugo-book) theme as a git submodule. Fetch it once with `git submodule update --init`, then `task docs:hugo:serve` serves the guide with live reload (it needs Hugo extended). `task docs:hugo:build` builds it into `docs/hugo/public`. The demo posters come straight from `web/assets`.

## Where it came from

nt3d was extracted from [ntcharts3d](https://github.com/NimbleMarkets/ntcharts3d), keeping the engine and leaving the charts behind. [MIGRATION.md](https://github.com/NimbleTerminal/nt3d/blob/main/MIGRATION.md) describes how ntcharts3d could later be rebuilt on it. Open work is in [ROADMAP.md](https://github.com/NimbleTerminal/nt3d/blob/main/ROADMAP.md).
