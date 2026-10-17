// Copyright (c) 2026 Neomantra Corp.
// Package nt3dwidget presents an nt3d scene through Bubble Tea and Kitty graphics.
package nt3dwidget

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone/v2"
	"image"
	"image/color"
	"image/draw"
	"nimbleterminal.dev/nt3d"
	picture "nimbleterminal.dev/ntpicture"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

// Rendering types are shared with the engine.
type Frame = nt3d.Frame
type RenderMode = nt3d.RenderMode

const (
	WebGPU    = nt3d.WebGPU
	Software  = nt3d.Software
	Wireframe = nt3d.Wireframe
)

var nextID atomic.Uint64

type Model struct {
	scene                           *nt3d.Scene
	sceneRevision                   uint64
	camera                          nt3d.Camera
	lighting                        nt3d.Lighting
	fog                             nt3d.Fog
	background                      color.RGBA
	renderer                        nt3d.Renderer
	decorate                        func(*nt3d.Frame)
	pic                             *picture.Picture
	session                         *picture.TerminalSession
	ownSession                      bool
	nextImageID                     func() (int, error)
	width, height                   int
	medium                          picture.KittyMedium
	kittyFormat                     picture.KittyFormat
	renderMode, requestedRenderMode RenderMode
	id, seq, wake                   uint64
	started, busy, dirty, closed    bool
	forceGlyph                      bool
	wire                            string
	zones                           *zone.Manager
	ownZones                        bool
	zoneID                          string
	drag, moved                     bool
	lastX, lastY                    int
	lastInput                       time.Time
	lastTick                        time.Time // The last auto-rotate tick, or zero.
	err                             error
	slow                            int
	shrink                          float64
	gpuFailure                      GPUFailure
	failed                          error // A reported GPU failure; rendering waits for a change.
	renderErr                       error // The last frame's, until a frame draws without one.
	supersample                     int
	postProcess                     func(*image.RGBA)
	shown                           []time.Time   // When frames were shown, over the last second.
	renderTime                      time.Duration // The last frame's render.
}
type Option func(*Model)

// GPUFailure says what the widget does when the GPU fails to render a frame.
// A GPU that cannot be used at all (nt3d.ErrGPUUnavailable: no adapter, or a
// failed setup) always falls back to software, as terminals without one must.
type GPUFailure int

const (
	// ReportGPUFailure, the default, stops drawing and reports the error: the
	// view says so, Failed returns it, and the parent receives RenderFailedMsg.
	// A GPU that ran out of memory for a scene would otherwise fall back to a
	// software rendering far slower than the user expects. Rendering resumes
	// when the scene changes or SetRenderMode is called, as after choosing
	// fewer parts or Software.
	ReportGPUFailure GPUFailure = iota
	// FallBackOnGPUFailure switches to software rendering for good.
	FallBackOnGPUFailure
)

// RenderFailedMsg reports a GPU failure under ReportGPUFailure.
type RenderFailedMsg struct {
	ID  uint64 // The widget's ID.
	Err error
}

// WithSupersample renders GPU frames at n times the size, 1..4, averaged
// down: smoother edges, thin strands, and cutouts, at a cost growing with
// n². Software frames are not supersampled, since their pacing already trades
// resolution for speed; Snapshot supersamples on every backend.
func WithSupersample(n int) Option {
	return func(m *Model) { m.supersample = max(0, min(nt3d.MaxSupersample, n)) }
}

// WithPostProcess runs fn on each rendered image, GPU or software, before it
// is shown, and on snapshots. fn may change the pixels in place; it runs on
// the render goroutine, one frame at a time, so it must not touch state the
// update goroutine changes without synchronization. Wireframe text is not
// passed to it.
func WithPostProcess(fn func(*image.RGBA)) Option { return func(m *Model) { m.postProcess = fn } }

// WithGPUFailure chooses what a GPU render failure does.
func WithGPUFailure(p GPUFailure) Option { return func(m *Model) { m.gpuFailure = p } }

func WithRenderMode(mode nt3d.RenderMode) Option {
	return func(m *Model) {
		if mode <= nt3d.Wireframe {
			m.renderMode = mode
			m.requestedRenderMode = mode
		}
	}
}

// WithRenderer transfers lifecycle ownership to the widget.
func WithRenderer(r nt3d.Renderer) Option { return func(m *Model) { m.renderer = r } }
func WithCamera(c nt3d.Camera) Option     { return func(m *Model) { m.camera = c.Valid() } }
func WithBackground(c color.Color) Option {
	return func(m *Model) { m.background = color.RGBAModel.Convert(c).(color.RGBA); m.background.A = 255 }
}

// WithTerminalSession shares discovery with other pictures on the same terminal.
// The caller owns the session and must close it after its widgets are disposed.
func WithTerminalSession(s *picture.TerminalSession) Option { return func(m *Model) { m.session = s } }

// WithImageIDAllocator shares a fresh-ID allocator with other image components.
func WithImageIDAllocator(next func() (int, error)) Option {
	return func(m *Model) { m.nextImageID = next }
}
func WithKittyMedium(v picture.KittyMedium) Option { return func(m *Model) { m.medium = v } }
func WithKittyFormat(v picture.KittyFormat) Option { return func(m *Model) { m.kittyFormat = v } }

// WithZoneManager requires the parent to Scan its complete View.
func WithZoneManager(z *zone.Manager) Option { return func(m *Model) { m.zones = z } }

// WithFrameDecorator supplies generic lines/overlays before each render, on the
// update goroutine. Do not mutate shared geometry or instances inside it.
func WithFrameDecorator(fn func(*nt3d.Frame)) Option { return func(m *Model) { m.decorate = fn } }
func New(scene *nt3d.Scene, width, height int, opts ...Option) *Model {
	if scene == nil {
		scene = nt3d.NewScene()
	}
	m := &Model{scene: scene, sceneRevision: scene.Revision(), camera: nt3d.DefaultCamera(), lighting: nt3d.DefaultLighting(), background: color.RGBA{235, 238, 242, 255}, renderer: nt3d.NewGPURenderer(), id: nextID.Add(1), dirty: true, kittyFormat: picture.KittyFormatZlib}
	for _, o := range opts {
		o(m)
	}
	if m.session == nil {
		m.session = picture.NewTerminalSession(picture.TerminalOptions{})
		m.ownSession = true
	}
	if m.zones == nil {
		m.zones = zone.New()
		m.ownZones = true
	}
	m.zoneID = m.zones.NewPrefix() + "nt3d"
	m.pic = picture.New(picture.Options{Session: m.session, NextID: m.nextImageID, StableID: true, Render: picture.Config{KittyZ: -1, Fit: picture.FitFill, Background: m.background, KittyMedium: m.medium, KittyFormat: m.kittyFormat}})
	_ = m.pic.SetVisible(m.renderMode != Wireframe)
	m.SetSize(width, height)
	return m
}
func (m *Model) Scene() *nt3d.Scene  { return m.scene }
func (m *Model) Camera() nt3d.Camera { return m.camera }
func (m *Model) ID() uint64          { return m.id }

// Err is the last frame's render error, until a frame draws without one, or
// else the picture error that changed how frames are shown. A GPU failure
// that software drew the frame for is not an error: RenderMode shows it.
func (m *Model) Err() error {
	if m.renderErr != nil {
		return m.renderErr
	}
	return m.err
}

// Supersample is the GPU supersampling factor; see WithSupersample.
func (m *Model) Supersample() int { return max(1, m.supersample) }

// SetSupersample changes the GPU supersampling factor, 1..4.
func (m *Model) SetSupersample(n int) tea.Cmd {
	m.supersample = max(0, min(nt3d.MaxSupersample, n))
	return m.changed(false)
}

// SetPostProcess replaces the post-processing function; nil removes it.
func (m *Model) SetPostProcess(fn func(*image.RGBA)) tea.Cmd {
	m.postProcess = fn
	return m.changed(false)
}

// postProcessed hands a rendered image to fn as RGBA. Renderers return
// opaque images they no longer use, so GPU readbacks, which are NRGBA, are
// viewed as RGBA without a copy.
func postProcessed(img image.Image, fn func(*image.RGBA)) image.Image {
	if fn == nil || img == nil {
		return img
	}
	var rgba *image.RGBA
	switch v := img.(type) {
	case *image.RGBA:
		rgba = v
	case *image.NRGBA:
		rgba = &image.RGBA{Pix: v.Pix, Stride: v.Stride, Rect: v.Rect}
	default:
		rgba = image.NewRGBA(img.Bounds())
		draw.Draw(rgba, rgba.Rect, img, img.Bounds().Min, draw.Src)
	}
	fn(rgba)
	return rgba
}

// Failed returns the GPU failure that has stopped rendering, or nil.
func (m *Model) Failed() error          { return m.failed }
func (m *Model) RenderMode() RenderMode { return m.renderMode }

// FrameRate is how many frames a second the widget has shown, over the last
// second; zero once a second has passed without one. Frames are drawn as the
// scene or camera changes, and at most 30 a second while it turns itself.
func (m *Model) FrameRate() float64 {
	m.prune(time.Now())
	if len(m.shown) < 2 {
		return float64(len(m.shown))
	}
	return float64(len(m.shown)-1) / m.shown[len(m.shown)-1].Sub(m.shown[0]).Seconds()
}

// RenderTime is how long the last frame took to render, from the scene to
// its pixels or wireframe, before the terminal shows it.
func (m *Model) RenderTime() time.Duration { return m.renderTime }

// prune drops shown times more than a second before now.
func (m *Model) prune(now time.Time) {
	i := 0
	for i < len(m.shown) && now.Sub(m.shown[i]) > time.Second {
		i++
	}
	m.shown = m.shown[i:]
}

// frameShown records a frame reaching the terminal.
func (m *Model) frameShown() {
	now := time.Now()
	m.prune(now)
	m.shown = append(m.shown, now)
}
func (m *Model) RequestedRenderMode() RenderMode  { return m.requestedRenderMode }
func (m *Model) PictureStatus() picture.Status    { return m.pic.Status() }
func (m *Model) PictureMode() picture.PictureMode { return m.pic.Mode() }
func (m *Model) Init() tea.Cmd                    { m.started = true; return tea.Batch(m.pic.Init(), m.schedule()) }

// Busy reports whether rendering or picture presentation is in flight.
// Animation drivers can skip intermediate updates until it becomes false.
func (m *Model) Busy() bool { return m.busy || m.pic.Status().Busy }

// Refresh schedules scene edits made by a parent. Execute the returned command.
func (m *Model) Refresh() tea.Cmd { return m.changed(true) }

// Fit aims the camera at the visible scene and backs it off until all of it
// shows, keeping the viewing direction and projection.
func (m *Model) Fit() tea.Cmd {
	aspect := m.pic.RasterGeometry().DisplayAspect
	if !(aspect > 0) {
		aspect = float64(m.width) / float64(2*m.height)
	}
	return m.SetCamera(m.camera.Fit(m.scene.Bounds(), aspect))
}
func (m *Model) SetCamera(c nt3d.Camera) tea.Cmd {
	m.camera = c.Valid()
	m.wake++
	return m.changed(false)
}
func (m *Model) SetSize(w, h int) tea.Cmd {
	w = max(1, min(4096, w))
	// Glyphs require two pixel rows per cell; keep their fixed raster in budget.
	h = max(1, min(2160, nt3d.MaxFramePixels/(2*w), h))
	if m.width == w && m.height == h {
		return nil
	}
	m.width, m.height = w, h
	return tea.Batch(m.pic.SetSize(w, h), m.changed(false))
}
func (m *Model) SetRenderMode(mode RenderMode) tea.Cmd {
	if mode > Wireframe {
		return nil
	}
	m.renderMode, m.requestedRenderMode = mode, mode
	m.slow, m.shrink = 0, 0
	m.failed = nil
	m.wire = ""
	m.wake++
	if mode == WebGPU && m.renderer == nil {
		m.renderer = nt3d.NewGPURenderer()
	}
	return tea.Batch(m.pic.SetVisible(mode != Wireframe), m.changed(false))
}
func (m *Model) Snapshot(w, h int) (image.Image, RenderMode, error) {
	if m.closed {
		return nil, m.requestedRenderMode, fmt.Errorf("nt3dwidget: closed")
	}
	f, err := m.scene.Frame(m.camera, w, h)
	if err != nil {
		return nil, m.requestedRenderMode, err
	}
	f.Background, f.Lighting, f.Fog = m.background, m.lighting, m.fog
	if m.decorate != nil {
		m.decorate(&f)
	}
	f.Supersample = m.supersample
	img, mode, err := nt3d.Snapshot(f, m.requestedRenderMode, m.renderer)
	return postProcessed(img, m.postProcess), mode, err
}

// Keep stops the widget for good while leaving its last presented frame on
// screen, as ntpicture.Picture.Keep does, for a viewer that quits and leaves
// its picture behind. Auto-rotation stops, and renders still running or
// pending are discarded, so nothing newer is presented; the last frame
// keeps its placement, and View keeps returning it. A Kitty image sent
// through shared memory is sent again directly by the returned command, as
// its buffer is released; one sent directly stays in place.
//
// Call Keep from Update as the program quits, and run its command before
// quitting, after any final layout:
//
//	return m, tea.Sequence(view.Keep(), tea.Quit)
//
// Then call Close after the program returns, to release the GPU renderer
// and an owned terminal session; it leaves the kept image alone, and the
// returned command stays valid. Keep after Keep or Dispose, and Dispose
// after Keep, do nothing, and Keep with nothing yet presented keeps an empty
// view. To remove the widget's images instead, use Dispose. An image outlasts
// the program only on the main screen: the alternate screen is cleared on
// exit.
func (m *Model) Keep() tea.Cmd {
	if m.closed {
		return nil
	}
	m.closed = true
	m.wake++ // Pending auto-rotation ticks are stale.
	m.seq++  // So are renders in flight.
	m.dirty, m.busy = false, false
	return m.pic.Keep()
}

// Dispose clears terminal resources while the event loop is still running.
// Call Close after the program exits to release GPU resources as well.
func (m *Model) Dispose() tea.Cmd {
	if m.closed {
		return nil
	}
	m.closed = true
	m.wake++
	cmd := m.pic.Close()
	if m.ownSession {
		m.session.Close()
	}
	return cmd
}
func (m *Model) Close() error {
	m.Dispose()
	if m.ownSession {
		m.session.Close() // Again after Dispose does nothing; after Keep it is due.
	}
	if m.ownZones {
		m.zones.Close()
	}
	if m.renderer != nil {
		return m.renderer.Close()
	}
	return nil
}
func (m *Model) View() tea.View {
	body := m.pic.View()
	if m.renderMode == Wireframe {
		body = m.wire
	}
	if m.failed != nil {
		body = failedView(m.failed, m.width, m.height)
	}
	if body == "" {
		body = strings.Repeat(strings.Repeat(" ", m.width)+"\n", m.height-1) + strings.Repeat(" ", m.width)
	}
	body = m.zones.Mark(m.zoneID, body)
	if m.ownZones {
		body = m.zones.Scan(body)
	}
	v := tea.NewView(body)
	v.MouseMode = tea.MouseModeAllMotion
	return v
}

func (m *Model) SetBackground(c color.Color) tea.Cmd {
	if c == nil {
		return nil
	}
	m.background = color.RGBAModel.Convert(c).(color.RGBA)
	m.background.A = 255
	return m.changed(false)
}

// SetLighting sets how the scene is lit: ambient light and up to
// nt3d.MaxLights directional or point lights. The lights are copied, so the
// caller may change its own while frames render.
func (m *Model) SetLighting(l nt3d.Lighting) tea.Cmd {
	m.lighting = l
	m.lighting.Lights = slices.Clone(l.Lights)
	return m.changed(false)
}

// SetFog fades the scene toward a color with distance from the camera; the
// zero Fog is none. Distances are in world units, so a fog that should keep
// its look as the camera zooms is better set by WithFrameDecorator, from the
// camera's distance.
func (m *Model) SetFog(f nt3d.Fog) tea.Cmd { m.fog = f; return m.changed(false) }

// failedView fills the viewport with the reason nothing is drawn.
func failedView(err error, width, height int) string {
	lines := make([]string, height)
	words := strings.Fields("Not rendered: " + err.Error())
	for i, row := 0, height/2; i < len(words) && row < height; row++ {
		line := words[i]
		for i++; i < len(words) && len([]rune(line))+1+len([]rune(words[i])) <= width; i++ {
			line += " " + words[i]
		}
		lines[row] = ansi.Truncate(line, width, "…")
	}
	for i, line := range lines {
		lines[i] = line + strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
	}
	return strings.Join(lines, "\n")
}
