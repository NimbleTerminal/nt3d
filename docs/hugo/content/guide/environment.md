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

## Image skies

Pass a decoded PNG, JPEG, or other Go image to `nt3d.NewEnvironment(img)`.
It snapshots the pixels, so callers may reuse or modify the source afterwards.
The image is an **equirectangular panorama**, normally twice as wide as it is tall.
The top is +Z, the center faces +X, and increasing U turns toward +Y. Horizontal
sampling wraps at the seam, while the poles clamp. Alpha is ignored. The same
8192-pixel side limit as `NewTexture` applies. Input colors are sRGB, not HDR.

```go
environment, err := nt3d.NewEnvironment(img)
if err != nil {
    return err
}
scene.SetEnvironment(environment)
```

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
sky while disabling its light. `Frame.Environment` can also override the scene's
choice for one frame. In a widget, set these fields in a frame decorator; use
`SetLighting(nt3d.Lighting{})` to remove its default lights.

This first version uses four first-order spherical-harmonic coefficients, with
cosine convolution, for broad directional diffuse illumination. It is a cheap,
low-frequency approximation: it does not reproduce small bright light sources,
cast environment shadows, or provide glossy/specular reflections. Existing
directional and point lights still provide highlights and directional shadows.
Unlit materials, points, lines, and overlays remain unlit. Prefer the default
linear-light frames for physically meaningful color blending.

The GPU uploads the panorama when it changes and draws one fullscreen triangle.
Software samples the same sky and uses the same diffuse coefficients. The sky
writes no depth, so transparency composites over it and it never changes bounds,
culling, picking, or shadow-map fitting. Wireframe uses sky-colored terminal cell
backgrounds and contrasting default strokes. Fog keeps its existing behavior:
set an explicit `Fog.Color` near the horizon color if you want it to meet the sky.

Try `task lights` or the **Lights** web gallery: it starts with **sky and key**, then
`l` selects **sky IBL**, **image IBL**, the original light rig, sun and sky, and lamp presets.
The image preset is an original generated warm/cool panorama, with no downloaded
assets. `1`, `2`, and `3` compare GPU, software, and wireframe; drag to orbit.
