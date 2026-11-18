# Working on nt3d

Rules and invariants for agents and humans changing this repository. To *use*
nt3d, read the package documentation (`go doc nimbleterminal.dev/nt3d`) and the
guide in `docs/hugo/content/guide/`. To change it, read the guide's
[Architecture](docs/hugo/content/guide/architecture.md) page first: it maps
packages and files and explains what holds the three renderers together.

## The shape to keep

- **One pipeline, one direction:** `Scene` → `Frame` → `Renderer`. Renderers
  read frames and never the scene.
- **Say it on the scene.** Draw order, application data, and the look
  (lighting, fog, background, post-processing) are set on the `Scene` and
  reach the frame through `Draw` and `Look`. Do not add things callers must
  patch onto each frame, or encode into node names.
- **`Geometry` is plain data.** No unexported fields; a test enforces it. What
  the scene learns about a geometry travels beside it in the `Draw`.
- **Angles on cameras and lights are degrees,** named for what they measure
  (`Elevation`, `Azimuth`, `FOV`, `InnerAngle`). Package `math` takes
  radians, as Go's `math` does. Convert where a file's radians come in.
- **Wrong input is an error, not a correction.** `Geometry`, `Material`, and
  `Camera` each have one set of rules (`Validate`), and the scene returns its
  error. Only two things are corrected silently, and both are documented: a
  zero means the default, and an orbit camera's angles and distance are held
  to their limits.
- **A zero value should draw something sensible.** `Draw{Geometry: g}` alone
  draws `g`. Prefer `Hidden`-style flags, where zero is the usual case.
- **The engine links no terminal UI.** Only `widget` imports Bubble Tea; a
  test fails if the root package, `water`, `fur`, `mesh`, `clod`, `cam`, `sh`,
  `aabbtree`, or `math` does.
- **The engine knows nothing of camera paths.** `nt3d/cam` does not import the
  engine and the engine does not import it; `nt3d/cam/scene` joins them.
- **Shading is written twice,** in Go and in WGSL. Change both, and add or
  extend a `_gpu_test.go` that compares them.
- **Go and WGSL layouts are mirrored by hand.** A layout test compares byte
  counts with the shader's structs; keep its table current when adding a
  buffer.
- **Math lives in `math`.** Vectors, quaternions, matrices, bounds, and ray
  and view-volume tests are written there once, in float32 and float64, and
  used from everywhere else, the camera packages included.
- **Untrusted input never panics.** Decoders return errors; each has a fuzz
  target.

## Workflow

- `task ci` is the gate: format check, race tests, vet (which type-checks the
  GPU tests), native builds, and the WebAssembly gallery build.
- `task test-gpu` runs the GPU tests. They need a real adapter, carry the
  `webgpu` build tag, and must be named `TestGPU…` to be run. CI does not run
  them, so run them yourself after touching a renderer, a shader, or a frame.
- **A shader change is not verified until a browser has compiled it.** Browsers
  use Tint, which rejects some WGSL the native translator accepts.
  `task build-web` then `task serve`, and load a demo that uses the shader.
  Moving shader text without changing a byte needs no such check.
- `task fuzz-smoke` runs every fuzz target briefly. The targets also run on
  the NimbleTerminal fuzzing fleet, where a seed over about 1 KB stalls
  minimization: keep seeds small.
- Build and test against published modules. A `go.work` is ignored by git and
  only for trying a sibling checkout; nothing committed may depend on it. A
  change that needs an unreleased sibling waits for its tag.
- newlock.city and nebula build against tagged nt3d releases. Do not edit them
  for API that is not yet released.

## Conventions

- Every Go file starts with `// Copyright (c) 2026 Neomantra Corp.`
- Packages are named for the module: `nt3dmath` at `nt3d/math`, `nt3dmesh` at
  `nt3d/mesh`, and so on.
- Name a file for what it holds, and put a declaration where its name says it
  is. Per-node scene methods are `SetNodeX` and `NodeX`.
- Scene methods address a node by `NodeID`. One that reaches a whole subtree
  is `ApplyX(root, …)`. Only `Find` and the create-or-replace shortcuts
  (`SetGeometry`, `SetShared`, `SetLabel`) take a name; a test holds this.
- Tests state the behaviour they hold, in their name and in a comment, and are
  written before the code they test. A fix comes with the test that failed
  without it.
- Something callers do gets a compiled `Example` in `example_test.go`.
- Document a behaviour or API change in `CHANGELOG.md` under Unreleased and in
  the guide. Breaking changes get a before-and-after table.
- Open work lives in `ROADMAP.md`; move an item to Done when it lands.
- Plans, specs, and working notes do not belong in this repository. Prompts a
  roadmap item points to live in `docs/prompts/`, and benchmark records in
  `docs/performance/`.
- Do not track built binaries, recordings, or site output.
- Do not commit, push, tag, or release unless asked.
