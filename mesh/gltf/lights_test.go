// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"encoding/json"
	"image/color"
	"math"
	"testing"

	"github.com/qmuntal/gltf"
	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	check(t, err)
	return b
}

func TestGLTFKHRLightsPunctualSceneAndTransforms(t *testing.T) {
	doc := gltfDoc(upTriangle)
	doc.ExtensionsUsed = []string{"KHR_lights_punctual"}
	doc.ExtensionsRequired = []string{"KHR_lights_punctual"}
	doc.Extensions = gltf.Extensions{
		"KHR_lights_punctual": rawJSON(t, map[string]any{
			"lights": []map[string]any{
				{
					"name":      "key-spot",
					"type":      "spot",
					"color":     []float64{1.0, 0.5, 0.25},
					"intensity": 3.5,
					"range":     15.0,
					"spot": map[string]any{
						"innerConeAngle": 0.2,
						"outerConeAngle": 0.6,
					},
				},
				{
					"name":      "fill-point",
					"type":      "point",
					"intensity": 0.0, // explicit zero intensity -> black light
					"range":     8.0,
				},
				{
					"name":      "sun",
					"type":      "directional",
					"color":     []float64{1, 1, 1},
					"intensity": 1.25,
				},
			},
		}),
	}

	// In glTF Y-up:
	// - A node at (1, 3, 2) with identity rotation shines down glTF -Z.
	//   After nt3d's Y-up -> Z-up root rotation Rx(+90 deg):
	//   position (1, 3, 2)_gltf -> (1, -2, 3)_nt3d
	//   glTF forward (0, 0, -1)_gltf -> (0, +1, 0)_nt3d!
	doc.Nodes = append(doc.Nodes,
		&gltf.Node{
			Name:        "spot-node",
			Translation: [3]float64{1, 3, 2},
			Rotation:    [4]float64{0, 0, 0, 1},
			Scale:       [3]float64{1, 1, 1},
			Extensions:  gltf.Extensions{"KHR_lights_punctual": rawJSON(t, map[string]any{"light": 0})},
		},
		&gltf.Node{
			Translation: [3]float64{0, 2, 0},
			Rotation:    [4]float64{0, 0, 0, 1},
			Scale:       [3]float64{1, 1, 1},
			Extensions:  gltf.Extensions{"KHR_lights_punctual": rawJSON(t, map[string]any{"light": 1})},
		},
		&gltf.Node{
			Name:       "sun-node",
			Rotation:   [4]float64{0, 0, 0, 1},
			Scale:      [3]float64{1, 1, 1},
			Extensions: gltf.Extensions{"KHR_lights_punctual": rawJSON(t, map[string]any{"light": 2})},
		},
	)
	doc.Scenes[0].Nodes = []int{0, 1, 2, 3}

	m, err := Decode(glb(t, doc), nil)
	check(t, err)

	s := nt3d.NewScene()
	inst, err := m.Asset.Instantiate(s, nt3d.Root, "scene")
	check(t, err)

	lights := s.SceneLights(inst.Root)
	if len(lights) != 3 {
		t.Fatalf("expected 3 punctual lights, got %d", len(lights))
	}

	spot := lights[0]
	if spot.Kind != nt3d.SpotLight || spot.Intensity != 3.5 || spot.Range != 15 ||
		math.Abs(float64(spot.InnerCone-0.2)) > 1e-5 || math.Abs(float64(spot.OuterCone-0.6)) > 1e-5 {
		t.Fatalf("unexpected decoded spot light: %+v", spot)
	}
	if !near(spot.Position, nt3dmath.Vec3{X: 1, Y: -2, Z: 3}) || !near(spot.Direction, nt3dmath.Vec3{Y: 1}) {
		t.Fatalf("spot world transform mismatch: pos=%v dir=%v", spot.Position, spot.Direction)
	}

	offPoint := lights[1]
	if offPoint.Kind != nt3d.PointLight || offPoint.Color != (color.RGBA{0, 0, 0, 255}) || offPoint.Range != 8 {
		t.Fatalf("expected zero-intensity point light mapped to black, got %+v", offPoint)
	}

	// Unnamed node inherited light name "fill-point" in AssetNode and Scene.
	if _, ok := s.Find("scene/3:fill-point"); !ok {
		t.Fatal("expected unnamed light node to inherit KHR_lights_punctual light name")
	}

	sun := lights[2]
	// DirectionalLight Direction points TOWARD the light (+Z local in glTF -> -Y in nt3d).
	if sun.Kind != nt3d.DirectionalLight || !near(sun.Direction, nt3dmath.Vec3{Y: -1}) {
		t.Fatalf("unexpected directional light: %+v", sun)
	}
}

func TestGLTFKHRLightsPunctualValidationErrors(t *testing.T) {
	badLights := []struct {
		name  string
		light map[string]any
		want  string
	}{
		{"unknown-type", map[string]any{"type": "area"}, "unknown type"},
		{"bad-color-len", map[string]any{"type": "point", "color": []float64{1, 1}}, "3 values"},
		{"bad-color-range", map[string]any{"type": "point", "color": []float64{1, 2, 0}}, "invalid KHR_lights_punctual color"},
		{"neg-intensity", map[string]any{"type": "point", "intensity": -1.0}, "intensity"},
		{"zero-range", map[string]any{"type": "point", "range": 0.0}, "range"},
		{"bad-cone", map[string]any{"type": "spot", "spot": map[string]any{"innerConeAngle": 0.8, "outerConeAngle": 0.5}}, "spot cone"},
	}
	for _, tc := range badLights {
		doc := gltfDoc(upTriangle)
		doc.Extensions = gltf.Extensions{
			"KHR_lights_punctual": rawJSON(t, map[string]any{"lights": []map[string]any{tc.light}}),
		}
		_, err := Decode(glb(t, doc), nil)
		fails(t, err, tc.want)
	}

	// Out-of-bounds node light index.
	doc := gltfDoc(upTriangle)
	doc.Extensions = gltf.Extensions{
		"KHR_lights_punctual": rawJSON(t, map[string]any{"lights": []map[string]any{{"type": "point"}}}),
	}
	doc.Nodes[0].Extensions = gltf.Extensions{
		"KHR_lights_punctual": rawJSON(t, map[string]any{"light": 5}),
	}
	_, err := Decode(glb(t, doc), nil)
	fails(t, err, "light index")
}
