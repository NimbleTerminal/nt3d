# Design Decisions (`nt3d/cam`)

This file records design and convention choices made where requirements left room for interpretation, following Blender and glTF 2.0 conventions unless purity or determinism dictates otherwise.

1. **Package path, identifier, and dependencies**
   - Core evaluator lives at `nimbleterminal.dev/nt3d/cam` (`package nt3dcam`), matching `nt3dmath`, `nt3dmesh`, `nt3dwater`, `nt3dfur`, `nt3dclod`, `nt3daabbtree`, `nt3dsh`, and `nt3dwidget`.
   - Spec serialization lives at `nimbleterminal.dev/nt3d/cam/spec` (`package nt3dcamspec`), with subpackages `cam/curve`, `cam/frames`, and `cam/ease`.
   - `nt3d/cam` and all its subpackages (`curve`, `frames`, `ease`, `spec`) use **only the Go standard library** (zero external module dependencies), keeping `nt3d`'s `go.mod` completely free of new third-party dependencies. `cam/spec` supports full JSON (`encoding/json`) plus a bounded, deterministic YAML 1.2 document reader/writer covering the complete `camera_path` grammar (block and flow mappings/sequences, comments, strings, numbers, booleans, durations).

2. **World coordinate frame & camera-local axes**
   - **Camera-local axes** follow **glTF 2.0 / Blender / OpenGL / `nt3d.Camera.Pose`**: `+X` right, `+Y` up, `-Z` forward. `CameraState.Rot` (`Quatd`, XYZW) is the camera-to-world rotation: `forward = Rot.Rotate(0, 0, -1)`, `up = Rot.Rotate(0, 1, 0)`, `right = Rot.Rotate(1, 0, 0)`.
   - **World up axis** defaults to **`[0, 0, 1]` (`+Z`)**, matching `nt3d`, Blender, and GIS **ENU (`+X` East, `+Y` North, `+Z` Up)**. Paths authored for unrotated Y-up glTF scenes may set `up: [0, 1, 0]` (or any unit vector) at the top level of `camera_path`.

3. **Roll convention**
   - `roll` is specified in **degrees** in the spec (`0°` default) and stored in **radians** internally.
   - Applied after the base `look` orientation as a right-handed local rotation around the camera's forward axis (`forward = -Z` local), i.e. `Rot = Rot_base * QuatFromAxisAngle((0, 0, -1), rollRad)`: a positive roll tilts the camera's `+X` right axis *down* / rolls the image horizon counterclockwise (matching aeronautical right-wing-down bank and right-handed rotation about the viewing ray).

4. **Causal `look` segment blending (`blend`)**
   - When `look[i]` (`i > 0`) sets `blend: d` (`d > 0`), the crossfade runs causally over `[from_i, from_i + d]` using shortest-arc `slerp(q_{i-1}(t), q_i(t), ease(u))` where `u = (t - from_i) / d`, defaulting to `ease-in-out`. Setting `blend: 0s` (or omitting `blend`) performs an instant cut at `t = from_i`.

5. **Quaternion keyframe interpolation default**
   - `look` segments with `mode: keyframes` support `interp: squad` and `interp: slerp`, defaulting to **`squad`** when 3 or more keyframes are present ($C^1$ smooth angular velocity through poses, degenerating cleanly to `slerp` for 2 keyframes and constant for 1 keyframe). Explicit per-key `ease: hold` steps discretely as in glTF `STEP`.

6. **Centripetal Catmull-Rom endpoint & closed-loop behavior**
   - Open `catmull-rom-centripetal` paths synthesize phantom endpoints by chord reflection ($P_{-1} = 2P_0 - P_1$, $P_n = 2P_{n-1} - P_{n-2}$, with non-zero knot deltas guarded against coincident points) so the curve passes smoothly through both `points[0]` and `points[n-1]`, matching Blender/standard centripetal splines.
   - Setting `closed: true` wraps control points cyclically ($P_{-1} = P_{n-1}$, $P_n = P_0$, $P_{n+1} = P_1$) for seamless orbit loops.

7. **Bézier & Hermite input equivalence**
   - `interp: bezier` accepts control knots with either explicit handle positions (`in`, `out` in world coordinates, as in Blender Bézier curves) or tangent vectors (`tangent_in`, `tangent_out`), related exactly by `out = p + tangent_out / 3` and `in = p - tangent_in / 3`.

8. **Lens vertical FOV vs. focal length + sensor height**
   - Following `nt3d.Camera.YFOV` and glTF `yfov`, `lens.fov` specifies **vertical** field of view in degrees `(0, 180)` (default `45°`, matching `nt3d.DefaultCamera()`).
   - Alternatively, `lens.focal_length` (mm) with optional `lens.sensor_height` (mm, default `24.0` mm for standard 36×24 mm full-frame 35 mm film/sensor, matching Blender's camera sensor) converts deterministically via $\text{fov}_y = 2 \arctan(\text{sensor\_height} / (2 \cdot f))$. `fov` and `focal_length` are mutually exclusive in a single spec.
   - Static `near` and `far` clip planes (meters, `0` = host automatic clipping) travel on `CameraState` so camera switchers and `nt3d.Camera` need no side-channel state.

9. **Future-proofing hook for captured trajectories (`samples`)**
   - `PositionSpec` reserves `samples` (timestamped poses for offline GNSS/IMU smoothing in v2) and rejects it with a clear `"unsupported in v1"` error during validation so v1 consumers fail cleanly without changing `Path.Eval`.
