# Prompt: global LOD bias and hysteresis for `Scene.SetLOD`

Add a scene-wide LOD distance bias and a per-node hysteresis band to `Scene.SetLOD`,
so applications that drive detail from a frame-time budget can use nt3d's built-in
LOD tiers instead of swapping geometry themselves.

## Why

`Scene.SetLOD` (scene_lod.go) selects a tier each frame in `sceneNode.selectLOD`.
It measures the distance from `Frame.Eye` to the node's world-space reference
bounds (`aabbDistance`) and picks the first level whose `MaxDistance` covers it.
Tiers stay resident and switching retains GPU buffers, so a switch costs nothing.
That design is right, but two things keep a large streamed scene from adopting it:

1. **No global bias.** A frame-time budget scales every LOD distance continuously,
   for example 1.0 → 0.9 → 0.81, to shed detail when frames run long and win it
   back when they are short. Distances are fixed per node today. Changing them means
   calling `SetLOD` again on every node, which rebuilds level copies and pick indexes
   for thousands of nodes per budget step.
2. **No hysteresis.** A node whose distance sits on a tier boundary flips between
   tiers on successive frames as the camera drifts by centimetres. That shows as
   popping. Applications that do their own swapping use a band (for example 5%)
   and keep the current tier until the distance clears the boundary by that margin.

An application today has to keep its own swapping: one geometry per node plus a
per-node "current zone", hysteresis, and a capped number of `SetNodeGeometry` swaps
per frame. That works, but it duplicates what `SetLOD` already does better (resident
tiers, shared buffers, BVH reuse).

## What to add

### 1. Scene-wide LOD bias

- `func (s *Scene) SetLODBias(f float32) error` and `func (s *Scene) LODBias() float32`.
- Every tier's `MaxDistance` is effectively `MaxDistance * f` when selecting,
  including the final level's hide-beyond distance when it is finite.
- `f` must be finite and > 0. The default is 1. Invalid values return an error and
  leave the bias unchanged.
- Setting the bias must be O(1). Do not touch nodes or rebuild anything; the bias
  applies on the next `Scene.Frame`.
- Apply the multiplication in float64 and store float32, so results are
  byte-identical across amd64 and arm64 (no FMA drift), as the rest of nt3d does.

A per-`Frame` field (e.g. `Frame.LODBias`) is an acceptable alternative if it fits
the renderer better. The requirement is that one call changes selection for the
whole scene without per-node work.

### 2. Hysteresis

- `func (s *Scene) SetLODHysteresis(band float32) error` and `LODHysteresis() float32`.
  `band` is a fraction in [0, 0.5], default 0, which keeps today's behaviour exactly.
- Each LOD node remembers the tier it drew last frame. With band `b` and effective
  boundary distance `D` between tier i and i+1:
  - Moving to a coarser tier (i → i+1) happens only when distance > `D * (1 + b)`.
  - Moving to a finer tier (i+1 → i) happens only when distance < `D * (1 - b)`.
  - A node with no remembered tier (first frame, or after `SetLOD`, `SetVisible`
    false→true, or a bias change) selects without hysteresis.
  - The final level's hide-beyond cutoff uses the same band.
- Selection stays deterministic: the same sequence of cameras and calls gives the
  same tiers on every machine and renderer. Store the remembered tier in the scene
  node, not in a renderer, so GPU and software agree and `Scene.Frame` stays the
  single place selection happens.
- A bias change counts as a reset (no hysteresis on the next frame) so a budget step
  takes effect at once rather than being held back by the band. If you think holding
  it is better, document the choice and test it.

### 3. Stats

Extend `FrameStats` with a per-tier drawn count, e.g. `LODTierInstances [8]int` with
tiers past 7 folded into index 7, or a slice. Applications can then show "tiles at
full / mid / far detail" in their HUD and verify a bias change took effect.

## Out of scope

- Lazy tier construction (building a tier only when first selected). Worth a
  separate proposal: large terrains would save memory by not building far tiers for
  near tiles and vice versa.
- Screen-space-error or projected-size selection. Distance plus bias is enough here.
- Cross-fading between tiers.

## Constraints

- GPU (WebGPU) and software renderers must select identical tiers. `js/wasm` and
  TinyGo-relevant packages must still build.
- No behaviour change for existing callers: bias 1 and band 0 must give
  byte-identical frames to today. Add a test that pins this against the current
  selection.
- Follow the repository's documentation, copyright header and CHANGELOG
  conventions. Add a CHANGELOG "Unreleased" entry.

## Tests

- Bias: a node with tiers at 10/20/∞ selects tier 1 at distance 15 with bias 1,
  tier 0 at bias 2, tier 2 at bias 0.5. An invalid bias (0, -1, NaN, +Inf) returns
  an error and leaves the old value in place.
- Bias is O(1): setting it on a scene with many LOD nodes does not touch their
  level data (assert revisions unchanged, or benchmark).
- Hysteresis with band 0.1 and a boundary at 20: a camera sweeping 19 → 21.9 stays
  on tier 0, switches at > 22, sweeping back stays on tier 1 until < 18. Band 0
  reproduces today's switch points.
- Reset rules: a first frame, `SetLOD`, re-showing a hidden node, and a bias change
  each select without hysteresis.
- GPU and software agreement over a camera path, comparing per-tier stats.
- `FrameStats` per-tier counts match the selected tiers.

## Demo

Extend the `forest` demo, which already cycles LOD on, LOD tint and LOD off: add
keys to step the bias (×0.9 / ×1.1) and to toggle a 5% hysteresis band. Show the
bias and per-tier counts in its stats line, and add a short note to the docs.
