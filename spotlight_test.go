// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image/color"
	"math"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func testRotationX(radians float64) nt3dmath.Mat4 {
	m := nt3dmath.Identity()
	c, s := float32(math.Cos(radians)), float32(math.Sin(radians))
	m[5], m[6], m[9], m[10] = c, s, -s, c
	return m
}

func testRotationY(radians float64) nt3dmath.Mat4 {
	m := nt3dmath.Identity()
	c, s := float32(math.Cos(radians)), float32(math.Sin(radians))
	m[0], m[2], m[8], m[10] = c, -s, s, c
	return m
}

func TestSpotLightConeFalloffRangeAndValidation(t *testing.T) {
	inner := float32(math.Pi / 12) // 15 deg
	outer := float32(math.Pi / 4)  // 45 deg
	f := Frame{
		Width:  32,
		Height: 32,
		Linear: true,
		Lighting: Lighting{
			Lights: []Light{{
				Kind:      SpotLight,
				Position:  nt3dmath.Vec3{Z: 2},
				Direction: nt3dmath.Vec3{Z: -1},
				Color:     white,
				Intensity: 1,
				Range:     10,
				InnerCone: inner,
				OuterCone: outer,
			}},
		},
	}
	if !validLighting(f.Lighting) {
		t.Fatal("valid spotlight rejected")
	}

	rig := newRig(&f)
	evalAt := func(x float32) float32 {
		rgb := [3]float32{1, 1, 1}
		rig.light(&rgb, nt3dmath.Vec3{Z: 1}, nt3dmath.Vec3{X: x, Z: 0})
		return rgb[0]
	}

	center := evalAt(0)
	insideInner := evalAt(2 * float32(math.Tan(float64(inner*0.8))))
	penumbra := evalAt(2 * float32(math.Tan(float64((inner+outer)*0.5))))
	outside := evalAt(2 * float32(math.Tan(float64(outer*1.1))))
	// At distance 2 with Range 10, range attenuation is (1 - (2/10)^2)^2 = 0.9216.
	if math.Abs(float64(center-0.9216)) > 0.005 {
		t.Fatalf("expected range-attenuated spotlight center intensity ~0.9216, got %v", center)
	}
	if insideInner < 0.78 {
		t.Fatalf("expected strong inner-cone intensity, got %v", insideInner)
	}
	if !(penumbra > 0.05 && penumbra < insideInner*0.75) {
		t.Fatalf("expected smooth penumbra falloff: inner=%v penumbra=%v", insideInner, penumbra)
	}
	if outside != 0 {
		t.Fatalf("expected zero illumination outside outer cone, got %v", outside)
	}

	for _, okLight := range []Light{
		{Kind: SpotLight, Position: nt3dmath.Vec3{Z: 2}, Direction: nt3dmath.Vec3{Z: -1}},
		{Kind: SpotLight, Position: nt3dmath.Vec3{Z: 2}, Direction: nt3dmath.Vec3{Z: -1}, InnerCone: .3, OuterCone: .3},
	} {
		if !validLighting(Lighting{Lights: []Light{okLight}}) {
			t.Fatalf("expected valid spotlight %+v", okLight)
		}
	}

	for _, bad := range []Light{
		{Kind: SpotLight, Position: nt3dmath.Vec3{Z: 2}},
		{Kind: SpotLight, Direction: nt3dmath.Vec3{Z: -1}, InnerCone: -0.1, OuterCone: 0.4},
		{Kind: SpotLight, Direction: nt3dmath.Vec3{Z: -1}, InnerCone: 0.6, OuterCone: 0.4},
		{Kind: SpotLight, Direction: nt3dmath.Vec3{Z: -1}, OuterCone: float32(math.Pi / 2)},
		{Kind: SpotLight, Direction: nt3dmath.Vec3{Z: -1}, OuterCone: float32(math.NaN())},
		{Kind: LightKind(99), Direction: nt3dmath.Vec3{Z: -1}},
		{Kind: SpotLight, Position: nt3dmath.Vec3{Z: float32(math.Inf(1))}, Direction: nt3dmath.Vec3{Z: -1}},
	} {
		if validLighting(Lighting{Lights: []Light{bad}}) {
			t.Errorf("accepted invalid spotlight %+v", bad)
		}
	}
}

func TestSpotLightPerspectiveShadows(t *testing.T) {
	s := NewScene()
	floor, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 5, Y: 5, Z: 0.2}, Color: white})
	check(t, err)
	nFloor, err := s.SetGeometry("floor", floor)
	check(t, err)
	check(t, s.SetTransform(nFloor, nt3dmath.Translation(nt3dmath.Vec3{Z: -1.1})))

	blocker, err := BuildSphere(Sphere{Radius: 0.45, Color: color.RGBA{180, 60, 60, 255}})
	check(t, err)
	nBlocker, err := s.SetGeometry("blocker", blocker)
	check(t, err)
	check(t, s.SetTransform(nBlocker, nt3dmath.Translation(nt3dmath.Vec3{Z: -0.2})))

	cam := Camera{
		Projection: Perspective,
		Target:     nt3dmath.Vec3{Z: -0.6},
		Distance:   4.8,
		Alpha:      32,
		Beta:       35,
	}
	f, err := s.Frame(cam, 96, 96)
	check(t, err)
	f.Linear = true
	f.Lighting = Lighting{
		Ambient: Ambient{Sky: white, Ground: white, Intensity: 0.05},
		Lights: []Light{{
			Kind:      SpotLight,
			Position:  nt3dmath.Vec3{X: -1.4, Y: -1.0, Z: 2.2},
			Direction: nt3dmath.Vec3{X: 1.4, Y: 1.0, Z: -2.8}.Normalize(),
			Color:     white,
			Intensity: 2.8,
			Range:     12,
			InnerCone: 0.25,
			OuterCone: 0.58,
			Shadows:   true,
		}},
	}

	if shadowCaster(f.Lighting) != 0 {
		t.Fatalf("expected SpotLight at index 0 to be shadowCaster, got %d", shadowCaster(f.Lighting))
	}

	withShadow := softwareRender(f)
	fNoShadow := f
	fNoShadow.Lighting.Lights = []Light{f.Lighting.Lights[0]}
	fNoShadow.Lighting.Lights[0].Shadows = false
	noShadow := softwareRender(fNoShadow)

	shadowedPixels := 0
	litConePixels := 0
	for y := range 96 {
		for x := range 96 {
			cs := at(withShadow, x, y)
			cn := at(noShadow, x, y)
			if int(cn.G) > 80 && int(cs.G)+35 < int(cn.G) {
				shadowedPixels++
			}
			if int(cs.G) > 100 && cs.G == cs.B {
				litConePixels++
			}
		}
	}
	if shadowedPixels < 25 || litConePixels < 100 {
		t.Fatalf("expected both lit cone floor (%d px) and perspective shadow umbra (%d px)", litConePixels, shadowedPixels)
	}
}

func TestSceneNodeLightsAndHierarchyTransforms(t *testing.T) {
	s := NewScene()
	floor, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 4, Y: 4, Z: 0.2}, Color: white})
	check(t, err)
	nFloor, err := s.SetGeometry("floor", floor)
	check(t, err)
	check(t, s.SetTransform(nFloor, nt3dmath.Translation(nt3dmath.Vec3{Z: -1.1})))

	rig, err := s.Add(Root, "rig")
	check(t, err)
	check(t, s.SetTransform(rig, nt3dmath.Translation(nt3dmath.Vec3{X: 1.5, Y: -0.5, Z: 3.0})))

	spotNode, err := s.Add(rig, "key-spot")
	check(t, err)
	check(t, s.SetNodeLight(spotNode, Light{
		Kind:      SpotLight,
		Color:     color.RGBA{255, 220, 180, 255},
		Intensity: 2.5,
		Range:     12,
		InnerCone: 0.2,
		OuterCone: 0.5,
		Shadows:   true,
	}))

	dirNode, err := s.Add(Root, "rim-dir")
	check(t, err)
	check(t, s.SetNodeLight(dirNode, Light{
		Kind:      DirectionalLight,
		Intensity: 0.4,
	}))

	if err := s.SetNodeLight(Root, Light{Kind: PointLight}); err == nil {
		t.Fatal("expected error attaching light to Root")
	}

	ids := s.Lights(Root)
	if len(ids) != 2 || ids[0] != spotNode || ids[1] != dirNode {
		t.Fatalf("unexpected light node list: %v", ids)
	}

	resolvedSpot, err := s.NodeLight(spotNode)
	check(t, err)
	if resolvedSpot.Position != (nt3dmath.Vec3{X: 1.5, Y: -0.5, Z: 3.0}) || resolvedSpot.Direction != (nt3dmath.Vec3{Z: -1}) {
		t.Fatalf("unexpected resolved spot light: %+v", resolvedSpot)
	}

	// Rotating the parent rig updates resolved world Position/Direction dynamically.
	check(t, s.SetTransform(rig, testRotationX(math.Pi/2)))
	rotatedSpot, err := s.NodeLight(spotNode)
	check(t, err)
	// Rx(+90 deg) maps (0,0,-1) -> (0, +1, 0).
	if math.Abs(float64(rotatedSpot.Direction.Y-1)) > 1e-5 {
		t.Fatalf("expected rotated spot direction +Y, got %v", rotatedSpot.Direction)
	}

	f, err := s.Frame(DefaultCamera(), 48, 48)
	check(t, err)
	if len(f.Lighting.Lights) != 2 || f.Lighting.Lights[0].Kind != SpotLight || f.Lighting.Lights[1].Kind != DirectionalLight {
		t.Fatalf("Scene.Frame did not populate SceneLights: %+v", f.Lighting.Lights)
	}

	check(t, s.SetVisible(spotNode, false))
	fHidden, err := s.Frame(DefaultCamera(), 48, 48)
	check(t, err)
	if len(fHidden.Lighting.Lights) != 1 || fHidden.Lighting.Lights[0].Kind != DirectionalLight {
		t.Fatalf("expected hidden light node to be excluded, got %+v", fHidden.Lighting.Lights)
	}
}
