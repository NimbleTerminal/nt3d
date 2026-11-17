package nt3d

import (
	"image"
	"image/color"
	"reflect"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func testLook() Look {
	return Look{
		Background: color.RGBA{10, 20, 30, 255},
		Lighting: Lighting{
			Ambient: Ambient{Sky: color.RGBA{200, 210, 255, 255}, Ground: color.RGBA{40, 30, 20, 255}, Intensity: .5},
			Lights:  []Light{{Kind: PointLight, Position: nt3dmath.Vec3{X: 1, Y: 2, Z: 3}, Intensity: 2}},
		},
		Fog:                       Fog{Near: 5, Far: 50, Color: color.RGBA{90, 90, 90, 255}},
		EnvironmentStrength:       .25,
		EnvironmentBackgroundOnly: true,
		PostProcess:               PostProcess{Exposure: 1.5, Vignette: .2},
	}
}

// A scene that has been given no look draws as scenes always have.
func TestLookDefaultsToTheFrameDefaults(t *testing.T) {
	s := NewScene()
	want := Look{Background: color.RGBA{235, 238, 242, 255}, Lighting: DefaultLighting()}
	if got := DefaultLook(); !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultLook() = %+v, want %+v", got, want)
	}
	if got := s.Look(); !reflect.DeepEqual(got, want) {
		t.Fatalf("a new scene's look is %+v, want %+v", got, want)
	}
	f, err := s.Frame(DefaultCamera(), 32, 32)
	check(t, err)
	if f.Background != want.Background || !reflect.DeepEqual(f.Lighting, want.Lighting) || f.Fog != (Fog{}) || f.PostProcess != (PostProcess{}) || f.Environment != nil {
		t.Fatalf("a new scene's frame is lit and finished as %+v", f)
	}
}

func TestLookDressesEveryFrame(t *testing.T) {
	s := NewScene()
	look := testLook()
	env, err := NewEnvironment(image.NewRGBA(image.Rect(0, 0, 8, 4)))
	check(t, err)
	look.Environment = env
	s.SetLook(look)
	for _, frame := range []func(Camera, int, int) (Frame, error){s.Frame, s.StillFrame} {
		f, err := frame(DefaultCamera(), 32, 32)
		check(t, err)
		got := Look{Background: f.Background, Lighting: f.Lighting, Fog: f.Fog, Environment: f.Environment, EnvironmentStrength: f.EnvironmentStrength, EnvironmentBackgroundOnly: f.EnvironmentBackgroundOnly, PostProcess: f.PostProcess}
		if !reflect.DeepEqual(got, look) {
			t.Fatalf("frame is dressed as %+v, want %+v", got, look)
		}
	}
	if got := s.Look(); !reflect.DeepEqual(got, look) {
		t.Fatalf("Look() = %+v, want %+v", got, look)
	}
}

// The scene keeps its own copy of the lights, and each frame gets its own.
func TestLookLightsAreCopied(t *testing.T) {
	s := NewScene()
	look := testLook()
	s.SetLook(look)
	look.Lighting.Lights[0].Intensity = 99
	got := s.Look()
	got.Lighting.Lights[0].Intensity = 98
	f, err := s.Frame(DefaultCamera(), 32, 32)
	check(t, err)
	f.Lighting.Lights[0].Intensity = 97
	f.Lighting.Lights = append(f.Lighting.Lights, Light{})
	if again := s.Look().Lighting.Lights; len(again) != 1 || again[0].Intensity != 2 {
		t.Fatalf("the scene's lights became %+v through a copy", again)
	}
	g, err := s.Frame(DefaultCamera(), 32, 32)
	check(t, err)
	if len(g.Lighting.Lights) != 1 || g.Lighting.Lights[0].Intensity != 2 {
		t.Fatalf("a later frame's lights are %+v", g.Lighting.Lights)
	}
}

// Lights placed in the scene are its lights: they take the place of the
// look's, under the look's ambient light.
func TestLookNodeLightsReplaceItsLights(t *testing.T) {
	s := NewScene()
	look := testLook()
	s.SetLook(look)
	id, err := s.Add(Root, "lamp")
	check(t, err)
	check(t, s.SetNodeLight(id, Light{Kind: DirectionalLight, Direction: nt3dmath.Vec3{Z: 1}, Intensity: 3}))
	f, err := s.Frame(DefaultCamera(), 32, 32)
	check(t, err)
	if len(f.Lighting.Lights) != 1 || f.Lighting.Lights[0].Kind != DirectionalLight || f.Lighting.Lights[0].Intensity != 3 {
		t.Fatalf("frame lights %+v, want the node's light alone", f.Lighting.Lights)
	}
	if f.Lighting.Ambient != look.Lighting.Ambient {
		t.Fatalf("frame ambient %+v, want the look's %+v", f.Lighting.Ambient, look.Lighting.Ambient)
	}
}

// A look is in world space, as nodes are: its lights follow the camera's origin.
func TestLookLightsAreRelativeToTheFrameOrigin(t *testing.T) {
	s := NewScene()
	look := testLook()
	look.Lighting.Lights = []Light{
		{Kind: PointLight, Position: nt3dmath.Vec3{X: 1000, Y: 2000, Z: 3000}},
		{Kind: DirectionalLight, Direction: nt3dmath.Vec3{X: 1000, Y: 2000, Z: 3000}},
	}
	s.SetLook(look)
	camera := DefaultCamera()
	camera.Origin = [3]float64{990, 1980, 2970}
	f, err := s.Frame(camera, 32, 32)
	check(t, err)
	if got, want := f.Lighting.Lights[0].Position, (nt3dmath.Vec3{X: 10, Y: 20, Z: 30}); got != want {
		t.Fatalf("point light at %v in the frame, want %v", got, want)
	}
	if got := f.Lighting.Lights[1]; got != look.Lighting.Lights[1] {
		t.Fatalf("directional light became %+v", got)
	}
	if got := s.Look().Lighting.Lights[0].Position; got != look.Lighting.Lights[0].Position {
		t.Fatalf("the scene's own light moved to %v", got)
	}
}

func TestLookAndSetEnvironmentAreOneSetting(t *testing.T) {
	s := NewScene()
	env, err := NewEnvironment(image.NewRGBA(image.Rect(0, 0, 8, 4)))
	check(t, err)
	s.SetEnvironment(env)
	if s.Look().Environment != env {
		t.Fatal("SetEnvironment did not set the look's environment")
	}
	look := s.Look()
	look.Environment = nil
	s.SetLook(look)
	f, err := s.Frame(DefaultCamera(), 32, 32)
	check(t, err)
	if f.Environment != nil {
		t.Fatal("a look without an environment left the frame its old one")
	}
}

func TestLookChangesTheSceneRevisionOnlyWhenItChanges(t *testing.T) {
	s := NewScene()
	s.SetLook(testLook())
	rev := s.Revision()
	s.SetLook(testLook())
	if s.Revision() != rev {
		t.Fatal("setting the same look changed the scene's revision")
	}
	look := testLook()
	look.Lighting.Lights[0].Intensity = 3
	s.SetLook(look)
	if s.Revision() == rev {
		t.Fatal("a new look left the scene's revision unchanged")
	}
}
