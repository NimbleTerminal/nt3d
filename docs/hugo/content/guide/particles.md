---
title: "Particles and sprites"
weight: 46
---

# Particles and sprites

[Try the particles demo](../demos/#particles) with `task particles`: `n` cycles up to 100,000 particles, `t` switches between discs and animated atlas sprites, `b` changes blending, and `a` pauses. All three renderers use the same scene; wireframe shows dots.

## Updating points

Create a geometry with `Points` and `PointStyle` as usual. For subsequent frames, keep its count and style and call:

```go
var bounds nt3dmath.AABB
bounds.Include(nt3dmath.Vec3{X: -3, Y: -3, Z: 0})
bounds.Include(nt3dmath.Vec3{X: 3, Y: 3, Z: 3})
err := scene.SetNodePoints(node, particles, bounds)
// Or replace only a range, starting at index 100:
err = scene.UpdateNodePoints(node, 100, particles[100:200], bounds)
```

These methods check and copy the supplied points, share all unrelated geometry, and use the supplied bounds without recomputing them. Existing frame snapshots remain immutable, so each update still allocates a point slice; this API does not expose mutable scene storage. Partial updates copy that slice too, but validate and upload only the changed range. If the GPU skipped updates or replays an older frame, it uploads the complete point buffer to restore the correct snapshot. Buffer allocations, other primitive uploads, and repeated point validation during rendering are avoided. Alpha sorting is refreshed when points move.

Gaussian splats update the same way. The GPU stores them compactly, with half-float axes and byte spherical harmonics at SPZ's precision; set `PointStyle.Precise` for splats that must not be quantized (see [3D Gaussian splatting](../rendering/#3d-gaussian-splatting)).

Bounds must enclose **all primitive positions in the geometry's local coordinates**, including untouched points and any triangles, boxes, lines or arrows. Use a fixed conservative box when the emitter's extent is known. Point radii are handled separately by culling. Updated positions outside the supplied box are rejected; unchanged primitives are the caller's responsibility. Bounds also determine scene framing and the depth slice of additive emitters and transparent meshes, and with every blended geometry's bounds the depth range the slices divide, so loose bounds coarsen alpha ordering too. Picking reads the new points.

Use `SetNodeGeometry` to change point count or style, or for geometries with morph/skin deformation. The fast methods return errors without changing the scene on invalid input. Scene mutation has the same synchronization requirements as other scene setters.

The 100k-point update benchmark (`go test -run '^$' -bench BenchmarkPointUpdate100K -benchmem`) measured about 0.31 ms for `SetNodePoints` versus 0.61 ms for `SetNodeGeometry` on an Apple M5 Max. These numbers measure scene updates, not rendering or sorting.

## Textures and atlases

```go
style := nt3d.PointStyle{
    Texture: atlas, // An immutable image texture from nt3d.NewTexture.
    Columns: 4, Rows: 2,
    Units: nt3d.WorldUnits,
    Blend: nt3d.AlphaBlend,
}
particles[0].Frame = 3
particles[0].Radius = 0.1
particles[0].Color = color.RGBA{255, 255, 255, 255}
```

A texture replaces the disc with a square camera-facing quad. `Radius` is its half-width, in pixels or world units. `Point.Color` tints the sampled color; white preserves it. `Shape` applies only to untextured discs.

The atlas is an evenly divided grid, numbered left to right, top to bottom. Zero columns or rows means one. The selected frame is `(PointStyle.Frame + Point.Frame)` modulo the number of cells: set the style's frame for a shared default or each point's frame for independent animation and variants. Update per-point frames with the fast point API. Changing the style uses `SetNodeGeometry`.

Sprites use bilinear filtering at full texture resolution, clamped to texel centers inside the selected cell. This prevents atlas bleeding without padding, though very small sprites can alias because they do not use mipmaps. Procedural textures and normal maps are not accepted as sprite images.

- `OpaqueBlend` discards texture alpha below 0.5, then writes opaque color and depth. Point color alpha is ignored.
- `AlphaBlend` multiplies texture alpha by point alpha and sorts points back to front, together with every other alpha point geometry in the frame.
- `AdditiveBlend` adds tinted color times that combined alpha.

All modes respect nearer opaque surfaces and fog, with matching GPU/software linear-light and supersampling behavior. Blended sprites do not write depth; alpha emitters sort together with every other alpha point geometry and with transparent meshes, to within one of 64 depth slices, while additive emitters draw at the depth slice of their bounds' center. Picking tests the square and its texture alpha; wireframe retains point dots.

## Soft intersections

Set `PointStyle.SoftDistance` to a positive distance in world units to fade
blended particles near the scene's depth surface. For example, `.16` gives
water spray a soft intersection over a 16 cm view-ray gap when one world unit
is a meter. Alpha and additive discs, sprites, and Gaussians support this in
both GPU and software. Opaque points and wireframe ignore the setting.

The scene depth includes opaque and transmissive surfaces, so spray fades
against refractive water. Ordinary transparent meshes do not write depth and
cannot soften particles. Zero preserves the existing hard depth intersection.
