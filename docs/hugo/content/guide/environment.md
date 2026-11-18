---
title: Environment and sky
weight: 65
---

An environment gives a scene a distant sky and diffuse image-based lighting.
It is optional: scenes without one keep their solid background and existing lighting.

```go
sky := nt3d.GradientEnvironment(
    color.RGBA{72, 130, 220, 255},   // zenith
    color.RGBA{218, 231, 245, 255},  // horizon
    color.RGBA{115, 100, 77, 255},   // ground
)
scene.SetEnvironment(sky)
```

The widget picks it up through the scene's frames. Call `view.Refresh()` after
changing it when the application is not already scheduling frames. Remove it
with `scene.SetEnvironment(nil)`. Environment objects are immutable and can be
shared between scenes; frames retain the environment they were made with.

## Image and Radiance `.hdr` skies

Pass a decoded PNG, JPEG, or other Go image to `nt3d.NewEnvironment(img)` for an 8-bit sRGB panorama, or pass raw Radiance RGBE (`.hdr` / `.pic`) bytes to `nt3d.NewHDREnvironment(data)` for unclamped linear HDR radiance. Both snapshot their input, so callers may reuse or discard the source afterwards.
The image is an **equirectangular panorama**, normally twice as wide as it is tall.
The top is +Z, the center faces +X, and increasing U turns toward +Y. Horizontal
sampling wraps at the seam, while the poles clamp. Alpha is ignored. The same
8192-pixel side limit as `NewTexture` applies.

```go
hdrBytes, err := os.ReadFile("studio.hdr")
if err != nil {
    return err
}
environment, err := nt3d.NewHDREnvironment(hdrBytes)
if err != nil {
    return err
}
scene.SetEnvironment(environment)
```

`NewHDREnvironment` decodes adaptive per-channel RLE and uncompressed RGBE scanlines (`FORMAT=32-bit_rle_rgbe`), honors `EXPOSURE=` headers and `-Y/+Y`/`+X/-X` resolution orientations, and uploads `RGBA16Float` mip chains on WebGPU alongside matching float buffers in software. Unclamped solar disks and studio softboxes feed directly into `DiffuseSH()`, PBR roughness-filtered specular IBL, `Frame.PostProcess.Bloom`, `Exposure`, and `ACESToneMap` (while clamping gracefully to 8-bit cell backgrounds in wireframe).

The sky follows camera orientation but stays fixed under translation and zoom.
Perspective views use the camera's authored field of view and roll. Orthographic
views use a fixed 45-degree field of view, so models have a useful sky instead
of a single constant direction.
For hand-built frames, set `Frame.Forward` to the camera's viewing direction.
`Scene.Frame` does this automatically.

## Light from the environment

Both renderers add the same precomputed diffuse light to `Frame.Lighting`.
To use the environment alone, clear the usual lights:

```go
frame, err := scene.Frame(camera, 960, 640)
if err != nil {
    return err
}
frame.Lighting = nt3d.Lighting{}
frame.EnvironmentStrength = 0.6
```

`EnvironmentStrength` scales only the lighting; zero means 1, as with the
other lighting intensity fields. `EnvironmentBackgroundOnly = true` keeps the
sky while disabling its diffuse light and its reflections. `Frame.Environment` can also override the scene's
choice for one frame. In a widget, set these fields in a frame decorator; use
`SetLighting(nt3d.Lighting{})` to remove its default lights.

Environment lighting uses nine degree-two RGB spherical-harmonic coefficients,
with cosine convolution, for broad directional diffuse illumination. It is a cheap,
low-frequency approximation: it does not reproduce small bright light sources,
cast environment shadows, or capture sharp reflections. Existing
directional and point lights still provide highlights and mapped shadows.
Unlit materials, points, lines, and overlays remain unlit. Prefer the default
linear-light frames for physically meaningful color blending.

Use `nt3d.NewSHEnvironment(radiance)` to build an environment directly from SH
radiance coefficients, including a generated sky panorama. `Environment.DiffuseSH()`
returns its nine convolved diffuse coefficients. The shared `nt3dsh` package
provides projection, evaluation, and rotation; see
[spherical-harmonic lighting](../rendering/#spherical-harmonic-lighting) for an
example and coefficient conventions. Gaussian splats use SH for their own
view-dependent color and remain unlit by the environment.

The GPU uploads the panorama when it changes and draws one fullscreen triangle.
Software samples the same sky and uses the same diffuse coefficients. The sky
writes no depth, so transparency composites over it and it never changes bounds,
culling, picking, or shadow-map fitting. Wireframe uses sky-colored terminal cell
backgrounds and contrasting default strokes. Fog keeps its existing behavior:
set an explicit `Fog.Color` near the horizon color if you want it to meet the sky.

Try `task lights` or the **Lights** web gallery: it starts with **sky and key**, then
`l` cycles through the environment and light presets, including **SH studio**.
Press `h` to select the SH studio environment and rotate it in 30-degree steps.
The image preset is an original generated warm/cool panorama, with no downloaded
assets. `1`, `2`, and `3` compare GPU, software, and wireframe; drag to orbit.

## Reflections

`Material.Reflectivity` samples the panorama along the reflected view direction.
It is the normal-incidence reflectance F0, in [0,1]: zero explicitly disables
reflection, `.02` suits water, and `1` is a mirror. Schlick Fresnel increases
reflection toward grazing angles, blending it with the lit base color. Direct
specular highlights are added afterward. Normal maps affect both contributions.

GPU and software share panorama orientation, seam wrapping, pole clamping, and
color-space behavior. `EnvironmentStrength` scales reflected light too.
`EnvironmentBackgroundOnly` disables reflections along with diffuse environment
light. For reflective (`Reflectivity > 0`) or `PBR` surfaces that should still
reflect the sky, such as water or glass, without the sky lighting the land,
set a reflection probe that holds the sky panorama over them: probes are
independent of `EnvironmentBackgroundOnly`.
Without an environment, reflectivity has no effect.

This is a distant-sky reflection that does not reflect nearby geometry. On classic materials (`PBR: false`), reflections sample the sharp base panorama while `Shininess` controls direct highlights; on [PBR materials](../materials/#metallicroughness-pbr-and-orm-maps), `Roughness` samples a six-level GGX-prefiltered mip chain of the LDR or `.hdr` panorama.

## Local reflection probes

Use an authored panorama to anchor reflections to a room rather than to the
faraway sky. Each `ReflectionProbe` has a capture `Position`, an axis-aligned
`Bounds` box, an `Environment` image, and optional `Strength` (zero means 1).
The position must be strictly inside the box. The panorama uses the same
orientation and LDR/HDR loaders as a sky.

```go
roomHDR, err := os.ReadFile("room.hdr") // panorama captured at the position below
if err != nil {
    return err
}
roomImage, err := nt3d.NewHDREnvironment(roomHDR)
if err != nil {
    return err
}
var room nt3dmath.AABB // import nt3dmath "nimbleterminal.dev/nt3d/math"
room.Include(nt3dmath.Vec3{X: -4, Y: -3, Z: 0})
room.Include(nt3dmath.Vec3{X: 4, Y: 3, Z: 3})
if err := scene.SetReflectionProbes([]nt3d.ReflectionProbe{{
    Environment: roomImage,
    Position:    nt3dmath.Vec3{Z: 1.5},
    Bounds:      room,
}}); err != nil {
    return err
}
```

Both WebGPU and software intersect the reflected ray from the surface with the
box, then sample the panorama in the direction from the capture position to
that intersection. This corrects the main parallax error as objects or the
camera move through a rectangular room. PBR and clearcoat use roughness-filtered
samples; classic `Material.Reflectivity` uses the sharp image.

Probes supply specular light only: they do not replace the visible sky, add
diffuse light, or capture geometry automatically. They work without a global
environment and are independent of `EnvironmentBackgroundOnly` and
`EnvironmentStrength`. Their radiance replaces the indirect specular fallback
inside the box; outside all boxes the existing hemisphere/environment fallback
remains. Material response and ambient occlusion still apply.

There are at most `MaxReflectionProbes` (currently four) per frame. In overlapping
boxes the first probe in the list wins. Box edges have a hard transition;
overlap blending is not implemented yet. Use `scene.SetReflectionProbes(nil)`
to remove probes. Setters and `scene.ReflectionProbes()` copy the list, and old
frames retain their probes. Refresh a static widget after changing the scene.

Scene probe positions and bounds are in world coordinates. `Scene.Frame`
converts them relative to `Camera.Origin`. To override `Frame.ReflectionProbes`
directly, supply coordinates in that frame's space, like `Frame.Eye`.

## Externally baked lightmaps

Use `Material.LightMap` for static diffuse lighting baked by an external tool.
`Geometry.LightMapUV` is an independent coordinate per vertex; it never falls
back to the base or normal-map UVs. Supply these coordinates alongside the
mesh, splitting vertices at UV chart seams. Lightmaps do not require base UVs.

```go
baked, err := nt3d.NewHDRLightMap(hdrBytes) // Radiance RGBE UV atlas
if err != nil {
    return err
}
geometry.LightMapUV = bakedUVs
geometry.Material = &nt3d.Material{
    PBR: true,
    Roughness: 0.7,
    LightMap: baked,
}
```

Alternatively, `NewLightMap(width, height, rgb)` accepts row-major float32 RGB
**linear irradiance**, before albedo and division by pi. Do not pass a display
image, tone-mapped image, or baked final surface color. nt3d divides by pi and
applies the surface's material response. Values can exceed one; finite values
in 0..65504 are stored as half floats. Both constructors copy their inputs.
Dimensions must be 1..8192 per side. UV (0,0) is the top left; sampling clamps
at the edges and uses mipmaps and anisotropic filtering. Dilate the bake into
chart gutters so filtering does not bleed from unrelated charts.

A lightmap replaces hemisphere and environment **diffuse** on its material,
including at black texels. Direct lights, emissive color, and specular lighting
(including local reflection probes) remain active. Existing indirect AO applies
to the baked diffuse source. For a bake that includes occlusion, set material
AO appropriately and disable SSAO if additional contact darkening is unwanted.
Bake indirect diffuse only when keeping the corresponding direct lights in
nt3d; otherwise their diffuse illumination would be counted twice.

Classic, PBR, and toon materials consume the same source; unlit materials ignore
it. Keep `Frame.Linear` enabled for meaningful irradiance units and use exposure,
tone mapping, or bloom for HDR presentation. GPU and software use the same mip
chain. This first format is nondirectional, so normal maps do not rotate baked
lighting. Changes to static geometry or lighting require rebaking. Animated
objects still need their own lighting; no probe grid is sampled from this atlas.

Assign the lightmap and its UVs before `Scene.SetGeometry` or
`NewSharedGeometry`. `Scene.ApplyMaterial` can replace or remove a lightmap while
preserving earlier frame snapshots. Different lightmaps can share geometry but
use separate draw batches. Hand-built frames must update `Frame.Revision` when
changing geometry or adding/removing its UV extension, following the usual
frame caching contract. nt3d does not bake, unwrap, or automatically import
lightmap metadata from glTF; applications supply the atlas and UVs explicitly.
