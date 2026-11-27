---
title: "Scenes"
weight: 20
---

# Scenes

A `Scene` is a tree of nodes. Each node has a name, a place in the tree, a transform relative to its parent, a visibility switch, and optionally some geometry to draw. Moving, hiding, or rotating a node does the same to everything under it.

```go
scene := nt3d.NewScene()
group, err := scene.Add(nt3d.Root, "objects")
if err != nil { panic(err) }
node, err := scene.SetGeometry("marker", nt3d.Geometry{
    Points: []nt3d.Point{{
        Position: nt3dmath.Vec3{Z: .5},
        Radius: 5, Color: color.RGBA{R: 240, A: 255},
    }},
})
if err != nil { panic(err) }
if err := scene.Reparent(node, group); err != nil { panic(err) }
if err := scene.SetTransform(group, nt3dmath.RotationZ(.4)); err != nil { panic(err) }
```

## Nodes

- `Root` is always there, and every other node descends from it.
- `Add(parent, name)` makes an empty group. Names must be unique and not empty.
- `SetGeometry(name, g)` creates a node with that geometry, or replaces the geometry of the node already called that. `SetNodeGeometry(id, g)` does the same by ID.
- `Reparent` moves a node under a new parent, keeping its local transform. A node can't become its own ancestor.
- `SetVisible` hides or shows a node and everything under it.
- `Remove` deletes a node and everything under it.
- `Node(id)` returns a copy of a node's details.
- `SetNodeData(id, v)` attaches a value of your own to a node: the record it stands for, a key, a pointer to whatever placed it. nt3d never looks inside. It comes back from `NodeData(id)`, in `Node.Data`, in each frame's `Draw.Data`, and in a pick's `Hit.Data`, so a drawn or picked node leads straight to your record. Use it instead of encoding facts into the node's name and parsing them back out.

Node IDs are stable and never reused, so an ID you hold either still means the same node or means nothing.

### How a method says which nodes it means

Nodes are addressed by `NodeID`. Every method that reads or changes one node takes its ID first, and every method that makes a node returns the ID. The method's name tells you how far it reaches:

| Name | Reaches | Examples |
| --- | --- | --- |
| `SetX`, `SetNodeX`, `NodeX` | The one node `id` names. (Transform and visibility are the node's own, though everything under it moves and hides with it.) | `SetTransform`, `SetNodeGeometry`, `SetNodeTexture`, `SetNodeData`, `NodeLight` |
| `ApplyX` | Every mesh under `root` when it is called, `root` included (`ApplyToon` reaches points and splats too). Nothing is remembered: a node added afterwards is not changed. Pass `Root` for the whole scene. | `ApplyMaterial`, `ApplyToon`, `ApplyOutline`, `ApplyDisplacement`, `ApplyMaterialShader` |
| plural or scene-wide | What is under `root`, or the scene | `Lights(root)`, `Cameras(root)`, `SetLook`, `Bounds` |

Names are for finding, not for addressing. `Find(name)` turns a name into an ID. Three shortcuts take a name because they create the node when it is missing, under `Root`, and replace its contents when it is there: `SetGeometry`, `SetShared`, and `SetLabel`. Each returns the ID, and each has a `SetNode…` form for a node you already hold.

nt3d copies what you give it: `SetGeometry` copies the slices, and a `Texture` holds its own snapshot of its pixels. Changing your data afterwards never changes the scene behind your back. `SetNodeTexture` swaps a node's texture without touching its mesh.

nt3d draws what you give it and nothing more: there are no axes, legends, palettes, or plot ranges. Those belong to whatever is built on top, such as a charting library.

## Transforms

Transforms are `nt3dmath.Mat4` values: translations, rotations, and scales, combined as you like. They must be finite, invertible, and affine, and they can't mirror (no negative or zero scales). To mirror geometry, bake it in with [`TransformGeometry`](../shapes/#changing-geometry), which keeps faces pointing outward.

Moving things is cheap. The GPU keeps each node's mesh in its local coordinates, so moving the camera, moving a node, or hiding one only changes a matrix, never re-uploads the mesh. Replacing one node's geometry uploads just that node; adding and removing nodes leaves the others' buffers alone.

For geographic or astronomical worlds where coordinates exceed `float32`'s ~7 decimal digits of precision, `scene.SetNodeOrigin(id, [3]float64{x, y, z})` attaches a double-precision world-offset origin (`NodeOrigin`, `NodeWorldPosition`). When paired with `Camera.Origin` (set automatically by `nt3dcamscene.CameraFromState`), `Scene.Frame` and `Scene.Pick` subtract the camera's origin in `float64` before building `float32` render and BVH transforms—eliminating vertex, shadow, and ray-picking jitter megameters from `(0, 0, 0)` with zero overhead when origins are unset. [Geographic scenes](../geo/) covers where those origins come from: latitude and longitude, Earth-centered coordinates, and `nt3dgeo.ENU.Place`, which splits a transform into the origin and the matrix.

## Draw order

Nodes are drawn in the order they were added, unless you say otherwise. Order shows in two places: where two surfaces lie at exactly the same depth the later one wins, and blended geometry (water, glass, Gaussian splats) has to be drawn after whatever is seen through it.

`SetNodeDrawOrder(id, nt3d.DrawOrder{Layer: l, Key: k})` places a node. Frames draw in ascending `Layer`, then ascending `Key`, then the order nodes were added.

```go
const (
    layerScenery = iota // the default: every node starts in layer 0
    layerSplats
    layerWater
)
water, _ := scene.Add(nt3d.Root, "water")
_ = scene.SetNodeDrawOrder(water, nt3d.DrawOrder{Layer: layerWater}) // everything under water draws last
_ = scene.SetNodeDrawOrder(tile, nt3d.DrawOrder{Key: int64(row)<<32 | int64(col)})
```

- **It is inherited.** A node with no order of its own (the zero `DrawOrder`) draws with its parent's, so ordering a group places everything under it. A node's own order takes the place of its ancestors'. Setting the zero `DrawOrder` hands a node back to its parent's.
- **`Layer` is for coarse passes**, such as scenery, then decals, then water.
- **`Key` is for a fixed order within a layer.** A scene that loads in the background adds nodes in whatever order the loads finish; a key from something stable (a tile's row and column, a placement's index) makes two renders of the same view identical.

Changing an order uploads nothing, and the sorted order is kept between frames until a node is added, removed, moved, or reordered.

## Sharing geometry

When many nodes show the same mesh, like a forest of one tree or a field of markers, make it shared:

```go
tree, err := nt3d.NewSharedGeometry(treeMesh) // checked and copied once
for i, at := range places {
    id, _ := scene.SetShared(fmt.Sprintf("tree %d", i), tree)
    scene.SetTransform(id, nt3dmath.Translation(at))
}
```

A shared mesh is stored once, indexed for picking once, and uploaded to the GPU once, and the GPU draws all of its visible nodes in one instanced call. In a test of 2,500 spheres, the first frame took 43 ms instead of 189 ms, and each later frame 5.5 ms instead of 13.6 ms.

Shared geometry can't change, and holds only triangles; points, boxes, lines, and arrows belong to each node. Nodes given different textures with `SetNodeTexture` are drawn in separate batches, and transparent nodes are still drawn one by one so they blend in the right order. `SetNodeShared(id, sg)` shares by ID, and frames list shared geometry in `Frame.Shared`. Subtree appearance setters—`ApplyMaterial` (including per-instance `Material.BaseColor`), `SetMaterialVariant`, `ApplyDisplacement`, `ApplyMaterialShader`, `ApplyOutline`, and `ApplyToon`—preserve `SharedGeometry` instancing and uploaded GPU mesh buffers across matching instances.

The [forest demo](../demos/#forest) grows thousands of trees from three shared meshes, and switches them to copies so you can compare.

## Level of detail

`SetLOD(id, levels)` gives a node distance-switched geometry tiers, chosen in `Scene.Frame` by the world-space distance from `Camera.Eye()` to the node's world-space AABB:

```go
id, _ := scene.Add(nt3d.Root, "block-04")
err := scene.SetLOD(id, []nt3d.LODLevel{
    {MaxDistance: 150, Geometry: highMesh, Outline: creaseOutline},
    {MaxDistance: 400, Geometry: midMesh, Outline: hullOutline},
    {Geometry: lowMesh}, // MaxDistance 0 (or +Inf) stays visible at any distance
})
```

Each `LODLevel` sets either `Geometry` or `Shared` (`*SharedGeometry`) and an optional `Outline` override. Measuring distance to the world-space AABB means a large terrain or city tile right beneath the camera evaluates near zero rather than to the tile's center. Setting a finite positive `MaxDistance` on the final level hides the node beyond that distance. Uploaded GPU buffers for visited tiers stay cached as the camera moves across thresholds, and picking builds BVH indexes per tier.

To drive detail from a frame-time budget without rebuilding per-node tiers, `scene.SetLODBias(f)` scales every `MaxDistance` (including finite hide-beyond cutoffs) by `f > 0` in `O(1)` (`1` by default; `scene.LODBias()`), resetting per-node hysteresis so budget steps apply on the very next `Scene.Frame`. `scene.SetLODHysteresis(band)` adds a fractional deadband in `[0, 0.5]` (`0` by default; `scene.LODHysteresis()`) so a node crossing effective boundary `D` switches to a coarser tier only above `D * (1 + band)` and returns to a finer tier only below `D * (1 - band)`; first frames, `SetLOD`, re-showing a hidden node with `SetVisible`, and `SetLODBias` changes select without hysteresis. `frame.Stats().LODTierInstances` reports how many drawn LOD instances landed in each tier (`0..6`, with `7+` folded into index `7`). The [forest demo](../demos/#forest) uses three shared tiers per tree species (`l` cycles `LOD on`, color-coded `LOD tint`, and `LOD off`; `[` and `]` step bias by `×0.9` / `×1.1`; `b` toggles a 5% hysteresis band).

The hysteresis band makes a frame depend on the frames before it, which is right for a moving camera and wrong for a screenshot. [`Scene.StillFrame`](../rendering/#frames) chooses every level by distance alone and leaves the remembered levels untouched.

Reusable `SceneAsset` templates can also carry `AssetNode.LOD` directly (with optional `LODBounds` for synchronized multi-part prop switching and `LODVariantMaterials` for per-tier colorways): call `asset.WithLOD(40, 120)` to collapse sibling `*_LOD0`, `*_LOD1`, `*_LOD2` subtrees in a single `.glb`, or `CombineAssetLOD` / `nt3dmesh.CombineLOD` to merge separate per-tier files into one reusable template (see [Multi-LOD assets](../meshes/#multi-lod-assets)).

## Labels

[`SetLabel` and `NewBillboard`](../labels/) add world-anchored text and images.
They use ordinary geometry nodes, so parenting, visibility, shared instances,
and picking work the same way.

## Threads

Edit the scene and make frames from one goroutine. Rendering a frame can happen on another, since a frame is a read-only snapshot of the scene.
