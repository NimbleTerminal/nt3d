// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
)

const coreNS = `xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02"`
const production3MF = `xmlns:p="http://schemas.microsoft.com/3dmanufacturing/production/2015/06" requiredextensions="p"`

const rels3MF = `<?xml version="1.0"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
 <Relationship Target="/3D/3dmodel.model" Id="rel-1" Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"/>
 <Relationship Target="/Metadata/thumbnail.png" Id="rel-2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/thumbnail"/>
</Relationships>`

// A tetrahedron with corners at the origin and one unit along each axis,
// wound so that its faces look outward.
const tetrahedron = `<mesh><vertices>
 <vertex x="0" y="0" z="0"/><vertex x="1" y="0" z="0"/><vertex x="0" y="1" z="0"/><vertex x="0" y="0" z="1"/>
</vertices><triangles>
 <triangle v1="0" v2="2" v3="1"/><triangle v1="0" v2="1" v3="3"/><triangle v1="0" v2="3" v3="2"/><triangle v1="1" v2="2" v3="3"/>
</triangles></mesh>`

func model3MF(attrs, body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><model ` + coreNS + ` ` + attrs + `>` + body + `</model>`
}

// archive3MF packs parts into a package, adding relationships naming the
// root model unless the parts supply their own.
func archive3MF(t testing.TB, parts map[string]string) []byte {
	t.Helper()
	if _, ok := parts["_rels/.rels"]; !ok {
		parts["_rels/.rels"] = rels3MF
	}
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, content := range parts {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprint(f, content)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func picture(t testing.TB, w, h int) string {
	t.Helper()
	var b bytes.Buffer
	check(t.(*testing.T), png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))))
	return b.String()
}

func single3MF(t *testing.T) []byte {
	return archive3MF(t, map[string]string{"3D/3dmodel.model": model3MF(`unit="millimeter"`,
		`<resources><object id="1" type="model">`+tetrahedron+`</object></resources><build><item objectid="1"/></build>`)})
}

// closedOutward checks that a mesh's faces enclose positive volume.
func closedOutward(t *testing.T, g nt3d.Geometry) {
	t.Helper()
	volume := float32(0)
	for i := 0; i < len(g.Indices); i += 3 {
		a, b, c := g.Vertices[g.Indices[i]].Position, g.Vertices[g.Indices[i+1]].Position, g.Vertices[g.Indices[i+2]].Position
		volume += a.Dot(b.Cross(c)) / 6
	}
	if volume <= 0 {
		t.Fatalf("volume %g: faces wind inward", volume)
	}
}

func TestDecode3MF(t *testing.T) {
	data := single3MF(t)
	if f, ok := FormatOf("anything", data); !ok || f != ThreeMF {
		t.Fatal("3MF not detected")
	}
	m, err := Decode(data, ThreeMF, nil)
	check(t, err)
	if m.Triangles() != 4 || len(m.Parts) != 1 || m.Parts[0].Name != "Object 1" || m.Unit != "millimeter" || !m.Objects[0].Plain[0] {
		t.Fatalf("%+v", m.Parts)
	}
	closedOutward(t, m.Objects[0].Geometry)
	if b := m.Bounds(); b.Max != (nt3dmath.Vec3{X: 1, Y: 1, Z: 1}) {
		t.Fatal(b)
	}
}

func TestPlacement3MF(t *testing.T) {
	// Object 2 holds object 1 twice: as it is, and moved 10 along x and
	// mirrored in z. The build then lifts the pair by 5.
	m, err := Decode3MF(archive3MF(t, map[string]string{"3D/3dmodel.model": model3MF(`unit="inch"`,
		`<resources><object id="1">`+tetrahedron+`</object>
		 <object id="2"><components>
		   <component objectid="1"/>
		   <component objectid="1" transform="1 0 0 0 1 0 0 0 -1 10 0 0"/>
		 </components></object></resources>
		 <build><item objectid="2" transform="1 0 0 0 1 0 0 0 1 0 0 5"/></build>`)}), nil)
	check(t, err)
	g := m.Objects[0].Geometry
	if m.Triangles() != 8 || m.Unit != "inch" || g.Bounds.Min.X != 0 || g.Bounds.Max.X != 11 || g.Bounds.Min.Z != 4 || g.Bounds.Max.Z != 6 {
		t.Fatalf("triangles=%d bounds=%+v", m.Triangles(), g.Bounds)
	}
	// The mirrored copy's winding is turned back, so it still encloses volume.
	mirrored := g
	mirrored.Indices = g.Indices[12:]
	closedOutward(t, mirrored)
	if n := g.Vertices[12+9].Normal; n.X <= 0 || n.Y <= 0 || n.Z >= 0 {
		t.Fatalf("mirrored normal %+v", n)
	}
}

func TestProduction3MF(t *testing.T) {
	// Slicers keep each object in a part of its own.
	m, err := Decode3MF(archive3MF(t, map[string]string{
		"3D/3dmodel.model": model3MF(production3MF, `<resources><object id="7"><components>
			<component p:path="/3D/Objects/object_1.model" objectid="1" transform="1 0 0 0 1 0 0 0 1 3 0 0"/>
		</components></object></resources><build><item objectid="7"/></build>`),
		"3D/Objects/object_1.model": model3MF(production3MF, `<resources><object id="1">`+tetrahedron+`</object></resources>`),
	}), nil)
	check(t, err)
	if g := m.Objects[0].Geometry; m.Triangles() != 4 || g.Bounds.Min.X != 3 || g.Bounds.Max.X != 4 {
		t.Fatalf("triangles=%d bounds=%+v", m.Triangles(), g.Bounds)
	}
}

func TestColors3MF(t *testing.T) {
	colored := strings.Replace(tetrahedron, `<triangle v1="1" v2="2" v3="3"/>`, `<triangle v1="1" v2="2" v3="3" pid="5" p1="1"/>`, 1)
	m, err := Decode3MF(archive3MF(t, map[string]string{"3D/3dmodel.model": model3MF(`xmlns:m="http://schemas.microsoft.com/3dmanufacturing/material/2015/02"`,
		`<resources>
		 <basematerials id="5"><base name="red" displaycolor="#FF0000"/><base name="green" displaycolor="#00FF0080"/></basematerials>
		 <m:colorgroup id="6"><m:color color="#0000FFFF"/></m:colorgroup>
		 <object id="1" pid="6" pindex="0">`+colored+`</object>
		 <object id="2">`+tetrahedron+`</object>
		 </resources><build><item objectid="1"/><item objectid="2"/></build>`)}), &Options{Color: color.RGBA{1, 2, 3, 255}})
	check(t, err)
	first, second := m.Objects[0], m.Objects[1]
	if first.Geometry.Vertices[0].Color != (color.RGBA{B: 255, A: 255}) || first.Geometry.Vertices[9].Color != (color.RGBA{G: 255, A: 255}) || first.Plain[0] {
		t.Fatal(first.Geometry.Vertices[0].Color, first.Geometry.Vertices[9].Color)
	}
	if second.Geometry.Vertices[0].Color != (color.RGBA{1, 2, 3, 255}) || !second.Plain[0] || second.Part != 1 {
		t.Fatal(second.Geometry.Vertices[0].Color)
	}
}

func TestMetadata3MF(t *testing.T) {
	m, err := Decode3MF(archive3MF(t, map[string]string{
		"Metadata/thumbnail.png": picture(t, 8, 6),
		"3D/3dmodel.model": model3MF(`unit="centimeter"`, `
		 <metadata name="Title">Bracket</metadata><metadata name="Designer">R. Maker</metadata>
		 <resources><object id="1" name="body">`+tetrahedron+`</object></resources><build><item objectid="1"/></build>`)}), nil)
	check(t, err)
	if m.Metadata["Title"] != "Bracket" || m.Metadata["Designer"] != "R. Maker" || m.Unit != "centimeter" || m.Parts[0].Name != "body" {
		t.Fatalf("%+v %+v", m.Metadata, m.Parts)
	}
	if m.Thumbnail == nil || m.Thumbnail.Bounds().Dx() != 8 {
		t.Fatalf("thumbnail %v", m.Thumbnail)
	}
}

// many3MF has one object of n triangles, each a copy of the first.
func many3MF(t *testing.T, n int, parts map[string]string) []byte {
	var b strings.Builder
	b.WriteString(`<resources><object id="1"><mesh><vertices><vertex x="0" y="0" z="0"/><vertex x="2" y="0" z="0"/><vertex x="0" y="3" z="0"/></vertices><triangles>`)
	for range n {
		b.WriteString(`<triangle v1="0" v2="1" v3="2"/>`)
	}
	b.WriteString(`</triangles></mesh></object></resources><build><item objectid="1"/></build>`)
	parts["3D/3dmodel.model"] = model3MF("", b.String())
	return archive3MF(t, parts)
}

func TestLarge3MFReturnsWhatItLearned(t *testing.T) {
	data := many3MF(t, 1001, map[string]string{"Metadata/thumbnail.png": picture(t, 8, 6)})
	m, err := Decode3MF(data, &Options{MaxTriangles: 1000})
	if !errors.Is(err, ErrTooManyTriangles) || m == nil || m.Thumbnail == nil || len(m.Parts) != 1 || m.Parts[0].Triangles != 1001 || len(m.Objects) != 0 {
		t.Fatalf("err=%v model=%+v", err, m)
	}
	if m, err = Decode3MF(data, &Options{MaxTriangles: 1001}); err != nil || m.Triangles() != 1001 {
		t.Fatalf("at the limit: %v", err)
	}
	// Too large to unpack: the thumbnail, read first, is still there.
	m, err = Decode3MF(data, &Options{MaxBytes: 8 << 10})
	if !errors.Is(err, ErrTooLarge) || m == nil || m.Thumbnail == nil {
		t.Fatalf("err=%v model=%v", err, m)
	}
}

func TestExpanding3MFIsRefused(t *testing.T) {
	// A small archive that unpacks to more than the limit.
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	f, _ := w.Create("3D/3dmodel.model")
	fmt.Fprint(f, `<?xml version="1.0"?><model `+coreNS+`><resources><object id="1"><mesh><vertices>`)
	padding := bytes.Repeat([]byte(" "), 1<<20)
	for range 5 {
		f.Write(padding)
	}
	w.Close()
	if b.Len() > 64<<10 {
		t.Fatalf("the fixture is %d bytes; it should be small", b.Len())
	}
	if _, err := Decode3MF(b.Bytes(), &Options{MaxBytes: 4 << 20}); !errors.Is(err, ErrTooLarge) || !strings.Contains(err.Error(), "4 MiB") {
		t.Fatalf("err = %v", err)
	}
}

func TestDamaged3MF(t *testing.T) {
	object := func(body string) string {
		return model3MF("", `<resources>`+body+`</resources><build><item objectid="1"/></build>`)
	}
	var crowded bytes.Buffer
	w := zip.NewWriter(&crowded)
	for i := range 5000 {
		f, _ := w.Create(fmt.Sprintf("3D/filler-%d.model", i))
		fmt.Fprint(f, "x")
	}
	w.Close()
	// A second end record, appended, that claims a few entries of the same
	// directory: the zip reader trusts it and counts only modulo 65536.
	disguised := bytes.Clone(crowded.Bytes())
	end := disguised[len(disguised)-zipEndSize:]
	disguised = append(disguised, end...)
	binary.LittleEndian.PutUint16(disguised[len(disguised)-12:], 10)
	for name, tt := range map[string]struct {
		data []byte
		want string
	}{
		"not an archive":      {[]byte("PK\x03\x04 but not really"), "zip"},
		"no model":            {archive3MF(t, map[string]string{"3D/readme.txt": "hello"}), "no model"},
		"empty":               {archive3MF(t, map[string]string{"3D/3dmodel.model": object(`<object id="1"><mesh><vertices/><triangles/></mesh></object>`)}), "no triangles"},
		"malformed":           {archive3MF(t, map[string]string{"3D/3dmodel.model": `<model ` + coreNS + `><resources><object id="1"><mesh>`}), "3MF"},
		"vertex out of range": {archive3MF(t, map[string]string{"3D/3dmodel.model": object(`<object id="1"><mesh><vertices><vertex x="0" y="0" z="0"/></vertices><triangles><triangle v1="0" v2="1" v3="2"/></triangles></mesh></object>`)}), "vertex"},
		"not a number":        {archive3MF(t, map[string]string{"3D/3dmodel.model": object(`<object id="1"><mesh><vertices><vertex x="wide" y="0" z="0"/></vertices><triangles/></mesh></object>`)}), "vertex"},
		"infinite":            {archive3MF(t, map[string]string{"3D/3dmodel.model": object(`<object id="1"><mesh><vertices><vertex x="1e999" y="0" z="0"/><vertex x="1" y="0" z="0"/><vertex x="0" y="1" z="0"/></vertices><triangles><triangle v1="0" v2="1" v3="2"/></triangles></mesh></object>`)}), "vertex"},
		"huge placement":      {archive3MF(t, map[string]string{"3D/3dmodel.model": model3MF("", `<resources><object id="1">`+tetrahedron+`</object></resources><build><item objectid="1" transform="1e30 0 0 0 1 0 0 0 1 0 0 0"/></build>`)}), "3MF vertex is non-finite or exceeds"},
		"missing object":      {archive3MF(t, map[string]string{"3D/3dmodel.model": model3MF("", `<resources/><build><item objectid="9"/></build>`)}), "object 9"},
		"an object in itself": {archive3MF(t, map[string]string{"3D/3dmodel.model": object(`<object id="1"><components><component objectid="1"/></components></object>`)}), "nest"},
		"two in each other":   {archive3MF(t, map[string]string{"3D/3dmodel.model": object(`<object id="1"><components><component objectid="2"/></components></object><object id="2"><components><component objectid="1"/></components></object>`)}), "nest"},
		"bad transform":       {archive3MF(t, map[string]string{"3D/3dmodel.model": model3MF("", `<resources><object id="1">`+tetrahedron+`</object></resources><build><item objectid="1" transform="1 0 0"/></build>`)}), "transform"},
		"missing part":        {archive3MF(t, map[string]string{"3D/3dmodel.model": model3MF(production3MF, `<resources/><build><item objectid="1" p:path="/3D/gone.model"/></build>`)}), "gone.model"},
		"too many entries":    {crowded.Bytes(), "entries"},
		"two end records":     {disguised, "end records"},
		"bad tex2coord": {archive3MF(t, map[string]string{"3D/3dmodel.model": model3MF(`xmlns:m="http://schemas.microsoft.com/3dmanufacturing/material/2015/02"`, `<resources>
			<m:texture2dgroup id="7" texid="6"><m:tex2coord u="0" v="0"/><m:tex2coord u="wide" v="0"/><m:tex2coord u="0" v="1"/></m:texture2dgroup>
			<object id="1">`+tetrahedron+`</object></resources><build><item objectid="1"/></build>`)}), "tex2coord"},
	} {
		if _, err := Decode3MF(tt.data, nil); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v, want an error naming %q", name, err, tt.want)
		}
	}
}

// slicerProject is a project as Bambu Studio and its relatives save it: one
// object of two parts, each in the filament its settings give it.
func slicerProject(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	all := map[string]string{
		"3D/3dmodel.model": model3MF(production3MF, `<resources><object id="2" type="model"><components>
				<component p:path="/3D/Objects/object_1.model" objectid="1"/>
				<component p:path="/3D/Objects/object_1.model" objectid="3" transform="1 0 0 0 1 0 0 0 1 5 0 0"/>
			</components></object></resources><build><item objectid="2"/></build>`),
		"3D/Objects/object_1.model": model3MF(production3MF, `<resources><object id="1">`+tetrahedron+`</object><object id="3">`+tetrahedron+`</object></resources>`),
		"Metadata/model_settings.config": `<?xml version="1.0"?><config><object id="2">
			<metadata key="name" value="toy.stl"/><metadata key="extruder" value="2"/>
			<part id="1" subtype="normal_part"><metadata key="name" value="body"/></part>
			<part id="3" subtype="normal_part"><metadata key="extruder" value="1"/></part>
		</object></config>`,
		"Metadata/project_settings.config": `{"filament_colour": ["#FF0000", "#5E43B7"], "filament_type": ["PLA", "PLA"]}`,
		"Metadata/thumbnail.png":           picture(t, 3, 5),
		"Metadata/plate_1.png":             picture(t, 8, 6),
	}
	for name, content := range parts {
		all[name] = content
	}
	return archive3MF(t, all)
}

func TestSlicerProject3MF(t *testing.T) {
	m, err := Decode3MF(slicerProject(t, nil), nil)
	check(t, err)
	g := m.Objects[0].Geometry
	purple, red := color.RGBA{R: 0x5e, G: 0x43, B: 0xb7, A: 255}, color.RGBA{R: 255, A: 255}
	// The first part takes the object's filament; the second has its own.
	if first, second := g.Vertices[0].Color, g.Vertices[12].Color; first != purple || second != red || m.Objects[0].Plain[0] {
		t.Errorf("parts are %v and %v, want %v and %v", first, second, purple, red)
	}
	if m.Parts[0].Name != "toy.stl" || m.Triangles() != 8 {
		t.Errorf("parts %+v", m.Parts)
	}
	// The slicer's rendering of the plate is preferred to the declared picture.
	if m.Thumbnail == nil || m.Thumbnail.Bounds().Dx() != 8 {
		t.Errorf("thumbnail %v", m.Thumbnail)
	}
	// Colors the model itself declares are the standard, and win.
	m, err = Decode3MF(slicerProject(t, map[string]string{
		"3D/Objects/object_1.model": model3MF(production3MF, `<resources><basematerials id="9"><base name="green" displaycolor="#00FF00"/></basematerials>
			<object id="1" pid="9" pindex="0">`+tetrahedron+`</object><object id="3">`+tetrahedron+`</object></resources>`),
	}), nil)
	check(t, err)
	if g := m.Objects[0].Geometry; g.Vertices[0].Color != (color.RGBA{G: 255, A: 255}) || g.Vertices[12].Color != red {
		t.Errorf("parts are %v and %v", g.Vertices[0].Color, g.Vertices[12].Color)
	}
}

func TestDamagedSlicerSettingsAreIgnored(t *testing.T) {
	for name, parts := range map[string]map[string]string{
		"settings are not XML":     {"Metadata/model_settings.config": "<config><object id="},
		"project is not JSON":      {"Metadata/project_settings.config": "{not json"},
		"colors are not colors":    {"Metadata/project_settings.config": `{"filament_colour": ["mauve", 7]}`},
		"colors are not a list":    {"Metadata/project_settings.config": `{"filament_colour": "#FF0000"}`},
		"no such filament":         {"Metadata/model_settings.config": `<config><object id="2"><metadata key="extruder" value="9"/></object></config>`},
		"filament is not a number": {"Metadata/model_settings.config": `<config><object id="2"><metadata key="extruder" value="-1"/><part id="1"><metadata key="extruder" value="x"/></part></object></config>`},
		"plate is not a picture":   {"Metadata/plate_1.png": "not a png"},
	} {
		m, err := Decode3MF(slicerProject(t, parts), nil)
		if err != nil || m.Triangles() != 8 || m.Thumbnail == nil {
			t.Errorf("%s: %v %+v", name, err, m)
			continue
		}
		if name == "plate is not a picture" && m.Thumbnail.Bounds().Dx() != 3 {
			t.Errorf("%s: the declared thumbnail was not used instead: %v", name, m.Thumbnail.Bounds())
		}
	}
}

// An assembly of three placed parts: two named objects and one that only a
// slicer's settings name.
func assembly3MF(t *testing.T) []byte {
	return archive3MF(t, map[string]string{
		"3D/3dmodel.model": model3MF(`unit="millimeter"`, `<resources>
			 <object id="1" name="Base">`+tetrahedron+`</object>
			 <object id="2" name="Lid">`+tetrahedron+`</object>
			 <object id="3">`+tetrahedron+`</object>
			 </resources><build>
			 <item objectid="1"/>
			 <item objectid="2" transform="1 0 0 0 1 0 0 0 1 10 0 0"/>
			 <item objectid="3" transform="1 0 0 0 1 0 0 0 1 20 0 0"/>
			 </build>`),
		"Metadata/model_settings.config": `<?xml version="1.0"?><config><object id="3"><metadata key="name" value="Hinge"/></object></config>`,
	})
}

func TestPartsOf3MF(t *testing.T) {
	data := assembly3MF(t)
	m, err := Decode3MF(data, nil)
	check(t, err)
	if len(m.Parts) != 3 || m.Parts[0].Name != "Base" || m.Parts[1].Name != "Lid" || m.Parts[2].Name != "Hinge" || m.Parts[2].Triangles != 4 || len(m.Objects) != 3 {
		t.Fatalf("parts: %+v", m.Parts)
	}
	// Choosing a part assembles it alone, where it is placed.
	m, err = Decode3MF(data, &Options{Parts: []bool{false, true}})
	check(t, err)
	if len(m.Objects) != 1 || m.Objects[0].Name != "Lid" || m.Objects[0].Part != 1 || m.Bounds().Min.X != 10 || len(m.Parts) != 3 {
		t.Fatalf("lid: %+v %v", m.Objects, m.Bounds())
	}
	// The limit applies to what is chosen.
	if _, err = Decode3MF(data, &Options{Parts: []bool{true}, MaxTriangles: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err = Decode3MF(data, &Options{Parts: []bool{false, false, false}}); err == nil {
		t.Fatal("assembled nothing")
	}
}

func Test3MFRoundTrip(t *testing.T) {
	cone, err := nt3d.BuildCylinder(nt3d.Cylinder{BottomRadius: 1, Height: 2, Color: color.RGBA{200, 40, 40, 255}})
	check(t, err)
	for i := range cone.Vertices[:len(cone.Vertices)/2] {
		cone.Vertices[i].Color = color.RGBA{10, 20, 30, 255}
	}
	var buf bytes.Buffer
	check(t, Encode3MF(&buf, cone))
	m, err := Decode(buf.Bytes(), ThreeMF, nil)
	check(t, err)
	g := m.Objects[0].Geometry
	if len(g.Indices) != len(cone.Indices) || m.Metadata["Application"] != "nt3d" {
		t.Fatal(len(g.Indices))
	}
	closedOutward(t, g)
	for i, k := range cone.Indices {
		if g.Vertices[i].Position != cone.Vertices[k].Position {
			t.Fatal("position", i)
		}
		if i%3 == 0 && g.Vertices[i].Color != cone.Vertices[k].Color {
			t.Fatal("color", i, g.Vertices[i].Color, cone.Vertices[k].Color)
		}
	}
}

func TestCornerColors3MF(t *testing.T) {
	// One triangle with a color at each corner, placed as it is and mirrored in x.
	m, err := Decode3MF(archive3MF(t, map[string]string{"3D/3dmodel.model": model3MF(`xmlns:m="http://schemas.microsoft.com/3dmanufacturing/material/2015/02"`,
		`<resources><m:colorgroup id="5"><m:color color="#FF0000"/><m:color color="#00FF00"/><m:color color="#0000FF"/></m:colorgroup>
		 <object id="1"><mesh><vertices><vertex x="0" y="0" z="0"/><vertex x="1" y="0" z="0"/><vertex x="0" y="1" z="0"/></vertices>
		 <triangles><triangle v1="0" v2="1" v3="2" pid="5" p1="0" p2="1" p3="2"/></triangles></mesh></object></resources>
		 <build><item objectid="1"/><item objectid="1" transform="-1 0 0 0 1 0 0 0 1 0 0 0"/></build>`)}), nil)
	check(t, err)
	want := map[nt3dmath.Vec3]color.RGBA{{}: {R: 255, A: 255}, {X: 1}: {G: 255, A: 255}, {X: -1}: {G: 255, A: 255}, {Y: 1}: {B: 255, A: 255}}
	for _, o := range m.Objects {
		for _, v := range o.Geometry.Vertices {
			if v.Color != want[v.Position] {
				t.Fatalf("corner %v is %v", v.Position, v.Color)
			}
		}
	}
	// The mirrored copy still faces up.
	if n := m.Objects[1].Geometry.Vertices[0].Normal; n.Z != 1 {
		t.Fatalf("mirrored normal %v", n)
	}
}

func TestPaintState(t *testing.T) {
	for code, want := range map[string]int{
		"":      0,
		"4":     1,
		"8":     2,
		"0C":    3, // 3 plus the next digit.
		"5C":    8, // 3 plus 5.
		"04883": 2, // Split in four: two parts filament 2, one 1, one unpainted.
		"841":   1, // Split in two, a tie: the lower filament.
		"3":     0, // Truncated.
		"4x":    1, // Not hex: skipped.
	} {
		if got := paintState(code); got != want {
			t.Errorf("paintState(%q) = %d, want %d", code, got, want)
		}
	}
}

func TestPainted3MF(t *testing.T) {
	// The slicer project's two parts, each with one face painted: the first
	// red (filament 1) as Bambu Studio writes it, the second purple
	// (filament 2) as PrusaSlicer does.
	painted := func(attribute string) string {
		return strings.Replace(tetrahedron, `<triangle v1="1" v2="2" v3="3"/>`, `<triangle v1="1" v2="2" v3="3" `+attribute+`/>`, 1)
	}
	m, err := Decode3MF(slicerProject(t, map[string]string{
		"3D/Objects/object_1.model": model3MF(production3MF+` xmlns:slic3rpe="http://schemas.slic3r.org/3mf/2017/06"`, `<resources>
			<object id="1">`+painted(`paint_color="4"`)+`</object><object id="3">`+painted(`slic3rpe:mmu_segmentation="8"`)+`</object></resources>`),
	}), nil)
	check(t, err)
	g := m.Objects[0].Geometry
	purple, red := color.RGBA{R: 0x5e, G: 0x43, B: 0xb7, A: 255}, color.RGBA{R: 255, A: 255}
	// Unpainted faces keep their part's filament; painted ones take theirs.
	for vertex, want := range map[int]color.RGBA{0: purple, 9: red, 12: red, 21: purple} {
		if got := g.Vertices[vertex].Color; got != want {
			t.Errorf("vertex %d is %v, want %v", vertex, got, want)
		}
	}
}

func TestTextured3MF(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
	var texture bytes.Buffer
	check(t, png.Encode(&texture, img))
	textured := strings.Replace(tetrahedron, `<triangle v1="1" v2="2" v3="3"/>`, `<triangle v1="1" v2="2" v3="3" pid="7" p1="0" p2="1" p3="2"/>`, 1)
	model := model3MF(`xmlns:m="http://schemas.microsoft.com/3dmanufacturing/material/2015/02"`, `<resources>
		<m:texture2d id="6" path="/3D/Textures/wood.png" contenttype="image/png"/>
		<m:texture2dgroup id="7" texid="6"><m:tex2coord u="0" v="0"/><m:tex2coord u="1" v="0"/><m:tex2coord u="0" v="1"/></m:texture2dgroup>
		<object id="1">`+textured+`</object></resources><build><item objectid="1"/></build>`)
	m, err := Decode3MF(archive3MF(t, map[string]string{"3D/3dmodel.model": model, "3D/Textures/wood.png": texture.String()}), nil)
	check(t, err)
	if len(m.Objects) != 2 || len(m.Warnings) != 0 {
		t.Fatalf("%d objects, warnings %v", len(m.Objects), m.Warnings)
	}
	plain, wood := m.Objects[0].Geometry, m.Objects[1].Geometry
	if plain.Material != nil || len(plain.Indices) != 9 || wood.Material == nil || wood.Material.Texture == nil || m.Objects[1].Material != "/3D/Textures/wood.png" {
		t.Fatal("faces not gathered by texture")
	}
	// v counts up in 3MF and down in nt3d.
	if len(wood.UV) != 3 || wood.UV[0] != (nt3d.UV{U: 0, V: 1}) || wood.UV[2] != (nt3d.UV{U: 0, V: 0}) || m.Objects[1].Plain[0] {
		t.Fatalf("UV %v", wood.UV)
	}
	closedOutward(t, func() nt3d.Geometry {
		g, err := nt3d.MergeGeometry(plain, nt3d.Geometry{Vertices: wood.Vertices, Indices: wood.Indices})
		check(t, err)
		return g
	}())
	// A texture the package lacks is a warning; its faces keep their colors.
	m, err = Decode3MF(archive3MF(t, map[string]string{"3D/3dmodel.model": model}), nil)
	check(t, err)
	if len(m.Objects) != 1 || len(m.Warnings) != 1 || !strings.Contains(m.Warnings[0], "wood.png") || m.Triangles() != 4 {
		t.Fatalf("%d objects, warnings %v", len(m.Objects), m.Warnings)
	}
}

// Decoders tile textures as their formats do: OBJ and PLY by default, 3MF
// unless both tile styles clamp.
func TestTextureTiling(t *testing.T) {
	files := cubeFiles(t)
	m, err := DecodeOBJ([]byte(cubeOBJ), &Options{Open: opener(files)})
	check(t, err)
	if !m.Objects[0].Geometry.Material.Repeat {
		t.Fatal("OBJ texture does not repeat")
	}
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	var texture bytes.Buffer
	check(t, png.Encode(&texture, img))
	textured := strings.Replace(tetrahedron, `<triangle v1="1" v2="2" v3="3"/>`, `<triangle v1="1" v2="2" v3="3" pid="7" p1="0" p2="1" p3="2"/>`, 1)
	for style, want := range map[string]bool{``: true, `tilestyleu="clamp" tilestylev="clamp"`: false, `tilestyleu="mirror" tilestylev="clamp"`: true} {
		model := model3MF(`xmlns:m="http://schemas.microsoft.com/3dmanufacturing/material/2015/02"`, `<resources>
			<m:texture2d id="6" path="/3D/Textures/t.png" contenttype="image/png" `+style+`/>
			<m:texture2dgroup id="7" texid="6"><m:tex2coord u="0" v="0"/><m:tex2coord u="2" v="0"/><m:tex2coord u="0" v="2"/></m:texture2dgroup>
			<object id="1">`+textured+`</object></resources><build><item objectid="1"/></build>`)
		m, err := Decode3MF(archive3MF(t, map[string]string{"3D/3dmodel.model": model, "3D/Textures/t.png": texture.String()}), nil)
		check(t, err)
		if got := m.Objects[1].Geometry.Material.Repeat; got != want {
			t.Errorf("tile style %q: repeat %v, want %v", style, got, want)
		}
	}
}

// tetraArea is the tetrahedron's surface: three right triangles and the
// slanted face.
var tetraArea = 1.5 + math.Sqrt(3)/2

func near3(a, b nt3dmath.Vec3) bool {
	d := a.Sub(b)
	return d.Dot(d) < 1e-9
}

// The summary measures every part where the build places it, through
// components and scales, even past the triangle limit, without assembling.
func TestSummary3MF(t *testing.T) {
	// Part 0: object 2 holds the tetrahedron as it is and at twice its size,
	// moved 20 along x; the build lifts it 5. Part 1: the tetrahedron
	// itself, moved -10 along y. Object 2 only assembles.
	data := archive3MF(t, map[string]string{"3D/3dmodel.model": model3MF(`unit="millimeter"`,
		`<resources><object id="1">`+tetrahedron+`</object>
		 <object id="2"><components>
		   <component objectid="1"/>
		   <component objectid="1" transform="2 0 0 0 2 0 0 0 2 20 0 0"/>
		 </components></object></resources>
		 <build><item objectid="2" transform="1 0 0 0 1 0 0 0 1 0 0 5"/>
		  <item objectid="1" transform="1 0 0 0 1 0 0 0 1 0 -10 0"/></build>`)})
	for _, o := range []*Options{nil, {MaxTriangles: 4}, {Parts: []bool{false, true}}} {
		m, err := Decode3MF(data, o)
		if o != nil && o.MaxTriangles == 4 {
			if !errors.Is(err, ErrTooManyTriangles) || len(m.Objects) != 0 {
				t.Fatalf("over the limit: %v", err)
			}
		} else {
			check(t, err)
		}
		s := m.Summary
		if s == nil || s.Triangles != 12 || s.Meshes != 1 || math.Abs(s.Area-6*tetraArea) > 1e-9 {
			t.Fatalf("options %+v: summary %+v", o, s)
		}
		if !near3(s.Bounds.Min, nt3dmath.Vec3{X: 0, Y: -10, Z: 0}) || !near3(s.Bounds.Max, nt3dmath.Vec3{X: 22, Y: 2, Z: 7}) {
			t.Fatalf("bounds %v", s.Bounds)
		}
		p := m.Parts
		if len(p) != 2 || math.Abs(p[0].Area-5*tetraArea) > 1e-9 || math.Abs(p[1].Area-tetraArea) > 1e-9 ||
			!near3(p[0].Bounds.Min, nt3dmath.Vec3{Z: 5}) || !near3(p[0].Bounds.Max, nt3dmath.Vec3{X: 22, Y: 2, Z: 7}) {
			t.Fatalf("parts %+v", p)
		}
	}
	// Turned Y-up, as the objects are.
	m, err := Decode3MF(data, &Options{Up: YUp})
	check(t, err)
	if b := m.Summary.Bounds; !near3(b.Min, nt3dmath.Vec3{X: 0, Y: -7, Z: -10}) || !near3(b.Max, nt3dmath.Vec3{X: 22, Y: 0, Z: 2}) || !near3(m.Bounds().Max, b.Max) {
		t.Fatalf("Y-up bounds %v, objects %v", b, m.Bounds())
	}
	// A mirrored placement measures its area outward, the same.
	m, err = Decode3MF(archive3MF(t, map[string]string{"3D/3dmodel.model": model3MF(``,
		`<resources><object id="1">`+tetrahedron+`</object></resources>
		 <build><item objectid="1" transform="1 0 0 0 1 0 0 0 -1 0 0 0"/></build>`)}), nil)
	check(t, err)
	if math.Abs(m.Summary.Area-tetraArea) > 1e-9 || m.Summary.Bounds.Min.Z != -1 {
		t.Fatalf("mirrored: %+v", m.Summary)
	}
}

// A non-finite vertex is refused even in a part that is not assembled,
// whose summary would otherwise carry NaN into the build's.
func TestNaN3MFVertex(t *testing.T) {
	data := archive3MF(t, map[string]string{"3D/3dmodel.model": model3MF("", `<resources>
		<object id="1"><mesh><vertices><vertex x="0" y="0" z="NaN"/><vertex x="1" y="0" z="0"/><vertex x="0" y="1" z="0"/></vertices>
		 <triangles><triangle v1="0" v2="1" v3="2"/></triangles></mesh></object>
		<object id="2"><mesh><vertices><vertex x="0" y="0" z="0"/><vertex x="1" y="0" z="0"/><vertex x="0" y="1" z="0"/></vertices>
		 <triangles><triangle v1="0" v2="1" v3="2"/></triangles></mesh></object>
		</resources><build><item objectid="1"/><item objectid="2"/></build>`)})
	m, err := Decode3MF(data, &Options{Parts: []bool{false, true}})
	if err == nil || !strings.Contains(err.Error(), "vertex") {
		t.Fatalf("decoded with summary %+v, error %v", m.Summary, err)
	}
}

// A model over a low limit still summarizes all of it.
func TestLarge3MFSummary(t *testing.T) {
	m, err := Decode3MF(many3MF(t, 1001, map[string]string{}), &Options{MaxTriangles: 10})
	if !errors.Is(err, ErrTooManyTriangles) || m.Summary == nil || m.Summary.Triangles != 1001 || m.Summary.Meshes != 1 || !m.Summary.Bounds.IsValid() || m.Summary.Area <= 0 {
		t.Fatalf("err %v, summary %+v", err, m.Summary)
	}
}

// One end record that understates the entries is no way past the cap: the
// zip reader checks its count only modulo 65536, so the files it read are
// counted too.
func TestUnderstated3MFEntries(t *testing.T) {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for i := range 65536 + 10 {
		if _, err := w.CreateHeader(&zip.FileHeader{Name: fmt.Sprint(i), Method: zip.Store}); err != nil {
			t.Fatal(err)
		}
	}
	check(t, w.Close())
	// So many entries make a zip64 archive: its directory's size and
	// offset are in the zip64 end record. Keep the directory, and end it
	// with a plain record that claims 10 entries, 65546 modulo 65536.
	data := b.Bytes()
	end64 := bytes.LastIndex(data, []byte("PK\x06\x06"))
	size := binary.LittleEndian.Uint64(data[end64+40:])
	offset := binary.LittleEndian.Uint64(data[end64+48:])
	lying := bytes.Clone(data[:offset+size])
	record := make([]byte, zipEndSize)
	copy(record, "PK\x05\x06")
	binary.LittleEndian.PutUint16(record[8:], 10)
	binary.LittleEndian.PutUint16(record[10:], 10)
	binary.LittleEndian.PutUint32(record[12:], uint32(size))
	binary.LittleEndian.PutUint32(record[16:], uint32(offset))
	lying = append(lying, record...)
	if n, err := zipEntries(lying); n != 10 || err != nil {
		t.Fatalf("fixture's end record claims %d entries: %v", n, err)
	}
	if _, err := Decode3MF(lying, nil); err == nil || !strings.Contains(err.Error(), "entries") {
		t.Fatalf("%v, want an error naming the entries", err)
	}
}
