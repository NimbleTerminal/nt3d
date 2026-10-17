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
	if got := m.frame().Lighting; !reflect.DeepEqual(got, nt3d.DefaultLighting()) {
		t.Fatalf("default lighting %+v", got)
	}
	l := nt3d.Lighting{
		Ambient: nt3d.Ambient{Sky: color.RGBA{100, 120, 200, 255}, Ground: color.RGBA{60, 40, 20, 255}},
		Lights:  []nt3d.Light{{Kind: nt3d.PointLight, Position: nt3dmath.Vec3{Z: 2}, Range: 4}},
	}
	m.SetLighting(l)
	if got := m.frame().Lighting; !reflect.DeepEqual(got, l) {
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
	f := m.frame()
	l.Lights[0].Position.X = 5
	if got := f.Lighting.Lights[0].Position.X; got != 0 {
		t.Fatalf("frame's light moved with the caller's: X = %v", got)
	}
}
