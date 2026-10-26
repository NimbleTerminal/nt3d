---
title: "Clustering and LOD"
weight: 47
---

# Clustering and LOD

[Try the point cloud demo](../demos/#point-cloud) with `task cloud`: a galaxy of up to a million points, `l` switches clustering off to compare, `n` cycles the count, and `-` and `=` zoom. All three renderers draw the same selection.

A large set of similar, independently placed items (points, instances, labels) need not be drawn whole every frame. `nimbleterminal.dev/nt3d/clod` (`nt3dclod`) groups such a set into spatial clusters once and, each frame, selects the clusters inside the view and a level of detail for each: near clusters list all their items, far ones a spread fraction or none, leaving you to draw one representative. The output is a compact list of item indices that the existing point, instance, or label paths draw. The package knows nothing of what an item is beyond its position, and never draws.

## Building

```go
positions := make([]nt3dmath.Vec3, len(points))
for i, p := range points {
    positions[i] = p.Position
}
set, err := nt3dclod.Build(positions, nt3dclod.Options{ClusterSize: 256, Pad: 0})
```

`Build` splits the items at the spatial midpoint of their longest axis until a group holds at most `ClusterSize` items (256 by default); the groups are the clusters, and the splits a hierarchy in `set.Nodes`. `Pad` grows every cluster's bounds by the largest distance an item draws past its position, in world units, so a cluster is kept while any of it could show; points sized in pixels use `View.Margin` instead. Within a cluster, items are ordered along a space-filling curve and then taken at ever finer, evenly spaced steps, so any prefix of `set.Items(c)` is a spread subsample of the cluster: one order serves every level of detail.

`set.Rebuild(positions)` regroups a changed set, reusing its allocations, and `set.Refit(positions)` keeps the groups and recomputes their bounds when items moved a little. Both change `set.Revision()`.

## Selecting each frame

```go
view := nt3dclod.View{Matrix: f.Matrix, Eye: f.Eye, Perspective: f.Perspective, Width: f.Width, Height: f.Height, Margin: 2}
levels := []nt3dclod.Level{{Range: 6, Fraction: 1}, {Range: 12, Fraction: .5}, {Range: 24, Fraction: .25}, {Fraction: 0}}
var sel nt3dclod.Selection
err := set.Select(view, levels, &sel) // Reuses sel's slices.
drawn := nt3dclod.Gather(buffer[:0], points, sel.Items)
for _, pick := range sel.Picks {
    if pick.Count == 0 { // A far cluster: draw it as one point.
        c := set.Clusters[pick.Cluster]
        drawn = append(drawn, nt3d.Point{Position: c.Center, Radius: 3, Color: meanColor[pick.Cluster]})
    }
}
err = scene.SetNodePoints(node, drawn, bounds) // Or SetNodeGeometry when the count changed.
```

A `View` is what a frame sees, in the space of the positions: for a set under a node with a transform, multiply the frame's matrix by the node's world matrix and take the eye into local space. `Margin` is pixels of slack around the view, as nt3d pads its own culling: a point's pixel radius and one more.

Each level applies to clusters nearer than its `Range`; the last level has no range and takes the rest. `Fraction` is how many of a cluster's items the level lists, rounded up and at least one, or none when it is zero. A cluster is culled when its padded bounds lie wholly outside the view, by the same clip-space test nt3d culls geometry with, so a cluster survives whenever nt3d would draw it. Distance is measured from the eye to the cluster's bounding sphere in perspective, zero inside it; an orthographic view converges nowhere, so every cluster counts as the view's height in world units away, which for nt3d's orbit camera is its `Distance` in either projection, so one set of ranges serves both.

`Picks` list the visible clusters in index order, each with its level and its items in `sel.Items`, so the output is deterministic. The selection runs on the CPU for every renderer; the three agree because they draw the same points.

## On the GPU

The same rules run in WebGPU compute, for a draw path that keeps its items on the device:

```go
g, err := nt3dclod.NewGPU(dev)
err = g.Upload(dev, set)                              // When set.Revision() changed.
err = g.Encode(dev, encoder, view, levels, 6)         // Three compute passes.
// Bind g.Items() (u32 indices) and draw with g.Indirect() (DrawIndirect arguments).
sel, err := g.Read(ctx, dev, view, levels)           // Or read the selection back.
```

Three passes cull and level each cluster, lay their items out with a prefix sum, and copy the indices into a compact buffer, writing the item count into indirect draw arguments with the vertex count each item draws. nt3d's renderer does not consume these buffers yet (see the roadmap), so a consumer brings its own device and draw path; `Read` serves tests and CPU consumers. The GPU and CPU agree except where rounding moves a cluster across a plane or a range boundary, which the agreement test tolerates and nothing else.

## Gaussian clouds

Splat clouds use this package without any code of yours. Every Gaussian decoder clusters each piece at load (`nt3dmesh.ClusterPieces`, 256 splats a cluster) and stores the set in `Geometry.Clusters`, unless `nt3dmesh.Options.NoClusters` says not to. A scene's copy of the geometry shares the set by pointer. Each frame both renderers share `Frame.SplatBudget` (`DefaultSplatBudget`, one million; `Scene.SetSplatBudget` sets it for the scene's frames) among the visible clusters and draw a prefix of each one's progressive order.

The selection works per cluster. A cluster outside the view, by its bounds padded for the largest splat, is dropped. The rest project their bounding sphere to a pixel radius `r` and want `ceil(0.5·π·r²)` splats, no more than they hold: about one splat per two pixels of footprint, so a cluster far enough away to cover a few pixels draws a few splats. Unclustered Gaussian geometry draws in full and comes off the budget first. If the wants add up to more than what is left, every want is scaled by the same factor (rounded up, and at least one for each visible cluster); nothing is scaled when they fit.

Because the shares scale continuously with distance and with the budget, there are no level boundaries for a cluster to flicker across, and so no hysteresis. A thinned cluster's survivors grow their axes by `1/√f`, where `f` is the fraction drawn, so the cluster keeps its coverage and does not fade as it thins. The selection is a pure function of the frame: both renderers and `Frame.SplatSelection`, which reports `drawn, resident` counts, agree exactly. On the GPU the keying pass sorts only the selected prefixes, so sort and draw cost follow the budget rather than the capture's size; see [Rendering](../rendering/#budgeting-splats) for measured frames.

A set describes the positions it was built from, so operations that move splats drop it: `Scene.UpdateNodePoints` and `SetNodePoints`, `TransformGeometry`, `MergeGeometry`, `Clip`, and the splits `nt3dmesh.PointPieces` makes. The geometry then draws in full under the same budget. `Model.OrientYUp`, which turns a Y-up capture upright, refits it instead. To keep a set across edits that move splats a little, `Refit` it, or `Rebuild` it after larger ones, with the positions in the geometry's order; either alters every geometry that shares the set.

Later work: levels that merge distant clusters into aggregates, streaming and partial loading, and budgeting plain point clouds.

## Costs and limits

A set holds at most `MaxItems` (16,777,216) items in at most `MaxClusters` (4,194,304) clusters, and a selection distinguishes at most `MaxLevels` (8) levels. An item costs four bytes of order on the CPU and the GPU, a cluster about 100 bytes on the CPU and 48 on the GPU. On an Apple M5 Max, building a million random items takes about 133 ms and selecting over their 5,002 clusters about 0.11 ms, listing 220,000 items. Gathering those into points and handing them to the scene is the frame's real cost; with `Fraction: 0` beyond a range, zooming out replaces whole clusters with one point each.
