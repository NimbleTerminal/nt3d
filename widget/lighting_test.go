package nt3dwidget

import (
	"image/color"
	"reflect"
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
)

// SetLighting reaches the frames the widget renders; the default is the
// engine's.
func TestSetLighting(t *testing.T) {
	m := New(scene(t), 20, 8)
	defer m.Close()
	if got := frameOf(t, m).Lighting; !reflect.DeepEqual(got, nt3d.DefaultLighting()) {
		t.Fatalf("default lighting %+v", got)
	}
	l := nt3d.Lighting{
		Ambient: nt3d.Ambient{Sky: color.RGBA{100, 120, 200, 255}, Ground: color.RGBA{60, 40, 20, 255}},
		Lights:  []nt3d.Light{{Kind: nt3d.PointLight, Position: nt3dmath.Vec3{Z: 2}, Range: 4}},
	}
	m.SetLighting(l)
	if got := frameOf(t, m).Lighting; !reflect.DeepEqual(got, l) {
		t.Fatalf("lighting %+v, want %+v", got, l)
	}
}

// The widget keeps its own copy of the lights: a caller moving a light in
// place does not reach frames already taken, which may be rendering.
func TestSetLightingCopiesLights(t *testing.T) {
	m := New(scene(t), 20, 8)
	defer m.Close()
	l := nt3d.Lighting{Lights: []nt3d.Light{{Kind: nt3d.PointLight, Position: nt3dmath.Vec3{Z: 2}}}}
	m.SetLighting(l)
	f := frameOf(t, m)
	l.Lights[0].Position.X = 5
	if got := f.Lighting.Lights[0].Position.X; got != 0 {
		t.Fatalf("frame's light moved with the caller's: X = %v", got)
	}
}

func TestSceneEnvironmentReachesWidget(t *testing.T) {
	s := nt3d.NewScene()
	m := New(s, 20, 10, WithRenderMode(nt3d.Software))
	defer m.Close()
	e := nt3d.GradientEnvironment(color.RGBA{60, 100, 200, 255}, color.RGBA{200, 220, 240, 255}, color.RGBA{90, 80, 70, 255})
	s.SetEnvironment(e)
	f := frameOf(t, m)
	if f.Environment != e {
		t.Fatal("scene environment lost in widget frame")
	}
	s.SetEnvironment(nil)
	if frameOf(t, m).Environment != nil || f.Environment != e {
		t.Fatal("widget environment snapshot changed")
	}
}

// A scene's look reaches the widget's frames and snapshots; a setting the
// application gave the widget itself is the widget's and stands over it.
func TestSceneLookReachesWidget(t *testing.T) {
	s := scene(t)
	look := nt3d.Look{
		Background:  color.RGBA{10, 20, 30, 255},
		Lighting:    nt3d.Lighting{Ambient: nt3d.Ambient{Sky: color.RGBA{100, 120, 200, 255}, Ground: color.RGBA{60, 40, 20, 255}}},
		Fog:         nt3d.Fog{Near: 2, Far: 9},
		PostProcess: nt3d.PostProcess{Exposure: 1.5},
	}
	s.SetLook(look)
	m := New(s, 20, 8)
	defer m.Close()
	f := frameOf(t, m)
	if f.Background != look.Background || f.Fog != look.Fog || f.PostProcess != look.PostProcess || f.Lighting.Ambient != look.Lighting.Ambient {
		t.Fatalf("widget frame has background %v, fog %v, effects %v, ambient %v; want the scene's look", f.Background, f.Fog, f.PostProcess, f.Lighting.Ambient)
	}

	fog, effects := nt3d.Fog{Near: 1, Far: 3}, nt3d.PostProcess{Vignette: .5}
	m.SetFog(fog)
	m.SetPostProcess(effects)
	m.SetBackground(color.RGBA{200, 0, 0, 255})
	m.SetLighting(nt3d.DefaultLighting())
	f = frameOf(t, m)
	if f.Fog != fog || f.PostProcess != effects || f.Background != (color.RGBA{200, 0, 0, 255}) || !reflect.DeepEqual(f.Lighting, nt3d.DefaultLighting()) {
		t.Fatalf("widget frame has background %v, fog %v, effects %v, lighting %+v; want the widget's own", f.Background, f.Fog, f.PostProcess, f.Lighting)
	}
}

func TestWidgetOptionsStandOverSceneLook(t *testing.T) {
	s := scene(t)
	s.SetLook(nt3d.Look{Background: color.RGBA{10, 20, 30, 255}, PostProcess: nt3d.PostProcess{Exposure: 1.5}})
	m := New(s, 20, 8, WithBackground(color.RGBA{0, 200, 0, 255}), WithPostProcess(nt3d.PostProcess{Vignette: .5}))
	defer m.Close()
	f := frameOf(t, m)
	if f.Background != (color.RGBA{0, 200, 0, 255}) || f.PostProcess != (nt3d.PostProcess{Vignette: .5}) {
		t.Fatalf("widget frame has background %v and effects %v, want the options'", f.Background, f.PostProcess)
	}
}
