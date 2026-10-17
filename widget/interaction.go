// Copyright (c) 2026 Neomantra Corp.
package nt3dwidget

import (
	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/nt3d"
	picture "nimbleterminal.dev/ntpicture"
	"time"
)

// PickMsg is emitted on a click. Hit is nil when no primitive is hit.
type PickMsg struct {
	ModelID uint64
	Hit     *nt3d.Hit
}

func (m *Model) Pick(x, y int) *nt3d.Hit {
	if x < 0 || y < 0 || x >= m.width || y >= m.height {
		return nil
	}
	cw, ch := m.pic.CellPixelSize()
	f := m.frame()
	f.Width, f.Height = m.width*cw, m.height*ch
	f.Matrix = m.camera.Matrix(float32(f.Width) / float32(f.Height))
	px, py := (float32(x)+.5)*float32(cw), (float32(y)+.5)*float32(ch)
	return m.scene.Pick(m.camera, f, px, py, float32(cw)*.6)
}
func (m *Model) mouse(msg tea.MouseMsg) tea.Cmd {
	z := m.zones.Get(m.zoneID)
	event := msg.Mouse()
	inside := z.InBounds(msg)
	x, y := z.Pos(msg)
	switch msg.(type) {
	case tea.MouseReleaseMsg:
		click := m.drag && !m.moved
		m.drag = false
		if click && inside {
			hit := m.Pick(x, y)
			id := m.id
			return func() tea.Msg { return PickMsg{id, hit} }
		}
		return nil
	case tea.MouseClickMsg:
		if inside && event.Button == tea.MouseLeft {
			m.drag = true
			m.moved = false
			m.lastX, m.lastY = event.X, event.Y
		}
		return nil
	case tea.MouseWheelMsg:
		if !inside {
			return nil
		}
		factor := .9
		if event.Button != tea.MouseWheelUp {
			factor = 1 / .9
		}
		m.camera = m.camera.Zoom(factor)
	case tea.MouseMotionMsg:
		if !m.drag {
			return nil
		}
		dx, dy := event.X-m.lastX, event.Y-m.lastY
		m.lastX, m.lastY = event.X, event.Y
		if dx == 0 && dy == 0 {
			return nil
		}
		m.moved = true
		if event.Mod&tea.ModShift != 0 {
			cw, ch := m.pic.CellPixelSize()
			m.camera = m.camera.Pan(float32(dx*cw), float32(dy*ch), m.width*cw, m.height*ch)
		} else {
			m.camera = m.camera.Orbit(float64(dx)*2, float64(dy)*2)
		}
	default:
		return nil
	}
	m.lastInput = time.Now()
	return m.changed(false)
}

// togglePicture forces glyphs, or lets the picture follow the session's
// Kitty capability, as it does from the start.
func (m *Model) togglePicture() tea.Cmd {
	if m.forceGlyph {
		return m.pic.SetMode(picture.PictureGlyph)
	}
	return m.pic.SetMode(picture.PictureAuto)
}
