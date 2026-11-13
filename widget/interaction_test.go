// Copyright (c) 2026 Neomantra Corp.

package nt3dwidget

import (
	tea "charm.land/bubbletea/v2"
	"testing"

	"nimbleterminal.dev/nt3d"
)

// A click's ray leaves the camera through the clicked cell's center, as
// Pick's ray does, so a scene can run its own picking on it.
func TestClickRay(t *testing.T) {
	m := New(nt3d.NewScene(), 30, 10, WithRenderMode(Software))
	defer m.Close()
	cw, ch := m.pic.CellPixelSize()
	want := m.camera.RayFromPixel((7+.5)*float32(cw), (3+.5)*float32(ch), 30*cw, 10*ch)
	if got := m.clickRay(7, 3); got != want {
		t.Fatalf("ray %v, want %v", got, want)
	}
}

func TestDefaultKeyBindingsOptOut(t *testing.T) {
	m := New(nt3d.NewScene(), 30, 10, WithRenderMode(Software), WithDefaultKeyBindings(false))
	defer m.Close()
	before := m.Camera()
	for _, key := range "hjl k+-_=crog" {
		m.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
		if m.Camera() != before || m.forceGlyph {
			t.Fatalf("disabled key %q changed widget", key)
		}
	}
	WithDefaultKeyBindings(true)(m)
	m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	if m.Camera().Projection == before.Projection {
		t.Fatal("enabled projection shortcut did nothing")
	}
}
