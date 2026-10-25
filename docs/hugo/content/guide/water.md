---
title: "Wave surfaces"
weight: 66
---

# Wave surfaces

`WaveSurface` combines directional Gerstner waves on the XY plane, with Z up
and mean height zero. It supplies positions, analytic normals, tangents, and
material-point velocities. One evaluator can move a water mesh and answer
surface queries for floating objects. The surface is immutable and safe to
sample concurrently; its zero value is flat water.

```go
surface, err := nt3d.NewWaveSurface([]nt3d.GerstnerWave{
    {Direction: nt3dmath.Vec3{X: 1, Y: .3}, Wavelength: 4.5, Amplitude: .2},
    {Direction: nt3dmath.Vec3{X: -.3, Y: 1}, Wavelength: 2.7, Amplitude: .09, Phase: 1.1},
}, 9.81)
if err != nil {
    return err
}

// Move a mesh vertex from its original, undisplaced XY coordinates.
sample, err := surface.Sample(restX, restY, seconds)
if err != nil {
    return err
}
vertex.Position = sample.Position
vertex.Normal = sample.Normal

// Find the water under an object at a world XY location.
water, err := surface.AtWorld(worldX, worldY, seconds)
if err != nil {
    return err
}
height := water.Position.Z
```

Directions are normalized on construction and must have zero Z. Wavelengths
are positive, amplitudes nonnegative, and phases are radians. With gravity
9.81, lengths are meters and time is seconds. Other consistent units work too.
Angular speed follows the deep-water relation `sqrt(gravity * 2π / wavelength)`.
The constructor rejects non-finite inputs and total steepness
`sum(2π * amplitude / wavelength) >= 1`, so waves cannot fold over themselves.

## Mesh coordinates and world queries

Gerstner waves displace points horizontally as well as vertically. `Sample`
takes the **original** mesh coordinates, which must be retained across frames.
Sampling last frame's displaced positions would make the grid drift.
`TangentX` and `TangentY` differentiate the displaced position with respect to
those original coordinates; `Normal` is their normalized cross product.

`AtWorld` solves for the original coordinates that arrive at the requested
world XY. It uses a damped Newton iteration with a horizontal residual tolerance
of 1e-7 world units and a 64-iteration limit. It returns an error on failure;
nearly folding waves and extreme scales can be poorly conditioned. Returned
geometry uses float32, so its precision also depends on distance from the origin.

`Velocity` follows a material point at fixed original coordinates. It is not
a background current or the rate of height change at fixed world XY. Keep time
continuous: different wavelengths have different periods, so wrapping all of
them at a common arbitrary time creates a jump.

Queries use the surface's coordinate system. If a scene node transforms the
water, convert world queries into that local system and transform samples back.

## The water demo

Run `task water`, or open Water in the browser gallery. Three waves displace an
81 × 81 grid split into four tiles. Each frame updates its positions, analytic normals, and bounds,
then replaces that node's geometry. GPU, software, wireframe, picking, and
shadows consequently use the same mesh; there is no GPU-only displacement.
World queries evaluate the continuous surface, while the grid is its triangle
approximation. Finer grids reduce the difference between them.

## Driving and crest launches

The orange test craft starts on an autopilot circuit. Four hull cells sample
water height and velocity, producing Archimedes buoyancy, directional drag,
and optional speed-dependent planing lift. The demo integrates translation
and quaternion rotation at 120 Hz. Throttle drives a stern jet; its offset
from the center of mass produces steering and trim torque. The autopilot
steers that jet rather than assigning position or heading. It anticipates the
path using velocity and sideslip, and governs power to follow its circuit.
The throttle setting limits that automatic power; manual mode uses the full setting.

Waves and wake displacement act through forces. A dry hull follows gravity
and retains angular momentum until reentry; no jump impulse or per-frame
surface snapping is used. The 80 kg mass and drag/lift coefficients are tuned
for this stylized 1.5 m hull, not calibrated to a production personal watercraft.
Patch edges and the bank still use simple velocity-reflecting constraints.
Pause holds the shared wave and craft clock.

### Handling and inspection

`u` switches touring/sport without resetting motion. Touring uses 260 N peak
jet thrust, stronger lateral drag, and slower input response. Sport uses 340 N,
faster throttle/steering response, and more rider lean. Both share the same
80 kg hull, buoyancy cells, and passive heave damping. Input values ramp on the
fixed physics clock, so key repeat does not instantly change thrust or nozzle angle.
Jet thrust falls with forward speed, preventing runaway acceleration as planing
reduces immersion. A flat-water, full-throttle check settles near 7.4 m/s touring
and 8.7 m/s sport; waves, steering, and trim change those values.
`j`/`l` latch a steering command; `k` centers it and selects manual control.
`[`/`]` adjust stern-jet trim within ±8.6°; positive trim generates bow-up torque.
A damped, contact-gated rider torque leans into turns. It is an explicit gameplay
control force, separate from passive water forces.

`b` cycles normal display, force telemetry, and handling telemetry. Both debug
pages draw the same depth-tested world geometry on all renderers:

- Green/red dots and vertical bars identify wet/dry hull probes; cyan crosses
  mark their sampled water heights. Cells use a world-vertical immersion range.
- Green arrows show buoyancy, cyan planing, red drag, and yellow jet thrust.
  Arrows start 0.5 m above the center of mass for visibility, with **1 m = 500 N**.
- The white velocity arrow uses **1 m = 5 m/s**.

The force page reports wet fraction/contact count and force magnitudes in N,
plus net acceleration in g and its peak. Net acceleration includes gravity,
water, and thrust, excludes boundary collision impulses, and reads zero at rest.
The handling page reports actual power, steering, trim, pitch, bank, sideslip,
and the last dry-to-wet entry speed in m/s. The latter is a landing measurement,
not a solved impact impulse or pressure. Readings are captured from the last
physics force evaluation; toggling debug never recomputes or alters the body.

Pause with `a`, then press `.` to advance one 1/120-second step, including waves,
wake history, and effects. `x` clears peak acceleration, last entry speed, and
launch/landing counts while retaining body state. Paused preset/trim changes
apply on the next physics step. The presets are starting points for game feel,
not measured Sea-Doo parameters.

### Integration with a physics engine

`NewBuoyantHull` creates an immutable force provider. It accepts any
`WaterSurface`, including `WaveSurface.WithWake` snapshots. At each engine
physics step:

1. Supply the body's rigid center-of-mass transform and world linear/angular
   velocities in `WaterBodyState`.
2. Call `hull.Forces(surface, body, simulationTime)`.
3. Apply the returned world-space `Force` and `Torque` about the center of mass.
4. Let the engine add gravity, propulsion, contacts, and constraints, integrate
   the body, and copy its resulting transform to the scene.

Use meters, kilograms, seconds, and Z up. The result is force, not impulse:
only multiply by the timestep if the engine expects impulses. Hull probe
positions are relative to the center of mass; transforms must have no scale.
Point velocity includes angular velocity crossed with its lever arm. Drag
uses velocity relative to the sampled water; a surface adapter can add a
physical current. Material flow maps alone do not apply current forces.

This component owns no rigid body, clock, collision system, or renderer state.
The small integrator in `water_body.go` is demo code, replaceable by the game's
physics engine. Probe cells approximate submerged volume; empirical planing
lift does no work along relative motion, but does not solve hull pressures,
ventilation, breaking waves, or two-way fluid momentum. Wake emission remains
an analytic effect rather than a conservation-based fluid solver.

| Key | Action |
| --- | --- |
| `w` / `s` | Increase / decrease throttle, 0–100% |
| `j` / `l` | Adjust left / right jet steering and take manual control |
| `k` | Center steering and take manual control |
| `u` | Switch touring / sport handling |
| `[` / `]` | Adjust bow-down / bow-up jet trim |
| `b` | Cycle force / handling debug pages / off |
| `.` | Advance one physics step while paused |
| `x` | Clear peak acceleration and landing measurements |
| Space | Toggle autopilot; clears manual steering |
| `n` | Toggle ripple normal detail |
| `e` | Toggle environment reflections |
| `f` | Cycle wake foam + spray, foam only, spray only, and hidden |
| `z` | Toggle wake deformation without changing body state |
| `p` | Toggle empirical planing lift |
| `t` | Toggle refraction and depth-dependent absorption |
| `h` | Toggle spray intersection fading |
| `v` | Cycle shore foam + flow, shore foam, flow, and neither |
| `m` | Toggle 10×10 / 30×30 water extents |
| `a` | Pause waves and craft together |
| `1` / `2` / `3` | GPU / software / wireframe |

Steering persists between key presses, making the demo usable in terminals
without key-release events. Counter-steer to straighten, or use Space.

## Ripple normals and sky reflections

The large waves displace geometry; small procedural waves only change shading.
A periodic height pattern supplies analytic derivatives encoded into a tangent
normal map. `NewShaderNormalMapWithParameters` evaluates it on the GPU, while a
matching image supplies software rendering. Each time produces an immutable
snapshot with linear-data mipmaps. Authored tangent directions follow the
Gerstner mesh. `n` disables ripple strength without changing geometry or physics.

`Material.Reflectivity` enables distant environment reflection with Schlick
Fresnel blending. Water uses `.02` at normal incidence, rising toward full
reflection at grazing angles. The procedural cloud panorama supplies both the
sky and reflections. `e` disables reflection; direct-light highlights remain.
These reflections show the distant panorama, not the craft or nearby objects.
There is no roughness prefilter or screen-space reflection. Refraction uses the
opaque scene as described below.

Mesh evaluation runs on the CPU and replaces water geometry and bounds each
update. This is a modest patch, not a large-ocean performance claim. All
renderers and picking see that mesh; queries sample the continuous surface.
Wireframe shows large waves and craft motion, not ripple detail or reflections.

The approach is informed by Catlike Coding's
[Waves](https://catlikecoding.com/unity/tutorials/flow/waves/),
[Swimming](https://catlikecoding.com/unity/tutorials/movement/swimming/), and
[Reflections](https://catlikecoding.com/unity/tutorials/rendering/part-8/) tutorials.

## Wake ribbons and spray

Two textured, transparent mesh ribbons follow the stern's recent path. They
spread and fade over 3.6 seconds, resample the current wave surface, and break
when the craft leaves the water or changes direction abruptly. A bounded
128-station history keeps geometry costs predictable.

Hull contact throws soft point particles, with stronger spray on turns and
bursts on landing. The existing world-space particle renderer updates a fixed
384-slot pool. Droplets follow ballistic paths and disappear on water contact
or expiry. Emission runs on the craft's fixed physics clock; hiding effects
with `f` continues the simulation, and pausing freezes it.

The ribbons and droplets use the existing transparent geometry and particle
infrastructure. Separate wake impulses deform the surface beneath them, as
described below. Hiding foam/spray with `f` does not disable displacement.

## Refraction, absorption, and soft spray

The patterned seabed slopes from shallow to deep water. `t` compares the lit
water surface with scene transmission: ripple normals distort the bottom, and
longer underwater view paths attenuate red light more strongly than green and
blue. Fresnel sky reflections remain on top. Pause with `a` to compare the same
waves and craft pose.

The water material uses `Transmission: .94`, `Refraction: .018`, and RGB
`Absorption: (.85, .28, .12)`. Absorption is a Beer–Lambert coefficient per world
unit, mixing transmitted light toward the lit water color. This approximates
the underwater segment seen through the surface; it is not a camera-underwater
volume or a fluid simulation.

Spray uses `PointStyle.SoftDistance: .16`. Its alpha fades over that world-space
view-ray distance to the scene surface, including the water. `h` switches back
to hard intersections without advancing the simulation.

Both GPU and software render opaque geometry first, capture scene color and
depth, then draw transmissive surfaces and write their depth. Blended ribbons
and particles draw afterward. Captures use the current frame and render
resolution, including supersampling. Distorted samples outside the viewport or
in front of the water fall back to the original pixel to avoid foreground leaks.
Only opaque geometry is refracted: multiple glass/water layers do not refract
one another, and transparent objects behind water are not captured. Transmission
is an open-surface approximation, not Snell-law ray tracing or volumetric transport.

## Shorelines and flow

A curved terrain bank intersects the water through the existing depth buffer.
The shoreline foam samples terrain height at each displaced water vertex and
covers the shallow depth band with a textured transparent mesh. It follows the
waves without changing the analytic surface used by the craft. `v` compares
foam and flow independently; it keeps the terrain and craft simulation intact.
The craft uses an arcade bank constraint to stay clear of dry land.

`NewFlowMap(image)` snapshots an authored red/green vector field. Red controls
normal-map U and green controls V; 0 encodes −1, 255 encodes +1, and 127/128
are neutral. Blue and alpha are ignored. In this demo the field bends along
the bank and grows stronger across the channel. Material controls are:

```go
material.FlowMap = flow
material.FlowStrength = .7     // normal-UV displacement scale per cycle
material.FlowPhase = float32(math.Mod(seconds / 3, 1))
material.FlowRepeat = false   // clamp the field at its borders
```

The field is sampled at mesh vertices using `Geometry.UV`, then interpolated
across triangles. Use enough mesh vertices to resolve changes in the field.
`NormalUV` independently sets ripple tiling. Two advection phases crossfade
through their resets, avoiding periodic jumps. `FlowStrength: 0` restores the
original normals. GPU and software use the same sampled field and phase math.
Flow only affects normal shading, including reflection and refraction; it does
not move the craft, mesh, or wake particles. Camera/reflection/soft-spray
keyboard controls remain available even when absent from the compact touch bar.

## Larger water meshes

`BuildWaveTiles` creates bounded tiles from one global rest grid:

```go
tiles, err := nt3d.BuildWaveTiles(nt3d.WaveGrid{
    NX: 161, NY: 161, TileCells: 40,
    MinX: -15, MaxX: 15, MinY: -15, MaxY: 15,
    NormalTileSize: 2.5,
    Material: material,
})
if err != nil { return err }
for i, tile := range tiles {
    if err := tile.Update(surface, seconds); err != nil { return err }
    if err := scene.SetNodeGeometry(nodes[i], tile.Geometry); err != nil { return err }
}
```

Create one scene node per tile before the update loop. `Update` reuses vertex,
tangent, and index storage, updates exact bounds, and evaluates identical global
coordinates at shared edges. Neighboring tiles match positions, normals, UVs,
and tangents exactly, including partially filled edge tiles. Base UVs span the
whole field; normal UVs are anchored to world XY. `RestBounds()` supplies the
undisplaced tile extent for application bookkeeping.

The default demo uses four 40×40-cell tiles over 10×10 units. `m` switches to
sixteen tiles over 30×30 units (161×161 global vertices), adjusts the camera,
and expands the craft/effect limits. Shrinking while the craft is outside the
smaller patch clamps it back inside and clears the displaced wake/spray.

This is finite tiled geometry, not an infinite ocean or an LOD system. Separate
nodes allow render-time frustum culling; the demo still updates every tile on
the CPU. Applications can schedule updates or stream tiles as needed. The
builder caps allocation at 1024 tiles and 1,048,576 vertices including borders;
`TileCells` defaults to 32 and must be 1–128.

## Boat-driven wake deformation

Hull contact emits paired crest/trough packets along the stern's path. They
travel outward, forming a diverging wake; stronger landings emit a short radial
burst. Each packet rises smoothly, spreads, and decays. The combined vertical
displacement is smoothly limited to 18 cm in the demo, so overlapping wakes
remain bounded. It is real mesh displacement, visible in wireframe as well as
through changing reflections and refraction.

`WaveSurface.WithWake` creates an immutable snapshot from bounded history:

```go
surfaceWithWake, err := surface.WithWake([]nt3d.WakeImpulse{
    {
        Origin: nt3dmath.Vec3{X: sternX, Y: sternY},
        Velocity: nt3dmath.Vec3{Y: .4},
        Start: seconds, Duration: 3.6,
        Amplitude: .08, Radius: .8, Spread: .12,
    },
}, .18)
if err != nil { return err }
```

`Origin` and `Velocity` are world-XY vectors with Z zero. Radius grows by
`Spread` units per second. Positive amplitude produces a crest surrounded by
a trough; negative amplitude inverts it. Up to `MaxWakeImpulses` (128) are
accepted. The method copies the history, replaces any previous wake layer, and
preserves the base Gerstner waves. An empty history removes wake displacement.

Both `Sample` and `AtWorld` include analytic wake normals, tangents, and
velocities, including the derivative of wake height along Gerstner horizontal
motion. Horizontal coordinates are unchanged, so world queries retain the
base surface's unique inversion. Use the returned surface for mesh updates,
craft support, and other surface queries.

The demo emits at fixed physics steps, freezes history into shared snapshots,
and samples that same combined surface for the craft, tile meshes, foam, and
spray contacts. Landing and contact history is bounded and expires independently
of frame rate. `z` compares deformation on/off while emission continues; toggling
preserves body pose and velocity, with subsequent steps responding to the new forces. Pausing
freezes the simulation. Foam visibility (`f`) is independent.

These are analytic wave packets, not a fluid simulation. They do not conserve
momentum, reflect from shorelines, or model hull pressure and shallow-water
hydrodynamics. They provide a practical wake that other surface-following
objects can sample and ride over.
