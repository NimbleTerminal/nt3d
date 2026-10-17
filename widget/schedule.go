// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3dwidget

import (
	"errors"
	"fmt"
	"image"
	"math"
	"nimbleterminal.dev/nt3d"

	"time"

	tea "charm.land/bubbletea/v2"
	picture "nimbleterminal.dev/ntpicture"
)

// changed marks the view for a new frame. It starts no render: its command,
// when a render could start, wakes the view, whose Update starts one. A
// caller that drops the command only delays the frame to the view's next
// message, where a render started here, busy until its command ran, would
// stall the view for good.
func (m *Model) changed(data bool) tea.Cmd {
	m.seq++
	m.dirty = true
	var retry, w tea.Cmd
	if data {
		m.sceneRevision = m.scene.Revision()
		retry = m.retry()
	}
	if m.ready() {
		w = wake
	}
	return tea.Batch(retry, w)
}

// wakeMsg brings a changed view to Update, which draws it.
type wakeMsg struct{}

func wake() tea.Msg { return wakeMsg{} }

// retry clears a reported GPU failure when the scene has changed.
func (m *Model) retry() tea.Cmd {
	if m.failed == nil {
		return nil
	}
	m.failed, m.renderErr, m.err = nil, nil, nil
	return m.pic.SetVisible(m.renderMode != Wireframe)
}

type rendered struct {
	id, seq    uint64
	frame      image.Image
	geometry   picture.RasterGeometry // The raster frame was drawn for.
	wire       string
	renderMode RenderMode
	err        error
	elapsed    time.Duration
	fellBack   bool // The GPU failed and software drew the frame instead.
}

type tick struct {
	id, generation uint64
	at             time.Time
}

// Software frames are drawn at full size while they are prompt, and smaller
// while they are not: a frame over softwareSlow is followed by one reduced to
// take softwareAim, and one under softwareFast by one a half larger.
const (
	softwareSlow = 60 * time.Millisecond
	softwareAim  = 40 * time.Millisecond
	softwareFast = 20 * time.Millisecond
)

// smallest is the Kitty resolution floor for adaptive software rendering.
// Glyph resolution is fixed by ntpicture and does not use this scale.
func (m *Model) smallest() float64 {
	cw, ch := m.pic.CellPixelSize()
	return math.Min(1, math.Min(320/float64(m.width*cw), 200/float64(m.height*ch)))
}

// softwareScale is the scale of the next software frame.
func (m *Model) softwareScale() float64 {
	return math.Max(m.smallest(), 1-m.shrink)
}

// paced sets the size of software frames to come by the time the last took.
// It reports whether that frame was slow at the smallest size, where there
// is nothing left to reduce.
func (m *Model) paced(elapsed time.Duration) bool {
	if m.pic.Mode() == picture.PictureGlyph {
		return elapsed > 150*time.Millisecond
	}
	scale, smallest := m.softwareScale(), m.smallest()
	switch {
	case scale <= smallest:
		if elapsed < softwareFast && scale < 1 {
			break
		}
		return elapsed > 150*time.Millisecond
	case elapsed > softwareSlow:
		// Time goes with area, and area with the square of the scale.
		m.shrink = 1 - math.Max(smallest, scale*math.Sqrt(float64(softwareAim)/float64(elapsed)))
		return false
	case elapsed >= softwareFast || scale >= 1:
		return false
	}
	// Quick enough to be drawn again, larger, even if nothing else changes.
	m.shrink = 1 - math.Min(1, scale*1.5)
	m.dirty = true
	return false
}

// resolution applies nt3d's quality policy; ntpicture owns target rounding,
// encoding dimensions, and the framebuffer budget (Config.MaxRasterPixels).
// Run its command even when a new frame is also scheduled.
func (m *Model) resolution() tea.Cmd {
	factor := 1.0
	if m.renderMode == Software {
		factor = m.softwareScale()
	}
	return m.pic.SetResolution(factor)
}

// frame uses the exact encoding target but projects with physical display
// aspect, which differs from pixel aspect in glyph mode and after rounding.
func (m *Model) frame() Frame {
	g := m.pic.RasterGeometry()
	f, _ := m.scene.Frame(m.camera, max(1, g.Pixels.X), max(1, g.Pixels.Y))
	f.Matrix = m.camera.Matrix(float32(g.DisplayAspect))
	f.Background = m.background
	f.Lighting, f.Fog = m.lighting, m.fog
	if m.decorate != nil {
		m.decorate(&f)
	}
	return f
}

// ready reports whether a render can start: the view is started and open,
// has changed, and is neither drawing nor presenting a frame nor failed.
func (m *Model) ready() bool {
	return m.started && !m.closed && !m.busy && m.dirty && m.failed == nil && (m.renderMode == Wireframe || !m.pic.Status().Busy)
}

func (m *Model) schedule() tea.Cmd {
	if !m.ready() {
		return nil
	}
	m.busy = true
	m.dirty = false
	resolutionCmd := m.resolution()
	f := m.frame()
	geometry := m.pic.RasterGeometry()
	id, seq, renderMode, r, cols, rows, fallBack, post := m.id, m.seq, m.renderMode, m.renderer, m.width, m.height, m.gpuFailure == FallBackOnGPUFailure, m.postProcess
	f.Supersample = m.supersample
	return tea.Batch(resolutionCmd, func() tea.Msg {
		start := time.Now()
		var img image.Image
		var err error
		wire, fellBack := "", false
		if renderMode == WebGPU {
			if r == nil {
				err = fmt.Errorf("nt3dwidget: %w", nt3d.ErrGPUUnavailable)
			} else {
				img, err = r.Render(f)
			}
			if err != nil && !fallBack && !errors.Is(err, nt3d.ErrGPUUnavailable) {
				return rendered{id: id, seq: seq, renderMode: WebGPU, err: err, elapsed: time.Since(start)}
			}
			if err != nil {
				// Software draws the frame: its error, if any, is the frame's.
				renderMode, fellBack, err = Software, true, nil
			}
		}
		if renderMode == Software {
			f.Supersample = 0 // Software pacing trades resolution for speed instead.
			img, err = nt3d.NewSoftwareRenderer().Render(f)
		}
		if renderMode == Wireframe {
			var renderErr error
			wire, renderErr = nt3d.RenderWireframe(f, cols, rows)
			if renderErr != nil {
				err = renderErr
			}
		}
		return rendered{id: id, seq: seq, frame: postProcessed(img, post), geometry: geometry, wire: wire, renderMode: renderMode, err: err, elapsed: time.Since(start), fellBack: fellBack}
	})
}

// finish schedules the newest scene or resumes idle camera animation.
func (m *Model) finish() tea.Cmd {
	// A resolution change can finish re-encoding the old front while its
	// replacement is still rendering. Do not start another camera tick yet.
	if m.busy {
		return nil
	}
	if m.dirty {
		return m.schedule()
	}
	return m.timer()
}

func (m *Model) timer() tea.Cmd {
	if !m.camera.AutoRotate || m.closed {
		return nil
	}
	m.wake++
	id, generation := m.id, m.wake
	return tea.Tick(time.Second/30, func(at time.Time) tea.Msg { return tick{id, generation, at} })
}
