// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3dwidget

import (
	"fmt"
	"image"
	"math"
	"nimbleterminal.dev/nt3d"

	"time"

	tea "charm.land/bubbletea/v2"
	picture "nimbleterminal.dev/ntpicture"
)

func (m *Model) changed(data bool) tea.Cmd {
	m.seq++
	m.dirty = true
	if data {
		m.sceneRevision = m.scene.Revision()
	}
	return m.schedule()
}

type rendered struct {
	id, seq    uint64
	frame      image.Image
	wire       string
	renderMode RenderMode
	err        error
	elapsed    time.Duration
}

type tick struct {
	id, generation uint64
}

// Software frames are drawn at full size while they are prompt, and smaller
// while they are not: a frame over softwareSlow is followed by one reduced to
// take softwareAim, and one under softwareFast by one a half larger.
const (
	softwareSlow = 60 * time.Millisecond
	softwareAim  = 40 * time.Millisecond
	softwareFast = 20 * time.Millisecond
)

// smallest caps the source image at 320 by 200 pixels. Glyph mode uses this
// scale; adaptive software rendering never reduces the source further.
func (m *Model) smallest() float64 {
	cw, ch := m.pic.CellPixelSize()
	return math.Min(1, math.Min(320/float64(m.width*cw), 200/float64(m.height*ch)))
}

// softwareScale is the scale of the next software frame.
func (m *Model) softwareScale() float64 {
	if m.pic.Mode() == picture.PictureGlyph {
		return m.smallest()
	}
	return math.Max(m.smallest(), 1-m.shrink)
}

// paced sets the size of software frames to come by the time the last took.
// It reports whether that frame was slow at the smallest size, where there
// is nothing left to reduce.
func (m *Model) paced(elapsed time.Duration) bool {
	scale, smallest := m.softwareScale(), m.smallest()
	switch {
	case scale <= smallest:
		if elapsed < softwareFast && m.pic.Mode() != picture.PictureGlyph && scale < 1 {
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

// frame is what the terminal shows: sized by its cells, and reduced where
// the picture is shown as glyphs or drawn slowly in software.
func (m *Model) frame() Frame {
	cw, ch := m.pic.CellPixelSize()
	w, h := m.width*cw, m.height*ch
	factor := 1.0
	switch {
	case m.pic.Mode() == picture.PictureGlyph:
		factor = m.smallest()
	case m.renderMode == Software:
		factor = m.softwareScale()
	}
	w, h = max(1, int(float64(w)*factor)), max(1, int(float64(h)*factor))
	if w*h > nt3d.MaxFramePixels {
		factor := math.Sqrt(float64(nt3d.MaxFramePixels) / float64(w*h))
		w, h = max(1, int(float64(w)*factor)), max(1, int(float64(h)*factor))
	}

	f, _ := m.scene.Frame(m.camera, w, h)
	f.Matrix = m.camera.Matrix(float32(m.width*cw) / float32(m.height*ch))
	f.Background = m.background
	f.Light = m.light
	if m.decorate != nil {
		m.decorate(&f)
	}
	return f
}

func (m *Model) schedule() tea.Cmd {
	if !m.started || m.closed || m.busy || !m.dirty || (m.renderMode != Wireframe && m.pic.Status().Busy) {
		return nil
	}
	m.busy = true
	m.dirty = false
	f := m.frame()
	id, seq, renderMode, r, cols, rows := m.id, m.seq, m.renderMode, m.renderer, m.width, m.height
	return func() tea.Msg {
		start := time.Now()
		var img image.Image
		var err error
		wire := ""
		if renderMode == WebGPU {
			if r == nil {
				err = fmt.Errorf("nt3dwidget: GPU unavailable")
			} else {
				img, err = r.Render(f)
			}
			if err != nil {
				renderMode = Software
			}
		}
		if renderMode == Software {
			var renderErr error
			img, renderErr = nt3d.NewSoftwareRenderer().Render(f)
			if renderErr != nil {
				err = renderErr
			}
		}
		if renderMode == Wireframe {
			var renderErr error
			wire, renderErr = nt3d.RenderWireframe(f, cols, rows)
			if renderErr != nil {
				err = renderErr
			}
		}
		return rendered{id, seq, img, wire, renderMode, err, time.Since(start)}
	}
}

// finish schedules the newest scene or resumes idle camera animation.
func (m *Model) finish() tea.Cmd {
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
	return tea.Tick(time.Second/30, func(time.Time) tea.Msg { return tick{id, generation} })
}
