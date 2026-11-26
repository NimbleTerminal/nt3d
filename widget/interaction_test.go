// Copyright (c) 2026 Neomantra Corp.

package nt3dwidget

import (
	tea "charm.land/bubbletea/v2"
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
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

// The keys move the camera as the mouse does, through Orbit and Zoom, so they
// work on a camera placed by a Pose, which the orbit fields alone do not move.
func TestKeysOrbitAndZoomAPlacedCamera(t *testing.T) {
	placed := nt3d.DefaultCamera()
	placed.Pose = nt3dmath.Identity()
	placed.Pose[12], placed.Pose[13], placed.Pose[14] = 4, -3, 2
	for key, want := range map[rune]nt3d.Camera{
		'h': placed.Orbit(-5, 0),
		'l': placed.Orbit(5, 0),
		'k': placed.Orbit(0, 5),
		'j': placed.Orbit(0, -5),
		'+': placed.Zoom(.9),
		'-': placed.Zoom(1 / .9),
	} {
		m := New(nt3d.NewScene(), 30, 10, WithRenderMode(Software), WithCamera(placed))
		before := m.Camera()
		m.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
		got := m.Camera()
		m.Close()
		if got.Eye() == before.Eye() {
			t.Errorf("key %q did not move the camera", key)
		}
		if got != want {
			t.Errorf("key %q gave %+v, want %+v", key, got, want)
		}
	}
}
