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

Node IDs are stable and never reused, so an ID you hold either still means the same node or means nothing.

nt3d copies what you give it: `SetGeometry` copies the slices, and a `Texture` holds its own snapshot of its pixels. Changing your data afterwards never changes the scene behind your back. `SetTexture` swaps a node's texture without touching its mesh.

nt3d draws what you give it and nothing more: there are no axes, legends, palettes, or plot ranges. Those belong to whatever is built on top, such as a charting library.

## Transforms

Transforms are `nt3dmath.Mat4` values: translations, rotations, and scales, combined as you like. They must be finite, invertible, and affine, and they can't mirror (no negative or zero scales). To mirror geometry, bake it in with [`TransformGeometry`](../shapes/#changing-geometry), which keeps faces pointing outward.

Moving things is cheap. The GPU keeps each node's mesh in its local coordinates, so moving the camera, moving a node, or hiding one only changes a matrix, never re-uploads the mesh. Replacing one node's geometry uploads just that node; adding and removing nodes leaves the others' buffers alone.

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

Shared geometry can't change, and holds only triangles; points, boxes, lines, and arrows belong to each node. Nodes given different textures with `SetTexture` are drawn in separate batches, and transparent nodes are still drawn one by one so they blend in the right order. `SetNodeShared(id, sg)` shares by ID, and frames list shared geometry in `Frame.Shared`.

The [forest demo](../demos/#forest) grows thousands of trees from three shared meshes, and switches them to copies so you can compare.

## Threads

Edit the scene and make frames from one goroutine. Rendering a frame can happen on another, since a frame is a read-only snapshot of the scene.
