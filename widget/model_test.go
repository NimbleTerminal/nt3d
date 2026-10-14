package nt3dwidget

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	"fmt"
	"image"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"strings"
	"testing"
	"time"
)

// failRenderer fails with err, a missing GPU when nil, until ok.
type failRenderer struct {
	closed, ok bool
	err        error
}

func (r *failRenderer) Render(f Frame) (image.Image, error) {
	switch {
	case r.ok:
		return image.NewRGBA(image.Rect(0, 0, f.Width, f.Height)), nil
	case r.err != nil:
		return nil, r.err
	}
	return nil, fmt.Errorf("no adapter: %w", nt3d.ErrGPUUnavailable)
}
func (r *failRenderer) Close() error { r.closed = true; return nil }
func scene(t *testing.T) *nt3d.Scene {
	t.Helper()
	s := nt3d.NewScene()
	_, err := s.SetGeometry("point", nt3d.Geometry{Points: []nt3d.Point{{Radius: 4}}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestFallbackAndStaleFrame(t *testing.T) {
	r := &failRenderer{}
	m := New(scene(t), 30, 10, WithRenderer(r))
	defer m.Close()
	m.started = true
	cmd := m.schedule()
	if cmd == nil {
		t.Fatal("no render")
	}
	msg := cmd().(rendered)
	if msg.renderMode != Software || msg.frame == nil {
		t.Fatal("no fallback")
	}
	m.Update(msg)
	if m.RenderMode() != Software || m.RequestedRenderMode() != WebGPU || !r.closed {
		t.Fatal("fallback state")
	}
	m.busy = false
	m.SetRenderMode(Wireframe)
	seq := m.seq
	m.Update(rendered{id: m.id, seq: seq - 1, renderMode: Software, err: errors.New("old")})
	if m.RenderMode() != Wireframe {
		t.Fatal("stale render changed mode")
	}
}
func TestSceneEditsAndResize(t *testing.T) {
	s := scene(t)
	m := New(s, 30, 10, WithRenderMode(Software))
	defer m.Close()
	m.started = true
	old := m.frame()
	id, _ := s.Find("point")
	if err := s.SetTransform(id, nt3dmath.Translation(nt3dmath.Vec3{X: .2})); err != nil {
		t.Fatal(err)
	}
	_, cmd := m.Update(struct{}{})
	if cmd == nil {
		t.Fatal("scene change not scheduled")
	}
	if next := m.frame(); old.Revision != next.Revision || next.Instances[0].World[12] != .2 {
		t.Fatal("transform update")
	}
	m.Update(tea.WindowSizeMsg{Width: 50, Height: 20})
	if m.width != 50 || m.height != 20 {
		t.Fatal("resize")
	}
	m.Close()
	if _, _, err := m.Snapshot(10, 10); err == nil {
		t.Fatal("closed snapshot")
	}
}
func TestPacingAndModelIsolation(t *testing.T) {
	m := New(scene(t), 80, 24, WithRenderMode(Software))
	defer m.Close()
	seq := m.seq
	_, cmd := m.Update(rendered{id: m.id + 1, seq: seq, renderMode: Wireframe})
	if cmd != nil || m.RenderMode() != Software {
		t.Fatal("foreign message")
	}
	if !m.paced(200 * time.Millisecond) {
		t.Fatal("slow glyph frame not detected")
	}
	m.started = true
	m.dirty = false
	m.camera.AutoRotate = true
	m.lastInput = time.Now()
	generation := m.wake
	m.Update(tick{id: m.id, generation: generation - 1})
	if m.dirty {
		t.Fatal("stale tick")
	}
}

// messages runs a command and the commands it batches, collecting messages.
func messages(cmd tea.Cmd) (out []tea.Msg) {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			out = append(out, messages(c)...)
		}
	default:
		out = append(out, msg)
	}
	return out
}

func TestGPUFailureIsReported(t *testing.T) {
	r := &failRenderer{err: errors.New("out of memory")}
	s := scene(t)
	m := New(s, 30, 10, WithRenderer(r))
	defer m.Close()
	m.started = true
	msg := m.schedule()().(rendered)
	if msg.renderMode != WebGPU || msg.err == nil || msg.frame != nil {
		t.Fatal("fell back instead of reporting")
	}
	_, cmd := m.Update(msg)
	reported := false
	for _, msg := range messages(cmd) {
		if v, ok := msg.(RenderFailedMsg); ok && v.ID == m.ID() && v.Err != nil {
			reported = true
		}
	}
	if !reported || m.Failed() == nil || m.RenderMode() != WebGPU || r.closed {
		t.Fatalf("reported=%v failed=%v mode=%v closed=%v", reported, m.Failed(), m.RenderMode(), r.closed)
	}
	if view := m.View().Content; !strings.Contains(view, "Not rendered: out of memory") {
		t.Fatalf("view %q", view)
	}
	// Moving the camera cannot help, so nothing renders.
	if m.SetCamera(m.Camera().Orbit(10, 0)) != nil || m.Busy() {
		t.Fatal("rendered again for a camera change")
	}
	// A smaller scene is tried on the GPU again.
	r.ok = true
	_, err := s.SetGeometry("point", nt3d.Geometry{Points: []nt3d.Point{{Radius: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	m.Refresh()
	if m.Failed() != nil || m.Err() != nil || !m.Busy() {
		t.Fatal("scene change did not retry")
	}
	// Choosing a mode also clears it.
	m.failed = errors.New("again")
	m.busy = false
	m.SetRenderMode(Software)
	if m.Failed() != nil {
		t.Fatal("SetRenderMode kept the failure")
	}
}

func TestGPUFailureCanFallBack(t *testing.T) {
	r := &failRenderer{err: errors.New("out of memory")}
	m := New(scene(t), 30, 10, WithRenderer(r), WithGPUFailure(FallBackOnGPUFailure))
	defer m.Close()
	m.started = true
	msg := m.schedule()().(rendered)
	if msg.renderMode != Software || msg.frame == nil {
		t.Fatal("no fallback")
	}
	m.Update(msg)
	if m.Failed() != nil || m.RenderMode() != Software || !r.closed {
		t.Fatal("fallback state")
	}
}
