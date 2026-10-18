---
title: Animation and skinning
weight: 65
---

`AnimationClip`, `Track`, `Skeleton`, and `Deformation` are independent of file formats. Tracks target scene nodes; procedural code can supply the same data or update joint transforms and morph weights directly.

## Playing and sampling

```go
clip := nt3d.AnimationClip{Name: "slide", Tracks: []nt3d.Track{{
    Node: node, Channel: nt3d.Translation, Width: 3,
    Times: []float32{0, 2},
    Values: []float32{0, 0, 0, 2, 0, 0},
}}}
player, err := nt3d.NewAnimation(scene, clip)
if err != nil { return err }
player.Mode = nt3d.PingPong // Once (clamped), Loop, or PingPong
if err := player.Step(elapsed.Seconds()); err != nil { return err }
player.Paused = true
if err := player.Sample(1.25); err != nil { return err } // works while paused
```

Time is in seconds. `Speed` defaults to one and may be negative. `Time` is the unwrapped playback clock; `Sample` folds it according to `Mode`. `Once` clamps at the endpoints. `Translation` and `Scale` use width three; `Rotation` uses normalized XYZW quaternions and width four. `MorphWeights` uses the target count. `UserChannel` invokes the player's `Apply(name, values)` callback.

Tracks support `Linear`, `Step`, and `CubicSpline`. Cubic keys pack incoming tangent, value, outgoing tangent, with tangents in units per second. Linear rotation uses shortest-arc spherical interpolation; cubic rotation evaluates components and normalizes the result. Times must be finite, nonnegative, and strictly increasing.

`SampleBlend(time, weight)` mixes a clip into the current pose. Sample a base clip first each frame, then apply overlay clips in order with weights between zero and one. This is ordered layering, not an additive mixer. Channels absent from a clip retain their current values. Rotation tracks require a TRS transform; static nodes may still use matrices. The scene's existing restriction to invertible transforms with positive determinant applies to animation too.

All animation calls run on the scene's owning goroutine. The widget does not own the playback clock: sample on your application's tick, then return `view.Refresh()`. Existing frames remain immutable while an asynchronous render finishes. See the [animation demo](../demos/#animation) for playback with pause, elapsed time, and renderer switching.

## Morphs and skeletons

Bind a node's rest geometry with `scene.SetDeformation(node, definition)`:

```go
definition := nt3d.Deformation{
    Morphs: []nt3d.MorphTarget{{Positions: positionDeltas, Normals: normalDeltas}},
    Weights: []float32{0},
    Skeleton: &nt3d.Skeleton{Joints: joints, InverseBind: inverseBind},
    Influences: influences,
}
if err := scene.SetDeformation(node, definition); err != nil { return err }
```

Each morph supplies per-vertex deltas; position or normal deltas may be omitted. Each `Influence` has four joint indices and nonnegative weights, normalized at binding. Zero total weight leaves the vertex unskinned. Missing inverse bind matrices mean identity. Morphs and skins may be used separately or together. Setting new geometry detaches the deformation; bind it again to use that new rest mesh.

Evaluation applies morph deltas first, then linear blend skinning with the mesh-local palette:

```
inverse(meshWorld) × jointWorld × inverseBind
```

Positions use homogeneous W=1 and normals W=0; normals are normalized after blending. CPU and GPU use the same palette and normal convention. Joint indices reference the skeleton, whose joints are ordinary scene nodes. Parenting an entire animated instance transforms the mesh and its joints together.

`SetMorphWeights` updates procedural weights. `Frame` and `Bounds` evaluate changed palettes and weights; `UpdateDeformations` exposes errors explicitly. If a joint was removed or a procedural pose is invalid, `Frame` reports the error and `Bounds` retains the last valid mesh. Old frames retain their old geometry and palettes.

The CPU pose supplies exact bounds, culling, picking, software rendering, and wireframe edges. WebGPU uses a compute pass to skin persistent triangle buffers before both color and shadow passes. Joint-only motion uploads the palette; changed morphs upload the morphed source vertices. CPU posing still runs for exact bounds and picking, so this path is not GPU-only deformation. Unchanged poses keep their geometry revision and upload nothing. Transform-only animation preserves shared mesh instancing; independently posed meshes have independent deformation buffers.

## glTF mapping

`nt3dgltf.Decode` keeps the compatibility `Model.Objects` flat rest-pose view. All glTF files also populate `Model.Asset`, a generic `SceneAsset`, preserving static nodes and cameras as well as animation. `Model.AddTo` automatically instantiates that hierarchy; retrieve its bound clips with `scene.AnimationClips(root)`. Alternatively, instantiate explicitly:

```go
instance, err := model.Asset.Instantiate(scene, nt3d.Root, "character")
if err != nil { return err }
player, err := nt3d.NewAnimation(scene, instance.Clips[0])
if err != nil { return err }
player.Mode = nt3d.Loop
```

`SceneAsset` can also be built by another loader or procedural code. Its parent indices are zero-based, with -1 for roots; joint and track NodeIDs are one-based asset-node indices. Instantiation remaps them into independent scene identities.

The glTF loader retains node TRS/matrices beneath one Y-up to Z-up conversion group. Primitive children hold local geometry, including split pieces and double-sided vertices. Weight tracks fan out to these children. Joint nodes outside the selected scene remain available as dependencies, while only the selected scene's meshes are shown.

Supported animation data includes TRS and weight tracks, all three interpolation modes, sparse accessors, normalized integer weights, quaternion normalization, morph position/normal deltas, and inverse bind matrices. The existing triangle limit applies, and decoded animation/deformation allocations are also bounded by `Options.MaxBytes`. Deformation applies to triangle primitives. Up to four influences per vertex are supported; additional influence sets are rejected explicitly. Animated reflected/singular transforms remain unsupported by the scene graph. Cameras, texture transforms, and normal/emissive maps remain separate glTF follow-ups.

The generated ribbon in the gallery and test fixtures is original CC0 content, requiring no external download.
