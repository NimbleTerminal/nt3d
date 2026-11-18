package nt3d

import (
	"reflect"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// Clone gives a geometry that can be edited without touching the original:
// every slice is copied, and so are the material and billboard. This walks
// Geometry's fields, so a field added later must be cloned, or named here as
// shared on purpose.
func TestGeometryCloneCopiesEveryField(t *testing.T) {
	// Pointer fields whose targets never change once made, and are shared.
	shared := map[string]bool{"Displacement": true, "Clusters": true, "GaussianSHPalette": true}

	var g Geometry
	v := reflect.ValueOf(&g).Elem()
	for i := range v.NumField() {
		f := v.Field(i)
		switch f.Kind() {
		case reflect.Slice:
			f.Set(reflect.MakeSlice(f.Type(), 2, 2))
		case reflect.Pointer:
			f.Set(reflect.New(f.Type().Elem()))
		}
	}
	c := g.Clone()
	cv := reflect.ValueOf(&c).Elem()
	for i := range v.NumField() {
		name, f, cf := v.Type().Field(i).Name, v.Field(i), cv.Field(i)
		switch f.Kind() {
		case reflect.Slice:
			if cf.Len() != f.Len() {
				t.Errorf("%s: the clone has %d elements, the original %d", name, cf.Len(), f.Len())
			} else if cf.Pointer() == f.Pointer() {
				t.Errorf("%s: the clone shares the original's array", name)
			}
		case reflect.Pointer:
			if same := cf.Pointer() == f.Pointer(); same != shared[name] {
				t.Errorf("%s: shared with the original is %v, want %v", name, same, shared[name])
			}
		}
	}
}

// Editing a clone leaves the original as it was, and a clone of nothing is nothing.
func TestGeometryCloneIsIndependent(t *testing.T) {
	g, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 1, Y: 1, Z: 1}, Material: &Material{Shading: ShadePBR, PBR: PBRStyle{Roughness: .5}}})
	check(t, err)
	c := g.Clone()
	c.Vertices[0].Position.X += 10
	c.Indices[0] = 7
	c.Material.PBR.Roughness = 1
	if g.Vertices[0].Position.X == c.Vertices[0].Position.X || g.Indices[0] == 7 || g.Material.PBR.Roughness != .5 {
		t.Fatal("editing the clone changed the original")
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("a clone of a valid geometry: %v", err)
	}
	empty := Geometry{}.Clone()
	if empty.Vertices != nil || empty.Material != nil {
		t.Fatalf("a clone of the empty geometry holds %v, %v", empty.Vertices, empty.Material)
	}
}
