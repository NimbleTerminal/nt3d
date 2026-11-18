// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"testing"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
	"nimbleterminal.dev/nt3d"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

// A ruleSeed is a small document built to reach one place where a file says
// something nt3d's own rules do not allow as written, so that the reader has
// to translate it: a group for a lighting model the material does not use, a
// strength of zero, a combination that cannot be drawn. FuzzDecode starts
// from each, and holds whatever it mutates them into to the same rules.
type ruleSeed struct {
	name  string
	doc   func() *gltf.Document
	check func(t *testing.T, m *nt3dmesh.Model) // What the reader made of it.
}

// raw is v as an extension's JSON.
func raw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// one is the material of the model's one object.
func one(t *testing.T, m *nt3dmesh.Model) *nt3d.Material {
	t.Helper()
	if len(m.Objects) != 1 || m.Objects[0].Geometry.Material == nil {
		t.Fatalf("want one object with a material, got %d objects", len(m.Objects))
	}
	return m.Objects[0].Geometry.Material
}

// withMaterial is a triangle wearing material.
func withMaterial(material *gltf.Material, used ...string) *gltf.Document {
	p := upTriangle
	p.uv = [][2]float32{{0, 0}, {1, 0}, {0, 1}}
	p.material = gltf.Index(0)
	doc := gltfDoc(p)
	doc.Materials = []*gltf.Material{material}
	doc.ExtensionsUsed = used
	return doc
}

func ruleSeeds() []ruleSeed {
	return []ruleSeed{
		{
			// Unlit, yet carrying extensions that only a PBR surface has:
			// nt3d refuses a PBR group on a material that is not ShadePBR.
			name: "unlit with PBR extensions",
			doc: func() *gltf.Document {
				return withMaterial(&gltf.Material{Extensions: gltf.Extensions{
					"KHR_materials_unlit":     raw(map[string]any{}),
					"KHR_materials_clearcoat": raw(map[string]any{"clearcoatFactor": 1}),
					"KHR_materials_sheen":     raw(map[string]any{"sheenColorFactor": []float64{1, 0, 0}}),
					"KHR_materials_specular":  raw(map[string]any{"specularFactor": 0}),
				}}, "KHR_materials_unlit", "KHR_materials_clearcoat", "KHR_materials_sheen", "KHR_materials_specular")
			},
			check: func(t *testing.T, m *nt3dmesh.Model) {
				if mat := one(t, m); mat.Shading != nt3d.ShadeUnlit || mat.PBR != (nt3d.PBRStyle{}) {
					t.Fatalf("want an unlit material with no PBR group, got %+v", mat)
				}
			},
		},
		{
			// A highlight of no strength: a zero Strength would mean 1.
			name: "specular factor of zero",
			doc: func() *gltf.Document {
				return withMaterial(&gltf.Material{Extensions: gltf.Extensions{
					"KHR_materials_specular": raw(map[string]any{"specularFactor": 0}),
				}}, "KHR_materials_specular")
			},
			check: func(t *testing.T, m *nt3dmesh.Model) {
				if mat := one(t, m); mat.Shading != nt3d.ShadePBR || !mat.PBR.Specular.Off {
					t.Fatalf("want a PBR material with its highlight off, got %+v", mat)
				}
			},
		},
		{
			// Transmission through a blended surface, which nt3d cannot draw.
			name: "transmission on a blended surface",
			doc: func() *gltf.Document {
				return withMaterial(&gltf.Material{AlphaMode: gltf.AlphaBlend, Extensions: gltf.Extensions{
					"KHR_materials_transmission": raw(map[string]any{"transmissionFactor": 1}),
				}}, "KHR_materials_transmission")
			},
			check: func(t *testing.T, m *nt3dmesh.Model) {
				if mat := one(t, m); mat.Alpha != nt3d.AlphaTransparent || mat.Transmission != (nt3d.TransmissionStyle{}) {
					t.Fatalf("want a blended material with no transmission, got %+v", mat)
				}
			},
		},
		{
			// A normal map scaled to nothing: a zero Strength would mean 1.
			name: "normal map of zero scale",
			doc: func() *gltf.Document {
				doc := withMaterial(&gltf.Material{NormalTexture: &gltf.NormalTexture{Index: gltf.Index(0), Scale: gltf.Float(0)}})
				var pixel bytes.Buffer
				if err := png.Encode(&pixel, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
					panic(err)
				}
				img, err := modeler.WriteImage(doc, "n", "image/png", &pixel)
				if err != nil {
					panic(err)
				}
				doc.Textures = []*gltf.Texture{{Source: gltf.Index(img)}}
				return doc
			},
			check: func(t *testing.T, m *nt3dmesh.Model) {
				if len(m.Objects) != 1 {
					t.Fatalf("want one object, got %d", len(m.Objects))
				}
				if mat := m.Objects[0].Geometry.Material; mat != nil && mat.Normal != (nt3d.NormalStyle{}) {
					t.Fatalf("want no normal map, got %+v", mat.Normal)
				}
			},
		},
		{
			// A light of no intensity: a zero Intensity would mean 1.
			name: "light of no intensity",
			doc: func() *gltf.Document {
				doc := gltfDoc(upTriangle)
				doc.ExtensionsUsed = []string{"KHR_lights_punctual"}
				doc.Extensions = gltf.Extensions{"KHR_lights_punctual": raw(map[string]any{
					"lights": []map[string]any{{"type": "point", "intensity": 0}},
				})}
				doc.Nodes[0].Extensions = gltf.Extensions{"KHR_lights_punctual": raw(map[string]any{"light": 0})}
				return doc
			},
			check: func(t *testing.T, m *nt3dmesh.Model) {
				s := nt3d.NewScene()
				if _, err := m.Asset.Instantiate(s, nt3d.Root, "seed"); err != nil {
					t.Fatal(err)
				}
				if lights := s.SceneLights(nt3d.Root); len(lights) != 1 || !lights[0].Off {
					t.Fatalf("want one light that is off, got %+v", lights)
				}
			},
		},
	}
}

// encoded is the seed's document as FuzzDecode is given it.
func (s ruleSeed) encoded() []byte {
	var buf bytes.Buffer
	if err := gltf.NewEncoder(&buf).Encode(s.doc()); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// Each rule seed is accepted, passes the fuzz target's own checks, was read
// the way its rule requires, and is small enough for the fuzzing fleet, where
// a seed over about 1 KB stalls minimization.
func TestRuleSeedsReachTheirRules(t *testing.T) {
	for _, seed := range ruleSeeds() {
		t.Run(seed.name, func(t *testing.T) {
			data := seed.encoded()
			if len(data) > 1024 {
				t.Errorf("seed is %d bytes, over 1 KB", len(data))
			}
			o := fuzzOptions()
			m, err := Decode(data, o)
			if err != nil {
				t.Fatalf("the seed is refused, so the fuzzer would start from nothing: %v", err)
			}
			checkModel(t, m, err, o)
			checkAsset(t, m)
			seed.check(t, m)
		})
	}
}
