// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3dwidget

import (
	"nimbleterminal.dev/nt3d"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	picture "nimbleterminal.dev/ntpicture"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.closed {
		return m, nil
	}
	if m.scene.Revision() != m.sceneRevision {
		m.sceneRevision = m.scene.Revision()
		m.seq++
		m.dirty = true
	}
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		return m, m.SetSize(v.Width, v.Height)
	case uv.CellSizeEvent:
		if v.Width > 0 && v.Height > 0 {
			_ = m.pic.SetCellPixelSize(v.Width, v.Height)
			return m, m.changed(false)
		}
	case rendered:
		if v.id != m.id {
			return m, nil
		}
		if v.seq != m.seq {
			m.busy = false
			return m, m.schedule()
		}
		if v.err != nil {
			m.err = v.err
			m.renderMode = v.renderMode
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
			_ = m.pic.SetSize(m.width, m.height)
			m.dirty = true
		}
		if v.renderMode == Wireframe {
			cleanup := m.pic.SetImage(nil)
			return m, tea.Sequence(cleanup, func() tea.Msg { return presented{m.id} })
		}
		if v.frame == nil {
			m.busy = false
			return m, nil
		}
		return m, m.present(v.frame, v.frame.Bounds())
	case encoded:
		if v.id != m.id {
			return m, nil
		}
		if frame, ok := v.msg.(picture.KittyFrameMsg); ok {
			m.transport = frame.Format.String()
			if frame.Medium == picture.KittyMediumSharedMemory {
				m.transport = "shm"
			}
		}
		id := m.id
		return m, tea.Sequence(m.pic.Update(v.msg), func() tea.Msg { return presented{id} })
	case presented:
		if v.id != m.id {
			return m, nil
		}
		m.busy = false
		if m.dirty {
			return m, m.schedule()
		}
		return m, m.timer()
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
	before := m.pic.Mode()
	cmd := m.pic.Update(msg)
	if !m.forceGlyph && m.pic.Mode() == picture.PictureGlyph && picture.KittySupported() == picture.KittyCapabilitySupported {
		toggle := m.togglePicture()
		return m, tea.Batch(cmd, toggle, m.changed(false))
	}
	if before != m.pic.Mode() {
		return m, tea.Batch(cmd, m.changed(false))
	}
	return m, tea.Batch(cmd, m.schedule())
}
