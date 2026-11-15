# `nt3d/cam` — Declarative Camera Paths

`nimbleterminal.dev/nt3d/cam` is a pure, deterministic, `float64`, zero-dependency camera-path specification and evaluator designed for NT3D scenes, **Nebula** reactive node graphs, and **`ntlayout`** spatial UI framing.

## Packages

| Package | Description |
| :--- | :--- |
| `nimbleterminal.dev/nt3d/cam` (`nt3dcam`) | Precompiled `Path`, `CameraState`, `Eval` / `EvalWith` / `EvalOverrides`, `Blend`, `Switch`, and `FitRect` |
| `nimbleterminal.dev/nt3d/cam/spec` (`nt3dcamspec`) | Zero-dependency YAML 1.2 & JSON `camera_path` v1 parser, validator, JSON Schema (`ntcamera.schema.json`), and round-trip marshaler |
| `nimbleterminal.dev/nt3d/cam/curve` (`nt3dcurve`) | Centripetal Catmull-Rom ($\alpha = 0.5$), cubic Bézier/Hermite, clamped/periodic B-splines, and Gauss-Legendre arc-length LUT |
| `nimbleterminal.dev/nt3d/cam/frames` (`nt3dframes`) | Unit XYZW quaternions (`Slerp`, $C^1$ `Squad`), `LookRotation`, and twist-free Hanson & Ma parallel-transport (Bishop) frames |
| `nimbleterminal.dev/nt3d/cam/ease` (`nt3dease`) | CSS `cubic-bezier` presets & custom curves, `smoothstep`, `smootherstep`, `hold`, and $O(\log n)$ scalar keyframe tracks |

All five packages depend **only on the Go standard library**.

## Quickstart

```go
raw := []byte(`
camera_path:
  version: 1
  duration: 12s
  position:
    interp: catmull-rom-centripetal
    points: [[0, -10, 4], [6, -6, 3.5], [8, 2, 2.5]]
  timing:
    - {to: 1.0, over: 12s, ease: ease-in-out}
  look:
    - {mode: target, target: "node:island", from: 0s}
    - {mode: tangent, from: 8s, blend: 1.5s}
  lens:
    fov: [{t: 0s, v: 55}, {t: 12s, v: 35, ease: ease-in-out}]
`)

path, err := nt3dcamspec.Compile(raw, nt3dcam.WithResolver(nt3d.SceneResolver(scene, nil)))
if err != nil {
    log.Fatal(err)
}

state := path.Eval(4500 * time.Millisecond)
camera := nt3d.CameraFromState(state)
```

## Architecture Highlights

- **Orthogonal tracks**: `position` (spatial curve parameterized by normalized arc-length $s \in [0, 1]$), `timing` ($t \to s$), `look` (`target`, twist-free `tangent`, or `squad`/`slerp` `keyframes` with causal crossfades), `roll` (right-handed bank around camera-local `-Z`), `lens` (`fov` or `focal_length`, `focus_dist`, `aperture`, `near`, `far`), and named `markers`.
- **Constant-speed arc-length LUT**: 5-point Gauss-Legendre quadrature per sub-interval plus binary search and 2 safeguarded Newton refinement steps give `< 0.05%` speed uniformity even across extreme control-point spacing changes.
- **No Frenet flips, no gimbal lock**: `tangent` mode precomputes parallel-transport (Bishop) frames across inflection points; all orientations are stored and blended as `float64` unit quaternions.
- **Full `float64` world precision**: `CameraState.Pos` stays in `float64` meters so NT3D subtracts `Camera.Origin` in `float64` before `float32` rasterization/GPU submission, preventing vertex jitter at geographic (`FrameENU` / UTM) coordinates.

## Benchmarks

Measured with `go test -bench=. -benchmem ./cam` on an **Apple M5 Max** (`darwin/arm64`, Go 1.26.8):

| Benchmark | Control Points | Time / op | Bytes / op | Allocs / op |
| :--- | ---: | ---: | ---: | ---: |
| `BenchmarkEval/Points_10` | 10 | **192.9 ns** | 0 B | 0 |
| `BenchmarkEval/Points_100` | 100 | **206.6 ns** (0.21 µs) | 0 B | 0 |
| `BenchmarkEval/Points_1000` | 1,000 | **227.0 ns** | 0 B | 0 |
| `BenchmarkNewPath/Points_10` | 10 | 12.3 µs | 21.5 KB | 24 |
| `BenchmarkNewPath/Points_100` | 100 | 126.6 µs | 195.0 KB | 24 |
| `BenchmarkNewPath/Points_1000` | 1,000 | 1.27 ms | 1.89 MB | 24 |

See [SPEC.md](SPEC.md) for the complete `camera_path` v1 specification and [DECISIONS.md](DECISIONS.md) for convention rationale.
