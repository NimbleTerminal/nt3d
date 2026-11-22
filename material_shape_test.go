// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image/color"
	"reflect"
	"strings"
	"testing"
)

// A material's lighting model is what Shading says and nothing else: a PBR
// material stays PBR when its roughness and metalness are zero, and fields
// that belong to another model are refused instead of switching the model or
// being ignored.
func TestShadingModelIsExplicit(t *testing.T) {
	if !pbrActive(&Material{Shading: ShadePBR}) {
		t.Fatal("a PBR material with zero roughness and metalness is not PBR")
	}
	if pbrActive(&Material{}) || pbrActive(&Material{Shading: ShadeUnlit}) {
		t.Fatal("a material that does not say ShadePBR is PBR")
	}
	for name, c := range map[string]struct {
		m    Material
		want string // What the error must mention.
	}{
		"PBR fields on Phong":   {Material{PBR: PBRStyle{Roughness: .4}}, "ShadePBR"},
		"a coat on Phong":       {Material{PBR: PBRStyle{Clearcoat: ClearcoatStyle{Amount: 1}}}, "ShadePBR"},
		"PBR fields on unlit":   {Material{Shading: ShadeUnlit, PBR: PBRStyle{Metallic: 1}}, "ShadePBR"},
		"Phong fields on PBR":   {Material{Shading: ShadePBR, Phong: PhongStyle{Specular: .5}}, "ShadePhong"},
		"Phong fields on unlit": {Material{Shading: ShadeUnlit, Phong: PhongStyle{Reflectivity: .5}}, "ShadePhong"},
		"an unknown model":      {Material{Shading: ShadeUnlit + 1}, "Shading"},
		"an unknown alpha mode": {Material{Alpha: AlphaHoldout + 1}, "Alpha"},
		"lit holdout":           {Material{Alpha: AlphaHoldout}, "ShadeUnlit"},
		"depth offset non-hold": {Material{Shading: ShadeUnlit, DepthOffset: .1}, "AlphaHoldout"},
	} {
		err := c.m.Validate()
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: the error does not mention %s: %v", name, c.want, err)
		}
	}
	for name, m := range map[string]Material{
		"zero":   {},
		"Phong":  {Phong: PhongStyle{Specular: .5, Shininess: 64, Reflectivity: .1}},
		"PBR":    {Shading: ShadePBR, PBR: PBRStyle{Roughness: .4, Metallic: 1}},
		"mirror": {Shading: ShadePBR},
		"unlit":  {Shading: ShadeUnlit},
	} {
		if err := m.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// What a material does with alpha is one choice, so a cutout cannot also be
// blended. Edge-on is a kind of cutout, and transmission needs an opaque,
// lit surface.
func TestAlphaIsOneChoice(t *testing.T) {
	for mode, cutout := range map[AlphaMode]bool{AlphaOpaque: false, AlphaCutout: true, AlphaEdgeOn: true, AlphaTransparent: false} {
		m := &Material{Alpha: mode}
		if m.cutout() != cutout || m.blended() != (mode == AlphaTransparent) || m.edgeOn() != (mode == AlphaEdgeOn) {
			t.Errorf("mode %d: cutout %v, blended %v, edge-on %v", mode, m.cutout(), m.blended(), m.edgeOn())
		}
	}
	through := TransmissionStyle{Amount: .5}
	if err := (Material{Transmission: through}).Validate(); err != nil {
		t.Fatalf("transmission through an opaque surface: %v", err)
	}
	for name, m := range map[string]Material{
		"blended": {Transmission: through, Alpha: AlphaTransparent},
		"cutout":  {Transmission: through, Alpha: AlphaCutout},
		"edge-on": {Transmission: through, Alpha: AlphaEdgeOn},
		"unlit":   {Transmission: through, Shading: ShadeUnlit},
	} {
		if m.Validate() == nil {
			t.Errorf("transmission through a %s surface was accepted", name)
		}
	}
}

// A strength left zero means 1 wherever its map or group is in use, the
// normal map's included, and none is said by leaving the map out or by a
// flag: NoOcclusion for the ORM map's occlusion, Off for the PBR highlight.
func TestMaterialStrengthsDefaultToOne(t *testing.T) {
	bumps, orm := testNormalMap(t, color.NRGBA{128, 128, 255, 255}), testORMMap(t, color.NRGBA{255, 255, 255, 255})
	if got := (&Material{Normal: NormalStyle{Map: bumps}}).normalStrength(); got != 1 {
		t.Errorf("a normal map with no strength has strength %v, want 1", got)
	}
	if got := (&Material{Normal: NormalStyle{Map: bumps, Strength: .5}}).normalStrength(); got != .5 {
		t.Errorf("normal strength %v, want .5", got)
	}
	if got := (&Material{Normal: NormalStyle{Strength: 1}}).normalStrength(); got != 0 {
		t.Errorf("normal strength %v with no map, want 0", got)
	}
	pbr := Material{Shading: ShadePBR, PBR: PBRStyle{ORMMap: orm}}
	if got := occlusionStrength(&pbr); got != 1 {
		t.Errorf("occlusion strength %v, want 1", got)
	}
	pbr.PBR.NoOcclusion = true
	if got := occlusionStrength(&pbr); got != 0 {
		t.Errorf("occlusion strength %v with NoOcclusion, want 0", got)
	}
}

// The PBR highlight's strength is a number of its own and Off turns it off:
// nothing is carried in its color's alpha, which is ignored.
func TestPBRSpecularStrengthIsNotCarriedInAlpha(t *testing.T) {
	factor := func(s SpecularStyle) (f0, factor float32) {
		df0, factor, _ := dielectricSpecular(&Material{Shading: ShadePBR, PBR: PBRStyle{Specular: s}}, true)
		return df0[0], factor
	}
	full, one := factor(SpecularStyle{})
	if one != 1 || full < .039 || full > .041 {
		t.Fatalf("the default highlight is F0 %v, factor %v", full, one)
	}
	if f0, k := factor(SpecularStyle{Color: color.RGBA{255, 255, 255, 0}}); k != 1 || f0 < full-1e-3 || f0 > full+1e-3 {
		t.Errorf("a clear color changed the highlight: F0 %v, factor %v", f0, k)
	}
	if f0, k := factor(SpecularStyle{Strength: .5}); k != .5 || f0 >= full || f0 <= 0 {
		t.Errorf("half strength gives F0 %v, factor %v", f0, k)
	}
	if f0, k := factor(SpecularStyle{Off: true}); k != 0 || f0 != 0 {
		t.Errorf("off gives F0 %v, factor %v", f0, k)
	}
	if err := (Material{Shading: ShadePBR, PBR: PBRStyle{Specular: SpecularStyle{Strength: 1.5}}}).Validate(); err == nil {
		t.Error("a specular strength over 1 was accepted")
	}
}

// A Material's groups are named for their fields: field X has type XStyle,
// at every depth, so the type to write is never a guess.
func TestMaterialGroupsAreNamedForTheirField(t *testing.T) {
	groups := 0
	var walk func(reflect.Type)
	walk = func(s reflect.Type) {
		for i := range s.NumField() {
			f := s.Field(i)
			if f.Type.Kind() != reflect.Struct || f.Type.PkgPath() != s.PkgPath() {
				continue // Colors, vectors, and pointers are not groups.
			}
			groups++
			if f.Type.Name() != f.Name+"Style" {
				t.Errorf("%s.%s has type %s, want %sStyle", s.Name(), f.Name, f.Type.Name(), f.Name)
			}
			walk(f.Type)
		}
	}
	walk(reflect.TypeOf(Material{}))
	if groups < 9 {
		t.Fatalf("found %d groups, want at least 9", groups)
	}
}
