package nt3dwidget

import (
	"errors"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	picture "nimbleterminal.dev/ntpicture"
)

// Execute Bubble Tea batches/sequences and route their messages through Update.
// Reflection unwraps Tea's unexported sequence slice without depending on its name.
func drain(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	count := 0
	var run func(tea.Cmd)
	run = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		count++
		if count > 200 {
			t.Fatal("command loop did not settle")
		}
		msg := cmd()
		if msg == nil {
			return
		}
		v := reflect.ValueOf(msg)
		if v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeOf(tea.Cmd(nil)) {
			for i := 0; i < v.Len(); i++ {
				run(v.Index(i).Interface().(tea.Cmd))
			}
			return
		}
		if _, ok := msg.(tea.RawMsg); ok {
			return
		}
		_, next := m.Update(msg)
		run(next)
	}
	run(cmd)
}

func TestPicturePresentationLifecycle(t *testing.T) {
	session := picture.NewTerminalSession(picture.TerminalOptions{Kitty: picture.KittyCapabilitySupported, SharedMemory: picture.KittyCapabilityUnsupported})
	defer session.Close()
	allocated := 0
	m := New(scene(t), 20, 8, WithRenderMode(Software), WithTerminalSession(session), WithImageIDAllocator(func() (int, error) { allocated++; return 100 + allocated, nil }))
	defer m.Close()
	m.started = true
	// Establish terminal mode before the first render.
	_, cmd := m.Update(struct{}{})
	drain(t, m, cmd)
	status := m.PictureStatus()
	if status.Busy || status.Presented == 0 || status.Generation != status.Presented {
		t.Fatalf("not presented: %+v", status)
	}
	view := m.pic.View()
	drain(t, m, m.Refresh())
	if allocated != 1 || m.pic.View() != view {
		t.Fatal("streaming frame changed stable placement")
	}
	drain(t, m, m.SetSize(25, 9))
	if allocated <= 1 || m.PictureStatus().Busy {
		t.Fatal("resize did not replace placement")
	}
	drain(t, m, m.SetRenderMode(Wireframe))
	if m.pic.View() != "" || m.wire == "" {
		t.Fatal("wireframe retained picture placement")
	}
	drain(t, m, m.SetRenderMode(Software))
	if m.pic.View() == "" || m.PictureStatus().Busy {
		t.Fatal("picture did not resume")
	}
	drain(t, m, m.Dispose())
	if m.pic.View() != "" || m.PictureStatus().Busy {
		t.Fatal("dispose retained picture")
	}
	session.Update(uv.CellSizeEvent{Width: 9, Height: 18})
	if session.Snapshot().CellPixelWidth != 9 {
		t.Fatal("widget closed caller-owned session")
	}
}

func TestPicturePendingResizeAndDispose(t *testing.T) {
	m := New(scene(t), 20, 8, WithRenderMode(Software), WithTerminalSession(picture.NewTerminalSession(picture.TerminalOptions{Kitty: picture.KittyCapabilityUnsupported})))
	defer m.Close()
	defer m.session.Close()
	m.started = true
	renderedMsg := m.schedule()()
	_, prepare := m.Update(renderedMsg)
	if !m.PictureStatus().Busy {
		t.Fatal("picture preparation not pending")
	}
	if cmd := m.Refresh(); cmd != nil {
		t.Fatal("render scheduled while picture busy")
	}
	drain(t, m, m.SetSize(30, 10))
	drain(t, m, prepare)
	if m.busy || m.PictureStatus().Busy || m.PictureStatus().Presented != m.PictureStatus().Generation {
		t.Fatal("resize did not settle")
	}
	render := m.Refresh()
	_, prepare = m.Update(render())
	drain(t, m, m.Dispose())
	drain(t, m, prepare) // Late cancellation completion must still be forwarded.
	if m.pic.View() != "" || m.PictureStatus().Busy {
		t.Fatal("late preparation revived disposed picture")
	}
}

func TestPictureErrorAndSessionIsolation(t *testing.T) {
	a := picture.NewTerminalSession(picture.TerminalOptions{Kitty: picture.KittyCapabilityUnsupported})
	b := picture.NewTerminalSession(picture.TerminalOptions{Kitty: picture.KittyCapabilityUnsupported})
	defer a.Close()
	defer b.Close()
	m := New(scene(t), 20, 8, WithRenderMode(Software), WithTerminalSession(a))
	defer m.Close()
	m.Update(picture.TerminalSessionMsg{Session: b, State: picture.TerminalState{CellPixelWidth: 99, CellPixelHeight: 99}})
	if w, _ := m.pic.CellPixelSize(); w == 99 {
		t.Fatal("foreign session changed geometry")
	}
	m.Update(uv.CellSizeEvent{Width: 10, Height: 20})
	if w, h := m.pic.CellPixelSize(); w != 10 || h != 20 {
		t.Fatal("cell event not forwarded")
	}
	foreign := picture.New(picture.Options{})
	defer foreign.Close()
	m.Update(picture.ErrorMsg{Owner: foreign, Err: errors.New("foreign")})
	if m.Err() != nil {
		t.Fatal("foreign error accepted")
	}
	m.Update(picture.ErrorMsg{Owner: m.pic, Generation: m.pic.Status().Generation - 1, Err: errors.New("stale")})
	if m.Err() != nil {
		t.Fatal("stale error accepted")
	}
}

func TestPictureSubmissionFailureFallsBackToGlyph(t *testing.T) {
	session := picture.NewTerminalSession(picture.TerminalOptions{Kitty: picture.KittyCapabilitySupported, SharedMemory: picture.KittyCapabilityUnsupported})
	defer session.Close()
	exhausted := errors.New("image allocator exhausted")
	m := New(scene(t), 20, 8, WithRenderMode(Software), WithTerminalSession(session), WithImageIDAllocator(func() (int, error) { return 0, exhausted }))
	defer m.Close()
	m.started = true
	_, cmd := m.Update(struct{}{})
	drain(t, m, cmd)
	if !errors.Is(m.Err(), exhausted) || m.PictureMode() != picture.PictureGlyph || m.RenderMode() != Software || m.PictureStatus().Busy || m.PictureStatus().Presented == 0 {
		t.Fatalf("submission fallback failed: mode=%v picture=%v status=%+v error=%v", m.RenderMode(), m.PictureMode(), m.PictureStatus(), m.Err())
	}
}

func TestDisposeClosesOwnedSession(t *testing.T) {
	m := New(scene(t), 20, 8)
	defer m.Close()
	before := m.session.Snapshot()
	drain(t, m, m.Dispose())
	m.session.Update(uv.CellSizeEvent{Width: before.CellPixelWidth + 1, Height: before.CellPixelHeight + 1})
	if m.session.Snapshot() != before {
		t.Fatal("owned terminal session remained open")
	}
}
