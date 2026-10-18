# Software texture sampling: mapview3d

Measured on 2026-10-18, Apple M5 Max, darwin/arm64, Go 1.27.2.
These are native CPU snapshot times, not terminal FPS. Resolution, anisotropy,
mipmap generation, camera paths, color spaces, and image ownership are unchanged.

## Revisions and isolation

- nt3d baseline: `afc39e3ad60110c396c34d6f168b8c64eda56308` (the checkout before
  this optimization, including outlines).
- Published comparison: nt3d `v0.2.0`, ntcharts3d `v0.3.1`, ntgpuimage `v0.3.0`.
- Local comparisons retain the published ntcharts3d and ntgpuimage versions,
  replacing **only nt3d** with an isolated baseline, sampling-only snapshot,
  or this implementation. `GOWORK=off` prevents incidental workspace replacements.
- Consumer repository HEAD: `b3976974a3b9ccf188879b82635acbdafe0fea46`.
  Its mapview3d harness and examples module were **uncommitted** at measurement
  time. Tests ran in a temporary copy of the consumer's working files; no sibling
  files, pins, or Git state were changed.

SHA-256 of the copied consumer inputs:

| File | SHA-256 |
| --- | --- |
| `examples/mapview3d/main.go` | `051f1433286d2ef1ffbab03afbd273ce0cb2672f710f6f6d9b3e3c43b1bf4ed7` |
| `examples/mapview3d/benchmark_test.go` | `505f2c7e79c7ae358cf5a899868432abf9ad1c0deaa0f81844f16c239b26a59d` |
| `examples/go.mod` | `ad7a04962c777f1ee52a51132edfd718bb261e49ad69a66a617f78f3d1a162dd` |
| `examples/go.sum` | `97bf7c542dc3454fda5c0f8a1eaf6081de68d43eada2534dbf17a629dcf93750` |

## Two independent changes

1. `texture.go`: calculate each axis's two addressed texels once, specialize
   repeat/clamp addressing, and unroll four taps and RGB channels. Keep the tap
   order, float32 weights, alpha division, mip blending, and anisotropic sampling
   algorithm. A shared 1024-byte data decode table replaces repeated divisions
   for normal/data maps; it uses exactly the original float32 divisions.
2. `texture_encode.go` and the guarded branch in `render_software.go`: replace
   final encoded-byte conversion only for opaque, unlit, untinted, non-fogged,
   non-hull textured output. Cutouts still test alpha first. The existing encode
   LUT estimates a byte; a shared 1020-byte table of exact float32 transition
   boundaries corrects it. Intermediate encoding for tinting, lighting, fog,
   transparency, and linear frames retains the original float path.

White vertex colors can interpolate a fraction away from one. The fast path
also requires the interpolated white multiplier to equal exactly one; otherwise
it retains the original conversion. An initial prototype omitted this guard and
failed consumer hashes despite passing isolated encoder tests; it was rejected.

There is no per-texture memory increase, framebuffer pooling, lifecycle/API
change, mipmap change, or new dependency. Both changes together add 2044 bytes
of shared lookup data. The boundary table is computed once during initialization,
not per texture/frame. This is a pixel-identical optimization, not a color
approximation.

## Measurement protocol

The existing consumer benchmark makes a deterministic opaque 512×512 map,
warms one snapshot outside timing, then advances camera azimuth in five-degree
steps. Every timed case runs exactly 144 operations (two full rotations), three
sequential repetitions. Baseline, sampling-only, and combined variants run
sequentially, without concurrent tests or benchmarks. Texture replacement creates
a fresh immutable texture each operation, so it also includes unchanged mipmap
construction. Allocation figures are per operation, not retained memory.

Isolated `BenchmarkSampling` uses identical UV/derivative sequences, warmed mip
chains, 1,000,000 operations per case, and three repetitions for old and new.
Its old build is the baseline archive plus the same benchmark test source.

## Results

Times are median **ms/op (min–max)** over three equal-length runs.

| Consumer workload | Baseline | Sampling only | Sampling + conversion |
| --- | ---: | ---: | ---: |
| Camera 320x160 / anisotropy 1 | 1.708 (1.698–1.718) | 1.547 (1.531–1.563) | 1.153 (1.148–1.176) |
| Camera 320x160 / anisotropy 4 | 2.308 (2.299–2.397) | 1.910 (1.904–1.911) | 1.610 (1.608–1.616) |
| Camera 640x320 / anisotropy 1 | 6.555 (6.554–6.718) | 6.140 (5.986–6.274) | 4.560 (4.530–4.611) |
| Camera 640x320 / anisotropy 4 | 8.861 (8.850–8.866) | 7.795 (7.782–7.812) | 6.241 (6.231–6.271) |
| Camera 1280x640 / anisotropy 1 | 25.973 (25.404–26.144) | 24.059 (24.018–24.120) | 17.745 (17.700–17.791) |
| Camera 1280x640 / anisotropy 4 | 28.021 (27.144–28.902) | 24.615 (24.511–24.674) | 18.846 (18.748–18.856) |
| Fresh texture + 640×320 render / anisotropy 4 | 14.985 (14.677–15.038) | 13.715 (13.711–13.729) | 11.874 (11.860–11.919) |

At 640×320 / 4×, sampling alone reduces median frame time by **12%**;
the guarded conversion reduces the remaining time by another **20%**.
Combined: **8.861 → 6.241 ms/frame, about 30% less time**. Replacement plus
rendering improves about **21%**. Camera frames retain **8 allocations**
and about **1.56 MiB** at 640×320; replacement retains **39 allocations** and
about **3.90 MiB**. Across all variants the byte figures differ only by small
amortized harness/runtime overhead (see raw results).

Initial reproduction measured published v0.2.0 at 9.26–9.46 ms/frame and the
local baseline at 8.92–9.14 ms/frame. A preliminary paired sampling experiment
measured 8.81–8.91 → 7.17–7.79 ms/frame. The table above is a later complete
sequential matrix; ordinary run-to-run variation means these are local ranges,
not latency percentiles or a cross-machine performance guarantee. The smaller
address-only prototype improved the consumer only about 3%; full tap unrolling
made the improvement repeatable and worth keeping.

Isolated footprint samples, **median ns/op (min–max)**; all cases allocate
**0 bytes / 0 allocations**:

| Texture / addressing / anisotropy | Baseline | Optimized |
| --- | ---: | ---: |
| color / clamp / aniso1 | 55.0 (54.0–63.4) | 31.8 (30.4–33.0) |
| color / clamp / aniso4 | 186.5 (185.7–192.3) | 112.8 (111.9–112.9) |
| color / clamp / aniso16 | 386.3 (384.5–387.4) | 260.7 (257.9–261.4) |
| color / repeat / aniso1 | 57.1 (57.1–57.7) | 32.5 (32.5–33.4) |
| color / repeat / aniso4 | 208.7 (208.2–209.1) | 115.0 (114.0–115.7) |
| color / repeat / aniso16 | 448.5 (440.2–453.9) | 262.3 (262.2–262.8) |
| data / clamp / aniso1 | 54.1 (54.1–54.3) | 30.6 (30.6–31.4) |
| data / clamp / aniso4 | 197.0 (196.5–197.2) | 110.4 (110.2–111.2) |
| data / clamp / aniso16 | 424.4 (419.1–425.6) | 262.9 (262.2–263.5) |
| data / repeat / aniso1 | 63.1 (60.4–63.1) | 32.4 (32.3–33.0) |
| data / repeat / aniso4 | 232.8 (231.2–233.2) | 113.7 (113.4–113.9) |
| data / repeat / aniso16 | 499.4 (498.9–502.1) | 264.6 (263.4–266.4) |

The isolated final-byte encoder measured **14.17–18.17 ns** for the original
float/power path versus **1.228–1.229 ns** for the corrected lookup, with no
allocations. This does not imply that every textured pixel qualifies for it.

Raw evidence: [consumer baseline](software-sampling/consumer-before.txt),
[sampling-only](software-sampling/consumer-sampling.txt),
[combined](software-sampling/consumer-combined.txt),
[isolated before](software-sampling/sampling-before.txt),
[isolated after](software-sampling/sampling-after.txt),
[encoder](software-sampling/encoding.txt), and
[published baseline](software-sampling/published-baseline.txt).

### Remaining bottlenecks

The original 432-frame profile measured 8.874 ms/frame; the final profile
measured 6.047 ms/frame (profiled timings are separate from the table).
Bilinear sampling drops from about **39% to 24%** cumulative CPU samples;
footprint sampling from **50% to 42%** (overlapping, not additive). Rasterization,
UV interpolation, footprint/logarithm calculations, and the retained float
conversion remain substantial. The latter still takes about **17%** of final
samples because preserving interpolated-white rounding requires fallback.
Framebuffer/depth allocation remains about eight bytes per output pixel.

Profiles: [before](software-sampling/profile-baseline.txt),
[after](software-sampling/profile-final.txt). No framebuffer reuse was introduced;
that needs the separate renderer lifecycle/ownership work. Fresh-texture
mipmap construction also remains unchanged, pending ntgpuimage's separate work.

## Correctness

- Frozen baseline samplers are test oracles for clamped/repeated addressing,
  negative/out-of-range UVs, edges/corners, 1-pixel dimensions, non-square and
  strided images, fractional mip transitions, oblique footprints, anisotropy
  1/4/16, linear-data/normal textures, RGB, and alpha. Float results match exactly.
- Concurrent readers exercise lazy mip initialization under the race detector;
  existing immutable texture ownership tests remain in the suite.
- Encoder tests check every byte transition and coarse-LUT transition plus
  neighboring float32 values, a 2^20-point uniform grid, and 2^20 additional
  deterministic float bit patterns. Maximum channel error: **0**.
- Output tests compare the optimized path against the original float path using
  mathematically neutral white tinting. They cover cutouts, transparency, fog,
  lighting, normal maps, outlines, both frame color spaces, supersampling, and
  two full camera rotations. Non-neutral tinting keeps the unchanged float path.
- The actual consumer's output was hashed for 216 frames per anisotropy setting
  (1, 4, 16), using three full rotations at 640×320: **648 matching RGBA frames,
  132,710,400 pixels, zero changed pixels**, for sampling-only and combined changes.
  [Comparison digests](software-sampling/pixel-comparison.txt).

Validation passed:

- `task ci`: formatting, race tests for the library and demos, vet, all native
  builds, all WASM builds, and web asset assembly (Taskfile's Go 1.26.8 toolchain).
- `go test -mod=readonly -tags webgpu -run '^TestGPU' -count=1 .`
- `go -C examples/demos test -mod=readonly -tags webgpu -run '^TestGPU' -count=1 ./internal/demo`

Both hardware suites ran on the native GPU; no hardware checks were skipped.
In a temporary copy, removing the interpolated-white guard makes
`TestDirectTextureMovingCamera` fail at the first camera angle (-90°), confirming
that the regression test detects the rejected shortcut. The guarded checkout
passes. The plain Go benchmark and hardware-test commands use Go 1.27.2.

## Reproduction

Copy the consumer into a temporary directory, retaining its root module and
`examples` layout. Do not change its working tree. Archive baseline nt3d with
`git archive afc39e3` into another temporary directory. For the sampling-only
variant, copy that baseline and replace only `texture.go` with this version.
Copy the consumer's `examples/go.mod` and `go.sum` to `baseline.mod/.sum`,
`sampling.mod/.sum`, and `local.mod/.sum` **inside the temporary examples folder**.
Add a `replace nimbleterminal.dev/nt3d => /absolute/path/to/variant` to each
modfile. Keep every other dependency unchanged.

The exact [consumer benchmark source](software-sampling/consumer_benchmark_test.go.txt)
and [pixel-hash test](software-sampling/consumer_pixel_oracle_test.go.txt) are saved
beside the raw data. To repeat the hash check, copy the latter to
`mapview3d/pixel_oracle_test.go` in the temporary consumer, then run:

```sh
NT3D_PIXEL_HASHES=/tmp/before.hashes GOWORK=off go test -modfile=baseline.mod \
  -run '^TestConsumerPixelOracle$' -count=1 ./mapview3d
NT3D_PIXEL_HASHES=/tmp/after.hashes GOWORK=off go test -modfile=local.mod \
  -run '^TestConsumerPixelOracle$' -count=1 ./mapview3d
cmp /tmp/before.hashes /tmp/after.hashes
```

From the temporary consumer's examples folder, run each benchmark variant sequentially:

```sh
GOWORK=off go list -modfile=baseline.mod -m all
GOWORK=off go test -modfile=baseline.mod -run '^$' \
  -bench '^BenchmarkMap3D(Camera|TextureReplacement)$' \
  -benchmem -benchtime=144x -count=3 ./mapview3d
# Repeat with sampling.mod, then local.mod.

GOWORK=off go test -modfile=local.mod -run '^$' \
  -bench '^BenchmarkMap3DCamera/640x320/aniso4$' -benchtime=432x \
  -cpuprofile=/tmp/map3d-after.pprof -o /tmp/map3d-after.test ./mapview3d
go tool pprof -top /tmp/map3d-after.pprof
```

In nt3d, run the isolated sampling/encoder benchmarks:

```sh
go test -run '^$' -bench '^BenchmarkSampling$' \
  -benchmem -benchtime=1000000x -count=3 .
go test -run '^$' -bench '^BenchmarkOpaqueEncoding$' \
  -benchmem -benchtime=1000000x -count=3 .
```

## Consumer release update

These changes are **unreleased** and are not in nt3d v0.2.0. No tags or pushes
were made for this work. After the next nt3d version is published, update
`nimbleterminal.dev/nt3d` in the consumer's examples module and tidy its sums.
The existing ntcharts3d v0.3.1 API can use the optimization without a wrapper
code change. ntgpuimage can remain at v0.3.0; its separately planned mipmap
optimization is not required and is not included in these measurements.
