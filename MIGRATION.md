# Extraction boundary and future ntcharts3d migration

This change creates nt3d only. It does not edit, move files out of, or change the
module dependencies of github.com/NimbleMarkets/ntcharts3d. Existing chart users
have no migration to perform yet.

## Extracted engine

The new implementation derives from the existing math, camera, geometry,
clipping, stroke, GPU/software/wireframe renderers, WGSL, texture handling, and
Bubble Tea scheduling/interaction. Chart-specific state was removed rather than
retained as dormant options. New scene nodes provide hierarchical transforms,
visibility, stable identity, and geometry ownership. Meshes stay in local space;
a separate GPU storage binding holds world and normal transforms.

Heightfield construction supplies geometry/normals/UVs without mapping heights
to data colors. Texture-only edits preserve meshes and in-flight snapshots.
Generic point, box, triangle, line, and arrow geometry belongs to the engine.

The widget uses nimbleterminal.dev/ntpicture for presentation and the wireframe
renderer uses nimbleterminal.dev/ntcanvas surfaces. Neither imports ntcharts3d
or ntcharts/v2. The terrain example uses go-booba without adding it to the library
module. The old chart gallery and transport diagnostic remain where they are.

## Proposed chart changes (not implemented here)

| Existing concern | Future owner / integration |
| --- | --- |
| Axis layout, ticks, labels and categories | ntcharts3d; emit generic overlays and wireframe cell labels |
| Back-plane grid selection | ntcharts3d; emit dynamic world-space line pairs |
| Legends, palettes, shared/fixed color domains | ntcharts3d; compile explicit primitive colors and overlays |
| Hover emphasis and formatted datum labels | ntcharts3d; interpret engine Hit identities and emit decorations |
| Scatter, surface, bar, line and vector-field data models | ntcharts3d; compile engine geometry |
| Auto-ranging, fixed ranges, plot proportions | ntcharts3d; choose clipping bounds and scene transforms |
| Camera, textures, lighting, render backends | nt3d |
| Rendering scheduling and terminal presentation | nt3dwidget, composed by the chart Model |

Keep high-level chart constructors, setters, options, custom Series compilation,
and Snapshot behavior where practical. Chart Geometry can initially retain its
flat fields and chart metadata, with a small conversion to engine Geometry; no
rendering implementation needs to remain duplicated. Chart code maps engine node
IDs and primitive indices back to its data models and labels. Picking a box now
reports an actual surface hit rather than the old chart-specific bar value.

A chart-specific frame builder computes axes, ranges, clipped geometry, and
palette colors. Camera-dependent decorations must be refreshed for every frame
without changing mesh revisions. Pixel decoration layout and cell label layout
remain distinct so wireframe text stays legible. The widget's frame decorator is
an integration point, not an engine-owned axis/legend system.

The likely explicit breaking changes for low-level consumers are:

- Import `nimbleterminal.dev/nt3d/math`; its package identifier is `nt3dmath`.
- Import `nimbleterminal.dev/nt3d/widget`; its identifier is `nt3dwidget`.
- Custom renderers consume chart-free nt3d Frames with local geometry/instances,
  generic overlays, and independent texture revisions.
- Custom non-chart geometry no longer carries Labels or color-domain fields;
  keep that metadata in the higher layer and key it by node/primitive identity.
- Direct engine geometry uses world units; apply normalization explicitly when
  migrating code that relied on automatic chart fitting.
- Transport types come from nimbleterminal.dev/ntpicture.

No compatibility math package or chart API forwarding shim is introduced here.
The eventual chart release should document its precise source-level changes and
verify the existing gallery on native Kitty, browser WebGPU, software, and wireframe.

## Release coordination

Keep Go 1.26.8 and the MIT copyright notices. ntcanvas v0.1.0 and ntgpuimage
v0.2.0 resolve upstream. ntpicture is pinned to commit 204e10268ffe and CI uses
a clean sibling checkout until its vanity module resolves. Publish that commit
to the ntpicture remote before running CI (the remote advertised no refs during
this migration). No uncommitted
ntpicture changes are required.

## Widget presentation migration

The widget now uses ntpicture `Picture` and explicit `TerminalSession` instead
of the legacy `Model`. Existing render modes, camera controls, medium/format
options, and scene APIs remain available.

- `WithKittyID` is removed. Use `WithImageIDAllocator(func() (int, error))` when
  coordinating image IDs with other components. Every allocation must return a
  fresh positive 24-bit ID; streaming reuses the displayed ID internally, while
  resize/mode changes require fresh placements. The default is ntpicture's shared
  allocator.
- Use `WithTerminalSession(session)` for widgets sharing one terminal connection.
  Hosts own supplied sessions. Default sessions are owned by the widget.
- Execute commands from `SetSize`, `SetRenderMode`, `Update`, and `Dispose`.
  `Dispose` cancels preparation and returns placement deletion commands; run it
  before quitting. `Close` releases GPU resources after the event loop exits.
- `PictureStatus()` reports the presentation generation, busy/error state, and
  encoding/transport choice. ntpicture presentation events identify ordered
  output completion, not a terminal display acknowledgement.

Register nt3d in NimbleTerminal-infra's module mapping:
`nt3d = "https://github.com/NimbleTerminal/nt3d"`.
Publish the engine before adding a released dependency to ntcharts3d. The
infrastructure repository and GitHub remotes are not changed by this extraction.
