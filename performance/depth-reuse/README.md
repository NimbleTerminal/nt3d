# Bounded software depth reuse

Measured 2026-10-18 on Apple M5 Max, darwin/arm64, Go 1.27.2.

## Outcome

A retained nt3d software renderer avoids **819,200 bytes and one allocation per
640×320 frame** after warmup. ntcharts3d now retains one dedicated software
renderer across scheduled frames, snapshots, and GPU fallback. Color images
and public `SoftwareResult.Depth` remain independently owned.

This is an allocation improvement, **not a general latency improvement**.
Camera Snapshot median moved from 6.187 to 6.257 ms; texture replacement from
12.118 to 12.187 ms. Direct camera presentation fell from 7.599 to 6.918 ms,
but shm bars rose from 2.531 to 2.871 ms. Resize-heavy rendering was slightly
slower. These short local samples do not establish a systematic speedup or
interactive FPS improvement. All ranges, including slower cases, appear below.

The allocation reduction is useful for sustained camera motion: approximately
23.4 MiB/s less allocation at 30 frames/s, in exchange for 0.78125 MiB of
persistent depth storage at this raster size. The synchronization and ownership
rules are small and bounded; retaining the adapter also protects old engines.
Applications interested only in latency should measure their own workloads.
Creating a new software renderer for every frame gains nothing and now costs
one extra small renderer allocation; keep it alive to benefit. This change
integrates persistence into ntcharts3d, not nt3d's widget scheduling or one-shot
`Snapshot` helper.

## Ownership and lifetime

- The nt3d renderer owns only private depth scratch; output RGBA pixels are new
  on every call. `RenderSoftware` stays on the allocating path, including its
  independently owned exposed depth and supersampled nearest-depth reduction.
- Every active depth element is reset to `noDepth` on every draw, even when only
  the raster shape changes. Invalid frames do not allocate or alter scratch.
- Retention is capped at 1,048,576 float32 values (**4 MiB**). Requests above that
  size allocate temporary depth and retain the previous interactive scratch.
  Smaller requests reuse capacity unless they need less than one quarter of
  it, in which case scratch shrinks to exactly the requested size. Growth is
  exact, not geometric. The policy counts the enlarged supersampling raster.
- Image-only supersampling avoids allocating/reducing a depth result that no
  caller receives; color reduction, overlays and linear-light behavior agree
  with the allocating path.
- `Render`/`Close` serialize internally. Close waits for any current render,
  clears scratch, is idempotent, and rejects subsequent renders. Frames must
  stay immutable while rendering.
- ntcharts3d owns a dedicated software adapter independently of GPU lifetime.
  Snapshot and scheduled render commands share it. Its lock also handles the
  currently pinned, older engine. Close waits for active work and rejects late
  queued work without locks across callbacks. Model mutation/frame construction
  still belongs to the event loop; Close runs after that loop exits. Dispose
  retains its existing cancellation/presentation contract and does not preempt
  a synchronous render. Stale results remain rejected by the existing sequence
  checks.

Observed retained depth capacity in the regression test:

| Point | Retained depth bytes |
| --- | ---: |
| After 640×320 | 819,200 |
| After a 2048×1024 snapshot | 819,200 |
| After shrinking to 32×32 | 4,096 |
| After Close | 0 |

These are exact renderer-owned depth capacities, not process RSS or total heap.
The large snapshot's temporary 8 MiB depth and independently returned 8 MiB image
are additional peak allocations; the retention cap is not a peak-memory cap.
See [retention/ownership test log](final-root-tests.txt).

## Controlled comparison

Baseline nt3d is `2111d0c31b625272bacc43e9c3d0fe687eda45fb`, captured before
this change. Baseline ntcharts3d is `f5ee68cb05686221c8b6056163606c42819c6ba0`.
Both chart variants use the same explicit camera-field mapping: direct struct
conversion no longer compiles with the engine's new glTF camera fields.
The after variant adds only depth reuse and dedicated software lifetime.
No sampling, mipmap, anisotropy, transport or quality defaults changed.

Temporary workspaces select these copies; production module pins are unchanged.
[Selected dependency lists](modules-before.txt) [match exactly](modules-after.txt).
The benchmark fixture is ntcharts3d's existing deterministic 512×512 map,
4× anisotropy, perspective camera and 8×4 bars control. [Source identifiers](sources.json)
and the [chart integration patch](ntcharts3d.patch) record the tested changes.

All runs were sequential, after builds and tests. Each row reports the median
and range of three samples. Lifetime and snapshot benchmarks use 72 operations;
large-small uses nine renders per operation (one 2048×1024, then eight 640×320).
Resize rotates through 640×320, 320×640, 1280×640 and 320×160. Steady camera motion
covers a full rotation in 72 steps. Fresh-texture snapshots include rotation;
presentation texture replacement keeps the camera fixed.

Presentation glyph/direct use 72 operations; shm uses three separate processes
of 20 operations per workload to stay below the sink's pending-shm limit. Glyph
is 80×40; direct/shm are 640×320. No resolution adaptation or renderer/transport
fallback occurred. Burst issues four requests, coalesces two, and completes two
frames per operation with 1 ms simulated delay per raw output write.

`PresentedMsg` measures application output completion, **not terminal display
acknowledgement**. The harness includes frame construction, rendering, encoding,
ordered output into `io.Discard`, and View. It excludes terminal painting, screen
diffing, and real OS output backpressure. Do not compare glyph versus Kitty as
if they had the same raster workload.

[Before raw measurements](before.txt) · [After raw measurements](after.txt).
Bytes and allocation columns below are medians per operation. Presentation
allocation counts include asynchronous harness work and vary slightly by run.

## Renderer lifetime (640×320)

| Workload | Before ms/op (range) | After ms/op (range) | B/op before → after | Allocs/op before → after |
| --- | ---: | ---: | ---: | ---: |
| camera/retain=false | 6.081 (6.005–6.118) | 6.196 (6.158–6.438) | 1,638,503 → 1,638,549 | 5 → 6 |
| camera/retain=true | 6.204 (6.188–6.209) | 6.109 (6.082–6.219) | 1,638,501 → 819,299 | 5 → 4 |
| bars/retain=false | 2.180 (2.177–2.206) | 2.335 (2.303–2.426) | 1,671,275 → 1,671,371 | 37 → 38 |
| bars/retain=true | 2.136 (2.083–2.146) | 2.099 (2.088–2.188) | 1,671,272 → 852,072 | 37 → 36 |

## Snapshot workloads

| Workload | Before ms/op (range) | After ms/op (range) | B/op before → after | Allocs/op before → after |
| --- | ---: | ---: | ---: | ---: |
| camera | 6.187 (6.171–6.195) | 6.257 (6.090–6.408) | 1,638,915 → 819,715 | 8 → 7 |
| texture | 12.118 (11.923–12.249) | 12.187 (12.158–12.316) | 4,087,334 → 3,268,129 | 39 → 38 |
| resize | 9.778 (9.768–9.828) | 9.998 (9.965–10.018) | 2,560,518 → 2,344,338 | 8 → 7 |
| large-small | 86.813 (85.054–87.169) | 84.522 (84.009–86.006) | 29,889,079 → 23,335,452 | 72 → 64 |

## Application presentation completion

| Workload | Before ms/op (range) | After ms/op (range) | B/op before → after | Allocs/op before → after |
| --- | ---: | ---: | ---: | ---: |
| glyph/camera | 0.205 (0.205–0.295) | 0.200 (0.200–0.287) | 226,947 → 213,383 | 55 → 54 |
| glyph/texture | 5.997 (5.988–6.590) | 6.015 (6.009–6.177) | 2,676,304 → 2,662,699 | 87 → 86 |
| glyph/bars | 0.164 (0.149–0.195) | 0.149 (0.146–0.150) | 209,824 → 196,214 | 86 → 85 |
| glyph/burst | 0.363 (0.363–0.364) | 0.349 (0.346–0.353) | 454,181 → 426,950 | 113 → 111 |
| direct/camera | 7.599 (7.505–7.985) | 6.918 (6.899–6.992) | 3,689,456 → 2,870,043 | 209 → 207 |
| direct/texture | 12.639 (12.638–12.937) | 12.508 (12.508–12.530) | 5,980,406 → 5,160,957 | 211 → 209 |
| direct/bars | 2.894 (2.880–2.903) | 2.818 (2.804–2.818) | 3,456,531 → 2,636,799 | 183 → 181 |
| direct/burst | 17.155 (16.550–18.282) | 16.221 (16.082–16.256) | 7,379,110 → 5,740,052 | 424 → 420 |
| shm/camera | 6.657 (6.534–6.691) | 6.448 (6.380–6.485) | 1,734,881 → 915,339 | 165 → 162 |
| shm/texture | 12.519 (12.448–12.577) | 12.209 (12.174–12.299) | 4,184,275 → 3,364,812 | 207 → 205 |
| shm/bars | 2.531 (2.521–2.565) | 2.871 (2.835–3.169) | 1,767,211 → 947,840 | 188 → 187 |
| shm/burst | 16.250 (15.245–16.447) | 15.095 (15.068–15.144) | 3,474,096 → 1,834,683 | 413 → 404 |

## Validation and remaining gaps

- nt3d `task ci`: formatting, full race suites, vet, native and browser builds
  passed ([log](nt3d-ci.txt)). Final expanded depth tests passed under race
  ([log](final-root-tests.txt)). Native `task test-gpu` passed ([log](gpu.txt)).
- ntcharts3d `task ci` in a temporary workspace containing local nt3d, charts,
  gallery and transport modules passed: tidy-diff, checksum verification, race
  tests, vet and native/WASM builds ([log](charts-ci.txt)). Its native GPU suite
  passed ([log](charts-gpu.txt)). Root tests also pass with published dependency
  pins ([log](charts-pinned-tests.txt)); those pins do not yet enable reuse.
- Deterministically gated concurrency tests cover scheduled rendering overlapping
  Snapshot, Close waiting for an active render, queued commands after Close,
  repeated Close, GPU fallback cleanup, invalid frames, and retained output.
  Existing stale/cancelled/coalesced presentation tests pass under race
  ([integration log](charts-race.txt), [final tests](final-charts-tests.txt)).
- 648 before/after frames (maps, bars, lines and points, 72 camera poses and three
  raster shapes) match byte-for-byte via aggregate SHA-256
  `99f7a5f2cea9fc8753cfc9ef4bff0f0d76926371accc1d3ac4a7645ca4e1d62e`:
  [before](pixels-before.txt), [after](pixels-after.txt).
  nt3d tests additionally cover transparency, clipping, texture repeat, outlines,
  overlays, SSAA/linear-light reduction, and independently owned public depth.
- mapview3d's race tests pass in an isolated consumer copy ([log](consumer-tests.txt)).
- Browser execution, Node/TinyGo timing, actual terminal display latency and
  emulator shared-memory consumption were **not measured**. Browser compilation
  and the native output sink do not establish these results.

## Reproduce and release coordination

Create disposable source copies of the revisions above and the changed trees.
Apply the explicit `engineCamera` mapping from `ntcharts3d.patch` to the baseline
chart too. Existing `presentation_bench_test.go` is unchanged. The script installs
only the identical [snapshot benchmark](depth_bench_test.go.txt) and
[pixel oracle](pixel_oracle_test.go.txt) into the supplied chart copies and makes
temporary workspaces without modifying module pins:

```sh
python3 performance/depth-reuse/reproduce.py \
  --nt3d-before /tmp/nt3d-before --nt3d-after /tmp/nt3d-after \
  --charts-before /tmp/charts-before --charts-after /tmp/charts-after \
  --output /tmp/depth-results
```

Run with normal Go cache and native shared-memory permissions, without competing
builds or benchmarks. All benchmark dimensions, transports and adaptation checks
are enforced by the harness; a failed check invalidates that sample.

No push, tag, release, or production dependency-pin change was made.
Release nt3d with this change first, then select that version in ntcharts3d and
its example modules, release ntcharts3d, and update the mapview3d consumer pin.
The chart adapter remains compatible with its current published nt3d pin, but
allocation savings require the new engine as well as the persistent adapter.
