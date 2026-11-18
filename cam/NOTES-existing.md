# Phase 0 Notes: Existing Camera, Animation, and Prior Art

This document surveys `nt3d`'s current camera, scene, and animation architecture, records the integration boundaries for **Nebula** and **`ntlayout`**, and summarizes the mathematical and authoring references backing `nimbleterminal.dev/nt3d/cam`.

---

## 1. NT3D's Camera, Scene Graph, and Precision Today

### Camera representation (`camera.go`, `scene_camera.go`)
- **`nt3d.Camera`** (`camera.go:33-52`) supports two pose modes:
  1. **Orbit camera (default when `Pose == Mat4{}`)**: parameterized by spherical elevation `Alpha` (`[-89°, +89°]`), azimuth `Beta` (degrees around `+Z`), radial `Distance` (`[1e-4, 1e7]`), and `Target` (`nt3dmath.Vec3`), assuming world `+Z` is up (`camera.go:134-139`). `LookAt` and `LookDirection` (`camera.go:317-348`) write into these spherical orbit parameters and clamp elevation to `±89°`.
  2. **Explicit 6-DOF pose (`Camera.Pose`, `nt3dmath.Mat4`)**: an affine camera-to-world matrix (`camera.go:34-36, 127-132`). In camera-local space:
     - **`+X`** is right,
     - **`+Y`** is up,
     - **`-Z`** is forward (`forward = Pose.Direction(Vec3{Z: -1}).Normalize()`).
     - `Camera.Pose` has **no** `±89°` elevation clamp and naturally represents arbitrary unit-quaternion orientations and camera roll without gimbal lock.
- **Scene camera nodes** (`scene_camera.go:14-60`): `Scene.SetNodeCamera(id, cam)` attaches projection settings to any non-root scene node, and `Scene.NodeCamera(id)` resolves its world `Mat4` into `Camera.Pose` (matching glTF 2.0 `cameras` decoded in `mesh/gltf/cameras.go:12-60`).
- **Coordinate frame & units**:
  - **World space** across `nt3d` (`camera.go`, `environment.go`, `water/`, `toon`) is **right-handed Z-up** (`+Z` up), which also matches **Blender** and GIS **East-North-Up (ENU: `+X` East, `+Y` North, `+Z` Up)**.
  - **Camera-local space** is **right-handed OpenGL / glTF / Blender camera space** (`+X` right, `+Y` up, `-Z` forward).
  - Units are scene world units (conventionally **meters**, e.g. `toon`'s Manhattan block and glTF 2.0 assets).
- **Projection & lens**:
  - `Projection` (`Perspective` or `Orthographic`), vertical `YFOV` in radians (`0` defaults to `π/4 = 45°`), `AspectRatio` (`0` uses viewport `width/height`), `Near` and `Far` clip planes in `float64` (`0` uses `Distance`-scaled defaults `0.05/3.4` and `200/3.4`, and `Near > 0, Far == 0` produces an infinite WebGPU `[0, 1]` perspective far plane).
  - Note: `nt3d.Camera` does not yet carry `FocusDist` or `Aperture` fields; `CameraState` carries them for Nebula / future Depth-of-Field post-processing.

### Render loop & animation today (`renderer.go`, `animation.go`, `widget/`)
- **Render loop**: `Scene.Frame(camera, width, height)` (`frame.go`) snapshots visible scene nodes, world `Mat4` transforms, `Camera.Matrix(aspect)`, `Eye`, and `Forward` into an immutable `Frame` value consumed identically by WebGPU (`GPURenderer` / `GPUSession`), `SoftwareRenderer`, and `RenderWireframe`.
- **`nt3dwidget.Model`** (`widget/model.go`, `widget/update.go`): holds `camera nt3d.Camera`, supports `SetCamera` / `Camera`, mouse orbit/pan/zoom, `WithDefaultKeyBindings(false)` for host-driven controls, and `WithFrameDecorator(func(*nt3d.Frame))` for dynamic debug overlays (`Frame.DynamicLines`).
- **Scene animation (`animation.go`)**: `AnimationClip` and `Track` implement glTF 2.0 keyframe playback (`Translation`, `Rotation` XYZW quaternions, `Scale`, `MorphWeights`, `UserChannel`) with `Linear` (shortest-arc `slerp` via `mixQ` for rotations), `Step`, and Hermite `CubicSpline`, plus `Once`, `Loop`, and `PingPong` time wrapping (`animation.go:261-278`). All `Track` times and values are `float32`, without arc-length parameterization or parallel-transport frames.

### Float precision & why camera-relative `float64` origin is needed
- Today in `nt3d`:
  - `Camera.YFOV`, `Near`, `Far`, `Distance`, `Alpha`, `Beta` are `float64`.
  - **`nt3dmath.Vec3` and `nt3dmath.Mat4` are `float32`** (`math/vec3.go:8`, `math/mat4.go:6`).
  - `Camera.Matrix(aspect)` (`camera.go:144-188`) builds `view` in `float32` world coordinates (`-r.Dot(e)`, `-u.Dot(e)`, `f.Dot(e)`), and `Scene.Frame` emits `Instance.World` in world `float32`.
- At ENU / geographic offsets (`10⁵–10⁷` m from origin), `float32` ULP spacing reaches `0.008 m – 1.0 m`, causing severe vertex and camera jitter if positions are cast to `float32` before subtracting the camera position.
- **Planned NT3D Phase 3 enhancement**:
  - Keep `nt3d/cam` **100% `float64`** (`Vec3d`, `Quatd`, `CameraState`).
  - Add `float64` world-origin support to `nt3d` (a `float64` `[3]float64` world origin on `Camera` and scene nodes / `Scene`), subtracting `cameraOrigin` from node origins in `float64` before casting camera-relative translations into `Instance.World` (`float32`), `DynamicLines`, light positions, picking rays, and constructing `Camera.Matrix` with eye at `(0, 0, 0)` in camera-relative space. Scenes near `(0, 0, 0)` behave identically with zero overhead; geographic-scale scenes render jitter-free.

---

## 2. Nebula Graph Spec & Execution Model (Integration Contract)

The Nebula repository is not checked out in this workspace and its direct node wiring is deferred until Nebula integration, so `nt3d/cam` is designed around Nebula's push/pull dataflow contract with zero host coupling:

- **Node shape (`camera_path` node)**:
  - **Inlets**:
    - `path`: declarative `camera_path` spec (`nt3dcamspec.Document` / compiled `*nt3dcam.Path`).
    - `clock`: evaluation timestamp `t time.Duration` (from a transport / timeline clock).
    - Optional live overrides (`nt3dcam.EvalOverrides`):
      - `target`: live `Vec3d` stream overriding `"target"` look segments.
      - `points`: live `[]Vec3d` control-point stream (rebuilding only the curve & arc-length LUT when upstream points change).
      - `roll`: live `float64` roll override/offset in degrees (converted to radians at boundary).
  - **Outlet**: `CameraState` stream (`Pos`, `Rot`, `FovY`, `FocusDist`, `Aperture`, `Near`, `Far`).
- **Push vs. Pull execution**:
  - **Push mode**: a realtime transport ticks `t = t₀ + (now - start) * rate` every frame and pushes `t` into `clock`, emitting `CameraState`.
  - **Pull mode**: timeline scrubbing, multi-surface sync, or offline frame rendering requests `Eval(t)` at any arbitrary `t` (forward, backward, or out-of-order). Because `Path.Eval` / `Path.EvalWith` are **pure, stateless, and deterministic**, push and pull yield bit-identical `CameraState` outputs for the same `t`.
- **Minimal transport proposal (if Nebula lacks one when wired)**:
  - A transport value `{ Time time.Duration, Playing bool, Rate float64, Loop LoopMode }` plus named marker cue events derived deterministically from `Path.Markers()` whenever `[t_prev, t_curr]` crosses a marker timestamp.
- **Camera Switcher node**:
  - Backed directly by `nt3dcam.Blend(a, b CameraState, alpha float64) CameraState` and `nt3dcam.Switch(states []CameraState, select float64, ease EaseSpec) CameraState`: cuts (`alpha ∈ {0, 1}`) and crossfades (`alpha ∈ (0, 1)`) are pure functions of upstream `CameraState` inlets.

---

## 3. Survey of Existing Animation & Easing Code

| Path | Description | Reuse / Disposition |
| :--- | :--- | :--- |
| `animation.go:30-45` | `Interpolation` (`Linear`, `Step`, `CubicSpline`) and `LoopMode` (`Once`, `Loop`, `PingPong`) for `float32` glTF tracks. | Keep for glTF mesh/skeleton clips; match its `Loop` / `PingPong` boundary semantics in `float64` `time.Duration` for `nt3d/cam`. |
| `animation.go:126-150` | `mixQ`: shortest-arc `float32` quaternion `slerp` with `dot < 0.9995` linear fallback. | Reimplement in `float64` (`Quatd.Slerp` + `Squad`) in `nt3d/cam` with tighter `float64` threshold (`1 - 1e-9`) and antipodal guard. |
| `animation.go:180-185` | `float32` 1D Hermite cubic spline (`2u³ - 3u² + 1`, etc.). | Useful reference for Hermite-to-Bézier equivalence (`b₁ = p₀ + m₀/3`, `b₂ = p₁ - m₁/3`). |
| `shading.go` / WGSL | Scalar `smoothstep` for spotlight cones and cel bands (`float32`). | Trivial 3-line polynomial (`3u² - 2u³`); reimplement in `float64` in `nt3d/cam/ease`. |
| Charm `harmonica` (external ecosystem) | Stateful spring damper (`Update(pos, vel)` per tick). | **Do not use in `nt3d/cam`**: stateful frame integration violates pure `Eval(t)`. Springs belong in upstream Nebula nodes. |

Because `nt3d/cam` requires zero non-stdlib dependencies and `float64` precision throughout, `nt3d/cam/ease` provides a self-contained, generic `float64` easing and keyframe library (`linear`, `ease`, `ease-in`, `ease-out`, `ease-in-out`, `smoothstep`, `smootherstep`, `hold`, and CSS `cubic-bezier(x1, y1, x2, y2)` solved via Newton-Raphson with bisection fallback to `< 1e-12`), designed so future 2D `ntmaps` camera tracks can share it directly.

---

## 4. `ntlayout` `spatial/` Target & Framing Design

`ntlayout` is not part of this repository, so `nt3d/cam` exposes host-agnostic interfaces ready for `ntlayout`'s `Plane`, `Cylinder`, and `Billboard` surfaces:

- **Target resolution (`"panel:<id>"`)**:
  - `Resolver` (`ResolveTarget(ref string, t time.Duration) (TargetInfo, bool)`) resolves `"node:<name>"`, `"panel:<id>"`, or `"path:<name>"` into a `float64` target position `Pos Vec3d` (and optional surface normal `Normal Vec3d` + `Width, Height float64` extent when framing a spatial UI panel).
- **Framing helper (`FitRect`)**:
  - Pure helper `FitRect(center, normal, up Vec3d, width, height, fovYRad, aspect, margin float64) (eye Vec3d, rot Quatd)` computes the exact camera distance:
    $$d = \text{margin} \cdot \max\!\left(\frac{h/2}{\tan(\text{fov}_y/2)},\; \frac{w/2}{\text{aspect}\cdot\tan(\text{fov}_y/2)}\right)$$
    and places the camera at `center + normal * d` aimed along `-normal`, enabling `"fly to panel X"` workflows without coupling `nt3d/cam` to `ntlayout`.

---

## 5. Mathematical & Authoring References

1. **Centripetal Catmull-Rom Splines** — Cem Yuksel, Scott Schaefer, John Keyser, *"Parameterization and Applications of Catmull-Rom Curves"*, Computer-Aided Design 43(7), 2011 ([paper](http://www.cemyuksel.com/research/catmullrom_param/catmullrom.pdf)):
   - Knot spacing $t_{i+1} = t_i + \|P_{i+1} - P_i\|^\alpha$ with $\alpha = 0.5$ (centripetal) is the **unique** value in Barry & Goldman's pyramidal formulation guaranteed **never to form cusps or self-intersections** within a curve segment, even when control points have wildly uneven spacing (e.g. a tight corner after a long straightaway, where uniform $\alpha = 0$ overshoots and loops).
2. **Arc-Length Parameterization by Lookup Table** — Brian Guenter & Richard Parent, *"Computing the Arc Length of Parametric Curves"*, IEEE CG&A 1990; Walter & Fournier 1997:
   - Spline parameter $u$ speeds up on long spans and slows down on short spans ($\|C'(u)\| \neq \text{const}$). Integrating segment speed $\int \|C'(u)\|\,du$ via 5-point Gauss-Legendre quadrature over subdivided spans builds a strictly monotonic cumulative table $(u_k, L_k)$ at load time. At `Eval(t)`, normalized distance $s \in [0, 1]$ maps to $L = s \cdot L_{\text{total}}$, locates the bracket in $O(\log M)$ via binary search, and refines $u$ with 1–2 safeguarded Newton steps ($u \leftarrow u - (L(u) - L)/\|C'(u)\|$) for sub-$10^{-9}$ constant-speed accuracy.
3. **Parallel-Transport Frames vs. Frenet-Serret Frames** — Andrew J. Hanson & Hui Ma, *"Parallel Transport Approach to Curve Framing"*, Indiana University Tech Report TR425, 1995 ([report](https://legacy.cs.indiana.edu/ftp/techreports/TR425.pdf)):
   - The Frenet-Serret normal $N = T' / \|T'\|$ is undefined where curvature vanishes ($C''(u) \parallel C'(u)$) and **flips $180^\circ$ instantaneously** across inflection points (such as a planar S-curve).
   - Bishop / parallel-transport frames propagate an initial up vector $U_0 \perp T_0$ along sample tangents $T_k \to T_{k+1}$ using the minimal-rotation axis $B = T_k \times T_{k+1}$ and angle $\theta = \arccos(T_k \cdot T_{k+1})$ (with continuous 180° fallback if antipodal), producing twist-minimizing orthonormal frames $(R_k, U_k, T_k)$ precomputed alongside the arc-length table at load time and interpolated in $O(\log M)$ at eval time.
4. **Quaternion `slerp` and `squad`** — Ken Shoemake, *"Animating Rotation with Quaternion Curves"*, SIGGRAPH 1985 ([doi:10.1145/325334.325242](https://doi.org/10.1145/325334.325242)) & 1987 tutorial:
   - Double-cover canonicalization ($q_{i} \cdot q_{i+1} < 0 \implies q_{i+1} \leftarrow -q_{i+1}$) guarantees shortest-path geodesics on $S^3$.
   - `slerp(q₀, q₁, u)` traces constant-angular-velocity arcs between two orientations ($C^0$ across interior keys).
   - `squad(q_i, q_{i+1}, a_i, a_{i+1}, u) = slerp(slerp(q_i, q_{i+1}, u), slerp(a_i, a_{i+1}, u), 2u(1-u))` with inner quadrangle tangents $a_i = q_i \exp\!\bigl(-\tfrac{1}{4}(\log(q_i^{-1} q_{i+1}) + \log(q_i^{-1} q_{i-1}))\bigr)$ achieves $C^1$-continuous angular velocity through keyframed orientations.
5. **Blender & glTF 2.0 Camera Authoring Prior Art**:
   - **Blender** (*Camera Object*, *Follow Path* constraint with *Fixed Position* `offset_factor ∈ [0,1]`, *Track To* `-Z` forward / `Y` up): separates the 3D curve object from the `offset_factor` evaluation f-curve (equivalent to our `position` vs. `timing` split!) and separates aim (*Track To* / *Follow Curve*) from camera roll and 36×24 mm full-frame lens focal length ([Blender Manual: Follow Path](https://docs.blender.org/manual/en/latest/animation/constraints/relationship/follow_path.html), [Track To](https://docs.blender.org/manual/en/latest/animation/constraints/tracking/track_to.html)).
   - **glTF 2.0** ([glTF 2.0 Cameras & Animations](https://registry.khronos.org/glTF/specs/2.0/glTF-2.0.html#cameras)): local camera looks along `-Z` with `+Y` up and `+X` right; perspective `yfov` is vertical field of view; rotations are unit XYZW quaternions (`[x, y, z, w]`).
