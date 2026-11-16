// Copyright (c) 2026 Neomantra Corp.

package main

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

func TestPostPresetsAndFrameBuilder(t *testing.T) {
	lighting := newStudioLighting()
	if err := lighting.setPostByName("studio"); err != nil {
		t.Fatalf("setPostByName(studio): %v", err)
	}
	if !lighting.overridePostInDemo || lighting.postName() != "Studio" {
		t.Fatalf("unexpected post state: override=%v name=%q", lighting.overridePostInDemo, lighting.postName())
	}
	if err := lighting.setPostByName("unknown"); err == nil {
		t.Fatal("expected error for unknown post preset")
	}

	v, err := buildDemoScene()
	if err != nil {
		t.Fatal(err)
	}
	defer v.close()

	f, err := v.buildFrame(nt3d.DefaultCamera(), 320, 200, true, 2)
	if err != nil {
		t.Fatal(err)
	}
	lighting.applyFramePost(&f, false)
	if !f.MSAA || !f.OIT || f.Supersample != 2 || f.PostProcess.ToneMap != nt3d.ACESToneMap || f.PostProcess.SSAO.Strength <= 0 {
		t.Fatalf("frame settings not applied: msaa=%v oit=%v ss=%d post=%+v", f.MSAA, f.OIT, f.Supersample, f.PostProcess)
	}
	if got := formatAAStatus(true, 2); got != "4x MSAA+2x SS" {
		t.Fatalf("formatAAStatus(true, 2) = %q", got)
	}
	if _, err := nt3d.NewSoftwareRenderer().Render(f); err != nil {
		t.Fatalf("software render of PBR+OIT demo scene: %v", err)
	}

	// Verify HDR Studio Sun environment + Spotlights rig with perspective shadow maps!
	lighting.envIndex = 1 // HDR Studio Sun
	lighting.rigIndex = 2 // Spotlights
	lighting.shadows = true
	lighting.applyEnvironment(v)
	f2, err := v.buildFrame(nt3d.DefaultCamera(), 160, 100, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	lighting.applyFrameLighting(&f2, v)
	lighting.applyFramePost(&f2, false)
	if len(f2.Lighting.Lights) != 3 || f2.Lighting.Lights[0].Kind != nt3d.SpotLight || !f2.Lighting.Lights[0].Shadows {
		t.Fatalf("expected Spotlights rig with key shadow, got %+v", f2.Lighting.Lights)
	}
	if _, err := nt3d.NewSoftwareRenderer().Render(f2); err != nil {
		t.Fatalf("software render of HDR+Spotlights+OIT scene: %v", err)
	}

	// Verify authored node lights (KHR_lights_punctual) are preserved until overridden.
	ln, err := v.scene.Add(nt3d.Root, "gltf-spot")
	if err != nil {
		t.Fatal(err)
	}
	if err := v.scene.SetNodeLight(ln, nt3d.Light{Kind: nt3d.SpotLight, Intensity: 2.0, InnerCone: 0.2, OuterCone: 0.5}); err != nil {
		t.Fatal(err)
	}
	lighting.overrideInDemo = false
	f3, err := v.buildFrame(nt3d.DefaultCamera(), 160, 100, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	lighting.applyFrameLighting(&f3, v)
	if len(f3.Lighting.Lights) != 1 || f3.Lighting.Lights[0].Kind != nt3d.SpotLight || !f3.Lighting.Lights[0].Shadows {
		t.Fatalf("expected preserved scene spotlight, got %+v", f3.Lighting.Lights)
	}

	// Verify intersecting crystal-glass rings in default scene show distinct OIT vs sorted output.
	fOIT, err := v.buildFrame(nt3d.DefaultCamera(), 120, 90, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	lighting.oit = true
	lighting.applyFramePost(&fOIT, false)
	fSorted := fOIT
	lighting.toggleOIT()
	lighting.applyFramePost(&fSorted, false)
	if !fOIT.OIT || fSorted.OIT {
		t.Fatalf("unexpected OIT toggle flags: oit=%v sorted=%v", fOIT.OIT, fSorted.OIT)
	}
	sw := nt3d.NewSoftwareRenderer()
	imgOIT, err := sw.Render(fOIT)
	if err != nil {
		t.Fatal(err)
	}
	imgSorted, err := sw.Render(fSorted)
	if err != nil {
		t.Fatal(err)
	}
	diff := 0
	for y := 0; y < 90; y++ {
		for x := 0; x < 120; x++ {
			if imgOIT.At(x, y) != imgSorted.At(x, y) {
				diff++
			}
		}
	}
	if diff < 20 {
		t.Fatalf("expected intersecting glass rings in gogpu scene to differ between OIT and sorted, got %d pixels", diff)
	}
}

func TestViewerLoadModelSceneVariantsAndMultiLOD(t *testing.T) {
	var buf []byte
	appendF32 := func(vals ...float32) {
		for _, v := range vals {
			var b [4]byte
			binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
			buf = append(buf, b[:]...)
		}
	}
	appendU16 := func(vals ...uint16) {
		for _, v := range vals {
			var b [2]byte
			binary.LittleEndian.PutUint16(b[:], v)
			buf = append(buf, b[:]...)
		}
	}
	appendF32(-1, -1, 0, 1, -1, 0, 1, 1, 0, -1, 1, 0)
	appendF32(0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1)
	appendU16(0, 1, 2, 0, 2, 3)
	appendU16(0, 1, 2, 0)

	uri := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(buf)
	gltfJSON := fmt.Sprintf(`{
  "asset": {"version": "2.0"},
  "extensionsUsed": ["KHR_materials_variants"],
  "extensions": {
    "KHR_materials_variants": {
      "variants": [{"name": "Taxi"}, {"name": "Police"}]
    }
  },
  "buffers": [{"uri": %q, "byteLength": %d}],
  "bufferViews": [
    {"buffer": 0, "byteOffset": 0, "byteLength": 48},
    {"buffer": 0, "byteOffset": 48, "byteLength": 48},
    {"buffer": 0, "byteOffset": 96, "byteLength": 12},
    {"buffer": 0, "byteOffset": 108, "byteLength": 6}
  ],
  "accessors": [
    {"bufferView": 0, "componentType": 5126, "count": 4, "type": "VEC3", "min": [-1, -1, 0], "max": [1, 1, 0]},
    {"bufferView": 1, "componentType": 5126, "count": 4, "type": "VEC3"},
    {"bufferView": 2, "componentType": 5123, "count": 6, "type": "SCALAR"},
    {"bufferView": 3, "componentType": 5123, "count": 3, "type": "SCALAR"}
  ],
  "materials": [
    {"name": "Default", "pbrMetallicRoughness": {"baseColorFactor": [0.8, 0.8, 0.8, 1]}},
    {"name": "Taxi", "pbrMetallicRoughness": {"baseColorFactor": [0.95, 0.75, 0.05, 1]}},
    {"name": "Police", "pbrMetallicRoughness": {"baseColorFactor": [0.1, 0.3, 0.95, 1]}}
  ],
  "meshes": [
    {"name": "Sign_LOD0", "primitives": [{"attributes": {"POSITION": 0, "NORMAL": 1}, "indices": 2, "material": 0, "extensions": {"KHR_materials_variants": {"mappings": [{"material": 1, "variants": [0]}, {"material": 2, "variants": [1]}]}}}]},
    {"name": "Sign_LOD1", "primitives": [{"attributes": {"POSITION": 0, "NORMAL": 1}, "indices": 3, "material": 0, "extensions": {"KHR_materials_variants": {"mappings": [{"material": 1, "variants": [0]}, {"material": 2, "variants": [1]}]}}}]}
  ],
  "nodes": [
    {"name": "SM_Sign_LOD0", "mesh": 0, "translation": [0, 0, 0]},
    {"name": "SM_Sign_LOD1", "mesh": 1, "translation": [4, 0, 0]}
  ],
  "scenes": [{"nodes": [0, 1]}],
  "scene": 0
}`, uri, len(buf))

	p := filepath.Join(t.TempDir(), "sign_pack.gltf")
	if err := os.WriteFile(p, []byte(gltfJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	v, err := loadModelScene([]string{p}, nt3dmesh.ZUp)
	if err != nil {
		t.Fatal(err)
	}
	defer v.close()

	if !v.hasLOD || !v.lodAuto || len(v.variants) != 2 || v.totalTriangles != 2 {
		t.Fatalf("unexpected initial state: hasLOD=%v lodAuto=%v variants=%v tris=%d",
			v.hasLOD, v.lodAuto, v.variants, v.totalTriangles)
	}

	cam := nt3d.Camera{Target: nt3dmath.Vec3{}, Distance: 3.0, Beta: -90, Near: 0.1, Far: 100, Projection: nt3d.Perspective}
	f0, err := v.buildFrame(cam, 120, 90, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	v.lastStats = f0.Stats()
	v.haveStats = true

	hud := newHUDOverlay()
	lighting := newStudioLighting()
	txt0 := hud.formatText(v, cam, lighting, true, false, false, true, false, false, 1)
	if !strings.Contains(txt0, "2 / 2 tris") || !strings.Contains(txt0, "Variant: default (0/2) [V]") ||
		!strings.Contains(txt0, "LOD: auto (bias 1.00x, tiers 1/0) [D/-/=/0]") ||
		!strings.Contains(txt0, "[V] Variant [D] LOD") {
		t.Fatalf("unexpected HUD text:\n%s", txt0)
	}

	// Step variant forward to Taxi, then backward to default, then backward to Police.
	v.stepVariant(1)
	if got := v.activeVariantName(); got != "Taxi" {
		t.Fatalf("got variant %q, want Taxi", got)
	}
	v.stepVariant(-1)
	if got := v.activeVariantName(); got != "" {
		t.Fatalf("got variant %q, want default", got)
	}
	v.stepVariant(-1)
	if got := v.activeVariantName(); got != "Police" {
		t.Fatalf("got variant %q, want Police", got)
	}

	// Step LOD bias down to 0.5x so threshold 4.0 becomes 2.0 (< Distance 3.0 -> switches to LOD1 = 1 tri!).
	v.stepLODBias(0.5)
	f1, err := v.buildFrame(cam, 120, 90, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	v.lastStats = f1.Stats()
	v.haveStats = true
	txt1 := hud.formatText(v, cam, lighting, true, false, false, true, false, false, 1)
	if !strings.Contains(txt1, "1 / 2 tris") || !strings.Contains(txt1, "tiers 0/1") || !strings.Contains(txt1, "Variant: Police (2/2) [V]") {
		t.Fatalf("unexpected LOD1 HUD text:\n%s", txt1)
	}

	// Reset bias with factor 0 and toggle LOD mode off (raw showcase: 2 + 1 = 3 tris).
	v.stepLODBias(0)
	v.toggleLODMode()
	if v.lodAuto || v.totalTriangles != 3 {
		t.Fatalf("expected raw LOD off with 3 tris, got lodAuto=%v tris=%d", v.lodAuto, v.totalTriangles)
	}
	txtRaw := hud.formatText(v, cam, lighting, true, false, false, true, false, false, 1)
	if !strings.Contains(txtRaw, "3 tris") || !strings.Contains(txtRaw, "LOD: raw [D]") {
		t.Fatalf("unexpected raw HUD text:\n%s", txtRaw)
	}
}
