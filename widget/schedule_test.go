package nt3dwidget

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/nt3d"
	picture "nimbleterminal.dev/ntpicture"
)

// A setter's command, dropped, leaves its change pending, not the view
// stuck waiting on a render that never runs: the next message the view
// handles draws it.
func TestDroppedSetterCommandDoesNotStall(t *testing.T) {
	m := New(scene(t), 20, 8, WithRenderMode(Software), WithTerminalSession(picture.NewTerminalSession(picture.TerminalOptions{Kitty: picture.KittyCapabilityUnsupported})))
	defer m.Close()
	drain(t, m, m.Init())
	_ = m.SetLighting(nt3d.Lighting{}) // Dropped, as a caller might.
	if m.Busy() {
		t.Fatal("the view is busy with a render whose command was dropped")
	}
	type unrelated struct{}
	drew := false
	for _, msg := range drain(t, m, func() tea.Msg { return unrelated{} }) {
		if _, ok := msg.(rendered); ok {
			drew = true
		}
	}
	if !drew || m.Busy() {
		t.Fatalf("the next message drew %v, busy after %v", drew, m.Busy())
	}
}
