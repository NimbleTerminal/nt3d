// Copyright (c) 2026 Neomantra Corp.
// Package nt3dwidget presents an nt3d scene through Bubble Tea and Kitty graphics.
package nt3dwidget

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	zone "github.com/lrstanley/bubblezone/v2"
	"image"
	"image/color"
	"nimbleterminal.dev/nt3d"
	picture "nimbleterminal.dev/ntpicture"
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
	light                           nt3d.Light
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
	err                             error
	slow                            int
	shrink                          float64
}
type Option func(*Model)

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
	m := &Model{scene: scene, sceneRevision: scene.Revision(), camera: nt3d.DefaultCamera(), light: nt3d.DefaultLight(), background: color.RGBA{235, 238, 242, 255}, renderer: nt3d.NewGPURenderer(), id: nextID.Add(1), dirty: true, kittyFormat: picture.KittyFormatZlib}
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
func (m *Model) Scene() *nt3d.Scene               { return m.scene }
func (m *Model) Camera() nt3d.Camera              { return m.camera }
func (m *Model) ID() uint64                       { return m.id }
func (m *Model) Err() error                       { return m.err }
func (m *Model) RenderMode() RenderMode           { return m.renderMode }
func (m *Model) RequestedRenderMode() RenderMode  { return m.requestedRenderMode }
func (m *Model) PictureStatus() picture.Status    { return m.pic.Status() }
func (m *Model) PictureMode() picture.PictureMode { return m.pic.Mode() }
func (m *Model) Init() tea.Cmd                    { m.started = true; return tea.Batch(m.pic.Init(), m.schedule()) }

// Busy reports whether rendering or picture presentation is in flight.
// Animation drivers can skip intermediate updates until it becomes false.
func (m *Model) Busy() bool { return m.busy || m.pic.Status().Busy }

// Refresh schedules scene edits made by a parent. Execute the returned command.
func (m *Model) Refresh() tea.Cmd { return m.changed(true) }
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
	f.Background, f.Light = m.background, m.light
	if m.decorate != nil {
		m.decorate(&f)
	}
	return nt3d.Snapshot(f, m.requestedRenderMode, m.renderer)
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
func (m *Model) SetLight(l nt3d.Light) tea.Cmd { m.light = l; return m.changed(false) }
