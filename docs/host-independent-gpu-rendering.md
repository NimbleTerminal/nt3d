# NT3D Host-Independent GPU Rendering Specification

## 1. Overview

NT3D provides three rendering execution paths over the same immutable `Frame` snapshot produced by `Scene.Frame(camera, width, height)`:

1. **Host-Supplied GPU Session (`GPUSession` / `GPUTarget`)**: Renders directly into a caller-owned `*wgpu.TextureView` (such as a swapchain surface view, UI canvas layer, or offscreen render target) using a caller-owned `*wgpu.Device` and `*wgpu.CommandEncoder`, with zero CPU readback.
2. **Standalone Offscreen GPU Renderer (`GPURenderer`)**: Owns an offscreen WebGPU device via `ntgpuimage.Renderer`, encodes into an offscreen target via the same GPU core, and reads pixels back to an `*image.RGBA`.
3. **Pure-Go Software & Wireframe Renderers (`SoftwareRenderer`, `RenderWireframe`)**: CPU rasterization and wireframe projection into `*image.RGBA` for terminal widgets, headless servers, and deterministic Golden tests.

All scene construction (`Scene`), hierarchy, materials, lighting, shadow mapping, spherical harmonics (`nt3d/sh`), asset loading (`nt3d/asset`, `nt3d/gltf`, `nt3d/stl`, `nt3d/ply`, `nt3d/vrm`), and picking remain completely host-agnostic.

---

## 2. Public API (`nimbleterminal.dev/nt3d`)

The host-supplied GPU rendering API lives directly in the root `nt3d` package and consists of two types (`GPUTarget`, `GPUSession`), two constructors, and four methods:

```go
// GPUTarget describes a caller-owned color attachment view to render a Frame into.
type GPUTarget struct {
	// Color is the destination texture view (required).
	// Its underlying texture must be created with TextureUsageRenderAttachment.
	Color *wgpu.TextureView

	// Format is the texture format of Color (required).
	// Supported formats:
	//   - gputypes.TextureFormatRGBA8Unorm
	//   - gputypes.TextureFormatRGBA8UnormSrgb
	//   - gputypes.TextureFormatBGRA8Unorm
	//   - gputypes.TextureFormatBGRA8UnormSrgb
	//   - gputypes.TextureFormatRGBA16Float
	Format gputypes.TextureFormat

	// Width and Height are the pixel dimensions of Color (required).
	// They must match Frame.Width and Frame.Height.
	Width  int
	Height int

	// Load preserves the existing contents of Color and alpha-blends the
	// resolved 3D frame over it when true. When false, Color is cleared
	// or overwritten by the resolved frame.
	Load bool
}

// GPUSession renders NT3D frames into caller-owned GPU targets using a
// caller-supplied WebGPU device.
type GPUSession struct { /* unexported */ }

// NewGPUSession creates a GPU rendering session bound to a caller-owned device.
// The caller retains ownership of dev; Close releases only NT3D-owned pipelines,
// buffers, samplers, and cached textures.
func NewGPUSession(dev *wgpu.Device) (*GPUSession, error)

// NewGPUSessionFromProvider creates a GPU rendering session from a
// gpucontext.DeviceProvider (such as gogpu's app.GPUContextProvider()).
func NewGPUSessionFromProvider(provider gpucontext.DeviceProvider) (*GPUSession, error)

// BindingLimit returns the maximum bindings per bind group supported by the
// session's device, clamped to maxBindingLimit.
func (s *GPUSession) BindingLimit() (int, error)

// Encode records all GPU passes required to render f into target on enc.
// Encode does not call enc.Finish() or submit commands to the device queue;
// any host-queued staging writes ( via dev.Queue().WriteBuffer / WriteTexture)
// are flushed when the host submits enc.
func (s *GPUSession) Encode(f Frame, enc *wgpu.CommandEncoder, target GPUTarget) error

// Render creates a CommandEncoder on the session's device, encodes f into
// target via Encode, finishes the encoder, and submits the command buffer to
// dev.Queue().
func (s *GPUSession) Render(f Frame, target GPUTarget) error

// Close releases all GPU resources created by the session without closing or
// destroying the caller-owned *wgpu.Device.
func (s *GPUSession) Close() error
```

---

## 3. Architecture & Key Design Decisions

### 3.1 Single-Pass Format Adaptation via `sceneTarget` (`RGBA16Float`)

NT3D's forward renderer compiles over 30 WebGPU render pipelines across opaque/double-sided meshes, weighted blended order-independent transparency (`blendTarget`), depth pre-pass, directional and point shadow maps (`shadowTarget`), outline/selection pass (`outlineTarget`), point clouds, and billboarding sprites.

Rather than duplicating all scene pipelines for every possible host swapchain format (`BGRA8Unorm`, `BGRA8UnormSrgb`, `RGBA8Unorm`, `RGBA8UnormSrgb`, `RGBA16Float`), NT3D renders all 3D geometry into its internal high-precision `sceneTarget` (`gputypes.TextureFormatRGBA16Float` with `Depth32Float`) at `(Frame.Width * Frame.Supersample, Frame.Height * Frame.Supersample)`.

Only the final fullscreen resolve pass (`supersample.go`) attaches `target.Color` and `target.Format`:

```
Procedural Textures / Mipmaps  ──┐
Directional & Point Shadows    ──┼──► Internal sceneTarget (RGBA16Float + Depth32Float)
Opaque + Sky + Sprites + WBOIT ──┘             │
                                               ▼
                                  Fullscreen Resolve Pass (fs_resolve)
                                  - Box-filter downsample (Supersample²)
                                  - Transfer curve adaptation (sRGB ◄──► Linear)
                                  - Optional coverage-preserving alpha blend (Load: true)
                                               │
                                               ▼
                                     Caller's GPUTarget.Color
```

`sceneTarget` caches resolve pipelines keyed by `resolveKey{format: target.Format, load: target.Load}` (`t.pipelines`), compiling at most one lightweight fullscreen triangle pipeline per `(Format, Load)` pair encountered during the session.

### 3.2 Color-Space & Transfer Function Matrix

NT3D supports two lighting/output modes controlled by `Frame.Linear`:

- **`Frame.Linear == false` (Default Legacy Mode)**: Scene shading produces display-ready values directly in `sceneTarget`.
- **`Frame.Linear == true` (Linear Lighting Mode)**: Scene shading operates in linear light in `sceneTarget`, expecting an sRGB transfer curve on display output.

Because a host swapchain or render target may be either non-sRGB (`RGBA8Unorm`, `BGRA8Unorm`), hardware-sRGB (`RGBA8UnormSrgb`, `BGRA8UnormSrgb`), or linear floating-point (`RGBA16Float`), `resolveTransfer(f.Linear, target.Format)` sets the resolve shader's `factor.y` uniform as follows:

| `Frame.Linear` | `target.Format` | `factor.y` | Resolve Shader Behavior |
| :--- | :--- | :---: | :--- |
| `false` | `RGBA8Unorm`, `BGRA8Unorm`, `RGBA16Float` | `0` | Pass-through (no software transfer curve; hardware stores raw values). |
| `false` | `RGBA8UnormSrgb`, `BGRA8UnormSrgb` | `-1` | Decodes sRGB $\to$ linear (`decode()`) before output so hardware sRGB encoding produces the exact legacy display colors. |
| `true` | `RGBA8Unorm`, `BGRA8Unorm` | `+1` | Encodes linear $\to$ sRGB (`encode()`) in software before writing to the non-sRGB 8-bit target. |
| `true` | `RGBA8UnormSrgb`, `BGRA8UnormSrgb` | `0` | Pass-through linear values; hardware sRGB attachment performs the linear $\to$ sRGB encoding. |
| `true` | `RGBA16Float` | `0` | Pass-through linear HDR/float values. |

### 3.3 Coverage-Preserving Alpha Composition (`GPUTarget.Load == true`)

When `target.Load == true` (for example, overlaying a 3D viewport or gizmo on top of a host 2D UI canvas or cleared swapchain):

1. `sceneTarget` is cleared with premultiplied background color `(rgb * a, a)` where `a = float64(f.Background.A) / 255.0`. When `f.Background.A == 0` and `f.Environment == nil`, uncovered background pixels in `sceneTarget` remain `(0, 0, 0, 0)`.
2. In the fullscreen resolve shader (`fs_resolve`), `factor.z = 1.0` enables coverage un-premultiplication across the `Supersample × Supersample` box filter:
   - Total coverage is $\alpha = \frac{\sum a_i}{S^2}$.
   - For $\alpha > 0$, the un-premultiplied color is $\text{rgb} = \frac{\sum \text{rgb}_i}{\sum a_i}$, which is then passed through the sRGB transfer function (`encode` or `decode`) without darkening semi-transparent or supersampled silhouette edges.
3. The resolve pipeline uses `gputypes.LoadOpLoad` on `target.Color` and `gputypes.BlendStateAlpha()` (`SrcAlpha, OneMinusSrcAlpha`), compositing the resolved 3D frame cleanly over prior host content.

### 3.4 Device-Native Textures & Procedural Mipmap Generation

To operate without `ntgpuimage.Renderer`, `gpuRenderer` manages textures directly on `*wgpu.Device` via `gpuTexture`:

- **Image & Environment Textures (`render_texture.go`, `render_environment.go`)**: Uploaded directly via `dev.Queue().WriteTexture` with cached bind groups matching `g.sampledLayout`.
- **Procedural GPU Textures (`render_procedural.go`)**: Rendered into level 0 of a device-native texture (`TextureUsageTextureBinding | TextureUsageCopyDst | TextureUsageCopySrc | TextureUsageRenderAttachment`), followed by box-filter mipmap generation across levels `1..mipLevelCount-1`.
- **Per-Level Scratch Textures for Mip Generation**: Because `gogpu/wgpu` tracks texture usage per `*wgpu.Texture` across a `CommandEncoder` scope (`CoreCommandEncoder.populateTextureScope`), each mip level `1..mipLevelCount-1` renders into its own 1-level scratch texture `t.mipScratch[level-1]` (`ColorTarget` $\to$ `CopySrc`) and copies into `t.tex` at `MipLevel: level` (`CopyDst`), followed by a 1-texel copy to `g.mipSink` so the encoder's read/write tracker retains the copy dependency before the main scene pass samples `t.tex`.

---

## 4. Ownership, Threading & Lifecycle Rules

1. **Device Ownership**: `NewGPUSession` and `NewGPUSessionFromProvider` borrow the caller's `*wgpu.Device`. `(*GPUSession).Close()` releases all pipelines, uniform/vertex/index/instance buffers, shadow/scene/outline/blend targets, samplers, and cached textures owned by the session, and never closes or destroys the caller's `*wgpu.Device`.
2. **CommandEncoder Ownership**: `(*GPUSession).Encode` records render and copy passes onto the caller's `*wgpu.CommandEncoder` and returns without calling `Finish()` or `Queue().Submit()`. Buffer and texture uploads queued via `dev.Queue().WriteBuffer` and `dev.Queue().WriteTexture` during `Encode` are automatically ordered before the submitted command buffer per WebGPU queue semantics.
3. **Target Lifetime**: `GPUTarget.Color` is only accessed during the `Encode` or `Render` call and is never retained by `GPUSession` after the call returns, making it safe to pass per-frame swapchain `SurfaceView()` attachments that change every frame or on window resize.
4. **Concurrency**: Like `GPURenderer`, a single `GPUSession` instance is serialized internally with an internal mutex (`g.mu`) and may be called from a render loop thread; multiple independent `GPUSession` instances may share the same `*wgpu.Device` across multiple windows or viewports.

---

## 5. Host Integration Patterns

### 5.1 GoGPU Window (`gogpu.App` / `gogpu.Context`)

```go
var session *nt3d.GPUSession

app.OnDraw(func(dc *gogpu.Context) {
	if session == nil {
		var err error
		session, err = nt3d.NewGPUSessionFromProvider(app.GPUContextProvider())
		if err != nil {
			log.Fatal(err)
		}
	}
	w, h := dc.SurfaceSize()
	if w == 0 || h == 0 {
		return
	}
	frame, err := scene.Frame(camera, int(w), int(h))
	if err != nil {
		return
	}
	if err := session.Encode(frame, dc.CommandEncoder(), nt3d.GPUTarget{
		Color:  dc.SurfaceView(),
		Format: dc.Format(),
		Width:  int(w),
		Height: int(h),
	}); err != nil {
		log.Printf("nt3d encode: %v", err)
		return
	}
	dc.MarkPreserveContent()
})
```

### 5.2 Overlaying NT3D on a Host Canvas (`Load: true`)

```go
dc.Clear(0.08, 0.14, 0.24, 1.0) // Host draws background or 2D content first

frame, _ := scene.Frame(camera, int(w), int(h))
frame.Environment = nil
frame.Background = color.RGBA{0, 0, 0, 0}

_ = session.Encode(frame, dc.CommandEncoder(), nt3d.GPUTarget{
	Color:  dc.SurfaceView(),
	Format: dc.Format(),
	Width:  int(w),
	Height: int(h),
	Load:   true,
})
dc.MarkPreserveContent()
```

### 5.3 Standalone Offscreen `wgpu.TextureView` (`session.Render`)

```go
err := session.Render(frame, nt3d.GPUTarget{
	Color:  offscreenView,
	Format: gputypes.TextureFormatRGBA8UnormSrgb,
	Width:  width,
	Height: height,
})
```

See [`examples/gogpu/main.go`](../examples/gogpu/main.go) for a complete interactive example with orbit camera controls, supersampling toggle, `Load: true` composition mode, and a second window sharing the same GPU device.
