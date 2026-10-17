package nt3dwidget

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
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
	retry := m.Refresh()
	if m.Failed() != nil || m.Err() != nil || retry == nil {
		t.Fatal("scene change did not retry")
	}
	for _, msg := range messages(retry) {
		m.Update(msg)
	}
	if !m.Busy() {
		t.Fatal("the retry did not render")
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
	// Software drew the frame: the GPU's failure is not the frame's error.
	if m.Err() != nil {
		t.Fatalf("error after a good fallback frame: %v", m.Err())
	}
}

// A frame's render error stands until a frame draws without one.
func TestRenderErrorClearsOnAGoodFrame(t *testing.T) {
	m := New(scene(t), 30, 10, WithRenderMode(Software))
	defer m.Close()
	m.started = true
	broken := errors.New("broken frame")
	m.Update(rendered{id: m.id, seq: m.seq, renderMode: Software, err: broken})
	if !errors.Is(m.Err(), broken) {
		t.Fatalf("error %v, want the frame's", m.Err())
	}
	m.busy, m.dirty = false, true
	m.Update(m.schedule()().(rendered))
	if m.Err() != nil {
		t.Fatalf("error %v after a good frame", m.Err())
	}
}

func TestFitShowsTheScene(t *testing.T) {
	s := nt3d.NewScene()
	g, err := nt3d.BuildCuboid(nt3d.Cuboid{Size: nt3dmath.Vec3{X: 300, Y: 120, Z: 80}})
	if err != nil {
		t.Fatal(err)
	}
	g, err = nt3d.TransformGeometry(g, nt3dmath.Translation(nt3dmath.Vec3{X: 1000, Y: -500, Z: 40}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetGeometry("part", g); err != nil {
		t.Fatal(err)
	}
	m := New(s, 40, 12, WithRenderer(&failRenderer{ok: true}))
	defer m.Close()
	m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if m.Camera().Target != (nt3dmath.Vec3{X: 1000, Y: -500, Z: 40}) {
		t.Fatalf("target %v", m.Camera().Target)
	}
	f := m.frame()
	b := s.Bounds()
	for _, p := range []nt3dmath.Vec3{b.Min, b.Max} {
		if _, _, _, ok := f.Matrix.Project(p, f.Width, f.Height); !ok {
			t.Fatalf("corner %v not in view", p)
		}
	}
}

// recordRenderer returns an opaque NRGBA image, as GPU readbacks are, and
// remembers the frame it drew.
type recordRenderer struct{ frame Frame }

func (r *recordRenderer) Render(f Frame) (image.Image, error) {
	r.frame = f
	img := image.NewNRGBA(image.Rect(0, 0, f.Width, f.Height))
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	return img, nil
}
func (r *recordRenderer) Close() error { return nil }

func TestSupersampleAndPostProcess(t *testing.T) {
	r := &recordRenderer{}
	marked := func(img *image.RGBA) { img.Pix[0] = 200 }
	m := New(scene(t), 30, 10, WithRenderer(r), WithSupersample(3), WithPostProcess(marked))
	defer m.Close()
	m.started = true
	msg := m.schedule()().(rendered)
	if r.frame.Supersample != 3 || m.Supersample() != 3 {
		t.Fatalf("GPU frame supersample %d", r.frame.Supersample)
	}
	// The post-process sees the GPU's NRGBA pixels as RGBA, in place.
	if img, ok := msg.frame.(*image.RGBA); !ok || img.Pix[0] != 200 {
		t.Fatalf("post-process not applied: %T", msg.frame)
	}
	// Software frames are not supersampled, but are post-processed.
	soft := New(scene(t), 30, 10, WithRenderMode(Software), WithSupersample(3), WithPostProcess(marked))
	defer soft.Close()
	soft.started = true
	msg = soft.schedule()().(rendered)
	if img, ok := msg.frame.(*image.RGBA); !ok || img.Pix[0] != 200 || msg.renderMode != Software {
		t.Fatal("software frame not post-processed")
	}
	if m.SetSupersample(9); m.Supersample() != 4 {
		t.Fatal("factor not limited")
	}
	img, _, err := m.Snapshot(40, 20)
	if err != nil || color.RGBAModel.Convert(img.At(0, 0)).(color.RGBA).R != 200 {
		t.Fatal("snapshot not post-processed", err)
	}
}

// The frame rate counts frames shown over the last second, and falls to
// zero once a second passes without one.
func TestFrameRate(t *testing.T) {
	m := &Model{}
	if m.FrameRate() != 0 {
		t.Fatal("rate before any frame")
	}
	now := time.Now()
	m.shown = []time.Time{now.Add(-1500 * time.Millisecond), now.Add(-900 * time.Millisecond), now.Add(-600 * time.Millisecond), now.Add(-300 * time.Millisecond), now}
	if r := m.FrameRate(); r < 3.2 || r > 3.4 {
		t.Fatalf("rate %.2f, want 3 frames in 0.9 s", r)
	}
	if len(m.shown) != 4 {
		t.Fatalf("%d frames kept, want the last second's 4", len(m.shown))
	}
	m.shown = []time.Time{now.Add(-2 * time.Second)}
	if m.FrameRate() != 0 {
		t.Fatal("rate after a second without frames")
	}
}

// Auto-rotation turns by the time between ticks: a slow frame turns as far
// as the fast ones it stood for.
func TestAutoRotateFollowsTime(t *testing.T) {
	m := New(scene(t), 80, 24, WithRenderMode(Software))
	defer m.Close()
	m.camera.AutoRotate = true
	start := time.Now()
	m.lastInput = start.Add(-time.Hour)
	turn := func(at time.Time) float64 {
		before := m.camera.Beta
		m.Update(tick{id: m.id, generation: m.wake, at: at})
		return m.camera.Beta - before
	}
	turn(start)
	if got := turn(start.Add(200 * time.Millisecond)); math.Abs(got-6) > 1e-9 {
		t.Fatalf("a 200 ms frame turned %v°, want 6°", got)
	}
	if got := turn(start.Add(1200*time.Millisecond + time.Hour)); math.Abs(got-30) > 1e-9 {
		t.Fatalf("a long gap turned %v°, want at most a second's 30°", got)
	}
}
