package nt3dwidget

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	picture "nimbleterminal.dev/ntpicture"
)

// rawOutput joins the terminal output among messages.
func rawOutput(messages []tea.Msg) string {
	var b strings.Builder
	for _, msg := range messages {
		if raw, ok := msg.(tea.RawMsg); ok {
			b.WriteString(raw.Msg.(string))
		}
	}
	return b.String()
}

// presented is a started widget that has presented a Kitty frame through
// the medium given, with the session it uses.
func presented(t *testing.T, medium picture.KittyMedium) (*Model, *picture.TerminalSession, string) {
	t.Helper()
	shared := picture.KittyCapabilityUnsupported
	if medium == picture.KittyMediumSharedMemory {
		shared = picture.KittyCapabilitySupported
	}
	session := picture.NewTerminalSession(picture.TerminalOptions{Kitty: picture.KittyCapabilitySupported, SharedMemory: shared})
	t.Cleanup(session.Close)
	m := New(scene(t), 20, 8, WithRenderMode(Software), WithTerminalSession(session), WithKittyMedium(medium))
	m.started = true
	_, cmd := m.Update(struct{}{})
	out := rawOutput(drain(t, m, cmd))
	if status := m.PictureStatus(); status.Busy || status.Presented == 0 || m.pic.View() == "" {
		t.Fatalf("not presented: %+v", status)
	}
	return m, session, out
}

// A frame sent through shared memory is kept by sending it again directly,
// in place, without deleting it; Close afterward leaves it, and the view.
func TestKeepRetainsSharedMemoryFrame(t *testing.T) {
	m, _, sent := presented(t, picture.KittyMediumSharedMemory)
	if !strings.Contains(sent, "t=s") {
		t.Skip("shared memory unavailable here: the frame was sent directly")
	}
	view := m.View().Content
	kept := rawOutput(drain(t, m, m.Keep()))
	if !strings.Contains(kept, "a=T") || strings.Contains(kept, "t=s") || strings.Contains(kept, "a=d") {
		t.Fatalf("keep must resend the shared frame directly without deleting it: %q", kept)
	}
	if m.View().Content != view {
		t.Fatal("keep changed the view")
	}
	if err := m.Close(); err != nil || m.View().Content != view {
		t.Fatalf("close after keep: %v", err)
	}
}

// Close may run before the kept command: the command still restores the
// frame, and Close deletes nothing.
func TestKeepThenClose(t *testing.T) {
	m, _, sent := presented(t, picture.KittyMediumSharedMemory)
	if !strings.Contains(sent, "t=s") {
		t.Skip("shared memory unavailable here: the frame was sent directly")
	}
	keep := m.Keep()
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if m.Dispose() != nil || m.Keep() != nil {
		t.Fatal("dispose or keep after keep did something")
	}
	if kept := rawOutput(drain(t, m, keep)); !strings.Contains(kept, "a=T") || strings.Contains(kept, "a=d") {
		t.Fatalf("kept command after close: %q", kept)
	}
}

// A frame sent directly needs nothing sent to stay.
func TestKeepDirectFrame(t *testing.T) {
	m, _, _ := presented(t, picture.KittyMediumDirect)
	defer m.Close()
	view := m.View().Content
	if kept := rawOutput(drain(t, m, m.Keep())); kept != "" {
		t.Fatalf("direct keep sent %q", kept)
	}
	if m.View().Content != view {
		t.Fatal("keep changed the view")
	}
}

// Renders in flight and auto-rotation ticks pending when Keep is called
// present nothing.
func TestKeepDiscardsPendingRenders(t *testing.T) {
	m, _, _ := presented(t, picture.KittyMediumDirect)
	defer m.Close()
	view, generation := m.View().Content, m.PictureStatus().Generation
	pending := m.Refresh() // A render, not yet run.
	m.camera.AutoRotate = true
	tick := m.timer()
	if pending == nil || tick == nil {
		t.Fatal("nothing pending")
	}
	if out := rawOutput(drain(t, m, m.Keep())); out != "" {
		t.Fatalf("keep sent %q", out)
	}
	if out := rawOutput(drain(t, m, tea.Batch(pending, tick))); out != "" {
		t.Fatalf("a late render or tick sent %q", out)
	}
	if m.View().Content != view || m.PictureStatus().Generation != generation || m.PictureStatus().Busy {
		t.Fatalf("a late render was presented: %+v", m.PictureStatus())
	}
	if m.timer() != nil || m.schedule() != nil {
		t.Fatal("kept widget still schedules work")
	}
}

// Keep is safe before anything is presented, and again.
func TestKeepRepeatedAndUnpresented(t *testing.T) {
	m := New(scene(t), 20, 8, WithRenderMode(Software), WithTerminalSession(picture.NewTerminalSession(picture.TerminalOptions{Kitty: picture.KittyCapabilitySupported, SharedMemory: picture.KittyCapabilityUnsupported})))
	view := m.View().Content
	drain(t, m, m.Keep())
	if m.Keep() != nil || m.Dispose() != nil {
		t.Fatal("repeated keep did something")
	}
	if m.View().Content != view {
		t.Fatal("keep without a frame changed the view")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}

// Keep leaves an owned session open, for the kept command; Close releases it.
func TestKeepLeavesOwnedSessionToClose(t *testing.T) {
	m := New(scene(t), 20, 8)
	drain(t, m, m.Keep())
	before := m.session.Snapshot()
	m.session.Update(uv.CellSizeEvent{Width: before.CellPixelWidth + 1, Height: before.CellPixelHeight + 1})
	open := m.session.Snapshot()
	if open == before {
		t.Fatal("keep closed the owned session")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	m.session.Update(uv.CellSizeEvent{Width: open.CellPixelWidth + 1, Height: open.CellPixelHeight + 1})
	if m.session.Snapshot() != open {
		t.Fatal("close left the owned session open")
	}
}

// KeepOnMainScreen sends even a direct frame again, so it survives leaving
// the alternate screen, and stops the widget as Keep does.
func TestKeepOnMainScreenResendsDirectFrame(t *testing.T) {
	m, _, _ := presented(t, picture.KittyMediumDirect)
	defer m.Close()
	view := m.View().Content
	pending := m.Refresh()
	if kept := rawOutput(drain(t, m, m.KeepOnMainScreen())); !strings.Contains(kept, "a=T") || strings.Contains(kept, "a=d") {
		t.Fatalf("keep on main screen must resend the frame without deleting it: %q", kept)
	}
	if out := rawOutput(drain(t, m, pending)); out != "" {
		t.Fatalf("a late render sent %q", out)
	}
	if m.View().Content != view {
		t.Fatal("keep on main screen changed the view")
	}
	if m.KeepOnMainScreen() != nil || m.Keep() != nil || m.Dispose() != nil {
		t.Fatal("a second keep or dispose did something")
	}
}
