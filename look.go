package nt3d

import (
	"image/color"
	"slices"
)

// Look is how a scene is lit and finished: everything a frame shows that is
// neither a node nor the camera. A scene holds one (Scene.SetLook), and every
// frame made from it starts out dressed in it, so a scene is lit in one
// place rather than on each frame after it is made. A frame's own fields can
// still be changed afterwards, for one frame only.
//
// Positions are in world space, as nodes' are. A scene's light nodes
// (Scene.SetNodeLight), when it has any that are visible, are its lights and
// take the place of Lighting.Lights; Lighting.Ambient applies either way.
type Look struct {
	// Background is the color behind everything, where no Environment shows.
	Background color.RGBA
	Lighting   Lighting
	Fog        Fog
	// Environment is a distant sky, diffuse light, and reflections; see
	// Frame.Environment for it and the two settings below.
	Environment               *Environment
	EnvironmentStrength       float32
	EnvironmentBackgroundOnly bool
	PostProcess               PostProcess
}

// DefaultLook is the look of a new scene: a pale background and
// DefaultLighting, with no fog, environment, or post-processing.
func DefaultLook() Look {
	return Look{Background: color.RGBA{235, 238, 242, 255}, Lighting: DefaultLighting()}
}

// Look returns the scene's look. The lights are a copy.
func (s *Scene) Look() Look {
	l := s.look
	l.Lighting.Lights = slices.Clone(l.Lighting.Lights)
	return l
}

// SetLook sets how the scene is lit and finished, for every frame made from
// now on. The lights are copied. Frames already made keep the look they had,
// and geometry is not uploaded again. To change one setting, change it in
// what Look returns and set that.
func (s *Scene) SetLook(l Look) {
	if s.look.equal(l) {
		return
	}
	l.Lighting.Lights = slices.Clone(l.Lighting.Lights)
	s.look = l
	s.changed(false, false)
}

func (l Look) equal(m Look) bool {
	if !slices.Equal(l.Lighting.Lights, m.Lighting.Lights) {
		return false
	}
	l.Lighting.Lights, m.Lighting.Lights = nil, nil
	return l.Background == m.Background && l.Lighting.Ambient == m.Lighting.Ambient && l.Fog == m.Fog &&
		l.Environment == m.Environment && l.EnvironmentStrength == m.EnvironmentStrength &&
		l.EnvironmentBackgroundOnly == m.EnvironmentBackgroundOnly && l.PostProcess == m.PostProcess
}

// lightsRelative copies world-space lights into a frame's space, which is
// origin away: only a point or spot light has a position to move.
func lightsRelative(lights []Light, origin [3]float64) []Light {
	out := slices.Clone(lights)
	if origin == ([3]float64{}) {
		return out
	}
	for i := range out {
		if out[i].Kind == PointLight || out[i].Kind == SpotLight {
			p := &out[i].Position
			p.X = float32(float64(p.X) - origin[0])
			p.Y = float32(float64(p.Y) - origin[1])
			p.Z = float32(float64(p.Z) - origin[2])
		}
	}
	return out
}
