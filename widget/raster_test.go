package nt3dwidget

import (
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/nt3d"
	picture "nimbleterminal.dev/ntpicture"
)

func TestRasterTargetMatchesEncodedFrame(t *testing.T) {
	session := picture.NewTerminalSession(picture.TerminalOptions{Kitty: picture.KittyCapabilitySupported, SharedMemory: picture.KittyCapabilityUnsupported, CellPixelWidth: 9, CellPixelHeight: 17})
	defer session.Close()
	m := New(scene(t), 107, 43, WithRenderMode(Software), WithTerminalSession(session), WithKittyFormat(picture.KittyFormatRGBA))
	defer m.Close()
	drain(t, m, m.pic.SetMode(picture.PictureKitty))
	m.shrink = .7
	drain(t, m, m.resolution())
	f := m.frame()
	if f.Width != 289 || f.Height != 219 {
		t.Fatalf("frame %dx%d, want 289x219", f.Width, f.Height)
	}
	if f.Matrix != m.camera.Matrix(float32(963.0/731)) {
		t.Fatal("projection used rounded pixel aspect")
	}
	m.dirty = false
	img, err := nt3d.NewSoftwareRenderer().Render(f)
	if err != nil {
		t.Fatal(err)
	}
	_, cmd := m.Update(rendered{id: m.id, seq: m.seq, frame: img, geometry: m.pic.RasterGeometry(), renderMode: Software, elapsed: 40 * time.Millisecond})
	msgs := drain(t, m, cmd)
	var raw string
	for _, msg := range msgs {
		if v, ok := msg.(tea.RawMsg); ok {
			raw += fmt.Sprint(v.Msg)
		}
	}
	if !strings.Contains(raw, "s=289,v=219") {
		t.Fatal("encoder did not use producer dimensions")
	}
	m.forceGlyph = true
	drain(t, m, m.pic.SetMode(picture.PictureGlyph))
	f = m.frame()
	if f.Width != 107 || f.Height != 86 || f.Matrix != m.camera.Matrix(float32(963.0/731)) {
		t.Fatal("glyph target/aspect mismatch")
	}
}

func TestStaleRasterFrameIsDrawnAgain(t *testing.T) {
	session := picture.NewTerminalSession(picture.TerminalOptions{Kitty: picture.KittyCapabilitySupported, SharedMemory: picture.KittyCapabilityUnsupported, CellPixelWidth: 9, CellPixelHeight: 17})
	defer session.Close()
	m := New(scene(t), 40, 12, WithRenderMode(Software), WithTerminalSession(session), WithKittyFormat(picture.KittyFormatRGBA))
	defer m.Close()
	m.started = true
	stale := m.pic.RasterGeometry()
	m.shrink = .5 // Software pacing shrinks the raster while a frame renders.
	drain(t, m, m.resolution())
	if m.pic.RasterGeometry() == stale {
		t.Fatal("raster did not change")
	}
	img, err := nt3d.NewSoftwareRenderer().Render(m.frame())
	if err != nil {
		t.Fatal(err)
	}
	m.busy, m.dirty = true, false
	generation := m.pic.Status().Generation
	_, cmd := m.Update(rendered{id: m.id, seq: m.seq, frame: img, geometry: stale, renderMode: Software, elapsed: 40 * time.Millisecond})
	if m.pic.Status().Generation != generation {
		t.Fatal("frame for an old raster was submitted")
	}
	// ntpicture drops the frame and says so; the widget then draws again.
	var dropped *picture.StaleFrameMsg
	for _, msg := range batched(cmd) {
		if v, ok := msg.(picture.StaleFrameMsg); ok {
			dropped = &v
		}
	}
	if dropped == nil {
		t.Fatal("dropped frame was not reported")
	}
	m.Update(picture.StaleFrameMsg{Owner: picture.New(picture.Options{}), Geometry: dropped.Geometry})
	if m.busy {
		t.Fatal("another picture's stale frame drew this one")
	}
	if _, cmd := m.Update(*dropped); !m.busy || cmd == nil {
		t.Fatal("frame for an old raster was not drawn again")
	}
}

// batched runs cmd and any batch it returns, without feeding the messages
// back to a model.
func batched(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var messages []tea.Msg
		for _, c := range batch {
			messages = append(messages, batched(c)...)
		}
		return messages
	}
	return []tea.Msg{msg}
}

func TestRasterResolutionFollowsRenderModeAndBudget(t *testing.T) {
	session := picture.NewTerminalSession(picture.TerminalOptions{Kitty: picture.KittyCapabilitySupported, SharedMemory: picture.KittyCapabilityUnsupported, CellPixelWidth: 16, CellPixelHeight: 32})
	defer session.Close()
	m := New(scene(t), 200, 100, WithRenderMode(Software), WithTerminalSession(session))
	defer m.Close()
	drain(t, m, m.pic.SetMode(picture.PictureKitty))
	m.shrink = .75
	drain(t, m, m.resolution())
	if m.pic.RasterGeometry().Pixels != image.Pt(800, 800) {
		t.Fatal("software resolution not applied")
	}
	drain(t, m, m.SetRenderMode(WebGPU))
	drain(t, m, m.resolution())
	f := m.frame()
	if f.Width <= 800 || f.Width*f.Height > nt3d.MaxFramePixels {
		t.Fatal("GPU resolution not restored within budget")
	}
	if m.pic.RasterGeometry().Pixels != image.Pt(f.Width, f.Height) {
		t.Fatal("frame and encoding target differ")
	}
	m.forceGlyph = true
	drain(t, m, m.pic.SetTerminal(false, 1, 2))
	drain(t, m, m.SetSize(4096, 2160))
	f = m.frame()
	if f.Width*f.Height > nt3d.MaxFramePixels || f.Width != m.width || f.Height != 2*m.height {
		t.Fatal("large glyph layout exceeds budget")
	}
}

func TestResolutionPresentationDoesNotTickDuringRender(t *testing.T) {
	m := New(scene(t), 20, 8, WithRenderMode(Software))
	defer m.Close()
	m.camera.AutoRotate = true
	m.busy = true
	m.dirty = false
	if m.finish() != nil {
		t.Fatal("old front completion started camera animation during rendering")
	}
}

// A view with no cells settles rather than drawing over and over a frame
// ntpicture reports stale: ntpicture keeps its raster at least a cell.
func TestEmptyViewSettles(t *testing.T) {
	session := picture.NewTerminalSession(picture.TerminalOptions{Kitty: picture.KittyCapabilitySupported, SharedMemory: picture.KittyCapabilityUnsupported, CellPixelWidth: 9, CellPixelHeight: 17})
	defer session.Close()
	m := New(scene(t), 0, 0, WithRenderMode(Software), WithTerminalSession(session))
	defer m.Close()
	m.started = true
	drain(t, m, m.Refresh())
	drain(t, m, m.SetSize(0, 3))
}
