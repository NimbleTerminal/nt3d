// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3dwidget

import (
	"nimbleterminal.dev/nt3d"
	"time"

	tea "charm.land/bubbletea/v2"
	picture "nimbleterminal.dev/ntpicture"
)

func (m *Model) Update(msg tea.Msg) (model tea.Model, cmd tea.Cmd) {
	// Always route late preparation/retirement messages, including after Dispose.
	before := m.pic.RasterGeometry()
	pictureCmd := m.pic.Update(msg)
	defer func() { cmd = tea.Batch(pictureCmd, cmd) }()
	if m.closed {
		return m, nil
	}
	if !m.forceGlyph && m.pic.Mode() == picture.PictureGlyph && m.session.Snapshot().Kitty == picture.KittyCapabilitySupported {
		pictureCmd = tea.Batch(pictureCmd, m.pic.SetMode(picture.PictureKitty))
	}
	if before != m.pic.RasterGeometry() {
		m.seq++
		m.dirty = true
		m.wake++
	}
	if m.scene.Revision() != m.sceneRevision {
		m.sceneRevision = m.scene.Revision()
		m.seq++
		m.dirty = true
		pictureCmd = tea.Batch(pictureCmd, m.retry())
	}
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		return m, m.SetSize(v.Width, v.Height)
	case rendered:
		if v.id != m.id {
			return m, nil
		}
		m.busy = false
		if v.seq != m.seq {
			return m, m.schedule()
		}
		if v.err != nil && v.renderMode == WebGPU {
			// Reported, not fallen back: keep the renderer for a smaller scene.
			m.err, m.failed = v.err, v.err
			id, err := m.id, v.err
			failed := func() tea.Msg { return RenderFailedMsg{ID: id, Err: err} }
			return m, tea.Batch(m.pic.SetVisible(false), failed)
		}
		if v.err != nil {
			m.err = v.err
			if m.renderer != nil {
				_ = m.renderer.Close()
				m.renderer = nil
			}
		}

		m.renderMode = v.renderMode
		m.wire = v.wire
		if v.renderMode == Software && m.paced(v.elapsed) {
			m.slow++
		} else {
			m.slow = 0
		}
		if m.slow >= 3 {
			m.renderMode = Wireframe
			pictureCmd = tea.Batch(pictureCmd, m.pic.SetVisible(false))
			m.dirty = true
		}
		if v.renderMode == Wireframe {
			return m, m.finish()
		}
		if v.frame == nil {
			return m, m.finish()
		}
		if m.renderMode == Wireframe {
			return m, m.schedule()
		}
		// Renderers return independently owned pixels, safe for immutable submission.
		return m, m.pic.SetImageImmutable(v.frame)
	case picture.PresentedMsg:
		if v.Owner != m.pic || v.Generation != m.pic.Status().Generation {
			return m, nil
		}
		return m, m.finish()
	case picture.ErrorMsg:
		if v.Owner != m.pic || v.Generation != m.pic.Status().Generation {
			return m, nil
		}
		m.err = v.Err
		if m.pic.Mode() == picture.PictureKitty {
			m.forceGlyph = true
			return m, tea.Batch(m.pic.SetMode(picture.PictureGlyph), m.changed(false))
		}
		return m, m.SetRenderMode(Wireframe)
	case tick:
		if v.id != m.id || v.generation != m.wake || !m.camera.AutoRotate {
			return m, nil
		}
		if time.Since(m.lastInput).Seconds() >= m.camera.ResumeAfter {
			m.camera.Beta += 1
			return m, m.changed(false)
		}
		return m, m.timer()
	case tea.KeyPressMsg:
		switch v.String() {
		case "left", "h":
			m.camera.Beta -= 5
		case "right", "l":
			m.camera.Beta += 5
		case "up", "k":
			m.camera.Alpha += 5
		case "down", "j":
			m.camera.Alpha -= 5
		case "+", "=":
			m.camera.Distance *= .9
		case "-", "_":
			m.camera.Distance /= .9
		case "c":
			return m, m.Fit()
		case "r":
			m.camera.AutoRotate = !m.camera.AutoRotate
			m.wake++
		case "o":
			if m.camera.Projection == nt3d.Orthographic {
				m.camera.Projection = nt3d.Perspective
			} else {
				m.camera.Projection = nt3d.Orthographic
			}
		case "g":
			m.forceGlyph = !m.forceGlyph
			cleanup := m.togglePicture()
			render := m.changed(false)
			return m, tea.Batch(cleanup, render)
		default:
			return m, m.schedule()
		}
		m.camera = m.camera.Valid()
		m.lastInput = time.Now()
		return m, m.changed(false)
	case tea.MouseMsg:
		return m, m.mouse(v)
	}
	return m, m.schedule()
}
