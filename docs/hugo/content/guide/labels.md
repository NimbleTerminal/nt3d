---
title: Labels and billboards
weight: 65
---

# Labels and billboards

Labels are scene geometry. They move with their parents and use the same
textured triangles, clipping, depth, and picking as other meshes on WebGPU
and software. Wireframe uses depth-tested terminal text.

```go
id, err := scene.SetLabel("peak", nt3d.Label{
    Text: "Peak\nz = 0.60",
    Pixels: true, Height: 32,
    Color: color.RGBA{245, 248, 255, 255},
    Background: color.RGBA{24, 32, 48, 255},
    Padding: 2, Border: 1,
    BorderColor: color.RGBA{90, 160, 210, 255},
})
// Handle errors from each operation.
err = scene.Reparent(id, terrainNode)
err = scene.SetTransform(id, nt3dmath.Translation(nt3dmath.Vec3{Z: .8}))
```

`Height` is the whole label's height. With `Pixels`, it stays the same size
while zooming and supersampling; otherwise it is in world units. Zero height
uses the texture's native height in pixel mode, or one world unit. Labels are
centered on the node origin. Camera-facing labels inherit parent translation;
`Fixed: true` keeps the local XY plane and inherits parent rotation and scale.
Fixed opaque labels are single-sided, following the mesh winding convention.
Blended quads draw both sides, as other transparent meshes do. Scene bounds
include only the anchor for pixel-sized labels, so camera fitting does not
interpret pixels as world units.

Call `SetLabel` again to update text or appearance; the node, transform, and
children survive. Use `SetTransform` alone for movement, which preserves GPU
buffers and textures. For many identical labels, call `Label.Geometry()` once,
then `NewSharedGeometry` and `SetShared` for each instance. Hundreds of shared
labels use the existing instanced GPU draw path. Distinct label textures cost
separate bindings and draws; there is no global glyph atlas.

The default face is the small bitmap `basicfont.Face7x13`, suitable for short
ASCII annotations. `Face` accepts an existing `font.Face` for other sizes or
Unicode coverage; text is drawn at construction, so the face need not remain
alive afterward. Choose a raster size close to the intended display size.
Multiline text uses newline separators. `Icon` adds an image at native pixel
size to the left. Padding, border width, and the optional one-pixel `Shadow`
are measured in texture pixels. `Color` defaults to white.

By default the label uses alpha cutouts: glyphs and backgrounds write depth,
while transparent gaps leave geometry visible and pickable. `Transparent`
enables blended alpha edges and translucent backgrounds, using the existing
back-to-front transparent mesh pass; these labels test depth but do not write
it. `Opacity` in (0, 1) also enables blending; zero defaults to one, and `Hide`
sets opacity to zero. Supersampling and linear-light filtering apply normally.
Blended labels share the existing transparent mesh sorting limitations.

Labels are pickable triangles by default, with the usual node ID and name.
`NoPick` lets clicks pass through. Cutout picking respects texture alpha;
blended picking follows ordinary transparent meshes and tests the whole quad.
The anchor must be between the near and far planes, and quads outside the
frustum are culled. Regenerate the frame after changing camera or viewport:
`Scene.Frame` resolves the billboard transform for rendering and picking.

For an image without text, use `NewBillboard(texture, width, height, opacity,
transparent, Billboard{Pixels: true})`. Its opacity is explicit (zero means
invisible). Set `Billboard.Text` for a terminal representation, or leave it
empty for quad edges. A hand-built frame must supply resolved instance
transforms itself. `CellText`, `DynamicLines`, `Overlays`, and widget frame
decorators continue to work as before.

Wireframe preserves text as terminal graphemes, centered at the projected
anchor, testing occupied cells against surface depth. It simplifies orientation,
size, texture, background, and opacity to readable character cells; it does not
rasterize the label's rectangular background into its depth buffer. Icons
without text appear as quad edges. This is a terminal fallback rather than a
pixel-exact reproduction of the textured quad.

Try the **Terrain explorer** gallery (`task terrain`): labels move with the
terrain parent, the peak text changes when flattened, and ridges occlude
annotations as the camera orbits. Keys 1, 2, and 3 compare the renderers.
