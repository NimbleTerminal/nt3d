---
title: "Collision"
weight: 82
---

# Collision

`nt3d/collide` (package `nt3dcollide`) answers geometric questions about one shape against static triangles: where a ray hits, and where a capsule ends up when it moves, collides, and slides. It is what a walking camera or a character needs to stay out of walls and on the floor.

It is not a physics engine. It keeps no state between calls and advances no clock: you pass a pose in and get a pose and its contacts back. Nothing has mass, friction, or restitution, and nothing moves but the one shape you ask about. Bodies that push each other belong to a physics engine, outside nt3d.

The package imports only `nt3d/math`, so a server or a tool can use it without the renderer. Coordinates are Z-up `float64` (`nt3dmath.Vec3d`), and lengths are in the scene's units.

## A collision mesh

A `Mesh` is an immutable tree of triangles, built once and safe to query from many goroutines.

```go
// From triangles of your own:
mesh := nt3dcollide.NewMeshFromTriangles(triangles)

// From what a scene draws, with each geometry's world transform:
mesh := nt3dcollidescene.NewMesh(geometry, world)
mesh := nt3dcollidescene.NewMeshFromGeometries(geometries, worlds)
```

`nt3d/collide/scene` (package `nt3dcollidescene`) is the only package that joins collision to the engine. It takes a geometry's indexed triangles and leaves its points, lines, and boxes out. The mesh is static: when the geometry moves, build another.

Collide against something simpler than what you draw. A floor, walls, and ramps as a few hundred triangles are faster and behave better than a scanned model's millions, and a Gaussian splat capture has no triangles at all, so it needs a proxy mesh anyway (the same one can be its [holdout](../materials/#holdouts)).

## Ray casts

```go
hit, ok, err := mesh.RayCast(origin, direction, maxDistance)
if ok {
    // hit.Position, hit.Normal, hit.Distance
}
```

`RayCast` returns the nearest hit within `maxDistance`, which may be `+Inf`. The direction need not be unit length. A miss is `ok == false` with a nil error, and so is a nil or empty mesh. An error means the ray could not be cast: a non-finite origin or direction, a direction with no length, or a negative distance.

This is a different question from [picking](../picking/). `Scene.Pick` finds what is under a pixel, through the camera, including points and lines and the node they belong to. `RayCast` knows only triangles and positions, and suits line of sight, ground height under a point, and the like.

## Moving a capsule

A `Capsule` stands upright on its base: `Radius`, and `Height` from base to crown, at least twice the radius. `MoveCapsule` moves it by a velocity for a time step and resolves what it hits.

```go
body := nt3dcollide.Capsule{Radius: 0.4, Height: 1.8}
cfg := nt3dcollide.MoveConfig{StepHeight: 0.3, GroundSnap: 0.2}

// Each tick: apply your own gravity and input to vel, then
vel.Z -= 9.81 * dt
res, err := nt3dcollide.MoveCapsule(mesh, body, pos, vel, dt, grounded, cfg)
pos, vel, grounded = res.Position, res.Velocity, res.Grounded
```

You keep the position, the velocity, and whether it was grounded, and pass them back next time. Gravity, jumping, and input are yours: change the velocity before the call.

| `MoveConfig` field | Zero means | What it does |
| --- | --- | --- |
| `MaxSlope` | 50 | The steepest slope, in degrees from horizontal, that counts as ground; steeper is a wall to slide along |
| `StepHeight` | no stepping | The tallest ledge a grounded capsule steps up onto, such as a curb or a stair |
| `GroundSnap` | no snapping | How far down a grounded capsule is pulled to stay on a ramp or over a crest instead of flying off it |
| `SkinWidth` | 0.005 | The small gap kept from surfaces |

The `MoveResult` gives the resolved `Position` and `Velocity`, with the part of the velocity that ran into a surface removed. `Grounded` and `GroundNormal` say whether it stands on walkable ground and which way that ground faces; when several contacts are walkable, the normal is the most upright one. `HitWall`, `WallNormal`, and `ImpactSpeed` report the hardest contact that was not ground, for a bump sound or fall damage.

The move is taken in substeps of at most half the radius, so neither speed nor a long time step carries the capsule through a surface. Wrong input is an error and leaves the pose alone: a capsule or config that fails `Validate`, or a position, velocity, or `dt` that is not finite.

The arithmetic is written so that no platform fuses a multiply with an add, which is what makes floating-point results differ between machines; the aim is that a server and its clients compute the same pose.
