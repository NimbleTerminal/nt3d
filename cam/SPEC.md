# Camera Path Specification (`camera_path` v1)

`camera_path` is a declarative, host-independent format for describing 3D camera motion, orientation, timing, lens parameters, and timeline markers across the NimbleTerminal platform (`nt3d`, **Nebula**, and **`ntlayout`**).

A `camera_path` document is **pure data** — never a scripting language. Dynamic inputs (live target tracking, physics springs, user controllers, switching between cameras) are supplied at evaluation time through host resolvers or upstream **Nebula** graph nodes.

---

## 1. Design Principles

1. **Orthogonal tracks**: spatial geometry (`position`), speed profile (`timing`), aim (`look` + `roll`), and optics (`lens`) are independent tracks evaluated at absolute time $t$. Changing easing never warps the spatial path; editing control points never breaks constant-speed pacing.
2. **Arc-length parameterization**: every position curve is parameterized by normalized arc-length $s \in [0, 1]$, precomputed at load time. Linear timing produces genuinely constant world-space speed.
3. **Twist-free framing & quaternion orientation**: tangent orientation uses **parallel-transport (Bishop) frames** (never Frenet-Serret frames, which flip at inflection points), and all orientations are represented and blended as unit XYZW quaternions in `float64` (never Euler angles).
4. **Pure, deterministic evaluation**: `Eval(t)` is a pure function of `(spec, t, resolver)` in `float64`, producing identical results across live playback, timeline scrubbing, multi-viewport sync, and offline rendering.

---

## 2. Units & Coordinate Conventions

| Quantity | Spec Unit | Internal (`nt3d/cam`) Unit |
| :--- | :--- | :--- |
| Positions, distances, `near`, `far`, `focus_dist` | meters (`float64`) | meters (`float64`) |
| Time (`duration`, `t`, `from`, `over`, `blend`) | duration string (`"12s"`, `"500ms"`, `"1m30s"`) or non-negative seconds (`12.0`) | `time.Duration` / `float64` seconds |
| Angles (`lens.fov`, `roll`) | degrees (`float64`) | radians (`float64`) |
| Focal length / sensor height | millimeters (`float64`) | converted to vertical `FovY` radians |
| Aperture (`lens.aperture`) | f-number $N$ in $f/N$ (`0` = pinhole / disabled) | `float64` f-number |
| Quaternions (`q`) | `[x, y, z, w]` (unit length; normalized on load) | `Quatd{X, Y, Z, W float64}` |

### Coordinate Frames
- **Camera-local axes** (glTF 2.0 / Blender / `nt3d.Camera.Pose`): **`+X`** right, **`+Y`** up, **`-Z`** forward.
- **`frame`** (`"scene"` default, or `"enu"`):
  - `"scene"`: scene-local meters.
  - `"enu"`: local tangent East-North-Up meters (`+X` East, `+Y` North, `+Z` Up) anchored at `origin: {lat, lon, alt}` (WGS84 degrees and ellipsoidal meters).
- **`up`**: world up unit vector `[x, y, z]`, defaulting to **`[0, 0, 1]` (`+Z`)**, which matches `nt3d`, Blender, and `enu`. Unrotated Y-up glTF scenes may set `up: [0, 1, 0]`.

---

## 3. Document Structure

A spec may be encoded as **YAML** or **JSON**, either wrapped in a top-level `camera_path` object (canonical) or at the root object when embedded inside a larger scene/Nebula node config.

```yaml
camera_path:
  version: 1
  name: island-flyover        # optional identifier
  frame: scene                # "scene" (default) | "enu"
  # origin: {lat: 40.7411, lon: -73.9897, alt: 10.0}  # required when frame == "enu"
  up: [0, 0, 1]               # optional world up; default [0, 0, 1]
  duration: 12s               # total nominal duration (> 0; 0 allowed for static 1-point cameras)
  loop: none                  # "none" (default) | "loop" | "pingpong"

  position:
    interp: catmull-rom-centripetal  # default | "bezier" | "bspline" | "linear"
    closed: false                    # optional closed loop
    points:
      - [0, -10, 4]
      - [6, -6, 3.5]
      - [8, 2, 2.5]

  timing:
    start: 0.0                # optional initial arc-length s ∈ [0, 1], default 0.0
    segments:                 # may also be written directly as a YAML/JSON array under `timing:`
      - {to: 1.0, over: 12s, ease: ease-in-out}

  look:
    - {mode: target, target: "node:island", from: 0s}
    - {mode: tangent, from: 8s, blend: 1.5s, blend_ease: ease-in-out}

  roll:
    - {t: 0s, v: 0}
    - {t: 8s, v: -6, ease: ease-in-out}
    - {t: 12s, v: 0, ease: ease-in-out}

  lens:
    fov: [{t: 0s, v: 55}, {t: 12s, v: 35, ease: ease-in-out}]
    focus_dist: [{t: 0s, v: 10.0}]
    aperture: [{t: 0s, v: 2.8}]
    near: 0.05
    far: 500.0

  markers:
    - {name: reveal, t: 6s}
```

---

## 4. Track Semantics

### 4.1 Time Wrapping (`duration` & `loop`)
Given evaluation time $t$ and `duration` $D > 0$:
- `none`: $t_{\text{eff}} = \operatorname{clamp}(t, 0, D)$.
- `loop`: $t_{\text{eff}} = t \bmod D$ (wrapped into $[0, D)$, with negative $t$ wrapped positively).
- `pingpong`: $p = t \bmod (2D)$ (wrapped into $[0, 2D)$); if $p \le D$ then $t_{\text{eff}} = p$, else $t_{\text{eff}} = 2D - p$.

If $D = 0$, $t_{\text{eff}} = 0$.

### 4.2 Position Track (`position`)
 Evaluates at normalized arc-length $s \in [0, 1]$:
- **`catmull-rom-centripetal`** *(default)*: interpolating spline through `points` ($N \ge 1$; $N=1$ is stationary, $N=2$ is linear) with knot exponent $\alpha = 0.5$ (Yuksel et al. 2011). Guarantees no cusps or self-intersections within spans. Open paths reflect endpoint chords; `closed: true` ($N \ge 3$) wraps cyclically.
- **`bezier`**: piecewise cubic Bézier curve through `knots` ($N \ge 2$). Each knot specifies:
  - `p`: `[x, y, z]` anchor point, plus either:
  - explicit world-space handles `in: [x, y, z]` and `out: [x, y, z]` (omitted handles default to `p`), **or**
  - Hermite tangents `tangent_in: [vx, vy, vz]` and `tangent_out: [vx, vy, vz]`, mapped via $\text{out} = p + \text{tangent\_out}/3$ and $\text{in} = p - \text{tangent\_in}/3$.
- **`bspline`**: uniform clamped cubic B-spline ($N \ge 1$; $N < 4$ reduces degree to $N-1$) $C^2$-approximating interior `points` while clamping endpoints to `points[0]` and `points[N-1]` (or periodic uniform cubic B-spline when `closed: true`).
- **`linear`**: piecewise linear polyline through `points`.

At load time, the curve builds a monotonic arc-length lookup table $(u_k, L_k)$ so $s \in [0, 1]$ maps to total distance $s \cdot L_{\text{total}}$ with constant-speed travel. Zero-length curves ($L_{\text{total}} = 0$) evaluate to `points[0]` for all $s$.

### 4.3 Timing Track (`timing`)
Maps $t_{\text{eff}} \to s \in [0, 1]$:
- **Default** (when `timing` is omitted): linear constant speed from $s = 0$ at $t = 0$ to $s = 1$ at $t = D$ (`[{to: 1.0, over: duration, ease: linear}]`).
- **`timing` as a list of segments** `[{to, over, ease}, ...]` (or `{start, segments}`):
  - Segment $k$ spans $[T_k, T_k + \text{over}_k]$ where $T_0 = 0$ and $T_{k+1} = T_k + \text{over}_k$, interpolating from $s_k$ ($s_0 = \text{start}$, default `0`) to $s_{k+1} = \text{to}_k \in [0, 1]$ using `ease`.
  - For $t_{\text{eff}}$ after the last segment's end time, $s$ holds the final segment's `to`.

### 4.4 Easing (`ease`)
Used identically by `timing` segments, `look` blends, `roll`, and `lens` scalar/quaternion keyframes:
- `"linear"` *(default for keyframes and default timing)*: $f(u) = u$.
- `"ease"`: CSS `cubic-bezier(0.25, 0.1, 0.25, 1.0)`.
- `"ease-in"`: CSS `cubic-bezier(0.42, 0.0, 1.0, 1.0)`.
- `"ease-out"`: CSS `cubic-bezier(0.0, 0.0, 0.58, 1.0)`.
- `"ease-in-out"`: CSS `cubic-bezier(0.42, 0.0, 0.58, 1.0)`.
- `"smoothstep"`: $3u^2 - 2u^3$.
- `"smootherstep"`: $6u^5 - 15u^4 + 10u^3$.
- `"hold"`: step function ($f(u) = 0$ for $u < 1$, $f(1) = 1$).
- `[x1, y1, x2, y2]`: custom CSS `cubic-bezier(x1, y1, x2, y2)` with $x_1, x_2 \in [0, 1]$ (overshoot $y_1, y_2 \notin [0, 1]$ permitted for scalar tracks; clamped to $[0, 1]$ for normalized arc-length $s$).

When interpolating a keyframe list `[{t, v, ease}, ...]`, the `ease` field on keyframe `i + 1` governs the transition over $[t_i, t_{i+1}]$ (how the value arrives at `(t_{i+1}, v_{i+1})` from `(t_i, v_i)`, matching timing segments `{to, over, ease}`). Any `ease` on the initial keyframe `0` is ignored; before `t_0` the track clamps to `v_0`, and after `t_{n-1}` it clamps to `v_{n-1}`.

### 4.5 Orientation Track (`look` & `roll`)
`look` is a non-empty time-ordered list of segments (`from` non-decreasing, first segment `from: 0s`). If `look` is omitted, it defaults to `[{mode: tangent, from: 0s}]` when the path has non-zero length, or looking along `+Y` (horizontal North in Z-up) when stationary.

1. **`mode: "target"`**:
   - Specifies either `point: [x, y, z]` (literal coordinates in `frame`) or `target: "<scheme>:<id>"` (`node:<name>`, `panel:<id>`, `path:<name>`).
   - Forward direction is $F = \operatorname{normalize}(\text{targetPos}(t_{\text{eff}}) - \text{pos}(t_{\text{eff}}))$.
   - Right is $R = \operatorname{normalize}(F \times U_{\text{world}})$, camera up is $U = R \times F$.
   - Degenerate guards: if $\|\text{targetPos} - \text{pos}\| < 10^{-9}$, $F$ falls back to the curve tangent (or horizontal forward); if $\|F \times U_{\text{world}}\| < 10^{-9}$ (looking straight along $\pm U_{\text{world}}$), $U$ is chosen deterministically perpendicular to $F$ without NaN.
2. **`mode: "tangent"`**:
   - Uses **parallel-transport (Bishop) frames** precomputed along the position curve at load time (Hanson & Ma, 1995), seeded at $s = 0$ by projecting world `up` perpendicular to $T(0)$.
   - For reversed motion ($ds/dt < 0$, e.g. the return leg of `pingpong` unless `keep_forward: true`), `tangent` faces $T(s)$ in the path's authored forward direction by default (`reverse_with_motion: true` optionally flips to $-T(s)$).
3. **`mode: "keyframes"`**:
   - `keys: [{t, q: [x, y, z, w], ease}, ...]` with `interp: "squad"` *(default for $\ge 3$ keys)* or `"slerp"`.
   - Adjacent quaternions are sign-canonicalized ($q_k \cdot q_{k+1} \ge 0$) at load time for shortest-arc interpolation.

#### Blending Between Segments
When segment $i > 0$ sets `blend` ($b > 0$), for $t_{\text{eff}} \in [\text{from}_i, \text{from}_i + b]$:
$$q_{\text{base}}(t) = \operatorname{slerp}\!\bigl(q_{i-1}(t),\; q_i(t),\; \operatorname{ease}_{\text{blend}}((t - \text{from}_i)/b)\bigr)$$
Overlapping blends chain sequentially from segment $0$ upward so evaluation remains pure and $C^0$-continuous.

#### Roll (`roll`)
Scalar keyframe track in **degrees** (default `0°`). Composed onto $q_{\text{base}}$ around local forward `-Z`:
$$q(t) = q_{\text{base}}(t) \otimes \operatorname{QuatAxisAngle}\bigl((0, 0, -1),\; \operatorname{rad}(\text{roll}(t))\bigr)$$

### 4.6 Lens Track (`lens`)
- **`fov`**: scalar keyframes in vertical degrees `(0, 180)`, default `45°`.
- **`focal_length`** & **`sensor_height`**: alternative to `fov`; `focal_length` keyframes in mm `(> 0)` with static `sensor_height` in mm (`24.0` default), converted via $\text{fov}_y = 2 \arctan(\text{sensor\_height} / (2 f))$.
- **`focus_dist`**: scalar keyframes in meters (`>= 0`; `0` = unset / infinity).
- **`aperture`**: scalar keyframes in f-stops (`>= 0`; `0` = pinhole / DoF off).
- **`near`**, **`far`**: static clip planes in meters (`>= 0`; `0` = host automatic; if both `> 0`, requires `far > near`).

### 4.7 Named Markers (`markers`)
Optional list of `{name: string, t: duration}` pairs with unique non-empty names and $0 \le t \le \text{duration}$ (when `duration > 0`), used by Nebula switchers, timeline UIs, and cue synchronization.

---

## 5. External Reference Grammar (`Resolver`)

Target strings in `look` use `<scheme>:<identifier>`:
- `node:<name>`: world-space origin/pivot of a named scene node (e.g. `"node:island"`).
- `panel:<id>`: center of a spatial `ntlayout` panel (e.g. `"panel:telemetry"`).
- `path:<name>`: position evaluated on another named camera/object path at the same timestamp $t$ (e.g. `"path:lead-craft"`).

If a reference is unresolved at `Eval(t)` (or `Resolver` is `nil`), `look` falls back deterministically to `fallback` (`[x, y, z]` if provided on the segment, otherwise `[0, 0, 0]`) without panicking or mutating state.

---

## 6. Validation & Round-Trip Guarantees

1. `nt3dcamspec.Parse` accepts both YAML and JSON, validates all invariants (version `== 1`, finite coordinates, strictly increasing keyframe times, valid quaternions, positive FOV in `(0, 180)`, valid CSS `cubic-bezier` $x \in [0, 1]$), and returns descriptive errors.
2. `Spec → Path → Spec` preserves track structure, interpolation modes, control points, timing segments, look segments, keyframes, lens settings, and markers within floating-point formatting precision.
