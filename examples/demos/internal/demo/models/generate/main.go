// Copyright (c) 2026 Neomantra Corp.

// Command generate writes the sample models that the models demo embeds. They
// are made here from nt3d's shape builders, so the repository holds no
// third-party models, and each exercises what its format's decoder reads:
//
//   - knot.stl: binary STL with VisCAM/SolidView face colors.
//   - star.obj, star.mtl, wood.png: a Y-up OBJ whose concave caps need ear
//     clipping, with a textured material and a colored one.
//   - robot.3mf: a slicer-style 3MF package: objects in a production part,
//     a mirrored component, per-corner color gradients, painted faces with
//     filament colors, a textured screen, base materials, and a thumbnail
//     nt3d renders.
//
// Run it with go generate in the models package.
package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"strconv"
	"strings"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
)

func main() {
	for name, write := range map[string]func() error{"knot": knot, "star": star, "robot": robot} {
		if err := write(); err != nil {
			log.Fatalf("%s: %v", name, err)
		}
	}
}

func hue(h float64) color.RGBA {
	channel := func(shift float64) uint8 {
		return uint8(255 * (.5 + .45*math.Cos(2*math.Pi*(h+shift))))
	}
	return color.RGBA{channel(0), channel(-1. / 3), channel(1. / 3), 255}
}

// signedVolume is positive for a closed mesh whose faces wind outward.
func signedVolume(g nt3d.Geometry) float64 {
	v := 0.0
	for i := 0; i+2 < len(g.Indices); i += 3 {
		a, b, c := g.Vertices[g.Indices[i]].Position, g.Vertices[g.Indices[i+1]].Position, g.Vertices[g.Indices[i+2]].Position
		v += float64(a.Dot(b.Cross(c))) / 6
	}
	return v
}

// knot writes a trefoil knot tube as binary STL, colored along its length.
func knot() error {
	const segments, sides, radius = 240, 18, .38
	curve := func(t float64) nt3dmath.Vec3 {
		return nt3dmath.Vec3{X: float32(math.Sin(t) + 2*math.Sin(2*t)), Y: float32(math.Cos(t) - 2*math.Cos(2*t)), Z: float32(-math.Sin(3 * t))}
	}
	var g nt3d.Geometry
	for i := range segments {
		t := 2 * math.Pi * float64(i) / segments
		center := curve(t)
		tangent := curve(t + 1e-3).Sub(curve(t - 1e-3)).Normalize()
		// The knot is never vertical, so up gives a frame that closes on itself.
		normal := tangent.Cross(nt3dmath.Vec3{Z: 1}).Normalize()
		binormal := tangent.Cross(normal)
		for j := range sides {
			a := 2 * math.Pi * float64(j) / sides
			out := normal.Scale(float32(math.Cos(a))).Add(binormal.Scale(float32(math.Sin(a))))
			g.Vertices = append(g.Vertices, nt3d.Vertex{Position: center.Add(out.Scale(radius)), Normal: out, Color: hue(float64(i) / segments)})
		}
	}
	at := func(i, j int) uint32 { return uint32((i%segments)*sides + j%sides) }
	for i := range segments {
		for j := range sides {
			a, b, c, d := at(i, j), at(i, j+1), at(i+1, j), at(i+1, j+1)
			g.Indices = append(g.Indices, a, b, c, b, d, c)
		}
	}
	if signedVolume(g) < 0 {
		for i := 0; i < len(g.Indices); i += 3 {
			g.Indices[i+1], g.Indices[i+2] = g.Indices[i+2], g.Indices[i+1]
		}
	}
	g.Bounds = nt3d.GeometryBounds(g)
	var b bytes.Buffer
	if err := nt3dmesh.EncodeSTL(&b, g); err != nil {
		return err
	}
	return os.WriteFile("knot.stl", b.Bytes(), 0o644)
}

// star writes an extruded five-pointed star as Y-up OBJ: concave caps in an
// enamel color, and wooden sides.
func star() error {
	const points, height = 5, .5
	var corners [][2]float64
	for i := range 2 * points {
		r := 1.0
		if i%2 == 1 {
			r = .42
		}
		a := math.Pi/2 + math.Pi*float64(i)/points
		corners = append(corners, [2]float64{r * math.Cos(a), r * math.Sin(a)})
	}
	n := len(corners)
	var o strings.Builder
	number := func(v float64) string { return strconv.FormatFloat(v, 'g', 6, 64) }
	fmt.Fprintln(&o, "# A five-pointed star, Y-up, made by nt3d's sample generator.\nmtllib star.mtl\no star")
	// Bottom ring, then top ring; the star lies in XZ and rises along Y.
	for _, y := range []float64{0, height} {
		for _, c := range corners {
			fmt.Fprintln(&o, "v", number(c[0]), number(y), number(c[1]))
		}
	}
	perimeter := 0.0
	for i := range n {
		p, q := corners[i], corners[(i+1)%n]
		perimeter += math.Hypot(q[0]-p[0], q[1]-p[1])
	}
	along := 0.0
	for i := range n + 1 {
		for _, v := range []float64{0, 1} {
			fmt.Fprintln(&o, "vt", number(along/perimeter*4), number(v))
		}
		p, q := corners[i%n], corners[(i+1)%n]
		along += math.Hypot(q[0]-p[0], q[1]-p[1])
	}
	// Corners run counter-clockwise from +X toward +Z, which faces -Y.
	fmt.Fprintln(&o, "usemtl enamel")
	bottom, top := "f", "f"
	for i := range n {
		bottom += " " + strconv.Itoa(i+1)
		top += " " + strconv.Itoa(2*n-i)
	}
	fmt.Fprintln(&o, bottom)
	fmt.Fprintln(&o, top)
	fmt.Fprintln(&o, "usemtl wood")
	for i := range n {
		j := (i + 1) % n
		// Wound outward: bottom i, top i, top j, bottom j.
		fmt.Fprintf(&o, "f %d/%d %d/%d %d/%d %d/%d\n", i+1, 2*i+1, n+i+1, 2*i+2, n+j+1, 2*i+4, j+1, 2*i+3)
	}
	files := map[string][]byte{
		"star.obj": []byte(o.String()),
		"star.mtl": []byte("newmtl enamel\nKd 0.86 0.22 0.2\nnewmtl wood\nKd 0.62 0.42 0.22\nmap_Kd wood.png\n"),
		"wood.png": woodTexture(),
	}
	// The decoder must find the caps and sides wound outward, Y-up or not.
	m, err := nt3dmesh.DecodeOBJ(files["star.obj"], &nt3dmesh.Options{Open: func(name string) ([]byte, error) { return files[name], nil }, Up: nt3dmesh.YUp})
	if err != nil {
		return err
	}
	merged, err := nt3d.MergeGeometry(m.Objects[0].Geometry, nt3d.Geometry{Vertices: m.Objects[1].Geometry.Vertices, Indices: m.Objects[1].Geometry.Indices})
	if err != nil {
		return err
	}
	if signedVolume(merged) <= 0 || len(m.Warnings) != 0 {
		return fmt.Errorf("star winds inward or warns: %v", m.Warnings)
	}
	for name, data := range files {
		if err := os.WriteFile(name, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func encodePNG(img image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		panic(err)
	}
	return b.Bytes()
}

func woodTexture() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 256, 64))
	for y := range 64 {
		for x := range 256 {
			grain := math.Sin(float64(y)*.45+3*math.Sin(float64(x)*.05)) * .5
			ring := .5 + .5*math.Sin(float64(x)*.11+grain*4)
			img.SetRGBA(x, y, color.RGBA{uint8(150 + 60*ring), uint8(98 + 40*ring), uint8(52 + 22*ring), 255})
		}
	}
	return encodePNG(img)
}

func screenTexture() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := range 48 {
		for x := range 64 {
			c := color.RGBA{18, 40, 52, 255}
			if y%6 == 0 {
				c = color.RGBA{24, 56, 70, 255} // Scan lines.
			}
			eye := func(cx float64) bool { return math.Hypot(float64(x)-cx, float64(y)-17) < 5 }
			smile := math.Abs(math.Hypot(float64(x)-32, float64(y)-20)-15) < 2 && y > 26
			if eye(21) || eye(43) || smile {
				c = color.RGBA{120, 240, 200, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return encodePNG(img)
}

// meshXML writes a 3MF mesh; attributes gives each triangle's own.
func meshXML(g nt3d.Geometry, attributes func(triangle int) string) string {
	var b strings.Builder
	number := func(v float32) string { return strconv.FormatFloat(float64(v), 'g', 6, 32) }
	b.WriteString("<mesh><vertices>\n")
	for _, v := range g.Vertices {
		fmt.Fprintf(&b, "<vertex x=\"%s\" y=\"%s\" z=\"%s\"/>\n", number(v.Position.X), number(v.Position.Y), number(v.Position.Z))
	}
	b.WriteString("</vertices><triangles>\n")
	for i := 0; i+2 < len(g.Indices); i += 3 {
		extra := ""
		if attributes != nil {
			extra = attributes(i / 3)
		}
		fmt.Fprintf(&b, "<triangle v1=\"%d\" v2=\"%d\" v3=\"%d\"%s/>\n", g.Indices[i], g.Indices[i+1], g.Indices[i+2], extra)
	}
	b.WriteString("</triangles></mesh>")
	return b.String()
}

// rotateToX turns a shape built along Z to lie along X.
var rotateToX = nt3dmath.Mat4{0, 0, -1, 0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 1}

func robot() error {
	must := func(g nt3d.Geometry, err error) nt3d.Geometry {
		if err != nil {
			panic(err)
		}
		return g
	}
	move := func(g nt3d.Geometry, m nt3dmath.Mat4) nt3d.Geometry { return must(nt3d.TransformGeometry(g, m)) }
	at := func(x, y, z float32) nt3dmath.Mat4 { return nt3dmath.Translation(nt3dmath.Vec3{X: x, Y: y, Z: z}) }

	body := move(must(nt3d.BuildCylinder(nt3d.Cylinder{BottomRadius: .6, TopRadius: .55, Height: 1.4, Segments: 24})), at(0, 0, .9))
	head := move(must(nt3d.BuildSphere(nt3d.Sphere{Radius: .45, Segments: 24, Rings: 12})), at(0, 0, 2.05))
	arm := must(nt3d.MergeGeometry(
		move(must(nt3d.BuildCylinder(nt3d.Cylinder{BottomRadius: .12, TopRadius: .1, Height: .8, Segments: 12})), at(.4, 0, 0).Mul(rotateToX)),
		move(must(nt3d.BuildCylinder(nt3d.Cylinder{BottomRadius: .2, Height: .3, Segments: 12})), at(.95, 0, 0).Mul(rotateToX)),
	))
	antenna := must(nt3d.MergeGeometry(
		move(must(nt3d.BuildCylinder(nt3d.Cylinder{BottomRadius: .03, TopRadius: .03, Height: .4, Segments: 8})), at(0, 0, 2.62)),
		move(must(nt3d.BuildSphere(nt3d.Sphere{Radius: .08, Segments: 12, Rings: 6})), at(0, 0, 2.85)),
	))
	screen := nt3d.Geometry{Vertices: []nt3d.Vertex{
		{Position: nt3dmath.Vec3{X: -.3, Y: -.63, Z: .8}}, {Position: nt3dmath.Vec3{X: .3, Y: -.63, Z: .8}},
		{Position: nt3dmath.Vec3{X: .3, Y: -.63, Z: 1.25}}, {Position: nt3dmath.Vec3{X: -.3, Y: -.63, Z: 1.25}},
	}, Indices: []uint32{0, 1, 2, 0, 2, 3}}

	// The head shades from sky to white by height, a color at each corner.
	const shades = 8
	var gradient strings.Builder
	for i := range shades {
		f := float64(i) / (shades - 1)
		fmt.Fprintf(&gradient, `<m:color color="#%02X%02X%02X"/>`, uint8(90+165*f), uint8(170+85*f), 255)
	}
	level := func(v nt3d.Vertex) int {
		return min(shades-1, max(0, int((v.Position.Z-1.6)/.9*shades)))
	}
	headCorners := func(t int) string {
		i := head.Indices[3*t : 3*t+3]
		return fmt.Sprintf(` pid="20" p1="%d" p2="%d" p3="%d"`, level(head.Vertices[i[0]]), level(head.Vertices[i[1]]), level(head.Vertices[i[2]]))
	}
	// The body is painted in stripes, as a slicer paints: filament 2 and 3,
	// with one face split between filaments.
	stripes := func(t int) string {
		if t >= 48 {
			return "" // The caps.
		}
		switch t / 2 % 4 {
		case 0:
			return ` paint_color="8"`
		case 2:
			if t == 4 {
				return ` paint_color="04883"`
			}
			return ` paint_color="0C"`
		}
		return ""
	}
	screenUV := func(t int) string {
		if t == 0 {
			return ` pid="31" p1="0" p2="1" p3="2"`
		}
		return ` pid="31" p1="0" p2="2" p3="3"`
	}
	const ns = `xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02" xmlns:m="http://schemas.microsoft.com/3dmanufacturing/material/2015/02" xmlns:p="http://schemas.microsoft.com/3dmanufacturing/production/2015/06"`
	objects := `<?xml version="1.0" encoding="UTF-8"?>
<model unit="millimeter" ` + ns + ` requiredextensions="p">
<resources>
<m:colorgroup id="20">` + gradient.String() + `</m:colorgroup>
<m:texture2d id="30" path="/3D/Textures/screen.png" contenttype="image/png"/>
<m:texture2dgroup id="31" texid="30"><m:tex2coord u="0" v="0"/><m:tex2coord u="1" v="0"/><m:tex2coord u="1" v="1"/><m:tex2coord u="0" v="1"/></m:texture2dgroup>
<basematerials id="40"><base name="signal red" displaycolor="#E03A3AFF"/></basematerials>
<object id="1" type="model">` + meshXML(body, stripes) + `</object>
<object id="2" type="model">` + meshXML(head, headCorners) + `</object>
<object id="3" type="model">` + meshXML(arm, nil) + `</object>
<object id="4" type="model">` + meshXML(screen, screenUV) + `</object>
<object id="5" type="model" pid="40" pindex="0">` + meshXML(antenna, nil) + `</object>
</resources>
</model>`
	part := `/3D/Objects/robot.model`
	component := func(id int, transform string) string {
		return fmt.Sprintf(`<component p:path="%s" objectid="%d" transform="%s"/>`, part, id, transform)
	}
	root := `<?xml version="1.0" encoding="UTF-8"?>
<model unit="millimeter" ` + ns + ` requiredextensions="p">
<metadata name="Title">Robot</metadata>
<metadata name="Designer">nt3d sample generator</metadata>
<metadata name="Description">Components, a mirrored arm, corner colors, painted stripes, and a textured screen.</metadata>
<resources><object id="10" type="model"><components>
` + component(1, "1 0 0 0 1 0 0 0 1 0 0 0") + component(2, "1 0 0 0 1 0 0 0 1 0 0 0") +
		component(3, "1 0 0 0 1 0 0 0 1 .55 0 1.3") + component(3, "-1 0 0 0 1 0 0 0 1 -.55 0 1.3") +
		component(4, "1 0 0 0 1 0 0 0 1 0 0 0") + component(5, "1 0 0 0 1 0 0 0 1 0 0 0") + `
</components></object></resources>
<build><item objectid="10"/></build>
</model>`
	files := map[string]string{
		"[Content_Types].xml":              `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml"/><Default Extension="png" ContentType="image/png"/><Default Extension="config" ContentType="text/xml"/></Types>`,
		"_rels/.rels":                      `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Target="/3D/3dmodel.model" Id="rel0" Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"/><Relationship Target="/Metadata/plate_1.png" Id="rel1" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/thumbnail"/></Relationships>`,
		"3D/3dmodel.model":                 root,
		"3D/Objects/robot.model":           objects,
		"3D/Textures/screen.png":           string(screenTexture()),
		"Metadata/project_settings.config": `{"filament_colour": ["#C9CED6", "#2F6FD6", "#F2A93B"], "filament_type": ["PLA", "PLA", "PLA"]}`,
		"Metadata/model_settings.config": `<?xml version="1.0" encoding="UTF-8"?><config><object id="10">
<metadata key="name" value="Robot"/><metadata key="extruder" value="1"/>
<part id="3" subtype="normal_part"><metadata key="name" value="arm"/><metadata key="extruder" value="3"/></part>
</object></config>`,
	}
	pack := func() ([]byte, error) {
		var b bytes.Buffer
		w := zip.NewWriter(&b)
		for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "3D/3dmodel.model", "3D/Objects/robot.model", "3D/Textures/screen.png", "Metadata/project_settings.config", "Metadata/model_settings.config", "Metadata/plate_1.png"} {
			content, ok := files[name]
			if !ok {
				continue
			}
			f, err := w.Create(name)
			if err != nil {
				return nil, err
			}
			if _, err := f.Write([]byte(content)); err != nil {
				return nil, err
			}
		}
		err := w.Close()
		return b.Bytes(), err
	}
	data, err := pack()
	if err != nil {
		return err
	}
	// The thumbnail is nt3d's own rendering of the package.
	m, err := nt3dmesh.Decode3MF(data, nil)
	if err != nil {
		return err
	}
	if len(m.Warnings) != 0 {
		return fmt.Errorf("warnings: %v", m.Warnings)
	}
	s := nt3d.NewScene()
	if _, err := m.AddTo(s, nt3d.Root, "robot"); err != nil {
		return err
	}
	camera := nt3d.DefaultCamera()
	camera.Alpha, camera.Beta = 15, -70
	camera = camera.Fit(s.Bounds(), 1)
	f, err := s.Frame(camera, 320, 320)
	if err != nil {
		return err
	}
	img, _, err := nt3d.Snapshot(f, nt3d.Software, nil)
	if err != nil {
		return err
	}
	files["Metadata/plate_1.png"] = string(encodePNG(img))
	if data, err = pack(); err != nil {
		return err
	}
	return os.WriteFile("robot.3mf", data, 0o644)
}
